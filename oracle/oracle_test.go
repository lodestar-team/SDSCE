package oracle

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	oraclev1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/oracle/v1"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDiscoverProviders(t *testing.T) {
	catalog, err := ParseCatalog([]byte(`
providers:
  - id: provider-b
    service_provider: "0x1111111111111111111111111111111111111111"
    control_plane_endpoint: "https://provider-b.example.com:9001"
  - id: provider-a
    service_provider: "0x2222222222222222222222222222222222222222"
    control_plane_endpoint: "https://provider-a.example.com:9001"
networks:
  mainnet:
    pricing:
      price_per_block: "0.000001 GRT"
      price_per_byte: "0.0000000001 GRT"
    provider_ids:
      - provider-b
      - provider-a
    preferred_provider_id: "provider-b"
`))
	require.NoError(t, err)

	oracle := New(&Config{Catalog: catalog}, zap.NewNop())

	resp, err := oracle.DiscoverProviders(context.Background(), connect.NewRequest(&oraclev1.DiscoverProvidersRequest{
		Network: "mainnet",
	}))
	require.NoError(t, err)

	require.NotNil(t, resp.Msg)
	assert.Equal(t, "mainnet", resp.Msg.Network)
	assert.Equal(t, "provider-b", resp.Msg.RecommendedProviderId)
	assert.Equal(t, "provider-b", resp.Msg.SelectedProvider.ProviderId)
	require.Len(t, resp.Msg.EligibleProviders, 2)
	assert.Equal(t, "provider-a", resp.Msg.EligibleProviders[0].ProviderId)
	assert.Equal(t, "provider-b", resp.Msg.EligibleProviders[1].ProviderId)
	pricePerBlock := resp.Msg.CanonicalPricing.PricePerBlock.ToNative()
	pricePerByte := resp.Msg.CanonicalPricing.PricePerByte.ToNative()
	assert.Equal(t, "0.000001 GRT", pricePerBlock.String())
	assert.Equal(t, "0.0000000001 GRT", pricePerByte.String())
}

func TestDiscoverProviders_EmptyNetwork(t *testing.T) {
	oracle := New(&Config{Catalog: &Catalog{}}, zap.NewNop())

	_, err := oracle.DiscoverProviders(context.Background(), connect.NewRequest(&oraclev1.DiscoverProvidersRequest{}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestDiscoverProviders_UnknownNetwork(t *testing.T) {
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
`))
	require.NoError(t, err)

	oracle := New(&Config{Catalog: catalog}, zap.NewNop())

	_, err = oracle.DiscoverProviders(context.Background(), connect.NewRequest(&oraclev1.DiscoverProvidersRequest{
		Network: "arbitrum-one",
	}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

func TestOracleRun_InvalidTransportConfig(t *testing.T) {
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
`))
	require.NoError(t, err)

	oracle := New(&Config{
		ListenAddr: "127.0.0.1:0",
		Catalog:    catalog,
		TransportConfig: sidecar.ServerTransportConfig{
			TLSCertFile: "missing-cert.pem",
			TLSKeyFile:  "missing-key.pem",
		},
	}, zap.NewNop())

	oracle.Run()

	select {
	case <-oracle.Terminated():
	case <-time.After(2 * time.Second):
		t.Fatal("oracle did not terminate after invalid transport configuration")
	}

	require.Error(t, oracle.Err())
}

func TestOracleRun_PlaintextTransportStarts(t *testing.T) {
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
`))
	require.NoError(t, err)

	oracle := New(&Config{
		ListenAddr: "127.0.0.1:0",
		Catalog:    catalog,
		TransportConfig: sidecar.ServerTransportConfig{
			Plaintext: true,
		},
	}, zap.NewNop())

	go oracle.Run()

	time.Sleep(200 * time.Millisecond)
	assert.False(t, oracle.IsTerminated())

	oracle.Shutdown(nil)

	select {
	case <-oracle.Terminated():
	case <-time.After(2 * time.Second):
		t.Fatal("oracle did not terminate after shutdown")
	}
}
