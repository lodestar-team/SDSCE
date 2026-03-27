# MVP Gap Analysis

Drafted: 2026-03-12  
Revised: 2026-03-26

This document maps the current repository state against the MVP defined in `docs/mvp-scope.md`.

It reflects:

- the 2026-03-24 MVP scope rewrite
- the current `plans/mvp-implementation-backlog.md`
- provider/runtime work that landed in StreamingFast commits `5ffca3d` through `1416020`

Unlike the MVP scope document, this file is expected to change frequently.

Status values used here:

- `implemented`
- `partial`
- `missing`
- `open_question`

## Summary

The repository still has a strong technical foundation:

- Horizon V2 / TAP signing, verification, and aggregation are implemented and tested
- local chain/contracts and integration tests are in place
- consumer sidecar and provider-side payment/session surfaces exist
- sidecar-to-gateway session start and payment-session flow exist
- provider-side plugin services exist for auth, session, and usage

Provider-side runtime foundations are materially stronger than before:

- DSN-backed repository selection now exists
- PostgreSQL-backed provider persistence foundation now exists
- the provider runtime is now shaped as a public Payment Gateway plus a private Plugin Gateway
- firecore/plugin integration scaffolding is stronger than it was when this document was first drafted

Validation infrastructure is also healthier than before:

- PostgreSQL repository tests no longer rely on an author-specific absolute migration path
- integration bootstrap no longer depends on localhost port `58545` being free in order to start the shared devenv
- `go test ./...` is no longer blocked by those two non-product validation failures

The biggest remaining MVP gaps are now:

- standalone oracle/discovery component
- consumer-side Substreams-compatible endpoint/proxy behavior
- provider collection lifecycle persistence and inspection/collection APIs
- low-funds enforcement in the real live stream path
- operator funding and collection tooling
- authenticated admin/operator surfaces
- finalized observability floor

## Acceptance Scenario Status

| Scenario | Status | Notes |
| --- | --- | --- |
| A. Discovery to paid streaming | `partial` | Paid session flow and provider runtime foundations exist, but the standalone oracle is still missing and the consumer sidecar is not yet the Substreams-compatible ingress described by the scope |
| B. Fresh session after interruption | `partial` | Fresh-session semantics are implemented in the init contract, but broader real-path interruption validation still remains |
| C. Low funds during streaming | `missing` | Session-local low-funds handling in the real live stream path is still backlog work |
| D. Provider restart without losing collectible state | `partial` | Provider persistence is no longer purely in-memory because PostgreSQL support exists, but collectible/collection lifecycle tracking is still incomplete |
| E. Manual funding flow | `partial` | Demo-oriented setup/funding helpers exist, but real operator-grade funding CLI flows do not |
| F. Manual collection flow | `missing` | RAV tooling exists, but provider-backed settlement inspection and collect workflow are not implemented |
| G. Secure deployment posture | `partial` | TLS hooks and provider public/private split exist, but authenticated admin/operator surfaces remain unfinished |

## Component Status

### Core Payment / Horizon

Status: `implemented`

Evidence:

- `horizon/`
- `test/integration/rav_test.go`
- `test/integration/collect_test.go`
- `test/integration/authorization_test.go`

Notes:

- This area remains strong enough to support the rest of the MVP work.

### Consumer Sidecar

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

What is still missing for MVP:

- the real user-facing integration is still wrapper-centric rather than endpoint-centric
- finalized low-funds stop/pause handling in the real usage path

### Provider Gateway

Status: `partial`

Evidence:

- `provider/gateway/gateway.go`
- `provider/gateway/handler_start_session.go`
- `provider/gateway/handler_payment_session.go`
- `provider/gateway/handler_submit_rav.go`
- `provider/gateway/handler_get_session_status.go`

What already exists:

- public payment gateway
- session start
- bidirectional payment session
- RAV validation and authorization checks
- basic runtime/session status inspection
- repository-backed session state foundation

What is still missing for MVP:

- collection lifecycle state
- live low-funds logic during active streaming
- authenticated admin/operator surfaces

### Provider Plugin Services

Status: `partial`

Evidence:

- `provider/auth/service.go`
- `provider/session/service.go`
- `provider/usage/service.go`
- `provider/plugin/gateway.go`

What already exists:

- private plugin gateway
- auth, session, and usage services for `sds://`
- typed session ID propagation through plugin/runtime requests
- provider-authoritative metering path foundation

What is still missing for MVP:

- full live-provider-path acceptance in production-like usage
- finalized byte-billing/runtime contract documentation
- live stop/pause behavior enforced in the provider stream lifecycle

### Oracle

Status: `missing`

What MVP requires:

- standalone service
- manual whitelist
- canonical pricing for the curated provider set
- eligible provider set plus recommended provider response
- selected provider control-plane endpoint return
- authenticated admin/governance actions

### Provider Persistence

Status: `partial`

Current state:

- provider persistence is no longer only in-memory
- PostgreSQL repository support exists
- the provider gateway can instantiate repositories via DSN
- migrations and repository tests exist
- the current durable model already covers runtime/session state plus the latest accepted RAV snapshot for a session

Evidence:

- `provider/gateway/repository.go`
- `provider/repository/psql/`
- `provider/gateway/REPOSITORY.md`
- `docs/provider-persistence-boundary.md`

