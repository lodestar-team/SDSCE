package psql

import (
	"context"
	"fmt"

	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/streamingfast/eth-go"
)

func init() {
	registerFiles([]string{
		"quota/get_for_update.sql",
		"quota/get.sql",
		"quota/increment.sql",
		"quota/decrement.sql",
	})
}

// QuotaGet retrieves quota usage for a payer
func (r *Database) QuotaGet(ctx context.Context, payer eth.Address) (*repository.QuotaUsage, error) {
	payerAddr := newAddress(payer)
	payerVal := mustValue(payerAddr)

	row, err := getOne[quotaUsageRow](ctx, r, "quota/get.sql", map[string]any{
		"payer": payerVal,
	})
	if err != nil {
		if err == repository.ErrNotFound {
			// Return zero usage if not found
			return &repository.QuotaUsage{
				Payer:          payer,
				ActiveSessions: 0,
				ActiveWorkers:  0,
			}, nil
		}
		return nil, err
	}

	return row.toRepository(), nil
}

// QuotaReserve atomically reserves worker quota for a payer.
func (r *Database) QuotaReserve(ctx context.Context, payer eth.Address, maxWorkers int, workers int) (quota *repository.QuotaUsage, err error) {
	if workers <= 0 {
		return nil, fmt.Errorf("workers must be positive")
	}
	if maxWorkers < 0 {
		return nil, fmt.Errorf("max workers must not be negative")
	}

	payerAddr := newAddress(payer)
	payerVal := mustValue(payerAddr)

	tx, err := r.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.NamedExecContext(ctx, `
		INSERT INTO quota_usage (payer, active_sessions, active_workers, last_updated)
		VALUES (:payer, 0, 0, CURRENT_TIMESTAMP)
		ON CONFLICT (payer) DO NOTHING
	`, map[string]any{
		"payer": payerVal,
	}); err != nil {
		return nil, fmt.Errorf("ensure quota row: %w", err)
	}

	currentRows, err := bindAndQueryxContext(ctx, tx, onDiskStatement("quota/get_for_update.sql"), map[string]any{
		"payer": payerVal,
	})
	if err != nil {
		return nil, err
	}
	var current quotaUsageRow
	var currentFound bool
	if currentRows.Next() {
		currentFound = true
		if err := currentRows.StructScan(&current); err != nil {
			_ = currentRows.Close()
			return nil, fmt.Errorf("scan quota row: %w", err)
		}
	}
	if err := currentRows.Err(); err != nil {
		_ = currentRows.Close()
		return nil, fmt.Errorf("scan quota row: %w", err)
	}
	if closeErr := currentRows.Close(); closeErr != nil {
		return nil, fmt.Errorf("close quota row result: %w", closeErr)
	}
	if !currentFound {
		return nil, fmt.Errorf("quota row missing after ensure")
	}

	if current.ActiveWorkers+workers > maxWorkers {
		return current.toRepository(), repository.ErrQuotaExceeded
	}

	updatedRows, err := bindAndQueryxContext(ctx, tx, onDiskStatement("quota/increment.sql"), map[string]any{
		"payer":    payerVal,
		"sessions": 0,
		"workers":  workers,
	})
	if err != nil {
		return nil, err
	}
	var updated quotaUsageRow
	var updatedFound bool
	if updatedRows.Next() {
		updatedFound = true
		if err := updatedRows.StructScan(&updated); err != nil {
			_ = updatedRows.Close()
			return nil, fmt.Errorf("scan updated quota row: %w", err)
		}
	}
	if err := updatedRows.Err(); err != nil {
		_ = updatedRows.Close()
		return nil, fmt.Errorf("scan updated quota row: %w", err)
	}
	if closeErr := updatedRows.Close(); closeErr != nil {
		return nil, fmt.Errorf("close updated quota row result: %w", closeErr)
	}
	if !updatedFound {
		return nil, fmt.Errorf("quota reservation update returned no row")
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit quota reservation transaction: %w", err)
	}

	return updated.toRepository(), nil
}

// QuotaIncrement increments quota usage for a payer
func (r *Database) QuotaIncrement(ctx context.Context, payer eth.Address, sessions int, workers int) error {
	payerAddr := newAddress(payer)
	payerVal := mustValue(payerAddr)

	_, err := execOne[quotaUsageRow](ctx, r, "quota/increment.sql", map[string]any{
		"payer":    payerVal,
		"sessions": sessions,
		"workers":  workers,
	})

	return err
}

// QuotaDecrement decrements quota usage for a payer
func (r *Database) QuotaDecrement(ctx context.Context, payer eth.Address, sessions int, workers int) error {
	payerAddr := newAddress(payer)
	payerVal := mustValue(payerAddr)

	_, err := execOne[quotaUsageRow](ctx, r, "quota/decrement.sql", map[string]any{
		"payer":    payerVal,
		"sessions": sessions,
		"workers":  workers,
	})

	// If no rows affected (payer doesn't exist), that's OK - just ignore
	if err == repository.ErrNotFound {
		return nil
	}

	return err
}
