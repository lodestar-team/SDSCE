# Implementation Plan: PostgreSQL Repository

This document outlines the step-by-step implementation plan for the PostgreSQL-based GlobalRepository.

## Implementation Strategy

**Incremental approach:** Start with smallest working "shell", then gradually add features. Break down into 7 phases with small, manageable tasks.

---

## Phase 1: Infrastructure Setup

### Task 1.1: Docker Compose Setup
**Goal:** Set up local PostgreSQL environment for development

**Steps:**
1. Copy `resources/sf-saas-priv/docker-compose.yml` to project root
2. Remove `big-query-emulator` service
3. Update service names: `postgres-portal` → `postgres-sds`, `pgweb-portal` → `pgweb-sds`, `redis-portal` → `redis-sds`
4. Update PostgreSQL version to **18**
5. Update password to `changeme`
6. Replace volume bind mount with Docker volume:
   ```yaml
   volumes:
     - postgres-data:/var/lib/postgresql/data
   # At bottom of file:
   volumes:
     postgres-data:
   ```
7. Remove `./devel/db/initialize-scripts` volume
8. Test: `docker compose up -d`

**Acceptance Criteria:**
- ✓ PostgreSQL 18 running on localhost:5432
- ✓ pgweb on localhost:8081
- ✓ Redis on localhost:6379
- ✓ Data in Docker volume (not host directory)

### Task 1.2: Migration Script
**Goal:** Create migration management script

**Steps:**
1. Create `devel/migrate.sh` from `resources/sf-saas-priv/script/migrate.sh`
2. Update: `MIGRATIONS="${MIGRATIONS_PATH:-$ROOT/provider/repository/psql/migrations}"`
3. Update: `PG_DSN="postgres://dev-node:changeme@localhost:5432/dev-node?sslmode=disable"`
4. `chmod +x devel/migrate.sh`
5. Test: `./devel/migrate.sh version`

**Acceptance Criteria:**
- ✓ Supports: version, new, up, down, force
- ✓ Connects to PostgreSQL
- ✓ Can create migration files

---

## Phase 2: Database Schema & Migrations

### Task 2.1: Create Migrations Directory
**Goal:** Set up migration infrastructure

**Steps:**
1. `mkdir -p provider/repository/psql/migrations/`
2. `./devel/migrate.sh new init_schema`

**Acceptance Criteria:**
- ✓ Migrations directory exists
- ✓ Files created: 000001_init_schema.up.sql and .down.sql

### Task 2.2: Write Session Table Migration
**Goal:** Create sessions table with proper constraints

**Migration:** `provider/repository/psql/migrations/000001_init_schema.up.sql`

```sql
-- Reusable trigger function for updated_at
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Sessions table (NO string address fields - BYTEA only)
CREATE TABLE sessions (
    id VARCHAR(255) PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_keep_alive TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status VARCHAR(50) NOT NULL,
    metadata JSONB,
    ended_at TIMESTAMP,
    end_reason INTEGER,

    -- Escrow addresses - BYTEA with CHECK constraints
    payer BYTEA NOT NULL CHECK (length(payer) = 20),
    receiver BYTEA NOT NULL CHECK (length(receiver) = 20),
    data_service BYTEA NOT NULL CHECK (length(data_service) = 20),
    signer BYTEA NOT NULL CHECK (length(signer) = 20),

    -- Usage tracking
    blocks_processed BIGINT NOT NULL DEFAULT 0,
    bytes_transferred BIGINT NOT NULL DEFAULT 0,
    requests BIGINT NOT NULL DEFAULT 0,
    total_cost NUMERIC,

    -- Baseline snapshots
    baseline_blocks BIGINT NOT NULL DEFAULT 0,
    baseline_bytes BIGINT NOT NULL DEFAULT 0,
    baseline_reqs BIGINT NOT NULL DEFAULT 0,
    baseline_cost NUMERIC
);

CREATE INDEX idx_sessions_payer ON sessions(payer);
CREATE INDEX idx_sessions_status ON sessions(status);
CREATE INDEX idx_sessions_created_at ON sessions(created_at);

CREATE TRIGGER sessions_updated_at
BEFORE UPDATE ON sessions
FOR EACH ROW
EXECUTE FUNCTION update_updated_at_column();
```

