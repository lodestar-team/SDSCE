# Provider Gateway Repository Configuration

The provider gateway supports pluggable repository backends for session and usage state storage. The repository is configured via a DSN (Data Source Name) string.

## Supported Repository Types

### In-Memory Repository

An in-memory repository that stores all data in RAM. The CLI still falls back to this when `--repository-dsn` is omitted, but it is only suitable for development, testing, and single-process demo runs.

**DSN Format:**
```
inmemory://
```

**Features:**
- Zero configuration required
- Fast access with no network overhead
- Data is lost on restart
- Not suitable for production use with multiple gateway instances

**Example:**
```bash
sds provider gateway \
  --repository-dsn "inmemory://" \
  --plaintext \
  --plugin-plaintext \
  --service-provider "0x..." \
  --collector-address "0x..." \
  --escrow-address "0x..." \
  --rpc-endpoint "http://localhost:8545" \
  --data-plane-endpoint "https://localhost:10016?insecure=true"
```

### PostgreSQL Repository

A PostgreSQL-backed repository that provides persistent storage with ACID guarantees.

**DSN Format:**
```
psql://[user[:password]@][netloc][:port][/dbname][?param1=value1&...]
```

The PostgreSQL DSN follows standard connection string format. Common parameters:
- `sslmode` - SSL mode (disable, require, verify-ca, verify-full)
- `connect_timeout` - Connection timeout in seconds
- `application_name` - Application name for connection tracking

**Features:**
- Persistent storage across restarts
- Supports multiple gateway instances (with proper configuration)
- Full ACID transaction support
- Automatic schema migrations
- Efficient prepared statement caching

**Example:**
```bash
sds provider gateway \
  --repository-dsn "psql://sds_user:secret@localhost:5432/sds?sslmode=disable" \
  --plaintext \
  --plugin-plaintext \
  --service-provider "0x..." \
  --collector-address "0x..." \
  --escrow-address "0x..." \
  --rpc-endpoint "http://localhost:8545" \
  --data-plane-endpoint "https://localhost:10016?insecure=true"
```

**Production Example with SSL:**
```bash
sds provider gateway \
  --repository-dsn "psql://sds_user:secret@prod-db.example.com:5432/sds?sslmode=require&connect_timeout=10" \
  --tls-cert-file "/etc/sds/provider.crt" \
  --tls-key-file "/etc/sds/provider.key" \
  --service-provider "0x..." \
  --collector-address "0x..." \
  --escrow-address "0x..." \
  --rpc-endpoint "https://mainnet.infura.io/v3/YOUR-KEY" \
  --data-plane-endpoint "https://tier1.example.com:10016"
```

When the payment gateway uses TLS and no plugin-specific transport flags are supplied, the private Plugin Gateway uses the same TLS certificate/key configuration. If a local/dev stack uses `--plaintext`, plugin-gateway plaintext must also be explicit with `--plugin-plaintext`; otherwise provide `--plugin-tls-cert-file` and `--plugin-tls-key-file`.

## Security Considerations

### Password Protection

The gateway automatically sanitizes DSN strings in logs to prevent password leakage:

```
Input:  psql://user:secret@localhost:5432/sds
Logged: psql://user:***@localhost:5432/sds
```

### Environment Variables

For production deployments, consider using environment variables for the DSN:

```bash
export SDS_REPOSITORY_DSN="psql://user:${DB_PASSWORD}@db.example.com:5432/sds?sslmode=require"
sds provider gateway --repository-dsn "${SDS_REPOSITORY_DSN}" ...
```

## Database Setup

### PostgreSQL

1. **Create Database and User:**
```sql
CREATE DATABASE sds;
CREATE USER sds_user WITH PASSWORD 'your_secure_password';
GRANT ALL PRIVILEGES ON DATABASE sds TO sds_user;
```

2. **Migrations:**
The gateway automatically applies migrations on startup. Ensure the user has schema creation privileges:
```sql
GRANT CREATE ON DATABASE sds TO sds_user;
```

3. **Connection Pooling:**
The repository configures reasonable connection pool defaults:
- Max open connections: 25
- Max idle connections: 5

For high-traffic deployments, consider using a connection pooler like PgBouncer.

## Error Handling

### Invalid DSN

```bash
$ sds provider gateway --repository-dsn "invalid-dsn" ...
Error: invalid DSN format: missing '://' separator
```

