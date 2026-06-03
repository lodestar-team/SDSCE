package integration

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	sds "github.com/graphprotocol/substreams-data-service"
	"github.com/graphprotocol/substreams-data-service/consumer/sidecar"
	"github.com/graphprotocol/substreams-data-service/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1/providerv1connect"
	providerauth "github.com/graphprotocol/substreams-data-service/provider/auth"
	paymentgateway "github.com/graphprotocol/substreams-data-service/provider/gateway"
	"github.com/graphprotocol/substreams-data-service/provider/plugin"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	psqlrepo "github.com/graphprotocol/substreams-data-service/provider/repository/psql"
	providersession "github.com/graphprotocol/substreams-data-service/provider/session"
	providerusage "github.com/graphprotocol/substreams-data-service/provider/usage"
	sidecarlib "github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/streamingfast/eth-go"
	"github.com/streamingfast/logging"
	"github.com/streamingfast/substreams/manifest"
	pbsubstreamsrpcv3 "github.com/streamingfast/substreams/pb/sf/substreams/rpc/v3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"
)

var firecoreLog, _ = logging.PackageLogger("firecore_test", "github.com/graphprotocol/substreams-data-service/test/integration/firecore")

const (
	defaultDummyBlockchainImage = "ghcr.io/streamingfast/dummy-blockchain:v1.7.7"
	dummyBlockchainImageEnvVar  = "SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE"
)

type dummyBlockchainOptions struct {
	GenesisBlockBurst   int
	SessionPluginConfig string
}

type firecoreProviderStack struct {
	PaymentGateway *paymentgateway.Gateway
	PluginGateway  *plugin.PluginGateway
}

func (s *firecoreProviderStack) Shutdown(err error) {
	if s.PaymentGateway != nil {
		s.PaymentGateway.Shutdown(err)
	}
	if s.PluginGateway != nil {
		s.PluginGateway.Shutdown(err)
	}
}

