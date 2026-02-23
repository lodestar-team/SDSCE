# Substreams Data Service — Implementation Backlog

_Last updated: 2026-02-23_

This repo already contains a working **Horizon V2 (TAP) signing/verification core** (`horizon/`) and a **development environment + integration tests** (`horizon/devenv/`, `test/integration/`).

The two sidecars (`consumer/sidecar/`, `provider/sidecar/`) are currently a **scaffold**: they start servers, manage in-memory sessions, and implement the RPC surfaces, but most of the “real protocol” glue (sidecar↔sidecar negotiation, dynamic authorization, funding enforcement, RAV request policy, etc.) is not wired up yet.

This document is meant to be a practical tracking/backlog list of what still needs to be implemented, with pointers to the current code.

See also: `docs/agent-workflow.md` for the step-by-step implementation/verification workflow.

## How To Use This Document

- Each item has:
  - **Context**: why it exists / where it comes from.
  - **Done when**: objective acceptance criteria.
  - **Verify**: concrete commands and expected outcomes to corroborate completion.
- Unless explicitly scoped otherwise, the baseline repo validation for any code change is:
  - `go test ./...`
  - `go vet ./...`
  - plus formatting (`gofmt`) for changed `.go` files.
- For changes affecting protobufs:
  - regenerate (`buf generate`) and ensure `go test ./...` passes.

## Verification Conventions

- **Integration tests** require Docker and will start Anvil/contracts via testcontainers (`test/integration/main_test.go`).
- “Verify” sections use `go test` and/or the existing CLI fake clients; when a task adds new behavior, it should also add or extend tests in `test/` unless impractical.
- For RPC behavior, use either:
  - integration tests (`test/integration/*`), or
  - manual smoke tests using the CLI (`cmd/sds/*`) against a local `sds devenv`.

---

## Current State (What Works Today)

- **EIP-712 / Horizon V2**
  - Domain separator + typed-data hashing implemented and tested (`horizon/eip712.go`, `horizon/eip712_test.go`).
  - Signing + signer recovery implemented and tested (`horizon/signed_message.go`, `horizon/signature.go`, `horizon/signature_test.go`).
  - Receipt aggregation into RAV implemented and tested (`horizon/aggregator.go`, `horizon/aggregator_test.go`).
  - Solidity compatibility tests exist (hashing + signature recovery) (`test/integration/rav_test.go`).
- **Deterministic local chain + contracts**
  - `sds devenv` boots Anvil in Docker and deploys contracts; prints addresses and test keys (`cmd/sds/devenv.go`, `horizon/devenv/devenv.go`).
  - On-chain flows for signer authorization and `collect()` are covered by integration tests (`test/integration/authorization_test.go`, `test/integration/collect_test.go`).
- **RPC surfaces exist**
  - Consumer sidecar exposes `ConsumerSidecarService` (`consumer/sidecar/sidecar.go`, `proto/.../consumer.proto`).
  - Provider sidecar exposes `ProviderSidecarService` and `PaymentGatewayService` (`provider/sidecar/sidecar.go`, `proto/.../provider.proto`, `proto/.../gateway.proto`).
  - Provider sidecar also exposes firehose-core-facing `sds://` plugin services (`AuthService`, `UsageService`, `SessionService`) (`provider/auth`, `provider/usage`, `provider/session`, `proto/.../sds/*`).

---

## Milestones (Suggested Order)

1. **Sidecar↔Sidecar handshake + shared session ID**
2. **Dynamic signer authorization (on-chain) + escrow enforcement**
3. **RAV request policy + streaming loop (Continue/Stop/Pause)**
4. **Production hardening (authn/z, persistence, metrics, rate limits, etc.)**

---

## Status Tracker

Status values:
- `not_started`
- `in_progress`
- `blocked`
- `done`
- `deferred`

Update process:
- Change **Status** here first.
- When a task is `done`, also tick its checkbox in the detailed sections.

