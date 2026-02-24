package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"time"

	"connectrpc.com/connect"
	sds "github.com/graphprotocol/substreams-data-service"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	consumerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1/consumerv1connect"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
	"github.com/streamingfast/eth-go"
	"github.com/streamingfast/substreams/client"
	pbsubstreamsrpc "github.com/streamingfast/substreams/pb/sf/substreams/rpc/v2"
	"github.com/streamingfast/substreams/sink"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

var sinkLog, sinkTracer = zlog.Named("sink"), tracer

var sinkRunCmd = Command(
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
	gatewayEndpoint := sflags.MustGetString(cmd, "gateway-endpoint")
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

	// Create the sinker from config
	sinker, err := sink.NewFromConfig(sinkerConfig)
	cli.NoError(err, "unable to create sinker")

	wrapper := newPaymentWrapper(
		consumerSidecarAddr,
		payer,
		receiver,
		dataService,
		gatewayEndpoint,
		sinkerConfig.ClientConfig,
		reportInterval,
		sinkLog,
	)

	app := cli.NewApplication(cmd.Context())

	// Initialize payment session and get the RAV for authentication
	paymentRAV, err := wrapper.init(app.Context())
	if err != nil {
		return fmt.Errorf("failed to initialize payment session: %w", err)
	}

	// Add the RAV header for authentication with the Substreams endpoint
	if paymentRAV != nil {
		ravHeader, err := encodeRAVHeader(paymentRAV)
		if err != nil {
			return fmt.Errorf("failed to encode RAV header: %w", err)
		}
		sinker.ExtraHeaders = append(sinker.ExtraHeaders, "x-sds-rav:"+ravHeader)
		sinkLog.Debug("added x-sds-rav header to sinker")
	}

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
	sidecarClient      consumerv1connect.ConsumerSidecarServiceClient
	payer              eth.Address
	receiver           eth.Address
	dataService        eth.Address
	gatewayEndpoint    string // Provider gateway for payment session management
	substreamsEndpoint string // Substreams endpoint for data streaming
	reportInterval     time.Duration
	logger             *zap.Logger

	sessionID      string
	usageTracker   *UsageTracker
	priceConverter PriceConverter
	pricingConfig  *sds.PricingConfig // Provider's pricing config received during init

	// Track latest RAV value for final report
	latestRAVValue sds.GRT
}

func newPaymentWrapper(
	sidecarAddr string,
	payer, receiver, dataService eth.Address,
	gatewayEndpoint string,
	clientConfig *client.SubstreamsClientConfig,
	reportInterval time.Duration,
	logger *zap.Logger,
) *paymentWrapper {
	// Build substreams endpoint URL from client config
	scheme := "https"
	if clientConfig.PlainText() {
		scheme = "http"
	}
	substreamsEndpoint := fmt.Sprintf("%s://%s", scheme, clientConfig.Endpoint())
	if clientConfig.Insecure() {
		substreamsEndpoint += "?insecure=true"
	}

	// If gateway endpoint is not specified, default to substreams endpoint
	if gatewayEndpoint == "" {
		gatewayEndpoint = substreamsEndpoint
	}

	priceConverter := NewStaticPriceConverter(0.15) // Default: 1 GRT = $0.15

	return &paymentWrapper{
		sidecarClient:      consumerv1connect.NewConsumerSidecarServiceClient(http.DefaultClient, sidecarAddr),
		payer:              payer,
		receiver:           receiver,
		dataService:        dataService,
		gatewayEndpoint:    gatewayEndpoint,
		substreamsEndpoint: substreamsEndpoint,
		reportInterval:     reportInterval,
		logger:             logger,
		usageTracker:       NewUsageTracker(priceConverter),
		priceConverter:     priceConverter,
	}
}

func (w *paymentWrapper) init(ctx context.Context) (*commonv1.SignedRAV, error) {
	fmt.Fprintln(os.Stderr, "Initializing payment session...")

	resp, err := w.sidecarClient.Init(ctx, connect.NewRequest(&consumerv1.InitRequest{
		EscrowAccount: &commonv1.EscrowAccount{
			Payer:       commonv1.AddressFromEth(w.payer),
			Receiver:    commonv1.AddressFromEth(w.receiver),
			DataService: commonv1.AddressFromEth(w.dataService),
		},
		GatewayEndpoint:    w.gatewayEndpoint,
		SubstreamsEndpoint: w.substreamsEndpoint,
	}))
	if err != nil {
		return nil, fmt.Errorf("init payment session: %w", err)
	}

	w.sessionID = resp.Msg.Session.SessionId
	fmt.Fprintf(os.Stderr, "Session initialized: %s\n", w.sessionID)

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

	return resp.Msg.PaymentRav, nil
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
