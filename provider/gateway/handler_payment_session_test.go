package gateway

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/graphprotocol/substreams-data-service/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"github.com/streamingfast/eth-go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type cancellationAwareRepo struct {
	*repository.InMemoryRepository
}

func (r *cancellationAwareRepo) SessionGet(ctx context.Context, sessionID string) (*repository.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return r.InMemoryRepository.SessionGet(context.Background(), sessionID)
}

func (r *cancellationAwareRepo) SessionUpdate(ctx context.Context, session *repository.Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return r.InMemoryRepository.SessionUpdate(context.Background(), session)
}

func (r *cancellationAwareRepo) SessionUpdateRAVAndBaseline(ctx context.Context, sessionID string, currentRAV *horizon.SignedRAV, baselineBlocks, baselineBytes, baselineReqs uint64, baselineCost *big.Int) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return r.InMemoryRepository.SessionUpdateRAVAndBaseline(context.Background(), sessionID, currentRAV, baselineBlocks, baselineBytes, baselineReqs, baselineCost)
}

func (r *cancellationAwareRepo) SessionUpdateRuntimeState(ctx context.Context, sessionID string, status repository.SessionStatus, metadata map[string]string, endedAt *time.Time, endReason commonv1.EndReason, updatedAt time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return r.InMemoryRepository.SessionUpdateRuntimeState(context.Background(), sessionID, status, metadata, endedAt, endReason, updatedAt)
}

type ravCommitFailRepo struct {
	*repository.InMemoryRepository
	err error
}

func (r *ravCommitFailRepo) SessionUpdateRAVAndBaseline(context.Context, string, *horizon.SignedRAV, uint64, uint64, uint64, *big.Int) error {
	return r.err
}

type evalFailAfterCommitRepo struct {
	*repository.InMemoryRepository
	err            error
	failSessionGet bool
}

func (r *evalFailAfterCommitRepo) SessionUpdateRAVAndBaseline(ctx context.Context, sessionID string, currentRAV *horizon.SignedRAV, baselineBlocks, baselineBytes, baselineReqs uint64, baselineCost *big.Int) error {
	if err := r.InMemoryRepository.SessionUpdateRAVAndBaseline(ctx, sessionID, currentRAV, baselineBlocks, baselineBytes, baselineReqs, baselineCost); err != nil {
		return err
	}
	r.failSessionGet = true
	return nil
}

func (r *evalFailAfterCommitRepo) SessionGet(ctx context.Context, sessionID string) (*repository.Session, error) {
	if r.failSessionGet {
		return nil, r.err
	}
	return r.InMemoryRepository.SessionGet(ctx, sessionID)
}

