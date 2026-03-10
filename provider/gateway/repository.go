package gateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/graphprotocol/substreams-data-service/provider/repository/psql"
	"github.com/streamingfast/derr"
	"go.uber.org/zap"
)

// NewRepositoryFromDSN creates a repository from a DSN string.
// Supported DSN schemes:
//   - inmemory:// - Creates an in-memory repository (no additional configuration)
//   - psql://... - Creates a PostgreSQL repository using the provided connection string
//
// Examples:
//   - inmemory://
//   - psql://user:pass@host:port/dbname?sslmode=disable
func NewRepositoryFromDSN(ctx context.Context, dsn string, logger *zap.Logger) (repository.GlobalRepository, error) {
	if dsn == "" {
		return nil, fmt.Errorf("DSN must not be empty")
	}

	// Parse the scheme
	scheme, rest, found := strings.Cut(dsn, "://")
	if !found {
		return nil, fmt.Errorf("invalid DSN format: missing '://' separator")
	}

	switch scheme {
	case "inmemory":
		logger.Info("creating in-memory repository")
		return repository.NewInMemoryRepository(), nil

	case "psql":
		// Reconstruct the PostgreSQL DSN
		postgresDSN := "postgres://" + rest
		logger.Info("creating PostgreSQL repository", zap.String("dsn", sanitizeDSN(postgresDSN)))

		var repo *psql.Database
		err := derr.RetryContext(ctx, 10, func(ctx context.Context) error {
			// Create database connection
			dbConn, err := psql.GetConnectionFromDSN(ctx, postgresDSN)
			if err != nil {
				logger.Warn("failed to connect to PostgreSQL, retrying", zap.Error(err))
				return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
			}

			// Create repository
			repo = psql.NewRepository(dbConn, logger)

			// Setup prepared statements
			if err := repo.Setup(); err != nil {
				dbConn.Close()
				logger.Warn("failed to setup PostgreSQL repository, retrying", zap.Error(err))
				return fmt.Errorf("failed to setup PostgreSQL repository: %w", err)
			}

			return nil
		})
		if err != nil {
			return nil, err
		}

		return repo, nil

	default:
		return nil, fmt.Errorf("unsupported DSN scheme %q (supported: inmemory, psql)", scheme)
	}
}

// sanitizeDSN removes sensitive information from DSN for logging
func sanitizeDSN(dsn string) string {
	// Hide password in DSN for logging
	if idx := strings.Index(dsn, "@"); idx > 0 {
		if userIdx := strings.Index(dsn, "://"); userIdx >= 0 {
			userInfo := dsn[userIdx+3 : idx]
			if colonIdx := strings.Index(userInfo, ":"); colonIdx >= 0 {
				user := userInfo[:colonIdx]
				return dsn[:userIdx+3] + user + ":***@" + dsn[idx+1:]
			}
		}
	}
	return dsn
}
