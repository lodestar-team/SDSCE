package sidecar

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	oraclev1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/oracle/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/oracle/v1/oraclev1connect"
	sidecarlib "github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/streamingfast/eth-go"
	pbsubstreams "github.com/streamingfast/substreams/pb/sf/substreams/v1"
	"go.uber.org/zap"
)

type initProviderSelection struct {
	ControlPlaneEndpoint string
	ProviderID           string
	Network              string
	OraclePricing        *sidecarlib.PricingConfig
	Receiver             eth.Address
}

var networkAliases = map[string]string{
	"arb":              "arbitrum-one",
	"arb-one":          "arbitrum-one",
	"arbitrum":         "arbitrum-one",
	"arbitrum-mainnet": "arbitrum-one",
	"arbitrum-one":     "arbitrum-one",
	"base":             "base",
	"base-mainnet":     "base",
	"eth":              "mainnet",
	"eth-mainnet":      "mainnet",
	"ethereum":         "mainnet",
	"ethereum-mainnet": "mainnet",
	"mainnet":          "mainnet",
	"matic":            "polygon",
	"matic-mainnet":    "polygon",
	"polygon":          "polygon",
	"polygon-mainnet":  "polygon",
	"sepolia":          "sepolia",
}

func normalizeNetworkKey(raw string) (string, error) {
	network := strings.TrimSpace(raw)
	if network == "" {
		return "", nil
	}

	network = strings.ToLower(network)
	network = strings.ReplaceAll(network, "_", "-")
	network = strings.Join(strings.Fields(network), "-")
	if network == "" {
		return "", errors.New("network is required")
	}

	if canonical, found := networkAliases[network]; found {
		return canonical, nil
	}

	return network, nil
}

func resolveRequestedNetwork(pkg *pbsubstreams.Package, requested string) (string, error) {
	derivedNetwork, err := normalizeNetworkKey(pkg.GetNetwork())
	if err != nil {
		return "", fmt.Errorf("invalid <substreams_package.network>: %w", err)
	}

	requestedNetwork, err := normalizeNetworkKey(requested)
	if err != nil {
		return "", fmt.Errorf("invalid <requested_network>: %w", err)
	}

	if derivedNetwork != "" && requestedNetwork != "" && derivedNetwork != requestedNetwork {
		return "", fmt.Errorf("package-derived network %q conflicts with requested network %q", derivedNetwork, requestedNetwork)
	}

	if derivedNetwork != "" {
		return derivedNetwork, nil
	}
	if requestedNetwork != "" {
		return requestedNetwork, nil
	}

	return "", errors.New("either <substreams_package.network> or <requested_network> is required when <provider_control_plane_endpoint> is not set")
}

func effectiveSessionPricing(
	providerPricing *sidecarlib.PricingConfig,
	oraclePricing *sidecarlib.PricingConfig,
) (*sidecarlib.PricingConfig, error) {
	if oraclePricing == nil {
		return providerPricing, nil
	}
	if providerPricing == nil {
		return oraclePricing, nil
	}

	if providerPricing.PricePerBlock.Cmp(&oraclePricing.PricePerBlock) > 0 {
		return nil, fmt.Errorf("provider <pricing_config.price_per_block> %s exceeds oracle canonical price %s", providerPricing.PricePerBlock.String(), oraclePricing.PricePerBlock.String())
	}
	if providerPricing.PricePerByte.Cmp(&oraclePricing.PricePerByte) > 0 {
		return nil, fmt.Errorf("provider <pricing_config.price_per_byte> %s exceeds oracle canonical price %s", providerPricing.PricePerByte.String(), oraclePricing.PricePerByte.String())
	}

	return providerPricing, nil
}

func (s *Sidecar) resolveProviderSelection(
	ctx context.Context,
	expectedReceiver *eth.Address,
	directEndpoint string,
	substreamsPackage *pbsubstreams.Package,
	requestedNetwork string,
) (*initProviderSelection, error) {
	if endpoint := strings.TrimSpace(directEndpoint); endpoint != "" {
		if expectedReceiver == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("<receiver> is required when <provider_control_plane_endpoint> is set"))
		}

		return &initProviderSelection{
			ControlPlaneEndpoint: endpoint,
			Receiver:             *expectedReceiver,
		}, nil
	}

	oracleEndpoint := strings.TrimSpace(s.oracleEndpoint)
	if oracleEndpoint == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("consumer sidecar oracle discovery is not configured"))
	}

	network, err := resolveRequestedNetwork(substreamsPackage, requestedNetwork)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	parsedOracleEndpoint := sidecarlib.ParseEndpoint(oracleEndpoint)
	if parsedOracleEndpoint.URL == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("invalid configured oracle endpoint"))
	}

	oracleClient := oraclev1connect.NewOracleServiceClient(parsedOracleEndpoint.HTTPClient(), parsedOracleEndpoint.URL)
	oracleResp, err := oracleClient.DiscoverProviders(ctx, connect.NewRequest(&oraclev1.DiscoverProvidersRequest{
		Network: network,
	}))
	if err != nil {
		return nil, err
	}

	selectedProvider := oracleResp.Msg.GetSelectedProvider()
	if selectedProvider == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("oracle returned no selected provider"))
	}

	selectedProviderAddr := selectedProvider.GetServiceProvider()
	if selectedProviderAddr == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("oracle selected provider is missing <service_provider>"))
	}

	selectedReceiver, err := selectedProviderAddr.ToEth()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("oracle returned invalid selected provider <service_provider>: %w", err))
	}

	if expectedReceiver != nil && !sidecarlib.AddressesEqual(selectedReceiver, *expectedReceiver) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("oracle selected provider %q (%s) does not match <receiver> %s", selectedProvider.GetProviderId(), selectedReceiver, *expectedReceiver))
	}

	oraclePricing := oracleResp.Msg.GetCanonicalPricing()
	if oraclePricing == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("oracle returned no canonical pricing"))
	}

	oraclePricingConfig := oraclePricing.ToNative()
	if oraclePricingConfig == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("oracle returned invalid canonical pricing"))
	}

	return &initProviderSelection{
		ControlPlaneEndpoint: selectedProvider.GetControlPlaneEndpoint(),
		ProviderID:           selectedProvider.GetProviderId(),
		Network:              network,
		OraclePricing:        oraclePricingConfig,
		Receiver:             selectedReceiver,
	}, nil
}

func logPricingDeviation(logger *zap.Logger, oraclePricing, providerPricing *sidecarlib.PricingConfig) {
	if logger == nil || oraclePricing == nil || providerPricing == nil {
		return
	}

	if providerPricing.PricePerBlock.Cmp(&oraclePricing.PricePerBlock) == 0 &&
		providerPricing.PricePerByte.Cmp(&oraclePricing.PricePerByte) == 0 {
		return
	}

	logger.Info("provider pricing differs from oracle canonical pricing but remains within oracle ceiling",
		zap.String("oracle_price_per_block", oraclePricing.PricePerBlock.String()),
		zap.String("oracle_price_per_byte", oraclePricing.PricePerByte.String()),
		zap.String("provider_price_per_block", providerPricing.PricePerBlock.String()),
		zap.String("provider_price_per_byte", providerPricing.PricePerByte.String()),
	)
}
