// Package session implements the gRPC SessionService that manages worker-pool
// slots for the dsession tgm:// plugin used by firehose-core tier1.
package session

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
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
//   - If the payer has reached max concurrent sessions a new session is
//     created here (workers and sessions are treated as equivalent in the
//     in-memory model; the dsession protocol is one worker == one connection).
//   - Returns RESOURCE_EXHAUSTED if the payer's quota is exceeded.
func (s *SessionService) BorrowWorker(
	ctx context.Context,
	req *connect.Request[sessionv1.BorrowWorkerRequest],
) (*connect.Response[sessionv1.BorrowWorkerResponse], error) {
	payerStr := req.Msg.OrganizationId
	traceID := req.Msg.TraceId

	if payerStr == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("organization_id is required"))
	}

	payer, err := eth.NewAddress(payerStr)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid organization_id address: %w", err))
	}

	zlog.Debug("BorrowWorker called",
		zap.Stringer("payer", payer),
		zap.String("trace_id", traceID),
		zap.String("service", req.Msg.Service),
	)

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

	// Create a session for this worker if trace_id implies one (one-to-one mapping).
	sessionID := buildSessionID(payerStr, traceID)

	// Ensure session exists.
	if _, getErr := s.repo.SessionGet(ctx, sessionID); getErr != nil {
		newSession := &repository.Session{
			ID:            sessionID,
			Payer:         payer,
			Status:        repository.SessionStatusActive,
			CreatedAt:     time.Now(),
			LastKeepAlive: time.Now(),
		}
		if createErr := s.repo.SessionCreate(ctx, newSession); createErr != nil {
			// A concurrent BorrowWorker may have created the session first; that's fine.
			zlog.Debug("session already exists or create failed",
				zap.String("session_id", sessionID),
				zap.Error(createErr),
			)
		}
	}

	// Create the worker entry.
	workerKey := buildWorkerKey(payerStr, traceID, time.Now())
	worker := &repository.Worker{
		Key:       workerKey,
		SessionID: sessionID,
		Payer:     payer,
		CreatedAt: time.Now(),
		TraceID:   traceID,
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
		// Worker not found is non-fatal; the session may have been cleaned up.
		return connect.NewResponse(&sessionv1.KeepAliveResponse{}), nil
	}

	session, err := s.repo.SessionGet(ctx, worker.SessionID)
	if err != nil {
		return connect.NewResponse(&sessionv1.KeepAliveResponse{}), nil
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

// buildSessionID constructs a stable session ID for a (payer, traceID) pair.
func buildSessionID(payer, traceID string) string {
	if traceID != "" {
		return fmt.Sprintf("%s|%s", payer, traceID)
	}
	return payer
}

// buildWorkerKey constructs a unique worker key.
func buildWorkerKey(payer, traceID string, createdAt time.Time) string {
	return fmt.Sprintf("%s|%s|%d", payer, traceID, createdAt.UnixNano())
}