**Acceptance Criteria:**
- ✓ BYTEA only for addresses (no VARCHAR duplicates)
- ✓ CHECK constraints for 20-byte addresses
- ✓ JSONB for metadata
- ✓ NUMERIC for GRT values
- ✓ Indexes on payer, status, created_at
- ✓ Auto-update trigger

### Task 2.3: Write RAVs Table Migration
**Goal:** Create ravs table for SignedRAV storage

**Add to migration:**
```sql
-- RAVs table (one-to-one with sessions)
CREATE TABLE ravs (
    session_id VARCHAR(255) PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,

    -- RAV message fields with CHECK constraints
    collection_id BYTEA NOT NULL CHECK (length(collection_id) = 32),
    payer BYTEA NOT NULL CHECK (length(payer) = 20),
    service_provider BYTEA NOT NULL CHECK (length(service_provider) = 20),
    data_service BYTEA NOT NULL CHECK (length(data_service) = 20),
    timestamp_ns BIGINT NOT NULL,
    value_aggregate NUMERIC NOT NULL,
    metadata BYTEA,

    -- Signature (always present)
    signature BYTEA NOT NULL CHECK (length(signature) = 65),

    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

**Acceptance Criteria:**
- ✓ One-to-one with sessions
- ✓ CHECK constraints for byte lengths
- ✓ Cascade delete
- ✓ NUMERIC for value_aggregate

### Task 2.4: Write Workers Table Migration
**Goal:** Create workers table

**Add to migration:**
```sql
CREATE TABLE workers (
    key VARCHAR(255) PRIMARY KEY,
    session_id VARCHAR(255) NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    payer BYTEA NOT NULL CHECK (length(payer) = 20),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    trace_id VARCHAR(255)
);

