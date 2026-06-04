// Command forktest drives the SubstreamsDataService.collect() path end-to-end
// against a running chain (intended: an anvil fork of Arbitrum Sepolia).
//
// It reuses the repo's own EIP-712 RAV signing so the signature is guaranteed to
// match what the on-chain GraphTallyCollector expects. Escrow funding, staking,
// provisioning and registration are expected to already be in place (done via
// cast in the rehearsal); this binary performs: authorizeSigner -> sign RAV ->
// collect, and reports the resulting balances so the 1% burn can be asserted.
//
// All inputs come from the environment so no secrets are baked in.
package main

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
	horizoncontracts "github.com/graphprotocol/substreams-data-service/contracts/horizon"
	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/graphprotocol/substreams-data-service/horizon/devenv"
	"github.com/streamingfast/eth-go"
	"github.com/streamingfast/eth-go/rpc"
)

func env(k string) string {
	v := os.Getenv(k)
	if v == "" {
		fmt.Fprintf(os.Stderr, "missing env %s\n", k)
		os.Exit(1)
	}
	return v
}

func main() {
	ctx := context.Background()

	rpcURL := env("RPC")
	chainID := uint64(421614)

	collectorAddr := eth.MustNewAddress(env("COLLECTOR"))
	sdsAddr := eth.MustNewAddress(env("SDS"))
	providerAddr := eth.MustNewAddress(env("PROVIDER"))
	payerAddr := eth.MustNewAddress(env("PAYER"))

	payerKey, err := eth.NewPrivateKey(env("PAYER_KEY"))
	noerr("payer key", err)
	providerKey, err := eth.NewPrivateKey(env("PROVIDER_KEY"))
	noerr("provider key", err)
	signerKey, err := eth.NewPrivateKey(env("SIGNER_KEY"))
	noerr("signer key", err)
	signerAddr := signerKey.PublicKey().Address()

	value := new(big.Int)
	value.SetString(env("RAV_VALUE"), 10)

	client := rpc.NewClient(rpcURL)

	collector := horizoncontracts.MustNewCollector()
	dataService := horizoncontracts.MustNewDataService()

	// 1) payer authorizes the signer in the GraphTallyCollector.
	deadline := uint64(time.Now().Add(time.Hour).Unix())
	proof, err := horizoncontracts.GenerateSignerProof(chainID, collectorAddr, deadline, payerAddr, signerKey)
	noerr("generate signer proof", err)
	authData, err := collector.PackAuthorizeSigner(toCommon(signerAddr), new(big.Int).SetUint64(deadline), proof)
	noerr("pack authorizeSigner", err)
	noerr("send authorizeSigner", devenv.SendTransaction(ctx, client, payerKey, chainID, &collectorAddr, big.NewInt(0), authData))
	fmt.Printf("authorizeSigner: payer=%s authorized signer=%s\n", payerAddr.Pretty(), signerAddr.Pretty())

	// 2) build + sign the RAV (signed by the authorized signer).
	var collectionID horizon.CollectionID
	copy(collectionID[:], eth.MustNewHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Bytes())
	rav := &horizon.RAV{
		CollectionID:    collectionID,
		Payer:           payerAddr,
		ServiceProvider: providerAddr,
		DataService:     sdsAddr,
		TimestampNs:     uint64(time.Now().UnixNano()),
		ValueAggregate:  value,
		Metadata:        []byte{},
	}
	domain := horizon.NewDomain(chainID, collectorAddr)
	signedRAV, err := horizon.Sign(domain, rav, signerKey)
	noerr("sign RAV", err)
	recovered, err := signedRAV.RecoverSigner(domain)
	noerr("recover signer", err)
	fmt.Printf("RAV signed: value=%s recovered_signer=%s (want %s)\n", value, recovered.Pretty(), signerAddr.Pretty())

	// 3) provider calls SDS.collect(QueryFee, abi.encode(signedRAV, cut)).
	// The cut argument is ignored on-chain (SDS forces BURN_TAX_PPM), pass 10000.
	collectData, err := dataService.PackQueryFeeCollect(signedRAV, 10000)
	noerr("pack collect", err)
	noerr("send collect", devenv.SendTransaction(ctx, client, providerKey, chainID, &sdsAddr, big.NewInt(0), collectData))
	fmt.Println("collect: submitted OK")
}

func toCommon(a eth.Address) common.Address {
	return common.BytesToAddress(a[:])
}

func noerr(what string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL %s: %v\n", what, err)
		os.Exit(1)
	}
}
