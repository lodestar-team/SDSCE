package plugin

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"connectrpc.com/connect"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	authv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/auth/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/auth/v1/authv1connect"
	"github.com/streamingfast/dauth"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

// RegisterAuth registers the "sds" scheme with dauth.
// The config URL format is:
//
//	sds://host:port?plaintext=true&insecure=true&dev-api-key=<key>
//
// The plugin connects to the provider sidecar's AuthService for RAV validation.
// All business logic (service provider, escrow, etc.) is on the server side.
func RegisterAuth() {
	dauth.Register("sds", func(config string, logger *zap.Logger) (dauth.Authenticator, error) {
		configExpanded := os.ExpandEnv(config)

		baseCfg, vals, err := parseBaseConfig(configExpanded)
		if err != nil {
			return nil, fmt.Errorf("failed to parse auth config %q: %w", config, err)
		}

		// Validate known parameters
		for k := range vals {
			switch k {
			case "insecure", "plaintext", "dev-api-key":
				// Known parameters
			default:
				return nil, fmt.Errorf("unknown query parameter: %s", k)
			}
		}

		devAPIKey := vals.Get("dev-api-key")

		return newAuthenticator(baseCfg, devAPIKey, logger)
	})
}

// authenticator implements dauth.Authenticator by calling the provider sidecar.
type authenticator struct {
	client    authv1connect.AuthServiceClient
	devAPIKey string
	logger    *zap.Logger
}

func newAuthenticator(cfg *baseConfig, devAPIKey string, logger *zap.Logger) (dauth.Authenticator, error) {
	httpClient := newHTTPClient(cfg)

	client := authv1connect.NewAuthServiceClient(
		httpClient,
		cfg.baseURL(),
	)

	return &authenticator{
		client:    client,
		devAPIKey: devAPIKey,
		logger:    logger.Named("sds-auth"),
	}, nil
}

// Authenticate implements dauth.Authenticator.
func (a *authenticator) Authenticate(ctx context.Context, path string, headers map[string][]string, ipAddress string) (context.Context, error) {
	a.logger.Debug("Authenticate called",
		zap.String("path", path),
		zap.Int("header_count", len(headers)),
		zap.String("ip", ipAddress),
	)

	// Convert headers to lowercase for case-insensitive lookup
	lowerHeaders := make(map[string][]string)
	for k, v := range headers {
		lowerHeaders[strings.ToLower(k)] = v
	}

	// Check for dev mode API key first (client-side bypass for local testing)
	if a.devAPIKey != "" {
		if apiKeys := lowerHeaders["x-api-key"]; len(apiKeys) > 0 && apiKeys[0] == a.devAPIKey {
			a.logger.Debug("dev mode auth bypass", zap.String("api_key", apiKeys[0]))
			return dauth.WithTrustedHeaders(ctx, dauth.TrustedHeaders{
				dauth.HeaderOrganizationID: "dev-test-org",
				dauth.HeaderApiKeyID:       "dev-api-key",
				dauth.HeaderIP:             ipAddress,
			}), nil
		}
	}

	// Look for the x-sds-rav header containing the SignedRAV
	ravHeaders, ok := lowerHeaders["x-sds-rav"]
	if !ok || len(ravHeaders) == 0 {
		a.logger.Warn("missing x-sds-rav header")
		return ctx, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("missing x-sds-rav header (or valid x-api-key)"))
	}

	// Decode the SignedRAV from base64-encoded protobuf
	signedRAV, err := decodeRAVFromHeader(ravHeaders[0])
	if err != nil {
		a.logger.Warn("failed to decode RAV from header", zap.Error(err))
		return ctx, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid x-sds-rav header: %w", err))
	}

	// Call the provider sidecar's AuthService to validate the RAV
	req := connect.NewRequest(&authv1.ValidateAuthRequest{
		PaymentRav: signedRAV,
		IpAddress:  ipAddress,
		Path:       path,
	})

	resp, err := a.client.ValidateAuth(ctx, req)
	if err != nil {
		a.logger.Warn("RAV validation failed", zap.Error(err))
		// Pass through the error from the server (it already has proper connect codes)
		return ctx, err
	}

	// Build trusted headers from the response
	trustedHeaders := dauth.TrustedHeaders{
		dauth.HeaderOrganizationID: resp.Msg.OrganizationId,
		dauth.HeaderIP:             ipAddress,
	}

	if resp.Msg.ApiKeyId != "" {
		trustedHeaders[dauth.HeaderApiKeyID] = resp.Msg.ApiKeyId
	}

	for k, v := range resp.Msg.Metadata {
		trustedHeaders[k] = v
	}

	a.logger.Debug("authentication successful",
		zap.String("organization_id", resp.Msg.OrganizationId),
		zap.Int("header_count", len(trustedHeaders)),
	)

	return dauth.WithTrustedHeaders(ctx, trustedHeaders), nil
}

// Ready implements dauth.Authenticator.
func (a *authenticator) Ready(ctx context.Context) bool {
	return true
}

// decodeRAVFromHeader decodes a SignedRAV from its base64-encoded protobuf format.
func decodeRAVFromHeader(headerValue string) (*commonv1.SignedRAV, error) {
	// Try base64 decoding first (standard format)
	data, err := base64.StdEncoding.DecodeString(headerValue)
	if err != nil {
		// Try raw base64 URL encoding
		data, err = base64.RawURLEncoding.DecodeString(headerValue)
		if err != nil {
			return nil, err
		}
	}

	// Parse as protobuf SignedRAV
	var protoRAV commonv1.SignedRAV
	if err := proto.Unmarshal(data, &protoRAV); err != nil {
		return nil, err
	}

	return &protoRAV, nil
}
