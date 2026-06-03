package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"connectrpc.com/connect"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1/providerv1connect"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const defaultProviderOperatorTimeout = 30 * time.Second

type providerOperatorClient struct {
	client providerv1connect.ProviderOperatorServiceClient
	token  string
}

var providerOperatorCmd = Group(
	"operator",
	"Provider operator inspection commands",
	providerOperatorSessionsCmd,
	providerOperatorRAVsCmd,
	providerOperatorCollectionsCmd,
	providerOperatorCollectCmd,
	providerOperatorCollectDaemonCmd,
)

var providerOperatorSessionsCmd = Group(
	"sessions",
	"Inspect provider sessions",
	providerOperatorSessionsListCmd,
	providerOperatorSessionsGetCmd,
)

var providerOperatorRAVsCmd = Group(
	"ravs",
	"Inspect accepted provider RAVs",
	providerOperatorRAVsListCmd,
	providerOperatorRAVsGetCmd,
)

var providerOperatorCollectionsCmd = Group(
	"collections",
	"Inspect provider collection lifecycle records",
	providerOperatorCollectionsListCmd,
	providerOperatorCollectionsGetCmd,
)

func addProviderOperatorFlags(flags *pflag.FlagSet) {
	flags.String("provider-endpoint", "", "Provider operator endpoint (required)")
	flags.String("operator-token-env", "", "Environment variable containing provider operator bearer token")
	flags.String("operator-token", "", "Provider operator bearer token; prefer --operator-token-env for shell history safety")
	flags.Bool("plaintext", false, "Use plaintext HTTP for local/dev provider operator endpoints")
	flags.Duration("timeout", defaultProviderOperatorTimeout, "Provider operator request timeout")
	flags.String("format", "text", "Output format: text or json")
}

func addProviderOperatorSessionFilters(flags *pflag.FlagSet) {
	flags.String("payer-address", "", "Filter by payer address")
	flags.String("receiver-address", "", "Filter by receiver/service provider address")
	flags.String("data-service-address", "", "Filter by data service address")
	flags.String("status", "", "Filter by session status: active, paused, or terminated")
	flags.String("funds-status", "", "Filter by last provider funds status: ok, insufficient, or unknown")
	flags.Bool("include-rav", false, "Include accepted RAV summary in session output")
	flags.Uint32("limit", 100, "Maximum records to return")
}

func addProviderOperatorRAVFilters(flags *pflag.FlagSet) {
	flags.String("session-id", "", "Filter by session ID")
	flags.String("payer-address", "", "Filter by payer address")
	flags.String("receiver-address", "", "Filter by receiver/service provider address")
	flags.String("data-service-address", "", "Filter by data service address")
	flags.String("collection-id", "", "Filter by collection ID hex")
	flags.Uint32("limit", 100, "Maximum records to return")
}

func addProviderOperatorCollectionFilters(flags *pflag.FlagSet) {
	flags.String("session-id", "", "Filter by session ID")
	flags.String("payer-address", "", "Filter by payer address")
	flags.String("receiver-address", "", "Filter by receiver/service provider address")
	flags.String("data-service-address", "", "Filter by data service address")
	flags.String("collection-id", "", "Filter by collection ID hex")
	flags.String("state", "", "Filter by lifecycle state: collectible, collect_pending, collected, or collect_failed_retryable")
	flags.Uint32("limit", 100, "Maximum records to return")
}

func withProviderOperatorClient(cmd *cobra.Command, fn func(context.Context, providerOperatorClient) error) error {
	client := providerOperatorClientFromFlags(cmd)
	ctx, cancel := providerOperatorContext(cmd)
	defer cancel()

	return fn(ctx, client)
}

func providerOperatorClientFromFlags(cmd *cobra.Command) providerOperatorClient {
	endpoint := requiredStringFlag(cmd, "provider-endpoint")
	token := providerOperatorTokenFromFlags(cmd)

	parsed, err := parseProviderOperatorEndpoint(endpoint, sflags.MustGetBool(cmd, "plaintext"))
	cli.NoError(err, "invalid --provider-endpoint %q", endpoint)

	client := providerv1connect.NewProviderOperatorServiceClient(parsed.HTTPClient(), parsed.URL)
	return providerOperatorClient{client: client, token: token}
}

func providerOperatorContext(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	timeout := sflags.MustGetDuration(cmd, "timeout")
	cli.Ensure(timeout > 0, "--timeout must be greater than 0")
	return context.WithTimeout(cmd.Context(), timeout)
}

func providerOperatorTokenFromFlags(cmd *cobra.Command) string {
	token := optionalStringFlag(cmd, "operator-token")
	tokenEnv := optionalStringFlag(cmd, "operator-token-env")
	value, err := resolveProviderOperatorToken(token, tokenEnv, os.LookupEnv)
	cli.NoError(err, "invalid provider operator token configuration")
	return value
}

