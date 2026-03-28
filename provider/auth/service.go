// Package auth implements the gRPC AuthService that validates SignedRAVs for
// the sds:// dauth plugin used by firehose-core tier1.
package auth

import (
	"context"
	"encoding/base64"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/alphadose/haxmap"
	sds "github.com/graphprotocol/substreams-data-service"
	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/graphprotocol/substreams-data-service/internal/session"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	authv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/auth/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/auth/v1/authv1connect"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/streamingfast/dauth"
	"github.com/streamingfast/eth-go"
	"github.com/streamingfast/logging"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

var zlog, _ = logging.PackageLogger("sds_auth", "github.com/graphprotocol/substreams-data-service/provider/auth")

// CollectorAuthorizer checks whether a signer is authorized to act on behalf
// of a payer (e.g., via on-chain delegation).
type CollectorAuthorizer interface {
	IsAuthorized(ctx context.Context, payer, signer eth.Address) (bool, error)
}

// AuthService implements authv1connect.AuthServiceHandler.
// It validates EIP-712 signed RAVs, recovers the signer, and checks
// authorization to return a payer-based auth context.
type AuthService struct {
	serviceProvider  eth.Address
	domain           *horizon.Domain
	collectorQuerier CollectorAuthorizer
	repository       repository.GlobalRepository

	authCache *haxmap.Map[string, authCacheEntry]
}

const (
	// SessionKeepAliveTimeout defines how long a session can be inactive before considered stale
	SessionKeepAliveTimeout = 5 * time.Minute
)

type authCacheEntry struct {
	ok      bool
	expires time.Time
}

var _ authv1connect.AuthServiceHandler = (*AuthService)(nil)

// NewAuthService creates a new AuthService with the given configuration.
// collectorQuerier may be nil if on-chain delegation checks are not needed.
// repository is required for session validation.
func NewAuthService(
	serviceProvider eth.Address,
	domain *horizon.Domain,
	collectorQuerier CollectorAuthorizer,
	repository repository.GlobalRepository,
) *AuthService {
	return &AuthService{
		serviceProvider:  serviceProvider,
		domain:           domain,
		collectorQuerier: collectorQuerier,
		repository:       repository,
		authCache:        haxmap.New[string, authCacheEntry](),
	}
}

// ValidateAuth validates incoming authentication and returns trusted headers.
// This is the main entry point called by the dauth plugin for every request.
// The plugin forwards all headers, path, and IP - this service extracts and
// validates the RAV, session ID, and any other auth context.
func (s *AuthService) ValidateAuth(
	ctx context.Context,
	req *connect.Request[authv1.ValidateAuthRequest],
) (*connect.Response[authv1.ValidateAuthResponse], error) {
	logger := logging.LoggerFromContext(ctx, zlog)
	logger.Debug("ValidateAuth called",
		zap.String("ip_address", req.Msg.IpAddress),
		zap.String("path", req.Msg.Path),
		zap.Int("header_count", len(req.Msg.UntrustedHeaders)),
	)

	// Convert UNTRUSTED headers to lowercase map for case-insensitive lookup
	// IMPORTANT: These headers come from the client and cannot be trusted until validated
	lowerHeaders := make(map[string][]string)
	for name, headerValues := range req.Msg.UntrustedHeaders {
		if headerValues != nil && len(headerValues.Values) > 0 {
			lowerHeaders[strings.ToLower(name)] = headerValues.Values
		}
	}

	// Extract x-sds-rav header
	ravHeaders, ok := lowerHeaders[strings.ToLower(sds.HeaderRAV)]
	if !ok || len(ravHeaders) == 0 {
		logger.Warn("missing x-sds-rav header",
			zap.Strings("received_header_names", slices.Collect(maps.Keys(lowerHeaders))),
		)
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("missing %s header", sds.HeaderRAV))
	}

	// Decode the SignedRAV from base64-encoded protobuf
	signedRAV, err := decodeRAVFromHeader(ravHeaders[0])
	if err != nil {
		logger.Warn("failed to decode RAV from header", zap.Error(err))
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid %s header: %w", sds.HeaderRAV, err))
	}

	// Convert proto SignedRAV to horizon SignedRAV for EIP-712 operations
	horizonRAV, err := sidecar.ProtoSignedRAVToHorizon(signedRAV)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid RAV: %w", err))
	}

	// Validate the RAV
	result, err := s.validateRAV(ctx, horizonRAV, req.Msg.IpAddress, req.Msg.Path)
	if err != nil {
		if authErr, ok := err.(*AuthError); ok {
			return nil, connect.NewError(authErr.Code, err)
		}
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}

	// Extract session ID from x-sds-session-id header (optional)
	var validatedSessionID string
	if sessionHeaders, ok := lowerHeaders[strings.ToLower(sds.HeaderSessionID)]; ok && len(sessionHeaders) > 0 {
		validatedSessionID = sessionHeaders[0]
		logger.Debug("extracted session ID from header", zap.String("session_id", validatedSessionID))

		// Validate session if provided
		if err := s.validateSession(ctx, validatedSessionID, result.OrganizationId); err != nil {
			logger.Warn("session validation failed",
				zap.String("session_id", validatedSessionID),
				zap.String("organization_id", result.OrganizationId),
				zap.Error(err),
			)
			return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("invalid session: %w", err))
		}
		logger.Debug("session validated successfully", zap.String("session_id", validatedSessionID))
	} else {
		// No session ID provided - generate a new one for this auth attempt
		// This allows backwards compatibility with clients that don't send session IDs yet
		validatedSessionID = session.GenerateID()
		logger.Debug("no session_id provided, generated new one", zap.String("session_id", validatedSessionID))
	}

	// Build trusted headers to return to the plugin
	trustedHeaders := make(map[string]string)
	trustedHeaders[dauth.HeaderOrganizationID] = result.OrganizationId
	trustedHeaders[dauth.HeaderIP] = req.Msg.IpAddress
	trustedHeaders[sds.HeaderSessionID] = validatedSessionID

	if result.ApiKeyId != "" {
		trustedHeaders[dauth.HeaderApiKeyID] = result.ApiKeyId
	}

	logger.Debug("authentication service successful",
		zap.String("organization_id", result.OrganizationId),
		zap.String("session_id", validatedSessionID),
		zap.Strings("trusted_header_names", slices.Collect(maps.Keys(trustedHeaders))),
	)

	return connect.NewResponse(&authv1.ValidateAuthResponse{
		TrustedHeaders: trustedHeaders,
	}), nil
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

