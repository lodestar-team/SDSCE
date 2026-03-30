package sidecar

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/graphprotocol/substreams-data-service/horizon"
	consumerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1/providerv1connect"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"go.uber.org/zap"
)

// Init initializes a new payment session with a provider.
// This is called by substreams before connecting to a provider.
// Returns the initial RAV to use for authentication.
func (s *Sidecar) Init(
	ctx context.Context,
	req *connect.Request[consumerv1.InitRequest],
) (*connect.Response[consumerv1.InitResponse], error) {
	s.logger.Info("init called",
		zap.String("provider_control_plane_endpoint", req.Msg.ProviderControlPlaneEndpoint),
	)

	// Extract escrow account details
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

	// Create a zero-value RAV for a fresh session. MVP init no longer accepts resume input.
	var collectionID horizon.CollectionID
	initialRAV, err := s.signRAV(
		collectionID,
		payer,
		dataService,
		receiver,
		uint64(time.Now().UnixNano()),
		big.NewInt(0),
		nil,
	)
	if err != nil {
		s.logger.Error("failed to sign initial RAV", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	providerSelection, err := s.resolveProviderSelection(
		ctx,
		ea,
		req.Msg.ProviderControlPlaneEndpoint,
		req.Msg.SubstreamsPackage,
		req.Msg.RequestedNetwork,
	)
	if err != nil {
		return nil, err
	}

	parsedEndpoint := sidecar.ParseEndpoint(providerSelection.ControlPlaneEndpoint)
	if parsedEndpoint.URL == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid <provider_control_plane_endpoint>"))
	}

	gatewayClient := providerv1connect.NewPaymentGatewayServiceClient(parsedEndpoint.HTTPClient(), parsedEndpoint.URL)
	gatewayResp, err := gatewayClient.StartSession(ctx, connect.NewRequest(&providerv1.StartSessionRequest{
		EscrowAccount: ea,
		InitialRav:    sidecar.HorizonSignedRAVToProto(initialRAV),
	}))
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, err)
	}

	if !gatewayResp.Msg.Accepted {
		reason := strings.TrimSpace(gatewayResp.Msg.RejectionReason)
		if reason == "" {
			reason = "provider rejected session"
		}
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New(reason))
	}

	sessionID := strings.TrimSpace(gatewayResp.Msg.SessionId)
	if sessionID == "" {
		return nil, connect.NewError(connect.CodeInternal, errors.New("provider returned an empty session id"))
	}

	dataPlaneEndpoint := strings.TrimSpace(gatewayResp.Msg.DataPlaneEndpoint)
	if dataPlaneEndpoint == "" {
		return nil, connect.NewError(connect.CodeInternal, errors.New("provider returned an empty data-plane endpoint"))
	}

	s.logger.Info("provider session started",
		zap.String("provider_control_plane_endpoint", parsedEndpoint.URL),
		zap.String("provider_session_id", sessionID),
		zap.String("data_plane_endpoint", dataPlaneEndpoint),
		zap.String("oracle_provider_id", providerSelection.ProviderID),
		zap.String("network", providerSelection.Network),
	)

	var providerPricingConfig *sidecar.PricingConfig
	if gatewayResp.Msg.PricingConfig != nil {
		providerPricingConfig = gatewayResp.Msg.PricingConfig.ToNative()
		s.logger.Debug("received confirmatory pricing config from provider",
			zap.Stringer("price_per_block", &providerPricingConfig.PricePerBlock),
			zap.Stringer("price_per_byte", &providerPricingConfig.PricePerByte),
		)
	}

	effectivePricingConfig, err := effectiveSessionPricing(providerPricingConfig, providerSelection.OraclePricing)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	logPricingDeviation(s.logger, providerSelection.OraclePricing, providerPricingConfig)

	if gatewayResp.Msg.UseRav != nil {
		useRAV, err := sidecar.ProtoSignedRAVToHorizon(gatewayResp.Msg.UseRav)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("invalid <use_rav> received from provider gateway: %w", err))
		}
		if useRAV != nil {
			initialRAV = useRAV
		}
	}

	session, err := s.sessions.CreateWithID(sessionID, payer, receiver, dataService)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("unable to create session: %w", err))
	}

	session.SetRAV(initialRAV)
	if effectivePricingConfig != nil {
		session.SetPricingConfig(effectivePricingConfig)
	}
	s.paymentSessions.SetEndpoint(sessionID, parsedEndpoint.URL)

	s.logger.Debug("created session",
		zap.String("session_id", session.ID),
		zap.Stringer("payer", payer),
		zap.Stringer("receiver", receiver),
		zap.Stringer("data_service", dataService),
	)

	response := &consumerv1.InitResponse{
		Session:           session.ToSessionInfo(),
		PaymentRav:        sidecar.HorizonSignedRAVToProto(initialRAV),
		DataPlaneEndpoint: dataPlaneEndpoint,
	}

	s.logger.Info("Init completed",
		zap.String("session_id", session.ID),
	)

	return connect.NewResponse(response), nil
}
