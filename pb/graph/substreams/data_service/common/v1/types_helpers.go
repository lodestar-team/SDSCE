package commonv1

import (
	"fmt"
	"math/big"

	"github.com/streamingfast/eth-go"
)

// ToEth converts the Address to an eth.Address.
func (a *Address) ToEth() (eth.Address, error) {
	if a == nil {
		return nil, fmt.Errorf("address is required")
	}
	if len(a.Bytes) != 20 {
		return nil, fmt.Errorf("invalid address length: got %d bytes, want 20", len(a.Bytes))
	}
	return eth.Address(a.Bytes), nil
}

// AddressFromEth creates an Address from an eth.Address.
func AddressFromEth(addr eth.Address) *Address {
	return &Address{Bytes: addr}
}

// ToNative converts the BigInt to a *big.Int.
func (b *BigInt) ToNative() *big.Int {
	return new(big.Int).SetBytes(b.Bytes)
}

// BigIntFromNative creates a BigInt from a *big.Int.
func BigIntFromNative(i *big.Int) *BigInt {
	return &BigInt{Bytes: i.Bytes()}
}
