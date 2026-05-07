package main

import (
	"context"
	"fmt"

	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
)

var providerOperatorCollectionsListCmd = Command(
	runProviderOperatorCollectionsList,
	"list",
	"List provider collection lifecycle records",
	Flags(func(flags *pflag.FlagSet) {
		addProviderOperatorFlags(flags)
		addProviderOperatorCollectionFilters(flags)
		flags.Bool("include-rav", false, "Include base64 protobuf signed RAV payloads")
	}),
)

var providerOperatorCollectionsGetCmd = Command(
	runProviderOperatorCollectionsGet,
	"get",
	"Get one provider collection lifecycle record",
	Flags(func(flags *pflag.FlagSet) {
		addProviderOperatorFlags(flags)
		flags.String("session-id", "", "Optional session ID to fetch by exact repository key")
		flags.String("collection-id", "", "Collection ID hex (required)")
		flags.String("payer-address", "", "Payer address (required)")
		flags.String("receiver-address", "", "Receiver/service provider address (required)")
		flags.String("data-service-address", "", "Data service address (required)")
		flags.Bool("include-rav", true, "Include base64 protobuf signed RAV payload")
	}),
)

func runProviderOperatorCollectionsList(cmd *cobra.Command, args []string) error {
	format := providerOperatorFormat(cmd)
	req := &providerv1.ListCollectionsRequest{
		Limit:           sflags.MustGetUint32(cmd, "limit"),
		SessionId:       optionalStringFlag(cmd, "session-id"),
		State:           parseCollectionStateFlag(cmd, "state"),
		Payer:           optionalAddressProtoFlag(cmd, "payer-address"),
		ServiceProvider: optionalAddressProtoFlag(cmd, "receiver-address"),
		DataService:     optionalAddressProtoFlag(cmd, "data-service-address"),
		CollectionId:    optionalCollectionIDFlag(cmd, "collection-id"),
	}
	includeRAV := sflags.MustGetBool(cmd, "include-rav")

	return withProviderOperatorClient(cmd, func(ctx context.Context, client providerOperatorClient) error {
		resp, err := client.client.ListCollections(ctx, providerOperatorRequest(client, req))
		if err != nil {
			return err
		}
		redactCollectionRAVPayloads(resp.Msg.GetCollections(), includeRAV)
		if format == "json" {
			return printProtoJSON(resp.Msg)
		}
		printCollectionRecords(resp.Msg.GetCollections(), includeRAV)
		return nil
	})
}

func runProviderOperatorCollectionsGet(cmd *cobra.Command, args []string) error {
	format := providerOperatorFormat(cmd)
	key := providerOperatorCollectionKeyFromFlags(cmd)
	includeRAV := sflags.MustGetBool(cmd, "include-rav")

	return withProviderOperatorClient(cmd, func(ctx context.Context, client providerOperatorClient) error {
		record, err := fetchProviderOperatorCollection(ctx, client, key)
		if err != nil {
			return err
		}
		redactCollectionRAVPayload(record, includeRAV)
		if format == "json" {
			return printProtoJSON(&providerv1.GetCollectionResponse{Collection: record})
		}
		printCollectionRecord(record, includeRAV)
		return nil
	})
}

func providerOperatorCollectionKeyFromFlags(cmd *cobra.Command) *providerv1.CollectionKey {
	return &providerv1.CollectionKey{
		SessionId:       optionalStringFlag(cmd, "session-id"),
		CollectionId:    requiredCollectionIDFlag(cmd, "collection-id"),
		Payer:           requiredAddressProtoFlag(cmd, "payer-address"),
		ServiceProvider: requiredAddressProtoFlag(cmd, "receiver-address"),
		DataService:     requiredAddressProtoFlag(cmd, "data-service-address"),
	}
}

func fetchProviderOperatorCollection(ctx context.Context, client providerOperatorClient, key *providerv1.CollectionKey) (*providerv1.CollectionRecord, error) {
	if key.GetSessionId() != "" {
		resp, err := client.client.GetCollection(ctx, providerOperatorRequest(client, &providerv1.GetCollectionRequest{Key: key}))
		if err != nil {
			return nil, err
		}
		return resp.Msg.GetCollection(), nil
	}

	resp, err := client.client.ListCollections(ctx, providerOperatorRequest(client, &providerv1.ListCollectionsRequest{
		Limit:           2,
		CollectionId:    key.GetCollectionId(),
		Payer:           key.GetPayer(),
		ServiceProvider: key.GetServiceProvider(),
		DataService:     key.GetDataService(),
	}))
	if err != nil {
		return nil, err
	}
	records := resp.Msg.GetCollections()
	cli.Ensure(len(records) != 0, "collection not found")
	cli.Ensure(len(records) == 1, "collection lookup matched %d records; rerun with --session-id", len(records))
	return records[0], nil
}

