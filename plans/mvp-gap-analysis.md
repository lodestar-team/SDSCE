# MVP Gap Analysis

Drafted: 2026-03-12  
Revised: 2026-04-01

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

- provider collection lifecycle persistence and inspection/collection APIs
- refreshed published firecore/dummy-blockchain images plus deterministic full-suite runtime validation
- operator funding and collection tooling
- authenticated admin/operator surfaces
- finalized observability floor

## Acceptance Scenario Status

| Scenario | Status | Notes |
| --- | --- | --- |
| A. Discovery to paid streaming | `partial` | The sidecar ingress, provider-originated payment loop, and compatibility contract are documented, but refreshed published firecore/dummy-blockchain images still remain |
| B. Fresh session after interruption | `partial` | Fresh-session semantics are implemented in the init contract, but broader real-path interruption validation still remains |
| C. Low funds during streaming | `partial` | Session-local low-funds stop behavior now reaches both the real sidecar ingress path and the local-first Firecore runtime path, but deterministic full-suite hardening and refreshed published images still remain |
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
- `consumer/sidecar/ingress.go`
- `consumer/sidecar/payment_session_manager.go`
- `consumer/sidecar/handler_init.go`
- `consumer/sidecar/handler_report_usage.go`
- `consumer/sidecar/handler_end_session.go`

What already exists:

- session init
- oracle-backed provider selection with direct-provider override fallback
- package-derived network resolution with explicit fallback only when derivation is unavailable
- Substreams gRPC ingress services on the consumer sidecar (`sf.substreams.rpc.v2/v3/v4`)
- sidecar-owned provider discovery/session bootstrap and upstream stream proxying behind that ingress
- long-lived payment-session control loop bound to the provider session behind that ingress
- provider-originated RAV requests and low-funds control propagated through that ingress/runtime path
- startup-driven ingress config via CLI/YAML, with oracle-first discovery and direct provider override as explicit bypass
- low-funds termination surfaced through the client-facing ingress as runtime `ResourceExhausted`
- wrapper-era `Init` / `ReportUsage` / `EndSession` RPC surfaces remain in-tree only as deprecated transitional paths and are no longer required for the supported runtime flow

What is still missing for MVP:

- broader Substreams compatibility validation remains, including final runtime/acceptance convergence around the sidecar ingress as the default entrypoint
- some real-path acceptance still depends on the Firecore/runtime compatibility caveats tracked separately under `MVP-036` and `MVP-037`

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
- gateway-owned runtime manager for provider-originated session control
- RAV validation and authorization checks
- session-local low-funds detection during metered runtime evaluation
- deterministic cost-based RAV request thresholds in the provider runtime path
- terminal `NeedMoreFunds` response plus payment-issue session termination when live escrow is insufficient
- persisted machine-readable funding metadata for `ok`, `insufficient`, and `unknown` session state
- basic runtime/session status inspection
- repository-backed session state foundation
- provider-originated `rav_request`, `need_more_funds`, and session-control flow over the long-lived `PaymentSession` stream

What is still missing for MVP:

- collection lifecycle state
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

- finalized byte-billing/runtime contract documentation
- broader production-like validation around the current real-path runtime tuple

### Oracle

Status: `implemented`

Evidence:

- `proto/graph/substreams/data_service/oracle/v1/oracle.proto`
- `oracle/config.go`
- `oracle/oracle.go`
- `cmd/sds/impl/oracle.go`
- `oracle/config_test.go`
- `oracle/oracle_test.go`

What already exists:

- standalone oracle service
- deployment-managed manual whitelist/provider metadata config
- canonical pricing by network
- eligible provider set plus deterministic recommended provider response
- selected provider control-plane endpoint return
- no data-plane endpoint resolution in the oracle response

Notes:

- Oracle governance remains deployment-managed internal config for MVP; no writable admin API is required yet.
- Standalone oracle service plus consumer-side oracle discovery are now both implemented, and scenario A remains partial only because of the remaining runtime compatibility/published-image work tracked under `MVP-030`, `MVP-036`, and `MVP-037`.

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

Status: `partial`

Evidence:

- `proto/graph/substreams/data_service/consumer/v1/consumer.proto`
- `cmd/sds/impl/sink_run.go`
- `consumer/sidecar/ingress.go`
- `test/integration/consumer_ingress_test.go`

Current state:

- the consumer sidecar now exposes a Substreams-compatible ingress and can proxy real client streams
- oracle-backed ingress derives provider receiver identity from the oracle-selected provider
- direct provider override remains available with startup-configured receiver identity
- `sds sink run` still exists as transitional scaffolding and the ingress still internalizes the legacy usage-report loop

What is still missing for MVP:

- full replacement of the transitional internal usage-report loop with the provider-originated runtime-payment flow
- broader compatibility/acceptance hardening so the sidecar ingress is the clear default entrypoint for real runtime traffic

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

Notes:

- Shared-state integration flakiness still remains in the full-suite lane and is tracked separately under `MVP-037`.

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
- Session-local low-funds handling is no longer fully missing.
  - The payment-session path now evaluates projected session-local exposure against live escrow, fails open on unknown balance, and terminates the current session with `NeedMoreFunds` when funds are insufficient.
- Deterministic RAV issuance policy is no longer an open runtime gap.
  - The provider now requests new RAVs based on unbaselined `delta_cost` reaching a provider-side `rav_request_threshold`, with a built-in `10 GRT` fallback when not configured.
- Real-path integration scaffolding is stronger.
  - The repo now includes stronger firecore/plugin integration setup and a `TestFirecore` scaffold, even though that path is not yet MVP-complete.
  - The current blocker is now identified more precisely: the prebuilt `dummy-blockchain`/`firecore` runtime used by that scaffold embeds an older SDS snapshot and therefore drifts from the current auth/session/usage plugin contracts implemented in this repo.
  - This is protocol drift caused by SDS contract evolution, not just a generic “firecore test is flaky” issue.
- Consumer-side MVP UX is materially closer to the revised scope.
  - The sidecar now exposes a Substreams-compatible ingress and runs the provider-originated payment/control loop through that path.
  - Legacy wrapper-era usage-report surfaces remain only as deprecated cleanup follow-up under `MVP-038`, not as part of the supported runtime architecture.

## Remaining Backlog Alignment

The remaining MVP gaps now align with the rewritten MVP backlog as follows.

Oracle, consumer ingress, and runtime compatibility:

- `MVP-006`
- `MVP-030`
- `MVP-036`

Provider runtime hardening and cleanup:

- `MVP-040`
- `MVP-041`
- `MVP-037`
- `MVP-038`

Persistence and settlement:

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

Validation and docs:

- `MVP-025`
- `MVP-026`

The gap analysis and the backlog now agree that:

- pricing authority is resolved for MVP
- reconnect/payment-session reuse is not an MVP target
- the remaining runtime follow-ups before shared-state hardening are deterministic ingress termination ordering and explicit `RavRequest` response semantics
- the remaining open question is observability

## Open Questions Carrying Risk

These are no longer architecture-blocking for the main SDS flow, but they do still block clean closure of the security/admin and observability parts of MVP.

- metrics endpoints vs logs-plus-status-only for MVP observability

## Recommended Usage

Use `docs/mvp-scope.md` as the stable target-state reference.

Use this file to:

- assess current progress
- identify MVP gaps
- map current repo status to the MVP target
- keep implementation status current without rewriting the MVP scope itself

Use `plans/mvp-implementation-backlog.md` as the concrete task backlog aligned to the revised MVP scope.
