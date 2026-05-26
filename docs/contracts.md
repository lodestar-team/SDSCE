# Contract And Devenv Reference

This document describes the smart contracts SDS uses for local development,
integration tests, and operator tooling.

## Contract Sources

The local Horizon/devenv stack deploys a mix of upstream Graph Protocol Horizon
contracts, SDS-specific contract code, and mocks:

- `GraphPayments`, `PaymentsEscrow`, and `GraphTallyCollector` artifacts are
  deployed as the Horizon payment stack used by SDS tests and local flows.
- `SubstreamsDataService` is the SDS data-service contract used for provider
  registration and `collect()` calls.
- `MockGRTToken`, `MockController`, `MockStaking`, and other Graph-directory
  mocks provide the minimum registry, token, provision, and protocol plumbing
  required by the local stack.

Artifacts are embedded in:

- `contracts/artifacts/`
- `horizon/devenv/contracts/`

The source Solidity used to build the local artifacts lives in:

- `horizon/devenv/build/contracts/SubstreamsDataService.sol`
- `horizon/devenv/build/contracts/TestMocks.sol`

## Runtime Contract Roles

| Contract | Role |
| --- | --- |
| `SubstreamsDataService` | SDS data-service contract. Registers providers and forwards query-fee collection to `GraphTallyCollector`. |
| `GraphTallyCollector` | Verifies signed RAVs and tracks incremental collection. The EIP-712 domain is `GraphTallyCollector`, version `1`. |
| `PaymentsEscrow` | Holds payer escrow and exposes the balance state used by SDS funding and low-funds flows. |
| `GraphPayments` | Horizon payment distribution contract used by the local payment stack. |
| `MockGRTToken` | ERC20-compatible GRT token for local/dev tests. |
| `MockController` | Contract registry used by Horizon contracts in the local stack. |
| `MockStaking` | Provision/delegation-fee mock used by provider registration and collection validation. |

## Go Packages

Shared contract-facing code lives outside development-only packages:

- `contracts/chain/` sends signed transactions and estimates/calls chain RPCs.
- `contracts/erc20/` wraps token approval/balance/allowance operations.
- `contracts/horizon/` wraps escrow, collector, data-service, and signer proof
  helpers used by CLI/operator flows.
- `horizon/` contains RAV signing, verification, aggregation, and EIP-712
  helpers.
- `horizon/devenv/` owns local Anvil deployment and deterministic demo state.

Development-only contract deployment helpers should remain in `horizon/devenv`.
Code used by production-oriented CLI/operator flows should use `contracts/*`
or other shared packages rather than importing `horizon/devenv`.

## Local Devenv Defaults

`sds devenv` deploys the local contracts, funds deterministic test accounts,
sets the default data-service provision minimum to `0`, registers the default
service provider, and authorizes the deterministic demo signer.

The deterministic addresses and local-only private keys are documented in
`README.md`. They are Anvil/devenv values only and are intentionally public so
the local reflex stack is reproducible.

## Compatibility Points

SDS relies on these contract compatibility points:

- RAV EIP-712 domain: `GraphTallyCollector`, version `1`
- RAV fields and ordering as defined by Horizon `ReceiptAggregateVoucher`
- Go signature representation converted to Solidity `r || s || v` at the ABI
  boundary when needed
- signer authorization checked by the collector/data-service path
- incremental collection using previously collected value tracking
- provider registration and provision checks through `SubstreamsDataService`
  and Horizon staking/payment contracts

Focused coverage lives in:

- `test/integration/rav_test.go`
- `test/integration/collect_test.go`
- `test/integration/authorization_test.go`
- `contracts/horizon/contracts_test.go`
- `contracts/erc20/erc20_test.go`

## Intentional Local Simplifications

The local/devenv contracts are sufficient for SDS MVP acceptance, but they are
not a full Graph Network production deployment. The local stack intentionally
simplifies:

- governance
- staking/slashing mechanics
- protocol pause behavior
- permissionless registry management
- production GRT token economics
- real provider provisioning on public testnets

Public testnet or production deployments must use the actual deployed Horizon
contracts and provision/register the SDS data service in that environment.