| ID | Pri | Status | Task |
|---|---|---|---|
| SDS-001 | P0 | done | Fix consumer `ReportUsage` nil-usage crash |
| SDS-002 | P0 | done | Fix consumer inactive-session error construction |
| SDS-003 | P0 | done | Add required-field validation across handlers |
| SDS-004 | P0 | done | Fix README vs CLI flag drift (provider sidecar) |
| SDS-005 | P0 | done | Fix `devel/sds` version ldflags mismatch |
| SDS-006 | P0 | done | Validate address/signature byte lengths in conversions |
| SDS-007 | P1 | done | Add explicit `collection_id` to proto `common.v1.RAV` |
| SDS-008 | P1 | not_started | Define and document `metadata` schema + encoding |
| SDS-009 | P1 | done | Align pricing/service parameters across proto + impl |
| SDS-010 | P1 | done | Define canonical signature encoding for RPC/header |
| SDS-011 | P1 | done | Wire consumer `Init` → gateway `StartSession` |
| SDS-012 | P1 | done | Decide and implement shared session ID strategy |
| SDS-013 | P1 | done | Implement session resumption end-to-end |
| SDS-014 | P2 | done | Bind `PaymentSession` stream to a specific session |
| SDS-015 | P2 | done | Implement provider-driven RAV request policy |
| SDS-034 | P2 | done | Wire consumer sidecar to provider `PaymentSession` loop |
| SDS-035 | P2 | done | Enforce RAV submissions cover requested usage |
| SDS-036 | P2 | done | Propagate session close across sidecars |
| SDS-016 | P2 | not_started | Implement `NeedMoreFunds` loop + Continue/Stop/Pause |
| SDS-017 | P2 | done | Verify signer authorization on-chain (`isAuthorized`) |
| SDS-018 | P2 | done | Remove CLI/env allowlist override (rely on on-chain auth) |
| SDS-019 | P2 | done | Define cost computation trust boundary (`Usage.cost`) |
| SDS-020 | P2 | not_started | Add signing thresholds (don’t sign every report) |
| SDS-021 | P2 | not_started | Decide/implement on-chain collection workflow |
| SDS-022 | P2 | not_started | Track outstanding RAVs across concurrent streams |
| SDS-023 | P3 | not_started | Add session TTL/GC and explicit cleanup |
| SDS-024 | P3 | not_started | Add durable state storage (if required) |
| SDS-025 | P3 | not_started | Add transport security + authn/authz |
| SDS-026 | P3 | not_started | Add observability (metrics/tracing/log correlation) |
| SDS-027 | P3 | not_started | Add rate limiting / abuse protection |
| SDS-037 | P2 | done | Add CLI helper to prepare on-chain demo state (devenv) |
| SDS-031 | P3 | done | Add `sds demo flow` manual harness (optional) |
| SDS-032 | P3 | not_started | Explore `protovalidate` for request validation |
| SDS-033 | P3 | not_started | Reuse/caching for provider gateway clients |
| SDS-028 | X | not_started | Define payment header format (client ↔ provider) |
| SDS-029 | X | not_started | Integrate provider sidecar into tier1 provider |
| SDS-030 | X | not_started | Integrate consumer sidecar into substreams client |

## P0 — Correctness, Crashers, Repo Consistency

- [x] SDS-001 Fix `consumer/sidecar/handler_report_usage.go` nil deref when `req.Msg.Usage == nil`.
  - Today: `usage := req.Msg.Usage` is checked before `session.AddUsage(...)` but later `usage.Cost...` is used unconditionally.
  - Target: treat `Usage` as required (return `InvalidArgument`) or handle nil by no-op.
  - Done when:
    - `ReportUsage` returns `InvalidArgument` (or is a no-op) when `Usage` is missing.
    - No panics are possible from missing `Usage` or missing `Usage.cost`.
  - Verify:
    - Add a unit/integration test that calls `ConsumerSidecarService.ReportUsage` with `usage=nil` and asserts a non-OK RPC status.
    - Run `go test ./...` and confirm the new test covers the code path.
