package integration

import (
	"context"
	"crypto/tls"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/streamingfast/eth-go"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"google.golang.org/protobuf/types/known/timestamppb"

	sds "github.com/graphprotocol/substreams-data-service"
	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/graphprotocol/substreams-data-service/horizon/devenv"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1/providerv1connect"
	usagev1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/usage/v1"
	providergateway "github.com/graphprotocol/substreams-data-service/provider/gateway"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	providerusage "github.com/graphprotocol/substreams-data-service/provider/usage"
	"github.com/graphprotocol/substreams-data-service/sidecar"
)

func TestPaymentSession_StopsOnLowFunds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	config := DefaultTestSetupConfig()
	config.EscrowAmount = big.NewInt(1)
	payer := newFundedTestAccount(t, env)
	serviceProvider := newFundedTestAccount(t, env)
	setup, err := env.SetupCustomPaymentParticipantsWithSigner(payer, serviceProvider, config)
	require.NoError(t, err)

	repo := repository.NewInMemoryRepository()
	providerGateway, gatewayClient, shutdown := startPaymentGatewayForTest(t, ":19015", &providergateway.Config{
		ListenAddr:          ":19015",
		ServiceProvider:     serviceProvider.Address,
		Domain:              env.Domain(),
		CollectorAddr:       env.Collector.Address,
		EscrowAddr:          env.Escrow.Address,
		RPCEndpoint:         env.RPCURL,
		PricingConfig:       deterministicPricingConfig(),
		RAVRequestThreshold: sds.NewGRTFromUint64(1),
		DataPlaneEndpoint:   "substreams.provider.example:443",
		TransportConfig:     sidecar.ServerTransportConfig{Plaintext: true},
		Repository:          repo,
	})
	defer shutdown()
	usageService := providerusage.NewUsageService(repo, deterministicRepositoryPricingConfig(), providerGateway)

	startResp := startGatewaySession(t, ctx, gatewayClient, payer.Address, serviceProvider.Address, env.DataService.Address, setup.SignerKey, env.Domain())
	stream := bindPaymentSession(t, ctx, gatewayClient, startResp.Msg.SessionId)
	reportMeteredUsage(t, ctx, usageService, payer.Address, serviceProvider.Address, startResp.Msg.SessionId, 2, 0, 1)

	resp, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, resp.GetNeedMoreFunds(), "expected NeedMoreFunds response")
	require.Nil(t, resp.GetRavRequest())

	need := resp.GetNeedMoreFunds()
	require.Len(t, need.GetOutstandingRavs(), 1)
	require.Equal(t, 0, need.GetTotalOutstanding().ToBigInt().Cmp(big.NewInt(0)))
	require.Equal(t, 0, need.GetEscrowBalance().ToBigInt().Cmp(big.NewInt(1)))
	require.Equal(t, 0, need.GetMinimumNeeded().ToBigInt().Cmp(big.NewInt(1)))

	session, err := repo.SessionGet(ctx, startResp.Msg.SessionId)
	require.NoError(t, err)
	require.Equal(t, repository.SessionStatusTerminated, session.Status)
	require.Equal(t, commonv1.EndReason_END_REASON_PAYMENT_ISSUE, session.EndReason)
	require.NotNil(t, session.EndedAt)
	require.NotNil(t, session.CurrentRAV)
	require.Equal(t, 0, session.CurrentRAV.Message.ValueAggregate.Cmp(big.NewInt(0)))
	require.Equal(t, uint64(0), session.BaselineBlocks)
	require.Equal(t, 0, session.BaselineCost.Cmp(big.NewInt(0)))
	require.Equal(t, 0, session.TotalCost.Cmp(big.NewInt(2)))
	require.Equal(t, "insufficient", session.Metadata["funds_status"])
	require.Equal(t, "0", session.Metadata["funds_current_outstanding_wei"])
	require.Equal(t, "2", session.Metadata["funds_projected_outstanding_wei"])
	require.Equal(t, "1", session.Metadata["funds_escrow_balance_wei"])
	require.Equal(t, "1", session.Metadata["funds_minimum_needed_wei"])
	_, hasError := session.Metadata["funds_check_error"]
	require.False(t, hasError)

	require.NoError(t, stream.CloseRequest())
	_ = stream.CloseResponse()
}

