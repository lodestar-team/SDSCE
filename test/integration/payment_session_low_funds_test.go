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

	sds "github.com/graphprotocol/substreams-data-service"
	consumersidecar "github.com/graphprotocol/substreams-data-service/consumer/sidecar"
	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/graphprotocol/substreams-data-service/horizon/devenv"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	consumerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1/consumerv1connect"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1/providerv1connect"
	providergateway "github.com/graphprotocol/substreams-data-service/provider/gateway"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
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
	setup, err := env.SetupCustomPaymentParticipantsWithSigner(env.User1, env.User2, config)
	require.NoError(t, err)

	repo := repository.NewInMemoryRepository()
	gatewayClient, shutdown := startPaymentGatewayForTest(t, ":19015", &providergateway.Config{
		ListenAddr:          ":19015",
		ServiceProvider:     env.User2.Address,
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

	startResp := startGatewaySession(t, ctx, gatewayClient, env.User1.Address, env.User2.Address, env.DataService.Address, setup.SignerKey, env.Domain())
	stream := gatewayClient.PaymentSession(ctx)

	require.NoError(t, stream.Send(&providerv1.PaymentSessionRequest{
		SessionId: startResp.Msg.SessionId,
		Message: &providerv1.PaymentSessionRequest_UsageReport{
			UsageReport: &providerv1.UsageReport{
				Usage: &commonv1.Usage{
					BlocksProcessed:  2,
					BytesTransferred: 0,
					Requests:         1,
				},
			},
		},
	}))

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
	setup, err := env.SetupCustomPaymentParticipantsWithSigner(env.User3, env.ServiceProvider, config)
	require.NoError(t, err)

	repo := repository.NewInMemoryRepository()
	gatewayClient, shutdown := startPaymentGatewayForTest(t, ":19016", &providergateway.Config{
		ListenAddr:          ":19016",
		ServiceProvider:     env.ServiceProvider.Address,
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

	startResp := startGatewaySession(t, ctx, gatewayClient, env.User3.Address, env.ServiceProvider.Address, env.DataService.Address, setup.SignerKey, env.Domain())
	stream := gatewayClient.PaymentSession(ctx)

	require.NoError(t, stream.Send(&providerv1.PaymentSessionRequest{
		SessionId: startResp.Msg.SessionId,
		Message: &providerv1.PaymentSessionRequest_UsageReport{
			UsageReport: &providerv1.UsageReport{
				Usage: &commonv1.Usage{
					BlocksProcessed:  1,
					BytesTransferred: 0,
					Requests:         1,
				},
			},
		},
	}))

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
	gatewayClient, shutdown := startPaymentGatewayForTest(t, ":19017", &providergateway.Config{
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

	startResp := startGatewaySession(t, ctx, gatewayClient, env.Payer.Address, env.ServiceProvider.Address, env.DataService.Address, env.Payer.PrivateKey, env.Domain())
	stream := gatewayClient.PaymentSession(ctx)

	require.NoError(t, stream.Send(&providerv1.PaymentSessionRequest{
		SessionId: startResp.Msg.SessionId,
		Message: &providerv1.PaymentSessionRequest_UsageReport{
			UsageReport: &providerv1.UsageReport{
				Usage: &commonv1.Usage{
					BlocksProcessed:  1,
					BytesTransferred: 0,
					Requests:         1,
				},
			},
		},
	}))

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

func TestConsumerSidecar_ReportUsage_StopsOnLowFunds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	config := DefaultTestSetupConfig()
	config.EscrowAmount = big.NewInt(1)
	setup, err := env.SetupCustomPaymentParticipantsWithSigner(env.User1, env.User3, config)
	require.NoError(t, err)

	repo := repository.NewInMemoryRepository()
	_, shutdownProvider := startPaymentGatewayForTest(t, ":19018", &providergateway.Config{
		ListenAddr:          ":19018",
		ServiceProvider:     env.User3.Address,
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
	defer shutdownProvider()

	consumerSidecar := consumersidecar.New(&consumersidecar.Config{
		ListenAddr:      ":19019",
		SignerKey:       setup.SignerKey,
		Domain:          env.Domain(),
		TransportConfig: sidecar.ServerTransportConfig{Plaintext: true},
	}, zlog.Named("consumer"))
	go consumerSidecar.Run()
	defer consumerSidecar.Shutdown(nil)
	time.Sleep(100 * time.Millisecond)

	consumerClient := consumerv1connect.NewConsumerSidecarServiceClient(http.DefaultClient, "http://localhost:19019")

	initResp, err := consumerClient.Init(ctx, connect.NewRequest(&consumerv1.InitRequest{
		EscrowAccount: &commonv1.EscrowAccount{
			Payer:       commonv1.AddressFromEth(env.User1.Address),
			Receiver:    commonv1.AddressFromEth(env.User3.Address),
			DataService: commonv1.AddressFromEth(env.DataService.Address),
		},
		ProviderControlPlaneEndpoint: "http://localhost:19018",
	}))
	require.NoError(t, err)
	require.Equal(t, "substreams.provider.example:443", initResp.Msg.GetDataPlaneEndpoint())

	usageResp, err := consumerClient.ReportUsage(ctx, connect.NewRequest(&consumerv1.ReportUsageRequest{
		SessionId: initResp.Msg.GetSession().GetSessionId(),
		Usage: &commonv1.Usage{
			BlocksProcessed:  2,
			BytesTransferred: 0,
			Requests:         1,
		},
	}))
	require.NoError(t, err)
	require.False(t, usageResp.Msg.GetShouldContinue())
	require.Equal(t, "need more funds", usageResp.Msg.GetStopReason())
	require.Nil(t, usageResp.Msg.GetUpdatedRav())

	session, err := repo.SessionGet(ctx, initResp.Msg.GetSession().GetSessionId())
	require.NoError(t, err)
	require.Equal(t, repository.SessionStatusTerminated, session.Status)
	require.Equal(t, commonv1.EndReason_END_REASON_PAYMENT_ISSUE, session.EndReason)
}

func deterministicPricingConfig() *sidecar.PricingConfig {
	return &sidecar.PricingConfig{
		PricePerBlock: sds.NewGRTFromUint64(1),
		PricePerByte:  sds.ZeroGRT(),
	}
}

func startPaymentGatewayForTest(t *testing.T, endpoint string, config *providergateway.Config) (providerv1connect.PaymentGatewayServiceClient, func()) {
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
	return client, func() {
		providerGateway.Shutdown(nil)
	}
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
