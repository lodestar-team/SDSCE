package chain

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

type Client struct {
	rpc *ethclient.Client
}

type TxOptions struct {
	ChainID              *big.Int
	From                 common.Address
	PrivateKey           *ecdsa.PrivateKey
	TxType               string
	GasLimit             uint64
	MaxFeePerGas         *big.Int
	MaxPriorityFeePerGas *big.Int
	GasPrice             *big.Int
	ReceiptTimeout       time.Duration
	ReceiptPollInterval  time.Duration
	NoWait               bool
	DryRun               bool
}

type TxResult struct {
	Hash                 common.Hash
	Receipt              *types.Receipt
	DryRun               bool
	GasLimit             uint64
	MaxFeePerGas         *big.Int
	MaxPriorityFeePerGas *big.Int
	GasPrice             *big.Int
}

func DialContext(ctx context.Context, endpoint string) (*Client, error) {
	rpc, err := ethclient.DialContext(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("dialing RPC endpoint: %w", err)
	}
	return &Client{rpc: rpc}, nil
}

func (c *Client) Close() {
	if c.rpc != nil {
		c.rpc.Close()
	}
}

func (c *Client) CallContract(ctx context.Context, to common.Address, data []byte) ([]byte, error) {
	result, err := c.rpc.CallContract(ctx, ethereum.CallMsg{
		To:   &to,
		Data: data,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("calling contract %s: %w", to.Hex(), err)
	}
	return result, nil
}

func (c *Client) SendDynamicFeeTransaction(ctx context.Context, to common.Address, value *big.Int, data []byte, opts TxOptions) (*TxResult, error) {
	if opts.TxType == "legacy" {
		return c.SendLegacyTransaction(ctx, to, value, data, opts)
	}
	if opts.ChainID == nil || opts.ChainID.Sign() <= 0 {
		return nil, errors.New("chain ID is required")
	}
	if opts.PrivateKey == nil {
		return nil, errors.New("private key is required")
	}
	if value == nil {
		value = big.NewInt(0)
	}

	gasLimit := opts.GasLimit
	if gasLimit == 0 {
		estimated, err := c.rpc.EstimateGas(ctx, ethereum.CallMsg{
			From:  opts.From,
			To:    &to,
			Value: value,
			Data:  data,
		})
		if err != nil {
			return nil, fmt.Errorf("estimating gas: %w", err)
		}
		gasLimit = estimated
	}

	maxPriorityFeePerGas := opts.MaxPriorityFeePerGas
	if maxPriorityFeePerGas == nil {
		tip, err := c.rpc.SuggestGasTipCap(ctx)
		if err != nil {
			return nil, fmt.Errorf("suggesting gas tip cap: %w", err)
		}
		maxPriorityFeePerGas = tip
	}

	maxFeePerGas := opts.MaxFeePerGas
	if maxFeePerGas == nil {
		header, err := c.rpc.HeaderByNumber(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("fetching latest header: %w", err)
		}
		if header.BaseFee == nil {
			return nil, errors.New("latest block has no base fee; EIP-1559 dynamic fee transactions are not supported by this chain")
		}
		maxFeePerGas = new(big.Int).Mul(header.BaseFee, big.NewInt(2))
		maxFeePerGas.Add(maxFeePerGas, maxPriorityFeePerGas)
	}
	if maxFeePerGas.Cmp(maxPriorityFeePerGas) < 0 {
		return nil, fmt.Errorf("max fee per gas %s is below max priority fee per gas %s", maxFeePerGas.String(), maxPriorityFeePerGas.String())
	}

	result := &TxResult{
		DryRun:               opts.DryRun,
		GasLimit:             gasLimit,
		MaxFeePerGas:         new(big.Int).Set(maxFeePerGas),
		MaxPriorityFeePerGas: new(big.Int).Set(maxPriorityFeePerGas),
	}
	if opts.DryRun {
		return result, nil
	}

	nonce, err := c.rpc.PendingNonceAt(ctx, opts.From)
	if err != nil {
		return nil, fmt.Errorf("getting pending nonce for %s: %w", opts.From.Hex(), err)
	}

	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   new(big.Int).Set(opts.ChainID),
		Nonce:     nonce,
		GasTipCap: maxPriorityFeePerGas,
		GasFeeCap: maxFeePerGas,
		Gas:       gasLimit,
		To:        &to,
		Value:     value,
		Data:      data,
	})

	signedTx, err := types.SignTx(tx, types.LatestSignerForChainID(opts.ChainID), opts.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("signing transaction: %w", err)
	}

	if err := c.rpc.SendTransaction(ctx, signedTx); err != nil {
		return nil, fmt.Errorf("sending transaction: %w", err)
	}

	result.Hash = signedTx.Hash()
	if opts.NoWait {
		return result, nil
	}

	receipt, err := c.WaitReceipt(ctx, result.Hash, opts.ReceiptTimeout, opts.ReceiptPollInterval)
	if err != nil {
		return result, err
	}
	if receipt.Status == types.ReceiptStatusFailed {
		return result, fmt.Errorf("transaction %s failed with receipt status 0", result.Hash.Hex())
	}
	result.Receipt = receipt
	return result, nil
}

