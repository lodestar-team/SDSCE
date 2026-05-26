# Current Implementation Review

Drafted: 2026-04-07

This document tracks the current code-review findings before further MVP implementation work continues.

It is intended to be iterated on:

- confirm or narrow each issue
- decide remediation shape
- track whether the fix is code, docs, tests, or backlog alignment

## Review Scope

The review focused on:

- cohesion across the current provider, consumer, plugin, and repository code
- Go best practices
- repo-specific guidance in `AGENTS.md`
- the reviewer-driven rules around synchronization boundaries, hidden timeout/retry policy, secure defaults, and explicit runtime contracts

## Clarified Assumptions

These are confirmed for this review and should be treated as the working contract unless explicitly changed later.

- Concurrent streams for the same payer are an expected normal use case and should not be intentionally blocked or disabled.
- The MVP concurrent-stream limitation is narrower:
  - the system does not yet account correctly for aggregate outstanding liability across multiple provider instances for the same payer
  - this is mainly a correctness limitation for low-funds / aggregate exposure decisions when concurrent streams are split across instances
  - it is not a justification for rejecting or tearing down concurrent streams themselves
- The private Plugin Gateway should not be assumed to be intentionally plaintext long-term; current plaintext-only behavior should be treated as a problem unless an explicit MVP exception is documented.

## Validation Baseline

Validated on the current tree:

- `go test ./...`
- `go vet ./...`
- `go test -race ./provider/repository ./provider/session ./provider/plugin ./consumer/sidecar`

All passed while the findings below were still present.

## Status Values

- `open`: validated issue, remediation not yet decided
- `in_progress`: remediation path chosen and being worked
- `resolved`: fixed and validated
- `deferred`: accepted for later work with explicit reason

## Findings

### R-001 Force-terminating same-payer sessions conflicts with the MVP contract

