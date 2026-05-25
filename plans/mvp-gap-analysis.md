# MVP Gap Analysis

Drafted: 2026-03-12
Revised: 2026-05-06

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

- acceptance coverage and final documentation refresh

## Acceptance Scenario Status

| Scenario | Status | Notes |
| --- | --- | --- |
| A. Discovery to paid streaming | `partial` | The sidecar ingress, provider-originated payment loop, and compatibility contract are documented; broader acceptance coverage and final docs refresh still remain |
| B. Fresh session after interruption | `partial` | Fresh-session semantics are implemented in the init contract, but broader real-path interruption validation still remains |
| C. Low funds during streaming | `partial` | Session-local low-funds stop behavior now reaches both the real sidecar ingress path and the local-first Firecore runtime path; broader acceptance coverage still remains |
| D. Provider restart without losing collectible state | `partial` | Accepted RAV runtime state, collection lifecycle persistence, authenticated retrieval APIs, and provider operator CLI surfaces now exist; remaining validation work belongs to broader acceptance coverage |
| E. Manual funding flow | `implemented` | Operator-grade funding and signer authorization CLI flows now exist outside local demo assumptions |
| F. Manual collection flow | `implemented` | Provider-backed settlement inspection and `sds provider operator collect` now fetch collectible state, locally sign/submit `SubstreamsDataService.collect`, and drive provider lifecycle state |
| G. Secure deployment posture | `implemented` | TLS is the default server posture, plaintext requires explicit local/dev flags, and authenticated operator/admin surfaces exist |

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
- low-funds termination now resolves ambiguous ingress EOF against provider-persisted session end state, so provider payment issues surface through the client-facing ingress as runtime `ResourceExhausted` without relying on control-loop timing heuristics
- expected finite EOF now also checks explicit provider-reported payment-control pending state before returning cleanly, so final provider RAV/control work is not hidden by upstream stream completion timing
- the wrapper-era `ReportUsage` runtime path has been removed, and the remaining manual `Init` / `EndSession` RPC surfaces are not part of the supported runtime flow

What is still missing for MVP:

- broader Substreams compatibility validation remains, including final runtime/acceptance convergence around the sidecar ingress as the default entrypoint

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
- runtime `rav_request` responses are now validated against the exact in-flight request snapshot on `PaymentSession`, while unary `SubmitRAV` remains only as a deprecated legacy/manual surface for non-runtime flows
- `GetSessionStatus` exposes active/end/payment status plus provider-side payment-control pending state for runtime coordination
- accepted runtime RAV responses commit the signed RAV and covered usage baseline after validation even if the consumer disconnects; post-commit metering refresh is best-effort and does not invalidate the committed RAV

What is still missing for MVP:

- remaining metrics/log correlation polish

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
- Standalone oracle service plus consumer-side oracle discovery are now both implemented; scenario A remains partial because broader acceptance coverage and final docs refresh still remain.

### Provider Persistence

Status: `partial`

Current state:

- provider persistence is no longer only in-memory
- PostgreSQL repository support exists
- the provider gateway can instantiate repositories via DSN
- migrations and repository tests exist
- the current durable model already covers runtime/session state plus the latest accepted RAV snapshot for a session
- narrow repository updates preserve usage aggregates while independently updating keepalive, runtime lifecycle/metadata, or accepted RAV plus baseline state
- keepalive and runtime lifecycle updates are monotonic and non-regressive across in-memory and PostgreSQL backends
- accepted RAV, settlement tuple, usage totals, and baseline state survive reopening the PostgreSQL repository against the same durable schema
- collection lifecycle state is modeled separately from runtime sessions across in-memory and PostgreSQL backends, including `collectible`, `collect_pending`, `collected`, and `collect_failed_retryable`

Evidence:

- `provider/gateway/repository.go`
- `provider/repository/psql/`
- `provider/repository/psql/repository_test.go`
- `provider/gateway/REPOSITORY.md`
- `docs/provider-persistence-boundary.md`

What is still missing for MVP:

- remaining metrics/log correlation polish

Notes:

- `MVP-003` now freezes the boundary between runtime/session persistence and later settlement lifecycle tracking so `MVP-008` and `MVP-029` do not overlap semantically.
- `MVP-008` is closed for the current runtime repository model, `MVP-029` is closed for provider-side collection lifecycle persistence, and `MVP-009`/`MVP-019`/`MVP-020` now provide authenticated retrieval and provider operator tooling.
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
- the supported runtime entrypoint is now the consumer sidecar ingress directly; the deprecated wrapper CLI flow has been removed

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

- Shared-state interference in the low-funds/runtime lane is now addressed by using fresh on-chain identities plus explicit pre-state guards for stateful runtime scenarios rather than snapshot/restore.

### Funding CLI

Status: `implemented`

Current state:

- local/demo funding setup exists
- operator funding status, approve, deposit, and top-up commands exist
- payer sidecar signer proof, authorization status, authorize, thaw, revoke, and cancel-thaw commands exist
- production-facing commands require explicit RPC, chain, contract, payer/receiver, and key inputs

Evidence:

- `cmd/sds/demo_setup.go`
- `cmd/sds/consumer_funding.go`
- `cmd/sds/consumer_signer.go`
- `docs/operator-funding.md`

### Settlement / Collection CLI

Status: `implemented`

Current state:

- Provider-backed operator inspection and manual collection tooling now exists.
- Read-only provider commands inspect sessions, accepted RAVs, collection lifecycle state, and runtime payment/funds state through the authenticated operator API.
- Manual collection fetches exact provider settlement state, signs/submits `SubstreamsDataService.collect()` locally, and drives provider lifecycle state through pending, collected, retryable failure, no-wait, dry-run, and already-collected no-op paths.
- Local RAV creation/inspection tooling still exists as support tooling, but it is not the provider-backed settlement workflow.

