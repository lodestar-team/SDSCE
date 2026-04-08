package impl

import (
	"context"
	"fmt"
	"time"

	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/graphprotocol/substreams-data-service/provider/auth"
	"github.com/graphprotocol/substreams-data-service/provider/gateway"
	"github.com/graphprotocol/substreams-data-service/provider/plugin"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/graphprotocol/substreams-data-service/provider/session"
	"github.com/graphprotocol/substreams-data-service/provider/usage"
	sidecarlib "github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
	"github.com/streamingfast/eth-go"
	"github.com/streamingfast/logging"
)

var providerLog, _ = logging.PackageLogger("provider", "github.com/graphprotocol/substreams-data-service/cmd/sds@provider")

// ProviderGateways holds both the Payment Gateway and Plugin Gateway servers
type ProviderGateways struct {
	PaymentGateway *gateway.Gateway
	PluginGateway  *plugin.PluginGateway
}

type pluginTransportFlags struct {
	Plaintext   bool
	TLSCertFile string
	TLSKeyFile  string
}

// Shutdown gracefully shuts down both gateways
func (g *ProviderGateways) Shutdown(err error) {
	if g.PaymentGateway != nil {
		g.PaymentGateway.Shutdown(err)
	}
	if g.PluginGateway != nil {
		g.PluginGateway.Shutdown(err)
	}
}

func resolvePluginTransportConfig(paymentTransportConfig sidecarlib.ServerTransportConfig, pluginFlags pluginTransportFlags) (sidecarlib.ServerTransportConfig, error) {
	if !pluginFlags.hasOverrides() {
		return paymentTransportConfig, nil
	}

	pluginTransportConfig := sidecarlib.ServerTransportConfig{
		Plaintext:   pluginFlags.Plaintext,
		TLSCertFile: pluginFlags.TLSCertFile,
		TLSKeyFile:  pluginFlags.TLSKeyFile,
	}
	if err := pluginTransportConfig.Validate("plugin gateway"); err != nil {
		return sidecarlib.ServerTransportConfig{}, err
	}

	return pluginTransportConfig, nil
}

func (f pluginTransportFlags) hasOverrides() bool {
	return f.Plaintext || f.TLSCertFile != "" || f.TLSKeyFile != ""
}

var ProviderGatewayCommand = Command(
	runProviderGateway,
	"gateway",
	"Start the provider payment gateway server",
	Description(`
		Starts the provider payment gateway which handles payment session management
		and RAV exchange for data providers.

		The gateway starts TWO separate servers with different security profiles:

		1. Payment Gateway (--grpc-listen-addr, default :9001)
		   - PUBLIC endpoint - should be exposed to the internet
		   - Called by consumer sidecars for session management and RAV exchange
		   - Handles payment flows and session lifecycle

		2. Plugin Gateway (--plugin-listen-addr, default :9003)
		   - PRIVATE endpoint - should NOT be publicly exposed
		   - Only for internal firehose-core sds:// plugin communication
		   - Provides Auth/Session/Usage services for the tier1 server
		   - Keep this on a private network or localhost only

		IMPORTANT: The Plugin Gateway should only be accessible by your own firehose-core
		instance(s). Never expose it publicly as it provides internal service APIs.

		Pricing configuration should be provided via a YAML file with the following format:
		  price_per_block: "0.000001"   # Price per processed block in GRT
		  price_per_byte: "0.0000000001" # Price per byte transferred in GRT
		  rav_request_threshold: "10 GRT" # Optional provider-side threshold for requesting a new RAV
	`),
	Flags(func(flags *pflag.FlagSet) {
		flags.String("grpc-listen-addr", ":9001", "Payment Gateway listen address (PUBLIC - consumer sidecars connect here)")
		flags.String("plugin-listen-addr", ":9003", "Plugin Gateway listen address (PRIVATE - only firehose-core sds:// plugins, keep internal)")
		flags.String("service-provider", "", "Service provider address (required)")
		flags.Uint64("chain-id", 1337, "Chain ID for EIP-712 domain")
		flags.String("collector-address", "", "Collector contract address for EIP-712 domain (required)")
		flags.String("escrow-address", "", "PaymentsEscrow contract address for balance queries (required)")
		flags.String("rpc-endpoint", "", "Ethereum RPC endpoint for on-chain queries (required)")
		flags.String("data-plane-endpoint", "", "Session-specific Substreams data-plane endpoint advertised by the provider handshake (required)")
		flags.String("pricing-config", "", "Path to pricing configuration YAML file (uses defaults if not provided)")
		flags.Bool("plaintext", false, "Serve plaintext h2c instead of TLS (local/demo only)")
		flags.String("tls-cert-file", "", "Path to the TLS certificate PEM file")
		flags.String("tls-key-file", "", "Path to the TLS private key PEM file")
		flags.Bool("plugin-plaintext", false, "Serve the Plugin Gateway as plaintext h2c instead of TLS (local/demo only)")
		flags.String("plugin-tls-cert-file", "", "Path to the Plugin Gateway TLS certificate PEM file")
		flags.String("plugin-tls-key-file", "", "Path to the Plugin Gateway TLS private key PEM file")
		flags.String("repository-dsn", "inmemory://", "Repository DSN (inmemory:// or psql://user:pass@host:port/dbname)")
	}),
)

