package sidecar

import (
	"context"
	"errors"
	"math/big"
	"net"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1/consumerv1connect"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/streamingfast/dgrpc/server"
	"github.com/streamingfast/dgrpc/server/connectrpc"
	"github.com/streamingfast/eth-go"
	"github.com/streamingfast/shutter"
	pbsubstreamsrpcv2 "github.com/streamingfast/substreams/pb/sf/substreams/rpc/v2"
	pbsubstreamsrpcv3 "github.com/streamingfast/substreams/pb/sf/substreams/rpc/v3"
	pbsubstreamsrpcv4 "github.com/streamingfast/substreams/pb/sf/substreams/rpc/v4"
	"go.uber.org/zap"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
)

var _ consumerv1connect.ConsumerSidecarServiceHandler = (*Sidecar)(nil)

type Sidecar struct {
	*shutter.Shutter

	listenAddr string
	logger     *zap.Logger
	server     *connectrpc.ConnectWebServer

	// Session management
	sessions *sidecar.SessionManager

	paymentSessions *paymentSessionManager

	// Signing configuration
	signerKey *eth.PrivateKey
	domain    *horizon.Domain

	oracleEndpoint string

	ingressConfig *IngressConfig

	paymentSessionRoundtripTimeout time.Duration
	transportConfig                sidecar.ServerTransportConfig
}

type Config struct {
	ListenAddr                     string
	SignerKey                      *eth.PrivateKey
	Domain                         *horizon.Domain
	OracleEndpoint                 string
	IngressConfig                  *IngressConfig
	PaymentSessionRoundtripTimeout time.Duration
	TransportConfig                sidecar.ServerTransportConfig
}

func New(config *Config, logger *zap.Logger) *Sidecar {
	paymentSessionRoundtripTimeout := config.PaymentSessionRoundtripTimeout
	if paymentSessionRoundtripTimeout <= 0 {
		paymentSessionRoundtripTimeout = 30 * time.Second
	}

	return &Sidecar{
		Shutter:                        shutter.New(),
		listenAddr:                     config.ListenAddr,
		logger:                         logger,
		sessions:                       sidecar.NewSessionManager(),
		paymentSessions:                newPaymentSessionManager(),
		signerKey:                      config.SignerKey,
		domain:                         config.Domain,
		oracleEndpoint:                 config.OracleEndpoint,
		ingressConfig:                  config.IngressConfig,
		paymentSessionRoundtripTimeout: paymentSessionRoundtripTimeout,
		transportConfig:                config.TransportConfig,
	}
}

func (s *Sidecar) Run() {
	handlerGetters := []connectrpc.HandlerGetter{
		func(opts ...connect.HandlerOption) (string, http.Handler) {
			return consumerv1connect.NewConsumerSidecarServiceHandler(s, opts...)
		},
	}

	transportOpt, err := s.transportConfig.DGRPCOption("consumer sidecar")
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
		server.WithConnectReflection(consumerv1connect.ConsumerSidecarServiceName),
	)

	grpcServer := grpc.NewServer()
	s.registerGRPCServices(grpcServer)

	routedHandler := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if isIngressGRPCPath(req.URL.Path) {
			grpcServer.ServeHTTP(rw, req)
			return
		}

		s.server.ServeHTTP(rw, req)
	})

	errorLogger, err := zap.NewStdLogAt(s.logger, zap.ErrorLevel)
	if err != nil {
		s.Shutdown(err)
		return
	}

	httpServer := &http.Server{
		Handler: h2c.NewHandler(routedHandler, &http2.Server{
			MaxConcurrentStreams: 1000,
		}),
		ErrorLog: errorLogger,
	}

	s.OnTerminating(func(_ error) {
		grpcServer.Stop()
		_ = httpServer.Close()
		s.server.Shutdown(0)
	})

	listener, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		s.Shutdown(err)
		return
	}

	s.logger.Info("starting consumer sidecar", zap.String("listen_addr", s.listenAddr))
	if s.transportConfig.Plaintext {
		s.logger.Info("serving plaintext", zap.String("listen_addr", s.listenAddr))
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.Shutdown(err)
		}
		return
	}

	tlsConfig, err := server.SecuredByX509KeyPair(s.transportConfig.TLSCertFile, s.transportConfig.TLSKeyFile)
	if err != nil {
		s.Shutdown(err)
		return
	}

	httpServer.TLSConfig = tlsConfig.AsTLSConfig()
	s.logger.Info("serving over TLS", zap.String("listen_addr", s.listenAddr))
	if err := httpServer.ServeTLS(listener, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.Shutdown(err)
	}
}

func (s *Sidecar) healthCheck(ctx context.Context) (isReady bool, out interface{}, err error) {
	return true, nil, nil
}

func (s *Sidecar) registerGRPCServices(gs *grpc.Server) {
	pbsubstreamsrpcv2.RegisterStreamServer(gs, &ingressV2Server{sidecar: s})
	pbsubstreamsrpcv2.RegisterEndpointInfoServer(gs, &ingressEndpointInfoServer{sidecar: s})
	pbsubstreamsrpcv3.RegisterStreamServer(gs, &ingressV3Server{sidecar: s})
	pbsubstreamsrpcv4.RegisterStreamServer(gs, &ingressV4Server{sidecar: s})
}

func isIngressGRPCPath(path string) bool {
	if !strings.HasPrefix(path, "/sf.substreams.rpc.") {
		return false
	}

	switch path {
	case "/sf.substreams.rpc.v2.Stream/Blocks",
		"/sf.substreams.rpc.v2.EndpointInfo/Info",
		"/sf.substreams.rpc.v3.Stream/Blocks",
		"/sf.substreams.rpc.v4.Stream/Blocks":
		return true
	default:
		return false
	}
}

// signRAV creates a signed RAV for the given parameters
func (s *Sidecar) signRAV(
	collectionID horizon.CollectionID,
	payer, dataService, serviceProvider eth.Address,
	timestampNs uint64,
	valueAggregate *big.Int,
	metadata []byte,
) (*horizon.SignedRAV, error) {
	rav := &horizon.RAV{
		CollectionID:    collectionID,
		Payer:           payer,
		DataService:     dataService,
		ServiceProvider: serviceProvider,
		TimestampNs:     timestampNs,
		ValueAggregate:  valueAggregate,
		Metadata:        metadata,
	}

	return horizon.Sign(s.domain, rav, s.signerKey)
}
