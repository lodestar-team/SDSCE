// Package auth implements the gRPC AuthService that validates SignedRAVs for
// the sds:// dauth plugin used by firehose-core tier1.
package auth

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/alphadose/haxmap"
	"github.com/graphprotocol/substreams-data-service/horizon"
	authv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/auth/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/auth/v1/authv1connect"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/streamingfast/eth-go"
	"github.com/streamingfast/logging"
	"go.uber.org/zap"
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

	authCache *haxmap.Map[string, authCacheEntry]
}

type authCacheEntry struct {
	ok      bool
	expires time.Time
}

var _ authv1connect.AuthServiceHandler = (*AuthService)(nil)

// NewAuthService creates a new AuthService with the given configuration.
// collectorQuerier may be nil if on-chain delegation checks are not needed.
func NewAuthService(
	serviceProvider eth.Address,
	domain *horizon.Domain,
	collectorQuerier CollectorAuthorizer,
) *AuthService {
	return &AuthService{
		serviceProvider:  serviceProvider,
		domain:           domain,
		collectorQuerier: collectorQuerier,
		authCache:        haxmap.New[string, authCacheEntry](),
	}
}

// ValidateAuth validates a SignedRAV received in the x-sds-rav header and
// returns the payer address as organization_id for use in trusted headers.
func (s *AuthService) ValidateAuth(
	ctx context.Context,
	req *connect.Request[authv1.ValidateAuthRequest],
) (*connect.Response[authv1.ValidateAuthResponse], error) {
	zlog.Debug("ValidateAuth called",
		zap.String("ip_address", req.Msg.IpAddress),
		zap.String("path", req.Msg.Path),
	)

	if req.Msg.PaymentRav == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("missing payment_rav"))
	}

	// Convert proto SignedRAV to horizon SignedRAV for EIP-712 operations.
	signedRAV, err := sidecar.ProtoSignedRAVToHorizon(req.Msg.PaymentRav)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid payment_rav: %w", err))
	}

	result, err := s.validateRAV(ctx, signedRAV, req.Msg.IpAddress, req.Msg.Path)
	if err != nil {
		if authErr, ok := err.(*AuthError); ok {
			return nil, connect.NewError(authErr.Code, err)
		}
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}

	return connect.NewResponse(&authv1.ValidateAuthResponse{
		OrganizationId: result.OrganizationId,
		ApiKeyId:       result.ApiKeyId,
		Metadata:       result.Metadata,
	}), nil
}

// AuthResult holds the result of a successful authentication.
type AuthResult struct {
	OrganizationId string
	ApiKeyId       string
	Metadata       map[string]string
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
	// Recover the signer from the EIP-712 signature.
	signerAddr, err := signedRAV.RecoverSigner(s.domain)
	if err != nil {
		zlog.Warn("RAV signature verification failed", zap.Error(err))
		return nil, &AuthError{Code: connect.CodeUnauthenticated, Msg: fmt.Sprintf("invalid signature: %v", err)}
	}

	payer := signedRAV.Message.Payer

	// Verify that the signer is authorized to act for the payer.
	authorized, err := s.isSignerAuthorized(ctx, payer, signerAddr)
	if err != nil {
		zlog.Warn("authorization check failed",
			zap.Stringer("payer", payer),
			zap.Stringer("signer", signerAddr),
			zap.Error(err),
		)
		return nil, &AuthError{Code: connect.CodeInternal, Msg: fmt.Sprintf("authorization check failed: %v", err)}
	}
	if !authorized {
		zlog.Warn("signer not authorized for payer",
			zap.Stringer("payer", payer),
			zap.Stringer("signer", signerAddr),
		)
		return nil, &AuthError{Code: connect.CodePermissionDenied, Msg: fmt.Sprintf("signer %s is not authorized for payer %s", signerAddr.Pretty(), payer.Pretty())}
	}

	// Verify that the RAV targets this service provider.
	if !sidecar.AddressesEqual(signedRAV.Message.ServiceProvider, s.serviceProvider) {
		zlog.Warn("RAV targets a different service provider",
			zap.Stringer("expected", s.serviceProvider),
			zap.Stringer("got", signedRAV.Message.ServiceProvider),
		)
		return nil, &AuthError{Code: connect.CodePermissionDenied, Msg: fmt.Sprintf("RAV targets service provider %s, not %s", signedRAV.Message.ServiceProvider.Pretty(), s.serviceProvider.Pretty())}
	}

	zlog.Debug("validateRAV succeeded",
		zap.Stringer("payer", payer),
		zap.Stringer("signer", signerAddr),
	)

	return &AuthResult{
		OrganizationId: payer.Pretty(),
		ApiKeyId:       "",
		Metadata: map[string]string{
			"signer": signerAddr.Pretty(),
		},
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
