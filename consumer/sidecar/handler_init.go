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
		zap.String("gateway_endpoint", req.Msg.GatewayEndpoint),
		zap.String("substreams_endpoint", req.Msg.SubstreamsEndpoint),
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

	// Check if we have an existing RAV to continue from
	var existingRAV *horizon.SignedRAV
	if req.Msg.ExistingRav != nil {
		existingRAV, err = sidecar.ProtoSignedRAVToHorizon(req.Msg.ExistingRav)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid <existing_rav>: %w", err))
		}
		if existingRAV != nil && existingRAV.Message != nil {
			if !sidecar.AddressesEqual(existingRAV.Message.Payer, payer) {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("<existing_rav.rav.payer> %s does not match <escrow_account.payer> %s", existingRAV.Message.Payer.Pretty(), payer.Pretty()))
			}
			if !sidecar.AddressesEqual(existingRAV.Message.ServiceProvider, receiver) {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("<existing_rav.rav.service_provider> %s does not match <escrow_account.receiver> %s", existingRAV.Message.ServiceProvider.Pretty(), receiver.Pretty()))
			}
			if !sidecar.AddressesEqual(existingRAV.Message.DataService, dataService) {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("<existing_rav.rav.data_service> %s does not match <escrow_account.data_service> %s", existingRAV.Message.DataService.Pretty(), dataService.Pretty()))
			}
		}
	}

	// Create initial RAV (can be zero-value for new sessions)
	var initialRAV *horizon.SignedRAV

	if existingRAV != nil {
		// Use the existing RAV
		initialRAV = existingRAV
	} else {
		// Create a zero-value RAV for new sessions
		// This establishes the session parameters without committing to any value
		var collectionID horizon.CollectionID
		// Collection ID can be derived from session or left empty for now

		initialRAV, err = s.signRAV(
			collectionID,
			payer,
			dataService,
			receiver,
			uint64(time.Now().UnixNano()),
			big.NewInt(0), // Zero value
			nil,           // No metadata yet
		)
		if err != nil {
			s.logger.Error("failed to sign initial RAV", zap.Error(err))
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}

	parsedEndpoint := sidecar.ParseEndpoint(req.Msg.GatewayEndpoint)
	var sessionID string
	var providerPricingConfig *sidecar.PricingConfig
	if parsedEndpoint.URL != "" {
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

		sessionID = strings.TrimSpace(gatewayResp.Msg.SessionId)
		if sessionID == "" {
			return nil, connect.NewError(connect.CodeInternal, errors.New("provider returned an empty session id"))
		}

		s.logger.Info("provider session started",
			zap.String("gateway_endpoint", parsedEndpoint.URL),
			zap.String("provider_session_id", sessionID),
		)

		// Store pricing config from provider
		if gatewayResp.Msg.PricingConfig != nil {
			providerPricingConfig = gatewayResp.Msg.PricingConfig.ToNative()
			s.logger.Debug("received pricing config from provider",
				zap.Stringer("price_per_block", &providerPricingConfig.PricePerBlock),
				zap.Stringer("price_per_byte", &providerPricingConfig.PricePerByte),
			)
		}

		if gatewayResp.Msg.UseRav != nil {
			useRAV, err := sidecar.ProtoSignedRAVToHorizon(gatewayResp.Msg.UseRav)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("invalid <use_rav> received from provider gateway: %w", err))
			}
			if useRAV != nil {
				initialRAV = useRAV
			}
		}
	}

	var session *sidecar.Session
	if sessionID != "" {
		session, err = s.sessions.CreateWithID(sessionID, payer, receiver, dataService)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("unable to create session: %w", err))
		}
	} else {
		session = s.sessions.Create(payer, receiver, dataService)
		sessionID = session.ID
	}

	session.SetRAV(initialRAV)
	if providerPricingConfig != nil {
		session.SetPricingConfig(providerPricingConfig)
	}

	s.logger.Debug("created session",
		zap.String("session_id", session.ID),
		zap.Stringer("payer", payer),
		zap.Stringer("receiver", receiver),
		zap.Stringer("data_service", dataService),
	)

	response := &consumerv1.InitResponse{
		Session:    session.ToSessionInfo(),
		PaymentRav: sidecar.HorizonSignedRAVToProto(initialRAV),
	}

	s.logger.Info("Init completed",
		zap.String("session_id", session.ID),
	)

	return connect.NewResponse(response), nil
}
