# Substreams Data Service MVP Scope

Drafted: 2026-03-12

## Purpose

This document defines the target MVP for Substreams Data Service (SDS).

It is intended to be the stable source of truth for:

- engineering scope
- product and operational scope
- architectural decisions and their rationale
- MVP acceptance scenarios
- explicit non-goals and open questions

It is not a task tracker. Detailed current-state assessment and implementation tracking should live in a separate document.

## Audience

This document is written for:

- SDS engineers
- product and planning stakeholders
- external collaborators such as StreamingFast

## MVP Definition

The SDS MVP is a usable end-to-end payment-enabled Substreams stack, not just a local demo. It must support real provider discovery, real consumer and provider integration paths, paid streaming with provider-authoritative byte metering, live low-funds handling, durable provider-side payment state, manual operator-driven funding and settlement workflows, and a production-oriented transport/security posture.

The MVP may intentionally simplify parts of the system where doing so materially reduces implementation complexity without invalidating the architecture. In particular, the MVP may assume session-local funding logic and defer correct payer-level aggregate exposure handling across concurrent streams.

## Current Status Summary

As of 2026-03-12, the repo already contains important parts of the MVP foundation:

- working Horizon V2 / TAP signing, verification, and aggregation
- deterministic local chain/contracts and integration coverage
- consumer sidecar and provider gateway RPC surfaces
- sidecar-to-gateway session start and bidirectional payment session flow
- provider-side Firehose plugin services (`auth`, `session`, `usage`)
- a development/demo stack and sink wrapper

However, the current repo does not yet constitute the MVP. Major remaining gaps include:

- standalone oracle/discovery component
- real production-path provider and consumer integration completion
- provider-side durable persistence for accepted RAV and collection state
- low-funds stop/pause behavior during live streaming
- operator funding and settlement CLI flows
- authenticated admin/operator surfaces
- finalization of several protocol decisions called out below as open questions

See `plans/mvp-gap-analysis.md` for a detailed status map.

## Goals

- Deliver a full SDS stack that can be used against a real provider deployment, initially expected to be StreamingFast.
- Make the consumer sidecar the mandatory client-side integration component.
- Use a standalone oracle service for provider discovery, while still supporting direct provider configuration as fallback.
- Use provider-authoritative byte metering as the billing source of truth.
- Support reconnect/resume behavior without making consumer-local persistence mandatory.
- Preserve accepted RAV and settlement-relevant state durably on the provider side.
- Support manual operator-driven funding and collection workflows through CLI tooling.
- Use TLS by default outside local/dev usage.

## Non-Goals

- Correct aggregate funding/exposure handling across multiple concurrent streams for the same payer.
- Blocking concurrent streams at runtime.
- Permissionless oracle provider sourcing from on-chain registry data.
- Wallet-connected end-user funding UI.
- Automated/background settlement collection.
- Rich provider ranking or QoS-based oracle selection.
- Full observability hardening with finalized metrics/tracing strategy.

## Key Workflows

### 1. Discover Provider

- The consumer sidecar queries a standalone oracle service.
- The oracle receives the requested chain/network context.
- The oracle returns:
  - the eligible provider set
  - a recommended provider choice
- The consumer sidecar uses the recommended provider by default.
- Direct provider configuration remains a supported fallback path.

### 2. Initialize or Reconnect a Paid Session

- The consumer sidecar initiates the provider handshake.
- The provider responds with either:
  - a fresh zero-value RAV for a new handshake, or
  - the latest known resumable RAV for a reconnect
- The provider remains the authoritative side for accepted payment state.
- Recovery should be folded into the initial handshake rather than introduced as a separate recovery endpoint.

### 3. Stream and Update Payment State

- The real provider integration path meters streamed bytes using the provider-side metering plugin path.
- The provider is authoritative for billable usage.
- The consumer sidecar participates in payment/session control but is not authoritative for billed byte usage.
- While streaming:
  - provider reports/payment state advances
  - RAVs are requested and updated as needed
  - low-funds conditions can be surfaced during the live stream
- For MVP, low-funds decisions are session-local, not payer-global across concurrent streams.

### 4. Fund or Top Up Escrow

- Funding is an operator/developer workflow, not an end-user wallet UI.
- CLI tooling should make approve/deposit/top-up simple enough for MVP operations.
- The system should surface when additional funding is needed, but the act of funding remains external to the core runtime path.

