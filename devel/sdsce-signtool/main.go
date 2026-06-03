// Command sdsce-signtool emits the two EIP-712 artifacts the collect() flow needs,
// reusing SDSCE's own signing code so the rehearsal exercises the real encoders.
//
// It performs NO chain access — it only prints hex for `cast` to submit:
//
//	proof         -> GraphTallyCollector.authorizeSigner proof bytes
//	collect-data  -> the `data` argument for SubstreamsDataService.collect()
//	                 (ABI-encoded SignedRAV + dataServiceCut)
package main

import (
	"flag"
	"fmt"
	"math/big"
	"os"
	"time"

	horizoncontracts "github.com/graphprotocol/substreams-data-service/contracts/horizon"
	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/streamingfast/eth-go"
)

func main() {
	if len(os.Args) < 2 {
		fatal("usage: sdsce-signtool <proof|collect-data> [flags]")
	}
	switch os.Args[1] {
	case "proof":
		cmdProof(os.Args[2:])
	case "collect-data":
		cmdCollectData(os.Args[2:])
	default:
		fatal("unknown subcommand %q (want proof|collect-data)", os.Args[1])
	}
}

func cmdProof(args []string) {
	fs := flag.NewFlagSet("proof", flag.ExitOnError)
	chainID := fs.Uint64("chain-id", 0, "chain id")
	collector := fs.String("collector", "", "GraphTallyCollector address")
	deadline := fs.Uint64("deadline", 0, "proof deadline (unix seconds)")
	authorizer := fs.String("authorizer", "", "authorizer (payer) address")
	signerKey := fs.String("signer-key", "", "signer private key hex")
	_ = fs.Parse(args)

	proof, err := horizoncontracts.GenerateSignerProof(
		*chainID, mustAddr(*collector), *deadline, mustAddr(*authorizer), mustKey(*signerKey),
	)
	if err != nil {
		fatal("generating proof: %v", err)
	}
	fmt.Printf("0x%x\n", proof)
}

func cmdCollectData(args []string) {
	fs := flag.NewFlagSet("collect-data", flag.ExitOnError)
	chainID := fs.Uint64("chain-id", 0, "chain id")
	collector := fs.String("collector", "", "GraphTallyCollector address (EIP-712 domain)")
	collectionID := fs.String("collection-id", "", "collection id (32-byte hex)")
	payer := fs.String("payer", "", "payer address")
	serviceProvider := fs.String("service-provider", "", "service provider address")
	dataService := fs.String("data-service", "", "data service address")
	value := fs.String("value", "", "value aggregate (raw wei)")
	cutPPM := fs.Uint64("data-service-cut", 0, "data service cut in PPM")
	signerKey := fs.String("signer-key", "", "signer private key hex")
	_ = fs.Parse(args)

	val, ok := new(big.Int).SetString(*value, 10)
	if !ok {
		fatal("invalid --value %q", *value)
	}
	var cid horizon.CollectionID
	copy(cid[:], mustHash(*collectionID))

	rav := &horizon.RAV{
		CollectionID:    cid,
		Payer:           mustAddr(*payer),
		ServiceProvider: mustAddr(*serviceProvider),
		DataService:     mustAddr(*dataService),
		TimestampNs:     uint64(time.Now().UnixNano()),
		ValueAggregate:  val,
		Metadata:        []byte{},
	}

	domain := horizon.NewDomain(*chainID, mustAddr(*collector))
	signedRAV, err := horizon.Sign(domain, rav, mustKey(*signerKey))
	if err != nil {
		fatal("signing RAV: %v", err)
	}
	data, err := horizoncontracts.EncodeDataServiceCollectData(signedRAV, *cutPPM)
	if err != nil {
		fatal("encoding collect data: %v", err)
	}
	fmt.Printf("0x%x\n", data)
}

func mustAddr(s string) eth.Address {
	a, err := eth.NewAddress(s)
	if err != nil {
		fatal("invalid address %q: %v", s, err)
	}
	return a
}

func mustKey(s string) *eth.PrivateKey {
	k, err := eth.NewPrivateKey(s)
	if err != nil {
		fatal("invalid private key: %v", err)
	}
	return k
}

func mustHash(s string) eth.Hash {
	h, err := eth.NewHash(s)
	if err != nil {
		fatal("invalid hash %q: %v", s, err)
	}
	return h
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}
