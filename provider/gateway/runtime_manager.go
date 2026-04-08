package gateway

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/graphprotocol/substreams-data-service/sidecar"
	"go.uber.org/zap"
)

type runtimeManager struct {
	mu       sync.Mutex
	sessions map[string]*runtimeSessionState
}

type runtimeSessionState struct {
	evalMu         sync.Mutex
	events         chan *providerv1.PaymentSessionResponse
	pendingRAV     *pendingRAVRequest
	queuedResponse *providerv1.PaymentSessionResponse
}

func newRuntimeManager() *runtimeManager {
	return &runtimeManager{
		sessions: make(map[string]*runtimeSessionState),
	}
}

func (m *runtimeManager) bindSession(
	ctx context.Context,
	gateway *Gateway,
	sessionID string,
	events chan *providerv1.PaymentSessionResponse,
) error {
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if events == nil {
		return fmt.Errorf("events channel is required")
	}

	m.mu.Lock()
	current := m.ensureSessionStateLocked(sessionID)
	if current.events != nil {
		m.mu.Unlock()
		return fmt.Errorf("payment session stream already bound for %q", sessionID)
	}
	needsRefresh := current.pendingRAV != nil && current.queuedResponse == nil
	if needsRefresh {
		// The previous stream had already received the RAV request before it disconnected.
		// Clear the pending marker so the fresh bind can regenerate the in-flight request.
		current.pendingRAV = nil
	}
	current.events = events
	queued := current.queuedResponse
	pending := current.pendingRAV.clone()
	m.mu.Unlock()

	m.tryDeliverQueuedResponse(sessionID, events, queued, pending)

	if err := m.onMeteredUsage(ctx, gateway, sessionID); err != nil {
		m.cleanupSessionState(sessionID, events)
		return err
	}

	return nil
}

func (m *runtimeManager) unbindSession(sessionID string, events chan *providerv1.PaymentSessionResponse) {
	if sessionID == "" || events == nil {
		return
	}

	m.cleanupSessionState(sessionID, events)
}

func (m *runtimeManager) onMeteredUsage(ctx context.Context, gateway *Gateway, sessionID string) error {
	return m.withSessionEval(sessionID, func(_ *runtimeSessionState) error {
		return m.evaluateMeteredUsage(ctx, gateway, sessionID)
	})
}

func (m *runtimeManager) evaluateMeteredUsage(ctx context.Context, gateway *Gateway, sessionID string) error {
	session, err := gateway.repo.SessionGet(ctx, sessionID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return err
	}

	if session == nil || !session.IsActive() {
		return nil
	}

	assessment := gateway.assessSessionFunds(ctx, session)
	applyFundsAssessmentMetadata(session, assessment)

	if assessment.unknown() {
		if assessment.checkErr != nil {
			gateway.logger.Warn("unable to determine escrow balance during metered runtime evaluation; continuing",
				zap.String("session_id", sessionID),
				zap.Error(assessment.checkErr),
			)
		} else {
			gateway.logger.Warn("escrow balance unavailable during metered runtime evaluation; continuing",
				zap.String("session_id", sessionID),
			)
		}
	}

	if assessment.insufficient() {
		session.End(commonv1.EndReason_END_REASON_PAYMENT_ISSUE)
	}

	if err := gateway.repo.SessionUpdate(ctx, session); err != nil {
		return err
	}

	if assessment.insufficient() {
		m.clearPendingRAV(sessionID)
		gateway.logger.Info("stopping session due to insufficient funds from metered runtime evaluation",
			zap.String("session_id", sessionID),
		)
		m.dispatch(sessionID, needMoreFundsResponse(session, assessment), nil)
		return nil
	}

	if !gateway.shouldRequestRAV(session) {
		return nil
	}

	events, pending := m.runtimeState(sessionID)
	if events == nil || pending != nil {
		return nil
	}

	if session.CurrentRAV == nil {
		return nil
	}

	blocks, bytes, reqs, deltaCost := session.UsageDeltaSinceBaseline()
	usage := &commonv1.Usage{
		BlocksProcessed:  blocks,
		BytesTransferred: bytes,
		Requests:         reqs,
		Cost:             commonv1.GRTFromBigInt(deltaCost),
	}

	resp := &providerv1.PaymentSessionResponse{
		Message: &providerv1.PaymentSessionResponse_RavRequest{
			RavRequest: &providerv1.RAVRequest{
				CurrentRav: sidecar.HorizonSignedRAVToProto(session.CurrentRAV),
				Usage:      usage,
				Deadline:   uint64(time.Now().Add(30 * time.Second).Unix()),
			},
		},
	}

	m.dispatch(sessionID, resp, newPendingRAVRequest(session, usage))
	return nil
}

