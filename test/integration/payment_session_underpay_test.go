package integration

import (
	"context"
	"math/big"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	sds "github.com/graphprotocol/substreams-data-service"
	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/graphprotocol/substreams-data-service/horizon/devenv"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	providergateway "github.com/graphprotocol/substreams-data-service/provider/gateway"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	providerusage "github.com/graphprotocol/substreams-data-service/provider/usage"
	"github.com/graphprotocol/substreams-data-service/sidecar"
)

func TestPaymentSession_RejectsUnderpayingRAV(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	setup, err := env.SetupTestWithSigner(nil)
	require.NoError(t, err)

	domain := env.Domain()

	// Make pricing deterministic: 1 wei per block, 0 per byte.
	pricingConfig := &sidecar.PricingConfig{
		PricePerBlock: sds.NewGRTFromUint64(1),
		PricePerByte:  sds.ZeroGRT(),
	}

	repo := repository.NewInMemoryRepository()
	providerGateway, gatewayClient, shutdown := startPaymentGatewayForTest(t, ":19014", &providergateway.Config{
		ListenAddr:          ":19014",
		ServiceProvider:     env.ServiceProvider.Address,
		Domain:              domain,
		CollectorAddr:       env.Collector.Address,
		EscrowAddr:          env.Escrow.Address,
		RPCEndpoint:         env.RPCURL,
		PricingConfig:       pricingConfig,
		RAVRequestThreshold: sds.NewGRTFromUint64(1),
		DataPlaneEndpoint:   "substreams.provider.example:443",
		TransportConfig:     sidecar.ServerTransportConfig{Plaintext: true},
		Repository:          repo,
	})
	defer shutdown()
	usageService := providerusage.NewUsageService(repo, deterministicRepositoryPricingConfig(), providerGateway)

	rav0 := &horizon.RAV{
		Payer:           env.Payer.Address,
		DataService:     env.DataService.Address,
		ServiceProvider: env.ServiceProvider.Address,
		TimestampNs:     uint64(time.Now().UnixNano()),
		ValueAggregate:  big.NewInt(0),
		Metadata:        nil,
	}
	signedRAV0, err := horizon.Sign(domain, rav0, setup.SignerKey)
	require.NoError(t, err)

	startResp, err := gatewayClient.StartSession(ctx, connect.NewRequest(&providerv1.StartSessionRequest{
		EscrowAccount: &commonv1.EscrowAccount{
			Payer:       commonv1.AddressFromEth(env.Payer.Address),
			Receiver:    commonv1.AddressFromEth(env.ServiceProvider.Address),
			DataService: commonv1.AddressFromEth(env.DataService.Address),
		},
		InitialRav: sidecar.HorizonSignedRAVToProto(signedRAV0),
	}))
	require.NoError(t, err)
	require.True(t, startResp.Msg.Accepted)
	require.NotEmpty(t, startResp.Msg.SessionId)
	require.Equal(t, "substreams.provider.example:443", startResp.Msg.GetDataPlaneEndpoint())

	stream := bindPaymentSession(t, ctx, gatewayClient, startResp.Msg.SessionId)
	reportMeteredUsage(t, ctx, usageService, env.Payer.Address, env.ServiceProvider.Address, startResp.Msg.SessionId, 1, 0, 1)

	resp, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, resp.GetRavRequest(), "expected provider to emit a rav_request")

	current := resp.GetRavRequest().GetCurrentRav().GetRav().GetValueAggregate().ToBigInt()
	require.Equal(t, 0, current.Cmp(big.NewInt(0)))

	// Underpay: keep same value even though usage delta is 1 wei.
	ravUnderpay := &horizon.RAV{
		Payer:           env.Payer.Address,
		DataService:     env.DataService.Address,
		ServiceProvider: env.ServiceProvider.Address,
		TimestampNs:     uint64(time.Now().UnixNano()),
		ValueAggregate:  current,
		Metadata:        nil,
	}
	signedUnderpay, err := horizon.Sign(domain, ravUnderpay, setup.SignerKey)
	require.NoError(t, err)

	require.NoError(t, stream.Send(&providerv1.PaymentSessionRequest{
		SessionId: startResp.Msg.SessionId,
		Message: &providerv1.PaymentSessionRequest_RavSubmission{
			RavSubmission: &providerv1.SignedRAVSubmission{
				SignedRav: sidecar.HorizonSignedRAVToProto(signedUnderpay),
				Usage:     resp.GetRavRequest().GetUsage(),
			},
		},
	}))

	resp2, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, resp2.GetSessionControl())
	require.Equal(t, providerv1.SessionControl_ACTION_STOP, resp2.GetSessionControl().GetAction())
	require.Contains(t, resp2.GetSessionControl().GetReason(), "underpays")

	statusResp, err := gatewayClient.GetSessionStatus(ctx, connect.NewRequest(&providerv1.GetSessionStatusRequest{
		SessionId: startResp.Msg.SessionId,
	}))
	require.NoError(t, err)
	require.NotNil(t, statusResp.Msg.GetPaymentStatus())
	require.Equal(t, 0, statusResp.Msg.GetPaymentStatus().GetCurrentRavValue().ToBigInt().Cmp(big.NewInt(0)))

	require.NoError(t, stream.CloseRequest())
	_ = stream.CloseResponse()
}
