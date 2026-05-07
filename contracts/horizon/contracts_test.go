package horizoncontracts

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/streamingfast/eth-go"
)

func TestEscrowPackAndUnpack(t *testing.T) {
	escrow := MustNewEscrow()
	payer := common.HexToAddress("0x1111111111111111111111111111111111111111")
	collector := common.HexToAddress("0x2222222222222222222222222222222222222222")
	receiver := common.HexToAddress("0x3333333333333333333333333333333333333333")

	getBalance, err := escrow.PackGetBalance(payer, collector, receiver)
	if err != nil {
		t.Fatalf("PackGetBalance() error = %v", err)
	}
	if got, want := hex.EncodeToString(getBalance[:4]), selector("getBalance(address,address,address)"); got != want {
		t.Fatalf("getBalance selector = %s, want %s", got, want)
	}

	deposit, err := escrow.PackDeposit(collector, receiver, big.NewInt(10))
	if err != nil {
		t.Fatalf("PackDeposit() error = %v", err)
	}
	if got, want := hex.EncodeToString(deposit[:4]), selector("deposit(address,address,uint256)"); got != want {
		t.Fatalf("deposit selector = %s, want %s", got, want)
	}

	output, err := escrow.abi.Methods["getBalance"].Outputs.Pack(big.NewInt(123))
	if err != nil {
		t.Fatalf("pack output: %v", err)
	}
	got, err := escrow.UnpackGetBalance(output)
	if err != nil {
		t.Fatalf("UnpackGetBalance() error = %v", err)
	}
	if got.Cmp(big.NewInt(123)) != 0 {
		t.Fatalf("UnpackGetBalance() = %s, want 123", got.String())
	}
}

func TestCollectorPackAndUnpack(t *testing.T) {
	collector := MustNewCollector()
	payer := common.HexToAddress("0x1111111111111111111111111111111111111111")
	signer := common.HexToAddress("0x2222222222222222222222222222222222222222")

	authorize, err := collector.PackAuthorizeSigner(signer, big.NewInt(1778112000), []byte{1, 2, 3})
	if err != nil {
		t.Fatalf("PackAuthorizeSigner() error = %v", err)
	}
	if got, want := hex.EncodeToString(authorize[:4]), selector("authorizeSigner(address,uint256,bytes)"); got != want {
		t.Fatalf("authorizeSigner selector = %s, want %s", got, want)
	}

	isAuthorized, err := collector.PackIsAuthorized(payer, signer)
	if err != nil {
		t.Fatalf("PackIsAuthorized() error = %v", err)
	}
	if got, want := hex.EncodeToString(isAuthorized[:4]), selector("isAuthorized(address,address)"); got != want {
		t.Fatalf("isAuthorized selector = %s, want %s", got, want)
	}

	output, err := collector.abi.Methods["isAuthorized"].Outputs.Pack(true)
	if err != nil {
		t.Fatalf("pack output: %v", err)
	}
	got, err := collector.UnpackIsAuthorized(output)
	if err != nil {
		t.Fatalf("UnpackIsAuthorized() error = %v", err)
	}
	if !got {
		t.Fatal("UnpackIsAuthorized() = false, want true")
	}
}

func TestDataServicePackCollect(t *testing.T) {
	dataService := MustNewDataService()
	signedRAV := testSignedRAV()

	data, err := EncodeDataServiceCollectData(signedRAV, 100_000)
	if err != nil {
		t.Fatalf("EncodeDataServiceCollectData() error = %v", err)
	}
	if len(data) == 0 {
		t.Fatal("EncodeDataServiceCollectData() returned empty calldata payload")
	}

	calldata, err := dataService.PackQueryFeeCollect(signedRAV, 100_000)
	if err != nil {
		t.Fatalf("PackQueryFeeCollect() error = %v", err)
	}
	if got, want := hex.EncodeToString(calldata[:4]), selector("collect(address,uint8,bytes)"); got != want {
		t.Fatalf("collect selector = %s, want %s", got, want)
	}

	values, err := dataService.abi.Methods["collect"].Inputs.Unpack(calldata[4:])
	if err != nil {
		t.Fatalf("unpack collect inputs: %v", err)
	}
	if got, want := values[0].(common.Address), common.HexToAddress(signedRAV.Message.ServiceProvider.Pretty()); got != want {
		t.Fatalf("indexer = %s, want %s", got.Hex(), want.Hex())
	}
	if got, want := values[1].(uint8), PaymentTypeQueryFee; got != want {
		t.Fatalf("paymentType = %d, want %d", got, want)
	}
	if got := values[2].([]byte); hex.EncodeToString(got) != hex.EncodeToString(data) {
		t.Fatal("collect data argument does not match encoded RAV payload")
	}
}

