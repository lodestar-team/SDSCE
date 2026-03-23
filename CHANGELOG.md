# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## Unreleased

### Added

- Add E2E Substreams request execution to `TestFirecore` using `sds sink run` command with `common@v0.1.0` manifest
- Add `runSDSSink()` helper function to execute sds sink commands from integration tests
- Add consumer sidecar startup to `TestFirecore` E2E integration test
- Add `impl.StartProviderGateway()` helper function for starting provider gateway programmatically (useful for testing)
- Add full E2E integration test with dummy-blockchain container, Substreams tier1, SDS plugins, and consumer sidecar (`TestFirecore`)
- Add automatic database migrations in integration test setup using golang-migrate
- Add `waitForSidecarHealth()` helper function for integration tests

### Changed

- Refactor session and usage services to use lightweight plugin pattern with session ID as first-class protobuf field instead of HTTP headers
  - Session service: Added `sds_session_id` field to `BorrowWorkerRequest`, plugin extracts from auth context and sets in request
  - Usage service: Added `sds_session_id` field to `Event` message, metering plugin populates from auth context Meta
  - Both services now read session ID from request fields instead of HTTP headers, making the flow explicit and type-safe
  - Removed `propagateTrustedHeaders` function from session plugin as it's no longer needed
- Refactor integration tests to use `impl.StartProviderGateway()` instead of direct gateway instantiation
- Refactor integration test helpers: extract `startDummyBlockchainContainer()` to encapsulate container setup, port retrieval, and health verification
- Remove unused functions from integration tests (`startFirecore`, `waitForSubstreamsTier1Ready`) and simplify `newDummyBlockchainContainer` signature
- Rename session protocol fields for clarity: `trace_id` → `session_id` and `max_worker_for_trace_id` → `max_workers_per_session` in BorrowWorkerRequest
- Move `ErrNotFound` from psql package to repository package for interface-level error handling

### Fixed

- Fix PostgreSQL timezone handling by changing all TIMESTAMP columns to TIMESTAMPTZ in schema migration, preventing 4-hour session timeout errors caused by timezone interpretation issues
- Fix Anvil container startup failures in dev environment by adding retry logic (up to 5 attempts with exponential backoff) to handle "port is already allocated" errors when previous container hasn't fully terminated
- Fix session ID propagation to usage events by setting session ID as Meta in auth plugin trusted headers, ensuring dmetering middleware includes correct session ID in usage events instead of falling back to organization ID
- Fix session ID retrieval in session service by reading directly from HTTP request headers (`x-sds-session-id`) instead of context, ensuring proper session ID propagation from auth service through session plugin
- Fix PostgreSQL DSN scheme conversion in integration tests to handle both `postgresql://` and `postgres://` schemes from testcontainers
- Fix session ID propagation from auth plugin to session service by adding trusted headers interceptor to gateway and propagating trusted headers in session plugin HTTP requests
- Rename `provider/repository/psql/models.go` to `mappings.go` for clarity
- Replace ignored errors in Value() calls with `mustValue()` helper
- Use `sds.MustNewGRT(n)` in tests instead of `sds.NewGRTFromBigInt(big.NewInt(n))`
- **Clean up Session, Worker, QuotaUsage, SessionFilter structs to use typed eth.Address instead of string duplicates**:
  - Remove `PayerAddress`, `SignerAddress`, `ServiceProvider` string fields from `Session` (keep only `Payer`, `Receiver`, `DataService` eth.Address fields)
  - Change `Worker.PayerAddress string` to `Worker.Payer eth.Address`
  - Change `QuotaUsage.PayerAddress string` to `QuotaUsage.Payer eth.Address`
  - Change `SessionFilter.PayerAddress *string` to `SessionFilter.Payer *eth.Address`
  - Update `GlobalRepository` quota methods to use `eth.Address` instead of `string` for payer parameter
  - Update all repository implementations and tests to use proper typed addresses
