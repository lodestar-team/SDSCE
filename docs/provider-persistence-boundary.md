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
- Client and CLI flows should read settlement-relevant provider state through provider-owned APIs, not by assuming direct database access.
- `MVP-008` extends durable runtime storage around the existing repository model.
- `MVP-029` owns collection lifecycle persistence, transitions, and retry semantics.
- `MVP-009` owns provider retrieval APIs for accepted and collectible settlement-relevant state.

## Implications For Downstream Tasks

- `MVP-008` should focus on restart-safe runtime durability for sessions, workers, usage, and the latest accepted RAV state already represented in the current repository model.
- `MVP-008` should not absorb collection lifecycle tracking just because accepted RAV state is also settlement-relevant.
- `MVP-029` should introduce the distinct provider-side persistence/update model needed for collection lifecycle state.
- `MVP-019` and `MVP-020` should consume provider-backed settlement retrieval flows after `MVP-009` and `MVP-029`, not direct backend reads.

## Out Of Scope For MVP-003

`MVP-003` does not:

- add or change protobuf APIs
- add or change repository interfaces
- add or change database schema
- define the final collection lifecycle schema or transitions in implementation detail
- resolve authn/authz for operator/admin surfaces
- define the exact inspection or collection API shape
