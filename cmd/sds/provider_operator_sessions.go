package main

import (
	"context"
	"fmt"
	"time"

	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
)

var providerOperatorSessionsListCmd = Command(
	runProviderOperatorSessionsList,
	"list",
	"List provider payment sessions",
	Flags(func(flags *pflag.FlagSet) {
		addProviderOperatorFlags(flags)
		addProviderOperatorSessionFilters(flags)
	}),
)

var providerOperatorSessionsGetCmd = Command(
	runProviderOperatorSessionsGet,
	"get",
	"Get one provider payment session",
	Flags(func(flags *pflag.FlagSet) {
		addProviderOperatorFlags(flags)
		flags.String("session-id", "", "Session ID (required)")
		flags.Bool("include-rav", true, "Include accepted RAV summary")
	}),
)

func runProviderOperatorSessionsList(cmd *cobra.Command, args []string) error {
	format := providerOperatorFormat(cmd)
	req := &providerv1.ListSessionsRequest{
		Limit:       sflags.MustGetUint32(cmd, "limit"),
		Payer:       optionalAddressProtoFlag(cmd, "payer-address"),
		Receiver:    optionalAddressProtoFlag(cmd, "receiver-address"),
		DataService: optionalAddressProtoFlag(cmd, "data-service-address"),
		Status:      parseOperatorSessionStatusFlag(cmd, "status"),
		FundsStatus: parseOperatorFundsStatusFlag(cmd, "funds-status"),
		IncludeRav:  sflags.MustGetBool(cmd, "include-rav"),
	}

	return withProviderOperatorClient(cmd, func(ctx context.Context, client providerOperatorClient) error {
		resp, err := client.client.ListSessions(ctx, providerOperatorRequest(client, req))
		if err != nil {
			return err
		}
		if format == "json" {
			return printProtoJSON(resp.Msg)
		}
		printOperatorSessions(resp.Msg.GetSessions())
		return nil
	})
}

func runProviderOperatorSessionsGet(cmd *cobra.Command, args []string) error {
	format := providerOperatorFormat(cmd)
	sessionID := requiredStringFlag(cmd, "session-id")
	req := &providerv1.GetSessionRequest{
		SessionId:  sessionID,
		IncludeRav: sflags.MustGetBool(cmd, "include-rav"),
	}

	return withProviderOperatorClient(cmd, func(ctx context.Context, client providerOperatorClient) error {
		resp, err := client.client.GetSession(ctx, providerOperatorRequest(client, req))
		if err != nil {
			return err
		}
		if format == "json" {
			return printProtoJSON(resp.Msg)
		}
		printOperatorSession(resp.Msg.GetSession())
		return nil
	})
}

func parseOperatorSessionStatusFlag(cmd *cobra.Command, name string) providerv1.OperatorSessionStatus {
	raw := optionalStringFlag(cmd, name)
	switch raw {
	case "":
		return providerv1.OperatorSessionStatus_OPERATOR_SESSION_STATUS_UNSPECIFIED
	case "active":
		return providerv1.OperatorSessionStatus_OPERATOR_SESSION_STATUS_ACTIVE
	case "paused":
		return providerv1.OperatorSessionStatus_OPERATOR_SESSION_STATUS_PAUSED
	case "terminated":
		return providerv1.OperatorSessionStatus_OPERATOR_SESSION_STATUS_TERMINATED
	default:
		cli.Ensure(false, "--%s must be one of: active, paused, terminated", name)
		return providerv1.OperatorSessionStatus_OPERATOR_SESSION_STATUS_UNSPECIFIED
	}
}

func parseOperatorFundsStatusFlag(cmd *cobra.Command, name string) providerv1.OperatorFundsStatus {
	raw := optionalStringFlag(cmd, name)
	switch raw {
	case "":
		return providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_UNSPECIFIED
	case "ok":
		return providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_OK
	case "insufficient":
		return providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_INSUFFICIENT
	case "unknown":
		return providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_UNKNOWN
	default:
		cli.Ensure(false, "--%s must be one of: ok, insufficient, unknown", name)
		return providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_UNSPECIFIED
	}
}

func printOperatorSessions(sessions []*providerv1.OperatorSession) {
	fmt.Printf("sessions_count: %d\n", len(sessions))
	for i, session := range sessions {
		if i > 0 {
			fmt.Println()
		}
		printOperatorSession(session)
	}
}

