package erc20

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestPackERC20Selectors(t *testing.T) {
	contract := MustNew()
	account := common.HexToAddress("0x1111111111111111111111111111111111111111")
	spender := common.HexToAddress("0x2222222222222222222222222222222222222222")

	balanceOfData, err := contract.PackBalanceOf(account)
	balanceOf := mustPack(t, balanceOfData, err)
	allowanceData, err := contract.PackAllowance(account, spender)
	allowance := mustPack(t, allowanceData, err)
	approveData, err := contract.PackApprove(spender, big.NewInt(10))
	approve := mustPack(t, approveData, err)

	tests := []struct {
		name       string
		data       []byte
		selector   string
		wantLength int
	}{
		{name: "balanceOf", data: balanceOf, selector: "70a08231", wantLength: 36},
		{name: "allowance", data: allowance, selector: "dd62ed3e", wantLength: 68},
		{name: "approve", data: approve, selector: "095ea7b3", wantLength: 68},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hex.EncodeToString(test.data[:4]); got != test.selector {
				t.Fatalf("selector = %s, want %s", got, test.selector)
			}
			if len(test.data) != test.wantLength {
				t.Fatalf("len(data) = %d, want %d", len(test.data), test.wantLength)
			}
		})
	}
}

func TestUnpackUint256(t *testing.T) {
	contract := MustNew()
	output, err := contract.abi.Methods["balanceOf"].Outputs.Pack(big.NewInt(123))
	if err != nil {
		t.Fatalf("pack output: %v", err)
	}

	got, err := contract.UnpackBalanceOf(output)
	if err != nil {
		t.Fatalf("UnpackBalanceOf() error = %v", err)
	}
	if got.Cmp(big.NewInt(123)) != 0 {
		t.Fatalf("UnpackBalanceOf() = %s, want 123", got.String())
	}
}

func mustPack(t *testing.T, data []byte, err error) []byte {
	t.Helper()
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	return data
}