func TestFirecore(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping firecore integration test in short mode")
	}

	ctx := context.Background()
	env := SetupEnv(t)
	testStartedAt := time.Now().UTC()
	dummyBlockchainImage := getDummyBlockchainImage()

	firecoreLog.Info("selected dummy-blockchain runtime image",
		zap.String("image", dummyBlockchainImage),
		zap.String("env_var", dummyBlockchainImageEnvVar),
	)
	t.Logf("using dummy-blockchain image: %s", dummyBlockchainImage)

	// Step 1: Start dummy-blockchain/firecore and capture the host-reachable data plane endpoint.
	// The provider handshake must advertise the mapped host port, not the in-container :10016.
	dummyBlockchainContainer, substreamsEndpoint, err := startDummyBlockchainContainer(ctx, dummyBlockchainImage, 100)
	require.NoError(t, err, "failed to start dummy-blockchain container from image %q", dummyBlockchainImage)
	defer dummyBlockchainContainer.Terminate(ctx)
	defer func() {
		if !t.Failed() {
			return
		}
		dumpContainerLogs(t, ctx, dummyBlockchainContainer)
	}()
	providerDataPlaneEndpoint := "http://" + substreamsEndpoint

	// Step 2: Start provider gateways (payment + plugin) with Postgres repository
	// Listen on 0.0.0.0 so Docker containers can reach them via host.docker.internal
	//
	// Port allocation:
	//   19001: Payment Gateway (PUBLIC) - consumer sidecars connect here
	//   19003: Plugin Gateway (PRIVATE) - firehose-core plugins connect here
	//   9002:  Consumer Sidecar (localhost only)
	firecoreLog.Info("starting provider gateways",
		zap.String("payment_addr", "0.0.0.0:19001"),
		zap.String("plugin_addr", "0.0.0.0:19003"),
		zap.String("postgres_dsn", sanitizeDSN(PostgresTestDSN)),
	)

	pricingConfig := deterministicPricingConfig()
	gateways, err := startFirecoreProviderStack(
		ctx,
		"0.0.0.0:19001", // Payment Gateway - for consumer sidecars
		"0.0.0.0:19003", // Plugin Gateway - for firehose-core sds:// plugins
		env.ServiceProvider.Address,
		env.ChainID,
		env.Collector.Address,
		env.Escrow.Address,
		env.RPCURL,
		providerDataPlaneEndpoint,
		PostgresTestDSN,
		sidecarlib.ServerTransportConfig{
			Plaintext:   true,
			TLSCertFile: "",
			TLSKeyFile:  "",
		},
		sidecarlib.ServerTransportConfig{
			Plaintext:   true,
			TLSCertFile: "",
			TLSKeyFile:  "",
		},
		pricingConfig,
		sds.NewGRTFromUint64(1),
	)
	require.NoError(t, err, "failed to start provider gateways")
	defer gateways.Shutdown(nil)

	// Step 2: Wait for both gateways to be healthy
	firecoreLog.Info("waiting for payment gateway to become healthy")
	err = waitForGatewayHealth(ctx, "http://localhost:19001/healthz", 30*time.Second)
	require.NoError(t, err, "payment gateway failed to become healthy")
	firecoreLog.Info("payment gateway is healthy")

	firecoreLog.Info("waiting for plugin gateway to become healthy")
	err = waitForGatewayHealth(ctx, "http://localhost:19003/healthz", 30*time.Second)
	require.NoError(t, err, "plugin gateway failed to become healthy")
	firecoreLog.Info("plugin gateway is healthy")

	firecoreLog.Info("all infrastructure started successfully",
		zap.String("substreams_endpoint", substreamsEndpoint),
		zap.String("provider_control_plane_endpoint", "http://localhost:19001"),
	)

	// Step 3: Start consumer sidecar
	firecoreLog.Info("starting consumer sidecar", zap.String("listen_addr", ":9002"))

	receiver := env.ServiceProvider.Address
	sidecarConfig := &sidecar.Config{
		ListenAddr: ":9002",
		SignerKey:  env.Payer.PrivateKey,
		Domain:     horizon.NewDomain(env.ChainID, env.Collector.Address),
		IngressConfig: &sidecar.IngressConfig{
			Payer:                        env.Payer.Address,
			Receiver:                     &receiver,
			DataService:                  env.DataService.Address,
			ProviderControlPlaneEndpoint: "http://localhost:19001",
		},
		TransportConfig: sidecarlib.ServerTransportConfig{Plaintext: true},
	}

	consumerSidecar := sidecar.New(sidecarConfig, firecoreLog)
	go consumerSidecar.Run()
	defer consumerSidecar.Shutdown(nil)

	// Wait for sidecar to be healthy
	err = waitForSidecarHealth(ctx, "http://localhost:9002/healthz", 10*time.Second)
	require.NoError(t, err, "consumer sidecar failed to become healthy")
	firecoreLog.Info("consumer sidecar is healthy")

	// Step 4: Run E2E Substreams request (blocks 0-20)
	// This exercises the real provider path through firecore.
	firecoreLog.Info("running E2E Substreams request for blocks 0-20")

	blockCount, err := runSubstreamsViaSidecar(
		t,
		ctx,
		"common@v0.1.0",
		"map_clocks",
		"http://localhost:9002",
		0,
		20,
	)
	if isKnownFirecoreHeaderPropagationBlocker(err) {
		dumpContainerLogs(t, ctx, dummyBlockchainContainer)
		t.Skipf("default dummy-blockchain image is not compatible with the current SDS runtime path; set SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE to a rebuilt compatible image documented in docs/provider-runtime-compatibility.md: %v", err)
	}
	require.NoError(t, err, "firecore-backed sidecar ingress request must succeed")
	require.Greater(t, blockCount, 0, "expected at least one streamed Substreams response")

	evidence := loadFirecoreEvidence(t, ctx, testStartedAt, env)
	require.NotEmpty(t, evidence.SessionID, "expected a provider session to be created")
	require.Equal(t, 1, evidence.SessionCount, "expected exactly one matching provider session")
	require.GreaterOrEqual(t, evidence.WorkerCount+evidence.UsageEventCount, int64(1), "expected plugin activity to leave worker or usage evidence")

	providerClient := providerv1connect.NewPaymentGatewayServiceClient(http.DefaultClient, "http://localhost:19001")
	statusResp, err := providerClient.GetSessionStatus(ctx, connect.NewRequest(&providerv1.GetSessionStatusRequest{
		SessionId: evidence.SessionID,
	}))
	require.NoError(t, err, "payment gateway must expose repo-backed session status")
	require.True(t, statusResp.Msg.GetActive(), "session should still be active after the short stream")
	require.NotNil(t, statusResp.Msg.GetPaymentStatus(), "payment status must be present")
	require.Eventually(t, func() bool {
		statusResp, err = providerClient.GetSessionStatus(ctx, connect.NewRequest(&providerv1.GetSessionStatusRequest{
			SessionId: evidence.SessionID,
		}))
		if err != nil {
			firecoreLog.Warn("failed to refresh gateway session status for current rav check",
				zap.String("session_id", evidence.SessionID),
				zap.Error(err),
			)
			return false
		}

		paymentStatus := statusResp.Msg.GetPaymentStatus()
		if paymentStatus == nil || paymentStatus.GetCurrentRavValue() == nil {
			return false
		}

		return paymentStatus.GetCurrentRavValue().ToBigInt().Cmp(big.NewInt(0)) > 0
	}, 5*time.Second, 100*time.Millisecond, "expected provider session current_rav_value to advance above zero during the live stream")
	require.Eventually(t, func() bool {
		usageEvidence, err := loadFirecoreUsageEvidence(ctx, evidence.SessionID)
		if err != nil {
			firecoreLog.Warn("failed to refresh firecore usage evidence",
				zap.String("session_id", evidence.SessionID),
				zap.Error(err),
			)
			return false
		}

		evidence.UsageEventCount = usageEvidence.UsageEventCount
		evidence.UsageBlocks = usageEvidence.UsageBlocks
		evidence.UsageBytes = usageEvidence.UsageBytes
		evidence.UsageRequests = usageEvidence.UsageRequests

		statusResp, err = providerClient.GetSessionStatus(ctx, connect.NewRequest(&providerv1.GetSessionStatusRequest{
			SessionId: evidence.SessionID,
		}))
		if err != nil {
			firecoreLog.Warn("failed to refresh gateway session status",
				zap.String("session_id", evidence.SessionID),
				zap.Error(err),
			)
			return false
		}

		paymentStatus := statusResp.Msg.GetPaymentStatus()
		if paymentStatus == nil || paymentStatus.GetAccumulatedUsageValue() == nil {
			return false
		}

		if evidence.UsageBlocks+evidence.UsageBytes+evidence.UsageRequests < 1 {
			return false
		}

		expectedAccumulatedValue := pricingConfig.CalculateUsageCost(uint64(evidence.UsageBlocks), uint64(evidence.UsageBytes)).BigInt()
		return paymentStatus.GetAccumulatedUsageValue().ToBigInt().Cmp(expectedAccumulatedValue) == 0
	}, 3*time.Second, 100*time.Millisecond, "expected metering to update the payment-state repository with the exact provider-priced value")

	expectedAccumulatedValue := pricingConfig.CalculateUsageCost(uint64(evidence.UsageBlocks), uint64(evidence.UsageBytes)).BigInt()
	require.Equal(t, 0, statusResp.Msg.GetPaymentStatus().GetAccumulatedUsageValue().ToBigInt().Cmp(expectedAccumulatedValue), "expected payment status to match the exact provider-priced plugin metering total")

	firecoreLog.Info("E2E Substreams request completed successfully",
		zap.String("session_id", evidence.SessionID),
		zap.Int64("worker_count", evidence.WorkerCount),
		zap.Int64("usage_event_count", evidence.UsageEventCount),
	)

	// Full circle: the RAV produced by the live metered stream is collectible
	// on-chain (satisfies collect()'s on-chain gates against the live contracts).
	verifyFirecoreProducedRAVCollectible(t, ctx, env, evidence.SessionID)
}

