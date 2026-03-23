package plugin

import (
	"context"
	"fmt"
	"maps"
	"os"

	"connectrpc.com/connect"
	sds "github.com/graphprotocol/substreams-data-service"
	authv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/auth/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/auth/v1/authv1connect"
	"github.com/streamingfast/dauth"
	"go.uber.org/zap"
)

// RegisterAuth registers the "sds" scheme with dauth.
// The config URL format is:
//
//	sds://host:port?plaintext=true&insecure=true
//
// The plugin connects to the provider gateway's AuthService for RAV validation.
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
			case "insecure", "plaintext":
				// Known parameters
			default:
				return nil, fmt.Errorf("unknown query parameter: %s", k)
			}
		}

		return newAuthenticator(baseCfg, logger)
	})
}

// authenticator implements dauth.Authenticator by calling the provider gateway.
type authenticator struct {
	client authv1connect.AuthServiceClient
	logger *zap.Logger
}

func newAuthenticator(cfg *baseConfig, logger *zap.Logger) (dauth.Authenticator, error) {
	httpClient := newHTTPClient(cfg)

	client := authv1connect.NewAuthServiceClient(
		httpClient,
		cfg.baseURL(),
	)

	return &authenticator{
		client: client,
		logger: logger.Named("sds-auth"),
	}, nil
}

// Authenticate implements dauth.Authenticator.
//
// This is a lightweight shim that forwards all authentication context to the
// provider's AuthService via gRPC. All authentication logic lives in the
// AuthService, which allows operators to update auth logic without rebuilding
// and redeploying firehose-core.
//
// You will not see this being called in this project, if you do a references or textual search. This is because
// this is called via a plugin system within another module (firehose-core & substreams) and it's only in there
// that would see it called. And even there, there is still another indirection at least for Substreams. Indeed,
// in Substreams, that auth plugin is called by a gRPC middleware that is installed on the HTTP server when the Substreams
// Tier1 configures its server. In the middleware, it has access to the [dauth.Authenticator] instance which was passed
// when firehose-core instantiated the Substreams Tier1 app via an instantiation of the `--common-auth-plugin` flag.
//
// See:
// - https://github.com/streamingfast/firehose-core/blob/f0da3d2112fc9d22935563b8f6f96247759b85dc/cmd/apps/substreams_tier1.go#L88-L94
// - https://github.com/streamingfast/substreams/blob/c5e1527d982cc6d9da991ccdf620ef2f93ca07f0/app/tier1.go#L38-L39
// - https://github.com/streamingfast/substreams/blob/c5e1527d982cc6d9da991ccdf620ef2f93ca07f0/service/service_grpc.go#L42-L43
// - https://github.com/streamingfast/dauth/blob/02898e30442d6edbb7c304b77e8592fb12f268ca/middleware/grpc/middleware.go#L13-L34
// - https://github.com/streamingfast/dauth/blob/02898e30442d6edbb7c304b77e8592fb12f268ca/middleware/connect/authentication.go#L28
func (a *authenticator) Authenticate(ctx context.Context, path string, headers map[string][]string, ipAddress string) (context.Context, error) {
	a.logger.Debug("authenticate plugin called",
		zap.String("path", path),
		zap.Int("header_count", len(headers)),
		zap.String("ip", ipAddress),
	)

	// Convert headers to protobuf format
	protoHeaders := make(map[string]*authv1.HeaderValues, len(headers))
	for name, values := range headers {
		protoHeaders[name] = &authv1.HeaderValues{
			Values: values,
		}
	}

	// Call the provider gateway's AuthService - it will handle all validation logic
	req := connect.NewRequest(&authv1.ValidateAuthRequest{
		UntrustedHeaders: protoHeaders,
		Path:             path,
		IpAddress:        ipAddress,
	})

	resp, err := a.client.ValidateAuth(ctx, req)
	if err != nil {
		a.logger.Warn("authentication failed", zap.Error(err))
		// Pass through the error from the server (it already has proper connect codes)
		return ctx, err
	}

	// Extract trusted headers from the response
	trustedHeaders := make(dauth.TrustedHeaders, len(resp.Msg.TrustedHeaders))
	maps.Copy(trustedHeaders, resp.Msg.TrustedHeaders)

	// Extract session ID from trusted headers and set it as Meta
	// The dmetering middleware reads Meta from context (via dauth.HeaderMeta) and includes it in metering events
	sessionID := trustedHeaders.Get(sds.HeaderSessionID)
	if sessionID != "" {
		trustedHeaders[dauth.HeaderMeta] = sessionID
	}

	a.logger.Debug("authentication plugin successful",
		zap.Strings("trusted_header_names", trustedHeaders.Names()),
		zap.String("session_id", sessionID),
	)

	return dauth.WithTrustedHeaders(ctx, trustedHeaders), nil
}

// Ready implements dauth.Authenticator.
func (a *authenticator) Ready(ctx context.Context) bool {
	return true
}
