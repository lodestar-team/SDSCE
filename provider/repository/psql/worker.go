package psql

import (
	"context"
	"fmt"
	"strings"

	"github.com/graphprotocol/substreams-data-service/provider/repository"
)

func init() {
	registerFiles([]string{
		"worker/create.sql",
		"worker/get.sql",
		"worker/count_by_session.sql",
		"worker/delete.sql",
	})
}

// WorkerCreate creates a new worker
func (r *Database) WorkerCreate(ctx context.Context, worker *repository.Worker) error {
	row := fromWorker(worker)

	// Convert custom types to their database values
	payerVal := mustValue(row.Payer)

	_, err := execOne[workerRow](ctx, r, "worker/create.sql", map[string]any{
		"key":        row.Key,
		"session_id": row.SessionID,
		"payer":      payerVal,
		"created_at": row.CreatedAt,
		"trace_id":   row.TraceID,
	})

	return err
}

// WorkerCreateAndReserveQuota atomically creates a worker and reserves worker quota for its payer.
func (r *Database) WorkerCreateAndReserveQuota(ctx context.Context, worker *repository.Worker, maxWorkers int) (quota *repository.QuotaUsage, err error) {
	if worker == nil {
		return nil, fmt.Errorf("worker must not be nil")
	}
	if worker.Key == "" {
		return nil, fmt.Errorf("worker key must not be empty")
	}
	if maxWorkers < 0 {
		return nil, fmt.Errorf("max workers must not be negative")
	}

	payerAddr := newAddress(worker.Payer)
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

	if current.ActiveWorkers+1 > maxWorkers {
		return current.toRepository(), repository.ErrQuotaExceeded
	}

	workerRow := fromWorker(worker)
	payerWorkerVal := mustValue(workerRow.Payer)
	workerRows, err := bindAndQueryxContext(ctx, tx, onDiskStatement("worker/create.sql"), map[string]any{
		"key":        workerRow.Key,
		"session_id": workerRow.SessionID,
		"payer":      payerWorkerVal,
		"created_at": workerRow.CreatedAt,
		"trace_id":   workerRow.TraceID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed %s: %w", strings.ReplaceAll("worker/create.sql", "_", " "), err)
	}
	var workerFound bool
	if workerRows.Next() {
		workerFound = true
	}
	if err := workerRows.Err(); err != nil {
		_ = workerRows.Close()
		return nil, fmt.Errorf("scan worker create rows: %w", err)
	}
	if closeErr := workerRows.Close(); closeErr != nil {
		return nil, fmt.Errorf("close worker create rows: %w", closeErr)
	}
	if !workerFound {
		return nil, fmt.Errorf("worker create returned no row")
	}

	updatedRows, err := bindAndQueryxContext(ctx, tx, onDiskStatement("quota/increment.sql"), map[string]any{
		"payer":    payerVal,
		"sessions": 0,
		"workers":  1,
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
		return nil, fmt.Errorf("commit worker reservation transaction: %w", err)
	}

	return updated.toRepository(), nil
}

// WorkerGet retrieves a worker by key
func (r *Database) WorkerGet(ctx context.Context, workerKey string) (*repository.Worker, error) {
	row, err := getOne[workerRow](ctx, r, "worker/get.sql", map[string]any{
		"key": workerKey,
	})
	if err != nil {
		return nil, err
	}

	return row.toRepository(), nil
}

// WorkerCountBySession returns the number of active worker rows for a session.
func (r *Database) WorkerCountBySession(ctx context.Context, sessionID string) (int, error) {
	count, err := getOne[int](ctx, r, "worker/count_by_session.sql", map[string]any{
		"session_id": sessionID,
	})
	if err != nil {
		return 0, err
	}
	return *count, nil
}

// WorkerDelete deletes a worker by key
func (r *Database) WorkerDelete(ctx context.Context, workerKey string) error {
	return execSimple(ctx, r, "worker/delete.sql", map[string]any{
		"key": workerKey,
	})
}
