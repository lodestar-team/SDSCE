package main

import (
	"time"

	"github.com/graphprotocol/substreams-data-service/cmd/sds/impl"
	"github.com/spf13/pflag"
	. "github.com/streamingfast/cli"
)

var sinkGroup = Group(
	"sink",
	"Substreams sink commands",

	PersistentFlags(func(flags *pflag.FlagSet) {
		flags.String("consumer-sidecar-addr", "http://localhost:9002", "Consumer sidecar address")
		flags.String("provider-control-plane-endpoint", "", "Provider control-plane endpoint for payment session management (e.g., 'https://gateway:9001?insecure=true')")
		flags.String("payer-address", "", "Payer address (required)")
		flags.String("receiver-address", "", "Receiver/service provider address (required)")
		flags.String("data-service-address", "", "Data service contract address (required)")
		flags.Duration("report-interval", 1*time.Second, "Interval between usage reports")
	}),

	impl.SinkRunCommand,
)
