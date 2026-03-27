# Substreams Data Service — MVP Implementation Backlog

_Last updated: 2026-03-24_

This document translates [docs/mvp-scope.md](../docs/mvp-scope.md) into concrete implementation tasks for the MVP.

It is intentionally separate from `plans/implementation-backlog.md`.

Rationale for the split:

- `plans/implementation-backlog.md` reflects the earlier implementation sequence and still contains useful historical context
- this document reflects the revised MVP scope agreed after the 2026-03-24 architecture rewrite
- the backlog now also incorporates provider/runtime work that landed separately in StreamingFast commits `5ffca3d` through `1416020`

This document is a scope-aligned execution backlog, not a priority list.

## How To Use This Document

- Use [docs/mvp-scope.md](../docs/mvp-scope.md) as the stable target-state definition.
- Use `plans/mvp-gap-analysis.md` for current-state assessment.
- Use this file to define the concrete MVP implementation work that remains.

Each task includes:

- **Context**: why the task exists
- **Assumptions**: scope-aligned assumptions that shape the task
- **Done when**: objective completion criteria
- **Verify**: how to corroborate the behavior

The status tracker below also includes:

- **Depends on**: tasks that should be frozen or completed first so downstream work does not build on moving semantics
- **Scenarios**: acceptance scenarios from [docs/mvp-scope.md](../docs/mvp-scope.md) (`A` through `G`) that the task materially contributes to

Unless otherwise scoped, the baseline validation for code changes remains:

- `go test ./...`
- `go vet ./...`
- `gofmt` on changed Go files

Recent provider persistence and integration scaffolding landed outside the original backlog sequencing. The tracker below treats that work as existing foundation and updates task status accordingly.

## Assumptions Register

These assumptions are referenced by task ID so it is clear which scope decisions or remaining open questions still matter.

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

- `A4` Observability scope beyond logs/status tooling is still open.
  - MVP work should implement structured logging and inspection/status surfaces without forcing a final metrics/tracing backend choice.

