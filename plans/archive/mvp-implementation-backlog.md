# Substreams Data Service — MVP Implementation Backlog

Archived note: this is the completed MVP execution record. The active post-MVP
follow-up tracker is `plans/post-mvp-backlog.md`.

_Last updated: 2026-05-25_

This document translates [docs/mvp-scope.md](../../docs/mvp-scope.md) into concrete implementation tasks for the MVP.

It is intentionally separate from `plans/archive/implementation-backlog.md`.

Rationale for the split:

- `plans/archive/implementation-backlog.md` reflects the earlier implementation sequence and still contains useful historical context
- this document reflects the revised MVP scope agreed after the 2026-03-24 architecture rewrite
- the backlog now also incorporates provider/runtime work that landed separately in StreamingFast commits `5ffca3d` through `1416020`

This document is a scope-aligned execution backlog, not a priority list.

## How To Use This Document

- Use [docs/mvp-scope.md](../../docs/mvp-scope.md) as the stable target-state definition.
- Use `plans/archive/mvp-gap-analysis.md` for final MVP readiness/current-state assessment.
- Use this file as the concrete MVP execution record and task index.

Each task includes:

- **Context**: why the task exists
- **Assumptions**: scope-aligned assumptions that shape the task
- **Done when**: objective completion criteria
- **Verify**: how to corroborate the behavior

The status tracker below also includes:

- **Depends on**: tasks that should be frozen or completed first so downstream work does not build on moving semantics
- **Scenarios**: acceptance scenarios from [docs/mvp-scope.md](../../docs/mvp-scope.md) (`A` through `G`) that the task materially contributes to

Unless otherwise scoped, the baseline validation for code changes remains:

- `go test ./...`
- `go vet ./...`
- `gofmt` on changed Go files

Recent provider persistence and integration scaffolding landed outside the original backlog sequencing. The tracker below treats that work as existing foundation and updates task status accordingly.

## Assumptions Register

These assumptions are referenced by task ID so it is clear which scope decisions and documented deferrals still matter.

- `A1` Chain/network discovery input is frozen for MVP.
  - Consumer sidecar derives network from the Substreams package by default.
  - If a package/module resolves a specific `networks` entry, that takes precedence over top-level `network`.
  - Explicit input remains supported as fallback when package derivation is unavailable.
  - If both explicit input and derived package network exist and differ after normalization, fail fast.
  - If neither source yields a usable network, fail fast.
  - SDS should use repo-owned/pinned mappings to the Graph networks registry keys for MVP rather than live runtime registry lookups.

- `A2` Pricing is oracle-authoritative for MVP.
  - The oracle returns canonical pricing for the curated provider set.
  - The provider handshake is not a price negotiation step in normal operation.
  - The consumer should not discover price disagreement only after connecting to a selected provider.

- `A3` Every new request/connection creates a fresh SDS payment session for MVP.
  - No resumable payment-session semantics are required for MVP.
  - No RAV or payment-session reuse occurs across reconnects.
  - Any Substreams cursor/block continuation remains a normal data-plane concern, not an SDS payment-session recovery flow.

- `A4` Observability floor is frozen for MVP.
  - MVP work should include structured logs, operator inspection/status tooling, and basic Prometheus-style metrics endpoints.
  - Full distributed tracing and backend-specific observability hardening remain post-MVP unless required by a named target provider environment.

- `A5` Admin/operator authentication contract is frozen, with provider gateway auth wiring and concrete provider operator APIs in place.
  - MVP work should require authentication using the shared bearer-token role contract from `MVP-028`.
  - Minimal runtime-coordination status used by the consumer sidecar may remain separate from richer operator inspection APIs.
  - Rich provider operator/admin inspection and mutation APIs must use the shared bearer-token role contract from `MVP-028`.

- `A6` MVP funding-control logic is intentionally session-local.
  - Do not require aggregate concurrent-stream liability tracking to complete MVP.

## Status Values

- `not_started`
- `in_progress`
- `blocked`
- `open_question`
- `done`
- `deferred`

`open_question` tasks still need a concrete output:

- a documented decision, narrowed contract, or explicit recorded deferral that downstream implementation tasks can reference

## Status Tracker

| ID | Status | Area | Assumptions | Depends on | Scenarios | Task |
| --- | --- | --- | --- | --- | --- | --- |
| MVP-001 | `done` | protocol | `A2` | none | `A` | Freeze the oracle-authoritative MVP pricing contract across oracle, consumer, and provider flows |
| MVP-002 | `done` | protocol | `A2`, `A3` | `MVP-033` | `A`, `B` | Freeze fresh-session init semantics and provider-returned data-plane endpoint behavior |
| MVP-003 | `done` | protocol | `A3`, `A6` | `MVP-027` | `D`, `F` | Define and document the provider-side runtime persistence model and its boundary with settlement lifecycle tracking |
| MVP-004 | `done` | protocol | `A2`, `A3` | none | `A`, `C` | Define and document the real runtime payment contract used by the public payment gateway, private plugin gateway, and consumer/provider payment loop |
| MVP-005 | `done` | oracle | `A1`, `A2`, `A5` | `MVP-033` | `A` | Implement a standalone oracle service with manual whitelist, canonical pricing, recommended-provider response, and control-plane endpoint return |
| MVP-006 | `done` | oracle | `A5` | `MVP-028` | `A`, `G` | Add admin-only oracle whitelist/provider metadata management workflow for the curated MVP provider set |
| MVP-007 | `done` | consumer | `A1`, `A2`, `A3` | `MVP-005`, `MVP-033` | `A` | Integrate consumer sidecar with oracle discovery while preserving direct-provider fallback and provider-returned data-plane resolution |
| MVP-008 | `done` | provider-state | `A3`, `A6` | `MVP-003` | `D`, `F` | Complete durable provider runtime storage for sessions, usage, and accepted RAV state, distinct from collection lifecycle tracking |
| MVP-009 | `done` | provider-state | `A3`, `A5` | `MVP-003`, `MVP-022`, `MVP-029` | `D`, `F` | Expose authenticated provider inspection and settlement-data retrieval APIs for accepted and collectible RAV state |
| MVP-010 | `done` | funding-control | `A6` | `MVP-004` | `C` | Implement session-local low-funds detection and provider terminal stop behavior during streaming |
| MVP-011 | `done` | funding-control | `A6` | `MVP-010` | `C` | Propagate provider low-funds stop decisions through consumer sidecar into the real ingress/client path |
| MVP-012 | `done` | funding-control | none | `MVP-004` | `A`, `C` | Add deterministic cost-based RAV issuance thresholds suitable for real runtime behavior |
| MVP-013 | `deferred` | consumer | `A3` | none | none | Post-MVP only: implement true provider-authoritative payment-session reconnect/resume semantics |
| MVP-014 | `done` | provider-integration | `A3` | `MVP-004` | `A` | Integrate the public Payment Gateway and private Plugin Gateway into the real provider streaming path |
| MVP-015 | `done` | provider-integration | `A3` | `MVP-004`, `MVP-014` | `A`, `C` | Wire real byte metering and session correlation from the plugin path into the payment-state repository used by the gateway |
| MVP-016 | `done` | provider-integration | `A6` | `MVP-010`, `MVP-014` | `C` | Enforce gateway Continue/Stop decisions in the live provider stream lifecycle |
| MVP-017 | `done` | consumer-integration | `A1`, `A2`, `A3` | `MVP-007`, `MVP-011`, `MVP-033` | `A`, `C` | Implement the consumer sidecar as the Substreams-compatible endpoint/proxy and primary SDS-facing runtime boundary |
| MVP-018 | `done` | tooling | none | `MVP-032` | `E` | Implement operator funding CLI flows for approve/deposit/top-up beyond local demo assumptions |
| MVP-019 | `done` | tooling | `A5` | `MVP-009`, `MVP-022` | `D`, `F` | Implement provider inspection CLI flows for accepted and collectible RAV data |
| MVP-020 | `done` | tooling | `A5` | `MVP-009`, `MVP-022`, `MVP-029` | `F` | Implement manual collection CLI flow that fetches provider settlement state and crafts/signs/submits collect transactions locally |
| MVP-021 | `done` | security | `A5` | none | `G` | Make TLS the default non-dev runtime posture for oracle, sidecar, and provider integration paths |
| MVP-022 | `done` | security | `A5` | `MVP-028` | `D`, `F`, `G` | Add authentication and authorization to provider admin/operator APIs using the shared bearer-token role contract from MVP-028 |
| MVP-023 | `done` | observability | `A4` | none | `A`, `C`, `D`, `F`, `G` | Define the final MVP observability floor beyond structured logs and status tooling |
| MVP-024 | `done` | observability | `A4` | `MVP-023` | `C`, `D`, `F`, `G` | Implement basic operator-facing inspection/status surfaces, metrics, and log correlation |
| MVP-025 | `done` | validation | none | none | `A`, `B`, `C`, `D`, `E`, `F`, `G` | Add MVP acceptance coverage for the primary end-to-end scenarios in docs/tests/manual verification |
| MVP-026 | `done` | docs | `A1`, `A4`, `A5` | `MVP-023`, `MVP-028`, `MVP-033` | `A`, `B`, `C`, `D`, `E`, `F`, `G` | Refresh protocol/runtime docs so they match the revised MVP architecture and completed acceptance scope |
| MVP-027 | `done` | protocol | `A3` | none | `B`, `D`, `F` | Freeze MVP payment/session identity semantics for fresh sessions and non-reused collection/payment lineage |
| MVP-028 | `done` | security | `A5` | none | `G` | Define the MVP authentication and authorization contract for provider operator APIs and future oracle admin surfaces |
| MVP-029 | `done` | provider-state | `A3`, `A5` | `MVP-003` | `D`, `F` | Implement provider collection lifecycle transitions and update surfaces for `collectible`, `collect_pending`, `collected`, and retryable collection state |
| MVP-030 | `done` | provider-integration | `A5` | `MVP-014`, `MVP-017` | `A`, `G` | Define and document the MVP runtime-compatibility contract for real provider/plugin deployments without side-effectful automatic probes |
| MVP-031 | `done` | runtime-payment | `A2`, `A3` | `MVP-004`, `MVP-012`, `MVP-014`, `MVP-017` | `A`, `C` | Wire the long-lived provider-originated payment-control loop behind the consumer-sidecar ingress path used by real runtime traffic |
| MVP-032 | `done` | operations | `A4`, `A5`, `A6` | `MVP-008`, `MVP-010`, `MVP-022` | `C`, `D`, `F`, `G` | Expose authenticated operator runtime/session/payment inspection APIs and CLI/status flows |
| MVP-033 | `done` | protocol | `A1` | none | `A` | Freeze the chain/network discovery input contract across client, sidecar, and oracle |
| MVP-034 | `done` | validation | none | none | none | Fix repository PostgreSQL tests so migrations resolve from repo-relative state rather than a machine-specific absolute path |
| MVP-035 | `done` | validation | none | none | none | Make integration devenv startup resilient to local fixed-port collisions so the shared test environment is reproducible |
| MVP-036 | `done` | operations | `A5` | `MVP-014` | `A`, `G` | Verify published `firehose-core` and `dummy-blockchain` image state and document the local-image fallback/update workflow |
| MVP-037 | `done` | validation | none | `MVP-014`, `MVP-016` | `A`, `C` | Isolate and harden the shared-state Firecore and low-funds integration tests so real-path acceptance remains deterministic across full-suite runs |
| MVP-038 | `done` | protocol | `A2`, `A3` | `MVP-017`, `MVP-031` | `A`, `C` | Remove the deprecated wrapper-era usage-report runtime path and protobuf surfaces once the sidecar-ingress flow is the only supported MVP runtime path |
| MVP-040 | `done` | runtime-payment | `A2`, `A3` | `MVP-017`, `MVP-031` | `A`, `C` | Make sidecar ingress termination ordering deterministic so provider payment-control stops win over upstream EOF without changing Substreams data-plane semantics |
| MVP-041 | `done` | runtime-payment | `A2`, `A3` | `MVP-031` | `A`, `C` | Define and enforce exact response semantics for provider-originated `RavRequest` handling in the long-lived `PaymentSession` loop |
| MVP-039 | `deferred` | provider-integration | `A3`, `A6` | `MVP-008`, `MVP-014`, `MVP-031` | none | Post-MVP only: decouple the private Plugin Gateway and public Provider Gateway via an explicit internal RPC/event boundary and clarified runtime-state ownership |