func TestHandleRAVSubmission_PersistsAcceptedRAVAfterCallerCancellation(t *testing.T) {
	payerKey, err := eth.NewPrivateKey("0x0000000000000000000000000000000000000000000000000000000000000042")
	require.NoError(t, err)

	payer := payerKey.PublicKey().Address()
	serviceProvider := eth.MustNewAddress("0x2222222222222222222222222222222222222222")
	dataService := eth.MustNewAddress("0x3333333333333333333333333333333333333333")
	collector := eth.MustNewAddress("0x4444444444444444444444444444444444444444")

	repo := &cancellationAwareRepo{InMemoryRepository: repository.NewInMemoryRepository()}
	gateway := &Gateway{
		logger:              zap.NewNop(),
		serviceProvider:     serviceProvider,
		domain:              horizon.NewDomain(1337, collector),
		repo:                repo,
		runtime:             newRuntimeManager(),
		ravRequestThreshold: big.NewInt(10),
	}

	sessionID := "session-cancelled-rav"
	session := repository.NewSession(sessionID, payer, serviceProvider, dataService, repository.PricingConfig{})
	session.CurrentRAV = &horizon.SignedRAV{
		Message: &horizon.RAV{
			Payer:           payer,
			ServiceProvider: serviceProvider,
			DataService:     dataService,
			ValueAggregate:  big.NewInt(0),
		},
	}
	require.NoError(t, repo.SessionCreate(context.Background(), session))

	usage := &commonv1.Usage{
		BlocksProcessed:  1,
		BytesTransferred: 0,
		Requests:         1,
		Cost:             commonv1.GRTFromBigInt(big.NewInt(1)),
	}

	gateway.runtime.sessions[sessionID] = &runtimeSessionState{
		pendingRAV: newPendingRAVRequest(session, usage),
	}

	rav := &horizon.RAV{
		Payer:           payer,
		ServiceProvider: serviceProvider,
		DataService:     dataService,
		TimestampNs:     1,
		ValueAggregate:  big.NewInt(1),
	}
	signedRAV, err := horizon.Sign(gateway.domain, rav, payerKey)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resp := gateway.handleRAVSubmission(ctx, sessionID, session, &providerv1.SignedRAVSubmission{
		SignedRav: sidecar.HorizonSignedRAVToProto(signedRAV),
		Usage:     usage,
	})
	require.NotNil(t, resp)
	require.NotNil(t, resp.GetSessionControl())
	require.Equal(t, providerv1.SessionControl_ACTION_CONTINUE, resp.GetSessionControl().GetAction())

	updated, err := repo.SessionGet(context.Background(), sessionID)
	require.NoError(t, err)
	require.NotNil(t, updated.CurrentRAV)
	require.Equal(t, 0, big.NewInt(1).Cmp(updated.CurrentRAV.Message.ValueAggregate))
	require.Equal(t, uint64(0), updated.BaselineBlocks)
	require.Equal(t, 0, big.NewInt(0).Cmp(updated.BaselineCost))

	gateway.runtime.mu.Lock()
	state := gateway.runtime.sessions[sessionID]
	gateway.runtime.mu.Unlock()
	require.NotNil(t, state)
	require.Nil(t, state.pendingRAV)
	require.Nil(t, state.queuedResponse)
}

func TestHandleRAVSubmission_KeepsPendingStateWhenAcceptedRAVCommitFails(t *testing.T) {
	payerKey, err := eth.NewPrivateKey("0x0000000000000000000000000000000000000000000000000000000000000043")
	require.NoError(t, err)

	payer := payerKey.PublicKey().Address()
	serviceProvider := eth.MustNewAddress("0x2222222222222222222222222222222222222222")
	dataService := eth.MustNewAddress("0x3333333333333333333333333333333333333333")
	collector := eth.MustNewAddress("0x4444444444444444444444444444444444444444")

	baseRepo := repository.NewInMemoryRepository()
	repo := &ravCommitFailRepo{
		InMemoryRepository: baseRepo,
		err:                errors.New("commit failed"),
	}
	gateway := &Gateway{
		logger:              zap.NewNop(),
		serviceProvider:     serviceProvider,
		domain:              horizon.NewDomain(1337, collector),
		repo:                repo,
		runtime:             newRuntimeManager(),
		ravRequestThreshold: big.NewInt(10),
	}

	sessionID := "session-rav-commit-fails"
	session := repository.NewSession(sessionID, payer, serviceProvider, dataService, repository.PricingConfig{})
	session.CurrentRAV = &horizon.SignedRAV{
		Message: &horizon.RAV{
			Payer:           payer,
			ServiceProvider: serviceProvider,
			DataService:     dataService,
			ValueAggregate:  big.NewInt(0),
		},
	}
	require.NoError(t, repo.SessionCreate(context.Background(), session))

	usage := &commonv1.Usage{
		BlocksProcessed:  1,
		BytesTransferred: 0,
		Requests:         1,
		Cost:             commonv1.GRTFromBigInt(big.NewInt(1)),
	}
	pending := newPendingRAVRequest(session, usage)
	gateway.runtime.sessions[sessionID] = &runtimeSessionState{
		pendingRAV:     pending,
		queuedResponse: continuePaymentSessionResponse(),
	}

	rav := &horizon.RAV{
		Payer:           payer,
		ServiceProvider: serviceProvider,
		DataService:     dataService,
		TimestampNs:     1,
		ValueAggregate:  big.NewInt(1),
	}
	signedRAV, err := horizon.Sign(gateway.domain, rav, payerKey)
	require.NoError(t, err)

	resp := gateway.handleRAVSubmission(context.Background(), sessionID, session, &providerv1.SignedRAVSubmission{
		SignedRav: sidecar.HorizonSignedRAVToProto(signedRAV),
		Usage:     usage,
	})
	require.NotNil(t, resp)
	require.Equal(t, providerv1.SessionControl_ACTION_STOP, resp.GetSessionControl().GetAction())

	updated, err := repo.SessionGet(context.Background(), sessionID)
	require.NoError(t, err)
	require.Equal(t, 0, big.NewInt(0).Cmp(updated.CurrentRAV.Message.ValueAggregate))
	assertPending := gateway.runtime.hasPendingRAV(sessionID)
	require.True(t, assertPending, "expected failed commit to keep the pending RAV")
}

