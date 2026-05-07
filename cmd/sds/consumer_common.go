package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	sds "github.com/graphprotocol/substreams-data-service"
	chainclient "github.com/graphprotocol/substreams-data-service/contracts/chain"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
	"github.com/streamingfast/eth-go"
)

const (
	defaultRPCTimeout          = 30 * time.Second
	defaultReceiptTimeout      = 2 * time.Minute
	defaultReceiptPollInterval = time.Second
)

type payerKey struct {
	PrivateKey *ecdsa.PrivateKey
	Address    common.Address
	EthKey     *eth.PrivateKey
	EthAddress eth.Address
}

func addRPCFlags(flags *pflag.FlagSet) {
	flags.String("rpc-endpoint", "", "Ethereum JSON-RPC endpoint (required)")
	flags.Duration("rpc-timeout", defaultRPCTimeout, "Per-RPC call timeout")
}

func addTxFlags(flags *pflag.FlagSet) {
	flags.Uint64("chain-id", 0, "Chain ID for transaction signing (required)")
	flags.String("payer-private-key-env", "", "Environment variable containing payer private key")
	flags.String("payer-private-key", "", "Payer private key hex; prefer --payer-private-key-env for shell history safety")
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

func addSignerKeyFlags(flags *pflag.FlagSet) {
	flags.String("signer-private-key-env", "", "Environment variable containing sidecar signer private key")
	flags.String("signer-private-key", "", "Sidecar signer private key hex; prefer --signer-private-key-env")
}

func requiredStringFlag(cmd *cobra.Command, name string) string {
	value := strings.TrimSpace(sflags.MustGetString(cmd, name))
	cli.Ensure(value != "", "--%s is required", name)
	return value
}

func optionalStringFlag(cmd *cobra.Command, name string) string {
	return strings.TrimSpace(sflags.MustGetString(cmd, name))
}

func parseAddressFlag(cmd *cobra.Command, name string) common.Address {
	raw := requiredStringFlag(cmd, name)
	return parseAddressValue(raw, "--"+name)
}

func parseOptionalAddressFlag(cmd *cobra.Command, name string) (common.Address, bool) {
	raw := optionalStringFlag(cmd, name)
	if raw == "" {
		return common.Address{}, false
	}
	return parseAddressValue(raw, "--"+name), true
}

func parseAddressValue(raw string, label string) common.Address {
	cli.Ensure(common.IsHexAddress(raw), "invalid %s address %q", label, raw)
	return common.HexToAddress(raw)
}

func parseGRTFlag(cmd *cobra.Command, name string) sds.GRT {
	raw := requiredStringFlag(cmd, name)
	value, err := sds.ParseGRT(raw)
	cli.NoError(err, "invalid --%s %q, expected GRT amount like %q", name, raw, "10 GRT")
	return value
}

func parseOptionalGRTFlag(cmd *cobra.Command, name string) (sds.GRT, bool) {
	raw := optionalStringFlag(cmd, name)
	if raw == "" {
		return sds.ZeroGRT(), false
	}
	value, err := sds.ParseGRT(raw)
	cli.NoError(err, "invalid --%s %q, expected GRT amount like %q", name, raw, "10 GRT")
	return value, true
}

func parseBigIntFlag(cmd *cobra.Command, name string) *big.Int {
	raw := optionalStringFlag(cmd, name)
	if raw == "" {
		return nil
	}
	value, ok := new(big.Int).SetString(raw, 10)
	cli.Ensure(ok && value.Sign() >= 0, "invalid --%s %q, expected non-negative decimal wei", name, raw)
	return value
}

func parseHexBytesFlag(cmd *cobra.Command, name string) []byte {
	raw := requiredStringFlag(cmd, name)
	value, err := parseHexBytes(raw)
	cli.NoError(err, "invalid --%s %q", name, raw)
	return value
}

func parseHexBytes(raw string) ([]byte, error) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "0x")
	if len(raw)%2 != 0 {
		raw = "0" + raw
	}
	return hex.DecodeString(raw)
}

func parsePayerKey(cmd *cobra.Command) payerKey {
	return parseKeyPair(cmd, "payer")
}

func parseSignerKey(cmd *cobra.Command) payerKey {
	return parseKeyPair(cmd, "signer")
}

func parseKeyPair(cmd *cobra.Command, role string) payerKey {
	keyFlag := role + "-private-key"
	envFlag := role + "-private-key-env"
	direct := optionalStringFlag(cmd, keyFlag)
	envName := optionalStringFlag(cmd, envFlag)
	cli.Ensure((direct == "") != (envName == ""), "exactly one of --%s or --%s is required", keyFlag, envFlag)

	keyHex := direct
	if envName != "" {
		keyHex = strings.TrimSpace(os.Getenv(envName))
		cli.Ensure(keyHex != "", "environment variable %s from --%s is empty or unset", envName, envFlag)
	}

	privateKey, err := parseECDSAPrivateKey(keyHex)
	cli.NoError(err, "invalid --%s private key", role)

	ethKey, err := eth.NewPrivateKey(keyHex)
	cli.NoError(err, "invalid --%s private key for proof generation", role)

	return payerKey{
		PrivateKey: privateKey,
		Address:    chainclient.AddressFromPrivateKey(privateKey),
		EthKey:     ethKey,
		EthAddress: ethKey.PublicKey().Address(),
	}
}

