# MVP Gap Analysis

Drafted: 2026-03-12

This document maps the current repository state against the MVP defined in `docs/mvp-scope.md`.

Unlike the MVP scope document, this file is expected to change frequently.

Status values used here:

- `implemented`
- `partial`
- `missing`
- `open_question`

## Summary

The repository already has a strong technical foundation:

- Horizon V2 / TAP signing, verification, and aggregation are implemented and tested
- local chain/contracts and integration tests are in place
- consumer sidecar and provider gateway exist
- sidecar-to-gateway session start and payment-session flow exist
- provider-side plugin services exist for auth, session, and usage

The main MVP gaps are not the cryptographic/payment core. They are the surrounding system capabilities required to make SDS a usable product stack:

- standalone oracle/discovery component
- real provider and consumer production-path integration completion
- provider-side durable payment/collection persistence
- live low-funds enforcement in the real stream path
- funding and settlement CLI workflows
- authenticated admin/operator surfaces

## Acceptance Scenario Status

| Scenario | Status | Notes |
| --- | --- | --- |
| Discovery to paid streaming | `partial` | Paid session flow exists, but standalone oracle is missing and real production-path integration is not complete |
| Reconnect and resume | `partial` | Resume with `existing_rav` exists; provider-authoritative recovery during normal handshake is not finalized |
| Low funds during streaming | `missing` | Session-local low-funds decisions during active streaming are still backlog work |
| Provider restart without losing collectible state | `missing` | Provider accepted RAV state is still in-memory today |
| Manual funding flow | `partial` | Local/demo helper exists via `sds demo setup`, but general MVP funding CLI workflow is not implemented |
| Manual collection flow | `missing` | No MVP settlement inspection/collection CLI flow yet |
| Secure deployment posture | `partial` | TLS hooks exist, but admin authentication and final secure operational surfaces are not complete |

## Component Status

### Core Payment / Horizon

Status: `implemented`

Evidence:

- `horizon/`
- `test/integration/rav_test.go`
- `test/integration/collect_test.go`
- `test/integration/authorization_test.go`

Notes:

- This area is already strong enough to support the rest of MVP work.

### Consumer Sidecar RPC Surface

Status: `partial`

Evidence:

- `consumer/sidecar/sidecar.go`
- `consumer/sidecar/handler_init.go`
- `consumer/sidecar/handler_report_usage.go`
- `consumer/sidecar/handler_end_session.go`

What already exists:

- session init
- usage reporting
- end session
- payment-session loop wiring to provider gateway
- existing-RAV-based resumption

What is still missing for MVP:

- finalized provider-authoritative reconnect flow in the normal handshake
- completion of real client integration path
- finalized handling around low-funds stop/pause in real usage path

### Provider Gateway RPC Surface

Status: `partial`

Evidence:

- `provider/gateway/gateway.go`
- `provider/gateway/handler_start_session.go`
- `provider/gateway/handler_payment_session.go`
- `provider/gateway/handler_submit_rav.go`
- `provider/gateway/handler_get_session_status.go`

What already exists:

- session start
- bidirectional payment session
- RAV validation and authorization checks
- basic session status inspection

What is still missing for MVP:

- durable accepted-RAV persistence
- collection lifecycle state
- low-funds logic during active streaming
- authenticated admin/operator surfaces

### Provider Plugin Services

Status: `partial`

Evidence:

- `provider/auth/service.go`
- `provider/session/service.go`
- `provider/usage/service.go`
- `provider/plugin/`

What already exists:

- auth, session, and usage services for `sds://`
- provider-authoritative metering path foundation

What is still missing for MVP:

- full real provider path integration and validation against production-like usage
- finalized byte-billing semantics in the complete runtime path
- stop/pause behavior enforced in the live stream path

### Oracle

Status: `missing`

What MVP requires:

- standalone service
- manual whitelist
- eligible provider set plus recommended provider response
- authenticated admin/governance actions

### Provider Persistence

Status: `missing`

Current state:

- provider repository is in-memory
- accepted RAV/session state is lost on restart

Evidence:

- `provider/repository/inmemory.go`
- `provider/repository/repository.go`

What MVP requires:

- durable provider-side state for accepted collectible RAVs
- settlement lifecycle state
- persistence across restarts

### Funding CLI

Status: `partial`

Current state:

- local/demo funding helper exists

Evidence:

- `cmd/sds/demo_setup.go`

What MVP requires:

- operator-oriented approve/deposit/top-up workflow beyond local demo assumptions

### Settlement / Collection CLI

Status: `missing`

What MVP requires:

- inspect collectible accepted RAV data
- fetch settlement-relevant data from provider
- craft/sign/submit `collect()` transaction locally
- retry-safe operator workflow

### Transport Security

Status: `partial`

Evidence:

- `cmd/sds/provider_gateway.go`
- `cmd/sds/consumer_sidecar.go`
- `sidecar/server_transport.go`
- `provider/plugin/plugin.go`

What already exists:

- plaintext vs TLS transport configuration paths

What is still missing for MVP:

- finalized secure deployment defaults and operational guidance
- authenticated admin/operator surfaces

### Observability

Status: `partial`

What already exists:

- structured logging
- health endpoints
- status inspection basics

What is still missing for MVP:

- final MVP decision on metrics endpoints
- better operator-facing inspection for payment/collection state

## Backlog Alignment

The largest currently tracked backlog items that still map directly to MVP are:

- `SDS-008` Define and document `metadata` schema + encoding
- `SDS-016` Implement `NeedMoreFunds` loop + Continue/Stop/Pause
- `SDS-020` Add signing thresholds
- `SDS-021` Decide/implement on-chain collection workflow
- `SDS-022` Track outstanding RAVs across concurrent streams
  - note: full aggregate concurrent-stream correctness is no longer assumed to be MVP-critical
- `SDS-024` Add durable state storage
- `SDS-025` Add transport security + authn/authz
- `SDS-026` Add observability
- `SDS-028` Define payment header format
- `SDS-029` Integrate provider gateway into tier1 provider
- `SDS-030` Integrate consumer sidecar into substreams client
- `SDS-038` Make `sds sink run` the primary end-to-end demo (STOP-aware)
- `SDS-039` Document/enforce required firehose-core version for `sds://` plugins

Additional MVP work not yet clearly represented as a complete deliverable set in the existing backlog:

- standalone oracle component
- authenticated provider/oracle admin surfaces
- operator-oriented funding CLI
- operator-oriented settlement inspection and collection CLI
- provider-authoritative reconnect flow folded into the normal handshake

## Open Questions Carrying Risk

These are not implementation gaps yet, but unresolved design points that could change scope or interfaces:

- chain/network derivation from package vs explicit input
- pricing authority between oracle metadata and provider handshake
- canonical payment identity and `collection_id` reuse semantics
- metrics endpoints vs logs-plus-status-only for MVP observability
- exact admin authentication mechanism

## Recommended Usage

Use `docs/mvp-scope.md` as the stable target-state reference.

Use this file to:

- assess current progress
- identify MVP gaps
- map backlog work to the target MVP
- keep implementation status current without rewriting the MVP scope itself

Use `plans/mvp-implementation-backlog.md` as the concrete task backlog aligned to the revised MVP scope.
