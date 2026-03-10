package main

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	consumerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1/consumerv1connect"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1/providerv1connect"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
	"github.com/streamingfast/eth-go"
)

var demoFlowCmd = Command(
	runDemoFlow,
	"flow",
	"Run an end-to-end sidecar demo flow against already-running sidecars",
	Description(`
		Runs a single end-to-end demo of the sidecar protocol:
		- consumer Init (which also starts a provider gateway session)
		- consumer ReportUsage loop (which triggers PaymentSession rav_request/rav_submission)
		- consumer EndSession (closes PaymentSession; provider becomes inactive)

		This command does not start devenv or sidecars; run those separately.
	`),
	Flags(func(flags *pflag.FlagSet) {
		flags.String("consumer-sidecar-addr", "http://localhost:9002", "Consumer sidecar address")
		flags.String("provider-sidecar-addr", "http://localhost:9001", "Provider gateway address (used for status checks)")
		flags.String("provider-endpoint", "http://localhost:9001", "Provider gateway endpoint to pass to consumer Init (PaymentGatewayService)")

		flags.String("payer-address", "", "Payer address (required)")
		flags.String("receiver-address", "", "Receiver/service provider address (required)")
		flags.String("data-service-address", "", "Data service contract address (required)")

		flags.Uint64("blocks-to-simulate", 10, "Total blocks to simulate")
		flags.Uint64("bytes-per-block", 0, "Bytes per block to report (for metering)")
		flags.Uint64("batch-size", 1, "Blocks per ReportUsage call")
		flags.Duration("delay-between-batches", 200*time.Millisecond, "Delay between usage reports")
	}),
)

