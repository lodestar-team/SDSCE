// Package plugin provides SDS plugin registration for firehose-core.
// It registers "sds" scheme handlers with dauth, dsession, and dmetering
// that connect to an SDS provider gateway via gRPC/Connect.
//
// The plugins are gRPC clients - all business logic (service provider address,
// escrow address, etc.) is configured on the provider gateway server side.
// Plugin configuration only needs connection parameters.
//
// Usage in firehose-core:
//
//	common-auth-plugin: "sds://localhost:9003"
//	common-session-plugin: "sds://localhost:9003"
//	common-metering-plugin: "sds://localhost:9003?network=my-network"
//
// For local/demo-only plaintext, explicitly append ?plaintext=true.
// Note: Port 9003 is the Plugin Gateway (PRIVATE internal services).
// Port 9001 is the Payment Gateway (PUBLIC for consumer sidecars).
package plugin

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/streamingfast/logging"
	"golang.org/x/net/http2"
)

var zlog, _ = logging.PackageLogger("sds_plugin", "github.com/graphprotocol/substreams-data-service/provider/plugin")

// baseConfig holds common connection configuration for all plugins.
type baseConfig struct {
	Endpoint  string // host:port
	Insecure  bool   // skip TLS certificate verification
	Plaintext bool   // use plaintext (no TLS)
}

// parseBaseConfig parses common connection parameters from a URL.
func parseBaseConfig(configURL string) (*baseConfig, url.Values, error) {
	u, err := url.Parse(configURL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	if u.Scheme != "sds" {
		return nil, nil, fmt.Errorf("invalid scheme %q, expected 'sds'", u.Scheme)
	}

	hostname := u.Hostname()
	if hostname == "" {
		return nil, nil, fmt.Errorf("hostname is required, e.g. sds://localhost:9003")
	}

	port := u.Port()
	if port == "" {
		port = "443"
	}

	cfg := &baseConfig{
		Endpoint: fmt.Sprintf("%s:%s", hostname, port),
	}

	vals := u.Query()

	if vals.Get("insecure") == "true" {
		cfg.Insecure = true
	}

	if vals.Get("plaintext") == "true" {
		cfg.Plaintext = true
	}

	return cfg, vals, nil
}

// newHTTPClient creates an HTTP client configured for the given base config.
// For plaintext connections, it uses HTTP/2 cleartext (h2c).
// For TLS connections, it uses standard HTTPS.
func newHTTPClient(cfg *baseConfig) *http.Client {
	if cfg.Plaintext {
		// Use HTTP/2 cleartext (h2c) for plaintext connections
		return &http.Client{
			Transport: &http2.Transport{
				AllowHTTP: true,
				DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, network, addr)
				},
			},
		}
	}

	// Use standard HTTPS with optional certificate verification skip
	tlsConfig := &tls.Config{}
	if cfg.Insecure {
		tlsConfig.InsecureSkipVerify = true
	}

	return &http.Client{
		Transport: &http2.Transport{
			TLSClientConfig: tlsConfig,
		},
	}
}

// baseURL returns the base URL for the given config.
func (cfg *baseConfig) baseURL() string {
	if cfg.Plaintext {
		return "http://" + cfg.Endpoint
	}
	return "https://" + cfg.Endpoint
}

// parseDuration parses a duration string, returning the default if empty.
func parseDuration(s string, defaultVal time.Duration) (time.Duration, error) {
	if s == "" {
		return defaultVal, nil
	}
	return time.ParseDuration(s)
}

// parseInt64 parses an int64 string, returning the default if empty.
func parseInt64(s string, defaultVal int64) (int64, error) {
	if s == "" {
		return defaultVal, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

// parseUint64 parses a uint64 string, returning the default if empty.
func parseUint64(s string, defaultVal uint64) (uint64, error) {
	if s == "" {
		return defaultVal, nil
	}
	return strconv.ParseUint(s, 10, 64)
}
