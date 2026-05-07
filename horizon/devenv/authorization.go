package devenv

import (
	horizoncontracts "github.com/graphprotocol/substreams-data-service/contracts/horizon"
	"github.com/streamingfast/eth-go"
)

// GenerateSignerProof generates a proof for authorizing a signer.
// The proof is the signer's signature over a message containing:
// - chainId (uint256)
// - collectorAddress (address - the contract address)
// - "authorizeSignerProof" (string literal)
// - proofDeadline (uint256)
// - authorizer (address - msg.sender who will call authorizeSigner)
//
// This matches the Solidity verification in Authorizable.sol:
//
//	bytes32 messageHash = keccak256(
//	    abi.encodePacked(block.chainid, address(this), "authorizeSignerProof", _proofDeadline, msg.sender)
//	);
//	bytes32 digest = MessageHashUtils.toEthSignedMessageHash(messageHash);
//	require(ECDSA.recover(digest, _proof) == _signer, AuthorizableInvalidSignerProof());
func GenerateSignerProof(
	chainID uint64,
	collectorAddress eth.Address,
	proofDeadline uint64,
	authorizer eth.Address,
	signerKey *eth.PrivateKey,
) ([]byte, error) {
	return horizoncontracts.GenerateSignerProof(chainID, collectorAddress, proofDeadline, authorizer, signerKey)
}
