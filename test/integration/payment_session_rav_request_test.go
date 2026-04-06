package integration

import (
	"context"
	"math/big"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/streamingfast/eth-go"
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

func TestPaymentSession_ProviderRequestsRAVOnUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	setup, err := env.SetupTestWithSigner(nil)
	require.NoError(t, err)

	domain := env.Domain()

	// Make pricing deterministic: 1 base unit per block, 0 per byte.
	pricingConfig := &sidecar.PricingConfig{
		PricePerBlock: sds.NewGRTFromUint64(1),
		PricePerByte:  sds.ZeroGRT(),
	}

	providerConfig := &providergateway.Config{
		ListenAddr:          ":19007",
		ServiceProvider:     env.ServiceProvider.Address,
		Domain:              domain,
		CollectorAddr:       env.Collector.Address,
		EscrowAddr:          env.Escrow.Address,
		RPCEndpoint:         env.RPCURL,
		PricingConfig:       pricingConfig,
		RAVRequestThreshold: sds.NewGRTFromUint64(1),
		DataPlaneEndpoint:   "substreams.provider.example:443",
		TransportConfig:     sidecar.ServerTransportConfig{Plaintext: true},
	}
	repo := repository.NewInMemoryRepository()
	providerConfig.Repository = repo

	providerGateway, gatewayClient, shutdown := startPaymentGatewayForTest(t, ":19007", providerConfig)
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
	reportMeteredUsage(t, ctx, usageService, env.Payer.Address, env.ServiceProvider.Address, startResp.Msg.SessionId, 1, 1, 1)

	resp, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, resp.GetRavRequest(), "expected provider to emit a rav_request")
	require.NotNil(t, resp.GetRavRequest().GetCurrentRav())
	require.NotNil(t, resp.GetRavRequest().GetUsage())
	require.Equal(t, uint64(1), resp.GetRavRequest().GetUsage().GetBlocksProcessed())
	require.Equal(t, big.NewInt(1).Bytes(), resp.GetRavRequest().GetUsage().GetCost().GetBytes())

	current := resp.GetRavRequest().GetCurrentRav().GetRav().GetValueAggregate().ToNative()
	nextValue := new(big.Int).Add(current.BigInt(), big.NewInt(1))

	rav1 := &horizon.RAV{
		Payer:           env.Payer.Address,
		DataService:     env.DataService.Address,
		ServiceProvider: env.ServiceProvider.Address,
		TimestampNs:     uint64(time.Now().UnixNano()),
		ValueAggregate:  nextValue,
		Metadata:        nil,
	}
	signedRAV1, err := horizon.Sign(domain, rav1, setup.SignerKey)
	require.NoError(t, err)

	require.NoError(t, stream.Send(&providerv1.PaymentSessionRequest{
		SessionId: startResp.Msg.SessionId,
		Message: &providerv1.PaymentSessionRequest_RavSubmission{
			RavSubmission: &providerv1.SignedRAVSubmission{
				SignedRav: sidecar.HorizonSignedRAVToProto(signedRAV1),
				Usage:     resp.GetRavRequest().GetUsage(),
			},
		},
	}))

	resp2, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, resp2.GetSessionControl())
	require.Equal(t, providerv1.SessionControl_ACTION_CONTINUE, resp2.GetSessionControl().GetAction())

	require.NoError(t, stream.CloseRequest())
	_ = stream.CloseResponse()
}

