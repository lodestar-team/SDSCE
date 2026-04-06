package integration

import (
	"context"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	sds "github.com/graphprotocol/substreams-data-service"
	consumersidecar "github.com/graphprotocol/substreams-data-service/consumer/sidecar"
	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/graphprotocol/substreams-data-service/horizon/devenv"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	usagev1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/usage/v1"
	providergateway "github.com/graphprotocol/substreams-data-service/provider/gateway"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	providerusage "github.com/graphprotocol/substreams-data-service/provider/usage"
	sidecarlib "github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/streamingfast/eth-go"
	ssclient "github.com/streamingfast/substreams/client"
	pbsubstreamsrpcv2 "github.com/streamingfast/substreams/pb/sf/substreams/rpc/v2"
	pbsubstreamsrpcv3 "github.com/streamingfast/substreams/pb/sf/substreams/rpc/v3"
	pbsubstreams "github.com/streamingfast/substreams/pb/sf/substreams/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestConsumerIngress_UsesOracleSelectedProviderReceiver(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	setup, err := env.SetupTestWithSigner(nil)
	require.NoError(t, err)

	providerAddr := reserveLocalAddress(t)
	oracleAddr := reserveLocalAddress(t)
	sidecarAddr := reserveLocalAddress(t)

	upstreamEndpoint, upstreamServer, shutdownUpstream := startFakeSubstreamsV3Server(t, func(_ *pbsubstreamsrpcv3.Request, stream grpc.ServerStreamingServer[pbsubstreamsrpcv2.Response]) error {
		return stream.Send(testBlockResponse([]byte("oracle-path-block")))
	})
	defer shutdownUpstream()

	repo := repository.NewInMemoryRepository()
	providerGateway := providergateway.New(&providergateway.Config{
		ListenAddr:          providerAddr,
		ServiceProvider:     env.ServiceProvider.Address,
		Domain:              env.Domain(),
		CollectorAddr:       env.Collector.Address,
		EscrowAddr:          env.Escrow.Address,
		RPCEndpoint:         env.RPCURL,
		DataPlaneEndpoint:   upstreamEndpoint,
		PricingConfig:       deterministicPricingConfig(),
		RAVRequestThreshold: deterministicPricingConfig().PricePerBlock,
		TransportConfig:     sidecarlib.ServerTransportConfig{Plaintext: true},
		Repository:          repo,
	}, zlog.Named("provider"))
	go providerGateway.Run()
	defer providerGateway.Shutdown(nil)
	time.Sleep(100 * time.Millisecond)

	oracleShutdown := startOracleForTest(t, oracleAddr, `
providers:
  - id: provider-a
    service_provider: "`+env.ServiceProvider.Address.Pretty()+`"
    control_plane_endpoint: "`+httpEndpoint(providerAddr)+`"
networks:
  mainnet:
    pricing:
      price_per_block: "1 GRT"
      price_per_byte: "0 GRT"
    provider_ids:
      - provider-a
`)
	defer oracleShutdown()

	consumerSidecar := consumersidecar.New(&consumersidecar.Config{
		ListenAddr:     sidecarAddr,
		SignerKey:      setup.SignerKey,
		Domain:         horizon.NewDomain(env.ChainID, env.Collector.Address),
		OracleEndpoint: httpEndpoint(oracleAddr),
		IngressConfig: &consumersidecar.IngressConfig{
			Payer:       env.Payer.Address,
			DataService: env.DataService.Address,
		},
		TransportConfig: sidecarlib.ServerTransportConfig{Plaintext: true},
	}, zlog.Named("consumer"))
	go consumerSidecar.Run()
	defer consumerSidecar.Shutdown(nil)

	require.NoError(t, waitForSidecarHealth(ctx, httpEndpoint(sidecarAddr)+"/healthz", 10*time.Second))

	conn, closeConn := dialSubstreamsGRPC(t, httpEndpoint(sidecarAddr))
	defer closeConn()

	stream, err := pbsubstreamsrpcv3.NewStreamClient(conn).Blocks(ctx, &pbsubstreamsrpcv3.Request{
		StartBlockNum: 0,
		StopBlockNum:  1,
		OutputModule:  "map_clocks",
		Package:       &pbsubstreams.Package{Network: "eth-mainnet"},
	})
	require.NoError(t, err)

	resp, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, []byte("oracle-path-block"), resp.GetBlockScopedData().GetOutput().GetMapOutput().GetValue())

	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)

	sessions, err := repo.SessionList(ctx, repository.SessionFilter{})
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, env.ServiceProvider.Address.Pretty(), sessions[0].Receiver.Pretty(), "expected receiver to be derived from oracle-selected provider")

	upstreamMetadata := upstreamServer.LastMetadata()
	require.NotEmpty(t, upstreamMetadata.Get("x-sds-rav"), "expected proxied Substreams request to carry the payment RAV header")
	require.NotEmpty(t, upstreamMetadata.Get("x-sds-session-id"), "expected proxied Substreams request to carry the session id header")
}

