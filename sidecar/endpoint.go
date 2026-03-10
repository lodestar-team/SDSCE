package sidecar

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/http2"
)

// ParsedEndpoint contains the parsed endpoint URL and connection settings.
type ParsedEndpoint struct {
	// URL is the endpoint URL with the insecure query parameter removed.
	URL string
	// Plaintext indicates whether the endpoint uses cleartext HTTP.
	Plaintext bool
	// Insecure indicates whether to skip TLS certificate verification.
	// Only applicable for https:// URLs.
	Insecure bool
}

// ParseEndpoint parses an endpoint string and extracts connection settings.
//
// The endpoint can include an "insecure=true" query parameter to indicate
// that TLS certificate verification should be skipped (for self-signed certs).
// This parameter is removed from the returned URL.
//
// Examples:
//   - "localhost:9001" -> "https://localhost:9001", insecure=false
//   - "http://localhost:9001" -> "http://localhost:9001", insecure=false
//   - "https://localhost:9001?insecure=true" -> "https://localhost:9001", insecure=true
//   - "https://localhost:9001?foo=bar&insecure=true" -> "https://localhost:9001?foo=bar", insecure=true
func ParseEndpoint(endpoint string) ParsedEndpoint {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ParsedEndpoint{}
	}

	// Add default scheme if missing
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		// If parsing fails, return as-is
		return ParsedEndpoint{URL: endpoint}
	}

	// Check for insecure query parameter
	query := parsed.Query()
	insecure := strings.EqualFold(query.Get("insecure"), "true")

	// Remove the insecure parameter from the URL
	query.Del("insecure")
	parsed.RawQuery = query.Encode()

	return ParsedEndpoint{
		URL:       parsed.String(),
		Plaintext: strings.EqualFold(parsed.Scheme, "http"),
		Insecure:  insecure,
	}
}

// HTTPClient returns an http.Client configured for this endpoint.
// If Insecure is true and the URL uses https, the client will skip
// TLS certificate verification.
func (p ParsedEndpoint) HTTPClient() *http.Client {
	if !p.Insecure {
		return http.DefaultClient
	}

	if p.Plaintext {
		return http.DefaultClient
	}

	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
}

// GRPCClient returns an HTTP/2 client for Connect/gRPC calls.
// Plaintext endpoints use h2c, while TLS endpoints use HTTPS with optional
// certificate verification skipping when Insecure is set.
func (p ParsedEndpoint) GRPCClient() *http.Client {
	if p.Plaintext {
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

	tlsConfig := &tls.Config{}
	if p.Insecure {
		tlsConfig.InsecureSkipVerify = true
	}

	return &http.Client{
		Transport: &http2.Transport{
			TLSClientConfig: tlsConfig,
		},
	}
}