func resolveProviderOperatorToken(token string, tokenEnv string, lookupEnv func(string) (string, bool)) (string, error) {
	if (token == "") == (tokenEnv == "") {
		return "", fmt.Errorf("exactly one of --operator-token or --operator-token-env is required")
	}
	if tokenEnv == "" {
		if strings.ContainsAny(token, " \t\r\n") {
			return "", fmt.Errorf("--operator-token contains whitespace and cannot be used as a bearer token")
		}
		return token, nil
	}

	value, ok := lookupEnv(tokenEnv)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("environment variable %s from --operator-token-env is empty or unset", tokenEnv)
	}
	if strings.ContainsAny(value, " \t\r\n") {
		return "", fmt.Errorf("environment variable %s from --operator-token-env contains whitespace and cannot be used as a bearer token", tokenEnv)
	}
	return value, nil
}

func providerOperatorFormat(cmd *cobra.Command) string {
	format := optionalStringFlag(cmd, "format")
	cli.Ensure(format == "text" || format == "json", "--format must be either %q or %q", "text", "json")
	return format
}

func parseProviderOperatorEndpoint(endpoint string, plaintext bool) (sidecar.ParsedEndpoint, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return sidecar.ParsedEndpoint{}, fmt.Errorf("empty endpoint")
	}

	hasScheme := strings.Contains(endpoint, "://")
	if plaintext && !hasScheme {
		endpoint = "http://" + endpoint
	}

	parsed := sidecar.ParseEndpoint(endpoint)
	if parsed.URL == "" {
		return sidecar.ParsedEndpoint{}, fmt.Errorf("empty endpoint")
	}

	u, err := url.Parse(parsed.URL)
	if err != nil {
		return sidecar.ParsedEndpoint{}, err
	}
	if u.Scheme == "http" && !plaintext {
		return sidecar.ParsedEndpoint{}, fmt.Errorf("http endpoint requires explicit --plaintext")
	}
	if plaintext && u.Scheme != "http" {
		return sidecar.ParsedEndpoint{}, fmt.Errorf("--plaintext requires an http endpoint")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return sidecar.ParsedEndpoint{}, fmt.Errorf("unsupported endpoint scheme %q", u.Scheme)
	}

	return parsed, nil
}

func providerOperatorRequest[T any](client providerOperatorClient, msg *T) *connect.Request[T] {
	req := connect.NewRequest(msg)
	req.Header().Set("Authorization", "Bearer "+client.token)
	return req
}

func printProtoJSON(msg proto.Message) error {
	out, err := protojson.MarshalOptions{
		Multiline:       true,
		Indent:          "  ",
		EmitUnpopulated: false,
	}.Marshal(msg)
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

func optionalAddressProtoFlag(cmd *cobra.Command, name string) *commonv1.Address {
	addr, ok := parseOptionalAddressFlag(cmd, name)
	if !ok {
		return nil
	}
	return &commonv1.Address{Bytes: addr.Bytes()}
}

func requiredAddressProtoFlag(cmd *cobra.Command, name string) *commonv1.Address {
	addr := parseAddressFlag(cmd, name)
	return &commonv1.Address{Bytes: addr.Bytes()}
}

func optionalCollectionIDFlag(cmd *cobra.Command, name string) []byte {
	raw := optionalStringFlag(cmd, name)
	if raw == "" {
		return nil
	}
	value, err := parseHexBytes(raw)
	cli.NoError(err, "invalid --%s %q", name, raw)
	cli.Ensure(len(value) == 32, "invalid --%s %q, expected 32 bytes", name, raw)
	return value
}

func requiredCollectionIDFlag(cmd *cobra.Command, name string) []byte {
	value := optionalCollectionIDFlag(cmd, name)
	cli.Ensure(len(value) != 0, "--%s is required", name)
	return value
}

func formatCollectionID(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	return "0x" + hex.EncodeToString(raw)
}

func formatProtoAddress(addr *commonv1.Address) string {
	if addr == nil {
		return ""
	}
	ethAddr, err := addr.ToEth()
	if err != nil {
		return ""
	}
	return ethAddr.Pretty()
}

func formatProtoGRT(value *commonv1.GRT) string {
	if value == nil {
		return ""
	}
	grt := value.ToNative()
	return fmt.Sprintf("%s (raw: %s)", grt.String(), grt.BigInt().String())
}

func printUsage(prefix string, usage *commonv1.Usage) {
	if usage == nil {
		return
	}
	fmt.Printf("%s_blocks_processed: %d\n", prefix, usage.GetBlocksProcessed())
	fmt.Printf("%s_bytes_transferred: %d\n", prefix, usage.GetBytesTransferred())
	fmt.Printf("%s_requests: %d\n", prefix, usage.GetRequests())
	fmt.Printf("%s_cost: %s\n", prefix, formatProtoGRT(usage.GetCost()))
}
