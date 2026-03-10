package artifacts

import (
	"encoding/json"
	"fmt"

	"github.com/streamingfast/eth-go"
)

// Artifact represents a compiled contract artifact with ABI and bytecode.
type Artifact struct {
	ABI      json.RawMessage `json:"abi"`
	Bytecode struct {
		Object string `json:"object"`
	} `json:"bytecode"`
}

// Load reads an embedded artifact by contract name.
func Load(name string) (*Artifact, error) {
	data, err := FS.ReadFile(name + ".json")
	if err != nil {
		return nil, fmt.Errorf("reading embedded artifact: %w", err)
	}

	var artifact Artifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return nil, fmt.Errorf("parsing artifact: %w", err)
	}

	return &artifact, nil
}

// LoadABI reads an embedded artifact and parses its ABI.
func LoadABI(name string) (*eth.ABI, error) {
	artifact, err := Load(name)
	if err != nil {
		return nil, err
	}

	abi, err := eth.ParseABIFromBytes(artifact.ABI)
	if err != nil {
		return nil, fmt.Errorf("parsing %s ABI: %w", name, err)
	}

	return abi, nil
}
