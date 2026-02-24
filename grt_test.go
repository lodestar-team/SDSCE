package sds

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestParseGRT(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedRaw string // base units (18 decimals)
		wantErr     bool
	}{
		{
			name:        "empty string",
			input:       "",
			expectedRaw: "0",
		},
		{
			name:        "zero",
			input:       "0",
			expectedRaw: "0",
		},
		{
			name:        "one GRT plain",
			input:       "1",
			expectedRaw: "1000000000000000000",
		},
		{
			name:        "one GRT with suffix",
			input:       "1 GRT",
			expectedRaw: "1000000000000000000",
		},
		{
			name:        "one GRT no space",
			input:       "1GRT",
			expectedRaw: "1000000000000000000",
		},
		{
			name:        "lowercase grt suffix",
			input:       "1 grt",
			expectedRaw: "1000000000000000000",
		},
		{
			name:        "half GRT",
			input:       "0.5",
			expectedRaw: "500000000000000000",
		},
		{
			name:        "half GRT with suffix",
			input:       "0.5 GRT",
			expectedRaw: "500000000000000000",
		},
		{
			name:        "one millionth GRT",
			input:       "0.000001",
			expectedRaw: "1000000000000",
		},
		{
			name:        "one millionth GRT with suffix",
			input:       "0.000001 GRT",
			expectedRaw: "1000000000000",
		},
		{
			name:        "very small price",
			input:       "0.0000000001",
			expectedRaw: "100000000",
		},
		{
			name:        "large value",
			input:       "1000000 GRT",
			expectedRaw: "1000000000000000000000000",
		},
		{
			name:        "decimal with trailing zeros",
			input:       "1.500000 GRT",
			expectedRaw: "1500000000000000000",
		},
		{
			name:        "whitespace handling",
			input:       "  1.5  GRT  ",
			expectedRaw: "1500000000000000000",
		},
		{
			name:    "invalid format",
			input:   "not.a.number",
			wantErr: true,
		},
		{
			name:    "multiple decimal points",
			input:   "1.2.3",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grt, err := ParseGRT(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			expected, ok := new(big.Int).SetString(tt.expectedRaw, 10)
			require.True(t, ok, "invalid expected value: %s", tt.expectedRaw)
			assert.Equal(t, expected.String(), grt.BigInt().String())
		})
	}
}

func TestGRT_ToDecimalString(t *testing.T) {
	tests := []struct {
		name     string
		rawUnits string
		expected string
	}{
		{
			name:     "zero",
			rawUnits: "0",
			expected: "0",
		},
		{
			name:     "one GRT",
			rawUnits: "1000000000000000000",
			expected: "1",
		},
		{
			name:     "half GRT",
			rawUnits: "500000000000000000",
			expected: "0.5",
		},
		{
			name:     "one millionth GRT",
			rawUnits: "1000000000000",
			expected: "0.000001",
		},
		{
			name:     "very small",
			rawUnits: "1",
			expected: "0.000000000000000001",
		},
		{
			name:     "large value",
			rawUnits: "1000000000000000000000000",
			expected: "1000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, ok := new(big.Int).SetString(tt.rawUnits, 10)
			require.True(t, ok)

			grt := NewGRTFromBigInt(raw)
			assert.Equal(t, tt.expected, grt.ToDecimalString())
		})
	}
}

func TestGRT_String(t *testing.T) {
	grt, err := ParseGRT("1.5")
	require.NoError(t, err)
	assert.Equal(t, "1.5 GRT", grt.String())
}

func TestGRT_Arithmetic(t *testing.T) {
	t.Run("Add", func(t *testing.T) {
		a, _ := ParseGRT("1.5 GRT")
		b, _ := ParseGRT("0.5 GRT")
		result := a.Add(&b)
		assert.Equal(t, "2 GRT", result.String())
	})

	t.Run("Sub", func(t *testing.T) {
		a, _ := ParseGRT("2 GRT")
		b, _ := ParseGRT("0.5 GRT")
		result := a.Sub(&b)
		assert.Equal(t, "1.5 GRT", result.String())
	})

	t.Run("Sub underflow returns zero", func(t *testing.T) {
		a, _ := ParseGRT("1 GRT")
		b, _ := ParseGRT("2 GRT")
		result := a.Sub(&b)
		assert.Equal(t, "0 GRT", result.String())
	})

	t.Run("Mul", func(t *testing.T) {
		price, _ := ParseGRT("0.000001 GRT")
		// 1 million blocks at 0.000001 GRT/block = 1 GRT
		result := price.Mul(1000000)
		assert.Equal(t, "1 GRT", result.String())
	})

	t.Run("Cmp", func(t *testing.T) {
		a, _ := ParseGRT("1 GRT")
		b, _ := ParseGRT("2 GRT")
		c, _ := ParseGRT("1 GRT")

		assert.Equal(t, -1, a.Cmp(&b))
		assert.Equal(t, 1, b.Cmp(&a))
		assert.Equal(t, 0, a.Cmp(&c))
	})
}

