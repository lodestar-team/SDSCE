# CRT-05B: Session/Auth Identity and Quota Contract

## Scope

This task slice covers the contract between `provider/auth` and `provider/session` for:

- how a session ID is established and validated
- how worker quota is reserved and released
- how missing session IDs are handled in the auth path

This slice is intentionally narrow. It does **not** cover the repository/runtime-construction semantics owned by CRT-05A, except where a repository API change is required to make quota reservation atomic.

## Current Behavior and Evidence

- `provider/auth/service.go` currently accepts requests without `x-sds-session-id` and fabricates a new session ID with `session.GenerateID()`. That ID is returned in trusted headers, but it is not backed by any stored session record.
  - Evidence: [provider/auth/service.go](/home/juan/GraphOps/substreams/data-service/provider/auth/service.go#L136-L156)
- `provider/session/service.go` requires `SessionId` on `BorrowWorker` and rejects the call if it is missing.
  - Evidence: [provider/session/service.go](/home/juan/GraphOps/substreams/data-service/provider/session/service.go#L61-L65)
- `BorrowWorker` performs quota enforcement as a read/check/create/increment sequence:
  - read current quota with `QuotaGet`
  - compare against `maxWorkers`
  - create the worker record
  - then call `QuotaIncrement`
  - Evidence: [provider/session/service.go](/home/juan/GraphOps/substreams/data-service/provider/session/service.go#L77-L118)
- `QuotaIncrement` and `QuotaDecrement` are separate repository operations, so the current quota contract is advisory at the service layer unless the backend provides a stronger atomic guarantee.
  - Evidence: [provider/repository/repository.go](/home/juan/GraphOps/substreams/data-service/provider/repository/repository.go#L37-L40)
  - Evidence: [provider/repository/inmemory.go](/home/juan/GraphOps/substreams/data-service/provider/repository/inmemory.go#L198-L229)
  - Evidence: [provider/repository/psql/quota.go](/home/juan/GraphOps/substreams/data-service/provider/repository/psql/quota.go#L18-L71)
- `validateSession` in the auth service assumes the session already exists and is active, which means the generated compatibility ID does not actually satisfy the downstream session contract.
  - Evidence: [provider/auth/service.go](/home/juan/GraphOps/substreams/data-service/provider/auth/service.go#L295-L333)
- There is already a test expectation in session service that missing `session_id` is rejected, which makes the synthetic auth path especially misleading.
  - Evidence: [provider/session/service_test.go](/home/juan/GraphOps/substreams/data-service/provider/session/service_test.go#L91-L104)

## Proposed Implementation Shape

Recommended shape:

1. Make the session identity contract explicit and honest.
   - For any flow that can reach `BorrowWorker`, the session ID must correspond to a real persisted session record.
   - Remove the current “generate a fake session ID and hope later code accepts it” behavior from `ValidateAuth`.
   - If backward compatibility is still needed for a transition period, replace the synthetic ID with a real bootstrap path that creates and persists the session before returning its ID.

2. Move worker quota enforcement to an atomic reservation boundary.
   - Replace the service-layer read/check/create/increment sequence with a single repository-backed reservation operation, or an equivalent transactional check-and-reserve primitive.
   - The quota contract should return one of two outcomes only:
     - reservation succeeded and the worker can proceed
     - reservation failed because the limit was reached or the state was invalid
   - The service should not treat quota increment failure as non-fatal after a worker record has already been created.

3. Keep the current worker-vs-session scope explicit.
   - This slice should continue to reserve worker capacity on `BorrowWorker`, not invent a new session-count policy.
   - If the atomic implementation needs to touch both worker and session counters, that should be spelled out as part of the quota contract and coordinated with CRT-05A before changing repository interfaces.

4. Preserve the current external intent.
   - Valid sessions should continue to borrow workers normally.
   - The fix should remove the false compatibility path, not introduce a new user-visible restriction on legitimate concurrent streams.

Practical implementation direction:

- Prefer a repository-level atomic reservation API over local locking in `SessionService`.
- If the repository API changes, make the in-memory and SQL-backed implementations match the same semantics.
- If the transition path must preserve legacy clients, make that explicit in auth and session state, not via a synthetic session ID.

## Files Likely To Change

Primary implementation files:

- [provider/auth/service.go](/home/juan/GraphOps/substreams/data-service/provider/auth/service.go)
- [provider/session/service.go](/home/juan/GraphOps/substreams/data-service/provider/session/service.go)
- [provider/repository/repository.go](/home/juan/GraphOps/substreams/data-service/provider/repository/repository.go)

Backend implementations that may need to follow the contract change:

- [provider/repository/inmemory.go](/home/juan/GraphOps/substreams/data-service/provider/repository/inmemory.go)
- [provider/repository/psql/quota.go](/home/juan/GraphOps/substreams/data-service/provider/repository/psql/quota.go)
- possibly additional `provider/repository/psql/sql/quota/*.sql` files if the quota operation becomes a single atomic statement

Tests that should probably move with the change:

- [provider/auth/service_test.go](/home/juan/GraphOps/substreams/data-service/provider/auth/service_test.go)
- [provider/session/service_test.go](/home/juan/GraphOps/substreams/data-service/provider/session/service_test.go)
- repository-level tests for the quota reservation path if a new atomic API is added

## Validation Plan

- Add or update auth tests so missing `x-sds-session-id` behavior is covered explicitly.
  - If the contract becomes fail-closed, assert the error is clear and stable.
  - If compatibility is preserved via real bootstrap, assert that the returned session ID can be used by session-aware calls.
- Add concurrency coverage for worker borrowing so two callers cannot oversubscribe quota by racing between `QuotaGet` and `QuotaIncrement`.
- Add a test that shows quota reservation failure leaves no orphaned worker record.
- Run focused tests first:
  - `go test ./provider/auth ./provider/session ./provider/repository/...`
- Then run the broader checks:
  - `go test ./...`
  - `go vet ./...`
  - `go test -race ./provider/auth ./provider/session ./provider/repository/...`

## Risks / Edge Cases

- Removing the synthetic session ID path may break any client that currently relies on auth succeeding without a real session record.
- If the implementation creates a real bootstrap session, that session must have correct payer, status, timestamps, and keepalive semantics from the start.
- Atomic quota reservation needs rollback-safe behavior if worker creation and quota reservation are not performed in the same transaction.
- In-memory and SQL-backed repositories must agree on semantics; otherwise local tests may pass while production behavior differs.
- If the quota reservation API is added at the repository layer, CRT-05A must define that contract before this slice finalizes its own implementation details.

## Open Questions

- Should missing `x-sds-session-id` be a hard failure, or should auth bootstrap a real session for legacy clients?
- If compatibility is preserved, where does the real bootstrap happen: auth service, session service, or a shared repository helper?
- Should quota reservation be expressed as a dedicated “reserve worker slot” operation, or as a more general quota transaction that also tracks session counts?
- Should the auth service be allowed to update session keepalive timestamps during validation, or should that remain strictly in the session service?
- Do we want this slice to be the first place where the repo-level atomic reservation API is introduced, or should CRT-05A define that shape first and have this slice consume it?

## Cross-Task Interactions

- **CRT-05A** is the direct dependency for any repository API change. If the quota fix requires a new atomic reservation method, CRT-05A must establish the repository/runtime-construction slice first so this task can consume a stable contract.
- **CRT-03** is adjacent because it also works with provider-side session lifecycle and runtime state. If CRT-03 changes how worker/session lifecycle events are emitted or cleaned up, this slice should re-check its assumptions about release and keepalive timing.
- **CRT-06** and **CRT-08** are not direct dependencies, but any wording change in the session/auth contract may need to be reflected in operator-facing docs once the transport and startup posture are settled.