## Protocol and Contract Tasks

- [x] MVP-001 Freeze the oracle-authoritative MVP pricing contract across oracle, consumer, and provider flows.
  - Context:
    - The revised MVP scope fixes pricing at the oracle layer for the curated provider set.
    - The normal consumer/provider handshake is no longer a pricing negotiation step.
  - Assumptions:
    - `A2`
  - Done when:
    - The repo documents oracle-authoritative pricing for MVP.
    - Consumer and provider tasks no longer assume provider-side price negotiation in normal operation.
    - Oracle and handshake wording are consistent across scope and backlog.
  - Verify:
    - Review [docs/mvp-scope.md](../../docs/mvp-scope.md) and confirm there is no conflicting pricing authority language.

- [x] MVP-002 Freeze fresh-session init semantics and provider-returned data-plane endpoint behavior.
  - Context:
    - The revised MVP scope no longer includes resumable payment-session behavior.
    - The provider handshake, not the oracle, owns session-specific data-plane endpoint resolution.
  - Assumptions:
    - `A2`
    - `A3`
  - Done when:
    - The repo documents that every new request/connection creates a fresh SDS payment session.
    - The provider handshake is described as returning the data-plane endpoint.
    - No task still assumes latest-known resumable RAV behavior during normal init.
  - Verify:
    - Review [docs/mvp-scope.md](../../docs/mvp-scope.md) and confirm the workflow and decisions table match this contract.

- [x] MVP-033 Freeze the chain/network discovery input contract across client, sidecar, and oracle.
  - Context:
    - Oracle-backed provider discovery depends on a stable chain/network contract.
  - Assumptions:
    - `A1`
  - Done when:
    - The repo defines the canonical chain/network identifier shape used by the oracle query path.
    - Consumer sidecar owns derivation, normalization, validation, and conflict detection.
    - Oracle and consumer tasks point to the same frozen contract.
  - Verify:
    - Review [docs/mvp-scope.md](../../docs/mvp-scope.md) and confirm the network-discovery contract is present in the main workflow text.

- [x] MVP-027 Freeze MVP payment/session identity semantics for fresh sessions and non-reused collection/payment lineage.
  - Context:
    - The revised MVP scope intentionally avoids reconnect/payment identity reuse.
  - Assumptions:
    - `A3`
  - Done when:
    - The repo documents that reconnects create new SDS payment sessions rather than reusing prior payment lineage.
    - Collection/payment identity reuse is closed as an MVP scope decision.
  - Verify:
    - Review [docs/mvp-scope.md](../../docs/mvp-scope.md) and confirm the reconnect scenario and major decisions table match this rule.

- [x] MVP-003 Define and document the provider-side runtime persistence model and its boundary with settlement lifecycle tracking.
  - Context:
    - StreamingFast landed the shared repository model, PostgreSQL schema, and DSN-backed repository instantiation.
    - The remaining work is to make the runtime-versus-settlement boundary explicit in the MVP backlog and docs.
  - Assumptions:
    - `A3`
    - `A6`
  - Done when:
    - The backlog and docs clearly separate runtime/session persistence from collection lifecycle persistence.
    - The provider-side durable model is described in terms of sessions, workers, usage, current accepted RAV state, and separate collection lifecycle tracking.
    - Downstream tasks no longer assume reconnect-driven reuse semantics.
  - Verify:
    - Review [docs/provider-persistence-boundary.md](../../docs/provider-persistence-boundary.md), [provider/repository/repository.go](../../provider/repository/repository.go), and [provider/gateway/REPOSITORY.md](../../provider/gateway/REPOSITORY.md) against backlog task wording.

- [x] MVP-004 Define and document the real runtime payment contract used by the public payment gateway, private plugin gateway, and consumer/provider payment loop.
  - Context:
    - The runtime shape changed materially in the recent commit range.
    - The current repo now has:
      - a public Payment Gateway
      - a private Plugin Gateway
      - typed plugin session IDs
      - shared repository-backed runtime state
  - Assumptions:
    - `A2`
    - `A3`
  - Done when:
    - The runtime contract is documented in terms of the actual provider shape now in repo.
    - Provider handshake returns the session-specific data-plane endpoint used by the runtime path.
    - Consumer init takes a single provider control-plane override input rather than client-supplied split stream/control endpoints.
    - Pricing exposed in provider handshake remains confirmatory rather than negotiable for MVP.
    - Plugin session/usage correlation is described using typed protobuf fields rather than old implicit header flow.
    - Consumer/provider payment-loop expectations are documented without revive/resume assumptions.
  - Verify:
    - Review the backlog wording against [cmd/sds/impl/provider_gateway.go](../../cmd/sds/impl/provider_gateway.go), [provider/plugin/gateway.go](../../provider/plugin/gateway.go), and the plugin protobufs.

## Oracle Tasks

