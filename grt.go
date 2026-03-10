package sds

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/holiman/uint256"
)

const (
	// GRTDecimals is the number of decimal places for GRT (18 decimals, like ETH).
	GRTDecimals = 18
)

var (
	// grtBase is 10^18 for converting between GRT and base units.
	grtBase = func() uint256.Int {
		var v uint256.Int
		v.Exp(uint256.NewInt(10), uint256.NewInt(GRTDecimals))
		return v
	}()

	// grtBaseBig is the big.Int equivalent for interop.
	grtBaseBig = new(big.Int).Exp(big.NewInt(10), big.NewInt(GRTDecimals), nil)
)

// GRT represents a GRT token amount stored in base units (18 decimals).
// It wraps uint256.Int as a value type for efficient stack-based arithmetic,
// suitable for blockchain token amounts.
//
// Parsing supports:
//   - "1.5 GRT" or "1.5GRT" - explicit GRT suffix
//   - "1.5" - plain decimal (assumed to be GRT)
//
// For serialization, the canonical format is "<decimal> GRT" (e.g., "1.5 GRT").
//
// Zero value is valid and represents 0 GRT.
type GRT struct {
	raw uint256.Int
}

// NewGRT is a dynamic function that creates a GRT from various input types
// (e.g., uint64, *big.Int, string). It uses type assertions to determine
// the appropriate constructor. This provides a convenient API for creating GRT
// values from different sources and is used more for testing and flexibility.
func NewGRT(input any) (GRT, error) {
	switch v := input.(type) {
	case int:
		return NewGRTFromInt64(int64(v)), nil
	case int8:
		return NewGRTFromInt64(int64(v)), nil
	case int16:
		return NewGRTFromInt64(int64(v)), nil
	case int32:
		return NewGRTFromInt64(int64(v)), nil
	case int64:
		return NewGRTFromInt64(v), nil
	case uint:
		return NewGRTFromUint64(uint64(v)), nil
	case uint8:
		return NewGRTFromUint64(uint64(v)), nil
	case uint16:
		return NewGRTFromUint64(uint64(v)), nil
	case uint32:
		return NewGRTFromUint64(uint64(v)), nil
	case uint64:
		return NewGRTFromUint64(v), nil
	case *big.Int:
		return NewGRTFromBigInt(v), nil
	case *uint256.Int:
		return NewGRTFromUint256(v), nil
	case string:
		return NewGRTFromString(v)
	default:
		return GRT{}, fmt.Errorf("unsupported input type: %T", input)
	}
}

// MustNewGRT is like NewGRT but panics on error. Use when input is known to be valid (e.g., in tests).
func MustNewGRT(input any) GRT {
	grt, err := NewGRT(input)
	if err != nil {
		panic(fmt.Sprintf("failed to create GRT from input %q: %w", input, err))
	}

	return grt
}

// NewGRTFromInt64 creates a GRT from an int64 value in base units.
// This is more efficient than NewGRTFromBigInt for small values.
func NewGRTFromInt64(raw int64) GRT {
	unsigned := raw
	if raw < 0 {
		unsigned = -raw
	}

	value := uint256.NewInt(uint64(unsigned))
	if raw < 0 {
		value.Neg(value)
	}

	return GRT{raw: *value}
}

// NewGRTFromUint64 creates a GRT from a uint64 value in base units.
// This is more efficient than NewGRTFromBigInt for small values.
func NewGRTFromUint64(raw uint64) GRT {
	return GRT{raw: *uint256.NewInt(raw)}
}

// NewGRTFromUint256Ptr creates a GRT from a *uint256.Int value in base units.
// If raw is nil, returns zero GRT.
func NewGRTFromUint256(raw *uint256.Int) GRT {
	if raw == nil {
		return GRT{}
	}
	return GRT{raw: *raw}
}