func TestConsumerIngress_StopsStreamOnLowFunds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	config := DefaultTestSetupConfig()
	config.EscrowAmount = big.NewInt(1)
	setup, err := env.SetupCustomPaymentParticipantsWithSigner(env.User1, env.User2, config)
	require.NoError(t, err)

	providerAddr := reserveLocalAddress(t)
	sidecarAddr := reserveLocalAddress(t)
	var usageService *providerusage.UsageService

	upstreamEndpoint, _, shutdownUpstream := startFakeSubstreamsV3Server(t, func(_ *pbsubstreamsrpcv3.Request, stream grpc.ServerStreamingServer[pbsubstreamsrpcv2.Response]) error {
		md, _ := metadata.FromIncomingContext(stream.Context())
		sessionIDs := md.Get(sds.HeaderSessionID)
		require.Len(t, sessionIDs, 1, "expected the ingress to propagate a single session id header")

		require.NoError(t, stream.Send(testBlockResponse([]byte("block-1"))))
		reportMeteredUsage(t, stream.Context(), usageService, env.User1.Address, env.User2.Address, sessionIDs[0], 1, 0, 1)

		require.NoError(t, stream.Send(testBlockResponse([]byte("block-2"))))
		reportMeteredUsage(t, stream.Context(), usageService, env.User1.Address, env.User2.Address, sessionIDs[0], 1, 0, 1)

		<-stream.Context().Done()
		return stream.Context().Err()
	})
	defer shutdownUpstream()

	repo := repository.NewInMemoryRepository()
	providerGateway := providergateway.New(&providergateway.Config{
		ListenAddr:          providerAddr,
		ServiceProvider:     env.User2.Address,
		Domain:              env.Domain(),
		CollectorAddr:       env.Collector.Address,
		EscrowAddr:          env.Escrow.Address,
		RPCEndpoint:         env.RPCURL,
		DataPlaneEndpoint:   upstreamEndpoint,
		PricingConfig:       deterministicPricingConfig(),
		RAVRequestThreshold: deterministicPricingConfig().PricePerBlock,
		TransportConfig:     sidecarlib.ServerTransportConfig{Plaintext: true},
		Repository:          repo,
	}, zlog.Named("provider"))
	usageService = providerusage.NewUsageService(repo, deterministicRepositoryPricingConfig(), providerGateway)
	go providerGateway.Run()
	defer providerGateway.Shutdown(nil)
	time.Sleep(100 * time.Millisecond)

	receiver := env.User2.Address
	consumerSidecar := consumersidecar.New(&consumersidecar.Config{
		ListenAddr: sidecarAddr,
		SignerKey:  setup.SignerKey,
		Domain:     env.Domain(),
		IngressConfig: &consumersidecar.IngressConfig{
			Payer:                        env.User1.Address,
			Receiver:                     &receiver,
			DataService:                  env.DataService.Address,
			ProviderControlPlaneEndpoint: httpEndpoint(providerAddr),
		},
		TransportConfig: sidecarlib.ServerTransportConfig{Plaintext: true},
	}, zlog.Named("consumer"))
	go consumerSidecar.Run()
	defer consumerSidecar.Shutdown(nil)

	require.NoError(t, waitForSidecarHealth(ctx, httpEndpoint(sidecarAddr)+"/healthz", 10*time.Second))

	conn, closeConn := dialSubstreamsGRPC(t, httpEndpoint(sidecarAddr))
	defer closeConn()

	stream, err := pbsubstreamsrpcv3.NewStreamClient(conn).Blocks(ctx, &pbsubstreamsrpcv3.Request{
		StartBlockNum: 0,
		StopBlockNum:  100,
		OutputModule:  "map_clocks",
		Package:       &pbsubstreams.Package{Network: "mainnet"},
	})
	require.NoError(t, err)

	_, err = stream.Recv()
	require.NoError(t, err)
	_, err = stream.Recv()
	require.NoError(t, err)

	_, err = stream.Recv()
	require.Error(t, err)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	require.Contains(t, err.Error(), "need more funds")

	require.Eventually(t, func() bool {
		sessions, err := repo.SessionList(ctx, repository.SessionFilter{})
		if err != nil || len(sessions) != 1 {
			return false
		}

		session := sessions[0]
		return session.Status == repository.SessionStatusTerminated &&
			session.EndReason == commonv1.EndReason_END_REASON_PAYMENT_ISSUE
	}, 5*time.Second, 100*time.Millisecond, "expected provider session to terminate on low funds")
}

