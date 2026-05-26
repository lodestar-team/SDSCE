# Agent Workflow

This document defines the default workflow for human or LLM agents making
changes in this repository.

Use it together with `AGENTS.md`, which contains repo-specific build, test,
style, and commit rules.

The MVP-specific workflow used during MVP execution has been archived at
`docs/archive/mvp-agent-workflow.md`.

## 1. Identify The Work Item

- Start from the user request and the most relevant active plan or issue.
- If the request references an archived MVP or review task, use the archived
  document as historical context and create or update an active follow-up only
  when new work remains.
- Prefer one clearly scoped task per implementation pass.
- Record or restate the task goal, dependencies, acceptance criteria, and
  verification plan before editing when the work is non-trivial.

Active planning references:

- `plans/post-mvp-backlog.md` for post-MVP follow-up tasks.
- `docs/provider-runtime-compatibility.md` for runtime/plugin compatibility
  assumptions.
- `docs/provider-persistence-boundary.md` for provider runtime and settlement
  persistence boundaries.
- `docs/mvp-scope.md` and archived MVP planning docs for historical MVP scope.

## 2. Classify The Task

Before making changes, decide whether the task is:

- **Implementation**: code or behavior must change.
- **Documentation**: operator, developer, or planning docs must change.
- **Validation/review**: behavior must be checked without necessarily changing
  code.
- **Decision/deferral**: the output is a recorded contract, narrowed scope, or
  explicit follow-up.

Do not silently answer unresolved product or architecture questions in code. If
a change depends on a missing decision, document the blocker or create a
follow-up task.

## 3. Keep Scope Tight

- Make the smallest change that satisfies the request and acceptance criteria.
- Avoid drive-by refactors.
- Do not absorb adjacent work just because it touches the same files.
- If you discover related but non-blocking work, add it to the appropriate
  active plan instead of expanding the current patch.

Quick checks before implementation:

- **Domain type check**: use existing project types such as `sds.GRT`, address
  helpers, and signature helpers.
- **Ownership check**: identify which type owns concurrency, stream lifecycle,
  retries, and timeouts.
- **Configuration check**: prefer explicit required config over hidden runtime
  fallbacks for deployed workflows.
- **Transport-security check**: keep insecure/plaintext behavior explicit and
  local/dev-scoped.
- **Package-boundary check**: do not import development-only packages from
  production-oriented runtime or CLI code.

## 4. Implement And Test

- Prefer existing patterns and package boundaries.
- Add focused tests for behavior changes.
- Extend integration tests when the behavior crosses RPC, CLI, chain, or runtime
  boundaries and the cost is reasonable.
- For protobuf changes, edit `.proto` first, regenerate code, and update
  conversions/tests.

Follow `AGENTS.md` for validation. In general:

- Run `gofmt` on changed Go files.
- Run `go test ./...` and `go vet ./...` before commits unless the user
  explicitly asks otherwise.
- For docs-only changes, run relevant link/staleness checks and `git diff
  --check`; Go tests are not mandatory unless code changed.

## 5. Update Documentation And Tracking

- Update docs in the same change when behavior, CLI flags, runtime topology,
  security posture, or operator workflows change.
- Update `docs/provider-runtime-compatibility.md` when shared runtime/plugin
  contracts, protobufs, or external runtime compatibility assumptions change.
- Update `CHANGELOG.md` for user-visible behavior, CLI/API/runtime changes,
  operator docs, planning structure changes, or notable fixes.
- Keep archived docs as historical records; do not rely on them as active task
  trackers.

## 6. Multi-Agent Coordination

When multiple agents work in parallel:

- assign disjoint file or component ownership where possible
- avoid overlapping writes unless one agent is explicitly review-only
- appoint one owner for shared planning/status documents
- merge shared-contract or protobuf work before starting dependent
  implementation
- never revert changes you did not make unless the user explicitly asks

## 7. Close The Loop

Before finalizing:

- verify the acceptance criteria or explain what could not be verified
- check for stale references or docs affected by the change
- summarize files changed and validation performed
- call out any follow-up task added to active planning docs