- [x] MVP-005 Implement a standalone oracle service with manual whitelist, canonical pricing, recommended-provider response, and control-plane endpoint return.
  - Context:
    - The oracle is a mandatory MVP component.
    - The revised scope fixes both the selection default and the pricing authority model.
  - Assumptions:
    - `A1`
    - `A2`
    - `A5`
  - Done when:
    - A standalone oracle component exists.
    - It serves a manually curated provider set.
    - It returns eligible providers, a recommended provider, canonical pricing, and the selected provider control-plane endpoint for a requested chain/network.
    - It does not require the oracle to resolve the final stream endpoint up front.
  - Verify:
    - Add tests for whitelist lookup, response validation, and deterministic recommendation behavior.
    - Review [proto/graph/substreams/data_service/oracle/v1/oracle.proto](../../proto/graph/substreams/data_service/oracle/v1/oracle.proto), [oracle/config.go](../../oracle/config.go), [oracle/oracle.go](../../oracle/oracle.go), and [cmd/sds/impl/oracle.go](../../cmd/sds/impl/oracle.go).

- [x] MVP-006 Add admin-only oracle whitelist/provider metadata management workflow for the curated MVP provider set.
  - Context:
    - Oracle governance must not rely on a public writable surface in MVP.
    - The curated whitelist is temporary MVP machinery and may remain deployment-managed internal config.
    - Decision on 2026-05-06: deployment-managed YAML config is sufficient for MVP oracle governance.
  - Assumptions:
    - `A5`
  - Done when:
    - Oracle whitelist/provider metadata changes are restricted to admins/council.
    - MVP does not require a public oracle management API.
    - If a public oracle admin API is added, it reuses the bearer-token role contract defined by MVP-028.
  - Verify:
    - Document the supported admin workflow and confirm the oracle does not rely on an open writable management surface.

## Consumer Tasks

- [x] MVP-007 Integrate consumer sidecar with oracle discovery while preserving direct-provider fallback and provider-returned data-plane resolution.
  - Context:
    - Consumer sidecar is the mandatory client-side integration point and must support oracle-driven default behavior.
  - Assumptions:
    - `A1`
    - `A2`
    - `A3`
  - Done when:
    - Consumer sidecar can query the oracle and choose the recommended provider.
    - Direct provider configuration still works as a fallback/override.
    - The consumer/provider flow uses the provider control-plane endpoint from the oracle and receives the data-plane endpoint during provider handshake.
  - Verify:
    - Add tests or documented smoke steps for both oracle-backed and direct-provider flows.

- [ ] MVP-013 Post-MVP only: implement true provider-authoritative payment-session reconnect/resume semantics.
  - Context:
    - The revised MVP scope explicitly defers payment-session continuation across reconnects.
    - This item remains only as a post-MVP placeholder so the historical requirement is not lost.
  - Assumptions:
    - `A3`
  - Done when:
    - This item is not part of MVP delivery.
  - Verify:
    - Confirm it is not referenced by current MVP acceptance scenarios.

## Provider State and Settlement Tasks

- [x] MVP-008 Complete durable provider runtime storage for sessions, usage, and accepted RAV state, distinct from collection lifecycle tracking.
  - Context:
    - StreamingFast landed:
      - PostgreSQL repository foundation
      - DSN-based repository selection
      - gateway integration with that repository
      - repository test coverage
    - The restart-focused accepted RAV proof now closes the gap between existing runtime persistence and the MVP durability scenarios for this task.
    - Recent payment-session hardening advanced this task by making runtime-state writes narrower, keepalive/runtime lifecycle updates monotonic, and accepted RAV plus baseline commits explicit and atomic.
  - Assumptions:
    - `A3`
    - `A6`
  - Done when:
    - Provider restart does not lose session/runtime state required by the gateway and plugin path.
    - Accepted RAV state needed for post-restart inspection and settlement survives restart in the durable backend.
    - The task no longer includes collection lifecycle state, which remains tracked under MVP-029.
  - Verify:
    - Added `TestSessionUpdateRAVAndBaseline_SurvivesRepositoryRestart`, a PostgreSQL-backed persistence test that validates accepted RAV, settlement tuple, usage totals, and baseline state survive reopening the repository against the same durable schema.
    - Existing supporting coverage now includes field-specific repository updates, monotonic keepalive/runtime lifecycle behavior, and accepted RAV plus baseline commit semantics across in-memory and PostgreSQL paths.

- [x] MVP-009 Expose authenticated provider inspection and settlement-data retrieval APIs for accepted and collectible RAV state.
  - Context:
    - CLI inspection and manual collection require a provider-side way to retrieve settlement-relevant data.
    - Current `GetSessionStatus` is useful runtime-coordination scaffolding and now exposes provider-side payment-control pending state, but it is intentionally not the richer operator inspection surface.
    - Richer provider status/inspection APIs should be authenticated and can expose more settlement/payment detail than the minimal runtime coordination status.
  - Assumptions:
    - `A3`
    - `A5`
  - Done when:
    - Provider exposes authenticated APIs for listing and fetching accepted and collectible payment state.
    - The returned data is sufficient for operator inspection and CLI-based collection.
    - The API shape is stable enough for MVP-019 and MVP-020 without direct backend reads.
  - Verify:
    - Add integration coverage for listing and fetching settlement-relevant accepted state.
  - Implemented:
    - Added `ProviderOperatorService` as a separate private Connect service under `provider/v1/operator.proto`.
    - Added authenticated read RPCs for sessions, accepted RAVs, and collection lifecycle records.
    - Added authenticated admin RPCs for collection lifecycle transitions to pending, collected, and retryable failure.
    - Registered the service on the private provider operator listener, separate from the public payment gateway service.
    - Added focused Connect handler tests for missing auth, read-token success, read-token mutation denial, admin mutation success, accepted RAV retrieval, collection retrieval, filtering, stale expected-value conflicts, and invalid collection keys.

- [x] MVP-029 Implement provider collection lifecycle transitions and update surfaces for `collectible`, `collect_pending`, `collected`, and retryable collection state.
  - Context:
    - The revised scope keeps collection lifecycle tracking as explicit provider-side work.
    - The repository now models collection lifecycle separately from runtime session state.
  - Assumptions:
    - `A3`
    - `A5`
  - Done when:
    - Provider persistence supports the required collection lifecycle states and transition rules.
    - There is a defined provider-side update path for marking collection attempts pending, completed, or retryable.
    - Retry behavior is documented so CLI flows can be idempotent.
  - Verify:
    - Added in-memory and PostgreSQL persistence tests covering `collectible` -> `collect_pending` -> `collected`, `collect_pending` -> `collect_failed_retryable` -> `collect_pending`, stale expected-value rejection, and backwards-transition rejection.
    - PostgreSQL migration coverage now includes the `collection_records` table.

## Funding Control and Runtime Payment Tasks

- [x] MVP-010 Implement session-local low-funds detection and provider terminal stop behavior during streaming.
  - Context:
    - The MVP requires low-funds handling during active streaming, but only on a session-local basis.
    - The implemented MVP policy is stop-only on insufficient funds, with fail-open behavior when live escrow balance cannot be queried.
  - Assumptions:
    - `A6`
  - Done when:
    - Provider compares projected session-local outstanding exposure against live escrow during provider-side runtime payment/control handling.
    - If funds are insufficient, provider persists machine-readable funds metadata, terminates the session with `END_REASON_PAYMENT_ISSUE`, and emits `NeedMoreFunds` as the terminal response for that session roundtrip.
    - If live escrow balance cannot be determined, provider records `unknown` funding status and continues normal runtime behavior rather than stopping solely on the failed check.
    - The MVP does not reinterpret temporary escrow-RPC failures as pause semantics; any future bounded-retry or infrastructure-failure stop policy should remain distinct from `NeedMoreFunds`.
  - Verify:
    - Integration coverage exists for insufficient-funds stop, exact-balance continue, unknown-balance fail-open, and consumer-side stop behavior on `NeedMoreFunds`.

- [x] MVP-011 Propagate provider low-funds stop decisions through consumer sidecar into the real ingress/client path.
  - Context:
    - Low-funds logic is incomplete until the client path actually obeys it.
    - The implemented ingress slice makes the consumer sidecar the runtime owner of provider discovery/session init, upstream stream setup, and low-funds termination propagation for real client-facing Substreams traffic.
    - This task intentionally stopped short of the full provider-originated runtime-payment loop now tracked as the remaining scope of `MVP-031`.
  - Assumptions:
    - `A6`
  - Done when:
    - Consumer sidecar propagates provider-originated low-funds stop decisions through the real client-facing ingress path rather than relying on a wrapper-specific stop flow.
    - Real client integration honors those stop decisions and surfaces a clear client-visible reason.
  - Verify:
    - `go test ./test/integration -run TestConsumerIngress_StopsStreamOnLowFunds -count=1 -v` passes with downstream `ResourceExhausted` surfaced through the sidecar ingress.
    - `go test ./test/integration -run TestConsumerIngress_UsesOracleSelectedProviderReceiver -count=1 -v` passes to confirm oracle-backed ingress derives the receiver/service provider from oracle-selected provider identity.
    - Historical note: wrapper-era low-funds coverage existed during the transition and was later removed under `MVP-038`.