- [x] SDS-002 Fix incorrect error construction in `consumer/sidecar/handler_report_usage.go` when session is inactive.
  - Today: returns a `FailedPrecondition` wrapping another `FailedPrecondition` with nil cause.
  - Target: a single `connect.NewError(connect.CodeFailedPrecondition, errors.New("..."))`.
  - Done when:
    - The returned error has code `FailedPrecondition` and a stable, user-readable message.
  - Verify:
    - Add a test that ends a session then calls `ReportUsage` and asserts error code/message.
- [x] SDS-003 Add required-field validation across handlers (avoid panics on nil nested messages).
  - Consumer `Init`: `req.Msg.EscrowAccount` is assumed non-nil (`consumer/sidecar/handler_init.go`).
  - Provider `StartSession`: `req.Msg.EscrowAccount` is assumed non-nil (`provider/sidecar/handler_start_session.go`).
  - Target: return `InvalidArgument` with precise messages.
  - Done when:
    - Each handler returns `InvalidArgument` when required nested messages are missing.
  - Verify:
    - Add tests for each handler with missing nested messages.
    - `go test ./...` passes.
- [x] SDS-004 Fix README/CLI drift for provider sidecar flags.
  - README currently mentions `--accepted-signers`, but `cmd/sds/provider_sidecar.go` does not define it.
  - Decide: either remove that flag from README or implement it in CLI (as a temporary dev override) while on-chain auth is implemented.
  - Done when:
    - `README.md` examples match `sds provider sidecar --help`.
  - Verify:
    - Run `./devel/sds provider sidecar --help` and compare flags to README examples.
- [x] SDS-005 Fix `devel/sds` version ldflags mismatch.
  - Script sets `-X main.Version=...` but CLI uses `var version = "dev"` (`devel/sds`, `cmd/sds/main.go`).
  - Target: align names so `sds --version` reflects `.version` when present.
  - Done when:
    - Running `./devel/sds --version` prints the `.version` file value (when present).
  - Verify:
    - Create a temporary `.version` file (local dev only), run `./devel/sds --version`, confirm it matches.
- [x] SDS-006 Tighten proto conversions to validate byte sizes.
  - `pb/.../types_helpers.go` and `sidecar/convert.go` accept arbitrary-length `Address` and `Signature`.
  - Target: reject invalid lengths (address must be 20 bytes; signature must be 65 bytes) with contextual errors.
  - Done when:
    - All proto→native conversions validate sizes and return errors, not malformed values.
    - Sidecar handlers return `InvalidArgument` on invalid address/signature lengths.
  - Verify:
    - Add unit tests for helpers/converters with invalid lengths.
    - Add at least one integration test that sends invalid wire data and asserts `InvalidArgument`.

---

## P1 — Protocol/Data Model Alignment (Before Wiring “Real” Flows)

- [x] SDS-007 Add `collection_id` to the protobuf `common.v1.RAV`.
  - Today: `horizon.RAV` has `CollectionID`, but proto `RAV` does not; conversion tries to infer it from the first 32 bytes of `metadata` (`sidecar/convert.go`).
  - Target: explicit `bytes collection_id = ...` (32 bytes) and stop overloading `metadata`.
  - Follow-ups:
    - Update `sidecar/convert.go` and generated `pb/` via `buf generate`.
    - Update sidecar handlers to require it (or define derivation rules).
  - Done when:
    - `common.v1.RAV` includes `collection_id` (32 bytes) and conversions do not read collection ID from `metadata`.
    - All EIP-712 signing uses the explicit `collection_id`.
  - Verify:
    - Regenerate protos (`buf generate`) and ensure `go test ./...` passes.
    - Add a test asserting `collection_id` round-trips proto↔horizon without touching `metadata`.
- [ ] SDS-008 Decide and document a stable `metadata` schema.
  - Current state: sidecars sign RAVs with `metadata=nil` almost everywhere.
  - Target: define what goes in metadata (e.g., request CID, stream parameters, provider endpoint hash, etc.) and how it’s encoded (protobuf, JSON, ABI-encoding, etc.).
  - Done when:
    - `README.md` (or a doc under `docs/`) defines the schema, encoding, and versioning strategy.
  - Verify:
    - Add tests that encode/decode metadata and assert stable canonical bytes.
