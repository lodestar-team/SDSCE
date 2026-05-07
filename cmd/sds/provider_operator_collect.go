package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	chainclient "github.com/graphprotocol/substreams-data-service/contracts/chain"
	horizoncontracts "github.com/graphprotocol/substreams-data-service/contracts/horizon"
	"github.com/graphprotocol/substreams-data-service/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
	"github.com/streamingfast/eth-go"
)

const maxDataServiceCutPPM = 1_000_000

var providerOperatorCollectCmd = Command(
	runProviderOperatorCollect,
	"collect",
	"Manually collect one provider settlement",
	Flags(func(flags *pflag.FlagSet) {
		addProviderOperatorFlags(flags)
		addRPCFlags(flags)
		addProviderCollectTxFlags(flags)
		flags.String("session-id", "", "Optional session ID to fetch by exact repository key")
		flags.String("collection-id", "", "Collection ID hex (required)")
		flags.String("payer-address", "", "Payer address (required)")
		flags.String("receiver-address", "", "Receiver/service provider address (required)")
		flags.String("data-service-address", "", "SubstreamsDataService contract address (required)")
		flags.Uint64("data-service-cut-ppm", 0, "Data service cut in parts per million (required, 0 to 1000000)")
	}),
)

func addProviderCollectTxFlags(flags *pflag.FlagSet) {
	flags.Uint64("chain-id", 0, "Chain ID for transaction signing (required)")
	flags.String("provider-private-key-env", "", "Environment variable containing provider private key")
	flags.String("provider-private-key", "", "Provider private key hex; prefer --provider-private-key-env for shell history safety")
	flags.String("tx-type", "dynamic", "Transaction type: dynamic or legacy")
	flags.Uint64("gas-limit", 0, "Gas limit override; defaults to eth_estimateGas")
	flags.String("max-fee-per-gas-wei", "", "EIP-1559 max fee per gas in wei; defaults to 2*baseFee + priority fee")
	flags.String("max-priority-fee-per-gas-wei", "", "EIP-1559 max priority fee per gas in wei; defaults to eth_maxPriorityFeePerGas")
	flags.String("gas-price-wei", "", "Legacy transaction gas price in wei; defaults to eth_gasPrice")
	flags.Duration("receipt-timeout", defaultReceiptTimeout, "Receipt wait timeout")
	flags.Duration("receipt-poll-interval", defaultReceiptPollInterval, "Receipt polling interval")
	flags.Bool("dry-run", false, "Estimate gas and fees without submitting a transaction")
	flags.Bool("no-wait", false, "Return after transaction submission without waiting for a receipt")
}

