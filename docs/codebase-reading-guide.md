# SDS Codebase Reading Guide

This document is a developer-oriented map of the current SDS codebase.

It is meant to help you read the implementation efficiently, not replace reading the code.

It focuses on:

- what each major subsystem does
- which files and functions are the main entrypoints
- how consumer, provider, oracle, and plugin pieces interact
- which files are implementation-critical versus generated or support code
- where the current implementation is known to be wrong or unstable, based on the current review work

For current product intent, use [docs/mvp-scope.md](./mvp-scope.md).

For current implementation issues and remediation tasks, use:

- [plans/current-implementation-review.md](../plans/current-implementation-review.md)
- [plans/current-implementation-review-tasks/](../plans/current-implementation-review-tasks/)

## 1. Mental Model

The repo implements three main runtime roles plus shared types/helpers:

1. Oracle
   - answers "which provider should this consumer use for this network?"
2. Consumer sidecar
   - exposes a Substreams-compatible ingress
   - hides SDS-specific discovery, session bootstrap, and payment/control coordination
3. Provider side
   - public Payment Gateway for consumer sidecars
   - private Plugin Gateway for firehose-core SDS plugins (`auth`, `session`, `usage`)

At a high level:

1. A user points Substreams tooling at the consumer sidecar.
2. The sidecar either:
   - uses a direct provider endpoint, or
   - asks the oracle for a provider
3. The sidecar opens a provider payment session through the Payment Gateway.
4. The sidecar proxies the Substreams stream to the provider data-plane endpoint, while also maintaining the SDS payment/control loop.
5. On the provider side, firehose-core plugins call the private Plugin Gateway:
   - `auth` validates incoming SDS headers
   - `session` borrows/returns worker capacity
   - `usage` reports metered usage
6. Provider-side metering drives provider-authoritative runtime control through the Payment Gateway `PaymentSession` bidi stream.

## 2. Best Reading Order

If you want to understand the system with minimal thrash, read in this order:

1. Product/runtime intent
   - [docs/mvp-scope.md](./mvp-scope.md)
2. CLI wiring
   - [cmd/sds/main.go](../cmd/sds/main.go)
   - [cmd/sds/consumer_sidecar.go](../cmd/sds/consumer_sidecar.go)
   - [cmd/sds/impl/provider_gateway.go](../cmd/sds/impl/provider_gateway.go)
   - [cmd/sds/impl/oracle.go](../cmd/sds/impl/oracle.go)
3. Consumer-side runtime path
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
   - [provider/auth/service.go](../provider/auth/service.go)
   - [provider/session/service.go](../provider/session/service.go)
   - [provider/usage/service.go](../provider/usage/service.go)
   - [provider/plugin/auth.go](../provider/plugin/auth.go)
   - [provider/plugin/session.go](../provider/plugin/session.go)
   - [provider/plugin/metering.go](../provider/plugin/metering.go)
6. Storage/state model
   - [provider/repository/repository.go](../provider/repository/repository.go)
   - [provider/repository/inmemory.go](../provider/repository/inmemory.go)
   - [provider/repository/psql/](../provider/repository/psql)
7. Shared protocol/helpers
   - [headers.go](../headers.go)
   - [sidecar/endpoint.go](../sidecar/endpoint.go)
   - [sidecar/session.go](../sidecar/session.go)
   - [oracle/oracle.go](../oracle/oracle.go)
   - [oracle/config.go](../oracle/config.go)

## 3. Repo Layout By Responsibility

### Runtime / server entrypoints

- [cmd/sds/main.go](../cmd/sds/main.go)
  - top-level CLI
- [cmd/sds/consumer_sidecar.go](../cmd/sds/consumer_sidecar.go)
  - consumer-side server startup and config validation
- [cmd/sds/impl/provider_gateway.go](../cmd/sds/impl/provider_gateway.go)
  - provider startup
  - wires both Payment Gateway and Plugin Gateway
- [cmd/sds/impl/oracle.go](../cmd/sds/impl/oracle.go)
  - oracle startup

### Consumer-side runtime

