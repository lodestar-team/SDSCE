// Package session implements the gRPC SessionService that manages worker-pool
// slots for the dsession tgm:// plugin used by firehose-core tier1.
package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	sessionv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/session/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/session/v1/sessionv1connect"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/streamingfast/eth-go"
	"github.com/streamingfast/logging"
	"go.uber.org/zap"
)

var zlog, _ = logging.PackageLogger("sds_session", "github.com/graphprotocol/substreams-data-service/provider/session")

// SessionService implements sessionv1connect.SessionServiceHandler.
// It manages worker-pool slots and enforces per-payer quotas.
type SessionService struct {
	repo   repository.GlobalRepository
	quotas *QuotaConfig
}

var _ sessionv1connect.SessionServiceHandler = (*SessionService)(nil)

// NewSessionService creates a new SessionService.
// If quotas is nil, DefaultQuotaConfig() is used.
func NewSessionService(repo repository.GlobalRepository, quotas *QuotaConfig) *SessionService {
	if quotas == nil {
		quotas = DefaultQuotaConfig()
	}
	return &SessionService{repo: repo, quotas: quotas}
}

// BorrowWorker acquires a worker slot for a new streaming request.
//
// The request must reference a preexisting active SDS session created through
// the payment gateway flow. This keeps the live plugin path bound to
// provider-authoritative session state.
func (s *SessionService) BorrowWorker(
	ctx context.Context,
	req *connect.Request[sessionv1.BorrowWorkerRequest],
) (*connect.Response[sessionv1.BorrowWorkerResponse], error) {
	payerStr := req.Msg.OrganizationId

	if payerStr == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("organization_id is required"))
	}

	payer, err := eth.NewAddress(payerStr)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid organization_id address: %w", err))
	}

	// Get the SDS session ID from the request (set by session plugin from auth context).
	sessionID := req.Msg.SessionId
	if sessionID == "" {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("session_id not provided in request"))
	}

	zlog.Debug("BorrowWorker service called",
		zap.Stringer("payer", payer),
		zap.String("session_id", sessionID),
		zap.String("service", req.Msg.Service),
	)

	if _, err := s.authorizeSession(ctx, sessionID, payer); err != nil {
		return nil, err
	}

	// Check current quota usage.
	quota, err := s.repo.QuotaGet(ctx, payer)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("reading quota: %w", err))
	}

	maxWorkers := s.quotas.MaxWorkersPerSession(payerStr) * s.quotas.MaxConcurrentSessions(payerStr)

	if quota.ActiveWorkers >= maxWorkers {
		zlog.Warn("quota exceeded for payer",
			zap.Stringer("payer", payer),
			zap.Int("active_workers", quota.ActiveWorkers),
			zap.Int("max_workers", maxWorkers),
		)
		return connect.NewResponse(&sessionv1.BorrowWorkerResponse{
			Status: sessionv1.BorrowStatus_BORROW_STATUS_RESOURCE_EXHAUSTED,
			WorkerState: &sessionv1.WorkerState{
				MaxWorkers:    int64(maxWorkers),
				ActiveWorkers: int64(quota.ActiveWorkers),
			},
		}), nil
	}

	// Create the worker entry.
	// Worker key is unique per request, built from payer and timestamp.
	workerKey := buildWorkerKey(payerStr, sessionID, time.Now())
	worker := &repository.Worker{
		Key:       workerKey,
		SessionID: sessionID,
		Payer:     payer,
		CreatedAt: time.Now(),
		TraceID:   "", // No trace ID - workers are identified by their unique key
	}
	if err := s.repo.WorkerCreate(ctx, worker); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("creating worker: %w", err))
	}

	// Increment quota.
	if err := s.repo.QuotaIncrement(ctx, payer, 0, 1); err != nil {
		// Non-fatal: log and continue (quota is eventually consistent in the in-memory model).
		zlog.Warn("failed to increment quota", zap.Stringer("payer", payer), zap.Error(err))
	}

	zlog.Debug("worker borrowed",
		zap.String("worker_key", workerKey),
		zap.String("session_id", sessionID),
		zap.Stringer("payer", payer),
	)

	return connect.NewResponse(&sessionv1.BorrowWorkerResponse{
		WorkerKey: workerKey,
		Status:    sessionv1.BorrowStatus_BORROW_STATUS_BORROWED,
		WorkerState: &sessionv1.WorkerState{
			MaxWorkers:    int64(maxWorkers),
			ActiveWorkers: int64(quota.ActiveWorkers + 1),
		},
	}), nil
}

