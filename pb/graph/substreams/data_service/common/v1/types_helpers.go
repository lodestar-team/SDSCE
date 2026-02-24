package commonv1

import (
	"fmt"
	"math/big"

	sds "github.com/graphprotocol/substreams-data-service"
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

// ToNative converts the GRT proto to a sds.GRT.
func (g *GRT) ToNative() sds.GRT {
	if g == nil || len(g.Bytes) == 0 {
		return sds.ZeroGRT()
	}
	return sds.NewGRTFromBigInt(new(big.Int).SetBytes(g.Bytes))
}

// ToBigInt converts the GRT proto directly to a *big.Int.
// Shorthand for ToNative().BigInt().
func (g *GRT) ToBigInt() *big.Int {
	return g.ToNative().BigInt()
}

// ToInt64 converts the GRT proto directly to an int64.
// Shorthand for ToNative().BigInt().Int64().
func (g *GRT) ToInt64() int64 {
	return g.ToNative().BigInt().Int64()
}

// ToUint64 converts the GRT proto directly to a uint64.
// Shorthand for ToNative().BigInt().Uint64().
func (g *GRT) ToUint64() uint64 {
	return g.ToNative().BigInt().Uint64()
}

// GRTFromNative creates a GRT proto from a sds.GRT.
func GRTFromNative(grt sds.GRT) *GRT {
	return &GRT{Bytes: grt.BigInt().Bytes()}
}

// GRTFromBigInt creates a GRT proto from a *big.Int.
// Convenience function combining sds.NewGRTFromBigInt and GRTFromNative.
func GRTFromBigInt(i *big.Int) *GRT {
	return GRTFromNative(sds.NewGRTFromBigInt(i))
}

// ToNative converts PricingConfig proto to sidecar.PricingConfig.
func (p *PricingConfig) ToNative() *sds.PricingConfig {
	if p == nil {
		return nil
	}
	return &sds.PricingConfig{
		PricePerBlock: p.PricePerBlock.ToNative(),
		PricePerByte:  p.PricePerByte.ToNative(),
	}
}

// PricingConfigFromNative creates a PricingConfig proto from sds.PricingConfig.
func PricingConfigFromNative(cfg *sds.PricingConfig) *PricingConfig {
	if cfg == nil {
		return nil
	}
	return &PricingConfig{
		PricePerBlock: GRTFromNative(cfg.PricePerBlock),
		PricePerByte:  GRTFromNative(cfg.PricePerByte),
	}
}
