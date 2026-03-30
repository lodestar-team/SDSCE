package oracle

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCatalog(t *testing.T) {
	catalog, err := ParseCatalog([]byte(`
providers:
  - id: z-provider
    service_provider: "0x1111111111111111111111111111111111111111"
    control_plane_endpoint: "provider-b.example.com:9001"
  - id: a-provider
    service_provider: "0x2222222222222222222222222222222222222222"
    control_plane_endpoint: "https://provider-a.example.com:9001?insecure=true"
networks:
  mainnet:
    pricing:
      price_per_block: "0.000001 GRT"
      price_per_byte: "0.0000000001 GRT"
    provider_ids:
      - z-provider
      - a-provider
    preferred_provider_id: "z-provider"
`))
	require.NoError(t, err)

	discovery, found := catalog.Discover("mainnet")
	require.True(t, found)
	require.NotNil(t, discovery.Pricing)

	assert.Equal(t, "0.000001 GRT", discovery.Pricing.PricePerBlock.String())
	assert.Equal(t, "0.0000000001 GRT", discovery.Pricing.PricePerByte.String())
	assert.Equal(t, "z-provider", discovery.RecommendedProvider.ID)

	require.Len(t, discovery.EligibleProviders, 2)
	assert.Equal(t, "a-provider", discovery.EligibleProviders[0].ID)
	assert.Equal(t, "https://provider-a.example.com:9001?insecure=true", discovery.EligibleProviders[0].ControlPlaneEndpoint)
	assert.Equal(t, "https://provider-b.example.com:9001", discovery.EligibleProviders[1].ControlPlaneEndpoint)
}

func TestParseCatalog_DeterministicFallbackRecommendation(t *testing.T) {
	catalog, err := ParseCatalog([]byte(`
providers:
  - id: z-provider
    service_provider: "0x1111111111111111111111111111111111111111"
    control_plane_endpoint: "https://provider-b.example.com:9001"
  - id: a-provider
    service_provider: "0x2222222222222222222222222222222222222222"
    control_plane_endpoint: "https://provider-a.example.com:9001"
networks:
  arbitrum-one:
    pricing:
      price_per_block: "1 GRT"
      price_per_byte: "0 GRT"
    provider_ids:
      - z-provider
      - a-provider
`))
	require.NoError(t, err)

	discovery, found := catalog.Discover("arbitrum-one")
	require.True(t, found)
	assert.Equal(t, "a-provider", discovery.RecommendedProvider.ID)
}

func TestParseCatalog_RejectsDuplicateProviderIDs(t *testing.T) {
	catalog, err := ParseCatalog([]byte(`
providers:
  - id: provider-a
    service_provider: "0x1111111111111111111111111111111111111111"
    control_plane_endpoint: "https://provider-a.example.com:9001"
  - id: provider-a
    service_provider: "0x2222222222222222222222222222222222222222"
    control_plane_endpoint: "https://provider-b.example.com:9001"
networks:
  mainnet:
    pricing:
      price_per_block: "1 GRT"
      price_per_byte: "0 GRT"
    provider_ids:
      - provider-a
`))
	require.Error(t, err)
	assert.Nil(t, catalog)
	assert.Contains(t, err.Error(), "duplicate provider id")
}

func TestParseCatalog_RejectsUnknownProviderReference(t *testing.T) {
	catalog, err := ParseCatalog([]byte(`
providers:
  - id: provider-a
    service_provider: "0x1111111111111111111111111111111111111111"
    control_plane_endpoint: "https://provider-a.example.com:9001"
networks:
  mainnet:
    pricing:
      price_per_block: "1 GRT"
      price_per_byte: "0 GRT"
    provider_ids:
      - provider-b
`))
	require.Error(t, err)
	assert.Nil(t, catalog)
	assert.Contains(t, err.Error(), "unknown provider id")
}

func TestParseCatalog_RejectsEmptyProviderSet(t *testing.T) {
	catalog, err := ParseCatalog([]byte(`
providers:
  - id: provider-a
    service_provider: "0x1111111111111111111111111111111111111111"
    control_plane_endpoint: "https://provider-a.example.com:9001"
networks:
  mainnet:
    pricing:
      price_per_block: "1 GRT"
      price_per_byte: "0 GRT"
    provider_ids: []
`))
	require.Error(t, err)
	assert.Nil(t, catalog)
	assert.Contains(t, err.Error(), "at least one provider id")
}

func TestParseCatalog_RejectsInvalidPreferredProvider(t *testing.T) {
	catalog, err := ParseCatalog([]byte(`
providers:
  - id: provider-a
    service_provider: "0x1111111111111111111111111111111111111111"
    control_plane_endpoint: "https://provider-a.example.com:9001"
networks:
  mainnet:
    pricing:
      price_per_block: "1 GRT"
      price_per_byte: "0 GRT"
    provider_ids:
      - provider-a
    preferred_provider_id: "provider-b"
`))
	require.Error(t, err)
	assert.Nil(t, catalog)
	assert.Contains(t, err.Error(), "preferred_provider_id")
}

func TestParseCatalog_RejectsInvalidEndpoint(t *testing.T) {
	catalog, err := ParseCatalog([]byte(`
providers:
  - id: provider-a
    service_provider: "0x1111111111111111111111111111111111111111"
    control_plane_endpoint: "://bad-endpoint"
networks:
  mainnet:
    pricing:
      price_per_block: "1 GRT"
      price_per_byte: "0 GRT"
    provider_ids:
      - provider-a
`))
	require.Error(t, err)
	assert.Nil(t, catalog)
	assert.Contains(t, err.Error(), "control_plane_endpoint")
}

func TestParseCatalog_RejectsInvalidPricingConfig(t *testing.T) {
	t.Run("malformed pricing value", func(t *testing.T) {
		catalog, err := ParseCatalog([]byte(`
providers:
  - id: provider-a
    service_provider: "0x1111111111111111111111111111111111111111"
    control_plane_endpoint: "https://provider-a.example.com:9001"
networks:
  mainnet:
    pricing:
      price_per_block: "wat"
      price_per_byte: "0 GRT"
    provider_ids:
      - provider-a
`))
		require.Error(t, err)
		assert.Nil(t, catalog)
	})

	t.Run("all zero prices", func(t *testing.T) {
		catalog, err := ParseCatalog([]byte(`
providers:
  - id: provider-a
    service_provider: "0x1111111111111111111111111111111111111111"
    control_plane_endpoint: "https://provider-a.example.com:9001"
networks:
  mainnet:
    pricing:
      price_per_block: "0 GRT"
      price_per_byte: "0 GRT"
    provider_ids:
      - provider-a
`))
		require.Error(t, err)
		assert.Nil(t, catalog)
		assert.Contains(t, err.Error(), "pricing")
	})
}
