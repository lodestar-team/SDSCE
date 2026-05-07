package main

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	sds "github.com/graphprotocol/substreams-data-service"
	chainclient "github.com/graphprotocol/substreams-data-service/contracts/chain"
	"github.com/graphprotocol/substreams-data-service/contracts/erc20"
	horizoncontracts "github.com/graphprotocol/substreams-data-service/contracts/horizon"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
)

var consumerFundingCmd = Group(
	"funding",
	"Consumer payer funding commands",
	consumerFundingStatusCmd,
	consumerFundingApproveCmd,
	consumerFundingDepositCmd,
	consumerFundingTopUpCmd,
)

var consumerFundingStatusCmd = Command(
	runConsumerFundingStatus,
	"status",
	"Show payer wallet, allowance, and escrow balances",
	Flags(func(flags *pflag.FlagSet) {
		addRPCFlags(flags)
		flags.String("grt-token-address", "", "GRT token contract address (required)")
		flags.String("escrow-address", "", "PaymentsEscrow contract address (required)")
		flags.String("collector-address", "", "GraphTallyCollector contract address for escrow pair (required)")
		flags.String("payer-address", "", "Payer address (required)")
		flags.String("receiver-address", "", "Receiver/provider address for escrow pair (required)")
		flags.String("min-escrow-balance", "", "Optional minimum escrow balance to compare, for example '100 GRT'")
	}),
)

func runConsumerFundingStatus(cmd *cobra.Command, args []string) error {
	grtAddress := parseAddressFlag(cmd, "grt-token-address")
	escrowAddress := parseAddressFlag(cmd, "escrow-address")
	collectorAddress := parseAddressFlag(cmd, "collector-address")
	payerAddress := parseAddressFlag(cmd, "payer-address")
	receiverAddress := parseAddressFlag(cmd, "receiver-address")
	minEscrowBalance, hasMinEscrowBalance := parseOptionalGRTFlag(cmd, "min-escrow-balance")

	return withRPCClient(cmd, func(ctx context.Context, client *chainclient.Client) error {
		token := erc20.MustNew()
		escrow := horizoncontracts.MustNewEscrow()

		walletBalance, err := queryERC20Balance(ctx, client, token, grtAddress, payerAddress)
		if err != nil {
			return err
		}
		allowance, err := queryERC20Allowance(ctx, client, token, grtAddress, payerAddress, escrowAddress)
		if err != nil {
			return err
		}
		escrowBalance, err := queryEscrowBalance(ctx, client, escrow, escrowAddress, payerAddress, collectorAddress, receiverAddress)
		if err != nil {
			return err
		}

		fmt.Printf("payer_address: %s\n", payerAddress.Hex())
		fmt.Printf("receiver_address: %s\n", receiverAddress.Hex())
		fmt.Printf("collector_address: %s\n", collectorAddress.Hex())
		fmt.Printf("escrow_address: %s\n", escrowAddress.Hex())
		formatGRTLine("wallet_balance", walletBalance)
		formatGRTLine("escrow_allowance", allowance)
		formatGRTLine("escrow_balance", escrowBalance)

		if hasMinEscrowBalance {
			needed := minEscrowBalance.Sub(&escrowBalance)
			formatGRTLine("min_escrow_balance", minEscrowBalance)
			formatGRTLine("needed_to_minimum", needed)
		}

		return nil
	})
}

var consumerFundingApproveCmd = Command(
	runConsumerFundingApprove,
	"approve",
	"Approve PaymentsEscrow to spend payer GRT",
	Flags(func(flags *pflag.FlagSet) {
		addRPCFlags(flags)
		addTxFlags(flags)
		flags.String("grt-token-address", "", "GRT token contract address (required)")
		flags.String("escrow-address", "", "PaymentsEscrow contract address (required)")
		flags.String("payer-address", "", "Optional payer address; must match payer key when supplied")
		flags.String("amount", "", "Allowance amount, for example '1000 GRT' (required)")
		flags.Bool("force", false, "Allow non-zero to non-zero allowance replacement")
		flags.Bool("reset-first", false, "Submit approve(escrow, 0) before approve(escrow, amount)")
	}),
)

