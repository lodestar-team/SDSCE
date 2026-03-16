# Substreams Data Service — MVP Implementation Backlog

_Last updated: 2026-03-13_

This document translates `docs/mvp-scope.md` into concrete implementation tasks for the MVP.

It is intentionally separate from `plans/implementation-backlog.md`.

Rationale for the split:

- `plans/implementation-backlog.md` reflects the earlier implementation sequence and still contains useful historical context
- this document reflects the revised MVP scope agreed after the MVP rescoping work
- the MVP scope is now broader than the original sidecar-centric backlog and includes new deliverables such as the oracle, operator tooling, and provider-side persistence/settlement workflows

This document is a scope-aligned execution backlog, not a priority list.

## How To Use This Document

- Use `docs/mvp-scope.md` as the stable target-state definition.
- Use `plans/mvp-gap-analysis.md` for current-state assessment.
- Use this file to define the concrete MVP implementation work that remains.

Each task includes:

- **Context**: why the task exists
- **Assumptions**: design assumptions or unresolved questions that affect the task definition
- **Done when**: objective completion criteria
- **Verify**: how to corroborate the behavior

The status tracker below also includes:

- **Depends on**: tasks that should be frozen or completed first so downstream work does not build on moving semantics
- **Scenarios**: acceptance scenarios from `docs/mvp-scope.md` (`A` through `G`) that the task materially contributes to

Unless otherwise scoped, the baseline validation for code changes remains:

- `go test ./...`
- `go vet ./...`
- `gofmt` on changed Go files

## Assumptions Register

These assumptions are referenced by task ID so it is clear where unresolved decisions still matter.

- `A1` Chain/network discovery input is still open.
  - MVP work should support an explicit chain/network input path now.
  - Automatic derivation from the Substreams package remains optional/open.

- `A2` Pricing authority between oracle metadata and provider handshake is still open.
  - MVP work should avoid hard-coding a final authority rule unless/until aligned with StreamingFast.

