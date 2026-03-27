package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"

	"connectrpc.com/connect"
	sds "github.com/graphprotocol/substreams-data-service"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"go.uber.org/zap"
)

// PaymentSession is a bidirectional stream for ongoing payment negotiation.
// This allows the provider gateway to request RAVs and notify about
// funding requirements in real-time.
func (s *Gateway) PaymentSession(
	ctx context.Context,
	stream *connect.BidiStream[providerv1.PaymentSessionRequest, providerv1.PaymentSessionResponse],
) error {
	s.logger.Info("PaymentSession stream started")

	var session *repository.Session
	var sessionID string
	var awaitingRAV bool

	for {
		// Receive message from consumer sidecar
		msg, err := stream.Receive()
		if err == io.EOF {
			s.logger.Info("PaymentSession stream closed by client")
			if session != nil && session.IsActive() {
				session.End(commonv1.EndReason_END_REASON_CLIENT_DISCONNECT)
			}
			return nil
		}
		if err != nil {
			if session != nil && session.IsActive() {
				if errors.Is(err, context.Canceled) || connect.CodeOf(err) == connect.CodeCanceled {
					session.End(commonv1.EndReason_END_REASON_CLIENT_DISCONNECT)
				} else {
					session.End(commonv1.EndReason_END_REASON_ERROR)
				}
			}

			if errors.Is(err, context.Canceled) || connect.CodeOf(err) == connect.CodeCanceled {
				s.logger.Info("PaymentSession stream closed by client", zap.Error(err))
				return nil
			}

			s.logger.Error("PaymentSession receive error", zap.Error(err))
			return err
		}

		if session == nil {
			sessionID = strings.TrimSpace(msg.GetSessionId())
			if sessionID == "" {
				_ = stream.Send(&providerv1.PaymentSessionResponse{
					Message: &providerv1.PaymentSessionResponse_SessionControl{
						SessionControl: &providerv1.SessionControl{
							Action: providerv1.SessionControl_ACTION_STOP,
							Reason: "<session_id> is required",
						},
					},
				})
				return nil
			}

			session, err = s.repo.SessionGet(ctx, sessionID)
			if err != nil {
				_ = stream.Send(&providerv1.PaymentSessionResponse{
					Message: &providerv1.PaymentSessionResponse_SessionControl{
						SessionControl: &providerv1.SessionControl{
							Action: providerv1.SessionControl_ACTION_STOP,
							Reason: "session not found",
						},
					},
				})
				return nil
			}
		} else if got := strings.TrimSpace(msg.GetSessionId()); got != "" && got != sessionID {
			_ = stream.Send(&providerv1.PaymentSessionResponse{
				Message: &providerv1.PaymentSessionResponse_SessionControl{
					SessionControl: &providerv1.SessionControl{
						Action: providerv1.SessionControl_ACTION_STOP,
						Reason: fmt.Sprintf("unexpected session_id %q", got),
					},
				},
			})
			return nil
		}

		if !session.IsActive() {
			_ = stream.Send(&providerv1.PaymentSessionResponse{
				Message: &providerv1.PaymentSessionResponse_SessionControl{
					SessionControl: &providerv1.SessionControl{
						Action: providerv1.SessionControl_ACTION_STOP,
						Reason: "session is not active",
					},
				},
			})
			return nil
		}

		// Handle the message based on type
		switch m := msg.Message.(type) {
		case *providerv1.PaymentSessionRequest_RavSubmission:
			s.handleRAVSubmission(ctx, stream, sessionID, session, m.RavSubmission)
			awaitingRAV = false

		case *providerv1.PaymentSessionRequest_FundsAck:
			s.handleFundsAcknowledgment(ctx, stream, sessionID, session, m.FundsAck)

		case *providerv1.PaymentSessionRequest_UsageReport:
			var terminate bool
			awaitingRAV, terminate = s.handleUsageReport(ctx, stream, sessionID, session, awaitingRAV, m.UsageReport)
			if terminate {
				return nil
			}

		default:
			s.logger.Warn("unknown message type in PaymentSession")
		}
	}
}