### 5. Collect Accepted RAVs On-Chain

- The provider side stores accepted collectible RAV state durably.
- Operator CLI tooling queries the provider for settlement-relevant RAV data.
- The CLI crafts, signs, and submits the `collect()` transaction locally.
- The settlement signing key remains outside the provider sidecar.
- Collection state should distinguish between at least:
  - `collectible`
  - `collect_pending`
  - `collected`

## Major MVP Decisions

| Decision | MVP Choice | Short Rationale |
| --- | --- | --- |
| Consumer integration | Consumer sidecar is mandatory | SDS is a full stack, not a loose protocol suggestion |
| Provider discovery | Standalone oracle component | Discovery is a real product component and should exist independently even if initial logic is simple |
| Oracle selection logic | Whitelist plus simple selection among eligible providers | Good enough for MVP while preserving future ranking logic |
| Oracle response shape | Return eligible providers plus a recommended provider | Keeps default client behavior simple while preserving future flexibility |
| Direct provider connection | Supported as fallback | Useful bridge from current implementation and operational fallback |
| Funding model | Session-local low-funds logic | Avoids premature distributed liability accounting for concurrent streams |
| Concurrent streams | Documented limitation, not blocked | Simpler MVP with explicit limitation instead of partial enforcement |
| Billing unit | Streamed bytes | Aligns with provider-authoritative metering path |
| Funding UX | CLI/operator-driven | Avoids premature UI scope |
| Collection execution | CLI signs and submits locally | Keeps settlement key custody outside provider sidecar |
| Provider payment state | Durable persistence required | Losing accepted RAV state is unacceptable |
| Consumer persistence | Not required for MVP | Better to recover from provider-authoritative state during handshake |
| Recovery shape | Part of initial handshake | Avoids separate recovery API surface |
| Security posture | TLS by default outside local/dev | Better security without forcing heavy hardening |
| Admin/operator actions | Require authentication | Oracle governance and provider operations should not be effectively public |
| Real integration | Real provider and consumer paths are mandatory | Local demo flow is insufficient for MVP |
| Validation scope | One real provider environment is enough for MVP acceptance | Narrow operational validation is acceptable if architecture stays generic |

## Why Multi-Stream Support Is Deferred

Correct multi-stream support is more than summing usage across sessions.

If a single payer can run multiple streams from different machines, correct funding control requires:

- a provider-authoritative global liability ledger keyed by payer or payment identity
- durable shared state across provider instances
- session liveness and stale-session cleanup
- race-safe exposure accounting when streams start concurrently
- clear rules for pending requested RAVs, accepted RAVs, and unaggregated usage
- restart and resume semantics that avoid duplicated or lost liability

That is a materially larger distributed-state problem than the session-local MVP design. The MVP therefore documents concurrent streams as a known limitation for funding-control correctness and does not attempt to enforce or fully solve them.

## Component Deliverables

### Oracle

- Standalone service and deployment unit
- Manually managed provider whitelist
- Provider selection based on minimal metadata, at least:
  - endpoint information
  - chain/network eligibility
  - possibly pricing metadata
- Returns eligible providers plus one recommended provider
- Administrative/governance actions require authentication

### Consumer Sidecar

- Mandatory client-side SDS integration component
- Supports oracle-backed discovery and direct provider fallback
- Performs session initialization with provider
- Participates in reconnect flow where provider may return fresh or latest known RAV
- Maintains payment/session coordination during streaming
- Works with the real client integration path, not only demo wrappers
- Does not require durable local persistence for MVP

### Provider Gateway / Provider Integration

- Real integration into the provider path is mandatory
- Validates payment/session state for real streaming traffic
- Uses provider-authoritative byte metering from plugin/integration path
- Drives RAV request/response flow
- Handles live low-funds conditions during streaming
- Persists accepted RAV and settlement-relevant state durably
- Exposes authenticated operator/admin surfaces for inspection and settlement data retrieval

### Provider State Storage

- Durable persistence for accepted RAV state
- Durable persistence for settlement-relevant metadata and collection lifecycle state
- Durable persistence should survive provider restarts
- Storage model should support:
  - latest accepted collectible state
  - session/runtime state
  - collection status tracking

### CLI / Operator Tooling

