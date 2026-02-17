package repository

import (
	"context"
	"time"
)

// GlobalRepository provides global state storage for live session/client tracking.
// All methods are namespaced by domain (Session*, Client*, Quota*, etc.)
// All implementations must be safe for concurrent use.
type GlobalRepository interface {
	// Session management
	SessionCreate(ctx context.Context, session *Session) error
	SessionGet(ctx context.Context, sessionID string) (*Session, error)
	SessionUpdate(ctx context.Context, session *Session) error
	SessionDelete(ctx context.Context, sessionID string) error
	SessionList(ctx context.Context, filter SessionFilter) ([]*Session, error)
	SessionGetByPayer(ctx context.Context, payer string) ([]*Session, error)

	// Worker/connection tracking within sessions
	WorkerCreate(ctx context.Context, worker *Worker) error
	WorkerGet(ctx context.Context, workerKey string) (*Worker, error)
	WorkerDelete(ctx context.Context, workerKey string) error
	WorkerListBySession(ctx context.Context, sessionID string) ([]*Worker, error)
	WorkerCountByPayer(ctx context.Context, payer string) (int, error)

	// Quota tracking
	QuotaGet(ctx context.Context, payer string) (*QuotaUsage, error)
	QuotaIncrement(ctx context.Context, payer string, sessions int, workers int) error
	QuotaDecrement(ctx context.Context, payer string, sessions int, workers int) error

	// Usage accumulation (for metering)
	UsageAdd(ctx context.Context, sessionID string, usage *UsageEvent) error
	UsageGetTotal(ctx context.Context, sessionID string) (*UsageSummary, error)

	// Health/lifecycle
	Ping(ctx context.Context) error
	Close() error
}

// SessionStatus represents the lifecycle state of a session.
type SessionStatus string

const (
	SessionStatusActive     SessionStatus = "active"
	SessionStatusTerminated SessionStatus = "terminated"
)

// Session represents an active or terminated payment/streaming session.
type Session struct {
	ID              string
	PayerAddress    string
	SignerAddress   string
	ServiceProvider string
	CreatedAt       time.Time
	LastKeepAlive   time.Time
	Status          SessionStatus
	Metadata        map[string]string
}

// Worker represents a single streaming connection (worker) within a session.
type Worker struct {
	Key          string
	SessionID    string
	PayerAddress string
	CreatedAt    time.Time
	TraceID      string
}

// QuotaUsage tracks the current quota consumption for a payer address.
type QuotaUsage struct {
	PayerAddress   string
	ActiveSessions int
	ActiveWorkers  int
	LastUpdated    time.Time
}

// UsageEvent represents a single metered usage event within a session.
type UsageEvent struct {
	Timestamp time.Time
	Blocks    int64
	Bytes     int64
	Requests  int64
}

// UsageSummary aggregates total usage across all events for a session.
type UsageSummary struct {
	TotalBlocks   int64
	TotalBytes    int64
	TotalRequests int64
}

// SessionFilter specifies criteria for filtering sessions in a list operation.
type SessionFilter struct {
	PayerAddress *string
	Status       *SessionStatus
	CreatedAfter *time.Time
}
