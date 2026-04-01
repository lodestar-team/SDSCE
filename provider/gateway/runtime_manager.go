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
	evalMu      sync.Mutex
	events      chan *providerv1.PaymentSessionResponse
	awaitingRAV bool
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
	current.events = events
	m.mu.Unlock()

	return m.onMeteredUsage(ctx, gateway, sessionID)
}

func (m *runtimeManager) unbindSession(sessionID string, events chan *providerv1.PaymentSessionResponse) {
	if sessionID == "" || events == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	current := m.sessions[sessionID]
	if current == nil || current.events != events {
		return
	}

	current.events = nil
}

func (m *runtimeManager) clearAwaitingRAV(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if current := m.sessions[sessionID]; current != nil {
		current.awaitingRAV = false
	}
}

func (m *runtimeManager) onMeteredUsage(ctx context.Context, gateway *Gateway, sessionID string) error {
	state := m.ensureSessionState(sessionID)
	state.evalMu.Lock()
	defer state.evalMu.Unlock()

	return m.evaluateMeteredUsage(ctx, gateway, sessionID)
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
		gateway.logger.Info("stopping session due to insufficient funds from metered runtime evaluation",
			zap.String("session_id", sessionID),
		)
		m.dispatch(sessionID, needMoreFundsResponse(session, assessment), false)
		return nil
	}

	if !gateway.shouldRequestRAV(session) {
		return nil
	}

	events, awaiting := m.runtimeState(sessionID)
	if events == nil || awaiting {
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

	m.dispatch(sessionID, resp, true)
	return nil
}

func (m *runtimeManager) runtimeState(sessionID string) (chan *providerv1.PaymentSessionResponse, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	current := m.sessions[sessionID]
	if current == nil {
		return nil, false
	}

	return current.events, current.awaitingRAV
}

func (m *runtimeManager) dispatch(sessionID string, resp *providerv1.PaymentSessionResponse, awaitingRAV bool) {
	if resp == nil {
		return
	}

	var events chan *providerv1.PaymentSessionResponse

	m.mu.Lock()
	current := m.sessions[sessionID]
	if current != nil {
		events = current.events
		current.awaitingRAV = awaitingRAV
	}
	m.mu.Unlock()

	if events == nil {
		return
	}

	events <- resp
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