// verifyFirecoreProducedRAVCollectible proves the full circle: a RAV produced by a
// real metered Substreams stream (not a hand-built one) satisfies the on-chain
// gates SubstreamsDataService.collect() enforces. It reads the collectible the
// stream left in the provider repository and checks, against the live contracts,
// that the GraphTallyCollector recovers its EIP-712 signature to an authorized
// signer and that the service provider is registered — i.e. collect() would
// accept it. The GRT-moving collect for realistic values is covered by
// TestCollectRAV and the Arbitrum One fork rehearsal (devel/arb-one-collect-rehearsal.sh);
// the value here is intentionally dust (the test's PricePerBlock is 1 wei).
func verifyFirecoreProducedRAVCollectible(t *testing.T, ctx context.Context, env *TestEnv, sessionID string) {
	t.Helper()

	repo, err := paymentgateway.NewRepositoryFromDSN(ctx, PostgresTestDSN, zap.NewNop())
	require.NoError(t, err, "open provider repository")
	defer repo.Close()

	collectibleState := repository.CollectionStateCollectible
	var record *repository.CollectionRecord
	require.Eventually(t, func() bool {
		records, err := repo.CollectionList(ctx, repository.CollectionFilter{State: &collectibleState})
		if err != nil {
			return false
		}
		for _, r := range records {
			if r.Key.SessionID == sessionID && r.SignedRAV != nil {
				record = r
				return true
			}
		}
		return false
	}, 5*time.Second, 100*time.Millisecond, "expected a collectible record from the streamed session")

	rav := record.SignedRAV
	require.Positive(t, rav.Message.ValueAggregate.Sign(), "streamed RAV must carry a positive value")

	// The live GraphTallyCollector must recover the RAV's EIP-712 signature to the
	// streaming signer (the payer signs its own RAVs in this harness).
	recovered, err := callRecoverRAVSigner(env, rav)
	require.NoError(t, err, "collector must recover the streamed RAV signer")
	require.Equal(t, env.Payer.Address, recovered, "recovered signer must match the streaming signer")

	// collect()'s on-chain signature gate checks isAuthorized(payer, signer). The
	// demo state authorizes a separate signer, so authorize the payer-as-signer
	// here, exactly as a real consumer would before settlement.
	require.NoError(t, env.AuthorizeSigner(env.Payer.PrivateKey), "authorize the streaming signer for collection")

	// Settle the stream-produced RAV on-chain via SubstreamsDataService.collect() —
	// the same step the automated collection daemon performs.
	collected, err := callDataServiceCollect(env, rav, 100000)
	require.NoError(t, err, "stream-produced RAV must be collectible on-chain")
	require.Equal(t, rav.Message.ValueAggregate.Uint64(), collected, "collected amount must equal the streamed RAV value")

	firecoreLog.Info("full-circle: stream-produced RAV settled on-chain",
		zap.String("session_id", sessionID),
		zap.Stringer("recovered_signer", recovered),
		zap.Uint64("collected", collected),
	)
}

