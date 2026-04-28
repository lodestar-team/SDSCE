package sidecar

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	consumerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/consumer/v1"
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

	bootstrap, err := s.bootstrapManagedSession(ctx, sessionBootstrapInput{
		Payer:                        payer,
		Receiver:                     &receiver,
		DataService:                  dataService,
		ProviderControlPlaneEndpoint: req.Msg.ProviderControlPlaneEndpoint,
		SubstreamsPackage:            req.Msg.SubstreamsPackage,
		RequestedNetwork:             req.Msg.RequestedNetwork,
	})
	if err != nil {
		return nil, err
	}

	response := &consumerv1.InitResponse{
		Session:           bootstrap.LocalSession.ToSessionInfo(),
		PaymentRav:        bootstrap.PaymentRAV,
		DataPlaneEndpoint: bootstrap.DataPlaneEndpoint,
	}

	s.logger.Info("Init completed",
		zap.String("session_id", bootstrap.LocalSession.ID),
	)

	return connect.NewResponse(response), nil
}
