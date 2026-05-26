# MVP Implementation Sequencing

Archived note: this is the historical MVP sequencing record. The active
post-MVP follow-up tracker is
`../../plans/post-mvp-backlog.md`.

This document records the final MVP implementation order derived from
`../../plans/archive/mvp-implementation-backlog.md`.

It is no longer active sequencing guidance. The MVP implementation scope is
complete after `MVP-026` Documentation refresh.

Use:

- `../mvp-scope.md` as the MVP target-state definition
- `../../plans/archive/mvp-implementation-backlog.md` as the task execution record
- `../../plans/archive/mvp-gap-analysis.md` as the final readiness summary
- `../mvp-acceptance-matrix.md` as acceptance evidence

## Final Sequence

### Phase 0: Shared Contract Gate

Completed:

- `MVP-001` Pricing contract
- `MVP-002` Fresh sessions
- `MVP-003` Provider persistence boundary
- `MVP-004` Runtime payment contract
- `MVP-027` Session identity
- `MVP-028` Operator auth contract
- `MVP-033` Network discovery

These tasks froze the semantics used by every downstream lane: oracle pricing,
fresh session creation, provider-returned data-plane endpoints, session-local
funding, bearer-token operator roles, and package-derived network resolution.

### Phase 1: Discovery And Consumer Entry

Completed:

- `MVP-005` Oracle service
- `MVP-006` Oracle admin workflow
- `MVP-007` Consumer oracle discovery
- `MVP-017` Sidecar ingress

The consumer sidecar is now the supported SDS-facing Substreams-compatible
entrypoint. It owns provider discovery/session bootstrap and hides SDS payment
coordination behind the ingress path.

### Phase 2: Runtime Payment And Provider Integration

Completed:

- `MVP-010` Low funds
- `MVP-011` Low-funds propagation
- `MVP-012` RAV thresholds
- `MVP-014` Provider integration
- `MVP-015` Byte metering
- `MVP-016` Stream lifecycle control
- `MVP-031` Provider-originated payment loop
- `MVP-037` Runtime test hardening
- `MVP-038` Wrapper removal
- `MVP-040` Ingress termination ordering
- `MVP-041` RAV response semantics

The wrapper-era usage-report runtime path is removed. The supported runtime path
is the sidecar ingress plus public Provider Gateway, private Plugin Gateway, and
provider-originated `PaymentSession` control driven by provider-side metering.

### Phase 3: Provider State, Settlement, And Operator APIs

Completed:

- `MVP-008` Runtime durability
- `MVP-009` Provider inspection APIs
- `MVP-019` Inspection CLI
- `MVP-020` Manual collection CLI
- `MVP-022` Operator auth enforcement
- `MVP-029` Collection lifecycle
- `MVP-032` Runtime/session/payment inspection

Provider runtime state and collection lifecycle state are persisted through the
repository model. Operator flows use authenticated provider APIs rather than
direct database access.

### Phase 4: Security, Operations, Validation, And Docs

Completed:

- `MVP-018` Funding CLI
- `MVP-021` TLS defaults
- `MVP-023` Observability floor
- `MVP-024` Metrics/status/log correlation
- `MVP-025` Acceptance coverage
- `MVP-026` Documentation refresh
- `MVP-030` Runtime compatibility contract
- `MVP-034` PostgreSQL test portability
- `MVP-035` Devenv port resilience
- `MVP-036` Published runtime image state

TLS is the default non-dev posture, plaintext is explicit for the reflex
devenv, MVP acceptance is local-stack based, and runtime compatibility is
documented without side-effectful automatic probes.

## Deferred Lanes

These tasks remain outside the MVP sequence:

- `MVP-013` Reconnect/resume: true provider-authoritative payment-session continuation and RAV-lineage reuse.
- `MVP-039` Provider runtime decoupling: explicit internal RPC/event boundary between the private Plugin Gateway and public Provider Gateway for independently deployed provider surfaces.

Future work in either lane should start from a new planning pass rather than
using this MVP sequencing record as an active implementation plan.