func parseECDSAPrivateKey(raw string) (*ecdsa.PrivateKey, error) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "0x")
	if raw == "" {
		return nil, fmt.Errorf("empty private key")
	}
	keyBytes, err := hex.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	return crypto.ToECDSA(keyBytes)
}

func validateOptionalPayerAddress(cmd *cobra.Command, key payerKey) {
	payerAddress, ok := parseOptionalAddressFlag(cmd, "payer-address")
	if !ok {
		return
	}
	cli.Ensure(payerAddress == key.Address, "--payer-address %s does not match payer private key address %s", payerAddress.Hex(), key.Address.Hex())
}

func txOptionsFromFlags(cmd *cobra.Command, key payerKey) chainclient.TxOptions {
	chainID := sflags.MustGetUint64(cmd, "chain-id")
	cli.Ensure(chainID != 0, "--chain-id is required")
	txType := optionalStringFlag(cmd, "tx-type")
	cli.Ensure(txType == "dynamic" || txType == "legacy", "--tx-type must be either %q or %q", "dynamic", "legacy")

	return chainclient.TxOptions{
		ChainID:              new(big.Int).SetUint64(chainID),
		From:                 key.Address,
		PrivateKey:           key.PrivateKey,
		TxType:               txType,
		GasLimit:             sflags.MustGetUint64(cmd, "gas-limit"),
		MaxFeePerGas:         parseBigIntFlag(cmd, "max-fee-per-gas-wei"),
		MaxPriorityFeePerGas: parseBigIntFlag(cmd, "max-priority-fee-per-gas-wei"),
		GasPrice:             parseBigIntFlag(cmd, "gas-price-wei"),
		ReceiptTimeout:       sflags.MustGetDuration(cmd, "receipt-timeout"),
		ReceiptPollInterval:  sflags.MustGetDuration(cmd, "receipt-poll-interval"),
		NoWait:               sflags.MustGetBool(cmd, "no-wait"),
		DryRun:               sflags.MustGetBool(cmd, "dry-run"),
	}
}

func withRPCClient(cmd *cobra.Command, fn func(ctx context.Context, client *chainclient.Client) error) error {
	return withRPCClientReceiptWaits(cmd, 1, fn)
}

func withRPCClientReceiptWaits(cmd *cobra.Command, receiptWaits int, fn func(ctx context.Context, client *chainclient.Client) error) error {
	endpoint := requiredStringFlag(cmd, "rpc-endpoint")
	rpcTimeout := sflags.MustGetDuration(cmd, "rpc-timeout")
	dialCtx, dialCancel := context.WithTimeout(cmd.Context(), rpcTimeout)
	defer dialCancel()

	client, err := chainclient.DialContext(dialCtx, endpoint)
	if err != nil {
		return err
	}
	defer client.Close()

	timeout := rpcTimeout
	if cmd.Flags().Lookup("receipt-timeout") != nil {
		if receiptWaits < 1 {
			receiptWaits = 1
		}
		timeout += time.Duration(receiptWaits) * sflags.MustGetDuration(cmd, "receipt-timeout")
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()

	return fn(ctx, client)
}

func commonToEthAddress(address common.Address) eth.Address {
	out := make(eth.Address, common.AddressLength)
	copy(out, address.Bytes())
	return out
}

func grtFromBigInt(value *big.Int) sds.GRT {
	return sds.NewGRTFromBigInt(value)
}

func formatGRTLine(label string, value sds.GRT) {
	fmt.Printf("%s: %s (raw: %s)\n", label, value.String(), value.BigInt().String())
}

func formatTxResult(result *chainclient.TxResult) {
	if result.DryRun {
		fmt.Println("dry_run: true")
		fmt.Printf("gas_limit: %d\n", result.GasLimit)
		if result.GasPrice != nil {
			fmt.Printf("gas_price_wei: %s\n", result.GasPrice.String())
			return
		}
		if result.MaxFeePerGas != nil {
			fmt.Printf("max_fee_per_gas_wei: %s\n", result.MaxFeePerGas.String())
		}
		if result.MaxPriorityFeePerGas != nil {
			fmt.Printf("max_priority_fee_per_gas_wei: %s\n", result.MaxPriorityFeePerGas.String())
		}
		return
	}

	fmt.Printf("tx_hash: %s\n", result.Hash.Hex())
	if result.GasPrice != nil {
		fmt.Printf("gas_price_wei: %s\n", result.GasPrice.String())
	}
	if result.Receipt != nil {
		fmt.Printf("receipt_status: %d\n", result.Receipt.Status)
		fmt.Printf("block_number: %s\n", result.Receipt.BlockNumber.String())
	}
}

func parseDeadlineFlag(cmd *cobra.Command, name string) uint64 {
	raw := requiredStringFlag(cmd, name)
	deadline, err := parseDeadline(raw, time.Now())
	cli.NoError(err, "invalid --%s %q, expected duration like 1h, unix timestamp, or RFC3339 time", name, raw)
	return deadline
}

func parseDeadline(raw string, now time.Time) (uint64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("empty deadline")
	}
	if duration, err := time.ParseDuration(raw); err == nil {
		if duration <= 0 {
			return 0, fmt.Errorf("duration must be positive")
		}
		return uint64(now.Add(duration).Unix()), nil
	}
	if unix, err := strconv.ParseUint(raw, 10, 64); err == nil {
		return unix, nil
	}
	timestamp, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return 0, err
	}
	return uint64(timestamp.Unix()), nil
}