- Funding flows:
  - approve
  - deposit
  - top up
- Settlement flows:
  - inspect collectible state
  - fetch settlement-relevant RAV data
  - craft and submit `collect()` transaction locally
  - inspect or retry pending collection attempts
- Tooling should be sufficient for operators without requiring a dedicated UI

### Security and Admin Surfaces

- TLS enabled by default for non-dev usage
- Plaintext allowed only for local/dev/demo workflows
- Authenticated admin/operator actions for:
  - oracle management
  - provider inspection
  - collection-data retrieval
- Final auth mechanism remains an implementation choice

### Observability

- Sufficient operational visibility for MVP
- Structured logs and status/inspection tools are required
- Richer metrics/tracing strategy remains open

## Operational Deliverables

- Providers can restart without losing accepted collectible RAV state
- Operators can inspect session/payment/collection state
- Operators can fund and top up escrow through CLI workflows
- Operators can perform manual on-chain collection through CLI workflows
- The system can surface low-funds conditions during active streams
- Recovery/reconnect behavior is defined well enough for operators to understand expected runtime behavior

## Acceptance Scenarios

The scenarios below are the primary definition of done for the MVP.

### A. Discovery to Paid Streaming

- Consumer sidecar queries the oracle for a required chain/network
- Oracle returns eligible providers plus a recommended choice
- Consumer sidecar uses the recommended provider
- Provider handshake succeeds
- Real streaming begins through the production integration path
- Byte metering occurs on the provider side
- Payment state advances correctly during streaming

### B. Reconnect and Resume

- An active SDS session is interrupted
- Consumer sidecar reconnects through the normal handshake path
- Provider responds with the appropriate fresh or resumable RAV state
- Streaming resumes without losing the authoritative accepted payment state

### C. Low Funds During Streaming

- Streaming starts with initially sufficient funds
- Usage progresses until provider-side session-local funding logic determines funds are too low
- Provider surfaces the low-funds condition during the live stream
- The client path receives and reacts to the stop/pause decision correctly

### D. Provider Restart Without Losing Collectible State

- Provider accepts at least one updated RAV
- Provider process restarts
- Accepted collectible RAV state remains available after restart
- Operator can still inspect and use that state for settlement

### E. Manual Funding Flow

- Operator can approve token spend and deposit/top up escrow through CLI tooling
- The resulting on-chain funding state is usable by SDS runtime flows

### F. Manual Collection Flow

- Provider exposes settlement-relevant accepted RAV data
- CLI fetches that data
- CLI crafts, signs, and submits the `collect()` transaction locally
- Collection can be retried safely if needed
- Provider-side collection state can distinguish pending vs completed status

### G. Secure Deployment Posture

- Non-dev deployments use TLS by default
- Operator/admin actions are authenticated
- Local/dev/demo workflows may still use simpler transport settings explicitly

## Known Limitations for MVP

- Funding-control correctness is session-local, not payer-global across concurrent streams
- Concurrent streams are not blocked, only documented as a limitation
- Funding remains an operator/developer workflow rather than end-user wallet UI
- Collection remains operator-driven rather than automatic
- Oracle provider set is manually curated rather than permissionless
- Observability scope is intentionally basic

## Post-MVP Follow-Ups

- Correct multi-stream aggregate exposure handling
- Permissionless oracle sourcing from the Substreams Data Service contract registry
- Richer oracle metadata and provider ranking
- Automated/background collection using a separate settlement agent
- Better consumer recovery semantics if needed beyond handshake-based recovery
- Better funding UX, including possible wallet-connected UI
- Stronger observability and operational tooling

## Open Questions

- Should chain/network be derived automatically from the Substreams package, or supplied explicitly to the oracle query path?
- What is the pricing authority contract between oracle metadata and provider handshake responses?
- What is the exact canonical payment identity and `collection_id` reuse policy for fresh workloads versus reconnects?
- How much of the reconnect/recovery state should be keyed by session versus on-chain payment identity?
- Should simple observability for MVP include metrics endpoints, or are structured logs plus inspection/status tooling sufficient?
- What exact authentication mechanism should protect provider and oracle admin/operator surfaces?

## References

- `docs/phase1-sidecar-spec.md`
- `plans/implementation-backlog.md`
- `plans/component-task-breakdown.md`
- `README.md`
