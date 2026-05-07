package erc20

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

const minimalABI = `[
	{"type":"function","name":"balanceOf","stateMutability":"view","inputs":[{"name":"account","type":"address"}],"outputs":[{"name":"","type":"uint256"}]},
	{"type":"function","name":"allowance","stateMutability":"view","inputs":[{"name":"owner","type":"address"},{"name":"spender","type":"address"}],"outputs":[{"name":"","type":"uint256"}]},
	{"type":"function","name":"approve","stateMutability":"nonpayable","inputs":[{"name":"spender","type":"address"},{"name":"value","type":"uint256"}],"outputs":[{"name":"","type":"bool"}]},
	{"type":"function","name":"decimals","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint8"}]}
]`

type Contract struct {
	abi abi.ABI
}

func New() (*Contract, error) {
	parsed, err := abi.JSON(strings.NewReader(minimalABI))
	if err != nil {
		return nil, fmt.Errorf("parsing ERC20 ABI: %w", err)
	}
	return &Contract{abi: parsed}, nil
}

func MustNew() *Contract {
	contract, err := New()
	if err != nil {
		panic(err)
	}
	return contract
}

func (c *Contract) PackBalanceOf(account common.Address) ([]byte, error) {
	return c.abi.Pack("balanceOf", account)
}

func (c *Contract) UnpackBalanceOf(data []byte) (*big.Int, error) {
	return unpackUint256(c.abi, "balanceOf", data)
}

func (c *Contract) PackAllowance(owner common.Address, spender common.Address) ([]byte, error) {
	return c.abi.Pack("allowance", owner, spender)
}

func (c *Contract) UnpackAllowance(data []byte) (*big.Int, error) {
	return unpackUint256(c.abi, "allowance", data)
}

func (c *Contract) PackApprove(spender common.Address, value *big.Int) ([]byte, error) {
	return c.abi.Pack("approve", spender, value)
}

func (c *Contract) PackDecimals() ([]byte, error) {
	return c.abi.Pack("decimals")
}

func (c *Contract) UnpackDecimals(data []byte) (uint8, error) {
	values, err := c.abi.Unpack("decimals", data)
	if err != nil {
		return 0, fmt.Errorf("unpacking decimals: %w", err)
	}
	if len(values) != 1 {
		return 0, fmt.Errorf("unpacking decimals: expected 1 value, got %d", len(values))
	}
	decimals, ok := values[0].(uint8)
	if !ok {
		return 0, fmt.Errorf("unpacking decimals: expected uint8, got %T", values[0])
	}
	return decimals, nil
}

func unpackUint256(contractABI abi.ABI, method string, data []byte) (*big.Int, error) {
	values, err := contractABI.Unpack(method, data)
	if err != nil {
		return nil, fmt.Errorf("unpacking %s: %w", method, err)
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("unpacking %s: expected 1 value, got %d", method, len(values))
	}
	value, ok := values[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("unpacking %s: expected *big.Int, got %T", method, values[0])
	}
	return value, nil
}
