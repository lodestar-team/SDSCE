package gateway

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/alphadose/haxmap"
	sds "github.com/graphprotocol/substreams-data-service"
	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/graphprotocol/substreams-data-service/internal/operatorauth"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1/providerv1connect"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/streamingfast/dgrpc/server"
	"github.com/streamingfast/dgrpc/server/connectrpc"
	"github.com/streamingfast/eth-go"
	"github.com/streamingfast/shutter"
	"go.uber.org/zap"
)

var _ providerv1connect.PaymentGatewayServiceHandler = (*Gateway)(nil)

// Gateway is the Payment Gateway server that handles payment session management
// and RAV exchange for consumer sidecars.
//
// This is the PUBLIC-facing gateway that should be exposed to the internet for
// consumers to connect to. It handles the payment protocol between consumers
// and providers.
//
// Note: This gateway does NOT include the plugin services (auth, session, usage)
// which are handled by the separate PluginGateway for internal firehose-core use.
type Gateway struct {
	*shutter.Shutter

	listenAddr string
	logger     *zap.Logger
	server     *connectrpc.ConnectWebServer

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
	pricingConfig       *sidecar.PricingConfig
	ravRequestThreshold *big.Int
	dataPlaneEndpoint   string
	transportConfig     sidecar.ServerTransportConfig
	operatorAuthConfig  operatorauth.Config

	authCache *haxmap.Map[string, authCacheEntry]

	// Global repository for session/usage state (shared across all components)
	repo repository.GlobalRepository
	// runtime owns live PaymentSession stream bindings and provider-originated control dispatch.
	runtime *runtimeManager
}

type Config struct {
	ListenAddr          string
	ServiceProvider     eth.Address
	Domain              *horizon.Domain
	CollectorAddr       eth.Address
	EscrowAddr          eth.Address
	RPCEndpoint         string
	PricingConfig       *sidecar.PricingConfig
	RAVRequestThreshold sds.GRT
	DataPlaneEndpoint   string
	TransportConfig     sidecar.ServerTransportConfig
	OperatorAuthConfig  operatorauth.Config

	// Repository provides session/usage state storage and must be selected
	// explicitly by the caller.
	Repository repository.GlobalRepository
}

type authCacheEntry struct {
	ok      bool
	expires time.Time
}

func New(config *Config, logger *zap.Logger) (*Gateway, error) {
	if config == nil {
		return nil, fmt.Errorf("gateway config must not be nil")
	}
	if config.Repository == nil {
		return nil, fmt.Errorf("gateway repository must be provided explicitly")
	}

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
	ravRequestThreshold := config.RAVRequestThreshold
	if ravRequestThreshold.IsZero() {
		ravRequestThreshold = DefaultRAVRequestThreshold()
	}

	return &Gateway{
		Shutter:             shutter.New(),
		listenAddr:          config.ListenAddr,
		logger:              logger,
		serviceProvider:     config.ServiceProvider,
		domain:              config.Domain,
		collectorAddr:       config.CollectorAddr,
		escrowAddr:          config.EscrowAddr,
		escrowQuerier:       escrowQuerier,
		collectorQuerier:    collectorQuerier,
		pricingConfig:       pricingConfig,
		ravRequestThreshold: ravRequestThreshold.BigInt(),
		dataPlaneEndpoint:   config.DataPlaneEndpoint,
		transportConfig:     config.TransportConfig,
		operatorAuthConfig:  config.OperatorAuthConfig,
		authCache:           haxmap.New[string, authCacheEntry](),
		repo:                config.Repository,
		runtime:             newRuntimeManager(),
	}, nil
}

// toRepoPricingConfig converts sidecar.PricingConfig to repository.PricingConfig.
func toRepoPricingConfig(pc *sidecar.PricingConfig) repository.PricingConfig {
	if pc == nil {
		return repository.PricingConfig{}
	}
	return repository.PricingConfig{
		PricePerBlock: pc.PricePerBlock,
		PricePerByte:  pc.PricePerByte,
	}
}

// GetEscrowBalance queries the on-chain escrow balance for a payer
func (s *Gateway) GetEscrowBalance(ctx context.Context, payer eth.Address) (*big.Int, error) {
	if s.escrowQuerier == nil {
		return nil, nil // No RPC configured
	}
	return s.escrowQuerier.GetBalance(ctx, payer, s.collectorAddr, s.serviceProvider)
}

func (s *Gateway) SessionCount() int {
	return s.repo.SessionCount(context.Background())
}

func (s *Gateway) OnMeteredUsage(ctx context.Context, sessionID string) error {
	return s.runtime.onMeteredUsage(ctx, s, sessionID)
}

func (s *Gateway) authorizeOperator(header http.Header, required operatorauth.Role) (operatorauth.Role, error) {
	return operatorauth.AuthorizeHeader(header, s.operatorAuthConfig, required)
}

func (s *Gateway) paymentControlPending(ctx context.Context, session *repository.Session) (bool, error) {
	if session == nil || !session.IsActive() {
		return false, nil
	}

	activeWorkers, err := s.repo.WorkerCountBySession(ctx, session.ID)
	if err != nil {
		return false, err
	}

	return activeWorkers > 0 || s.runtime.hasPendingPaymentControl(session.ID) || s.shouldRequestRAV(session), nil
}

func (s *Gateway) shouldRequestRAV(session *repository.Session) bool {
	if session == nil || session.CurrentRAV == nil || s.ravRequestThreshold == nil {
		return false
	}

	_, _, _, deltaCost := session.UsageDeltaSinceBaseline()
	return deltaCost.Cmp(s.ravRequestThreshold) >= 0
}

func (s *Gateway) Run() {
	// Connect/HTTP server for Payment Gateway service
	handlerGetters := []connectrpc.HandlerGetter{
		func(opts ...connect.HandlerOption) (string, http.Handler) {
			return providerv1connect.NewPaymentGatewayServiceHandler(s, opts...)
		},
	}

	transportOpt, err := s.transportConfig.DGRPCOption("provider gateway")
	if err != nil {
		s.Shutdown(err)
		return
	}

	s.server = connectrpc.New(
		handlerGetters,
		transportOpt,
		server.WithLogger(s.logger),
		server.WithHealthCheck(server.HealthCheckOverHTTP, s.healthCheck),
		server.WithConnectPermissiveCORS(),
		server.WithConnectReflection(providerv1connect.PaymentGatewayServiceName),
	)

	s.server.OnTerminated(func(err error) {
		s.Shutdown(err)
	})

	s.OnTerminating(func(_ error) {
		s.server.Shutdown(0)
	})

	s.logger.Info("starting provider gateway", zap.String("listen_addr", s.listenAddr))
	s.server.Launch(s.listenAddr)
}

func (s *Gateway) healthCheck(ctx context.Context) (isReady bool, out interface{}, err error) {
	return true, nil, nil
}

func (s *Gateway) isSignerAuthorized(ctx context.Context, payer, signer eth.Address) (bool, error) {
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
