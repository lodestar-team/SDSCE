package horizoncontracts

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"

	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/streamingfast/eth-go"
	"golang.org/x/crypto/sha3"
)

const signerProofMessage = "authorizeSignerProof"

// GenerateSignerProof creates the proof consumed by GraphTallyCollector.authorizeSigner.
//
// The message mirrors Solidity's abi.encodePacked:
// chainId, collector address, "authorizeSignerProof", proofDeadline, authorizer.
func GenerateSignerProof(
	chainID uint64,
	collectorAddress eth.Address,
	proofDeadline uint64,
	authorizer eth.Address,
	signerKey *eth.PrivateKey,
) ([]byte, error) {
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
	digest := keccak256(append([]byte("\x19Ethereum Signed Message:\n32"), messageHash...))

	ecdsaKey, err := signerECDSAKey(signerKey)
	if err != nil {
		return nil, err
	}

	sig, err := gethcrypto.Sign(digest, ecdsaKey)
	if err != nil {
		return nil, fmt.Errorf("signing proof: %w", err)
	}

	if len(sig) != 65 {
		return nil, fmt.Errorf("signing proof: expected 65-byte signature, got %d bytes", len(sig))
	}

	proof := make([]byte, 65)
	copy(proof[0:64], sig[0:64])
	proof[64] = sig[64] + 27

	return proof, nil
}

func signerECDSAKey(signerKey *eth.PrivateKey) (*ecdsa.PrivateKey, error) {
	key, err := gethcrypto.ToECDSA(signerKey.Bytes())
	if err != nil {
		return nil, fmt.Errorf("converting signer key: %w", err)
	}
	return key, nil
}

func keccak256(data []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	return h.Sum(nil)
}