func TestHandleRAVSubmission_PostCommitEvaluationFailureDoesNotStopAcceptedRAV(t *testing.T) {
	payerKey, err := eth.NewPrivateKey("0x0000000000000000000000000000000000000000000000000000000000000044")
	require.NoError(t, err)

	payer := payerKey.PublicKey().Address()
	serviceProvider := eth.MustNewAddress("0x2222222222222222222222222222222222222222")
	dataService := eth.MustNewAddress("0x3333333333333333333333333333333333333333")
	collector := eth.MustNewAddress("0x4444444444444444444444444444444444444444")

	repo := &evalFailAfterCommitRepo{
		InMemoryRepository: repository.NewInMemoryRepository(),
		err:                errors.New("post-commit evaluation failed"),
	}
	gateway := &Gateway{
		logger:              zap.NewNop(),
		serviceProvider:     serviceProvider,
		domain:              horizon.NewDomain(1337, collector),
		repo:                repo,
		runtime:             newRuntimeManager(),
		ravRequestThreshold: big.NewInt(10),
	}

	sessionID := "session-rav-eval-fails"
	session := repository.NewSession(sessionID, payer, serviceProvider, dataService, repository.PricingConfig{})
	session.CurrentRAV = &horizon.SignedRAV{
		Message: &horizon.RAV{
			Payer:           payer,
			ServiceProvider: serviceProvider,
			DataService:     dataService,
			ValueAggregate:  big.NewInt(0),
		},
	}
	require.NoError(t, repo.SessionCreate(context.Background(), session))

	usage := &commonv1.Usage{
		BlocksProcessed: 1,
		Requests:        1,
		Cost:            commonv1.GRTFromBigInt(big.NewInt(1)),
	}
	gateway.runtime.sessions[sessionID] = &runtimeSessionState{
		pendingRAV: newPendingRAVRequest(session, usage),
	}

	rav := &horizon.RAV{
		Payer:           payer,
		ServiceProvider: serviceProvider,
		DataService:     dataService,
		TimestampNs:     1,
		ValueAggregate:  big.NewInt(1),
	}
	signedRAV, err := horizon.Sign(gateway.domain, rav, payerKey)
	require.NoError(t, err)

	resp := gateway.handleRAVSubmission(context.Background(), sessionID, session, &providerv1.SignedRAVSubmission{
		SignedRav: sidecar.HorizonSignedRAVToProto(signedRAV),
		Usage:     usage,
	})
	require.NotNil(t, resp)
	require.Equal(t, providerv1.SessionControl_ACTION_CONTINUE, resp.GetSessionControl().GetAction())
	require.False(t, gateway.runtime.hasPendingRAV(sessionID))

	repo.failSessionGet = false
	updated, err := repo.SessionGet(context.Background(), sessionID)
	require.NoError(t, err)
	require.NotNil(t, updated.CurrentRAV)
	require.Equal(t, 0, big.NewInt(1).Cmp(updated.CurrentRAV.Message.ValueAggregate))
}
