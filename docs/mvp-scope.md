# Substreams Data Service MVP Scope

Drafted: 2026-03-12  
Revised: 2026-03-24

## Purpose

This document defines the target MVP for Substreams Data Service (SDS).

It is intended to be the stable source of truth for:

- engineering scope
- product and operational scope
- architectural decisions and their rationale
- MVP acceptance scenarios
- explicit non-goals and remaining open questions

It is not a task tracker. Detailed current-state assessment and implementation tracking should live in separate planning documents.

## Audience

This document is written for:

- SDS engineers
- product and planning stakeholders
- external collaborators such as StreamingFast

## MVP Definition

The SDS MVP is a usable end-to-end payment-enabled Substreams stack, not just a local demo. It must support real provider discovery, real consumer and provider integration paths, paid streaming with provider-authoritative byte metering, live low-funds handling, durable provider-side payment state, manual operator-driven funding and settlement workflows, and a production-oriented transport/security posture.

The MVP should preserve Substreams-compatible data-plane usage as much as possible. Existing users should be able to point Substreams tooling at a consumer sidecar endpoint and use it like a normal Substreams endpoint, while SDS-specific discovery, payment, and provider coordination happen behind that boundary.

The MVP may intentionally simplify parts of the system where doing so materially reduces implementation complexity without invalidating the architecture. In particular, the MVP may assume:

- session-local funding logic rather than payer-global aggregate exposure control across concurrent streams
- oracle-authoritative pricing across a curated provider set
- fresh SDS payment sessions for new requests or reconnects rather than payment-session continuation

## Current Status Summary

As of 2026-03-24, the repo already contains important parts of the MVP foundation:

- working Horizon V2 / TAP signing, verification, and aggregation
- deterministic local chain/contracts and integration coverage
- consumer sidecar and provider gateway RPC surfaces
- sidecar-to-gateway session start and bidirectional payment session flow
- provider-side Firehose plugin services (`auth`, `session`, `usage`)
- a development/demo stack and sink wrapper

However, the current repo does not yet constitute the MVP. Major remaining gaps include:

- standalone oracle/discovery component
- consumer-side endpoint compatibility that hides SDS control flow behind a Substreams-compatible ingress
- provider-side durable persistence for accepted RAV and collection state
- low-funds stop/pause behavior during live streaming
- operator funding and settlement CLI flows
- authenticated admin/operator surfaces
- finalization of observability scope

See `plans/mvp-gap-analysis.md` for a detailed status map.

## Goals

- Deliver a full SDS stack that can be used against a real provider deployment, initially expected to be StreamingFast.
- Make the consumer sidecar the mandatory client-side SDS integration component and primary user entrypoint.
- Preserve backwards-compatible Substreams data-plane interaction semantics through the consumer sidecar.
- Use a standalone oracle service for provider discovery, while still supporting direct provider configuration as fallback.
- Use provider-authoritative byte metering as the billing source of truth.
- Use provider-authoritative accepted payment state and durable provider-side settlement state.
- Support manual operator-driven funding and collection workflows through CLI tooling.
- Use TLS by default outside local/dev usage.

## Non-Goals

- Correct aggregate funding/exposure handling across multiple concurrent streams for the same payer.
- Blocking concurrent streams at runtime.
- Permissionless oracle provider sourcing from on-chain registry data.
- Provider-specific or negotiated pricing during MVP session handshake.
- Payment-session continuation or RAV-lineage reuse across reconnects.
- Wallet-connected end-user funding UI.
- Automated/background settlement collection.
- Rich provider ranking or QoS-based oracle selection.
- Full observability hardening with finalized metrics/tracing strategy.

## Key Workflows

### 1. Discover Provider

- The consumer sidecar is the default SDS-facing entrypoint for the user.
- The consumer sidecar derives the requested network from the Substreams package by default.
- The consumer sidecar queries a standalone oracle service.
- The oracle receives the requested chain/network context.
- The oracle returns:
  - the eligible provider set
  - a recommended provider choice
  - the provider control-plane endpoint for the selected provider
- The consumer sidecar uses the recommended provider by default.
- Direct provider configuration remains a supported fallback or override path.
- The oracle is not required to fully resolve the data-plane endpoint used for streaming.

MVP network-discovery contract:

- The consumer sidecar derives network from the Substreams package by default.
- If a package or module resolves a specific `networks` entry, that takes precedence over top-level `network`.
- Explicit user-supplied network input remains supported only as fallback when package derivation is unavailable.
- If both explicit input and package-derived network exist and differ after normalization, the request fails fast.
- If neither source yields a usable network, the request fails fast.
- SDS uses the same canonical network keys as the Graph networks registry for MVP, with repo-owned or pinned mappings rather than live runtime registry lookups.

### 2. Initialize a Paid Session

