package impl

import (
	"time"

	"github.com/graphprotocol/substreams-data-service/oracle"
	sidecarlib "github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
	"github.com/streamingfast/logging"
)

var oracleLog, _ = logging.PackageLogger("oracle", "github.com/graphprotocol/substreams-data-service/cmd/sds@oracle")

var OracleServeCommand = Command(
	runOracleServe,
	"serve",
	"Start the standalone oracle discovery service",
	Description(`
		Starts the standalone SDS oracle used for provider discovery.

		The oracle is a separate, externally deployable component. Consumer sidecars
		query it with an already-normalized canonical network key and receive:

		- the eligible provider set
		- a deterministic recommended provider
		- canonical pricing for the network
		- the selected provider control-plane endpoint

		Whitelist and provider metadata remain deployment-managed through the oracle
		config file for MVP; this command does not expose a writable admin API.
	`),
	Flags(func(flags *pflag.FlagSet) {
		flags.String("grpc-listen-addr", ":9004", "Oracle listen address")
		flags.String("config", "", "Path to the oracle YAML config file (required)")
		flags.Bool("plaintext", false, "Serve plaintext h2c instead of TLS (local/dev only)")
		flags.String("tls-cert-file", "", "Path to the TLS certificate PEM file")
		flags.String("tls-key-file", "", "Path to the TLS private key PEM file")
	}),
)

func runOracleServe(cmd *cobra.Command, args []string) error {
	listenAddr := sflags.MustGetString(cmd, "grpc-listen-addr")
	configPath := sflags.MustGetString(cmd, "config")
	plaintext := sflags.MustGetBool(cmd, "plaintext")
	tlsCertFile := sflags.MustGetString(cmd, "tls-cert-file")
	tlsKeyFile := sflags.MustGetString(cmd, "tls-key-file")

	cli.Ensure(configPath != "", "<config> is required")

	transportConfig := sidecarlib.ServerTransportConfig{
		Plaintext:   plaintext,
		TLSCertFile: tlsCertFile,
		TLSKeyFile:  tlsKeyFile,
	}
	cli.NoError(transportConfig.Validate("oracle"), "invalid transport configuration")

	catalog, err := oracle.LoadCatalog(configPath)
	cli.NoError(err, "failed to load oracle config from %q", configPath)

	server := oracle.New(&oracle.Config{
		ListenAddr:      listenAddr,
		Catalog:         catalog,
		TransportConfig: transportConfig,
	}, oracleLog)

	app := cli.NewApplication(cmd.Context())
	app.SuperviseAndStart(server)

	return app.WaitForTermination(oracleLog, 0*time.Second, 30*time.Second)
}
