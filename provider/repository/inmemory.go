package repository

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/alphadose/haxmap"
	"github.com/graphprotocol/substreams-data-service/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	"github.com/streamingfast/eth-go"
)

// newStringMap creates a new haxmap keyed by string.
func newStringMap[V any]() *haxmap.Map[string, V] {
	return haxmap.New[string, V]()
}

// InMemoryRepository is an in-memory implementation of GlobalRepository.
// It is safe for concurrent use.
type InMemoryRepository struct {
	sessions *haxmap.Map[string, *Session]
	workers  *haxmap.Map[string, *Worker]
	quotas   *haxmap.Map[string, *QuotaUsage]

	// mu serializes worker/quota mutations so multi-step repository operations
	// can be made atomic inside the in-memory backend.
	mu sync.Mutex

	// usageMu guards usage slice append operations; haxmap does not natively
	// support atomic append-to-slice, so we use a thin mutex here.
	usageMu sync.Mutex
	usage   *haxmap.Map[string, []*UsageEvent]
}

var _ GlobalRepository = (*InMemoryRepository)(nil)

// NewInMemoryRepository creates and returns a new InMemoryRepository.
func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{
		sessions: newStringMap[*Session](),
		workers:  newStringMap[*Worker](),
		quotas:   newStringMap[*QuotaUsage](),
		usage:    newStringMap[[]*UsageEvent](),
	}
}

// --- Session management ---

// SessionCreate stores a new session. Returns an error if the session ID already exists.
func (r *InMemoryRepository) SessionCreate(_ context.Context, session *Session) error {
	if session == nil {
		return fmt.Errorf("session must not be nil")
	}
	if session.ID == "" {
		return fmt.Errorf("session ID must not be empty")
	}
	if _, loaded := r.sessions.GetOrSet(session.ID, cloneSession(session)); loaded {
		return fmt.Errorf("session %q already exists", session.ID)
	}
	return nil
}

// SessionGet retrieves a session by ID.
func (r *InMemoryRepository) SessionGet(_ context.Context, sessionID string) (*Session, error) {
	s, ok := r.sessions.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %q: %w", sessionID, ErrNotFound)
	}
	return cloneSession(s), nil
}

// SessionUpdate replaces the stored session. Returns an error if the session does not exist.
func (r *InMemoryRepository) SessionUpdate(_ context.Context, session *Session) error {
	if session == nil {
		return fmt.Errorf("session must not be nil")
	}

	next := cloneSession(session)
	for {
		current, ok := r.sessions.Get(session.ID)
		if !ok {
			return fmt.Errorf("session %q not found", session.ID)
		}
		if r.sessions.CompareAndSwap(session.ID, current, next) {
			return nil
		}
	}
}

// SessionTouch updates only the last keep-alive timestamp for the stored session.
func (r *InMemoryRepository) SessionTouch(_ context.Context, sessionID string, lastKeepAlive time.Time) error {
	for {
		session, ok := r.sessions.Get(sessionID)
		if !ok {
			return fmt.Errorf("session %q: %w", sessionID, ErrNotFound)
		}

		next := cloneSession(session)
		if lastKeepAlive.After(next.LastKeepAlive) {
			next.LastKeepAlive = lastKeepAlive
		}
		if next.LastKeepAlive.After(next.UpdatedAt) {
			next.UpdatedAt = next.LastKeepAlive
		}

		if r.sessions.CompareAndSwap(sessionID, session, next) {
			return nil
		}
	}
}

// SessionUpdateRuntimeState updates only lifecycle and runtime metadata fields.
func (r *InMemoryRepository) SessionUpdateRuntimeState(_ context.Context, sessionID string, status SessionStatus, metadata map[string]string, endedAt *time.Time, endReason commonv1.EndReason, updatedAt time.Time) error {
	for {
		session, ok := r.sessions.Get(sessionID)
		if !ok {
			return fmt.Errorf("session %q: %w", sessionID, ErrNotFound)
		}

		next := cloneSession(session)
		if next.Status != SessionStatusTerminated {
			next.Status = status
			next.Metadata = cloneStringMap(metadata)
			next.EndedAt = cloneTimePtr(endedAt)
			next.EndReason = endReason
			if updatedAt.After(next.UpdatedAt) {
				next.UpdatedAt = updatedAt
			}
		}

		if r.sessions.CompareAndSwap(sessionID, session, next) {
			return nil
		}
	}
}

