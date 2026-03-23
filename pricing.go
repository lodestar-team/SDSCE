package sds

import "math/big"

// PriceConverter converts GRT amounts to fiat currency
type PriceConverter interface {
	// ToFiat converts a GRT amount (in wei) to fiat value
	ToFiat(grtWei *big.Int) float64
	// Symbol returns the fiat currency symbol (e.g., "$")
	Symbol() string
}

// StaticPriceConverter uses a fixed GRT/USD exchange rate
type StaticPriceConverter struct {
	grtToUSD float64 // e.g., 0.15 means 1 GRT = $0.15
}

// NewStaticPriceConverter creates a converter with a fixed GRT/USD rate
func NewStaticPriceConverter(grtToUSD float64) *StaticPriceConverter {
	return &StaticPriceConverter{grtToUSD: grtToUSD}
}

func (c *StaticPriceConverter) ToFiat(grtWei *big.Int) float64 {
	if grtWei == nil || grtWei.Sign() == 0 {
		return 0
	}

	// Convert wei (10^18 base units) to GRT as float64
	// grtWei / 10^18 = GRT value
	grtFloat := new(big.Float).SetInt(grtWei)
	divisor := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	grtFloat.Quo(grtFloat, divisor)

	grtValue, _ := grtFloat.Float64()
	return grtValue * c.grtToUSD
}

func (c *StaticPriceConverter) Symbol() string {
	return "$"
}
