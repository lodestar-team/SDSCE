package psql

import (
	"bytes"
	"context"
	"database/sql"
	"math/big"
	"time"

	"github.com/graphprotocol/substreams-data-service/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/jmoiron/sqlx"
)

func init() {
	registerFiles([]string{
		"session/apply_usage.sql",
		"session/create.sql",
		"session/get.sql",
		"session/update.sql",
		"session/touch.sql",
		"session/update_runtime_state.sql",
		"session/update_rav_baseline.sql",
		"session/list.sql",
		"session/count.sql",
		"session/get_rav.sql",
		"session/upsert_rav.sql",
	})
}

// SessionCreate creates a new session
func (r *Database) SessionCreate(ctx context.Context, session *repository.Session) error {
	row := fromSession(session)

	// Convert custom types to their database values
	payerVal := mustValue(row.Payer)
	receiverVal := mustValue(row.Receiver)
	dataServiceVal := mustValue(row.DataService)
	signerVal := mustValue(row.Signer)
	totalCostVal := mustValue(row.TotalCost)
	baselineCostVal := mustValue(row.BaselineCost)
	metadataVal := mustValue(row.Metadata)

	// Insert session
	params := map[string]any{
		"id":                row.ID,
		"created_at":        row.CreatedAt,
		"updated_at":        row.UpdatedAt,
		"last_keep_alive":   row.LastKeepAlive,
		"status":            row.Status,
		"metadata":          metadataVal,
		"ended_at":          row.EndedAt,
		"end_reason":        row.EndReason,
		"payer":             payerVal,
		"receiver":          receiverVal,
		"data_service":      dataServiceVal,
		"signer":            signerVal,
		"blocks_processed":  row.BlocksProcessed,
		"bytes_transferred": row.BytesTransferred,
		"requests":          row.Requests,
		"total_cost":        totalCostVal,
		"baseline_blocks":   row.BaselineBlocks,
		"baseline_bytes":    row.BaselineBytes,
		"baseline_reqs":     row.BaselineReqs,
		"baseline_cost":     baselineCostVal,
	}
	if session.CurrentRAV != nil {
		return r.sessionCreateWithRAVTx(ctx, session, params)
	}
	_, err := execOne[sessionRow](ctx, r, "session/create.sql", params)
	if err != nil {
		return err
	}

	return nil
}

// SessionGet retrieves a session by ID
func (r *Database) SessionGet(ctx context.Context, sessionID string) (*repository.Session, error) {
	// Get session
	row, err := getOne[sessionRow](ctx, r, "session/get.sql", map[string]any{
		"id": sessionID,
	})
	if err != nil {
		return nil, err
	}

	// Get RAV if exists
	var rav *horizon.SignedRAV
	ravRow, err := getOne[ravRow](ctx, r, "session/get_rav.sql", map[string]any{
		"session_id": sessionID,
	})
	if err != nil && err != repository.ErrNotFound {
		return nil, err
	}
	if ravRow != nil {
		rav = ravRow.toRepository()
	}

	// Note: PricingConfig must be provided by the caller after retrieval
	// We use an empty PricingConfig as a placeholder
	return row.toRepository(rav, repository.PricingConfig{}), nil
}

// SessionUpdate updates an existing session
func (r *Database) SessionUpdate(ctx context.Context, session *repository.Session) error {
	row := fromSession(session)

	// Convert custom types to their database values
	payerVal := mustValue(row.Payer)
	receiverVal := mustValue(row.Receiver)
	dataServiceVal := mustValue(row.DataService)
	signerVal := mustValue(row.Signer)
	totalCostVal := mustValue(row.TotalCost)
	baselineCostVal := mustValue(row.BaselineCost)
	metadataVal := mustValue(row.Metadata)

	params := map[string]any{
		"id":                row.ID,
		"created_at":        row.CreatedAt,
		"updated_at":        row.UpdatedAt,
		"last_keep_alive":   row.LastKeepAlive,
		"status":            row.Status,
		"metadata":          metadataVal,
		"ended_at":          row.EndedAt,
		"end_reason":        row.EndReason,
		"payer":             payerVal,
		"receiver":          receiverVal,
		"data_service":      dataServiceVal,
		"signer":            signerVal,
		"blocks_processed":  row.BlocksProcessed,
		"bytes_transferred": row.BytesTransferred,
		"requests":          row.Requests,
		"total_cost":        totalCostVal,
		"baseline_blocks":   row.BaselineBlocks,
		"baseline_bytes":    row.BaselineBytes,
		"baseline_reqs":     row.BaselineReqs,
		"baseline_cost":     baselineCostVal,
	}
	if session.CurrentRAV != nil {
		return r.sessionUpdateWithRAVTx(ctx, session, params)
	}

	// Update session
	_, err := execOne[sessionRow](ctx, r, "session/update.sql", params)
	if err != nil {
		return err
	}

	return nil
}

