package main

import (
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/logging"
	"go.uber.org/zap"
)

var zlog, _ = logging.PackageLogger("sds", "github.com/graphprotocol/substreams-data-service/cmd/sds")
var Version = "dev"

func init() {
	logging.InstantiateLoggers(logging.WithDefaultLevel(zap.ErrorLevel))
}

func main() {
	Run(
		"sds",
		"Substreams Data Service CLI",
		ConfigureVersion(Version),
		OnCommandErrorLogAndExit(zlog),

		devenvCmd,

		Group(
			"demo",
			"Demo helpers (local/dev only)",
			demoSetupCmd,
			demoFlowCmd,
		),

		Group(
			"provider",
			"Provider-side commands",
			providerSidecarCmd,
			providerFakeOperatorCmd,
		),

		Group(
			"consumer",
			"Consumer-side commands",
			consumerSidecarCmd,
			consumerFakeClientCmd,
		),

		toolsCmd,
	)
}
