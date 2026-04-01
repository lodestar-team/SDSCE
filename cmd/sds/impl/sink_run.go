package impl

import (
	"context"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"time"

	"connectrpc.com/connect"
	sds "github.com/graphprotocol/substreams-data-service"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	consumerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1/consumerv1connect"
	sidecarlib "github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
	"github.com/streamingfast/eth-go"
	"github.com/streamingfast/logging"
	"github.com/streamingfast/substreams/client"
	pbsubstreamsrpc "github.com/streamingfast/substreams/pb/sf/substreams/rpc/v2"
	pbsubstreams "github.com/streamingfast/substreams/pb/sf/substreams/v1"
	"github.com/streamingfast/substreams/sink"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

var sinkLog, sinkTracer = logging.PackageLogger("sink/run", "github.com/graphprotocol/substreams-data-service/cmd/sds@sink/run")

var SinkRunCommand = Command(
	runSinkRun,
	"run [<manifest> [<output_module>]]",
	"Run a data service aware substreams sink",
	Description(`
		Runs a substreams sink that integrates with the Substreams Data Service
		payment system. It wraps the standard substreams sink with:

		1. Payment session initialization with the consumer sidecar
		2. Periodic usage reporting during streaming
		3. Session cleanup on completion

		The sink uses all standard substreams flags (endpoint, cursor, etc.)
		plus additional flags for payment integration.

		DEPRECATED: this command is transitional wrapper scaffolding. The supported
		MVP runtime path is a Substreams client talking directly to the consumer
		sidecar ingress endpoint. Expect this wrapper flow to fail against
		provider-managed runtime sessions.

		By default, runs in development mode. Use --production-mode for production workloads.

		Example:
		  sds sink run ./substreams.spkg map_events \
		    --consumer-sidecar-addr=http://localhost:9002 \
		    --payer-address=0xe90874856c339d5d3733c92ea5acadc6014b34d5 \
		    --receiver-address=0xa6f1845e54b1d6a95319251f1ca775b4ad406cdf \
		    --data-service-address=0x37478fd2f5845e3664fe4155d74c00e1a4e7a5e2
	`),
	RangeArgs(0, 2),
	Flags(func(flags *pflag.FlagSet) {
		// Standard substreams sink flags (exclude development-mode, we use production-mode like substreams run)
		sink.AddFlagsToSet(flags, sink.FlagExcludeDefault(sink.FlagDevelopmentMode))

		// Production mode flag (same as substreams run - defaults to dev mode)
		flags.Bool("production-mode", false, "Enable Production Mode, with high-speed parallel processing")
	}),
)

func runSinkRun(cmd *cobra.Command, args []string) error {
	sinkLog.Warn("sds sink run is deprecated transitional scaffolding; provider-managed sessions use the consumer sidecar ingress endpoint directly")

	manifestPath := "substreams.yaml"
	outputModuleName := sink.InferOutputModuleFromPackage

	if len(args) > 0 {
		manifestPath = args[0]
	}
	if len(args) > 1 {
		outputModuleName = args[1]
	}

	// Payment flags
	consumerSidecarAddr := sflags.MustGetString(cmd, "consumer-sidecar-addr")
	providerControlPlaneEndpoint := sflags.MustGetString(cmd, "provider-control-plane-endpoint")
	payerHex := sflags.MustGetString(cmd, "payer-address")
	receiverHex := sflags.MustGetString(cmd, "receiver-address")
	dataServiceHex := sflags.MustGetString(cmd, "data-service-address")
	reportInterval := sflags.MustGetDuration(cmd, "report-interval")

	cli.Ensure(payerHex != "", "<payer-address> is required")
	payer, err := eth.NewAddress(payerHex)
	cli.NoError(err, "invalid <payer-address> %q", payerHex)

	cli.Ensure(receiverHex != "", "<receiver-address> is required")
	receiver, err := eth.NewAddress(receiverHex)
	cli.NoError(err, "invalid <receiver-address> %q", receiverHex)

	cli.Ensure(dataServiceHex != "", "<data-service-address> is required")
	dataService, err := eth.NewAddress(dataServiceHex)
	cli.NoError(err, "invalid <data-service-address> %q", dataServiceHex)

	// Create sinker config from viper
	sinkerConfig, err := sink.ConfigFromViper(
		cmd,
		sink.IgnoreOutputModuleType,
		manifestPath,
		outputModuleName,
		"sds-sink/"+sds.Version,
		sinkLog,
		sinkTracer,
	)
	cli.NoError(err, "unable to create sinker config")

	// Set mode based on production-mode flag (same as substreams run)
	productionMode := sflags.MustGetBool(cmd, "production-mode")
	if productionMode {
		sinkerConfig.Mode = sink.SubstreamsModeProduction
	} else {
		if sinkerConfig.NoopMode {
			sinkLog.Warn("noop-mode used without production-mode: server will execute in development mode without sending the data...")
		}
		sinkerConfig.Mode = sink.SubstreamsModeDevelopment
	}

	wrapper := newPaymentWrapper(
		consumerSidecarAddr,
		payer,
		receiver,
		dataService,
		providerControlPlaneEndpoint,
		sinkerConfig.Pkg,
		sinkerConfig.Network,
		reportInterval,
		sinkLog,
	)

	app := cli.NewApplication(cmd.Context())

	// Initialize payment session and get the RAV for authentication
	initResult, err := wrapper.init(app.Context())
	if err != nil {
		return fmt.Errorf("failed to initialize payment session: %w", err)
	}

	sinkerConfig.ClientConfig = newClientConfigForDataPlaneEndpoint(sinkerConfig.ClientConfig, initResult.DataPlaneEndpoint)

	var extraHeaders []string

	// Add the RAV header for authentication with the Substreams endpoint
	if initResult.PaymentRAV != nil {
		ravHeader, err := encodeRAVHeader(initResult.PaymentRAV)
		if err != nil {
			return fmt.Errorf("failed to encode RAV header: %w", err)
		}
		extraHeaders = append(extraHeaders, sds.HeaderRAV+":"+ravHeader)
		sinkLog.Debug("added x-sds-rav header to sinker")
	}

	// Add the session ID header for session tracking
	if wrapper.sessionID != "" {
		extraHeaders = append(extraHeaders, sds.HeaderSessionID+":"+wrapper.sessionID)
		sinkLog.Debug("added x-sds-session-id header to sinker", zap.String("session_id", wrapper.sessionID))
	}

	if len(extraHeaders) > 0 {
		sinkerConfig.ExtraHeaders = append(append([]string(nil), sinkerConfig.ExtraHeaders...), extraHeaders...)
		sinkLog.Info("configured SDS data-plane headers",
			zap.Int("header_count", len(sinkerConfig.ExtraHeaders)),
			zap.Strings("headers", sinkerConfig.ExtraHeaders),
		)
	}

	// Create the sinker from config after the provider handshake so the real data-plane
	// endpoint uses the provider-returned session-specific value.
	sinker, err := sink.NewFromConfig(sinkerConfig)
	cli.NoError(err, "unable to create sinker")

	// Supervise the sinker - app will shutdown sinker on termination signal
	app.Supervise(sinker)

	// Set up cleanup when sinker terminates
	sinker.OnTerminated(func(err error) {
		if err != nil {
			sinkLog.Error("sinker terminated with error", zap.Error(err))
		}
		if endErr := wrapper.end(context.Background()); endErr != nil {
			sinkLog.Warn("failed to end payment session cleanly", zap.Error(endErr))
		}
	})

	handlers := sink.NewSinkerHandlers(
		wrapper.wrapBlockHandler(handleBlockScopedData),
		wrapper.wrapUndoHandler(handleBlockUndoSignal),
	)

	go wrapper.runUsageReporter(app.Context())

	// Run sinker in goroutine - it will be supervised by the app
	go sinker.Run(app.Context(), sink.NewBlankCursor(), handlers)

	// Wait for termination (handles SIGINT/SIGTERM)
	return app.WaitForTermination(sinkLog, 0, 30*time.Second)
}

func handleBlockScopedData(ctx context.Context, data *pbsubstreamsrpc.BlockScopedData, isLive *bool, cursor *sink.Cursor) error {
	age := time.Since(data.Clock.Timestamp.AsTime())
	fmt.Fprintf(os.Stderr, "----------- BLOCK #%s (%s) age=%s ---------------\n",
		formatBlockNum(data.Clock.Number),
		data.Clock.Id,
		age,
	)
	return nil
}

func handleBlockUndoSignal(ctx context.Context, undoSignal *pbsubstreamsrpc.BlockUndoSignal, cursor *sink.Cursor) error {
	fmt.Fprintln(os.Stderr, "UNDO:", undoSignal.LastValidBlock)
	return nil
}

// paymentWrapper wraps the sink with payment session management
type paymentWrapper struct {
	sidecarClient                consumerv1connect.ConsumerSidecarServiceClient
	payer                        eth.Address
	receiver                     eth.Address
	dataService                  eth.Address
	providerControlPlaneEndpoint string
	substreamsPackage            *pbsubstreams.Package
	requestedNetwork             string
	reportInterval               time.Duration
	logger                       *zap.Logger

	sessionID      string
	usageTracker   *sds.UsageTracker
	priceConverter sds.PriceConverter
	pricingConfig  *sds.PricingConfig // Provider's pricing config received during init

	// Track latest RAV value for final report
	latestRAVValue sds.GRT
}

func newPaymentWrapper(
	sidecarAddr string,
	payer, receiver, dataService eth.Address,
	providerControlPlaneEndpoint string,
	substreamsPackage *pbsubstreams.Package,
	requestedNetwork string,
	reportInterval time.Duration,
	logger *zap.Logger,
) *paymentWrapper {
	priceConverter := sds.NewStaticPriceConverter(0.15) // Default: 1 GRT = $0.15

	return &paymentWrapper{
		sidecarClient:                consumerv1connect.NewConsumerSidecarServiceClient(http.DefaultClient, sidecarAddr),
		payer:                        payer,
		receiver:                     receiver,
		dataService:                  dataService,
		providerControlPlaneEndpoint: providerControlPlaneEndpoint,
		substreamsPackage:            substreamsPackage,
		requestedNetwork:             requestedNetwork,
		reportInterval:               reportInterval,
		logger:                       logger,
		usageTracker:                 sds.NewUsageTracker(priceConverter),
		priceConverter:               priceConverter,
	}
}

type paymentInitResult struct {
	PaymentRAV        *commonv1.SignedRAV
	DataPlaneEndpoint string
}

func (w *paymentWrapper) init(ctx context.Context) (*paymentInitResult, error) {
	fmt.Fprintln(os.Stderr, "Initializing payment session...")

	resp, err := w.sidecarClient.Init(ctx, connect.NewRequest(&consumerv1.InitRequest{
		EscrowAccount: &commonv1.EscrowAccount{
			Payer:       commonv1.AddressFromEth(w.payer),
			Receiver:    commonv1.AddressFromEth(w.receiver),
			DataService: commonv1.AddressFromEth(w.dataService),
		},
		SubstreamsPackage:            w.substreamsPackage,
		RequestedNetwork:             w.requestedNetwork,
		ProviderControlPlaneEndpoint: w.providerControlPlaneEndpoint,
	}))
	if err != nil {
		return nil, fmt.Errorf("init payment session: %w", err)
	}

	if resp.Msg.GetDataPlaneEndpoint() == "" {
		return nil, fmt.Errorf("provider returned an empty data-plane endpoint")
	}

	w.sessionID = resp.Msg.Session.SessionId
	fmt.Fprintf(os.Stderr, "Session initialized: %s\n", w.sessionID)
	fmt.Fprintf(os.Stderr, "Data plane endpoint: %s\n", resp.Msg.GetDataPlaneEndpoint())

	// Extract pricing config from session info
	if resp.Msg.Session.PricingConfig != nil {
		w.pricingConfig = resp.Msg.Session.PricingConfig.ToNative()
		fmt.Fprintf(os.Stderr, "Pricing: %s/block, %s/byte\n",
			w.pricingConfig.PricePerBlock.String(),
			w.pricingConfig.PricePerByte.String())
	}

	if resp.Msg.PaymentRav != nil {
		fmt.Fprintf(os.Stderr, "Initial RAV value: %s\n", formatRAVValue(resp.Msg.PaymentRav))
	}
	fmt.Fprintln(os.Stderr)

	return &paymentInitResult{
		PaymentRAV:        resp.Msg.PaymentRav,
		DataPlaneEndpoint: resp.Msg.GetDataPlaneEndpoint(),
	}, nil
}

func newClientConfigForDataPlaneEndpoint(existing *client.SubstreamsClientConfig, dataPlaneEndpoint string) *client.SubstreamsClientConfig {
	parsedEndpoint := sidecarlib.ParseEndpoint(dataPlaneEndpoint)
	return client.NewSubstreamsClientConfig(client.SubstreamsClientConfigOptions{
		Endpoint:             parsedEndpoint.URL,
		AuthToken:            existing.AuthToken(),
		AuthType:             existing.AuthType(),
		Insecure:             parsedEndpoint.Insecure,
		PlainText:            parsedEndpoint.Plaintext,
		Agent:                existing.Agent(),
		ForceProtocolVersion: existing.ForceProtocolVersion(),
	})
}

func (w *paymentWrapper) end(ctx context.Context) error {
	if w.sessionID == "" {
		return nil
	}

	w.logger.Info("ending payment session")

	// Get final usage totals
	blocksReceived, blocksProcessed, bytes, reqs := w.usageTracker.GetTotalUsage()

	// Note: Cost is not calculated here - the provider is cost-authoritative.
	// The consumer sidecar will need to get pricing info from the provider.
	resp, err := w.sidecarClient.EndSession(ctx, connect.NewRequest(&consumerv1.EndSessionRequest{
		SessionId: w.sessionID,
		FinalUsage: &commonv1.Usage{
			BlocksProcessed:  blocksProcessed,
			BytesTransferred: bytes,
			Requests:         reqs,
			Cost:             commonv1.GRTFromNative(sds.ZeroGRT()),
		},
	}))

	// Clear session ID to prevent duplicate calls
	w.sessionID = ""

	if err != nil {
		return err
	}

	var finalRAVValue sds.GRT
	if resp.Msg.FinalRav != nil && resp.Msg.FinalRav.Rav != nil && resp.Msg.FinalRav.Rav.ValueAggregate != nil {
		finalRAVValue = resp.Msg.FinalRav.Rav.ValueAggregate.ToNative()
	} else if !w.latestRAVValue.IsZero() {
		finalRAVValue = w.latestRAVValue
	}

	PrintUsageReport(UsageReport{
		BlocksReceived:   blocksReceived,
		BlocksProcessed:  blocksProcessed,
		BytesTransferred: bytes,
		Requests:         reqs,
	}, finalRAVValue, w.pricingConfig, w.priceConverter)

	return nil
}

func (w *paymentWrapper) runUsageReporter(ctx context.Context) {
	ticker := time.NewTicker(w.reportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.reportUsage(ctx); err != nil {
				w.logger.Warn("failed to report usage", zap.Error(err))
			}
		}
	}
}