func TestFirecoreStopsStreamOnLowFunds(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping firecore integration test in short mode")
	}

	ctx := context.Background()
	env := SetupEnv(t)
	dummyBlockchainImage := getDummyBlockchainImage()

	config := DefaultTestSetupConfig()
	config.EscrowAmount = big.NewInt(1)
	participants := SetupIsolatedRuntimeParticipants(t, env, config)
	payer := participants.Payer
	serviceProvider := participants.ServiceProvider
	setup := participants.Setup
	assertNoProviderRuntimeEvidenceForParticipants(t, ctx, payer.Address, serviceProvider.Address, env.DataService.Address)
	testStartedAt := time.Now().UTC()

	dummyBlockchainContainer, substreamsEndpoint, err := startDummyBlockchainContainerWithOptions(ctx, dummyBlockchainImage, dummyBlockchainOptions{
		GenesisBlockBurst:   100,
		SessionPluginConfig: "sds://host.docker.internal:19003?plaintext=true&keep-alive-delay=250ms&minimal-worker-life-duration=100ms",
	})
	require.NoError(t, err, "failed to start dummy-blockchain container from image %q", dummyBlockchainImage)
	defer dummyBlockchainContainer.Terminate(ctx)
	defer func() {
		if !t.Failed() {
			return
		}
		dumpContainerLogs(t, ctx, dummyBlockchainContainer)
	}()

	pricingConfig := deterministicPricingConfig()
	gateways, err := startFirecoreProviderStack(
		ctx,
		"0.0.0.0:19001",
		"0.0.0.0:19003",
		serviceProvider.Address,
		env.ChainID,
		env.Collector.Address,
		env.Escrow.Address,
		env.RPCURL,
		"http://"+substreamsEndpoint,
		PostgresTestDSN,
		sidecarlib.ServerTransportConfig{
			Plaintext:   true,
			TLSCertFile: "",
			TLSKeyFile:  "",
		},
		sidecarlib.ServerTransportConfig{
			Plaintext:   true,
			TLSCertFile: "",
			TLSKeyFile:  "",
		},
		pricingConfig,
		sds.NewGRTFromUint64(1),
	)
	require.NoError(t, err, "failed to start provider gateways")
	defer gateways.Shutdown(nil)

	require.NoError(t, waitForGatewayHealth(ctx, "http://localhost:19001/healthz", 30*time.Second), "payment gateway failed to become healthy")
	require.NoError(t, waitForGatewayHealth(ctx, "http://localhost:19003/healthz", 30*time.Second), "plugin gateway failed to become healthy")

	receiver := serviceProvider.Address
	consumerSidecar := sidecar.New(&sidecar.Config{
		ListenAddr: ":9002",
		SignerKey:  setup.SignerKey,
		Domain:     horizon.NewDomain(env.ChainID, env.Collector.Address),
		IngressConfig: &sidecar.IngressConfig{
			Payer:                        payer.Address,
			Receiver:                     &receiver,
			DataService:                  env.DataService.Address,
			ProviderControlPlaneEndpoint: "http://localhost:19001",
		},
		TransportConfig: sidecarlib.ServerTransportConfig{Plaintext: true},
	}, firecoreLog)
	go consumerSidecar.Run()
	defer consumerSidecar.Shutdown(nil)

	require.NoError(t, waitForSidecarHealth(ctx, "http://localhost:9002/healthz", 10*time.Second), "consumer sidecar failed to become healthy")

	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	_, err = runSubstreamsViaSidecar(
		t,
		runCtx,
		"common@v0.1.0",
		"map_clocks",
		"http://localhost:9002",
		0,
		100_000,
	)
	if isKnownFirecoreHeaderPropagationBlocker(err) {
		dumpContainerLogs(t, ctx, dummyBlockchainContainer)
		t.Skipf("default dummy-blockchain image is not compatible with the current SDS runtime path; set SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE to a rebuilt compatible image documented in docs/provider-runtime-compatibility.md: %v", err)
	}
	require.Error(t, err, "low-funds Firecore run must stop the live stream")
	require.True(t, isQuotaExceededRuntimeFailure(err), "expected quota/resource exhausted runtime failure, got: %v", err)

	evidence := loadFirecoreEvidenceForParticipants(t, ctx, testStartedAt, payer.Address, serviceProvider.Address, env.DataService.Address)
	require.NotEmpty(t, evidence.SessionID, "expected a provider session to be created")
	require.Equal(t, 1, evidence.SessionCount, "expected exactly one low-funds provider session")

	providerClient := providerv1connect.NewPaymentGatewayServiceClient(http.DefaultClient, "http://localhost:19001")
	require.Eventually(t, func() bool {
		statusResp, err := providerClient.GetSessionStatus(ctx, connect.NewRequest(&providerv1.GetSessionStatusRequest{
			SessionId: evidence.SessionID,
		}))
		if err != nil {
			firecoreLog.Warn("failed to refresh low-funds gateway session status",
				zap.String("session_id", evidence.SessionID),
				zap.Error(err),
			)
			return false
		}
		if statusResp.Msg.GetActive() {
			return false
		}

		state, err := loadFirecoreSessionState(ctx, evidence.SessionID)
		if err != nil {
			firecoreLog.Warn("failed to refresh low-funds session state",
				zap.String("session_id", evidence.SessionID),
				zap.Error(err),
			)
			return false
		}

		return state.Status == repository.SessionStatusTerminated &&
			state.EndReason == commonv1.EndReason_END_REASON_PAYMENT_ISSUE &&
			state.WorkerCount == 0
	}, 10*time.Second, 100*time.Millisecond, "expected low-funds session termination and worker cleanup")
}

