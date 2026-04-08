package gateway

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/graphprotocol/substreams-data-service/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/streamingfast/eth-go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestStartSession_AllowsConcurrentSessionsForSamePayer(t *testing.T) {
	ctx := context.Background()
	payer := eth.MustNewAddress("0x1111111111111111111111111111111111111111")
	serviceProvider := eth.MustNewAddress("0x2222222222222222222222222222222222222222")
	dataService := eth.MustNewAddress("0x3333333333333333333333333333333333333333")
	collector := eth.MustNewAddress("0x4444444444444444444444444444444444444444")

	repo := repository.NewInMemoryRepository()
	gateway, err := New(&Config{
		ListenAddr:        ":0",
		ServiceProvider:   serviceProvider,
		Domain:            horizon.NewDomain(1337, collector),
		CollectorAddr:     collector,
		DataPlaneEndpoint: "https://data-plane.example",
		Repository:        repo,
		TransportConfig:   sidecar.ServerTransportConfig{Plaintext: true},
	}, zap.NewNop())
	require.NoError(t, err)

	start := func() *connect.Response[providerv1.StartSessionResponse] {
		resp, err := gateway.StartSession(ctx, connect.NewRequest(&providerv1.StartSessionRequest{
			EscrowAccount: &commonv1.EscrowAccount{
				Payer:       commonv1.AddressFromEth(payer),
				Receiver:    commonv1.AddressFromEth(serviceProvider),
				DataService: commonv1.AddressFromEth(dataService),
			},
		}))
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.True(t, resp.Msg.Accepted, resp.Msg.RejectionReason)
		require.NotEmpty(t, resp.Msg.SessionId)
		return resp
	}

	first := start()
	second := start()
	require.NotEqual(t, first.Msg.SessionId, second.Msg.SessionId, "expected each accepted start to create a distinct session")

	active := repository.SessionStatusActive
	sessions, err := repo.SessionList(ctx, repository.SessionFilter{
		Payer:  &payer,
		Status: &active,
	})
	require.NoError(t, err)
	require.Len(t, sessions, 2, "expected both same-payer sessions to remain active")

	for _, session := range sessions {
		require.Equal(t, repository.SessionStatusActive, session.Status)
		require.Nil(t, session.EndedAt)
		require.Equal(t, commonv1.EndReason_END_REASON_UNSPECIFIED, session.EndReason)
	}
}
