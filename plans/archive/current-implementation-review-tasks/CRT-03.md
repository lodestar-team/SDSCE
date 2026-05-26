# CRT-03 — Runtime-Manager Lifecycle and Dispatch Policy

## Scope

This task covers the provider runtime manager and the two paths that depend on it:

- atomic bind/unbind cleanup for `PaymentSession` stream attachment
- the delivery and backpressure policy between metering ingestion and provider control dispatch

The intent is to fix the lifecycle contract around per-session runtime state, not to redesign the payment protocol itself.

Out of scope for this task:

- plugin-session release mechanics owned by `CRT-02`
- metering-emitter shutdown owned by `CRT-04`
- consumer-side stream locking in `CRT-09`

## Current Behavior and Evidence

### Bind attaches stream state before the initial runtime evaluation completes

In `provider/gateway/runtime_manager.go`, `bindSession(...)` creates or reuses the per-session runtime state, stores the stream channel in `current.events`, and only then runs the initial metered-usage evaluation:

- `bindSession(...)` publishes `events` under the manager lock
- `onMeteredUsage(...)` is called after the lock is released

That means the stream is visible to the runtime manager before the bind process has fully succeeded.

### Bind failure can strand a half-bound runtime session

In `provider/gateway/handler_payment_session.go`, the first bind attempt allocates `runtimeEvents`, then calls `s.runtime.bindSession(...)`, and only assigns `sessionID` after bind succeeds.

If `bindSession(...)` returns an error:

- the deferred unbind does not run, because `sessionID` is still empty
- the runtime manager has already published the stream channel into its session state
- the next attach attempt can be rejected as "already bound" even though the stream never became a healthy active session

That is the atomicity gap this task needs to close.

### Unbind clears the channel but keeps the runtime session entry

In `provider/gateway/runtime_manager.go`, `unbindSession(...)` only nils out `current.events`.

It does not:

- delete the session entry from `m.sessions`
- reset any pending control state
- distinguish between an active session, a stale binding, or a fully terminal runtime state

That leaves behind a zombie session record that is easy to misread later as still meaningful state.

### Control delivery currently blocks the ingestion path

The metering path in `provider/usage/service.go` calls `s.runtime.OnMeteredUsage(ctx, sessionID)` inline inside `Report(...)`.

`runtimeManager.dispatch(...)` then sends the control message directly into the per-session channel:

- `dispatch(...)` stores `pendingRAV` under the manager lock
- it then performs `events <- resp` outside the lock
- if the `PaymentSession` receiver is slow, wedged, or unavailable, the send can block

This means a runtime control delivery problem can backpressure metering ingestion unexpectedly.

## Proposed Implementation Shape

The key design goal is to make runtime-session lifecycle transitions explicit and single-owned by `runtimeManager`.

### 1. Make session binding a two-phase lifecycle with atomic cleanup

Recommended shape:

- introduce an explicit internal session-state transition for bind success vs bind failure
- keep the runtime manager as the sole owner of the `sessions` map
- ensure bind failure can atomically clear the half-published stream state
- ensure unbind is idempotent and can fully remove stale session state once the session is no longer live

Concretely, the manager should have a single cleanup path that can:

- verify the bound channel is still the one being cleaned up
- clear the live event channel
- clear any pending dispatch state
- delete the session entry when no active binding remains

That cleanup path should be usable from both:

- bind-time failure handling
- normal stream teardown

This avoids having one path that only nils a field and another path that expects the entry to disappear.

### 2. Separate "decision to dispatch" from "delivery to the stream"

The current design lets metering ingestion drive the actual stream send. That is the part that should change.

Recommended shape:

- `UsageService.Report(...)` should keep doing repository mutation and runtime notification
- `runtimeManager.onMeteredUsage(...)` should decide what control message, if any, needs to be produced
- the actual delivery should be moved behind an explicit per-session delivery mechanism owned by the runtime manager

The important policy decision is:

- metering ingestion must not accidentally block on the live `PaymentSession` stream

