# Track: PostgreSQL Repository Implementation

## Overview

Implement a PostgreSQL-based `GlobalRepository` to replace the current `InMemoryRepository` implementation with persistent storage. This provides durability and scalability for session/worker/quota/usage tracking in the Substreams Data Service.

## Requirements

### Environment
- **Local Development Only** - Docker Compose + testcontainers
- No CI/CD-specific configuration (testcontainers work automatically on GitHub CI)

### Transaction Support
- **Basic** - Single operations only
- No cross-method transactions needed

### Repository Strategy
- **Replace** - PostgresRepository becomes the default
- InMemoryRepository kept for simple tests

### Schema Evolution
- **Incremental migrations** after first version
- Can drop/recreate during initial development

### Migration Path
- **Gradual** - Create PostgresRepository first
- Update actual usage in follow-up track

## Design Decisions

### Data Persistence

**PricingConfig:**
- ✅ **NOT STORED** in database
- Provided at runtime
- Rationale: Configuration data, not persistent state

**Metadata (map[string]string):**
- ✅ **JSONB column**
- Rationale: Simple key-value map, perfect for JSONB

**SignedRAV:**
- ✅ **Separate `ravs` table** (relational)
- Stores complete SignedRAV (message + signature together)
- One-to-one relationship with sessions
- Rationale: Queryable fields, clean relational design

### Data Types

**eth.Address (20 bytes):**
- ✅ **BYTEA** (canonical representation)
- ❌ NOT VARCHAR(42)
- Rationale: More compact, type-safe, canonical
- Database CHECK constraint: `CHECK (length(payer) = 20)`
- Go custom type validates length on scan

**big.Int (GRT values):**
- ✅ **NUMERIC** (arbitrary precision)
- ❌ NOT TEXT
- Rationale: Native PostgreSQL support, can do SQL math operations
- Go custom `grt` type handles scanning/marshaling

**eth.Signature (65 bytes):**
- ✅ **BYTEA** with validation
- Database CHECK constraint: `CHECK (length(signature) = 65)`
- Go custom type validates length on scan

**CollectionID (32 bytes):**
- ✅ **BYTEA** with validation
- Database CHECK constraint: `CHECK (length(collection_id) = 32)`
- Go custom type validates length on scan

### Custom SQL Types

To ensure type safety and validation, we use custom Go types that implement `sql.Scanner` and `driver.Valuer`:

**`grt`** - Wraps `*big.Int` for GRT values
- Validates on scan (handles string/bytes/int64 from PostgreSQL NUMERIC)
- Provides `BigInt()` accessor

**`address`** - Wraps `[20]byte` for Ethereum addresses
- Validates exactly 20 bytes on scan
- Provides `Address()` accessor to `eth.Address`

**`signature`** - Wraps `[65]byte` for ECDSA signatures
- Validates exactly 65 bytes on scan
- Provides `Signature()` accessor to `eth.Signature`

**`collectionID`** - Wraps `[32]byte` for collection identifiers
- Validates exactly 32 bytes on scan
- Provides `Bytes()` accessor

**`jsonbMap`** - Wraps `map[string]string` for metadata
- Handles JSON marshaling/unmarshaling

## Schema Design

### Sessions Table

```sql
CREATE TABLE sessions (
    id VARCHAR(255) PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_keep_alive TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status VARCHAR(50) NOT NULL,
    metadata JSONB,
    ended_at TIMESTAMP,
    end_reason INTEGER,

    -- Escrow addresses (20 bytes each) - BYTEA only, no string duplicates
    payer BYTEA NOT NULL CHECK (length(payer) = 20),
    receiver BYTEA NOT NULL CHECK (length(receiver) = 20),
    data_service BYTEA NOT NULL CHECK (length(data_service) = 20),
    signer BYTEA NOT NULL CHECK (length(signer) = 20),

    -- Usage tracking
    blocks_processed BIGINT NOT NULL DEFAULT 0,
    bytes_transferred BIGINT NOT NULL DEFAULT 0,
    requests BIGINT NOT NULL DEFAULT 0,
    total_cost NUMERIC,  -- GRT value

    -- Baseline snapshots
    baseline_blocks BIGINT NOT NULL DEFAULT 0,
    baseline_bytes BIGINT NOT NULL DEFAULT 0,
    baseline_reqs BIGINT NOT NULL DEFAULT 0,
    baseline_cost NUMERIC  -- GRT value
);

CREATE INDEX idx_sessions_payer ON sessions(payer);
CREATE INDEX idx_sessions_status ON sessions(status);
CREATE INDEX idx_sessions_created_at ON sessions(created_at);

CREATE TRIGGER sessions_updated_at
BEFORE UPDATE ON sessions
FOR EACH ROW
EXECUTE FUNCTION update_updated_at_column();
```

### RAVs Table

