package psql

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/jmoiron/sqlx"
)

func init() {
	registerFiles([]string{
		"usage/add.sql",
	})
}

// UsageAdd adds a usage event for a session
func (r *Database) UsageAdd(ctx context.Context, sessionID string, usage *repository.UsageEvent) error {
	row := fromUsageEvent(sessionID, usage)

	_, err := execOne[usageEventRow](ctx, r, "usage/add.sql", map[string]any{
		"session_id": row.SessionID,
		"timestamp":  row.Timestamp,
		"blocks":     row.Blocks,
		"bytes":      row.Bytes,
		"requests":   row.Requests,
	})

	return err
}

// SessionApplyUsage atomically persists a metering event and advances the owning session aggregates.
func (r *Database) SessionApplyUsage(ctx context.Context, sessionID string, usage *repository.UsageEvent, cost *big.Int) (err error) {
	if usage == nil {
		return fmt.Errorf("usage event must not be nil")
	}

	blocks, bytes, requests := usage.SanitizedTotals()
	costVal := mustValue(newGRT(cost))

	tx, err := r.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin usage transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	sessionRows, err := bindAndQueryxContext(ctx, tx, onDiskStatement("session/apply_usage.sql"), map[string]any{
		"id":             sessionID,
		"blocks_delta":   int64(blocks),
		"bytes_delta":    int64(bytes),
		"requests_delta": int64(requests),
		"cost_delta":     costVal,
	})
	if err != nil {
		return fmt.Errorf("failed %s: %w", strings.ReplaceAll("session/apply_usage.sql", "_", " "), err)
	}
	var sessionUpdated bool
	for sessionRows.Next() {
		sessionUpdated = true
	}
	if err := sessionRows.Err(); err != nil {
		_ = sessionRows.Close()
		return fmt.Errorf("scan session apply rows: %w", err)
	}
	if closeErr := sessionRows.Close(); closeErr != nil {
		return fmt.Errorf("close session apply rows: %w", closeErr)
	}
	if !sessionUpdated {
		return repository.ErrNotFound
	}

	usageRows, err := bindAndQueryxContext(ctx, tx, onDiskStatement("usage/add.sql"), map[string]any{
		"session_id": sessionID,
		"timestamp":  usage.Timestamp,
		"blocks":     int64(blocks),
		"bytes":      int64(bytes),
		"requests":   int64(requests),
	})
	if err != nil {
		return fmt.Errorf("failed %s: %w", strings.ReplaceAll("usage/add.sql", "_", " "), err)
	}
	for usageRows.Next() {
	}
	if err := usageRows.Err(); err != nil {
		_ = usageRows.Close()
		return fmt.Errorf("scan usage add rows: %w", err)
	}
	if closeErr := usageRows.Close(); closeErr != nil {
		return fmt.Errorf("close usage add rows: %w", closeErr)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit usage transaction: %w", err)
	}

	return nil
}

func bindAndQueryxContext(ctx context.Context, tx *sqlx.Tx, statement string, args map[string]any) (*sqlx.Rows, error) {
	query, boundArgs, err := sqlx.Named(statement, args)
	if err != nil {
		return nil, err
	}

	query = tx.Rebind(query)
	return tx.QueryxContext(ctx, query, boundArgs...)
}
