package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadConsumerSidecarRuntimeFileConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "consumer-sidecar.yaml")

	err := os.WriteFile(path, []byte(`
payer_address: "  0x1111111111111111111111111111111111111111  "
receiver_address: "  0x2222222222222222222222222222222222222222 "
data_service_address: " 0x3333333333333333333333333333333333333333 "
oracle_endpoint: "  http://oracle.example:9000 "
provider_control_plane_endpoint: " http://provider.example:9001 "
`), 0o600)
	require.NoError(t, err)

	cfg, err := loadConsumerSidecarRuntimeFileConfig(path)
	require.NoError(t, err)
	require.Equal(t, "0x1111111111111111111111111111111111111111", cfg.PayerAddress)
	require.Equal(t, "0x2222222222222222222222222222222222222222", cfg.ReceiverAddress)
	require.Equal(t, "0x3333333333333333333333333333333333333333", cfg.DataServiceAddress)
	require.Equal(t, "http://oracle.example:9000", cfg.OracleEndpoint)
	require.Equal(t, "http://provider.example:9001", cfg.ProviderControlPlaneEndpoint)
}
