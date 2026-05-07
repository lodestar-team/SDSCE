package psql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/jmoiron/sqlx"
)

func init() {
	registerFiles([]string{
		"collection/get.sql",
		"collection/list.sql",
		"collection/mark_collected.sql",
		"collection/mark_failed_retryable.sql",
		"collection/mark_pending.sql",
		"collection/upsert_collectible.sql",
	})
}

func (r *Database) CollectionCreateOrUpdateCollectible(ctx context.Context, sessionID string, rav *horizon.SignedRAV) (*repository.CollectionRecord, error) {
	params, key, err := collectionCollectibleParams(sessionID, rav)
	if err != nil {
		return nil, err
	}

	row, err := execOne[collectionRecordRow](ctx, r, "collection/upsert_collectible.sql", params)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			existing, getErr := r.CollectionGet(ctx, key)
			if getErr != nil {
				return nil, getErr
			}
			if existing.State == repository.CollectionStateCollectPending && !collectionBigIntsEqual(existing.ValueAggregate, rav.Message.ValueAggregate) {
				return nil, repository.ErrCollectionConflict
			}
			if existing.State == repository.CollectionStateCollected && existing.ValueAggregate.Cmp(rav.Message.ValueAggregate) > 0 {
				return nil, repository.ErrCollectionConflict
			}
			return existing, nil
		}
		return nil, err
	}

	return row.toRepository(), nil
}

func (r *Database) collectionCreateOrUpdateCollectibleTx(ctx context.Context, tx *sqlx.Tx, sessionID string, rav *horizon.SignedRAV) (*repository.CollectionRecord, error) {
	params, key, err := collectionCollectibleParams(sessionID, rav)
	if err != nil {
		return nil, err
	}

	rows, err := bindAndQueryxContext(ctx, tx, onDiskStatement("collection/upsert_collectible.sql"), params)
	if err != nil {
		return nil, fmt.Errorf("failed %s: %w", strings.ReplaceAll("collection/upsert_collectible.sql", "_", " "), err)
	}
	var row collectionRecordRow
	var found bool
	if rows.Next() {
		found = true
		if err := rows.StructScan(&row); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan collection upsert row: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("scan collection upsert row: %w", err)
	}
	if closeErr := rows.Close(); closeErr != nil {
		return nil, fmt.Errorf("close collection upsert rows: %w", closeErr)
	}
	if found {
		return row.toRepository(), nil
	}

	existing, err := collectionGetTx(ctx, tx, key)
	if err != nil {
		return nil, err
	}
	if existing.State == repository.CollectionStateCollectPending && !collectionBigIntsEqual(existing.ValueAggregate, rav.Message.ValueAggregate) {
		return nil, repository.ErrCollectionConflict
	}
	if existing.State == repository.CollectionStateCollected && existing.ValueAggregate.Cmp(rav.Message.ValueAggregate) > 0 {
		return nil, repository.ErrCollectionConflict
	}
	return existing, nil
}

func (r *Database) CollectionGet(ctx context.Context, key repository.CollectionKey) (*repository.CollectionRecord, error) {
	row, err := getOne[collectionRecordRow](ctx, r, "collection/get.sql", collectionKeyParams(key))
	if err != nil {
		return nil, err
	}
	return row.toRepository(), nil
}

