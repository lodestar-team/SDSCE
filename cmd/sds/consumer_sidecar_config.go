package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type consumerSidecarRuntimeFileConfig struct {
	PayerAddress                 string        `yaml:"payer_address"`
	ReceiverAddress              string        `yaml:"receiver_address"`
	DataServiceAddress           string        `yaml:"data_service_address"`
	OracleEndpoint               string        `yaml:"oracle_endpoint"`
	ProviderControlPlaneEndpoint string        `yaml:"provider_control_plane_endpoint"`
	IngressReportInterval        time.Duration `yaml:"ingress_report_interval"`
}

func loadConsumerSidecarRuntimeFileConfig(path string) (*consumerSidecarRuntimeFileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading consumer sidecar config: %w", err)
	}

	var cfg consumerSidecarRuntimeFileConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing consumer sidecar config: %w", err)
	}

	cfg.PayerAddress = strings.TrimSpace(cfg.PayerAddress)
	cfg.ReceiverAddress = strings.TrimSpace(cfg.ReceiverAddress)
	cfg.DataServiceAddress = strings.TrimSpace(cfg.DataServiceAddress)
	cfg.OracleEndpoint = strings.TrimSpace(cfg.OracleEndpoint)
	cfg.ProviderControlPlaneEndpoint = strings.TrimSpace(cfg.ProviderControlPlaneEndpoint)

	return &cfg, nil
}