- [consumer/sidecar/](../consumer/sidecar)
  - user-facing SDS boundary
  - ingress proxy
  - provider discovery
  - managed local session state
  - PaymentSession client

### Provider public control plane

- [provider/gateway/](../provider/gateway)
  - consumer-facing provider API
  - session bootstrap
  - PaymentSession bidi stream
  - provider-originated runtime payment control

### Provider private plugin control plane

- [provider/plugin/](../provider/plugin)
  - firehose-core plugin adapters
  - private Plugin Gateway
- [provider/auth/](../provider/auth)
  - validates RAV/session headers from plugin-auth requests
- [provider/session/](../provider/session)
  - worker/session slot management
- [provider/usage/](../provider/usage)
  - metering ingestion

### Storage / durable state

- [provider/repository/](../provider/repository)
  - repository interface plus in-memory implementation
- [provider/repository/psql/](../provider/repository/psql)
  - PostgreSQL implementation

### Oracle

- [oracle/](../oracle)
  - provider catalog
  - network-to-provider discovery

### Shared protocol and types

- [proto/](../proto)
  - source protobuf contracts
- [pb/](../pb)
  - generated code, generally not hand-edited
- [sidecar/](../sidecar)
  - shared helpers used by consumer/provider/oracle
- [horizon/](../horizon)
  - RAV/domain/signature helpers

### Tests

- [test/integration/](../test/integration)
  - best place to understand intended end-to-end behavior
- package-local `*_test.go`
  - best place to understand smaller service contracts

## 4. Core Runtime Flows

### A. Oracle discovery flow

Main files:

- [cmd/sds/impl/oracle.go](../cmd/sds/impl/oracle.go)
- [oracle/config.go](../oracle/config.go)
- [oracle/oracle.go](../oracle/oracle.go)
- [consumer/sidecar/discovery.go](../consumer/sidecar/discovery.go)

Main responsibilities:

- `oracle.LoadCatalog(...)`
  - loads static provider/network config
- `Oracle.DiscoverProviders(...)`
  - returns:
    - canonical pricing
    - eligible providers
    - one recommended provider
- `Sidecar.resolveProviderSelection(...)`
  - uses direct provider override if configured
  - otherwise resolves network and asks the oracle

Important current nuance:

- the intended contract says `Package.Networks` should take precedence over top-level `package.network`
- current code only looks at `pkg.GetNetwork()`
- that gap is tracked by `CRT-07`

### B. Consumer session bootstrap flow

Main files:

- [consumer/sidecar/managed_session.go](../consumer/sidecar/managed_session.go)
- [consumer/sidecar/discovery.go](../consumer/sidecar/discovery.go)
- [provider/gateway/handler_start_session.go](../provider/gateway/handler_start_session.go)

Main sequence:

1. Consumer sidecar resolves the provider.
2. It signs an initial zero-value RAV with `Sidecar.signRAV(...)`.
3. It calls provider `StartSession(...)`.
4. The provider validates the request and creates repository session state.
5. The provider returns:
   - `session_id`
   - `data_plane_endpoint`
   - pricing confirmation
6. The sidecar creates local session state and stores the provider endpoint in `paymentSessionManager`.

Main functions to read:

- `Sidecar.bootstrapManagedSession(...)`
- `Gateway.StartSession(...)`

Important current issue:

- `StartSession(...)` currently force-terminates other active sessions for the same payer on the same instance
- that is contrary to the intended MVP behavior
- tracked by `CRT-01`

### C. Consumer ingress proxy flow

Main files:

- [consumer/sidecar/sidecar.go](../consumer/sidecar/sidecar.go)
- [consumer/sidecar/ingress.go](../consumer/sidecar/ingress.go)

What the sidecar server does:

- serves:
  - Connect handler for SDS consumer service
  - gRPC handlers for Substreams v2/v3/v4 ingress
- chooses the upstream provider per request
- injects SDS headers on the upstream provider request
- proxies provider stream responses back to the client

Main functions to read:

- `Sidecar.Run(...)`
- `ingressV2Server.Blocks(...)`
- `ingressV3Server.Blocks(...)`
- `ingressV4Server.Blocks(...)`
- `Sidecar.proxyV2Stream(...)`
- `Sidecar.proxyV3Stream(...)`
- `Sidecar.proxyV4Stream(...)`