func TestConsumerIngress_FiniteEOFReturnsPromptlyWithoutControlStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	setup, err := env.SetupTestWithSigner(nil)
	require.NoError(t, err)

	providerAddr := reserveLocalAddress(t)
	sidecarAddr := reserveLocalAddress(t)

	upstreamEndpoint, _, shutdownUpstream := startFakeSubstreamsV3Server(t, func(_ *pbsubstreamsrpcv3.Request, stream grpc.ServerStreamingServer[pbsubstreamsrpcv2.Response]) error {
		require.NoError(t, stream.Send(testBlockResponseAt(0, []byte("finite-block"))))
		return nil
	})
	defer shutdownUpstream()

	repo := repository.NewInMemoryRepository()
	providerGateway := providergateway.New(&providergateway.Config{
		ListenAddr:          providerAddr,
		ServiceProvider:     env.ServiceProvider.Address,
		Domain:              env.Domain(),
		CollectorAddr:       env.Collector.Address,
		EscrowAddr:          env.Escrow.Address,
		RPCEndpoint:         env.RPCURL,
		DataPlaneEndpoint:   upstreamEndpoint,
		PricingConfig:       deterministicPricingConfig(),
		RAVRequestThreshold: deterministicPricingConfig().PricePerBlock,
		TransportConfig:     sidecarlib.ServerTransportConfig{Plaintext: true},
		Repository:          repo,
	}, zlog.Named("provider"))
	go providerGateway.Run()
	defer providerGateway.Shutdown(nil)
	time.Sleep(100 * time.Millisecond)

	receiver := env.ServiceProvider.Address
	consumerSidecar := consumersidecar.New(&consumersidecar.Config{
		ListenAddr:                     sidecarAddr,
		SignerKey:                      setup.SignerKey,
		Domain:                         env.Domain(),
		PaymentSessionRoundtripTimeout: 2 * time.Second,
		IngressConfig: &consumersidecar.IngressConfig{
			Payer:                        env.Payer.Address,
			Receiver:                     &receiver,
			DataService:                  env.DataService.Address,
			ProviderControlPlaneEndpoint: httpEndpoint(providerAddr),
		},
		TransportConfig: sidecarlib.ServerTransportConfig{Plaintext: true},
	}, zlog.Named("consumer"))
	go consumerSidecar.Run()
	defer consumerSidecar.Shutdown(nil)

	require.NoError(t, waitForSidecarHealth(ctx, httpEndpoint(sidecarAddr)+"/healthz", 10*time.Second))

	conn, closeConn := dialSubstreamsGRPC(t, httpEndpoint(sidecarAddr))
	defer closeConn()

	stream, err := pbsubstreamsrpcv3.NewStreamClient(conn).Blocks(ctx, &pbsubstreamsrpcv3.Request{
		StartBlockNum: 0,
		StopBlockNum:  1,
		OutputModule:  "map_clocks",
		Package:       &pbsubstreams.Package{Network: "mainnet"},
	})
	require.NoError(t, err)

	resp, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, []byte("finite-block"), resp.GetBlockScopedData().GetOutput().GetMapOutput().GetValue())

	start := time.Now()
	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)
	require.Less(t, time.Since(start), time.Second, "finite EOF should not wait for the ingress control-resolution timeout")
}