func (r *Database) CollectionList(ctx context.Context, filter repository.CollectionFilter) ([]*repository.CollectionRecord, error) {
	rows, err := getMany[collectionRecordRow](ctx, r, "collection/list.sql", map[string]any{})
	if err != nil {
		return nil, err
	}

	records := make([]*repository.CollectionRecord, 0, len(rows))
	for _, row := range rows {
		record := row.toRepository()
		if filter.SessionID != nil && record.Key.SessionID != *filter.SessionID {
			continue
		}
		if filter.State != nil && record.State != *filter.State {
			continue
		}
		if filter.Payer != nil && record.Key.Payer.Pretty() != filter.Payer.Pretty() {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

func (r *Database) CollectionMarkPending(ctx context.Context, key repository.CollectionKey, expectedValue *big.Int, txHash string, updatedAt time.Time) (*repository.CollectionRecord, error) {
	params := collectionKeyParams(key)
	params["expected_value"] = mustValue(newGRT(expectedValue))
	params["last_tx_hash"] = nullableString(txHash)
	params["updated_at"] = collectionUpdatedAt(updatedAt)
	return r.execCollectionTransition(ctx, "collection/mark_pending.sql", key, expectedValue, params)
}

func (r *Database) CollectionMarkCollected(ctx context.Context, key repository.CollectionKey, expectedValue *big.Int, txHash string, collectedAmount *big.Int, updatedAt time.Time) (*repository.CollectionRecord, error) {
	params := collectionKeyParams(key)
	params["expected_value"] = mustValue(newGRT(expectedValue))
	params["last_tx_hash"] = nullableString(txHash)
	params["collected_amount"] = nullableGRT(collectedAmount)
	params["updated_at"] = collectionUpdatedAt(updatedAt)
	return r.execCollectionTransition(ctx, "collection/mark_collected.sql", key, expectedValue, params)
}

func (r *Database) CollectionMarkFailedRetryable(ctx context.Context, key repository.CollectionKey, expectedValue *big.Int, txHash string, lastError string, updatedAt time.Time) (*repository.CollectionRecord, error) {
	params := collectionKeyParams(key)
	params["expected_value"] = mustValue(newGRT(expectedValue))
	params["last_tx_hash"] = nullableString(txHash)
	params["last_error"] = nullableString(lastError)
	params["updated_at"] = collectionUpdatedAt(updatedAt)
	return r.execCollectionTransition(ctx, "collection/mark_failed_retryable.sql", key, expectedValue, params)
}

func (r *Database) execCollectionTransition(ctx context.Context, statement string, key repository.CollectionKey, expectedValue *big.Int, params map[string]any) (*repository.CollectionRecord, error) {
	row, err := execOne[collectionRecordRow](ctx, r, statement, params)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, r.classifyCollectionTransitionFailure(ctx, key, expectedValue)
		}
		return nil, err
	}
	return row.toRepository(), nil
}

func (r *Database) classifyCollectionTransitionFailure(ctx context.Context, key repository.CollectionKey, expectedValue *big.Int) error {
	current, err := r.CollectionGet(ctx, key)
	if err != nil {
		return err
	}
	if !collectionBigIntsEqual(current.ValueAggregate, expectedValue) {
		return repository.ErrCollectionConflict
	}
	return repository.ErrInvalidCollectionTransition
}

func collectionKeyParams(key repository.CollectionKey) map[string]any {
	return map[string]any{
		"session_id":       key.SessionID,
		"collection_id":    mustValue(newCollectionID(key.CollectionID)),
		"payer":            mustValue(newAddress(key.Payer)),
		"service_provider": mustValue(newAddress(key.ServiceProvider)),
		"data_service":     mustValue(newAddress(key.DataService)),
	}
}

func collectionCollectibleParams(sessionID string, rav *horizon.SignedRAV) (map[string]any, repository.CollectionKey, error) {
	if sessionID == "" {
		return nil, repository.CollectionKey{}, fmt.Errorf("session ID must not be empty")
	}
	if rav == nil || rav.Message == nil {
		return nil, repository.CollectionKey{}, fmt.Errorf("signed RAV must not be nil")
	}
	if rav.Message.ValueAggregate == nil {
		return nil, repository.CollectionKey{}, fmt.Errorf("signed RAV value aggregate must not be nil")
	}

	key := repository.CollectionKey{
		SessionID:       sessionID,
		CollectionID:    rav.Message.CollectionID,
		Payer:           rav.Message.Payer,
		ServiceProvider: rav.Message.ServiceProvider,
		DataService:     rav.Message.DataService,
	}
	now := time.Now()
	params := collectionKeyParams(key)
	params["rav_timestamp_ns"] = int64(rav.Message.TimestampNs)
	params["value_aggregate"] = mustValue(newGRT(rav.Message.ValueAggregate))
	params["rav_metadata"] = rav.Message.Metadata
	params["rav_signature"] = mustValue(newSignature(rav.Signature))
	params["created_at"] = now
	params["updated_at"] = now
	return params, key, nil
}

func collectionGetTx(ctx context.Context, tx *sqlx.Tx, key repository.CollectionKey) (*repository.CollectionRecord, error) {
	rows, err := bindAndQueryxContext(ctx, tx, onDiskStatement("collection/get.sql"), collectionKeyParams(key))
	if err != nil {
		return nil, fmt.Errorf("failed %s: %w", strings.ReplaceAll("collection/get.sql", "_", " "), err)
	}
	var row collectionRecordRow
	var found bool
	if rows.Next() {
		found = true
		if err := rows.StructScan(&row); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan collection get row: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("scan collection get row: %w", err)
	}
	if closeErr := rows.Close(); closeErr != nil {
		return nil, fmt.Errorf("close collection get rows: %w", closeErr)
	}
	if !found {
		return nil, repository.ErrNotFound
	}
	return row.toRepository(), nil
}

func nullableString(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func nullableGRT(value *big.Int) any {
	if value == nil {
		return nil
	}
	return mustValue(newGRT(value))
}

func collectionBigIntsEqual(a, b *big.Int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Cmp(b) == 0
}

func collectionUpdatedAt(updatedAt time.Time) time.Time {
	if updatedAt.IsZero() {
		return time.Now()
	}
	return updatedAt
}
