# SDS Codebase Reading Guide

This document is a developer-oriented map of the current SDS codebase.

It is meant to help you read the implementation efficiently, not replace reading the code.

It focuses on:

- the current runtime shape
- the main entrypoints and ownership boundaries
- the fastest reading order for understanding the live architecture
- which parts of the old review drift were fixed in the recent review-fix waves
- which remaining items are deferred post-MVP work rather than stale review bugs

For product intent and target-state decisions, start with [docs/mvp-scope.md](./mvp-scope.md).

For post-MVP follow-up work, use:

- [plans/post-mvp-backlog.md](../plans/post-mvp-backlog.md)

For MVP readiness and task history, use:

- [plans/archive/mvp-gap-analysis.md](../plans/archive/mvp-gap-analysis.md)
- [plans/archive/mvp-implementation-backlog.md](../plans/archive/mvp-implementation-backlog.md)

For provider runtime/state boundaries and external runtime compatibility, also use:

- [docs/provider-persistence-boundary.md](./provider-persistence-boundary.md)
- [docs/provider-runtime-compatibility.md](./provider-runtime-compatibility.md)

For review rationale, use:

- [plans/archive/current-implementation-review.md](../plans/archive/current-implementation-review.md)
- [plans/archive/current-implementation-review-tasks/](../plans/archive/current-implementation-review-tasks/) for completed or superseded review tasks

Important context: that review is no longer a reliable list of "current bugs". Much of it was addressed by the four review-fix commits `8de75f3` through `5aad2ab`. Use it as design rationale and historical hardening context, not as a current status snapshot.

## 1. Current Status At A Glance

As of the current tree, the repo already contains the core MVP runtime path:

- standalone oracle discovery
- consumer sidecar ingress as the SDS-facing Substreams-compatible boundary
- provider `StartSession` handshake returning the session-specific data-plane endpoint
- long-lived provider-originated `PaymentSession` control loop
- real provider-side plugin path (`auth`, `session`, `usage`)
- shared provider repository state for sessions, usage, workers, quotas, and latest accepted RAV state
- local-first integration coverage for the real consumer/provider/plugin flow

The most important recent hardening work landed in the four review-fix waves:

- Wave 1
  - concurrent same-payer sessions are allowed again
  - consumer network discovery now handles deterministic `Package.Networks` precedence
  - Plugin Gateway transport is split from Payment Gateway transport
  - plugin metering shutdown now has explicit timeout/drain behavior
- Wave 2
  - plugin session keep-alives are session-owned
  - plugin release is idempotent
  - provider runtime-manager bind/unbind and control replay were hardened
  - in-memory repository now returns snapshots and has stronger mutation semantics
- Wave 3
  - provider auth now fails closed when `x-sds-session-id` is missing
  - worker-plus-quota reservation became atomic in both in-memory and PostgreSQL backends
  - operator/demo docs were refreshed to the current runtime contract
- Wave 4
  - consumer `paymentSessionManager` no longer performs blocking stream sends under the main state mutex
  - bind-in-flight state and per-generation send serialization are now explicit

The MVP implementation scope is complete. Post-MVP follow-up work is tracked in
[plans/post-mvp-backlog.md](../plans/post-mvp-backlog.md), including:

- `PMVP-002` Payment-session reconnect/resume semantics, carried forward from deferred `MVP-013`
- `PMVP-003` Provider runtime decoupling for independently deployed public/private provider surfaces, carried forward from deferred `MVP-039`
- repository snapshot/runtime-construction hardening from the completed CRT review pass
- aggregate payer-level exposure control across concurrent streams
- permissionless oracle sourcing, automated collection, richer observability, and end-user funding UX

## 2. Mental Model

The repo implements four main runtime roles plus shared protocol/types:

1. Oracle
   - answers "which provider should this consumer use for this network?"
2. Consumer sidecar
   - the user-facing SDS boundary
   - exposes a Substreams-compatible ingress
   - hides provider discovery, `StartSession`, and payment-control coordination
