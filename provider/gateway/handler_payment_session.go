package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"connectrpc.com/connect"
	"github.com/graphprotocol/substreams-data-service/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/streamingfast/eth-go"
	"go.uber.org/zap"
)

type paymentSessionRequestResult struct {
	msg *providerv1.PaymentSessionRequest
	err error
}

// PaymentSession is a bidirectional stream for ongoing payment negotiation.
// The stream binds to exactly one session and receives provider-originated
// runtime control messages from the gateway runtime manager.
func (s *Gateway) PaymentSession(
	ctx context.Context,
	stream *connect.BidiStream[providerv1.PaymentSessionRequest, providerv1.PaymentSessionResponse],
) error {
	s.logger.Info("PaymentSession stream started")

	requests := make(chan paymentSessionRequestResult, 1)
	go receivePaymentSessionRequests(stream, requests)

	var sessionID string
	var runtimeEvents chan *providerv1.PaymentSessionResponse

	defer func() {
		if sessionID != "" && runtimeEvents != nil {
			s.runtime.unbindSession(sessionID, runtimeEvents)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil

		case runtimeResp, ok := <-runtimeEvents:
			if !ok || runtimeResp == nil {
				runtimeEvents = nil
				continue
			}

			if err := stream.Send(runtimeResp); err != nil {
				if errors.Is(err, context.Canceled) || connect.CodeOf(err) == connect.CodeCanceled {
					s.logger.Info("PaymentSession stream closed by client", zap.Error(err))
					return nil
				}
				return err
			}

			if isTerminalPaymentSessionResponse(runtimeResp) {
				return nil
			}

		case res, ok := <-requests:
			if !ok {
				return nil
			}

			if res.err != nil {
				if errors.Is(res.err, io.EOF) {
					s.logger.Info("PaymentSession stream closed by client")
					return nil
				}
				if errors.Is(res.err, context.Canceled) || connect.CodeOf(res.err) == connect.CodeCanceled {
					s.logger.Info("PaymentSession stream closed by client", zap.Error(res.err))
					return nil
				}

				s.logger.Error("PaymentSession receive error", zap.Error(res.err))
				return res.err
			}

			msg := res.msg
			if msg == nil {
				continue
			}

			gotSessionID := strings.TrimSpace(msg.GetSessionId())
			if sessionID == "" {
				if gotSessionID == "" {
					_ = stream.Send(stopPaymentSessionResponse("<session_id> is required"))
					return nil
				}

				session, err := s.repo.SessionGet(ctx, gotSessionID)
				if err != nil {
					_ = stream.Send(stopPaymentSessionResponse("session not found"))
					return nil
				}
				if !session.IsActive() {
					_ = stream.Send(stopResponseForInactiveSession(session))
					return nil
				}

				runtimeEvents = make(chan *providerv1.PaymentSessionResponse, 4)
				sessionID = gotSessionID
				if err := s.runtime.bindSession(ctx, s, gotSessionID, runtimeEvents); err != nil {
					_ = stream.Send(stopPaymentSessionResponse(err.Error()))
					return nil
				}

				if msg.GetMessage() == nil {
					continue
				}
			} else if gotSessionID != "" && gotSessionID != sessionID {
				_ = stream.Send(stopPaymentSessionResponse(fmt.Sprintf("unexpected session_id %q", gotSessionID)))
				return nil
			}

			session, err := s.repo.SessionGet(ctx, sessionID)
			if err != nil {
				_ = stream.Send(stopPaymentSessionResponse("session not found"))
				return nil
			}
			if !session.IsActive() {
				_ = stream.Send(stopResponseForInactiveSession(session))
				return nil
			}

			var resp *providerv1.PaymentSessionResponse
			switch m := msg.Message.(type) {
			case nil:
				continue
			case *providerv1.PaymentSessionRequest_RavSubmission:
				resp = s.handleRAVSubmission(ctx, sessionID, session, m.RavSubmission)
			case *providerv1.PaymentSessionRequest_FundsAck:
				resp = s.handleFundsAcknowledgment(sessionID, m.FundsAck)
			default:
				resp = stopPaymentSessionResponse("unknown payment session message")
			}

			if resp == nil {
				continue
			}

			if err := stream.Send(resp); err != nil {
				if errors.Is(err, context.Canceled) || connect.CodeOf(err) == connect.CodeCanceled {
					s.logger.Info("PaymentSession stream closed by client", zap.Error(err))
					return nil
				}
				return err
			}

			if isTerminalPaymentSessionResponse(resp) {
				return nil
			}
		}
	}
}

