package integration

import (
	"context"
	"math/big"
	"testing"
	"time"

	"connectrpc.com/connect"
	sds "github.com/graphprotocol/substreams-data-service"
	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/graphprotocol/substreams-data-service/horizon/devenv"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	providergateway "github.com/graphprotocol/substreams-data-service/provider/gateway"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	providerusage "github.com/graphprotocol/substreams-data-service/provider/usage"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/stretchr/testify/require"
)

func TestPaymentSession_BelowThresholdContinuesWithoutRAVRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	setup, err := env.SetupTestWithSigner(nil)
	require.NoError(t, err)

	repo := repository.NewInMemoryRepository()
	providerGateway, gatewayClient, shutdown := startPaymentGatewayForTest(t, ":19020", &providergateway.Config{
		ListenAddr:          ":19020",
		ServiceProvider:     env.ServiceProvider.Address,
		Domain:              env.Domain(),
		CollectorAddr:       env.Collector.Address,
		EscrowAddr:          env.Escrow.Address,
		RPCEndpoint:         env.RPCURL,
		PricingConfig:       deterministicPricingConfig(),
		RAVRequestThreshold: sds.NewGRTFromUint64(2),
		DataPlaneEndpoint:   "substreams.provider.example:443",
		TransportConfig:     sidecar.ServerTransportConfig{Plaintext: true},
		Repository:          repo,
	})
	defer shutdown()
	usageService := providerusage.NewUsageService(repo, deterministicRepositoryPricingConfig(), providerGateway)

	startResp := startGatewaySession(t, ctx, gatewayClient, env.Payer.Address, env.ServiceProvider.Address, env.DataService.Address, setup.SignerKey, env.Domain())
	streamCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	stream := bindPaymentSession(t, streamCtx, gatewayClient, startResp.Msg.SessionId)
	reportMeteredUsage(t, ctx, usageService, env.Payer.Address, env.ServiceProvider.Address, startResp.Msg.SessionId, 1, 0, 1)

	resp, err := stream.Receive()
	require.Error(t, err)
	require.Nil(t, resp)

	statusResp, err := gatewayClient.GetSessionStatus(ctx, connect.NewRequest(&providerv1.GetSessionStatusRequest{
		SessionId: startResp.Msg.SessionId,
	}))
	require.NoError(t, err)
	require.True(t, statusResp.Msg.GetActive())
	require.NotNil(t, statusResp.Msg.GetPaymentStatus())
	require.Equal(t, 0, statusResp.Msg.GetPaymentStatus().GetCurrentRavValue().ToBigInt().Cmp(big.NewInt(0)))
	require.Equal(t, 0, statusResp.Msg.GetPaymentStatus().GetAccumulatedUsageValue().ToBigInt().Cmp(big.NewInt(1)))
}

func TestPaymentSession_AboveThresholdRequestsRAV(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	setup, err := env.SetupTestWithSigner(nil)
	require.NoError(t, err)

	repo := repository.NewInMemoryRepository()
	providerGateway, gatewayClient, shutdown := startPaymentGatewayForTest(t, ":19021", &providergateway.Config{
		ListenAddr:          ":19021",
		ServiceProvider:     env.ServiceProvider.Address,
		Domain:              env.Domain(),
		CollectorAddr:       env.Collector.Address,
		EscrowAddr:          env.Escrow.Address,
		RPCEndpoint:         env.RPCURL,
		PricingConfig:       deterministicPricingConfig(),
		RAVRequestThreshold: sds.NewGRTFromUint64(2),
		DataPlaneEndpoint:   "substreams.provider.example:443",
		TransportConfig:     sidecar.ServerTransportConfig{Plaintext: true},
		Repository:          repo,
	})
	defer shutdown()
	usageService := providerusage.NewUsageService(repo, deterministicRepositoryPricingConfig(), providerGateway)

	startResp := startGatewaySession(t, ctx, gatewayClient, env.Payer.Address, env.ServiceProvider.Address, env.DataService.Address, setup.SignerKey, env.Domain())
	stream := bindPaymentSession(t, ctx, gatewayClient, startResp.Msg.SessionId)
	reportMeteredUsage(t, ctx, usageService, env.Payer.Address, env.ServiceProvider.Address, startResp.Msg.SessionId, 3, 0, 1)

	resp, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, resp.GetRavRequest())
	require.Equal(t, 0, resp.GetRavRequest().GetUsage().GetCost().ToBigInt().Cmp(big.NewInt(3)))

	require.NoError(t, stream.CloseRequest())
	_ = stream.CloseResponse()
}