func (c *Client) SendLegacyTransaction(ctx context.Context, to common.Address, value *big.Int, data []byte, opts TxOptions) (*TxResult, error) {
	if opts.ChainID == nil || opts.ChainID.Sign() <= 0 {
		return nil, errors.New("chain ID is required")
	}
	if opts.PrivateKey == nil {
		return nil, errors.New("private key is required")
	}
	if value == nil {
		value = big.NewInt(0)
	}

	gasLimit := opts.GasLimit
	if gasLimit == 0 {
		estimated, err := c.rpc.EstimateGas(ctx, ethereum.CallMsg{
			From:  opts.From,
			To:    &to,
			Value: value,
			Data:  data,
		})
		if err != nil {
			return nil, fmt.Errorf("estimating gas: %w", err)
		}
		gasLimit = estimated
	}

	gasPrice := opts.GasPrice
	if gasPrice == nil {
		suggested, err := c.rpc.SuggestGasPrice(ctx)
		if err != nil {
			return nil, fmt.Errorf("suggesting gas price: %w", err)
		}
		gasPrice = suggested
	}

	result := &TxResult{
		DryRun:   opts.DryRun,
		GasLimit: gasLimit,
		GasPrice: new(big.Int).Set(gasPrice),
	}
	if opts.DryRun {
		return result, nil
	}

	nonce, err := c.rpc.PendingNonceAt(ctx, opts.From)
	if err != nil {
		return nil, fmt.Errorf("getting pending nonce for %s: %w", opts.From.Hex(), err)
	}

	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: gasPrice,
		Gas:      gasLimit,
		To:       &to,
		Value:    value,
		Data:     data,
	})

	signedTx, err := types.SignTx(tx, types.LatestSignerForChainID(opts.ChainID), opts.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("signing transaction: %w", err)
	}

	if err := c.rpc.SendTransaction(ctx, signedTx); err != nil {
		return nil, fmt.Errorf("sending transaction: %w", err)
	}

	result.Hash = signedTx.Hash()
	if opts.NoWait {
		return result, nil
	}

	receipt, err := c.WaitReceipt(ctx, result.Hash, opts.ReceiptTimeout, opts.ReceiptPollInterval)
	if err != nil {
		return result, err
	}
	if receipt.Status == types.ReceiptStatusFailed {
		return result, fmt.Errorf("transaction %s failed with receipt status 0", result.Hash.Hex())
	}
	result.Receipt = receipt
	return result, nil
}

func (c *Client) WaitReceipt(ctx context.Context, txHash common.Hash, timeout time.Duration, pollInterval time.Duration) (*types.Receipt, error) {
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	if pollInterval <= 0 {
		pollInterval = time.Second
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		receipt, err := c.rpc.TransactionReceipt(waitCtx, txHash)
		if err == nil {
			return receipt, nil
		}
		if !errors.Is(err, ethereum.NotFound) {
			return nil, fmt.Errorf("fetching transaction receipt %s: %w", txHash.Hex(), err)
		}

		select {
		case <-waitCtx.Done():
			return nil, fmt.Errorf("waiting for transaction receipt %s: %w", txHash.Hex(), waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func AddressFromPrivateKey(privateKey *ecdsa.PrivateKey) common.Address {
	return crypto.PubkeyToAddress(privateKey.PublicKey)
}