- [x] SDS-009 Align `ServiceParameters`/pricing across proto and implementation.
  - Provider sidecar supports `price_per_block` and `price_per_byte` via YAML (`sidecar/pricing.go`), but proto `ServiceParameters` only carries `price_per_block` (`proto/.../types.proto`).
  - Target: include both (and any additional required params like “price per request”, min prepaid, etc.).
  - Decision (2026-02-17): the **provider sidecar is authoritative** for pricing knobs (at least per-byte), and will compute costs from raw metering inputs; consumer-side verification against provider-reported usage/cost is a potential future hardening item.
  - Done when:
    - Proto carries all pricing inputs that the provider uses to compute cost.
    - Sidecars either compute costs server-side or explicitly validate caller-provided cost against pricing.
  - Verify:
    - Add tests that show consistent cost calculation for blocks+bytes across both sides.
- [x] SDS-010 Define the canonical **signature byte order** for the proto wire format.
  - Go uses `eth.Signature` (V+R+S); Solidity `ECDSA.recover` expects R+S+V (see `docs/contracts.md` and integration helpers).
  - Target: pick one canonical encoding for RPC/headers (recommended: keep V+R+S internally, convert only at Solidity boundary), and document it clearly.
  - Done when:
    - `proto` + docs clearly state the encoding and any required conversions.
  - Verify:
    - Add a test that takes a proto `SignedRAV`, recovers signer in Go, and also recovers in Solidity in the integration suite (if used on-chain).

---

## P1 — Sidecar↔Sidecar Session Negotiation (Core Missing Piece)

The flow diagram in `docs/flowchart.txt` implies:
`substreams -> consumer sidecar (Init)` then `consumer sidecar -> provider sidecar (StartSession)` *before* the client connects to the provider.

- [x] SDS-011 Implement `consumer/sidecar.Init` to call `PaymentGatewayService.StartSession`.
  - Today: comment explicitly says this is not done (`consumer/sidecar/handler_init.go`).
  - Target:
    - Use `provider_endpoint` from `InitRequest` to create a gateway client (`pb/.../providerv1connect/...`).
    - Create/send an initial RAV (possibly zero-value) + escrow account in `StartSessionRequest`.
    - Store the **provider-assigned session ID** and returned `use_rav`.
  - Done when:
    - Consumer `Init` performs a real `StartSession` call and returns `payment_rav` equal to `use_rav`.
    - Failures propagate as sensible RPC errors (`Unavailable`, `InvalidArgument`, etc.).
  - Verify:
    - Extend `test/integration/sidecar_test.go` to run consumer+provider sidecars and assert `Init` triggers `StartSession`.
    - Optionally add a manual smoke test: start both sidecars + run `sds consumer fake-client` and confirm provider logs show `StartSession called`.
- [x] SDS-012 Decide a **shared session ID story** across components.
  - Today: consumer and provider sidecars each generate their own UUID session IDs (`sidecar/session.go`), and there is no mapping.
  - Chosen strategy:
    - The **provider-assigned `StartSessionResponse.session_id` is canonical**.
    - Consumer sidecar uses that ID for its local session and returns it in `InitResponse.session.session_id`.
    - Provider sidecar reuses that same session when `ValidatePaymentRequest.client_session_id` is set.
  - Done when:
    - Consumer `Init` returns the provider gateway’s `session_id` when `provider_endpoint` is set.
    - Provider `ValidatePayment` reuses the `StartSession` session when `client_session_id` is provided.
  - Verify:
    - Run `go test ./test/integration -run TestPaymentFlowBasic` and assert:
      - provider session count remains `1` after `ValidatePayment`, and
      - `ValidatePaymentResponse.session_id == InitResponse.session.session_id`.
