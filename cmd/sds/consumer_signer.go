package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	chainclient "github.com/graphprotocol/substreams-data-service/contracts/chain"
	horizoncontracts "github.com/graphprotocol/substreams-data-service/contracts/horizon"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
)

var consumerSignerCmd = Group(
	"signer",
	"Consumer sidecar signer authorization commands",
	consumerSignerProofCmd,
	consumerSignerStatusCmd,
	consumerSignerAuthorizeCmd,
	consumerSignerThawCmd,
	consumerSignerRevokeCmd,
	consumerSignerCancelThawCmd,
)

var consumerSignerProofCmd = Command(
	runConsumerSignerProof,
	"proof",
	"Generate an offline sidecar signer authorization proof",
	Flags(func(flags *pflag.FlagSet) {
		flags.Uint64("chain-id", 0, "Chain ID for proof generation (required)")
		flags.String("collector-address", "", "GraphTallyCollector contract address (required)")
		flags.String("payer-address", "", "Payer/authorizer address (required)")
		addSignerKeyFlags(flags)
		flags.String("deadline", "", "Proof deadline as duration, unix timestamp, or RFC3339 time (required)")
	}),
)

func runConsumerSignerProof(cmd *cobra.Command, args []string) error {
	chainID := sflags.MustGetUint64(cmd, "chain-id")
	cli.Ensure(chainID != 0, "--chain-id is required")
	collectorAddress := parseAddressFlag(cmd, "collector-address")
	payerAddress := parseAddressFlag(cmd, "payer-address")
	signerKey := parseSignerKey(cmd)
	deadline := parseDeadlineFlag(cmd, "deadline")

	proof, err := horizoncontracts.GenerateSignerProof(chainID, commonToEthAddress(collectorAddress), deadline, commonToEthAddress(payerAddress), signerKey.EthKey)
	if err != nil {
		return err
	}

	fmt.Printf("signer_address: %s\n", signerKey.Address.Hex())
	fmt.Printf("payer_address: %s\n", payerAddress.Hex())
	fmt.Printf("collector_address: %s\n", collectorAddress.Hex())
	fmt.Printf("proof_deadline: %d\n", deadline)
	fmt.Printf("proof_deadline_time: %s\n", time.Unix(int64(deadline), 0).UTC().Format(time.RFC3339))
	fmt.Printf("proof: 0x%s\n", hex.EncodeToString(proof))
	return nil
}

var consumerSignerStatusCmd = Command(
	runConsumerSignerStatus,
	"status",
	"Show signer authorization and thaw status",
	Flags(func(flags *pflag.FlagSet) {
		addRPCFlags(flags)
		flags.String("collector-address", "", "GraphTallyCollector contract address (required)")
		flags.String("payer-address", "", "Payer/authorizer address (required)")
		flags.String("signer-address", "", "Sidecar signer address (required)")
	}),
)

func runConsumerSignerStatus(cmd *cobra.Command, args []string) error {
	collectorAddress := parseAddressFlag(cmd, "collector-address")
	payerAddress := parseAddressFlag(cmd, "payer-address")
	signerAddress := parseAddressFlag(cmd, "signer-address")

	return withRPCClient(cmd, func(ctx context.Context, client *chainclient.Client) error {
		collector := horizoncontracts.MustNewCollector()
		authorized, err := querySignerAuthorized(ctx, client, collector, collectorAddress, payerAddress, signerAddress)
		if err != nil {
			return err
		}
		thawEnd, err := querySignerThawEnd(ctx, client, collector, collectorAddress, signerAddress)
		if err != nil {
			return err
		}

		fmt.Printf("payer_address: %s\n", payerAddress.Hex())
		fmt.Printf("signer_address: %s\n", signerAddress.Hex())
		fmt.Printf("collector_address: %s\n", collectorAddress.Hex())
		fmt.Printf("authorized: %t\n", authorized)
		printThawEnd(thawEnd)
		return nil
	})
}

var consumerSignerAuthorizeCmd = Command(
	runConsumerSignerAuthorize,
	"authorize",
	"Authorize a sidecar signer for the payer",
	Flags(func(flags *pflag.FlagSet) {
		addRPCFlags(flags)
		addTxFlags(flags)
		addSignerKeyFlags(flags)
		flags.String("collector-address", "", "GraphTallyCollector contract address (required)")
		flags.String("payer-address", "", "Optional payer address; must match payer key when supplied")
		flags.String("signer-address", "", "Sidecar signer address for externally generated proof")
		flags.String("proof", "", "Externally generated signer proof hex")
		flags.Uint64("proof-deadline", 0, "Proof deadline unix timestamp for externally generated proof")
		flags.String("deadline", "", "Convenience proof deadline as duration, unix timestamp, or RFC3339 time")
	}),
)