func parseCollectionStateFlag(cmd *cobra.Command, name string) providerv1.CollectionState {
	raw := optionalStringFlag(cmd, name)
	switch raw {
	case "":
		return providerv1.CollectionState_COLLECTION_STATE_UNSPECIFIED
	case "collectible":
		return providerv1.CollectionState_COLLECTION_STATE_COLLECTIBLE
	case "collect_pending":
		return providerv1.CollectionState_COLLECTION_STATE_COLLECT_PENDING
	case "collected":
		return providerv1.CollectionState_COLLECTION_STATE_COLLECTED
	case "collect_failed_retryable":
		return providerv1.CollectionState_COLLECTION_STATE_COLLECT_FAILED_RETRYABLE
	default:
		cli.Ensure(false, "--%s must be one of: collectible, collect_pending, collected, collect_failed_retryable", name)
		return providerv1.CollectionState_COLLECTION_STATE_UNSPECIFIED
	}
}

func printCollectionRecords(records []*providerv1.CollectionRecord, includeRAV bool) {
	fmt.Printf("collections_count: %d\n", len(records))
	for i, record := range records {
		if i > 0 {
			fmt.Println()
		}
		printCollectionRecord(record, includeRAV)
	}
}

func printCollectionRecord(record *providerv1.CollectionRecord, includeRAV bool) {
	if record == nil {
		fmt.Println("collection: <nil>")
		return
	}
	key := record.GetKey()
	fmt.Printf("session_id: %s\n", key.GetSessionId())
	fmt.Printf("collection_id: %s\n", formatCollectionID(key.GetCollectionId()))
	fmt.Printf("payer_address: %s\n", formatProtoAddress(key.GetPayer()))
	fmt.Printf("receiver_address: %s\n", formatProtoAddress(key.GetServiceProvider()))
	fmt.Printf("data_service_address: %s\n", formatProtoAddress(key.GetDataService()))
	fmt.Printf("state: %s\n", formatCollectionState(record.GetState()))
	fmt.Printf("value_aggregate: %s\n", formatProtoGRT(record.GetValueAggregate()))
	fmt.Printf("attempt_count: %d\n", record.GetAttemptCount())
	fmt.Printf("last_tx_hash: %s\n", record.GetLastTxHash())
	fmt.Printf("last_error: %s\n", record.GetLastError())
	if record.GetCollectedAmount() != nil {
		fmt.Printf("collected_amount: %s\n", formatProtoGRT(record.GetCollectedAmount()))
	}
	fmt.Printf("created_at: %s\n", formatUnixNano(record.GetCreatedAtNs()))
	fmt.Printf("updated_at: %s\n", formatUnixNano(record.GetUpdatedAtNs()))
	if includeRAV && record.GetSignedRav() != nil {
		fmt.Printf("signed_rav_base64: %s\n", signedRAVBase64(record.GetSignedRav()))
	}
}

func formatCollectionState(state providerv1.CollectionState) string {
	switch state {
	case providerv1.CollectionState_COLLECTION_STATE_COLLECTIBLE:
		return "collectible"
	case providerv1.CollectionState_COLLECTION_STATE_COLLECT_PENDING:
		return "collect_pending"
	case providerv1.CollectionState_COLLECTION_STATE_COLLECTED:
		return "collected"
	case providerv1.CollectionState_COLLECTION_STATE_COLLECT_FAILED_RETRYABLE:
		return "collect_failed_retryable"
	default:
		return "unspecified"
	}
}

func redactCollectionRAVPayloads(records []*providerv1.CollectionRecord, includeRAV bool) {
	for _, record := range records {
		redactCollectionRAVPayload(record, includeRAV)
	}
}

func redactCollectionRAVPayload(record *providerv1.CollectionRecord, includeRAV bool) {
	if includeRAV || record == nil {
		return
	}
	record.SignedRav = nil
}
