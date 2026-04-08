package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	sessionv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/session/v1"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/graphprotocol/substreams-data-service/provider/session"
	"github.com/streamingfast/eth-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestService(quotas *session.QuotaConfig) (*session.SessionService, *repository.InMemoryRepository) {
	repo := repository.NewInMemoryRepository()
	svc := session.NewSessionService(repo, quotas)
	return svc, repo
}

// newTestRequest creates a request with the session ID field set.
func newTestRequest(msg *sessionv1.BorrowWorkerRequest, sessionID string) *connect.Request[sessionv1.BorrowWorkerRequest] {
	msg.SessionId = sessionID
	return connect.NewRequest(msg)
}

// newTestReturnWorkerRequest creates a return worker request.
func newTestReturnWorkerRequest(msg *sessionv1.ReturnWorkerRequest) *connect.Request[sessionv1.ReturnWorkerRequest] {
	return connect.NewRequest(msg)
}

// newTestKeepAliveRequest creates a keep alive request.
func newTestKeepAliveRequest(msg *sessionv1.KeepAliveRequest) *connect.Request[sessionv1.KeepAliveRequest] {
	return connect.NewRequest(msg)
}

type quotaReservationFailRepo struct {
	*repository.InMemoryRepository
}

func (r *quotaReservationFailRepo) WorkerCreateAndReserveQuota(_ context.Context, worker *repository.Worker, maxWorkers int) (*repository.QuotaUsage, error) {
	return &repository.QuotaUsage{
		Payer:         worker.Payer,
		ActiveWorkers: maxWorkers,
		LastUpdated:   time.Now(),
	}, repository.ErrQuotaExceeded
}

type atomicBorrowFailRepo struct {
	*repository.InMemoryRepository
	calls int
}

func (r *atomicBorrowFailRepo) WorkerCreateAndReserveQuota(_ context.Context, worker *repository.Worker, maxWorkers int) (*repository.QuotaUsage, error) {
	r.calls++
	return nil, errors.New("atomic borrow failed")
}

func mustCreateSession(t *testing.T, repo *repository.InMemoryRepository, sessionID, payer string) *repository.Session {
	t.Helper()

	sess := &repository.Session{
		ID:            sessionID,
		Payer:         eth.MustNewAddress(payer),
		Status:        repository.SessionStatusActive,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		LastKeepAlive: time.Now(),
	}
	require.NoError(t, repo.SessionCreate(context.Background(), sess))
	return sess
}

// --- BorrowWorker ---

func TestSessionService_BorrowWorker_Success(t *testing.T) {
	svc, repo := newTestService(nil)
	mustCreateSession(t, repo, "test-session-001", "0x1111111111111111111111111111111111111111")

	req := newTestRequest(&sessionv1.BorrowWorkerRequest{
		Service:        "substreams",
		OrganizationId: "0x1111111111111111111111111111111111111111",
		SessionId:      "trace-001",
	}, "test-session-001")
	resp, err := svc.BorrowWorker(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, sessionv1.BorrowStatus_BORROW_STATUS_BORROWED, resp.Msg.Status)
	assert.NotEmpty(t, resp.Msg.WorkerKey)

	// Quota should have been incremented.
	quota, err := repo.QuotaGet(context.Background(), eth.MustNewAddress("0x1111111111111111111111111111111111111111"))
	require.NoError(t, err)
	assert.Equal(t, 1, quota.ActiveWorkers)
}

func TestSessionService_BorrowWorker_MissingOrganizationId(t *testing.T) {
	svc, _ := newTestService(nil)

	req := newTestRequest(&sessionv1.BorrowWorkerRequest{
		Service:   "substreams",
		SessionId: "trace-001",
	}, "test-session-002")
	_, err := svc.BorrowWorker(context.Background(), req)
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
}

func TestSessionService_BorrowWorker_MissingSessionID(t *testing.T) {
	svc, _ := newTestService(nil)

	// Missing session_id field - should fail
	_, err := svc.BorrowWorker(context.Background(), connect.NewRequest(&sessionv1.BorrowWorkerRequest{
		Service:        "substreams",
		OrganizationId: "0x1111111111111111111111111111111111111111",
		// SessionId intentionally not set
	}))
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodePermissionDenied, connectErr.Code())
	assert.Contains(t, connectErr.Message(), "session_id not provided")
}