- [x] MVP-012 Add deterministic cost-based RAV issuance thresholds suitable for real runtime behavior.
  - Context:
    - The current runtime/payment loop foundation exists, but the real-runtime issuance policy still needs to be made explicit.
  - Assumptions:
    - none
  - Done when:
    - Provider requests a new RAV only when unbaselined `delta_cost` since the last accepted RAV reaches a deterministic provider-side threshold.
    - The threshold is configured through provider pricing YAML as `rav_request_threshold`, with a documented fallback of `10 GRT` when omitted.
    - The threshold policy remains provider-internal and is not exposed through shared pricing protobufs or handshake payloads.
    - Threshold behavior is covered for below-threshold continue, threshold-triggered request, and post-acceptance baseline reset.
  - Verify:
    - Integration coverage shows repeated metered usage no longer forces a RAV request on every control roundtrip and only triggers a request once `delta_cost >= rav_request_threshold`.

## Real Provider and Consumer Integration Tasks

- [x] MVP-014 Integrate the public Payment Gateway and private Plugin Gateway into the real provider streaming path.
  - Context:
    - The recent commit range established the provider-side dual-gateway shape and the shared repository wiring.
    - The repo now also has a stronger `TestFirecore` real-path harness that boots payment gateway, plugin gateway, consumer sidecar, Postgres, and dummy-blockchain/firecore together.
    - The backlog should now treat that as the concrete provider integration target.
    - Current status:
      - The repo-local integration work is complete enough for acceptance: provider handshake returns the correct mapped data-plane endpoint, both gateways start in the expected topology, and the real-path `TestFirecore` run succeeds through auth, session, and usage correlation when pointed at a locally rebuilt runtime image.
      - The local-first acceptance run was validated on 2026-03-28 against:
        - SDS `f9bcdbfdccaa9bc1de9fd655c613a59699596c47`
        - `firehose-core` `b574a98babcb0338198e0ff4db7ebd0e404f6529`
        - `dummy-blockchain` `1cea671e78cbb069d64333fdbf4a6c9dd5502d58`
        - `substreams` `8897dccff3e2f989867b7711be91d613d256a36a`
        - image tags `ghcr.io/streamingfast/firehose-core:sds-local` and `ghcr.io/streamingfast/dummy-blockchain:sds-local`
      - `MVP-036` later verified that `ghcr.io/streamingfast/firehose-core:latest` is compatible with the current SDS runtime path when embedded in a rebuilt dummy-chain image, while the prebuilt `ghcr.io/streamingfast/dummy-blockchain:v1.7.7` image remains stale.
  - Assumptions:
    - `A3`
  - Done when:
    - The real provider path validates payment/session state through the public Payment Gateway.
    - Firehose-core plugin traffic goes through the private Plugin Gateway.
    - Both paths share the same authoritative provider-side repository state.
    - The real-path acceptance run uses a firecore/dummy-blockchain runtime built against the current SDS protocol contract rather than the stale prebuilt image, with the SDS test harness pointed at that image via `SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE`.
  - Verify:
    - `SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE=ghcr.io/streamingfast/dummy-blockchain:sds-upstream-firecore-latest go test ./test/integration -run '^TestFirecore$' -v -count=1` passes without skip against a dummy-chain image rebuilt on top of the compatible published `firehose-core:latest` image.
    - The backlog and runtime-compatibility docs explicitly identify the prebuilt `dummy-blockchain:v1.7.7` image as incompatible with the current SDS provider/plugin contract.

- [x] MVP-015 Wire real byte metering and session correlation from the plugin path into the payment-state repository used by the gateway.
  - Context:
    - The recent commit range fixed session ID propagation and pushed more correlation through typed plugin fields and shared repository state.
    - The repo-local acceptance path is now validated: provider-side plugin metering advances the same session aggregates and accumulated cost surfaced by `GetSessionStatus`, and the real Firecore path proves that exact pricing alignment against persisted metering evidence.
    - The local-first acceptance run was validated on 2026-03-28 against:
      - SDS `1171ed0bbf7a7254f6655d98c1e7947f5a3bd776` plus `ad3420a6ac9c11f48f6a9d7f478cf487233357d7`
      - `firehose-core` `b574a98babcb0338198e0ff4db7ebd0e404f6529`
      - `dummy-blockchain` `1cea671e78cbb069d64333fdbf4a6c9dd5502d58`
      - `substreams` `8897dccff3e2f989867b7711be91d613d256a36a`
      - image tags `ghcr.io/streamingfast/firehose-core:sds-local` and `ghcr.io/streamingfast/dummy-blockchain:sds-local`
  - Assumptions:
    - `A3`
  - Done when:
    - Real provider-side byte metering feeds the repository state used for payment progression.
    - Session correlation is stable across auth, session, usage, and gateway-side payment state.
    - The runtime path does not rely on consumer-reported bytes as the billing source of truth.
  - Verify:
    - `go test ./provider/usage ./provider/repository/psql -count=1` passes with repository/service coverage for authoritative metering application.
    - Historical note: wrapper-era payment-loop coverage existed during the transition and was later removed under `MVP-038`.
    - `SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE=ghcr.io/streamingfast/dummy-blockchain:sds-local go test ./test/integration -run TestFirecore -count=1 -v` passes with `GetSessionStatus().payment_status.accumulated_usage_value` exactly matching the provider-priced total derived from persisted plugin metering evidence.

- [x] MVP-016 Enforce gateway Continue/Stop decisions in the live provider stream lifecycle.
  - Context:
    - Provider-side control logic is incomplete if the live provider stream does not obey it.
    - The repo-local acceptance path is now validated: plugin keepalive enforcement stops the live Firecore/Substreams stream when the provider session is no longer allowed to continue, while preserving the exact real-path `MVP-014` happy-path flow.
    - The local-first acceptance run was validated on 2026-03-28 against:
      - SDS `1171ed0bbf7a7254f6655d98c1e7947f5a3bd776` plus the current uncommitted `MVP-016` worktree changes
      - `firehose-core` `b574a98babcb0338198e0ff4db7ebd0e404f6529`
      - `dummy-blockchain` `1cea671e78cbb069d64333fdbf4a6c9dd5502d58`
      - `substreams` `8897dccff3e2f989867b7711be91d613d256a36a`
      - image tags `ghcr.io/streamingfast/firehose-core:sds-local` and `ghcr.io/streamingfast/dummy-blockchain:sds-local`
  - Assumptions:
    - `A6`
  - Done when:
    - The real provider path can enforce SDS control decisions during live streaming.
    - Gateway-driven low-funds stop behavior interrupts the live provider stream lifecycle appropriately rather than only ending the control-plane session.
  - Verify:
    - `go test ./provider/session ./provider/plugin ./provider/repository -count=1` passes with fail-closed provider session-service coverage and plugin error-mapping coverage.
    - Historical note: wrapper-era low-funds stop coverage existed during the transition and was later removed under `MVP-038`.
    - `SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE=ghcr.io/streamingfast/dummy-blockchain:sds-local go test ./test/integration -run 'TestFirecore|TestFirecoreStopsStreamOnLowFunds' -count=1 -v` passes with:
      - the normal `TestFirecore` happy path still succeeding
      - the dedicated low-funds Firecore path stopping the live stream early
      - provider session state ending with `END_REASON_PAYMENT_ISSUE`
      - worker cleanup eventually completing after the stop
    - `MVP-036` documents the current runtime-image state: published `firehose-core:latest` is compatible, while the prebuilt `ghcr.io/streamingfast/dummy-blockchain:v1.7.7` image remains stale and still blocks the default image path on the known header-propagation/runtime-drift issue.

- [x] MVP-017 Implement the consumer sidecar as the Substreams-compatible endpoint/proxy and primary SDS-facing runtime boundary.
  - Context:
    - The revised MVP scope elevates the consumer sidecar from helper service to user-facing SDS boundary.
    - A minimal ingress slice is now implemented: the sidecar exposes Substreams gRPC services, performs oracle/direct provider selection, starts provider sessions internally, proxies upstream streams, and surfaces low-funds termination through the client-facing ingress.
    - At the time of this task, `sds sink run` still existed as transitional scaffolding, but it no longer represented the only real-path integration shape.
  - Assumptions:
    - `A1`
    - `A2`
    - `A3`
  - Done when:
    - Existing Substreams tooling can point at the consumer sidecar endpoint for the normal data-plane path.
    - The consumer sidecar hides oracle lookup, provider session init, and runtime payment/control coordination behind that ingress.
    - The user-facing runtime path does not require external wrapper-specific `Init` / `ReportUsage` / `EndSession` orchestration.
  - Verify:
    - Current status:
      - The sidecar now exposes the real Substreams ingress path, owns discovery/session bootstrap behind that ingress, and coordinates runtime payment/control through the long-lived provider `PaymentSession` flow rather than an internalized usage-report loop.
      - That transition temporarily left legacy wrapper-era `Init` / `ReportUsage` / `EndSession` RPCs in-tree; the deprecated `ReportUsage` path and wrapper CLI cleanup later landed under `MVP-038`.
    - `go test ./test/integration -run 'TestConsumerIngress_UsesOracleSelectedProviderReceiver|TestConsumerIngress_StopsStreamOnLowFunds' -count=1 -v` passes.
    - `SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE=ghcr.io/streamingfast/dummy-blockchain:sds-local go test ./test/integration -run 'TestFirecore|TestFirecoreStopsStreamOnLowFunds' -count=1 -v` passes against the rebuilt local runtime image path.
    - `MVP-036` documents that published `firehose-core:latest` is compatible, while the published `ghcr.io/streamingfast/dummy-blockchain:v1.7.7` image remains stale and still blocks the default image path.

