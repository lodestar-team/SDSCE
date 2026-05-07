package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/graphprotocol/substreams-data-service/internal/operatorauth"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/streamingfast/eth-go"
)

const (
	defaultOperatorListLimit = 100
	maxOperatorListLimit     = 1000
)

func (s *OperatorGateway) ListSessions(
	ctx context.Context,
	req *connect.Request[providerv1.ListSessionsRequest],
) (*connect.Response[providerv1.ListSessionsResponse], error) {
	if err := s.authorizeRead(req.Header()); err != nil {
		return nil, err
	}

	payer, err := optionalAddress(req.Msg.GetPayer(), "payer")
	if err != nil {
		return nil, err
	}
	receiver, err := optionalAddress(req.Msg.GetReceiver(), "receiver")
	if err != nil {
		return nil, err
	}
	dataService, err := optionalAddress(req.Msg.GetDataService(), "data_service")
	if err != nil {
		return nil, err
	}
	status, err := operatorSessionStatusToRepository(req.Msg.GetStatus())
	if err != nil {
		return nil, err
	}
	fundsStatus, err := protoFundsStatusToMetadata(req.Msg.GetFundsStatus())
	if err != nil {
		return nil, err
	}

	filter := repository.SessionFilter{
		Payer:  payer,
		Status: status,
	}
	sessions, err := s.paymentGateway.repo.SessionList(ctx, filter)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	limit := operatorLimit(req.Msg.GetLimit())
	resp := &providerv1.ListSessionsResponse{
		Sessions: make([]*providerv1.OperatorSession, 0, min(limit, len(sessions))),
	}
	for _, session := range sessions {
		if !sessionMatchesAddresses(session, receiver, dataService) {
			continue
		}
		if fundsStatus != nil && sessionFundsStatus(session) != *fundsStatus {
			continue
		}
		operatorSession, err := s.sessionToProto(ctx, session, req.Msg.GetIncludeRav())
		if err != nil {
			return nil, err
		}
		resp.Sessions = append(resp.Sessions, operatorSession)
		if len(resp.Sessions) >= limit {
			break
		}
	}

	return connect.NewResponse(resp), nil
}

func (s *OperatorGateway) GetSession(
	ctx context.Context,
	req *connect.Request[providerv1.GetSessionRequest],
) (*connect.Response[providerv1.GetSessionResponse], error) {
	if err := s.authorizeRead(req.Header()); err != nil {
		return nil, err
	}

	sessionID := req.Msg.GetSessionId()
	if sessionID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("<session_id> is required"))
	}

	session, err := s.paymentGateway.repo.SessionGet(ctx, sessionID)
	if err != nil {
		return nil, repoError(err)
	}

	operatorSession, err := s.sessionToProto(ctx, session, req.Msg.GetIncludeRav())
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&providerv1.GetSessionResponse{Session: operatorSession}), nil
}

func (s *OperatorGateway) ListAcceptedRAVs(
	ctx context.Context,
	req *connect.Request[providerv1.ListAcceptedRAVsRequest],
) (*connect.Response[providerv1.ListAcceptedRAVsResponse], error) {
	if err := s.authorizeRead(req.Header()); err != nil {
		return nil, err
	}

	payer, err := optionalAddress(req.Msg.GetPayer(), "payer")
	if err != nil {
		return nil, err
	}
	serviceProvider, err := optionalAddress(req.Msg.GetServiceProvider(), "service_provider")
	if err != nil {
		return nil, err
	}
	dataService, err := optionalAddress(req.Msg.GetDataService(), "data_service")
	if err != nil {
		return nil, err
	}
	collectionID, err := optionalCollectionID(req.Msg.GetCollectionId(), "collection_id")
	if err != nil {
		return nil, err
	}

	sessions, err := acceptedRAVSessions(ctx, s.paymentGateway.repo, req.Msg.GetSessionId(), payer)
	if err != nil {
		return nil, connectOrInternal(err)
	}

	limit := operatorLimit(req.Msg.GetLimit())
	resp := &providerv1.ListAcceptedRAVsResponse{
		Ravs: make([]*providerv1.AcceptedRAV, 0, min(limit, len(sessions))),
	}
	for _, session := range sessions {
		if session.CurrentRAV == nil || session.CurrentRAV.Message == nil {
			continue
		}
		if !ravMatches(session.CurrentRAV, serviceProvider, dataService, collectionID) {
			continue
		}
		accepted, err := s.acceptedRAVToProto(ctx, session.ID, session.CurrentRAV)
		if err != nil {
			return nil, err
		}
		resp.Ravs = append(resp.Ravs, accepted)
		if len(resp.Ravs) >= limit {
			break
		}
	}

	return connect.NewResponse(resp), nil
}

