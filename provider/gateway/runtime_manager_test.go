package gateway

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/graphprotocol/substreams-data-service/horizon"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/streamingfast/eth-go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type sessionGetErrorRepo struct {
	repository.GlobalRepository
	err error
}

func (r *sessionGetErrorRepo) SessionGet(context.Context, string) (*repository.Session, error) {
	return nil, r.err
}

func TestRuntimeManager_BindFailureReleasesSessionState(t *testing.T) {
	ctx := context.Background()
	sessionID := "session-1"
	events := make(chan *providerv1.PaymentSessionResponse, 1)
	repo := &sessionGetErrorRepo{
		GlobalRepository: repository.NewInMemoryRepository(),
		err:              errors.New("session lookup failed"),
	}
	gateway := &Gateway{
		logger:              zap.NewNop(),
		repo:                repo,
		runtime:             newRuntimeManager(),
		ravRequestThreshold: big.NewInt(1),
	}

	err := gateway.runtime.bindSession(ctx, gateway, sessionID, events)
	require.Error(t, err)

	gateway.runtime.mu.Lock()
	_, ok := gateway.runtime.sessions[sessionID]
	gateway.runtime.mu.Unlock()
	require.False(t, ok, "expected failed bind to release runtime session state")
}

func TestRuntimeManager_UnbindSessionDeletesIdleState(t *testing.T) {
	sessionID := "session-2"
	events := make(chan *providerv1.PaymentSessionResponse, 1)
	manager := newRuntimeManager()

	manager.mu.Lock()
	manager.sessions[sessionID] = &runtimeSessionState{events: events}
	manager.mu.Unlock()

	manager.unbindSession(sessionID, events)

	manager.mu.Lock()
	_, ok := manager.sessions[sessionID]
	manager.mu.Unlock()
	require.False(t, ok, "expected idle session state to be removed on unbind")

	manager.unbindSession(sessionID, events)

	manager.mu.Lock()
	_, ok = manager.sessions[sessionID]
	manager.mu.Unlock()
	require.False(t, ok, "expected repeated unbind to remain a no-op")
}

func TestRuntimeManager_BindSessionDoesNotBlockOnControlDispatch(t *testing.T) {
	ctx := context.Background()
	sessionID := "session-3"
	events := make(chan *providerv1.PaymentSessionResponse)

	payer := eth.MustNewAddress("0x1111111111111111111111111111111111111111")
	serviceProvider := eth.MustNewAddress("0x2222222222222222222222222222222222222222")
	dataService := eth.MustNewAddress("0x3333333333333333333333333333333333333333")
	collector := eth.MustNewAddress("0x4444444444444444444444444444444444444444")

	repo := repository.NewInMemoryRepository()
	session := repository.NewSession(
		sessionID,
		payer,
		serviceProvider,
		dataService,
		repository.PricingConfig{},
	)
	session.CurrentRAV = &horizon.SignedRAV{
		Message: &horizon.RAV{
			ValueAggregate: big.NewInt(0),
		},
	}
	session.TotalCost = big.NewInt(10)
	require.NoError(t, repo.SessionCreate(ctx, session))

	gateway := &Gateway{
		logger:              zap.NewNop(),
		domain:              horizon.NewDomain(1337, collector),
		repo:                repo,
		runtime:             newRuntimeManager(),
		ravRequestThreshold: big.NewInt(1),
	}

	done := make(chan error, 1)
	go func() {
		done <- gateway.runtime.bindSession(ctx, gateway, sessionID, events)
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("expected bindSession to return without blocking on control dispatch")
	}

	gateway.runtime.unbindSession(sessionID, events)

	require.True(t, gateway.runtime.hasPendingRAV(sessionID), "expected dropped dispatch to preserve the pending RAV state")

	reboundEvents := make(chan *providerv1.PaymentSessionResponse, 1)
	require.NoError(t, gateway.runtime.bindSession(ctx, gateway, sessionID, reboundEvents))

	select {
	case resp := <-reboundEvents:
		require.NotNil(t, resp.GetRavRequest(), "expected queued RAV request to be replayed on rebind")
	case <-time.After(time.Second):
		t.Fatal("expected queued runtime control response to be replayed on rebind")
	}
	require.True(t, gateway.runtime.hasPendingRAV(sessionID), "expected replayed RAV request to preserve pending state")

	gateway.runtime.unbindSession(sessionID, reboundEvents)
}

func TestRuntimeManager_RebindRegeneratesDeliveredPendingRAV(t *testing.T) {
	ctx := context.Background()
	sessionID := "session-4"

	payer := eth.MustNewAddress("0x1111111111111111111111111111111111111111")
	serviceProvider := eth.MustNewAddress("0x2222222222222222222222222222222222222222")
	dataService := eth.MustNewAddress("0x3333333333333333333333333333333333333333")
	collector := eth.MustNewAddress("0x4444444444444444444444444444444444444444")

	repo := repository.NewInMemoryRepository()
	session := repository.NewSession(
		sessionID,
		payer,
		serviceProvider,
		dataService,
		repository.PricingConfig{},
	)
	session.CurrentRAV = &horizon.SignedRAV{
		Message: &horizon.RAV{
			ValueAggregate: big.NewInt(0),
		},
	}
	session.TotalCost = big.NewInt(10)
	require.NoError(t, repo.SessionCreate(ctx, session))

	gateway := &Gateway{
		logger:              zap.NewNop(),
		domain:              horizon.NewDomain(1337, collector),
		repo:                repo,
		runtime:             newRuntimeManager(),
		ravRequestThreshold: big.NewInt(1),
	}

	firstEvents := make(chan *providerv1.PaymentSessionResponse, 1)
	require.NoError(t, gateway.runtime.bindSession(ctx, gateway, sessionID, firstEvents))

	select {
	case resp := <-firstEvents:
		require.NotNil(t, resp.GetRavRequest(), "expected initial bind to deliver a RAV request")
	case <-time.After(time.Second):
		t.Fatal("expected initial bind to deliver a RAV request")
	}
	require.True(t, gateway.runtime.hasPendingRAV(sessionID), "expected delivered request to remain pending")

	gateway.runtime.unbindSession(sessionID, firstEvents)

	reboundEvents := make(chan *providerv1.PaymentSessionResponse, 1)
	require.NoError(t, gateway.runtime.bindSession(ctx, gateway, sessionID, reboundEvents))

	select {
	case resp := <-reboundEvents:
		require.NotNil(t, resp.GetRavRequest(), "expected rebound bind to regenerate the in-flight RAV request")
	case <-time.After(time.Second):
		t.Fatal("expected rebound bind to regenerate the in-flight RAV request")
	}
	require.True(t, gateway.runtime.hasPendingRAV(sessionID), "expected regenerated request to restore pending state")

	gateway.runtime.unbindSession(sessionID, reboundEvents)
}