func TestPaymentSession_ExactBalanceContinues(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	config := DefaultTestSetupConfig()
	config.EscrowAmount = big.NewInt(1)
	payer := newFundedTestAccount(t, env)
	serviceProvider := newFundedTestAccount(t, env)
	setup, err := env.SetupCustomPaymentParticipantsWithSigner(payer, serviceProvider, config)
	require.NoError(t, err)

	repo := repository.NewInMemoryRepository()
	providerGateway, gatewayClient, shutdown := startPaymentGatewayForTest(t, ":19016", &providergateway.Config{
		ListenAddr:          ":19016",
		ServiceProvider:     serviceProvider.Address,
		Domain:              env.Domain(),
		CollectorAddr:       env.Collector.Address,
		EscrowAddr:          env.Escrow.Address,
		RPCEndpoint:         env.RPCURL,
		PricingConfig:       deterministicPricingConfig(),
		RAVRequestThreshold: sds.NewGRTFromUint64(1),
		DataPlaneEndpoint:   "substreams.provider.example:443",
		TransportConfig:     sidecar.ServerTransportConfig{Plaintext: true},
		Repository:          repo,
	})
	defer shutdown()
	usageService := providerusage.NewUsageService(repo, deterministicRepositoryPricingConfig(), providerGateway)

	startResp := startGatewaySession(t, ctx, gatewayClient, payer.Address, serviceProvider.Address, env.DataService.Address, setup.SignerKey, env.Domain())
	stream := bindPaymentSession(t, ctx, gatewayClient, startResp.Msg.SessionId)
	reportMeteredUsage(t, ctx, usageService, payer.Address, serviceProvider.Address, startResp.Msg.SessionId, 1, 0, 1)

	resp, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, resp.GetRavRequest(), "expected RAV request when projected outstanding equals escrow")
	require.Nil(t, resp.GetNeedMoreFunds())

	session, err := repo.SessionGet(ctx, startResp.Msg.SessionId)
	require.NoError(t, err)
	require.True(t, session.IsActive())
	require.Equal(t, "ok", session.Metadata["funds_status"])
	require.Equal(t, "0", session.Metadata["funds_current_outstanding_wei"])
	require.Equal(t, "1", session.Metadata["funds_projected_outstanding_wei"])
	require.Equal(t, "1", session.Metadata["funds_escrow_balance_wei"])
	require.Equal(t, "0", session.Metadata["funds_minimum_needed_wei"])

	require.NoError(t, stream.CloseRequest())
	_ = stream.CloseResponse()
}

func TestPaymentSession_FailsOpenWhenEscrowBalanceUnknown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	repo := repository.NewInMemoryRepository()
	providerGateway, gatewayClient, shutdown := startPaymentGatewayForTest(t, ":19017", &providergateway.Config{
		ListenAddr:          ":19017",
		ServiceProvider:     env.ServiceProvider.Address,
		Domain:              env.Domain(),
		CollectorAddr:       env.Collector.Address,
		EscrowAddr:          env.Escrow.Address,
		PricingConfig:       deterministicPricingConfig(),
		RAVRequestThreshold: sds.NewGRTFromUint64(1),
		DataPlaneEndpoint:   "substreams.provider.example:443",
		TransportConfig:     sidecar.ServerTransportConfig{Plaintext: true},
		Repository:          repo,
	})
	defer shutdown()
	usageService := providerusage.NewUsageService(repo, deterministicRepositoryPricingConfig(), providerGateway)

	startResp := startGatewaySession(t, ctx, gatewayClient, env.Payer.Address, env.ServiceProvider.Address, env.DataService.Address, env.Payer.PrivateKey, env.Domain())
	stream := bindPaymentSession(t, ctx, gatewayClient, startResp.Msg.SessionId)
	reportMeteredUsage(t, ctx, usageService, env.Payer.Address, env.ServiceProvider.Address, startResp.Msg.SessionId, 1, 0, 1)

	resp, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, resp.GetRavRequest(), "expected fail-open behavior to continue normal flow")
	require.Nil(t, resp.GetNeedMoreFunds())

	session, err := repo.SessionGet(ctx, startResp.Msg.SessionId)
	require.NoError(t, err)
	require.True(t, session.IsActive())
	require.Equal(t, "unknown", session.Metadata["funds_status"])
	require.Equal(t, "0", session.Metadata["funds_current_outstanding_wei"])
	require.Equal(t, "1", session.Metadata["funds_projected_outstanding_wei"])
	_, hasBalance := session.Metadata["funds_escrow_balance_wei"]
	require.False(t, hasBalance)
	_, hasNeeded := session.Metadata["funds_minimum_needed_wei"]
	require.False(t, hasNeeded)
	_, hasError := session.Metadata["funds_check_error"]
	require.False(t, hasError)

	require.NoError(t, stream.CloseRequest())
	_ = stream.CloseResponse()
}

