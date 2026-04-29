package gateway

import (
	"context"
	"math/big"
	"testing"

	"connectrpc.com/connect"
	"github.com/graphprotocol/substreams-data-service/horizon"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/streamingfast/eth-go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestGetSessionStatus_ReportsPaymentControlPendingForQueuedRuntimeRAV(t *testing.T) {
	ctx := context.Background()
	sessionID := "session-status-pending"

	payer := eth.MustNewAddress("0x1111111111111111111111111111111111111111")
	serviceProvider := eth.MustNewAddress("0x2222222222222222222222222222222222222222")
	dataService := eth.MustNewAddress("0x3333333333333333333333333333333333333333")

	repo := repository.NewInMemoryRepository()
	session := repository.NewSession(sessionID, payer, serviceProvider, dataService, repository.PricingConfig{})
	session.CurrentRAV = &horizon.SignedRAV{
		Message: &horizon.RAV{
			Payer:           payer,
			ServiceProvider: serviceProvider,
			DataService:     dataService,
			ValueAggregate:  big.NewInt(0),
		},
	}
	session.TotalCost = big.NewInt(10)
	require.NoError(t, repo.SessionCreate(ctx, session))

	gateway := &Gateway{
		logger:              zap.NewNop(),
		repo:                repo,
		runtime:             newRuntimeManager(),
		ravRequestThreshold: big.NewInt(1),
	}

	resp, err := gateway.GetSessionStatus(ctx, connect.NewRequest(&providerv1.GetSessionStatusRequest{
		SessionId: sessionID,
	}))
	require.NoError(t, err)
	require.True(t, resp.Msg.GetPaymentControlPending())
}
