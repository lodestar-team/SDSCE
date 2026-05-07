package gateway

import (
	"context"
	"fmt"
	"net/http"

	"connectrpc.com/connect"
	"github.com/graphprotocol/substreams-data-service/internal/operatorauth"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1/providerv1connect"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/streamingfast/dgrpc/server"
	"github.com/streamingfast/dgrpc/server/connectrpc"
	"github.com/streamingfast/shutter"
	"go.uber.org/zap"
)

var _ providerv1connect.ProviderOperatorServiceHandler = (*OperatorGateway)(nil)

// OperatorGateway is the private provider operator API listener.
//
// MVP-022 wires the authenticated listener and role helper. MVP-009/MVP-032 add
// the concrete operator Connect service handlers to this server.
type OperatorGateway struct {
	*shutter.Shutter

	listenAddr      string
	logger          *zap.Logger
	server          *connectrpc.ConnectWebServer
	paymentGateway  *Gateway
	transportConfig sidecar.ServerTransportConfig
}

type OperatorGatewayConfig struct {
	ListenAddr      string
	PaymentGateway  *Gateway
	TransportConfig sidecar.ServerTransportConfig
}

func NewOperatorGateway(config *OperatorGatewayConfig, logger *zap.Logger) (*OperatorGateway, error) {
	if config == nil {
		return nil, fmt.Errorf("operator gateway config must not be nil")
	}
	if config.ListenAddr == "" {
		return nil, fmt.Errorf("operator gateway listen address must be provided")
	}
	if config.PaymentGateway == nil {
		return nil, fmt.Errorf("operator gateway payment gateway must be provided")
	}

	return &OperatorGateway{
		Shutter:         shutter.New(),
		listenAddr:      config.ListenAddr,
		logger:          logger,
		paymentGateway:  config.PaymentGateway,
		transportConfig: config.TransportConfig,
	}, nil
}

func (s *OperatorGateway) Run() {
	handlerGetters := []connectrpc.HandlerGetter{
		func(opts ...connect.HandlerOption) (string, http.Handler) {
			return providerv1connect.NewProviderOperatorServiceHandler(s, opts...)
		},
	}

	transportOpt, err := s.transportConfig.DGRPCOption("provider operator gateway")
	if err != nil {
		s.Shutdown(err)
		return
	}

	s.server = connectrpc.New(
		handlerGetters,
		transportOpt,
		server.WithLogger(s.logger),
		server.WithHealthCheck(server.HealthCheckOverHTTP, s.healthCheck),
		server.WithConnectReflection(providerv1connect.ProviderOperatorServiceName),
		server.WithConnectWebHTTPHandlers([]server.HTTPHandlerGetter{
			func() (string, http.Handler) {
				return providerMetricsPath, s.metricsHandler()
			},
		}),
	)

	s.server.OnTerminated(func(err error) {
		s.Shutdown(err)
	})

	s.OnTerminating(func(_ error) {
		s.server.Shutdown(0)
	})

	s.logger.Info("starting provider operator gateway", zap.String("listen_addr", s.listenAddr))
	s.server.Launch(s.listenAddr)
}

func (s *OperatorGateway) healthCheck(ctx context.Context) (isReady bool, out interface{}, err error) {
	return true, nil, nil
}

func (s *OperatorGateway) authorize(header http.Header, requiredRole operatorauth.Role) (operatorauth.Role, error) {
	return s.paymentGateway.authorizeOperator(header, requiredRole)
}