func (w *paymentWrapper) reportUsage(ctx context.Context) error {
	_, blocksProcessed, bytes, reqs := w.usageTracker.SwapAndGetUsage()

	if blocksProcessed == 0 && bytes == 0 {
		return nil // Nothing to report
	}

	// Note: Cost is not calculated here - the provider is cost-authoritative.
	// The consumer sidecar will need to get pricing info from the provider
	// to properly calculate costs. For now, we send usage metrics only.
	resp, err := w.sidecarClient.ReportUsage(ctx, connect.NewRequest(&consumerv1.ReportUsageRequest{
		SessionId: w.sessionID,
		Usage: &commonv1.Usage{
			BlocksProcessed:  blocksProcessed,
			BytesTransferred: bytes,
			Requests:         reqs,
			Cost:             commonv1.GRTFromNative(sds.ZeroGRT()),
		},
	}))
	if err != nil {
		return err
	}

	// Track latest RAV value for final report
	if resp.Msg.UpdatedRav != nil && resp.Msg.UpdatedRav.Rav != nil && resp.Msg.UpdatedRav.Rav.ValueAggregate != nil {
		w.latestRAVValue = resp.Msg.UpdatedRav.Rav.ValueAggregate.ToNative()
	}

	if !resp.Msg.ShouldContinue {
		w.logger.Warn("sidecar requested stop", zap.String("reason", resp.Msg.StopReason))
	}

	return nil
}

