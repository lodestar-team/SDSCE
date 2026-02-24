package sidecar

import (
	"fmt"
	"os"

	sds "github.com/graphprotocol/substreams-data-service"
	"gopkg.in/yaml.v3"
)

// PricingConfig is an alias to sds.PricingConfig.
// Kept for backwards compatibility.
type PricingConfig = sds.PricingConfig

// LoadPricingConfig loads pricing configuration from a YAML file.
func LoadPricingConfig(path string) (*PricingConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading pricing config: %w", err)
	}

	return ParsePricingConfig(data)
}

// ParsePricingConfig parses pricing configuration from YAML bytes.
func ParsePricingConfig(data []byte) (*PricingConfig, error) {
	var config PricingConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing pricing config: %w", err)
	}

	return &config, nil
}

// DefaultPricingConfig returns a default pricing configuration.
func DefaultPricingConfig() *PricingConfig {
	return sds.DefaultPricingConfig()
}