- [x] MVP-031 Wire the long-lived provider-originated payment-control loop behind the consumer-sidecar ingress path used by real runtime traffic.
  - Context:
    - MVP runtime traffic now flows through the sidecar ingress, and payment progression is driven from provider-side metering through the long-lived provider-originated `PaymentSession` control loop.
    - The initial closure intentionally left legacy wrapper-era usage-report surfaces as explicit rejected paths pending later cleanup under `MVP-038`.
    - Two correctness follow-ups were discovered after the initial integration closure and are tracked separately under `MVP-040` and `MVP-041` rather than reopening the main runtime-loop task wholesale.
  - Assumptions:
    - `A2`
    - `A3`
  - Done when:
    - The real client/provider integration keeps the SDS payment-control loop active alongside the live stream behind the consumer-sidecar ingress path.
    - Provider-driven RAV requests, acknowledgements, and control messages flow through the production runtime path rather than only through wrapper commands.
    - The production runtime path does not require a separate external client/wrapper `ReportUsage` step.
  - Verify:
    - `go test ./test/integration -run 'TestPaymentSession_ProviderRequestsRAVOnUsage|TestPaymentSession_AcceptedRAVResetsThresholdWindow|TestPaymentSession_StopsOnLowFunds' -count=1 -v` passes.
    - `go test ./test/integration -run 'TestConsumerIngress_UsesOracleSelectedProviderReceiver|TestConsumerIngress_StopsStreamOnLowFunds' -count=1 -v` passes.
    - `SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE=ghcr.io/streamingfast/dummy-blockchain:sds-local go test ./test/integration -run 'TestFirecore|TestFirecoreStopsStreamOnLowFunds' -count=1 -v` passes against the rebuilt local runtime image path, demonstrating provider-originated runtime control during live streaming without an external usage-report loop.

- [x] MVP-040 Make sidecar ingress termination ordering deterministic so provider payment-control stops win over upstream EOF without changing Substreams data-plane semantics.
  - Context:
    - The consumer ingress must preserve Substreams-compatible data-plane behavior, so the fix cannot rely on injecting SDS-specific terminal metadata into the proxied Substreams stream.
    - The current runtime path still has a lifecycle race between the upstream data-plane stream ending and the provider `PaymentSession` loop delivering a terminal payment-control decision.
    - That race can make a real provider-enforced payment stop surface to the client as generic upstream EOF/transport closure instead of the intended runtime `ResourceExhausted` style outcome.
    - On April 6, 2026, the task was reopened after the same residual first-pass full-suite failure reproduced on both `486700a` (`Remove deprecated usage-report runtime path`) and the pre-`MVP-038` commit `403bf9f` (`Harden stateful runtime test isolation`), which shows this is not newly introduced by `MVP-038`.
  - Assumptions:
    - `A2`
    - `A3`
  - Done when:
    - The sidecar ingress resolves data-plane completion versus provider payment-control termination through explicit control-plane/lifecycle coordination against provider-persisted session end state rather than a fixed timing heuristic.
    - Expected finite EOF also checks explicit provider-reported payment-control pending state before returning cleanly, so a final provider RAV/control request is not lost just because the local control goroutine has not observed it yet.
    - Provider low-funds or terminal payment-control decisions deterministically win over competing upstream EOF timing when both refer to the same live session.
    - The solution does not require changing the Substreams data-plane response shape or adding SDS-specific payloads to proxied stream messages.
  - Verify:
    - `buf generate`
    - `go test ./consumer/sidecar/...`
    - `go test ./provider/gateway/...`
    - `go test ./test/integration -run 'TestConsumerIngress_StopsStreamOnLowFunds|TestConsumerIngress_ResolvesAmbiguousEOFWithDelayedProviderStop|TestConsumerIngress_ResolvesAmbiguousEOFFromProviderSessionStatus|TestConsumerIngress_FiniteEOFReturnsPromptlyWithoutControlStop|TestFirecoreStopsStreamOnLowFunds' -count=1 -v`
    - `go test ./test/integration/... -count=1`
    - `go test ./...`
    - `go vet ./...`
    - Implemented in `consumer/sidecar/ingress.go` by keeping the fast path for expected finite EOF, then resolving ambiguous upstream EOF/internal-cancel termination against provider-persisted session end state via `GetSessionStatus.end_reason` within `payment-session-roundtrip-timeout`.
    - Expected finite EOF now waits only when local payment-control state or provider `GetSessionStatus.payment_control_pending` reports explicit pending work; it no longer relies on an unconditional finite-EOF sleep.
    - Added `GetSessionStatusResponse.payment_control_pending` as an additive provider control-plane field so the sidecar can distinguish clean finite completion from outstanding provider-side payment control.
    - Added focused unit coverage in `consumer/sidecar/ingress_test.go` for end-reason mapping, timeout/error handling, coordinator preemption, prompt finite EOF without pending control, local pending control, and provider-reported pending control.
    - Added focused integration coverage in `test/integration/consumer_ingress_test.go` for delayed provider stop after upstream end, prompt finite EOF without teardown-delay regression, and the status-driven payment-issue resolution case where the provider `PaymentSession` stream does not deliver the terminal semantic before ingress sees EOF.

- [x] MVP-041 Define and enforce exact response semantics for provider-originated `RavRequest` handling in the long-lived `PaymentSession` loop.
  - Context:
    - Provider-side metering and the client response to a provider-originated `RavRequest` are asynchronous, so the acceptance rule cannot rely on a moving live usage delta after the provider has already emitted a concrete request.
    - The current runtime loop needs an explicit contract for what a `PaymentSession` RAV submission is answering, especially if usage continues to accrue before the response arrives.
    - This task is about the `PaymentSession` runtime contract itself; any later repository hardening or broader concurrency/versioning changes remain separate follow-up work unless this task proves they are strictly required.
  - Assumptions:
    - `A2`
    - `A3`
  - Done when:
    - The repo documents and implements the authoritative rule for what provider-issued `RavRequest` a client response is satisfying.
    - The runtime path no longer rejects a valid response to the provider’s own in-flight request merely because live metering advanced afterward.
    - The supported response path for provider-managed runtime requests is explicit:
      - either `PaymentSession` is the only valid path
      - or any remaining alternative path is defined so it cannot race or silently diverge from the `PaymentSession` contract
    - Tests no longer depend on implicit assumptions about exact-vs-greater-than-request RAV submissions.
  - Verify:
    - Add coverage for a provider-issued `RavRequest` that is answered after additional metering arrives.
    - Add coverage for the chosen exact-vs-greater-than-request policy.
    - Re-run `go test ./test/integration -run 'TestPaymentSession_ProviderRequestsRAVOnUsage|TestPaymentSession_AcceptedRAVResetsThresholdWindow|TestFirecore' -count=1 -v` and confirm the accepted-RAV path remains stable without relying on moving-delta validation.
  - Implemented:
    - Provider runtime state now stores the authoritative in-flight `RavRequest` snapshot and validates `PaymentSession` `rav_submission` messages against that exact request rather than a moving live usage delta.
    - Accepted runtime responses commit the signed RAV and covered usage baseline after validation with a bounded provider-owned context, so peer disconnect after acceptance cannot cancel the accounting write.
    - Accepted runtime responses advance the session baseline only to the requested snapshot, then best-effort re-evaluate later accrued usage for the next provider-originated request without turning a committed RAV into a `STOP` solely because the refresh failed.
    - Provider-managed runtime requests are now explicit `PaymentSession`-only behavior; unary `SubmitRAV` is documented and enforced as a deprecated legacy/manual surface for non-runtime flows when a live runtime request is outstanding.
    - Added integration coverage for delayed exact-snapshot acceptance, exact-only rejection of overpaying stream responses, and unary `SubmitRAV` rejection while a live provider-issued request is in flight.
    - Added focused gateway coverage for post-disconnect RAV persistence, preserving pending state on commit failure, and treating post-commit runtime refresh failure as best-effort after the accepted RAV is durably stored.

## Operator Tooling Tasks