func runConsumerFundingApprove(cmd *cobra.Command, args []string) error {
	grtAddress := parseAddressFlag(cmd, "grt-token-address")
	escrowAddress := parseAddressFlag(cmd, "escrow-address")
	amount := parseGRTFlag(cmd, "amount")
	key := parsePayerKey(cmd)
	validateOptionalPayerAddress(cmd, key)
	opts := txOptionsFromFlags(cmd, key)
	force := sflags.MustGetBool(cmd, "force")
	resetFirst := sflags.MustGetBool(cmd, "reset-first")
	cli.Ensure(!(force && resetFirst), "--force and --reset-first are mutually exclusive")
	cli.Ensure(!rejectNoWaitResetFirst(resetFirst, opts.NoWait), "--reset-first cannot be used with --no-wait because the second approval must wait for the reset receipt")

	receiptWaits := 1
	if resetFirst {
		receiptWaits = 2
	}

	return withRPCClientReceiptWaits(cmd, receiptWaits, func(ctx context.Context, client *chainclient.Client) error {
		token := erc20.MustNew()
		currentAllowance, err := queryERC20Allowance(ctx, client, token, grtAddress, key.Address, escrowAddress)
		if err != nil {
			return err
		}

		if rejectAllowanceReplacement(currentAllowance, amount, force, resetFirst) {
			return fmt.Errorf("current allowance is %s; refusing non-zero to non-zero approve replacement without --force or --reset-first", currentAllowance.String())
		}

		if resetFirst && !currentAllowance.IsZero() {
			fmt.Printf("payer_address: %s\n", key.Address.Hex())
			fmt.Println("approval_step: reset")
			if err := submitApproval(ctx, client, token, grtAddress, escrowAddress, sds.ZeroGRT(), opts); err != nil {
				return err
			}
		}

		fmt.Printf("payer_address: %s\n", key.Address.Hex())
		fmt.Println("approval_step: approve")
		if err := submitApproval(ctx, client, token, grtAddress, escrowAddress, amount, opts); err != nil {
			return err
		}

		if opts.NoWait || opts.DryRun {
			return nil
		}

		finalAllowance, err := queryERC20Allowance(ctx, client, token, grtAddress, key.Address, escrowAddress)
		if err != nil {
			return err
		}
		formatGRTLine("final_allowance", finalAllowance)
		return nil
	})
}

var consumerFundingDepositCmd = Command(
	runConsumerFundingDeposit,
	"deposit",
	"Deposit payer GRT into PaymentsEscrow",
	Flags(func(flags *pflag.FlagSet) {
		addRPCFlags(flags)
		addTxFlags(flags)
		flags.String("escrow-address", "", "PaymentsEscrow contract address (required)")
		flags.String("collector-address", "", "GraphTallyCollector contract address for escrow pair (required)")
		flags.String("receiver-address", "", "Receiver/provider address for escrow pair (required)")
		flags.String("payer-address", "", "Optional payer address; must match payer key when supplied")
		flags.String("amount", "", "Deposit amount, for example '100 GRT' (required)")
		flags.String("grt-token-address", "", "Optional GRT token contract address for allowance pre-check")
	}),
)

func runConsumerFundingDeposit(cmd *cobra.Command, args []string) error {
	escrowAddress := parseAddressFlag(cmd, "escrow-address")
	collectorAddress := parseAddressFlag(cmd, "collector-address")
	receiverAddress := parseAddressFlag(cmd, "receiver-address")
	grtAddress, hasGRTAddress := parseOptionalAddressFlag(cmd, "grt-token-address")
	amount := parseGRTFlag(cmd, "amount")
	key := parsePayerKey(cmd)
	validateOptionalPayerAddress(cmd, key)
	opts := txOptionsFromFlags(cmd, key)

	return withRPCClient(cmd, func(ctx context.Context, client *chainclient.Client) error {
		token := erc20.MustNew()
		escrow := horizoncontracts.MustNewEscrow()

		if hasGRTAddress {
			if err := ensureAllowance(ctx, client, token, grtAddress, key.Address, escrowAddress, amount); err != nil {
				return err
			}
		}

		return submitDepositAndPrint(ctx, client, escrow, escrowAddress, collectorAddress, receiverAddress, amount, key.Address, opts)
	})
}

var consumerFundingTopUpCmd = Command(
	runConsumerFundingTopUp,
	"top-up",
	"Top up escrow to a target balance",
	Flags(func(flags *pflag.FlagSet) {
		addRPCFlags(flags)
		addTxFlags(flags)
		flags.String("grt-token-address", "", "GRT token contract address (required)")
		flags.String("escrow-address", "", "PaymentsEscrow contract address (required)")
		flags.String("collector-address", "", "GraphTallyCollector contract address for escrow pair (required)")
		flags.String("receiver-address", "", "Receiver/provider address for escrow pair (required)")
		flags.String("payer-address", "", "Optional payer address; must match payer key when supplied")
		flags.String("target-balance", "", "Target escrow balance, for example '500 GRT' (required)")
	}),
)

func runConsumerFundingTopUp(cmd *cobra.Command, args []string) error {
	grtAddress := parseAddressFlag(cmd, "grt-token-address")
	escrowAddress := parseAddressFlag(cmd, "escrow-address")
	collectorAddress := parseAddressFlag(cmd, "collector-address")
	receiverAddress := parseAddressFlag(cmd, "receiver-address")
	targetBalance := parseGRTFlag(cmd, "target-balance")
	key := parsePayerKey(cmd)
	validateOptionalPayerAddress(cmd, key)
	opts := txOptionsFromFlags(cmd, key)

	return withRPCClient(cmd, func(ctx context.Context, client *chainclient.Client) error {
		token := erc20.MustNew()
		escrow := horizoncontracts.MustNewEscrow()

		currentBalance, err := queryEscrowBalance(ctx, client, escrow, escrowAddress, key.Address, collectorAddress, receiverAddress)
		if err != nil {
			return err
		}
		formatGRTLine("current_escrow_balance", currentBalance)
		formatGRTLine("target_escrow_balance", targetBalance)

		depositAmount, needed := topUpDepositAmount(currentBalance, targetBalance)
		if !needed {
			fmt.Println("top_up_needed: false")
			return nil
		}

		fmt.Println("top_up_needed: true")
		formatGRTLine("deposit_amount", depositAmount)
		if err := ensureAllowance(ctx, client, token, grtAddress, key.Address, escrowAddress, depositAmount); err != nil {
			return err
		}

		return submitDepositAndPrint(ctx, client, escrow, escrowAddress, collectorAddress, receiverAddress, depositAmount, key.Address, opts)
	})
}

