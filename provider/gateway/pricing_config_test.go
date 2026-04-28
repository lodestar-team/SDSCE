package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseProviderPricingConfig_WithThreshold(t *testing.T) {
	config, err := ParseProviderPricingConfig([]byte(`
price_per_block: "0.000001 GRT"
price_per_byte: "0.0000000001 GRT"
rav_request_threshold: "2 GRT"
`))
	require.NoError(t, err)

	assert.Equal(t, "0.000001 GRT", config.PricePerBlock.String())
	assert.Equal(t, "0.0000000001 GRT", config.PricePerByte.String())
	assert.Equal(t, "2 GRT", config.RAVRequestThreshold.String())
}

func TestParseProviderPricingConfig_DefaultThreshold(t *testing.T) {
	config, err := ParseProviderPricingConfig([]byte(`
price_per_block: "0.000001 GRT"
price_per_byte: "0.0000000001 GRT"
`))
	require.NoError(t, err)

	assert.Equal(t, "10 GRT", config.RAVRequestThreshold.String())
}

func TestParseProviderPricingConfig_RejectsZeroThreshold(t *testing.T) {
	config, err := ParseProviderPricingConfig([]byte(`
price_per_block: "0.000001 GRT"
price_per_byte: "0.0000000001 GRT"
rav_request_threshold: "0 GRT"
`))
	require.Error(t, err)
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "rav_request_threshold")
}

func TestDefaultProviderPricingConfig(t *testing.T) {
	config := DefaultProviderPricingConfig()

	require.NotNil(t, config)
	assert.Equal(t, "10 GRT", config.RAVRequestThreshold.String())
}
