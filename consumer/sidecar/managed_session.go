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
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1/providerv1connect"
	sidecarlib "github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/streamingfast/eth-go"
	pbsubstreams "github.com/streamingfast/substreams/pb/sf/substreams/v1"
	"go.uber.org/zap"
)

type sessionBootstrapInput struct {
	Payer                        eth.Address
	Receiver                     *eth.Address
	DataService                  eth.Address
	ProviderControlPlaneEndpoint string
	SubstreamsPackage            *pbsubstreams.Package
	RequestedNetwork             string
}

type sessionBootstrapResult struct {
	LocalSession      *sidecarlib.Session
	PaymentRAV        *commonv1.SignedRAV
	DataPlaneEndpoint string
}

func (s *Sidecar) bootstrapManagedSession(
	ctx context.Context,
	input sessionBootstrapInput,
) (*sessionBootstrapResult, error) {
	providerSelection, err := s.resolveProviderSelection(
		ctx,
		input.Receiver,
		input.ProviderControlPlaneEndpoint,
		input.SubstreamsPackage,
		input.RequestedNetwork,
	)
	if err != nil {
		return nil, err
	}

	initialRAV, err := s.signRAV(
		horizon.CollectionID{},
		input.Payer,
		input.DataService,
		providerSelection.Receiver,
		uint64(time.Now().UnixNano()),
		big.NewInt(0),
		nil,
	)
	if err != nil {
		s.logger.Error("failed to sign initial RAV", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	parsedEndpoint := sidecarlib.ParseEndpoint(providerSelection.ControlPlaneEndpoint)
	if parsedEndpoint.URL == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid <provider_control_plane_endpoint>"))
	}

	gatewayClient := providerv1connect.NewPaymentGatewayServiceClient(parsedEndpoint.HTTPClient(), parsedEndpoint.URL)
	escrowAccount := &commonv1.EscrowAccount{
		Payer:       commonv1.AddressFromEth(input.Payer),
		Receiver:    commonv1.AddressFromEth(providerSelection.Receiver),
		DataService: commonv1.AddressFromEth(input.DataService),
	}
	gatewayResp, err := gatewayClient.StartSession(ctx, connect.NewRequest(&providerv1.StartSessionRequest{
		EscrowAccount: escrowAccount,
		InitialRav:    sidecarlib.HorizonSignedRAVToProto(initialRAV),
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

	var providerPricingConfig *sidecarlib.PricingConfig
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
		useRAV, err := sidecarlib.ProtoSignedRAVToHorizon(gatewayResp.Msg.UseRav)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("invalid <use_rav> received from provider gateway: %w", err))
		}
		if useRAV != nil {
			initialRAV = useRAV
		}
	}

	session, err := s.sessions.CreateWithID(sessionID, input.Payer, providerSelection.Receiver, input.DataService)
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
		zap.Stringer("payer", input.Payer),
		zap.Stringer("receiver", providerSelection.Receiver),
		zap.Stringer("data_service", input.DataService),
	)

	return &sessionBootstrapResult{
		LocalSession:      session,
		PaymentRAV:        sidecarlib.HorizonSignedRAVToProto(initialRAV),
		DataPlaneEndpoint: dataPlaneEndpoint,
	}, nil
}