3. Provider public control plane
   - public Payment Gateway for consumer sidecars
   - owns `StartSession`, the `PaymentSession` bidi stream, and live runtime payment decisions
4. Provider private plugin control plane
   - private Plugin Gateway for firehose-core / runtime plugins
   - serves `auth`, `session`, and `usage`

At a high level:

1. A user points Substreams tooling at the consumer sidecar.
2. The sidecar derives or validates the requested network and either:
   - uses a direct provider override, or
   - asks the oracle for a recommended provider
3. The sidecar calls provider `StartSession`.
4. The provider creates a fresh provider-side session and returns:
   - `session_id`
   - session-specific `data_plane_endpoint`
   - confirmatory pricing
5. The sidecar opens:
   - the upstream Substreams data-plane stream to the returned endpoint
   - the long-lived provider `PaymentSession` control stream
6. On the provider side, runtime plugins call the private Plugin Gateway:
   - `auth` validates the signed RAV and required session header
   - `session` borrows/returns worker capacity for the already-created SDS session
   - `usage` records metering and notifies the provider runtime manager
7. The provider runtime manager decides whether to:
   - do nothing
   - emit a `RAVRequest`
   - stop the session for low funds

The most important architectural boundary is this:

- the consumer sidecar owns client-facing ingress/runtime coordination
- the provider gateway owns provider-authoritative payment/session state
- the private Plugin Gateway is an internal adapter layer, not the business-logic owner
- the shared provider repository is the durable runtime state beneath both provider surfaces

## 3. Best Reading Order

If you want to understand the current system with minimal thrash, read in this order:

1. Product/runtime intent
   - [docs/mvp-scope.md](./mvp-scope.md)
   - [docs/provider-persistence-boundary.md](./provider-persistence-boundary.md)
   - [docs/provider-runtime-compatibility.md](./provider-runtime-compatibility.md)
2. CLI wiring and process topology
   - [cmd/sds/main.go](../cmd/sds/main.go)
   - [cmd/sds/consumer_sidecar.go](../cmd/sds/consumer_sidecar.go)
   - [cmd/sds/impl/provider_gateway.go](../cmd/sds/impl/provider_gateway.go)
   - [cmd/sds/impl/oracle.go](../cmd/sds/impl/oracle.go)
3. Consumer ingress runtime path
   - [consumer/sidecar/sidecar.go](../consumer/sidecar/sidecar.go)
   - [consumer/sidecar/ingress.go](../consumer/sidecar/ingress.go)
   - [consumer/sidecar/managed_session.go](../consumer/sidecar/managed_session.go)
   - [consumer/sidecar/discovery.go](../consumer/sidecar/discovery.go)
   - [consumer/sidecar/payment_session_manager.go](../consumer/sidecar/payment_session_manager.go)
4. Provider public gateway path
   - [provider/gateway/gateway.go](../provider/gateway/gateway.go)
   - [provider/gateway/handler_start_session.go](../provider/gateway/handler_start_session.go)
   - [provider/gateway/handler_payment_session.go](../provider/gateway/handler_payment_session.go)
   - [provider/gateway/runtime_manager.go](../provider/gateway/runtime_manager.go)
5. Provider private plugin path
   - [provider/plugin/gateway.go](../provider/plugin/gateway.go)
   - [provider/plugin/auth.go](../provider/plugin/auth.go)
   - [provider/plugin/session.go](../provider/plugin/session.go)
   - [provider/plugin/metering.go](../provider/plugin/metering.go)
   - [provider/auth/service.go](../provider/auth/service.go)
   - [provider/session/service.go](../provider/session/service.go)
   - [provider/usage/service.go](../provider/usage/service.go)
6. Storage/state model
   - [provider/repository/repository.go](../provider/repository/repository.go)
   - [provider/repository/inmemory.go](../provider/repository/inmemory.go)
   - [provider/repository/psql/](../provider/repository/psql)
