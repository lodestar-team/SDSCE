package sidecar

import (
	"context"
	"math/big"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/alphadose/haxmap"
	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1/providerv1connect"
	authv1connect "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/auth/v1/authv1connect"
	sessionv1connect "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/session/v1/sessionv1connect"
	usagev1connect "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/usage/v1/usagev1connect"
	providerauth "github.com/graphprotocol/substreams-data-service/provider/auth"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	providersession "github.com/graphprotocol/substreams-data-service/provider/session"
	providerusage "github.com/graphprotocol/substreams-data-service/provider/usage"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/streamingfast/dgrpc/server"
	"github.com/streamingfast/dgrpc/server/connectrpc"
	"github.com/streamingfast/eth-go"
	"github.com/streamingfast/shutter"
	"go.uber.org/zap"
)

var _ providerv1connect.ProviderSidecarServiceHandler = (*Sidecar)(nil)
var _ providerv1connect.PaymentGatewayServiceHandler = (*Sidecar)(nil)

type Sidecar struct {
	*shutter.Shutter

	listenAddr string
	logger     *zap.Logger
	server     *connectrpc.ConnectWebServer

	// Session management (legacy payment sessions)
	sessions *sidecar.SessionManager

	// Service provider identity
	serviceProvider eth.Address

	// Domain for signature verification
	domain *horizon.Domain

	// Contract addresses for on-chain queries
	collectorAddr eth.Address
	escrowAddr    eth.Address

	// Escrow balance querier
	escrowQuerier *sidecar.EscrowQuerier
	// Collector authorization querier
	collectorQuerier sidecar.CollectorAuthorizer

	// Pricing configuration
	pricingConfig *sidecar.PricingConfig

	authCache *haxmap.Map[string, authCacheEntry]

	// Plugin services (serve firehose-core sds:// plugins via Connect)
	authService    *providerauth.AuthService
	usageService   *providerusage.UsageService
	sessionService *providersession.SessionService
	repo           repository.GlobalRepository
}

type Config struct {
	ListenAddr      string
	ServiceProvider eth.Address
	Domain          *horizon.Domain
	CollectorAddr   eth.Address
	EscrowAddr      eth.Address
	RPCEndpoint     string
	PricingConfig   *sidecar.PricingConfig

	// QuotaConfig configures per-payer worker quota limits for the session service.
	// If nil, DefaultQuotaConfig() is used.
	QuotaConfig *providersession.QuotaConfig
}

type authCacheEntry struct {
	ok      bool
	expires time.Time
}

func New(config *Config, logger *zap.Logger) *Sidecar {
	var escrowQuerier *sidecar.EscrowQuerier
	if config.RPCEndpoint != "" && config.EscrowAddr != nil {
		escrowQuerier = sidecar.NewEscrowQuerier(config.RPCEndpoint, config.EscrowAddr)
	}

	var collectorQuerier *sidecar.CollectorQuerier
	if config.RPCEndpoint != "" && config.CollectorAddr != nil {
		collectorQuerier = sidecar.NewCollectorQuerier(config.RPCEndpoint, config.CollectorAddr)
	}

	pricingConfig := config.PricingConfig
	if pricingConfig == nil {
		pricingConfig = sidecar.DefaultPricingConfig()
	}

	// Build the global repository and plugin services.
	repo := repository.NewInMemoryRepository()

	// The auth service needs to call IsAuthorized on the collector; reuse
	// the collectorQuerier from the existing sidecar if available.
	var authCollectorQuerier providerauth.CollectorAuthorizer
	if collectorQuerier != nil {
		authCollectorQuerier = collectorQuerier
	}

	authSvc := providerauth.NewAuthService(
		config.ServiceProvider,
		config.Domain,
		authCollectorQuerier,
	)
	usageSvc := providerusage.NewUsageService(repo)
	sessionSvc := providersession.NewSessionService(repo, config.QuotaConfig)

	return &Sidecar{
		Shutter:          shutter.New(),
		listenAddr:       config.ListenAddr,
		logger:           logger,
		sessions:         sidecar.NewSessionManager(),
		serviceProvider:  config.ServiceProvider,
		domain:           config.Domain,
		collectorAddr:    config.CollectorAddr,
		escrowAddr:       config.EscrowAddr,
		escrowQuerier:    escrowQuerier,
		collectorQuerier: collectorQuerier,
		pricingConfig:    pricingConfig,
		authCache:        haxmap.New[string, authCacheEntry](),
		repo:             repo,
		authService:      authSvc,
		usageService:     usageSvc,
		sessionService:   sessionSvc,
	}
}

// GetEscrowBalance queries the on-chain escrow balance for a payer
func (s *Sidecar) GetEscrowBalance(ctx context.Context, payer eth.Address) (*big.Int, error) {
	if s.escrowQuerier == nil {
		return nil, nil // No RPC configured
	}
	return s.escrowQuerier.GetBalance(ctx, payer, s.collectorAddr, s.serviceProvider)
}

func (s *Sidecar) SessionCount() int {
	return s.sessions.Count()
}

func (s *Sidecar) Run() {
	// Connect/HTTP server for SDS services
	handlerGetters := []connectrpc.HandlerGetter{
		func(opts ...connect.HandlerOption) (string, http.Handler) {
			return providerv1connect.NewProviderSidecarServiceHandler(s, opts...)
		},
		func(opts ...connect.HandlerOption) (string, http.Handler) {
			return providerv1connect.NewPaymentGatewayServiceHandler(s, opts...)
		},
		// Plugin services for sds:// firehose-core plugins
		func(opts ...connect.HandlerOption) (string, http.Handler) {
			return authv1connect.NewAuthServiceHandler(s.authService, opts...)
		},
		func(opts ...connect.HandlerOption) (string, http.Handler) {
			return usagev1connect.NewUsageServiceHandler(s.usageService, opts...)
		},
		func(opts ...connect.HandlerOption) (string, http.Handler) {
			return sessionv1connect.NewSessionServiceHandler(s.sessionService, opts...)
		},
	}

	s.server = connectrpc.New(
		handlerGetters,
		server.WithPlainTextServer(),
		server.WithLogger(s.logger),
		server.WithHealthCheck(server.HealthCheckOverHTTP, s.healthCheck),
		server.WithConnectPermissiveCORS(),
		server.WithConnectReflection(providerv1connect.ProviderSidecarServiceName),
		server.WithConnectReflection(providerv1connect.PaymentGatewayServiceName),
		server.WithConnectReflection(authv1connect.AuthServiceName),
		server.WithConnectReflection(usagev1connect.UsageServiceName),
		server.WithConnectReflection(sessionv1connect.SessionServiceName),
	)

	s.server.OnTerminated(func(err error) {
		s.Shutdown(err)
	})

	s.OnTerminating(func(_ error) {
		s.server.Shutdown(nil)
	})

	s.logger.Info("starting provider sidecar", zap.String("listen_addr", s.listenAddr))
	s.server.Launch(s.listenAddr)
}

func (s *Sidecar) healthCheck(ctx context.Context) (isReady bool, out interface{}, err error) {
	return true, nil, nil
}

func (s *Sidecar) isSignerAuthorized(ctx context.Context, payer, signer eth.Address) (bool, error) {
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

	s.authCache.Set(key, authCacheEntry{ok: ok, expires: time.Now().Add(30 * time.Second)})

	return ok, nil
}
