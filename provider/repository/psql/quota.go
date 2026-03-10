package psql

import (
	"context"

	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/streamingfast/eth-go"
)

func init() {
	registerFiles([]string{
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