func runDemoFlow(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	consumerSidecarAddr := strings.TrimSpace(sflags.MustGetString(cmd, "consumer-sidecar-addr"))
	providerSidecarAddr := strings.TrimSpace(sflags.MustGetString(cmd, "provider-sidecar-addr"))
	providerEndpoint := strings.TrimSpace(sflags.MustGetString(cmd, "provider-endpoint"))

	payerHex := sflags.MustGetString(cmd, "payer-address")
	receiverHex := sflags.MustGetString(cmd, "receiver-address")
	dataServiceHex := sflags.MustGetString(cmd, "data-service-address")

	totalBlocks := sflags.MustGetUint64(cmd, "blocks-to-simulate")
	bytesPerBlock := sflags.MustGetUint64(cmd, "bytes-per-block")
	batchSize := sflags.MustGetUint64(cmd, "batch-size")
	delayBetweenBatches := sflags.MustGetDuration(cmd, "delay-between-batches")

	cli.Ensure(consumerSidecarAddr != "", "<consumer-sidecar-addr> is required")
	cli.Ensure(providerSidecarAddr != "", "<provider-sidecar-addr> is required")
	cli.Ensure(providerEndpoint != "", "<provider-endpoint> is required")

	cli.Ensure(payerHex != "", "<payer-address> is required")
	payer, err := eth.NewAddress(payerHex)
	cli.NoError(err, "invalid <payer-address> %q", payerHex)

	cli.Ensure(receiverHex != "", "<receiver-address> is required")
	receiver, err := eth.NewAddress(receiverHex)
	cli.NoError(err, "invalid <receiver-address> %q", receiverHex)

	cli.Ensure(dataServiceHex != "", "<data-service-address> is required")
	dataService, err := eth.NewAddress(dataServiceHex)
	cli.NoError(err, "invalid <data-service-address> %q", dataServiceHex)

	cli.Ensure(batchSize > 0, "<batch-size> must be > 0")

	consumerClient := consumerv1connect.NewConsumerSidecarServiceClient(http.DefaultClient, consumerSidecarAddr)
	providerClient := providerv1connect.NewPaymentGatewayServiceClient(http.DefaultClient, providerSidecarAddr)

	fmt.Printf("Step 1: Init\n")
	initResp, err := consumerClient.Init(ctx, connect.NewRequest(&consumerv1.InitRequest{
		EscrowAccount: &commonv1.EscrowAccount{
			Payer:       commonv1.AddressFromEth(payer),
			Receiver:    commonv1.AddressFromEth(receiver),
			DataService: commonv1.AddressFromEth(dataService),
		},
		GatewayEndpoint: providerEndpoint,
	}))
	cli.NoError(err, "consumer Init failed")

	sessionID := strings.TrimSpace(initResp.Msg.GetSession().GetSessionId())
	cli.Ensure(sessionID != "", "consumer Init returned an empty session_id")

	fmt.Printf("  session_id: %s\n", sessionID)

	fmt.Printf("\nStep 2: ReportUsage loop\n")
	var totalBlocksSent uint64
	for totalBlocksSent < totalBlocks {
		batch := batchSize
		if totalBlocksSent+batch > totalBlocks {
			batch = totalBlocks - totalBlocksSent
		}

		bytes := batch * bytesPerBlock

		resp, err := consumerClient.ReportUsage(ctx, connect.NewRequest(&consumerv1.ReportUsageRequest{
			SessionId: sessionID,
			Usage: &commonv1.Usage{
				BlocksProcessed:  batch,
				BytesTransferred: bytes,
				Requests:         1,
				Cost:             nil, // provider computes cost and requests RAVs over PaymentSession
			},
		}))
		cli.NoError(err, "consumer ReportUsage failed")

		totalBlocksSent += batch
		if resp.Msg.GetUpdatedRav() != nil && resp.Msg.GetUpdatedRav().GetRav() != nil {
			v := resp.Msg.GetUpdatedRav().GetRav().GetValueAggregate().ToBigInt()
			fmt.Printf("  blocks=%d total=%d updated_rav_value=%s\n", batch, totalBlocksSent, v.String())
		} else {
			fmt.Printf("  blocks=%d total=%d\n", batch, totalBlocksSent)
		}

		if !resp.Msg.GetShouldContinue() {
			fmt.Printf("  STOP: %s\n", resp.Msg.GetStopReason())
			break
		}

		if delayBetweenBatches > 0 && totalBlocksSent < totalBlocks {
			time.Sleep(delayBetweenBatches)
		}
	}

	fmt.Printf("\nStep 3: Provider GetSessionStatus\n")
	statusResp, err := providerClient.GetSessionStatus(ctx, connect.NewRequest(&providerv1.GetSessionStatusRequest{
		SessionId: sessionID,
	}))
	cli.NoError(err, "provider GetSessionStatus failed")

	cur := big.NewInt(0)
	if ps := statusResp.Msg.GetPaymentStatus(); ps != nil && ps.GetCurrentRavValue() != nil {
		cur = ps.GetCurrentRavValue().ToBigInt()
	}
	fmt.Printf("  provider_current_rav_value=%s\n", cur.String())

	fmt.Printf("\nStep 4: EndSession\n")
	endResp, err := consumerClient.EndSession(ctx, connect.NewRequest(&consumerv1.EndSessionRequest{
		SessionId:  sessionID,
		FinalUsage: nil,
	}))
	cli.NoError(err, "consumer EndSession failed")

	final := big.NewInt(0)
	if endResp.Msg.GetFinalRav() != nil && endResp.Msg.GetFinalRav().GetRav() != nil {
		final = endResp.Msg.GetFinalRav().GetRav().GetValueAggregate().ToBigInt()
	}
	fmt.Printf("  final_rav_value=%s\n", final.String())

	fmt.Printf("\nStep 5: Provider becomes inactive\n")
	inactive, err := waitProviderInactive(ctx, providerClient, sessionID, 2*time.Second)
	cli.NoError(err, "waiting for provider inactive")
	cli.Ensure(inactive, "provider session still active after EndSession")
	fmt.Printf("  provider_active=false\n")

	return nil
}

func waitProviderInactive(ctx context.Context, providerClient providerv1connect.PaymentGatewayServiceClient, sessionID string, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := providerClient.GetSessionStatus(ctx, connect.NewRequest(&providerv1.GetSessionStatusRequest{
			SessionId: sessionID,
		}))
		if err == nil && !resp.Msg.GetActive() {
			return true, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false, nil
}
