package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"

	"connectrpc.com/connect"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/graphprotocol/substreams-data-service/sidecar"
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
					_ = stream.Send(stopPaymentSessionResponse("session is not active"))
					return nil
				}

				runtimeEvents = make(chan *providerv1.PaymentSessionResponse, 4)
				if err := s.runtime.bindSession(ctx, s, gotSessionID, runtimeEvents); err != nil {
					_ = stream.Send(stopPaymentSessionResponse(err.Error()))
					return nil
				}

				sessionID = gotSessionID
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
				_ = stream.Send(stopPaymentSessionResponse("session is not active"))
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
			case *providerv1.PaymentSessionRequest_UsageReport:
				resp = stopPaymentSessionResponse("usage_report is deprecated for provider-managed runtime sessions")
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

	signedRAV, err := sidecar.ProtoSignedRAVToHorizon(submission.SignedRav)
	if err != nil {
		s.logger.Warn("invalid RAV submission", zap.Error(err))
		return stopPaymentSessionResponse("invalid RAV")
	}

	signerAddr, err := signedRAV.RecoverSigner(s.domain)
	if err != nil {
		s.logger.Warn("RAV signature verification failed", zap.Error(err))
		return stopPaymentSessionResponse("signature verification failed")
	}

	if !sidecar.AddressesEqual(signedRAV.Message.Payer, session.Payer) {
		return stopPaymentSessionResponse("RAV payer does not match session")
	}
	if !sidecar.AddressesEqual(signedRAV.Message.ServiceProvider, s.serviceProvider) {
		return stopPaymentSessionResponse("RAV service provider does not match")
	}
	if !sidecar.AddressesEqual(signedRAV.Message.DataService, session.DataService) {
		return stopPaymentSessionResponse("RAV data service does not match session")
	}

	isAuthorized, err := s.isSignerAuthorized(ctx, session.Payer, signerAddr)
	if err != nil {
		s.logger.Warn("authorization check failed", zap.Error(err))
		return stopPaymentSessionResponse("authorization check failed")
	}
	if !isAuthorized {
		s.logger.Warn("RAV signer not authorized", zap.Stringer("signer", signerAddr))
		return stopPaymentSessionResponse("signer not authorized")
	}

	currentRAV := session.CurrentRAV
	if currentRAV != nil && currentRAV.Message != nil {
		if signedRAV.Message.ValueAggregate.Cmp(currentRAV.Message.ValueAggregate) < 0 {
			return stopPaymentSessionResponse("RAV value is less than current RAV")
		}
	}

	currentValue := big.NewInt(0)
	if currentRAV != nil && currentRAV.Message != nil && currentRAV.Message.ValueAggregate != nil {
		currentValue = currentRAV.Message.ValueAggregate
	}
	_, _, _, deltaCost := session.UsageDeltaSinceBaseline()
	minValue := new(big.Int).Add(currentValue, deltaCost)
	if signedRAV.Message.ValueAggregate.Cmp(minValue) < 0 {
		return stopPaymentSessionResponse(
			fmt.Sprintf("RAV underpays usage: want >= %s (current %s + delta %s)", minValue.String(), currentValue.String(), deltaCost.String()),
		)
	}

	baselineBlocks := session.BlocksProcessed
	baselineBytes := session.BytesTransferred
	baselineReqs := session.Requests
	baselineCost := big.NewInt(0)
	if session.TotalCost != nil {
		baselineCost = new(big.Int).Set(session.TotalCost)
	}
	if err := s.repo.SessionUpdateRAVAndBaseline(ctx, sessionID, signedRAV, baselineBlocks, baselineBytes, baselineReqs, baselineCost); err != nil {
		s.logger.Warn("failed to update session", zap.String("session_id", sessionID), zap.Error(err))
		return stopPaymentSessionResponse("failed to update session state")
	}

	s.runtime.clearAwaitingRAV(sessionID)

	s.logger.Info("RAV accepted via stream",
		zap.String("session_id", sessionID),
		zap.Stringer("signer", signerAddr),
		zap.Stringer("value", signedRAV.Message.ValueAggregate),
	)

	return continuePaymentSessionResponse()
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
		return continuePaymentSessionResponse()
	}

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