func (r *Database) sessionCreateWithRAVTx(ctx context.Context, session *repository.Session, sessionParams map[string]any) (err error) {
	tx, err := r.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if err := execTxRows(ctx, tx, "session/create.sql", sessionParams); err != nil {
		return err
	}
	if err := upsertRAVTx(ctx, tx, session.ID, session.CurrentRAV); err != nil {
		return err
	}
	if _, err := r.collectionCreateOrUpdateCollectibleTx(ctx, tx, session.ID, session.CurrentRAV); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Database) sessionUpdateWithRAVTx(ctx context.Context, session *repository.Session, sessionParams map[string]any) (err error) {
	tx, err := r.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if err := execTxRows(ctx, tx, "session/update.sql", sessionParams); err != nil {
		return err
	}
	if err := upsertRAVTx(ctx, tx, session.ID, session.CurrentRAV); err != nil {
		return err
	}
	if _, err := r.collectionCreateOrUpdateCollectibleTx(ctx, tx, session.ID, session.CurrentRAV); err != nil {
		return err
	}
	return tx.Commit()
}

func upsertRAVTx(ctx context.Context, tx *sqlx.Tx, sessionID string, rav *horizon.SignedRAV) error {
	ravRw := fromRAV(sessionID, rav)
	collectionIDVal := mustValue(ravRw.CollectionID)
	ravPayerVal := mustValue(ravRw.Payer)
	serviceProviderVal := mustValue(ravRw.ServiceProvider)
	ravDataServiceVal := mustValue(ravRw.DataService)
	valueAggregateVal := mustValue(ravRw.ValueAggregate)
	signatureVal := mustValue(ravRw.Signature)

	return execTxRows(ctx, tx, "session/upsert_rav.sql", map[string]any{
		"session_id":       ravRw.SessionID,
		"collection_id":    collectionIDVal,
		"payer":            ravPayerVal,
		"service_provider": serviceProviderVal,
		"data_service":     ravDataServiceVal,
		"timestamp_ns":     ravRw.TimestampNs,
		"value_aggregate":  valueAggregateVal,
		"metadata":         ravRw.Metadata,
		"signature":        signatureVal,
		"created_at":       ravRw.CreatedAt,
	})
}