func (s *OperatorGateway) GetAcceptedRAV(
	ctx context.Context,
	req *connect.Request[providerv1.GetAcceptedRAVRequest],
) (*connect.Response[providerv1.GetAcceptedRAVResponse], error) {
	if err := s.authorizeRead(req.Header()); err != nil {
		return nil, err
	}

	sessionID := req.Msg.GetSessionId()
	if sessionID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("<session_id> is required"))
	}

	session, err := s.paymentGateway.repo.SessionGet(ctx, sessionID)
	if err != nil {
		return nil, repoError(err)
	}
	if session.CurrentRAV == nil || session.CurrentRAV.Message == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("session %q has no accepted RAV", sessionID))
	}

	accepted, err := s.acceptedRAVToProto(ctx, session.ID, session.CurrentRAV)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&providerv1.GetAcceptedRAVResponse{Rav: accepted}), nil
}

func (s *OperatorGateway) ListCollections(
	ctx context.Context,
	req *connect.Request[providerv1.ListCollectionsRequest],
) (*connect.Response[providerv1.ListCollectionsResponse], error) {
	if err := s.authorizeRead(req.Header()); err != nil {
		return nil, err
	}

	payer, err := optionalAddress(req.Msg.GetPayer(), "payer")
	if err != nil {
		return nil, err
	}
	serviceProvider, err := optionalAddress(req.Msg.GetServiceProvider(), "service_provider")
	if err != nil {
		return nil, err
	}
	dataService, err := optionalAddress(req.Msg.GetDataService(), "data_service")
	if err != nil {
		return nil, err
	}
	collectionID, err := optionalCollectionID(req.Msg.GetCollectionId(), "collection_id")
	if err != nil {
		return nil, err
	}
	state, err := protoCollectionStateToRepository(req.Msg.GetState())
	if err != nil {
		return nil, err
	}

	filter := repository.CollectionFilter{
		Payer: payer,
		State: state,
	}
	if sessionID := req.Msg.GetSessionId(); sessionID != "" {
		filter.SessionID = &sessionID
	}

	records, err := s.paymentGateway.repo.CollectionList(ctx, filter)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	limit := operatorLimit(req.Msg.GetLimit())
	resp := &providerv1.ListCollectionsResponse{
		Collections: make([]*providerv1.CollectionRecord, 0, min(limit, len(records))),
	}
	for _, record := range records {
		if !collectionRecordMatches(record, serviceProvider, dataService, collectionID) {
			continue
		}
		resp.Collections = append(resp.Collections, collectionRecordToProto(record))
		if len(resp.Collections) >= limit {
			break
		}
	}

	return connect.NewResponse(resp), nil
}

func (s *OperatorGateway) GetCollection(
	ctx context.Context,
	req *connect.Request[providerv1.GetCollectionRequest],
) (*connect.Response[providerv1.GetCollectionResponse], error) {
	if err := s.authorizeRead(req.Header()); err != nil {
		return nil, err
	}

	key, err := collectionKeyFromProto(req.Msg.GetKey())
	if err != nil {
		return nil, err
	}
	record, err := s.paymentGateway.repo.CollectionGet(ctx, key)
	if err != nil {
		return nil, repoError(err)
	}

	return connect.NewResponse(&providerv1.GetCollectionResponse{Collection: collectionRecordToProto(record)}), nil
}

