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

### Quick Start

The `devel/sds` wrapper automatically compiles and runs the CLI on each invocation. Using [direnv](https://direnv.net/), create an `.envrc` file to add it to your PATH:

```bash
echo 'path_add PATH "`pwd`/devel"' > .envrc && direnv allow
```

Now `sds` invokes `devel/sds` directly. Use [reflex](https://github.com/cespare/reflex) to start everything (`devenv`, Consumer Sidecar, Provider Gateway & Firehose Stack) and auto-restart services (Consuemr Sidecar and Provider Gateway) on code changes:

```bash
reflex -c .reflex
```

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
git clone https://github.com/streamingfast/firehose-core
cd firehose-core && go install ./cmd/firecore && cd ..

# Build dummy-blockchain
git clone https://github.com/streamingfast/dummy-blockchain
cd dummy-blockchain && go install . && cd ..
```

A sample firecore configuration is provided in `devel/firecore.config.yaml` that uses dummy-blockchain as the reader node and configures the SDS plugins (auth, session, metering) to connect to the provider gateway on `:9001`.

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
  --signer-private-key 0xdd02564c0e9836fb570322be23f8355761d4d04ebccdc53f4f53325227680a9f \
  --collector-address 0x1d01649b4f94722b55b5c3b3e10fe26cd90c1ba9
```

#### Provider Gateway (`provider/sidecar`)

Runs alongside the data provider (substreams-tier1) and handles:
- RAV validation and signature verification
- Session management and usage tracking
- Escrow balance queries
- Payment status monitoring

**Usage metering note:** the provider gateway does **not** meter bytes/blocks directly from the Substreams/Firehose stream. Usage is reported via `PaymentGatewayService.PaymentSession` `usage_report` or through the Firehose plugin services (`sds://` URI scheme). The Firehose provider plugins handle authentication, session management, and usage reporting for production integrations.

```bash
# Using devenv addresses
sds provider gateway \
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