- [x] SDS-013 Implement session resumption end-to-end.
  - Protos hint at it (`InitRequest.existing_rav`, `ValidatePaymentRequest.client_session_id`) but sidecars don’t coordinate.
  - Target: if consumer has `existing_rav`, it should attempt to resume with provider (and provider should accept/reject consistently).
  - Done when:
    - Resuming with a valid `existing_rav` reuses the session/payment state as designed.
    - Resuming with an invalid `existing_rav` fails clearly.
  - Verify:
    - Integration test that:
      - creates a session + signs a RAV,
      - calls consumer `Init(existing_rav=...)`,
      - asserts the returned `payment_rav` matches the resumed state.

---

## P2 — PaymentSession Stream (Real-Time Negotiation)

- [x] SDS-014 Make `PaymentGatewayService.PaymentSession` usable for real sessions.
  - Today: stream handler ignores session identity and is effectively “stateless” (`provider/sidecar/handler_payment_session.go`).
  - Target:
    - Decide how the stream is bound to a session (proto field vs headers vs first message).
    - Update protos if needed (likely add `session_id`).
    - Persist per-session stream state and connect it to `SessionManager`.
  - Done when:
    - A consumer can open a stream bound to a specific session and the provider can enforce that binding.
  - Verify:
    - `go test ./test/integration -run TestPaymentSession_BindsToSessionID` passes.
- [x] SDS-015 Implement provider-driven RAV requests.
  - Proto has `RAVRequest` + `deadline` (`proto/.../gateway.proto`), but nothing triggers it today.
  - Target:
    - Provider sidecar requests a new RAV when usage/cost threshold is reached.
    - A consumer client responds with `SignedRAVSubmission` (consumer-sidecar wiring is tracked separately).
  - Done when:
    - Provider triggers `rav_request` based on a deterministic policy and handles responses.
  - Verify:
    - `go test ./test/integration -run TestPaymentSession_ProviderRequestsRAVOnUsage` passes.
- [x] SDS-034 Wire consumer sidecar to provider `PaymentSession` loop.
  - Context:
    - Provider sidecar implements `PaymentSession` and emits `rav_request`, but consumer sidecar does not open/manage the stream, so the sidecar↔sidecar negotiation loop is not demoable via `cmd/sds/*` today.
  - Target:
    - Maintain one `PaymentSession` stream per session (create at `Init` or lazily on first `ReportUsage`).
    - On `ConsumerSidecarService.ReportUsage`, forward usage (blocks/bytes/requests) to provider via `PaymentSessionRequest{usage_report}`.
    - Handle provider responses:
      - `rav_request` → sign an updated RAV and send `PaymentSessionRequest{rav_submission}` (including the provider-provided `usage`).
      - `session_control` / `need_more_funds` → propagate to `ReportUsageResponse.should_continue/stop_reason` and transition local session state (pause/stop) accordingly.
    - Close the stream on `ConsumerSidecarService.EndSession`.
  - Done when:
    - Consumer `ReportUsage` causes provider `GetSessionStatus.payment_status.current_rav_value` to advance.
  - Verify:
    - Add/extend an integration test that starts both sidecars, calls consumer `ReportUsage`, and asserts provider `current_rav_value` increases.
    - Manual: start `sds devenv`, start both sidecars, run `sds consumer fake-client`, and observe provider logs showing `rav_request` + `RAV accepted via stream`.
- [x] SDS-035 Enforce that RAV submissions cover the requested usage.
  - Context:
    - Provider currently accepts any submission where `new_value >= current_value`, which allows “no payment” updates and makes `rav_request` ineffective.
  - Target:
    - For both `PaymentSession` (`handleRAVSubmission`) and unary `SubmitRAV`, enforce:
      - `submitted_value >= current_value + expected_delta_cost`.
    - Compute `expected_delta_cost` server-side (from usage tracked since baseline + pricing config), not from caller-provided `Usage.cost`.
    - On violation, STOP/reject with a clear reason.
  - Done when:
    - Underpaying submissions are rejected/stopped and covered by tests.
  - Verify:
    - Extend `TestPaymentSession_ProviderRequestsRAVOnUsage` to submit an underpaying RAV and assert STOP/rejection.
