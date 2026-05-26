# Post-MVP Backlog

This document tracks follow-up work discovered during MVP execution that is not
required for MVP acceptance.

Use it for post-MVP tasks that are specific enough to preserve, but not yet
ready for a larger project plan. Keep entries short, with enough context to
restart the work later.

## Status Values

- `not_started`
- `in_progress`
- `blocked`
- `done`
- `deferred`

## Tasks

| ID | Status | Area | Task |
| --- | --- | --- | --- |
| PMVP-001 | `not_started` | provider-state | Harden repository snapshot semantics and runtime repository construction |
| PMVP-002 | `not_started` | runtime-payment | Implement provider-authoritative payment-session reconnect/resume semantics if product scope requires it |
| PMVP-003 | `not_started` | provider-integration | Decouple the private Plugin Gateway and public Provider Gateway through an explicit internal RPC/event boundary |

## PMVP-001 Repository Snapshot and Runtime Construction Hardening

Context:

- This follow-up preserves the remaining issue from archived review task
  `plans/archive/current-implementation-review-tasks/CRT-05A.md`.
- The in-memory repository should not leak mutable shared state through getters
  or list methods.
- Runtime construction should not silently invent an in-memory repository when a
  caller forgot to make an explicit repository choice.

Done when:

- In-memory repository getters and list methods return snapshots rather than
  live mutable pointers.
- Mutable nested values such as maps, `*big.Int`, and signed RAV structures are
  copied deeply enough that callers cannot mutate repository state without an
  explicit repository update method.
- `gateway.New(...)` no longer silently creates an in-memory repository when
  `config.Repository` is nil.
- The explicit `inmemory://` DSN path remains available for local/dev/test use.
- Deployment docs continue to distinguish restart-durable PostgreSQL state from
  full active/active runtime topology.

Verify:

- Add tests proving mutating returned repository values does not mutate stored
  repository state.
- Add tests proving runtime construction fails fast or otherwise rejects a nil
  repository unless a caller selected one explicitly.
- Run `go test ./provider/repository ./provider/gateway`.
- Run `go test -race ./provider/repository`.

## PMVP-002 Payment-Session Reconnect/Resume

Context:

- This task carries forward deferred MVP task `MVP-013` under a post-MVP ID.
- MVP semantics intentionally create fresh SDS payment sessions for new requests
  or reconnects and do not reuse payment lineage.

Done when:

- Product scope requires true reconnect/resume semantics.
- Provider-authoritative identity, RAV lineage, stale-session cleanup, and replay
  rules are designed before implementation.

## PMVP-003 Provider Runtime Decoupling

Context:

- This task carries forward deferred MVP task `MVP-039` under a post-MVP ID.
- MVP co-deploys the public Provider Gateway and private Plugin Gateway as one
  provider runtime with shared repository state and process-local live
  `PaymentSession` bindings.

Done when:

- The private plugin-facing usage path and public payment/control path
  communicate through an explicit internal gRPC or equivalent event boundary.
- Runtime payment-state ownership is defined clearly enough for independently
  deployed provider surfaces.
- Tests cover the chosen internal-boundary contract or decoupled topology.
