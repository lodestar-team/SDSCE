# CRT-09 - Remove Consumer Send-Under-Lock Behavior

## Scope

This task covers `consumer/sidecar/payment_session_manager.go` and the focused tests needed to prove the new behavior.

The scope is intentionally narrow:

- `BindSession(...)`
- `SendRAVSubmission(...)`
- the local stream acquisition and teardown paths those methods depend on

This task does not change provider-side lifecycle semantics, runtime dispatch policy, transport configuration, or the consumer ingress flow beyond what is required to keep the payment-session client coherent.

This proposal is intentionally dependent on `CRT-02` and `CRT-03`. Those provider-side tasks define the final lifecycle and dispatch shape that the consumer client should follow. The consumer-side fix should align with that shape, not try to guess or stabilize it early.

## Current Behavior and Evidence

The current client holds `paymentSessionClient.mu` across transport writes.

- `BindSession(...)` takes the mutex, creates or reuses the stream, and then calls `stream.Send(...)` while still inside the lock.
  - Evidence: [consumer/sidecar/payment_session_manager.go:114](/home/juan/GraphOps/substreams/data-service/consumer/sidecar/payment_session_manager.go#L114) through [consumer/sidecar/payment_session_manager.go:131](/home/juan/GraphOps/substreams/data-service/consumer/sidecar/payment_session_manager.go#L131)
- `SendRAVSubmission(...)` does the same for the RAV send path.
  - Evidence: [consumer/sidecar/payment_session_manager.go:142](/home/juan/GraphOps/substreams/data-service/consumer/sidecar/payment_session_manager.go#L142) through [consumer/sidecar/payment_session_manager.go:164](/home/juan/GraphOps/substreams/data-service/consumer/sidecar/payment_session_manager.go#L164)
- The same mutex also protects endpoint replacement, stream creation, and close/reset state.
  - Evidence: [consumer/sidecar/payment_session_manager.go:92](/home/juan/GraphOps/substreams/data-service/consumer/sidecar/payment_session_manager.go#L92), [consumer/sidecar/payment_session_manager.go:208](/home/juan/GraphOps/substreams/data-service/consumer/sidecar/payment_session_manager.go#L208), [consumer/sidecar/payment_session_manager.go:242](/home/juan/GraphOps/substreams/data-service/consumer/sidecar/payment_session_manager.go#L242)
- When a send fails, the client currently calls `closeLocked()` while still under the same mutex.
  - Evidence: [consumer/sidecar/payment_session_manager.go:125](/home/juan/GraphOps/substreams/data-service/consumer/sidecar/payment_session_manager.go#L125) through [consumer/sidecar/payment_session_manager.go:127](/home/juan/GraphOps/substreams/data-service/consumer/sidecar/payment_session_manager.go#L127), and [consumer/sidecar/payment_session_manager.go:161](/home/juan/GraphOps/substreams/data-service/consumer/sidecar/payment_session_manager.go#L161) through [consumer/sidecar/payment_session_manager.go:163](/home/juan/GraphOps/substreams/data-service/consumer/sidecar/payment_session_manager.go#L163)

Why this matters:

- a slow or wedged provider can block close, rebind, and recovery paths
- the state mutex is currently acting as a transport mutex
- that makes the consumer client harder to evolve once `CRT-02` and `CRT-03` settle the provider-side lifecycle contract

## Proposed Implementation Shape

The goal is to keep `paymentSessionClient.mu` as a state lock, not a network I/O lock.

1. Split state ownership from transport I/O.
   - Keep `mu` for endpoint, gateway client, stream pointer, receive channel, and binding state.
   - Move the actual `stream.Send(...)` call outside that mutex.
   - Use a dedicated transport serialization boundary for the bidi stream instead of reusing the state lock.
   - Preferred shape: a small per-client sender goroutine with request/result signaling.
   - Acceptable fallback if the implementation needs to stay smaller: a dedicated send mutex that is documented as the transport boundary, not the state boundary.

2. Add an explicit in-flight bind state.
   - Track an internal "binding" state, such as `bindingSessionID`, so a second bind cannot race the first one while the bind request is in flight.
   - Only commit `boundSessionID` after the bind request has been sent successfully.
   - If the bind request fails, clear the in-flight state and route cleanup through the same local teardown path used today.

3. Keep RAV submission consistent with the committed bound session.
   - Validate the currently bound session under `mu`.
   - Snapshot the current stream or stream generation under `mu`, then release the lock before the transport write.
   - On send failure, re-enter the single cleanup path if the stream is still current.
   - Do not resurrect stale state after `SetEndpoint(...)` or `Close()`.

4. Preserve close/reset semantics.
   - `closeLocked()` should remain the single place that cancels the active stream and clears binding state.
   - `receiveLoop(...)` should continue to own response-channel closure.
   - `SetEndpoint(...)` and `Close()` should still tear down the current stream cleanly, even if a send is in flight.

The important design constraint is that the client still behaves like a single logical session owner. The fix should remove blocking I/O from the state lock, not allow the same stream to become concurrently writable without an explicit serialization plan.

## Files Likely To Change

- `consumer/sidecar/payment_session_manager.go`
- `consumer/sidecar/payment_session_manager_test.go` or another focused `consumer/sidecar/*_test.go`
- possibly `consumer/sidecar/ingress.go` if the final state machine needs a small adjustment to call ordering or error handling

## Validation Plan

The validation goal is to prove that the client no longer blocks on network I/O while preserving binding and teardown semantics.

Recommended tests:

- a unit test that proves `BindSession(...)` still binds successfully without holding the state lock across the transport send
- a unit test that proves `SendRAVSubmission(...)` does not deadlock when `Close()` or `SetEndpoint(...)` races with it
- a unit test that proves a second bind for the same session remains idempotent, while a bind for a different session still fails fast
- a unit test that proves endpoint replacement still tears down the old stream and allows a later bind to recreate a fresh one
- a race test over bind/send/close/rebind interleavings under `go test -race ./consumer/sidecar`

If the implementation uses a sender goroutine, add one test that proves the goroutine exits on close and does not leak a blocked send path.

## Risks / Edge Cases

- If the transport write is moved out too aggressively, `Close()` or `SetEndpoint(...)` can race with an in-flight bind unless the implementation has an explicit in-flight bind state.
- If a shared send mutex is used, it still has to be documented as a deliberate transport serialization boundary, otherwise the change only moves the blocking point without improving clarity.
- If `boundSessionID` is committed before the bind send succeeds, the client can report a false-positive bound state.
- If `boundSessionID` is committed only after the send succeeds, concurrent bind calls need a separate in-flight marker to avoid duplicate bind races.
- If the provider-side lifecycle changes again in `CRT-02` or `CRT-03`, the consumer client may need a second pass to keep its state transitions aligned.
- A failed send must not leave the client with a stale stream pointer that still looks usable to later calls.

## Open Questions

- Should the transport serialization boundary be a dedicated sender goroutine or a dedicated send mutex?
- Do we want an explicit binding-in-flight field, or is a small internal state enum a cleaner fit?
- On bind failure, should the client always tear down the current stream, or only do so when the send failure indicates the stream itself is unhealthy?
- Should `SendRAVSubmission(...)` return a retryable error when the stream is unavailable, or should it always force a local reset the way the current code does?
- Do we want this task to remain purely local to `payment_session_manager.go`, or should it also absorb any small call-site cleanup in `ingress.go` if the new state model needs it?

## Cross-Task Interactions

### `CRT-02`

`CRT-02` owns plugin-session lifecycle and teardown ownership on the provider side. This task should wait for that work to settle because the consumer client should mirror the final session-lifecycle boundaries, not invent its own interpretation of when a stream is considered dead, closing, or reusable.

If `CRT-02` changes the way session teardown is signaled or observed, this task may need to adjust when it clears the bound state and when it retries or resets the stream.

### `CRT-03`

`CRT-03` owns runtime-manager lifecycle, bind/unbind cleanup, and dispatch policy on the provider side. That task determines the final control-plane behavior the consumer is talking to, so `CRT-09` should be implemented only after that shape is stable enough to avoid a local refactor that immediately becomes stale.

The main dependency is conceptual, not file-level:

- `CRT-03` decides how the provider reacts to stream lifecycle, control dispatch, and teardown timing
- `CRT-09` should keep the consumer-side send path compatible with that contract while removing the blocking lock scope

If `CRT-03` changes when a control stream is considered bound or reusable, this task may need to revisit its cleanup and rebind logic.