CREATE INDEX idx_workers_session_id ON workers(session_id);
CREATE INDEX idx_workers_payer ON workers(payer);
```

**Acceptance Criteria:**
- ✓ Foreign key to sessions with cascade
- ✓ BYTEA only for payer (no VARCHAR)
- ✓ CHECK constraint for 20 bytes

### Task 2.5: Write Quota & Usage Tables Migration
**Goal:** Create quota_usage and usage_events tables

**Add to migration:**
```sql
-- Quota usage table
CREATE TABLE quota_usage (
    payer BYTEA PRIMARY KEY CHECK (length(payer) = 20),
    active_sessions INTEGER NOT NULL DEFAULT 0,
    active_workers INTEGER NOT NULL DEFAULT 0,
    last_updated TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TRIGGER quota_usage_updated_at
BEFORE UPDATE ON quota_usage
FOR EACH ROW
EXECUTE FUNCTION update_updated_at_column();

-- Usage events table
CREATE TABLE usage_events (
    id SERIAL PRIMARY KEY,
    session_id VARCHAR(255) NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    timestamp TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    blocks BIGINT NOT NULL DEFAULT 0,
    bytes BIGINT NOT NULL DEFAULT 0,
    requests BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX idx_usage_events_session_id ON usage_events(session_id);
CREATE INDEX idx_usage_events_timestamp ON usage_events(timestamp);
```

**Acceptance Criteria:**
- ✓ quota_usage keyed by BYTEA payer
- ✓ usage_events with foreign key to sessions
- ✓ Proper indexes

### Task 2.6: Write Down Migration
**Goal:** Create rollback migration

**File:** `provider/repository/psql/migrations/000001_init_schema.down.sql`

```sql
DROP TABLE IF EXISTS usage_events;
DROP TABLE IF EXISTS workers;
DROP TABLE IF EXISTS quota_usage;
DROP TABLE IF EXISTS ravs;
DROP TABLE IF EXISTS sessions;
DROP FUNCTION IF EXISTS update_updated_at_column();
```

**Acceptance Criteria:**
- ✓ Drops tables in correct order (reverse dependencies)

### Task 2.7: Run Initial Migration
**Goal:** Apply schema to database

**Steps:**
1. Ensure Docker Compose running
2. `./devel/migrate.sh up`
3. Verify with psql:
   ```bash
   psql postgres://dev-node:changeme@localhost:5432/dev-node -c "\dt"
   psql postgres://dev-node:changeme@localhost:5432/dev-node -c "\d sessions"
   psql postgres://dev-node:changeme@localhost:5432/dev-node -c "\d ravs"
   ```
4. Test down: `./devel/migrate.sh down`
5. Re-apply: `./devel/migrate.sh up`

**Acceptance Criteria:**
- ✓ Migration applies successfully
- ✓ All tables exist with correct schema
- ✓ CHECK constraints in place
- ✓ Indexes and triggers present
- ✓ Down migration works

---

## Phase 3: Core PostgreSQL Package Structure

### Task 3.1: Create Package Directories
**Goal:** Set up Go package structure

**Steps:**
```bash
mkdir -p provider/repository/psql/sql/{session,worker,quota,usage}
```

**Acceptance Criteria:**
- ✓ Directory structure matches sf-saas-priv pattern

### Task 3.2: Create statements.go (SQL Embedding & Helpers)
**Goal:** Implement SQL embedding and helper functions

**File:** `provider/repository/psql/statements.go`

```go
package psql

import (
    "bytes"
    "context"
    "database/sql"
    "embed"
    "fmt"
    "strings"
    "text/template"
)

//go:embed sql
var statements embed.FS

var templates *template.Template

func initTemplates() {
    var err error
    templates, err = template.ParseFS(statements, "sql/*/*.sql")
    if err != nil {
        panic(fmt.Errorf("unable to parse embedded sql statements: %w", err))
    }
}

func onDiskStatement(file string) string {
    if templates == nil {
        initTemplates()
    }

    _, name, found := strings.Cut(file, "/")
    if !found {
        panic(fmt.Errorf("unable to find 'folder/name' in %q", file))
    }

    buffer := bytes.NewBuffer(make([]byte, 0, 1024))
    if err := templates.ExecuteTemplate(buffer, name, map[string]any{}); err != nil {
        panic(fmt.Errorf("unable to execute embedded sql statements: %w", err))
    }

    return buffer.String()
}

// getOne retrieves a single record
func getOne[T any](ctx context.Context, db *Database, statement string, args map[string]any) (*T, error) {
    stmt := db.mustGetStmt(statement)
    var model T
    err := stmt.GetContext(ctx, &model, args)
    if err != nil {
        if err == sql.ErrNoRows {
            return nil, ErrNotFound
        }
        return nil, fmt.Errorf("failed %s: %w", strings.ReplaceAll(statement, "_", " "), err)
    }
    return &model, nil
}

// getMany retrieves multiple records
func getMany[T any](ctx context.Context, db *Database, statement string, args map[string]any) ([]*T, error) {
    stmt := db.mustGetStmt(statement)
    var models []*T
    err := stmt.SelectContext(ctx, &models, args)
    if err != nil {
        if err == sql.ErrNoRows {
            return nil, nil
        }
        return nil, fmt.Errorf("failed %s: %w", strings.ReplaceAll(statement, "_", " "), err)
    }
    return models, nil
}

// execOne executes INSERT/UPDATE with RETURNING
func execOne[T any](ctx context.Context, db *Database, statement string, args map[string]any) (*T, error) {
    stmt := db.mustGetStmt(statement)
    var model T
    err := stmt.GetContext(ctx, &model, args)
    if err != nil {
        return nil, fmt.Errorf("failed %s: %w", strings.ReplaceAll(statement, "_", " "), err)
    }
    return &model, nil
}

// execSimple executes without returning data
func execSimple(ctx context.Context, db *Database, statement string, args map[string]any) error {
    stmt := db.mustGetStmt(statement)
    _, err := stmt.ExecContext(ctx, args)
    if err != nil {
        return fmt.Errorf("failed %s: %w", strings.ReplaceAll(statement, "_", " "), err)
    }
    return nil
}
```

**Acceptance Criteria:**
- ✓ SQL embedding with go:embed
- ✓ Helper functions: getOne, getMany, execOne, execSimple
- ✓ Template parsing

### Task 3.3: Create database.go (Core Database Type)
**Goal:** Implement Database struct and setup

**File:** `provider/repository/psql/database.go`

```go
package psql

import (
    "context"
    "errors"
    "fmt"

    "github.com/jmoiron/sqlx"
    _ "github.com/lib/pq"
    "go.uber.org/zap"
)

var ErrNotFound = errors.New("not found")

var preparedStmts = map[string]string{}

func registerFiles(files []string) {
    for _, file := range files {
        stmt := onDiskStatement(file)
        if _, found := preparedStmts[file]; found {
            panic(fmt.Errorf("statement %q already registered", file))
        }
        preparedStmts[file] = stmt
    }
}

type Database struct {
    *sqlx.DB
    stmts  map[string]*sqlx.NamedStmt
    logger *zap.Logger
}

func NewRepository(dbConn *sqlx.DB, logger *zap.Logger) *Database {
    return &Database{
        DB:     dbConn,
        stmts:  make(map[string]*sqlx.NamedStmt),
        logger: logger,
    }
}

func (r *Database) Setup() error {
    if err := r.setupPreparedStmt(preparedStmts); err != nil {
        return fmt.Errorf("failed to register prepared stmt: %w", err)
    }
    return nil
}

func (r *Database) setupPreparedStmt(stmts map[string]string) error {
    for k, s := range stmts {
        if _, found := r.stmts[k]; found {
            return fmt.Errorf("statement key %q already in use", k)
        }

        ps, err := r.PrepareNamed(s)
        if err != nil {
            return fmt.Errorf("failed to register prepared statement with key %q: %w", k, err)
        }

        r.stmts[k] = ps
    }
    return nil
}

func (r *Database) mustGetStmt(key string) *sqlx.NamedStmt {
    v, ok := r.stmts[key]
    if !ok {
        panic(fmt.Errorf("unable to find prepared stmt %q", key))
    }
    return v
}

// Ping checks database connectivity
func (r *Database) Ping(ctx context.Context) error {
    return r.DB.PingContext(ctx)
}

// Close closes the database connection
func (r *Database) Close() error {
    return r.DB.Close()
}

// GetConnectionFromDSN creates a database connection
func GetConnectionFromDSN(ctx context.Context, dsn string) (*sqlx.DB, error) {
    db, err := sqlx.ConnectContext(ctx, "postgres", dsn)
    if err != nil {
        return nil, fmt.Errorf("failed to connect to database: %w", err)
    }

    db.SetMaxOpenConns(25)
    db.SetMaxIdleConns(5)

    return db, nil
}
```

**Acceptance Criteria:**
- ✓ Database struct with sqlx.DB
- ✓ Prepared statement management
- ✓ Setup(), Ping(), Close() methods
- ✓ Connection helper

---

## Phase 4: Test Infrastructure

### Task 4.1: Create Test Logging
**Goal:** Set up proper logging for tests

**File:** `provider/repository/psql/log_test.go`

```go
package psql

import (
    "github.com/streamingfast/logging"
    "go.uber.org/zap"
)

var zlog, _ = logging.PackageLogger("psql-test", "github.com/graphprotocol/substreams-data-service/provider/repository/psql")

func init() {
    logging.InstantiateLoggers(logging.WithDefaultLevel(zap.PanicLevel))
}
```

**Acceptance Criteria:**
- ✓ Uses logging.PackageLogger
- ✓ InstantiateLoggers called
- ✓ Supports DLOG=debug

### Task 4.2: Create TestMain Bootstrap
**Goal:** Bootstrap PostgreSQL container once

**File:** `provider/repository/psql/main_test.go`

```go
package psql

import (
    "context"
    "fmt"
    "os"
    "testing"
    "time"

    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/modules/postgres"
    "github.com/testcontainers/testcontainers-go/wait"
)

var (
    postgresContainer *postgres.PostgresContainer
    postgresTestDSN   string
)

func TestMain(m *testing.M) {
    ctx := context.Background()

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
        fmt.Fprintf(os.Stderr, "Failed to start PostgreSQL container: %v\n", err)
        os.Exit(1)
    }

    postgresTestDSN, err = postgresContainer.ConnectionString(ctx, "sslmode=disable")
    if err != nil {
        fmt.Fprintf(os.Stderr, "Failed to get connection string: %v\n", err)
        os.Exit(1)
    }

    code := m.Run()

    if err := postgresContainer.Terminate(ctx); err != nil {
        fmt.Fprintf(os.Stderr, "Failed to terminate container: %v\n", err)
    }

    os.Exit(code)
}
```

**Acceptance Criteria:**
- ✓ PostgreSQL 18 container
- ✓ Bootstrap once in TestMain
- ✓ No sync.Once needed
- ✓ Pattern from test/integration/main_test.go

### Task 4.3: Create Test Helper
**Goal:** Per-test schema isolation

**File:** `provider/repository/psql/database_test.go`

```go
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

    // Set search path
    _, err = db.ExecContext(ctx, fmt.Sprintf("SET search_path TO %s", schemaName))
    require.NoError(t, err)

    // Run migrations
    runMigrationsInSchema(t, db, schemaName)

    // Create repository
    repo := NewRepository(db, zlog)
    require.NoError(t, repo.Setup())

    testFunc(repo)
}