- `A3` Canonical payment identity and `collection_id` reuse semantics are still open.
  - MVP work should isolate persistence and reconnect logic behind a model that can evolve without a full rewrite.

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
| MVP-001 | `open_question` | protocol | `A2` | none | `A` | Freeze the pricing exposure contract between oracle metadata and provider handshake |
| MVP-002 | `not_started` | protocol | `A1`, `A3` | `MVP-027` | `B` | Freeze reconnect handshake semantics so provider can return fresh or latest-known resumable RAV during normal session init |
| MVP-003 | `not_started` | protocol | `A3` | `MVP-027` | `B`, `D`, `F` | Define the durable provider-side payment and settlement data model |
| MVP-004 | `not_started` | protocol | none | none | `A`, `B`, `C` | Define and document the byte-billing and payment/header contract used in the real runtime path |
| MVP-005 | `not_started` | oracle | `A1`, `A2`, `A5` | `MVP-033` | `A` | Implement a standalone oracle service with manual whitelist and recommended-provider response |
| MVP-006 | `not_started` | oracle | `A5` | `MVP-028` | `A`, `G` | Add authenticated oracle administration for whitelist and provider metadata management |
| MVP-007 | `not_started` | consumer | `A1`, `A2` | `MVP-001`, `MVP-005`, `MVP-033` | `A` | Integrate consumer sidecar with oracle discovery while preserving direct-provider fallback |
| MVP-008 | `not_started` | provider-state | `A3`, `A6` | `MVP-003` | `B`, `D`, `F` | Add durable provider storage for accepted RAV, session state, and collection lifecycle state |
| MVP-009 | `not_started` | provider-state | `A3` | `MVP-003`, `MVP-029` | `D`, `F` | Expose provider inspection and settlement-data retrieval APIs for accepted/collectible RAV state |
| MVP-010 | `not_started` | funding-control | `A6` | `MVP-004` | `C` | Implement session-local low-funds detection and provider Continue/Pause/Stop decisions during streaming |
| MVP-011 | `not_started` | funding-control | `A6` | `MVP-010` | `C` | Propagate provider stop/pause decisions through consumer sidecar into the real client path |
| MVP-012 | `not_started` | funding-control | none | `MVP-004` | `A`, `C` | Add deterministic RAV issuance thresholds suitable for real runtime behavior |
| MVP-013 | `not_started` | consumer | `A3` | `MVP-002`, `MVP-008` | `B` | Implement provider-authoritative reconnect/resume in the normal handshake path |
| MVP-014 | `not_started` | provider-integration | none | `MVP-004` | `A` | Integrate provider gateway validation into the real provider streaming path |
| MVP-015 | `not_started` | provider-integration | none | `MVP-004`, `MVP-014` | `A`, `C` | Wire real byte metering from the provider/plugin path into gateway payment state |
| MVP-016 | `not_started` | provider-integration | `A6` | `MVP-010`, `MVP-014` | `C` | Enforce gateway Continue/Pause/Stop decisions in the live provider stream lifecycle |
| MVP-017 | `not_started` | consumer-integration | `A1` | `MVP-007`, `MVP-011`, `MVP-033` | `A`, `C` | Integrate the real consumer/client path with consumer sidecar init, usage reporting, and session end |
| MVP-018 | `not_started` | tooling | none | `MVP-032` | `E` | Implement operator funding CLI flows for approve/deposit/top-up beyond local demo assumptions |
| MVP-019 | `not_started` | tooling | `A3`, `A5` | `MVP-009`, `MVP-022` | `D`, `F` | Implement provider inspection CLI flows for collectible/accepted RAV data |
| MVP-020 | `not_started` | tooling | `A3` | `MVP-009`, `MVP-029` | `F` | Implement manual collection CLI flow that crafts/signs/submits collect transactions locally |
| MVP-021 | `not_started` | security | `A5` | none | `G` | Make TLS the default non-dev runtime posture for oracle, sidecar, and provider integration paths |
| MVP-022 | `not_started` | security | `A5` | `MVP-009`, `MVP-028` | `D`, `F`, `G` | Add authentication and authorization to provider admin/operator APIs |
| MVP-023 | `open_question` | observability | `A4` | none | `A`, `B`, `C`, `D`, `F`, `G` | Define the final MVP observability floor beyond structured logs and status tooling |
| MVP-024 | `not_started` | observability | `A4` | `MVP-023` | `B`, `C`, `D`, `F`, `G` | Implement basic operator-facing inspection/status surfaces and log correlation |
| MVP-025 | `not_started` | validation | none | none | `A`, `B`, `C`, `D`, `E`, `F`, `G` | Add MVP acceptance coverage for the primary end-to-end scenarios in docs/tests/manual verification |
| MVP-026 | `not_started` | docs | `A1`, `A2`, `A3`, `A4`, `A5` | `MVP-001`, `MVP-002`, `MVP-003`, `MVP-004`, `MVP-023`, `MVP-027`, `MVP-028`, `MVP-033` | `A`, `B`, `C`, `D`, `E`, `F`, `G` | Refresh protocol/runtime docs so they match the MVP architecture and explicit open questions |
| MVP-027 | `open_question` | protocol | `A3` | none | `B`, `D`, `F` | Freeze canonical payment identity, `collection_id` reuse, and session-vs-payment keying semantics |
| MVP-028 | `open_question` | security | `A5` | none | `G` | Define the MVP authentication and authorization contract for oracle and provider operator surfaces |
| MVP-029 | `not_started` | provider-state | `A3` | `MVP-003`, `MVP-027` | `D`, `F` | Implement provider collection lifecycle transitions and update surfaces for `collectible`, `collect_pending`, `collected`, and retryable collection state |
| MVP-030 | `not_started` | provider-integration | none | `MVP-014`, `MVP-017` | `A`, `G` | Add runtime compatibility and preflight checks for real provider/plugin deployments |
| MVP-031 | `not_started` | runtime-payment | none | `MVP-004`, `MVP-012`, `MVP-014`, `MVP-017` | `A`, `C` | Wire the live PaymentSession and RAV-control loop into the real client/provider runtime path |
| MVP-032 | `not_started` | operations | `A3`, `A4`, `A5` | `MVP-003`, `MVP-008`, `MVP-010`, `MVP-022` | `B`, `C`, `D`, `F`, `G` | Expose operator runtime/session/payment inspection APIs and CLI/status flows |
| MVP-033 | `open_question` | protocol | `A1` | none | `A` | Freeze the chain/network discovery input contract across client, sidecar, and oracle |

## Protocol and Contract Tasks