A good concrete implementation is a small per-session delivery queue or dispatcher owned by `runtimeManager`, so the runtime manager can queue or coalesce control messages and deliver them without making `Report(...)` wait on the stream receiver.

### 3. Make backpressure semantics explicit

The proposal should not leave dispatch behavior implicit.

Recommended policy:

- metering ingestion is never blocked by control delivery
- `NeedMoreFunds` and RAV-related control messages should be coalesced per session rather than queued unboundedly
- if the stream is not live, the runtime manager should retain the latest relevant state and let the next binding/evaluation decide what to do
- terminal control responses should be prioritized over speculative follow-up messages

In other words, control delivery should be best-effort for a live stream, but the ingestion path must remain independent of stream responsiveness.

### 4. Keep runtime state transitions observable and testable

The runtime manager should expose enough internal behavior through tests to prove:

- a failed bind does not strand a permanently "already bound" session
- a clean unbind removes or fully finalizes the runtime state
- metering evaluation can continue even if a control stream is slow or absent

If the implementation needs a small helper type for per-session state or delivery bookkeeping, that is preferable to encoding the policy indirectly in the existing channel send path.

## Files Likely To Change

- `provider/gateway/runtime_manager.go`
- `provider/gateway/handler_payment_session.go`
- `provider/usage/service.go`
- `provider/gateway/*_test.go`
- `provider/usage/*_test.go`
- possibly focused integration tests around payment-session lifecycle

## Validation Plan

Targeted checks for this task:

- bind-time failure leaves no stale runtime binding that blocks a later successful attach
- unbind is idempotent and removes or fully finalizes the runtime state
- a slow or wedged `PaymentSession` receiver does not stall metering ingestion
- control dispatch remains coherent when a session binds, unbinds, and rebinds

Suggested test coverage:

- unit test for bind failure cleanup
- unit test for normal unbind cleanup
- unit test for dispatch behavior when the control channel is blocked or absent
- race-sensitive test around concurrent bind/unbind/usage notification ordering

Suggested commands once the implementation exists:

- `go test ./provider/gateway ./provider/usage`
- `go test -race ./provider/gateway ./provider/usage`
- `go test ./...`

## Risks / Edge Cases

- If cleanup is too aggressive, an in-flight runtime evaluation could repopulate stale state after a session has already been torn down.
- If delivery is coalesced too aggressively, a terminal response could be overwritten by a later informational message.
- If the new delivery mechanism reuses the request context from `UsageService.Report(...)`, shutdown or client cancellation could still leak into the control plane in an unintended way.
- If bind failure and unbind both try to clean up the same state, the cleanup path must be idempotent.
- If the runtime manager continues to store pending RAV metadata, state deletion needs to be coordinated with that metadata so the next bind sees a consistent view.

## Open Questions

- Should control delivery be queue-based or dispatcher-goroutine-based?
- What is the exact overflow policy for non-terminal control messages?
- Should the runtime manager retry delivery after a blocked send, or should it only keep the latest state and wait for a fresh evaluation trigger?
- When bind-time evaluation fails, should the session state be deleted immediately or marked terminal until the next explicit bind attempt?
- Should `OnMeteredUsage(...)` ever return a distinguished "dispatch deferred" outcome, or should all delivery failures be treated as log-only from the usage path?

## Cross-Task Interactions

### `CRT-02`

`CRT-02` owns plugin-session release behavior. That task and this one both define the lifecycle boundaries around session teardown, so the two implementation proposals should agree on:

- who is allowed to close or invalidate a session
- whether release/bind/unbind paths are idempotent
- how a teardown signal propagates without double-closing or leaving stale state behind

### `CRT-04`

`CRT-04` owns metering-emitter shutdown. This task must coordinate with it because both touch the metering-to-runtime path:

- `CRT-04` decides how the ingestion side shuts down safely
- this task decides how runtime notifications behave once ingestion is active

If `CRT-04` changes how `UsageService.Report(...)` is cancelled or drained, this task should not assume a particular shutdown timing when deciding dispatch behavior.