Key conceptual point:

- the sidecar is the user-facing runtime boundary
- the user does not manually drive payment/session RPCs in the intended MVP flow
- the sidecar hides provider selection, `StartSession`, and `PaymentSession` coordination

### D. Provider PaymentSession control loop

Main files:

- [provider/gateway/handler_payment_session.go](../provider/gateway/handler_payment_session.go)
- [provider/gateway/runtime_manager.go](../provider/gateway/runtime_manager.go)
- [consumer/sidecar/payment_session_manager.go](../consumer/sidecar/payment_session_manager.go)

What this path does:

- consumer sidecar opens one long-lived `PaymentSession` bidi stream per SDS session
- the provider binds that stream to runtime session state
- provider-originated control messages are sent on that stream:
  - `rav_request`
  - `need_more_funds`
  - stop/continue responses
- consumer sidecar responds with RAV submissions and funds acknowledgments

Main functions to read:

- `Gateway.PaymentSession(...)`
- `runtimeManager.bindSession(...)`
- `runtimeManager.onMeteredUsage(...)`
- `runtimeManager.evaluateMeteredUsage(...)`
- `paymentSessionClient.BindSession(...)`
- `paymentSessionClient.SendRAVSubmission(...)`
- `paymentSessionClient.Receive(...)`

This is one of the most important parts of the codebase.

It is also one of the least settled parts of the implementation right now.

Known current issues here are tracked by:

- `CRT-02`
- `CRT-03`
- `CRT-09`

### E. Provider plugin path from firehose-core

Main files:

- [provider/plugin/auth.go](../provider/plugin/auth.go)
- [provider/plugin/session.go](../provider/plugin/session.go)
- [provider/plugin/metering.go](../provider/plugin/metering.go)
- [provider/plugin/gateway.go](../provider/plugin/gateway.go)
- [provider/auth/service.go](../provider/auth/service.go)
- [provider/session/service.go](../provider/session/service.go)
- [provider/usage/service.go](../provider/usage/service.go)

This path is easy to misunderstand.

The files in `provider/plugin/` are mostly client adapters loaded by firehose-core through plugin schemes like `sds://...`.

They are not the core business logic.

The actual provider logic lives in:

- `provider/auth/service.go`
- `provider/session/service.go`
- `provider/usage/service.go`

Flow by plugin:

- Auth plugin
  - forwards untrusted headers to provider `AuthService.ValidateAuth(...)`
  - trusted session metadata comes back from the provider
- Session plugin
  - borrows/returns worker capacity from `SessionService`
  - runs keep-alive behavior
- Metering plugin
  - buffers usage events and reports them to `UsageService.Report(...)`

The private Plugin Gateway exists only to serve those three services internally.

### F. Provider auth/session/usage service flow

Main files:

- [provider/auth/service.go](../provider/auth/service.go)
- [provider/session/service.go](../provider/session/service.go)
- [provider/usage/service.go](../provider/usage/service.go)
- [provider/repository/repository.go](../provider/repository/repository.go)

Responsibilities:

- `AuthService.ValidateAuth(...)`
  - validates `x-sds-rav`
  - optionally validates `x-sds-session-id`
  - returns trusted headers to the auth plugin
- `SessionService.BorrowWorker(...)`
  - authorizes the session
  - enforces payer quota
  - creates worker entries
- `SessionService.ReturnWorker(...)`
  - removes worker entries and decrements quota
- `SessionService.KeepAlive(...)`
  - refreshes session liveness
- `UsageService.Report(...)`
  - records metered usage in the repository
  - notifies provider runtime so the gateway can decide whether to request a RAV or stop for low funds

These three services are the provider-side bridge between:

- firehose-core runtime behavior
- provider repository state
- the public Payment Gateway control loop

## 5. State Model

There are two session models in the repo:

### Consumer-local session model

- [sidecar/session.go](../sidecar/session.go)

Used by:

- consumer sidecar only

Purpose:

- track local consumer-side session and RAV state
- not authoritative for provider billing

### Provider repository session model

- [provider/repository/repository.go](../provider/repository/repository.go)

Used by:

- provider gateway
- provider auth/session/usage services

Purpose:

- authoritative provider-side session/payment/usage state

Other important repository entities:

- `Worker`
  - plugin-side runtime worker slot
- `QuotaUsage`
  - per-payer active session/worker counts
- `UsageEvent`
  - metering event accumulated into session usage

Important boundary:

- consumer-side session state is convenience/runtime-local
- provider-side repository state is the authoritative provider payment/runtime state

## 6. File-Level Guide By Subsystem

### Consumer sidecar

- [consumer/sidecar/sidecar.go](../consumer/sidecar/sidecar.go)
  - top-level server object
  - networking/transport setup
  - gRPC ingress registration
- [consumer/sidecar/ingress.go](../consumer/sidecar/ingress.go)
  - actual Substreams proxy runtime
  - bootstrap, upstream connection, EOF/control resolution
- [consumer/sidecar/managed_session.go](../consumer/sidecar/managed_session.go)
  - provider selection
  - `StartSession` handshake
  - local session creation
- [consumer/sidecar/discovery.go](../consumer/sidecar/discovery.go)
  - oracle integration
  - network normalization and provider selection
- [consumer/sidecar/payment_session_manager.go](../consumer/sidecar/payment_session_manager.go)
  - one client per SDS session
  - owns provider PaymentSession client stream
  - currently has lock-scope problems

### Provider public gateway

- [provider/gateway/gateway.go](../provider/gateway/gateway.go)
  - gateway construction
  - transport setup
  - runtime manager ownership
- [provider/gateway/handler_start_session.go](../provider/gateway/handler_start_session.go)
  - session handshake
  - current same-payer termination bug
- [provider/gateway/handler_payment_session.go](../provider/gateway/handler_payment_session.go)
  - bidi stream request/response handling
  - provider control loop entrypoint
- [provider/gateway/runtime_manager.go](../provider/gateway/runtime_manager.go)
  - live runtime binding state
  - pending RAV request state
  - metered-usage evaluation and dispatch

### Provider private gateway and plugin adapters

- [provider/plugin/gateway.go](../provider/plugin/gateway.go)
  - serves Auth/Session/Usage handlers privately
  - currently hardcoded plaintext
- [provider/plugin/auth.go](../provider/plugin/auth.go)
  - auth plugin adapter
- [provider/plugin/session.go](../provider/plugin/session.go)
  - session plugin adapter
  - current keep-alive/release ownership problems
- [provider/plugin/metering.go](../provider/plugin/metering.go)
  - metering plugin adapter
  - current shutdown problems

### Provider services

- [provider/auth/service.go](../provider/auth/service.go)
  - authoritative validation of incoming payment/session headers
- [provider/session/service.go](../provider/session/service.go)
  - quota and worker slot service
- [provider/usage/service.go](../provider/usage/service.go)
  - metering ingestion to repository/runtime

### Repository

- [provider/repository/repository.go](../provider/repository/repository.go)
  - storage contract
  - state shapes
- [provider/repository/inmemory.go](../provider/repository/inmemory.go)
  - simple local implementation
  - currently violates some of its own concurrency promises
- [provider/repository/psql/](../provider/repository/psql)
  - database-backed implementation

### Shared helpers worth reading

- [sidecar/endpoint.go](../sidecar/endpoint.go)
  - endpoint parsing and HTTP/GRPC transport clients
- [sidecar/server_transport.go](../sidecar/server_transport.go)
  - server-side plaintext/TLS configuration contract
- [headers.go](../headers.go)
  - SDS protocol headers
- [horizon/](../horizon)
  - typed-data signing and verification helpers

## 7. What Is Generated Vs Hand-Written

Generally hand-written:

- `cmd/`
- `consumer/`
- `provider/`
- `oracle/`
- `sidecar/`
- `horizon/`
- `plans/`
- `docs/`

Generally generated or data-only:

- [pb/](../pb)
  - generated from protobuf
