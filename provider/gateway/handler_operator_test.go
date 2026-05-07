package gateway

import (
	"context"
	"math/big"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/graphprotocol/substreams-data-service/internal/operatorauth"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1/providerv1connect"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/streamingfast/eth-go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProviderOperatorService_AuthEnforcedThroughHandlers(t *testing.T) {
	ctx := context.Background()
	_, client, rav := newOperatorServiceFixture(t)

	_, err := client.ListSessions(ctx, connect.NewRequest(&providerv1.ListSessionsRequest{}))
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))

	readReq := operatorRequest(&providerv1.ListSessionsRequest{}, "read-token")
	readResp, err := client.ListSessions(ctx, readReq)
	require.NoError(t, err)
	require.Len(t, readResp.Msg.GetSessions(), 1)

	mutationReq := operatorRequest(&providerv1.MarkCollectionPendingRequest{
		Key:           collectionKeyProto("session-1", rav),
		ExpectedValue: commonv1.GRTFromBigInt(big.NewInt(1000)),
		TxHash:        "0xabc",
	}, "read-token")
	_, err = client.MarkCollectionPending(ctx, mutationReq)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	adminReq := operatorRequest(&providerv1.MarkCollectionPendingRequest{
		Key:           collectionKeyProto("session-1", rav),
		ExpectedValue: commonv1.GRTFromBigInt(big.NewInt(1000)),
	}, "admin-token")
	adminResp, err := client.MarkCollectionPending(ctx, adminReq)
	require.NoError(t, err)
	require.Equal(t, providerv1.CollectionState_COLLECTION_STATE_COLLECT_PENDING, adminResp.Msg.GetCollection().GetState())
	require.Equal(t, uint64(1), adminResp.Msg.GetCollection().GetAttemptCount())
	require.Empty(t, adminResp.Msg.GetCollection().GetLastTxHash())
}

func TestProviderOperatorService_ReadSurfaces(t *testing.T) {
	ctx := context.Background()
	_, client, rav := newOperatorServiceFixture(t)

	listSessionsResp, err := client.ListSessions(ctx, operatorRequest(&providerv1.ListSessionsRequest{
		IncludeRav: true,
		Payer:      commonv1.AddressFromEth(rav.Message.Payer),
	}, "read-token"))
	require.NoError(t, err)
	require.Len(t, listSessionsResp.Msg.GetSessions(), 1)
	session := listSessionsResp.Msg.GetSessions()[0]
	require.Equal(t, "session-1", session.GetSessionId())
	require.Equal(t, providerv1.OperatorSessionStatus_OPERATOR_SESSION_STATUS_ACTIVE, session.GetStatus())
	require.Equal(t, uint64(10), session.GetAccumulatedUsage().GetBlocksProcessed())
	require.Equal(t, uint64(5), session.GetBaselineUsage().GetBlocksProcessed())
	require.Equal(t, providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_INSUFFICIENT, session.GetPaymentState().GetFundsStatus())
	require.False(t, session.GetPaymentState().GetPaymentStatus().GetFundsSufficient())
	require.Equal(t, commonv1.GRTFromBigInt(big.NewInt(600)).GetBytes(), session.GetPaymentState().GetMinimumNeeded().GetBytes())
	require.Equal(t, providerv1.CollectionState_COLLECTION_STATE_COLLECTIBLE, session.GetAcceptedRav().GetCollectionState())

	filteredSessionsResp, err := client.ListSessions(ctx, operatorRequest(&providerv1.ListSessionsRequest{
		FundsStatus: providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_INSUFFICIENT,
	}, "read-token"))
	require.NoError(t, err)
	require.Len(t, filteredSessionsResp.Msg.GetSessions(), 1)

	filteredSessionsResp, err = client.ListSessions(ctx, operatorRequest(&providerv1.ListSessionsRequest{
		FundsStatus: providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_OK,
	}, "read-token"))
	require.NoError(t, err)
	require.Empty(t, filteredSessionsResp.Msg.GetSessions())

	getRAVResp, err := client.GetAcceptedRAV(ctx, operatorRequest(&providerv1.GetAcceptedRAVRequest{
		SessionId: "session-1",
	}, "read-token"))
	require.NoError(t, err)
	require.Equal(t, rav.Message.CollectionID[:], getRAVResp.Msg.GetRav().GetCollectionId())
	require.Equal(t, commonv1.GRTFromBigInt(big.NewInt(1000)).GetBytes(), getRAVResp.Msg.GetRav().GetValueAggregate().GetBytes())

	listCollectionsResp, err := client.ListCollections(ctx, operatorRequest(&providerv1.ListCollectionsRequest{
		State:        providerv1.CollectionState_COLLECTION_STATE_COLLECTIBLE,
		CollectionId: rav.Message.CollectionID[:],
	}, "read-token"))
	require.NoError(t, err)
	require.Len(t, listCollectionsResp.Msg.GetCollections(), 1)
	require.Equal(t, "session-1", listCollectionsResp.Msg.GetCollections()[0].GetKey().GetSessionId())

	getCollectionResp, err := client.GetCollection(ctx, operatorRequest(&providerv1.GetCollectionRequest{
		Key: collectionKeyProto("session-1", rav),
	}, "read-token"))
	require.NoError(t, err)
	require.Equal(t, providerv1.CollectionState_COLLECTION_STATE_COLLECTIBLE, getCollectionResp.Msg.GetCollection().GetState())
}

