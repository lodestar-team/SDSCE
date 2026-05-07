package main

import (
	sds "github.com/graphprotocol/substreams-data-service"
	"github.com/graphprotocol/substreams-data-service/cmd/sds/impl"
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

		Group(
			"demo",
			"Demo helpers (local/dev only)",
			demoSetupCmd,
		),

		Group(
			"provider",
			"Provider-side commands",
			impl.ProviderGatewayCommand,
			providerOperatorCmd,
		),

		Group(
			"oracle",
			"Oracle/discovery commands",
			impl.OracleServeCommand,
		),

		Group(
			"consumer",
			"Consumer-side commands",
			consumerSidecarCmd,
			consumerFundingCmd,
			consumerSignerCmd,
		),

		Group(
			"tools",
			"Development and debugging tools",
			toolsRAVCmd,
		),
	)
}