- [x] SDS-036 Propagate “session close” across sidecars.
  - Context:
    - Consumer `EndSession` ends local state, but provider sessions can remain active even if the `PaymentSession` stream is closed.
  - Target:
    - Define minimal close behavior for demo:
      - Consumer closes `PaymentSession` on `EndSession`.
      - Provider marks the session ended when the stream closes (EOF) and/or when it emits `SessionControl{STOP}`.
    - Ensure final RAV is consistent on both sides.
  - Done when:
    - Ending the session on consumer results in provider `GetSessionStatus.active=false`.
  - Verify:
    - Integration test that runs `Init` + `ReportUsage` + `EndSession` and asserts provider becomes inactive.
- [ ] SDS-016 Implement “NeedMoreFunds” loop.
  - Flowchart calls out periodic escrow checks (`docs/flowchart.txt`).
  - Provider sidecar already has a low-level escrow query (`sidecar/escrow_querier.go`); integrate it into stream control messages.
  - Note: there is (or will be) an escrow/authorization subgraph that could be queried via GraphQL in the future; keep the current direct-RPC approach as the source of truth for now.
  - Done when:
    - Provider emits `need_more_funds` when escrow is insufficient and transitions the session (Continue/Stop/Pause) accordingly.
  - Verify:
    - Integration test where escrow is intentionally low; assert `need_more_funds` is emitted and that the provider instructs Stop/Pause depending on policy.

---

## P2 — Dynamic Authorization (Stop Using Static Accepted Signer Lists)

- [x] SDS-017 Verify authorized signer on-chain in `ValidatePayment` / `StartSession` / `SubmitRAV`.
  - Today: provider sidecar uses an in-memory allowlist (`provider/sidecar/sidecar.go`).
  - Target:
    - Call collector `isAuthorized(payer, signer)` (see how tests do it: `horizon/devenv/helpers.go` and `test/integration/authorization_test.go`).
    - Add caching with TTL to avoid RPC overload.
    - Note: there is (or will be) an escrow/authorization subgraph that could be queried via GraphQL in the future; keep the current direct-RPC approach as the source of truth for now.
    - Decide how “payer signs directly” is handled (implicitly authorized or not).
  - Done when:
    - Provider accepts RAVs signed by authorized signers and rejects unauthorized ones without relying on static config.
  - Verify:
    - Extend integration suite to run provider sidecar against devenv and validate both authorized and unauthorized signer cases.
- [x] SDS-018 Remove CLI/env allowlist override (rely on on-chain auth).
  - Context:
    - Now that on-chain signer authorization is implemented and covered by integration tests, we avoid shipping a CLI/env escape hatch that bypasses authorization checks.
  - Done when:
    - Provider sidecar CLI does not accept an allowlist override via env/flags (e.g. no `SDS_DEV_ACCEPTED_SIGNERS`).
    - Devenv/integration flows authorize signers on-chain instead.
  - Verify:
    - Integration tests that rely on on-chain `isAuthorized` pass (`go test ./test/integration -run TestProviderSidecar_OnChainAuthorization`).

---

## P2 — Usage Metering + Cost Calculation (Trust Boundaries)

- [x] SDS-019 Decide who computes `Usage.cost` and enforce it consistently.
  - Current scaffolding trusts `Usage.cost` provided by callers:
    - Consumer sidecar increments RAV by `usage.cost` (`consumer/sidecar/handler_report_usage.go`).
    - Provider sidecar tracks session totals from `usage.cost` (`provider/sidecar/handler_report_usage.go`).
  - Target:
    - Either compute cost server-side from raw usage using pricing config (preferred for provider sidecar), or
    - Define `Usage.cost` as consumer-authoritative and verify/compare it on provider side.
  - Decision (2026-02-17): treat **provider sidecar as cost-authoritative**. Provider sidecar computes `Usage.cost` from raw usage + pricing config, and rejects/overrides mismatches. Consumer-side “anti-overcharge” checks are deferred until pricing/usage semantics with tier1 are finalized.
  - Done when:
    - The trust boundary is documented and enforced in code (no silent mismatches).
  - Verify:
    - Add tests that attempt to submit incorrect `Usage.cost` and assert rejection or correction.
