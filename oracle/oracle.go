package oracle

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	oraclev1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/oracle/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/oracle/v1/oraclev1connect"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/streamingfast/dgrpc/server"
	"github.com/streamingfast/dgrpc/server/connectrpc"
	"github.com/streamingfast/shutter"
	"go.uber.org/zap"
)

var _ oraclev1connect.OracleServiceHandler = (*Oracle)(nil)

type Oracle struct {
	*shutter.Shutter

	listenAddr string
	logger     *zap.Logger
	server     *connectrpc.ConnectWebServer

	catalog         *Catalog
	transportConfig sidecar.ServerTransportConfig
}

type Config struct {
	ListenAddr      string
	Catalog         *Catalog
	TransportConfig sidecar.ServerTransportConfig
}

func New(config *Config, logger *zap.Logger) *Oracle {
	return &Oracle{
		Shutter:         shutter.New(),
		listenAddr:      config.ListenAddr,
		logger:          logger,
		catalog:         config.Catalog,
		transportConfig: config.TransportConfig,
	}
}

func (o *Oracle) Run() {
	handlerGetters := []connectrpc.HandlerGetter{
		func(opts ...connect.HandlerOption) (string, http.Handler) {
			return oraclev1connect.NewOracleServiceHandler(o, opts...)
		},
	}

	transportOpt, err := o.transportConfig.DGRPCOption("oracle")
	if err != nil {
		o.Shutdown(err)
		return
	}

	o.server = connectrpc.New(
		handlerGetters,
		transportOpt,
		server.WithLogger(o.logger),
		server.WithHealthCheck(server.HealthCheckOverHTTP, o.healthCheck),
		server.WithConnectPermissiveCORS(),
		server.WithConnectReflection(oraclev1connect.OracleServiceName),
	)

	o.server.OnTerminated(func(err error) {
		o.Shutdown(err)
	})

	o.OnTerminating(func(_ error) {
		o.server.Shutdown(0)
	})

	o.logger.Info("starting oracle", zap.String("listen_addr", o.listenAddr))
	o.server.Launch(o.listenAddr)
}

func (o *Oracle) DiscoverProviders(
	ctx context.Context,
	req *connect.Request[oraclev1.DiscoverProvidersRequest],
) (*connect.Response[oraclev1.DiscoverProvidersResponse], error) {
	network := strings.TrimSpace(req.Msg.Network)
	if network == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("<network> is required"))
	}

	discovery, found := o.catalog.Discover(network)
	if !found {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("network not found"))
	}

	eligibleProviders := make([]*oraclev1.EligibleProvider, 0, len(discovery.EligibleProviders))
	for _, provider := range discovery.EligibleProviders {
		eligibleProviders = append(eligibleProviders, providerToProto(provider))
	}

	response := &oraclev1.DiscoverProvidersResponse{
		Network:               discovery.Network,
		CanonicalPricing:      commonv1.PricingConfigFromNative(discovery.Pricing),
		EligibleProviders:     eligibleProviders,
		RecommendedProviderId: discovery.RecommendedProvider.ID,
		SelectedProvider:      providerToProto(discovery.RecommendedProvider),
	}

	return connect.NewResponse(response), nil
}

func (o *Oracle) healthCheck(ctx context.Context) (bool, interface{}, error) {
	return true, nil, nil
}

func providerToProto(provider Provider) *oraclev1.EligibleProvider {
	return &oraclev1.EligibleProvider{
		ProviderId:           provider.ID,
		ServiceProvider:      commonv1.AddressFromEth(provider.ServiceProvider),
		ControlPlaneEndpoint: provider.ControlPlaneEndpoint,
	}
}