What is still missing for MVP:

- explicit collection lifecycle persistence
- provider-backed collectible/collect_pending/collected tracking
- acceptance-level proof for the full restart/collectible scenario

Notes:

- `MVP-003` now freezes the boundary between runtime/session persistence and later settlement lifecycle tracking so `MVP-008` and `MVP-029` do not overlap semantically.
- Repository validation is now portable across checkout paths because PostgreSQL test migrations resolve from repo-local state rather than a machine-specific absolute path.

### Consumer Data-Plane Compatibility

Status: `missing`

Evidence:

- `proto/graph/substreams/data_service/consumer/v1/consumer.proto`
- `cmd/sds/impl/sink_run.go`

Current state:

- the consumer sidecar is still used through SDS-specific RPC plus wrapper orchestration
- `sds sink run` is the closest real-path integration today

What is still missing for MVP:

- a Substreams-compatible consumer-side endpoint/proxy that hides SDS discovery/session/payment coordination behind the data-plane ingress

### Validation Infrastructure

Status: `implemented`

Evidence:

- `provider/repository/psql/database_test.go`
- `provider/repository/psql/migrations_path.go`
- `test/integration/main_test.go`

What already exists:

- PostgreSQL repository tests resolve migrations from repo-local state
- integration bootstrap selects a safe devenv RPC port instead of assuming fixed host port `58545`
- full-repo validation is no longer blocked by those two environment-specific failures

### Funding CLI

Status: `partial`

Current state:

- local/demo funding setup exists

Evidence:

- `cmd/sds/demo_setup.go`

What is still missing for MVP:

- operator-oriented approve/deposit/top-up workflow beyond local demo assumptions

### Settlement / Collection CLI

Status: `missing`

Current state:

- local RAV creation/inspection tooling exists, but it is not provider-backed settlement tooling

Evidence:

- `cmd/sds/tools_rav.go`

What is still missing for MVP:

- inspect collectible accepted RAV data from the provider
- fetch settlement-relevant data from the provider
- craft/sign/submit `collect()` transaction locally
- retry-safe operator workflow

### Transport Security

Status: `partial`

Evidence:

- `cmd/sds/impl/provider_gateway.go`
- `cmd/sds/consumer_sidecar.go`
- `sidecar/server_transport.go`
- `provider/plugin/gateway.go`

What already exists:

- plaintext vs TLS transport configuration paths
- provider public/private network split for payment gateway vs plugin gateway

What is still missing for MVP:

- finalized secure deployment defaults across all relevant surfaces
- authenticated admin/operator surfaces
- validated TLS-by-default posture for the full MVP deployment shape

### Observability

Status: `partial`

What already exists:

- structured logging
- health endpoints
- basic runtime/status inspection

What is still missing for MVP:

- final MVP decision on metrics endpoints
- better operator-facing inspection for payment, runtime, and collection state

## Current Implementation Highlights

The most important recent status changes versus the original draft are:

- Provider persistence should no longer be treated as fully missing.
  - The repo now includes PostgreSQL-backed repository code, DSN-based selection, migrations, and tests.
- Provider runtime shape is more concrete than before.
  - The repo now explicitly separates a public Payment Gateway from a private Plugin Gateway.
- Real-path integration scaffolding is stronger.
  - The repo now includes stronger firecore/plugin integration setup and a `TestFirecore` scaffold, even though that path is not yet MVP-complete.
- Consumer-side MVP UX is still notably behind the revised scope.
  - The code still reflects a control-plane RPC plus wrapper model rather than the endpoint/proxy boundary the scope now requires.

## Backlog Alignment

The current MVP gaps now align with the rewritten MVP backlog as follows.

Oracle and consumer ingress:

- `MVP-005`
- `MVP-007`
- `MVP-017`

Provider runtime and payment control:

- `MVP-010`
- `MVP-011`
- `MVP-012`
- `MVP-014`
- `MVP-015`
- `MVP-016`
- `MVP-031`

Persistence and settlement:

- `MVP-003`
- `MVP-008`
- `MVP-009`
- `MVP-029`

Tooling and operations:

- `MVP-018`
- `MVP-019`
- `MVP-020`
- `MVP-032`

Security and observability:

- `MVP-021`
- `MVP-022`
- `MVP-023`
- `MVP-024`
- `MVP-028`
- `MVP-030`

Validation and docs:

- `MVP-025`
- `MVP-026`

The gap analysis and the backlog now agree that:

- pricing authority is resolved for MVP
- reconnect/payment-session reuse is not an MVP target
- the remaining open questions are observability and auth only

## Open Questions Carrying Risk

These are no longer architecture-blocking for the main SDS flow, but they do still block clean closure of the security/admin and observability parts of MVP.

- metrics endpoints vs logs-plus-status-only for MVP observability
- exact admin/operator authentication mechanism

## Recommended Usage

Use `docs/mvp-scope.md` as the stable target-state reference.

Use this file to:

- assess current progress
- identify MVP gaps
- map current repo status to the MVP target
- keep implementation status current without rewriting the MVP scope itself

Use `plans/mvp-implementation-backlog.md` as the concrete task backlog aligned to the revised MVP scope.