// NewGRTFromBigInt creates a GRT from a big.Int value in base units.
func NewGRTFromBigInt(raw *big.Int) GRT {
	if raw == nil {
		return GRT{}
	}
	v, overflow := uint256.FromBig(raw)
	if overflow {
		// If overflow, clamp to max value
		v = uint256.MustFromHex("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	}
	return GRT{raw: *v}
}

func NewGRTFromString(s string) (GRT, error) {
	return ParseGRT(s)
}

// ZeroGRT returns a zero-valued GRT.
func ZeroGRT() GRT {
	return GRT{}
}

// ParseGRT parses a GRT amount from a string.
//
// Supported formats:
//   - "1.5 GRT" or "1.5GRT" - explicit GRT suffix, value is in GRT
//   - "1.5" - plain decimal without suffix, assumed to be GRT
//   - Empty string returns zero GRT
//
// All formats interpret the numeric value as GRT (not base units).
func ParseGRT(s string) (GRT, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return GRT{}, nil
	}

	// Check for GRT suffix (case-insensitive)
	if strings.HasSuffix(strings.ToUpper(s), "GRT") {
		s = strings.TrimSpace(s[:len(s)-3])
	}

	return parseDecimalToGRT(s)
}

// parseDecimalToGRT parses a decimal string as GRT and converts to base units.
func parseDecimalToGRT(decimal string) (GRT, error) {
	decimal = strings.TrimSpace(decimal)
	if decimal == "" {
		return GRT{}, nil
	}

	// Handle negative numbers
	negative := false
	if strings.HasPrefix(decimal, "-") {
		negative = true
		decimal = decimal[1:]
	}

	// Split by decimal point
	parts := strings.Split(decimal, ".")
	if len(parts) > 2 {
		return GRT{}, fmt.Errorf("invalid decimal format: %s", decimal)
	}

	// Parse integer part
	intPart := parts[0]
	if intPart == "" {
		intPart = "0"
	}

	intValue, ok := new(big.Int).SetString(intPart, 10)
	if !ok {
		return GRT{}, fmt.Errorf("invalid integer part: %s", intPart)
	}

	// Convert integer part to base units (multiply by 10^18)
	raw := new(big.Int).Mul(intValue, grtBaseBig)

	// Handle fractional part
	if len(parts) == 2 {
		fracPart := parts[1]
		// Pad or truncate to 18 decimals
		if len(fracPart) > GRTDecimals {
			fracPart = fracPart[:GRTDecimals]
		} else {
			fracPart = fracPart + strings.Repeat("0", GRTDecimals-len(fracPart))
		}

		fracValue, ok := new(big.Int).SetString(fracPart, 10)
		if !ok {
			return GRT{}, fmt.Errorf("invalid fractional part: %s", fracPart)
		}
		raw.Add(raw, fracValue)
	}

	if negative {
		raw.Neg(raw)
	}

	// Convert to uint256
	v, overflow := uint256.FromBig(raw)
	if overflow {
		return GRT{}, fmt.Errorf("value overflow: %s", decimal)
	}

	return GRT{raw: *v}, nil
}

// Raw returns a pointer to the underlying uint256.Int value in base units.
// The returned pointer references the internal value - do not mutate.
func (g *GRT) Raw() *uint256.Int {
	return &g.raw
}

// BigInt returns the value as a big.Int in base units.
func (g GRT) BigInt() *big.Int {
	return g.raw.ToBig()
}

// IsZero returns true if the GRT value is zero.
func (g GRT) IsZero() bool {
	return g.raw.IsZero()
}

// Cmp compares g and other and returns:
//
//	-1 if g <  other
//	 0 if g == other
//	+1 if g >  other
func (g *GRT) Cmp(other *GRT) int {
	return g.raw.Cmp(&other.raw)
}

// Add returns g + other.
func (g *GRT) Add(other *GRT) GRT {
	var result uint256.Int
	result.Add(&g.raw, &other.raw)
	return GRT{raw: result}
}

// Sub returns g - other. Returns zero if result would be negative.
func (g *GRT) Sub(other *GRT) GRT {
	if g.raw.Cmp(&other.raw) < 0 {
		return GRT{}
	}
	var result uint256.Int
	result.Sub(&g.raw, &other.raw)
	return GRT{raw: result}
}

// Mul returns g * n where n is a multiplier (e.g., quantity).
func (g *GRT) Mul(n uint64) GRT {
	var result uint256.Int
	result.Mul(&g.raw, uint256.NewInt(n))
	return GRT{raw: result}
}

// String returns the GRT value as a decimal string with " GRT" suffix.
// This is the canonical serialization format.
func (g *GRT) String() string {
	return g.ToDecimalString() + " GRT"
}

// ToDecimalString returns the GRT value as a decimal string without suffix.
// Examples: "1.5", "0.000001", "1000"
func (g *GRT) ToDecimalString() string {
	if g.raw.IsZero() {
		return "0"
	}

	// Convert to big.Int for division
	raw := g.raw.ToBig()

	// Divide by 10^18 to get GRT value
	grt := new(big.Int).Div(raw, grtBaseBig)
	remainder := new(big.Int).Mod(raw, grtBaseBig)

	if remainder.Sign() == 0 {
		return grt.String()
	}

	// Format with fractional part (pad to 18 digits, then trim trailing zeros)
	// Note: big.Int implements fmt.Formatter, so %018d works correctly
	fracStr := fmt.Sprintf("%018d", remainder)
	fracStr = strings.TrimRight(fracStr, "0")
	return fmt.Sprintf("%s.%s", grt.String(), fracStr)
}

// MarshalText implements encoding.TextMarshaler.
// Format: "<decimal> GRT" (e.g., "1.5 GRT")
func (g GRT) MarshalText() ([]byte, error) {
	return []byte(g.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// Accepts formats: "1.5 GRT", "1.5GRT", "1.5" (assumed GRT)
func (g *GRT) UnmarshalText(text []byte) error {
	parsed, err := ParseGRT(string(text))
	if err != nil {
		return err
	}
	*g = parsed
	return nil
}

// MarshalJSON implements json.Marshaler.
// Serializes as a JSON string: "1.5 GRT"
func (g GRT) MarshalJSON() ([]byte, error) {
	return []byte(`"` + g.String() + `"`), nil
}

// UnmarshalJSON implements json.Unmarshaler.
// Accepts JSON string formats: "1.5 GRT", "1.5GRT", "1.5"
func (g *GRT) UnmarshalJSON(data []byte) error {
	// Remove quotes
	s := string(data)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	return g.UnmarshalText([]byte(s))
}

// MarshalYAML implements yaml.Marshaler.
// Serializes as: "1.5 GRT"
func (g GRT) MarshalYAML() (any, error) {
	return g.String(), nil
}

// UnmarshalYAML implements yaml.Unmarshaler.
// Accepts formats: "1.5 GRT", "1.5GRT", "1.5"
func (g *GRT) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	return g.UnmarshalText([]byte(s))
}

// PricingConfig holds the pricing configuration for a data service.
// Prices are specified in GRT.
type PricingConfig struct {
	// PricePerBlock is the price per processed block in GRT
	PricePerBlock GRT `yaml:"price_per_block" json:"price_per_block"`
	// PricePerByte is the price per byte transferred in GRT
	PricePerByte GRT `yaml:"price_per_byte" json:"price_per_byte"`
}

// CalculateUsageCost calculates the total cost for given usage.
func (c *PricingConfig) CalculateUsageCost(blocksProcessed, bytesTransferred uint64) GRT {
	blockCost := c.PricePerBlock.Mul(blocksProcessed)
	byteCost := c.PricePerByte.Mul(bytesTransferred)
	return blockCost.Add(&byteCost)
}

// DefaultPricingConfig returns a default pricing configuration.
// Based on: $3/million blocks, $175/TiB, at $0.02602/GRT
// Default: 0.000115 GRT per block (~$3 per million blocks)
// Default: 0.0000000061 GRT per byte (~$175 per TiB)
func DefaultPricingConfig() *PricingConfig {
	blockPrice, _ := ParseGRT("0.000115 GRT")
	bytePrice, _ := ParseGRT("0.0000000061 GRT")

	return &PricingConfig{
		PricePerBlock: blockPrice,
		PricePerByte:  bytePrice,
	}
}