func TestSessionService_BorrowWorker_UnknownSession(t *testing.T) {
	svc, _ := newTestService(nil)

	_, err := svc.BorrowWorker(context.Background(), newTestRequest(&sessionv1.BorrowWorkerRequest{
		Service:        "substreams",
		OrganizationId: "0x1111111111111111111111111111111111111111",
		SessionId:      "trace-unknown",
	}, "missing-session"))
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodePermissionDenied, connectErr.Code())
	assert.Contains(t, connectErr.Message(), "session not found")
}

func TestSessionService_BorrowWorker_InactivePaymentIssueUsesResourceExhausted(t *testing.T) {
	svc, repo := newTestService(nil)
	sess := mustCreateSession(t, repo, "terminated-payment-session", "0x1111111111111111111111111111111111111111")
	sess.End(commonv1.EndReason_END_REASON_PAYMENT_ISSUE)
	require.NoError(t, repo.SessionUpdate(context.Background(), sess))

	_, err := svc.BorrowWorker(context.Background(), newTestRequest(&sessionv1.BorrowWorkerRequest{
		Service:        "substreams",
		OrganizationId: "0x1111111111111111111111111111111111111111",
		SessionId:      "trace-payment",
	}, "terminated-payment-session"))
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeResourceExhausted, connectErr.Code())
}

func TestSessionService_BorrowWorker_InactiveNonPaymentUsesPermissionDenied(t *testing.T) {
	svc, repo := newTestService(nil)
	sess := mustCreateSession(t, repo, "terminated-nonpayment-session", "0x1111111111111111111111111111111111111111")
	sess.End(commonv1.EndReason_END_REASON_CLIENT_DISCONNECT)
	require.NoError(t, repo.SessionUpdate(context.Background(), sess))

	_, err := svc.BorrowWorker(context.Background(), newTestRequest(&sessionv1.BorrowWorkerRequest{
		Service:        "substreams",
		OrganizationId: "0x1111111111111111111111111111111111111111",
		SessionId:      "trace-disconnect",
	}, "terminated-nonpayment-session"))
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodePermissionDenied, connectErr.Code())
}

func TestSessionService_BorrowWorker_QuotaExceeded(t *testing.T) {
	// Create a config with maxSessions=1, maxWorkers=1 → effective max = 1 worker.
	quotas := &session.QuotaConfig{
		DefaultMaxConcurrentSessions: 1,
		DefaultMaxWorkersPerSession:  1,
		PerPayerOverrides:            make(map[string]*session.PayerQuota),
	}
	svc, repo := newTestService(quotas)
	mustCreateSession(t, repo, "test-session-003", "0x1111111111111111111111111111111111111111")
	mustCreateSession(t, repo, "test-session-004", "0x1111111111111111111111111111111111111111")

	// Borrow first worker - should succeed.
	req1 := newTestRequest(&sessionv1.BorrowWorkerRequest{
		Service:        "substreams",
		OrganizationId: "0x1111111111111111111111111111111111111111",
		SessionId:      "trace-001",
	}, "test-session-003")
	resp1, err := svc.BorrowWorker(context.Background(), req1)
	require.NoError(t, err)
	assert.Equal(t, sessionv1.BorrowStatus_BORROW_STATUS_BORROWED, resp1.Msg.Status)

	// Borrow second worker - should be exhausted.
	req2 := newTestRequest(&sessionv1.BorrowWorkerRequest{
		Service:        "substreams",
		OrganizationId: "0x1111111111111111111111111111111111111111",
		SessionId:      "trace-002",
	}, "test-session-004")
	resp2, err := svc.BorrowWorker(context.Background(), req2)
	require.NoError(t, err)
	assert.Equal(t, sessionv1.BorrowStatus_BORROW_STATUS_RESOURCE_EXHAUSTED, resp2.Msg.Status)
}

func TestSessionService_BorrowWorker_QuotaReservationFailure_DoesNotCreateWorker(t *testing.T) {
	spy := &quotaReservationFailRepo{InMemoryRepository: repository.NewInMemoryRepository()}
	svc := session.NewSessionService(spy, nil)
	repo := spy.InMemoryRepository
	mustCreateSession(t, repo, "reservation-failure-session", "0x1111111111111111111111111111111111111111")

	resp, err := svc.BorrowWorker(context.Background(), newTestRequest(&sessionv1.BorrowWorkerRequest{
		Service:        "substreams",
		OrganizationId: "0x1111111111111111111111111111111111111111",
		SessionId:      "trace-reserve-failure",
	}, "reservation-failure-session"))
	require.NoError(t, err)
	assert.Equal(t, sessionv1.BorrowStatus_BORROW_STATUS_RESOURCE_EXHAUSTED, resp.Msg.Status)
}