func (s *OperatorGateway) MarkCollectionPending(
	ctx context.Context,
	req *connect.Request[providerv1.MarkCollectionPendingRequest],
) (*connect.Response[providerv1.MarkCollectionPendingResponse], error) {
	if err := s.authorizeAdmin(req.Header()); err != nil {
		return nil, err
	}

	key, expectedValue, err := mutationKeyAndExpectedValue(req.Msg.GetKey(), req.Msg.GetExpectedValue())
	if err != nil {
		return nil, err
	}
	record, err := s.paymentGateway.repo.CollectionMarkPending(ctx, key, expectedValue, req.Msg.GetTxHash(), time.Now())
	if err != nil {
		return nil, collectionMutationError(err)
	}

	return connect.NewResponse(&providerv1.MarkCollectionPendingResponse{Collection: collectionRecordToProto(record)}), nil
}

func (s *OperatorGateway) MarkCollectionCollected(
	ctx context.Context,
	req *connect.Request[providerv1.MarkCollectionCollectedRequest],
) (*connect.Response[providerv1.MarkCollectionCollectedResponse], error) {
	if err := s.authorizeAdmin(req.Header()); err != nil {
		return nil, err
	}

	key, expectedValue, err := mutationKeyAndExpectedValue(req.Msg.GetKey(), req.Msg.GetExpectedValue())
	if err != nil {
		return nil, err
	}
	if req.Msg.GetTxHash() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("<tx_hash> is required"))
	}

	var collectedAmount *big.Int
	if req.Msg.GetCollectedAmount() != nil {
		collectedAmount = req.Msg.GetCollectedAmount().ToBigInt()
	}
	record, err := s.paymentGateway.repo.CollectionMarkCollected(ctx, key, expectedValue, req.Msg.GetTxHash(), collectedAmount, time.Now())
	if err != nil {
		return nil, collectionMutationError(err)
	}

	return connect.NewResponse(&providerv1.MarkCollectionCollectedResponse{Collection: collectionRecordToProto(record)}), nil
}

func (s *OperatorGateway) MarkCollectionRetryable(
	ctx context.Context,
	req *connect.Request[providerv1.MarkCollectionRetryableRequest],
) (*connect.Response[providerv1.MarkCollectionRetryableResponse], error) {
	if err := s.authorizeAdmin(req.Header()); err != nil {
		return nil, err
	}

	key, expectedValue, err := mutationKeyAndExpectedValue(req.Msg.GetKey(), req.Msg.GetExpectedValue())
	if err != nil {
		return nil, err
	}
	if req.Msg.GetLastError() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("<last_error> is required"))
	}
	record, err := s.paymentGateway.repo.CollectionMarkFailedRetryable(ctx, key, expectedValue, req.Msg.GetTxHash(), req.Msg.GetLastError(), time.Now())
	if err != nil {
		return nil, collectionMutationError(err)
	}

	return connect.NewResponse(&providerv1.MarkCollectionRetryableResponse{Collection: collectionRecordToProto(record)}), nil
}

func (s *OperatorGateway) authorizeRead(header http.Header) error {
	_, err := s.authorize(header, operatorauth.RoleOperatorRead)
	return err
}

func (s *OperatorGateway) authorizeAdmin(header http.Header) error {
	_, err := s.authorize(header, operatorauth.RoleAdminWrite)
	return err
}

