package integration

import (
	"context"
	"math/big"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	sds "github.com/graphprotocol/substreams-data-service"
	consumersidecar "github.com/graphprotocol/substreams-data-service/consumer/sidecar"
	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/graphprotocol/substreams-data-service/horizon/devenv"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	consumerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1/consumerv1connect"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	providergateway "github.com/graphprotocol/substreams-data-service/provider/gateway"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
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

	gatewayClient, shutdown := startPaymentGatewayForTest(t, ":19020", &providergateway.Config{
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
		Repository:          repository.NewInMemoryRepository(),
	})
	defer shutdown()

	startResp := startGatewaySession(t, ctx, gatewayClient, env.Payer.Address, env.ServiceProvider.Address, env.DataService.Address, setup.SignerKey, env.Domain())
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
	require.Nil(t, resp.GetRavRequest())
	require.NotNil(t, resp.GetSessionControl())
	require.Equal(t, providerv1.SessionControl_ACTION_CONTINUE, resp.GetSessionControl().GetAction())

	require.NoError(t, stream.CloseRequest())
	_ = stream.CloseResponse()
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

	gatewayClient, shutdown := startPaymentGatewayForTest(t, ":19021", &providergateway.Config{
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
		Repository:          repository.NewInMemoryRepository(),
	})
	defer shutdown()

	startResp := startGatewaySession(t, ctx, gatewayClient, env.Payer.Address, env.ServiceProvider.Address, env.DataService.Address, setup.SignerKey, env.Domain())
	stream := gatewayClient.PaymentSession(ctx)

	require.NoError(t, stream.Send(&providerv1.PaymentSessionRequest{
		SessionId: startResp.Msg.SessionId,
		Message: &providerv1.PaymentSessionRequest_UsageReport{
			UsageReport: &providerv1.UsageReport{
				Usage: &commonv1.Usage{
					BlocksProcessed:  3,
					BytesTransferred: 0,
					Requests:         1,
				},
			},
		},
	}))

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

	gatewayClient, shutdown := startPaymentGatewayForTest(t, ":19022", &providergateway.Config{
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
		Repository:          repository.NewInMemoryRepository(),
	})
	defer shutdown()

	startResp := startGatewaySession(t, ctx, gatewayClient, env.Payer.Address, env.ServiceProvider.Address, env.DataService.Address, setup.SignerKey, env.Domain())
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

	resp3, err := stream.Receive()
	require.NoError(t, err)
	require.Nil(t, resp3.GetRavRequest())
	require.NotNil(t, resp3.GetSessionControl())
	require.Equal(t, providerv1.SessionControl_ACTION_CONTINUE, resp3.GetSessionControl().GetAction())

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

	resp4, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, resp4.GetRavRequest())
	require.Equal(t, 0, resp4.GetRavRequest().GetUsage().GetCost().ToBigInt().Cmp(big.NewInt(2)))

	require.NoError(t, stream.CloseRequest())
	_ = stream.CloseResponse()
}

func TestConsumerSidecar_ReportUsage_BelowThresholdContinuesWithoutUpdatedRAV(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	setup, err := env.SetupTestWithSigner(nil)
	require.NoError(t, err)

	_, shutdownProvider := startPaymentGatewayForTest(t, ":19023", &providergateway.Config{
		ListenAddr:          ":19023",
		ServiceProvider:     env.ServiceProvider.Address,
		Domain:              env.Domain(),
		CollectorAddr:       env.Collector.Address,
		EscrowAddr:          env.Escrow.Address,
		RPCEndpoint:         env.RPCURL,
		PricingConfig:       deterministicPricingConfig(),
		RAVRequestThreshold: sds.NewGRTFromUint64(2),
		DataPlaneEndpoint:   "substreams.provider.example:443",
		TransportConfig:     sidecar.ServerTransportConfig{Plaintext: true},
		Repository:          repository.NewInMemoryRepository(),
	})
	defer shutdownProvider()

	consumerSidecar := consumersidecar.New(&consumersidecar.Config{
		ListenAddr:      ":19024",
		SignerKey:       setup.SignerKey,
		Domain:          env.Domain(),
		TransportConfig: sidecar.ServerTransportConfig{Plaintext: true},
	}, zlog.Named("consumer"))
	go consumerSidecar.Run()
	defer consumerSidecar.Shutdown(nil)
	time.Sleep(100 * time.Millisecond)

	consumerClient := consumerv1connect.NewConsumerSidecarServiceClient(http.DefaultClient, "http://localhost:19024")

	initResp, err := consumerClient.Init(ctx, connect.NewRequest(&consumerv1.InitRequest{
		EscrowAccount: &commonv1.EscrowAccount{
			Payer:       commonv1.AddressFromEth(env.Payer.Address),
			Receiver:    commonv1.AddressFromEth(env.ServiceProvider.Address),
			DataService: commonv1.AddressFromEth(env.DataService.Address),
		},
		ProviderControlPlaneEndpoint: "http://localhost:19023",
	}))
	require.NoError(t, err)

	usageResp, err := consumerClient.ReportUsage(ctx, connect.NewRequest(&consumerv1.ReportUsageRequest{
		SessionId: initResp.Msg.GetSession().GetSessionId(),
		Usage: &commonv1.Usage{
			BlocksProcessed:  1,
			BytesTransferred: 0,
			Requests:         1,
		},
	}))
	require.NoError(t, err)
	require.True(t, usageResp.Msg.GetShouldContinue())
	require.Empty(t, usageResp.Msg.GetStopReason())
	require.Nil(t, usageResp.Msg.GetUpdatedRav())
}
