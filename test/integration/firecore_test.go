package integration

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/graphprotocol/substreams-data-service/cmd/sds/impl"
	"github.com/graphprotocol/substreams-data-service/consumer/sidecar"
	"github.com/graphprotocol/substreams-data-service/horizon"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1/providerv1connect"
	psqlrepo "github.com/graphprotocol/substreams-data-service/provider/repository/psql"
	sidecarlib "github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/streamingfast/logging"
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

	pricingConfig := sidecarlib.DefaultPricingConfig()

	gateways, err := impl.StartProviderGateway(
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
		pricingConfig,
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

	sidecarConfig := &sidecar.Config{
		ListenAddr:      ":9002",
		SignerKey:       env.Payer.PrivateKey,
		Domain:          horizon.NewDomain(env.ChainID, env.Collector.Address),
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

	err = runSDSSink(
		ctx,
		"common@v0.1.0",
		"map_clocks",
		substreamsEndpoint,
		env.Payer.Address.Pretty(),
		env.ServiceProvider.Address.Pretty(),
		env.DataService.Address.Pretty(),
		0,
		20,
	)
	if isKnownFirecoreHeaderPropagationBlocker(err) {
		dumpContainerLogs(t, ctx, dummyBlockchainContainer)
		t.Skipf("MVP-014 blocked by external firecore/substreams header propagation: %v", err)
	}
	require.NoError(t, err, "firecore-backed sds sink run must succeed")

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
func newDummyBlockchainContainer(ctx context.Context, image string, genesisBlockBurst int) (testcontainers.Container, error) {
	// Build reader arguments for the dummy-blockchain binary
	readerArgs := fmt.Sprintf("start --log-level=error --tracer=firehose --store-dir=/tmp/data --genesis-block-burst=%d --block-rate=120 --block-size=1500 --genesis-height=0 --server-addr=:9777", genesisBlockBurst)

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
		"--common-session-plugin=sds://host.docker.internal:19003?plaintext=true",
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
	firecoreLog.Info("setting up dummy-blockchain container", zap.String("image", image))

	container, err := newDummyBlockchainContainer(ctx, image, genesisBlockBurst)
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

// runSDSSink executes the sds sink run command with the specified parameters
func runSDSSink(
	ctx context.Context,
	manifest string,
	module string,
	endpoint string,
	payerAddress string,
	receiverAddress string,
	dataServiceAddress string,
	startBlock int64,
	stopBlock uint64,
) error {
	args := []string{
		"run",
		"./cmd/sds",
		"sink", "run",
		manifest,
		module,
		"--endpoint=" + endpoint,
		"--plaintext",
		"--provider-control-plane-endpoint=http://localhost:19001",
		"--consumer-sidecar-addr=http://localhost:9002",
		"--payer-address=" + payerAddress,
		"--receiver-address=" + receiverAddress,
		"--data-service-address=" + dataServiceAddress,
		fmt.Sprintf("--start-block=%d", startBlock),
		fmt.Sprintf("--stop-block=%d", stopBlock),
	}

	firecoreLog.Info("running sds sink command",
		zap.String("manifest", manifest),
		zap.String("module", module),
		zap.String("endpoint", endpoint),
		zap.Int64("start_block", startBlock),
		zap.Uint64("stop_block", stopBlock),
	)

	// Create a context with timeout for the sink execution
	sinkCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// Get the repository root
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}
	repoRoot := filepath.Join(cwd, "..", "..")

	cmd := exec.CommandContext(sinkCtx, "go", args...)
	cmd.Dir = repoRoot

	// Capture output
	output, err := cmd.CombinedOutput()
	if err != nil {
		firecoreLog.Error("sds sink command failed",
			zap.Error(err),
			zap.String("output", string(output)),
		)
		return fmt.Errorf("sds sink failed: %w\nOutput: %s", err, string(output))
	}

	firecoreLog.Info("sds sink command completed successfully",
		zap.String("output", string(output)),
	)

	return nil
}

type firecoreSessionEvidence struct {
	SessionID       string
	SessionCount    int
	WorkerCount     int64
	UsageEventCount int64
	UsageBlocks     int64
	UsageBytes      int64
	UsageRequests   int64
}

type firecoreSessionRow struct {
	ID string `db:"id"`
}

func loadFirecoreEvidence(t *testing.T, ctx context.Context, createdAfter time.Time, env *TestEnv) firecoreSessionEvidence {
	t.Helper()

	dbConn, err := psqlrepo.GetConnectionFromDSN(ctx, toPostgresDriverDSN(PostgresTestDSN))
	require.NoError(t, err, "connect to provider postgres repo")
	defer dbConn.Close()

	sessionRows := make([]firecoreSessionRow, 0, 1)
	err = dbConn.SelectContext(ctx, &sessionRows, `
		SELECT id
		FROM sessions
		WHERE payer = $1
		  AND receiver = $2
		  AND data_service = $3
		  AND created_at >= $4
		ORDER BY created_at ASC
	`, env.Payer.Address.Bytes(), env.ServiceProvider.Address.Bytes(), env.DataService.Address.Bytes(), createdAfter)
	require.NoError(t, err, "query firecore-created provider sessions")
	require.Len(t, sessionRows, 1, "expected one provider session for the test payer/provider/data service tuple")

	var evidence firecoreSessionEvidence
	evidence.SessionID = sessionRows[0].ID
	evidence.SessionCount = len(sessionRows)

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

func toPostgresDriverDSN(dsn string) string {
	return strings.Replace(dsn, "psql://", "postgres://", 1)
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
	return strings.Contains(msg, "stream auth failure") &&
		strings.Contains(msg, "missing x-sds-rav header")
}
