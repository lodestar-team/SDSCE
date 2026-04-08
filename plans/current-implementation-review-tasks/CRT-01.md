# CRT-01 - Remove Same-Payer Session Teardown

## Scope

This task covers `provider/gateway/handler_start_session.go` and the regression coverage needed to prove the intended concurrent-stream behavior on a single provider instance.

The only behavior change in scope is removing the current same-payer active-session teardown from `StartSession`.

This task does not change low-funds accounting, cross-instance liability handling, provider-session persistence semantics, or any other lifecycle behavior outside the `StartSession` request path.

Backlog overlap note:
- this is conceptually adjacent to `MVP-010`, `MVP-031`, and `MVP-040`
- it should still be treated as a contract-correction fix, not as a new backlog feature

## Current Behavior and Evidence

`StartSession` currently lists all active sessions for the same payer and force-terminates them before creating the new session.

Relevant behavior in `provider/gateway/handler_start_session.go`:
- the handler queries `s.repo.SessionList(...)` using the payer and `SessionStatusActive`
- any returned sessions are marked ended with `END_REASON_CLIENT_DISCONNECT`
- each existing session is written back through `s.repo.SessionUpdate(...)`
- only after that cleanup does the handler create the new session

This is a direct mismatch with the clarified MVP contract:
- concurrent streams for the same payer are expected and normal
- the real MVP limitation is cross-instance aggregate liability / low-funds correctness, not blocking concurrent streams on one instance

Relevant scope evidence in `docs/mvp-scope.md`:
- the MVP explicitly says blocking concurrent streams at runtime is a non-goal
- the concurrent-stream limitation is documented as a funding/accounting limitation, not as a reason to tear down active sessions

Current test coverage does not appear to assert the desired same-payer concurrency behavior on a single provider instance.

## Proposed Implementation Shape

Remove the active-session teardown block from `StartSession` entirely.

Recommended shape:
- keep the request validation and RAV authorization flow as-is
- keep the session creation path as-is
- delete the `SessionList` / `SessionUpdate` loop that terminates other active sessions for the same payer
- continue creating a fresh session for every accepted request
- let the repository accumulate multiple active sessions for the same payer when those sessions are legitimately concurrent

Regression coverage should explicitly prove that:
- a first session remains active after a second `StartSession` call for the same payer
- the second `StartSession` succeeds without terminating the first
- both sessions can coexist on the same provider instance

Test shape to prefer:
- reuse the existing integration-style provider/consumer setup if that gives the clearest end-to-end assertion
- if the current consumer ingress integration test is too broad, add a focused provider-gateway integration test around `StartSession`
- assert on repository state, not only on response acceptance, so the test proves the first session was not ended as a side effect

Implementation details to prefer:
- do not replace teardown with a “conditional” cleanup path
- do not gate concurrent same-payer sessions behind a hidden feature flag
- do not alter the low-funds policy in the same change; that would mix contract correction with policy work

## Files Likely To Change

- `provider/gateway/handler_start_session.go`
- `test/integration/consumer_ingress_test.go`
- possibly a new focused integration test under `test/integration/`
- `docs/mvp-scope.md` only if the current wording still needs a small clarification to prevent the old interpretation from recurring

## Validation Plan

- add a regression test that starts two sessions for the same payer on one provider instance and verifies the first remains active
- add a regression test that asserts the second `StartSession` call does not terminate or rewrite the first session to `END_REASON_CLIENT_DISCONNECT`
- run the integration test(s) covering the new behavior
- run `go test ./...` after the change is merged with the rest of the task set
- if the test uses the provider gateway directly, also run `go test ./provider/gateway`

## Risks / Edge Cases

- if the repository or downstream runtime logic implicitly assumed one active session per payer, removing the teardown may expose unrelated drift elsewhere
- the test must distinguish between “multiple sessions are accepted” and “session accounting is correct”; it should not accidentally pass just because the second request returns success
- the change should not be confused with the separate cross-instance low-funds limitation described in the MVP scope
- if the current integration harness only exercises one stream at a time, the regression should intentionally open two live sessions or otherwise demonstrate overlap on the same provider instance

## Open Questions

- Should the regression live in the existing consumer-ingress integration test, or would a smaller provider-gateway-focused test make the behavior clearer?
- Do we want the test to assert that both sessions remain `active`, or is it enough to assert that the first session is not force-ended?
- Should `docs/mvp-scope.md` get a narrow wording clarification now, or should the task stay code-only and leave the doc edit to a separate pass?
- Do we want a follow-up audit of any code that still assumes one active session per payer after this teardown is removed?

## Cross-Task Interactions

CRT-01 is mostly independent, but it can surface assumptions in other workstreams.

Potential interactions:
- `CRT-05` if repository/session contract changes later assume one active session per payer
- `CRT-03` if runtime-session cleanup code implicitly relies on the old “terminate existing sessions first” behavior
- `CRT-08` if operator or demo docs still describe the old behavior and need to be updated after the contract change lands

This task should stay narrowly focused so it can be merged before the broader lifecycle hardening work.