// StartProviderGateway creates and starts both provider gateway servers in the background
// Returns a ProviderGateways instance which can be shut down with Shutdown(nil)
// Useful for testing where you want to run the gateways and control their lifecycle
//
// paymentListenAddr: Address for the Payment Gateway (consumer sidecars)
// pluginListenAddr: Address for the Plugin Gateway (firehose-core sds:// plugins)
func StartProviderGateway(
	ctx context.Context,
	paymentListenAddr string,
	pluginListenAddr string,
	serviceProviderAddr eth.Address,
	chainID uint64,
	collectorAddr eth.Address,
	escrowAddr eth.Address,
	rpcEndpoint string,
	dataPlaneEndpoint string,
	repositoryDSN string,
	paymentTransportConfig sidecarlib.ServerTransportConfig,
	pluginTransportConfig sidecarlib.ServerTransportConfig,
	pricingConfig *sidecarlib.PricingConfig,
) (*ProviderGateways, error) {
	if dataPlaneEndpoint == "" {
		return nil, fmt.Errorf("<data-plane-endpoint> is required")
	}

	// Create repository from DSN (shared between both gateways)
	repo, err := gateway.NewRepositoryFromDSN(ctx, repositoryDSN, providerLog)
	if err != nil {
		return nil, err
	}

	domain := horizon.NewDomain(chainID, collectorAddr)

	// Create Payment Gateway
	paymentConfig := &gateway.Config{
		ListenAddr:        paymentListenAddr,
		ServiceProvider:   serviceProviderAddr,
		Domain:            domain,
		CollectorAddr:     collectorAddr,
		EscrowAddr:        escrowAddr,
		RPCEndpoint:       rpcEndpoint,
		PricingConfig:     pricingConfig,
		DataPlaneEndpoint: dataPlaneEndpoint,
		Repository:        repo,
		TransportConfig:   paymentTransportConfig,
	}

	paymentGateway, err := gateway.New(paymentConfig, providerLog)
	if err != nil {
		return nil, fmt.Errorf("failed to create payment gateway: %w", err)
	}
	go paymentGateway.Run()

	// Create Plugin Gateway services
	var collectorQuerier auth.CollectorAuthorizer
	if rpcEndpoint != "" && collectorAddr != nil {
		collectorQuerier = sidecarlib.NewCollectorQuerier(rpcEndpoint, collectorAddr)
	}

	authService := auth.NewAuthService(serviceProviderAddr, domain, collectorQuerier, repo)
	usageService := usage.NewUsageService(repo, toRepositoryPricingConfig(pricingConfig), paymentGateway)
	sessionService := session.NewSessionService(repo, nil) // Use default quota config

	// Create Plugin Gateway
	pluginConfig := &plugin.PluginGatewayConfig{
		ListenAddr:      pluginListenAddr,
		AuthService:     authService,
		UsageService:    usageService,
		SessionService:  sessionService,
		TransportConfig: pluginTransportConfig,
	}

	pluginGateway := plugin.NewPluginGateway(pluginConfig, providerLog)
	go pluginGateway.Run()

	return &ProviderGateways{
		PaymentGateway: paymentGateway,
		PluginGateway:  pluginGateway,
	}, nil
}

