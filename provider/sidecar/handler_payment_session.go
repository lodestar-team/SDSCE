package sidecar

import (
	"context"
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"

	"connectrpc.com/connect"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"go.uber.org/zap"
)

// PaymentSession is a bidirectional stream for ongoing payment negotiation.
// This allows the provider sidecar to request RAVs and notify about
// funding requirements in real-time.
func (s *Sidecar) PaymentSession(
	ctx context.Context,
	stream *connect.BidiStream[providerv1.PaymentSessionRequest, providerv1.PaymentSessionResponse],
) error {
	s.logger.Info("PaymentSession stream started")

	var session *sidecar.Session
	var sessionID string
	var awaitingRAV bool

	for {
		// Receive message from consumer sidecar
		msg, err := stream.Receive()
		if err == io.EOF {
			s.logger.Info("PaymentSession stream closed by client")
			return nil
		}
		if err != nil {
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

			session, err = s.sessions.Get(sessionID)
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
			awaitingRAV = s.handleUsageReport(ctx, stream, sessionID, session, awaitingRAV, m.UsageReport)

		default:
			s.logger.Warn("unknown message type in PaymentSession")
		}
	}
}

func (s *Sidecar) handleRAVSubmission(
	ctx context.Context,
	stream *connect.BidiStream[providerv1.PaymentSessionRequest, providerv1.PaymentSessionResponse],
	sessionID string,
	session *sidecar.Session,
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
	if signedRAV == nil || signedRAV.Message == nil {
		s.logger.Warn("invalid RAV submission")
		// Send stop message
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
	signerAddr, err := s.verifyRAVSignature(signedRAV)
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
	currentRAV := session.GetRAV()
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

	// Store the new RAV
	session.SetRAV(signedRAV)
	session.MarkBaseline()

	s.logger.Info("RAV accepted via stream",
		zap.String("session_id", sessionID),
		zap.Stringer("signer", signerAddr),
		zap.String("value", signedRAV.Message.ValueAggregate.String()),
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

func (s *Sidecar) handleFundsAcknowledgment(
	ctx context.Context,
	stream *connect.BidiStream[providerv1.PaymentSessionRequest, providerv1.PaymentSessionResponse],
	sessionID string,
	session *sidecar.Session,
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

func (s *Sidecar) handleUsageReport(
	ctx context.Context,
	stream *connect.BidiStream[providerv1.PaymentSessionRequest, providerv1.PaymentSessionResponse],
	sessionID string,
	session *sidecar.Session,
	awaitingRAV bool,
	report *providerv1.UsageReport,
) bool {
	s.logger.Debug("received usage report via stream",
		zap.String("session_id", sessionID),
		zap.Uint64("blocks", report.Usage.GetBlocksProcessed()),
	)

	if report != nil && report.Usage != nil {
		var cost *big.Int
		if report.Usage.Cost != nil {
			cost = report.Usage.Cost.ToNative()
		}
		session.AddUsage(
			report.Usage.BlocksProcessed,
			report.Usage.BytesTransferred,
			report.Usage.Requests,
			cost,
		)
	}

	if !awaitingRAV {
		blocks, bytes, reqs, deltaCost := session.UsageDeltaSinceBaseline()
		if deltaCost.Sign() > 0 {
			currentRAV := session.GetRAV()
			if currentRAV != nil {
				usage := &commonv1.Usage{
					BlocksProcessed:  blocks,
					BytesTransferred: bytes,
					Requests:         reqs,
					Cost:             commonv1.BigIntFromNative(deltaCost),
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
				return true
			}
		}
	}

	stream.Send(&providerv1.PaymentSessionResponse{
		Message: &providerv1.PaymentSessionResponse_SessionControl{
			SessionControl: &providerv1.SessionControl{
				Action: providerv1.SessionControl_ACTION_CONTINUE,
			},
		},
	})
	return awaitingRAV
}
