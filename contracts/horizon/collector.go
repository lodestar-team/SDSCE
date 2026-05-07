package horizoncontracts

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/graphprotocol/substreams-data-service/contracts/artifacts"
)

type Collector struct {
	abi abi.ABI
}

func NewCollector() (*Collector, error) {
	artifact, err := artifacts.Load("GraphTallyCollector")
	if err != nil {
		return nil, err
	}
	parsed, err := abi.JSON(strings.NewReader(string(artifact.ABI)))
	if err != nil {
		return nil, fmt.Errorf("parsing GraphTallyCollector ABI: %w", err)
	}
	return &Collector{abi: parsed}, nil
}

func MustNewCollector() *Collector {
	collector, err := NewCollector()
	if err != nil {
		panic(err)
	}
	return collector
}

func (c *Collector) PackIsAuthorized(authorizer common.Address, signer common.Address) ([]byte, error) {
	return c.abi.Pack("isAuthorized", authorizer, signer)
}

func (c *Collector) UnpackIsAuthorized(data []byte) (bool, error) {
	values, err := c.abi.Unpack("isAuthorized", data)
	if err != nil {
		return false, fmt.Errorf("unpacking isAuthorized: %w", err)
	}
	if len(values) != 1 {
		return false, fmt.Errorf("unpacking isAuthorized: expected 1 value, got %d", len(values))
	}
	authorized, ok := values[0].(bool)
	if !ok {
		return false, fmt.Errorf("unpacking isAuthorized: expected bool, got %T", values[0])
	}
	return authorized, nil
}

func (c *Collector) PackGetThawEnd(signer common.Address) ([]byte, error) {
	return c.abi.Pack("getThawEnd", signer)
}

func (c *Collector) UnpackGetThawEnd(data []byte) (*big.Int, error) {
	values, err := c.abi.Unpack("getThawEnd", data)
	if err != nil {
		return nil, fmt.Errorf("unpacking getThawEnd: %w", err)
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("unpacking getThawEnd: expected 1 value, got %d", len(values))
	}
	thawEnd, ok := values[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("unpacking getThawEnd: expected *big.Int, got %T", values[0])
	}
	return thawEnd, nil
}

func (c *Collector) PackAuthorizeSigner(signer common.Address, proofDeadline *big.Int, proof []byte) ([]byte, error) {
	return c.abi.Pack("authorizeSigner", signer, proofDeadline, proof)
}

func (c *Collector) PackThawSigner(signer common.Address) ([]byte, error) {
	return c.abi.Pack("thawSigner", signer)
}

func (c *Collector) PackRevokeAuthorizedSigner(signer common.Address) ([]byte, error) {
	return c.abi.Pack("revokeAuthorizedSigner", signer)
}

func (c *Collector) PackCancelThawSigner(signer common.Address) ([]byte, error) {
	return c.abi.Pack("cancelThawSigner", signer)
}
