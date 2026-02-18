package sidecar

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"go.uber.org/zap"
)

// ValidatePayment validates a RAV received from a client.
// Called by the provider when a client connects with a payment header.
func (s *Sidecar) ValidatePayment(
	ctx context.Context,
	req *connect.Request[providerv1.ValidatePaymentRequest],
) (*connect.Response[providerv1.ValidatePaymentResponse], error) {
	s.logger.Info("ValidatePayment called")

	if req.Msg.PaymentRav == nil {
		return connect.NewResponse(&providerv1.ValidatePaymentResponse{
			Valid:           false,
			RejectionReason: "invalid or missing RAV",
		}), nil
	}

	// Convert proto RAV to horizon RAV for verification
	signedRAV, err := sidecar.ProtoSignedRAVToHorizon(req.Msg.PaymentRav)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid <payment_rav>: %w", err))
	}

	// Verify the signature
	signerAddr, err := signedRAV.RecoverSigner(s.domain)
	if err != nil {
		s.logger.Warn("failed to verify RAV signature", zap.Error(err))
		return connect.NewResponse(&providerv1.ValidatePaymentResponse{
			Valid:           false,
			RejectionReason: fmt.Sprintf("signature verification failed: %v", err),
		}), nil
	}

	// Check if signer is authorized
	isAuthorized, err := s.isSignerAuthorized(ctx, signedRAV.Message.Payer, signerAddr)
	if err != nil {
		s.logger.Warn("authorization check failed", zap.Error(err))
		return connect.NewResponse(&providerv1.ValidatePaymentResponse{
			Valid:           false,
			RejectionReason: fmt.Sprintf("authorization check failed: %v", err),
		}), nil
	}
	if !isAuthorized {
		s.logger.Warn("signer not authorized",
			zap.Stringer("signer", signerAddr),
		)
		return connect.NewResponse(&providerv1.ValidatePaymentResponse{
			Valid:           false,
			RejectionReason: fmt.Sprintf("signer %s is not authorized", signerAddr.Pretty()),
		}), nil
	}

	// Verify RAV is for this service provider
	if !sidecar.AddressesEqual(signedRAV.Message.ServiceProvider, s.serviceProvider) {
		s.logger.Warn("RAV is for different service provider",
			zap.Stringer("expected", s.serviceProvider),
			zap.Stringer("got", signedRAV.Message.ServiceProvider),
		)
		return connect.NewResponse(&providerv1.ValidatePaymentResponse{
			Valid:           false,
			RejectionReason: "RAV is for a different service provider",
		}), nil
	}

	// Create or get session
	payer := signedRAV.Message.Payer
	dataService := signedRAV.Message.DataService

	// Look for existing session or create new one
	var session *sidecar.Session
	if req.Msg.ClientSessionId != "" {
		session, err = s.sessions.Get(req.Msg.ClientSessionId)
		if err != nil {
			// Create new session if not found, using the client-provided ID.
			session, err = s.sessions.CreateWithID(req.Msg.ClientSessionId, payer, s.serviceProvider, dataService)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("unable to create session: %w", err))
			}
		}
	} else {
		session = s.sessions.Create(payer, s.serviceProvider, dataService)
	}

	// Store the RAV
	session.SetRAV(signedRAV)
	session.MarkBaseline()

	// Set pricing config on session
	session.SetPricingConfig(s.pricingConfig)

	// Query escrow balance from chain
	var availableBalance *commonv1.BigInt
	if escrowBalance, err := s.GetEscrowBalance(ctx, payer); err != nil {
		s.logger.Warn("failed to query escrow balance", zap.Error(err))
	} else if escrowBalance != nil {
		availableBalance = commonv1.BigIntFromNative(escrowBalance)
	}

	// Build response
	response := &providerv1.ValidatePaymentResponse{
		Valid:         true,
		SessionId:     session.ID,
		ServiceParams: req.Msg.ServiceParams, // Echo back the service params
		EscrowAccount: &commonv1.EscrowAccount{
			Payer:       commonv1.AddressFromEth(payer),
			Receiver:    commonv1.AddressFromEth(s.serviceProvider),
			DataService: commonv1.AddressFromEth(dataService),
		},
		AvailableBalance: availableBalance,
	}

	s.logger.Info("ValidatePayment succeeded",
		zap.String("session_id", session.ID),
		zap.Stringer("payer", payer),
		zap.Stringer("signer", signerAddr),
	)

	return connect.NewResponse(response), nil
}