func (s *Gateway) handleRAVSubmission(
	ctx context.Context,
	stream *connect.BidiStream[providerv1.PaymentSessionRequest, providerv1.PaymentSessionResponse],
	sessionID string,
	session *repository.Session,
	submission *providerv1.SignedRAVSubmission,
) {
	s.logger.Debug("received RAV submission in stream")

	if submission == nil {
		stream.Send(&providerv1.PaymentSessionResponse{
			Message: &providerv1.PaymentSessionResponse_SessionControl{
				SessionControl: &providerv1.SessionControl{
					Action: providerv1.SessionControl_ACTION_STOP,
					Reason: "missing RAV submission",
				},
			},
		})
		return
	}
	if submission.SignedRav == nil {
		stream.Send(&providerv1.PaymentSessionResponse{
			Message: &providerv1.PaymentSessionResponse_SessionControl{
				SessionControl: &providerv1.SessionControl{
					Action: providerv1.SessionControl_ACTION_STOP,
					Reason: "missing signed_rav",
				},
			},
		})
		return
	}

	// Validate the RAV
	signedRAV, err := sidecar.ProtoSignedRAVToHorizon(submission.SignedRav)
	if err != nil {
		s.logger.Warn("invalid RAV submission", zap.Error(err))
		stream.Send(&providerv1.PaymentSessionResponse{
			Message: &providerv1.PaymentSessionResponse_SessionControl{
				SessionControl: &providerv1.SessionControl{
					Action: providerv1.SessionControl_ACTION_STOP,
					Reason: "invalid RAV",
				},
			},
		})
		return
	}

	// Verify signature
	signerAddr, err := signedRAV.RecoverSigner(s.domain)
	if err != nil {
		s.logger.Warn("RAV signature verification failed", zap.Error(err))
		stream.Send(&providerv1.PaymentSessionResponse{
			Message: &providerv1.PaymentSessionResponse_SessionControl{
				SessionControl: &providerv1.SessionControl{
					Action: providerv1.SessionControl_ACTION_STOP,
					Reason: "signature verification failed",
				},
			},
		})
		return
	}

	// Verify RAV is for the correct session participants
	if !sidecar.AddressesEqual(signedRAV.Message.Payer, session.Payer) {
		stream.Send(&providerv1.PaymentSessionResponse{
			Message: &providerv1.PaymentSessionResponse_SessionControl{
				SessionControl: &providerv1.SessionControl{
					Action: providerv1.SessionControl_ACTION_STOP,
					Reason: "RAV payer does not match session",
				},
			},
		})
		return
	}
	if !sidecar.AddressesEqual(signedRAV.Message.ServiceProvider, s.serviceProvider) {
		stream.Send(&providerv1.PaymentSessionResponse{
			Message: &providerv1.PaymentSessionResponse_SessionControl{
				SessionControl: &providerv1.SessionControl{
					Action: providerv1.SessionControl_ACTION_STOP,
					Reason: "RAV service provider does not match",
				},
			},
		})
		return
	}
	if !sidecar.AddressesEqual(signedRAV.Message.DataService, session.DataService) {
		stream.Send(&providerv1.PaymentSessionResponse{
			Message: &providerv1.PaymentSessionResponse_SessionControl{
				SessionControl: &providerv1.SessionControl{
					Action: providerv1.SessionControl_ACTION_STOP,
					Reason: "RAV data service does not match session",
				},
			},
		})
		return
	}

	// Check if signer is authorized
	isAuthorized, err := s.isSignerAuthorized(ctx, session.Payer, signerAddr)
	if err != nil {
		s.logger.Warn("authorization check failed", zap.Error(err))
		stream.Send(&providerv1.PaymentSessionResponse{
			Message: &providerv1.PaymentSessionResponse_SessionControl{
				SessionControl: &providerv1.SessionControl{
					Action: providerv1.SessionControl_ACTION_STOP,
					Reason: "authorization check failed",
				},
			},
		})
		return
	}
	if !isAuthorized {
		s.logger.Warn("RAV signer not authorized", zap.Stringer("signer", signerAddr))
		stream.Send(&providerv1.PaymentSessionResponse{
			Message: &providerv1.PaymentSessionResponse_SessionControl{
				SessionControl: &providerv1.SessionControl{
					Action: providerv1.SessionControl_ACTION_STOP,
					Reason: "signer not authorized",
				},
			},
		})
		return
	}

	// Verify RAV value is greater than or equal to previous RAV
	currentRAV := session.CurrentRAV
	if currentRAV != nil && currentRAV.Message != nil {
		if signedRAV.Message.ValueAggregate.Cmp(currentRAV.Message.ValueAggregate) < 0 {
			stream.Send(&providerv1.PaymentSessionResponse{
				Message: &providerv1.PaymentSessionResponse_SessionControl{
					SessionControl: &providerv1.SessionControl{
						Action: providerv1.SessionControl_ACTION_STOP,
						Reason: "RAV value is less than current RAV",
					},
				},
			})
			return
		}
	}

	// Enforce that the submitted RAV covers the server-computed usage since baseline.
	// Do not trust caller-provided Usage.cost.
	currentValue := big.NewInt(0)
	if currentRAV != nil && currentRAV.Message != nil && currentRAV.Message.ValueAggregate != nil {
		currentValue = currentRAV.Message.ValueAggregate
	}
	_, _, _, deltaCost := session.UsageDeltaSinceBaseline()
	minValue := new(big.Int).Add(currentValue, deltaCost)
	if signedRAV.Message.ValueAggregate.Cmp(minValue) < 0 {
		stream.Send(&providerv1.PaymentSessionResponse{
			Message: &providerv1.PaymentSessionResponse_SessionControl{
				SessionControl: &providerv1.SessionControl{
					Action: providerv1.SessionControl_ACTION_STOP,
					Reason: fmt.Sprintf("RAV underpays usage: want >= %s (current %s + delta %s)", minValue.String(), currentValue.String(), deltaCost.String()),
				},
			},
		})
		return
	}

	// Store the new RAV
	session.CurrentRAV = signedRAV
	session.MarkBaseline()

	// Update the session in the repository
	if err := s.repo.SessionUpdate(ctx, session); err != nil {
		s.logger.Warn("failed to update session", zap.String("session_id", sessionID), zap.Error(err))
	}

	s.logger.Info("RAV accepted via stream",
		zap.String("session_id", sessionID),
		zap.Stringer("signer", signerAddr),
		zap.Stringer("value", signedRAV.Message.ValueAggregate),
	)

	// Send continue message
	stream.Send(&providerv1.PaymentSessionResponse{
		Message: &providerv1.PaymentSessionResponse_SessionControl{
			SessionControl: &providerv1.SessionControl{
				Action: providerv1.SessionControl_ACTION_CONTINUE,
			},
		},
	})
}

