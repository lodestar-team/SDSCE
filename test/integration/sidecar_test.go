package integration

import (
	"context"
	"math/big"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sds "github.com/graphprotocol/substreams-data-service"
	consumersidecar "github.com/graphprotocol/substreams-data-service/consumer/sidecar"
	"github.com/graphprotocol/substreams-data-service/horizon/devenv"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	consumerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1/consumerv1connect"
	providergateway "github.com/graphprotocol/substreams-data-service/provider/gateway"
	"github.com/graphprotocol/substreams-data-service/sidecar"
)

// TestPaymentFlowBasic tests a basic payment flow:
// 1. Consumer sidecar Init -> creates session with initial RAV
// 2. Provider sidecar validates the RAV
// 3. Usage reporting and RAV updates
// 4. Session ends with final RAV
func TestPaymentFlowBasic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Get the shared development environment
	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	// Setup test with authorized signer
	setup, err := env.SetupTestWithSigner(nil)
	require.NoError(t, err, "failed to setup test")

	// Create domain for signature verification
	domain := env.Domain()

	// Create consumer sidecar
	consumerConfig := &consumersidecar.Config{
		ListenAddr:      ":19002",
		SignerKey:       setup.SignerKey,
		Domain:          domain,
		TransportConfig: sidecar.ServerTransportConfig{Plaintext: true},
	}
	consumerSidecar := consumersidecar.New(consumerConfig, zlog.Named("consumer"))
	go consumerSidecar.Run()
	defer consumerSidecar.Shutdown(nil)
	time.Sleep(100 * time.Millisecond) // Wait for server to start

	// Create provider gateway
	providerConfig := &providergateway.Config{
		ListenAddr:        ":19001",
		ServiceProvider:   env.ServiceProvider.Address,
		Domain:            domain,
		CollectorAddr:     env.Collector.Address,
		EscrowAddr:        env.Escrow.Address,
		RPCEndpoint:       env.RPCURL,
		DataPlaneEndpoint: "substreams.provider.example:443",
		TransportConfig:   sidecar.ServerTransportConfig{Plaintext: true},
	}
	providerGateway := providergateway.New(providerConfig, zlog.Named("provider"))
	go providerGateway.Run()
	defer providerGateway.Shutdown(nil)
	time.Sleep(100 * time.Millisecond) // Wait for server to start

	// Create client
	consumerClient := consumerv1connect.NewConsumerSidecarServiceClient(
		http.DefaultClient,
		"http://localhost:19002",
	)

	// Step 1: Consumer Init - creates session with initial RAV
	t.Log("Step 1: Consumer Init")
	initReq := &consumerv1.InitRequest{
		EscrowAccount: &commonv1.EscrowAccount{
			Payer:       commonv1.AddressFromEth(env.Payer.Address),
			Receiver:    commonv1.AddressFromEth(env.ServiceProvider.Address),
			DataService: commonv1.AddressFromEth(env.DataService.Address),
		},
		ProviderControlPlaneEndpoint: "http://localhost:19001",
	}
	initResp, err := consumerClient.Init(ctx, connect.NewRequest(initReq))
	require.NoError(t, err, "consumer Init failed")
	require.NotNil(t, initResp.Msg.PaymentRav, "expected payment RAV")
	require.NotEmpty(t, initResp.Msg.Session.SessionId, "expected session ID")
	require.Equal(t, "substreams.provider.example:443", initResp.Msg.GetDataPlaneEndpoint())

	consumerSessionID := initResp.Msg.Session.SessionId
	t.Logf("Consumer session created: %s", consumerSessionID)

	// Consumer Init should have started a provider gateway session
	require.Equal(t, 1, providerGateway.SessionCount(), "expected provider gateway session to be created via StartSession during Init")

	// Step 2: Report usage on consumer side
	t.Log("Step 2: Report usage on consumer side")
	reportReq := &consumerv1.ReportUsageRequest{
		SessionId: consumerSessionID,
		Usage: &commonv1.Usage{
			BlocksProcessed:  100,
			BytesTransferred: 50000,
			Requests:         1,
			Cost:             commonv1.GRTFromBigInt(big.NewInt(100000000)), // 0.1 GRT
		},
	}
	reportResp, err := consumerClient.ReportUsage(ctx, connect.NewRequest(reportReq))
	require.NoError(t, err, "consumer ReportUsage failed")
	assert.True(t, reportResp.Msg.ShouldContinue, "session should continue")
	assert.NotNil(t, reportResp.Msg.UpdatedRav, "expected updated RAV")
	t.Log("Consumer reported usage, got updated RAV")

	// Step 3: End session on consumer side
	t.Log("Step 3: End session")
	endReq := &consumerv1.EndSessionRequest{
		SessionId: consumerSessionID,
		FinalUsage: &commonv1.Usage{
			BlocksProcessed:  50,
			BytesTransferred: 25000,
			Requests:         1,
			Cost:             commonv1.GRTFromBigInt(big.NewInt(50000000)), // 0.05 GRT
		},
	}
	endResp, err := consumerClient.EndSession(ctx, connect.NewRequest(endReq))
	require.NoError(t, err, "consumer EndSession failed")
	assert.NotNil(t, endResp.Msg.FinalRav, "expected final RAV")
	assert.Equal(t, uint64(150), endResp.Msg.TotalUsage.BlocksProcessed, "expected 150 total blocks")

	// Convert final RAV value
	finalValue := endResp.Msg.FinalRav.Rav.ValueAggregate.ToBigInt()
	t.Logf("Session ended. Final RAV value: %s", finalValue.String())

	t.Log("Payment flow test completed successfully!")
}

