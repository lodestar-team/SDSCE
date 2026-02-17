package sidecar

import (
	"context"
	"math/big"
	"net/http"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1/providerv1connect"
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

	// Session management
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

	authCacheMu sync.RWMutex
	authCache   map[string]authCacheEntry
}

type Config struct {
	ListenAddr      string
	ServiceProvider eth.Address
	Domain          *horizon.Domain
	CollectorAddr   eth.Address
	EscrowAddr      eth.Address
	RPCEndpoint     string
	PricingConfig   *sidecar.PricingConfig
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
		authCache:        make(map[string]authCacheEntry),
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
	handlerGetters := []connectrpc.HandlerGetter{
		func(opts ...connect.HandlerOption) (string, http.Handler) {
			return providerv1connect.NewProviderSidecarServiceHandler(s, opts...)
		},
		func(opts ...connect.HandlerOption) (string, http.Handler) {
			return providerv1connect.NewPaymentGatewayServiceHandler(s, opts...)
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

// verifyRAVSignature verifies a RAV signature and returns the signer address
func (s *Sidecar) verifyRAVSignature(signedRAV *horizon.SignedRAV) (eth.Address, error) {
	return signedRAV.RecoverSigner(s.domain)
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
	s.authCache[key] = authCacheEntry{ok: ok, expires: time.Now().Add(30 * time.Second)}
	s.authCacheMu.Unlock()

	return ok, nil
}