func TestConsumerIngress_ResolvesAmbiguousEOFWithDelayedProviderStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	config := DefaultTestSetupConfig()
	config.EscrowAmount = big.NewInt(1)
	setup, err := env.SetupCustomPaymentParticipantsWithSigner(env.User3, env.ServiceProvider, config)
	require.NoError(t, err)

	providerAddr := reserveLocalAddress(t)
	sidecarAddr := reserveLocalAddress(t)
	var usageService *providerusage.UsageService

	sessionIDCh := make(chan string, 1)
	upstreamEndpoint, _, shutdownUpstream := startFakeSubstreamsV3Server(t, func(_ *pbsubstreamsrpcv3.Request, stream grpc.ServerStreamingServer[pbsubstreamsrpcv2.Response]) error {
		md, _ := metadata.FromIncomingContext(stream.Context())
		sessionIDs := md.Get(sds.HeaderSessionID)
		require.Len(t, sessionIDs, 1, "expected the ingress to propagate a single session id header")

		select {
		case sessionIDCh <- sessionIDs[0]:
		default:
		}

		require.NoError(t, stream.Send(testBlockResponseAt(0, []byte("block-1"))))
		require.NoError(t, stream.Send(testBlockResponseAt(1, []byte("block-2"))))
		return nil
	})
	defer shutdownUpstream()

	repo := repository.NewInMemoryRepository()
	providerGateway := providergateway.New(&providergateway.Config{
		ListenAddr:          providerAddr,
		ServiceProvider:     env.ServiceProvider.Address,
		Domain:              env.Domain(),
		CollectorAddr:       env.Collector.Address,
		EscrowAddr:          env.Escrow.Address,
		RPCEndpoint:         env.RPCURL,
		DataPlaneEndpoint:   upstreamEndpoint,
		PricingConfig:       deterministicPricingConfig(),
		RAVRequestThreshold: deterministicPricingConfig().PricePerBlock,
		TransportConfig:     sidecarlib.ServerTransportConfig{Plaintext: true},
		Repository:          repo,
	}, zlog.Named("provider"))
	usageService = providerusage.NewUsageService(repo, deterministicRepositoryPricingConfig(), providerGateway)
	go providerGateway.Run()
	defer providerGateway.Shutdown(nil)
	time.Sleep(100 * time.Millisecond)

	receiver := env.ServiceProvider.Address
	consumerSidecar := consumersidecar.New(&consumersidecar.Config{
		ListenAddr:                     sidecarAddr,
		SignerKey:                      setup.SignerKey,
		Domain:                         env.Domain(),
		PaymentSessionRoundtripTimeout: 750 * time.Millisecond,
		IngressConfig: &consumersidecar.IngressConfig{
			Payer:                        env.User3.Address,
			Receiver:                     &receiver,
			DataService:                  env.DataService.Address,
			ProviderControlPlaneEndpoint: httpEndpoint(providerAddr),
		},
		TransportConfig: sidecarlib.ServerTransportConfig{Plaintext: true},
	}, zlog.Named("consumer"))
	go consumerSidecar.Run()
	defer consumerSidecar.Shutdown(nil)

	require.NoError(t, waitForSidecarHealth(ctx, httpEndpoint(sidecarAddr)+"/healthz", 10*time.Second))

	conn, closeConn := dialSubstreamsGRPC(t, httpEndpoint(sidecarAddr))
	defer closeConn()

	stream, err := pbsubstreamsrpcv3.NewStreamClient(conn).Blocks(ctx, &pbsubstreamsrpcv3.Request{
		StartBlockNum: 0,
		StopBlockNum:  100,
		OutputModule:  "map_clocks",
		Package:       &pbsubstreams.Package{Network: "mainnet"},
	})
	require.NoError(t, err)

	_, err = stream.Recv()
	require.NoError(t, err)
	_, err = stream.Recv()
	require.NoError(t, err)

	sessionID := <-sessionIDCh
	usageErrCh := reportMeteredUsageAsync(ctx, usageService, env.User3.Address, env.ServiceProvider.Address, sessionID, 2, 0, 2, 75*time.Millisecond)

	_, err = stream.Recv()
	require.Error(t, err)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	require.Contains(t, err.Error(), "need more funds")
	require.NoError(t, <-usageErrCh)
}