- [x] MVP-018 Implement operator funding CLI flows for approve/deposit/top-up beyond local demo assumptions.
  - Context:
    - Funding is an MVP operator workflow, but current tooling is still demo-oriented.
    - `sds tools rav` is useful support tooling, not a substitute for escrow funding flows.
  - Assumptions:
    - none
  - Done when:
    - CLI commands exist for approve/deposit/top-up in a provider-operator or payer-operator workflow.
    - CLI commands exist for payer signer proof generation, authorization status, authorize, thaw, revoke, and cancel-thaw.
    - The commands are not limited to local deterministic devenv assumptions.
    - The documented operator flow links funding actions to low-funds/runtime inspection surfaces.
  - Verify:
    - Add command-level tests where practical and document a manual funding flow that works against a non-demo configuration.
  - Implemented:
    - Added `sds consumer funding status|approve|deposit|top-up` with explicit RPC, chain, contract, payer, receiver, and key inputs.
    - Added `sds consumer signer proof|status|authorize|thaw|revoke|cancel-thaw` for sidecar signer lifecycle setup against `GraphTallyCollector`.
    - Added EIP-1559 dynamic-fee transaction submission with explicit gas, fee, timeout, dry-run, and no-wait flags.
    - Added shared ERC20, `PaymentsEscrow`, and `GraphTallyCollector` ABI wrappers plus shared signer proof generation outside `horizon/devenv`.
    - Documented the operator workflow in [docs/operator-funding.md](../../docs/operator-funding.md).

- [x] MVP-019 Implement provider inspection CLI flows for accepted and collectible RAV data.
  - Context:
    - Operators need to inspect what can be collected before settlement.
    - `sds tools rav` inspection is local protobuf inspection, not provider-backed operator inspection.
  - Assumptions:
    - `A5`
  - Done when:
    - CLI can retrieve and display accepted and collectible payment state from the provider.
    - It supports the lifecycle states needed for MVP operations.
  - Verify:
    - Add manual smoke coverage for inspecting accepted and `collect_pending` state.
  - Implemented:
    - Added `sds provider operator sessions list|get`, `ravs list|get`, and `collections list|get` read-only commands backed by the authenticated `ProviderOperatorService`.
    - CLI requires explicit `--provider-endpoint` and exactly one of `--operator-token-env` or `--operator-token`.
    - Secure HTTPS is the default for schemeless endpoints; plaintext HTTP requires explicit `--plaintext` for local/dev use.
    - Commands support text and JSON output, read filters for payer/receiver/data-service/session/collection/status/state, conservative limits, and explicit RAV payload inclusion.
    - Added focused tests for endpoint security parsing, token resolution, and text formatting.

- [x] MVP-020 Implement manual collection CLI flow that fetches provider settlement state and crafts/signs/submits collect transactions locally.
  - Context:
    - Settlement keys should stay outside the provider runtime.
    - Existing RAV tooling is helpful support, but it does not yet implement the provider-backed settlement flow required by MVP.
  - Assumptions:
    - `A5`
  - Done when:
    - CLI fetches settlement-relevant data from the provider.
    - CLI crafts and signs the collect transaction locally.
    - CLI can retry safely when collection is pending or needs to be re-attempted.
  - Verify:
    - Add a manual or automated integration scenario that retrieves collectible state and completes a collect transaction.
  - Evidence:
    - Added production `contracts/horizon` helpers to encode `SubstreamsDataService.collect(indexer, QueryFee, abi.encode(signedRAV, dataServiceCut))` calldata from `horizon.SignedRAV`.
    - Moved the integration setup helper for data-service collect payload encoding onto the production helper.
    - Added `sds provider operator collect`, backed by the authenticated provider operator API and `contracts/chain` transaction sender.
    - The command requires explicit provider endpoint/token, RPC/chain configuration, data service address, provider private key, collection identity, and `--data-service-cut-ppm`; no local production defaults are inferred.
    - The command validates collectible/retryable state, exact signed RAV data, provider key address, and data service address before submission.
    - The command marks pending before submission except in `--dry-run`, marks collected after a successful waited receipt, marks retryable after submission/receipt errors when pending was set, leaves `--no-wait` records pending with follow-up output, and treats already-collected records as a no-op.
    - Focused tests cover calldata packing, collect record validation, and admin pending mutation without a pre-submission tx hash.

## Security, Runtime Compatibility, and Observability Tasks

- [x] MVP-028 Define the MVP authentication and authorization contract for provider operator APIs and future oracle admin surfaces.
  - Context:
    - This task froze the shared bearer-token role contract used by provider operator/admin surfaces and any future public oracle admin API.
  - Assumptions:
    - `A5`
  - Done when:
    - The repo documents the MVP authn/authz approach for provider operator/admin surfaces.
    - It is clear which provider endpoints/actions require operator privileges and which credentials satisfy that requirement.
    - The oracle whitelist/provider metadata workflow is explicitly treated as admin/council-only internal governance for MVP rather than requiring a public management API.
    - MVP-022 and any future public oracle admin API can reuse the same contract rather than inventing separate security behavior.
  - Verify:
    - Confirm provider admin tasks and any future public oracle admin API point to the same bearer-token role contract.

- [x] MVP-021 Make TLS the default non-dev runtime posture for oracle, sidecar, and provider integration paths.
  - Context:
    - The MVP requires real transport security without forcing a perfect production-hardening story.
    - Shared server transport validation and explicit plaintext/TLS config now exist for oracle, consumer sidecar, provider payment gateway, and provider plugin gateway.
    - The reflex devenv is the explicit local/dev plaintext environment.
    - Provider plugin-gateway plaintext now requires explicit `--plugin-plaintext` when the payment gateway uses `--plaintext`; it no longer silently inherits plaintext from the payment gateway.
  - Assumptions:
    - `A5`
  - Done when:
    - Non-dev/runtime docs and defaults use TLS for oracle, consumer sidecar, and provider integration surfaces.
    - Plaintext behavior is clearly scoped to local/dev/demo usage.
    - Operator/admin traffic does not rely on plaintext-by-default behavior outside explicitly dev-scoped workflows.
  - Verify:
    - Server transport validation requires TLS cert/key material unless `--plaintext` is explicitly set.
    - Provider plugin-gateway transport resolution rejects implicit plaintext inheritance and requires `--plugin-plaintext` or explicit plugin TLS material.
    - Reflex devenv commands pass `--plaintext` and `--plugin-plaintext` explicitly.
    - Docs describe the public/non-dev default as TLS and the reflex devenv as the plaintext exception.

- [x] MVP-022 Add authentication and authorization to provider admin/operator APIs.
  - Context:
    - Provider-side operator actions must not rely on open or anonymous admin APIs.
    - The existing `GetSessionStatus` surface is a minimal runtime coordination helper used by the consumer sidecar; if it remains minimal, it can stay separate from richer authenticated operator inspection APIs.
    - If `GetSessionStatus` grows into a richer inspection surface, it should either be authenticated or split so the sidecar keeps only the minimal runtime status it needs.
  - Assumptions:
    - `A5`
  - Done when:
    - Provider gateway startup accepts explicit operator listener and read/admin token environment variable configuration.
    - Enabled provider operator surfaces fail fast without usable configured tokens.
    - Gateway-level operator authorization uses the shared bearer-token role contract from MVP-028, where read-only operator actions require `operator.read` and mutating provider admin actions require `admin.write`.
    - Concrete provider inspection, settlement-retrieval, and lifecycle API handlers added by `MVP-009`/`MVP-032` can enforce the wired authorization helper before serving operator-only actions.
  - Verify:
    - Add tests for token configuration validation, authenticated success, unauthenticated rejection, and role denial.
  - Implemented:
    - Added disabled-by-default provider operator gateway flags: `--operator-listen-addr`, `--operator-read-token-env`, and `--admin-write-token-env`.
    - Startup now resolves read/admin bearer tokens from explicit environment variable names and fails fast when an enabled operator service has missing, empty, or unusable token values.
    - `gateway.Config` and `Gateway` now carry `operatorauth.Config`, with a gateway helper that enforces `operator.read` and `admin.write` role requirements through the shared `internal/operatorauth` contract.
    - The provider command starts a separate operator gateway listener when enabled; concrete operator API handlers are added by `MVP-009`/`MVP-032`.
    - Focused tests cover command token resolution plus gateway/operator-listener authorization behavior for missing, malformed, read-only, and admin tokens.

## Runtime Compatibility Tasks