// wrapBlockHandler wraps a block handler with usage tracking
func (w *paymentWrapper) wrapBlockHandler(
	handler func(context.Context, *pbsubstreamsrpc.BlockScopedData, *bool, *sink.Cursor) error,
) func(context.Context, *pbsubstreamsrpc.BlockScopedData, *bool, *sink.Cursor) error {
	return func(ctx context.Context, data *pbsubstreamsrpc.BlockScopedData, isLive *bool, cursor *sink.Cursor) error {
		// Track usage
		var dataBytes uint64
		if data.Output != nil && data.Output.MapOutput != nil {
			dataBytes = uint64(len(data.Output.MapOutput.Value))
		}
		w.usageTracker.AddBlock(dataBytes)

		return handler(ctx, data, isLive, cursor)
	}
}

// wrapUndoHandler wraps an undo handler (no usage tracking needed for undos)
func (w *paymentWrapper) wrapUndoHandler(
	handler func(context.Context, *pbsubstreamsrpc.BlockUndoSignal, *sink.Cursor) error,
) func(context.Context, *pbsubstreamsrpc.BlockUndoSignal, *sink.Cursor) error {
	return handler
}

// formatBlockNum formats a block number with thousand separators
func formatBlockNum(num uint64) string {
	s := fmt.Sprintf("%d", num)
	n := len(s)
	if n <= 3 {
		return s
	}

	var result []byte
	for i, c := range s {
		if i > 0 && (n-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

// formatRAVValue formats the RAV value for display
func formatRAVValue(rav *commonv1.SignedRAV) string {
	if rav == nil || rav.Rav == nil || rav.Rav.ValueAggregate == nil {
		return "0 GRT"
	}
	value := rav.Rav.ValueAggregate.ToNative()
	return value.String()
}

// encodeRAVHeader encodes a SignedRAV as a base64 string for the x-sds-rav header
func encodeRAVHeader(signedRAV *commonv1.SignedRAV) (string, error) {
	protoBytes, err := proto.Marshal(signedRAV)
	if err != nil {
		return "", fmt.Errorf("marshal proto: %w", err)
	}
	return base64.StdEncoding.EncodeToString(protoBytes), nil
}

// UsageReport represents a usage report snapshot
type UsageReport struct {
	BlocksReceived   uint64 // All blocks received from stream
	BlocksProcessed  uint64 // Blocks with actual output data
	BytesTransferred uint64
	Requests         uint64
	CostGRT          *big.Int
}

// PrintUsageReport prints the usage report to stderr
func PrintUsageReport(report UsageReport, ravValue sds.GRT, pricingConfig *sds.PricingConfig, priceConverter sds.PriceConverter) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "📊 Usage Report")
	fmt.Fprintf(os.Stderr, " • Egress Bytes (uncompressed): %s\n", formatBytes(report.BytesTransferred))
	fmt.Fprintf(os.Stderr, " • Processed Blocks: %d blocks\n", report.BlocksProcessed)
	fmt.Fprintf(os.Stderr, " • Received Blocks: %d blocks\n", report.BlocksReceived)

	// Calculate and show cost
	var cost sds.GRT
	if pricingConfig != nil {
		cost = pricingConfig.CalculateUsageCost(report.BlocksProcessed, report.BytesTransferred)
	} else {
		cost = ravValue
	}

	if priceConverter != nil && !cost.IsZero() {
		fiat := priceConverter.ToFiat(cost.BigInt())
		fmt.Fprintf(os.Stderr, " • Cost: %s (%s%.4f)\n", cost.String(), priceConverter.Symbol(), fiat)
	} else {
		fmt.Fprintf(os.Stderr, " • Cost: %s\n", cost.String())
	}
}

// formatBytes formats bytes into human-readable format
func formatBytes(bytes uint64) string {
	const (
		KiB = 1024
		MiB = KiB * 1024
		GiB = MiB * 1024
		TiB = GiB * 1024
	)

	switch {
	case bytes >= TiB:
		return fmt.Sprintf("%.2f TiB", float64(bytes)/TiB)
	case bytes >= GiB:
		return fmt.Sprintf("%.2f GiB", float64(bytes)/GiB)
	case bytes >= MiB:
		return fmt.Sprintf("%.2f MiB", float64(bytes)/MiB)
	case bytes >= KiB:
		return fmt.Sprintf("%.1f KiB", float64(bytes)/KiB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