- [ ] SDS-020 Implement “signing thresholds” to avoid signing on every `ReportUsage`.
  - Target: sign only when (a) elapsed time, (b) delta-cost threshold, or (c) provider requests it.
  - Done when:
    - `ReportUsage` is cheap and does not sign on every call; signing happens under a deterministic policy.
  - Verify:
    - Add a benchmark or test that calls `ReportUsage` repeatedly and asserts the number of signatures created is below the number of calls.

---

## P2 — On-Chain Collection / Settlement Integration (Provider Operator Workflows)

- [ ] SDS-021 Decide what component triggers on-chain collection and when.
  - Integration tests call `collect()` directly (`test/integration/collect_test.go`), but provider sidecar does not.
  - Target options:
    - Provider sidecar exposes an operator/admin RPC to collect the latest RAV.
    - Separate “collector” daemon watches sessions and collects periodically.
    - Provider (tier1) calls collect itself using provider sidecar as a library.
  - Done when:
    - There is a documented, implemented operator workflow for collection (manual and/or automated).
  - Verify:
    - Integration test that runs the chosen workflow and asserts `tokensCollected` changes on-chain.
- [ ] SDS-022 Track “outstanding RAVs” per payer/collection and enforce escrow constraints across multiple concurrent streams.
  - Called out explicitly as a hard problem in `docs/flowchart.txt`.
  - Done when:
    - Provider can account for multiple simultaneous sessions and avoid over-serving beyond escrow.
  - Verify:
    - Integration test that runs two sessions against the same payer/escrow and asserts correct Stop/NeedMoreFunds behavior.

---

## P3 — Persistence, Security, and Ops Hardening

- [ ] SDS-023 Add session TTLs and cleanup (memory growth).
  - Today: sessions are never deleted from `SessionManager` (`sidecar/session.go`).
  - Target: TTL-based GC + explicit deletion on end, plus metrics.
  - Done when:
    - Ended/idle sessions are eventually removed and memory does not grow unbounded.
  - Verify:
    - Unit test using a fake clock or controllable TTL to assert sessions are removed.
- [ ] SDS-024 Add durable state storage (if required by protocol semantics).
  - If sidecars crash, resuming requires persisted sessions and last RAV.
  - Done when:
    - Restarting a sidecar can resume sessions according to the chosen semantics.
  - Verify:
    - Integration test that starts a sidecar, creates a session, restarts the sidecar, and verifies resumption.
- [ ] SDS-025 Add transport security and auth.
  - Today: plaintext ConnectRPC with permissive CORS.
  - Target: TLS/mTLS, authn between substreams client ↔ consumer sidecar and between sidecars.
  - Done when:
    - Sidecars can be configured to require TLS and authenticated peers.
  - Verify:
    - E2E test (or documented manual steps) showing unauthenticated calls are rejected.
- [ ] SDS-026 Add observability.
  - Metrics (Prometheus/OpenTelemetry), structured logs, traces, per-session correlation IDs.
  - Done when:
    - Key counters/histograms exist and can be scraped; logs include session IDs.
  - Verify:
    - Manual: start sidecars and confirm metrics endpoint exports expected series.
- [ ] SDS-027 Add rate limiting and abuse protection (especially on gateway endpoints).
  - Done when:
    - Basic per-client and/or per-session limits exist and are configurable.
  - Verify:
    - Test that floods gateway calls and asserts throttling.

---

## P3 — Dev Tooling (Demo Prereqs)

