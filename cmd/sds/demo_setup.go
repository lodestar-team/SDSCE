package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	sds "github.com/graphprotocol/substreams-data-service"
	"github.com/graphprotocol/substreams-data-service/contracts/artifacts"
	"github.com/graphprotocol/substreams-data-service/horizon/devenv"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
	"github.com/streamingfast/eth-go"
	"github.com/streamingfast/eth-go/rpc"
)

var demoSetupCmd = Command(
	runDemoSetup,
	"setup",
	"Prepare on-chain demo state for a running `sds devenv`",
	Description(`
		Connects to an already-running local devenv chain and prepares the on-chain state
		needed to run a manual sidecar demo:
		- mint/approve/deposit escrow funds
		- set provision + register service provider
		- authorize a signer on-chain

		This is intended for local development only.
	`),
	Flags(func(flags *pflag.FlagSet) {
		flags.String("rpc-endpoint", "http://localhost:58545", "Ethereum RPC endpoint (from `sds devenv` output)")
		flags.Uint64("chain-id", 1337, "Chain ID for the local devenv chain")

		// Contract addresses (defaults match current deterministic devenv).
		flags.String("collector-address", "0x1d01649b4f94722b55b5c3b3e10fe26cd90c1ba9", "GraphTallyCollector contract address")
		flags.String("escrow-address", "0xfc7487a37ca8eac2e64cba61277aa109e9b8631e", "PaymentsEscrow contract address")
		flags.String("data-service-address", "0x37478fd2f5845e3664fe4155d74c00e1a4e7a5e2", "SubstreamsDataService contract address")
		flags.String("staking-address", "0x32f01bc7a55d437b7a8354621a9486b9be08a3bb", "MockStaking contract address")
		flags.String("grt-token-address", "0xfa7a048544f86c11206afd89b40bc987e464cb58", "MockGRTToken contract address")

		// Keys (defaults match current deterministic devenv).
		flags.String("deployer-private-key", "0x1aa5d8f9a42ba0b9439c7034d24e93619f67af22a9ab15be9e4ce7eadddb5143", "Deployer private key (hex)")
		flags.String("payer-private-key", "0xe4c2694501255921b6588519cfd36d4e86ddc4ce19ab1bc91d9c58057c040304", "Payer private key (hex)")
		flags.String("service-provider-private-key", "0x41942233cf1d78b6e3262f1806f8da36aafa24a941031aad8e056a1d34640f8d", "Service provider private key (hex)")

		flags.String("signer-private-key", "", "Signer private key to authorize (hex). If empty, a new key is generated and printed")
		flags.String("env-file", "devel/.demo.env", "Write an env file for `.reflex.stack` (set to empty to disable)")

		flags.String("escrow-amount-grt", "10000", "Escrow amount to deposit in GRT (decimal, e.g. 10000)")
		flags.String("provision-amount-grt", "1000", "Provision amount in GRT (decimal, e.g. 1000)")
	}),
)

