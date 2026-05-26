# MVP Agent Workflow (Archived)

This is the historical agent workflow used during MVP execution. For the
current general workflow, use [../agent-workflow.md](../agent-workflow.md).

This repo contains two complementary process documents:

- `AGENTS.md`: repo-specific rules (commands, Go conventions, CLI flag validation patterns).
- `plans/archive/mvp-implementation-backlog.md`: the archived MVP task list with per-task **Done when** + **Verify** criteria and a status table.

This document defines the **step-by-step workflow** to implement backlog tasks consistently (human or LLM).

Note on the backlog switch:

- `plans/archive/mvp-implementation-backlog.md` was the active implementation backlog for MVP-scoped work.
- `plans/archive/implementation-backlog.md` remains useful historical context, but agents should not treat it as the primary task source for current MVP execution.

## Scope of This Workflow

This workflow defines the default execution behavior for MVP-scoped work.

Authoritative planning documents for MVP work:

- `docs/mvp-scope.md`: target-state definition
- `plans/archive/mvp-gap-analysis.md`: current-state assessment
- `plans/archive/mvp-implementation-backlog.md`: archived execution backlog

Historical context only:

- `plans/archive/implementation-backlog.md`

Agents used `plans/archive/mvp-implementation-backlog.md` as the primary task source during MVP execution.

---

## 1) Pick a Task

- Choose an `MVP-###` item from `plans/archive/mvp-implementation-backlog.md`.
- Read its **Context**, **Done when**, and **Verify** sections.
- If the task requires cross-repo coordination, confirm owners and dependencies first.

---

## 1.5) Classify the Task Before Acting

Before making changes, determine which kind of backlog item you are working on:

- **Implementation task**:
  - goal is code and behavior change
  - complete the smallest change that satisfies **Done when**
- **Open-question task**:
  - goal is a documented decision, narrowed contract, or explicit recorded deferral
  - do not silently choose an implementation-specific interpretation in code
- **Validation/review task**:
  - goal is to verify, review, or corroborate behavior against the backlog and current code
  - do not broaden into implementation unless explicitly asked

For every task, first extract and restate:

- task ID
- dependencies
- assumptions
- **Done when**
- **Verify**
- relevant MVP scenarios

Keep this restatement brief and execution-oriented. It should be a short working summary, not a long analysis section.

If a dependency is still unresolved and the current task cannot be completed safely without inventing semantics, mark the task `blocked` instead of coding around it.

When an `open_question` task needs information beyond the repo:

- use repo context first
- when external research is available, prefer primary sources
- record whether the result is a final decision, a narrowed contract, or a recommendation awaiting confirmation

---

## 2) Update Status First

In `plans/archive/mvp-implementation-backlog.md`:

- Set the chosen task `Status` to `in_progress` in the Status Tracker table.
- If you cannot proceed, set `blocked` and add a short reason in the task’s section.

---

## 3) Implement Minimally

- Make the smallest change that fully satisfies **Done when**.
- Prefer fixing root causes over adding workarounds.
- Keep changes narrowly scoped to the selected task (avoid drive-by refactors).

### Scope Control Rules

- Prefer one `MVP-###` task per implementation pass unless the backlog already makes a tightly-coupled dependency unavoidable.
- Small supporting edits are acceptable when they are strictly necessary to satisfy the selected task’s **Done when** or **Verify** criteria.
- Do not solve adjacent backlog items unless they are strictly required to satisfy the selected task’s **Done when** criteria.
- Do not absorb a second meaningful backlog item just because it touches the same files or component.
- Do not introduce new abstractions, helpers, configuration, or refactors unless they are necessary for the selected task.
- If you discover a missing prerequisite or unresolved contract, stop, document it, and update the backlog/task state instead of embedding an implicit decision in code.

Before writing code, run these quick checks:

- **Domain type check**: is there already a repo-level type/helper for this domain (`sds.GRT`, address/signature helpers, etc.)?
- **Ownership check**: if the change touches concurrency or streams, which type owns the resource, the locking, and the timeout/retry policy?
- **Determinism check**: if the change touches demo/dev orchestration, should missing config fail fast instead of silently defaulting?
- **Transport-security check**: if the change touches networking, is insecure/plaintext behavior explicitly scoped to local/dev and not the default for future production paths?
- **Package-boundary check**: if runtime code is importing from a development-only package, should that dependency be promoted to shared infrastructure instead?

### Protobuf changes

If a task touches `proto/`:

- Update `.proto` first.
- Regenerate code (`buf generate`).
- Update conversions/tests that depend on the changed schema.

---

## 4) Add/Update Tests

- Prefer extending existing tests when possible.
- If behavior is user-facing (RPC/CLI), add or extend integration tests under `test/integration/` when practical.
- Keep tests deterministic; rely on `devenv`/testcontainers when on-chain interactions are needed.

---

## 5) Format + Validate

Follow `AGENTS.md` guidance:

- Format changed Go files: `gofmt -w .` (or limited to modified files if preferred).
- Run:
  - `go test ./...`
  - `go vet ./...`

For docs-only, planning-only, or other non-code tasks:

- run the task-specific verification that actually applies
- do not treat `gofmt`, `go test ./...`, or `go vet ./...` as mandatory unless code changed

If validation fails:

- Fix the failure if it’s caused by your changes.
- Do not “fix unrelated” failures unless explicitly requested.

---

## 6) Corroborate With the Task’s Verify Steps

- Run the exact commands in the task’s **Verify** section.
- Confirm the expected outcomes (return codes, error codes, logs, state changes).
- If “Verify” is missing or insufficient, update the backlog entry to make it reproducible.

If the task is an `open_question` item, corroboration should include the concrete output artifact:

- updated docs or contract text
- narrowed decision record
- explicit deferral recorded in the backlog or docs
- downstream task references updated to point at that output when needed

---

## 7) Mark Done

In `plans/archive/mvp-implementation-backlog.md`:

- Set `Status` to `done` in the Status Tracker table.
- Tick the task checkbox in its detailed section.
- If the implementation changed assumptions, update the task description and/or add follow-up tasks.

---

## 8) If Blocked or Deferred

- `blocked`: record the concrete blocker (missing dependency, unclear spec, external repo change required).
- `deferred`: record the reason (not needed yet, too risky now, awaiting design decision) and what would un-defer it.

---

## 8.5) Multi-Agent Coordination

When using multiple agents in parallel:

- assign one primary `MVP-###` task per agent
- use separate git worktrees when parallel agents may edit code concurrently
- avoid overlapping file ownership unless one agent is review-only
- assign one owner for updates to shared planning/status documents when multiple agents are active
- only the assigned document owner should update shared status tables unless explicitly coordinated otherwise
- prefer splitting by dependency boundary or component boundary, not by arbitrary file count
- if a task changes shared contracts or protobufs, finish and merge that work before starting dependent implementation tasks

---

## 9) Incorporate Review Learnings

If you are implementing or revisiting code after reviewer feedback:

- Track the remediation work in the relevant plan/review document instead of leaving it as unstructured commentary.
- Update `AGENTS.md` or workflow docs when the feedback reflects a reusable engineering rule rather than a one-off preference.
- Prefer addressing the structural issue the reviewer pointed out, not just the narrow line comment.