- [x] MVP-030 Define and document the MVP runtime-compatibility contract for real provider/plugin deployments without side-effectful automatic probes.
  - Context:
    - Recent README, config, and firecore test scaffolding identify the target runtime more clearly.
    - MVP-014 uncovered a concrete compatibility failure in the prebuilt `dummy-blockchain:v1.7.7` image: its embedded `firecore` binary links an older SDS snapshot and therefore speaks older auth/session/usage plugin contracts than the current provider/plugin gateway.
    - A strong runtime compatibility probe is not currently available without exercising auth/session/usage behavior that can create runtime side effects against the underlying provider stack.
    - Recent additive provider payment control-plane fields such as `GetSessionStatusResponse.end_reason` and `payment_control_pending` are documented as backward-compatible for external `firecore` / Substreams runtime tuples because they do not change the auth/session/usage plugin surfaces.
  - Assumptions:
    - `A5`
  - Done when:
    - The repo identifies at least one named real-provider target environment for MVP acceptance and documents the required runtime compatibility constraints clearly enough for operators to validate before rollout.
    - The required runtime versions, plugin compatibility assumptions, and non-demo configuration prerequisites for that environment are documented.
    - The documented compatibility contract explicitly covers SDS protocol drift between provider/plugin gateway code and embedded firecore plugin binaries.
    - Contributor workflow explicitly requires compatibility docs and breaking-change notes to be updated when shared runtime/plugin contracts change.
    - The MVP guidance explicitly avoids side-effectful automatic startup probes until a true read-only compatibility handshake exists.
  - Verify:
    - Add a runtime-compatibility document that records the supported MVP runtime shape, validated tuple, known incompatible runtimes, and operator workflow.
    - Update contributor workflow guidance so shared runtime/plugin contract changes must also update compatibility documentation.

- [x] MVP-036 Verify published `firehose-core` and `dummy-blockchain` image state and document the local-image fallback/update workflow.
  - Context:
    - MVP-014 is now validated through the local-first runtime workflow using a locally rebuilt dummy-chain image.
    - Current image check on 2026-05-25: `ghcr.io/streamingfast/dummy-blockchain:v1.7.7` and `:latest` resolve to the same published manifest and still do not validate the current SDS runtime path; the harness skips after observing missing `x-sds-rav` metadata.
    - Current image check on 2026-05-25: `ghcr.io/streamingfast/dummy-blockchain:1cea671` embeds `firecore` `ffc6ba2` and still does not validate the current SDS runtime path; the harness skips after observing missing `x-sds-rav` metadata.
    - Current image check on 2026-05-25: `ghcr.io/streamingfast/firehose-core:latest` resolves to `firehose-core` `v1.14.4`, commit `4493c5ce0735c50c1b06591de99cf014123e2ae5`, and is compatible with the current SDS runtime path when embedded in a rebuilt dummy-chain image.
    - Current fallback check on 2026-05-25: a locally rebuilt `ghcr.io/streamingfast/dummy-blockchain:sds-upstream-firecore-latest` image built with `--build-arg FIRECORE_VERSION=latest` passes `SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE=ghcr.io/streamingfast/dummy-blockchain:sds-upstream-firecore-latest go test ./test/integration -run '^TestFirecore$' -v -count=1` without skip.
    - Current local fallback check on 2026-05-25: `SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE=ghcr.io/streamingfast/dummy-blockchain:sds-local go test ./test/integration -run '^TestFirecore$' -v -count=1` passes without skip.
    - Until a refreshed published dummy-chain image exists, or an already-published dummy-chain tag is identified and validated, the repo-local default integration path still depends on local retagging and override-based validation.
    - `firehose-core` and downstream runtime images need a bump when SDS plugin communication protos under `proto/graph/substreams/data_service/sds/...` change, or when auth/session/metering plugin behavior transitive to SDS gRPC client calls changes in `provider/plugin/auth.go`, `provider/plugin/session.go`, or `provider/plugin/metering.go`.
  - Assumptions:
    - `A5`
  - Done when:
    - The compatibility docs record the current published-image check, the known-good local fallback, and the concrete update criteria for when `firehose-core` and downstream runtime images need to be rebuilt.
    - A published `firehose-core` image exists that is built against the current SDS-compatible plugin/runtime contract, or an existing published tag is identified as compatible.
    - The current published `dummy-blockchain` image state is documented, including whether the published tag embeds the compatible `firehose-core` image.
    - The local fallback path for stale published chain images is documented and validated.
  - Verify:
    - Inspect the currently published `firehose-core` and `dummy-blockchain` tags before assuming a StreamingFast image refresh has landed.
    - Run `SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE=ghcr.io/streamingfast/dummy-blockchain:sds-upstream-firecore-latest go test ./test/integration -run '^TestFirecore$' -v -count=1` against a dummy-chain image rebuilt with `--build-arg FIRECORE_VERSION=latest` and confirm it passes without skip.
    - Keep the fully local fallback documented and validated with `SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE=ghcr.io/streamingfast/dummy-blockchain:sds-local go test ./test/integration -run '^TestFirecore$' -v -count=1`.

- [x] MVP-023 Define the final MVP observability floor beyond structured logs and status tooling.
  - Context:
    - MVP requires operational visibility beyond logs alone.
    - Decision on 2026-05-06: the MVP observability floor includes structured logs, operator inspection/status tooling, and basic Prometheus-style metrics endpoints.
    - Distributed tracing and richer backend-specific observability remain post-MVP unless required by a named target provider environment.
  - Assumptions:
    - `A4`
  - Done when:
    - The repo has a documented observability floor for MVP.
    - It is clear that basic metrics endpoints are part of MVP, while distributed tracing is not required for MVP.
  - Verify:
    - Update [docs/mvp-scope.md](../../docs/mvp-scope.md) with the observability decision and confirm the decision is reflected in scope.

- [x] MVP-024 Implement basic operator-facing inspection/status surfaces, metrics, and log correlation.
  - Context:
    - Operators need enough visibility to debug runtime/payment issues.
    - The MVP observability floor now includes structured logs, basic Prometheus-style metrics, and operator inspection/status tooling.
  - Assumptions:
    - `A4`
  - Done when:
    - Logs provide enough correlation to understand session/payment events.
    - Basic metrics cover active sessions/workers, session starts/ends by reason, RAV request/accept/reject outcomes, low-funds stops, metered usage, payment-control pending state, and key request latency/error counts where practical.
    - Provider/operator tooling exposes basic status views and correlation aids without assuming a finalized metrics/tracing backend.
    - This task complements MVP-032 rather than replacing concrete runtime/session/payment inspection APIs.
  - Verify:
    - Manual verification that operators can inspect and reason about low-funds, restart, and collection flows without code-level debugging.
  - Implemented:
    - The private provider operator gateway now exposes an authenticated Prometheus text endpoint at `/metrics`.
    - Metrics require `operator.read` bearer authorization and are served from the private operator listener rather than the public payment gateway listener.
    - The endpoint reports current provider sessions by status, terminated sessions by end reason, active runtime workers, accepted RAV count, collection lifecycle records by state, low-funds sessions, payment-control pending sessions, RAV-request eligible sessions, and retained usage totals.
    - Metric labels intentionally avoid high-cardinality session IDs while preserving operator-useful aggregate status.
    - Existing structured logs already carry session/payment correlation fields such as `session_id`, `payer`, signer, worker key, and payment/control outcomes; `MVP-032` operator status views provide the corresponding status correlation surface.
    - Focused coverage verifies metrics auth, emitted status/funds/usage metrics, collection metrics, and absence of session labels.

- [x] MVP-032 Expose operator runtime/session/payment inspection APIs and CLI/status flows.
  - Context:
    - The MVP scope requires operators to inspect session, payment, and collection state, not only settlement-ready records.
    - `GetSessionStatus` now exposes whether provider-side runtime payment control is pending, which is useful runtime scaffolding for this task but does not replace authenticated operator inspection APIs or CLI/status flows.
  - Assumptions:
    - `A4`
    - `A5`
    - `A6`
  - Done when:
    - The provider exposes authenticated runtime/status APIs for active or recent sessions, payment state, latest accepted/requested RAV context, and current low-funds/control state where applicable.
    - Operator-facing CLI or status tooling can retrieve and display that runtime state without direct backend/database access.
    - Low-funds inspection includes enough actionable information for an operator to understand whether additional escrow funding is required and why.
  - Verify:
    - Add manual or integration coverage for inspecting an active or recently interrupted session, a low-funds session, and persisted post-restart payment state.
  - Implemented:
    - Authenticated provider operator session APIs expose session status, accumulated usage, baseline usage, accepted RAV summary, collection state, payment-control pending state, and low-funds/payment state.
    - Added an operator payment state payload with last assessed funds status, current and projected outstanding value, escrow balance when known, minimum needed amount, check errors, and operator hints.
    - `sds provider operator sessions list|get` displays the runtime/payment state without direct backend access, and `sessions list` can filter by `--funds-status=ok|insufficient|unknown`.
    - Focused tests cover low-funds payment-state API output, funds-status filtering, and CLI text presentation.

## Validation and Documentation Tasks

