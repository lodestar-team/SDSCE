package sidecar

import (
	"time"

	"github.com/streamingfast/eth-go"
)

const defaultIngressReportInterval = time.Second

type IngressConfig struct {
	Payer                        eth.Address
	Receiver                     *eth.Address
	DataService                  eth.Address
	ProviderControlPlaneEndpoint string
	ReportInterval               time.Duration
}

func (c *IngressConfig) effectiveReportInterval() time.Duration {
	if c == nil || c.ReportInterval <= 0 {
		return defaultIngressReportInterval
	}

	return c.ReportInterval
}
