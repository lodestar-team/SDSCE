package main

import (
	"context"
	"encoding/base64"
	"fmt"

	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
	"google.golang.org/protobuf/proto"
)

var providerOperatorRAVsListCmd = Command(
	runProviderOperatorRAVsList,
	"list",
	"List accepted provider RAVs",
	Flags(func(flags *pflag.FlagSet) {
		addProviderOperatorFlags(flags)
		addProviderOperatorRAVFilters(flags)
		flags.Bool("include-rav", false, "Include base64 protobuf signed RAV payloads")
	}),
)

var providerOperatorRAVsGetCmd = Command(
	runProviderOperatorRAVsGet,
	"get",
	"Get the accepted RAV for one session",
	Flags(func(flags *pflag.FlagSet) {
		addProviderOperatorFlags(flags)
		flags.String("session-id", "", "Session ID (required)")
		flags.Bool("include-rav", true, "Include base64 protobuf signed RAV payload")
	}),
)

func runProviderOperatorRAVsList(cmd *cobra.Command, args []string) error {
	format := providerOperatorFormat(cmd)
	req := &providerv1.ListAcceptedRAVsRequest{
		Limit:           sflags.MustGetUint32(cmd, "limit"),
		SessionId:       optionalStringFlag(cmd, "session-id"),
		CollectionId:    optionalCollectionIDFlag(cmd, "collection-id"),
		Payer:           optionalAddressProtoFlag(cmd, "payer-address"),
		ServiceProvider: optionalAddressProtoFlag(cmd, "receiver-address"),
		DataService:     optionalAddressProtoFlag(cmd, "data-service-address"),
	}
	includeRAV := sflags.MustGetBool(cmd, "include-rav")

	return withProviderOperatorClient(cmd, func(ctx context.Context, client providerOperatorClient) error {
		resp, err := client.client.ListAcceptedRAVs(ctx, providerOperatorRequest(client, req))
		if err != nil {
			return err
		}
		redactAcceptedRAVPayloads(resp.Msg.GetRavs(), includeRAV)
		if format == "json" {
			return printProtoJSON(resp.Msg)
		}
		printAcceptedRAVs(resp.Msg.GetRavs(), includeRAV)
		return nil
	})
}

func runProviderOperatorRAVsGet(cmd *cobra.Command, args []string) error {
	format := providerOperatorFormat(cmd)
	req := &providerv1.GetAcceptedRAVRequest{
		SessionId: requiredStringFlag(cmd, "session-id"),
	}
	includeRAV := sflags.MustGetBool(cmd, "include-rav")

	return withProviderOperatorClient(cmd, func(ctx context.Context, client providerOperatorClient) error {
		resp, err := client.client.GetAcceptedRAV(ctx, providerOperatorRequest(client, req))
		if err != nil {
			return err
		}
		redactAcceptedRAVPayload(resp.Msg.GetRav(), includeRAV)
		if format == "json" {
			return printProtoJSON(resp.Msg)
		}
		printAcceptedRAV(resp.Msg.GetRav(), includeRAV)
		return nil
	})
}

func printAcceptedRAVs(ravs []*providerv1.AcceptedRAV, includeRAV bool) {
	fmt.Printf("ravs_count: %d\n", len(ravs))
	for i, rav := range ravs {
		if i > 0 {
			fmt.Println()
		}
		printAcceptedRAV(rav, includeRAV)
	}
}

func printAcceptedRAV(rav *providerv1.AcceptedRAV, includeRAV bool) {
	printAcceptedRAVIndented(rav, "")
	if includeRAV && rav != nil && rav.GetSignedRav() != nil {
		fmt.Printf("signed_rav_base64: %s\n", signedRAVBase64(rav.GetSignedRav()))
	}
}

func printAcceptedRAVIndented(rav *providerv1.AcceptedRAV, indent string) {
	if rav == nil {
		fmt.Printf("%srav: <nil>\n", indent)
		return
	}
	fmt.Printf("%ssession_id: %s\n", indent, rav.GetSessionId())
	fmt.Printf("%scollection_id: %s\n", indent, formatCollectionID(rav.GetCollectionId()))
	fmt.Printf("%spayer_address: %s\n", indent, formatProtoAddress(rav.GetPayer()))
	fmt.Printf("%sreceiver_address: %s\n", indent, formatProtoAddress(rav.GetServiceProvider()))
	fmt.Printf("%sdata_service_address: %s\n", indent, formatProtoAddress(rav.GetDataService()))
	fmt.Printf("%stimestamp_ns: %d\n", indent, rav.GetTimestampNs())
	fmt.Printf("%svalue_aggregate: %s\n", indent, formatProtoGRT(rav.GetValueAggregate()))
	fmt.Printf("%scollection_state: %s\n", indent, formatCollectionState(rav.GetCollectionState()))
}

func signedRAVBase64(rav *commonv1.SignedRAV) string {
	if rav == nil {
		return ""
	}
	data, err := proto.Marshal(rav)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

func redactAcceptedRAVPayloads(ravs []*providerv1.AcceptedRAV, includeRAV bool) {
	for _, rav := range ravs {
		redactAcceptedRAVPayload(rav, includeRAV)
	}
}

func redactAcceptedRAVPayload(rav *providerv1.AcceptedRAV, includeRAV bool) {
	if includeRAV || rav == nil {
		return
	}
	rav.SignedRav = nil
}