- [contracts/artifacts/](../contracts/artifacts)
  - ABI/artifact JSON

Normally do not start by reading generated code unless you are verifying exact wire shapes.

Read the `.proto` files instead if you need the contract:

- [proto/graph/substreams/data_service/provider/v1/gateway.proto](../proto/graph/substreams/data_service/provider/v1/gateway.proto)
- [proto/graph/substreams/data_service/oracle/v1/oracle.proto](../proto/graph/substreams/data_service/oracle/v1/oracle.proto)
- [proto/graph/substreams/data_service/sds/auth/v1/auth.proto](../proto/graph/substreams/data_service/sds/auth/v1/auth.proto)
- [proto/graph/substreams/data_service/sds/session/v1/session.proto](../proto/graph/substreams/data_service/sds/session/v1/session.proto)
- [proto/graph/substreams/data_service/sds/usage/v1/usage.proto](../proto/graph/substreams/data_service/sds/usage/v1/usage.proto)

## 8. Where The Current Implementation Is Known To Be Wrong

This section is intentionally direct.

The codebase is already substantial and usable, but several important areas are known to be wrong, misleading, or unstable.

The current review work does not treat these as hypothetical concerns.

They are tracked as concrete remediation tasks.

### Consumer/provider contract issues

- `CRT-01`
  - [plans/current-implementation-review-tasks/CRT-01.md](../plans/current-implementation-review-tasks/CRT-01.md)
  - `StartSession(...)` wrongly terminates other active same-payer sessions
- `CRT-07`
  - [plans/current-implementation-review-tasks/CRT-07.md](../plans/current-implementation-review-tasks/CRT-07.md)
  - discovery does not yet implement the intended `Package.Networks` precedence contract

### Provider lifecycle / control-loop issues

- `CRT-02`
  - [plans/current-implementation-review-tasks/CRT-02.md](../plans/current-implementation-review-tasks/CRT-02.md)
  - plugin session keep-alive/release ownership is wrong
- `CRT-03`
  - [plans/current-implementation-review-tasks/CRT-03.md](../plans/current-implementation-review-tasks/CRT-03.md)
  - runtime-manager bind/unbind and dispatch behavior is not cleanly owned
- `CRT-04`
  - [plans/current-implementation-review-tasks/CRT-04.md](../plans/current-implementation-review-tasks/CRT-04.md)
  - metering emitter shutdown can panic or hang
- `CRT-09`
  - [plans/current-implementation-review-tasks/CRT-09.md](../plans/current-implementation-review-tasks/CRT-09.md)
  - consumer PaymentSession client sends under lock

### Repository / identity / quota issues

- `CRT-05A`
  - [plans/current-implementation-review-tasks/CRT-05A.md](../plans/current-implementation-review-tasks/CRT-05A.md)
  - repository semantics and runtime-construction posture are not honest enough
- `CRT-05B`
  - [plans/current-implementation-review-tasks/CRT-05B.md](../plans/current-implementation-review-tasks/CRT-05B.md)
  - auth/session identity and quota enforcement contract is inconsistent

### Transport / operator guidance issues

- `CRT-06`
  - [plans/current-implementation-review-tasks/CRT-06.md](../plans/current-implementation-review-tasks/CRT-06.md)
  - Plugin Gateway transport is effectively plaintext-only today
- `CRT-08`
  - [plans/current-implementation-review-tasks/CRT-08.md](../plans/current-implementation-review-tasks/CRT-08.md)
  - operator/demo docs and generated commands have drifted from the actual runtime shape

## 9. Suggested Manual Reading Passes

If you want to review the code manually without drowning in it, do it in passes:

### Pass 1: Runtime intent and top-level wiring

Read:

- [docs/mvp-scope.md](./mvp-scope.md)
- [cmd/sds/main.go](../cmd/sds/main.go)
- [cmd/sds/consumer_sidecar.go](../cmd/sds/consumer_sidecar.go)
- [cmd/sds/impl/provider_gateway.go](../cmd/sds/impl/provider_gateway.go)
- [cmd/sds/impl/oracle.go](../cmd/sds/impl/oracle.go)

