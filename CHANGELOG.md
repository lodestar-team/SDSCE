# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## Unreleased

### Changed

- Move `ErrNotFound` from psql package to repository package for interface-level error handling
- Rename `provider/repository/psql/models.go` to `mappings.go` for clarity
- Replace ignored errors in Value() calls with `mustValue()` helper
- Use `sds.MustNewGRT(n)` in tests instead of `sds.NewGRTFromBigInt(big.NewInt(n))`

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