- The consumer sidecar initiates the provider handshake using the selected provider control-plane endpoint.
- Every new request or connection creates a fresh SDS payment session.
- The provider returns session-specific information needed to begin streaming, including the data-plane endpoint for that session.
- The provider remains the authoritative side for accepted payment state.
- The handshake is not a pricing negotiation step for MVP.
- Pricing is already fixed by the oracle for the curated MVP provider set.

### 3. Stream and Update Payment State

- The real provider integration path meters streamed bytes using the provider-side metering plugin path.
- The provider is authoritative for billable usage and low-funds decisions during live sessions.
- The consumer sidecar coordinates payment/session control while preserving normal Substreams-style usage at its user-facing endpoint.
- While streaming:
  - provider-authoritative usage advances
  - RAVs are requested and updated as needed
  - accepted payment state advances on the provider side
  - low-funds conditions can be surfaced during the live stream
- For MVP, low-funds decisions are session-local, not payer-global across concurrent streams.

### 4. Fund or Top Up Escrow

- Funding is an operator/developer workflow, not an end-user wallet UI.
- CLI tooling should make approve, deposit, and top-up flows simple enough for MVP operations.
- The consumer sidecar may provide lightweight advisory checks, for example:
  - warning if observed on-chain balance is below a coarse threshold
  - surfacing provider-reported low-funds signals more clearly
  - providing rough estimates based on oracle pricing
- The consumer sidecar is not authoritative for funding sufficiency.
- The act of funding remains external to the core runtime path.

### 5. Collect Accepted RAVs On-Chain

- The provider side stores accepted collectible RAV state durably.
- Operator CLI tooling queries the provider for settlement-relevant RAV data.
- The CLI crafts, signs, and submits the `collect()` transaction locally.
- The settlement signing key remains outside the provider gateway.
- Collection state should distinguish between at least:
  - `collectible`
  - `collect_pending`
  - `collected`
- Interrupted or restarted streams may create multiple independent collectible session records for the same payer.

## Major MVP Decisions

| Decision | MVP Choice | Short Rationale |
| --- | --- | --- |
| Consumer integration | Consumer sidecar is mandatory and acts as the SDS-facing Substreams-compatible endpoint/proxy | Preserve the familiar endpoint-driven data-plane workflow while hiding SDS coordination |
| Provider discovery | Standalone oracle component is the default path | Discovery is a real product component and should exist independently even if initial logic is simple |
| Oracle selection logic | Whitelist plus simple selection among eligible providers | Good enough for MVP while preserving future ranking logic |
| Oracle response shape | Return eligible providers, a recommended provider, and the selected provider control-plane endpoint | The oracle chooses who to talk to; the provider handshake resolves where to stream |
| Direct provider connection | Supported as fallback/override | Useful bridge from current implementation and operational fallback |
| Pricing authority | Oracle-authoritative pricing across the curated MVP provider set | Predictable pricing and simpler consumer/provider behavior while providers are manually curated |
| Billing unit | Streamed bytes | Aligns with provider-authoritative metering path |
| Funding model | Session-local low-funds logic | Avoids premature distributed liability accounting for concurrent streams |
| Funding UX | CLI/operator-driven with only lightweight consumer-side advisory guidance | Keeps MVP simple without pretending the consumer knows provider-side liability |
| Concurrent streams | Documented limitation, not blocked | Simpler MVP with explicit limitation instead of partial enforcement |
| Collection execution | CLI signs and submits locally | Keeps settlement key custody outside provider-side runtime |
| Provider payment state | Durable persistence required | Losing accepted RAV state is unacceptable |
| Consumer persistence | Durable local payment-session persistence is not required for MVP | New requests create fresh sessions instead of relying on payment-session recovery |
| Recovery shape | Fresh payment session per new request/connection | Avoids identity/recovery complexity around RAV reuse and session continuation |
| Provider control plane | Provider gateway is the authoritative provider-side SDS boundary | Gives the provider side a clear public control-plane authority |
| Security posture | TLS by default outside local/dev | Better security without forcing heavy hardening |
| Admin/operator actions | Require authentication | Oracle governance and provider operations should not be effectively public |
| Real integration | Real provider and consumer paths are mandatory | Local demo flow is insufficient for MVP |
| Validation scope | One real provider environment is enough for MVP acceptance | Narrow operational validation is acceptable if architecture stays generic |
| Compatibility constraint | Preserve backwards-compatible data-plane interaction semantics | SDS may add management workflows, but the data-plane experience should remain familiar |

## Why Multi-Stream Support Is Deferred

Correct multi-stream support is more than summing usage across sessions.

If a single payer can run multiple streams from different machines, correct funding control requires:

- a provider-authoritative global liability ledger keyed by payer or payment identity
- durable shared state across provider instances
- session liveness and stale-session cleanup
- race-safe exposure accounting when streams start concurrently
- clear rules for pending requested RAVs, accepted RAVs, and unaggregated usage
- restart and reconnect semantics that avoid duplicated or lost liability

