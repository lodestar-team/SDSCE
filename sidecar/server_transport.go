package sidecar

import (
	"fmt"

	"github.com/streamingfast/dgrpc/server"
)

type ServerTransportConfig struct {
	Plaintext   bool
	TLSCertFile string
	TLSKeyFile  string
}

func (c ServerTransportConfig) Validate(component string) error {
	if c.Plaintext {
		if c.TLSCertFile != "" || c.TLSKeyFile != "" {
			return fmt.Errorf("%s plaintext transport cannot be combined with TLS cert/key files", component)
		}
		return nil
	}

	if c.TLSCertFile == "" || c.TLSKeyFile == "" {
		return fmt.Errorf("%s secure transport requires both <tls-cert-file> and <tls-key-file>, or explicit --plaintext", component)
	}

	return nil
}

func (c ServerTransportConfig) DGRPCOption(component string) (server.Option, error) {
	if err := c.Validate(component); err != nil {
		return nil, err
	}

	if c.Plaintext {
		return server.WithPlainTextServer(), nil
	}

	tlsConfig, err := server.SecuredByX509KeyPair(c.TLSCertFile, c.TLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("%s TLS config: %w", component, err)
	}

	return server.WithSecureServer(tlsConfig), nil
}