- `A5` Admin/operator authentication mechanism is still open.
  - MVP work should require authentication and keep the implementation pluggable enough to avoid boxing in the final auth choice.

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
| MVP-003 | `in_progress` | protocol | `A3`, `A6` | `MVP-027` | `D`, `F` | Define and document the provider-side runtime persistence model and its boundary with settlement lifecycle tracking |
| MVP-004 | `done` | protocol | `A2`, `A3` | none | `A`, `C` | Define and document the real runtime payment contract used by the public payment gateway, private plugin gateway, and consumer/provider payment loop |
| MVP-005 | `not_started` | oracle | `A1`, `A2`, `A5` | `MVP-033` | `A` | Implement a standalone oracle service with manual whitelist, canonical pricing, recommended-provider response, and control-plane endpoint return |
| MVP-006 | `not_started` | oracle | `A5` | `MVP-028` | `A`, `G` | Add authenticated oracle administration for whitelist and provider metadata management |
| MVP-007 | `not_started` | consumer | `A1`, `A2`, `A3` | `MVP-005`, `MVP-033` | `A` | Integrate consumer sidecar with oracle discovery while preserving direct-provider fallback and provider-returned data-plane resolution |
| MVP-008 | `in_progress` | provider-state | `A3`, `A6` | `MVP-003` | `D`, `F` | Complete durable provider runtime storage for sessions, usage, and accepted RAV state, distinct from collection lifecycle tracking |
| MVP-009 | `not_started` | provider-state | `A3`, `A5` | `MVP-003`, `MVP-022`, `MVP-029` | `D`, `F` | Expose provider inspection and settlement-data retrieval APIs for accepted and collectible RAV state |
| MVP-010 | `not_started` | funding-control | `A6` | `MVP-004` | `C` | Implement session-local low-funds detection and provider Continue/Pause/Stop decisions during streaming |
| MVP-011 | `not_started` | funding-control | `A6` | `MVP-010` | `C` | Propagate provider stop/pause decisions through consumer sidecar into the real client path |
| MVP-012 | `not_started` | funding-control | none | `MVP-004` | `A`, `C` | Add deterministic RAV issuance thresholds suitable for real runtime behavior |
| MVP-013 | `deferred` | consumer | `A3` | none | none | Post-MVP only: implement true provider-authoritative payment-session reconnect/resume semantics |
| MVP-014 | `in_progress` | provider-integration | `A3` | `MVP-004` | `A` | Integrate the public Payment Gateway and private Plugin Gateway into the real provider streaming path |
| MVP-015 | `in_progress` | provider-integration | `A3` | `MVP-004`, `MVP-014` | `A`, `C` | Wire real byte metering and session correlation from the plugin path into the payment-state repository used by the gateway |
| MVP-016 | `not_started` | provider-integration | `A6` | `MVP-010`, `MVP-014` | `C` | Enforce gateway Continue/Pause/Stop decisions in the live provider stream lifecycle |
| MVP-017 | `not_started` | consumer-integration | `A1`, `A2`, `A3` | `MVP-007`, `MVP-011`, `MVP-033` | `A`, `C` | Implement the consumer sidecar as a Substreams-compatible endpoint/proxy rather than only a wrapper-controlled lifecycle service |
| MVP-018 | `not_started` | tooling | none | `MVP-032` | `E` | Implement operator funding CLI flows for approve/deposit/top-up beyond local demo assumptions |
| MVP-019 | `not_started` | tooling | `A5` | `MVP-009`, `MVP-022` | `D`, `F` | Implement provider inspection CLI flows for accepted and collectible RAV data |
| MVP-020 | `not_started` | tooling | `A5` | `MVP-009`, `MVP-022`, `MVP-029` | `F` | Implement manual collection CLI flow that fetches provider settlement state and crafts/signs/submits collect transactions locally |
| MVP-021 | `not_started` | security | `A5` | none | `G` | Make TLS the default non-dev runtime posture for oracle, sidecar, and provider integration paths |
| MVP-022 | `not_started` | security | `A5` | `MVP-009`, `MVP-028` | `D`, `F`, `G` | Add authentication and authorization to provider admin/operator APIs |
| MVP-023 | `open_question` | observability | `A4` | none | `A`, `C`, `D`, `F`, `G` | Define the final MVP observability floor beyond structured logs and status tooling |
| MVP-024 | `not_started` | observability | `A4` | `MVP-023` | `C`, `D`, `F`, `G` | Implement basic operator-facing inspection/status surfaces and log correlation |
| MVP-025 | `in_progress` | validation | none | none | `A`, `B`, `C`, `D`, `E`, `F`, `G` | Add MVP acceptance coverage for the primary end-to-end scenarios in docs/tests/manual verification |
| MVP-026 | `in_progress` | docs | `A1`, `A4`, `A5` | `MVP-023`, `MVP-028`, `MVP-033` | `A`, `B`, `C`, `D`, `E`, `F`, `G` | Refresh protocol/runtime docs so they match the revised MVP architecture and remaining open questions |
| MVP-027 | `done` | protocol | `A3` | none | `B`, `D`, `F` | Freeze MVP payment/session identity semantics for fresh sessions and non-reused collection/payment lineage |
| MVP-028 | `open_question` | security | `A5` | none | `G` | Define the MVP authentication and authorization contract for oracle and provider operator surfaces |
| MVP-029 | `not_started` | provider-state | `A3`, `A5` | `MVP-003`, `MVP-022` | `D`, `F` | Implement provider collection lifecycle transitions and update surfaces for `collectible`, `collect_pending`, `collected`, and retryable collection state |
| MVP-030 | `in_progress` | provider-integration | `A5` | `MVP-014`, `MVP-017` | `A`, `G` | Add runtime compatibility and preflight checks for real provider/plugin deployments |
| MVP-031 | `not_started` | runtime-payment | `A2`, `A3` | `MVP-004`, `MVP-012`, `MVP-014`, `MVP-017` | `A`, `C` | Wire the long-lived payment-control loop behind the consumer-sidecar ingress path used by real runtime traffic |
| MVP-032 | `not_started` | operations | `A4`, `A5`, `A6` | `MVP-008`, `MVP-010`, `MVP-022` | `C`, `D`, `F`, `G` | Expose operator runtime/session/payment inspection APIs and CLI/status flows |
| MVP-033 | `done` | protocol | `A1` | none | `A` | Freeze the chain/network discovery input contract across client, sidecar, and oracle |
| MVP-034 | `done` | validation | none | none | none | Fix repository PostgreSQL tests so migrations resolve from repo-relative state rather than a machine-specific absolute path |
| MVP-035 | `done` | validation | none | none | none | Make integration devenv startup resilient to local fixed-port collisions so the shared test environment is reproducible |

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
    - Review [docs/mvp-scope.md](../docs/mvp-scope.md) and confirm there is no conflicting pricing authority language.

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
    - Review [docs/mvp-scope.md](../docs/mvp-scope.md) and confirm the workflow and decisions table match this contract.

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
    - Review [docs/mvp-scope.md](../docs/mvp-scope.md) and confirm the network-discovery contract is present in the main workflow text rather than left as an open question.