func receivePaymentSessionRequests(
	stream *connect.BidiStream[providerv1.PaymentSessionRequest, providerv1.PaymentSessionResponse],
	out chan<- paymentSessionRequestResult,
) {
	defer close(out)

	for {
		msg, err := stream.Receive()
		out <- paymentSessionRequestResult{msg: msg, err: err}
		if err != nil {
			return
		}
	}
}

func (s *Gateway) handleRAVSubmission(
	ctx context.Context,
	sessionID string,
	session *repository.Session,
	submission *providerv1.SignedRAVSubmission,
) *providerv1.PaymentSessionResponse {
	s.logger.Debug("received RAV submission in stream")

	if submission == nil {
		return stopPaymentSessionResponse("missing RAV submission")
	}
	if submission.SignedRav == nil {
		return stopPaymentSessionResponse("missing signed_rav")
	}

	var (
		resp       *providerv1.PaymentSessionResponse
		signedRAV  *horizon.SignedRAV
		signerAddr eth.Address
	)

	err := s.runtime.withSessionEval(sessionID, func(state *runtimeSessionState) error {
		pending := state.pendingRAV
		if pending == nil {
			freshSession, err := s.repo.SessionGet(ctx, sessionID)
			if err == nil && freshSession != nil && !freshSession.IsActive() {
				resp = stopResponseForInactiveSession(freshSession)
				return nil
			}
			resp = stopPaymentSessionResponse("unexpected rav_submission without an in-flight provider rav_request; respond on PaymentSession only")
			return nil
		}
		if submission.Usage == nil {
			resp = stopPaymentSessionResponse("missing usage for rav_submission")
			return nil
		}
		if !pending.matchesUsage(submission.Usage) {
			resp = stopPaymentSessionResponse("rav_submission usage does not match the in-flight provider rav_request")
			return nil
		}

		var err error
		signedRAV, err = sidecar.ProtoSignedRAVToHorizon(submission.SignedRav)
		if err != nil {
			s.logger.Warn("invalid RAV submission", zap.Error(err))
			resp = stopPaymentSessionResponse("invalid RAV")
			return nil
		}

		signerAddr, err = signedRAV.RecoverSigner(s.domain)
		if err != nil {
			s.logger.Warn("RAV signature verification failed", zap.Error(err))
			resp = stopPaymentSessionResponse("signature verification failed")
			return nil
		}

		if !sidecar.AddressesEqual(signedRAV.Message.Payer, session.Payer) {
			resp = stopPaymentSessionResponse("RAV payer does not match session")
			return nil
		}
		if !sidecar.AddressesEqual(signedRAV.Message.ServiceProvider, s.serviceProvider) {
			resp = stopPaymentSessionResponse("RAV service provider does not match")
			return nil
		}
		if !sidecar.AddressesEqual(signedRAV.Message.DataService, session.DataService) {
			resp = stopPaymentSessionResponse("RAV data service does not match session")
			return nil
		}

		isAuthorized, err := s.isSignerAuthorized(ctx, session.Payer, signerAddr)
		if err != nil {
			s.logger.Warn("authorization check failed", zap.Error(err))
			resp = stopPaymentSessionResponse("authorization check failed")
			return nil
		}
		if !isAuthorized {
			s.logger.Warn("RAV signer not authorized", zap.Stringer("signer", signerAddr))
			resp = stopPaymentSessionResponse("signer not authorized")
			return nil
		}

		currentValue := pending.currentRAVValue
		if signedRAV.Message.ValueAggregate.Cmp(currentValue) < 0 {
			resp = stopPaymentSessionResponse("RAV value is less than current RAV")
			return nil
		}
		if signedRAV.Message.ValueAggregate.Cmp(pending.targetValue) < 0 {
			resp = stopPaymentSessionResponse(
				fmt.Sprintf("RAV underpays usage: want exactly %s (current %s + requested delta %s)", pending.targetValue.String(), currentValue.String(), submission.Usage.Cost.ToBigInt().String()),
			)
			return nil
		}
		if signedRAV.Message.ValueAggregate.Cmp(pending.targetValue) > 0 {
			resp = stopPaymentSessionResponse(
				fmt.Sprintf("RAV overpays in-flight request: want exactly %s, got %s", pending.targetValue.String(), signedRAV.Message.ValueAggregate.String()),
			)
			return nil
		}

		if err := s.repo.SessionUpdateRAVAndBaseline(
			ctx,
			sessionID,
			signedRAV,
			pending.baselineBlocks,
			pending.baselineBytes,
			pending.baselineReqs,
			pending.baselineCost,
		); err != nil {
			s.logger.Warn("failed to update session", zap.String("session_id", sessionID), zap.Error(err))
			resp = stopPaymentSessionResponse("failed to update session state")
			return nil
		}

		state.pendingRAV = nil
		state.queuedResponse = nil
		if err := s.runtime.evaluateMeteredUsage(ctx, s, sessionID); err != nil {
			s.logger.Warn("failed to re-evaluate metered usage after RAV acceptance", zap.String("session_id", sessionID), zap.Error(err))
			resp = stopPaymentSessionResponse("failed to refresh runtime payment state")
			return nil
		}

		resp = continuePaymentSessionResponse()
		return nil
	})
	if err != nil {
		s.logger.Warn("failed to serialize payment session RAV handling", zap.String("session_id", sessionID), zap.Error(err))
		return stopPaymentSessionResponse("failed to coordinate runtime payment state")
	}

	if resp == nil {
		return continuePaymentSessionResponse()
	}
	if resp.GetSessionControl() == nil || resp.GetSessionControl().GetAction() != providerv1.SessionControl_ACTION_CONTINUE {
		return resp
	}

	s.logger.Info("RAV accepted via stream",
		zap.String("session_id", sessionID),
		zap.Stringer("signer", signerAddr),
		zap.Stringer("value", signedRAV.Message.ValueAggregate),
	)

	return resp
}