- Status: `open`
- Severity: `high`
- Area: provider gateway / MVP contract
- Evidence:
  - [provider/gateway/handler_start_session.go:122](/home/juan/GraphOps/substreams/data-service/provider/gateway/handler_start_session.go#L122)
  - [docs/mvp-scope.md:81](/home/juan/GraphOps/substreams/data-service/docs/mvp-scope.md#L81)
  - [docs/mvp-scope.md:182](/home/juan/GraphOps/substreams/data-service/docs/mvp-scope.md#L182)
- Summary:
  - `StartSession` currently terminates every other active session for the payer before creating a new one.
  - That is not just a documented limitation; it is active blocking/teardown behavior.
  - This conflicts with the intended MVP scope: concurrent streams themselves are a normal use case, while the documented limitation is only about cross-instance aggregate liability / low-funds correctness.
- Likely remediation direction:
  - remove the same-payer active-session teardown path
  - add coverage proving a second session does not kill the first one
  - update docs only if behavior intentionally changes again

### R-002 Metering emitter shutdown is unsafe

- Status: `open`
- Severity: `high`
- Area: plugin metering / lifecycle / concurrency
- Evidence:
  - [provider/plugin/metering.go:105](/home/juan/GraphOps/substreams/data-service/provider/plugin/metering.go#L105)
  - [provider/plugin/metering.go:133](/home/juan/GraphOps/substreams/data-service/provider/plugin/metering.go#L133)
  - [provider/plugin/metering.go:155](/home/juan/GraphOps/substreams/data-service/provider/plugin/metering.go#L155)
  - [provider/plugin/metering.go:193](/home/juan/GraphOps/substreams/data-service/provider/plugin/metering.go#L193)
- Summary:
  - shutdown can wedge because the launch loop may block indefinitely in `Report(context.Background(), ...)`
  - shutdown can also panic because `flushAndClose()` closes `e.buffer` while `Emit()` can still send to it
- Why it matters:
  - this is both a correctness risk and a direct mismatch with the reviewer-driven rule that control-plane timeout/lifecycle policy should be explicit
- Likely remediation direction:
  - define an explicit metering-emitter shutdown model:
    - how new events are rejected during teardown
    - how in-flight buffered events are drained
    - how long report RPCs are allowed to block
  - remove the send-on-closed-channel race
  - scope report RPCs to a bounded context or explicit shutdown policy
  - add focused tests for termination behavior

### R-003 Worker quota enforcement is not atomic

- Status: `open`
- Severity: `high`
- Area: provider session / quotas / concurrency
- Evidence:
  - [provider/session/service.go:77](/home/juan/GraphOps/substreams/data-service/provider/session/service.go#L77)
  - [provider/session/service.go:100](/home/juan/GraphOps/substreams/data-service/provider/session/service.go#L100)
  - [provider/session/service.go:114](/home/juan/GraphOps/substreams/data-service/provider/session/service.go#L114)
- Summary:
  - quota enforcement uses read, compare, create, then increment
  - concurrent borrows can pass the check simultaneously and oversubscribe the configured worker limit
- Likely remediation direction:
  - move to an atomic reservation/update path in the repository boundary
  - align in-memory and PostgreSQL semantics
  - add concurrency coverage that proves oversubscription is blocked

### R-004 Plugin Gateway transport is plaintext-only

- Status: `open`
- Severity: `high`
- Area: provider plugin gateway / transport policy
- Evidence:
  - [provider/plugin/gateway.go:74](/home/juan/GraphOps/substreams/data-service/provider/plugin/gateway.go#L74)
  - [cmd/sds/impl/provider_gateway.go:82](/home/juan/GraphOps/substreams/data-service/cmd/sds/impl/provider_gateway.go#L82)
  - [cmd/sds/impl/provider_gateway.go:195](/home/juan/GraphOps/substreams/data-service/cmd/sds/impl/provider_gateway.go#L195)
- Summary:
  - the private Plugin Gateway is hardwired to plaintext
  - the provider CLI transport flags only secure the public payment gateway
  - operators could reasonably believe TLS flags cover both surfaces when they do not
- Likely remediation direction:
  - add an explicit transport configuration for the Plugin Gateway
  - keep plaintext as an explicit local/dev choice only if needed
  - update docs and examples to match the actual transport split

### R-005 Consumer network discovery does not implement `Package.Networks` precedence

- Status: `open`
- Severity: `medium`
- Area: consumer discovery / MVP contract
- Evidence:
  - [consumer/sidecar/discovery.go:66](/home/juan/GraphOps/substreams/data-service/consumer/sidecar/discovery.go#L66)
  - [docs/mvp-scope.md:109](/home/juan/GraphOps/substreams/data-service/docs/mvp-scope.md#L109)
  - [consumer/sidecar/discovery_test.go:12](/home/juan/GraphOps/substreams/data-service/consumer/sidecar/discovery_test.go#L12)
- Summary:
  - the code only reads top-level `pkg.GetNetwork()`
  - the documented contract says a resolved package or module `networks` entry takes precedence
  - current tests only cover the narrower implementation
- Likely remediation direction:
  - implement precedence according to the frozen contract
  - add tests covering top-level network, resolved network entry, and conflict behavior

### R-006 In-memory repository does not satisfy its advertised concurrency contract

- Status: `open`
- Severity: `medium`
- Area: repository / concurrency / runtime behavior
- Evidence:
  - [provider/repository/repository.go:19](/home/juan/GraphOps/substreams/data-service/provider/repository/repository.go#L19)
  - [provider/repository/inmemory.go:61](/home/juan/GraphOps/substreams/data-service/provider/repository/inmemory.go#L61)
  - [provider/repository/inmemory.go:83](/home/juan/GraphOps/substreams/data-service/provider/repository/inmemory.go#L83)
  - [provider/repository/inmemory.go:198](/home/juan/GraphOps/substreams/data-service/provider/repository/inmemory.go#L198)
  - [cmd/sds/impl/provider_gateway.go:85](/home/juan/GraphOps/substreams/data-service/cmd/sds/impl/provider_gateway.go#L85)
  - [provider/gateway/gateway.go:112](/home/juan/GraphOps/substreams/data-service/provider/gateway/gateway.go#L112)
- Summary:
  - the interface says implementations must be safe for concurrent use
  - the in-memory backend returns shared pointers and mutates shared objects directly
  - `inmemory://` remains the default, and `gateway.New(...)` also silently falls back to an in-memory repository when none is provided
  - this is therefore not just a test helper concern, and it also weakens the fail-fast configuration posture for runtime construction
- Likely remediation direction:
  - choose a real concurrency model for the in-memory backend
  - either return copies and update atomically, or add internal synchronization around mutable entities
  - make backend semantics match the repository contract more closely
  - remove or narrow silent in-memory fallback in runtime construction paths

### R-007 Backward-compatibility auth path fabricates unusable session IDs

- Status: `open`
- Severity: `medium`
- Area: provider auth / session contract
- Evidence:
  - [provider/auth/service.go:136](/home/juan/GraphOps/substreams/data-service/provider/auth/service.go#L136)
  - [provider/session/service.go:61](/home/juan/GraphOps/substreams/data-service/provider/session/service.go#L61)
- Summary:
  - auth generates a fresh session ID when `x-sds-session-id` is absent
  - session borrow then requires that ID to reference a provider-created session
  - the result is a delayed failure path rather than an honest early rejection
- Likely remediation direction:
  - reject missing session IDs early, or
  - implement a real compatibility path instead of fabricating an ID

### R-008 Consumer PaymentSession client performs blocking stream sends under lock

- Status: `open`
- Severity: `medium`
- Area: consumer payment-session client / synchronization boundary
- Evidence:
  - [consumer/sidecar/payment_session_manager.go:114](/home/juan/GraphOps/substreams/data-service/consumer/sidecar/payment_session_manager.go#L114)
  - [consumer/sidecar/payment_session_manager.go:142](/home/juan/GraphOps/substreams/data-service/consumer/sidecar/payment_session_manager.go#L142)
- Summary:
  - `stream.Send(...)` happens while holding `paymentSessionClient.mu`
  - that violates the repo guidance against blocking network I/O inside synchronization boundaries
  - slow or wedged providers can block close, rebind, and recovery paths
- Likely remediation direction:
  - narrow the lock scope to state checks and stream acquisition
  - perform the actual send outside the mutex while preserving session-binding guarantees
  - re-evaluate this together with the broader stream-ownership fixes in `R-013` and `R-014` rather than treating it as a purely local lock change

### R-009 Operator-facing docs drift from the implemented runtime shape

- Status: `open`
- Severity: `medium`
- Area: docs / operator workflow
- Evidence:
  - [README.md:209](/home/juan/GraphOps/substreams/data-service/README.md#L209)
  - [provider/plugin/register.go:8](/home/juan/GraphOps/substreams/data-service/provider/plugin/register.go#L8)
  - [provider/gateway/REPOSITORY.md:24](/home/juan/GraphOps/substreams/data-service/provider/gateway/REPOSITORY.md#L24)
  - [provider/gateway/REPOSITORY.md:170](/home/juan/GraphOps/substreams/data-service/provider/gateway/REPOSITORY.md#L170)
  - [cmd/sds/demo_setup.go:76](/home/juan/GraphOps/substreams/data-service/cmd/sds/demo_setup.go#L76)
  - [cmd/sds/demo_setup.go:84](/home/juan/GraphOps/substreams/data-service/cmd/sds/demo_setup.go#L84)
  - [cmd/sds/demo_setup.go:90](/home/juan/GraphOps/substreams/data-service/cmd/sds/demo_setup.go#L90)
  - [cmd/sds/consumer_sidecar.go:125](/home/juan/GraphOps/substreams/data-service/cmd/sds/consumer_sidecar.go#L125)
  - [sidecar/server_transport.go:23](/home/juan/GraphOps/substreams/data-service/sidecar/server_transport.go#L23)
- Summary:
  - docs still point plugin clients at `:9001` instead of the private plugin gateway on `:9003`
  - repository examples omit the now-required `--data-plane-endpoint`
  - `sds demo setup` also prints local start commands that no longer satisfy the actual runtime requirements:
    - the printed provider gateway command omits `--plaintext` for the local demo posture
    - the printed consumer sidecar command omits the ingress runtime configuration needed for the immediately-following Substreams ingress example to work
  - the problem is therefore broader than markdown drift: code-generated operator guidance is also stale
- Likely remediation direction:
  - refresh operator docs once transport/runtime decisions above are finalized
  - make `sds demo setup` emit commands that are valid for the current local/demo flow, or stop emitting step-by-step commands if they cannot be kept authoritative

### R-010 Session plugin release path is not idempotent

- Status: `open`
- Severity: `medium`
- Area: plugin session lifecycle / ownership
- Evidence:
  - [provider/plugin/session.go:169](/home/juan/GraphOps/substreams/data-service/provider/plugin/session.go#L169)
  - [provider/plugin/session.go:183](/home/juan/GraphOps/substreams/data-service/provider/plugin/session.go#L183)
  - [provider/plugin/session.go:186](/home/juan/GraphOps/substreams/data-service/provider/plugin/session.go#L186)
- Summary:
  - `Release(sessionKey)` performs teardown asynchronously in a goroutine.
  - Two concurrent `Release` calls can both read the same session entry before either goroutine deletes it.
  - Both goroutines can then attempt to close the same `done` channel, which can panic.
- Why it matters:
  - this is a concrete lifecycle-ownership bug in a path that is supposed to be best-effort cleanup
- Likely remediation direction:
  - make release idempotent at the point of call, not only inside the async goroutine
  - ensure session removal and close signaling happen under one ownership boundary
  - treat this as teardown hygiene adjacent to `R-013`, but keep it separate from the keep-alive ownership bug itself

### R-011 Runtime payment-control dispatch can backpressure metering ingestion

- Status: `open`
- Severity: `medium`
- Area: provider runtime control / stream ownership
- Evidence:
  - [provider/usage/service.go:95](/home/juan/GraphOps/substreams/data-service/provider/usage/service.go#L95)
  - [provider/gateway/handler_payment_session.go:110](/home/juan/GraphOps/substreams/data-service/provider/gateway/handler_payment_session.go#L110)
  - [provider/gateway/runtime_manager.go:192](/home/juan/GraphOps/substreams/data-service/provider/gateway/runtime_manager.go#L192)
- Summary:
  - metering ingestion calls `OnMeteredUsage(...)` inline on the usage-report path
  - runtime dispatch then performs a blocking send into the per-session event channel
  - if the live `PaymentSession` stream is slow or wedged, usage ingestion can block behind control-message delivery
- Why it matters:
  - this is another stream-ownership/coherency issue: control-plane delivery can stall the path that records metered runtime usage
- Likely remediation direction:
  - decouple control-message dispatch from usage ingestion, or
  - make dispatch explicitly non-blocking with a documented overflow/backpressure policy
  - treat this as a delivery/backpressure policy issue distinct from the runtime-session cleanup issue in `R-014`

### R-012 `sds demo setup` prints start commands that fail as written

- Status: `open`
- Severity: `low`
- Area: demo/dev orchestration / operator UX
- Evidence:
  - [cmd/sds/demo_setup.go:76](/home/juan/GraphOps/substreams/data-service/cmd/sds/demo_setup.go#L76)
  - [cmd/sds/demo_setup.go:84](/home/juan/GraphOps/substreams/data-service/cmd/sds/demo_setup.go#L84)
  - [cmd/sds/consumer_sidecar.go:117](/home/juan/GraphOps/substreams/data-service/cmd/sds/consumer_sidecar.go#L117)
  - [cmd/sds/impl/provider_gateway.go:195](/home/juan/GraphOps/substreams/data-service/cmd/sds/impl/provider_gateway.go#L195)
- Summary:
  - `sds demo setup` prints sample provider and consumer start commands without either `--plaintext` or TLS certificate/key flags
  - both CLIs now require explicit transport configuration
  - the printed commands therefore fail for the local/demo flow they are supposed to help bootstrap
- Why it matters:
  - this is a concrete example of dev/demo guidance drifting from the actual transport contract
- Likely remediation direction:
  - update the printed demo commands to include explicit local/demo transport flags
  - keep the examples aligned with the current secure-default posture

### R-013 Session-plugin keep-alives are tied to the borrow request context

- Status: `open`
- Severity: `medium`
- Area: plugin session lifecycle / ownership
- Evidence:
  - [provider/plugin/session.go:97](/home/juan/GraphOps/substreams/data-service/provider/plugin/session.go#L97)
  - [provider/plugin/session.go:160](/home/juan/GraphOps/substreams/data-service/provider/plugin/session.go#L160)
  - [provider/plugin/session.go:321](/home/juan/GraphOps/substreams/data-service/provider/plugin/session.go#L321)
- Summary:
  - the keep-alive loop for a borrowed session is started with the `Get(...)` request context
  - the loop exits immediately on `ctx.Done()`
  - if that borrow context is request-scoped, session heartbeats stop even though the borrowed session is still logically in use
- Why it matters:
  - this is a resource-ownership mismatch: session-lifetime maintenance is coupled to the initial borrow call instead of the borrowed session lifecycle
- Likely remediation direction:
  - scope keep-alive ownership to the borrowed session, not the borrow request
  - make the intended cancellation boundary explicit and test it
  - fix this together with other plugin-session lifecycle ownership work, but keep it distinct from the release idempotence issue in `R-010`

### R-014 Runtime session-state cleanup is incomplete on bind/error paths

- Status: `open`
- Severity: `medium`
- Area: provider runtime manager / lifecycle cleanup
- Evidence:
  - [provider/gateway/runtime_manager.go:47](/home/juan/GraphOps/substreams/data-service/provider/gateway/runtime_manager.go#L47)
  - [provider/gateway/runtime_manager.go:59](/home/juan/GraphOps/substreams/data-service/provider/gateway/runtime_manager.go#L59)
  - [provider/gateway/runtime_manager.go:218](/home/juan/GraphOps/substreams/data-service/provider/gateway/runtime_manager.go#L218)
  - [provider/gateway/handler_payment_session.go:111](/home/juan/GraphOps/substreams/data-service/provider/gateway/handler_payment_session.go#L111)
  - [provider/gateway/handler_payment_session.go:116](/home/juan/GraphOps/substreams/data-service/provider/gateway/handler_payment_session.go#L116)
- Summary:
  - `bindSession(...)` stores the event channel before the initial runtime evaluation completes
  - if bind-time evaluation fails, `PaymentSession(...)` returns before assigning `sessionID`, so its deferred unbind does not run
  - even on successful unbind, the runtime map entry is retained and only `events` is cleared, so per-session state can accumulate indefinitely
- Why it matters:
  - bind failures can strand sessions as already-bound
  - long-running providers can accumulate runtime session state without a clear cleanup point
- Likely remediation direction:
  - make bind/unbind atomic from the handler’s point of view
  - define when runtime session state is deleted instead of partially nulled
  - add error-path coverage so failed binds do not wedge future session attachment

## Secondary Follow-Ups

These came up during review but were not promoted into the main list yet.

- Reviewer guidance follow-up:
  - maoueh suggested thinking about the concurrent hash map library instead of defaulting to `Mutex` / `RWMutex` for registry-style state
  - this should be evaluated as a design improvement for keyed registries such as session/runtime maps
  - it should not be treated as a blanket replacement for `Mutex` / `RWMutex`, because concurrent maps do not solve value-level synchronization, stream ownership, or multi-field invariants by themselves
  - if we converge on a precise rule later, that can be promoted into `AGENTS.md`; for now it remains review guidance captured here
- Hidden timing policy remains in a few places, for example:
  - [consumer/sidecar/ingress.go:79](/home/juan/GraphOps/substreams/data-service/consumer/sidecar/ingress.go#L79)
  - [provider/gateway/runtime_manager.go:152](/home/juan/GraphOps/substreams/data-service/provider/gateway/runtime_manager.go#L152)
  - [provider/plugin/session.go:357](/home/juan/GraphOps/substreams/data-service/provider/plugin/session.go#L357)
- There is a minor guideline mismatch in [cmd/sds/tools_rav.go:283](/home/juan/GraphOps/substreams/data-service/cmd/sds/tools_rav.go#L283), which still uses `sds.NewGRTFromBigInt(...)` in a CLI path.

## Lessons From Historical Review Feedback

The older @maoueh review rounds in `data-service-old` are still useful as style and design guidance, even though they targeted earlier code.

### Already Captured In `AGENTS.md`

These lessons are already reflected in the current repo guidance and should be treated as established expectations, not open questions.

- use project domain types such as `sds.GRT` instead of adding local parsing/formatting helpers
- keep `*big.Int` usage at explicit boundaries only
- do not lock another struct's mutex from outside the owning type
- keep synchronization boundaries at public methods; `*Locked` helpers are internal-only patterns
- prefer a dedicated owner/manager for bidi streams over goroutine-per-operation wrappers
- treat timeout/retry behavior as explicit policy, not as hidden constants in handlers
- prefer fail-fast demo/dev configuration over silent fallback defaults
- keep insecure or plaintext transport as an explicit opt-in, not an implicit runtime default
- move shared ABI/artifact access out of development-only packages when runtime code needs it
- add a short comment for non-obvious transport setup such as h2c/plaintext HTTP/2 client configuration

### Still Worth Watching For Drift

These are not missing guidelines so much as areas where the codebase can drift away from agreed guidance if we stop checking.

- stream ownership gradually leaking back across type boundaries
- `context.Background()` creeping into long-lived control-plane RPC paths
- local/demo transport assumptions becoming permanent runtime defaults
- domain-specific helpers being bypassed by ad hoc parsing or naked primitive usage
- operator/docs examples lagging behind the actual runtime shape
- convenience defaults reappearing in orchestration or bootstrap code where fail-fast behavior is safer

### Historical Feedback That Should Not Become A Blanket Rule

Some historical comments are useful as prompts, but should not be promoted into project-wide rules without narrower agreement.

- “Prefer `RWMutex`” is too broad on its own; the correct primitive depends on ownership, invariants, and access patterns.
- “Use the concurrent hash map library instead of coding with mutex” is best treated as a design option for registry-style keyed state, not as a general replacement for `Mutex` / `RWMutex`.
- Per-call client reuse/caching is situational and should be justified by ownership or configuration needs, not adopted as a reflex.

### Implication For This Review

The main takeaway from the historical feedback is not that the repo needs many more rules.

It is that we should remain vigilant about enforcing the rules we already agreed on, especially in:

- stream/control-plane lifecycle code
- synchronization boundaries
- transport security posture
- demo/dev orchestration
- operator-facing documentation

## Initial Remediation Priority

This is a first-pass classification based on the current code, the frozen MVP contract, and the clarified assumptions recorded above.

Priority meanings:

- `blocker`: should be addressed before more MVP backlog work continues in the affected runtime/control-plane areas
- `should_fix_soon`: does not need to stop all work immediately, but should be scheduled before adjacent work expands the drift
- `can_batch_later`: real issue, but safe to batch behind the more structural correctness items

### Blocker

- `R-001`: current provider behavior contradicts the agreed MVP contract for concurrent streams.
- `R-002`: metering shutdown can both wedge and panic in a core runtime path.
- `R-003`: worker quota enforcement is not authoritative under concurrency.
- `R-004`: plugin-gateway transport posture is inconsistent with the intended secure-default direction.
- `R-013`: session keep-alives are tied to the borrow request context instead of session lifetime, which can silently stop heartbeats for live sessions.
- `R-014`: runtime bind/error cleanup can strand sessions as already-bound and leak per-session runtime state.

### Why These Are Blockers

- `R-001` is a direct contract violation, not just a quality issue.
- `R-002`, `R-013`, and `R-014` are lifecycle/ownership bugs in core runtime paths that can break live behavior in ways that are hard to reason about later.
- `R-003` undermines correctness of quota enforcement under concurrency, so more work on top of it risks baking in advisory-only semantics.
- `R-004` is a deployment-posture issue on a core internal surface and should be settled before more runtime/operator guidance is layered on top.

### Should Fix Soon

- `R-005`: consumer discovery behavior drifts from the frozen `Package.Networks` precedence contract.
- `R-006`: the in-memory repository is concurrency-unsafe, remains easy to use as the runtime default, and `gateway.New(...)` silently falls back to it.
- `R-007`: the backward-compatibility auth path advertises behavior that does not actually work.
- `R-008`: consumer `PaymentSession` sends still perform blocking stream I/O under lock.
- `R-009`: operator and demo guidance are stale enough to teach the wrong runtime shape.
- `R-010`: session-plugin release is not idempotent under concurrent teardown.
- `R-011`: runtime control dispatch can backpressure metering ingestion.

### Can Batch Later

- `R-012`: `sds demo setup` prints commands that fail as written; this should be fixed, but it is lower leverage than the core ownership and lifecycle issues above.

## Suggested Remediation Workstreams

The blocker set is not purely sequential. Several items can be prepared or implemented in parallel if the write scopes are kept separate.

### Workstream A: Provider Runtime / Session Lifecycle

Primary items:

- `R-013`
- `R-014`
- `R-010`
- `R-011`

Rationale:

- these all sit in the provider plugin/runtime ownership layer
- they should be designed coherently so keep-alives, release, binding, unbinding, and control dispatch all follow one lifecycle model
- `R-010` and `R-011` are narrower than `R-013` and `R-014`, but they are adjacent enough that the same design pass should at least define their boundaries

Parallelization note:

- one worker can own plugin-session lifecycle (`provider/plugin/session.go`)
- another can own runtime-manager/payment-session lifecycle (`provider/gateway/runtime_manager.go`, `provider/gateway/handler_payment_session.go`, `provider/usage/service.go`)
- those two tracks are related but have mostly disjoint write scopes if coordinated carefully

### Workstream B: Metering Shutdown / Emitter Lifecycle

Primary items:

- `R-002`

Rationale:

- this is structurally important, but it is mostly isolated to `provider/plugin/metering.go`
- it can run in parallel with Workstream A if the ownership model is kept local to the metering emitter

Parallelization note:

- low conflict with the runtime-manager/session-lifecycle fixes

### Workstream C: Session/Quota Contract Correctness

Primary items:

- `R-003`
- `R-006`
- `R-007`

Rationale:

- these are all about provider-side runtime/session contract correctness at repository and session-service boundaries
- they should be reviewed together because quota semantics, repository guarantees, and auth/session identity behavior all interact

Parallelization note:

- one worker could own repository semantics (`provider/repository/*`, `provider/gateway/gateway.go`)
- another could own session/auth contract fixes (`provider/session/service.go`, `provider/auth/service.go`)
- these may need light coordination around interfaces and tests

### Workstream D: Transport and Discovery Contract Alignment

Primary items:

- `R-004`
- `R-005`
- `R-009`
- `R-012`

Rationale:

- these are transport/discovery/operator-guidance alignment issues
- they are important, but less intertwined with the core runtime lifecycle bugs than Workstreams A through C

Parallelization note:

- `R-005` can likely be implemented independently in consumer discovery code
- `R-009` and `R-012` can be handled together once the transport decision for `R-004` is fixed

### Workstream E: Contract Correction For Concurrent Streams

Primary items:

- `R-001`

Rationale:

- this is logically simple but high priority because it contradicts the intended MVP contract
- it should be fixed early, with regression coverage, and does not require waiting for the larger lifecycle refactors

Parallelization note:

- this can likely be handled in parallel with every other workstream because its write scope is narrow

## Recommended Order

This is the order I would use when we start implementation:

1. `R-001`
2. Workstream A (`R-013`, `R-014`, then `R-010`/`R-011` in the same pass if feasible)
3. `R-002`
4. Workstream C (`R-003`, `R-006`, `R-007`)
5. `R-004`
6. `R-005`
7. `R-009` and `R-012`
8. `R-008`

Why this order:

- `R-001` is a narrow contract violation and easy to validate early.
- Workstream A fixes the most structural provider-runtime ownership problems.
- `R-002` is isolated and severe, so it should not wait long.
- Workstream C repairs correctness at repository/session boundaries before more behavior is layered on top.
- `R-004` should settle the transport contract before we refresh operator/demo guidance in `R-009` and `R-012`.
- `R-008` is important, but it is safer to address after the broader stream-lifecycle design has been corrected, so we do not overfit a local fix to a shape we are about to change again.

## Concrete Remediation Tasks

These tasks are intentionally tracked in this document rather than the MVP backlog. They are review-driven remediation tasks for the current implementation.

Task status values:

- `not_started`
- `in_progress`
- `done`
- `deferred`

### Task Tracker

| ID | Status | Covers | Primary files | Backlog overlap | Task |
| --- | --- | --- | --- | --- | --- |
| CRT-01 | `proposal_ready` | `R-001` | `provider/gateway/handler_start_session.go`, `test/integration/`, `docs/mvp-scope.md` | overlaps conceptually with `MVP-010`, `MVP-031`, `MVP-040`, but is primarily a contract-correction fix | Remove same-payer session teardown from `StartSession` and add regression coverage proving concurrent streams are not blocked on a single provider instance |
| CRT-02 | `proposal_ready` | `R-013`, `R-010` | `provider/plugin/session.go`, `provider/plugin/session_test.go` | no direct backlog task owns this precisely; closest current overlap is runtime/payment hardening around `MVP-031` and future ownership cleanup under deferred `MVP-039` | Refactor plugin-session lifecycle so keep-alives are session-owned, not request-owned, and make `Release(sessionKey)` teardown idempotent |
| CRT-03 | `proposal_ready` | `R-014`, `R-011` | `provider/gateway/runtime_manager.go`, `provider/gateway/handler_payment_session.go`, `provider/usage/service.go`, integration/unit tests | overlaps conceptually with `MVP-031`, `MVP-040`, and `MVP-041`; this should be treated as follow-up hardening of the already-landed runtime loop, not a new backlog item | Make runtime bind/unbind cleanup atomic and define a non-accidental delivery/backpressure policy between metering ingestion and `PaymentSession` control dispatch |
| CRT-04 | `proposal_ready` | `R-002` | `provider/plugin/metering.go`, `provider/plugin/*_test.go` | adjacent to `MVP-015` and `MVP-031`, but this is an isolated lifecycle hardening task | Fix metering-emitter shutdown semantics so teardown cannot panic and cannot wait forever on unbounded report RPCs |
| CRT-05 | `proposal_ready` | `R-003`, `R-006`, `R-007` | `provider/session/service.go`, `provider/repository/*`, `provider/auth/service.go`, repository/session tests | overlaps with `MVP-008` durable runtime-state work and later operator/runtime surfaces under `MVP-032`; worth flagging because repository semantics touched here affect those tasks | Repair provider session/repository correctness: atomic quota enforcement, honest session/auth identity behavior, and removal or narrowing of silent in-memory fallback in runtime construction |
| CRT-06 | `proposal_ready` | `R-004` | `provider/plugin/gateway.go`, `cmd/sds/impl/provider_gateway.go`, transport tests/docs | directly overlaps with `MVP-021` | Add explicit transport posture for the Plugin Gateway and align local/dev plaintext behavior with the intended non-dev secure-default model |
| CRT-07 | `proposal_ready` | `R-005` | `consumer/sidecar/discovery.go`, `consumer/sidecar/discovery_test.go`, possibly `docs/mvp-scope.md` | directly overlaps with `MVP-033` contract enforcement and `MVP-007` implementation drift | Implement the frozen `Package.Networks` precedence rule in consumer discovery and add focused coverage for precedence and conflict handling |
| CRT-08 | `proposal_ready` | `R-009`, `R-012` | `README.md`, `provider/gateway/REPOSITORY.md`, `provider/plugin/register.go`, `cmd/sds/demo_setup.go` | directly overlaps with `MVP-026`; partially adjacent to `MVP-021` because transport docs should not be refreshed until the transport contract is decided | Refresh operator/demo guidance so docs and generated demo commands match the current runtime and transport contract |
| CRT-09 | `proposal_ready` | `R-008` | `consumer/sidecar/payment_session_manager.go`, `consumer/sidecar/*_test.go` | adjacent to `MVP-031` and `MVP-040`, but best treated as a local ownership fix after provider-side lifecycle work settles | Remove blocking stream sends under lock in the consumer payment-session client without regressing binding and cleanup semantics |

### Task Research Docs

The first research phase is complete. Each task now has a proposal doc with:

- scope boundaries
- current behavior and evidence
- proposed implementation shape
- likely file ownership
- validation targets
- risks, open questions, and cross-task interactions

Research docs:

- `CRT-01`: [current-implementation-review-tasks/CRT-01.md](current-implementation-review-tasks/CRT-01.md)
- `CRT-02`: [current-implementation-review-tasks/CRT-02.md](current-implementation-review-tasks/CRT-02.md)
- `CRT-03`: [current-implementation-review-tasks/CRT-03.md](current-implementation-review-tasks/CRT-03.md)
- `CRT-04`: [current-implementation-review-tasks/CRT-04.md](current-implementation-review-tasks/CRT-04.md)
- `CRT-05A`: [current-implementation-review-tasks/CRT-05A.md](current-implementation-review-tasks/CRT-05A.md)
- `CRT-05B`: [current-implementation-review-tasks/CRT-05B.md](current-implementation-review-tasks/CRT-05B.md)
- `CRT-06`: [current-implementation-review-tasks/CRT-06.md](current-implementation-review-tasks/CRT-06.md)
- `CRT-07`: [current-implementation-review-tasks/CRT-07.md](current-implementation-review-tasks/CRT-07.md)
- `CRT-08`: [current-implementation-review-tasks/CRT-08.md](current-implementation-review-tasks/CRT-08.md)
- `CRT-09`: [current-implementation-review-tasks/CRT-09.md](current-implementation-review-tasks/CRT-09.md)

### Proposal Review Synthesis

The proposal set is in good shape for implementation planning. Most tasks are cleanly separated by file ownership, but a smaller set share design boundaries and should not be treated as fully independent.

#### Clean Independent Tracks

- `CRT-01` is self-contained and can move without waiting on other runtime work.
- `CRT-04` is isolated to plugin metering shutdown semantics.
- `CRT-07` is isolated to consumer discovery contract enforcement.
- `CRT-06` is largely isolated at the code level, but it defines wording and startup assumptions that `CRT-08` must follow.

#### Shared Design Boundaries

- `CRT-02` and `CRT-03` do not share the same primary files, but they do share lifecycle policy:
  - teardown should be idempotent
  - ownership should not be tied accidentally to request lifetime
  - shutdown behavior should be explicit, not incidental
- `CRT-05A` and `CRT-05B` share the repository contract:
  - `CRT-05A` owns snapshot/fallback semantics
  - `CRT-05B` likely needs a new repository-level atomic reservation operation
  - both slices touch `provider/repository/repository.go`, so they need one agreed API shape before implementation starts
- `CRT-06` and `CRT-08` share operator-facing transport wording:
  - `CRT-06` must freeze the Plugin Gateway transport contract first
  - `CRT-08` should then update docs and generated commands against that contract
- `CRT-09` is intentionally downstream of `CRT-02` and `CRT-03`:
  - its local state-machine cleanup depends on the final provider-side lifecycle model

#### Shared Open Questions To Resolve Before Implementation

- `CRT-03`: should runtime control delivery use a queue-backed dispatcher or a narrower coalescing mechanism, and what is the overflow policy for non-terminal messages?
- `CRT-05A` and `CRT-05B`: what repository API should own atomic worker-slot reservation, and should constructor failure on missing repository be returned as an error from `gateway.New(...)`?
- `CRT-05B`: should missing `x-sds-session-id` now fail closed, or should compatibility be preserved via a real session-bootstrap path?
- `CRT-06`: should the Plugin Gateway get explicit plugin-specific transport flags, or should it reuse a shared transport surface with separate wiring?
- `CRT-07`: what exact package/module lookup rule identifies the authoritative `Package.Networks` entry?
- `CRT-09`: once provider-side lifecycle work lands, should consumer stream writes be serialized by a sender goroutine or a smaller dedicated send mutex?

These are not reasons to stop planning, but they are the decisions most likely to create rework if parallel implementation starts without alignment.

#### Likely Merge-Conflict Areas

- `provider/repository/repository.go` is the main shared file between `CRT-05A` and `CRT-05B`.
- provider transport docs and generated commands are shared between `CRT-06` and `CRT-08`.
- `docs/mvp-scope.md` may be touched by both `CRT-01` and `CRT-07` if contract wording is clarified in the same phase.

Every other task currently has a reasonably distinct write scope.

#### Recommended Implementation Waves

- Wave 1:
  - `CRT-01`
  - `CRT-04`
  - `CRT-07`
  - `CRT-06`
- Wave 2:
  - `CRT-02`
  - `CRT-03`
  - `CRT-05A`
- Wave 3:
  - `CRT-05B`
  - `CRT-08`
- Wave 4:
  - `CRT-09`

This keeps the highest-confidence independent fixes moving first, freezes the shared transport and repository boundaries before dependent work starts, and leaves the consumer payment-session refactor until the provider lifecycle contract is more stable.

### Task Details

#### CRT-01 — Remove same-payer session teardown

- Why now:
  - This is a direct behavioral contradiction of the intended MVP contract.
- File ownership:
  - `provider/gateway/handler_start_session.go`
  - `test/integration/consumer_ingress_test.go` or adjacent integration coverage
  - relevant docs only if wording still implies the wrong behavior
- Validation target:
  - add coverage proving a second session for the same payer does not terminate the first one on a single provider instance

#### CRT-02 — Fix plugin-session lifecycle ownership

- Why now:
  - The keep-alive lifecycle and release teardown currently have the wrong ownership boundaries.
- File ownership:
  - `provider/plugin/session.go`
  - `provider/plugin/session_test.go`
- Validation target:
  - add tests for:
    - borrowed session heartbeats surviving beyond the initial borrow request scope
    - repeated/concurrent `Release(sessionKey)` not panicking

#### CRT-03 — Fix runtime-manager lifecycle and dispatch policy

- Why now:
  - Bind/unbind cleanup and dispatch behavior are structural runtime concerns and should be stabilized before more runtime work accumulates.
- File ownership:
  - `provider/gateway/runtime_manager.go`
  - `provider/gateway/handler_payment_session.go`
  - `provider/usage/service.go`
  - focused runtime/integration tests
- Validation target:
  - add tests for:
    - bind-time failure not wedging later session attachment
    - unbind deleting or safely finalizing runtime session state
    - slow/wedged control streams not accidentally stalling metering ingestion unless that becomes an explicit documented policy

#### CRT-04 — Harden metering-emitter shutdown

- Why now:
  - This is a severe but mostly isolated correctness issue.
- File ownership:
  - `provider/plugin/metering.go`
  - `provider/plugin/*_test.go`
- Validation target:
  - add tests proving:
    - no send-on-closed-channel panic during teardown
    - shutdown does not wait forever on an unbounded report RPC path

#### CRT-05 — Repair provider session/repository contract correctness

- Why now:
  - Quota, repository semantics, and auth/session identity all feed the same runtime contract and should not drift independently.
- File ownership:
  - shared task umbrella only; if delegated, split into two concrete slices:
    - repository/runtime-construction slice:
      - `provider/repository/inmemory.go`
      - `provider/repository/repository.go`
      - `provider/gateway/gateway.go`
    - session/auth contract slice:
      - `provider/session/service.go`
      - `provider/auth/service.go`
- Validation target:
  - add or extend tests for:
    - concurrent borrows honoring configured limits
    - no silent fake session ID path that later fails unexpectedly
    - runtime construction not silently choosing in-memory state where fail-fast configuration is intended

#### CRT-06 — Add explicit Plugin Gateway transport posture

- Why now:
  - This is the prerequisite for trustworthy transport/operator guidance.
- File ownership:
  - `provider/plugin/gateway.go`
  - `cmd/sds/impl/provider_gateway.go`
  - any provider transport tests/docs touched by the contract
- Validation target:
  - prove local/dev plaintext still works when explicitly selected
  - prove non-dev/runtime posture is no longer implicitly plaintext-only

#### CRT-07 — Enforce `Package.Networks` precedence in discovery

- Why now:
  - This is a frozen contract that the implementation still does not honor.
- File ownership:
  - `consumer/sidecar/discovery.go`
  - `consumer/sidecar/discovery_test.go`
- Validation target:
  - add coverage for:
    - top-level `network`
    - resolved `networks` entry precedence
    - explicit-input conflict behavior

#### CRT-08 — Refresh operator and demo guidance

- Why now:
  - Operator/docs drift is already teaching the wrong runtime shape.
  - This should follow the transport decision in `CRT-06` where they overlap.
- File ownership:
  - `README.md`
  - `provider/gateway/REPOSITORY.md`
  - `provider/plugin/register.go`
  - `cmd/sds/demo_setup.go`
- Validation target:
  - every printed or documented command should work as written for the intended local/demo or runtime posture

#### CRT-09 — Remove consumer send-under-lock behavior

- Why now:
  - Important, but safer to do after provider-side ownership fixes define the final interaction boundaries more clearly.
- File ownership:
  - `consumer/sidecar/payment_session_manager.go`
  - `consumer/sidecar/*_test.go`
- Validation target:
  - prove binding, submission, receive, and close semantics still hold without blocking stream I/O under the client mutex

### Parallelization Guidance

When implementation starts, these are the safest parallel slices:

- `CRT-01` can run independently of everything else.
- `CRT-02` and `CRT-03` can run in parallel if one owner takes `provider/plugin/session.go` and the other takes `provider/gateway/runtime_manager.go` plus related gateway/usage files.
- `CRT-04` can run in parallel with all other tracks; its write scope is mostly `provider/plugin/metering.go`.
- `CRT-05` should likely be split carefully if parallelized:
  - repository/runtime-construction slice
  - session/auth identity slice
- `CRT-06` should finish before `CRT-08` is finalized.
- `CRT-07` is independent and can run in parallel with every non-consumer task.
- `CRT-09` is best deferred until after `CRT-02` and `CRT-03` have stabilized the lifecycle model enough to avoid reworking the consumer side twice.

## Subagent Split Readiness

This is a short review pass on whether the current task boundaries are safe to hand to subagents with minimal merge conflict risk.

### Ready To Delegate Independently

- `CRT-01`
- `CRT-04`
- `CRT-07`

Reason:

- these have narrow write scopes and low dependency on unsettled design decisions elsewhere

### Safe To Delegate In Parallel With Coordinated Ownership

- `CRT-02` and `CRT-03`
- `CRT-06` and `CRT-08` only if `CRT-06` owns the transport contract first and `CRT-08` follows after that decision is fixed

Reason:

- `CRT-02` and `CRT-03` are related, but their main write scopes are distinct:
  - `CRT-02` owns `provider/plugin/session.go`
  - `CRT-03` owns `provider/gateway/runtime_manager.go`, `provider/gateway/handler_payment_session.go`, and `provider/usage/service.go`
- `CRT-08` should not finalize docs/generated guidance while `CRT-06` is still changing the transport contract

### Needs Internal Split Before Delegation

- `CRT-05`

Reason:

- as currently written, it spans repository semantics, gateway construction defaults, session-service quota logic, and auth/session identity behavior
- that is too broad for a single clean subagent task if we want parallel work

Recommended split:

- `CRT-05A`: repository/runtime-construction semantics
- `CRT-05B`: session/auth identity and quota contract

### Better Deferred Until Earlier Work Settles

- `CRT-09`

Reason:

- it touches the consumer-side payment-session client, but the provider-side lifecycle fixes in `CRT-02` and `CRT-03` may change the most sensible local ownership shape
- delegating it too early risks a local fix that has to be reshaped immediately afterward

### Backlog Overlap Summary

These review tasks are intentionally separate from the MVP backlog, but they do overlap existing backlog items and should be treated as implementation refinements or contract-correction work rather than unrelated new scope.

- `CRT-03` overlaps with already-done runtime-loop tasks `MVP-031`, `MVP-040`, and `MVP-041`.
- `CRT-05` overlaps with `MVP-008` because repository/runtime-state semantics touched here affect durable runtime-state work.
- `CRT-06` directly overlaps with `MVP-021`.
- `CRT-07` directly overlaps with the frozen chain/network contract from `MVP-033` and the shipped discovery implementation from `MVP-007`.
- `CRT-08` directly overlaps with `MVP-026`, and should be coordinated with `MVP-021`.
- `CRT-02` and parts of `CRT-03` are conceptually adjacent to deferred `MVP-039`, but they should still be treated as MVP-hardening work rather than waiting for post-MVP decoupling.

## Next Pass

The next useful steps are:

1. Decide whether the current task boundaries and backlog-overlap notes need any adjustment before implementation begins.
2. Decide whether any `should_fix_soon` items should be pulled into the blocker set after implementation sizing.
3. Add or extend tests that prove the intended contract for concurrency, transport, discovery, and lifecycle behavior.
