# MVP Gap Analysis

Archived note: this is the final MVP readiness summary. The active post-MVP
follow-up tracker is `plans/post-mvp-backlog.md`.

Drafted: 2026-03-12
Revised: 2026-05-25

This document maps the repository state against the MVP defined in
`docs/mvp-scope.md`.

It is a final MVP readiness summary rather than an active gap list. The
task-level execution record is archived at
`plans/archive/mvp-implementation-backlog.md`.

## Summary

The MVP implementation scope is complete.

The current repo includes:

- Horizon V2 / TAP signing, verification, aggregation, and collection helpers
- deterministic local chain/contracts and integration coverage
- standalone oracle discovery with deployment-managed curated providers
- consumer sidecar ingress as the SDS-facing Substreams-compatible entrypoint
- provider `StartSession` handshake returning the session-specific data-plane endpoint
- provider-originated `PaymentSession` control behind the ingress path
- private provider plugin services for auth, session, and usage
- provider-side byte metering, RAV request flow, low-funds stop behavior, and accepted-RAV durability
- PostgreSQL-backed provider runtime and collection lifecycle persistence
- authenticated provider operator APIs and CLI flows for inspection and manual collection
- operator funding/signing CLI flows for approve, deposit, top-up, and signer authorization
- TLS-by-default posture with explicit plaintext flags for the reflex devenv
- local-stack acceptance evidence for scenarios A through G
- runtime compatibility documentation for current `firehose-core` and `dummy-blockchain` image state

No MVP implementation gaps remain after `MVP-026` Documentation refresh.

## Acceptance Scenario Status

| Scenario | Status | Evidence |
| --- | --- | --- |
| A. Discovery to paid streaming | `implemented` | Oracle-backed ingress and Firecore/dummy-chain runtime coverage validate SDS discovery, session start, metadata propagation, provider-side metering, and payment-state progression. |
| B. Fresh session after interruption | `implemented` | Local ingress interruption coverage validates fresh SDS payment sessions and non-reused RAV lineage after a later request. |
| C. Low funds during streaming | `implemented` | Session-local low-funds stop behavior reaches the sidecar ingress path and Firecore runtime path. |
| D. Provider restart without losing collectible state | `implemented` | PostgreSQL restart coverage preserves accepted RAV/baseline state; operator APIs and CLI expose settlement-relevant state. |
| E. Manual funding flow | `implemented` | Funding and signer authorization CLI flows exist outside local demo assumptions. |
| F. Manual collection flow | `implemented` | Provider-backed settlement inspection and `sds provider operator collect` fetch collectible state, locally sign/submit `SubstreamsDataService.collect`, and drive provider lifecycle state. |
| G. Secure deployment posture | `implemented` | TLS is the default server posture, plaintext requires explicit local/dev flags, and operator/admin surfaces require bearer-token auth. |

Scenario evidence is tracked in `docs/mvp-acceptance-matrix.md`.

## Completed Capability Areas

### Protocol And Discovery

- `MVP-001` Oracle pricing, `MVP-002` Fresh sessions, `MVP-027` Session identity, and `MVP-033` Network discovery are frozen for MVP.
- The consumer sidecar derives package network by default, supports explicit network only as fallback, and fails fast on conflicts.
- The oracle is authoritative for curated-provider pricing and returns the selected provider control-plane endpoint.
- The provider handshake remains responsible for session-specific data-plane endpoint resolution.

### Runtime Path

- The supported runtime entrypoint is the consumer sidecar ingress.
- The deprecated wrapper-era `ReportUsage` runtime path and related CLI/protobuf surfaces were removed under `MVP-038`.
- The public Provider Gateway owns provider-authoritative session/payment state.
- The private Plugin Gateway adapts Firehose/Substreams auth, session, and usage callbacks.
- The provider-originated payment-control loop drives RAV requests and low-funds stop behavior from provider-side metering.

### Provider State And Settlement

- PostgreSQL persistence covers sessions, workers, usage, quota/runtime state, latest accepted RAV state, and collection lifecycle records.
- Collection lifecycle state supports `collectible`, `collect_pending`, `collected`, and `collect_failed_retryable`.
- Provider restart coverage validates accepted RAV and baseline persistence.
- Operator APIs and CLI flows retrieve accepted RAVs and collection records without direct database access.

### Operator Workflows

- Funding CLI flows support approve, deposit, top-up, and signer authorization.
- Manual collection fetches provider settlement state, signs locally with the provider key, submits `SubstreamsDataService.collect`, and updates provider collection lifecycle state.
- Provider operator APIs enforce the shared bearer-token role contract from `docs/operator-auth.md`.

### Security And Operations

- TLS is the default non-dev transport posture.
- Plaintext is explicit and documented as local/dev only, with the reflex devenv as the checked-in plaintext exception.
- Provider operator metrics and inspection surfaces require `operator.read`; mutation/collection transitions require `admin.write`.
- Basic Prometheus-style metrics, structured logs, and operator status surfaces satisfy the MVP observability floor.

### Runtime Compatibility

- `MVP-036` verified that `ghcr.io/streamingfast/firehose-core:latest` is compatible with the current SDS provider/plugin contract when embedded in a rebuilt dummy-chain image.
- The checked prebuilt `dummy-blockchain` tags remain stale for the current SDS runtime path; use `SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE` with a locally rebuilt image until StreamingFast publishes a refreshed dummy-chain image.
- `docs/provider-runtime-compatibility.md` is the operator-facing source of truth for validated tuples, known incompatible runtime images, and local-image fallback workflow.

## Deferred Or Post-MVP Work

The following items are intentionally outside the MVP scope:

- `MVP-013` Reconnect/resume: true provider-authoritative payment-session continuation and RAV-lineage reuse.
- `MVP-039` Provider runtime decoupling: explicit internal RPC/event boundary between the private Plugin Gateway and public Provider Gateway for independently deployed provider surfaces.
- Correct aggregate payer-level exposure control across concurrent streams.
- Permissionless oracle sourcing from on-chain provider registry data.
- Dynamic/provider-specific pricing and richer provider ranking.
- Wallet-connected end-user funding UI.
- Automated/background settlement collection.
- Rich distributed tracing and provider-specific production observability hardening.

## Source Of Truth

- MVP target state: `docs/mvp-scope.md`
- Acceptance evidence: `docs/mvp-acceptance-matrix.md`
- Task execution record: `plans/archive/mvp-implementation-backlog.md`
- Runtime compatibility: `docs/provider-runtime-compatibility.md`
- Persistence boundary: `docs/provider-persistence-boundary.md`
