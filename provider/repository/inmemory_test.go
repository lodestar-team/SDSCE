package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSession(id, payer string) *repository.Session {
	return &repository.Session{
		ID:           id,
		PayerAddress: payer,
		Status:       repository.SessionStatusActive,
		CreatedAt:    time.Now(),
	}
}

func newTestWorker(key, sessionID, payer string) *repository.Worker {
	return &repository.Worker{
		Key:          key,
		SessionID:    sessionID,
		PayerAddress: payer,
		CreatedAt:    time.Now(),
	}
}

// --- Session tests ---

func TestInMemory_SessionCreate_Get(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	s := newTestSession("s1", "0xpayer1")
	require.NoError(t, repo.SessionCreate(ctx, s))

	got, err := repo.SessionGet(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "s1", got.ID)
	assert.Equal(t, "0xpayer1", got.PayerAddress)
}

func TestInMemory_SessionCreate_Duplicate(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	s := newTestSession("s1", "0xpayer1")
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
	assert.Contains(t, err.Error(), "not found")
}

func TestInMemory_SessionUpdate(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	s := newTestSession("s1", "0xpayer1")
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

	s := newTestSession("missing", "0xpayer1")
	require.Error(t, repo.SessionUpdate(ctx, s))
}

func TestInMemory_SessionDelete(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	s := newTestSession("s1", "0xpayer1")
	require.NoError(t, repo.SessionCreate(ctx, s))
	require.NoError(t, repo.SessionDelete(ctx, "s1"))

	_, err := repo.SessionGet(ctx, "s1")
	require.Error(t, err)
}

func TestInMemory_SessionDelete_NotFound(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.Error(t, repo.SessionDelete(ctx, "missing"))
}

func TestInMemory_SessionList_NoFilter(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.NoError(t, repo.SessionCreate(ctx, newTestSession("s1", "0xpayer1")))
	require.NoError(t, repo.SessionCreate(ctx, newTestSession("s2", "0xpayer2")))
	require.NoError(t, repo.SessionCreate(ctx, newTestSession("s3", "0xpayer1")))

	all, err := repo.SessionList(ctx, repository.SessionFilter{})
	require.NoError(t, err)
	assert.Len(t, all, 3)
}

func TestInMemory_SessionList_ByPayer(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.NoError(t, repo.SessionCreate(ctx, newTestSession("s1", "0xpayer1")))
	require.NoError(t, repo.SessionCreate(ctx, newTestSession("s2", "0xpayer2")))
	require.NoError(t, repo.SessionCreate(ctx, newTestSession("s3", "0xpayer1")))

	payer := "0xpayer1"
	sessions, err := repo.SessionList(ctx, repository.SessionFilter{PayerAddress: &payer})
	require.NoError(t, err)
	assert.Len(t, sessions, 2)
	for _, s := range sessions {
		assert.Equal(t, "0xpayer1", s.PayerAddress)
	}
}

func TestInMemory_SessionList_ByStatus(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	s1 := newTestSession("s1", "0xpayer1")
	s2 := newTestSession("s2", "0xpayer1")
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

	s1 := newTestSession("s1", "0xpayer1")
	s1.CreatedAt = time.Now().Add(-30 * time.Minute) // between past and future
	s2 := newTestSession("s2", "0xpayer1")
	s2.CreatedAt = time.Now().Add(-2 * time.Hour) // before past

	require.NoError(t, repo.SessionCreate(ctx, s1))
	require.NoError(t, repo.SessionCreate(ctx, s2))

	_ = future
	sessions, err := repo.SessionList(ctx, repository.SessionFilter{CreatedAfter: &past})
	require.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, "s1", sessions[0].ID)
}

func TestInMemory_SessionGetByPayer(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.NoError(t, repo.SessionCreate(ctx, newTestSession("s1", "0xpayer1")))
	require.NoError(t, repo.SessionCreate(ctx, newTestSession("s2", "0xpayer2")))

	sessions, err := repo.SessionGetByPayer(ctx, "0xpayer1")
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, "s1", sessions[0].ID)
}

// --- Worker tests ---

func TestInMemory_WorkerCreate_Get(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	w := newTestWorker("w1", "s1", "0xpayer1")
	require.NoError(t, repo.WorkerCreate(ctx, w))

	got, err := repo.WorkerGet(ctx, "w1")
	require.NoError(t, err)
	assert.Equal(t, "w1", got.Key)
	assert.Equal(t, "s1", got.SessionID)
}

func TestInMemory_WorkerCreate_Duplicate(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	w := newTestWorker("w1", "s1", "0xpayer1")
	require.NoError(t, repo.WorkerCreate(ctx, w))
	require.Error(t, repo.WorkerCreate(ctx, w))
}

func TestInMemory_WorkerGet_NotFound(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	_, err := repo.WorkerGet(ctx, "missing")
	require.Error(t, err)
}

func TestInMemory_WorkerDelete(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	w := newTestWorker("w1", "s1", "0xpayer1")
	require.NoError(t, repo.WorkerCreate(ctx, w))
	require.NoError(t, repo.WorkerDelete(ctx, "w1"))

	_, err := repo.WorkerGet(ctx, "w1")
	require.Error(t, err)
}

