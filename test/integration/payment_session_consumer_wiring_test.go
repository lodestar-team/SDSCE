package integration

import (
	"context"
	"math/big"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	consumersidecar "github.com/graphprotocol/substreams-data-service/consumer/sidecar"
	"github.com/graphprotocol/substreams-data-service/horizon/devenv"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	consumerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1/consumerv1connect"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1/providerv1connect"
	providersidecar "github.com/graphprotocol/substreams-data-service/provider/sidecar"
	"github.com/graphprotocol/substreams-data-service/sidecar"
)

func TestConsumerSidecar_ReportUsage_WiresPaymentSessionLoop(t *testing.T) {
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
		PricePerBlock: sidecar.NewPriceFromWei(big.NewInt(1)),
		PricePerByte:  sidecar.NewPriceFromWei(big.NewInt(0)),
	}

	providerSidecar := providersidecar.New(&providersidecar.Config{
		ListenAddr:      ":19013",
		ServiceProvider: env.ServiceProvider.Address,
		Domain:          domain,
		CollectorAddr:   env.Collector.Address,
		EscrowAddr:      env.Escrow.Address,
		RPCEndpoint:     env.RPCURL,
		PricingConfig:   pricingConfig,
	}, zlog.Named("provider"))
	go providerSidecar.Run()
	defer providerSidecar.Shutdown(nil)
	time.Sleep(100 * time.Millisecond)

	consumerSidecar := consumersidecar.New(&consumersidecar.Config{
		ListenAddr: ":19012",
		SignerKey:  setup.SignerKey,
		Domain:     domain,
	}, zlog.Named("consumer"))
	go consumerSidecar.Run()
	defer consumerSidecar.Shutdown(nil)
	time.Sleep(100 * time.Millisecond)

	consumerClient := consumerv1connect.NewConsumerSidecarServiceClient(http.DefaultClient, "http://localhost:19012")
	providerClient := providerv1connect.NewProviderSidecarServiceClient(http.DefaultClient, "http://localhost:19013")

	initResp, err := consumerClient.Init(ctx, connect.NewRequest(&consumerv1.InitRequest{
		EscrowAccount: &commonv1.EscrowAccount{
			Payer:       commonv1.AddressFromEth(env.Payer.Address),
			Receiver:    commonv1.AddressFromEth(env.ServiceProvider.Address),
			DataService: commonv1.AddressFromEth(env.DataService.Address),
		},
		ProviderEndpoint: "http://localhost:19013",
	}))
	require.NoError(t, err)

	sessionID := initResp.Msg.GetSession().GetSessionId()
	require.NotEmpty(t, sessionID)

	usageResp, err := consumerClient.ReportUsage(ctx, connect.NewRequest(&consumerv1.ReportUsageRequest{
		SessionId: sessionID,
		Usage: &commonv1.Usage{
			BlocksProcessed:  1,
			BytesTransferred: 0,
			Requests:         1,
			Cost:             nil, // provider is cost-authoritative in PaymentSession loop
		},
	}))
	require.NoError(t, err)
	require.True(t, usageResp.Msg.GetShouldContinue())
	require.NotNil(t, usageResp.Msg.GetUpdatedRav())
	require.NotNil(t, usageResp.Msg.GetUpdatedRav().GetRav())
	require.Equal(t, big.NewInt(1).Bytes(), usageResp.Msg.GetUpdatedRav().GetRav().GetValueAggregate().ToNative().Bytes())

	statusResp, err := providerClient.GetSessionStatus(ctx, connect.NewRequest(&providerv1.GetSessionStatusRequest{
		SessionId: sessionID,
	}))
	require.NoError(t, err)
	require.NotNil(t, statusResp.Msg.GetPaymentStatus())
	require.Equal(t, big.NewInt(1).Bytes(), statusResp.Msg.GetPaymentStatus().GetCurrentRavValue().ToNative().Bytes())
}
