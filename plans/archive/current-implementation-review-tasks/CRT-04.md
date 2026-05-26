# CRT-04 — Harden metering-emitter shutdown

## Scope

This task covers the shutdown semantics of `provider/plugin/metering.go` only.

The goal is to make the metering emitter safe to terminate under load without:

- panicking on teardown
- waiting forever on report RPCs
- leaving the report-blocking policy implicit

This is the implementation proposal for `R-002` and the corresponding `CRT-04` task in the current implementation review.

## Current behavior and evidence

The current emitter has three shutdown problems:

- `OnTerminating` waits for the launch goroutine to signal `done`, then immediately calls `flushAndClose`.
  - Evidence: [`provider/plugin/metering.go:105`](/home/juan/GraphOps/substreams/data-service/provider/plugin/metering.go#L105) to [`provider/plugin/metering.go:108`](/home/juan/GraphOps/substreams/data-service/provider/plugin/metering.go#L108)
- `flushAndClose` closes `e.buffer`, then drains it.
  - Evidence: [`provider/plugin/metering.go:133`](/home/juan/GraphOps/substreams/data-service/provider/plugin/metering.go#L133) to [`provider/plugin/metering.go:151`](/home/juan/GraphOps/substreams/data-service/provider/plugin/metering.go#L151)
- `Emit` checks `IsTerminating()` and then sends to `e.buffer`, so shutdown can still race with a send on the now-closed channel.
  - Evidence: [`provider/plugin/metering.go:155`](/home/juan/GraphOps/substreams/data-service/provider/plugin/metering.go#L155) to [`provider/plugin/metering.go:175`](/home/juan/GraphOps/substreams/data-service/provider/plugin/metering.go#L175)
- `emit` uses `context.Background()` for `UsageService.Report`, so a slow or wedged report RPC can block the launch loop indefinitely.
  - Evidence: [`provider/plugin/metering.go:178`](/home/juan/GraphOps/substreams/data-service/provider/plugin/metering.go#L178) to [`provider/plugin/metering.go:195`](/home/juan/GraphOps/substreams/data-service/provider/plugin/metering.go#L195)

The failure mode is not just theoretical:

- a blocked `Report(...)` prevents `launch()` from reaching the `done` signal
- `OnTerminating` then waits forever
- the current shutdown path also makes it possible for `Emit` to race into a closed buffer and panic

## Proposed implementation shape

I would treat this as a small ownership redesign of the emitter rather than a one-line patch.

1. Stop closing `e.buffer` during shutdown.
   - Use the buffer as an internal queue only.
   - Replace the close-and-range drain with a bounded drain loop that reads whatever is already queued and then stops when the queue is empty.
   - This removes the send-on-closed-channel failure mode entirely.

2. Add an explicit emitter closing state.
   - Guard shutdown entry with a small internal state flag or mutex.
   - Once shutdown begins, `Emit` should reject new events deterministically instead of racing against teardown.
   - The reject path should be a normal drop path, not a panic path.

3. Bound every report RPC with an explicit timeout.
   - Replace `context.Background()` in `emit()` with `context.WithTimeout(...)`.
   - Make the timeout a first-class config value rather than a hidden constant.
   - Use the same bounded policy for the periodic flush path and the final shutdown flush path.
   - Recommendation: one `report-timeout` style config knob, with a sane default, is enough unless we later discover a reason to separate steady-state and shutdown limits.

4. Make shutdown wait bounded as well.
   - `OnTerminating` should wait for the launch goroutine only within a finite budget.
   - If the launch goroutine is still blocked after the report timeout budget, log and exit instead of hanging the process.
   - The shutdown path should favor termination progress over perfect metric delivery.

5. Stop the ticker on exit.
   - `launch()` should `defer ticker.Stop()` so the emitter does not leak ticker resources while terminating.

6. Preserve the current drop policy outside shutdown.
   - The existing `panicOnDrop` behavior can remain for steady-state buffer overflow if that is still desired.
   - Once shutdown starts, teardown safety should take precedence over `panicOnDrop`.

My preference is:

- keep the change local to `provider/plugin/metering.go`
- use a small explicit timeout config field instead of embedding a silent constant
- make the emitter “best effort until termination, bounded during termination”

## Files likely to change

- `provider/plugin/metering.go`
- `provider/plugin/metering_test.go`
- possibly `provider/plugin/plugin.go` if the new timeout/config name needs a comment or usage example update

## Validation plan

The implementation should be validated with focused tests around shutdown behavior.

Minimum test cases:

- teardown does not panic when `Emit` races with shutdown
- shutdown returns within a bounded time when `UsageService.Report` blocks or sleeps
- buffered events are drained once during shutdown and not double-sent
- a final flush still happens for events already queued before termination
- ticker resources are stopped on exit

I would also run the normal repo checks after the change:

- `go test ./...`
- `go vet ./...`
- `go test -race ./provider/plugin`

## Risks / edge cases

- A timeout that is too short will drop legitimate batches under transient load.
- If shutdown rejection is too aggressive, a small number of late events may be lost during normal termination.
- If the drain loop is not carefully bounded, it can still become an implicit wait loop even without closing the channel.
- Keeping `panicOnDrop` for steady-state overflow while suppressing it during termination needs to be spelled out clearly in code, or the behavior will be confusing.
- If the timeout is configurable, the default must be explicit in the config docs so the blocking policy is not hidden again.

## Open questions

- Should the timeout be one shared `report-timeout` knob, or should steady-state reporting and shutdown flushing have separate limits?
- Should shutdown drop late events silently, or should it log a specific “rejected during shutdown” message?
- Should the final flush attempt retry once on timeout, or should it fail fast and exit?
- Do we want the shutdown policy to be observable through a metric or only through logs?

## Cross-task interactions

- `CRT-03` is the main adjacent task because `provider/usage/service.go` currently calls runtime control synchronously after recording usage. If that path remains blocking, this task should still bound the client-side report RPC so the emitter itself cannot wedge on shutdown.
- `CRT-08` may need to mention the new timeout/config name if any demo or operator guidance shows the metering plugin URL.
- This task should stay independent of `CRT-02` and `CRT-01`; it does not need plugin-session or payer-session behavior changes to be implementable.