- [ ] MVP-001 Freeze the pricing exposure contract between oracle metadata and provider handshake.
  - Context:
    - The MVP scope expects pricing to likely appear in both places, but pricing authority is still open.
    - Real consumer/oracle/provider integration should not proceed on hand-wavy assumptions here.
  - Assumptions:
    - `A2`
  - Done when:
    - The intended relationship between oracle pricing metadata and provider handshake pricing is documented.
    - The implementation path does not rely on contradictory authority assumptions across components.
  - Verify:
    - Update `docs/mvp-scope.md` open question if unresolved, or close it if decided.
    - Add or update integration/manual verification notes for whichever pricing source is actually consumed at runtime.

- [ ] MVP-033 Freeze the chain/network discovery input contract across client, sidecar, and oracle.
  - Context:
    - The MVP requires oracle-backed provider discovery keyed by chain/network context, but the source of that context is still open.
    - Leaving this only as an assumption risks incompatible implementations across the real client path, sidecar API, and oracle API.
  - Assumptions:
    - `A1`
  - Done when:
    - The repo defines the canonical chain/network identifier shape used by the oracle query path.
    - It is explicit whether the real client must supply chain/network directly, whether the sidecar may derive it, and what fallback behavior is allowed when derivation is unavailable.
    - Validation and error behavior are documented for missing, invalid, or unsupported chain/network inputs.
    - MVP-005, MVP-007, and MVP-017 all point to the same contract.
  - Verify:
    - Update `docs/mvp-scope.md` open question if unresolved, or close/narrow it if decided.
    - Add contract-level tests or documented manual verification for valid, missing, and unsupported chain/network inputs.

- [ ] MVP-002 Freeze reconnect handshake semantics so provider can return fresh or latest-known resumable RAV during normal session init.
  - Context:
    - The current repo supports resume when the caller already has `existing_rav`, but the MVP requires provider-authoritative reconnect behavior in the handshake.
  - Assumptions:
    - `A1`
    - `A3`
  - Done when:
    - Consumer init has a documented reconnect story.
    - Provider can distinguish fresh handshake from reconnect/resume during the normal init flow.
    - Provider returns either a zero-value/fresh RAV or the latest resumable RAV according to the chosen semantics.
  - Verify:
    - Add an integration test that reconnects without relying solely on consumer-local in-memory session state.

- [ ] MVP-027 Freeze canonical payment identity, `collection_id` reuse, and session-vs-payment keying semantics.
  - Context:
    - Reconnect, durable provider state, inspection APIs, and manual collection all depend on a stable answer for what identity ties those records together.
    - Leaving this implicit risks implementing mutually incompatible storage and API shapes across provider, consumer, and tooling code.
  - Assumptions:
    - `A3`
  - Done when:
    - The repo documents the canonical payment identity used across runtime, persistence, and settlement flows.
    - The rules for `collection_id` reuse versus minting a new `collection_id` are explicit for fresh sessions, reconnects, and retryable collection flows.
    - It is clear which state is keyed by session identifier versus payment identity.
  - Verify:
    - Update `docs/mvp-scope.md` open questions if unresolved, or close/narrow them if decided.
    - Confirm MVP-002, MVP-003, MVP-008, MVP-013, MVP-019, and MVP-020 all reference the same identity semantics without contradiction.

- [ ] MVP-003 Define the durable provider-side payment and settlement data model.
  - Context:
    - Provider persistence is MVP-critical, but the canonical durable model still needs to support both runtime session state and settlement state.
  - Assumptions:
    - `A3`
  - Done when:
    - The provider-side durable record types are documented.
    - The model supports accepted RAV state, runtime session association, and collection lifecycle state.
    - The model is structured so the unresolved `collection_id` semantics do not force a rewrite later.
  - Verify:
    - Document the schema/record model in a repo plan or doc.
    - Confirm every persistence-related task below maps cleanly to the model.

- [ ] MVP-004 Define and document the byte-billing and payment/header contract used in the real runtime path.
  - Context:
    - The MVP now explicitly requires real provider and consumer integrations, so the runtime payment/header contract must be frozen enough for those paths.
  - Assumptions:
    - none
  - Done when:
    - The document explains how the real provider path receives/validates payment material.
    - Billable usage is defined as provider-authoritative streamed bytes.
    - Header/payment material, signature encoding, and session binding expectations are documented.
  - Verify:
    - Update the relevant docs and ensure implementation tasks that depend on the wire contract can point to a stable reference.

