package repository_test

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/graphprotocol/substreams-data-service/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/streamingfast/eth-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSession(id, payerHex string) *repository.Session {
	// Convert simple test addresses to valid ethereum addresses
	if len(payerHex) < 42 {
		payerHex = "0x" + payerHex[2:] + "00000000000000000000000000000000000000"
		payerHex = payerHex[:42]
	}
	return &repository.Session{
		ID:        id,
		Payer:     eth.MustNewAddress(payerHex),
		Status:    repository.SessionStatusActive,
		CreatedAt: time.Now(),
	}
}

func newTestWorker(key, sessionID, payerHex string) *repository.Worker {
	// Convert simple test addresses to valid ethereum addresses
	if len(payerHex) < 42 {
		payerHex = "0x" + payerHex[2:] + "00000000000000000000000000000000000000"
		payerHex = payerHex[:42]
	}
	return &repository.Worker{
		Key:       key,
		SessionID: sessionID,
		Payer:     eth.MustNewAddress(payerHex),
		CreatedAt: time.Now(),
	}
}

// --- Session tests ---

func TestInMemory_SessionCreate_Get(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	s := newTestSession("s1", "0x1111111111111111111111111111111111111111")
	require.NoError(t, repo.SessionCreate(ctx, s))

	got, err := repo.SessionGet(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "s1", got.ID)
	assert.Equal(t, "0x1111111111111111111111111111111111111111", got.Payer.Pretty())
}