func runConsumerSignerAuthorize(cmd *cobra.Command, args []string) error {
	collectorAddress := parseAddressFlag(cmd, "collector-address")
	payerKey := parsePayerKey(cmd)
	validateOptionalPayerAddress(cmd, payerKey)
	opts := txOptionsFromFlags(cmd, payerKey)

	signerAddressRaw := optionalStringFlag(cmd, "signer-address")
	proofRaw := optionalStringFlag(cmd, "proof")
	proofDeadlineFlag := sflags.MustGetUint64(cmd, "proof-deadline")
	deadlineRaw := optionalStringFlag(cmd, "deadline")
	signerKeyDirect := optionalStringFlag(cmd, "signer-private-key")
	signerKeyEnv := optionalStringFlag(cmd, "signer-private-key-env")

	externalMode := signerAddressRaw != "" || proofRaw != "" || proofDeadlineFlag != 0
	localMode := signerKeyDirect != "" || signerKeyEnv != "" || deadlineRaw != ""
	cli.Ensure(externalMode != localMode, "use either external proof mode (--signer-address, --proof, --proof-deadline) or local proof mode (--signer-private-key/--signer-private-key-env, --deadline)")

	var signerAddress common.Address
	var proof []byte
	var proofDeadline uint64

	if externalMode {
		cli.Ensure(signerAddressRaw != "", "--signer-address is required in external proof mode")
		cli.Ensure(proofRaw != "", "--proof is required in external proof mode")
		cli.Ensure(proofDeadlineFlag != 0, "--proof-deadline is required in external proof mode")
		signerAddress = parseAddressValue(signerAddressRaw, "--signer-address")
		proof = parseHexBytesFlag(cmd, "proof")
		proofDeadline = proofDeadlineFlag
	} else {
		cli.Ensure(deadlineRaw != "", "--deadline is required in local proof mode")
		signerKey := parseSignerKey(cmd)
		signerAddress = signerKey.Address
		proofDeadline = parseDeadlineFlag(cmd, "deadline")
		var err error
		proof, err = horizoncontracts.GenerateSignerProof(opts.ChainID.Uint64(), commonToEthAddress(collectorAddress), proofDeadline, payerKey.EthAddress, signerKey.EthKey)
		if err != nil {
			return err
		}
	}

	return withRPCClient(cmd, func(ctx context.Context, client *chainclient.Client) error {
		collector := horizoncontracts.MustNewCollector()
		data, err := collector.PackAuthorizeSigner(signerAddress, new(big.Int).SetUint64(proofDeadline), proof)
		if err != nil {
			return err
		}
		result, err := client.SendDynamicFeeTransaction(ctx, collectorAddress, big.NewInt(0), data, opts)
		if err != nil {
			return err
		}

		fmt.Printf("payer_address: %s\n", payerKey.Address.Hex())
		fmt.Printf("signer_address: %s\n", signerAddress.Hex())
		fmt.Printf("proof_deadline: %d\n", proofDeadline)
		formatTxResult(result)

		if opts.NoWait || opts.DryRun {
			return nil
		}

		authorized, err := querySignerAuthorized(ctx, client, collector, collectorAddress, payerKey.Address, signerAddress)
		if err != nil {
			return err
		}
		fmt.Printf("authorized: %t\n", authorized)
		return nil
	})
}

var consumerSignerThawCmd = Command(
	runConsumerSignerThaw,
	"thaw",
	"Start thawing a sidecar signer before revocation",
	Flags(func(flags *pflag.FlagSet) {
		addRPCFlags(flags)
		addTxFlags(flags)
		flags.String("collector-address", "", "GraphTallyCollector contract address (required)")
		flags.String("payer-address", "", "Optional payer address; must match payer key when supplied")
		flags.String("signer-address", "", "Sidecar signer address (required)")
	}),
)

func runConsumerSignerThaw(cmd *cobra.Command, args []string) error {
	return runSignerTx(cmd, "thawSigner", func(collector *horizoncontracts.Collector, signer common.Address) ([]byte, error) {
		return collector.PackThawSigner(signer)
	}, func(ctx context.Context, client *chainclient.Client, collector *horizoncontracts.Collector, collectorAddress common.Address, payer common.Address, signer common.Address) error {
		thawEnd, err := querySignerThawEnd(ctx, client, collector, collectorAddress, signer)
		if err != nil {
			return err
		}
		printThawEnd(thawEnd)
		return nil
	})
}

var consumerSignerRevokeCmd = Command(
	runConsumerSignerRevoke,
	"revoke",
	"Revoke a thawed sidecar signer",
	Flags(func(flags *pflag.FlagSet) {
		addRPCFlags(flags)
		addTxFlags(flags)
		flags.String("collector-address", "", "GraphTallyCollector contract address (required)")
		flags.String("payer-address", "", "Optional payer address; must match payer key when supplied")
		flags.String("signer-address", "", "Sidecar signer address (required)")
	}),
)