## Oracle Tasks

- [ ] MVP-005 Implement a standalone oracle service with manual whitelist and recommended-provider response.
  - Context:
    - The oracle is now a mandatory MVP component, even though the initial logic is intentionally simple.
  - Assumptions:
    - `A1`
    - `A2`
    - `A5`
  - Done when:
    - A standalone oracle component exists.
    - It can serve a manually curated provider set.
    - It returns eligible providers plus a recommended provider for a requested chain/network.
    - The oracle request/response contract is documented and stable enough for the consumer sidecar to integrate against without provider-specific assumptions.
    - Each provider record includes the minimum metadata required for MVP routing and connection setup, at least provider identity, endpoint/transport details, and chain/network eligibility.
    - Recommendation behavior is deterministic for the same request and whitelist state.
    - If pricing metadata is returned before pricing authority is fully frozen, the response documents that status clearly so the consumer does not treat advisory metadata as final authority by accident.
  - Verify:
    - Add tests for whitelist lookup and provider recommendation behavior.
    - Add API contract coverage for request validation and response shape.
    - Add a manual smoke flow that exercises oracle -> consumer sidecar -> provider selection.

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

- [ ] MVP-007 Integrate consumer sidecar with oracle discovery while preserving direct-provider fallback.
  - Context:
    - Consumer sidecar is the mandatory client-side integration point and must support oracle-driven default behavior.
  - Assumptions:
    - `A1`
    - `A2`
  - Done when:
    - Consumer sidecar can query the oracle and choose the recommended provider.
    - Direct provider configuration still works as a fallback.
    - The two flows share the same downstream session-init/payment behavior.
  - Verify:
    - Add tests or manual smoke steps for both oracle-backed and direct-provider flows.

- [ ] MVP-013 Implement provider-authoritative reconnect/resume in the normal handshake path.
  - Context:
    - Consumer persistence is intentionally not required for MVP, so the reconnect story must not depend entirely on consumer-local state.
  - Assumptions:
    - `A3`
  - Done when:
    - Consumer reconnect flow can recover via provider-authoritative handshake behavior.
    - The sidecar can handle the provider returning either fresh or resumable state.
  - Verify:
    - Add an integration scenario that disconnects and reconnects through the normal init flow and resumes against provider state.

## Provider State and Settlement Tasks

- [ ] MVP-008 Add durable provider storage for accepted RAV, session state, and collection lifecycle state.
  - Context:
    - Provider-side accepted payment state must survive restart for MVP.
  - Assumptions:
    - `A3`
    - `A6`
  - Done when:
    - Provider restart does not lose accepted collectible RAV state.
    - Collection lifecycle state persists across restart.
    - Runtime session state and settlement state are both recoverable enough for MVP behavior.
  - Verify:
    - Add a restart-focused integration or persistence test that validates accepted state survives process restart.

- [ ] MVP-009 Expose provider inspection and settlement-data retrieval APIs for accepted/collectible RAV state.
  - Context:
    - CLI inspection and manual collection require a provider-side way to retrieve settlement-relevant data.
  - Assumptions:
    - `A3`
  - Done when:
    - Provider exposes APIs for listing and fetching accepted/collectible payment state.
    - The returned data is sufficient for operator inspection and CLI-based collection.
    - The API shape is stable enough for MVP-019 and MVP-020 to build on it without provider-specific ad hoc reads.
  - Verify:
    - Add integration tests for listing and fetching settlement-relevant accepted state.

- [ ] MVP-029 Implement provider collection lifecycle transitions and update surfaces for `collectible`, `collect_pending`, `collected`, and retryable collection state.
  - Context:
    - The MVP requires provider-visible collection lifecycle state, but inspection APIs and CLI submission are not sufficient unless something owns the transitions between those states.
    - The provider needs a consistent way to track in-flight collection attempts, safe retries, and completed collection outcomes.
  - Assumptions:
    - `A3`
  - Done when:
    - Provider persistence supports the required collection lifecycle states and transition rules.
    - There is a defined provider-side update path for marking collection attempts pending, completed, or retryable after an on-chain submission outcome.
    - Retry behavior is documented so the CLI can interact with provider state idempotently.
  - Verify:
    - Add integration or persistence tests that cover `collectible` -> `collect_pending` -> `collected` and a retryable failure path.