func TestDataServiceCollectDataRequiresRAV(t *testing.T) {
	if _, err := EncodeDataServiceCollectData(nil, 0); err == nil {
		t.Fatal("expected nil signed RAV error")
	}

	signedRAV := testSignedRAV()
	signedRAV.Message.ValueAggregate = nil
	if _, err := EncodeDataServiceCollectData(signedRAV, 0); err == nil {
		t.Fatal("expected nil value aggregate error")
	}
}

func TestGenerateSignerProofLayout(t *testing.T) {
	signerKey, err := eth.NewPrivateKey("0xe4c2694501255921b6588519cfd36d4e86ddc4ce19ab1bc91d9c58057c040304")
	if err != nil {
		t.Fatalf("NewPrivateKey() error = %v", err)
	}
	collector := eth.MustNewAddress("0x2222222222222222222222222222222222222222")
	payer := eth.MustNewAddress("0x1111111111111111111111111111111111111111")

	proof, err := GenerateSignerProof(42161, collector, 1778112000, payer, signerKey)
	if err != nil {
		t.Fatalf("GenerateSignerProof() error = %v", err)
	}
	if len(proof) != 65 {
		t.Fatalf("len(proof) = %d, want 65", len(proof))
	}
	if proof[64] != 27 && proof[64] != 28 {
		t.Fatalf("proof v = %d, want 27 or 28", proof[64])
	}

	digest := signerProofDigest(42161, collector, 1778112000, payer)
	recoveryProof := append([]byte(nil), proof...)
	recoveryProof[64] -= 27
	recovered, err := crypto.SigToPub(digest, recoveryProof)
	if err != nil {
		t.Fatalf("SigToPub() error = %v", err)
	}
	if got, want := crypto.PubkeyToAddress(*recovered), common.HexToAddress(signerKey.PublicKey().Address().Pretty()); got != want {
		t.Fatalf("recovered signer = %s, want %s", got.Hex(), want.Hex())
	}
}

func testSignedRAV() *horizon.SignedRAV {
	var collectionID horizon.CollectionID
	copy(collectionID[:], common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Bytes())

	var sig eth.Signature
	for i := range sig {
		sig[i] = byte(i + 1)
	}

	return &horizon.SignedRAV{
		Message: &horizon.RAV{
			CollectionID:    collectionID,
			Payer:           eth.MustNewAddress("0x1111111111111111111111111111111111111111"),
			ServiceProvider: eth.MustNewAddress("0x2222222222222222222222222222222222222222"),
			DataService:     eth.MustNewAddress("0x3333333333333333333333333333333333333333"),
			TimestampNs:     123,
			ValueAggregate:  big.NewInt(1000),
			Metadata:        []byte("metadata"),
		},
		Signature: sig,
	}
}

func selector(signature string) string {
	return hex.EncodeToString(crypto.Keccak256([]byte(signature))[:4])
}

func signerProofDigest(chainID uint64, collectorAddress eth.Address, proofDeadline uint64, authorizer eth.Address) []byte {
	message := make([]byte, 0, 124)

	chainIDBytes := make([]byte, 32)
	new(big.Int).SetUint64(chainID).FillBytes(chainIDBytes)
	message = append(message, chainIDBytes...)

	message = append(message, collectorAddress[:]...)
	message = append(message, []byte(signerProofMessage)...)

	deadlineBytes := make([]byte, 32)
	new(big.Int).SetUint64(proofDeadline).FillBytes(deadlineBytes)
	message = append(message, deadlineBytes...)

	message = append(message, authorizer[:]...)

	messageHash := keccak256(message)
	return keccak256(append([]byte("\x19Ethereum Signed Message:\n32"), messageHash...))
}
