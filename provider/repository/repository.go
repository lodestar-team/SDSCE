package repository

import (
	"context"
	"errors"
	"math/big"
	"time"

	sds "github.com/graphprotocol/substreams-data-service"
	"github.com/graphprotocol/substreams-data-service/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	"github.com/streamingfast/eth-go"
)

// ErrNotFound is returned by repository methods when a requested entity does not exist.
// All repository implementations should return this error for consistency.
var ErrNotFound = errors.New("not found")

// GlobalRepository provides global state storage for live session/client tracking.
// All methods are namespaced by domain (Session*, Client*, Quota*, etc.)
// All implementations must be safe for concurrent use.
type GlobalRepository interface {
	// Session management
	SessionCreate(ctx context.Context, session *Session) error
	SessionGet(ctx context.Context, sessionID string) (*Session, error)
	SessionUpdate(ctx context.Context, session *Session) error
	SessionList(ctx context.Context, filter SessionFilter) ([]*Session, error)
	SessionCount(ctx context.Context) int

	// Worker/connection tracking within sessions
	WorkerCreate(ctx context.Context, worker *Worker) error
	WorkerGet(ctx context.Context, workerKey string) (*Worker, error)
	WorkerDelete(ctx context.Context, workerKey string) error

	// Quota tracking
	QuotaGet(ctx context.Context, payer eth.Address) (*QuotaUsage, error)
	QuotaIncrement(ctx context.Context, payer eth.Address, sessions int, workers int) error
	QuotaDecrement(ctx context.Context, payer eth.Address, sessions int, workers int) error

	// Usage accumulation (for metering)
	UsageAdd(ctx context.Context, sessionID string, usage *UsageEvent) error

	// Health/lifecycle
	Ping(ctx context.Context) error
	Close() error
}

// SessionStatus represents the lifecycle state of a session.
type SessionStatus string

const (
	SessionStatusActive     SessionStatus = "active"
	SessionStatusPaused     SessionStatus = "paused"
	SessionStatusTerminated SessionStatus = "terminated"
)

// Session represents an active or terminated payment/streaming session.
// This unified session model supports both firehose-core plugins (auth/session/metering)
// and the provider gateway payment flow.
type Session struct {
	ID            string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	LastKeepAlive time.Time
	Status        SessionStatus
	Metadata      map[string]string
	EndedAt       *time.Time
	EndReason     commonv1.EndReason

	// Escrow account details (eth.Address for precise type handling)
	Payer       eth.Address
	Receiver    eth.Address // Service provider
	DataService eth.Address

	// Current RAV state
	CurrentRAV *horizon.SignedRAV

	// Usage tracking
	BlocksProcessed  uint64
	BytesTransferred uint64
	Requests         uint64
	TotalCost        *big.Int

	// Baseline usage snapshot for determining "usage since last RAV".
	// Provider-side logic uses this to decide when to request a new RAV.
	BaselineBlocks uint64
	BaselineBytes  uint64
	BaselineReqs   uint64
	BaselineCost   *big.Int

	// Price configuration (required - must be set at session creation)
	PricingConfig PricingConfig
}

// PricingConfig is an alias to sds.PricingConfig.
type PricingConfig = sds.PricingConfig

// NewSession creates a new session with initialized fields.
// pricingConfig is required and must be provided at session creation.
func NewSession(id string, payer, receiver, dataService eth.Address, pricingConfig PricingConfig) *Session {
	now := time.Now()
	return &Session{
		ID:            id,
		Payer:         payer,
		Receiver:      receiver,
		DataService:   dataService,
		Status:        SessionStatusActive,
		CreatedAt:     now,
		UpdatedAt:     now,
		LastKeepAlive: now,
		TotalCost:     big.NewInt(0),
		BaselineCost:  big.NewInt(0),
		PricingConfig: pricingConfig,
		Metadata:      make(map[string]string),
	}
}

// IsActive returns true if the session is active.
func (s *Session) IsActive() bool {
	return s.Status == SessionStatusActive
}

// AddUsage adds usage to the session.
func (s *Session) AddUsage(blocks, bytes, requests uint64, cost *big.Int) {
	s.BlocksProcessed += blocks
	s.BytesTransferred += bytes
	s.Requests += requests
	if cost != nil {
		if s.TotalCost == nil {
			s.TotalCost = big.NewInt(0)
		}
		s.TotalCost = new(big.Int).Add(s.TotalCost, cost)
	}
	s.UpdatedAt = time.Now()
}

// UsageDeltaSinceBaseline returns the usage accrued since the baseline snapshot.
func (s *Session) UsageDeltaSinceBaseline() (blocks, bytes, requests uint64, cost *big.Int) {
	if s.BlocksProcessed >= s.BaselineBlocks {
		blocks = s.BlocksProcessed - s.BaselineBlocks
	}
	if s.BytesTransferred >= s.BaselineBytes {
		bytes = s.BytesTransferred - s.BaselineBytes
	}
	if s.Requests >= s.BaselineReqs {
		requests = s.Requests - s.BaselineReqs
	}

	if s.TotalCost == nil {
		cost = big.NewInt(0)
	} else if s.BaselineCost == nil {
		cost = new(big.Int).Set(s.TotalCost)
	} else {
		cost = new(big.Int).Sub(s.TotalCost, s.BaselineCost)
		if cost.Sign() < 0 {
			cost = big.NewInt(0)
		}
	}
	return
}

// MarkBaseline sets the baseline snapshot to the current accumulated usage.
func (s *Session) MarkBaseline() {
	s.BaselineBlocks = s.BlocksProcessed
	s.BaselineBytes = s.BytesTransferred
	s.BaselineReqs = s.Requests
	if s.TotalCost != nil {
		s.BaselineCost = new(big.Int).Set(s.TotalCost)
	} else {
		s.BaselineCost = big.NewInt(0)
	}
}

// SetPricingConfig sets the pricing configuration for the session.
func (s *Session) SetPricingConfig(config PricingConfig) {
	s.PricingConfig = config
}

// CalculateUsageCost calculates the cost for given usage using session's pricing config.
func (s *Session) CalculateUsageCost(blocksProcessed, bytesTransferred uint64) *big.Int {
	return s.PricingConfig.CalculateUsageCost(blocksProcessed, bytesTransferred).BigInt()
}

// End marks the session as terminated.
func (s *Session) End(reason commonv1.EndReason) {
	now := time.Now()
	s.Status = SessionStatusTerminated
	s.EndedAt = &now
	s.EndReason = reason
	s.UpdatedAt = now
}

// GetUsage returns the current usage as a proto message.
func (s *Session) GetUsage() *commonv1.Usage {
	return &commonv1.Usage{
		BlocksProcessed:  s.BlocksProcessed,
		BytesTransferred: s.BytesTransferred,
		Requests:         s.Requests,
		Cost:             commonv1.GRTFromBigInt(s.TotalCost),
	}
}

// Worker represents a single streaming connection (worker) within a session.
type Worker struct {
	Key       string
	SessionID string
	Payer     eth.Address
	CreatedAt time.Time
	TraceID   string
}

// QuotaUsage tracks the current quota consumption for a payer address.
type QuotaUsage struct {
	Payer          eth.Address
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
	Payer        *eth.Address
	Status       *SessionStatus
	CreatedAfter *time.Time
}
