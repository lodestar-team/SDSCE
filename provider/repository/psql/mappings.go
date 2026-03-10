package psql

import (
	"database/sql"
	"time"

	"github.com/graphprotocol/substreams-data-service/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/streamingfast/eth-go"
)

// sessionRow represents a session record in the database
type sessionRow struct {
	ID            string        `db:"id"`
	CreatedAt     time.Time     `db:"created_at"`
	UpdatedAt     time.Time     `db:"updated_at"`
	LastKeepAlive time.Time     `db:"last_keep_alive"`
	Status        string        `db:"status"`
	Metadata      jsonbMap      `db:"metadata"`
	EndedAt       sql.NullTime  `db:"ended_at"`
	EndReason     sql.NullInt32 `db:"end_reason"`

	// Escrow addresses (using custom types)
	Payer       address `db:"payer"`
	Receiver    address `db:"receiver"`
	DataService address `db:"data_service"`
	Signer      address `db:"signer"`

	// Usage tracking
	BlocksProcessed  int64 `db:"blocks_processed"`
	BytesTransferred int64 `db:"bytes_transferred"`
	Requests         int64 `db:"requests"`
	TotalCost        grt   `db:"total_cost"`

	// Baseline snapshots
	BaselineBlocks int64 `db:"baseline_blocks"`
	BaselineBytes  int64 `db:"baseline_bytes"`
	BaselineReqs   int64 `db:"baseline_reqs"`
	BaselineCost   grt   `db:"baseline_cost"`
}

// ravRow represents a RAV record in the database
type ravRow struct {
	SessionID string `db:"session_id"`

	// RAV message fields (using custom types)
	CollectionID    collectionID `db:"collection_id"`
	Payer           address      `db:"payer"`
	ServiceProvider address      `db:"service_provider"`
	DataService     address      `db:"data_service"`
	TimestampNs     int64        `db:"timestamp_ns"`
	ValueAggregate  grt          `db:"value_aggregate"`
	Metadata        []byte       `db:"metadata"`

	// Signature
	Signature signature `db:"signature"`

	CreatedAt time.Time `db:"created_at"`
}

// workerRow represents a worker record in the database
type workerRow struct {
	Key       string         `db:"key"`
	SessionID string         `db:"session_id"`
	Payer     address        `db:"payer"`
	CreatedAt time.Time      `db:"created_at"`
	TraceID   sql.NullString `db:"trace_id"`
}

// quotaUsageRow represents quota usage in the database
type quotaUsageRow struct {
	Payer          address   `db:"payer"`
	ActiveSessions int       `db:"active_sessions"`
	ActiveWorkers  int       `db:"active_workers"`
	LastUpdated    time.Time `db:"last_updated"`
}

// usageEventRow represents a usage event in the database
type usageEventRow struct {
	ID        int64     `db:"id"`
	SessionID string    `db:"session_id"`
	Timestamp time.Time `db:"timestamp"`
	Blocks    int64     `db:"blocks"`
	Bytes     int64     `db:"bytes"`
	Requests  int64     `db:"requests"`
}

// toRepository converts sessionRow to repository.Session
// Note: PricingConfig is NOT stored in the database and must be provided at runtime
func (row *sessionRow) toRepository(rav *horizon.SignedRAV, pricingConfig repository.PricingConfig) *repository.Session {
	var endedAt *time.Time
	if row.EndedAt.Valid {
		endedAt = &row.EndedAt.Time
	}

	var endReason commonv1.EndReason
	if row.EndReason.Valid {
		endReason = commonv1.EndReason(row.EndReason.Int32)
	}

	payerAddr := row.Payer.Address()
	receiverAddr := row.Receiver.Address()
	dataServiceAddr := row.DataService.Address()

	// jsonbMap is map[string]string, so we can use it directly
	metadata := make(map[string]string)
	if row.Metadata != nil {
		metadata = map[string]string(row.Metadata)
	}

	return &repository.Session{
		ID:               row.ID,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
		LastKeepAlive:    row.LastKeepAlive,
		Status:           repository.SessionStatus(row.Status),
		Metadata:         metadata,
		EndedAt:          endedAt,
		EndReason:        endReason,
		Payer:            payerAddr,
		Receiver:         receiverAddr,
		DataService:      dataServiceAddr,
		CurrentRAV:       rav,
		BlocksProcessed:  uint64(row.BlocksProcessed),
		BytesTransferred: uint64(row.BytesTransferred),
		Requests:         uint64(row.Requests),
		TotalCost:        row.TotalCost.BigInt(),
		BaselineBlocks:   uint64(row.BaselineBlocks),
		BaselineBytes:    uint64(row.BaselineBytes),
		BaselineReqs:     uint64(row.BaselineReqs),
		BaselineCost:     row.BaselineCost.BigInt(),
		PricingConfig:    pricingConfig,
	}
}