func (s *OperatorGateway) sessionToProto(ctx context.Context, session *repository.Session, includeRAV bool) (*providerv1.OperatorSession, error) {
	paymentControlPending, err := s.paymentGateway.paymentControlPending(ctx, session)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	out := &providerv1.OperatorSession{
		SessionId:             session.ID,
		Status:                repositorySessionStatusToProto(session.Status),
		Payer:                 commonv1.AddressFromEth(session.Payer),
		Receiver:              commonv1.AddressFromEth(session.Receiver),
		DataService:           commonv1.AddressFromEth(session.DataService),
		CreatedAtNs:           timeToUnixNano(session.CreatedAt),
		UpdatedAtNs:           timeToUnixNano(session.UpdatedAt),
		EndReason:             session.EndReason,
		AccumulatedUsage:      session.GetUsage(),
		BaselineUsage:         sessionBaselineUsage(session),
		PaymentControlPending: paymentControlPending,
		PaymentState:          operatorPaymentState(session, paymentControlPending),
	}
	if session.EndedAt != nil {
		out.EndedAtNs = timeToUnixNano(*session.EndedAt)
	}
	if includeRAV && session.CurrentRAV != nil {
		accepted, err := s.acceptedRAVToProto(ctx, session.ID, session.CurrentRAV)
		if err != nil {
			return nil, err
		}
		out.AcceptedRav = accepted
	}
	return out, nil
}

func (s *OperatorGateway) acceptedRAVToProto(ctx context.Context, sessionID string, rav *horizon.SignedRAV) (*providerv1.AcceptedRAV, error) {
	out := acceptedRAVToProto(sessionID, rav)
	if out == nil {
		return nil, nil
	}

	record, err := s.paymentGateway.repo.CollectionGet(ctx, collectionKeyFromRAV(sessionID, rav))
	if err == nil {
		out.CollectionState = repositoryCollectionStateToProto(record.State)
		return out, nil
	}
	if errors.Is(err, repository.ErrNotFound) {
		return out, nil
	}
	return nil, connect.NewError(connect.CodeInternal, err)
}

func acceptedRAVToProto(sessionID string, rav *horizon.SignedRAV) *providerv1.AcceptedRAV {
	if rav == nil || rav.Message == nil {
		return nil
	}

	return &providerv1.AcceptedRAV{
		SessionId:       sessionID,
		CollectionId:    append([]byte(nil), rav.Message.CollectionID[:]...),
		Payer:           commonv1.AddressFromEth(rav.Message.Payer),
		ServiceProvider: commonv1.AddressFromEth(rav.Message.ServiceProvider),
		DataService:     commonv1.AddressFromEth(rav.Message.DataService),
		TimestampNs:     rav.Message.TimestampNs,
		ValueAggregate:  commonv1.GRTFromBigInt(rav.Message.ValueAggregate),
		SignedRav:       sidecar.HorizonSignedRAVToProto(rav),
		CollectionState: providerv1.CollectionState_COLLECTION_STATE_UNSPECIFIED,
	}
}

func operatorPaymentState(session *repository.Session, paymentControlPending bool) *providerv1.OperatorPaymentState {
	currentRAVValue := sessionCurrentRAVValue(session)
	accumulatedUsageValue := cloneOrZero(session.TotalCost)
	currentOutstanding := sessionMetadataBigInt(session, fundsCurrentOutstandingWeiKey, currentRAVValue)
	projectedOutstanding := sessionMetadataBigInt(session, fundsProjectedOutstandingWeiKey, sessionProjectedOutstanding(session))
	escrowBalance, hasEscrowBalance := sessionMetadataBigIntOK(session, fundsEscrowBalanceWeiKey)
	minimumNeeded := sessionMetadataBigInt(session, fundsMinimumNeededWeiKey, big.NewInt(0))
	fundsStatus := repositoryFundsStatusToProto(sessionFundsStatus(session))

	paymentStatus := &commonv1.PaymentStatus{
		CurrentRavValue:       commonv1.GRTFromBigInt(currentRAVValue),
		AccumulatedUsageValue: commonv1.GRTFromBigInt(accumulatedUsageValue),
		FundsSufficient:       fundsStatus == providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_OK,
	}
	if hasEscrowBalance {
		paymentStatus.EscrowBalance = commonv1.GRTFromBigInt(escrowBalance)
	}
	paymentStatus.EstimatedBlocksRemaining = estimatedBlocksRemaining(session, projectedOutstanding, escrowBalance, hasEscrowBalance)

	return &providerv1.OperatorPaymentState{
		PaymentStatus:        paymentStatus,
		FundsStatus:          fundsStatus,
		CurrentOutstanding:   commonv1.GRTFromBigInt(currentOutstanding),
		ProjectedOutstanding: commonv1.GRTFromBigInt(projectedOutstanding),
		MinimumNeeded:        commonv1.GRTFromBigInt(minimumNeeded),
		FundsCheckError:      session.Metadata[fundsCheckErrorKey],
		OperatorHint:         operatorPaymentHint(session, fundsStatus, paymentControlPending),
	}
}

