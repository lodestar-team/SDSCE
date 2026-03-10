# Substreams Data Service

A Golang implementation of the payment infrastructure for Substreams Data Service on The Graph Network. This project provides sidecar services for both consumers and providers to handle payment sessions, RAV (Receipt Aggregate Voucher) signing, and on-chain settlement via the Graph Protocol's Horizon contracts.

## Development

### Prerequisites

- Go 1.25+
- Docker (for running [Development Environment](#development-environment) and integration tests)
- [direnv](https://direnv.net/) (optional, for auto-loading environment)
- [reflex](https://github.com/cespare/reflex) (optional, for auto-restart)
- [firecore](https://github.com/streamingfast/firehose-core)
- [dummy-blockchain](https://github.com/streamingfast/dummy-blockchain)

If you install `firecore`/`dummy-blockchain` with `go install`, ensure `$(go env GOPATH)/bin` is on your `PATH`.

### Quick Start

The `devel/sds` wrapper automatically compiles and runs the CLI on each invocation. Using [direnv](https://direnv.net/), create an `.envrc` file to add it to your PATH:

```bash
cat > .envrc <<'EOF'
PATH_add "$(pwd)/devel"
PATH_add "$(go env GOPATH)/bin"
EOF
direnv allow
```

Now `sds` invokes `devel/sds` directly. Use [reflex](https://github.com/cespare/reflex) to start everything (`devenv`, Consumer Sidecar, Provider Gateway & Firehose Stack) and auto-restart services (Consuemr Sidecar and Provider Gateway) on code changes:

```bash
reflex -c .reflex
```

Both reflex configs pass `--plaintext` explicitly for the local/demo sidecar↔gateway path. Outside local/demo usage, configure TLS certificate/key files instead of relying on plaintext defaults.

To keep on-chain state stable while restarting the rest of the stack, run `devenv` separately and use the stack-only reflex config:

```bash
./devel/sds devenv
./devel/sds demo setup  # writes devel/.demo.env required by `.reflex.stack`
reflex -c .reflex.stack
```

`.reflex.stack` now fails fast if `devel/.demo.env` is missing or does not contain the required demo variables.

We have `devel/sds_sink` helper that can be used to sink in data service mode (invokes `sds sink ...` configured for development environment):

```bash
sds_sink run common@v0.1.0 map_clocks -s -1
```

### Development Environment

> [!NOTE]
> If you are still having `reflex -c .reflex` running from quick start, your development environment is already running so no need to invoke `sds env`.

The `sds devenv` command starts an Anvil node and deploys Graph Protocol contracts (requires Docker). It deploys the original `PaymentsEscrow`, `GraphPayments`, and `GraphTallyCollector` contracts, plus `SubstreamsDataService` and various mock contracts (GRTToken, Controller, Staking, etc.) for testing. Integration tests use the same devenv via testcontainers.

```bash
sds devenv  # Prints contract addresses and test accounts
```

#### Docker Compose Services

The project includes Docker Compose for local development with PostgreSQL, Redis, and pgweb (database UI):

```bash
# Start all services (PostgreSQL 18, Redis, pgweb)
docker compose up -d

# Apply database migrations
./devel/migrate.sh up

# View database in browser
open http://localhost:8081

# Stop services
docker compose down
```

**Services:**
- **PostgreSQL 18**: localhost:5432 (credentials: `dev-node`/`changeme`)
- **pgweb**: localhost:8081 (database web UI)
- **Redis**: localhost:6379

**Database Migrations:**

The `devel/migrate.sh` script manages database schema migrations:

```bash
./devel/migrate.sh version       # Show current and target versions
./devel/migrate.sh up             # Apply all pending migrations
./devel/migrate.sh down           # Roll back one migration
./devel/migrate.sh new my_change  # Create new migration files
```

Migrations are stored in `provider/repository/psql/migrations/` and use the [golang-migrate](https://github.com/golang-migrate/migrate) library.

The devenv is deterministic. Key contract addresses:

| Contract | Address |
|----------|---------|
| GraphTallyCollector | `0x1d01649b4f94722b55b5c3b3e10fe26cd90c1ba9` |
| PaymentsEscrow | `0xfc7487a37ca8eac2e64cba61277aa109e9b8631e` |
| SubstreamsDataService | `0x37478fd2f5845e3664fe4155d74c00e1a4e7a5e2` |

Test accounts (10 ETH + 10,000 GRT each):

| Role | Address | Private Key |
|------|---------|-------------|
| Service Provider | `0xa6f1845e54b1d6a95319251f1ca775b4ad406cdf` | `0x41942233cf1d78b6e3262f1806f8da36aafa24a941031aad8e056a1d34640f8d` |
| Payer | `0xe90874856c339d5d3733c92ea5acadc6014b34d5` | `0xe4c2694501255921b6588519cfd36d4e86ddc4ce19ab1bc91d9c58057c040304` |
| User1 | `0x90353af8461a969e755ef1e1dbadb9415ae5cb6e` | `0xdd02564c0e9836fb570322be23f8355761d4d04ebccdc53f4f53325227680a9f` |
| User2 | `0x9585430b90248cd82cb71d5098ac3f747f89793b` | `0xbc3def46fab7929038dfb0df7e0168cba60d3384aceabf85e23e5e0ff90c8fe3` |
| User3 | `0x37305c711d52007a2bcfb33b37015f1d0e9ab339` | `0x7acd0f26d5be968f73ca8f2198fa52cc595650f8d5819ee9122fe90329847c48` |

### Running Tests

```bash
go test ./...                      # All tests
go test ./test/integration/... -v  # Integration tests (requires Docker)
```

### Running Full System with Firecore

To run the full Substreams Data Service stack with a Firehose provider, you need `firecore` and `dummy-blockchain` binaries (see [Prerequisites](#prerequisites)). Clone the repositories and build from source:

```bash
# Build firecore
#
# IMPORTANT: `firecore` must include SDS plugin registration support, otherwise the `sds://...` plugins
# configured in `devel/firecore.config.yaml` won't load. Use at least this commit:
#   536bcd99495f42a27b67b340ccf8416f0fc967bf
go install github.com/streamingfast/firehose-core/cmd/firecore@536bcd99495f42a27b67b340ccf8416f0fc967bf

# Build dummy-blockchain
go install github.com/streamingfast/dummy-blockchain@latest
```

A sample firecore configuration is provided in `devel/firecore.config.yaml` that uses dummy-blockchain as the reader node and configures the SDS plugins (auth, session, metering) to connect to the provider gateway on `:9001`.

Sanity check (what to look for in logs):
- Good:
  - `auth plugin instantiation {"plugin_kind": "sds"}`
  - `MeteringConfig:"sds://localhost:9001?..."`
  - `processing block {"block_number": ...}` (dummy-blockchain is running)
- Bad:
  - `executable file not found in $PATH` for `dummy-blockchain` → ensure `$(go env GOPATH)/bin` is on `PATH`
  - errors about unknown `sds` plugin kind/scheme → your `firecore` binary is too old

## Architecture

### Overview

The project implements a payment layer for Substreams data streaming. Consumers pay providers for streamed blockchain data using the TAP (Timeline Aggregation Protocol) V2 on Horizon.

```
┌─────────────┐         ┌──────────────────┐         ┌───────────────────┐
│  Substreams │ ──────► │ Consumer Sidecar │         │ substreams-tier1  │
│   Client    │         │    (signing)     │         │    (provider)     │
└─────────────┘         └────────┬─────────┘         └─────────┬─────────┘
                                 │                             │
                                 │       RAV in headers        │
                                 └─────────────────────────────┤
                                                               ▼
                                                      ┌────────────────┐
                                                      │Provider Gateway│
                                                      │ (validation)   │
                                                      └───────┬────────┘
                                                              │
                                                              ▼
                                                      ┌────────────────┐
                                                      │  On-Chain      │
                                                      │  Settlement    │
                                                      └────────────────┘
```

### Components

#### Consumer Sidecar (`consumer/sidecar`)

Runs alongside the Substreams client and handles:
- Payment session initialization
- RAV signing using EIP-712 typed data
- Usage tracking and reporting

```bash
# Using devenv addresses (User1 as signer)
sds consumer sidecar \
  --plaintext \
  --signer-private-key 0xdd02564c0e9836fb570322be23f8355761d4d04ebccdc53f4f53325227680a9f \
  --collector-address 0x1d01649b4f94722b55b5c3b3e10fe26cd90c1ba9
```

#### Provider Gateway (`provider/gateway`)

Runs alongside the data provider (substreams-tier1) and handles:
- RAV validation and signature verification
- Session management and usage tracking
- Escrow balance queries
- Payment status monitoring

**Usage metering note:** the provider gateway does **not** meter bytes/blocks directly from the Substreams/Firehose stream. Usage is reported via `PaymentGatewayService.PaymentSession` `usage_report` or through the Firehose plugin services (`sds://` URI scheme). The Firehose provider plugins handle authentication, session management, and usage reporting for production integrations.

**Repository Options:**

The provider gateway supports two repository backends via the `--repository-dsn` flag:

- **In-memory** (default): `--repository-dsn="inmemory://"` - For development/testing
- **PostgreSQL**: `--repository-dsn="psql://user:pass@host:port/dbname?sslmode=disable"` - For production

```bash
# Using devenv addresses with in-memory repository (default)
sds provider gateway \
  --plaintext \
  --service-provider 0xa6f1845e54b1d6a95319251f1ca775b4ad406cdf \
  --collector-address 0x1d01649b4f94722b55b5c3b3e10fe26cd90c1ba9 \
  --escrow-address 0xfc7487a37ca8eac2e64cba61277aa109e9b8631e \
  --rpc-endpoint <RPC_URL_FROM_DEVENV>

# Using PostgreSQL repository
sds provider gateway \
  --repository-dsn "psql://dev-node:changeme@localhost:5432/dev-node?sslmode=disable" \
  --service-provider 0xa6f1845e54b1d6a95319251f1ca775b4ad406cdf \
  --collector-address 0x1d01649b4f94722b55b5c3b3e10fe26cd90c1ba9 \
  --escrow-address 0xfc7487a37ca8eac2e64cba61277aa109e9b8631e \
  --rpc-endpoint <RPC_URL_FROM_DEVENV>
```

#### Horizon Package (`horizon/`)

Core RAV/Receipt implementation:
- EIP-712 domain configuration for GraphTallyCollector
- Receipt and RAV types with signing/verification
- Receipt aggregation with validation rules

#### Sidecar Package (`sidecar/`)

Shared components between consumer and provider:
- Session management
- Pricing configuration (supports small decimal values like "0.000001" GRT)
- Proto converters for RAV/Address/BigInt types
- Escrow balance querying

### Protocol Buffers

Service definitions are in `proto/`:
- `common/v1/types.proto`: Shared types (Address, BigInt, RAV, Usage, etc.)
- `consumer/v1/consumer.proto`: ConsumerSidecarService
- `provider/v1/gateway.proto`: PaymentGatewayService

## References

- [EIP-712: Typed structured data hashing and signing](https://eips.ethereum.org/EIPS/eip-712)
- [The Graph Protocol](https://thegraph.com/)
- [Timeline Aggregation Protocol (TAP)](https://github.com/semiotic-ai/timeline-aggregation-protocol)
- [Graph Protocol Contracts (Horizon)](https://github.com/graphprotocol/contracts/tree/main/packages/horizon)