- [x] MVP-027 Freeze MVP payment/session identity semantics for fresh sessions and non-reused collection/payment lineage.
  - Context:
    - The revised MVP scope intentionally avoids reconnect/payment identity reuse.
  - Assumptions:
    - `A3`
  - Done when:
    - The repo documents that reconnects create new SDS payment sessions rather than reusing prior payment lineage.
    - Collection/payment identity reuse is no longer an MVP open question.
  - Verify:
    - Review [docs/mvp-scope.md](../docs/mvp-scope.md) and confirm the reconnect scenario and major decisions table match this rule.

- [ ] MVP-003 Define and document the provider-side runtime persistence model and its boundary with settlement lifecycle tracking.
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
    - Review [provider/repository/repository.go](../provider/repository/repository.go) and [provider/gateway/REPOSITORY.md](../provider/gateway/REPOSITORY.md) against backlog task wording.

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
    - Review the backlog wording against [cmd/sds/impl/provider_gateway.go](../cmd/sds/impl/provider_gateway.go), [provider/plugin/gateway.go](../provider/plugin/gateway.go), and the plugin protobufs.

## Oracle Tasks

- [ ] MVP-005 Implement a standalone oracle service with manual whitelist, canonical pricing, recommended-provider response, and control-plane endpoint return.
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

- [ ] MVP-006 Add authenticated oracle administration for whitelist and provider metadata management.
  - Context:
    - Oracle governance actions must require authentication in MVP.
  - Assumptions:
    - `A5`
  - Done when:
    - Oracle whitelist/provider metadata changes require authenticated operator access.
    - The implementation does not rely on an open admin surface.
  - Verify:
    - Add tests for unauthenticated rejection and authenticated success on admin actions.

## Consumer Tasks

- [ ] MVP-007 Integrate consumer sidecar with oracle discovery while preserving direct-provider fallback and provider-returned data-plane resolution.
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

- [ ] MVP-008 Complete durable provider runtime storage for sessions, usage, and accepted RAV state, distinct from collection lifecycle tracking.
  - Context:
    - StreamingFast landed:
      - PostgreSQL repository foundation
      - DSN-based repository selection
      - gateway integration with that repository
      - repository test coverage
    - Remaining work is to close the gap between existing runtime persistence and the MVP durability scenarios.
  - Assumptions:
    - `A3`
    - `A6`
  - Done when:
    - Provider restart does not lose session/runtime state required by the gateway and plugin path.
    - Accepted RAV state needed for post-restart inspection and settlement survives restart in the durable backend.
    - The task no longer includes collection lifecycle state, which remains tracked under MVP-029.
  - Verify:
    - Add or unskip a restart-focused integration or persistence test that validates accepted state survives process restart using the durable repository path.

- [ ] MVP-009 Expose provider inspection and settlement-data retrieval APIs for accepted and collectible RAV state.
  - Context:
    - CLI inspection and manual collection require a provider-side way to retrieve settlement-relevant data.
    - Current `GetSessionStatus` is useful runtime scaffolding, but not sufficient settlement inspection coverage.
  - Assumptions:
    - `A3`
    - `A5`
  - Done when:
    - Provider exposes APIs for listing and fetching accepted and collectible payment state.
    - The returned data is sufficient for operator inspection and CLI-based collection.
    - The API shape is stable enough for MVP-019 and MVP-020 without direct backend reads.
  - Verify:
    - Add integration coverage for listing and fetching settlement-relevant accepted state.