func sessionCurrentRAVValue(session *repository.Session) *big.Int {
	if session.CurrentRAV != nil && session.CurrentRAV.Message != nil && session.CurrentRAV.Message.ValueAggregate != nil {
		return new(big.Int).Set(session.CurrentRAV.Message.ValueAggregate)
	}
	return big.NewInt(0)
}

func sessionProjectedOutstanding(session *repository.Session) *big.Int {
	currentOutstanding := sessionCurrentRAVValue(session)
	_, _, _, deltaCost := session.UsageDeltaSinceBaseline()
	return new(big.Int).Add(currentOutstanding, deltaCost)
}

func sessionFundsStatus(session *repository.Session) string {
	status := session.Metadata[fundsStatusKey]
	if status != "" {
		return status
	}
	if session.EndReason == commonv1.EndReason_END_REASON_PAYMENT_ISSUE {
		return fundsStatusInsufficient
	}
	return fundsStatusUnknown
}

func sessionMetadataBigInt(session *repository.Session, key string, fallback *big.Int) *big.Int {
	value, ok := sessionMetadataBigIntOK(session, key)
	if ok {
		return value
	}
	return cloneOrZero(fallback)
}

func sessionMetadataBigIntOK(session *repository.Session, key string) (*big.Int, bool) {
	raw := session.Metadata[key]
	if raw == "" {
		return nil, false
	}
	value, ok := new(big.Int).SetString(raw, 10)
	if !ok || value.Sign() < 0 {
		return nil, false
	}
	return value, true
}

func estimatedBlocksRemaining(session *repository.Session, projectedOutstanding *big.Int, escrowBalance *big.Int, hasEscrowBalance bool) uint64 {
	if !hasEscrowBalance || escrowBalance == nil || projectedOutstanding == nil {
		return 0
	}
	pricePerBlock := session.PricingConfig.PricePerBlock.BigInt()
	if pricePerBlock.Sign() <= 0 {
		return 0
	}
	remaining := new(big.Int).Sub(escrowBalance, projectedOutstanding)
	if remaining.Sign() <= 0 {
		return 0
	}
	return new(big.Int).Div(remaining, pricePerBlock).Uint64()
}

func cloneOrZero(value *big.Int) *big.Int {
	if value == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(value)
}

func repositoryFundsStatusToProto(status string) providerv1.OperatorFundsStatus {
	switch status {
	case fundsStatusOK:
		return providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_OK
	case fundsStatusInsufficient:
		return providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_INSUFFICIENT
	case fundsStatusUnknown:
		return providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_UNKNOWN
	default:
		return providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_UNSPECIFIED
	}
}

func protoFundsStatusToMetadata(status providerv1.OperatorFundsStatus) (*string, error) {
	switch status {
	case providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_UNSPECIFIED:
		return nil, nil
	case providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_OK:
		return ptr(fundsStatusOK), nil
	case providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_INSUFFICIENT:
		return ptr(fundsStatusInsufficient), nil
	case providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_UNKNOWN:
		return ptr(fundsStatusUnknown), nil
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unsupported funds_status %d", status))
	}
}

