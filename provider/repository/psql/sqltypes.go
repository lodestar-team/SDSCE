package psql

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math/big"

	eth "github.com/streamingfast/eth-go"
)

// mustValue converts a driver.Valuer to its database value, panicking on error.
// This is safe for our custom types since they should never error after Scan validation.
func mustValue(v driver.Valuer) driver.Value {
	val, err := v.Value()
	if err != nil {
		panic(fmt.Errorf("unexpected error converting value to driver.Value: %w", err))
	}
	return val
}

// jsonbMap wraps map[string]string for JSONB storage
type jsonbMap map[string]string

func (j *jsonbMap) Scan(value any) error {
	if value == nil {
		*j = nil
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("jsonbMap: expected []byte, got %T", value)
	}

	m := make(map[string]string)
	if err := json.Unmarshal(bytes, &m); err != nil {
		return fmt.Errorf("jsonbMap: failed to unmarshal: %w", err)
	}

	*j = m
	return nil
}

func (j jsonbMap) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}

	bytes, err := json.Marshal(j)
	if err != nil {
		return nil, fmt.Errorf("jsonbMap: failed to marshal: %w", err)
	}

	return bytes, nil
}

// grt wraps *big.Int for GRT values stored as NUMERIC
type grt struct {
	value *big.Int
}

func newGRT(value *big.Int) grt {
	if value == nil {
		return grt{value: big.NewInt(0)}
	}
	return grt{value: new(big.Int).Set(value)}
}

func (g *grt) Scan(value any) error {
	if value == nil {
		g.value = nil
		return nil
	}

	// PostgreSQL NUMERIC can come as string, []byte, or int64
	switch v := value.(type) {
	case string:
		bi := new(big.Int)
		if _, ok := bi.SetString(v, 10); !ok {
			return fmt.Errorf("grt: failed to parse string %q as big.Int", v)
		}
		g.value = bi
	case []byte:
		bi := new(big.Int)
		if _, ok := bi.SetString(string(v), 10); !ok {
			return fmt.Errorf("grt: failed to parse bytes %q as big.Int", string(v))
		}
		g.value = bi
	case int64:
		g.value = big.NewInt(v)
	default:
		return fmt.Errorf("grt: unsupported type %T", value)
	}

	return nil
}

func (g grt) Value() (driver.Value, error) {
	if g.value == nil {
		return nil, nil
	}
	return g.value.String(), nil
}

func (g grt) BigInt() *big.Int {
	if g.value == nil {
		return nil
	}
	return new(big.Int).Set(g.value)
}

// address wraps [20]byte for Ethereum addresses stored as BYTEA
type address [20]byte

func newAddress(addr eth.Address) address {
	var a address
	if len(addr) == 20 {
		copy(a[:], addr)
	}
	return a
}

func (a *address) Scan(value any) error {
	if value == nil {
		return fmt.Errorf("address: cannot scan nil")
	}

	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("address: expected []byte, got %T", value)
	}

	if len(bytes) != 20 {
		return fmt.Errorf("address: expected 20 bytes, got %d", len(bytes))
	}

	copy(a[:], bytes)
	return nil
}

func (a address) Value() (driver.Value, error) {
	return a[:], nil
}

func (a address) Address() eth.Address {
	// eth.Address is []byte, so create a copy
	addr := make(eth.Address, 20)
	copy(addr, a[:])
	return addr
}

// signature wraps [65]byte for ECDSA signatures stored as BYTEA
type signature [65]byte

func newSignature(sig eth.Signature) signature {
	var s signature
	copy(s[:], sig[:])
	return s
}

func (s *signature) Scan(value any) error {
	if value == nil {
		return fmt.Errorf("signature: cannot scan nil")
	}

	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("signature: expected []byte, got %T", value)
	}

	if len(bytes) != 65 {
		return fmt.Errorf("signature: expected 65 bytes, got %d", len(bytes))
	}

	copy(s[:], bytes)
	return nil
}

func (s signature) Value() (driver.Value, error) {
	return s[:], nil
}

func (s signature) Signature() eth.Signature {
	var sig eth.Signature
	copy(sig[:], s[:])
	return sig
}

// collectionID wraps [32]byte for collection identifiers stored as BYTEA
type collectionID [32]byte

func newCollectionID(id [32]byte) collectionID {
	var cid collectionID
	copy(cid[:], id[:])
	return cid
}

func (c *collectionID) Scan(value any) error {
	if value == nil {
		return fmt.Errorf("collectionID: cannot scan nil")
	}

	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("collectionID: expected []byte, got %T", value)
	}

	if len(bytes) != 32 {
		return fmt.Errorf("collectionID: expected 32 bytes, got %d", len(bytes))
	}

	copy(c[:], bytes)
	return nil
}

func (c collectionID) Value() (driver.Value, error) {
	return c[:], nil
}

func (c collectionID) Bytes() [32]byte {
	return c
}