Goal:

- understand which binaries/servers exist
- understand public vs private surfaces

### Pass 2: Consumer request path

Read:

- [consumer/sidecar/discovery.go](../consumer/sidecar/discovery.go)
- [consumer/sidecar/managed_session.go](../consumer/sidecar/managed_session.go)
- [consumer/sidecar/ingress.go](../consumer/sidecar/ingress.go)
- [consumer/sidecar/payment_session_manager.go](../consumer/sidecar/payment_session_manager.go)

Goal:

- understand how a user request becomes:
  - provider selection
  - provider session bootstrap
  - upstream streaming
  - provider control coordination

### Pass 3: Provider public gateway path

Read:

- [provider/gateway/handler_start_session.go](../provider/gateway/handler_start_session.go)
- [provider/gateway/handler_payment_session.go](../provider/gateway/handler_payment_session.go)
- [provider/gateway/runtime_manager.go](../provider/gateway/runtime_manager.go)

Goal:

- understand provider session creation
- understand how metering-driven runtime control is supposed to work

### Pass 4: Provider plugin path

Read:

- [provider/plugin/gateway.go](../provider/plugin/gateway.go)
- [provider/auth/service.go](../provider/auth/service.go)
- [provider/session/service.go](../provider/session/service.go)
- [provider/usage/service.go](../provider/usage/service.go)
- [provider/plugin/auth.go](../provider/plugin/auth.go)
- [provider/plugin/session.go](../provider/plugin/session.go)
- [provider/plugin/metering.go](../provider/plugin/metering.go)

Goal:

- understand how firehose-core reaches SDS logic
- distinguish adapters from business logic

### Pass 5: State and persistence

Read:

- [provider/repository/repository.go](../provider/repository/repository.go)
- [provider/repository/inmemory.go](../provider/repository/inmemory.go)
- a few files in [provider/repository/psql/](../provider/repository/psql)

Goal:

- understand which state is authoritative
- understand where concurrency/persistence semantics matter

### Pass 6: Review the known wrong parts

Read:

- [plans/current-implementation-review.md](../plans/current-implementation-review.md)
- each `CRT-*` doc relevant to the area you just read

Goal:

- avoid mistaking current behavior for intended design

## 10. Tests Worth Reading

If you want examples of intended behavior, these are high-value:

- [test/integration/consumer_ingress_test.go](../test/integration/consumer_ingress_test.go)
- [test/integration/payment_session_binding_test.go](../test/integration/payment_session_binding_test.go)
- [test/integration/payment_session_low_funds_test.go](../test/integration/payment_session_low_funds_test.go)
- [test/integration/payment_session_rav_request_test.go](../test/integration/payment_session_rav_request_test.go)
- [test/integration/firecore_test.go](../test/integration/firecore_test.go)
- [consumer/sidecar/discovery_test.go](../consumer/sidecar/discovery_test.go)
- [provider/session/service_test.go](../provider/session/service_test.go)
- [provider/plugin/session_test.go](../provider/plugin/session_test.go)
- [provider/gateway/repository_test.go](../provider/gateway/repository_test.go)

Use tests for two things:

- to see what the code is trying to guarantee
- to notice where coverage is still missing around the currently known bad areas

## 11. Practical Advice While Reading

- Do not assume current code always matches the intended contract.
- Treat `plans/current-implementation-review.md` as the correction layer while reading.
- When reading a plugin file under `provider/plugin/`, ask whether it is:
  - an adapter loaded by firehose-core, or
  - actual provider business logic
- When reading session state, always ask whether it is:
  - consumer-local convenience state, or
  - provider-authoritative repository state
- When reading transport code, distinguish:
  - public Payment Gateway
  - private Plugin Gateway
  - user-facing consumer sidecar ingress
  - oracle

The main implementation complexity today is not in isolated algorithms.

It is in ownership boundaries:

- who owns session lifetime
- who owns bidi stream lifetime
- where metering becomes runtime control
- where storage contracts are relied on as atomic or concurrency-safe

That is also why most of the current remediation tasks are about lifecycle, synchronization, and contract correctness, not missing raw functionality.