func TestProviderOperatorService_CollectionMutationErrors(t *testing.T) {
	ctx := context.Background()
	_, client, rav := newOperatorServiceFixture(t)

	_, err := client.MarkCollectionPending(ctx, operatorRequest(&providerv1.MarkCollectionPendingRequest{
		Key:           collectionKeyProto("session-1", rav),
		ExpectedValue: commonv1.GRTFromBigInt(big.NewInt(999)),
		TxHash:        "0xabc",
	}, "admin-token"))
	require.Equal(t, connect.CodeAborted, connect.CodeOf(err))

	_, err = client.GetCollection(ctx, operatorRequest(&providerv1.GetCollectionRequest{
		Key: &providerv1.CollectionKey{
			SessionId:    "session-1",
			CollectionId: []byte{1, 2, 3},
		},
	}, "read-token"))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func newOperatorServiceFixture(t *testing.T) (repository.GlobalRepository, providerv1connect.ProviderOperatorServiceClient, *horizon.SignedRAV) {
	t.Helper()

	ctx := context.Background()
	repo := repository.NewInMemoryRepository()
	payer := eth.MustNewAddress("0x1111111111111111111111111111111111111111")
	serviceProvider := eth.MustNewAddress("0x2222222222222222222222222222222222222222")
	dataService := eth.MustNewAddress("0x3333333333333333333333333333333333333333")

	session := repository.NewSession("session-1", payer, serviceProvider, dataService, repository.PricingConfig{})
	session.BlocksProcessed = 10
	session.BytesTransferred = 20
	session.Requests = 2
	session.TotalCost = big.NewInt(1000)
	session.Metadata = map[string]string{
		"funds_status":                    "insufficient",
		"funds_current_outstanding_wei":   "1000",
		"funds_projected_outstanding_wei": "1500",
		"funds_escrow_balance_wei":        "900",
		"funds_minimum_needed_wei":        "600",
	}
	require.NoError(t, repo.SessionCreate(ctx, session))

	rav := testOperatorSignedRAV(payer, serviceProvider, dataService, 1000)
	require.NoError(t, repo.SessionUpdateRAVAndBaseline(ctx, session.ID, rav, 5, 10, 1, big.NewInt(500)))

	paymentGateway, err := New(&Config{
		ListenAddr: ":0",
		OperatorAuthConfig: operatorauth.Config{
			ReadBearerToken:  "read-token",
			AdminBearerToken: "admin-token",
		},
		Repository: repo,
	}, zap.NewNop())
	require.NoError(t, err)

	operatorGateway, err := NewOperatorGateway(&OperatorGatewayConfig{
		ListenAddr:     ":0",
		PaymentGateway: paymentGateway,
	}, zap.NewNop())
	require.NoError(t, err)

	_, handler := providerv1connect.NewProviderOperatorServiceHandler(operatorGateway)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return repo, providerv1connect.NewProviderOperatorServiceClient(server.Client(), server.URL), rav
}

func operatorRequest[T any](msg *T, token string) *connect.Request[T] {
	req := connect.NewRequest(msg)
	if token != "" {
		req.Header().Set("Authorization", "Bearer "+token)
	}
	return req
}

func testOperatorSignedRAV(payer, serviceProvider, dataService eth.Address, value int64) *horizon.SignedRAV {
	var collectionID horizon.CollectionID
	for i := range collectionID {
		collectionID[i] = byte(i + 1)
	}
	var signature eth.Signature

	return &horizon.SignedRAV{
		Message: &horizon.RAV{
			CollectionID:    collectionID,
			Payer:           payer,
			ServiceProvider: serviceProvider,
			DataService:     dataService,
			TimestampNs:     uint64(time.Now().UnixNano()),
			ValueAggregate:  big.NewInt(value),
		},
		Signature: signature,
	}
}

func collectionKeyProto(sessionID string, rav *horizon.SignedRAV) *providerv1.CollectionKey {
	return &providerv1.CollectionKey{
		SessionId:       sessionID,
		CollectionId:    rav.Message.CollectionID[:],
		Payer:           commonv1.AddressFromEth(rav.Message.Payer),
		ServiceProvider: commonv1.AddressFromEth(rav.Message.ServiceProvider),
		DataService:     commonv1.AddressFromEth(rav.Message.DataService),
	}
}