// AuthResult holds the result of a successful authentication.
type AuthResult struct {
	OrganizationId string
	ApiKeyId       string
}

// AuthError represents an authentication error with an associated connect code.
type AuthError struct {
	Code connect.Code
	Msg  string
}

func (e *AuthError) Error() string { return e.Msg }

// validateRAV is the internal implementation used by both ValidateAuth and the SF adapter.
// It validates the SignedRAV and returns the authentication result.
// Errors returned are *AuthError with the appropriate connect code set.
func (s *AuthService) validateRAV(ctx context.Context, signedRAV *horizon.SignedRAV, ipAddress, path string) (*AuthResult, error) {
	logger := logging.LoggerFromContext(ctx, zlog)

	// Recover the signer from the EIP-712 signature.
	signerAddr, err := signedRAV.RecoverSigner(s.domain)
	if err != nil {
		logger.Warn("RAV signature verification failed", zap.Error(err))
		return nil, &AuthError{Code: connect.CodeUnauthenticated, Msg: fmt.Sprintf("invalid signature: %v", err)}
	}

	payer := signedRAV.Message.Payer

	// Verify that the signer is authorized to act for the payer.
	authorized, err := s.isSignerAuthorized(ctx, payer, signerAddr)
	if err != nil {
		logger.Warn("authorization check failed",
			zap.Stringer("payer", payer),
			zap.Stringer("signer", signerAddr),
			zap.Error(err),
		)
		return nil, &AuthError{Code: connect.CodeInternal, Msg: fmt.Sprintf("authorization check failed: %v", err)}
	}
	if !authorized {
		logger.Warn("signer not authorized for payer",
			zap.Stringer("payer", payer),
			zap.Stringer("signer", signerAddr),
		)
		return nil, &AuthError{Code: connect.CodePermissionDenied, Msg: fmt.Sprintf("signer %s is not authorized for payer %s", signerAddr.Pretty(), payer.Pretty())}
	}

	// Verify that the RAV targets this service provider.
	if !sidecar.AddressesEqual(signedRAV.Message.ServiceProvider, s.serviceProvider) {
		logger.Warn("RAV targets a different service provider",
			zap.Stringer("expected", s.serviceProvider),
			zap.Stringer("got", signedRAV.Message.ServiceProvider),
		)
		return nil, &AuthError{Code: connect.CodePermissionDenied, Msg: fmt.Sprintf("RAV targets service provider %s, not %s", signedRAV.Message.ServiceProvider.Pretty(), s.serviceProvider.Pretty())}
	}

	logger.Debug("validateRAV succeeded",
		zap.Stringer("payer", payer),
		zap.Stringer("signer", signerAddr),
	)
	return &AuthResult{
		OrganizationId: payer.Pretty(),
		ApiKeyId:       "",
	}, nil
}

