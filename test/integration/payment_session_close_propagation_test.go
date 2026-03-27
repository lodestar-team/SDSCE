package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	sds "github.com/graphprotocol/substreams-data-service"
	consumersidecar "github.com/graphprotocol/substreams-data-service/consumer/sidecar"
	"github.com/graphprotocol/substreams-data-service/horizon/devenv"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	consumerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1/consumerv1connect"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1/providerv1connect"
	providergateway "github.com/graphprotocol/substreams-data-service/provider/gateway"
	"github.com/graphprotocol/substreams-data-service/sidecar"
)

func TestSessionClose_ConsumerEndSession_MakesProviderInactive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	setup, err := env.SetupTestWithSigner(nil)
	require.NoError(t, err)

	domain := env.Domain()

	providerGateway := providergateway.New(&providergateway.Config{
		ListenAddr:        ":19016",
		ServiceProvider:   env.ServiceProvider.Address,
		Domain:            domain,
		CollectorAddr:     env.Collector.Address,
		EscrowAddr:        env.Escrow.Address,
		RPCEndpoint:       env.RPCURL,
		DataPlaneEndpoint: "substreams.provider.example:443",
		PricingConfig: &sidecar.PricingConfig{
			PricePerBlock: sds.NewGRTFromUint64(1),
			PricePerByte:  sds.ZeroGRT(),
		},
		RAVRequestThreshold: sds.NewGRTFromUint64(1),
		TransportConfig:     sidecar.ServerTransportConfig{Plaintext: true},
	}, zlog.Named("provider"))
	go providerGateway.Run()
	defer providerGateway.Shutdown(nil)
	time.Sleep(100 * time.Millisecond)

	consumerSidecar := consumersidecar.New(&consumersidecar.Config{
		ListenAddr:      ":19015",
		SignerKey:       setup.SignerKey,
		Domain:          domain,
		TransportConfig: sidecar.ServerTransportConfig{Plaintext: true},
	}, zlog.Named("consumer"))
	go consumerSidecar.Run()
	defer consumerSidecar.Shutdown(nil)
	time.Sleep(100 * time.Millisecond)

	consumerClient := consumerv1connect.NewConsumerSidecarServiceClient(http.DefaultClient, "http://localhost:19015")
	providerClient := providerv1connect.NewPaymentGatewayServiceClient(http.DefaultClient, "http://localhost:19016")

	initResp, err := consumerClient.Init(ctx, connect.NewRequest(&consumerv1.InitRequest{
		EscrowAccount: &commonv1.EscrowAccount{
			Payer:       commonv1.AddressFromEth(env.Payer.Address),
			Receiver:    commonv1.AddressFromEth(env.ServiceProvider.Address),
			DataService: commonv1.AddressFromEth(env.DataService.Address),
		},
		ProviderControlPlaneEndpoint: "http://localhost:19016",
	}))
	require.NoError(t, err)

	sessionID := initResp.Msg.GetSession().GetSessionId()
	require.NotEmpty(t, sessionID)

	_, err = consumerClient.ReportUsage(ctx, connect.NewRequest(&consumerv1.ReportUsageRequest{
		SessionId: sessionID,
		Usage: &commonv1.Usage{
			BlocksProcessed:  1,
			BytesTransferred: 0,
			Requests:         1,
			Cost:             nil,
		},
	}))
	require.NoError(t, err)

	_, err = consumerClient.EndSession(ctx, connect.NewRequest(&consumerv1.EndSessionRequest{
		SessionId:  sessionID,
		FinalUsage: nil,
	}))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		statusResp, err := providerClient.GetSessionStatus(ctx, connect.NewRequest(&providerv1.GetSessionStatusRequest{
			SessionId: sessionID,
		}))
		if err != nil {
			return false
		}
		return !statusResp.Msg.GetActive()
	}, 2*time.Second, 50*time.Millisecond)
}