func TestGRT_IsZero(t *testing.T) {
	assert.True(t, ZeroGRT().IsZero())

	zero, _ := ParseGRT("0")
	assert.True(t, zero.IsZero())

	nonZero, _ := ParseGRT("1")
	assert.False(t, nonZero.IsZero())

	var nilGRT GRT
	assert.True(t, nilGRT.IsZero())
}

func TestGRT_JSON(t *testing.T) {
	t.Run("Marshal", func(t *testing.T) {
		grt, _ := ParseGRT("1.5 GRT")
		data, err := json.Marshal(grt)
		require.NoError(t, err)
		assert.Equal(t, `"1.5 GRT"`, string(data))
	})

	t.Run("Unmarshal with suffix", func(t *testing.T) {
		var grt GRT
		err := json.Unmarshal([]byte(`"1.5 GRT"`), &grt)
		require.NoError(t, err)
		assert.Equal(t, "1.5 GRT", grt.String())
	})

	t.Run("Unmarshal without suffix", func(t *testing.T) {
		var grt GRT
		err := json.Unmarshal([]byte(`"1.5"`), &grt)
		require.NoError(t, err)
		assert.Equal(t, "1.5 GRT", grt.String())
	})

	t.Run("Roundtrip", func(t *testing.T) {
		original, _ := ParseGRT("0.000001 GRT")
		data, err := json.Marshal(original)
		require.NoError(t, err)

		var decoded GRT
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, original.BigInt().String(), decoded.BigInt().String())
	})
}

func TestGRT_YAML(t *testing.T) {
	t.Run("Marshal", func(t *testing.T) {
		grt, _ := ParseGRT("1.5 GRT")
		data, err := yaml.Marshal(grt)
		require.NoError(t, err)
		assert.Equal(t, "1.5 GRT\n", string(data))
	})

	t.Run("Unmarshal with suffix", func(t *testing.T) {
		var grt GRT
		err := yaml.Unmarshal([]byte(`"1.5 GRT"`), &grt)
		require.NoError(t, err)
		assert.Equal(t, "1.5 GRT", grt.String())
	})

	t.Run("Unmarshal without suffix", func(t *testing.T) {
		var grt GRT
		err := yaml.Unmarshal([]byte(`"0.000001"`), &grt)
		require.NoError(t, err)
		assert.Equal(t, "0.000001 GRT", grt.String())
	})

	t.Run("Unmarshal unquoted", func(t *testing.T) {
		var grt GRT
		err := yaml.Unmarshal([]byte(`0.5 GRT`), &grt)
		require.NoError(t, err)
		assert.Equal(t, "0.5 GRT", grt.String())
	})

	t.Run("Roundtrip", func(t *testing.T) {
		original, _ := ParseGRT("0.0000000001 GRT")
		data, err := yaml.Marshal(original)
		require.NoError(t, err)

		var decoded GRT
		err = yaml.Unmarshal(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, original.BigInt().String(), decoded.BigInt().String())
	})
}

func TestGRT_NewFromUint256(t *testing.T) {
	raw := uint256.NewInt(1000000000000000000) // 1 GRT in base units
	grt := NewGRTFromUint256(raw)
	assert.Equal(t, "1 GRT", grt.String())

	// Verify it's a copy (value semantics)
	raw.SetUint64(0)
	assert.Equal(t, "1 GRT", grt.String())
}

func TestGRT_NewFromBigInt(t *testing.T) {
	raw, _ := new(big.Int).SetString("1500000000000000000", 10) // 1.5 GRT
	grt := NewGRTFromBigInt(raw)
	assert.Equal(t, "1.5 GRT", grt.String())
}

func TestGRT_Raw(t *testing.T) {
	grt, _ := ParseGRT("1 GRT")
	raw := grt.Raw()

	// Verify we get the correct value
	expected := uint256.NewInt(1000000000000000000)
	assert.True(t, raw.Eq(expected))
}
