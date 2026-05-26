# CRT-02 - Plugin Session Lifecycle Ownership

## Scope

This task covers the lifecycle ownership model in `provider/plugin/session.go` only.

The two behaviors in scope are:
- keep-alive ownership for the long-lived session worker
- idempotent teardown in `Release(sessionKey)` so repeated release calls do not panic or double-close shared state

This task does not cover runtime-manager dispatch, payment-session backpressure, or broader provider lifecycle cleanup. Those are CRT-03 concerns and should remain separate.

## Current Behavior and Evidence

The current implementation mixes request lifetime with session lifetime.

- `Get(...)` starts the keep-alive loop with the incoming request context, so the loop is tied to the borrow call rather than the session itself.
- `startKeepAlive(...)` exits on `ctx.Done()`, which means request cancellation can stop the keep-alive path even if the session is still logically active.
- `Release(sessionKey)` is launched asynchronously and removes the session entry before closing the `done` channel.
- The current release path does not guard against duplicate calls, so repeated release can race on shared teardown state and double-close the same `done` channel.

Relevant code locations:
- `provider/plugin/session.go`
- `provider/plugin/session_test.go`

## Proposed Implementation Shape

Use a session-owned lifecycle object instead of borrowing the request context as the keep-alive owner.

Recommended shape:
- create a session-scoped cancellation context when the session worker is created or first registered
- store that cancel function and a one-time teardown guard in the session record
- start the keep-alive loop against the session-owned context, not the inbound request context
- make `Release(sessionKey)` perform an idempotent state transition first, then trigger teardown exactly once
- ensure the teardown signal closes the keep-alive path even if `Release(sessionKey)` is called multiple times or from concurrent callers

Implementation details to prefer:
- use a `sync.Once`, `atomic.Bool`, or equivalent one-way state in the session record to guard teardown
- remove the session entry from the registry only once
- close the keep-alive `done` signal only once
- if remote cleanup for the worker is still needed, keep that separate from the idempotent local teardown transition
- keep the release path explicit about whether it waits for goroutines to exit or only signals shutdown; do not let that be accidental

## Files Likely To Change

- `provider/plugin/session.go`
- `provider/plugin/session_test.go`

Possible follow-up test-only changes may also touch supporting plugin test helpers if the session lifecycle needs to be exercised more directly.

## Validation Plan

- add a unit test that calls `Release(sessionKey)` twice and confirms the second call is a no-op
- add a unit test that exercises concurrent `Release(sessionKey)` calls and confirms there is no panic
- add a unit test that proves the keep-alive loop is owned by session lifetime, not the borrow request context
- run `go test ./provider/plugin`
- run `go test -race ./provider/plugin`
- run `go test ./...` once the task is merged with related lifecycle changes

## Risks / Edge Cases

- if the keep-alive loop switches to a session-owned context, the implementation must still terminate promptly on explicit release
- if teardown is made synchronous, it must not block indefinitely on remote cleanup calls
- if teardown remains partially asynchronous, the local idempotent state transition must still happen before any goroutine is spawned
- any change to cancellation ownership must preserve the current error mapping behavior for keep-alive failures
- the release path should not regress worker-return behavior for the normal single-release case

## Open Questions

- Should `Release(sessionKey)` wait for the keep-alive goroutine to exit, or is signaling teardown and returning immediately acceptable?
- Should the session-owned lifecycle live directly in the session record, or in a small internal helper object attached to the record?
- Should the keep-alive loop use one shared session context for both session and worker pings, or separate child contexts for those responsibilities?
- Do we want a final log line or state transition when duplicate `Release(sessionKey)` calls arrive, or should duplicates stay silent?

## Cross-Task Interactions

CRT-02 must coordinate with CRT-03 because both tasks touch long-lived control-plane lifecycle semantics.

The main boundary to preserve is:
- CRT-02 owns plugin-session lifetime and keep-alive teardown semantics in `provider/plugin/session.go`
- CRT-03 owns runtime-manager lifecycle, payment-session dispatch policy, and metered-usage delivery

Coordination points with CRT-03:
- if CRT-03 changes shutdown ordering for provider control-plane handlers, CRT-02 should keep its teardown signal independent of runtime-manager event routing
- if CRT-03 introduces new cancellation or cleanup semantics around payment sessions, CRT-02 should not reuse those paths as the source of truth for plugin-session teardown
- both tasks should agree on whether local teardown is synchronous or signal-only, so that their tests do not encode conflicting shutdown expectations