func startFirecoreProviderStack(
	ctx context.Context,
	paymentListenAddr string,
	pluginListenAddr string,
	serviceProviderAddr eth.Address,
	chainID uint64,
	collectorAddr eth.Address,
	escrowAddr eth.Address,
	rpcEndpoint string,
	dataPlaneEndpoint string,
	repositoryDSN string,
	paymentTransportConfig sidecarlib.ServerTransportConfig,
	pluginTransportConfig sidecarlib.ServerTransportConfig,
	pricingConfig *sidecarlib.PricingConfig,
	ravRequestThreshold sds.GRT,
) (*firecoreProviderStack, error) {
	repo, err := paymentgateway.NewRepositoryFromDSN(ctx, repositoryDSN, firecoreLog)
	if err != nil {
		return nil, err
	}

	domain := horizon.NewDomain(chainID, collectorAddr)

	payment, err := paymentgateway.New(&paymentgateway.Config{
		ListenAddr:          paymentListenAddr,
		ServiceProvider:     serviceProviderAddr,
		Domain:              domain,
		CollectorAddr:       collectorAddr,
		EscrowAddr:          escrowAddr,
		RPCEndpoint:         rpcEndpoint,
		PricingConfig:       pricingConfig,
		RAVRequestThreshold: ravRequestThreshold,
		DataPlaneEndpoint:   dataPlaneEndpoint,
		Repository:          repo,
		TransportConfig:     paymentTransportConfig,
	}, firecoreLog)
	if err != nil {
		return nil, err
	}
	go payment.Run()

	var collectorQuerier providerauth.CollectorAuthorizer
	if rpcEndpoint != "" && collectorAddr != nil {
		collectorQuerier = sidecarlib.NewCollectorQuerier(rpcEndpoint, collectorAddr)
	}

	authService := providerauth.NewAuthService(serviceProviderAddr, domain, collectorQuerier, repo)
	repoPricingConfig := repository.PricingConfig{}
	if pricingConfig != nil {
		repoPricingConfig.PricePerBlock = pricingConfig.PricePerBlock
		repoPricingConfig.PricePerByte = pricingConfig.PricePerByte
	}
	usageService := providerusage.NewUsageService(repo, repoPricingConfig, payment)
	sessionService := providersession.NewSessionService(repo, nil)

	pluginGateway := plugin.NewPluginGateway(&plugin.PluginGatewayConfig{
		ListenAddr:      pluginListenAddr,
		AuthService:     authService,
		UsageService:    usageService,
		SessionService:  sessionService,
		TransportConfig: pluginTransportConfig,
	}, firecoreLog)
	go pluginGateway.Run()

	return &firecoreProviderStack{
		PaymentGateway: payment,
		PluginGateway:  pluginGateway,
	}, nil
}