type fakeSubstreamsV3Server struct {
	pbsubstreamsrpcv3.UnimplementedStreamServer

	handler func(*pbsubstreamsrpcv3.Request, grpc.ServerStreamingServer[pbsubstreamsrpcv2.Response]) error

	mu           sync.Mutex
	lastMetadata metadata.MD
	lastRequest  *pbsubstreamsrpcv3.Request
}

func (s *fakeSubstreamsV3Server) Blocks(req *pbsubstreamsrpcv3.Request, stream grpc.ServerStreamingServer[pbsubstreamsrpcv2.Response]) error {
	md, _ := metadata.FromIncomingContext(stream.Context())

	s.mu.Lock()
	s.lastMetadata = md.Copy()
	s.lastRequest = proto.Clone(req).(*pbsubstreamsrpcv3.Request)
	s.mu.Unlock()

	return s.handler(req, stream)
}

func (s *fakeSubstreamsV3Server) LastMetadata() metadata.MD {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastMetadata == nil {
		return nil
	}

	return s.lastMetadata.Copy()
}

func startFakeSubstreamsV3Server(
	t *testing.T,
	handler func(*pbsubstreamsrpcv3.Request, grpc.ServerStreamingServer[pbsubstreamsrpcv2.Response]) error,
) (string, *fakeSubstreamsV3Server, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := grpc.NewServer()
	fake := &fakeSubstreamsV3Server{handler: handler}
	pbsubstreamsrpcv3.RegisterStreamServer(server, fake)

	go func() {
		_ = server.Serve(lis)
	}()

	return "http://" + lis.Addr().String(), fake, func() {
		server.Stop()
		_ = lis.Close()
	}
}

func dialSubstreamsGRPC(t *testing.T, endpoint string) (*grpc.ClientConn, func()) {
	t.Helper()

	config := ssclient.NewSubstreamsClientConfig(ssclient.SubstreamsClientConfigOptions{
		Endpoint:  endpoint,
		PlainText: true,
	})
	conn, closeConn, _, _, err := ssclient.NewSubstreamsClientConn(config)
	require.NoError(t, err)
	return conn, func() {
		_ = closeConn()
	}
}

func testBlockResponse(payload []byte) *pbsubstreamsrpcv2.Response {
	return testBlockResponseAt(0, payload)
}

func testBlockResponseAt(blockNum uint64, payload []byte) *pbsubstreamsrpcv2.Response {
	return &pbsubstreamsrpcv2.Response{
		Message: &pbsubstreamsrpcv2.Response_BlockScopedData{
			BlockScopedData: &pbsubstreamsrpcv2.BlockScopedData{
				Output: &pbsubstreamsrpcv2.MapModuleOutput{
					Name:      "map_clocks",
					MapOutput: &anypb.Any{Value: payload},
				},
				Clock:  &pbsubstreams.Clock{Number: blockNum},
				Cursor: "cursor:1",
			},
		},
	}
}

func reportMeteredUsageAsync(
	ctx context.Context,
	usageService *providerusage.UsageService,
	payer eth.Address,
	provider eth.Address,
	sessionID string,
	blocks int64,
	bytes int64,
	requests int64,
	delay time.Duration,
) <-chan error {
	errCh := make(chan error, 1)

	go func() {
		if delay > 0 {
			time.Sleep(delay)
		}

		_, err := usageService.Report(ctx, connect.NewRequest(&usagev1.ReportRequest{
			Events: []*usagev1.Event{
				{
					OrganizationId: payer.Pretty(),
					SdsSessionId:   sessionID,
					Provider:       provider.Pretty(),
					Endpoint:       "sf.substreams.rpc.v3/Blocks",
					Network:        "mainnet",
					Metrics: []*usagev1.Metric{
						{Name: "blocks_count", Value: blocks},
						{Name: "bytes_count", Value: bytes},
						{Name: "requests_count", Value: requests},
					},
					Timestamp: timestamppb.Now(),
				},
			},
		}))
		errCh <- err
	}()

	return errCh
}

func reserveLocalAddress(t *testing.T) string {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer lis.Close()

	return lis.Addr().String()
}

func httpEndpoint(addr string) string {
	return "http://" + addr
}