// isSignerAuthorized checks whether signer may act on behalf of payer.
// Results are cached for 30 seconds to reduce on-chain RPC calls.
func (s *AuthService) isSignerAuthorized(ctx context.Context, payer, signer eth.Address) (bool, error) {
	if sidecar.AddressesEqual(payer, signer) {
		return true, nil
	}

	if s.collectorQuerier == nil {
		return false, nil
	}

	key := payer.String() + "|" + signer.String()
	now := time.Now()

	if entry, ok := s.authCache.Get(key); ok && now.Before(entry.expires) {
		return entry.ok, nil
	}

	ok, err := s.collectorQuerier.IsAuthorized(ctx, payer, signer)
	if err != nil {
		return false, err
	}

	s.authCache.Set(key, authCacheEntry{ok: ok, expires: now.Add(30 * time.Second)})

	return ok, nil
}

// validateSession validates that a session exists, is active, and hasn't expired.
// It also verifies that the session belongs to the authenticated organization (payer).
func (s *AuthService) validateSession(ctx context.Context, sessionID, organizationID string) error {
	if s.repository == nil {
		return fmt.Errorf("session validation not available (repository not configured)")
	}

	sess, err := s.repository.SessionGet(ctx, sessionID)
	if err != nil {
		if err == repository.ErrNotFound {
			return fmt.Errorf("session not found")
		}
		return fmt.Errorf("failed to retrieve session: %w", err)
	}

	// Verify session is active
	if !sess.IsActive() {
		return fmt.Errorf("session is not active (status: %s)", sess.Status)
	}

	// Verify session hasn't been idle too long
	if time.Since(sess.LastKeepAlive) > SessionKeepAliveTimeout {
		return fmt.Errorf("session has been idle for too long (last activity: %v ago)", time.Since(sess.LastKeepAlive))
	}

	// Verify session belongs to the authenticated payer
	// organizationID is the payer address from the RAV
	if sess.Payer.Pretty() != organizationID {
		return fmt.Errorf("session payer mismatch (session: %s, auth: %s)", sess.Payer.Pretty(), organizationID)
	}

	// Update LastKeepAlive to keep the session active
	sess.LastKeepAlive = time.Now()
	if err := s.repository.SessionUpdate(ctx, sess); err != nil {
		logging.Warn(ctx, zlog, "failed to update session LastKeepAlive", zap.String("session_id", sessionID), zap.Error(err))
		// Don't fail the auth request if we can't update the timestamp - just log it
	}

	return nil
}
