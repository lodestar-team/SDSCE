package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"time"

	sds "github.com/graphprotocol/substreams-data-service"
	"github.com/graphprotocol/substreams-data-service/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/graphprotocol/substreams-data-service/ui"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
	"github.com/streamingfast/eth-go"
	"google.golang.org/protobuf/proto"
)

var toolsRAVCmd = Group(
	"rav",
	"RAV (Receipt Aggregate Voucher) tools",
	toolsRAVCreateCmd,
	toolsRAVInspectCmd,
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

	// Parse value (supports "10 GRT", "0.5GRT", or plain decimal)
	value, err := sds.ParseGRT(valueStr)
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
		ValueAggregate:  value.BigInt(),
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
	fmt.Printf("  Value Aggregate:  %s (raw: %s)\n", value.String(), value.BigInt().String())
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

var toolsRAVInspectCmd = Command(
	runToolsRAVInspect,
	"inspect <input1> [<input2> ...]",
	"Inspect RAV(s) and display detailed information",
	Description(`
		Inspects one or more RAVs (Receipt Aggregate Vouchers) and displays
		detailed information including signature verification.

		Input formats supported:
		  - Base64-encoded protobuf (from x-sds-rav header)
		  - File path to a RAV file containing base64 or protobuf data
		  - "-" to read from stdin

		Examples:
		  # Inspect a base64-encoded RAV
		  sds tools rav inspect "Ckg..."

		  # Inspect multiple RAVs
		  sds tools rav inspect rav1.txt rav2.txt

		  # Inspect from stdin
		  echo "Ckg..." | sds tools rav inspect -

		  # With domain verification
		  sds tools rav inspect rav.txt \
		    --chain-id=1337 \
		    --collector-address=0x1d01649b4f94722b55b5c3b3e10fe26cd90c1ba9
	`),
	MinimumNArgs(1),
	Flags(func(flags *pflag.FlagSet) {
		flags.Uint64("chain-id", 0, "Chain ID for EIP-712 domain verification (optional)")
		flags.String("collector-address", "", "Collector contract address for EIP-712 verification (optional)")
	}),
)

func runToolsRAVInspect(cmd *cobra.Command, args []string) error {
	chainID := sflags.MustGetUint64(cmd, "chain-id")
	collectorHex := sflags.MustGetString(cmd, "collector-address")

	// Parse optional domain for verification
	var domain *horizon.Domain
	if chainID != 0 && collectorHex != "" {
		collector, err := eth.NewAddress(collectorHex)
		cli.NoError(err, "invalid --collector-address %q", collectorHex)
		domain = horizon.NewDomain(chainID, collector)
	}

	// Process each input
	for i, input := range args {
		if i > 0 {
			fmt.Println() // Separator between RAVs
		}

		if err := inspectRAV(input, domain); err != nil {
			return fmt.Errorf("failed to inspect RAV %d (%s): %w", i+1, input, err)
		}
	}

	return nil
}

func inspectRAV(input string, domain *horizon.Domain) error {
	// Read input (from file, stdin, or direct base64)
	var ravData []byte
	var err error

	if input == "-" {
		// Read from stdin
		ravData, err = readRAVFromStdin()
	} else if _, fileErr := os.Stat(input); fileErr == nil {
		// Read from file
		ravData, err = readRAVFromFile(input)
	} else {
		// Assume it's direct base64 input
		ravData, err = base64.StdEncoding.DecodeString(input)
		if err != nil {
			// Try RawURLEncoding
			ravData, err = base64.RawURLEncoding.DecodeString(input)
		}
	}

	if err != nil {
		return fmt.Errorf("failed to read RAV data: %w", err)
	}

	// Parse protobuf
	var protoRAV commonv1.SignedRAV
	if err := proto.Unmarshal(ravData, &protoRAV); err != nil {
		return fmt.Errorf("failed to unmarshal protobuf: %w", err)
	}

	// Convert to horizon RAV
	signedRAV, err := sidecar.ProtoSignedRAVToHorizon(&protoRAV)
	if err != nil {
		return fmt.Errorf("failed to convert RAV: %w", err)
	}

	// Display header
	fmt.Println(ui.Header("RAV Details"))

	// Display RAV fields
	fmt.Println(ui.Field("Collection ID", ui.StyleSignature.Render(eth.Hash(signedRAV.Message.CollectionID[:]).Pretty())))
	fmt.Println(ui.Field("Payer", ui.StyleAddress.Render(signedRAV.Message.Payer.Pretty())))
	fmt.Println(ui.Field("Service Provider", ui.StyleAddress.Render(signedRAV.Message.ServiceProvider.Pretty())))
	fmt.Println(ui.Field("Data Service", ui.StyleAddress.Render(signedRAV.Message.DataService.Pretty())))

	// Format value
	value := sds.NewGRTFromBigInt(signedRAV.Message.ValueAggregate)
	fmt.Println(ui.Field("Value Aggregate", ui.StyleValue.Render(value.String()+" ("+signedRAV.Message.ValueAggregate.String()+" wei)")))

	// Format timestamp
	timestampTime := time.Unix(0, int64(signedRAV.Message.TimestampNs))
	timestampStr := timestampTime.Format(time.RFC3339) + " (" + time.Since(timestampTime).Round(time.Second).String() + " ago)"
	fmt.Println(ui.Field("Timestamp", ui.StyleTimestamp.Render(timestampStr)))

	// Metadata
	if len(signedRAV.Message.Metadata) > 0 {
		fmt.Println(ui.Field("Metadata", ui.StyleDim.Render(string(signedRAV.Message.Metadata))))
	} else {
		fmt.Println(ui.Field("Metadata", ui.StyleDim.Render("(none)")))
	}

	// Signature section
	fmt.Println()
	fmt.Println(ui.Header("Signature"))
	fmt.Println(ui.Field("Signature", ui.StyleSignature.Render(eth.Hex(signedRAV.Signature[:]).Pretty())))

	// Recover signer
	var signerAddr eth.Address
	var signerErr error
	if domain != nil {
		signerAddr, signerErr = signedRAV.RecoverSigner(domain)
		if signerErr == nil {
			fmt.Println(ui.Field("Signer", ui.StyleAddress.Render(signerAddr.Pretty())+" "+ui.StyleSuccess.Render("✓ verified")))
		} else {
			fmt.Println(ui.Field("Signer", ui.StyleError.Render("✗ verification failed: "+signerErr.Error())))
		}
	} else {
		fmt.Println(ui.Field("Signer", ui.StyleWarning.Render("(verification skipped - provide --chain-id and --collector-address)")))
	}

	// Domain info
	if domain != nil {
		fmt.Println()
		fmt.Println(ui.Header("EIP-712 Domain"))
		fmt.Println(ui.Field("Name", domain.Name))
		fmt.Println(ui.Field("Version", domain.Version))
		fmt.Println(ui.Field("Chain ID", fmt.Sprintf("%d", domain.ChainID.Uint64())))
		fmt.Println(ui.Field("Verifying Contract", ui.StyleAddress.Render(domain.VerifyingContract.Pretty())))
	}

	return nil
}

func readRAVFromStdin() ([]byte, error) {
	data, err := os.ReadFile("/dev/stdin")
	if err != nil {
		return nil, err
	}

	// Try to decode as base64
	decoded, err := base64.StdEncoding.DecodeString(string(data))
	if err == nil {
		return decoded, nil
	}

	// Try RawURLEncoding
	decoded, err = base64.RawURLEncoding.DecodeString(string(data))
	if err == nil {
		return decoded, nil
	}

	// Assume it's already raw protobuf
	return data, nil
}

func readRAVFromFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Try to decode as base64
	decoded, err := base64.StdEncoding.DecodeString(string(data))
	if err == nil {
		return decoded, nil
	}

	// Try RawURLEncoding
	decoded, err = base64.RawURLEncoding.DecodeString(string(data))
	if err == nil {
		return decoded, nil
	}

	// Assume it's already raw protobuf
	return data, nil
}

// Ensure proto import is used
var _ = commonv1.SignedRAV{}