// SessionUpdateRAVAndBaseline updates only the accepted RAV and the corresponding baseline snapshot.
func (r *InMemoryRepository) SessionUpdateRAVAndBaseline(_ context.Context, sessionID string, currentRAV *horizon.SignedRAV, baselineBlocks, baselineBytes, baselineReqs uint64, baselineCost *big.Int) error {
	for {
		session, ok := r.sessions.Get(sessionID)
		if !ok {
			return fmt.Errorf("session %q: %w", sessionID, ErrNotFound)
		}

		next := cloneSession(session)
		next.CurrentRAV = cloneSignedRAV(currentRAV)
		next.BaselineBlocks = baselineBlocks
		next.BaselineBytes = baselineBytes
		next.BaselineReqs = baselineReqs
		if baselineCost != nil {
			next.BaselineCost = new(big.Int).Set(baselineCost)
		} else {
			next.BaselineCost = big.NewInt(0)
		}
		next.UpdatedAt = time.Now()

		if r.sessions.CompareAndSwap(sessionID, session, next) {
			return nil
		}
	}
}

// SessionApplyUsage appends a usage event and advances the owning session aggregates.
func (r *InMemoryRepository) SessionApplyUsage(ctx context.Context, sessionID string, usage *UsageEvent, cost *big.Int) error {
	if usage == nil {
		return fmt.Errorf("usage event must not be nil")
	}

	blocks, bytes, requests := usage.SanitizedTotals()
	for {
		session, ok := r.sessions.Get(sessionID)
		if !ok {
			return fmt.Errorf("session %q: %w", sessionID, ErrNotFound)
		}

		next := cloneSession(session)
		next.AddUsage(blocks, bytes, requests, cost)
		if r.sessions.CompareAndSwap(sessionID, session, next) {
			break
		}
	}

	return r.UsageAdd(ctx, sessionID, usage)
}

// SessionList returns all sessions that match the given filter.
func (r *InMemoryRepository) SessionList(_ context.Context, filter SessionFilter) ([]*Session, error) {
	var result []*Session
	r.sessions.ForEach(func(_ string, s *Session) bool {
		if filter.Payer != nil && s.Payer.Pretty() != filter.Payer.Pretty() {
			return true
		}
		if filter.Status != nil && s.Status != *filter.Status {
			return true
		}
		if filter.CreatedAfter != nil && !s.CreatedAt.After(*filter.CreatedAfter) {
			return true
		}
		result = append(result, cloneSession(s))
		return true
	})
	return result, nil
}

// SessionCount returns the total number of sessions.
func (r *InMemoryRepository) SessionCount(_ context.Context) int {
	return int(r.sessions.Len())
}

// --- Worker management ---

// WorkerCreate stores a new worker. Returns an error if the worker key already exists.
func (r *InMemoryRepository) WorkerCreate(_ context.Context, worker *Worker) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if worker == nil {
		return fmt.Errorf("worker must not be nil")
	}
	if worker.Key == "" {
		return fmt.Errorf("worker key must not be empty")
	}
	if _, loaded := r.workers.GetOrSet(worker.Key, cloneWorker(worker)); loaded {
		return fmt.Errorf("worker %q already exists", worker.Key)
	}
	return nil
}

