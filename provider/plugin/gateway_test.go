package plugin

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	sidecarlib "github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/stretchr/testify/require"
)

func TestPluginGatewayTransportOption(t *testing.T) {
	t.Run("plaintext is explicit", func(t *testing.T) {
		g := &PluginGateway{
			transportConfig: sidecarlib.ServerTransportConfig{
				Plaintext: true,
			},
		}

		opt, err := g.transportOption()
		require.NoError(t, err)
		require.NotNil(t, opt)
	})

	t.Run("tls is explicit", func(t *testing.T) {
		certFile, keyFile := writeSelfSignedCertPair(t)

		g := &PluginGateway{
			transportConfig: sidecarlib.ServerTransportConfig{
				TLSCertFile: certFile,
				TLSKeyFile:  keyFile,
			},
		}

		opt, err := g.transportOption()
		require.NoError(t, err)
		require.NotNil(t, opt)
	})

	t.Run("plaintext cannot be combined with tls material", func(t *testing.T) {
		g := &PluginGateway{
			transportConfig: sidecarlib.ServerTransportConfig{
				Plaintext:   true,
				TLSCertFile: "server.crt",
				TLSKeyFile:  "server.key",
			},
		}

		_, err := g.transportOption()
		require.Error(t, err)
		require.Contains(t, err.Error(), "plugin gateway plaintext transport cannot be combined")
	})
}

func writeSelfSignedCertPair(t *testing.T) (string, string) {
	t.Helper()

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "plugin-gateway-test",
		},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &privKey.PublicKey, privKey)
	require.NoError(t, err)

	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")

	certOut, err := os.Create(certPath)
	require.NoError(t, err)
	defer certOut.Close()
	require.NoError(t, pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}))

	keyOut, err := os.Create(keyPath)
	require.NoError(t, err)
	defer keyOut.Close()
	require.NoError(t, pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privKey)}))

	return certPath, keyPath
}
