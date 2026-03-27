package sidecar

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/graphprotocol/substreams-data-service/contracts/artifacts"
	"github.com/streamingfast/eth-go"
	"github.com/streamingfast/eth-go/rpc"
)

var loadPaymentsEscrowGetBalanceFn = sync.OnceValues(func() (*eth.MethodDef, error) {
	abi, err := artifacts.LoadABI("PaymentsEscrow")
	if err != nil {
		return nil, fmt.Errorf("loading PaymentsEscrow ABI: %w", err)
	}

	fn := abi.FindFunctionByName("getBalance")
	if fn == nil {
		return nil, fmt.Errorf("getBalance function not found in PaymentsEscrow ABI")
	}

	return fn, nil
})

// EscrowQuerier provides methods to query the PaymentsEscrow contract
type EscrowQuerier struct {
	rpcClient  *rpc.Client
	escrowAddr eth.Address
}

// NewEscrowQuerier creates a new EscrowQuerier
func NewEscrowQuerier(rpcEndpoint string, escrowAddr eth.Address) *EscrowQuerier {
	return &EscrowQuerier{
		rpcClient:  rpc.NewClient(rpcEndpoint),
		escrowAddr: escrowAddr,
	}
}

// GetBalance returns the escrow balance for a payer -> receiver via collector
// This calls PaymentsEscrow.getBalance(payer, collector, receiver)
func (q *EscrowQuerier) GetBalance(ctx context.Context, payer, collector, receiver eth.Address) (*big.Int, error) {
	fn, err := loadPaymentsEscrowGetBalanceFn()
	if err != nil {
		return nil, err
	}

	data, err := fn.NewCall(payer, collector, receiver).Encode()
	if err != nil {
		return nil, fmt.Errorf("encoding getBalance call: %w", err)
	}

	params := rpc.CallParams{
		To:   q.escrowAddr,
		Data: data,
	}

	resultHex, err := q.rpcClient.Call(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("calling getBalance: %w", err)
	}

	if strings.HasPrefix(resultHex, "0x") {
		resultHex = resultHex[2:]
	}

	resultBytes, err := hex.DecodeString(resultHex)
	if err != nil {
		return nil, fmt.Errorf("decoding result: %w", err)
	}

	if len(resultBytes) != 32 {
		return nil, fmt.Errorf("unexpected result length: %d", len(resultBytes))
	}

	return new(big.Int).SetBytes(resultBytes), nil
}
