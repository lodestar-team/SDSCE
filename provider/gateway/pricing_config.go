package gateway

import (
	"fmt"
	"os"

	sds "github.com/graphprotocol/substreams-data-service"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"gopkg.in/yaml.v3"
)

var defaultRAVRequestThreshold = sds.MustNewGRT("10 GRT")

// ProviderPricingConfig is the provider-gateway runtime pricing policy loaded from YAML.
// It includes the shared billing prices plus provider-only RAV request policy.
type ProviderPricingConfig struct {
	PricePerBlock       sds.GRT
	PricePerByte        sds.GRT
	RAVRequestThreshold sds.GRT
}

type providerPricingConfigYAML struct {
	PricePerBlock       sds.GRT  `yaml:"price_per_block"`
	PricePerByte        sds.GRT  `yaml:"price_per_byte"`
	RAVRequestThreshold *sds.GRT `yaml:"rav_request_threshold"`
}

// DefaultRAVRequestThreshold returns the fallback provider-side RAV request threshold.
func DefaultRAVRequestThreshold() sds.GRT {
	return defaultRAVRequestThreshold
}

// DefaultProviderPricingConfig returns the default provider pricing and RAV request policy.
func DefaultProviderPricingConfig() *ProviderPricingConfig {
	pricingConfig := sidecar.DefaultPricingConfig()
	return &ProviderPricingConfig{
		PricePerBlock:       pricingConfig.PricePerBlock,
		PricePerByte:        pricingConfig.PricePerByte,
		RAVRequestThreshold: DefaultRAVRequestThreshold(),
	}
}

// LoadProviderPricingConfig loads provider pricing and runtime request policy from a YAML file.
func LoadProviderPricingConfig(path string) (*ProviderPricingConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading pricing config: %w", err)
	}

	return ParseProviderPricingConfig(data)
}

// ParseProviderPricingConfig parses provider pricing and runtime request policy from YAML bytes.
func ParseProviderPricingConfig(data []byte) (*ProviderPricingConfig, error) {
	var raw providerPricingConfigYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing pricing config: %w", err)
	}

	threshold := DefaultRAVRequestThreshold()
	if raw.RAVRequestThreshold != nil {
		if raw.RAVRequestThreshold.IsZero() {
			return nil, fmt.Errorf("invalid rav_request_threshold: must be greater than zero")
		}
		threshold = *raw.RAVRequestThreshold
	}

	return &ProviderPricingConfig{
		PricePerBlock:       raw.PricePerBlock,
		PricePerByte:        raw.PricePerByte,
		RAVRequestThreshold: threshold,
	}, nil
}

// ToPricingConfig returns the shared billing pricing config used by runtime/session code.
func (c *ProviderPricingConfig) ToPricingConfig() *sidecar.PricingConfig {
	if c == nil {
		return nil
	}

	return &sidecar.PricingConfig{
		PricePerBlock: c.PricePerBlock,
		PricePerByte:  c.PricePerByte,
	}
}