func TestInit_CreatesFreshSessionWithoutResumeSemantics(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	setup, err := env.SetupTestWithSigner(nil)
	require.NoError(t, err, "failed to setup test")

	domain := env.Domain()

	consumerSidecar := consumersidecar.New(&consumersidecar.Config{
		ListenAddr:      ":19008",
		SignerKey:       setup.SignerKey,
		Domain:          domain,
		TransportConfig: sidecar.ServerTransportConfig{Plaintext: true},
	}, zlog.Named("consumer"))
	go consumerSidecar.Run()
	defer consumerSidecar.Shutdown(nil)
	time.Sleep(100 * time.Millisecond) // Wait for server to start

	providerGateway := providergateway.New(&providergateway.Config{
		ListenAddr:        ":19009",
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
		TransportConfig: sidecar.ServerTransportConfig{Plaintext: true},
	}, zlog.Named("provider"))
	go providerGateway.Run()
	defer providerGateway.Shutdown(nil)
	time.Sleep(100 * time.Millisecond) // Wait for server to start

	consumerClient := consumerv1connect.NewConsumerSidecarServiceClient(http.DefaultClient, "http://localhost:19008")

	escrowAccount := &commonv1.EscrowAccount{
		Payer:       commonv1.AddressFromEth(env.Payer.Address),
		Receiver:    commonv1.AddressFromEth(env.ServiceProvider.Address),
		DataService: commonv1.AddressFromEth(env.DataService.Address),
	}

	initResp, err := consumerClient.Init(ctx, connect.NewRequest(&consumerv1.InitRequest{
		EscrowAccount:                escrowAccount,
		ProviderControlPlaneEndpoint: "http://localhost:19009",
	}))
	require.NoError(t, err, "consumer Init failed")
	require.NotNil(t, initResp.Msg.PaymentRav)
	require.NotEmpty(t, initResp.Msg.Session.GetSessionId())
	require.Equal(t, "substreams.provider.example:443", initResp.Msg.GetDataPlaneEndpoint())

	reportResp, err := consumerClient.ReportUsage(ctx, connect.NewRequest(&consumerv1.ReportUsageRequest{
		SessionId: initResp.Msg.Session.GetSessionId(),
		Usage: &commonv1.Usage{
			BlocksProcessed:  1,
			BytesTransferred: 0,
			Requests:         1,
			Cost:             nil, // provider is cost-authoritative in PaymentSession loop
		},
	}))
	require.NoError(t, err, "consumer ReportUsage failed")
	require.NotNil(t, reportResp.Msg.GetUpdatedRav())
	require.NotNil(t, reportResp.Msg.GetUpdatedRav().GetRav())

	firstValue := reportResp.Msg.GetUpdatedRav().GetRav().GetValueAggregate().ToBigInt()
	require.Equal(t, 0, firstValue.Cmp(big.NewInt(1)))

	// A later Init creates a fresh payment session instead of resuming prior payment lineage.
	initResp2, err := consumerClient.Init(ctx, connect.NewRequest(&consumerv1.InitRequest{
		EscrowAccount:                escrowAccount,
		ProviderControlPlaneEndpoint: "http://localhost:19009",
	}))
	require.NoError(t, err, "consumer Init failed")
	require.NotNil(t, initResp2.Msg.GetPaymentRav())
	require.NotNil(t, initResp2.Msg.GetPaymentRav().GetRav())
	require.NotEmpty(t, initResp2.Msg.GetSession().GetSessionId())
	require.NotEqual(t, initResp.Msg.GetSession().GetSessionId(), initResp2.Msg.GetSession().GetSessionId())
	require.Equal(t, "substreams.provider.example:443", initResp2.Msg.GetDataPlaneEndpoint())

	freshValue := initResp2.Msg.GetPaymentRav().GetRav().GetValueAggregate().ToBigInt()
	require.Equal(t, 0, freshValue.Cmp(big.NewInt(0)))
}

func TestInit_RequiresProviderControlPlaneEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	setup, err := env.SetupTestWithSigner(nil)
	require.NoError(t, err)

	consumerSidecar := consumersidecar.New(&consumersidecar.Config{
		ListenAddr:      ":19018",
		SignerKey:       setup.SignerKey,
		Domain:          env.Domain(),
		TransportConfig: sidecar.ServerTransportConfig{Plaintext: true},
	}, zlog.Named("consumer"))
	go consumerSidecar.Run()
	defer consumerSidecar.Shutdown(nil)
	time.Sleep(100 * time.Millisecond)

	consumerClient := consumerv1connect.NewConsumerSidecarServiceClient(http.DefaultClient, "http://localhost:19018")

	_, err = consumerClient.Init(ctx, connect.NewRequest(&consumerv1.InitRequest{
		EscrowAccount: &commonv1.EscrowAccount{
			Payer:       commonv1.AddressFromEth(env.Payer.Address),
			Receiver:    commonv1.AddressFromEth(env.ServiceProvider.Address),
			DataService: commonv1.AddressFromEth(env.DataService.Address),
		},
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}
