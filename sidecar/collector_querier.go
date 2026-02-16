package sidecar

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/streamingfast/eth-go"
	"github.com/streamingfast/eth-go/rpc"
)

// CollectorQuerier provides methods to query the GraphTallyCollector contract.
type CollectorQuerier struct {
	rpcClient     *rpc.Client
	collectorAddr eth.Address
}

func NewCollectorQuerier(rpcEndpoint string, collectorAddr eth.Address) *CollectorQuerier {
	return &CollectorQuerier{
		rpcClient:     rpc.NewClient(rpcEndpoint),
		collectorAddr: collectorAddr,
	}
}

// IsAuthorized calls GraphTallyCollector.isAuthorized(authorizer, signer).
func (q *CollectorQuerier) IsAuthorized(ctx context.Context, authorizer, signer eth.Address) (bool, error) {
	// Build the call data for isAuthorized(address,address)
	// Function selector: keccak256("isAuthorized(address,address)")[:4]
	// = 0x65e4ad9e
	selector := []byte{0x65, 0xe4, 0xad, 0x9e}

	data := make([]byte, 4+32*2)
	copy(data[:4], selector)
	copy(data[4+12:4+32], authorizer[:])
	copy(data[4+32+12:4+64], signer[:])

	params := rpc.CallParams{
		To:   q.collectorAddr,
		Data: data,
	}

	resultHex, err := q.rpcClient.Call(ctx, params)
	if err != nil {
		return false, fmt.Errorf("calling isAuthorized: %w", err)
	}

	if strings.HasPrefix(resultHex, "0x") {
		resultHex = resultHex[2:]
	}

	resultBytes, err := hex.DecodeString(resultHex)
	if err != nil {
		return false, fmt.Errorf("decoding result: %w", err)
	}

	if len(resultBytes) != 32 {
		return false, fmt.Errorf("unexpected result length: %d", len(resultBytes))
	}

	return resultBytes[31] == 1, nil
}