func runSubstreamsViaSidecar(
	t *testing.T,
	ctx context.Context,
	manifestPath string,
	module string,
	endpoint string,
	startBlock int64,
	stopBlock uint64,
) (int, error) {
	t.Helper()

	reader, err := manifest.NewReader(manifestPath)
	require.NoError(t, err, "create manifest reader")

	bundle, err := reader.Read()
	require.NoError(t, err, "load substreams package")

	conn, closeConn := dialSubstreamsGRPC(t, endpoint)
	defer closeConn()

	stream, err := pbsubstreamsrpcv3.NewStreamClient(conn).Blocks(ctx, &pbsubstreamsrpcv3.Request{
		StartBlockNum: startBlock,
		StopBlockNum:  stopBlock,
		OutputModule:  module,
		Package:       bundle.Package,
	})
	if err != nil {
		return 0, err
	}

	blockCount := 0
	for {
		_, err := stream.Recv()
		if err == nil {
			blockCount++
			continue
		}
		if err == io.EOF {
			return blockCount, nil
		}
		return blockCount, err
	}
}

// waitForSidecarHealth polls the sidecar health endpoint until it returns 200 or timeout
func waitForSidecarHealth(ctx context.Context, healthURL string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for sidecar health: %w", ctx.Err())
		case <-ticker.C:
			resp, err := http.Get(healthURL)
			if err != nil {
				continue
			}
			resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
	}
}

// newDummyBlockchainContainer creates a dummy blockchain container for testing
// It starts reader-node, merger, relayer, and substreams-tier1 with SDS plugins
func newDummyBlockchainContainer(ctx context.Context, image string, opts dummyBlockchainOptions) (testcontainers.Container, error) {
	// Build reader arguments for the dummy-blockchain binary
	readerArgs := fmt.Sprintf("start --log-level=error --tracer=firehose --store-dir=/tmp/data --genesis-block-burst=%d --block-rate=120 --block-size=1500 --genesis-height=0 --server-addr=:9777", opts.GenesisBlockBurst)

	sessionPluginConfig := opts.SessionPluginConfig
	if sessionPluginConfig == "" {
		sessionPluginConfig = "sds://host.docker.internal:19003?plaintext=true"
	}

	// Build firecore start command - start required components
	// Configure SDS plugins to connect to the Provider Gateway running on the host
	cmd := []string{
		"start",
		"reader-node", "merger", "relayer", "substreams-tier1", // Explicitly specify components to start
		"-c", "",
		"--data-dir=/tmp/firehose-data",
		"--log-to-file=false",
		"--log-format=stackdriver",
		"-vvv", // Verbose logging to see auth plugin debug logs
		// SDS Plugin configuration - connect to plugin gateway on host (port 19003)
		// Use host.docker.internal to reach services running on the host machine
		"--common-auth-plugin=sds://host.docker.internal:19003?plaintext=true",
		"--common-session-plugin=" + sessionPluginConfig,
		"--common-metering-plugin=sds://host.docker.internal:19003?plaintext=true&network=test",
		"--reader-node-path=/app/dummy-blockchain",
		"--reader-node-arguments=" + readerArgs,
		// Substreams tier1 needs to know the block type and chain info for Info Endpoint
		"--substreams-tier1-block-type=sf.acme.type.v1.Block",
		"--advertise-chain-name=acme",
		"--ignore-advertise-validation", // Skip advertise/info server validation
	}

	req := testcontainers.ContainerRequest{
		Image:        image,
		Cmd:          cmd,
		ExposedPorts: []string{"10016/tcp"}, // Expose Substreams tier1 port
		Env: map[string]string{
			"DLOG": os.Getenv("DLOG"), // Pass through DLOG env var for logging configuration
		},
		// Use tmpfs for the data directory
		Tmpfs: map[string]string{
			"/tmp/firehose-data": "",
			"/tmp/data":          "",
		},
		// Add host.docker.internal mapping for Linux compatibility
		ExtraHosts: []string{"host.docker.internal:host-gateway"},
		WaitingFor: wait.ForAll(
			// Wait for reader-node to start producing blocks
			wait.ForLog("console reader protocol version init").WithStartupTimeout(15*time.Second),
			// Wait for hub to be ready (required before tier1 can start)
			wait.ForLog("Hub is ready").WithStartupTimeout(15*time.Second),
			// Wait for tier1 to start serving (happens after hub reaches real-time sync)
			wait.ForLog("serving gRPC").WithStartupTimeout(30*time.Second),
		),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start dummy-blockchain container: %w", err)
	}

	return container, nil
}

// startDummyBlockchainContainer starts a dummy blockchain container, retrieves its endpoint, and verifies it's healthy
func startDummyBlockchainContainer(ctx context.Context, image string, genesisBlockBurst int) (testcontainers.Container, string, error) {
	return startDummyBlockchainContainerWithOptions(ctx, image, dummyBlockchainOptions{GenesisBlockBurst: genesisBlockBurst})
}

