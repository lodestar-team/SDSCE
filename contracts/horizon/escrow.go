package horizoncontracts

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/graphprotocol/substreams-data-service/contracts/artifacts"
)

type Escrow struct {
	abi abi.ABI
}

func NewEscrow() (*Escrow, error) {
	artifact, err := artifacts.Load("PaymentsEscrow")
	if err != nil {
		return nil, err
	}
	parsed, err := abi.JSON(strings.NewReader(string(artifact.ABI)))
	if err != nil {
		return nil, fmt.Errorf("parsing PaymentsEscrow ABI: %w", err)
	}
	return &Escrow{abi: parsed}, nil
}

func MustNewEscrow() *Escrow {
	escrow, err := NewEscrow()
	if err != nil {
		panic(err)
	}
	return escrow
}

func (e *Escrow) PackGetBalance(payer common.Address, collector common.Address, receiver common.Address) ([]byte, error) {
	return e.abi.Pack("getBalance", payer, collector, receiver)
}

func (e *Escrow) UnpackGetBalance(data []byte) (*big.Int, error) {
	values, err := e.abi.Unpack("getBalance", data)
	if err != nil {
		return nil, fmt.Errorf("unpacking getBalance: %w", err)
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("unpacking getBalance: expected 1 value, got %d", len(values))
	}
	balance, ok := values[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("unpacking getBalance: expected *big.Int, got %T", values[0])
	}
	return balance, nil
}

func (e *Escrow) PackDeposit(collector common.Address, receiver common.Address, amount *big.Int) ([]byte, error) {
	return e.abi.Pack("deposit", collector, receiver, amount)
}