func runProviderOperatorCollect(cmd *cobra.Command, args []string) error {
	format := providerOperatorFormat(cmd)
	key := providerOperatorCollectionKeyFromFlags(cmd)
	dataServiceAddress := parseAddressFlag(cmd, "data-service-address")
	dataServiceCutPPM := parseDataServiceCutPPM(cmd)
	dryRun := sflags.MustGetBool(cmd, "dry-run")
	noWait := sflags.MustGetBool(cmd, "no-wait")
	cli.Ensure(!(dryRun && noWait), "--dry-run and --no-wait cannot be used together")

	operatorClient := providerOperatorClientFromFlags(cmd)
	fetchCtx, fetchCancel := providerOperatorContext(cmd)
	record, err := fetchProviderOperatorCollection(fetchCtx, operatorClient, key)
	fetchCancel()
	if err != nil {
		return err
	}

	if record.GetState() == providerv1.CollectionState_COLLECTION_STATE_COLLECTED {
		output := newProviderCollectOutput(record, dataServiceCutPPM, nil)
		output.FinalState = formatCollectionState(providerv1.CollectionState_COLLECTION_STATE_COLLECTED)
		output.FollowUp = "collection is already collected; no transaction submitted"
		return output.print(format)
	}

	providerKey := parseKeyPair(cmd, "provider")
	txOpts := txOptionsFromFlags(cmd, providerKey)
	signedRAV, err := validateProviderCollectRecord(record, providerKey.Address, dataServiceAddress)
	cli.NoError(err, "invalid provider collection record")

	dataService := horizoncontracts.MustNewDataService()
	calldata, err := dataService.PackQueryFeeCollect(signedRAV, dataServiceCutPPM)
	if err != nil {
		return fmt.Errorf("encoding SubstreamsDataService.collect calldata: %w", err)
	}

	output := newProviderCollectOutput(record, dataServiceCutPPM, calldata)
	return withRPCClientReceiptWaits(cmd, 1, func(ctx context.Context, rpcClient *chainclient.Client) error {
		if !dryRun {
			pendingCtx, pendingCancel := providerOperatorContext(cmd)
			pending, err := markCollectionPending(pendingCtx, operatorClient, record)
			pendingCancel()
			if err != nil {
				return err
			}
			output.PendingState = formatCollectionState(pending.GetState())
		}

		result, err := rpcClient.SendDynamicFeeTransaction(ctx, dataServiceAddress, big.NewInt(0), calldata, txOpts)
		output.setTxResult(result)
		if err != nil {
			if !dryRun {
				retryCtx, retryCancel := providerOperatorContext(cmd)
				retryErr := markCollectionRetryable(retryCtx, operatorClient, record, resultTxHash(result), err)
				retryCancel()
				if retryErr != nil {
					return fmt.Errorf("%w; additionally failed to mark collection retryable: %v", err, retryErr)
				}
				output.FinalState = formatCollectionState(providerv1.CollectionState_COLLECTION_STATE_COLLECT_FAILED_RETRYABLE)
				output.LastError = err.Error()
			}
			return err
		}

		switch {
		case dryRun:
			output.FinalState = formatCollectionState(record.GetState())
		case noWait:
			output.NoWait = true
			output.FinalState = formatCollectionState(providerv1.CollectionState_COLLECTION_STATE_COLLECT_PENDING)
			output.FollowUp = "transaction submitted; wait for the receipt and mark the collection collected or retryable"
		default:
			collectedCtx, collectedCancel := providerOperatorContext(cmd)
			collected, err := markCollectionCollected(collectedCtx, operatorClient, record, resultTxHash(result))
			collectedCancel()
			if err != nil {
				return err
			}
			output.FinalState = formatCollectionState(collected.GetState())
		}

		return output.print(format)
	})
}

func parseDataServiceCutPPM(cmd *cobra.Command) uint64 {
	cli.Ensure(cmd.Flags().Changed("data-service-cut-ppm"), "--data-service-cut-ppm is required")
	value := sflags.MustGetUint64(cmd, "data-service-cut-ppm")
	cli.Ensure(value <= maxDataServiceCutPPM, "--data-service-cut-ppm must be between 0 and %d", maxDataServiceCutPPM)
	return value
}

func validateProviderCollectRecord(record *providerv1.CollectionRecord, providerAddress common.Address, dataServiceAddress common.Address) (*horizon.SignedRAV, error) {
	if record == nil {
		return nil, fmt.Errorf("collection record is required")
	}
	if !isProviderCollectableState(record.GetState()) {
		return nil, fmt.Errorf("collection state is %s, expected collectible or collect_failed_retryable", formatCollectionState(record.GetState()))
	}
	if record.GetSignedRav() == nil {
		return nil, fmt.Errorf("signed RAV is required")
	}

	signedRAV, err := sidecar.ProtoSignedRAVToHorizon(record.GetSignedRav())
	if err != nil {
		return nil, fmt.Errorf("signed RAV: %w", err)
	}
	if record.GetValueAggregate() == nil {
		return nil, fmt.Errorf("value aggregate is required")
	}
	if record.GetValueAggregate().ToBigInt().Cmp(signedRAV.Message.ValueAggregate) != 0 {
		return nil, fmt.Errorf("record value aggregate does not match signed RAV value aggregate")
	}
	if err := validateCollectionKeyMatchesRAV(record.GetKey(), signedRAV); err != nil {
		return nil, err
	}

	ravProvider := ethAddressToCommonAddress(signedRAV.Message.ServiceProvider)
	if providerAddress != ravProvider {
		return nil, fmt.Errorf("provider private key address %s does not match RAV service provider %s", providerAddress.Hex(), ravProvider.Hex())
	}
	ravDataService := ethAddressToCommonAddress(signedRAV.Message.DataService)
	if dataServiceAddress != ravDataService {
		return nil, fmt.Errorf("--data-service-address %s does not match RAV data service %s", dataServiceAddress.Hex(), ravDataService.Hex())
	}

	return signedRAV, nil
}

