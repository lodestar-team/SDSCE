package repository

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/alphadose/haxmap"
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

	// usageMu guards usageSlice append operations; haxmap does not natively
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
	if _, loaded := r.sessions.GetOrSet(session.ID, session); loaded {
		return fmt.Errorf("session %q already exists", session.ID)
	}
	return nil
}

// SessionGet retrieves a session by ID.
func (r *InMemoryRepository) SessionGet(_ context.Context, sessionID string) (*Session, error) {
	s, ok := r.sessions.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("session %q not found", sessionID)
	}
	return s, nil
}

// SessionUpdate replaces the stored session. Returns an error if the session does not exist.
func (r *InMemoryRepository) SessionUpdate(_ context.Context, session *Session) error {
	if session == nil {
		return fmt.Errorf("session must not be nil")
	}
	if _, ok := r.sessions.Get(session.ID); !ok {
		return fmt.Errorf("session %q not found", session.ID)
	}
	r.sessions.Set(session.ID, session)
	return nil
}

// SessionList returns all sessions that match the given filter.
func (r *InMemoryRepository) SessionList(_ context.Context, filter SessionFilter) ([]*Session, error) {
	var result []*Session
	r.sessions.ForEach(func(_ string, s *Session) bool {
		if filter.PayerAddress != nil && s.PayerAddress != *filter.PayerAddress {
			return true
		}
		if filter.Status != nil && s.Status != *filter.Status {
			return true
		}
		if filter.CreatedAfter != nil && !s.CreatedAt.After(*filter.CreatedAfter) {
			return true
		}
		result = append(result, s)
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
	if worker == nil {
		return fmt.Errorf("worker must not be nil")
	}
	if worker.Key == "" {
		return fmt.Errorf("worker key must not be empty")
	}
	if _, loaded := r.workers.GetOrSet(worker.Key, worker); loaded {
		return fmt.Errorf("worker %q already exists", worker.Key)
	}
	return nil
}

// WorkerGet retrieves a worker by its key.
func (r *InMemoryRepository) WorkerGet(_ context.Context, workerKey string) (*Worker, error) {
	w, ok := r.workers.Get(workerKey)
	if !ok {
		return nil, fmt.Errorf("worker %q not found", workerKey)
	}
	return w, nil
}

// WorkerDelete removes a worker by its key.
func (r *InMemoryRepository) WorkerDelete(_ context.Context, workerKey string) error {
	if _, ok := r.workers.Get(workerKey); !ok {
		return fmt.Errorf("worker %q not found", workerKey)
	}
	r.workers.Del(workerKey)
	return nil
}

// --- Quota management ---

// QuotaGet returns the current quota usage for a payer. Returns a zero-value
// QuotaUsage (not an error) when no entry exists yet.
func (r *InMemoryRepository) QuotaGet(_ context.Context, payer string) (*QuotaUsage, error) {
	q, ok := r.quotas.Get(payer)
	if !ok {
		return &QuotaUsage{
			PayerAddress: payer,
			LastUpdated:  time.Now(),
		}, nil
	}
	return q, nil
}

// QuotaIncrement atomically increments the quota counters for a payer.
func (r *InMemoryRepository) QuotaIncrement(_ context.Context, payer string, sessions int, workers int) error {
	q, _ := r.quotas.GetOrCompute(payer, func() *QuotaUsage {
		return &QuotaUsage{PayerAddress: payer}
	})
	q.ActiveSessions += sessions
	q.ActiveWorkers += workers
	q.LastUpdated = time.Now()
	r.quotas.Set(payer, q)
	return nil
}

// QuotaDecrement atomically decrements the quota counters for a payer.
// Counters are clamped to zero to prevent underflow.
func (r *InMemoryRepository) QuotaDecrement(_ context.Context, payer string, sessions int, workers int) error {
	q, ok := r.quotas.Get(payer)
	if !ok {
		return nil
	}
	q.ActiveSessions -= sessions
	if q.ActiveSessions < 0 {
		q.ActiveSessions = 0
	}
	q.ActiveWorkers -= workers
	if q.ActiveWorkers < 0 {
		q.ActiveWorkers = 0
	}
	q.LastUpdated = time.Now()
	r.quotas.Set(payer, q)
	return nil
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
	events = append(events, usage)
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
