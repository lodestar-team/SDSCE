package sidecar

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"go.uber.org/zap"
)

// StartSession initiates a payment session with the provider.
// The consumer sidecar calls this to establish a session before
// the substreams client connects to the provider.
func (s *Sidecar) StartSession(
	ctx context.Context,
	req *connect.Request[providerv1.StartSessionRequest],
) (*connect.Response[providerv1.StartSessionResponse], error) {
	s.logger.Info("StartSession called")

	// Extract escrow account
	ea := req.Msg.EscrowAccount
	if ea == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("<escrow_account> is required"))
	}
	if ea.Payer == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("<escrow_account.payer> is required"))
	}
	if ea.Receiver == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("<escrow_account.receiver> is required"))
	}
	if ea.DataService == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("<escrow_account.data_service> is required"))
	}
	payer, err := ea.Payer.ToEth()
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid <escrow_account.payer>: %w", err))
	}
	receiver, err := ea.Receiver.ToEth()
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid <escrow_account.receiver>: %w", err))
	}
	dataService, err := ea.DataService.ToEth()
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid <escrow_account.data_service>: %w", err))
	}

	// Verify receiver matches this service provider
	if !sidecar.AddressesEqual(receiver, s.serviceProvider) {
		s.logger.Warn("escrow account receiver mismatch",
			zap.Stringer("expected", s.serviceProvider),
			zap.Stringer("got", receiver),
		)
		return connect.NewResponse(&providerv1.StartSessionResponse{
			Accepted:        false,
			RejectionReason: "escrow account receiver does not match this service provider",
		}), nil
	}

	// Validate initial RAV if provided
	initialRAV, err := sidecar.ProtoSignedRAVToHorizon(req.Msg.InitialRav)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid <initial_rav>: %w", err))
	}
	if initialRAV != nil && initialRAV.Message != nil {
		// Verify signature
		signerAddr, err := initialRAV.RecoverSigner(s.domain)
		if err != nil {
			s.logger.Warn("failed to verify initial RAV signature", zap.Error(err))
			return connect.NewResponse(&providerv1.StartSessionResponse{
				Accepted:        false,
				RejectionReason: fmt.Sprintf("initial RAV signature verification failed: %v", err),
			}), nil
		}

		// Check if signer is authorized
		isAuthorized, err := s.isSignerAuthorized(ctx, payer, signerAddr)
		if err != nil {
			s.logger.Warn("authorization check failed", zap.Error(err))
			return connect.NewResponse(&providerv1.StartSessionResponse{
				Accepted:        false,
				RejectionReason: fmt.Sprintf("authorization check failed: %v", err),
			}), nil
		}
		if !isAuthorized {
			s.logger.Warn("initial RAV signer not authorized",
				zap.Stringer("signer", signerAddr),
			)
			return connect.NewResponse(&providerv1.StartSessionResponse{
				Accepted:        false,
				RejectionReason: fmt.Sprintf("signer %s is not authorized", signerAddr.Pretty()),
			}), nil
		}

		// Verify RAV addresses match
		if !sidecar.AddressesEqual(initialRAV.Message.Payer, payer) {
			return connect.NewResponse(&providerv1.StartSessionResponse{
				Accepted:        false,
				RejectionReason: "RAV payer does not match escrow account payer",
			}), nil
		}
		if !sidecar.AddressesEqual(initialRAV.Message.ServiceProvider, s.serviceProvider) {
			return connect.NewResponse(&providerv1.StartSessionResponse{
				Accepted:        false,
				RejectionReason: "RAV service provider does not match",
			}), nil
		}
		if !sidecar.AddressesEqual(initialRAV.Message.DataService, dataService) {
			return connect.NewResponse(&providerv1.StartSessionResponse{
				Accepted:        false,
				RejectionReason: "RAV data service does not match escrow account data service",
			}), nil
		}
	}

	// Create session
	session := s.sessions.Create(payer, s.serviceProvider, dataService)
	session.SetPricingConfig(s.pricingConfig)
	if initialRAV != nil {
		session.SetRAV(initialRAV)
	}
	session.MarkBaseline()

	s.logger.Info("StartSession succeeded",
		zap.String("session_id", session.ID),
		zap.Stringer("payer", payer),
	)

	// Return the RAV to use (same as initial for now)
	response := &providerv1.StartSessionResponse{
		SessionId: session.ID,
		UseRav:    req.Msg.InitialRav, // Use the same RAV
		Accepted:  true,
	}

	return connect.NewResponse(response), nil
}
