package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/graphprotocol/substreams-data-service/horizon/devenv"
	"github.com/streamingfast/logging"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"
)

var (
	// PostgresTestDSN is the connection string for the test Postgres instance
	PostgresTestDSN string

	postgresContainer *postgres.PostgresContainer
)

func init() {
	logging.InstantiateLoggers(logging.WithDefaultLevel(zap.InfoLevel))
}

func TestMain(m *testing.M) {
	ctx := context.Background()

	// Start both devenv and Postgres in parallel
	var wg sync.WaitGroup
	errChan := make(chan error, 2)

	// Start devenv
	wg.Go(func() {
		zlog.Info("starting development environment (anvil + contracts)")
		_, err := devenv.Start(ctx)
		if err != nil {
			errChan <- fmt.Errorf("failed to start devenv: %w", err)
			return
		}
		zlog.Info("development environment started successfully")
	})

	// Start Postgres
	wg.Go(func() {
		zlog.Info("starting PostgreSQL test container")
		var err error
		postgresContainer, err = postgres.Run(ctx,
			"postgres:18-alpine",
			postgres.WithDatabase("sds_test"),
			postgres.WithUsername("testuser"),
			postgres.WithPassword("testpass"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(90*time.Second),
			),
		)
		if err != nil {
			errChan <- fmt.Errorf("failed to start PostgreSQL container: %w", err)
			return
		}

		dsn, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			errChan <- fmt.Errorf("failed to get PostgreSQL connection string: %w", err)
			return
		}

		// Run migrations (golang-migrate expects postgres:// scheme)
		migrationDSN := strings.Replace(dsn, "postgresql://", "postgres://", 1)
		zlog.Info("running database migrations", zap.String("dsn", migrationDSN))
		if err := runMigrations(migrationDSN); err != nil {
			errChan <- fmt.Errorf("failed to run migrations: %w", err)
			return
		}
		zlog.Info("database migrations completed successfully")

		// Convert to psql:// scheme for repository compatibility (handles both postgres:// and postgresql://)
		PostgresTestDSN = strings.Replace(dsn, "postgresql://", "psql://", 1)
		PostgresTestDSN = strings.Replace(PostgresTestDSN, "postgres://", "psql://", 1)
		zlog.Info("PostgreSQL test container started successfully", zap.String("dsn", PostgresTestDSN))
	})

	// Wait for both to complete
	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		fmt.Fprintf(os.Stderr, "Setup error: %v\n", err)
		cleanup(ctx)
		os.Exit(1)
	}

	code := m.Run()
	cleanup(ctx)

	os.Exit(code)
}

func cleanup(ctx context.Context) {
	zlog.Info("cleaning up test resources")

	if postgresContainer != nil {
		if err := postgresContainer.Terminate(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to terminate PostgreSQL container: %v\n", err)
		}
	}

	devenv.Shutdown()
}

// sanitizeDSN removes sensitive information from DSN for logging
func sanitizeDSN(dsn string) string {
	// Simple sanitization - in real implementation you might want more robust parsing
	return "postgres://testuser:***@localhost/sds_test"
}

// runMigrations runs database migrations using golang-migrate
func runMigrations(dsn string) error {
	// Get the absolute path to the migrations directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Navigate up to the repository root (from test/integration to .)
	migrationsPath := filepath.Join(cwd, "..", "..", "provider", "repository", "psql", "migrations")
	absPath, err := filepath.Abs(migrationsPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for migrations: %w", err)
	}

	// Use golang-migrate to run migrations
	cmd := exec.Command("go", "run", "-tags", "postgres",
		"github.com/golang-migrate/migrate/v4/cmd/migrate@latest",
		"-database", dsn,
		"-path", absPath,
		"up")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("migration failed: %w\nOutput: %s", err, string(output))
	}

	zlog.Debug("migration output", zap.String("output", string(output)))
	return nil
}