// WorkerCreateAndReserveQuota atomically creates a worker and reserves quota for its payer.
func (r *InMemoryRepository) WorkerCreateAndReserveQuota(_ context.Context, worker *Worker, maxWorkers int) (*QuotaUsage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if worker == nil {
		return nil, fmt.Errorf("worker must not be nil")
	}
	if worker.Key == "" {
		return nil, fmt.Errorf("worker key must not be empty")
	}
	if maxWorkers < 0 {
		return nil, fmt.Errorf("max workers must not be negative")
	}

	if _, loaded := r.workers.Get(worker.Key); loaded {
		return nil, fmt.Errorf("worker %q already exists", worker.Key)
	}

	payerKey := worker.Payer.Pretty()
	current, ok := r.quotas.Get(payerKey)
	if !ok {
		if 1 > maxWorkers {
			return &QuotaUsage{
				Payer:       worker.Payer,
				LastUpdated: time.Now(),
			}, ErrQuotaExceeded
		}

		next := &QuotaUsage{
			Payer:         worker.Payer,
			ActiveWorkers: 1,
			LastUpdated:   time.Now(),
		}
		r.workers.Set(worker.Key, cloneWorker(worker))
		r.quotas.Set(payerKey, next)
		return cloneQuotaUsage(next), nil
	}

	if current.ActiveWorkers+1 > maxWorkers {
		return cloneQuotaUsage(current), ErrQuotaExceeded
	}

	next := cloneQuotaUsage(current)
	next.ActiveWorkers++
	next.LastUpdated = time.Now()
	r.workers.Set(worker.Key, cloneWorker(worker))
	r.quotas.Set(payerKey, next)
	return cloneQuotaUsage(next), nil
}

// WorkerGet retrieves a worker by its key.
func (r *InMemoryRepository) WorkerGet(_ context.Context, workerKey string) (*Worker, error) {
	w, ok := r.workers.Get(workerKey)
	if !ok {
		return nil, fmt.Errorf("worker %q: %w", workerKey, ErrNotFound)
	}
	return cloneWorker(w), nil
}

// WorkerDelete removes a worker by its key.
func (r *InMemoryRepository) WorkerDelete(_ context.Context, workerKey string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.workers.Get(workerKey); !ok {
		return fmt.Errorf("worker %q not found", workerKey)
	}
	r.workers.Del(workerKey)
	return nil
}

// --- Quota management ---

// QuotaGet returns the current quota usage for a payer. Returns a zero-value
// QuotaUsage (not an error) when no entry exists yet.
func (r *InMemoryRepository) QuotaGet(_ context.Context, payer eth.Address) (*QuotaUsage, error) {
	payerKey := payer.Pretty()
	q, ok := r.quotas.Get(payerKey)
	if !ok {
		return &QuotaUsage{
			Payer:       payer,
			LastUpdated: time.Now(),
		}, nil
	}
	return cloneQuotaUsage(q), nil
}

// QuotaReserve atomically reserves worker quota for a payer.
func (r *InMemoryRepository) QuotaReserve(_ context.Context, payer eth.Address, maxWorkers int, workers int) (*QuotaUsage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if workers <= 0 {
		return nil, fmt.Errorf("workers must be positive")
	}
	if maxWorkers < 0 {
		return nil, fmt.Errorf("max workers must not be negative")
	}

	payerKey := payer.Pretty()
	for {
		current, ok := r.quotas.Get(payerKey)
		if !ok {
			if workers > maxWorkers {
				return &QuotaUsage{
					Payer:       payer,
					LastUpdated: time.Now(),
				}, ErrQuotaExceeded
			}

			next := &QuotaUsage{
				Payer:         payer,
				ActiveWorkers: workers,
				LastUpdated:   time.Now(),
			}
			if actual, loaded := r.quotas.GetOrSet(payerKey, next); !loaded {
				return cloneQuotaUsage(next), nil
			} else {
				current = actual
			}
		}

		if current.ActiveWorkers+workers > maxWorkers {
			return cloneQuotaUsage(current), ErrQuotaExceeded
		}

		next := cloneQuotaUsage(current)
		next.ActiveWorkers += workers
		next.LastUpdated = time.Now()

		if r.quotas.CompareAndSwap(payerKey, current, next) {
			return cloneQuotaUsage(next), nil
		}
	}
}

