package main

import (
	sds "github.com/graphprotocol/substreams-data-service"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/logging"
	"go.uber.org/zap"
)

var zlog, tracer = logging.PackageLogger("sds", "github.com/graphprotocol/substreams-data-service/cmd/sds")

func init() {
	logging.InstantiateLoggers(logging.WithDefaultLevel(zap.ErrorLevel))
}

func main() {
	Run(
		"sds",
		"Substreams Data Service CLI",
		ConfigureVersion(sds.Version),
		OnCommandErrorLogAndExit(zlog),

		devenvCmd,
		sinkGroup,

		Group(
			"provider",
			"Provider-side commands",
			providerGatewayCmd,
		),

		Group(
			"consumer",
			"Consumer-side commands",
			consumerSidecarCmd,
		),

		Group(
			"tools",
			"Development and debugging tools",
			toolsRAVCmd,
		),
	)
}