func isProviderCollectableState(state providerv1.CollectionState) bool {
	return state == providerv1.CollectionState_COLLECTION_STATE_COLLECTIBLE ||
		state == providerv1.CollectionState_COLLECTION_STATE_COLLECT_FAILED_RETRYABLE
}

func validateCollectionKeyMatchesRAV(key *providerv1.CollectionKey, signedRAV *horizon.SignedRAV) error {
	if key == nil {
		return fmt.Errorf("collection key is required")
	}
	rav := signedRAV.Message
	if hex.EncodeToString(key.GetCollectionId()) != hex.EncodeToString(rav.CollectionID[:]) {
		return fmt.Errorf("collection key collection ID does not match signed RAV collection ID")
	}
	if err := protoAddressMatchesEth(key.GetPayer(), rav.Payer, "payer"); err != nil {
		return err
	}
	if err := protoAddressMatchesEth(key.GetServiceProvider(), rav.ServiceProvider, "service provider"); err != nil {
		return err
	}
	if err := protoAddressMatchesEth(key.GetDataService(), rav.DataService, "data service"); err != nil {
		return err
	}
	return nil
}

func protoAddressMatchesEth(protoAddr *commonv1.Address, ethAddr eth.Address, field string) error {
	got, err := protoAddr.ToEth()
	if err != nil {
		return fmt.Errorf("collection key %s: %w", field, err)
	}
	if !sidecar.AddressesEqual(got, ethAddr) {
		return fmt.Errorf("collection key %s %s does not match signed RAV %s %s", field, got.Pretty(), field, ethAddr.Pretty())
	}
	return nil
}