## Funding Control and Runtime Payment Tasks

- [ ] MVP-010 Implement session-local low-funds detection and provider Continue/Pause/Stop decisions during streaming.
  - Context:
    - The MVP requires low-funds handling during active streaming, but only on a session-local basis.
  - Assumptions:
    - `A6`
  - Done when:
    - Provider can compare session-local exposure against available funding.
    - Provider emits the appropriate control/funding messages during active streams.
    - Low-funds behavior includes a structured operator-usable reason and enough funding state to explain why streaming was paused or stopped.
    - The low-funds signal is stable enough for operator tooling and client-side messaging to consume consistently.
  - Verify:
    - Add an integration test with intentionally low funding that reaches a stop/pause or low-funds condition during streaming.
    - Confirm the surfaced low-funds state includes a machine-readable reason and actionable funding context.

- [ ] MVP-011 Propagate provider stop/pause decisions through consumer sidecar into the real client path.
  - Context:
    - Low-funds logic is incomplete until the client path actually obeys it.
  - Assumptions:
    - `A6`
  - Done when:
    - Consumer sidecar converts provider control/funding messages into client-visible stop/pause behavior.
    - Real client integration honors those decisions.
  - Verify:
    - Add integration/manual verification showing the real client path stops or pauses when provider requires it.

- [ ] MVP-012 Add deterministic RAV issuance thresholds suitable for real runtime behavior.
  - Context:
    - The current "sign on every report" behavior is not a good real-runtime policy.
  - Assumptions:
    - none
  - Done when:
    - RAV issuance is controlled by explicit policy such as value/time/provider-request thresholds.
    - Threshold behavior is documented and tested.
  - Verify:
    - Add tests that show repeated usage does not force a signature on every report unless policy requires it.

## Real Provider and Consumer Integration Tasks

- [ ] MVP-014 Integrate provider gateway validation into the real provider streaming path.
  - Context:
    - MVP requires real provider-path integration, not just demo harness behavior.
  - Assumptions:
    - none
  - Done when:
    - The real provider path validates payment/session state through SDS integration before or during stream setup as required by the chosen runtime contract.
  - Verify:
    - Add a real-path integration test or manual verification against the production-like provider path.

- [ ] MVP-015 Wire real byte metering from the provider/plugin path into gateway payment state.
  - Context:
    - Billable usage for MVP is authoritative streamed bytes from provider-side metering.
  - Assumptions:
    - none
  - Done when:
    - Real provider-side byte metering feeds the payment state used for billing/RAV progression.
    - The runtime path does not rely on consumer-reported bytes as the billing source of truth.
  - Verify:
    - Add tests or manual instrumentation evidence showing the live provider path updates billing state from metered bytes.

- [ ] MVP-016 Enforce gateway Continue/Pause/Stop decisions in the live provider stream lifecycle.
  - Context:
    - Provider-side control logic is incomplete if the live provider stream does not obey it.
  - Assumptions:
    - `A6`
  - Done when:
    - The real provider path can enforce SDS control decisions during live streaming.
  - Verify:
    - Add manual or automated verification where the provider stops or pauses the live stream based on gateway control decisions.

- [ ] MVP-017 Integrate the real consumer/client path with consumer sidecar init, usage reporting, and session end.
  - Context:
    - The consumer sidecar is mandatory in the MVP architecture, but the real client path still needs to use it end to end.
    - This task covers lifecycle entry/exit integration; the long-lived payment-session control loop is tracked separately in MVP-031.
  - Assumptions:
    - `A1`
  - Done when:
    - The real client path uses consumer sidecar init before streaming.
    - It reports usage/end-of-session through the sidecar.
    - It participates in oracle-backed discovery or direct fallback according to configuration.
  - Verify:
    - Add a real-path integration or manual scenario covering init -> stream -> usage -> end-session.

- [ ] MVP-031 Wire the live PaymentSession and RAV-control loop into the real client/provider runtime path.
  - Context:
    - MVP requires payment state to keep advancing during real streaming, not only during local/demo harness flows.
    - The real integration is incomplete until provider-driven RAV requests and funding/control messages flow through the same production path used by the live stream.
  - Assumptions:
    - none
  - Done when:
    - The real client/provider integration keeps the long-lived SDS payment-session control loop active alongside the live stream.
    - Provider-driven RAV requests, acknowledgements, and funding/control messages flow through the production runtime path rather than only through demo wrappers.
    - Payment state advancement during streaming uses the same runtime path that real deployments will use.
  - Verify:
    - Add a real-path integration or documented manual verification showing stream start, at least one provider-driven payment/RAV update during live streaming, and synchronized session state until normal end or stop/pause.