func TestSessionService_BorrowWorker_AtomicReserveFailureReturnsInternalError(t *testing.T) {
	spy := &atomicBorrowFailRepo{InMemoryRepository: repository.NewInMemoryRepository()}
	svc := session.NewSessionService(spy, nil)
	repo := spy.InMemoryRepository
	mustCreateSession(t, repo, "atomic-borrow-failure-session", "0x1111111111111111111111111111111111111111")

	resp, err := svc.BorrowWorker(context.Background(), newTestRequest(&sessionv1.BorrowWorkerRequest{
		Service:        "substreams",
		OrganizationId: "0x1111111111111111111111111111111111111111",
		SessionId:      "trace-atomic-failure",
	}, "atomic-borrow-failure-session"))
	require.Error(t, err)
	require.Nil(t, resp)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInternal, connectErr.Code())
	assert.Equal(t, 1, spy.calls)
}

func TestSessionService_BorrowWorker_PerPayerOverride(t *testing.T) {
	// Default = 1 worker but payer1 has 5 workers override.
	quotas := &session.QuotaConfig{
		DefaultMaxConcurrentSessions: 1,
		DefaultMaxWorkersPerSession:  1,
		PerPayerOverrides: map[string]*session.PayerQuota{
			"0x1111111111111111111111111111111111111111": {MaxConcurrentSessions: 5, MaxWorkersPerSession: 2},
		},
	}
	svc, repo := newTestService(quotas)

	// Should be able to borrow multiple workers for payer1 (10 max).
	for i := range 5 {
		sessionID := "test-session-" + string(rune('0'+i))
		mustCreateSession(t, repo, sessionID, "0x1111111111111111111111111111111111111111")
		req := newTestRequest(&sessionv1.BorrowWorkerRequest{
			Service:        "substreams",
			OrganizationId: "0x1111111111111111111111111111111111111111",
			SessionId:      "trace-" + string(rune('0'+i)),
		}, sessionID)
		resp, err := svc.BorrowWorker(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, sessionv1.BorrowStatus_BORROW_STATUS_BORROWED, resp.Msg.Status)
	}
}

// --- ReturnWorker ---

func TestSessionService_ReturnWorker_Success(t *testing.T) {
	svc, repo := newTestService(nil)
	mustCreateSession(t, repo, "test-session-return-001", "0x1111111111111111111111111111111111111111")

	borrowReq := newTestRequest(&sessionv1.BorrowWorkerRequest{
		OrganizationId: "0x1111111111111111111111111111111111111111",
		SessionId:      "trace-001",
	}, "test-session-return-001")
	borrowResp, err := svc.BorrowWorker(context.Background(), borrowReq)
	require.NoError(t, err)
	workerKey := borrowResp.Msg.WorkerKey

	// Return it.
	_, err = svc.ReturnWorker(context.Background(), connect.NewRequest(&sessionv1.ReturnWorkerRequest{
		WorkerKey: workerKey,
	}))
	require.NoError(t, err)

	// Quota should be back to 0.
	quota, err := repo.QuotaGet(context.Background(), eth.MustNewAddress("0x1111111111111111111111111111111111111111"))
	require.NoError(t, err)
	assert.Equal(t, 0, quota.ActiveWorkers)
}

func TestSessionService_ReturnWorker_MissingKey(t *testing.T) {
	svc, _ := newTestService(nil)

	_, err := svc.ReturnWorker(context.Background(), connect.NewRequest(&sessionv1.ReturnWorkerRequest{}))
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
}

func TestSessionService_ReturnWorker_UnknownKey(t *testing.T) {
	svc, _ := newTestService(nil)

	// Returning an unknown key is non-fatal.
	_, err := svc.ReturnWorker(context.Background(), connect.NewRequest(&sessionv1.ReturnWorkerRequest{
		WorkerKey: "nonexistent-key",
	}))
	require.NoError(t, err)
}

// --- KeepAlive ---

func TestSessionService_KeepAlive_Success(t *testing.T) {
	svc, repo := newTestService(nil)
	mustCreateSession(t, repo, "test-session-keepalive-001", "0x1111111111111111111111111111111111111111")

	borrowReq := newTestRequest(&sessionv1.BorrowWorkerRequest{
		OrganizationId: "0x1111111111111111111111111111111111111111",
		SessionId:      "trace-001",
	}, "test-session-keepalive-001")
	borrowResp, err := svc.BorrowWorker(context.Background(), borrowReq)
	require.NoError(t, err)
	workerKey := borrowResp.Msg.WorkerKey

	_, err = svc.KeepAlive(context.Background(), connect.NewRequest(&sessionv1.KeepAliveRequest{
		WorkerKey: workerKey,
	}))
	require.NoError(t, err)

	// Verify session LastKeepAlive was updated.
	worker, err := repo.WorkerGet(context.Background(), workerKey)
	require.NoError(t, err)

	sess, err := repo.SessionGet(context.Background(), worker.SessionID)
	require.NoError(t, err)
	assert.False(t, sess.LastKeepAlive.IsZero())
}

