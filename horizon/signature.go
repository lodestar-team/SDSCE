package horizon

import (
	"math/big"

	"github.com/streamingfast/eth-go"
)

// secp256k1 curve order N
var secp256k1N, _ = new(big.Int).SetString(
	"FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141", 16)
var secp256k1HalfN = new(big.Int).Rsh(secp256k1N, 1)

// normalizeSignature returns signature in low-S canonical form
// This prevents signature malleability attacks where the same message
// can have two valid signatures that recover to the same address
func normalizeSignature(sig eth.Signature) [65]byte {
	var result [65]byte
	copy(result[:], sig[:])

	// eth.Signature is V (1 byte) + R (32 bytes) + S (32 bytes).
	// Extract S value (bytes 33-64).
	s := new(big.Int).SetBytes(sig[33:])

	// If S > N/2, replace with N - S and flip V
	if s.Cmp(secp256k1HalfN) > 0 {
		s = new(big.Int).Sub(secp256k1N, s)
		sBytes := s.Bytes()
		// Zero out and copy normalized S
		for i := 33; i < 65; i++ {
			result[i] = 0
		}
		copy(result[65-len(sBytes):65], sBytes)
		// Flip V (recovery bit).
		result[0] = flipSignatureV(result[0])
	}

	return result
}

func flipSignatureV(v byte) byte {
	// Some codepaths use 0/1, but eth-go uses compact signatures where
	// V is a header byte: 27 + recoveryID (+4 if compressed).
	if v < 27 {
		return v ^ 1
	}

	header := v - 27
	recID := header & 1
	flags := header &^ 1
	return 27 + (recID ^ 1) + flags
}

// SignaturesEqual compares two signatures in normalized form
func SignaturesEqual(a, b eth.Signature) bool {
	normA := normalizeSignature(a)
	normB := normalizeSignature(b)
	return normA == normB
}
