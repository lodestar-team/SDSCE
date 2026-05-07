package gateway

import (
	"context"
	"errors"
	"math/big"
	"strings"

	"connectrpc.com/connect"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
)

func (s *Gateway) GetSessionStatus(
	ctx context.Context,
	req *connect.Request[providerv1.GetSessionStatusRequest],
) (*connect.Response[providerv1.GetSessionStatusResponse], error) {
	sessionID := strings.TrimSpace(req.Msg.GetSessionId())
	if sessionID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("<session_id> is required"))
	}

	session, err := s.repo.SessionGet(ctx, sessionID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	paymentControlPending, err := s.paymentControlPending(ctx, session)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	cur := big.NewInt(0)
	if session.CurrentRAV != nil && session.CurrentRAV.Message != nil && session.CurrentRAV.Message.ValueAggregate != nil {
		cur = session.CurrentRAV.Message.ValueAggregate
	}

	acc := big.NewInt(0)
	if session.TotalCost != nil {
		acc = session.TotalCost
	}

	resp := &providerv1.GetSessionStatusResponse{
		Active:                session.IsActive(),
		EndReason:             commonv1.EndReason_END_REASON_UNSPECIFIED,
		PaymentControlPending: paymentControlPending,
		PaymentStatus: &commonv1.PaymentStatus{
			CurrentRavValue:       commonv1.GRTFromBigInt(cur),
			AccumulatedUsageValue: commonv1.GRTFromBigInt(acc),
		},
	}
	if !session.IsActive() {
		resp.EndReason = session.EndReason
	}
	return connect.NewResponse(resp), nil
}