- [x] MVP-025 Add MVP acceptance coverage for the primary end-to-end scenarios in docs/tests/manual verification.
  - Context:
    - The MVP scope makes scenarios the primary definition of done.
    - The local SDS stack is the MVP acceptance source of truth because it validates SDS-mediated paid Substreams access against a controlled Substreams-compatible data plane.
    - Substreams package execution correctness, Firehose indexing correctness, and production provider ingress behavior are outside SDS MVP acceptance and remain deployment smoke-test concerns.
    - The scenario matrix records the test/manual evidence for each scenario.
  - Assumptions:
    - none
  - Done when:
    - The key scenarios from [docs/mvp-scope.md](../../docs/mvp-scope.md) are covered by tests, reproducible manual flows, or both.
    - The repo identifies the local stack as the MVP acceptance environment and documents the scope boundary between SDS behavior and external Substreams/Firehose product correctness.
    - Scenario `B` is validated according to the fresh-session-after-interruption semantics in the revised scope, not resume semantics.
  - Verify:
    - [docs/mvp-acceptance-matrix.md](../../docs/mvp-acceptance-matrix.md) maps each scenario to local-stack validation evidence and out-of-scope data-plane concerns.
    - `TestConsumerIngress_CreatesFreshSessionAfterInterruptedStream` validates fresh-session semantics at the sidecar ingress boundary.
    - `go test ./test/integration/... -count=1` passes.

- [x] MVP-034 Fix repository PostgreSQL tests so migrations resolve from repo-relative state rather than a machine-specific absolute path.
  - Context:
    - `provider/repository/psql/database_test.go` currently points migrations at a machine-specific absolute path.
    - This breaks `go test ./...` outside the original author environment and makes validation results unreliable.
  - Assumptions:
    - none
  - Done when:
    - PostgreSQL repository tests load migrations from repo-relative state or embedded test-owned migration discovery rather than an author-specific filesystem path.
    - The test path works from a clean checkout on another machine and in CI-like environments.
    - Full-repo test failures are no longer caused by the current hardcoded migration location.
  - Verify:
    - Run `go test ./provider/repository/psql/...` from the repo root on a non-author-specific checkout path and confirm migrations apply successfully.

- [x] MVP-035 Make integration devenv startup resilient to local fixed-port collisions so the shared test environment is reproducible.
  - Context:
    - The integration stack currently relies on a fixed host RPC port for the local Anvil-based devenv.
    - Local port collisions can prevent `test/integration` startup even when the SDS code under test is otherwise correct.
  - Assumptions:
    - none
  - Done when:
    - Integration startup no longer depends on a single hardcoded host port being free with no fallback or operator override.
    - The devenv/test bootstrap either allocates ports safely, retries with a deterministic alternative strategy, or exposes a clear test/runtime override that the integration harness actually uses.
    - Port-allocation failures stop being a common non-product cause of integration test failure.
  - Verify:
    - Run `go test ./test/integration/...` with the default local port already occupied and confirm startup either succeeds using the supported fallback/override path or fails fast with a clear, actionable configuration message.

- [x] MVP-037 Isolate and harden the shared-state Firecore and low-funds integration tests so real-path acceptance remains deterministic across full-suite runs.
  - Context:
    - `MVP-014` introduced the heavier real-path Firecore acceptance harness, and `MVP-016` extends that harness with a real low-funds stream-stop scenario.
    - These tests are intentionally closer to a natural provider/runtime environment than typical unit-style integration tests: they boot the local chain/contracts, provider payment gateway, plugin gateway, consumer sidecar, Postgres, and dummy-blockchain/firecore together.
    - The integration suite still shares one devenv/chain state across tests, so MVP-037 standardizes on strictly unique on-chain payer/provider identities per stateful runtime scenario rather than snapshot/restore or per-test environment resets.
  - Assumptions:
    - none
  - Done when:
    - The real-path Firecore and consumer low-funds tests no longer rely on mutable shared payer/provider state across suite runs.
    - Full `go test ./test/integration/...` runs are deterministic with respect to escrow/provision setup for the low-funds scenarios used by `MVP-014` and `MVP-016`.
    - The repo documents that the chosen isolation model is strictly unique on-chain identities per stateful runtime scenario, with explicit pre-state guards rather than snapshot/restore.
  - Verify:
    - Run the affected low-funds and Firecore tests both in isolation and as part of a broader `./test/integration/...` run and confirm they produce the same result.
    - Add an assertion or helper-level guard that proves the expected pre-test escrow state before the behavioral assertion is evaluated.

- [x] MVP-038 Remove the deprecated wrapper-era usage-report runtime path and protobuf surfaces once the sidecar-ingress flow is the only supported MVP runtime path.
  - Context:
    - `MVP-017` and `MVP-031` intentionally kept explicit rejection handling for the legacy wrapper-era `ReportUsage` and `PaymentSession usage_report` paths so the transition remained fail-fast while the runtime shape was still settling.
    - The repo does not need to preserve that compatibility long-term once the consumer sidecar ingress is the only supported runtime integration path.
    - This task is specifically about removing the deprecated usage-report path end-to-end, not about preserving a deprecation shim indefinitely.
  - Assumptions:
    - `A2`
    - `A3`
  - Done when:
    - The deprecated consumer-side `ReportUsage` runtime path is removed rather than only rejected at runtime.
    - The deprecated provider `PaymentSessionRequest.usage_report` protobuf and handler path are removed.
    - Generated protobuf code, tests, and docs no longer describe wrapper-era usage-report progression as part of the supported MVP runtime contract.
  - Verify:
    - Regenerate protobuf outputs and confirm the repo builds cleanly without `usage_report` support.
    - Run the relevant provider, consumer-sidecar, and integration suites and confirm all supported ingress/runtime scenarios still pass without wrapper-era usage-report coverage.

- [ ] MVP-039 Post-MVP only: decouple the private Plugin Gateway and public Provider Gateway via an explicit internal RPC/event boundary and clarified runtime-state ownership.
  - Context:
    - The current MVP provider topology intentionally keeps a public Payment Gateway and a private Plugin Gateway as separate API/security surfaces while still co-deploying them as one provider runtime.
    - Today, metered-usage progression from the private plugin-facing path into provider-originated payment control relies on shared repository-backed runtime state plus an in-process notification seam.
    - That is acceptable for MVP because fully independent deployment of the public and private provider surfaces is not required.
    - If SDS later needs those surfaces to run more independently, the current in-memory notification seam and implicit co-location assumptions are not sufficient.
  - Assumptions:
    - `A3`
    - `A6`
  - Done when:
    - The private plugin-facing usage path and the public provider payment/control path communicate through an explicit internal gRPC or equivalent event boundary rather than an in-process callback.
    - The architecture clearly assigns authoritative ownership of runtime payment state needed for `RAVRequest` and low-funds decisions.
    - The provider runtime no longer depends on implicit single-process coordination between private and public gateway surfaces.
    - Docs describe the supported deployment topologies and the source of truth used for runtime payment decisions.
  - Verify:
    - Review the provider runtime docs and confirm they no longer assume implicit in-process notification between the two provider surfaces.
    - Add integration coverage for the chosen decoupled topology or equivalent internal-boundary contract tests before treating the split as supported.

- [x] MVP-026 Refresh protocol/runtime docs so they match the revised MVP architecture and completed acceptance scope.
  - Context:
    - [docs/mvp-scope.md](../../docs/mvp-scope.md) is the stable MVP target-state document.
    - The final MVP documentation pass aligned active docs with the sidecar-ingress runtime architecture, provider-originated payment control, authenticated operator APIs, TLS-by-default posture, runtime image compatibility state, and local-stack acceptance boundary.
  - Assumptions:
    - `A1`
    - `A4`
    - `A5`
  - Done when:
    - The repo documentation reflects the revised MVP architecture rather than the older reconnect/pricing assumptions.
    - MVP acceptance is documented as local-stack validation of SDS consumer/provider/oracle/payment behavior, with real-provider/testnet runs treated as deployment smoke tests rather than acceptance blockers.
    - Docs that describe provider runtime shape match the current public Payment Gateway plus private Plugin Gateway model.
    - Runtime compatibility docs identify the current `firehose-core`/`dummy-blockchain` image state and local-image fallback workflow from `MVP-036`.
    - Historical or superseded docs are archived away from the active documentation set.
  - Verify:
    - Review the updated docs against [docs/mvp-scope.md](../../docs/mvp-scope.md) and confirm there are no major contradictions.
    - Review [docs/mvp-acceptance-matrix.md](../../docs/mvp-acceptance-matrix.md), [docs/provider-runtime-compatibility.md](../../docs/provider-runtime-compatibility.md), [docs/provider-persistence-boundary.md](../../docs/provider-persistence-boundary.md), [docs/direct-provider-testnet-public-runbook.md](../../docs/direct-provider-testnet-public-runbook.md), and [README.md](../../README.md) for aligned runtime, security, and operator guidance.

## Notes on Scope Boundaries

- This backlog intentionally does **not** make aggregate multi-stream payer-level liability tracking an MVP requirement.
- It also does **not** make wallet-based funding UI or automated collection an MVP requirement.
- It does **not** make payment-session continuation across reconnects an MVP requirement.
- Supporting utilities such as `sds tools rav`, GRT/pricing refactors, and similar groundwork should be treated as helpful context unless they directly close an MVP acceptance task.