// QuotaIncrement atomically increments the quota counters for a payer.
func (r *InMemoryRepository) QuotaIncrement(_ context.Context, payer eth.Address, sessions int, workers int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	payerKey := payer.Pretty()
	for {
		current, ok := r.quotas.Get(payerKey)
		if !ok {
			next := &QuotaUsage{Payer: payer}
			next.ActiveSessions = sessions
			next.ActiveWorkers = workers
			next.LastUpdated = time.Now()
			if actual, loaded := r.quotas.GetOrSet(payerKey, next); !loaded {
				return nil
			} else {
				current = actual
			}
		}

		next := cloneQuotaUsage(current)
		next.ActiveSessions += sessions
		next.ActiveWorkers += workers
		next.LastUpdated = time.Now()

		if r.quotas.CompareAndSwap(payerKey, current, next) {
			return nil
		}
	}
}

// QuotaDecrement atomically decrements the quota counters for a payer.
// Counters are clamped to zero to prevent underflow.
func (r *InMemoryRepository) QuotaDecrement(_ context.Context, payer eth.Address, sessions int, workers int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	payerKey := payer.Pretty()
	for {
		current, ok := r.quotas.Get(payerKey)
		if !ok {
			return nil
		}

		next := cloneQuotaUsage(current)
		next.ActiveSessions -= sessions
		if next.ActiveSessions < 0 {
			next.ActiveSessions = 0
		}
		next.ActiveWorkers -= workers
		if next.ActiveWorkers < 0 {
			next.ActiveWorkers = 0
		}
		next.LastUpdated = time.Now()

		if r.quotas.CompareAndSwap(payerKey, current, next) {
			return nil
		}
	}
}

// --- Usage accumulation ---

// UsageAdd appends a usage event to the session's usage log.
func (r *InMemoryRepository) UsageAdd(_ context.Context, sessionID string, usage *UsageEvent) error {
	if usage == nil {
		return fmt.Errorf("usage event must not be nil")
	}
	r.usageMu.Lock()
	defer r.usageMu.Unlock()

	events, _ := r.usage.GetOrCompute(sessionID, func() []*UsageEvent {
		return make([]*UsageEvent, 0, 8)
	})
	events = append(events, cloneUsageEvent(usage))
	r.usage.Set(sessionID, events)
	return nil
}

// --- Health/lifecycle ---

// Ping is a no-op health check for the in-memory implementation.
func (r *InMemoryRepository) Ping(_ context.Context) error {
	return nil
}

// Close is a no-op for the in-memory implementation.
func (r *InMemoryRepository) Close() error {
	return nil
}

func cloneSession(session *Session) *Session {
	if session == nil {
		return nil
	}

	clone := *session
	clone.Metadata = cloneStringMap(session.Metadata)
	clone.EndedAt = cloneTimePtr(session.EndedAt)
	clone.CurrentRAV = cloneSignedRAV(session.CurrentRAV)
	clone.TotalCost = cloneBigInt(session.TotalCost)
	clone.BaselineCost = cloneBigInt(session.BaselineCost)
	return &clone
}

func cloneWorker(worker *Worker) *Worker {
	if worker == nil {
		return nil
	}

	clone := *worker
	return &clone
}

func cloneQuotaUsage(quota *QuotaUsage) *QuotaUsage {
	if quota == nil {
		return nil
	}

	clone := *quota
	return &clone
}

func cloneUsageEvent(usage *UsageEvent) *UsageEvent {
	if usage == nil {
		return nil
	}

	clone := *usage
	return &clone
}

func cloneSignedRAV(rav *horizon.SignedRAV) *horizon.SignedRAV {
	if rav == nil {
		return nil
	}

	clone := &horizon.SignedRAV{
		Signature: rav.Signature,
	}
	if rav.Message != nil {
		clone.Message = cloneRAV(rav.Message)
	}
	return clone
}

func cloneRAV(rav *horizon.RAV) *horizon.RAV {
	if rav == nil {
		return nil
	}

	clone := *rav
	clone.ValueAggregate = cloneBigInt(rav.ValueAggregate)
	clone.Metadata = append([]byte(nil), rav.Metadata...)
	return &clone
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return make(map[string]string)
	}

	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}

	clone := *value
	return &clone
}

func cloneBigInt(value *big.Int) *big.Int {
	if value == nil {
		return nil
	}

	return new(big.Int).Set(value)
}
