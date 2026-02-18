package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/graphprotocol/substreams-data-service/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
	"github.com/streamingfast/eth-go"
	"google.golang.org/protobuf/proto"
)

// GRT token definition (18 decimals like ETH)
var GRTToken = &eth.Token{
	Name:     "Graph Token",
	Symbol:   "GRT",
	Decimals: 18,
}

var toolsCmd = Group(
	"tools",
	"Development and debugging tools",
	toolsRAVCmd,
)

var toolsRAVCmd = Group(
	"rav",
	"RAV (Receipt Aggregate Voucher) tools",
	toolsRAVCreateCmd,
)

var toolsRAVCreateCmd = Command(
	runToolsRAVCreate,
	"create",
	"Create a signed RAV for testing",
	Description(`
		Creates a signed RAV (Receipt Aggregate Voucher) that can be used to
		authenticate requests to a provider.

		The output is a base64-encoded protobuf that can be used as the
		x-sds-rav header value.

		Example usage:
		  sds tools rav create \
		    --payer=0xe90874856c339d5d3733c92ea5acadc6014b34d5 \
		    --service-provider=0xa6f1845e54b1d6a95319251f1ca775b4ad406cdf \
		    --data-service=0x37478fd2f5845e3664fe4155d74c00e1a4e7a5e2 \
		    --collector-address=0x1d01649b4f94722b55b5c3b3e10fe26cd90c1ba9 \
		    --signer-key=0xe4c2694501255921b6588519cfd36d4e86ddc4ce19ab1bc91d9c58057c040304 \
		    --value="1 GRT"

		Value formats:
		  - "1 GRT" or "1GRT" (1 GRT = 1e18)
		  - "0.5 GRT" (0.5 GRT = 5e17)
		  - "1000000000000000000" (raw, 18 decimals)

		Use as header:
		  grpcurl -H "x-sds-rav: <output>" ...
	`),
	Flags(func(flags *pflag.FlagSet) {
		flags.String("payer", "", "Payer address (required)")
		flags.String("service-provider", "", "Service provider address (required)")
		flags.String("data-service", "", "Data service contract address (required)")
		flags.String("collector-address", "", "Collector contract address for EIP-712 domain (required)")
		flags.Uint64("chain-id", 1337, "Chain ID for EIP-712 domain")
		flags.String("signer-key", "", "Private key to sign the RAV (hex, with or without 0x prefix) (required)")
		flags.String("value", "1 GRT", "Value aggregate: '10 GRT', '0.5GRT', or raw like '1000000000000000000'")
		flags.String("collection-id", "", "Collection ID (32 bytes hex). If empty, a random one is generated")
	}),
)

