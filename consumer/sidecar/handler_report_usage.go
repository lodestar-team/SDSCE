package sidecar

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"connectrpc.com/connect"
	"github.com/graphprotocol/substreams-data-service/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
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

	// If this session is configured with a provider gateway endpoint, forward usage
	// to the provider over the PaymentSession stream and respond to rav_request.
	if client := s.paymentSessions.Get(sessionID); client != nil {
		resp, err := s.reportUsageViaPaymentSession(ctx, client, sessionID, session, usage)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(resp), nil
	}

	// Fallback: local signing (no provider gateway configured for this session).
	if usage.Cost == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("<usage.cost> is required"))
	}
	cost := usage.Cost.ToNative()

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

func (s *Sidecar) reportUsageViaPaymentSession(
	ctx context.Context,
	client *paymentSessionClient,
	sessionID string,
	session *sidecarlib.Session,
	usage *commonv1.Usage,
) (*consumerv1.ReportUsageResponse, error) {
	// Track raw usage locally for observability/debugging; cost is provider-authoritative in this flow.
	session.AddUsage(usage.BlocksProcessed, usage.BytesTransferred, usage.Requests, nil)

	roundtripCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client.mu.Lock()
	defer client.mu.Unlock()

	stream := client.ensureStreamLocked()
	if err := stream.Send(&providerv1.PaymentSessionRequest{
		SessionId: sessionID,
		Message: &providerv1.PaymentSessionRequest_UsageReport{
			UsageReport: &providerv1.UsageReport{Usage: usage},
		},
	}); err != nil {
		client.closeLocked()
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("sending usage_report via PaymentSession: %w", err))
	}

	var pendingRAV *horizon.SignedRAV

	for {
		msg, err := receivePaymentSessionResponse(roundtripCtx, stream)
		if err != nil {
			client.closeLocked()
			return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("receiving PaymentSessionResponse: %w", err))
		}

		if ravReq := msg.GetRavRequest(); ravReq != nil {
			signed, err := s.signRAVForRequest(session, ravReq)
			if err != nil {
				client.closeLocked()
				return nil, connect.NewError(connect.CodeInternal, err)
			}

			if err := stream.Send(&providerv1.PaymentSessionRequest{
				SessionId: sessionID,
				Message: &providerv1.PaymentSessionRequest_RavSubmission{
					RavSubmission: &providerv1.SignedRAVSubmission{
						SignedRav: sidecarlib.HorizonSignedRAVToProto(signed),
						Usage:     ravReq.GetUsage(),
					},
				},
			}); err != nil {
				client.closeLocked()
				return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("sending rav_submission via PaymentSession: %w", err))
			}

			pendingRAV = signed
			continue
		}

		if need := msg.GetNeedMoreFunds(); need != nil {
			return &consumerv1.ReportUsageResponse{
				ShouldContinue: false,
				StopReason:     "need more funds",
			}, nil
		}

		if ctrl := msg.GetSessionControl(); ctrl != nil {
			shouldContinue := ctrl.GetAction() == providerv1.SessionControl_ACTION_CONTINUE
			stopReason := ctrl.GetReason()

			var updated *horizon.SignedRAV
			if shouldContinue && pendingRAV != nil {
				session.SetRAV(pendingRAV)
				updated = pendingRAV
			}

			return &consumerv1.ReportUsageResponse{
				UpdatedRav:     sidecarlib.HorizonSignedRAVToProto(updated),
				ShouldContinue: shouldContinue,
				StopReason:     stopReason,
			}, nil
		}
	}
}

func receivePaymentSessionResponse(
	ctx context.Context,
	stream *connect.BidiStreamForClient[providerv1.PaymentSessionRequest, providerv1.PaymentSessionResponse],
) (*providerv1.PaymentSessionResponse, error) {
	type result struct {
		msg *providerv1.PaymentSessionResponse
		err error
	}

	ch := make(chan result, 1)
	go func() {
		msg, err := stream.Receive()
		ch <- result{msg: msg, err: err}
	}()

	select {
	case <-ctx.Done():
		_ = stream.CloseRequest()
		_ = stream.CloseResponse()
		return nil, ctx.Err()
	case res := <-ch:
		return res.msg, res.err
	}
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

	deltaCost := req.Usage.Cost.ToNative()
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
