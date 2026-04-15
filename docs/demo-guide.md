# Demo Guide

This guide walks you through running the full Substreams Data Service stack locally — a real end-to-end paid Substreams session on a local chain with real contract interactions.

By the end you will have:
- A local Anvil chain with deployed TAP/Horizon contracts and pre-funded escrow
- A running Provider Gateway, Consumer Sidecar, and Firehose Core instance
- A Substreams client streaming data through the full payment loop

---

## Prerequisites

Install the following before starting.

### Go 1.25+

```bash
go version  # must be >= 1.25
```

### Docker

Docker is used for PostgreSQL, Redis, and pgweb. Make sure `docker compose` (v2) is available:

```bash
docker compose version
```

### reflex

Used to orchestrate the dev stack with auto-restart on code changes:

```bash
go install github.com/cespare/reflex@latest
```

### dummy-blockchain

A lightweight fake blockchain used by Firehose Core in the dev environment:

```bash
go install github.com/streamingfast/dummy-blockchain@latest
```

### firecore

The Firehose Core binary. Follow the [firecore installation instructions](https://github.com/streamingfast/firehose-core) for your platform.

> **Tip:** After installing, verify with `firecore --version`.

### direnv (optional but recommended)

Automatically loads `.envrc` when you enter the directory:

```bash
brew install direnv   # macOS
# or: https://direnv.net/docs/installation.html
```

---

## 1. Clone and set up

```bash
git clone https://github.com/graphprotocol/substreams-data-service
cd substreams-data-service

# Check out the MVP scope branch (Juan's branch — has the latest reflex configs and MVP work)
git checkout juanmardefago/mvp-scope
```

### Set up your PATH (with direnv)

Create an `.envrc` file in the repo root:

```bash
cat > .envrc << 'EOF'
PATH_add "$(pwd)/devel"
PATH_add "$(go env GOPATH)/bin"
EOF

direnv allow
```

This adds the `devel/` wrapper scripts and your Go bin to `$PATH`, so you can run `sds` and `sds_sink` directly.

> **Without direnv:** prefix commands with `./devel/` (e.g. `./devel/sds devenv`) and make sure `$GOPATH/bin` is in your `$PATH`.

---

## 2. Run the full stack (recommended)

The easiest way to run everything is with `reflex`, which starts all services and restarts them automatically when you change Go or SQL files:

```bash
reflex -c .reflex
```

That's it. Reflex orchestrates the following in order:

| Step | What runs | Where |
|------|-----------|-------|
| 1 | Docker Compose — PostgreSQL, Redis, pgweb | Background |
| 2 | Database migrations | One-shot |
| 3 | `sds devenv` — Anvil chain + contract deployment | Foreground |
| 4 | Consumer Sidecar | `:9002` |
| 5 | Provider Gateway | `:9001` |
| 6 | Firehose Core | `:10016` (Substreams), `:10015` (Firehose) |

Wait until you see log output from all services before proceeding to step 3.

**Expected output (abridged):**

```
Starting Docker Compose (PostgreSQL, Redis, pgweb)...
Starting Devenv...
[devenv] Anvil started on http://localhost:58545
[devenv] Contracts deployed:
[devenv]   GraphTallyCollector: 0x1d01649b4f94722b55b5c3b3e10fe26cd90c1ba9
[devenv]   PaymentsEscrow:      0xfc7487a37ca8eac2e64cba61277aa109e9b8631e
[devenv]   SubstreamsDataService: 0x37478fd2f5845e3664fe4155d74c00e1a4e7a5e2
[devenv] Demo escrow funded. Ready.
Starting Consumer Sidecar ...
Starting Provider Gateway (with PostgreSQL)...
Restarting firehose-core instance...
Starting Firehose Core
  Substreams: sds_sink -e https://localhost:10016 --insecure
```

> **pgweb** is available at [http://localhost:8081](http://localhost:8081) — you can inspect the PostgreSQL database in your browser as sessions and RAVs are created.

---

## 3. Stream data through the payment loop

Once the stack is running, use the `sds_sink` wrapper to run a Substreams package against it:

```bash
# Stream the map_clocks module from the common package, starting from block 1
sds_sink run common@v0.1.0 map_clocks -s 1
```

Or, starting from the latest block:

```bash
sds_sink run common@v0.1.0 map_clocks -s -1
```

`sds_sink` connects to the Consumer Sidecar on `:9002`, which initiates a paid session with the Provider Gateway, which in turn authorises the Firehose Core connection. You are now streaming data through the full payment loop.

**What you should see:**

```
[consumer-sidecar] InitSession called
[consumer-sidecar] Session started: <session-id>
[provider-gateway] StartSession: consumer=0xe908... escrow_balance=10000 GRT
[provider-gateway] PaymentSession opened
[firehose] block 1 streamed
[firehose] block 2 streamed
...
[provider-gateway] RAV threshold reached — requesting RAV
[consumer-sidecar] Signing RAV for <amount> units
[provider-gateway] RAV accepted and stored
...
```

---

## 4. Manual setup (alternative to reflex)

If you prefer to run each service in a separate terminal, or if reflex is not available, follow these steps in order.

### Terminal 1: Docker + database migrations + devenv

```bash
# Start PostgreSQL, Redis, and pgweb
docker compose up -d

# Wait for PostgreSQL to be healthy, then apply migrations
./devel/migrate.sh up

# Start Anvil + deploy contracts + fund demo escrow
./devel/sds devenv
```

Leave this terminal running. The devenv process holds the Anvil chain; killing it loses all chain state.

**Devenv output to confirm success:**

```
Contracts deployed successfully.
Demo state prepared — escrow funded.
RPC available at http://localhost:58545
```

### Terminal 2: Consumer Sidecar

```bash
DLOG=".*=debug" ./devel/sds consumer sidecar \
  --grpc-listen-addr=:9002 \
  --plaintext \
  --signer-private-key=0x0bba7d355d1750fce9756af7887e826e8071a56d9d8e327f546b1f34c78f9281 \
  --collector-address=0x1d01649b4f94722b55b5c3b3e10fe26cd90c1ba9
```

Wait for: `Consumer Sidecar listening on :9002`

### Terminal 3: Provider Gateway

```bash
DLOG=".*=debug" ./devel/sds provider gateway \
  --repository-dsn="psql://dev-node:changeme@localhost:5432/dev-node?sslmode=disable" \
  --grpc-listen-addr=:9001 \
  --plaintext \
  --service-provider=0xa6f1845e54b1d6a95319251f1ca775b4ad406cdf \
  --collector-address=0x1d01649b4f94722b55b5c3b3e10fe26cd90c1ba9 \
  --escrow-address=0xfc7487a37ca8eac2e64cba61277aa109e9b8631e \
  --rpc-endpoint=http://localhost:58545
```

Wait for: `Provider Gateway listening on :9001`

> **In-memory mode:** Omit `--repository-dsn` to use the in-memory backend (no Docker needed, but state is lost on restart).

### Terminal 4: Firehose Core

```bash
firecore -c devel/firecore.config.yaml -d ./devel/.firehose start
```

Wait for: `Substreams Tier1 listening on :10016`

### Terminal 5: Stream data

```bash
sds_sink run common@v0.1.0 map_clocks -s 1
```

---

## Demo accounts and contract addresses

All addresses and keys in the dev environment are **deterministic** — the same every time `sds devenv` runs. Do not use these in production.

### Contract addresses

| Contract | Address |
|----------|---------|
| GraphTallyCollector | `0x1d01649b4f94722b55b5c3b3e10fe26cd90c1ba9` |
| PaymentsEscrow | `0xfc7487a37ca8eac2e64cba61277aa109e9b8631e` |
| SubstreamsDataService | `0x37478fd2f5845e3664fe4155d74c00e1a4e7a5e2` |

### Test accounts

Each account starts with 10 ETH and 10,000 GRT on the local chain.

| Role | Address | Private Key |
|------|---------|-------------|
| Service Provider | `0xa6f1845e54b1d6a95319251f1ca775b4ad406cdf` | — |
| Payer | `0xe90874856c339d5d3733c92ea5acadc6014b34d5` | — |
| Demo Signer | `0x82b6f0bbbab50f0ddc249e5ff60c6dc64d55340e` | `0x0bba7d355d1750fce9756af7887e826e8071a56d9d8e327f546b1f34c78f9281` |

---

## Oracle mode (alternative reflex config)

The default `.reflex` config runs in **direct mode** — the consumer sidecar connects directly to the provider gateway with no discovery step.

An **oracle mode** config is also available. In oracle mode, the consumer sidecar contacts the oracle first to discover available providers and pricing before initiating a session.

```bash
reflex -c .reflex.oracle
```

This adds:
- Oracle service on `:9004`
- Consumer sidecar uses `devel/consumer-sidecar.oracle.yaml` for configuration

> **Note:** The oracle implementation is partial (MVP-005). Oracle mode works for local dev but the full permissionless provider discovery flow is not yet complete.

---

## Database

pgweb provides a browser UI for the PostgreSQL database:

**URL:** [http://localhost:8081](http://localhost:8081)

Tables of interest:

| Table | What's in it |
|-------|-------------|
| `sessions` | All sessions — status, consumer, provider, timestamps |
| `workers` | Active worker assignments per session |
| `usage_events` | Raw metered usage events from the plugin gateway |
| `quota_usage` | Per-session quota consumption |
| `ravs` | Accepted RAVs — the collectible payment records |

---

## Database migrations

```bash
./devel/migrate.sh up          # Apply all pending migrations
./devel/migrate.sh down        # Roll back one migration
./devel/migrate.sh version     # Show current schema version
./devel/migrate.sh new <name>  # Create a new migration file pair
```

---

## Running integration tests

Integration tests use Docker (real chain, real contracts). Make sure Docker is running:

```bash
go test ./test/integration/...
```

Unit tests (no Docker needed):

```bash
go test ./...
```

---

## Troubleshooting

**`firecore` not found**

Make sure `firecore` is installed and on your `$PATH`. See the [firecore repository](https://github.com/streamingfast/firehose-core) for installation instructions.

**`dummy-blockchain` not found**

```bash
go install github.com/streamingfast/dummy-blockchain@latest
```

Ensure `$(go env GOPATH)/bin` is in your `$PATH`.

**Port already in use**

Check which process is using the port:

```bash
lsof -i :<port>
```

Kill the process or change the `--grpc-listen-addr` flag on the relevant service.

**`migrate.sh up` fails with "already up to date"**

This is fine — it means migrations have already been applied. The reflex config suppresses this error automatically.

**Consumer sidecar shows `NeedMoreFunds` immediately**

The demo escrow was not funded. Restart `sds devenv` — it re-deploys and re-funds the escrow on each run.

**Firehose Core exits immediately**

The Plugin Gateway (Provider Gateway side) must be running before Firehose Core starts. In manual mode, make sure Terminal 3 is ready before starting Terminal 4.

**pgweb shows empty tables**

No sessions have been created yet. Run `sds_sink` to initiate a paid stream and watch the tables populate.