func runDemoSetup(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	rpcEndpoint := strings.TrimSpace(sflags.MustGetString(cmd, "rpc-endpoint"))
	chainID := sflags.MustGetUint64(cmd, "chain-id")

	collectorHex := sflags.MustGetString(cmd, "collector-address")
	escrowHex := sflags.MustGetString(cmd, "escrow-address")
	dataServiceHex := sflags.MustGetString(cmd, "data-service-address")
	stakingHex := sflags.MustGetString(cmd, "staking-address")
	tokenHex := sflags.MustGetString(cmd, "grt-token-address")

	deployerKeyHex := sflags.MustGetString(cmd, "deployer-private-key")
	payerKeyHex := sflags.MustGetString(cmd, "payer-private-key")
	serviceProviderKeyHex := sflags.MustGetString(cmd, "service-provider-private-key")
	signerKeyHex := strings.TrimSpace(sflags.MustGetString(cmd, "signer-private-key"))
	envFilePath := strings.TrimSpace(sflags.MustGetString(cmd, "env-file"))

	escrowAmountStr := sflags.MustGetString(cmd, "escrow-amount-grt")
	provisionAmountStr := sflags.MustGetString(cmd, "provision-amount-grt")

	cli.Ensure(rpcEndpoint != "", "<rpc-endpoint> is required")

	collectorAddr, err := eth.NewAddress(collectorHex)
	cli.NoError(err, "invalid <collector-address> %q", collectorHex)
	escrowAddr, err := eth.NewAddress(escrowHex)
	cli.NoError(err, "invalid <escrow-address> %q", escrowHex)
	dataServiceAddr, err := eth.NewAddress(dataServiceHex)
	cli.NoError(err, "invalid <data-service-address> %q", dataServiceHex)
	stakingAddr, err := eth.NewAddress(stakingHex)
	cli.NoError(err, "invalid <staking-address> %q", stakingHex)
	tokenAddr, err := eth.NewAddress(tokenHex)
	cli.NoError(err, "invalid <grt-token-address> %q", tokenHex)

	deployerKey, err := eth.NewPrivateKey(deployerKeyHex)
	cli.NoError(err, "invalid <deployer-private-key> %q", deployerKeyHex)
	payerKey, err := eth.NewPrivateKey(payerKeyHex)
	cli.NoError(err, "invalid <payer-private-key> %q", payerKeyHex)
	serviceProviderKey, err := eth.NewPrivateKey(serviceProviderKeyHex)
	cli.NoError(err, "invalid <service-provider-private-key> %q", serviceProviderKeyHex)

	var signerKey *eth.PrivateKey
	if signerKeyHex != "" {
		signerKey, err = eth.NewPrivateKey(signerKeyHex)
		cli.NoError(err, "invalid <signer-private-key> %q", signerKeyHex)
	} else {
		signerKey, err = eth.NewRandomPrivateKey()
		cli.NoError(err, "unable to generate random signer key")
	}

	escrowAmount, err := sds.ParseGRT(escrowAmountStr)
	cli.NoError(err, "invalid <escrow-amount-grt> %q", escrowAmountStr)
	provisionAmount, err := sds.ParseGRT(provisionAmountStr)
	cli.NoError(err, "invalid <provision-amount-grt> %q", provisionAmountStr)

	rpcClient := rpc.NewClient(rpcEndpoint)
	serviceProviderAddr := serviceProviderKey.PublicKey().Address()
	payerAddr := payerKey.PublicKey().Address()
	signerAddr := signerKey.PublicKey().Address()

	// Load ABIs from embedded artifacts.
	tokenABI, err := artifacts.LoadABI("MockGRTToken")
	cli.NoError(err, "unable to load MockGRTToken ABI")
	escrowABI, err := artifacts.LoadABI("PaymentsEscrow")
	cli.NoError(err, "unable to load PaymentsEscrow ABI")
	collectorABI, err := artifacts.LoadABI("GraphTallyCollector")
	cli.NoError(err, "unable to load GraphTallyCollector ABI")
	dataServiceABI, err := artifacts.LoadABI("SubstreamsDataService")
	cli.NoError(err, "unable to load SubstreamsDataService ABI")
	stakingABI, err := artifacts.LoadABI("MockStaking")
	cli.NoError(err, "unable to load MockStaking ABI")

	tokenContract := &devenv.Contract{Address: tokenAddr, ABI: tokenABI}
	escrowContract := &devenv.Contract{Address: escrowAddr, ABI: escrowABI}
	collectorContract := &devenv.Contract{Address: collectorAddr, ABI: collectorABI}
	dataServiceContract := &devenv.Contract{Address: dataServiceAddr, ABI: dataServiceABI}
	stakingContract := &devenv.Contract{Address: stakingAddr, ABI: stakingABI}

	// Mint GRT to payer.
	{
		data, err := tokenContract.CallData("mint", payerAddr, escrowAmount.BigInt())
		cli.NoError(err, "unable to encode MockGRTToken.mint")
		cli.NoError(devenv.SendTransaction(ctx, rpcClient, deployerKey, chainID, &tokenContract.Address, sds.ZeroGRT().BigInt(), data), "minting GRT")
	}

	// Approve escrow to spend GRT.
	{
		data, err := tokenContract.CallData("approve", escrowContract.Address, escrowAmount.BigInt())
		cli.NoError(err, "unable to encode MockGRTToken.approve")
		cli.NoError(devenv.SendTransaction(ctx, rpcClient, payerKey, chainID, &tokenContract.Address, sds.ZeroGRT().BigInt(), data), "approving escrow spend")
	}

	// Deposit into escrow for collector+serviceProvider.
	{
		data, err := escrowContract.CallData("deposit", collectorContract.Address, serviceProviderAddr, escrowAmount.BigInt())
		cli.NoError(err, "unable to encode PaymentsEscrow.deposit")
		cli.NoError(devenv.SendTransaction(ctx, rpcClient, payerKey, chainID, &escrowContract.Address, sds.ZeroGRT().BigInt(), data), "depositing escrow")
	}

	// Set provision tokens range (min=0 for demo).
	{
		data, err := dataServiceContract.CallData("setProvisionTokensRange", sds.ZeroGRT().BigInt())
		cli.NoError(err, "unable to encode SubstreamsDataService.setProvisionTokensRange")
		cli.NoError(devenv.SendTransaction(ctx, rpcClient, deployerKey, chainID, &dataServiceContract.Address, sds.ZeroGRT().BigInt(), data), "setting provision tokens range")
	}

	// Set provision for service provider.
	{
		data, err := stakingContract.CallData("setProvision", serviceProviderAddr, dataServiceAddr, provisionAmount.BigInt(), uint32(0), uint64(0))
		cli.NoError(err, "unable to encode MockStaking.setProvision")
		cli.NoError(devenv.SendTransaction(ctx, rpcClient, deployerKey, chainID, &stakingContract.Address, sds.ZeroGRT().BigInt(), data), "setting provision")
	}

	// Register service provider.
	{
		registerData := make([]byte, 32)
		copy(registerData[12:], serviceProviderAddr[:])

		data, err := dataServiceContract.CallData("register", serviceProviderAddr, registerData)
		cli.NoError(err, "unable to encode SubstreamsDataService.register")
		cli.NoError(devenv.SendTransaction(ctx, rpcClient, serviceProviderKey, chainID, &dataServiceContract.Address, sds.ZeroGRT().BigInt(), data), "registering service provider")
	}

	// Authorize signer.
	{
		proofDeadline := uint64(time.Now().Add(1 * time.Hour).Unix())
		proof, err := devenv.GenerateSignerProof(chainID, collectorAddr, proofDeadline, payerAddr, signerKey)
		cli.NoError(err, "unable to generate signer proof")

		data, err := collectorContract.CallData("authorizeSigner", signerAddr, new(big.Int).SetUint64(proofDeadline), proof)
		cli.NoError(err, "unable to encode GraphTallyCollector.authorizeSigner")
		cli.NoError(devenv.SendTransaction(ctx, rpcClient, payerKey, chainID, &collectorContract.Address, sds.ZeroGRT().BigInt(), data), "authorizing signer")
	}

	authorized, err := isAuthorized(ctx, rpcClient, collectorContract, payerAddr, signerAddr)
	cli.NoError(err, "unable to verify isAuthorized")
	cli.Ensure(authorized, "expected signer %s to be authorized for payer %s", signerAddr.Pretty(), payerAddr.Pretty())

	if envFilePath != "" {
		if err := writeDemoEnvFile(envFilePath, map[string]string{
			"SDS_DEMO_CHAIN_ID":                 fmt.Sprintf("%d", chainID),
			"SDS_DEMO_RPC_ENDPOINT":             rpcEndpoint,
			"SDS_DEMO_COLLECTOR_ADDRESS":        collectorAddr.Pretty(),
			"SDS_DEMO_ESCROW_ADDRESS":           escrowAddr.Pretty(),
			"SDS_DEMO_DATA_SERVICE_ADDRESS":     dataServiceAddr.Pretty(),
			"SDS_DEMO_SERVICE_PROVIDER_ADDRESS": serviceProviderAddr.Pretty(),
			"SDS_DEMO_PAYER_ADDRESS":            payerAddr.Pretty(),
			"SDS_DEMO_SIGNER_PRIVATE_KEY":       "0x" + signerKey.String(),
			"SDS_DEMO_SIGNER_ADDRESS":           signerAddr.Pretty(),
		}); err != nil {
			return err
		}
		fmt.Printf("Wrote demo env file for `.reflex.stack`: %s\n\n", envFilePath)
	}

	fmt.Printf("\nDemo state prepared successfully.\n\n")
	fmt.Printf("AUTHORIZED SIGNER:\n")
	fmt.Printf("  Signer address:      %s\n", signerAddr.Pretty())
	fmt.Printf("  Signer private key:  0x%s\n", signerKey.String())
	fmt.Printf("\n")

	fmt.Printf("START COMMANDS:\n")
	fmt.Printf("  Provider gateway:\n")
	fmt.Printf("    sds provider gateway --service-provider %s --collector-address %s --escrow-address %s --rpc-endpoint %s\n",
		serviceProviderAddr.Pretty(),
		collectorAddr.Pretty(),
		escrowAddr.Pretty(),
		rpcEndpoint,
	)
	fmt.Printf("\n")
	fmt.Printf("  Consumer sidecar:\n")
	fmt.Printf("    sds consumer sidecar --signer-private-key 0x%s --collector-address %s\n",
		signerKey.String(),
		collectorAddr.Pretty(),
	)
	fmt.Printf("\n")
	fmt.Printf("  Demo flow:\n")
	fmt.Printf("    sds demo flow --payer-address %s --receiver-address %s --data-service-address %s --provider-endpoint http://localhost:9001 --consumer-sidecar-addr http://localhost:9002\n",
		payerAddr.Pretty(),
		serviceProviderAddr.Pretty(),
		dataServiceAddr.Pretty(),
	)

	return nil
}