func startDummyBlockchainContainerWithOptions(ctx context.Context, image string, opts dummyBlockchainOptions) (testcontainers.Container, string, error) {
	firecoreLog.Info("setting up dummy-blockchain container", zap.String("image", image))

	container, err := newDummyBlockchainContainer(ctx, image, opts)
	if err != nil {
		return nil, "", fmt.Errorf("failed to start dummy-blockchain container: %w", err)
	}

	// Get the exposed port for Substreams tier1
	substreamsHost, err := container.Host(ctx)
	if err != nil {
		container.Terminate(ctx)
		return nil, "", fmt.Errorf("failed to get dummy-blockchain host: %w", err)
	}

	substreamsPort, err := container.MappedPort(ctx, "10016/tcp")
	if err != nil {
		container.Terminate(ctx)
		return nil, "", fmt.Errorf("failed to get Substreams tier1 port: %w", err)
	}

	substreamsEndpoint := fmt.Sprintf("%s:%s", substreamsHost, substreamsPort.Port())
	firecoreLog.Info("dummy-blockchain container started successfully with Substreams tier1",
		zap.String("substreams_endpoint", substreamsEndpoint),
	)

	// Verify container is healthy and running
	firecoreLog.Info("verifying dummy-blockchain container health")
	state, err := container.State(ctx)
	if err != nil {
		container.Terminate(ctx)
		return nil, "", fmt.Errorf("failed to get container state: %w", err)
	}

	if !state.Running {
		container.Terminate(ctx)
		return nil, "", fmt.Errorf("dummy-blockchain container is not running")
	}

	firecoreLog.Info("all components started successfully")
	firecoreLog.Info("Substreams tier1 endpoint available", zap.String("endpoint", substreamsEndpoint))

	return container, substreamsEndpoint, nil
}

func getDummyBlockchainImage() string {
	image := strings.TrimSpace(os.Getenv(dummyBlockchainImageEnvVar))
	if image == "" {
		return defaultDummyBlockchainImage
	}

	return image
}

// waitForGatewayHealth polls the gateway health endpoint until it returns 200 or timeout
func waitForGatewayHealth(ctx context.Context, healthURL string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for gateway health: %w", ctx.Err())
		case <-ticker.C:
			resp, err := http.Get(healthURL)
			if err != nil {
				firecoreLog.Debug("health check failed, retrying", zap.Error(err))
				continue
			}
			resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				return nil
			}

			firecoreLog.Debug("health check returned non-200 status, retrying",
				zap.Int("status_code", resp.StatusCode),
			)
		}
	}
}

type firecoreSessionEvidence struct {
	SessionID       string
	SessionCount    int
	WorkerCount     int64
	UsageEventCount int64
	UsageBlocks     int64
	UsageBytes      int64
	UsageRequests   int64
	Status          repository.SessionStatus
	EndReason       commonv1.EndReason
}

type firecoreSessionRow struct {
	ID        string        `db:"id"`
	Status    string        `db:"status"`
	EndReason sql.NullInt32 `db:"end_reason"`
}

func loadFirecoreEvidence(t *testing.T, ctx context.Context, createdAfter time.Time, env *TestEnv) firecoreSessionEvidence {
	t.Helper()
	return loadFirecoreEvidenceForParticipants(t, ctx, createdAfter, env.Payer.Address, env.ServiceProvider.Address, env.DataService.Address)
}

func loadFirecoreEvidenceForParticipants(t *testing.T, ctx context.Context, createdAfter time.Time, payer, receiver, dataService eth.Address) firecoreSessionEvidence {
	t.Helper()

	dbConn, err := psqlrepo.GetConnectionFromDSN(ctx, toPostgresDriverDSN(PostgresTestDSN))
	require.NoError(t, err, "connect to provider postgres repo")
	defer dbConn.Close()

	sessionRows := make([]firecoreSessionRow, 0, 1)
	err = dbConn.SelectContext(ctx, &sessionRows, `
		SELECT id, status, end_reason
		FROM sessions
		WHERE payer = $1
		  AND receiver = $2
		  AND data_service = $3
		  AND created_at >= $4
		ORDER BY created_at ASC
	`, payer.Bytes(), receiver.Bytes(), dataService.Bytes(), createdAfter)
	require.NoError(t, err, "query firecore-created provider sessions")
	require.Len(t, sessionRows, 1, "expected one provider session for the test payer/provider/data service tuple")

	var evidence firecoreSessionEvidence
	evidence.SessionID = sessionRows[0].ID
	evidence.SessionCount = len(sessionRows)
	evidence.Status = repository.SessionStatus(sessionRows[0].Status)
	if sessionRows[0].EndReason.Valid {
		evidence.EndReason = commonv1.EndReason(sessionRows[0].EndReason.Int32)
	}

	err = dbConn.GetContext(ctx, &evidence.WorkerCount, `SELECT COUNT(*) FROM workers WHERE session_id = $1`, evidence.SessionID)
	require.NoError(t, err, "count worker rows for firecore session")

	err = dbConn.GetContext(ctx, &evidence.UsageEventCount, `SELECT COUNT(*) FROM usage_events WHERE session_id = $1`, evidence.SessionID)
	require.NoError(t, err, "count usage event rows for firecore session")

	err = dbConn.QueryRowxContext(ctx, `
		SELECT
			COALESCE(SUM(blocks), 0) AS blocks,
			COALESCE(SUM(bytes), 0) AS bytes,
			COALESCE(SUM(requests), 0) AS requests
		FROM usage_events
		WHERE session_id = $1
	`, evidence.SessionID).Scan(&evidence.UsageBlocks, &evidence.UsageBytes, &evidence.UsageRequests)
	require.NoError(t, err, "sum usage event rows for firecore session")

	return evidence
}