func TestInMemory_SessionCreate_Duplicate(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	s := newTestSession("s1", "0x1111111111111111111111111111111111111111")
	require.NoError(t, repo.SessionCreate(ctx, s))

	err := repo.SessionCreate(ctx, s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestInMemory_SessionCreate_NilAndEmpty(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.Error(t, repo.SessionCreate(ctx, nil))
	require.Error(t, repo.SessionCreate(ctx, &repository.Session{ID: ""}))
}

func TestInMemory_SessionGet_NotFound(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	_, err := repo.SessionGet(ctx, "missing")
	require.Error(t, err)
	assert.ErrorIs(t, err, repository.ErrNotFound)
	assert.Contains(t, err.Error(), "not found")
}

func TestInMemory_SessionUpdate(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	s := newTestSession("s1", "0x1111111111111111111111111111111111111111")
	require.NoError(t, repo.SessionCreate(ctx, s))

	updated := *s
	updated.Status = repository.SessionStatusTerminated
	require.NoError(t, repo.SessionUpdate(ctx, &updated))

	got, err := repo.SessionGet(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, repository.SessionStatusTerminated, got.Status)
}

func TestInMemory_SessionUpdate_NotFound(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	s := newTestSession("missing", "0x1111111111111111111111111111111111111111")
	require.Error(t, repo.SessionUpdate(ctx, s))
}

func TestInMemory_SessionUpdateRAVAndBaseline_PreservesUsageTotals(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	s := newTestSession("s1", "0x1111111111111111111111111111111111111111")
	s.BlocksProcessed = 10
	s.BytesTransferred = 20
	s.Requests = 3
	s.TotalCost = big.NewInt(30)
	require.NoError(t, repo.SessionCreate(ctx, s))

	rav := &horizon.SignedRAV{
		Message: &horizon.RAV{
			Payer:          s.Payer,
			ValueAggregate: big.NewInt(12),
		},
	}
	require.NoError(t, repo.SessionUpdateRAVAndBaseline(ctx, s.ID, rav, 10, 20, 3, big.NewInt(30)))

	got, err := repo.SessionGet(ctx, s.ID)
	require.NoError(t, err)
	require.NotNil(t, got.CurrentRAV)
	assert.Equal(t, 0, big.NewInt(12).Cmp(got.CurrentRAV.Message.ValueAggregate))
	assert.Equal(t, uint64(10), got.BlocksProcessed)
	assert.Equal(t, uint64(20), got.BytesTransferred)
	assert.Equal(t, uint64(3), got.Requests)
	assert.Equal(t, 0, big.NewInt(30).Cmp(got.TotalCost))
	assert.Equal(t, uint64(10), got.BaselineBlocks)
	assert.Equal(t, uint64(20), got.BaselineBytes)
	assert.Equal(t, uint64(3), got.BaselineReqs)
	assert.Equal(t, 0, big.NewInt(30).Cmp(got.BaselineCost))
}

func TestInMemory_SessionTouch_PreservesUsageTotals(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	s := newTestSession("s1", "0x1111111111111111111111111111111111111111")
	s.BlocksProcessed = 10
	s.BytesTransferred = 20
	s.Requests = 3
	s.TotalCost = big.NewInt(30)
	require.NoError(t, repo.SessionCreate(ctx, s))

	touchedAt := time.Now().Add(time.Minute)
	require.NoError(t, repo.SessionTouch(ctx, s.ID, touchedAt))

	got, err := repo.SessionGet(ctx, s.ID)
	require.NoError(t, err)
	assert.Equal(t, touchedAt, got.LastKeepAlive)
	assert.Equal(t, uint64(10), got.BlocksProcessed)
	assert.Equal(t, uint64(20), got.BytesTransferred)
	assert.Equal(t, uint64(3), got.Requests)
	assert.Equal(t, 0, big.NewInt(30).Cmp(got.TotalCost))
}

func TestInMemory_SessionTouch_IsMonotonic(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	s := newTestSession("s1", "0x1111111111111111111111111111111111111111")
	require.NoError(t, repo.SessionCreate(ctx, s))

	newer := time.Now().Add(time.Minute)
	older := newer.Add(-time.Hour)
	require.NoError(t, repo.SessionTouch(ctx, s.ID, newer))
	require.NoError(t, repo.SessionTouch(ctx, s.ID, older))

	got, err := repo.SessionGet(ctx, s.ID)
	require.NoError(t, err)
	assert.Equal(t, newer, got.LastKeepAlive)
	assert.Equal(t, newer, got.UpdatedAt)
}

func TestInMemory_SessionTouch_NotFound(t *testing.T) {
	repo := repository.NewInMemoryRepository()

	err := repo.SessionTouch(context.Background(), "missing", time.Now())
	require.ErrorIs(t, err, repository.ErrNotFound)
}

func TestInMemory_SessionUpdateRuntimeState_PreservesUsageTotals(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	s := newTestSession("s1", "0x1111111111111111111111111111111111111111")
	s.BlocksProcessed = 10
	s.BytesTransferred = 20
	s.Requests = 3
	s.TotalCost = big.NewInt(30)
	require.NoError(t, repo.SessionCreate(ctx, s))

	endedAt := time.Now().Add(time.Minute)
	metadata := map[string]string{"funds_check_error": "timeout"}
	require.NoError(t, repo.SessionUpdateRuntimeState(ctx, s.ID, repository.SessionStatusTerminated, metadata, &endedAt, commonv1.EndReason_END_REASON_PAYMENT_ISSUE, endedAt))

	got, err := repo.SessionGet(ctx, s.ID)
	require.NoError(t, err)
	assert.Equal(t, repository.SessionStatusTerminated, got.Status)
	assert.Equal(t, metadata, got.Metadata)
	require.NotNil(t, got.EndedAt)
	assert.Equal(t, endedAt, *got.EndedAt)
	assert.Equal(t, commonv1.EndReason_END_REASON_PAYMENT_ISSUE, got.EndReason)
	assert.Equal(t, uint64(10), got.BlocksProcessed)
	assert.Equal(t, uint64(20), got.BytesTransferred)
	assert.Equal(t, uint64(3), got.Requests)
	assert.Equal(t, 0, big.NewInt(30).Cmp(got.TotalCost))
}

func TestInMemory_SessionUpdateRuntimeState_DoesNotRegressTerminatedSession(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	s := newTestSession("s1", "0x1111111111111111111111111111111111111111")
	endedAt := time.Now()
	s.Status = repository.SessionStatusTerminated
	s.UpdatedAt = endedAt
	s.EndedAt = &endedAt
	s.EndReason = commonv1.EndReason_END_REASON_PAYMENT_ISSUE
	s.Metadata = map[string]string{"state": "terminated"}
	require.NoError(t, repo.SessionCreate(ctx, s))

	require.NoError(t, repo.SessionUpdateRuntimeState(ctx, s.ID, repository.SessionStatusActive, map[string]string{"state": "active"}, nil, commonv1.EndReason_END_REASON_UNSPECIFIED, endedAt.Add(time.Minute)))

	got, err := repo.SessionGet(ctx, s.ID)
	require.NoError(t, err)
	assert.Equal(t, repository.SessionStatusTerminated, got.Status)
	assert.Equal(t, map[string]string{"state": "terminated"}, got.Metadata)
	require.NotNil(t, got.EndedAt)
	assert.Equal(t, endedAt, *got.EndedAt)
	assert.Equal(t, commonv1.EndReason_END_REASON_PAYMENT_ISSUE, got.EndReason)
	assert.Equal(t, endedAt, got.UpdatedAt)
}

func TestInMemory_SessionUpdateRuntimeState_DoesNotMoveUpdatedAtBackwards(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	s := newTestSession("s1", "0x1111111111111111111111111111111111111111")
	newer := time.Now().Add(time.Minute)
	s.UpdatedAt = newer
	require.NoError(t, repo.SessionCreate(ctx, s))

	older := newer.Add(-time.Hour)
	require.NoError(t, repo.SessionUpdateRuntimeState(ctx, s.ID, repository.SessionStatusActive, nil, nil, commonv1.EndReason_END_REASON_UNSPECIFIED, older))

	got, err := repo.SessionGet(ctx, s.ID)
	require.NoError(t, err)
	assert.Equal(t, newer, got.UpdatedAt)
}

func TestInMemory_SessionUpdateRuntimeState_NotFound(t *testing.T) {
	repo := repository.NewInMemoryRepository()

	err := repo.SessionUpdateRuntimeState(context.Background(), "missing", repository.SessionStatusActive, nil, nil, commonv1.EndReason_END_REASON_UNSPECIFIED, time.Now())
	require.ErrorIs(t, err, repository.ErrNotFound)
}

// TestInMemory_SessionDelete and SessionDelete_NotFound removed - SessionDelete method no longer exists

func TestInMemory_SessionList_NoFilter(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.NoError(t, repo.SessionCreate(ctx, newTestSession("s1", "0x1111111111111111111111111111111111111111")))
	require.NoError(t, repo.SessionCreate(ctx, newTestSession("s2", "0x2222222222222222222222222222222222222222")))
	require.NoError(t, repo.SessionCreate(ctx, newTestSession("s3", "0x1111111111111111111111111111111111111111")))

	all, err := repo.SessionList(ctx, repository.SessionFilter{})
	require.NoError(t, err)
	assert.Len(t, all, 3)
}

func TestInMemory_SessionList_ByPayer(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.NoError(t, repo.SessionCreate(ctx, newTestSession("s1", "0x1111111111111111111111111111111111111111")))
	require.NoError(t, repo.SessionCreate(ctx, newTestSession("s2", "0x2222222222222222222222222222222222222222")))
	require.NoError(t, repo.SessionCreate(ctx, newTestSession("s3", "0x1111111111111111111111111111111111111111")))

	payer := eth.MustNewAddress("0x1111111111111111111111111111111111111111")
	sessions, err := repo.SessionList(ctx, repository.SessionFilter{Payer: &payer})
	require.NoError(t, err)
	assert.Len(t, sessions, 2)
	for _, s := range sessions {
		assert.Equal(t, "0x1111111111111111111111111111111111111111", s.Payer.Pretty())
	}
}

func TestInMemory_SessionList_ByStatus(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	s1 := newTestSession("s1", "0x1111111111111111111111111111111111111111")
	s2 := newTestSession("s2", "0x1111111111111111111111111111111111111111")
	s2.Status = repository.SessionStatusTerminated

	require.NoError(t, repo.SessionCreate(ctx, s1))
	require.NoError(t, repo.SessionCreate(ctx, s2))

	status := repository.SessionStatusActive
	active, err := repo.SessionList(ctx, repository.SessionFilter{Status: &status})
	require.NoError(t, err)
	assert.Len(t, active, 1)
	assert.Equal(t, "s1", active[0].ID)
}

func TestInMemory_SessionList_ByCreatedAfter(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)

	s1 := newTestSession("s1", "0x1111111111111111111111111111111111111111")
	s1.CreatedAt = time.Now().Add(-30 * time.Minute) // between past and future
	s2 := newTestSession("s2", "0x1111111111111111111111111111111111111111")
	s2.CreatedAt = time.Now().Add(-2 * time.Hour) // before past

	require.NoError(t, repo.SessionCreate(ctx, s1))
	require.NoError(t, repo.SessionCreate(ctx, s2))

	_ = future
	sessions, err := repo.SessionList(ctx, repository.SessionFilter{CreatedAfter: &past})
	require.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, "s1", sessions[0].ID)
}