func (s *Gateway) handleFundsAcknowledgment(
	sessionID string,
	ack *providerv1.FundsAcknowledgment,
) *providerv1.PaymentSessionResponse {
	if ack == nil {
		return stopPaymentSessionResponse("missing funds acknowledgment")
	}

	s.logger.Debug("received funds acknowledgment",
		zap.String("session_id", sessionID),
		zap.Bool("will_deposit", ack.WillDeposit),
	)

	if ack.WillDeposit {
		s.runtime.clearQueuedControl(sessionID)
		return continuePaymentSessionResponse()
	}

	s.runtime.clearQueuedControl(sessionID)
	return stopPaymentSessionResponse("insufficient funds and no deposit planned")
}

func continuePaymentSessionResponse() *providerv1.PaymentSessionResponse {
	return &providerv1.PaymentSessionResponse{
		Message: &providerv1.PaymentSessionResponse_SessionControl{
			SessionControl: &providerv1.SessionControl{
				Action: providerv1.SessionControl_ACTION_CONTINUE,
			},
		},
	}
}

func stopPaymentSessionResponse(reason string) *providerv1.PaymentSessionResponse {
	return &providerv1.PaymentSessionResponse{
		Message: &providerv1.PaymentSessionResponse_SessionControl{
			SessionControl: &providerv1.SessionControl{
				Action: providerv1.SessionControl_ACTION_STOP,
				Reason: reason,
			},
		},
	}
}

func stopResponseForInactiveSession(session *repository.Session) *providerv1.PaymentSessionResponse {
	if session == nil {
		return stopPaymentSessionResponse("session is not active")
	}

	switch session.EndReason {
	case commonv1.EndReason_END_REASON_PAYMENT_ISSUE:
		return stopPaymentSessionResponse("need more funds")
	case commonv1.EndReason_END_REASON_COMPLETE:
		return stopPaymentSessionResponse("session completed")
	case commonv1.EndReason_END_REASON_CLIENT_DISCONNECT:
		return stopPaymentSessionResponse("client disconnected")
	case commonv1.EndReason_END_REASON_PROVIDER_STOP:
		return stopPaymentSessionResponse("provider ended stream")
	case commonv1.EndReason_END_REASON_ERROR:
		return stopPaymentSessionResponse("provider session error")
	default:
		return stopPaymentSessionResponse("session is not active")
	}
}

func isTerminalPaymentSessionResponse(resp *providerv1.PaymentSessionResponse) bool {
	if resp == nil {
		return false
	}

	if resp.GetNeedMoreFunds() != nil {
		return true
	}

	ctrl := resp.GetSessionControl()
	if ctrl == nil {
		return false
	}

	return ctrl.GetAction() != providerv1.SessionControl_ACTION_CONTINUE
}