// ReturnWorker releases a previously borrowed worker slot.
func (s *SessionService) ReturnWorker(
	ctx context.Context,
	req *connect.Request[sessionv1.ReturnWorkerRequest],
) (*connect.Response[sessionv1.ReturnWorkerResponse], error) {
	workerKey := req.Msg.WorkerKey
	if workerKey == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("worker_key is required"))
	}

	zlog.Debug("ReturnWorker called", zap.String("worker_key", workerKey))

	// Look up the worker to find the payer.
	worker, err := s.repo.WorkerGet(ctx, workerKey)
	if err != nil {
		// Worker not found - may have already been returned.
		zlog.Warn("worker not found for return", zap.String("worker_key", workerKey), zap.Error(err))
		return connect.NewResponse(&sessionv1.ReturnWorkerResponse{}), nil
	}

	payer := worker.Payer

	// Honor minimal_worker_life_duration: if the worker has not been alive
	// long enough we simply wait before acknowledging the return.
	if req.Msg.MinimalWorkerLifeDuration != nil {
		minDuration := req.Msg.MinimalWorkerLifeDuration.AsDuration()
		elapsed := time.Since(worker.CreatedAt)
		if elapsed < minDuration {
			remaining := minDuration - elapsed
			zlog.Debug("waiting for minimal worker life duration",
				zap.String("worker_key", workerKey),
				zap.Duration("remaining", remaining),
			)
			select {
			case <-time.After(remaining):
			case <-ctx.Done():
				return nil, connect.NewError(connect.CodeDeadlineExceeded, ctx.Err())
			}
		}
	}

	// Delete the worker.
	if err := s.repo.WorkerDelete(ctx, workerKey); err != nil {
		zlog.Warn("failed to delete worker", zap.String("worker_key", workerKey), zap.Error(err))
	}

	// Decrement quota.
	if err := s.repo.QuotaDecrement(ctx, payer, 0, 1); err != nil {
		zlog.Warn("failed to decrement quota", zap.Stringer("payer", payer), zap.Error(err))
	}

	zlog.Debug("worker returned", zap.String("worker_key", workerKey), zap.Stringer("payer", payer))

	return connect.NewResponse(&sessionv1.ReturnWorkerResponse{}), nil
}

// KeepAlive refreshes the session's last-seen timestamp so it is not
// garbage-collected by background cleanup routines.
func (s *SessionService) KeepAlive(
	ctx context.Context,
	req *connect.Request[sessionv1.KeepAliveRequest],
) (*connect.Response[sessionv1.KeepAliveResponse], error) {
	workerKey := req.Msg.WorkerKey
	if workerKey == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("worker_key is required"))
	}

	zlog.Debug("KeepAlive called", zap.String("worker_key", workerKey))

	worker, err := s.repo.WorkerGet(ctx, workerKey)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("worker not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("reading worker: %w", err))
	}

	session, err := s.authorizeSession(ctx, worker.SessionID, worker.Payer)
	if err != nil {
		return nil, err
	}

	session.LastKeepAlive = time.Now()
	if updateErr := s.repo.SessionUpdate(ctx, session); updateErr != nil {
		zlog.Warn("failed to update session keep-alive",
			zap.String("session_id", session.ID),
			zap.Error(updateErr),
		)
	}

	return connect.NewResponse(&sessionv1.KeepAliveResponse{}), nil
}

func (s *SessionService) authorizeSession(ctx context.Context, sessionID string, payer eth.Address) (*repository.Session, error) {
	session, err := s.repo.SessionGet(ctx, sessionID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("session not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("reading session: %w", err))
	}

	if session.Payer.Pretty() != payer.Pretty() {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("session payer mismatch"))
	}

	if !session.IsActive() {
		return nil, sessionStateError(session)
	}

	return session, nil
}

func sessionStateError(session *repository.Session) error {
	if session != nil && session.EndReason == commonv1.EndReason_END_REASON_PAYMENT_ISSUE {
		return connect.NewError(connect.CodeResourceExhausted, fmt.Errorf("session ended due to payment issue"))
	}

	status := repository.SessionStatus("")
	if session != nil {
		status = session.Status
	}

	return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("session is not active (status: %s)", status))
}

// buildWorkerKey constructs a unique worker key.
func buildWorkerKey(payer, traceID string, createdAt time.Time) string {
	return fmt.Sprintf("%s|%s|%d", payer, traceID, createdAt.UnixNano())
}
