package psql

import (
	"context"

	"github.com/graphprotocol/substreams-data-service/provider/repository"
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