func runConsumerSignerRevoke(cmd *cobra.Command, args []string) error {
	return runSignerTx(cmd, "revokeAuthorizedSigner", func(collector *horizoncontracts.Collector, signer common.Address) ([]byte, error) {
		return collector.PackRevokeAuthorizedSigner(signer)
	}, func(ctx context.Context, client *chainclient.Client, collector *horizoncontracts.Collector, collectorAddress common.Address, payer common.Address, signer common.Address) error {
		authorized, err := querySignerAuthorized(ctx, client, collector, collectorAddress, payer, signer)
		if err != nil {
			return err
		}
		fmt.Printf("authorized: %t\n", authorized)
		return nil
	})
}

var consumerSignerCancelThawCmd = Command(
	runConsumerSignerCancelThaw,
	"cancel-thaw",
	"Cancel a pending sidecar signer thaw",
	Flags(func(flags *pflag.FlagSet) {
		addRPCFlags(flags)
		addTxFlags(flags)
		flags.String("collector-address", "", "GraphTallyCollector contract address (required)")
		flags.String("payer-address", "", "Optional payer address; must match payer key when supplied")
		flags.String("signer-address", "", "Sidecar signer address (required)")
	}),
)

func runConsumerSignerCancelThaw(cmd *cobra.Command, args []string) error {
	return runSignerTx(cmd, "cancelThawSigner", func(collector *horizoncontracts.Collector, signer common.Address) ([]byte, error) {
		return collector.PackCancelThawSigner(signer)
	}, func(ctx context.Context, client *chainclient.Client, collector *horizoncontracts.Collector, collectorAddress common.Address, payer common.Address, signer common.Address) error {
		thawEnd, err := querySignerThawEnd(ctx, client, collector, collectorAddress, signer)
		if err != nil {
			return err
		}
		printThawEnd(thawEnd)
		return nil
	})
}

func runSignerTx(
	cmd *cobra.Command,
	action string,
	pack func(collector *horizoncontracts.Collector, signer common.Address) ([]byte, error),
	afterWait func(context.Context, *chainclient.Client, *horizoncontracts.Collector, common.Address, common.Address, common.Address) error,
) error {
	collectorAddress := parseAddressFlag(cmd, "collector-address")
	signerAddress := parseAddressFlag(cmd, "signer-address")
	payerKey := parsePayerKey(cmd)
	validateOptionalPayerAddress(cmd, payerKey)
	opts := txOptionsFromFlags(cmd, payerKey)

	return withRPCClient(cmd, func(ctx context.Context, client *chainclient.Client) error {
		collector := horizoncontracts.MustNewCollector()
		data, err := pack(collector, signerAddress)
		if err != nil {
			return err
		}
		result, err := client.SendDynamicFeeTransaction(ctx, collectorAddress, big.NewInt(0), data, opts)
		if err != nil {
			return err
		}

		fmt.Printf("action: %s\n", action)
		fmt.Printf("payer_address: %s\n", payerKey.Address.Hex())
		fmt.Printf("signer_address: %s\n", signerAddress.Hex())
		formatTxResult(result)

		if opts.NoWait || opts.DryRun {
			return nil
		}
		return afterWait(ctx, client, collector, collectorAddress, payerKey.Address, signerAddress)
	})
}

func querySignerAuthorized(ctx context.Context, client *chainclient.Client, collector *horizoncontracts.Collector, collectorAddress common.Address, payer common.Address, signer common.Address) (bool, error) {
	data, err := collector.PackIsAuthorized(payer, signer)
	if err != nil {
		return false, err
	}
	result, err := client.CallContract(ctx, collectorAddress, data)
	if err != nil {
		return false, err
	}
	return collector.UnpackIsAuthorized(result)
}

func querySignerThawEnd(ctx context.Context, client *chainclient.Client, collector *horizoncontracts.Collector, collectorAddress common.Address, signer common.Address) (*big.Int, error) {
	data, err := collector.PackGetThawEnd(signer)
	if err != nil {
		return nil, err
	}
	result, err := client.CallContract(ctx, collectorAddress, data)
	if err != nil {
		return nil, err
	}
	return collector.UnpackGetThawEnd(result)
}

func printThawEnd(thawEnd *big.Int) {
	fmt.Printf("thaw_end: %s\n", thawEnd.String())
	if thawEnd.Sign() > 0 && thawEnd.IsInt64() {
		fmt.Printf("thaw_end_time: %s\n", time.Unix(thawEnd.Int64(), 0).UTC().Format(time.RFC3339))
	}
}
