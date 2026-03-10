package psql

import (
	"context"

	"github.com/graphprotocol/substreams-data-service/provider/repository"
)

func init() {
	registerFiles([]string{
		"worker/create.sql",
		"worker/get.sql",
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

// WorkerDelete deletes a worker by key
func (r *Database) WorkerDelete(ctx context.Context, workerKey string) error {
	return execSimple(ctx, r, "worker/delete.sql", map[string]any{
		"key": workerKey,
	})
}
