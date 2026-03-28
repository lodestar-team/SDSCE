package psql

import (
	"bytes"
	"context"

	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
)

func init() {
	registerFiles([]string{
		"session/apply_usage.sql",
		"session/create.sql",
		"session/get.sql",
		"session/update.sql",
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
	_, err := execOne[sessionRow](ctx, r, "session/create.sql", params)
	if err != nil {
		return err
	}

	// If session has a RAV, insert it
	if session.CurrentRAV != nil {
		ravRw := fromRAV(session.ID, session.CurrentRAV)

		collectionIDVal := mustValue(ravRw.CollectionID)
		ravPayerVal := mustValue(ravRw.Payer)
		serviceProviderVal := mustValue(ravRw.ServiceProvider)
		ravDataServiceVal := mustValue(ravRw.DataService)
		valueAggregateVal := mustValue(ravRw.ValueAggregate)
		signatureVal := mustValue(ravRw.Signature)

		_, err = execOne[ravRow](ctx, r, "session/upsert_rav.sql", map[string]any{
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

	// Update session
	_, err := execOne[sessionRow](ctx, r, "session/update.sql", map[string]any{
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
	})
	if err != nil {
		return err
	}

	// Upsert RAV if present
	if session.CurrentRAV != nil {
		ravRw := fromRAV(session.ID, session.CurrentRAV)

		collectionIDVal := mustValue(ravRw.CollectionID)
		ravPayerVal := mustValue(ravRw.Payer)
		serviceProviderVal := mustValue(ravRw.ServiceProvider)
		ravDataServiceVal := mustValue(ravRw.DataService)
		valueAggregateVal := mustValue(ravRw.ValueAggregate)
		signatureVal := mustValue(ravRw.Signature)

		_, err = execOne[ravRow](ctx, r, "session/upsert_rav.sql", map[string]any{
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