func TestPaymentSession_AcceptedRAVResetsThresholdWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	setup, err := env.SetupTestWithSigner(nil)
	require.NoError(t, err)

	repo := repository.NewInMemoryRepository()
	providerGateway, gatewayClient, shutdown := startPaymentGatewayForTest(t, ":19022", &providergateway.Config{
		ListenAddr:          ":19022",
		ServiceProvider:     env.ServiceProvider.Address,
		Domain:              env.Domain(),
		CollectorAddr:       env.Collector.Address,
		EscrowAddr:          env.Escrow.Address,
		RPCEndpoint:         env.RPCURL,
		PricingConfig:       deterministicPricingConfig(),
		RAVRequestThreshold: sds.NewGRTFromUint64(2),
		DataPlaneEndpoint:   "substreams.provider.example:443",
		TransportConfig:     sidecar.ServerTransportConfig{Plaintext: true},
		Repository:          repo,
	})
	defer shutdown()
	usageService := providerusage.NewUsageService(repo, deterministicRepositoryPricingConfig(), providerGateway)

	startResp := startGatewaySession(t, ctx, gatewayClient, env.Payer.Address, env.ServiceProvider.Address, env.DataService.Address, setup.SignerKey, env.Domain())
	stream := bindPaymentSession(t, ctx, gatewayClient, startResp.Msg.SessionId)
	reportMeteredUsage(t, ctx, usageService, env.Payer.Address, env.ServiceProvider.Address, startResp.Msg.SessionId, 2, 0, 1)

	resp1, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, resp1.GetRavRequest())

	rav1 := &horizon.RAV{
		Payer:           env.Payer.Address,
		DataService:     env.DataService.Address,
		ServiceProvider: env.ServiceProvider.Address,
		TimestampNs:     uint64(time.Now().UnixNano()),
		ValueAggregate:  big.NewInt(2),
	}
	signedRAV1, err := horizon.Sign(env.Domain(), rav1, setup.SignerKey)
	require.NoError(t, err)

	require.NoError(t, stream.Send(&providerv1.PaymentSessionRequest{
		SessionId: startResp.Msg.SessionId,
		Message: &providerv1.PaymentSessionRequest_RavSubmission{
			RavSubmission: &providerv1.SignedRAVSubmission{
				SignedRav: sidecar.HorizonSignedRAVToProto(signedRAV1),
				Usage:     resp1.GetRavRequest().GetUsage(),
			},
		},
	}))

	resp2, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, resp2.GetSessionControl())
	require.Equal(t, providerv1.SessionControl_ACTION_CONTINUE, resp2.GetSessionControl().GetAction())

	reportMeteredUsage(t, ctx, usageService, env.Payer.Address, env.ServiceProvider.Address, startResp.Msg.SessionId, 1, 0, 1)
	reportMeteredUsage(t, ctx, usageService, env.Payer.Address, env.ServiceProvider.Address, startResp.Msg.SessionId, 1, 0, 1)

	resp3, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, resp3.GetRavRequest())
	require.Equal(t, 0, resp3.GetRavRequest().GetUsage().GetCost().ToBigInt().Cmp(big.NewInt(2)))

	require.NoError(t, stream.CloseRequest())
	_ = stream.CloseResponse()
}
