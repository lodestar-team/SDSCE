package sidecar

import "github.com/streamingfast/eth-go"

type IngressConfig struct {
	Payer                        eth.Address
	Receiver                     *eth.Address
	DataService                  eth.Address
	ProviderControlPlaneEndpoint string
}