func (m *runtimeManager) runtimeState(sessionID string) (chan *providerv1.PaymentSessionResponse, *pendingRAVRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()

	current := m.sessions[sessionID]
	if current == nil {
		return nil, nil
	}

	return current.events, current.pendingRAV.clone()
}

func (m *runtimeManager) dispatch(sessionID string, resp *providerv1.PaymentSessionResponse, pending *pendingRAVRequest) {
	if resp == nil {
		return
	}

	var (
		events     chan *providerv1.PaymentSessionResponse
		pendingRef *pendingRAVRequest
	)

	m.mu.Lock()
	current := m.ensureSessionStateLocked(sessionID)
	events = current.events
	current.queuedResponse = resp
	current.pendingRAV = pending.clone()
	pendingRef = current.pendingRAV
	m.mu.Unlock()

	if events == nil {
		return
	}

	select {
	case events <- resp:
		m.clearQueuedResponse(sessionID, events, resp, pendingRef)
	default:
		// Best-effort dispatch: metering ingestion must not block on stream delivery.
		// The latest control response remains queued in runtime state for a later bind.
	}
}

func (m *runtimeManager) hasPendingRAV(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	current := m.sessions[sessionID]
	return current != nil && current.pendingRAV != nil
}

func (m *runtimeManager) withSessionEval(sessionID string, fn func(state *runtimeSessionState) error) error {
	state := m.ensureSessionState(sessionID)
	state.evalMu.Lock()
	defer state.evalMu.Unlock()

	return fn(state)
}

func (m *runtimeManager) ensureSessionState(sessionID string) *runtimeSessionState {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.ensureSessionStateLocked(sessionID)
}

func (m *runtimeManager) ensureSessionStateLocked(sessionID string) *runtimeSessionState {
	current := m.sessions[sessionID]
	if current != nil {
		return current
	}

	current = &runtimeSessionState{}
	m.sessions[sessionID] = current
	return current
}

func (m *runtimeManager) cleanupSessionState(sessionID string, events chan *providerv1.PaymentSessionResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cleanupSessionStateLocked(sessionID, events)
}

func (m *runtimeManager) cleanupSessionStateLocked(sessionID string, events chan *providerv1.PaymentSessionResponse) {
	current := m.sessions[sessionID]
	if current == nil || current.events != events {
		return
	}

	current.events = nil
	if current.pendingRAV == nil && current.queuedResponse == nil {
		delete(m.sessions, sessionID)
	}
}

func (m *runtimeManager) clearPendingRAV(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	current := m.sessions[sessionID]
	if current == nil {
		return
	}

	current.pendingRAV = nil
}

func (m *runtimeManager) clearQueuedControl(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	current := m.sessions[sessionID]
	if current == nil {
		return
	}

	current.pendingRAV = nil
	current.queuedResponse = nil
	if current.events == nil {
		delete(m.sessions, sessionID)
	}
}

func (m *runtimeManager) tryDeliverQueuedResponse(sessionID string, events chan *providerv1.PaymentSessionResponse, resp *providerv1.PaymentSessionResponse, pending *pendingRAVRequest) {
	if events == nil || resp == nil {
		return
	}

	select {
	case events <- resp:
		m.clearQueuedResponse(sessionID, events, resp, pending)
	default:
	}
}

func (m *runtimeManager) clearQueuedResponse(sessionID string, events chan *providerv1.PaymentSessionResponse, resp *providerv1.PaymentSessionResponse, pending *pendingRAVRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()

	current := m.sessions[sessionID]
	if current == nil || current.events != events || current.queuedResponse != resp {
		return
	}

	current.queuedResponse = nil
	current.pendingRAV = pending
	if current.events == nil && current.pendingRAV == nil {
		delete(m.sessions, sessionID)
	}
}