7. Shared protocol/helpers
   - [sidecar/endpoint.go](../sidecar/endpoint.go)
   - [sidecar/server_transport.go](../sidecar/server_transport.go)
   - [sidecar/session.go](../sidecar/session.go)
   - [oracle/oracle.go](../oracle/oracle.go)
   - [oracle/config.go](../oracle/config.go)
   - [horizon/](../horizon)
8. The tests for the subtle parts
   - [test/integration/](../test/integration)
   - [consumer/sidecar/payment_session_manager_test.go](../consumer/sidecar/payment_session_manager_test.go)
   - [provider/gateway/runtime_manager_test.go](../provider/gateway/runtime_manager_test.go)
   - [provider/plugin/metering_test.go](../provider/plugin/metering_test.go)
   - [provider/plugin/session_test.go](../provider/plugin/session_test.go)

## 4. Repo Layout By Responsibility

### Runtime entrypoints

- [cmd/sds/main.go](../cmd/sds/main.go)
  - top-level CLI
- [cmd/sds/consumer_sidecar.go](../cmd/sds/consumer_sidecar.go)
  - consumer-side startup, ingress config validation, transport config
- [cmd/sds/impl/provider_gateway.go](../cmd/sds/impl/provider_gateway.go)
  - provider startup
  - wires the public Payment Gateway and the private Plugin Gateway
  - chooses repository backend
- [cmd/sds/impl/oracle.go](../cmd/sds/impl/oracle.go)
  - standalone oracle startup

### Consumer-side runtime

- [consumer/sidecar/](../consumer/sidecar)
  - user-facing SDS boundary
  - provider discovery
  - session bootstrap
  - Substreams ingress proxy
  - provider payment-control client

### Provider public control plane

- [provider/gateway/](../provider/gateway)
  - consumer-facing provider API
  - `StartSession`
  - `PaymentSession`
  - live provider-originated payment control

### Provider private plugin control plane

- [provider/plugin/](../provider/plugin)
  - firehose-core plugin adapters and Plugin Gateway
- [provider/auth/](../provider/auth)
  - authoritative validation of signed RAV and session headers
- [provider/session/](../provider/session)
  - worker/session slot management and quota enforcement
- [provider/usage/](../provider/usage)
  - metering ingestion into repository/runtime state

### Storage / durable state

- [provider/repository/](../provider/repository)
  - repository interface plus in-memory implementation
- [provider/repository/psql/](../provider/repository/psql)
  - PostgreSQL implementation, migrations, SQL statements

### Oracle

- [oracle/](../oracle)
  - provider catalog parsing
  - network-to-provider discovery

### Shared protocol and types

- [proto/](../proto)
  - protobuf source contracts
- [pb/](../pb)
  - generated protobuf/connect code
- [sidecar/](../sidecar)
  - shared protocol helpers used by consumer/provider/oracle
- [horizon/](../horizon)
  - RAV/domain/signature helpers

### Tests

- [test/integration/](../test/integration)
  - best place to see intended end-to-end behavior
- package-local `*_test.go`
  - best place to understand narrower service contracts and hardening work

## 5. Core Runtime Flows

### A. Oracle discovery flow

Main files:

- [cmd/sds/impl/oracle.go](../cmd/sds/impl/oracle.go)
- [oracle/config.go](../oracle/config.go)
- [oracle/oracle.go](../oracle/oracle.go)
- [consumer/sidecar/discovery.go](../consumer/sidecar/discovery.go)

Main responsibilities:

- `oracle.LoadCatalog(...)`
  - loads the curated provider catalog and per-network canonical pricing
- `Catalog.Discover(...)`
  - returns the eligible providers and deterministic recommended provider
- `Oracle.DiscoverProviders(...)`
  - serves the wire API
- `resolveRequestedNetwork(...)`
  - derives the canonical network key from package metadata and/or explicit input
- `Sidecar.resolveProviderSelection(...)`
  - chooses direct provider override or oracle-backed discovery

Important current nuance:

- the `Package.Networks` precedence contract is now implemented
- the logic is deterministic:
  - resolved `Package.Networks` entry wins
  - top-level `package.network` is fallback within package metadata
  - explicit input is fallback only when package derivation is unavailable
  - conflicting explicit/package-derived values fail fast

### B. Consumer session bootstrap flow

Main files:

- [consumer/sidecar/managed_session.go](../consumer/sidecar/managed_session.go)
- [provider/gateway/handler_start_session.go](../provider/gateway/handler_start_session.go)

Main sequence:

1. The sidecar resolves the provider.
2. It signs the initial zero-value RAV.
3. It calls provider `StartSession(...)`.
4. The provider validates the escrow account and initial RAV.
5. The provider creates a fresh repository-backed session.
6. The provider returns:
   - `session_id`
   - `data_plane_endpoint`
   - confirmatory pricing
7. The sidecar creates a local session, stores the returned pricing/RAV, and configures the local payment-session client for that provider endpoint.

Main functions to read:

- `Sidecar.bootstrapManagedSession(...)`
- `Gateway.StartSession(...)`

Important current behavior:

- every request/connection creates a fresh SDS payment session
- concurrent same-payer sessions are allowed on the same provider instance
- the provider handshake owns the session-specific data-plane endpoint
- pricing is confirmatory, not negotiable, in the normal MVP flow

### C. Consumer ingress runtime flow

Main files:

- [consumer/sidecar/sidecar.go](../consumer/sidecar/sidecar.go)
- [consumer/sidecar/ingress.go](../consumer/sidecar/ingress.go)
- [consumer/sidecar/payment_session_manager.go](../consumer/sidecar/payment_session_manager.go)

What the sidecar server does:

- serves:
  - the legacy consumer sidecar Connect service
  - gRPC handlers for Substreams v2/v3/v4 ingress
- bootstraps a fresh SDS session behind each ingress request
- connects to the provider data-plane endpoint returned by `StartSession`
- injects SDS headers (`x-sds-rav`, `x-sds-session-id`) on the upstream request
- runs the provider `PaymentSession` control loop in parallel with the data-plane stream
- resolves ambiguous upstream EOF/cancel cases against provider session status

Main functions to read:

- `Sidecar.Run(...)`
- `prepareIngressRuntime(...)`
- `runIngressPaymentControl(...)`
- `resolveIngressStreamTermination(...)`
- `proxyV2Stream(...)`
- `proxyV3Stream(...)`
- `proxyV4Stream(...)`
- `paymentSessionClient.BindSession(...)`
- `paymentSessionClient.SendRAVSubmission(...)`
- `paymentSessionClient.Receive(...)`

Important current nuance:

- the consumer `paymentSessionManager` was recently hardened
- the stream send path is no longer serialized by the main state mutex
- bind-in-flight state is explicit
- send serialization is generation-local so a wedged old stream cannot block replacement streams

Also note:

- the intended MVP runtime is the ingress path
- [consumer/sidecar/handler_init.go](../consumer/sidecar/handler_init.go) and [consumer/sidecar/handler_end_session.go](../consumer/sidecar/handler_end_session.go) still exist, but they are no longer the primary runtime architecture
- `EndSession` explicitly rejects provider-managed sessions and points callers back to ingress usage

### D. Provider PaymentSession control loop

Main files:

- [provider/gateway/handler_payment_session.go](../provider/gateway/handler_payment_session.go)
- [provider/gateway/runtime_manager.go](../provider/gateway/runtime_manager.go)
- [provider/usage/service.go](../provider/usage/service.go)

What this path does:

- binds one live `PaymentSession` stream to one provider session at a time
- replays any queued control response on reconnect/bind
- evaluates metered usage after every usage report
- emits provider-originated control messages:
  - `rav_request`
  - `need_more_funds`
  - terminal stop/continue responses
- validates exact `rav_submission` semantics against the in-flight request

Main functions to read:

- `Gateway.PaymentSession(...)`
- `runtimeManager.bindSession(...)`
- `runtimeManager.unbindSession(...)`
- `runtimeManager.onMeteredUsage(...)`
- `runtimeManager.evaluateMeteredUsage(...)`
- `runtimeManager.dispatch(...)`
- `Gateway.handleRAVSubmission(...)`

Important current nuance:

- runtime dispatch is now best-effort and non-blocking for live stream delivery
- the latest control response is retained in runtime state for later bind/replay
- bind failure and unbind cleanup are explicit enough that stale half-bound sessions should not strand the runtime manager

### E. Provider plugin path from firehose-core

Main files:

- [provider/plugin/gateway.go](../provider/plugin/gateway.go)
- [provider/plugin/auth.go](../provider/plugin/auth.go)
- [provider/plugin/session.go](../provider/plugin/session.go)
- [provider/plugin/metering.go](../provider/plugin/metering.go)
- [provider/auth/service.go](../provider/auth/service.go)
- [provider/session/service.go](../provider/session/service.go)
- [provider/usage/service.go](../provider/usage/service.go)

This path is easy to misunderstand.

The files in `provider/plugin/` are mostly plugin adapters and registration code. They are not the core provider payment logic.

The core provider business logic lives in:

- `provider/auth/service.go`
- `provider/session/service.go`
- `provider/usage/service.go`

Flow by plugin:

- Auth plugin
  - forwards untrusted headers to `AuthService.ValidateAuth(...)`
  - requires both a valid signed RAV and `x-sds-session-id`
  - returns trusted headers for downstream plugin/runtime use
- Session plugin
  - borrows/returns worker capacity against the already-created provider session
  - owns session-scoped keep-alives
  - has idempotent release semantics
- Metering plugin
  - buffers usage events
  - flushes them to `UsageService.Report(...)`
  - uses explicit report timeouts and safe shutdown/drain behavior

Transport nuance:

- the Plugin Gateway is a separate private surface from the public Payment Gateway
- it has its own transport configuration
- it should only be exposed on localhost or a private network

### F. Provider auth/session/usage service flow

Main files:

- [provider/auth/service.go](../provider/auth/service.go)
- [provider/session/service.go](../provider/session/service.go)
- [provider/usage/service.go](../provider/usage/service.go)
- [provider/repository/repository.go](../provider/repository/repository.go)

Responsibilities:

- `AuthService.ValidateAuth(...)`
  - validates `x-sds-rav`
  - requires and validates `x-sds-session-id`
  - returns trusted headers to the auth plugin
- `SessionService.BorrowWorker(...)`
  - authorizes the session
  - enforces payer quota
  - atomically creates worker state and reserves quota
- `SessionService.ReturnWorker(...)`
  - removes worker entries and decrements quota
- `SessionService.KeepAlive(...)`
  - refreshes provider-side session liveness
- `UsageService.Report(...)`
  - records metered usage in the repository
  - notifies the provider runtime manager so the public gateway can emit control decisions

These three services bridge:

- firehose-core runtime behavior
- durable provider repository state
- the public Payment Gateway payment-control loop

## 6. State Model

There are three important state layers in the repo:

### Consumer-local session state

- [sidecar/session.go](../sidecar/session.go)

Used by:

- consumer sidecar only

Purpose:

- track consumer-local RAV and usage state for the current runtime session
- drive the local payment-control client and upstream headers

This state is convenience/runtime-local. It is not authoritative for provider billing.

### Provider repository runtime and settlement state

- [provider/repository/repository.go](../provider/repository/repository.go)
- [docs/provider-persistence-boundary.md](./provider-persistence-boundary.md)

Used by:

- provider gateway
- provider auth/session/usage services
- both in-memory and PostgreSQL backends

Purpose:

- authoritative provider-side runtime/session/payment state
- durable provider-side collection lifecycle state for manual settlement workflows

Main entities:

- `Session`
  - provider-authoritative session lifecycle, usage totals, baseline snapshot, current accepted RAV
- `Worker`
  - plugin/runtime worker slot
- `QuotaUsage`
  - per-payer worker/session usage
- `UsageEvent`
  - metering event history