- [ ] MVP-029 Implement provider collection lifecycle transitions and update surfaces for `collectible`, `collect_pending`, `collected`, and retryable collection state.
  - Context:
    - The revised scope keeps collection lifecycle tracking as explicit provider-side work.
    - The recent persistence work did not complete this lifecycle.
  - Assumptions:
    - `A3`
    - `A5`
  - Done when:
    - Provider persistence supports the required collection lifecycle states and transition rules.
    - There is a defined provider-side update path for marking collection attempts pending, completed, or retryable.
    - Retry behavior is documented so CLI flows can be idempotent.
  - Verify:
    - Add persistence or integration tests that cover `collectible` -> `collect_pending` -> `collected` and a retryable failure path.

## Funding Control and Runtime Payment Tasks

- [ ] MVP-010 Implement session-local low-funds detection and provider Continue/Pause/Stop decisions during streaming.
  - Context:
    - The MVP requires low-funds handling during active streaming, but only on a session-local basis.
  - Assumptions:
    - `A6`
  - Done when:
    - Provider can compare session-local exposure against available funding.
    - Provider emits the appropriate control/funding messages during active streams.
    - Low-funds behavior includes enough machine-readable state for operator tooling and client-side messaging.
  - Verify:
    - Add an integration test with intentionally low funding that reaches a stop/pause condition during streaming.

- [ ] MVP-011 Propagate provider stop/pause decisions through consumer sidecar into the real client path.
  - Context:
    - Low-funds logic is incomplete until the client path actually obeys it.
  - Assumptions:
    - `A6`
  - Done when:
    - Consumer sidecar converts provider control/funding messages into client-visible stop/pause behavior.
    - Real client integration honors those decisions.
  - Verify:
    - Add integration/manual verification showing the real client path stops or pauses when the provider requires it.

- [ ] MVP-012 Add deterministic RAV issuance thresholds suitable for real runtime behavior.
  - Context:
    - The current runtime/payment loop foundation exists, but the real-runtime issuance policy still needs to be made explicit.
  - Assumptions:
    - none
  - Done when:
    - RAV issuance is controlled by explicit policy such as value/time/provider-request thresholds.
    - Threshold behavior is documented and tested.
  - Verify:
    - Add tests that show repeated usage does not force a signature on every report unless policy requires it.

## Real Provider and Consumer Integration Tasks

- [ ] MVP-014 Integrate the public Payment Gateway and private Plugin Gateway into the real provider streaming path.
  - Context:
    - The recent commit range established the provider-side dual-gateway shape and the shared repository wiring.
    - The backlog should now treat that as the concrete provider integration target.
  - Assumptions:
    - `A3`
  - Done when:
    - The real provider path validates payment/session state through the public Payment Gateway.
    - Firehose-core plugin traffic goes through the private Plugin Gateway.
    - Both paths share the same authoritative provider-side repository state.
  - Verify:
    - Add a real-path integration test or manual verification against the current provider shape.

- [ ] MVP-015 Wire real byte metering and session correlation from the plugin path into the payment-state repository used by the gateway.
  - Context:
    - The recent commit range fixed session ID propagation and pushed more correlation through typed plugin fields and shared repository state.
    - The remaining work is to validate the billing and payment-state behavior at acceptance level.
  - Assumptions:
    - `A3`
  - Done when:
    - Real provider-side byte metering feeds the repository state used for payment progression.
    - Session correlation is stable across auth, session, usage, and gateway-side payment state.
    - The runtime path does not rely on consumer-reported bytes as the billing source of truth.
  - Verify:
    - Add tests or manual instrumentation evidence showing live provider/plugin activity updates the payment-state repository consistently.

- [ ] MVP-016 Enforce gateway Continue/Pause/Stop decisions in the live provider stream lifecycle.
  - Context:
    - Provider-side control logic is incomplete if the live provider stream does not obey it.
  - Assumptions:
    - `A6`
  - Done when:
    - The real provider path can enforce SDS control decisions during live streaming.
  - Verify:
    - Add manual or automated verification where the provider stops or pauses the live stream based on gateway control decisions.

