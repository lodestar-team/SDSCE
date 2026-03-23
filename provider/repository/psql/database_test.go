package psql

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

// withTestDB sets up test database in isolated schema
func withTestDB(t *testing.T, testFunc func(db *Database)) {
	ctx := context.Background()

	db, err := GetConnectionFromDSN(ctx, postgresTestDSN)
	require.NoError(t, err)
	defer db.Close()

	// Create unique schema for this test
	schemaName := fmt.Sprintf("test_%d", time.Now().UnixNano())
	_, err = db.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA %s", schemaName))
	require.NoError(t, err)
	defer func() {
		_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP SCHEMA %s CASCADE", schemaName))
	}()

	// Run migrations in schema
	runMigrationsInSchema(t, db, schemaName)

	// Set search path AFTER migrations (migrations change it)
	_, err = db.ExecContext(ctx, fmt.Sprintf("SET search_path TO %s", schemaName))
	require.NoError(t, err)

	// Verify search path
	var currentSchema string
	err = db.QueryRowContext(ctx, "SELECT current_schema()").Scan(&currentSchema)
	require.NoError(t, err)
	t.Logf("Current schema after migration: %s", currentSchema)

	// Create repository
	repo := NewRepository(db, zlog)
	require.NoError(t, repo.Setup())

	testFunc(repo)
}

func runMigrationsInSchema(t *testing.T, db *sqlx.DB, schema string) {
	ctx := context.Background()

	// Set search_path BEFORE creating the migrate instance
	_, err := db.ExecContext(ctx, fmt.Sprintf("SET search_path TO %s", schema))
	require.NoError(t, err)

	// Use absolute path to migrations
	migrationPath := "file:///Users/maoueh/work/sf/substreams-data-service/provider/repository/psql/migrations"

	// Don't use SchemaName in config - let it use search_path instead
	dbDriver, err := migratepg.WithInstance(db.DB, &migratepg.Config{
		MigrationsTable: "schema_migrations",
	})
	require.NoError(t, err)

	m, err := migrate.NewWithDatabaseInstance(migrationPath, schema, dbDriver)
	require.NoError(t, err, "Failed to create migrate instance")

	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		t.Logf("Migration error: %v", err)
		require.NoError(t, err, "Failed to run migrations")
	}

	t.Logf("Migrations applied successfully to schema %s", schema)
}

func TestDatabaseConnection(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()

		err := db.Ping(ctx)
		require.NoError(t, err)

		// List all tables in current schema
		var tables []string
		rows, err := db.QueryContext(ctx, `
            SELECT table_name FROM information_schema.tables
            WHERE table_schema = current_schema()
            ORDER BY table_name
        `)
		require.NoError(t, err)
		defer rows.Close()

		for rows.Next() {
			var table string
			err := rows.Scan(&table)
			require.NoError(t, err)
			tables = append(tables, table)
		}

		// Verify we have the expected tables
		require.Contains(t, tables, "sessions", "Expected sessions table to exist")
		require.Contains(t, tables, "ravs", "Expected ravs table to exist")
		require.Contains(t, tables, "workers", "Expected workers table to exist")
		require.Contains(t, tables, "quota_usage", "Expected quota_usage table to exist")
		require.Contains(t, tables, "usage_events", "Expected usage_events table to exist")
	})
}