func runToolsRAVCreate(cmd *cobra.Command, args []string) error {
	payerHex := sflags.MustGetString(cmd, "payer")
	serviceProviderHex := sflags.MustGetString(cmd, "service-provider")
	dataServiceHex := sflags.MustGetString(cmd, "data-service")
	collectorHex := sflags.MustGetString(cmd, "collector-address")
	chainID := sflags.MustGetUint64(cmd, "chain-id")
	signerKeyHex := sflags.MustGetString(cmd, "signer-key")
	valueStr := sflags.MustGetString(cmd, "value")
	collectionIDHex := sflags.MustGetString(cmd, "collection-id")

	// Validate required fields
	cli.Ensure(payerHex != "", "--payer is required")
	cli.Ensure(serviceProviderHex != "", "--service-provider is required")
	cli.Ensure(dataServiceHex != "", "--data-service is required")
	cli.Ensure(collectorHex != "", "--collector-address is required")
	cli.Ensure(signerKeyHex != "", "--signer-key is required")

	// Parse addresses
	payer, err := eth.NewAddress(payerHex)
	cli.NoError(err, "invalid --payer address %q", payerHex)

	serviceProvider, err := eth.NewAddress(serviceProviderHex)
	cli.NoError(err, "invalid --service-provider address %q", serviceProviderHex)

	dataService, err := eth.NewAddress(dataServiceHex)
	cli.NoError(err, "invalid --data-service address %q", dataServiceHex)

	collector, err := eth.NewAddress(collectorHex)
	cli.NoError(err, "invalid --collector-address address %q", collectorHex)

	// Parse signer key
	signerKey, err := eth.NewPrivateKey(signerKeyHex)
	cli.NoError(err, "invalid --signer-key %q", signerKeyHex)

	// Parse value (supports "10 GRT", "0.5GRT", or raw)
	value, err := parseGRTValue(valueStr)
	cli.NoError(err, "invalid --value %q", valueStr)

	// Parse or generate collection ID
	var collectionID horizon.CollectionID
	if collectionIDHex != "" {
		h, err := eth.NewHash(collectionIDHex)
		cli.NoError(err, "invalid --collection-id %q", collectionIDHex)
		copy(collectionID[:], h)
	} else {
		// Generate random collection ID
		if _, err := rand.Read(collectionID[:]); err != nil {
			return fmt.Errorf("generating random collection ID: %w", err)
		}
	}

	// Create the RAV
	rav := &horizon.RAV{
		CollectionID:    collectionID,
		Payer:           payer,
		ServiceProvider: serviceProvider,
		DataService:     dataService,
		TimestampNs:     uint64(time.Now().UnixNano()),
		ValueAggregate:  value,
		Metadata:        nil,
	}

	// Create the EIP-712 domain
	domain := horizon.NewDomain(chainID, collector)

	// Sign the RAV
	signedRAV, err := horizon.Sign(domain, rav, signerKey)
	if err != nil {
		return fmt.Errorf("signing RAV: %w", err)
	}

	// Convert to proto
	protoSignedRAV := sidecar.HorizonSignedRAVToProto(signedRAV)

	// Encode as protobuf
	protoBytes, err := proto.Marshal(protoSignedRAV)
	if err != nil {
		return fmt.Errorf("marshaling proto: %w", err)
	}

	// Encode as base64
	base64Encoded := base64.StdEncoding.EncodeToString(protoBytes)

	// Print info
	fmt.Println("RAV Details:")
	fmt.Printf("  Collection ID:    %s\n", eth.Hash(collectionID[:]).Pretty())
	fmt.Printf("  Payer:            %s\n", payer.Pretty())
	fmt.Printf("  Service Provider: %s\n", serviceProvider.Pretty())
	fmt.Printf("  Data Service:     %s\n", dataService.Pretty())
	fmt.Printf("  Value Aggregate:  %s (raw: %s)\n", formatGRT(value), value.String())
	fmt.Printf("  Timestamp:        %d\n", rav.TimestampNs)
	fmt.Printf("  Signer:           %s\n", signerKey.PublicKey().Address().Pretty())
	fmt.Println()
	fmt.Println("EIP-712 Domain:")
	fmt.Printf("  Name:              %s\n", domain.Name)
	fmt.Printf("  Version:           %s\n", domain.Version)
	fmt.Printf("  Chain ID:          %d\n", chainID)
	fmt.Printf("  Verifying Contract: %s\n", collector.Pretty())
	fmt.Println()
	fmt.Println("Base64-encoded SignedRAV (for x-sds-rav header):")
	fmt.Println(base64Encoded)

	return nil
}

// parseGRTValue parses a value string that can be:
// - "10 GRT" or "10GRT" (with optional space)
// - "0.5 GRT" (decimal GRT)
// - "1000000000000000000" (raw, 18 decimals)
func parseGRTValue(s string) (*big.Int, error) {
	s = strings.TrimSpace(s)

	// Check for GRT suffix (case-insensitive)
	lower := strings.ToLower(s)
	if strings.HasSuffix(lower, "grt") {
		// Remove "grt" suffix and trim
		numStr := strings.TrimSpace(s[:len(s)-3])
		return parseDecimalToBigInt(numStr, GRTToken.Decimals)
	}

	// Try parsing as raw integer
	value, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("invalid value: must be a number with optional 'GRT' suffix (e.g., '10 GRT', '0.5GRT') or raw integer")
	}
	return value, nil
}

// parseDecimalToBigInt parses a decimal string and converts to big.Int with given decimals
// e.g., "1.5" with decimals=18 -> 1500000000000000000
func parseDecimalToBigInt(s string, decimals uint) (*big.Int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty value")
	}

	// Split on decimal point
	parts := strings.Split(s, ".")
	if len(parts) > 2 {
		return nil, fmt.Errorf("invalid number: multiple decimal points")
	}

	// Parse integer part
	intPart, ok := new(big.Int).SetString(parts[0], 10)
	if !ok {
		return nil, fmt.Errorf("invalid integer part: %s", parts[0])
	}

	// Multiply by 10^decimals
	multiplier := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	result := new(big.Int).Mul(intPart, multiplier)

	// Handle fractional part if present
	if len(parts) == 2 {
		fracStr := parts[1]
		if len(fracStr) > int(decimals) {
			return nil, fmt.Errorf("too many decimal places: max %d", decimals)
		}

		// Pad with zeros to match decimals
		fracStr = fracStr + strings.Repeat("0", int(decimals)-len(fracStr))

		fracPart, ok := new(big.Int).SetString(fracStr, 10)
		if !ok {
			return nil, fmt.Errorf("invalid fractional part: %s", parts[1])
		}

		// Add fractional part (already scaled)
		result.Add(result, fracPart)
	}

	return result, nil
}

// formatGRT formats a raw value as GRT with up to 6 decimal places
func formatGRT(raw *big.Int) string {
	return GRTToken.AmountBig(raw).Format(6)
}

// Ensure proto import is used
var _ = commonv1.SignedRAV{}