func loadFirecoreSessionState(ctx context.Context, sessionID string) (firecoreSessionEvidence, error) {
	dbConn, err := psqlrepo.GetConnectionFromDSN(ctx, toPostgresDriverDSN(PostgresTestDSN))
	if err != nil {
		return firecoreSessionEvidence{}, err
	}
	defer dbConn.Close()

	var row firecoreSessionRow
	if err := dbConn.GetContext(ctx, &row, `
		SELECT id, status, end_reason
		FROM sessions
		WHERE id = $1
	`, sessionID); err != nil {
		return firecoreSessionEvidence{}, err
	}

	var workerCount int64
	if err := dbConn.GetContext(ctx, &workerCount, `SELECT COUNT(*) FROM workers WHERE session_id = $1`, sessionID); err != nil {
		return firecoreSessionEvidence{}, err
	}

	state := firecoreSessionEvidence{
		SessionID:   row.ID,
		Status:      repository.SessionStatus(row.Status),
		WorkerCount: workerCount,
	}
	if row.EndReason.Valid {
		state.EndReason = commonv1.EndReason(row.EndReason.Int32)
	}

	return state, nil
}

func loadFirecoreUsageEvidence(ctx context.Context, sessionID string) (firecoreSessionEvidence, error) {
	dbConn, err := psqlrepo.GetConnectionFromDSN(ctx, toPostgresDriverDSN(PostgresTestDSN))
	if err != nil {
		return firecoreSessionEvidence{}, fmt.Errorf("connect to provider postgres repo: %w", err)
	}
	defer dbConn.Close()

	evidence := firecoreSessionEvidence{SessionID: sessionID}
	if err := dbConn.GetContext(ctx, &evidence.UsageEventCount, `SELECT COUNT(*) FROM usage_events WHERE session_id = $1`, sessionID); err != nil {
		return firecoreSessionEvidence{}, fmt.Errorf("count usage event rows for firecore session: %w", err)
	}

	if err := dbConn.QueryRowxContext(ctx, `
		SELECT
			COALESCE(SUM(blocks), 0) AS blocks,
			COALESCE(SUM(bytes), 0) AS bytes,
			COALESCE(SUM(requests), 0) AS requests
		FROM usage_events
		WHERE session_id = $1
	`, sessionID).Scan(&evidence.UsageBlocks, &evidence.UsageBytes, &evidence.UsageRequests); err != nil {
		return firecoreSessionEvidence{}, fmt.Errorf("sum usage event rows for firecore session: %w", err)
	}

	return evidence, nil
}

func isQuotaExceededRuntimeFailure(err error) bool {
	if err == nil {
		return false
	}

	msg := err.Error()
	return strings.Contains(msg, "Quota exceeded") ||
		strings.Contains(msg, "ResourceExhausted") ||
		strings.Contains(msg, "resource exhausted")
}

func dumpContainerLogs(t *testing.T, ctx context.Context, container testcontainers.Container) {
	t.Helper()

	logs, err := container.Logs(ctx)
	if err != nil {
		t.Logf("failed to retrieve firecore container logs: %v", err)
		return
	}
	defer logs.Close()

	buf := make([]byte, 4096)
	var logBuf []byte
	for {
		n, readErr := logs.Read(buf)
		if n > 0 {
			logBuf = append(logBuf, buf[:n]...)
		}
		if readErr != nil {
			break
		}
	}

	if len(logBuf) == 0 {
		t.Log("firecore container produced no readable logs")
		return
	}

	firecoreLog.Info("firecore container logs",
		zap.String("logs", string(logBuf)),
	)
}

func isKnownFirecoreHeaderPropagationBlocker(err error) bool {
	if err == nil {
		return false
	}

	msg := err.Error()
	return strings.Contains(msg, "missing x-sds-rav header") &&
		(strings.Contains(msg, "stream auth failure") ||
			strings.Contains(msg, "authentication: unauthenticated") ||
			strings.Contains(msg, "code = Unauthenticated"))
}
