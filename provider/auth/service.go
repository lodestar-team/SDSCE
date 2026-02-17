// Package auth implements the gRPC AuthService that validates SignedRAVs for
// the sds:// dauth plugin used by firehose-core tier1.
package auth

import (
	"context"
	"fmt"
	"sync"
	"time"

	"connectrpc.com/connect"
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

	authCacheMu sync.RWMutex
	authCache   map[string]authCacheEntry
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
		authCache:        make(map[string]authCacheEntry),
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

	// Recover the signer from the EIP-712 signature.
	signerAddr, err := signedRAV.RecoverSigner(s.domain)
	if err != nil {
		zlog.Warn("RAV signature verification failed", zap.Error(err))
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("invalid signature: %w", err))
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
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("authorization check failed: %w", err))
	}
	if !authorized {
		zlog.Warn("signer not authorized for payer",
			zap.Stringer("payer", payer),
			zap.Stringer("signer", signerAddr),
		)
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("signer %s is not authorized for payer %s", signerAddr.Pretty(), payer.Pretty()))
	}

	// Verify that the RAV targets this service provider.
	if !sidecar.AddressesEqual(signedRAV.Message.ServiceProvider, s.serviceProvider) {
		zlog.Warn("RAV targets a different service provider",
			zap.Stringer("expected", s.serviceProvider),
			zap.Stringer("got", signedRAV.Message.ServiceProvider),
		)
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("RAV targets service provider %s, not %s", signedRAV.Message.ServiceProvider.Pretty(), s.serviceProvider.Pretty()))
	}

	zlog.Debug("ValidateAuth succeeded",
		zap.Stringer("payer", payer),
		zap.Stringer("signer", signerAddr),
	)

	return connect.NewResponse(&authv1.ValidateAuthResponse{
		OrganizationId: payer.Pretty(),
		ApiKeyId:       "",
		Metadata: map[string]string{
			"signer": signerAddr.Pretty(),
		},
	}), nil
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

	s.authCacheMu.RLock()
	if entry, ok := s.authCache[key]; ok && now.Before(entry.expires) {
		s.authCacheMu.RUnlock()
		return entry.ok, nil
	}
	s.authCacheMu.RUnlock()

	ok, err := s.collectorQuerier.IsAuthorized(ctx, payer, signer)
	if err != nil {
		return false, err
	}

	s.authCacheMu.Lock()
	s.authCache[key] = authCacheEntry{ok: ok, expires: now.Add(30 * time.Second)}
	s.authCacheMu.Unlock()

	return ok, nil
}