- Add retry logic to PostgreSQL repository creation with automatic retries on connection failures (10 retries with fibonacci backoff up to 5 seconds)
- Use `zap.Stringer()` instead of `zap.String(..., addr.Pretty())` for all eth.Address logging
- **Fix session ID correlation between session creation and usage tracking**:
  - Add explicit `session_id` field to `ValidateAuthResponse` protobuf message
  - Auth service generates unique UUID session ID and returns it in `session_id` field
  - Auth plugin explicitly sets `dauth.HeaderMeta` with session ID and validates it's present
  - **Remove dangerous metadata loop** that allowed arbitrary trusted header overrides
  - Session service reads session ID from trusted headers context (`x-meta` field)
  - **Remove fallback session ID generation** - session service now returns error if auth service doesn't provide session ID
  - Metering plugin copies `Meta` field from `dmetering.Event` to proto event
  - Usage service derives session ID from `event.Meta` (falls back to `api_key_id` or `organization_id`)
  - This ensures session IDs match across auth, session creation, and usage events

### Fixed

- Fix `MustNewGRT` panic message using `%w` in `fmt.Sprintf` (should be `%v`)
- Fix PostgreSQL SessionList payer filter returning inverted results (was skipping matching payers instead of non-matching payers)

### Removed

- Remove unused `fromQuotaUsage()` function from psql package
- Remove unused GlobalRepository methods not used in production code:
  - `SessionDelete` - not used anywhere
  - `SessionGetByPayer` - only used in tests
  - `WorkerListBySession` - not used anywhere
  - `WorkerCountByPayer` - not used anywhere
  - `UsageGetTotal` - not used anywhere
- Remove corresponding SQL files, tests, and implementations from both inmemory and psql packages

### Added

- Add `sds.GRT` type for GRT token amounts with 18 decimal precision
  - Backed by `holiman/uint256` for efficient arithmetic
  - Supports parsing "X GRT" strings and plain decimal numbers
  - Implements `encoding.TextMarshaler/TextUnmarshaler`, `json.Marshaler/Unmarshaler`, and `yaml.Marshaler/Unmarshaler`
- Add `sds://` scheme plugins for firehose-core integration (`provider/plugin` package)
  - `plugin.RegisterAuth()` - registers `sds://` with dauth for RAV-based authentication
  - `plugin.RegisterSession()` - registers `sds://` with dsession for worker pool management
  - `plugin.RegisterMetering()` - registers `sds://` with dmetering for usage tracking
  - `plugin.Register()` - convenience function to register all three plugins at once
- Plugins are gRPC/Connect clients that connect to the provider gateway
- All business logic (service provider, escrow, quotas) is configured on the gateway, not the plugin
- Plugin configuration is minimal: `sds://host:port?plaintext=true&network=my-network`
- Add PostgreSQL repository implementation (`provider/repository/psql` package)
  - Full implementation of `repository.Repository` interface backed by PostgreSQL
  - Custom SQL types for Ethereum addresses, GRT amounts, signatures, and JSONB storage
  - Comprehensive test suite with 21 tests covering all repository operations
  - Uses testcontainers for integration testing with real PostgreSQL database
  - Schema migrations using golang-migrate/migrate library
  - Efficient prepared statement caching with sqlx
- Add DSN-based repository configuration for provider gateway
  - Supports `inmemory://` and `psql://user:pass@host:port/dbname` schemes
  - Flag: `--repository-dsn` (defaults to inmemory://)
  - Automatic password sanitization in logs for security
- Add Docker Compose configuration for local development
  - PostgreSQL 18, Redis, and pgweb (database UI)
  - Migration script (`devel/migrate.sh`) for schema management
  - Integrated into reflex workflow for automatic startup
  - Support `inmemory://` DSN for in-memory repository (default)
  - Support `psql://user:pass@host:port/dbname` DSN for PostgreSQL repository
  - New `--repository-dsn` flag for `sds provider gateway` command
  - Automatic repository instantiation based on DSN scheme
  - Password sanitization in logs for security

### Changed

- Refactor `PricingConfig` to use `sds.GRT` type instead of `*big.Int` for prices
  - Pricing YAML now accepts "X GRT" format (e.g., `price_per_block: "0.000001 GRT"`) or plain decimals