// fromSession converts repository.Session to sessionRow
func fromSession(session *repository.Session) *sessionRow {
	var endedAt sql.NullTime
	if session.EndedAt != nil {
		endedAt = sql.NullTime{Time: *session.EndedAt, Valid: true}
	}

	var endReason sql.NullInt32
	if session.EndReason != 0 {
		endReason = sql.NullInt32{Int32: int32(session.EndReason), Valid: true}
	}

	// Convert map to jsonbMap (which is map[string]string)
	metadata := jsonbMap(session.Metadata)
	if metadata == nil {
		metadata = make(jsonbMap)
	}

	return &sessionRow{
		ID:               session.ID,
		CreatedAt:        session.CreatedAt,
		UpdatedAt:        session.UpdatedAt,
		LastKeepAlive:    session.LastKeepAlive,
		Status:           string(session.Status),
		Metadata:         metadata,
		EndedAt:          endedAt,
		EndReason:        endReason,
		Payer:            newAddress(session.Payer),
		Receiver:         newAddress(session.Receiver),
		DataService:      newAddress(session.DataService),
		Signer:           newAddress(eth.Address{}), // Zero address for signer
		BlocksProcessed:  int64(session.BlocksProcessed),
		BytesTransferred: int64(session.BytesTransferred),
		Requests:         int64(session.Requests),
		TotalCost:        newGRT(session.TotalCost),
		BaselineBlocks:   int64(session.BaselineBlocks),
		BaselineBytes:    int64(session.BaselineBytes),
		BaselineReqs:     int64(session.BaselineReqs),
		BaselineCost:     newGRT(session.BaselineCost),
	}
}

// toRepository converts ravRow to horizon.SignedRAV
func (row *ravRow) toRepository() *horizon.SignedRAV {
	if row == nil {
		return nil
	}

	return &horizon.SignedRAV{
		Message: &horizon.RAV{
			CollectionID:    horizon.CollectionID(row.CollectionID.Bytes()),
			Payer:           row.Payer.Address(),
			ServiceProvider: row.ServiceProvider.Address(),
			DataService:     row.DataService.Address(),
			TimestampNs:     uint64(row.TimestampNs),
			ValueAggregate:  row.ValueAggregate.BigInt(),
			Metadata:        row.Metadata,
		},
		Signature: row.Signature.Signature(),
	}
}

// fromRAV converts horizon.SignedRAV to ravRow
func fromRAV(sessionID string, rav *horizon.SignedRAV) *ravRow {
	if rav == nil || rav.Message == nil {
		return nil
	}

	return &ravRow{
		SessionID:       sessionID,
		CollectionID:    newCollectionID(rav.Message.CollectionID),
		Payer:           newAddress(rav.Message.Payer),
		ServiceProvider: newAddress(rav.Message.ServiceProvider),
		DataService:     newAddress(rav.Message.DataService),
		TimestampNs:     int64(rav.Message.TimestampNs),
		ValueAggregate:  newGRT(rav.Message.ValueAggregate),
		Metadata:        rav.Message.Metadata,
		Signature:       newSignature(rav.Signature),
		CreatedAt:       time.Now(),
	}
}

// toRepository converts workerRow to repository.Worker
func (row *workerRow) toRepository() *repository.Worker {
	var traceID string
	if row.TraceID.Valid {
		traceID = row.TraceID.String
	}

	return &repository.Worker{
		Key:       row.Key,
		SessionID: row.SessionID,
		Payer:     row.Payer.Address(),
		CreatedAt: row.CreatedAt,
		TraceID:   traceID,
	}
}

// fromWorker converts repository.Worker to workerRow
func fromWorker(worker *repository.Worker) *workerRow {
	var traceID sql.NullString
	if worker.TraceID != "" {
		traceID = sql.NullString{String: worker.TraceID, Valid: true}
	}

	return &workerRow{
		Key:       worker.Key,
		SessionID: worker.SessionID,
		Payer:     newAddress(worker.Payer),
		CreatedAt: worker.CreatedAt,
		TraceID:   traceID,
	}
}

// toRepository converts quotaUsageRow to repository.QuotaUsage
func (row *quotaUsageRow) toRepository() *repository.QuotaUsage {
	return &repository.QuotaUsage{
		Payer:          row.Payer.Address(),
		ActiveSessions: row.ActiveSessions,
		ActiveWorkers:  row.ActiveWorkers,
		LastUpdated:    row.LastUpdated,
	}
}

// toRepository converts usageEventRow to repository.UsageEvent
func (row *usageEventRow) toRepository() *repository.UsageEvent {
	return &repository.UsageEvent{
		Timestamp: row.Timestamp,
		Blocks:    row.Blocks,
		Bytes:     row.Bytes,
		Requests:  row.Requests,
	}
}

// fromUsageEvent converts repository.UsageEvent to usageEventRow
func fromUsageEvent(sessionID string, event *repository.UsageEvent) *usageEventRow {
	return &usageEventRow{
		SessionID: sessionID,
		Timestamp: event.Timestamp,
		Blocks:    event.Blocks,
		Bytes:     event.Bytes,
		Requests:  event.Requests,
	}
}