Evidence:

- `cmd/sds/provider_operator_sessions.go`
- `cmd/sds/provider_operator_ravs.go`
- `cmd/sds/provider_operator_collections.go`
- `cmd/sds/provider_operator_collect.go`
- `cmd/sds/tools_rav.go`

### Transport Security

Status: `implemented`

Evidence:

- `cmd/sds/impl/provider_gateway.go`
- `cmd/sds/consumer_sidecar.go`
- `sidecar/server_transport.go`
- `provider/plugin/gateway.go`

What already exists:

- plaintext vs TLS transport configuration paths
- provider public/private network split for payment gateway vs plugin gateway
- authenticated provider operator/admin surfaces on a private operator listener
- TLS is the default server posture unless plaintext is explicitly requested
- provider plugin-gateway plaintext requires explicit `--plugin-plaintext` rather than implicit inheritance
- the reflex devenv is the checked-in plaintext exception and passes plaintext flags explicitly
- public/non-dev docs describe TLS as the default posture

### Observability

Status: `partial`

What already exists:

- structured logging
- health endpoints
- basic runtime/status inspection

What is still missing for MVP:

- acceptance coverage across the primary end-to-end scenarios
- final protocol/runtime documentation refresh

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
  - `MVP-036` Published runtime images is now closed as a repo-side verification/documentation task: `ghcr.io/streamingfast/firehose-core:latest` is compatible with the current SDS runtime path when embedded in a rebuilt dummy-chain image.
  - Current image check on 2026-05-25: `ghcr.io/streamingfast/dummy-blockchain:v1.7.7`, `:latest`, and `:1cea671` do not validate the current SDS runtime path without local override tags; a rebuilt dummy-chain image using `--build-arg FIRECORE_VERSION=latest` passes `TestFirecore`.
  - This is protocol drift caused by SDS contract evolution, not just a generic “firecore test is flaky” issue.
- Consumer-side MVP UX is materially closer to the revised scope.
  - The sidecar now exposes a Substreams-compatible ingress and runs the provider-originated payment/control loop through that path.
  - The legacy wrapper-era usage-report surfaces have been removed under `MVP-038`; they are no longer part of the supported runtime architecture.
- Payment-session hardening has closed the main follow-up issues from the runtime-payment lane.
  - Finite EOF handling is now driven by local and provider-reported payment-control pending state, not a fixed delay.
  - Accepted runtime RAV persistence is separated from peer disconnect and post-commit refresh failure.
  - Repository updates for keepalive/runtime/RAV state are narrower and monotonic, reducing stale-write lost-update risk; `MVP-008` now adds restart-focused durable accepted RAV proof coverage.
  - Active Firecore workers now keep provider payment-control status pending, so finite sidecar ingress completion waits for possible final metering before returning cleanly.
- Security and observability contracts are now narrower.
  - Oracle whitelist/provider metadata governance is deployment-managed YAML for MVP, not a public writable admin API.
- The provider gateway now has explicit provider operator auth wiring with read/admin bearer token env resolution, a separate operator listener, and authenticated provider operator APIs for sessions, accepted RAVs, collection records, and lifecycle transitions.
  - Provider operator tooling now includes authenticated read-only inspection commands and a manual collect command that signs and submits `SubstreamsDataService.collect` transactions locally while driving provider lifecycle state through pending, collected, retryable failure, dry-run, no-wait, and already-collected no-op paths.
  - Provider operator session inspection now presents runtime payment state, including low-funds status, projected outstanding value, escrow balance when known, minimum needed, check errors, and operator hints.
  - The private provider operator listener exposes authenticated Prometheus-style `/metrics` for aggregate session, worker, usage, accepted-RAV, collection, low-funds, payment-control, and RAV-request visibility.
  - The MVP observability floor is structured logs, operator inspection/status tooling, and basic Prometheus-style metrics; distributed tracing remains post-MVP.

## Remaining Backlog Alignment

The remaining MVP gaps now align with the rewritten MVP backlog as follows.

Oracle, consumer ingress, and runtime compatibility:

- No remaining runtime-compatibility backlog item after `MVP-036` Published runtime images.

Provider runtime hardening and cleanup:

- No separate runtime-payment hardening task remains open after `MVP-040` and `MVP-041`.

Tooling and operations:

- No open provider operator tooling task remains in this lane after `MVP-019`, `MVP-020`, and `MVP-032`.

Security and observability:

- No open security or observability task remains after `MVP-021`, `MVP-022`, `MVP-023`, `MVP-024`, and `MVP-028`.

Validation and docs:

- `MVP-025`
- `MVP-026`

The gap analysis and the backlog now agree that:

- pricing authority is resolved for MVP
- reconnect/payment-session reuse is not an MVP target
- shared-state runtime hardening now relies on isolated per-test payer/provider identities with explicit pre-state guards; explicit `RavRequest` response semantics are now implemented
- oracle governance is config-managed for MVP
- the observability floor is resolved for MVP

## Open Questions Carrying Risk

None currently identified.

## Recommended Usage

Use `docs/mvp-scope.md` as the stable target-state reference.

Use this file to:

- assess current progress
- identify MVP gaps
- map current repo status to the MVP target
- keep implementation status current without rewriting the MVP scope itself

Use `plans/mvp-implementation-backlog.md` as the concrete task backlog aligned to the revised MVP scope.