- `CollectionRecord`
  - settlement lifecycle state for accepted RAVs

Important boundary:

- runtime/session state and collection lifecycle state are distinct concerns
- accepted RAV durability is part of the runtime/session model
- collection lifecycle records track settlement progress as `collectible`, `collect_pending`, `collected`, or `collect_failed_retryable`
- live `PaymentSession` stream bindings remain process-local even when repository state is durable

### Provider live in-memory runtime binding state

- [provider/gateway/runtime_manager.go](../provider/gateway/runtime_manager.go)

Purpose:

- track live `PaymentSession` stream bindings
- keep in-flight `pendingRAV` request state
- queue the latest provider control response for replay on rebind

This state is intentionally process-local and not a durable settlement record.

## 7. File-Level Guide By Subsystem

### Consumer sidecar

- [consumer/sidecar/sidecar.go](../consumer/sidecar/sidecar.go)
  - top-level server object
  - HTTP/2 transport and ingress registration
- [consumer/sidecar/ingress.go](../consumer/sidecar/ingress.go)
  - actual Substreams proxy runtime
  - control/data-plane coordination
  - ambiguous termination resolution
- [consumer/sidecar/managed_session.go](../consumer/sidecar/managed_session.go)
  - provider selection
  - `StartSession` handshake
  - local session creation
- [consumer/sidecar/discovery.go](../consumer/sidecar/discovery.go)
  - oracle integration
  - network normalization and conflict rules
- [consumer/sidecar/payment_session_manager.go](../consumer/sidecar/payment_session_manager.go)
  - one provider control client per SDS session
  - owns stream lifecycle, bind-in-flight state, per-generation send serialization

### Provider public gateway

- [provider/gateway/gateway.go](../provider/gateway/gateway.go)
  - gateway construction
  - repository/runtime ownership
  - explicit repository requirement
- [provider/gateway/handler_start_session.go](../provider/gateway/handler_start_session.go)
  - session handshake
  - initial RAV validation
  - fresh session creation
- [provider/gateway/handler_payment_session.go](../provider/gateway/handler_payment_session.go)
  - bidi stream lifecycle
  - exact RAV submission handling
- [provider/gateway/runtime_manager.go](../provider/gateway/runtime_manager.go)
  - live runtime control state
  - pending-RAV and queued-response ownership
  - non-blocking control delivery

### Provider private gateway and plugin adapters

- [provider/plugin/gateway.go](../provider/plugin/gateway.go)
  - private Auth/Session/Usage serving surface
  - transport split from public gateway
- [provider/plugin/auth.go](../provider/plugin/auth.go)
  - auth plugin adapter
- [provider/plugin/session.go](../provider/plugin/session.go)
  - session plugin adapter
  - session-scoped keep-alives
  - idempotent release
- [provider/plugin/metering.go](../provider/plugin/metering.go)
  - metering plugin adapter
  - shutdown, buffering, report timeout policy

### Provider services

- [provider/auth/service.go](../provider/auth/service.go)
  - authoritative auth/session-header validation
- [provider/session/service.go](../provider/session/service.go)
  - quota and worker slot service
  - atomic worker-plus-quota reservation
- [provider/usage/service.go](../provider/usage/service.go)
  - metering ingestion
  - provider runtime notification

### Repository

- [provider/repository/repository.go](../provider/repository/repository.go)
  - storage contract
  - canonical runtime state shapes
- [provider/repository/inmemory.go](../provider/repository/inmemory.go)
  - local/dev implementation
  - snapshot-returning getters and stronger concurrent mutation behavior
- [provider/repository/psql/](../provider/repository/psql)
  - database-backed implementation
  - migrations and SQL statements

### Shared helpers worth reading

- [sidecar/endpoint.go](../sidecar/endpoint.go)
  - endpoint parsing and client transport helpers
- [sidecar/server_transport.go](../sidecar/server_transport.go)
  - shared TLS/plaintext contract for servers
- [headers.go](../headers.go)
  - SDS protocol headers
