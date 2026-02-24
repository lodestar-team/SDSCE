package sidecar

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseEndpoint(t *testing.T) {
	tests := []struct {
		name             string
		endpoint         string
		expectedURL      string
		expectedInsecure bool
	}{
		{
			name:             "empty",
			endpoint:         "",
			expectedURL:      "",
			expectedInsecure: false,
		},
		{
			name:             "host:port without scheme",
			endpoint:         "localhost:9001",
			expectedURL:      "http://localhost:9001",
			expectedInsecure: false,
		},
		{
			name:             "http URL",
			endpoint:         "http://localhost:9001",
			expectedURL:      "http://localhost:9001",
			expectedInsecure: false,
		},
		{
			name:             "https URL without insecure",
			endpoint:         "https://localhost:9001",
			expectedURL:      "https://localhost:9001",
			expectedInsecure: false,
		},
		{
			name:             "https URL with insecure=true",
			endpoint:         "https://localhost:9001?insecure=true",
			expectedURL:      "https://localhost:9001",
			expectedInsecure: true,
		},
		{
			name:             "https URL with insecure=True (case insensitive)",
			endpoint:         "https://localhost:9001?insecure=True",
			expectedURL:      "https://localhost:9001",
			expectedInsecure: true,
		},
		{
			name:             "https URL with insecure=false",
			endpoint:         "https://localhost:9001?insecure=false",
			expectedURL:      "https://localhost:9001",
			expectedInsecure: false,
		},
		{
			name:             "https URL with other query params and insecure",
			endpoint:         "https://localhost:9001?foo=bar&insecure=true&baz=qux",
			expectedURL:      "https://localhost:9001?baz=qux&foo=bar",
			expectedInsecure: true,
		},
		{
			name:             "http URL with insecure (still parsed but http)",
			endpoint:         "http://localhost:9001?insecure=true",
			expectedURL:      "http://localhost:9001",
			expectedInsecure: true,
		},
		{
			name:             "URL with path",
			endpoint:         "https://example.com/api/v1?insecure=true",
			expectedURL:      "https://example.com/api/v1",
			expectedInsecure: true,
		},
		{
			name:             "whitespace trimmed",
			endpoint:         "  https://localhost:9001?insecure=true  ",
			expectedURL:      "https://localhost:9001",
			expectedInsecure: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := ParseEndpoint(tt.endpoint)
			assert.Equal(t, tt.expectedURL, parsed.URL)
			assert.Equal(t, tt.expectedInsecure, parsed.Insecure)
		})
	}
}

func TestParsedEndpoint_HTTPClient(t *testing.T) {
	t.Run("returns default client for http", func(t *testing.T) {
		parsed := ParseEndpoint("http://localhost:9001")
		client := parsed.HTTPClient()
		assert.Equal(t, http.DefaultClient, client)
	})

	t.Run("returns default client for https without insecure", func(t *testing.T) {
		parsed := ParseEndpoint("https://localhost:9001")
		client := parsed.HTTPClient()
		assert.Equal(t, http.DefaultClient, client)
	})

	t.Run("returns custom client for https with insecure", func(t *testing.T) {
		parsed := ParseEndpoint("https://localhost:9001?insecure=true")
		client := parsed.HTTPClient()
		assert.NotEqual(t, http.DefaultClient, client)
		// Verify transport has InsecureSkipVerify
		transport, ok := client.Transport.(*http.Transport)
		assert.True(t, ok)
		assert.True(t, transport.TLSClientConfig.InsecureSkipVerify)
	})

	t.Run("returns default client for http with insecure flag", func(t *testing.T) {
		parsed := ParseEndpoint("http://localhost:9001?insecure=true")
		client := parsed.HTTPClient()
		// For http, we use default client even if insecure is set
		assert.Equal(t, http.DefaultClient, client)
	})
}