func deterministicPricingConfig() *sidecar.PricingConfig {
	return &sidecar.PricingConfig{
		PricePerBlock: sds.NewGRTFromUint64(1),
		PricePerByte:  sds.ZeroGRT(),
	}
}

func deterministicRepositoryPricingConfig() repository.PricingConfig {
	pricingConfig := deterministicPricingConfig()
	return repository.PricingConfig{
		PricePerBlock: pricingConfig.PricePerBlock,
		PricePerByte:  pricingConfig.PricePerByte,
	}
}

func startPaymentGatewayForTest(t *testing.T, endpoint string, config *providergateway.Config) (*providergateway.Gateway, providerv1connect.PaymentGatewayServiceClient, func()) {
	t.Helper()

	providerGateway := providergateway.New(config, zlog.Named("provider"))
	go providerGateway.Run()
	time.Sleep(100 * time.Millisecond)

	h2cClient := &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			},
		},
	}

	client := providerv1connect.NewPaymentGatewayServiceClient(h2cClient, "http://localhost"+endpoint, connect.WithGRPC())
	return providerGateway, client, func() {
		providerGateway.Shutdown(nil)
	}
}

func bindPaymentSession(
	t *testing.T,
	ctx context.Context,
	gatewayClient providerv1connect.PaymentGatewayServiceClient,
	sessionID string,
) *connect.BidiStreamForClient[providerv1.PaymentSessionRequest, providerv1.PaymentSessionResponse] {
	t.Helper()

	stream := gatewayClient.PaymentSession(ctx)
	require.NoError(t, stream.Send(&providerv1.PaymentSessionRequest{SessionId: sessionID}))
	return stream
}

func reportMeteredUsage(
	t *testing.T,
	ctx context.Context,
	usageService *providerusage.UsageService,
	payer eth.Address,
	provider eth.Address,
	sessionID string,
	blocks int64,
	bytes int64,
	requests int64,
) {
	t.Helper()

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
	require.NoError(t, err)
}

func startGatewaySession(
	t *testing.T,
	ctx context.Context,
	gatewayClient providerv1connect.PaymentGatewayServiceClient,
	payer, serviceProvider, dataService eth.Address,
	signerKey *eth.PrivateKey,
	domain *horizon.Domain,
) *connect.Response[providerv1.StartSessionResponse] {
	t.Helper()

	rav0 := &horizon.RAV{
		Payer:           payer,
		DataService:     dataService,
		ServiceProvider: serviceProvider,
		TimestampNs:     uint64(time.Now().UnixNano()),
		ValueAggregate:  big.NewInt(0),
	}
	signedRAV0, err := horizon.Sign(domain, rav0, signerKey)
	require.NoError(t, err)

	startResp, err := gatewayClient.StartSession(ctx, connect.NewRequest(&providerv1.StartSessionRequest{
		EscrowAccount: &commonv1.EscrowAccount{
			Payer:       commonv1.AddressFromEth(payer),
			Receiver:    commonv1.AddressFromEth(serviceProvider),
			DataService: commonv1.AddressFromEth(dataService),
		},
		InitialRav: sidecar.HorizonSignedRAVToProto(signedRAV0),
	}))
	require.NoError(t, err)
	require.True(t, startResp.Msg.Accepted)
	require.NotEmpty(t, startResp.Msg.SessionId)
	return startResp
}