That is a materially larger distributed-state problem than the session-local MVP design. The MVP therefore documents concurrent streams as a known limitation and does not attempt to enforce or fully solve them.

## Component Deliverables

### Oracle

- Standalone service and deployment unit
- Manually managed provider whitelist
- Canonical MVP pricing for the curated provider set
- Provider selection based on minimal metadata, at least:
  - control-plane endpoint information
  - chain/network eligibility
- Returns eligible providers plus one recommended provider
- Returns the selected provider control-plane endpoint, not the final streaming endpoint
- Administrative/governance actions require authentication

### Consumer Sidecar

- Mandatory client-side SDS integration component
- Primary user-facing SDS boundary
- Presents a Substreams-compatible endpoint/proxy for normal data-plane usage
- Supports oracle-backed discovery and direct provider fallback
- Performs session initialization with the provider control plane
- Receives the provider data-plane endpoint during session handshake
- Maintains payment/session coordination during streaming
- Works with the real client integration path, not only demo wrappers
- Does not require durable local payment-session persistence for MVP

### Provider Gateway / Provider Integration

- Provider gateway is the public SDS control plane for providers
- Real integration into the provider path is mandatory
- Validates payment/session state for real streaming traffic
- Uses provider-authoritative byte metering from the plugin/integration path
- Drives RAV request/response flow
- Handles live low-funds conditions during streaming
- Persists accepted RAV and settlement-relevant state durably
- Exposes authenticated operator/admin surfaces for inspection and settlement data retrieval
- May rely on separate internal plugin/runtime components behind the public gateway boundary

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
- CLI/operator tooling remains the execution surface for funding and settlement actions

### Security and Admin Surfaces

- TLS enabled by default for non-dev usage
- Plaintext allowed only for local/dev/demo workflows
- Authenticated admin/operator actions for:
  - oracle management
  - provider inspection
  - collection-data retrieval
- Public vs private provider services may be separated for security and operational reasons
- That public/private split is not the main consumer-facing architecture contract
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
- Runtime behavior is documented clearly enough for operators to understand what happens when a stream is interrupted and restarted

## Acceptance Scenarios

The scenarios below are the primary definition of done for the MVP.

### A. Discovery to Paid Streaming

- Consumer sidecar queries the oracle for a required chain/network
- Oracle returns eligible providers plus a recommended choice
- Oracle returns the provider control-plane endpoint for the selected provider
- Consumer sidecar uses the recommended provider by default
- Provider handshake succeeds
- Provider returns the session-specific data-plane endpoint
- Real streaming begins through the production integration path
- Byte metering occurs on the provider side
- Payment state advances correctly during streaming

### B. Fresh Session After Interruption

- An SDS-backed stream is interrupted
- A later request is made again through the normal consumer-side flow
- The consumer sidecar performs normal discovery or uses an explicit provider override
- The provider handshake creates a fresh SDS payment session
- The new request does not reuse prior payment-session identity or RAV lineage
- Any Substreams cursor or start-block continuation is handled as normal data-plane behavior rather than SDS-specific payment-session recovery

### C. Low Funds During Streaming

- Streaming starts with initially sufficient funds
- Usage progresses until provider-side session-local funding logic determines funds are too low
- Provider surfaces the low-funds condition during the live stream
- The client path receives and reacts to the stop/pause decision correctly
- Any consumer-side warnings or balance checks remain advisory rather than authoritative

### D. Provider Restart Without Losing Collectible State

- Provider accepts at least one updated RAV
- Provider process restarts
- Accepted collectible RAV state remains available after restart
- Operator can still inspect and use that state for settlement

### E. Manual Funding Flow

- Operator can approve token spend and deposit/top up escrow through CLI tooling
- The resulting on-chain funding state is usable by SDS runtime flows
- Consumer-side advisory checks may help surface obviously low balances, but runtime sufficiency still depends on provider-side behavior

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
- Payment-session continuation across reconnects is intentionally deferred
- Observability scope is intentionally basic

## Post-MVP Follow-Ups

- Correct multi-stream aggregate exposure handling
- Permissionless oracle sourcing from the Substreams Data Service contract registry
- Richer oracle metadata and provider ranking
- Dynamic or provider-specific pricing and the corresponding oracle selection logic
- True payment-session continuation and recovery semantics if later required
- Automated/background collection using a separate settlement agent
- Better funding UX, including possible wallet-connected UI
- Stronger observability and operational tooling

## Open Questions

- Should simple observability for MVP include metrics endpoints, or are structured logs plus inspection/status tooling sufficient?
- What exact authentication mechanism should protect provider and oracle admin/operator surfaces?

## References

- `plans/mvp-gap-analysis.md`
- `plans/mvp-implementation-backlog.md`
- `docs/mvp-implementation-sequencing.md`
- `README.md`