func rejectAllowanceReplacement(currentAllowance sds.GRT, requestedAmount sds.GRT, force bool, resetFirst bool) bool {
	return !currentAllowance.IsZero() && !requestedAmount.IsZero() && !force && !resetFirst
}

func rejectNoWaitResetFirst(resetFirst bool, noWait bool) bool {
	return resetFirst && noWait
}

func topUpDepositAmount(currentBalance sds.GRT, targetBalance sds.GRT) (sds.GRT, bool) {
	if currentBalance.Cmp(&targetBalance) >= 0 {
		return sds.ZeroGRT(), false
	}
	return targetBalance.Sub(&currentBalance), true
}

func queryERC20Balance(ctx context.Context, client *chainclient.Client, token *erc20.Contract, tokenAddress common.Address, account common.Address) (sds.GRT, error) {
	data, err := token.PackBalanceOf(account)
	if err != nil {
		return sds.ZeroGRT(), err
	}
	result, err := client.CallContract(ctx, tokenAddress, data)
	if err != nil {
		return sds.ZeroGRT(), err
	}
	balance, err := token.UnpackBalanceOf(result)
	if err != nil {
		return sds.ZeroGRT(), err
	}
	return grtFromBigInt(balance), nil
}

func queryERC20Allowance(ctx context.Context, client *chainclient.Client, token *erc20.Contract, tokenAddress common.Address, owner common.Address, spender common.Address) (sds.GRT, error) {
	data, err := token.PackAllowance(owner, spender)
	if err != nil {
		return sds.ZeroGRT(), err
	}
	result, err := client.CallContract(ctx, tokenAddress, data)
	if err != nil {
		return sds.ZeroGRT(), err
	}
	allowance, err := token.UnpackAllowance(result)
	if err != nil {
		return sds.ZeroGRT(), err
	}
	return grtFromBigInt(allowance), nil
}

func queryEscrowBalance(ctx context.Context, client *chainclient.Client, escrow *horizoncontracts.Escrow, escrowAddress common.Address, payer common.Address, collector common.Address, receiver common.Address) (sds.GRT, error) {
	data, err := escrow.PackGetBalance(payer, collector, receiver)
	if err != nil {
		return sds.ZeroGRT(), err
	}
	result, err := client.CallContract(ctx, escrowAddress, data)
	if err != nil {
		return sds.ZeroGRT(), err
	}
	balance, err := escrow.UnpackGetBalance(result)
	if err != nil {
		return sds.ZeroGRT(), err
	}
	return grtFromBigInt(balance), nil
}

func ensureAllowance(ctx context.Context, client *chainclient.Client, token *erc20.Contract, tokenAddress common.Address, owner common.Address, spender common.Address, needed sds.GRT) error {
	allowance, err := queryERC20Allowance(ctx, client, token, tokenAddress, owner, spender)
	if err != nil {
		return err
	}
	if allowance.Cmp(&needed) < 0 {
		return fmt.Errorf("escrow allowance %s is below required amount %s; run consumer funding approve first", allowance.String(), needed.String())
	}
	return nil
}

func submitApproval(ctx context.Context, client *chainclient.Client, token *erc20.Contract, tokenAddress common.Address, spender common.Address, amount sds.GRT, opts chainclient.TxOptions) error {
	data, err := token.PackApprove(spender, amount.BigInt())
	if err != nil {
		return err
	}
	result, err := client.SendDynamicFeeTransaction(ctx, tokenAddress, big.NewInt(0), data, opts)
	if err != nil {
		return err
	}
	formatGRTLine("approval_amount", amount)
	formatTxResult(result)
	return nil
}

func submitDepositAndPrint(ctx context.Context, client *chainclient.Client, escrow *horizoncontracts.Escrow, escrowAddress common.Address, collector common.Address, receiver common.Address, amount sds.GRT, payer common.Address, opts chainclient.TxOptions) error {
	data, err := escrow.PackDeposit(collector, receiver, amount.BigInt())
	if err != nil {
		return err
	}
	result, err := client.SendDynamicFeeTransaction(ctx, escrowAddress, big.NewInt(0), data, opts)
	if err != nil {
		return err
	}
	fmt.Printf("payer_address: %s\n", payer.Hex())
	formatGRTLine("deposit_amount", amount)
	formatTxResult(result)

	if opts.NoWait || opts.DryRun {
		return nil
	}

	finalBalance, err := queryEscrowBalance(ctx, client, escrow, escrowAddress, payer, collector, receiver)
	if err != nil {
		return err
	}
	formatGRTLine("final_escrow_balance", finalBalance)
	return nil
}