func runProviderGateway(cmd *cobra.Command, args []string) error {
	paymentListenAddr := sflags.MustGetString(cmd, "grpc-listen-addr")
	pluginListenAddr := sflags.MustGetString(cmd, "plugin-listen-addr")
	serviceProviderHex := sflags.MustGetString(cmd, "service-provider")
	chainID := sflags.MustGetUint64(cmd, "chain-id")
	collectorHex := sflags.MustGetString(cmd, "collector-address")
	escrowHex := sflags.MustGetString(cmd, "escrow-address")
	rpcEndpoint := sflags.MustGetString(cmd, "rpc-endpoint")
	dataPlaneEndpoint := sflags.MustGetString(cmd, "data-plane-endpoint")
	pricingConfigPath := sflags.MustGetString(cmd, "pricing-config")
	plaintext := sflags.MustGetBool(cmd, "plaintext")
	tlsCertFile := sflags.MustGetString(cmd, "tls-cert-file")
	tlsKeyFile := sflags.MustGetString(cmd, "tls-key-file")
	pluginPlaintext := sflags.MustGetBool(cmd, "plugin-plaintext")
	pluginTLSCertFile := sflags.MustGetString(cmd, "plugin-tls-cert-file")
	pluginTLSKeyFile := sflags.MustGetString(cmd, "plugin-tls-key-file")
	repositoryDSN := sflags.MustGetString(cmd, "repository-dsn")

	cli.Ensure(serviceProviderHex != "", "<service-provider> is required")
	serviceProviderAddr, err := eth.NewAddress(serviceProviderHex)
	cli.NoError(err, "invalid <service-provider> %q", serviceProviderHex)

	cli.Ensure(collectorHex != "", "<collector-address> is required")
	collectorAddr, err := eth.NewAddress(collectorHex)
	cli.NoError(err, "invalid <collector-address> %q", collectorHex)

	cli.Ensure(escrowHex != "", "<escrow-address> is required")
	escrowAddr, err := eth.NewAddress(escrowHex)
	cli.NoError(err, "invalid <escrow-address> %q", escrowHex)

	cli.Ensure(rpcEndpoint != "", "<rpc-endpoint> is required")
	cli.Ensure(dataPlaneEndpoint != "", "<data-plane-endpoint> is required")

	transportConfig := sidecarlib.ServerTransportConfig{
		Plaintext:   plaintext,
		TLSCertFile: tlsCertFile,
		TLSKeyFile:  tlsKeyFile,
	}
	cli.NoError(transportConfig.Validate("provider gateway"), "invalid transport configuration")

	pluginTransportConfig, err := resolvePluginTransportConfig(transportConfig, pluginTransportFlags{
		Plaintext:   pluginPlaintext,
		TLSCertFile: pluginTLSCertFile,
		TLSKeyFile:  pluginTLSKeyFile,
	})
	cli.NoError(err, "invalid plugin gateway transport configuration")

	// Load provider pricing and RAV request configuration.
	providerPricingConfig := gateway.DefaultProviderPricingConfig()
	if pricingConfigPath != "" {
		providerPricingConfig, err = gateway.LoadProviderPricingConfig(pricingConfigPath)
		cli.NoError(err, "failed to load pricing config from %q", pricingConfigPath)
	}

	// Create repository from DSN (shared between both gateways)
	repo, err := gateway.NewRepositoryFromDSN(cmd.Context(), repositoryDSN, providerLog)
	cli.NoError(err, "failed to create repository from DSN %q", repositoryDSN)

	domain := horizon.NewDomain(chainID, collectorAddr)

	// Create Payment Gateway
	paymentConfig := &gateway.Config{
		ListenAddr:          paymentListenAddr,
		ServiceProvider:     serviceProviderAddr,
		Domain:              domain,
		CollectorAddr:       collectorAddr,
		EscrowAddr:          escrowAddr,
		RPCEndpoint:         rpcEndpoint,
		PricingConfig:       providerPricingConfig.ToPricingConfig(),
		RAVRequestThreshold: providerPricingConfig.RAVRequestThreshold,
		DataPlaneEndpoint:   dataPlaneEndpoint,
		TransportConfig:     transportConfig,
		Repository:          repo,
	}

	paymentGateway, err := gateway.New(paymentConfig, providerLog)
	cli.NoError(err, "failed to create payment gateway")

	// Create Plugin Gateway services
	var collectorQuerier auth.CollectorAuthorizer
	if rpcEndpoint != "" && collectorAddr != nil {
		collectorQuerier = sidecarlib.NewCollectorQuerier(rpcEndpoint, collectorAddr)
	}

	authService := auth.NewAuthService(serviceProviderAddr, domain, collectorQuerier, repo)
	usageService := usage.NewUsageService(repo, toRepositoryPricingConfig(providerPricingConfig.ToPricingConfig()), paymentGateway)
	sessionService := session.NewSessionService(repo, nil) // Use default quota config

	// Create Plugin Gateway
	pluginConfig := &plugin.PluginGatewayConfig{
		ListenAddr:      pluginListenAddr,
		AuthService:     authService,
		UsageService:    usageService,
		SessionService:  sessionService,
		TransportConfig: pluginTransportConfig,
	}

	pluginGateway := plugin.NewPluginGateway(pluginConfig, providerLog)

	app := cli.NewApplication(cmd.Context())

	// Supervise both gateways
	app.SuperviseAndStart(paymentGateway)
	app.SuperviseAndStart(pluginGateway)

	return app.WaitForTermination(providerLog, 0*time.Second, 30*time.Second)
}

func toRepositoryPricingConfig(pc *sidecarlib.PricingConfig) repository.PricingConfig {
	if pc == nil {
		pc = sidecarlib.DefaultPricingConfig()
	}

	return repository.PricingConfig{
		PricePerBlock: pc.PricePerBlock,
		PricePerByte:  pc.PricePerByte,
	}
}