### Unsupported Scheme

```bash
$ sds provider gateway --repository-dsn "redis://localhost:6379" ...
Error: unsupported DSN scheme "redis" (supported: inmemory, psql)
```

### Connection Failure

```bash
$ sds provider gateway --repository-dsn "psql://user:pass@localhost:5432/nonexistent" ...
Error: failed to connect to PostgreSQL: pq: database "nonexistent" does not exist
```

## Testing

### Unit Tests

Run the repository tests:
```bash
go test ./provider/gateway/... -v
```

### Integration Tests with PostgreSQL

The PostgreSQL repository includes integration tests using testcontainers:
```bash
go test ./provider/repository/psql/... -v
```

### Manual Testing

Start a local PostgreSQL instance:
```bash
docker run -d --name sds-postgres \
  -e POSTGRES_DB=sds \
  -e POSTGRES_USER=sds_user \
  -e POSTGRES_PASSWORD=secret \
  -p 5432:5432 \
  postgres:18-alpine
```

Test the gateway with PostgreSQL:
```bash
sds provider gateway \
  --repository-dsn "psql://sds_user:secret@localhost:5432/sds?sslmode=disable" \
  --plaintext \
  --plugin-plaintext \
  --operator-listen-addr ":9010" \
  --operator-read-token-env "SDS_OPERATOR_READ_TOKEN" \
  --admin-write-token-env "SDS_ADMIN_WRITE_TOKEN" \
  --service-provider "0x1234567890123456789012345678901234567890" \
  --collector-address "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd" \
  --escrow-address "0x1111111111111111111111111111111111111111" \
  --rpc-endpoint "http://localhost:8545" \
  --data-plane-endpoint "https://localhost:10016?insecure=true"
```

For local reflex development, the provider gateway listens on `localhost:9010` for authenticated operator APIs. The reflex files set explicit local-only fallback token values for the provider process:

```bash
SDS_OPERATOR_READ_TOKEN=local-operator-read-token
SDS_ADMIN_WRITE_TOKEN=local-admin-write-token
```

Use the read token for `sds provider operator sessions|ravs|collections` and the admin token for `sds provider operator collect`.

The reflex fallback values are scoped to the provider process. Operator CLI commands running in a separate shell must either export the same values and use `--operator-token-env`, or pass the local token with `--operator-token`.

## Implementation Details

### Factory Pattern

The repository instantiation uses a factory pattern in `provider/gateway/repository.go`:

```go
repo, err := gateway.NewRepositoryFromDSN(ctx, dsn, logger)
if err != nil {
    return err
}
```

### Repository Interface

All repositories implement the `repository.GlobalRepository` interface defined in `provider/repository/repository.go`.

### Persistence Boundary

The current repository model is the provider runtime/session persistence layer.

It persists:

- sessions and workers
- usage events and accumulated usage
- quota/runtime coordination state
- the latest accepted RAV snapshot associated with a session
- collection lifecycle records for `collectible`, `collect_pending`, `collected`, and `collect_failed_retryable`

The collection lifecycle model is settlement state, not runtime session state. That boundary is defined in [docs/provider-persistence-boundary.md](../../docs/provider-persistence-boundary.md); provider operator APIs and CLI flows consume it through downstream authenticated retrieval surfaces rather than direct database access.

Provider operator API handlers use the gateway-carried `operatorauth.Config` to enforce the bearer-token contract documented in [docs/operator-auth.md](../../docs/operator-auth.md). The private `ProviderOperatorService` exposes session, runtime payment/funds state, accepted RAV, and collection lifecycle retrieval for `operator.read` callers; lifecycle mutation and other provider admin actions require `admin.write`. The manual collect CLI uses those admin transitions around locally signed `SubstreamsDataService.collect` transactions rather than accessing repository storage directly.

The private provider operator listener also exposes `/metrics` in Prometheus text format. Metrics require `operator.read` bearer authorization and report aggregate session, worker, usage, accepted-RAV, collection lifecycle, low-funds, payment-control, and RAV-request visibility without session-id labels.

### Backward Compatibility

If no `--repository-dsn` flag is provided, the gateway currently falls back to `inmemory://` for backward compatibility. Treat that as a local/demo convenience only; deployed or multi-instance gateways should set an explicit PostgreSQL DSN.