func operatorPaymentHint(session *repository.Session, fundsStatus providerv1.OperatorFundsStatus, paymentControlPending bool) string {
	if paymentControlPending {
		return "provider payment control is pending; wait for the runtime RAV or funding decision before taking operator action"
	}
	switch fundsStatus {
	case providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_INSUFFICIENT:
		if session.EndReason == commonv1.EndReason_END_REASON_PAYMENT_ISSUE {
			return "session ended due to insufficient funds; top up escrow before starting a replacement session"
		}
		return "escrow funding is below projected outstanding usage; top up escrow before continuing heavy usage"
	case providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_UNKNOWN:
		if session.Metadata[fundsCheckErrorKey] != "" {
			return "escrow funding could not be checked; inspect RPC and escrow connectivity"
		}
		return "escrow funding has not been assessed yet for this session"
	case providerv1.OperatorFundsStatus_OPERATOR_FUNDS_STATUS_OK:
		return "escrow funding was sufficient at the last provider assessment"
	default:
		return ""
	}
}

func ptr[T any](value T) *T {
	return &value
}

func collectionRecordToProto(record *repository.CollectionRecord) *providerv1.CollectionRecord {
	if record == nil {
		return nil
	}

	out := &providerv1.CollectionRecord{
		Key:            collectionKeyToProto(record.Key),
		State:          repositoryCollectionStateToProto(record.State),
		SignedRav:      sidecar.HorizonSignedRAVToProto(record.SignedRAV),
		ValueAggregate: commonv1.GRTFromBigInt(record.ValueAggregate),
		AttemptCount:   uint64(record.AttemptCount),
		LastTxHash:     record.LastTxHash,
		LastError:      record.LastError,
		CreatedAtNs:    timeToUnixNano(record.CreatedAt),
		UpdatedAtNs:    timeToUnixNano(record.UpdatedAt),
	}
	if record.CollectedAmount != nil {
		out.CollectedAmount = commonv1.GRTFromBigInt(record.CollectedAmount)
	}
	return out
}

func collectionKeyFromProto(key *providerv1.CollectionKey) (repository.CollectionKey, error) {
	if key == nil {
		return repository.CollectionKey{}, connect.NewError(connect.CodeInvalidArgument, errors.New("<key> is required"))
	}
	if key.GetSessionId() == "" {
		return repository.CollectionKey{}, connect.NewError(connect.CodeInvalidArgument, errors.New("<key.session_id> is required"))
	}
	collectionID, err := requiredCollectionID(key.GetCollectionId(), "key.collection_id")
	if err != nil {
		return repository.CollectionKey{}, err
	}
	payer, err := requiredAddress(key.GetPayer(), "key.payer")
	if err != nil {
		return repository.CollectionKey{}, err
	}
	serviceProvider, err := requiredAddress(key.GetServiceProvider(), "key.service_provider")
	if err != nil {
		return repository.CollectionKey{}, err
	}
	dataService, err := requiredAddress(key.GetDataService(), "key.data_service")
	if err != nil {
		return repository.CollectionKey{}, err
	}

	return repository.CollectionKey{
		SessionID:       key.GetSessionId(),
		CollectionID:    collectionID,
		Payer:           payer,
		ServiceProvider: serviceProvider,
		DataService:     dataService,
	}, nil
}

func collectionKeyToProto(key repository.CollectionKey) *providerv1.CollectionKey {
	return &providerv1.CollectionKey{
		SessionId:       key.SessionID,
		CollectionId:    append([]byte(nil), key.CollectionID[:]...),
		Payer:           commonv1.AddressFromEth(key.Payer),
		ServiceProvider: commonv1.AddressFromEth(key.ServiceProvider),
		DataService:     commonv1.AddressFromEth(key.DataService),
	}
}

func collectionKeyFromRAV(sessionID string, rav *horizon.SignedRAV) repository.CollectionKey {
	return repository.CollectionKey{
		SessionID:       sessionID,
		CollectionID:    rav.Message.CollectionID,
		Payer:           rav.Message.Payer,
		ServiceProvider: rav.Message.ServiceProvider,
		DataService:     rav.Message.DataService,
	}
}