func TestPaymentSession_AcceptsExactSnapshotAfterAdditionalMetering(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	setup, err := env.SetupTestWithSigner(nil)
	require.NoError(t, err)

	repo := repository.NewInMemoryRepository()
	providerGateway, gatewayClient, shutdown := startPaymentGatewayForTest(t, ":19025", &providergateway.Config{
		ListenAddr:          ":19025",
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
	require.Equal(t, 0, resp1.GetRavRequest().GetUsage().GetCost().ToBigInt().Cmp(big.NewInt(2)))

	// Additional usage arrives after the provider emitted the request but before the client answers it.
	reportMeteredUsage(t, ctx, usageService, env.Payer.Address, env.ServiceProvider.Address, startResp.Msg.SessionId, 1, 0, 1)

	signedRAV1 := signExactRequestedRAV(t, env.Domain(), setup.SignerKey, env.Payer.Address, env.DataService.Address, env.ServiceProvider.Address, resp1.GetRavRequest())
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

	session, err := repo.SessionGet(ctx, startResp.Msg.SessionId)
	require.NoError(t, err)
	require.Equal(t, uint64(3), session.BlocksProcessed)
	require.Equal(t, uint64(2), session.BaselineBlocks)
	require.Equal(t, 0, session.TotalCost.Cmp(big.NewInt(3)))
	require.Equal(t, 0, session.BaselineCost.Cmp(big.NewInt(2)))
	require.Equal(t, 0, session.CurrentRAV.Message.ValueAggregate.Cmp(big.NewInt(2)))

	reportMeteredUsage(t, ctx, usageService, env.Payer.Address, env.ServiceProvider.Address, startResp.Msg.SessionId, 1, 0, 1)

	resp3, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, resp3.GetRavRequest())
	require.Equal(t, 0, resp3.GetRavRequest().GetUsage().GetCost().ToBigInt().Cmp(big.NewInt(2)))
	require.Equal(t, uint64(2), resp3.GetRavRequest().GetUsage().GetBlocksProcessed())

	require.NoError(t, stream.CloseRequest())
	_ = stream.CloseResponse()
}

func TestPaymentSession_RejectsRAVThatOverpaysInFlightRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	setup, err := env.SetupTestWithSigner(nil)
	require.NoError(t, err)

	repo := repository.NewInMemoryRepository()
	providerGateway, gatewayClient, shutdown := startPaymentGatewayForTest(t, ":19026", &providergateway.Config{
		ListenAddr:          ":19026",
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
	usageService := providerusage.NewUsageService(repo, deterministicRepositoryPricingConfig(), providerGateway)

	startResp := startGatewaySession(t, ctx, gatewayClient, env.Payer.Address, env.ServiceProvider.Address, env.DataService.Address, setup.SignerKey, env.Domain())
	stream := bindPaymentSession(t, ctx, gatewayClient, startResp.Msg.SessionId)
	reportMeteredUsage(t, ctx, usageService, env.Payer.Address, env.ServiceProvider.Address, startResp.Msg.SessionId, 1, 0, 1)

	resp1, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, resp1.GetRavRequest())

	signedRAV1 := signRequestedRAVDelta(t, env.Domain(), setup.SignerKey, env.Payer.Address, env.DataService.Address, env.ServiceProvider.Address, resp1.GetRavRequest(), big.NewInt(2))
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
	require.Equal(t, providerv1.SessionControl_ACTION_STOP, resp2.GetSessionControl().GetAction())
	require.Contains(t, resp2.GetSessionControl().GetReason(), "overpays")

	require.NoError(t, stream.CloseRequest())
	_ = stream.CloseResponse()
}

func TestSubmitRAV_IsRejectedWhileRuntimeRAVRequestIsInFlight(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	setup, err := env.SetupTestWithSigner(nil)
	require.NoError(t, err)

	repo := repository.NewInMemoryRepository()
	providerGateway, gatewayClient, shutdown := startPaymentGatewayForTest(t, ":19027", &providergateway.Config{
		ListenAddr:          ":19027",
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
	usageService := providerusage.NewUsageService(repo, deterministicRepositoryPricingConfig(), providerGateway)

	startResp := startGatewaySession(t, ctx, gatewayClient, env.Payer.Address, env.ServiceProvider.Address, env.DataService.Address, setup.SignerKey, env.Domain())
	stream := bindPaymentSession(t, ctx, gatewayClient, startResp.Msg.SessionId)
	reportMeteredUsage(t, ctx, usageService, env.Payer.Address, env.ServiceProvider.Address, startResp.Msg.SessionId, 1, 0, 1)

	resp1, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, resp1.GetRavRequest())

	signedRAV1 := signExactRequestedRAV(t, env.Domain(), setup.SignerKey, env.Payer.Address, env.DataService.Address, env.ServiceProvider.Address, resp1.GetRavRequest())
	submitResp, err := gatewayClient.SubmitRAV(ctx, connect.NewRequest(&providerv1.SubmitRAVRequest{
		SessionId: startResp.Msg.SessionId,
		SignedRav: sidecar.HorizonSignedRAVToProto(signedRAV1),
		Usage:     resp1.GetRavRequest().GetUsage(),
	}))
	require.NoError(t, err)
	require.False(t, submitResp.Msg.GetAccepted())
	require.True(t, submitResp.Msg.GetShouldContinue())
	require.Contains(t, submitResp.Msg.GetRejectionReason(), "PaymentSession")

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

	require.NoError(t, stream.CloseRequest())
	_ = stream.CloseResponse()
}

func signExactRequestedRAV(
	t *testing.T,
	domain *horizon.Domain,
	signerKey *eth.PrivateKey,
	payer eth.Address,
	dataService eth.Address,
	serviceProvider eth.Address,
	req *providerv1.RAVRequest,
) *horizon.SignedRAV {
	t.Helper()

	return signRequestedRAVDelta(t, domain, signerKey, payer, dataService, serviceProvider, req, req.GetUsage().GetCost().ToBigInt())
}

func signRequestedRAVDelta(
	t *testing.T,
	domain *horizon.Domain,
	signerKey *eth.PrivateKey,
	payer eth.Address,
	dataService eth.Address,
	serviceProvider eth.Address,
	req *providerv1.RAVRequest,
	delta *big.Int,
) *horizon.SignedRAV {
	t.Helper()
	require.NotNil(t, req)
	require.NotNil(t, req.GetCurrentRav())
	require.NotNil(t, req.GetCurrentRav().GetRav())
	require.NotNil(t, req.GetUsage())
	require.NotNil(t, delta)

	current := req.GetCurrentRav().GetRav().GetValueAggregate().ToBigInt()
	nextValue := new(big.Int).Add(current, delta)

	rav := &horizon.RAV{
		Payer:           payer,
		DataService:     dataService,
		ServiceProvider: serviceProvider,
		TimestampNs:     uint64(time.Now().UnixNano()),
		ValueAggregate:  nextValue,
	}
	signed, err := horizon.Sign(domain, rav, signerKey)
	require.NoError(t, err)
	return signed
}
