# Provider Persistence Boundary

This document is the canonical MVP reference for the provider-side persistence boundary introduced by `MVP-003`.

It defines the separation between:

- runtime/session persistence owned by the current provider repository model
- settlement/collection lifecycle persistence that remains separate MVP work, primarily `MVP-029`

Use this document together with:

- [docs/mvp-scope.md](./mvp-scope.md) for the MVP target state
- [plans/mvp-implementation-backlog.md](../plans/mvp-implementation-backlog.md) for task ownership and dependencies
- [provider/repository/repository.go](../provider/repository/repository.go) for the current repository interface
- [provider/repository/psql/migrations/000001_init_schema.up.sql](../provider/repository/psql/migrations/000001_init_schema.up.sql) for the current durable storage shape

## Current Provider Runtime Topology

For the current MVP implementation, the provider runtime is split into two provider-side API surfaces:

- a public `ProviderGateway` that serves the consumer sidecar payment/session protocol
- a private `PluginGateway` that serves the Firehose/Substreams plugin-facing `auth`, `session`, and `usage` services

That split exists because the two surfaces have different callers, trust boundaries, and exposure requirements:

- the public gateway is the provider-facing SDS control plane that consumer sidecars connect to
- the private gateway is an internal adapter layer for Firehose/Substreams plugin callbacks and should not be internet-exposed

For MVP, this split should be understood primarily as an API/security boundary, not as a requirement that the two gateways be independently deployable in separate environments.

The current implementation co-deploys both surfaces as one provider runtime and wires them to the same repository-backed runtime state.

## Current Interaction Model Between Plugin Gateway and Provider Gateway

The current metering-to-payment-control interaction is:

1. Firehose/Substreams metering code sends usage batches to the private `UsageService`.
2. `UsageService` persists the metered usage into the provider runtime repository and advances the owning session aggregates.
3. After usage is applied, `UsageService` notifies the public `ProviderGateway` runtime logic that the session has new metered usage.
4. `ProviderGateway` re-evaluates runtime payment control against the same session/runtime state, including:
   - current accepted RAV
   - usage delta since the last accepted-baseline snapshot
   - low-funds handling
   - whether a new `RAVRequest` should be emitted on the live `PaymentSession`

In the current codebase, step 3 is implemented as an in-process callback rather than an internal RPC. That choice is acceptable for the current MVP topology because both gateways are started together as one provider runtime and already share the same repository-backed runtime state.

This means the current provider runtime model assumes:

- the private and public gateway surfaces are part of the same provider runtime deployment unit for MVP
- authoritative provider runtime state is shared beneath both surfaces through the provider repository model
- live `PaymentSession` bindings and provider-originated control dispatch remain process-local to the public `ProviderGateway`

## Runtime Persistence Model

The current provider repository model is a runtime/session model.

For MVP purposes, that model includes:

- sessions and their lifecycle/status
- workers/connections attached to sessions
- usage events and accumulated usage totals
- quota/runtime coordination state
- the latest accepted RAV snapshot associated with a session

In the current PostgreSQL implementation, that concrete shape is represented by:

- `sessions`
- `workers`
- `usage_events`
- `quota_usage`
- `ravs` as a one-to-one latest accepted RAV record keyed by `session_id`

The `ravs` table should be interpreted as durable runtime state that preserves the latest accepted RAV needed after restart. It is not, by itself, a complete settlement lifecycle model.

## Why The Shared Runtime Repository Exists

The shared repository model exists because the private plugin-facing services and the public payment/session gateway are cooperating on the same provider-authoritative runtime payment state machine.

That shared state currently includes at least:

- session lifecycle/status
- usage totals and usage events
- the latest accepted RAV
- the baseline snapshot used to determine usage since the last accepted RAV
- worker/quota/runtime coordination state

Without a shared source of truth for that runtime state, the private plugin-facing usage path would not be able to advance the same payment/session state that drives provider-originated `RAVRequest` and low-funds decisions on the public `PaymentSession` path.

## Settlement Conceptual Model

Settlement and collection lifecycle tracking is a separate concern from runtime session tracking.

For MVP, the provider must eventually support durable collection-oriented state for accepted RAVs, including the conceptual lifecycle states:

- `collectible`
- `collect_pending`
- `collected`
- retryable failure / retryable collection state

That lifecycle is settlement state, not runtime session state. It exists so operator inspection and manual collection workflows can reason about what is ready to collect, what is in flight, and what has already completed.

`MVP-003` does not define the concrete persistence schema, repository interface, or API payloads for that lifecycle. That design and implementation belong to downstream tasks, especially `MVP-029`, with retrieval surfaces in `MVP-009`.

## Boundary Rules

- Provider restart must preserve accepted RAV state needed for post-restart inspection and settlement.
- Fresh reconnects create new SDS payment sessions and do not reuse prior runtime session identity or payment lineage.
- Runtime session records may reference settlement-relevant accepted state, but they do not define collection progress.
- For MVP, the public/private provider split is a boundary between API surfaces and trust zones, not a commitment that those two surfaces are independently deployable without further internal runtime decoupling work.
- Client and CLI flows should read settlement-relevant provider state through provider-owned APIs, not by assuming direct database access.
- `MVP-008` extends durable runtime storage around the existing repository model.
- `MVP-029` owns collection lifecycle persistence, transitions, and retry semantics.
- `MVP-009` owns provider retrieval APIs for accepted and collectible settlement-relevant state.

## Implications For Downstream Tasks

- `MVP-008` should focus on restart-safe runtime durability for sessions, workers, usage, and the latest accepted RAV state already represented in the current repository model.
- `MVP-008` should not absorb collection lifecycle tracking just because accepted RAV state is also settlement-relevant.
- `MVP-029` should introduce the distinct provider-side persistence/update model needed for collection lifecycle state.
- `MVP-019` and `MVP-020` should consume provider-backed settlement retrieval flows after `MVP-009` and `MVP-029`, not direct backend reads.

## Post-MVP Decoupling Direction

If SDS later needs to run the private plugin-facing services and the public provider gateway as more independently deployable components, the current in-process metering notification is not sufficient by itself.

That future work should:

- replace the current in-process metering notification with an explicit internal gRPC or equivalent eventing boundary
- clarify which component owns authoritative runtime payment state
- ensure the public `ProviderGateway` can make `RAVRequest` and low-funds decisions from an authoritative source of truth rather than implicit in-memory coordination
- keep the public/private split as an exposure/security boundary while removing the hidden assumption that both surfaces must share one process

That decoupling is intentionally post-MVP work. The current MVP architecture does not require fully separate deployment of the public and private provider surfaces.

## Out Of Scope For MVP-003

`MVP-003` does not:

- add or change protobuf APIs
- add or change repository interfaces
- add or change database schema
- define the final collection lifecycle schema or transitions in implementation detail
- resolve authn/authz for operator/admin surfaces
- define the exact inspection or collection API shape