func TestSessionService_KeepAlive_MissingKey(t *testing.T) {
	svc, _ := newTestService(nil)

	_, err := svc.KeepAlive(context.Background(), connect.NewRequest(&sessionv1.KeepAliveRequest{}))
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
}

func TestSessionService_KeepAlive_UnknownKey(t *testing.T) {
	svc, _ := newTestService(nil)

	_, err := svc.KeepAlive(context.Background(), connect.NewRequest(&sessionv1.KeepAliveRequest{
		WorkerKey: "unknown-key",
	}))
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodePermissionDenied, connectErr.Code())
}

func TestSessionService_KeepAlive_PaymentIssueReturnsResourceExhausted(t *testing.T) {
	svc, repo := newTestService(nil)
	sess := mustCreateSession(t, repo, "payment-ended-session", "0x1111111111111111111111111111111111111111")
	worker := &repository.Worker{
		Key:       "payment-worker",
		SessionID: sess.ID,
		Payer:     sess.Payer,
		CreatedAt: time.Now(),
	}
	require.NoError(t, repo.WorkerCreate(context.Background(), worker))

	sess.End(commonv1.EndReason_END_REASON_PAYMENT_ISSUE)
	lastKeepAlive := sess.LastKeepAlive
	require.NoError(t, repo.SessionUpdate(context.Background(), sess))

	_, err := svc.KeepAlive(context.Background(), connect.NewRequest(&sessionv1.KeepAliveRequest{
		WorkerKey: worker.Key,
	}))
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeResourceExhausted, connectErr.Code())

	updated, getErr := repo.SessionGet(context.Background(), sess.ID)
	require.NoError(t, getErr)
	assert.Equal(t, lastKeepAlive, updated.LastKeepAlive)
}

func TestSessionService_KeepAlive_NonPaymentTerminationReturnsPermissionDenied(t *testing.T) {
	svc, repo := newTestService(nil)
	sess := mustCreateSession(t, repo, "nonpayment-ended-session", "0x1111111111111111111111111111111111111111")
	worker := &repository.Worker{
		Key:       "nonpayment-worker",
		SessionID: sess.ID,
		Payer:     sess.Payer,
		CreatedAt: time.Now(),
	}
	require.NoError(t, repo.WorkerCreate(context.Background(), worker))

	sess.End(commonv1.EndReason_END_REASON_CLIENT_DISCONNECT)
	lastKeepAlive := sess.LastKeepAlive
	require.NoError(t, repo.SessionUpdate(context.Background(), sess))

	_, err := svc.KeepAlive(context.Background(), connect.NewRequest(&sessionv1.KeepAliveRequest{
		WorkerKey: worker.Key,
	}))
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodePermissionDenied, connectErr.Code())

	updated, getErr := repo.SessionGet(context.Background(), sess.ID)
	require.NoError(t, getErr)
	assert.Equal(t, lastKeepAlive, updated.LastKeepAlive)
}

// --- QuotaConfig ---

func TestQuotaConfig_Defaults(t *testing.T) {
	q := session.DefaultQuotaConfig()
	assert.Equal(t, 10, q.MaxConcurrentSessions("0xanypayer"))
	assert.Equal(t, 5, q.MaxWorkersPerSession("0xanypayer"))
}

func TestQuotaConfig_PerPayerOverride(t *testing.T) {
	q := &session.QuotaConfig{
		DefaultMaxConcurrentSessions: 10,
		DefaultMaxWorkersPerSession:  5,
		PerPayerOverrides: map[string]*session.PayerQuota{
			"0xvip": {MaxConcurrentSessions: 50, MaxWorkersPerSession: 20},
		},
	}
	assert.Equal(t, 50, q.MaxConcurrentSessions("0xvip"))
	assert.Equal(t, 20, q.MaxWorkersPerSession("0xvip"))
	assert.Equal(t, 10, q.MaxConcurrentSessions("0xnormal"))
	assert.Equal(t, 5, q.MaxWorkersPerSession("0xnormal"))
}
