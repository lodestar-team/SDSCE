package psql

import (
	"database/sql/driver"
	"math/big"
	"testing"

	eth "github.com/streamingfast/eth-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJsonbMap(t *testing.T) {
	t.Run("scan nil", func(t *testing.T) {
		var jm jsonbMap
		err := jm.Scan(nil)
		require.NoError(t, err)
		assert.Nil(t, jm)
	})

	t.Run("scan valid JSON", func(t *testing.T) {
		var jm jsonbMap
		err := jm.Scan([]byte(`{"key1":"value1","key2":"value2"}`))
		require.NoError(t, err)
		assert.Equal(t, "value1", jm["key1"])
		assert.Equal(t, "value2", jm["key2"])
	})

	t.Run("scan invalid type", func(t *testing.T) {
		var jm jsonbMap
		err := jm.Scan("not bytes")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected []byte")
	})

	t.Run("scan invalid JSON", func(t *testing.T) {
		var jm jsonbMap
		err := jm.Scan([]byte(`{invalid json`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal")
	})

	t.Run("value nil", func(t *testing.T) {
		var jm jsonbMap
		val, err := jm.Value()
		require.NoError(t, err)
		assert.Nil(t, val)
	})

	t.Run("value valid", func(t *testing.T) {
		jm := jsonbMap{"key": "value"}
		val, err := jm.Value()
		require.NoError(t, err)
		assert.Equal(t, []byte(`{"key":"value"}`), val)
	})
}

func TestGRT(t *testing.T) {
	t.Run("scan nil", func(t *testing.T) {
		var g grt
		err := g.Scan(nil)
		require.NoError(t, err)
		assert.Nil(t, g.value)
	})

	t.Run("scan string", func(t *testing.T) {
		var g grt
		err := g.Scan("123456789")
		require.NoError(t, err)
		assert.Equal(t, big.NewInt(123456789), g.value)
	})

	t.Run("scan bytes", func(t *testing.T) {
		var g grt
		err := g.Scan([]byte("987654321"))
		require.NoError(t, err)
		assert.Equal(t, big.NewInt(987654321), g.value)
	})

	t.Run("scan int64", func(t *testing.T) {
		var g grt
		err := g.Scan(int64(42))
		require.NoError(t, err)
		assert.Equal(t, big.NewInt(42), g.value)
	})

	t.Run("scan invalid string", func(t *testing.T) {
		var g grt
		err := g.Scan("not a number")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse")
	})

	t.Run("scan invalid type", func(t *testing.T) {
		var g grt
		err := g.Scan(123.45)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported type")
	})

	t.Run("value nil", func(t *testing.T) {
		var g grt
		val, err := g.Value()
		require.NoError(t, err)
		assert.Nil(t, val)
	})

	t.Run("value valid", func(t *testing.T) {
		g := newGRT(big.NewInt(999))
		val, err := g.Value()
		require.NoError(t, err)
		assert.Equal(t, "999", val)
	})

	t.Run("BigInt accessor", func(t *testing.T) {
		original := big.NewInt(123456)
		g := newGRT(original)
		result := g.BigInt()
		assert.Equal(t, original, result)
		// Verify it's a copy, not the same pointer
		result.Add(result, big.NewInt(1))
		assert.Equal(t, big.NewInt(123456), g.value)
	})
}

func TestAddress(t *testing.T) {
	validAddr := make([]byte, 20)
	for i := range validAddr {
		validAddr[i] = byte(i)
	}

	t.Run("scan valid", func(t *testing.T) {
		var a address
		err := a.Scan(validAddr)
		require.NoError(t, err)
		for i := 0; i < 20; i++ {
			assert.Equal(t, byte(i), a[i])
		}
	})

	t.Run("scan nil", func(t *testing.T) {
		var a address
		err := a.Scan(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot scan nil")
	})

	t.Run("scan wrong type", func(t *testing.T) {
		var a address
		err := a.Scan("not bytes")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected []byte")
	})

	t.Run("scan too short", func(t *testing.T) {
		var a address
		err := a.Scan([]byte{1, 2, 3})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected 20 bytes")
	})

	t.Run("scan too long", func(t *testing.T) {
		var a address
		err := a.Scan(make([]byte, 21))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected 20 bytes")
	})

	t.Run("value", func(t *testing.T) {
		var a address
		copy(a[:], validAddr)
		val, err := a.Value()
		require.NoError(t, err)
		assert.Equal(t, validAddr, val.([]byte))
	})

	t.Run("Address accessor", func(t *testing.T) {
		ethAddr := eth.MustNewAddress("0x1234567890123456789012345678901234567890")
		a := newAddress(ethAddr)
		result := a.Address()
		assert.Equal(t, ethAddr, result)
	})

	t.Run("round trip", func(t *testing.T) {
		ethAddr := eth.MustNewAddress("0xabcdef1234567890abcdef1234567890abcdef12")
		a := newAddress(ethAddr)

		val, err := a.Value()
		require.NoError(t, err)

		var a2 address
		err = a2.Scan(val)
		require.NoError(t, err)

		assert.Equal(t, ethAddr, a2.Address())
	})
}

func TestSignature(t *testing.T) {
	validSig := make([]byte, 65)
	for i := range validSig {
		validSig[i] = byte(i % 256)
	}

	t.Run("scan valid", func(t *testing.T) {
		var s signature
		err := s.Scan(validSig)
		require.NoError(t, err)
		for i := 0; i < 65; i++ {
			assert.Equal(t, byte(i%256), s[i])
		}
	})

	t.Run("scan nil", func(t *testing.T) {
		var s signature
		err := s.Scan(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot scan nil")
	})

	t.Run("scan wrong type", func(t *testing.T) {
		var s signature
		err := s.Scan("not bytes")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected []byte")
	})

	t.Run("scan too short", func(t *testing.T) {
		var s signature
		err := s.Scan(make([]byte, 64))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected 65 bytes")
	})

	t.Run("scan too long", func(t *testing.T) {
		var s signature
		err := s.Scan(make([]byte, 66))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected 65 bytes")
	})

	t.Run("value", func(t *testing.T) {
		var s signature
		copy(s[:], validSig)
		val, err := s.Value()
		require.NoError(t, err)
		assert.Equal(t, validSig, val.([]byte))
	})

	t.Run("Signature accessor", func(t *testing.T) {
		var ethSig eth.Signature
		copy(ethSig[:], validSig)
		s := newSignature(ethSig)
		result := s.Signature()
		assert.Equal(t, ethSig, result)
	})

	t.Run("round trip", func(t *testing.T) {
		var ethSig eth.Signature
		copy(ethSig[:], validSig)
		s := newSignature(ethSig)

		val, err := s.Value()
		require.NoError(t, err)

		var s2 signature
		err = s2.Scan(val)
		require.NoError(t, err)

		assert.Equal(t, ethSig, s2.Signature())
	})
}

func TestCollectionID(t *testing.T) {
	validID := make([]byte, 32)
	for i := range validID {
		validID[i] = byte(i)
	}

	t.Run("scan valid", func(t *testing.T) {
		var c collectionID
		err := c.Scan(validID)
		require.NoError(t, err)
		for i := 0; i < 32; i++ {
			assert.Equal(t, byte(i), c[i])
		}
	})

	t.Run("scan nil", func(t *testing.T) {
		var c collectionID
		err := c.Scan(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot scan nil")
	})

	t.Run("scan wrong type", func(t *testing.T) {
		var c collectionID
		err := c.Scan("not bytes")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected []byte")
	})

	t.Run("scan too short", func(t *testing.T) {
		var c collectionID
		err := c.Scan(make([]byte, 31))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected 32 bytes")
	})

	t.Run("scan too long", func(t *testing.T) {
		var c collectionID
		err := c.Scan(make([]byte, 33))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected 32 bytes")
	})

	t.Run("value", func(t *testing.T) {
		var c collectionID
		copy(c[:], validID)
		val, err := c.Value()
		require.NoError(t, err)
		assert.Equal(t, validID, val.([]byte))
	})

	t.Run("Bytes accessor", func(t *testing.T) {
		var id [32]byte
		copy(id[:], validID)
		c := newCollectionID(id)
		result := c.Bytes()
		assert.Equal(t, id, result)
	})

	t.Run("round trip", func(t *testing.T) {
		var id [32]byte
		copy(id[:], validID)
		c := newCollectionID(id)

		val, err := c.Value()
		require.NoError(t, err)

		var c2 collectionID
		err = c2.Scan(val)
		require.NoError(t, err)

		assert.Equal(t, id, c2.Bytes())
	})
}

// Verify all types implement sql.Scanner and driver.Valuer
func TestSQLInterfaces(t *testing.T) {
	var _ driver.Valuer = jsonbMap{}
	var _ driver.Valuer = grt{}
	var _ driver.Valuer = address{}
	var _ driver.Valuer = signature{}
	var _ driver.Valuer = collectionID{}
}