// TestInMemory_SessionGetByPayer removed - SessionGetByPayer method no longer exists

// --- Worker tests ---

func TestInMemory_WorkerCreate_Get(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	w := newTestWorker("w1", "s1", "0x1111111111111111111111111111111111111111")
	require.NoError(t, repo.WorkerCreate(ctx, w))

	got, err := repo.WorkerGet(ctx, "w1")
	require.NoError(t, err)
	assert.Equal(t, "w1", got.Key)
	assert.Equal(t, "s1", got.SessionID)
}

func TestInMemory_WorkerCreate_Duplicate(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	w := newTestWorker("w1", "s1", "0x1111111111111111111111111111111111111111")
	require.NoError(t, repo.WorkerCreate(ctx, w))
	require.Error(t, repo.WorkerCreate(ctx, w))
}

func TestInMemory_WorkerCreateAndReserveQuota(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	worker := newTestWorker("worker-atomic-1", "session-atomic-1", "0x1111111111111111111111111111111111111111")
	quota, err := repo.WorkerCreateAndReserveQuota(ctx, worker, 3)
	require.NoError(t, err)
	require.NotNil(t, quota)
	assert.Equal(t, 1, quota.ActiveWorkers)

	gotWorker, err := repo.WorkerGet(ctx, worker.Key)
	require.NoError(t, err)
	assert.Equal(t, worker.Key, gotWorker.Key)

	gotQuota, err := repo.QuotaGet(ctx, worker.Payer)
	require.NoError(t, err)
	assert.Equal(t, 1, gotQuota.ActiveWorkers)
}

