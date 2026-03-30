package oracle

import (
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"

	sds "github.com/graphprotocol/substreams-data-service"
	"github.com/streamingfast/eth-go"
	"gopkg.in/yaml.v3"
)

type Provider struct {
	ID                   string
	ServiceProvider      eth.Address
	ControlPlaneEndpoint string
}

type DiscoveryResult struct {
	Network             string
	Pricing             *sds.PricingConfig
	EligibleProviders   []Provider
	RecommendedProvider Provider
}

type Catalog struct {
	networks map[string]DiscoveryResult
}

type rawCatalog struct {
	Providers []rawProviderConfig            `yaml:"providers"`
	Networks  map[string]rawNetworkDiscovery `yaml:"networks"`
}

type rawProviderConfig struct {
	ID                   string `yaml:"id"`
	ServiceProvider      string `yaml:"service_provider"`
	ControlPlaneEndpoint string `yaml:"control_plane_endpoint"`
}

type rawNetworkDiscovery struct {
	Pricing             sds.PricingConfig `yaml:"pricing"`
	ProviderIDs         []string          `yaml:"provider_ids"`
	PreferredProviderID string            `yaml:"preferred_provider_id"`
}

func LoadCatalog(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading oracle config: %w", err)
	}

	return ParseCatalog(data)
}

func ParseCatalog(data []byte) (*Catalog, error) {
	var raw rawCatalog
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing oracle config: %w", err)
	}

	if len(raw.Networks) == 0 {
		return nil, fmt.Errorf("oracle config must define at least one network")
	}

	providers := make(map[string]Provider, len(raw.Providers))
	for _, provider := range raw.Providers {
		id := strings.TrimSpace(provider.ID)
		if id == "" {
			return nil, fmt.Errorf("provider id is required")
		}
		if _, found := providers[id]; found {
			return nil, fmt.Errorf("duplicate provider id %q", id)
		}

		serviceProvider, err := eth.NewAddress(strings.TrimSpace(provider.ServiceProvider))
		if err != nil {
			return nil, fmt.Errorf("invalid provider %q <service_provider> %q: %w", id, provider.ServiceProvider, err)
		}

		controlPlaneEndpoint, err := normalizeEndpoint(provider.ControlPlaneEndpoint)
		if err != nil {
			return nil, fmt.Errorf("invalid provider %q <control_plane_endpoint>: %w", id, err)
		}

		providers[id] = Provider{
			ID:                   id,
			ServiceProvider:      serviceProvider,
			ControlPlaneEndpoint: controlPlaneEndpoint,
		}
	}

	networks := make(map[string]DiscoveryResult, len(raw.Networks))
	for rawNetwork, rawDiscovery := range raw.Networks {
		network := strings.TrimSpace(rawNetwork)
		if network == "" {
			return nil, fmt.Errorf("network key is required")
		}

		pricing := rawDiscovery.Pricing
		if pricing.PricePerBlock.IsZero() && pricing.PricePerByte.IsZero() {
			return nil, fmt.Errorf("network %q pricing must define at least one non-zero price", network)
		}

		if len(rawDiscovery.ProviderIDs) == 0 {
			return nil, fmt.Errorf("network %q must define at least one provider id", network)
		}

		seenProviderIDs := make(map[string]struct{}, len(rawDiscovery.ProviderIDs))
		providerIDs := make([]string, 0, len(rawDiscovery.ProviderIDs))
		for _, rawProviderID := range rawDiscovery.ProviderIDs {
			providerID := strings.TrimSpace(rawProviderID)
			if providerID == "" {
				return nil, fmt.Errorf("network %q contains an empty provider id", network)
			}
			if _, found := seenProviderIDs[providerID]; found {
				return nil, fmt.Errorf("network %q contains duplicate provider id %q", network, providerID)
			}
			if _, found := providers[providerID]; !found {
				return nil, fmt.Errorf("network %q references unknown provider id %q", network, providerID)
			}

			seenProviderIDs[providerID] = struct{}{}
			providerIDs = append(providerIDs, providerID)
		}

		slices.Sort(providerIDs)
		eligibleProviders := make([]Provider, 0, len(providerIDs))
		for _, providerID := range providerIDs {
			eligibleProviders = append(eligibleProviders, providers[providerID])
		}

		recommendedProvider := eligibleProviders[0]
		preferredProviderID := strings.TrimSpace(rawDiscovery.PreferredProviderID)
		if preferredProviderID != "" {
			if _, found := seenProviderIDs[preferredProviderID]; !found {
				return nil, fmt.Errorf("network %q preferred_provider_id %q is not in provider_ids", network, preferredProviderID)
			}
			recommendedProvider = providers[preferredProviderID]
		}

		networks[network] = DiscoveryResult{
			Network:             network,
			Pricing:             &pricing,
			EligibleProviders:   eligibleProviders,
			RecommendedProvider: recommendedProvider,
		}
	}

	return &Catalog{networks: networks}, nil
}

func (c *Catalog) Discover(network string) (DiscoveryResult, bool) {
	if c == nil {
		return DiscoveryResult{}, false
	}

	discovery, found := c.networks[network]
	if !found {
		return DiscoveryResult{}, false
	}

	return discovery, true
}

func normalizeEndpoint(raw string) (string, error) {
	endpoint := strings.TrimSpace(raw)
	if endpoint == "" {
		return "", fmt.Errorf("endpoint is required")
	}

	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return "", fmt.Errorf("missing host")
	}

	return parsed.String(), nil
}