- [ ] MVP-017 Implement the consumer sidecar as a Substreams-compatible endpoint/proxy rather than only a wrapper-controlled lifecycle service.
  - Context:
    - The revised MVP scope elevates the consumer sidecar from helper service to user-facing SDS boundary.
    - Existing `sds sink run` integration is useful foundation, but it does not satisfy the data-plane compatibility goal by itself.
  - Assumptions:
    - `A1`
    - `A2`
    - `A3`
  - Done when:
    - Existing Substreams tooling can point at the consumer sidecar endpoint for the normal data-plane path.
    - The consumer sidecar hides oracle lookup, provider session init, and payment coordination behind that ingress.
    - Wrapper-specific orchestration is no longer the only real integration path.
  - Verify:
    - Add a real-path integration or documented manual scenario that runs through the consumer sidecar endpoint rather than only `sds sink run`.

- [ ] MVP-031 Wire the long-lived payment-control loop behind the consumer-sidecar ingress path used by real runtime traffic.
  - Context:
    - MVP requires payment state to keep advancing during real streaming, not only through wrapper-driven flows.
    - The loop should ultimately sit behind the same user-facing sidecar ingress the client uses.
  - Assumptions:
    - `A2`
    - `A3`
  - Done when:
    - The real client/provider integration keeps the SDS payment-control loop active alongside the live stream behind the consumer-sidecar ingress path.
    - Provider-driven RAV requests, acknowledgements, and control messages flow through the production runtime path rather than only through wrapper commands.
  - Verify:
    - Add a real-path integration or documented manual verification showing stream start, at least one provider-driven payment update during live streaming, and synchronized session state until normal end or stop/pause.

## Operator Tooling Tasks

- [ ] MVP-018 Implement operator funding CLI flows for approve/deposit/top-up beyond local demo assumptions.
  - Context:
    - Funding is an MVP operator workflow, but current tooling is still demo-oriented.
    - `sds tools rav` is useful support tooling, not a substitute for escrow funding flows.
  - Assumptions:
    - none
  - Done when:
    - CLI commands exist for approve/deposit/top-up in a provider-operator or payer-operator workflow.
    - The commands are not limited to local deterministic devenv assumptions.
    - The documented operator flow links funding actions to low-funds/runtime inspection surfaces.
  - Verify:
    - Add command-level tests where practical and document a manual funding flow that works against a non-demo configuration.

- [ ] MVP-019 Implement provider inspection CLI flows for accepted and collectible RAV data.
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

- [ ] MVP-020 Implement manual collection CLI flow that fetches provider settlement state and crafts/signs/submits collect transactions locally.
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

## Security, Runtime Compatibility, and Observability Tasks

- [ ] MVP-028 Define the MVP authentication and authorization contract for oracle and provider operator surfaces.
  - Context:
    - The only real architecture-level open questions still left in scope are authn/authz and observability depth.
  - Assumptions:
    - `A5`
  - Done when:
    - The repo documents the MVP authn/authz approach for oracle and provider operator/admin surfaces.
    - It is clear which endpoints/actions require operator privileges and which credentials satisfy that requirement.
    - MVP-006 and MVP-022 can implement the same contract rather than inventing separate security behavior.
  - Verify:
    - Confirm oracle and provider admin task definitions point to the same auth contract.

- [ ] MVP-021 Make TLS the default non-dev runtime posture for oracle, sidecar, and provider integration paths.
  - Context:
    - The MVP requires real transport security without forcing a perfect production-hardening story.
  - Assumptions:
    - `A5`
  - Done when:
    - Non-dev/runtime docs and defaults use TLS for oracle, consumer sidecar, and provider integration surfaces.
    - Plaintext behavior is clearly scoped to local/dev/demo usage.
    - Operator/admin traffic does not rely on plaintext-by-default behavior outside explicitly dev-scoped workflows.
  - Verify:
    - Add validation or smoke coverage for TLS-enabled startup and client connectivity across oracle and sidecar/provider paths.

- [ ] MVP-022 Add authentication and authorization to provider admin/operator APIs.
  - Context:
    - Provider-side operator actions must not rely on open or anonymous admin APIs.
  - Assumptions:
    - `A5`
  - Done when:
    - Provider inspection and settlement-retrieval APIs require authentication and authorization according to the shared MVP contract.
    - The implementation rejects unauthenticated or unauthorized access to operator-only provider actions.
  - Verify:
    - Add tests for authenticated success and unauthenticated rejection.

## Runtime Compatibility Tasks

