package plugin

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
	authv1connect "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/auth/v1/authv1connect"
	sessionv1connect "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/session/v1/sessionv1connect"
	usagev1connect "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/usage/v1/usagev1connect"
	providerauth "github.com/graphprotocol/substreams-data-service/provider/auth"
	providersession "github.com/graphprotocol/substreams-data-service/provider/session"
	providerusage "github.com/graphprotocol/substreams-data-service/provider/usage"
	"github.com/streamingfast/dgrpc/server"
	"github.com/streamingfast/dgrpc/server/connectrpc"
	"github.com/streamingfast/shutter"
	"go.uber.org/zap"
)

// PluginGateway serves the internal SDS plugin services (auth, session, usage) for firehose-core.
// These services are called by firehose-core's sds:// plugin URIs.
//
// SECURITY: This gateway should ONLY be accessible by your own firehose-core instance(s).
// It provides internal service APIs and should NOT be exposed publicly. Keep it on a
// private network or localhost only. Only the Payment Gateway should be public-facing.
type PluginGateway struct {
	*shutter.Shutter

	listenAddr string
	logger     *zap.Logger
	server     *connectrpc.ConnectWebServer

	// Plugin services (serve firehose-core sds:// plugins via Connect)
	authService    *providerauth.AuthService
	usageService   *providerusage.UsageService
	sessionService *providersession.SessionService
}

type PluginGatewayConfig struct {
	ListenAddr string

	// Services - all required
	AuthService    *providerauth.AuthService
	UsageService   *providerusage.UsageService
	SessionService *providersession.SessionService
}

func NewPluginGateway(config *PluginGatewayConfig, logger *zap.Logger) *PluginGateway {
	return &PluginGateway{
		Shutter:        shutter.New(),
		listenAddr:     config.ListenAddr,
		logger:         logger,
		authService:    config.AuthService,
		usageService:   config.UsageService,
		sessionService: config.SessionService,
	}
}

func (g *PluginGateway) Run() {
	// Connect/HTTP server for SDS plugin services
	handlerGetters := []connectrpc.HandlerGetter{
		func(opts ...connect.HandlerOption) (string, http.Handler) {
			return authv1connect.NewAuthServiceHandler(g.authService, opts...)
		},
		func(opts ...connect.HandlerOption) (string, http.Handler) {
			return usagev1connect.NewUsageServiceHandler(g.usageService, opts...)
		},
		func(opts ...connect.HandlerOption) (string, http.Handler) {
			return sessionv1connect.NewSessionServiceHandler(g.sessionService, opts...)
		},
	}

	g.server = connectrpc.New(
		handlerGetters,
		server.WithPlainTextServer(),
		server.WithLogger(g.logger),
		server.WithHealthCheck(server.HealthCheckOverHTTP, g.healthCheck),
		server.WithConnectPermissiveCORS(),
		server.WithConnectReflection(authv1connect.AuthServiceName),
		server.WithConnectReflection(usagev1connect.UsageServiceName),
		server.WithConnectReflection(sessionv1connect.SessionServiceName),
	)

	g.server.OnTerminated(func(err error) {
		g.Shutdown(err)
	})

	g.OnTerminating(func(_ error) {
		g.server.Shutdown(0)
	})

	g.logger.Info("starting plugin gateway", zap.String("listen_addr", g.listenAddr))
	g.server.Launch(g.listenAddr)
}

func (g *PluginGateway) healthCheck(ctx context.Context) (isReady bool, out interface{}, err error) {
	return true, nil, nil
}
