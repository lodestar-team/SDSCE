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
	oraclepkg "github.com/graphprotocol/substreams-data-service/oracle"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	consumerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1/consumerv1connect"
	providergateway "github.com/graphprotocol/substreams-data-service/provider/gateway"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	pbsubstreams "github.com/streamingfast/substreams/pb/sf/substreams/v1"
)

// TestPaymentFlowBasic_EndSessionRemainsDeprecatedForManagedSessions
// verifies that Init still bootstraps a managed provider session while the
// remaining manual EndSession surface stays disabled for managed sessions.
func TestPaymentFlowBasic_EndSessionRemainsDeprecatedForManagedSessions(t *testing.T) {
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
		ListenAddr:          ":19001",
		ServiceProvider:     env.ServiceProvider.Address,
		Domain:              domain,
		CollectorAddr:       env.Collector.Address,
		EscrowAddr:          env.Escrow.Address,
		RPCEndpoint:         env.RPCURL,
		RAVRequestThreshold: sds.NewGRTFromUint64(1),
		DataPlaneEndpoint:   "substreams.provider.example:443",
		TransportConfig:     sidecar.ServerTransportConfig{Plaintext: true},
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

	t.Log("Step 2: End session is deprecated for managed sessions")
	_, err = consumerClient.EndSession(ctx, connect.NewRequest(&consumerv1.EndSessionRequest{
		SessionId: consumerSessionID,
		FinalUsage: &commonv1.Usage{
			BlocksProcessed:  50,
			BytesTransferred: 25000,
			Requests:         1,
			Cost:             commonv1.GRTFromBigInt(big.NewInt(50000000)),
		},
	}))
	require.Error(t, err, "consumer EndSession must reject provider-managed wrapper flow")
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
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
		RAVRequestThreshold: sds.NewGRTFromUint64(1),
		TransportConfig:     sidecar.ServerTransportConfig{Plaintext: true},
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

func TestInit_FailsWithoutOracleConfigurationAndDirectProviderOverride(t *testing.T) {
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
		SubstreamsPackage: &pbsubstreams.Package{Network: "mainnet"},
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
}

func TestInit_UsesOracleDiscoveryWhenDirectProviderOverrideAbsent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	setup, err := env.SetupTestWithSigner(nil)
	require.NoError(t, err)

	providerGateway := providergateway.New(&providergateway.Config{
		ListenAddr:        ":19031",
		ServiceProvider:   env.ServiceProvider.Address,
		Domain:            env.Domain(),
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

	oracleShutdown := startOracleForTest(t, ":19032", `
providers:
  - id: provider-a
    service_provider: "`+env.ServiceProvider.Address.Pretty()+`"
    control_plane_endpoint: "http://localhost:19031"
networks:
  mainnet:
    pricing:
      price_per_block: "2 GRT"
      price_per_byte: "0 GRT"
    provider_ids:
      - provider-a
`)
	defer oracleShutdown()

	consumerSidecar := consumersidecar.New(&consumersidecar.Config{
		ListenAddr:      ":19033",
		SignerKey:       setup.SignerKey,
		Domain:          env.Domain(),
		OracleEndpoint:  "http://localhost:19032",
		TransportConfig: sidecar.ServerTransportConfig{Plaintext: true},
	}, zlog.Named("consumer"))
	go consumerSidecar.Run()
	defer consumerSidecar.Shutdown(nil)
	time.Sleep(100 * time.Millisecond)

	consumerClient := consumerv1connect.NewConsumerSidecarServiceClient(http.DefaultClient, "http://localhost:19033")

	initResp, err := consumerClient.Init(ctx, connect.NewRequest(&consumerv1.InitRequest{
		EscrowAccount: &commonv1.EscrowAccount{
			Payer:       commonv1.AddressFromEth(env.Payer.Address),
			Receiver:    commonv1.AddressFromEth(env.ServiceProvider.Address),
			DataService: commonv1.AddressFromEth(env.DataService.Address),
		},
		SubstreamsPackage: &pbsubstreams.Package{Network: "eth-mainnet"},
	}))
	require.NoError(t, err)
	require.Equal(t, "substreams.provider.example:443", initResp.Msg.GetDataPlaneEndpoint())
	require.NotNil(t, initResp.Msg.GetSession().GetPricingConfig())
	require.Equal(t, "1", initResp.Msg.GetSession().GetPricingConfig().GetPricePerBlock().ToBigInt().String())
	require.Equal(t, 1, providerGateway.SessionCount())
}

func TestInit_RejectsProviderPricingAboveOracleCeiling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	env := devenv.Get()
	require.NotNil(t, env, "devenv not started")

	setup, err := env.SetupTestWithSigner(nil)
	require.NoError(t, err)

	providerGateway := providergateway.New(&providergateway.Config{
		ListenAddr:        ":19034",
		ServiceProvider:   env.ServiceProvider.Address,
		Domain:            env.Domain(),
		CollectorAddr:     env.Collector.Address,
		EscrowAddr:        env.Escrow.Address,
		RPCEndpoint:       env.RPCURL,
		DataPlaneEndpoint: "substreams.provider.example:443",
		PricingConfig: &sidecar.PricingConfig{
			PricePerBlock: sds.MustNewGRT("2 GRT"),
			PricePerByte:  sds.ZeroGRT(),
		},
		RAVRequestThreshold: sds.NewGRTFromUint64(1),
		TransportConfig:     sidecar.ServerTransportConfig{Plaintext: true},
	}, zlog.Named("provider"))
	go providerGateway.Run()
	defer providerGateway.Shutdown(nil)
	time.Sleep(100 * time.Millisecond)

	oracleShutdown := startOracleForTest(t, ":19035", `
providers:
  - id: provider-a
    service_provider: "`+env.ServiceProvider.Address.Pretty()+`"
    control_plane_endpoint: "http://localhost:19034"
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
		ListenAddr:      ":19036",
		SignerKey:       setup.SignerKey,
		Domain:          env.Domain(),
		OracleEndpoint:  "http://localhost:19035",
		TransportConfig: sidecar.ServerTransportConfig{Plaintext: true},
	}, zlog.Named("consumer"))
	go consumerSidecar.Run()
	defer consumerSidecar.Shutdown(nil)
	time.Sleep(100 * time.Millisecond)

	consumerClient := consumerv1connect.NewConsumerSidecarServiceClient(http.DefaultClient, "http://localhost:19036")

	_, err = consumerClient.Init(ctx, connect.NewRequest(&consumerv1.InitRequest{
		EscrowAccount: &commonv1.EscrowAccount{
			Payer:       commonv1.AddressFromEth(env.Payer.Address),
			Receiver:    commonv1.AddressFromEth(env.ServiceProvider.Address),
			DataService: commonv1.AddressFromEth(env.DataService.Address),
		},
		SubstreamsPackage: &pbsubstreams.Package{Network: "mainnet"},
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
}

func startOracleForTest(t *testing.T, listenAddr string, configYAML string) func() {
	t.Helper()

	catalog, err := oraclepkg.ParseCatalog([]byte(configYAML))
	require.NoError(t, err)

	oracleServer := oraclepkg.New(&oraclepkg.Config{
		ListenAddr: listenAddr,
		Catalog:    catalog,
		TransportConfig: sidecar.ServerTransportConfig{
			Plaintext: true,
		},
	}, zlog.Named("oracle"))
	go oracleServer.Run()
	time.Sleep(100 * time.Millisecond)

	return func() {
		oracleServer.Shutdown(nil)
	}
}
