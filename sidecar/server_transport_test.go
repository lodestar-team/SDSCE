package sidecar

import "testing"

import "github.com/stretchr/testify/require"

func TestServerTransportConfigValidate(t *testing.T) {
	t.Run("plaintext is explicit and valid", func(t *testing.T) {
		cfg := ServerTransportConfig{Plaintext: true}
		require.NoError(t, cfg.Validate("consumer sidecar"))
	})

	t.Run("secure mode requires cert and key", func(t *testing.T) {
		cfg := ServerTransportConfig{}
		require.Error(t, cfg.Validate("provider gateway"))
	})

	t.Run("plaintext rejects tls files", func(t *testing.T) {
		cfg := ServerTransportConfig{
			Plaintext:   true,
			TLSCertFile: "server.crt",
			TLSKeyFile:  "server.key",
		}
		require.Error(t, cfg.Validate("provider gateway"))
	})
}
