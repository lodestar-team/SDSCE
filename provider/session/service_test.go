package session_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"
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

// --- BorrowWorker ---

func TestSessionService_BorrowWorker_Success(t *testing.T) {
	svc, repo := newTestService(nil)

	resp, err := svc.BorrowWorker(context.Background(), connect.NewRequest(&sessionv1.BorrowWorkerRequest{
		Service:        "substreams",
		OrganizationId: "0x1111111111111111111111111111111111111111",
		TraceId:        "trace-001",
	}))
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

	_, err := svc.BorrowWorker(context.Background(), connect.NewRequest(&sessionv1.BorrowWorkerRequest{
		Service: "substreams",
		TraceId: "trace-001",
	}))
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
}

func TestSessionService_BorrowWorker_QuotaExceeded(t *testing.T) {
	// Create a config with maxSessions=1, maxWorkers=1 → effective max = 1 worker.
	quotas := &session.QuotaConfig{
		DefaultMaxConcurrentSessions: 1,
		DefaultMaxWorkersPerSession:  1,
		PerPayerOverrides:            make(map[string]*session.PayerQuota),
	}
	svc, _ := newTestService(quotas)

	// Borrow first worker - should succeed.
	resp1, err := svc.BorrowWorker(context.Background(), connect.NewRequest(&sessionv1.BorrowWorkerRequest{
		Service:        "substreams",
		OrganizationId: "0x1111111111111111111111111111111111111111",
		TraceId:        "trace-001",
	}))
	require.NoError(t, err)
	assert.Equal(t, sessionv1.BorrowStatus_BORROW_STATUS_BORROWED, resp1.Msg.Status)

	// Borrow second worker - should be exhausted.
	resp2, err := svc.BorrowWorker(context.Background(), connect.NewRequest(&sessionv1.BorrowWorkerRequest{
		Service:        "substreams",
		OrganizationId: "0x1111111111111111111111111111111111111111",
		TraceId:        "trace-002",
	}))
	require.NoError(t, err)
	assert.Equal(t, sessionv1.BorrowStatus_BORROW_STATUS_RESOURCE_EXHAUSTED, resp2.Msg.Status)
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
	svc, _ := newTestService(quotas)

	// Should be able to borrow multiple workers for payer1 (10 max).
	for i := range 5 {
		resp, err := svc.BorrowWorker(context.Background(), connect.NewRequest(&sessionv1.BorrowWorkerRequest{
			Service:        "substreams",
			OrganizationId: "0x1111111111111111111111111111111111111111",
			TraceId:        "trace-" + string(rune('0'+i)),
		}))
		require.NoError(t, err)
		assert.Equal(t, sessionv1.BorrowStatus_BORROW_STATUS_BORROWED, resp.Msg.Status)
	}
}

// --- ReturnWorker ---

func TestSessionService_ReturnWorker_Success(t *testing.T) {
	svc, repo := newTestService(nil)

	borrowResp, err := svc.BorrowWorker(context.Background(), connect.NewRequest(&sessionv1.BorrowWorkerRequest{
		OrganizationId: "0x1111111111111111111111111111111111111111",
		TraceId:        "trace-001",
	}))
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

	borrowResp, err := svc.BorrowWorker(context.Background(), connect.NewRequest(&sessionv1.BorrowWorkerRequest{
		OrganizationId: "0x1111111111111111111111111111111111111111",
		TraceId:        "trace-001",
	}))
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
	// Unknown key is non-fatal.
	svc, _ := newTestService(nil)

	_, err := svc.KeepAlive(context.Background(), connect.NewRequest(&sessionv1.KeepAliveRequest{
		WorkerKey: "unknown-key",
	}))
	require.NoError(t, err)
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