```sql
CREATE TABLE ravs (
    session_id VARCHAR(255) PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,

    -- RAV message fields (with length validation)
    collection_id BYTEA NOT NULL CHECK (length(collection_id) = 32),
    payer BYTEA NOT NULL CHECK (length(payer) = 20),
    service_provider BYTEA NOT NULL CHECK (length(service_provider) = 20),
    data_service BYTEA NOT NULL CHECK (length(data_service) = 20),
    timestamp_ns BIGINT NOT NULL,
    value_aggregate NUMERIC NOT NULL,  -- GRT value
    metadata BYTEA,

    -- Signature (always present - we only store SignedRAVs)
    signature BYTEA NOT NULL CHECK (length(signature) = 65),

    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

**Design Notes:**
- One-to-one with sessions (session can have 0 or 1 RAV)
- Every RAV stored is a SignedRAV (no unsigned RAVs)
- Cascade delete when session is deleted
- All BYTEA fields have CHECK constraints

### Workers Table

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

**Design Notes:**
- Foreign key to sessions with cascade delete
- BYTEA only for payer (no redundant string field)

### Quota Usage Table

```sql
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
```

**Design Notes:**
- Keyed by BYTEA payer (no redundant string field)
- Tracks active sessions/workers per payer

### Usage Events Table

```sql
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

## Architecture

### Package Structure

```
provider/repository/psql/
├── migrations/              # Database migrations
│   └── 000001_init_schema.up.sql
│   └── 000001_init_schema.down.sql
├── sql/                     # Embedded SQL files
│   ├── session/
│   ├── worker/
│   ├── quota/
│   └── usage/
├── database.go             # Core Database type, prepared statements
├── statements.go           # SQL embedding, helper functions (getOne, getMany, etc.)
├── sqltypes.go            # Custom SQL types (grt, address, signature, etc.)
├── sqltypes_test.go       # Tests for custom SQL types
├── models.go              # SQL mapping structs (sessionRow, ravRow, etc.)
├── session.go             # Session CRUD implementation
├── worker.go              # Worker CRUD implementation
├── quota.go               # Quota CRUD implementation
├── usage.go               # Usage CRUD implementation
├── log_test.go            # Test logging setup
├── main_test.go           # TestMain for container bootstrap
└── database_test.go       # Test helpers
```

### Code Patterns

**From `resources/sf-saas-priv/api/store/psql/`:**
- SQL files in separate source files (embedded with `go:embed`)
- Prepared statements pattern
- Explicit column listing (NO `SELECT *`)
- Helper functions: `getOne`, `getMany`, `execOne`, `execSimple`
- Organized by domain/entity in separate files
- Transactions when appropriate

### Test Strategy

**TestMain Pattern:**
- Bootstrap PostgreSQL container once in `TestMain`
- No sync.Once or locking needed
- Pattern from `test/integration/main_test.go`

**Per-Test Isolation:**
- Each test runs in its own PostgreSQL schema
- Fast (no container restart per test)
- Clean separation (no test interference)

**Logging:**
- Proper `logging.PackageLogger` setup
- `InstantiateLoggers` in test init
- DLOG support for debugging (`DLOG=debug go test`)

**No Build Tags:**
- No `//go:build test` tags
- Just regular Go test files

## Success Criteria

- [ ] All GlobalRepository interface methods implemented
- [ ] All tests passing (100% coverage)
- [ ] Docker Compose setup working (PostgreSQL 18)
- [ ] Migration system functional
- [ ] Code follows sf-saas-priv patterns
- [ ] No wildcard SELECT statements
- [ ] Proper use of prepared statements
- [ ] **Minimal JSONB usage** (only metadata)
- [ ] **BYTEA for addresses** (20 bytes, with CHECK constraints)
- [ ] **NUMERIC for big.Int** (arbitrary precision)
- [ ] **Custom SQL types** with validation (grt, address, signature, collectionID)
- [ ] **No redundant string address fields**
- [ ] PricingConfig NOT stored in database (provided at runtime)
- [ ] Testcontainers with schema isolation using TestMain pattern
- [ ] Proper logging with `logging.PackageLogger` (DLOG support)
- [ ] Comprehensive tests for all custom SQL types

## Benefits

### Type Safety
- Custom Go types prevent mixing up address/signature/collectionID
- Compile-time type checking

### Double Validation
- Database CHECK constraints (20/32/65 bytes)
- Go type validation on Scan

### Performance
- BYTEA more compact than VARCHAR for addresses
- NUMERIC native support for big integers
- Can do SQL math on GRT values if needed

### Maintainability
- No redundant string address fields
- Single source of truth (BYTEA)
- Clear type semantics

### Clean Design
- Minimal JSONB (only where appropriate)
- Proper relational design for RAV
- Explicit column listing ensures schema changes don't break code