func TestInMemory_WorkerListBySession(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.NoError(t, repo.WorkerCreate(ctx, newTestWorker("w1", "s1", "0xpayer1")))
	require.NoError(t, repo.WorkerCreate(ctx, newTestWorker("w2", "s1", "0xpayer1")))
	require.NoError(t, repo.WorkerCreate(ctx, newTestWorker("w3", "s2", "0xpayer2")))

	workers, err := repo.WorkerListBySession(ctx, "s1")
	require.NoError(t, err)
	assert.Len(t, workers, 2)

	workers2, err := repo.WorkerListBySession(ctx, "s2")
	require.NoError(t, err)
	assert.Len(t, workers2, 1)
}

func TestInMemory_WorkerCountByPayer(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.NoError(t, repo.WorkerCreate(ctx, newTestWorker("w1", "s1", "0xpayer1")))
	require.NoError(t, repo.WorkerCreate(ctx, newTestWorker("w2", "s1", "0xpayer1")))
	require.NoError(t, repo.WorkerCreate(ctx, newTestWorker("w3", "s2", "0xpayer2")))

	count, err := repo.WorkerCountByPayer(ctx, "0xpayer1")
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	count2, err := repo.WorkerCountByPayer(ctx, "0xpayer3")
	require.NoError(t, err)
	assert.Equal(t, 0, count2)
}

// --- Quota tests ---

func TestInMemory_QuotaGet_NewPayer(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	q, err := repo.QuotaGet(ctx, "0xnewpayer")
	require.NoError(t, err)
	assert.NotNil(t, q)
	assert.Equal(t, "0xnewpayer", q.PayerAddress)
	assert.Equal(t, 0, q.ActiveSessions)
	assert.Equal(t, 0, q.ActiveWorkers)
}

func TestInMemory_QuotaIncrement(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.NoError(t, repo.QuotaIncrement(ctx, "0xpayer1", 1, 2))

	q, err := repo.QuotaGet(ctx, "0xpayer1")
	require.NoError(t, err)
	assert.Equal(t, 1, q.ActiveSessions)
	assert.Equal(t, 2, q.ActiveWorkers)

	require.NoError(t, repo.QuotaIncrement(ctx, "0xpayer1", 0, 1))

	q, err = repo.QuotaGet(ctx, "0xpayer1")
	require.NoError(t, err)
	assert.Equal(t, 1, q.ActiveSessions)
	assert.Equal(t, 3, q.ActiveWorkers)
}

func TestInMemory_QuotaDecrement(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.NoError(t, repo.QuotaIncrement(ctx, "0xpayer1", 3, 5))
	require.NoError(t, repo.QuotaDecrement(ctx, "0xpayer1", 1, 2))

	q, err := repo.QuotaGet(ctx, "0xpayer1")
	require.NoError(t, err)
	assert.Equal(t, 2, q.ActiveSessions)
	assert.Equal(t, 3, q.ActiveWorkers)
}

func TestInMemory_QuotaDecrement_Clamps(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.NoError(t, repo.QuotaIncrement(ctx, "0xpayer1", 1, 1))
	require.NoError(t, repo.QuotaDecrement(ctx, "0xpayer1", 5, 5))

	q, err := repo.QuotaGet(ctx, "0xpayer1")
	require.NoError(t, err)
	assert.Equal(t, 0, q.ActiveSessions)
	assert.Equal(t, 0, q.ActiveWorkers)
}

func TestInMemory_QuotaDecrement_NoPayer(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	// No error when payer does not exist yet
	require.NoError(t, repo.QuotaDecrement(ctx, "0xunknown", 1, 1))
}

// --- Usage tests ---

func TestInMemory_UsageAdd_GetTotal(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.NoError(t, repo.UsageAdd(ctx, "s1", &repository.UsageEvent{Blocks: 10, Bytes: 200, Requests: 3}))
	require.NoError(t, repo.UsageAdd(ctx, "s1", &repository.UsageEvent{Blocks: 5, Bytes: 100, Requests: 1}))

	total, err := repo.UsageGetTotal(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, int64(15), total.TotalBlocks)
	assert.Equal(t, int64(300), total.TotalBytes)
	assert.Equal(t, int64(4), total.TotalRequests)
}

func TestInMemory_UsageGetTotal_Empty(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	total, err := repo.UsageGetTotal(ctx, "nonexistent")
	require.NoError(t, err)
	assert.NotNil(t, total)
	assert.Equal(t, int64(0), total.TotalBlocks)
}

func TestInMemory_UsageAdd_NilEvent(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.Error(t, repo.UsageAdd(ctx, "s1", nil))
}

func TestInMemory_UsageAdd_MultipleSessionsIsolated(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.NoError(t, repo.UsageAdd(ctx, "s1", &repository.UsageEvent{Blocks: 10}))
	require.NoError(t, repo.UsageAdd(ctx, "s2", &repository.UsageEvent{Blocks: 20}))

	total1, err := repo.UsageGetTotal(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, int64(10), total1.TotalBlocks)

	total2, err := repo.UsageGetTotal(ctx, "s2")
	require.NoError(t, err)
	assert.Equal(t, int64(20), total2.TotalBlocks)
}

// --- Ping / Close ---

func TestInMemory_PingAndClose(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()

	require.NoError(t, repo.Ping(ctx))
	require.NoError(t, repo.Close())
}
