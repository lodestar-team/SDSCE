package sidecar

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"connectrpc.com/connect"
	"github.com/graphprotocol/substreams-data-service/horizon"
	consumerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	sidecarlib "github.com/graphprotocol/substreams-data-service/sidecar"
	"go.uber.org/zap"
)

// ReportUsage reports usage received from the provider.
// Called by substreams as data is received during streaming.
// This may trigger RAV signing if the accumulated usage warrants it.
func (s *Sidecar) ReportUsage(
	ctx context.Context,
	req *connect.Request[consumerv1.ReportUsageRequest],
) (*connect.Response[consumerv1.ReportUsageResponse], error) {
	sessionID := req.Msg.SessionId

	usage := req.Msg.Usage
	if usage == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("<usage> is required"))
	}

	s.logger.Debug("ReportUsage called",
		zap.String("session_id", sessionID),
	)

	// Get the session
	session, err := s.sessions.Get(sessionID)
	if err != nil {
		s.logger.Warn("session not found", zap.String("session_id", sessionID))
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	// Check session is active
	if !session.IsActive() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("session %q is not active", sessionID))
	}

	// Provider-managed sessions now use the sidecar ingress plus long-lived
	// PaymentSession control stream. The wrapper-era ReportUsage flow remains in-tree
	// temporarily but is no longer supported for provider-managed sessions.
	if client := s.paymentSessions.Get(sessionID); client != nil {
		_ = client
		return nil, connect.NewError(
			connect.CodeFailedPrecondition,
			fmt.Errorf("ReportUsage is deprecated for provider-managed sessions; use the consumer sidecar ingress endpoint instead"),
		)
	}

	// Fallback: local signing (no provider gateway configured for this session).
	//
	// Prefer provider-authoritative pricing config, if present on the session (set during Init via StartSession).
	// Otherwise require caller-provided cost (legacy mode).
	var cost *big.Int
	if session.PricingConfig != nil {
		cost = session.CalculateUsageCost(usage.BlocksProcessed, usage.BytesTransferred)
	} else {
		if usage.Cost == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("<usage.cost> is required"))
		}
		cost = usage.Cost.ToBigInt()
	}
	session.AddUsage(usage.BlocksProcessed, usage.BytesTransferred, usage.Requests, cost)

	// Get current RAV for value calculation
	currentRAV := session.GetRAV()

	// Calculate new value aggregate
	var newValue *big.Int
	if currentRAV != nil && currentRAV.Message != nil {
		newValue = new(big.Int).Add(currentRAV.Message.ValueAggregate, cost)
	} else {
		newValue = cost
	}

	// Create updated RAV with new value
	var collectionID [32]byte
	if currentRAV != nil && currentRAV.Message != nil {
		collectionID = currentRAV.Message.CollectionID
	}

	updatedRAV, err := s.signRAV(
		collectionID,
		session.Payer,
		session.DataService,
		session.Receiver,
		uint64(time.Now().UnixNano()),
		newValue,
		nil,
	)
	if err != nil {
		s.logger.Error("failed to sign updated RAV", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	session.SetRAV(updatedRAV)

	response := &consumerv1.ReportUsageResponse{
		UpdatedRav:     sidecarlib.HorizonSignedRAVToProto(updatedRAV),
		ShouldContinue: true,
	}

	s.logger.Debug("ReportUsage completed",
		zap.String("session_id", sessionID),
		zap.Uint64("blocks_processed", session.BlocksProcessed),
	)

	return connect.NewResponse(response), nil
}
func (s *Sidecar) signRAVForRequest(session *sidecarlib.Session, req *providerv1.RAVRequest) (*horizon.SignedRAV, error) {
	if req == nil {
		return nil, fmt.Errorf("rav_request is required")
	}
	if req.CurrentRav == nil {
		return nil, fmt.Errorf("rav_request.current_rav is required")
	}
	if req.Usage == nil {
		return nil, fmt.Errorf("rav_request.usage is required")
	}
	if req.Usage.Cost == nil {
		return nil, fmt.Errorf("rav_request.usage.cost is required")
	}

	current, err := sidecarlib.ProtoSignedRAVToHorizon(req.CurrentRav)
	if err != nil {
		return nil, fmt.Errorf("invalid rav_request.current_rav: %w", err)
	}
	if current == nil || current.Message == nil {
		return nil, fmt.Errorf("rav_request.current_rav.rav is required")
	}

	deltaCost := req.Usage.Cost.ToBigInt()
	nextValue := new(big.Int).Add(current.Message.ValueAggregate, deltaCost)

	return s.signRAV(
		current.Message.CollectionID,
		session.Payer,
		session.DataService,
		session.Receiver,
		uint64(time.Now().UnixNano()),
		nextValue,
		nil,
	)
}