func printOperatorSession(session *providerv1.OperatorSession) {
	if session == nil {
		fmt.Println("session: <nil>")
		return
	}
	fmt.Printf("session_id: %s\n", session.GetSessionId())
	fmt.Printf("status: %s\n", formatOperatorSessionStatus(session.GetStatus()))
	fmt.Printf("payer_address: %s\n", formatProtoAddress(session.GetPayer()))
	fmt.Printf("receiver_address: %s\n", formatProtoAddress(session.GetReceiver()))
	fmt.Printf("data_service_address: %s\n", formatProtoAddress(session.GetDataService()))
	fmt.Printf("created_at: %s\n", formatUnixNano(session.GetCreatedAtNs()))
	fmt.Printf("updated_at: %s\n", formatUnixNano(session.GetUpdatedAtNs()))
	if session.GetEndedAtNs() != 0 {
		fmt.Printf("ended_at: %s\n", formatUnixNano(session.GetEndedAtNs()))
	}
	fmt.Printf("end_reason: %s\n", session.GetEndReason().String())
	fmt.Printf("payment_control_pending: %t\n", session.GetPaymentControlPending())
	printOperatorPaymentState(session.GetPaymentState())
	printUsage("accumulated", session.GetAccumulatedUsage())
	printUsage("baseline", session.GetBaselineUsage())
	if session.GetAcceptedRav() != nil {
		fmt.Println("accepted_rav:")
		printAcceptedRAVIndented(session.GetAcceptedRav(), "  ")
	}
}

func printOperatorPaymentState(state *providerv1.OperatorPaymentState) {
	if state == nil {
		return
	}
	fmt.Printf("funds_status: %s\n", formatOperatorFundsStatus(state.GetFundsStatus()))
	if paymentStatus := state.GetPaymentStatus(); paymentStatus != nil {
		fmt.Printf("funds_sufficient: %t\n", paymentStatus.GetFundsSufficient())
		fmt.Printf("current_rav_value: %s\n", formatProtoGRT(paymentStatus.GetCurrentRavValue()))
		fmt.Printf("accumulated_usage_value: %s\n", formatProtoGRT(paymentStatus.GetAccumulatedUsageValue()))
		if paymentStatus.GetEscrowBalance() != nil {
			fmt.Printf("escrow_balance: %s\n", formatProtoGRT(paymentStatus.GetEscrowBalance()))
		}
		if paymentStatus.GetEstimatedBlocksRemaining() != 0 {
			fmt.Printf("estimated_blocks_remaining: %d\n", paymentStatus.GetEstimatedBlocksRemaining())
		}
	}
	fmt.Printf("current_outstanding: %s\n", formatProtoGRT(state.GetCurrentOutstanding()))
	fmt.Printf("projected_outstanding: %s\n", formatProtoGRT(state.GetProjectedOutstanding()))
	if state.GetMinimumNeeded() != nil {
		fmt.Printf("minimum_needed: %s\n", formatProtoGRT(state.GetMinimumNeeded()))
	}
	if state.GetFundsCheckError() != "" {
		fmt.Printf("funds_check_error: %s\n", state.GetFundsCheckError())
	}
	if state.GetOperatorHint() != "" {
		fmt.Printf("operator_hint: %s\n", state.GetOperatorHint())
	}
}

func formatOperatorSessionStatus(status providerv1.OperatorSessionStatus) string {
	switch status {
	case providerv1.OperatorSessionStatus_OPERATOR_SESSION_STATUS_ACTIVE:
		return "active"
	case providerv1.OperatorSessionStatus_OPERATOR_SESSION_STATUS_PAUSED:
		return "paused"
	case providerv1.OperatorSessionStatus_OPERATOR_SESSION_STATUS_TERMINATED:
		return "terminated"
	default:
		return "unspecified"
	}
}

func formatOperatorFundsStatus(status providerv1.OperatorFundsStatus) string {
	switch status {
	case providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_OK:
		return "ok"
	case providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_INSUFFICIENT:
		return "insufficient"
	case providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_UNKNOWN:
		return "unknown"
	default:
		return "unspecified"
	}
}

func formatUnixNano(ns uint64) string {
	if ns == 0 {
		return ""
	}
	return time.Unix(0, int64(ns)).UTC().Format(time.RFC3339Nano)
}