- [horizon/](../horizon)
  - RAV signing, verification, aggregation, EIP-712 helpers

## 8. Best Tests To Read

If you want current intended behavior rather than old architectural speculation, read these tests:

- [test/integration/consumer_ingress_test.go](../test/integration/consumer_ingress_test.go)
  - real ingress bootstrap/proxy behavior
- [test/integration/firecore_test.go](../test/integration/firecore_test.go)
  - local-first external runtime compatibility path
- [test/integration/payment_session_binding_test.go](../test/integration/payment_session_binding_test.go)
  - payment-session bind/rebind behavior
- [test/integration/payment_session_low_funds_test.go](../test/integration/payment_session_low_funds_test.go)
  - low-funds runtime stop behavior
- [test/integration/payment_session_rav_request_test.go](../test/integration/payment_session_rav_request_test.go)
  - provider-originated RAV request flow
- [test/integration/provider_gateway_auth_test.go](../test/integration/provider_gateway_auth_test.go)
  - auth/session header expectations
- [consumer/sidecar/discovery_test.go](../consumer/sidecar/discovery_test.go)
  - network derivation precedence rules
- [consumer/sidecar/payment_session_manager_test.go](../consumer/sidecar/payment_session_manager_test.go)
  - consumer control-stream ownership and wedged-generation recovery
- [provider/gateway/handler_start_session_test.go](../provider/gateway/handler_start_session_test.go)
  - same-payer concurrent session regression coverage
- [provider/gateway/runtime_manager_test.go](../provider/gateway/runtime_manager_test.go)
  - queued-response replay and non-blocking dispatch
- [provider/plugin/metering_test.go](../provider/plugin/metering_test.go)
  - metering shutdown/timeout semantics
- [provider/plugin/session_test.go](../provider/plugin/session_test.go)
  - keep-alive ownership and idempotent release
- [provider/session/service_test.go](../provider/session/service_test.go)
  - atomic quota reservation behavior

## 9. What Is Generated Vs Hand-Written

Generally hand-written:

- `cmd/`
- `consumer/`
- `provider/`
- `oracle/`
- `sidecar/`
- `horizon/`
- `docs/`
- `plans/`

Generally generated or data-only:

- [pb/](../pb)
  - generated protobuf/connect code
- [contracts/artifacts/](../contracts/artifacts)
  - ABI/artifact JSON

Normally do not start by reading generated code unless you are verifying exact wire shapes.

Read the `.proto` files instead if you need the contract:

- [proto/graph/substreams/data_service/provider/v1/gateway.proto](../proto/graph/substreams/data_service/provider/v1/gateway.proto)
- [proto/graph/substreams/data_service/oracle/v1/oracle.proto](../proto/graph/substreams/data_service/oracle/v1/oracle.proto)
- [proto/graph/substreams/data_service/sds/auth/v1/auth.proto](../proto/graph/substreams/data_service/sds/auth/v1/auth.proto)
- [proto/graph/substreams/data_service/sds/session/v1/session.proto](../proto/graph/substreams/data_service/sds/session/v1/session.proto)
- [proto/graph/substreams/data_service/sds/usage/v1/usage.proto](../proto/graph/substreams/data_service/sds/usage/v1/usage.proto)

## 10. Where To Look For Current Status

If you finish reading the implementation and want to know current MVP status or
post-MVP work, use these documents in this order:

1. [plans/post-mvp-backlog.md](../plans/post-mvp-backlog.md)
2. [docs/provider-persistence-boundary.md](./provider-persistence-boundary.md)
3. [docs/provider-runtime-compatibility.md](./provider-runtime-compatibility.md)
4. [plans/archive/mvp-implementation-backlog.md](../plans/archive/mvp-implementation-backlog.md) for MVP task history

The most important thing to remember is:

- the old current-implementation review mostly explains why the recent hardening work happened
- the post-MVP backlog explains what remains
- the MVP backlog is archived as the completed execution record
- the current code and tests are the source of truth for current behavior
