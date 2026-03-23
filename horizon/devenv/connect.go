package devenv

import (
	"context"

	"github.com/streamingfast/eth-go"
	"github.com/streamingfast/eth-go/rpc"
)

// Connect creates a detached Env view over a running deterministic devenv chain.
// It does not start or own any container lifecycle.
func Connect(ctx context.Context, rpcURL string, chainID uint64) *Env {
	ctx, cancel := context.WithCancel(ctx)
	accounts := DefaultAccounts()
	contracts := DefaultContractAddresses()

	return &Env{
		ctx:             ctx,
		cancel:          cancel,
		rpcClient:       rpc.NewClient(rpcURL),
		RPCURL:          rpcURL,
		ChainID:         chainID,
		GRTToken:        &Contract{Address: contracts.GRTToken, ABI: mustLoadContract("MockGRTToken").ABI},
		Controller:      &Contract{Address: eth.Address{}, ABI: mustLoadContract("MockController").ABI},
		Staking:         &Contract{Address: contracts.Staking, ABI: mustLoadContract("MockStaking").ABI},
		Escrow:          &Contract{Address: contracts.Escrow, ABI: mustLoadContract("PaymentsEscrow").ABI},
		GraphPayments:   &Contract{Address: eth.Address{}, ABI: mustLoadContract("GraphPayments").ABI},
		Collector:       &Contract{Address: contracts.Collector, ABI: mustLoadContract("GraphTallyCollector").ABI},
		DataService:     &Contract{Address: contracts.DataService, ABI: mustLoadContract("SubstreamsDataService").ABI},
		Deployer:        accounts.Deployer,
		ServiceProvider: accounts.ServiceProvider,
		Payer:           accounts.Payer,
		User1:           accounts.User1,
		User2:           accounts.User2,
		User3:           accounts.User3,
		DemoSigner:      accounts.DemoSigner,
	}
}