func TestInMemory_WorkerCreateAndReserveQuota_DuplicateWorkerDoesNotMutateQuota(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	worker := newTestWorker("worker-atomic-dup", "session-atomic-dup", "0x2222222222222222222222222222222222222222")
	quota, err := repo.WorkerCreateAndReserveQuota(ctx, worker, 3)
	require.NoError(t, err)
	require.NotNil(t, quota)
	assert.Equal(t, 1, quota.ActiveWorkers)

	quota, err = repo.WorkerCreateAndReserveQuota(ctx, worker, 3)
	require.Error(t, err)
	assert.ErrorContains(t, err, "already exists")
	assert.Nil(t, quota)

	gotQuota, err := repo.QuotaGet(ctx, worker.Payer)
	require.NoError(t, err)
	assert.Equal(t, 1, gotQuota.ActiveWorkers)
}

func TestInMemory_WorkerGet_NotFound(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	_, err := repo.WorkerGet(ctx, "missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, repository.ErrNotFound))
}

func TestInMemory_WorkerCountBySession(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.NoError(t, repo.WorkerCreate(ctx, newTestWorker("w1", "s1", "0x1111111111111111111111111111111111111111")))
	require.NoError(t, repo.WorkerCreate(ctx, newTestWorker("w2", "s1", "0x1111111111111111111111111111111111111111")))
	require.NoError(t, repo.WorkerCreate(ctx, newTestWorker("w3", "s2", "0x1111111111111111111111111111111111111111")))

	count, err := repo.WorkerCountBySession(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	count, err = repo.WorkerCountBySession(ctx, "missing")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestInMemory_WorkerDelete(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	w := newTestWorker("w1", "s1", "0x1111111111111111111111111111111111111111")
	require.NoError(t, repo.WorkerCreate(ctx, w))
	require.NoError(t, repo.WorkerDelete(ctx, "w1"))

	_, err := repo.WorkerGet(ctx, "w1")
	require.Error(t, err)
}

// TestInMemory_WorkerListBySession and WorkerCountByPayer removed - methods no longer exist

// --- Quota tests ---

func TestInMemory_QuotaGet_NewPayer(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	q, err := repo.QuotaGet(ctx, eth.MustNewAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"))
	require.NoError(t, err)
	assert.NotNil(t, q)
	assert.Equal(t, "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", q.Payer.Pretty())
	assert.Equal(t, 0, q.ActiveSessions)
	assert.Equal(t, 0, q.ActiveWorkers)
}

func TestInMemory_QuotaReserve(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	payer := eth.MustNewAddress("0x1111111111111111111111111111111111111111")
	quota, err := repo.QuotaReserve(ctx, payer, 3, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, quota.ActiveWorkers)

	quota, err = repo.QuotaReserve(ctx, payer, 3, 2)
	require.NoError(t, err)
	assert.Equal(t, 3, quota.ActiveWorkers)

	quota, err = repo.QuotaReserve(ctx, payer, 3, 1)
	require.ErrorIs(t, err, repository.ErrQuotaExceeded)
	assert.Equal(t, 3, quota.ActiveWorkers)
}

func TestInMemory_QuotaReserve_ExhaustedDoesNotMutate(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	payer := eth.MustNewAddress("0x2222222222222222222222222222222222222222")
	_, err := repo.QuotaReserve(ctx, payer, 1, 1)
	require.NoError(t, err)

	quota, err := repo.QuotaReserve(ctx, payer, 1, 1)
	require.ErrorIs(t, err, repository.ErrQuotaExceeded)
	assert.Equal(t, 1, quota.ActiveWorkers)

	got, err := repo.QuotaGet(ctx, payer)
	require.NoError(t, err)
	assert.Equal(t, 1, got.ActiveWorkers)
}

func TestInMemory_QuotaIncrement(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.NoError(t, repo.QuotaIncrement(ctx, eth.MustNewAddress("0x1111111111111111111111111111111111111111"), 1, 2))

	q, err := repo.QuotaGet(ctx, eth.MustNewAddress("0x1111111111111111111111111111111111111111"))
	require.NoError(t, err)
	assert.Equal(t, 1, q.ActiveSessions)
	assert.Equal(t, 2, q.ActiveWorkers)

	require.NoError(t, repo.QuotaIncrement(ctx, eth.MustNewAddress("0x1111111111111111111111111111111111111111"), 0, 1))

	q, err = repo.QuotaGet(ctx, eth.MustNewAddress("0x1111111111111111111111111111111111111111"))
	require.NoError(t, err)
	assert.Equal(t, 1, q.ActiveSessions)
	assert.Equal(t, 3, q.ActiveWorkers)
}

func TestInMemory_QuotaDecrement(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.NoError(t, repo.QuotaIncrement(ctx, eth.MustNewAddress("0x1111111111111111111111111111111111111111"), 3, 5))
	require.NoError(t, repo.QuotaDecrement(ctx, eth.MustNewAddress("0x1111111111111111111111111111111111111111"), 1, 2))

	q, err := repo.QuotaGet(ctx, eth.MustNewAddress("0x1111111111111111111111111111111111111111"))
	require.NoError(t, err)
	assert.Equal(t, 2, q.ActiveSessions)
	assert.Equal(t, 3, q.ActiveWorkers)
}

func TestInMemory_QuotaDecrement_Clamps(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.NoError(t, repo.QuotaIncrement(ctx, eth.MustNewAddress("0x1111111111111111111111111111111111111111"), 1, 1))
	require.NoError(t, repo.QuotaDecrement(ctx, eth.MustNewAddress("0x1111111111111111111111111111111111111111"), 5, 5))

	q, err := repo.QuotaGet(ctx, eth.MustNewAddress("0x1111111111111111111111111111111111111111"))
	require.NoError(t, err)
	assert.Equal(t, 0, q.ActiveSessions)
	assert.Equal(t, 0, q.ActiveWorkers)
}

func TestInMemory_QuotaDecrement_NoPayer(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	// No error when payer does not exist yet
	require.NoError(t, repo.QuotaDecrement(ctx, eth.MustNewAddress("0xBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"), 1, 1))
}

// --- Usage tests ---

// TestInMemory_UsageAdd_GetTotal and UsageGetTotal_Empty removed - UsageGetTotal method no longer exists

func TestInMemory_UsageAdd_NilEvent(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.Error(t, repo.UsageAdd(ctx, "s1", nil))
}

// TestInMemory_UsageAdd_MultipleSessionsIsolated removed - UsageGetTotal method no longer exists

// --- Ping / Close ---

func TestInMemory_PingAndClose(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.NoError(t, repo.Ping(ctx))
	require.NoError(t, repo.Close())
}