func (s *Gateway) handleFundsAcknowledgment(
	ctx context.Context,
	stream *connect.BidiStream[providerv1.PaymentSessionRequest, providerv1.PaymentSessionResponse],
	sessionID string,
	session *repository.Session,
	ack *providerv1.FundsAcknowledgment,
) {
	s.logger.Debug("received funds acknowledgment",
		zap.String("session_id", sessionID),
		zap.Bool("will_deposit", ack.WillDeposit),
	)

	// For now, just continue if they will deposit
	if ack.WillDeposit {
		stream.Send(&providerv1.PaymentSessionResponse{
			Message: &providerv1.PaymentSessionResponse_SessionControl{
				SessionControl: &providerv1.SessionControl{
					Action: providerv1.SessionControl_ACTION_CONTINUE,
				},
			},
		})
	} else {
		// They won't deposit more funds, stop the session
		stream.Send(&providerv1.PaymentSessionResponse{
			Message: &providerv1.PaymentSessionResponse_SessionControl{
				SessionControl: &providerv1.SessionControl{
					Action: providerv1.SessionControl_ACTION_STOP,
					Reason: "insufficient funds and no deposit planned",
				},
			},
		})
	}
}

func (s *Gateway) handleUsageReport(
	ctx context.Context,
	stream *connect.BidiStream[providerv1.PaymentSessionRequest, providerv1.PaymentSessionResponse],
	sessionID string,
	session *repository.Session,
	awaitingRAV bool,
	report *providerv1.UsageReport,
) (newAwaitingRAV bool, terminate bool) {
	s.logger.Debug("received usage report via stream",
		zap.String("session_id", sessionID),
		zap.Uint64("blocks", report.GetUsage().GetBlocksProcessed()),
	)

	if report == nil || report.Usage == nil {
		_ = stream.Send(&providerv1.PaymentSessionResponse{
			Message: &providerv1.PaymentSessionResponse_SessionControl{
				SessionControl: &providerv1.SessionControl{
					Action: providerv1.SessionControl_ACTION_STOP,
					Reason: "<usage> is required",
				},
			},
		})
		return awaitingRAV, true
	}

	// Provider gateway is cost-authoritative: compute cost from raw metering inputs.
	computedCost := session.CalculateUsageCost(report.Usage.BlocksProcessed, report.Usage.BytesTransferred)
	if report.Usage.Cost != nil {
		providedCost := report.Usage.Cost.ToNative()
		computedCostGRT := sds.NewGRTFromBigInt(computedCost)
		if providedCost.Cmp(&computedCostGRT) != 0 {
			s.logger.Warn("usage.cost mismatch in stream; overriding with computed cost",
				zap.String("session_id", sessionID),
				zap.Stringer("provided_cost", &providedCost),
				zap.Stringer("computed_cost", &computedCostGRT),
				zap.Uint64("blocks", report.Usage.BlocksProcessed),
				zap.Uint64("bytes", report.Usage.BytesTransferred),
			)
		}
	}

	session.AddUsage(
		report.Usage.BlocksProcessed,
		report.Usage.BytesTransferred,
		report.Usage.Requests,
		computedCost,
	)

	assessment := s.assessSessionFunds(ctx, session)
	applyFundsAssessmentMetadata(session, assessment)

	if assessment.unknown() {
		if assessment.checkErr != nil {
			s.logger.Warn("unable to determine escrow balance during PaymentSession; continuing",
				zap.String("session_id", sessionID),
				zap.Error(assessment.checkErr),
			)
		} else {
			s.logger.Warn("escrow balance unavailable during PaymentSession; continuing",
				zap.String("session_id", sessionID),
			)
		}
	}

	if assessment.insufficient() {
		session.End(commonv1.EndReason_END_REASON_PAYMENT_ISSUE)
	}

	// Update the session in the repository after usage and funds evaluation.
	if err := s.repo.SessionUpdate(ctx, session); err != nil {
		s.logger.Warn("failed to update session",
			zap.String("session_id", sessionID),
			zap.Error(err),
		)
	}

	if assessment.insufficient() {
		s.logger.Info("stopping session due to insufficient funds",
			zap.String("session_id", sessionID),
			zap.Stringer("current_outstanding", assessment.currentOutstanding),
			zap.Stringer("projected_outstanding", assessment.projectedOutstanding),
			zap.Stringer("escrow_balance", assessment.escrowBalance),
			zap.Stringer("minimum_needed", assessment.minimumNeeded),
		)

		stream.Send(needMoreFundsResponse(session, assessment))
		return awaitingRAV, true
	}

	if !awaitingRAV && s.shouldRequestRAV(session) {
		blocks, bytes, reqs, deltaCost := session.UsageDeltaSinceBaseline()
		currentRAV := session.CurrentRAV
		if currentRAV != nil {
			usage := &commonv1.Usage{
				BlocksProcessed:  blocks,
				BytesTransferred: bytes,
				Requests:         reqs,
				Cost:             commonv1.GRTFromBigInt(deltaCost),
			}

			stream.Send(&providerv1.PaymentSessionResponse{
				Message: &providerv1.PaymentSessionResponse_RavRequest{
					RavRequest: &providerv1.RAVRequest{
						CurrentRav: sidecar.HorizonSignedRAVToProto(currentRAV),
						Usage:      usage,
						Deadline:   uint64(time.Now().Add(30 * time.Second).Unix()),
					},
				},
			})
			return true, false
		}
	}

	stream.Send(&providerv1.PaymentSessionResponse{
		Message: &providerv1.PaymentSessionResponse_SessionControl{
			SessionControl: &providerv1.SessionControl{
				Action: providerv1.SessionControl_ACTION_CONTINUE,
			},
		},
	})
	return awaitingRAV, false
}
