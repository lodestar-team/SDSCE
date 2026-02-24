package sidecar

import (
	"testing"

	sds "github.com/graphprotocol/substreams-data-service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePricingConfig(t *testing.T) {
	yaml := `
price_per_block: "0.000001 GRT"
price_per_byte: "0.0000000001 GRT"
`
	config, err := ParsePricingConfig([]byte(yaml))
	require.NoError(t, err)

	// Verify block price
	assert.Equal(t, "0.000001 GRT", config.PricePerBlock.String())

	// Verify byte price
	assert.Equal(t, "0.0000000001 GRT", config.PricePerByte.String())
}

func TestParsePricingConfig_WithoutSuffix(t *testing.T) {
	// Test that plain numbers (without GRT suffix) are also accepted
	yaml := `
price_per_block: "0.000001"
price_per_byte: "0.0000000001"
`
	config, err := ParsePricingConfig([]byte(yaml))
	require.NoError(t, err)

	// Verify block price
	assert.Equal(t, "0.000001 GRT", config.PricePerBlock.String())

	// Verify byte price
	assert.Equal(t, "0.0000000001 GRT", config.PricePerByte.String())
}

func TestPricingConfig_CalculateUsageCost(t *testing.T) {
	config := DefaultPricingConfig()

	// Based on: $3/million blocks, $175/TiB, at $0.02602/GRT
	// 1 million blocks at 0.000115 GRT/block = 115 GRT
	// 10GB at 0.0000000061 GRT/byte = ~65.5 GRT (10*1024*1024*1024 bytes)
	// Total ~= 180.5 GRT
	cost := config.CalculateUsageCost(1000000, 10*1024*1024*1024)

	// Verify it's approximately 180 GRT
	lowBound, _ := sds.ParseGRT("175 GRT")
	highBound, _ := sds.ParseGRT("185 GRT")

	// Cost should be between 175 and 185 GRT
	assert.True(t, cost.Cmp(&lowBound) >= 0, "cost %s should be >= 175 GRT", cost.String())
	assert.True(t, cost.Cmp(&highBound) < 0, "cost %s should be < 185 GRT", cost.String())
}

func TestPricingConfig_CalculateUsageCost_BlocksOnly(t *testing.T) {
	blockPrice, _ := sds.ParseGRT("0.000001 GRT")

	config := &PricingConfig{
		PricePerBlock: blockPrice,
		PricePerByte:  sds.ZeroGRT(),
	}

	// 1 million blocks at 0.000001 GRT/block = 1 GRT
	cost := config.CalculateUsageCost(1000000, 1000)

	oneGRT, _ := sds.ParseGRT("1 GRT")
	assert.Equal(t, oneGRT.BigInt().String(), cost.BigInt().String())
}

func TestDefaultPricingConfig(t *testing.T) {
	config := DefaultPricingConfig()

	// Based on: $3/million blocks, $175/TiB, at $0.02602/GRT
	assert.Equal(t, "0.000115 GRT", config.PricePerBlock.String())
	assert.Equal(t, "0.0000000061 GRT", config.PricePerByte.String())
}