## Operator Tooling Tasks

- [ ] MVP-018 Implement operator funding CLI flows for approve/deposit/top-up beyond local demo assumptions.
  - Context:
    - Funding is an MVP operator workflow, but current tooling is still demo-oriented.
  - Assumptions:
    - none
  - Done when:
    - CLI commands exist for approve/deposit/top-up in a provider-operator/payer-operator workflow.
    - The commands are not limited to local deterministic devenv assumptions.
    - The documented operator flow links funding actions to the low-funds or runtime inspection surfaces so an operator can move from a funding-related stop condition to topping up the correct escrow without ad hoc investigation.
  - Verify:
    - Add command-level tests where practical and document a manual funding flow that works against a non-demo configuration.

- [ ] MVP-019 Implement provider inspection CLI flows for collectible/accepted RAV data.
  - Context:
    - Operators need to inspect what can be collected before settlement.
  - Assumptions:
    - `A3`
    - `A5`
  - Done when:
    - CLI can retrieve and display accepted/collectible payment state from the provider.
    - It supports the collection lifecycle states needed for MVP operations.
  - Verify:
    - Add manual smoke coverage for inspecting accepted and `collect_pending` state.

- [ ] MVP-020 Implement manual collection CLI flow that crafts/signs/submits collect transactions locally.
  - Context:
    - Settlement keys should stay outside the provider sidecar.
  - Assumptions:
    - `A3`
  - Done when:
    - CLI fetches settlement-relevant data from provider.
    - CLI crafts and signs the collect transaction locally.
    - CLI can retry safely when collection is pending or needs to be re-attempted.
  - Verify:
    - Add a manual or automated integration scenario that retrieves collectible state and completes a collect transaction.

## Security, Runtime Compatibility, and Observability Tasks

- [ ] MVP-028 Define the MVP authentication and authorization contract for oracle and provider operator surfaces.
  - Context:
    - The MVP requires authenticated operator/admin actions, but the exact auth mechanism remains open.
    - Oracle and provider surfaces should not drift into incompatible auth behavior without an explicit contract.
  - Assumptions:
    - `A5`
  - Done when:
    - The repo documents the MVP authn/authz approach for oracle and provider operator/admin surfaces.
    - It is clear which endpoints/actions require operator privileges and which identities or credentials satisfy that requirement.
    - MVP-006 and MVP-022 can implement the same contract rather than inventing separate security behavior.
  - Verify:
    - Update `docs/mvp-scope.md` open question if unresolved, or close/narrow it if decided.
    - Confirm oracle and provider admin task definitions point to the same auth contract.

- [ ] MVP-021 Make TLS the default non-dev runtime posture for oracle, sidecar, and provider integration paths.
  - Context:
    - The MVP requires real transport security without forcing a perfect production-hardening story.
  - Assumptions:
    - `A5`
  - Done when:
    - Non-dev/runtime docs and defaults use TLS for oracle, consumer sidecar, and provider integration surfaces.
    - Plaintext behavior is clearly scoped to local/dev/demo usage.
    - Oracle administration and provider/operator traffic do not rely on plaintext-by-default behavior outside explicitly dev-scoped workflows.
  - Verify:
    - Add validation or smoke coverage for TLS-enabled startup and client connectivity across oracle and sidecar/provider paths.

- [ ] MVP-022 Add authentication and authorization to provider admin/operator APIs.
  - Context:
    - Provider-side operator actions must not rely on open or anonymous admin APIs.
    - This task is about protecting provider operator surfaces, not defining the inspection/retrieval API shape itself.
  - Assumptions:
    - `A5`
  - Done when:
    - Provider inspection and settlement-retrieval APIs require authentication and authorization according to the shared MVP contract.
    - The implementation rejects unauthenticated or unauthorized access to operator-only provider actions.
    - The authentication requirement is documented and enforced in tests where practical.
  - Verify:
    - Add tests for authenticated success and unauthenticated rejection.

## Runtime Compatibility Tasks