func execTxRows(ctx context.Context, tx *sqlx.Tx, statement string, params map[string]any) error {
	rows, err := bindAndQueryxContext(ctx, tx, onDiskStatement(statement), params)
	if err != nil {
		return err
	}
	var found bool
	for rows.Next() {
		found = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if closeErr := rows.Close(); closeErr != nil {
		return closeErr
	}
	if !found {
		return repository.ErrNotFound
	}
	return nil
}

// SessionTouch updates only the keep-alive timestamp, preserving all usage aggregates.
func (r *Database) SessionTouch(ctx context.Context, sessionID string, lastKeepAlive time.Time) error {
	type idRow struct {
		ID string `db:"id"`
	}

	row, err := execOne[idRow](ctx, r, "session/touch.sql", map[string]any{
		"id":              sessionID,
		"updated_at":      lastKeepAlive,
		"last_keep_alive": lastKeepAlive,
	})
	if err != nil {
		return err
	}
	if row == nil {
		return repository.ErrNotFound
	}

	return nil
}

// SessionUpdateRuntimeState updates only lifecycle and runtime metadata fields, preserving usage aggregates.
func (r *Database) SessionUpdateRuntimeState(ctx context.Context, sessionID string, status repository.SessionStatus, metadata map[string]string, endedAt *time.Time, endReason commonv1.EndReason, updatedAt time.Time) error {
	metadataVal := mustValue(jsonbMap(metadata))

	var endedAtVal sql.NullTime
	if endedAt != nil {
		endedAtVal = sql.NullTime{Time: *endedAt, Valid: true}
	}

	var endReasonVal sql.NullInt32
	if endReason != commonv1.EndReason_END_REASON_UNSPECIFIED {
		endReasonVal = sql.NullInt32{Int32: int32(endReason), Valid: true}
	}

	type idRow struct {
		ID string `db:"id"`
	}

	row, err := execOne[idRow](ctx, r, "session/update_runtime_state.sql", map[string]any{
		"id":         sessionID,
		"updated_at": updatedAt,
		"status":     string(status),
		"metadata":   metadataVal,
		"ended_at":   endedAtVal,
		"end_reason": endReasonVal,
	})
	if err != nil {
		return err
	}
	if row == nil {
		return repository.ErrNotFound
	}

	return nil
}

// SessionUpdateRAVAndBaseline updates only the accepted RAV and baseline snapshot, preserving current usage aggregates.
func (r *Database) SessionUpdateRAVAndBaseline(ctx context.Context, sessionID string, currentRAV *horizon.SignedRAV, baselineBlocks, baselineBytes, baselineReqs uint64, baselineCost *big.Int) (err error) {
	baselineCostVal := mustValue(newGRT(baselineCost))

	tx, err := r.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	sessionRows, err := bindAndQueryxContext(ctx, tx, onDiskStatement("session/update_rav_baseline.sql"), map[string]any{
		"id":              sessionID,
		"updated_at":      time.Now(),
		"baseline_blocks": int64(baselineBlocks),
		"baseline_bytes":  int64(baselineBytes),
		"baseline_reqs":   int64(baselineReqs),
		"baseline_cost":   baselineCostVal,
	})
	if err != nil {
		return err
	}

	var sessionUpdated bool
	for sessionRows.Next() {
		sessionUpdated = true
	}
	if err := sessionRows.Err(); err != nil {
		_ = sessionRows.Close()
		return err
	}
	if closeErr := sessionRows.Close(); closeErr != nil {
		return closeErr
	}
	if !sessionUpdated {
		return repository.ErrNotFound
	}

	if currentRAV != nil {
		ravRw := fromRAV(sessionID, currentRAV)

		collectionIDVal := mustValue(ravRw.CollectionID)
		ravPayerVal := mustValue(ravRw.Payer)
		serviceProviderVal := mustValue(ravRw.ServiceProvider)
		ravDataServiceVal := mustValue(ravRw.DataService)
		valueAggregateVal := mustValue(ravRw.ValueAggregate)
		signatureVal := mustValue(ravRw.Signature)

		ravRows, err := bindAndQueryxContext(ctx, tx, onDiskStatement("session/upsert_rav.sql"), map[string]any{
			"session_id":       ravRw.SessionID,
			"collection_id":    collectionIDVal,
			"payer":            ravPayerVal,
			"service_provider": serviceProviderVal,
			"data_service":     ravDataServiceVal,
			"timestamp_ns":     ravRw.TimestampNs,
			"value_aggregate":  valueAggregateVal,
			"metadata":         ravRw.Metadata,
			"signature":        signatureVal,
			"created_at":       ravRw.CreatedAt,
		})
		if err != nil {
			return err
		}
		for ravRows.Next() {
		}
		if err := ravRows.Err(); err != nil {
			_ = ravRows.Close()
			return err
		}
		if closeErr := ravRows.Close(); closeErr != nil {
			return closeErr
		}

		if _, err := r.collectionCreateOrUpdateCollectibleTx(ctx, tx, sessionID, currentRAV); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

// SessionList lists sessions with optional filters
func (r *Database) SessionList(ctx context.Context, filter repository.SessionFilter) ([]*repository.Session, error) {
	rows, err := getMany[sessionRow](ctx, r, "session/list.sql", map[string]any{})
	if err != nil {
		return nil, err
	}

	sessions := make([]*repository.Session, 0, len(rows))
	for _, row := range rows {
		// Apply filters
		if filter.Payer != nil {
			if !bytes.Equal(row.Payer.Address(), *filter.Payer) {
				continue
			}
		}
		if filter.Status != nil && row.Status != string(*filter.Status) {
			continue
		}
		if filter.CreatedAfter != nil && row.CreatedAt.Before(*filter.CreatedAfter) {
			continue
		}

		// Get RAV for each session
		var rav *horizon.SignedRAV
		ravRow, err := getOne[ravRow](ctx, r, "session/get_rav.sql", map[string]any{
			"session_id": row.ID,
		})
		if err != nil && err != repository.ErrNotFound {
			return nil, err
		}
		if ravRow != nil {
			rav = ravRow.toRepository()
		}

		sessions = append(sessions, row.toRepository(rav, repository.PricingConfig{}))
	}

	return sessions, nil
}

// SessionCount returns the total number of sessions
func (r *Database) SessionCount(ctx context.Context) int {
	type countResult struct {
		Count int `db:"count"`
	}

	result, err := getOne[countResult](ctx, r, "session/count.sql", map[string]any{})
	if err != nil {
		// Return 0 on error (matches InMemoryRepository behavior)
		return 0
	}

	return result.Count
}