- [x] SDS-037 Add a CLI helper to prepare on-chain demo state for devenv.
  - Context:
    - Integration tests call `Env.SetupTestWithSigner(...)` (mint/approve/deposit escrow, register service provider, authorize signer), but there is no CLI workflow for humans to do it when running sidecars manually.
  - Target:
    - Add a command (e.g. `sds devenv setup-test-state` or `sds demo setup`) that:
      - connects to a running devenv RPC endpoint,
      - funds payer escrow with a configurable amount,
      - registers the service provider,
      - authorizes a signer key (generate + print it, or accept `--signer-private-key`),
      - prints the exact flags/env needed to start consumer/provider sidecars for a demo run.
  - Done when:
    - A user can run `sds devenv`, run the setup helper, then start both sidecars and observe on-chain `isAuthorized(payer, signer)=true`.
  - Verify:
    - Manual: run `sds devenv`, then the setup helper, then `go test ./test/integration -run TestProviderSidecar_OnChainAuthorization`.
- [x] SDS-031 Add `sds demo flow` manual harness (optional).
  - Context:
    - We currently have good coverage in `test/integration/sidecar_test.go`, but it’s not a friendly “demo” entrypoint when iterating.
    - A CLI subcommand (or small binary under `examples/`) that runs the same flow makes it easy to manually validate behavior while implementing production wiring.
  - Target:
    - Provide a single command that:
      - calls consumer `Init`,
      - calls provider `ValidatePayment`,
      - opens `PaymentSession`, exercises `rav_request`/`rav_submission`, and
      - sends usage updates (blocks/bytes),
      - ends the session, printing key IDs and RAV values.
    - Prefer to reuse the already-running `sds devenv` and the running sidecars, rather than spinning up containers itself.
  - Done when:
    - `./devel/sds demo flow ...` (or an `examples/` program) can be run by a human to demonstrate the end-to-end RPC flow with clear output.
  - Verify:
    - Manual: start `./devel/sds devenv`, start both sidecars, run the demo harness, and confirm it exercises both sidecars and prints session IDs + final RAV value.
    - Automated (preferred): add a lightweight integration test asserting the harness completes successfully.

---

## P3 — Future Improvements (Non-Blocking)

- [ ] SDS-032 Explore `protovalidate` for request validation.
  - Context:
    - Today we do explicit nil/required-field validation in handlers (good error messages, but repetitive).
  - Target:
    - Evaluate adding `protovalidate` rules to protos and a validation layer that maps violations to consistent ConnectRPC `InvalidArgument` errors.
    - Decide validation boundaries (proto validation vs `.ToEth()` conversions vs handler checks) and avoid duplicative work where possible.
  - Done when:
    - A minimal proof-of-concept validates at least one RPC request type end-to-end with clear error messages.
    - We have a documented approach (and follow-up tasks) for adopting it incrementally or rejecting it.

- [ ] SDS-033 Reuse/caching for provider gateway clients.
  - Context:
    - Consumer sidecar currently constructs a provider gateway client per `Init` call.
  - Target:
    - Add a small client cache keyed by provider endpoint if/when it improves performance or enables per-provider configuration (TLS/auth).
  - Done when:
    - There is a clear design decision and, if implemented, tests cover the caching behavior and it does not change semantics.

---

## Cross-Repo Integration Tasks (Will Require Coordination)

These can’t be completed solely in this repo, but should be tracked here because they drive protocol decisions:

- [ ] SDS-028 Define and implement the **payment header** format used by substreams client ↔ provider (RAV serialization, signature encoding, session ID).
  - Current direction (tier1 + firehose-core plugins): use `x-sds-rav` containing a protobuf `common.v1.SignedRAV` (raw bytes in gRPC; base64 for manual/operator tooling like `sds tools rav create`).
- [ ] SDS-029 Integrate provider sidecar into the actual provider service (tier1):
  - Option A (legacy RPC): call `ValidatePayment` on connect and `ReportUsage` during streaming.
  - Option B (firehose-core plugins): use `sds://` dauth/dmetering/dsession plugins (this repo contains both the plugin clients and the provider sidecar endpoints).
  - Act on Continue/Stop decisions from sidecar (and ensure metering feeds the RAV request loop).
- [ ] SDS-030 Integrate consumer sidecar into the actual substreams client:
  - Call `Init` before connecting to provider.
  - Call `ReportUsage` / `EndSession`.
  - Handle provider negotiation responses (RAV updates, funding requests) and keep `x-sds-rav` up-to-date during long streams.