func writeDemoEnvFile(path string, exports map[string]string) error {
	var b strings.Builder
	b.WriteString("# Generated by `sds demo setup`\n")
	b.WriteString("# Source this file to align `.reflex.stack` with the authorized signer.\n")

	keys := make([]string, 0, len(exports))
	for k := range exports {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := exports[k]
		if strings.TrimSpace(k) == "" {
			continue
		}
		v = strings.TrimSpace(v)
		v = strings.ReplaceAll(v, "'", "'\\''")
		fmt.Fprintf(&b, "export %s='%s'\n", k, v)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating env file directory for %q: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("writing env file %q: %w", path, err)
	}
	return nil
}

func isAuthorized(ctx context.Context, rpcClient *rpc.Client, collector *devenv.Contract, payer, signer eth.Address) (bool, error) {
	fn := collector.ABI.FindFunctionByName("isAuthorized")
	if fn == nil {
		return false, fmt.Errorf("isAuthorized function not found in ABI")
	}

	data, err := fn.NewCall(payer, signer).Encode()
	if err != nil {
		return false, fmt.Errorf("encoding isAuthorized call: %w", err)
	}

	params := rpc.CallParams{To: collector.Address, Data: data}
	resultHex, err := rpcClient.Call(ctx, params)
	if err != nil {
		return false, fmt.Errorf("calling isAuthorized: %w", err)
	}

	resultHex = strings.TrimPrefix(resultHex, "0x")
	out, err := hex.DecodeString(resultHex)
	if err != nil {
		return false, fmt.Errorf("decoding isAuthorized result: %w", err)
	}
	if len(out) != 32 {
		return false, fmt.Errorf("unexpected isAuthorized result length: %d", len(out))
	}

	return out[31] == 1, nil
}
