package main

import (
	"time"

	"github.com/graphprotocol/substreams-data-service/consumer/sidecar"
	"github.com/graphprotocol/substreams-data-service/horizon"
	sidecarlib "github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
	"github.com/streamingfast/eth-go"
	"github.com/streamingfast/logging"
)

var consumerLog, _ = logging.PackageLogger("consumer", "github.com/graphprotocol/substreams-data-service/cmd/sds@consumer")

var consumerSidecarCmd = Command(
	runConsumerSidecar,
	"sidecar",
	"Start the consumer sidecar gRPC server",
	Description(`
		Starts the consumer sidecar which handles payment session management
		and RAV signing for data consumers.

		The sidecar exposes:
		- ConsumerSidecarService: Called by the substreams client to manage payment sessions
	`),
	Flags(func(flags *pflag.FlagSet) {
		flags.String("grpc-listen-addr", ":9002", "gRPC server listen address")
		flags.String("signer-private-key", "", "Private key for signing RAVs (hex, required)")
		flags.Uint64("chain-id", 1337, "Chain ID for EIP-712 domain")
		flags.String("collector-address", "", "Collector contract address for EIP-712 domain (required)")
		flags.String("oracle-endpoint", "", "Oracle endpoint used for provider discovery when no direct provider override is supplied")
		flags.Bool("plaintext", false, "Serve plaintext h2c instead of TLS (local/demo only)")
		flags.String("tls-cert-file", "", "Path to the TLS certificate PEM file")
		flags.String("tls-key-file", "", "Path to the TLS private key PEM file")
		flags.Duration("payment-session-roundtrip-timeout", 30*time.Second, "Timeout for a PaymentSession request/response roundtrip with the provider gateway")
	}),
)

func runConsumerSidecar(cmd *cobra.Command, args []string) error {
	listenAddr := sflags.MustGetString(cmd, "grpc-listen-addr")
	signerKeyHex := sflags.MustGetString(cmd, "signer-private-key")
	chainID := sflags.MustGetUint64(cmd, "chain-id")
	collectorHex := sflags.MustGetString(cmd, "collector-address")
	oracleEndpoint := sflags.MustGetString(cmd, "oracle-endpoint")
	plaintext := sflags.MustGetBool(cmd, "plaintext")
	tlsCertFile := sflags.MustGetString(cmd, "tls-cert-file")
	tlsKeyFile := sflags.MustGetString(cmd, "tls-key-file")
	paymentSessionRoundtripTimeout := sflags.MustGetDuration(cmd, "payment-session-roundtrip-timeout")

	cli.Ensure(signerKeyHex != "", "<signer-private-key> is required")
	signerKey, err := eth.NewPrivateKey(signerKeyHex)
	cli.NoError(err, "invalid <signer-private-key> %q (expected 32-byte hex, with or without 0x prefix)", signerKeyHex)

	cli.Ensure(collectorHex != "", "<collector-address> is required")
	collectorAddr, err := eth.NewAddress(collectorHex)
	cli.NoError(err, "invalid <collector-address> %q", collectorHex)

	transportConfig := sidecarlib.ServerTransportConfig{
		Plaintext:   plaintext,
		TLSCertFile: tlsCertFile,
		TLSKeyFile:  tlsKeyFile,
	}
	cli.NoError(transportConfig.Validate("consumer sidecar"), "invalid transport configuration")

	config := &sidecar.Config{
		ListenAddr:                     listenAddr,
		SignerKey:                      signerKey,
		Domain:                         horizon.NewDomain(chainID, collectorAddr),
		OracleEndpoint:                 oracleEndpoint,
		PaymentSessionRoundtripTimeout: paymentSessionRoundtripTimeout,
		TransportConfig:                transportConfig,
	}

	app := cli.NewApplication(cmd.Context())

	sidecarServer := sidecar.New(config, consumerLog)
	app.SuperviseAndStart(sidecarServer)

	return app.WaitForTermination(consumerLog, 0*time.Second, 30*time.Second)
}