func mutationKeyAndExpectedValue(key *providerv1.CollectionKey, expectedValue *commonv1.GRT) (repository.CollectionKey, *big.Int, error) {
	repositoryKey, err := collectionKeyFromProto(key)
	if err != nil {
		return repository.CollectionKey{}, nil, err
	}
	if expectedValue == nil {
		return repository.CollectionKey{}, nil, connect.NewError(connect.CodeInvalidArgument, errors.New("<expected_value> is required"))
	}
	return repositoryKey, expectedValue.ToBigInt(), nil
}

func requiredAddress(addr *commonv1.Address, name string) (ethAddress, error) {
	out, err := addr.ToEth()
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid <%s>: %w", name, err))
	}
	return out, nil
}

type ethAddress = eth.Address

func optionalAddress(addr *commonv1.Address, name string) (*ethAddress, error) {
	if addr == nil {
		return nil, nil
	}
	out, err := requiredAddress(addr, name)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func requiredCollectionID(raw []byte, name string) (horizon.CollectionID, error) {
	collectionID, err := optionalCollectionID(raw, name)
	if err != nil {
		return horizon.CollectionID{}, err
	}
	if collectionID == nil {
		return horizon.CollectionID{}, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("<%s> is required", name))
	}
	return *collectionID, nil
}

func optionalCollectionID(raw []byte, name string) (*horizon.CollectionID, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) != 32 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid <%s>: got %d bytes, want 32", name, len(raw)))
	}
	var collectionID horizon.CollectionID
	copy(collectionID[:], raw)
	return &collectionID, nil
}

func acceptedRAVSessions(ctx context.Context, repo repository.GlobalRepository, sessionID string, payer *ethAddress) ([]*repository.Session, error) {
	if sessionID != "" {
		session, err := repo.SessionGet(ctx, sessionID)
		if err != nil {
			return nil, repoError(err)
		}
		if payer != nil && !bytes.Equal(session.Payer, *payer) {
			return nil, nil
		}
		return []*repository.Session{session}, nil
	}

	sessions, err := repo.SessionList(ctx, repository.SessionFilter{Payer: payer})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return sessions, nil
}

func sessionMatchesAddresses(session *repository.Session, receiver, dataService *ethAddress) bool {
	if receiver != nil && !bytes.Equal(session.Receiver, *receiver) {
		return false
	}
	if dataService != nil && !bytes.Equal(session.DataService, *dataService) {
		return false
	}
	return true
}

func ravMatches(rav *horizon.SignedRAV, serviceProvider, dataService *ethAddress, collectionID *horizon.CollectionID) bool {
	if serviceProvider != nil && !bytes.Equal(rav.Message.ServiceProvider, *serviceProvider) {
		return false
	}
	if dataService != nil && !bytes.Equal(rav.Message.DataService, *dataService) {
		return false
	}
	if collectionID != nil && rav.Message.CollectionID != *collectionID {
		return false
	}
	return true
}

func collectionRecordMatches(record *repository.CollectionRecord, serviceProvider, dataService *ethAddress, collectionID *horizon.CollectionID) bool {
	if serviceProvider != nil && !bytes.Equal(record.Key.ServiceProvider, *serviceProvider) {
		return false
	}
	if dataService != nil && !bytes.Equal(record.Key.DataService, *dataService) {
		return false
	}
	if collectionID != nil && record.Key.CollectionID != *collectionID {
		return false
	}
	return true
}

func operatorLimit(limit uint32) int {
	if limit == 0 {
		return defaultOperatorListLimit
	}
	if limit > maxOperatorListLimit {
		return maxOperatorListLimit
	}
	return int(limit)
}

func sessionBaselineUsage(session *repository.Session) *commonv1.Usage {
	return &commonv1.Usage{
		BlocksProcessed:  session.BaselineBlocks,
		BytesTransferred: session.BaselineBytes,
		Requests:         session.BaselineReqs,
		Cost:             commonv1.GRTFromBigInt(session.BaselineCost),
	}
}