- [ ] MVP-030 Add runtime compatibility and preflight checks for real provider/plugin deployments.
  - Context:
    - The MVP definition requires a real provider deployment path, not only a local happy-path demo.
    - Reproducible real-path validation is weaker if the repo does not explicitly check the runtime compatibility assumptions required by the `sds://` provider/plugin integration path.
  - Assumptions:
    - none
  - Done when:
    - The repo identifies at least one named real-provider target environment for MVP acceptance and documents the required runtime compatibility constraints clearly enough for operators to validate before rollout.
    - The required runtime versions, plugin compatibility assumptions, and non-demo configuration prerequisites for that environment are documented.
    - Startup or preflight checks fail fast when the provider/plugin environment is incompatible with the required SDS runtime contract.
  - Verify:
    - Add a startup/preflight validation test or a documented manual verification flow that demonstrates clear failure modes for unsupported runtime combinations.
    - Document a reproducible preflight or smoke checklist for the named real-provider environment.

- [ ] MVP-023 Define the final MVP observability floor beyond structured logs and status tooling.
  - Context:
    - MVP requires operational visibility, but metrics/tracing depth is still open.
  - Assumptions:
    - `A4`
  - Done when:
    - The repo has a documented observability floor for MVP.
    - It is clear whether metrics endpoints are part of MVP or not.
  - Verify:
    - Update `docs/mvp-scope.md` and remove or narrow the open question if a decision is made.

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
    - Manual verification that operators can inspect and reason about low-funds, reconnect, and collection flows without code-level debugging.

- [ ] MVP-032 Expose operator runtime/session/payment inspection APIs and CLI/status flows.
  - Context:
    - The MVP scope requires operators to inspect session, payment, and collection state, not only settlement-ready collectible records.
    - Reconnect debugging, low-funds handling, and restart validation are weaker if operators must infer runtime state from raw logs or direct datastore access.
  - Assumptions:
    - `A3`
    - `A4`
    - `A5`
  - Done when:
    - The provider exposes authenticated runtime/status APIs for active or recent sessions, payment state, latest accepted/requested RAV context, and current low-funds/control state where applicable.
    - Operator-facing CLI or status tooling can retrieve and display that runtime state without direct backend/database access.
    - Low-funds inspection includes enough actionable information for an operator to understand whether additional escrow funding is required and why.
    - Operators can inspect enough runtime/session/payment detail to understand reconnect, low-funds, and post-restart behavior without relying solely on logs.
  - Verify:
    - Add manual or integration coverage for inspecting an active or recently interrupted session, a low-funds session, and persisted post-restart payment state.

## Validation and Documentation Tasks

- [ ] MVP-025 Add MVP acceptance coverage for the primary end-to-end scenarios in docs/tests/manual verification.
  - Context:
    - The MVP scope makes scenarios the primary definition of done.
  - Assumptions:
    - none
  - Done when:
    - The key scenarios from `docs/mvp-scope.md` are covered by tests, reproducible manual flows, or both.
    - The repo identifies which scenarios are validated locally versus against a named real-provider environment.
    - At least scenarios `A`, `B`, `C`, and `G` have a defined validation path against a real-provider environment rather than relying only on local demo coverage.
    - The repo clearly states how each acceptance scenario is validated.
  - Verify:
    - Update the scenario matrix or equivalent test/docs references for each acceptance scenario, including environment, validation method, and source of truth for the result.

- [ ] MVP-026 Refresh protocol/runtime docs so they match the MVP architecture and explicit open questions.
  - Context:
    - The phase 1 spec remains useful but no longer matches the MVP architecture in several important ways.
  - Assumptions:
    - `A1`
    - `A2`
    - `A3`
    - `A4`
    - `A5`
  - Done when:
    - The repo documentation reflects the MVP architecture rather than the older API-key-centric/control-plane assumptions.
    - Open questions are called out explicitly rather than being hidden in outdated text.
  - Verify:
    - Review the updated docs against `docs/mvp-scope.md` and confirm there are no major contradictions.

## Notes on Scope Boundaries

- This backlog intentionally does **not** make aggregate multi-stream payer-level liability tracking an MVP requirement.
- It also does **not** make wallet-based funding UI or automated collection an MVP requirement.
- If future work needs those features, it should be tracked separately as post-MVP scope unless the MVP definition changes again.