- [ ] MVP-030 Add runtime compatibility and preflight checks for real provider/plugin deployments.
  - Context:
    - Recent README, config, and firecore test scaffolding identify the target runtime more clearly.
    - The repo still lacks proper enforced preflight validation for that deployment shape.
  - Assumptions:
    - `A5`
  - Done when:
    - The repo identifies at least one named real-provider target environment for MVP acceptance and documents the required runtime compatibility constraints clearly enough for operators to validate before rollout.
    - The required runtime versions, plugin compatibility assumptions, and non-demo configuration prerequisites for that environment are documented.
    - Startup or preflight checks fail fast when the provider/plugin environment is incompatible with the required SDS runtime contract.
  - Verify:
    - Add a startup/preflight validation test or a documented manual verification flow that demonstrates clear failure modes for unsupported runtime combinations.

- [ ] MVP-023 Define the final MVP observability floor beyond structured logs and status tooling.
  - Context:
    - MVP requires operational visibility, but metrics/tracing depth is still open.
  - Assumptions:
    - `A4`
  - Done when:
    - The repo has a documented observability floor for MVP.
    - It is clear whether metrics endpoints are part of MVP or not.
  - Verify:
    - Update [docs/mvp-scope.md](../docs/mvp-scope.md) and narrow the open question if a decision is made.

- [ ] MVP-024 Implement basic operator-facing inspection/status surfaces and log correlation.
  - Context:
    - Even if metrics remain open, operators need enough visibility to debug runtime/payment issues.
  - Assumptions:
    - `A4`
  - Done when:
    - Logs provide enough correlation to understand session/payment events.
    - Provider/operator tooling exposes basic status views and correlation aids without assuming a finalized metrics/tracing backend.
    - This task complements MVP-032 rather than replacing concrete runtime/session/payment inspection APIs.
  - Verify:
    - Manual verification that operators can inspect and reason about low-funds, restart, and collection flows without code-level debugging.

- [ ] MVP-032 Expose operator runtime/session/payment inspection APIs and CLI/status flows.
  - Context:
    - The MVP scope requires operators to inspect session, payment, and collection state, not only settlement-ready records.
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

## Validation and Documentation Tasks

- [ ] MVP-025 Add MVP acceptance coverage for the primary end-to-end scenarios in docs/tests/manual verification.
  - Context:
    - The MVP scope makes scenarios the primary definition of done.
    - The recent commit range added real-path integration scaffolding, including `TestFirecore`, but it is not yet enough to close the scenario matrix.
  - Assumptions:
    - none
  - Done when:
    - The key scenarios from [docs/mvp-scope.md](../docs/mvp-scope.md) are covered by tests, reproducible manual flows, or both.
    - The repo identifies which scenarios are validated locally versus against a named real-provider environment.
    - At least scenarios `A`, `C`, and `G` have a defined validation path against a real-provider environment rather than relying only on local demo coverage.
    - Scenario `B` is validated according to the fresh-session-after-interruption semantics in the revised scope, not resume semantics.
  - Verify:
    - Update the scenario matrix or equivalent test/docs references for each acceptance scenario, including environment, validation method, and source of truth for the result.

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

- [ ] MVP-026 Refresh protocol/runtime docs so they match the revised MVP architecture and remaining open questions.
  - Context:
    - [docs/mvp-scope.md](../docs/mvp-scope.md) has been updated.
    - The rest of the documentation set and backlog still needs to catch up to that architecture and to the recent provider-side implementation changes.
  - Assumptions:
    - `A1`
    - `A4`
    - `A5`
  - Done when:
    - The repo documentation reflects the revised MVP architecture rather than the older reconnect/pricing assumptions.
    - Remaining open questions are limited to auth and observability rather than already-resolved scope decisions.
    - Docs that describe provider runtime shape match the current public Payment Gateway plus private Plugin Gateway model.
  - Verify:
    - Review the updated docs against [docs/mvp-scope.md](../docs/mvp-scope.md) and confirm there are no major contradictions.

## Notes on Scope Boundaries

- This backlog intentionally does **not** make aggregate multi-stream payer-level liability tracking an MVP requirement.
- It also does **not** make wallet-based funding UI or automated collection an MVP requirement.
- It does **not** make payment-session continuation across reconnects an MVP requirement.
- Supporting utilities such as `sds tools rav`, GRT/pricing refactors, and similar groundwork should be treated as helpful context unless they directly close an MVP acceptance task.
