# Substreams Data Service

A Golang implementation of the payment infrastructure for Substreams Data Service on The Graph Network. This project provides sidecar services for both consumers and providers to handle payment sessions, RAV (Receipt Aggregate Voucher) signing, and on-chain settlement via the Graph Protocol's Horizon contracts.

> [!WARNING]
> **Community Edition — experimental and unaffiliated.** This is a community-maintained edition of the Substreams Data Service. It is **not affiliated with, endorsed by, or supported by the Graph Foundation or Edge & Node.** Treat it as **experimental software**: it is unaudited, may change without notice, and carries no warranty. Do not use it with production funds or rely on it for production workloads without conducting your own review.

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

Now `sds` invokes `devel/sds` directly. Use [reflex](https://github.com/cespare/reflex) to start everything (`devenv`, Consumer Sidecar, Provider Gateway & Firehose Stack) and auto-restart services (Consumer Sidecar and Provider Gateway) on code changes:

```bash
reflex -c .reflex
```

Local stack variants:

- `.reflex`: default direct-provider ingress stack
- `.reflex.direct`: same as `.reflex`, but with the mode named explicitly
- `.reflex.demo`: direct-provider ingress stack with a lower provider RAV request threshold for manual settlement testing
- `.reflex.oracle`: oracle-backed provider discovery stack

```bash
reflex -c .reflex.direct
reflex -c .reflex.demo
reflex -c .reflex.oracle
```

The default `.reflex` flow uses the deterministic demo signer that `sds devenv` authorizes automatically. It also passes `--plaintext` and `--plugin-plaintext` explicitly for the local reflex devenv path. Outside local/dev usage, omit those plaintext flags and configure TLS certificate/key files; secure transport is the default CLI posture.

The reflex stacks also enable the private provider operator listener on `localhost:9010` for local inspection and manual collection flows. The reflex command sets explicit local-only bearer tokens unless you override them:

```bash
SDS_OPERATOR_READ_TOKEN=local-operator-read-token
SDS_ADMIN_WRITE_TOKEN=local-admin-write-token
```

Those fallback values are scoped to the provider process. For operator CLI checks in another shell, either export the same values and use `--operator-token-env`, or pass the local token with `--operator-token`. Read-only commands use the read token; mutating collection commands, including `sds provider operator collect`, use the admin token.

The consumer sidecar `--payment-session-roundtrip-timeout` also bounds how long ingress will wait to resolve ambiguous upstream EOF or explicit pending payment-control work against provider session status before classifying the end of stream as an unresolved transport/control failure.

Local endpoints exposed by the direct-provider stack:

- Consumer sidecar ingress: `localhost:9002`
- Provider payment gateway: `localhost:9001`
- Provider plugin gateway: `localhost:9003` (private `sds://` surface)
- Provider operator gateway: `localhost:9010` (private authenticated operator surface)
- Substreams tier1: `localhost:10016`

The oracle-backed stack exposes the same sidecar/provider endpoints and also starts the local oracle on `localhost:9004`.

Manual validation against the consumer sidecar works with the direct-provider stack:

```bash
substreams run common@v0.1.0 map_clocks \
  -e localhost:9002 \
  --plaintext \
  -s 0 \
  -t +20
```

Read the provider operator status surface:

```bash
sds provider operator sessions list \
  --provider-endpoint localhost:9010 \
  --plaintext \
  --operator-token local-operator-read-token \
  --include-rav
```

```bash
substreams gui common@v0.1.0 map_clocks \
  -e localhost:9002 \
  --plaintext
```

Oracle-backed ingress also works through the same consumer endpoint, but packages that do not declare `package.network` must provide an explicit requested network:

```bash
substreams run common@v0.1.0 map_clocks \
  -e localhost:9002 \
  --plaintext \
  --network mainnet \
  -s 0 \
  -t +20
```

`common@v0.1.0` does not embed a network, so the oracle-backed flow needs `--network mainnet`. If your package already declares `package.network`, you do not need the extra flag.

Oracle-backed ingress requires a client/request path that sends Substreams v3/v4 package and network context. Older v2-only clients will fail with `oracle-backed ingress requires a v3/v4 Substreams request containing package/network context`. Packages without embedded network metadata will fail with `either <substreams_package.network> or <requested_network> is required when <provider_control_plane_endpoint> is not set` unless you pass `--network`. The same limitations apply if you call the gRPC stream directly instead of using the `substreams` CLI.

For Substreams CLI installation and upgrade instructions, use the official docs: <https://docs.substreams.dev/how-to-guides/installing-the-cli>

Use the normal Substreams CLI against the consumer sidecar ingress for local SDS flows:

```bash
substreams gui common@v0.1.0 map_clocks -e localhost:9002 --plaintext
```

### Development Environment

> [!NOTE]
> If you are still having `reflex -c .reflex` running from quick start, your development environment is already running so no need to invoke `sds env`.

The `sds devenv` command starts an Anvil node and deploys Graph Protocol contracts (requires Docker). It deploys the original `PaymentsEscrow`, `GraphPayments`, and `GraphTallyCollector` contracts, plus `SubstreamsDataService` and various mock contracts (GRTToken, Controller, Staking, etc.) for testing. Integration tests use the same devenv via testcontainers.

After deployment, `devenv` also prepares the default local demo state:

- payer escrow funded for the default service provider
- data service provision minimum set to `0`
- default service provider provisioned and registered
- deterministic demo signer authorized for the default payer

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

These are deterministic Anvil/devenv accounts for the local reflex setup only.
They are intentionally documented so the checked-in local stack is reproducible;
never use these keys outside the local test chain.

| Role | Address | Private Key |
|------|---------|-------------|
| Service Provider | `0xa6f1845e54b1d6a95319251f1ca775b4ad406cdf` | `0x41942233cf1d78b6e3262f1806f8da36aafa24a941031aad8e056a1d34640f8d` |
| Payer | `0xe90874856c339d5d3733c92ea5acadc6014b34d5` | `0xe4c2694501255921b6588519cfd36d4e86ddc4ce19ab1bc91d9c58057c040304` |
| User1 | `0x90353af8461a969e755ef1e1dbadb9415ae5cb6e` | `0xdd02564c0e9836fb570322be23f8355761d4d04ebccdc53f4f53325227680a9f` |
| User2 | `0x9585430b90248cd82cb71d5098ac3f747f89793b` | `0xbc3def46fab7929038dfb0df7e0168cba60d3384aceabf85e23e5e0ff90c8fe3` |
| User3 | `0x37305c711d52007a2bcfb33b37015f1d0e9ab339` | `0x7acd0f26d5be968f73ca8f2198fa52cc595650f8d5819ee9122fe90329847c48` |
| Demo Signer | `0x82b6f0bbbab50f0ddc249e5ff60c6dc64d55340e` | `0x0bba7d355d1750fce9756af7887e826e8071a56d9d8e327f546b1f34c78f9281` |

`sds demo setup` no longer mutates the chain. It verifies the default demo-ready state from `sds devenv` and writes `devel/.demo.env` for any manual env-driven workflows, but it is no longer required for the default `.reflex` flow.

### Running Tests

```bash
go test ./...                      # All tests
go test ./test/integration/... -v  # Integration tests (requires Docker)
```

### Running Full System with Firecore

The full local runtime path is validated through a local-first Firecore/dummy-chain workflow. The published `ghcr.io/streamingfast/firehose-core:latest` image was checked on 2026-05-25 and validates against the current SDS provider/plugin contract when embedded in a rebuilt dummy-chain image. The published `ghcr.io/streamingfast/dummy-blockchain:v1.7.7`, `:latest`, and `:1cea671` images still lag that `firehose-core` image, so rebuild `dummy-blockchain` locally and point the Firecore integration tests at that local image.

For the explicit MVP runtime-compatibility contract, validated tuple, and contributor/operator workflow, see [docs/provider-runtime-compatibility.md](docs/provider-runtime-compatibility.md).

Build the local dummy-chain runtime image from a sibling checkout:

```bash
# Build a local dummy-blockchain image on top of the latest published firecore tag.
cd ../dummy-blockchain
docker build \
  --build-arg FIRECORE_VERSION=latest \
  -t ghcr.io/streamingfast/dummy-blockchain:sds-upstream-firecore-latest .

# Run the SDS firecore integration test against the local runtime image.
cd ../data-service
SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE=ghcr.io/streamingfast/dummy-blockchain:sds-upstream-firecore-latest \
  go test ./test/integration -run '^(TestFirecore|TestFirecoreStopsStreamOnLowFunds)$' -v -count=1
```

`TestFirecore` and `TestFirecoreStopsStreamOnLowFunds` default to `ghcr.io/streamingfast/dummy-blockchain:v1.7.7`. Override them with `SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE` when validating locally rebuilt runtimes. `MVP-036` documents the current state: published `firehose-core:latest` is compatible, while the published dummy-chain tags still need to be refreshed or replaced before the default image path can be updated.

A sample firecore configuration is provided in `devel/firecore.config.yaml` that uses dummy-blockchain as the reader node and configures the SDS plugins (auth, session, metering) to connect to the private Plugin Gateway on `:9003`.

Sanity check (what to look for in logs):
- Good:
  - `auth plugin instantiation {"plugin_kind": "sds"}`
  - `MeteringConfig:"sds://localhost:9003?plaintext=true..."`
  - `processing block {"block_number": ...}` (dummy-blockchain is running)
- Bad:
  - `executable file not found in $PATH` for `dummy-blockchain` → ensure `$(go env GOPATH)/bin` is on `PATH`
  - errors about unknown `sds` plugin kind/scheme → your `firecore` binary is too old
  - auth/session/usage contract mismatch against the current SDS provider/plugin gateway → rebuild `dummy-blockchain` on top of a validated `firehose-core` image and rerun `TestFirecore`

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
- Long-lived provider payment/control coordination behind the user-facing ingress

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
- Session management and runtime payment/control from metered usage
- Escrow balance queries
- Payment status monitoring

**Usage metering note:** the provider gateway does **not** meter bytes/blocks directly from the Substreams/Firehose stream. In the supported runtime path, authoritative usage comes from the Firehose provider plugin services (`sds://` URI scheme), which feed provider-originated payment/control decisions back through the long-lived `PaymentSession` stream. Provider-issued runtime `rav_request` messages are answered only on that bound `PaymentSession` stream against the exact requested snapshot. Unary `SubmitRAV` remains a deprecated manual surface and is not part of the intended ingress/runtime flow.

**Repository Options:**

The provider gateway supports two repository backends via the `--repository-dsn` flag:

- **In-memory**: `--repository-dsn="inmemory://"` - Local/demo and test-only; appropriate for a single-process stack
- **PostgreSQL**: `--repository-dsn="psql://user:pass@host:port/dbname?sslmode=disable"` - Persistent and appropriate for deployed gateways that need restart-durable state. Full active/active provider runtime topology remains post-MVP work tracked by `PMVP-003` because live `PaymentSession` bindings are process-local.

For local commands that enable the operator listener, set local-only token env vars first:

```bash
export SDS_OPERATOR_READ_TOKEN=local-operator-read-token
export SDS_ADMIN_WRITE_TOKEN=local-admin-write-token
```

```bash
# Using devenv addresses with in-memory repository (local/demo only)
sds provider gateway \
  --repository-dsn "inmemory://" \
  --plaintext \
  --plugin-plaintext \
  --operator-listen-addr :9010 \
  --operator-read-token-env SDS_OPERATOR_READ_TOKEN \
  --admin-write-token-env SDS_ADMIN_WRITE_TOKEN \
  --service-provider 0xa6f1845e54b1d6a95319251f1ca775b4ad406cdf \
  --collector-address 0x1d01649b4f94722b55b5c3b3e10fe26cd90c1ba9 \
  --escrow-address 0xfc7487a37ca8eac2e64cba61277aa109e9b8631e \
  --rpc-endpoint <RPC_URL_FROM_DEVENV> \
  --data-plane-endpoint "https://localhost:10016?insecure=true"

# Using PostgreSQL repository
sds provider gateway \
  --repository-dsn "psql://dev-node:changeme@localhost:5432/dev-node?sslmode=disable" \
  --plaintext \
  --plugin-plaintext \
  --operator-listen-addr :9010 \
  --operator-read-token-env SDS_OPERATOR_READ_TOKEN \
  --admin-write-token-env SDS_ADMIN_WRITE_TOKEN \
  --service-provider 0xa6f1845e54b1d6a95319251f1ca775b4ad406cdf \
  --collector-address 0x1d01649b4f94722b55b5c3b3e10fe26cd90c1ba9 \
  --escrow-address 0xfc7487a37ca8eac2e64cba61277aa109e9b8631e \
  --rpc-endpoint <RPC_URL_FROM_DEVENV> \
  --data-plane-endpoint "https://localhost:10016?insecure=true"
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