func runMigrationsInSchema(t *testing.T, db *sqlx.DB, schema string) {
    migrationPath := "file://migrations"

    dbDriver, err := migratepg.WithInstance(db.DB, &migratepg.Config{
        MigrationsTable: "schema_migrations",
        SchemaName:      schema,
    })
    require.NoError(t, err)

    m, err := migrate.NewWithDatabaseInstance(migrationPath, schema, dbDriver)
    require.NoError(t, err)

    err = m.Up()
    if err != nil && err != migrate.ErrNoChange {
        require.NoError(t, err)
    }
}
```

**Acceptance Criteria:**
- ✓ Separate schema per test
- ✓ Automatic migration
- ✓ Cleanup after test

### Task 4.4: Create Smoke Test
**Goal:** Verify test infrastructure works

**File:** `provider/repository/psql/database_test.go` (add)

```go
func TestDatabaseConnection(t *testing.T) {
    withTestDB(t, func(db *Database) {
        ctx := context.Background()

        err := db.Ping(ctx)
        require.NoError(t, err)

        // Verify tables exist
        var tableCount int
        err = db.QueryRowContext(ctx, `
            SELECT COUNT(*) FROM information_schema.tables
            WHERE table_schema = current_schema()
        `).Scan(&tableCount)
        require.NoError(t, err)
        require.Greater(t, tableCount, 0)
    })
}
```

**Steps:**
1. Run: `go test ./provider/repository/psql -v`
2. Should see: Container starts, migrations run, test passes

**Acceptance Criteria:**
- ✓ Test runs successfully
- ✓ Container starts automatically
- ✓ Migrations apply
- ✓ Tables created

---

## Phase 5: Custom SQL Types

### Task 5.1: Create sqltypes.go
**Goal:** Implement custom SQL types with validation

**File:** `provider/repository/psql/sqltypes.go`

See spec.md for complete implementation of:
- `jsonbMap` - JSONB metadata
- `grt` - GRT values (wraps big.Int)
- `address` - 20-byte Ethereum address
- `signature` - 65-byte ECDSA signature
- `collectionID` - 32-byte collection identifier

All implement `sql.Scanner` and `driver.Valuer`.

**Acceptance Criteria:**
- ✓ All types implement sql.Scanner and driver.Valuer
- ✓ Validation on Scan (byte lengths)
- ✓ Accessor methods (BigInt(), Address(), Signature(), Bytes())

### Task 5.2: Create sqltypes_test.go
**Goal:** Comprehensive tests for custom types

**File:** `provider/repository/psql/sqltypes_test.go`

Tests for each type:
- Valid inputs (bytes, strings, nil)
- Invalid inputs (wrong length, wrong type)
- Round-trip Scan→Value
- Accessor methods

**Acceptance Criteria:**
- ✓ Tests for grt (string, bytes, int64, nil, invalid)
- ✓ Tests for address (valid 20 bytes, too short, too long, nil, wrong type)
- ✓ Tests for signature (valid 65 bytes, wrong lengths, nil)
- ✓ Tests for collectionID (valid 32 bytes, wrong lengths, nil)
- ✓ All tests passing

---

## Phase 6: SQL Models & Repository Implementation

### Task 6.1: Create models.go
**Goal:** Define SQL mapping structs using custom types

**File:** `provider/repository/psql/models.go`

Define structs using custom types:
- `sessionRow` - uses `address`, `grt`, `jsonbMap`
- `ravRow` - uses `address`, `signature`, `collectionID`, `grt`
- `workerRow` - uses `address`
- `quotaUsageRow` - uses `address`
- `usageEventRow` - standard types

Implement conversion methods:
- `toRepository()` - SQL → repository types
- `fromRepository()` - repository → SQL types

**Acceptance Criteria:**
- ✓ All custom types used appropriately
- ✓ Conversion methods handle all fields
- ✓ No manual byte length validation (custom types handle it)

### Task 6.2: Implement SessionCreate
**Goal:** Implement first Session method

**SQL:** `provider/repository/psql/sql/session/create_session.sql`
**Go:** `provider/repository/psql/session.go`

Start with SessionCreate, test thoroughly, then expand to other methods.

**Acceptance Criteria:**
- ✓ SQL with explicit columns (no SELECT *)
- ✓ Prepared statement registered
- ✓ Integration test passes

### Task 6.3-6.10: Implement Remaining Methods
Incrementally implement all GlobalRepository methods:
- Session: Get, Update, Delete, List, GetByPayer, Count
- Worker: Create, Get, Delete, ListBySession, CountByPayer
- Quota: Get, Increment, Decrement
- Usage: Add, GetTotal

For each:
1. Write SQL file
2. Register in init()
3. Implement method
4. Write integration test
5. Verify test passes

---

## Phase 7: Integration & Documentation

### Task 7.1: Comprehensive Integration Tests
**Goal:** Test full Repository lifecycle

Test scenarios:
- Session creation → RAV update → worker tracking → usage accumulation
- Quota tracking with concurrent sessions/workers
- Cascade deletes
- Edge cases (nil values, empty metadata, etc.)

**Acceptance Criteria:**
- ✓ All Repository methods tested
- ✓ 100% code coverage

### Task 7.2: Update CHANGELOG.md
**Goal:** Document the new feature

Add to CHANGELOG.md:
```markdown
## Unreleased

### Added
- PostgreSQL-based GlobalRepository implementation for persistent session/worker/quota/usage storage
- Custom SQL types (grt, address, signature, collectionID) with built-in validation
- Database migrations with CHECK constraints for data integrity
- Docker Compose setup for local development (PostgreSQL 18)
- Comprehensive integration tests using testcontainers
```

**Acceptance Criteria:**
- ✓ CHANGELOG updated
- ✓ Entry in ## Unreleased section

---

## Notes

- **During development:** Can drop/recreate schema freely
- **After first version:** Use incremental migrations only
- **Transaction support:** Basic (single operations only)
- **Migration path:** Actual usage migration is separate track
- **Test with DLOG:** `DLOG=debug go test ./provider/repository/psql -v`