func markCollectionPending(ctx context.Context, client providerOperatorClient, record *providerv1.CollectionRecord) (*providerv1.CollectionRecord, error) {
	resp, err := client.client.MarkCollectionPending(ctx, providerOperatorRequest(client, &providerv1.MarkCollectionPendingRequest{
		Key:           record.GetKey(),
		ExpectedValue: record.GetValueAggregate(),
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetCollection(), nil
}

func markCollectionCollected(ctx context.Context, client providerOperatorClient, record *providerv1.CollectionRecord, txHash string) (*providerv1.CollectionRecord, error) {
	resp, err := client.client.MarkCollectionCollected(ctx, providerOperatorRequest(client, &providerv1.MarkCollectionCollectedRequest{
		Key:           record.GetKey(),
		ExpectedValue: record.GetValueAggregate(),
		TxHash:        txHash,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetCollection(), nil
}

func markCollectionRetryable(ctx context.Context, client providerOperatorClient, record *providerv1.CollectionRecord, txHash string, cause error) error {
	_, err := client.client.MarkCollectionRetryable(ctx, providerOperatorRequest(client, &providerv1.MarkCollectionRetryableRequest{
		Key:           record.GetKey(),
		ExpectedValue: record.GetValueAggregate(),
		TxHash:        txHash,
		LastError:     cause.Error(),
	}))
	return err
}

func resultTxHash(result *chainclient.TxResult) string {
	if result == nil || result.Hash == (common.Hash{}) {
		return ""
	}
	return result.Hash.Hex()
}

func ethAddressToCommonAddress(address eth.Address) common.Address {
	return common.BytesToAddress(address)
}

type providerCollectOutput struct {
	SessionID          string `json:"session_id,omitempty"`
	CollectionID       string `json:"collection_id,omitempty"`
	PayerAddress       string `json:"payer_address,omitempty"`
	ReceiverAddress    string `json:"receiver_address,omitempty"`
	DataServiceAddress string `json:"data_service_address,omitempty"`
	ValueAggregate     string `json:"value_aggregate,omitempty"`
	ValueAggregateRaw  string `json:"value_aggregate_raw,omitempty"`
	StateBefore        string `json:"state_before,omitempty"`
	PendingState       string `json:"pending_state,omitempty"`
	FinalState         string `json:"final_state,omitempty"`
	DataServiceCutPPM  uint64 `json:"data_service_cut_ppm"`
	Calldata           string `json:"calldata,omitempty"`
	DryRun             bool   `json:"dry_run,omitempty"`
	NoWait             bool   `json:"no_wait,omitempty"`
	TxHash             string `json:"tx_hash,omitempty"`
	GasLimit           uint64 `json:"gas_limit,omitempty"`
	GasPriceWei        string `json:"gas_price_wei,omitempty"`
	MaxFeePerGasWei    string `json:"max_fee_per_gas_wei,omitempty"`
	PriorityFeeWei     string `json:"max_priority_fee_per_gas_wei,omitempty"`
	ReceiptStatus      uint64 `json:"receipt_status,omitempty"`
	BlockNumber        string `json:"block_number,omitempty"`
	LastError          string `json:"last_error,omitempty"`
	FollowUp           string `json:"follow_up,omitempty"`
}

func newProviderCollectOutput(record *providerv1.CollectionRecord, dataServiceCutPPM uint64, calldata []byte) *providerCollectOutput {
	out := &providerCollectOutput{
		SessionID:          record.GetKey().GetSessionId(),
		CollectionID:       formatCollectionID(record.GetKey().GetCollectionId()),
		PayerAddress:       formatProtoAddress(record.GetKey().GetPayer()),
		ReceiverAddress:    formatProtoAddress(record.GetKey().GetServiceProvider()),
		DataServiceAddress: formatProtoAddress(record.GetKey().GetDataService()),
		StateBefore:        formatCollectionState(record.GetState()),
		FinalState:         formatCollectionState(record.GetState()),
		DataServiceCutPPM:  dataServiceCutPPM,
	}
	if len(calldata) != 0 {
		out.Calldata = "0x" + hex.EncodeToString(calldata)
	}
	if value := record.GetValueAggregate(); value != nil {
		grt := value.ToNative()
		out.ValueAggregate = grt.String()
		out.ValueAggregateRaw = grt.BigInt().String()
	}
	return out
}

func (o *providerCollectOutput) setTxResult(result *chainclient.TxResult) {
	if result == nil {
		return
	}
	o.DryRun = result.DryRun
	o.TxHash = resultTxHash(result)
	o.GasLimit = result.GasLimit
	if result.GasPrice != nil {
		o.GasPriceWei = result.GasPrice.String()
	}
	if result.MaxFeePerGas != nil {
		o.MaxFeePerGasWei = result.MaxFeePerGas.String()
	}
	if result.MaxPriorityFeePerGas != nil {
		o.PriorityFeeWei = result.MaxPriorityFeePerGas.String()
	}
	if result.Receipt != nil {
		o.ReceiptStatus = result.Receipt.Status
		o.BlockNumber = result.Receipt.BlockNumber.String()
	}
}

func (o *providerCollectOutput) print(format string) error {
	if format == "json" {
		out, err := json.MarshalIndent(o, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}

	fmt.Printf("session_id: %s\n", o.SessionID)
	fmt.Printf("collection_id: %s\n", o.CollectionID)
	fmt.Printf("payer_address: %s\n", o.PayerAddress)
	fmt.Printf("receiver_address: %s\n", o.ReceiverAddress)
	fmt.Printf("data_service_address: %s\n", o.DataServiceAddress)
	fmt.Printf("value_aggregate: %s (raw: %s)\n", o.ValueAggregate, o.ValueAggregateRaw)
	fmt.Printf("state_before: %s\n", o.StateBefore)
	if o.PendingState != "" {
		fmt.Printf("pending_state: %s\n", o.PendingState)
	}
	fmt.Printf("final_state: %s\n", o.FinalState)
	fmt.Printf("data_service_cut_ppm: %d\n", o.DataServiceCutPPM)
	if o.Calldata != "" {
		fmt.Printf("calldata: %s\n", o.Calldata)
	}
	if o.DryRun {
		fmt.Println("dry_run: true")
	}
	if o.TxHash != "" {
		fmt.Printf("tx_hash: %s\n", o.TxHash)
	}
	if o.GasLimit != 0 {
		fmt.Printf("gas_limit: %d\n", o.GasLimit)
	}
	if o.GasPriceWei != "" {
		fmt.Printf("gas_price_wei: %s\n", o.GasPriceWei)
	}
	if o.MaxFeePerGasWei != "" {
		fmt.Printf("max_fee_per_gas_wei: %s\n", o.MaxFeePerGasWei)
	}
	if o.PriorityFeeWei != "" {
		fmt.Printf("max_priority_fee_per_gas_wei: %s\n", o.PriorityFeeWei)
	}
	if o.BlockNumber != "" {
		fmt.Printf("receipt_status: %d\n", o.ReceiptStatus)
		fmt.Printf("block_number: %s\n", o.BlockNumber)
	}
	if o.FollowUp != "" {
		fmt.Printf("follow_up: %s\n", o.FollowUp)
	}
	return nil
}
