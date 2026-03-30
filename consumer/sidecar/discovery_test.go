package sidecar

import (
	"testing"

	sds "github.com/graphprotocol/substreams-data-service"
	sidecarlib "github.com/graphprotocol/substreams-data-service/sidecar"
	pbsubstreams "github.com/streamingfast/substreams/pb/sf/substreams/v1"
	"github.com/stretchr/testify/require"
)

func TestResolveRequestedNetwork_UsesPackageNetwork(t *testing.T) {
	network, err := resolveRequestedNetwork(&pbsubstreams.Package{Network: "eth-mainnet"}, "")
	require.NoError(t, err)
	require.Equal(t, "mainnet", network)
}

func TestResolveRequestedNetwork_FallsBackToRequestedNetwork(t *testing.T) {
	network, err := resolveRequestedNetwork(&pbsubstreams.Package{}, "Arbitrum One")
	require.NoError(t, err)
	require.Equal(t, "arbitrum-one", network)
}

func TestResolveRequestedNetwork_RejectsConflict(t *testing.T) {
	_, err := resolveRequestedNetwork(&pbsubstreams.Package{Network: "mainnet"}, "arbitrum-one")
	require.Error(t, err)
	require.Contains(t, err.Error(), "conflicts")
}

func TestResolveRequestedNetwork_RejectsMissingSources(t *testing.T) {
	_, err := resolveRequestedNetwork(nil, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "is required")
}

func TestEffectiveSessionPricing_RejectsHigherProviderBlockPrice(t *testing.T) {
	_, err := effectiveSessionPricing(
		&sidecarlib.PricingConfig{
			PricePerBlock: sds.MustNewGRT("2 GRT"),
			PricePerByte:  sds.ZeroGRT(),
		},
		&sidecarlib.PricingConfig{
			PricePerBlock: sds.MustNewGRT("1 GRT"),
			PricePerByte:  sds.ZeroGRT(),
		},
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "price_per_block")
}

func TestEffectiveSessionPricing_RejectsHigherProviderBytePrice(t *testing.T) {
	_, err := effectiveSessionPricing(
		&sidecarlib.PricingConfig{
			PricePerBlock: sds.ZeroGRT(),
			PricePerByte:  sds.MustNewGRT("2 GRT"),
		},
		&sidecarlib.PricingConfig{
			PricePerBlock: sds.ZeroGRT(),
			PricePerByte:  sds.MustNewGRT("1 GRT"),
		},
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "price_per_byte")
}

func TestEffectiveSessionPricing_AcceptsEqualProviderPricing(t *testing.T) {
	pricing := &sidecarlib.PricingConfig{
		PricePerBlock: sds.MustNewGRT("1 GRT"),
		PricePerByte:  sds.MustNewGRT("1 GRT"),
	}

	effective, err := effectiveSessionPricing(pricing, pricing)
	require.NoError(t, err)
	require.Same(t, pricing, effective)
}

func TestEffectiveSessionPricing_AcceptsLowerProviderPricing(t *testing.T) {
	effective, err := effectiveSessionPricing(
		&sidecarlib.PricingConfig{
			PricePerBlock: sds.MustNewGRT("1 GRT"),
			PricePerByte:  sds.MustNewGRT("1 GRT"),
		},
		&sidecarlib.PricingConfig{
			PricePerBlock: sds.MustNewGRT("2 GRT"),
			PricePerByte:  sds.MustNewGRT("2 GRT"),
		},
	)
	require.NoError(t, err)
	require.Equal(t, "1 GRT", effective.PricePerBlock.String())
	require.Equal(t, "1 GRT", effective.PricePerByte.String())
}

func TestEffectiveSessionPricing_FallsBackToOraclePricing(t *testing.T) {
	oraclePricing := &sidecarlib.PricingConfig{
		PricePerBlock: sds.MustNewGRT("3 GRT"),
		PricePerByte:  sds.MustNewGRT("4 GRT"),
	}

	effective, err := effectiveSessionPricing(nil, oraclePricing)
	require.NoError(t, err)
	require.Same(t, oraclePricing, effective)
}