func repositorySessionStatusToProto(status repository.SessionStatus) providerv1.OperatorSessionStatus {
	switch status {
	case repository.SessionStatusActive:
		return providerv1.OperatorSessionStatus_OPERATOR_SESSION_STATUS_ACTIVE
	case repository.SessionStatusPaused:
		return providerv1.OperatorSessionStatus_OPERATOR_SESSION_STATUS_PAUSED
	case repository.SessionStatusTerminated:
		return providerv1.OperatorSessionStatus_OPERATOR_SESSION_STATUS_TERMINATED
	default:
		return providerv1.OperatorSessionStatus_OPERATOR_SESSION_STATUS_UNSPECIFIED
	}
}

func operatorSessionStatusToRepository(status providerv1.OperatorSessionStatus) (*repository.SessionStatus, error) {
	switch status {
	case providerv1.OperatorSessionStatus_OPERATOR_SESSION_STATUS_UNSPECIFIED:
		return nil, nil
	case providerv1.OperatorSessionStatus_OPERATOR_SESSION_STATUS_ACTIVE:
		out := repository.SessionStatusActive
		return &out, nil
	case providerv1.OperatorSessionStatus_OPERATOR_SESSION_STATUS_PAUSED:
		out := repository.SessionStatusPaused
		return &out, nil
	case providerv1.OperatorSessionStatus_OPERATOR_SESSION_STATUS_TERMINATED:
		out := repository.SessionStatusTerminated
		return &out, nil
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid <status> %d", status))
	}
}

func repositoryCollectionStateToProto(state repository.CollectionState) providerv1.CollectionState {
	switch state {
	case repository.CollectionStateCollectible:
		return providerv1.CollectionState_COLLECTION_STATE_COLLECTIBLE
	case repository.CollectionStateCollectPending:
		return providerv1.CollectionState_COLLECTION_STATE_COLLECT_PENDING
	case repository.CollectionStateCollected:
		return providerv1.CollectionState_COLLECTION_STATE_COLLECTED
	case repository.CollectionStateCollectFailedRetryable:
		return providerv1.CollectionState_COLLECTION_STATE_COLLECT_FAILED_RETRYABLE
	default:
		return providerv1.CollectionState_COLLECTION_STATE_UNSPECIFIED
	}
}

func protoCollectionStateToRepository(state providerv1.CollectionState) (*repository.CollectionState, error) {
	switch state {
	case providerv1.CollectionState_COLLECTION_STATE_UNSPECIFIED:
		return nil, nil
	case providerv1.CollectionState_COLLECTION_STATE_COLLECTIBLE:
		out := repository.CollectionStateCollectible
		return &out, nil
	case providerv1.CollectionState_COLLECTION_STATE_COLLECT_PENDING:
		out := repository.CollectionStateCollectPending
		return &out, nil
	case providerv1.CollectionState_COLLECTION_STATE_COLLECTED:
		out := repository.CollectionStateCollected
		return &out, nil
	case providerv1.CollectionState_COLLECTION_STATE_COLLECT_FAILED_RETRYABLE:
		out := repository.CollectionStateCollectFailedRetryable
		return &out, nil
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid <state> %d", state))
	}
}

func repoError(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}

func connectOrInternal(err error) error {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr
	}
	return connect.NewError(connect.CodeInternal, err)
}

func collectionMutationError(err error) error {
	switch {
	case errors.Is(err, repository.ErrCollectionConflict):
		return connect.NewError(connect.CodeAborted, err)
	case errors.Is(err, repository.ErrInvalidCollectionTransition):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, repository.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

func timeToUnixNano(t time.Time) uint64 {
	if t.IsZero() {
		return 0
	}
	nano := t.UnixNano()
	if nano < 0 {
		return 0
	}
	return uint64(nano)
}
