# Agent Workflow (Implementation + Verification)

This repo contains two complementary process documents:

- `AGENTS.md`: repo-specific rules (commands, Go conventions, CLI flag validation patterns).
- `plans/implementation-backlog.md`: the task list with per-task **Done when** + **Verify** criteria and a status table.

This document defines the **step-by-step workflow** to implement backlog tasks consistently (human or LLM).

---

## 1) Pick a Task

- Choose an `SDS-###` item from `plans/implementation-backlog.md`.
- Read its **Context**, **Done when**, and **Verify** sections.
- If the task requires cross-repo coordination, confirm owners and dependencies first.

---

## 2) Update Status First

In `plans/implementation-backlog.md`:

- Set the chosen task `Status` to `in_progress` in the Status Tracker table.
- If you cannot proceed, set `blocked` and add a short reason in the task’s section.

---

## 3) Implement Minimally

- Make the smallest change that fully satisfies **Done when**.
- Prefer fixing root causes over adding workarounds.
- Keep changes narrowly scoped to the selected task (avoid drive-by refactors).

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

If validation fails:

- Fix the failure if it’s caused by your changes.
- Do not “fix unrelated” failures unless explicitly requested.

---

## 6) Corroborate With the Task’s Verify Steps

- Run the exact commands in the task’s **Verify** section.
- Confirm the expected outcomes (return codes, error codes, logs, state changes).
- If “Verify” is missing or insufficient, update the backlog entry to make it reproducible.

---

## 7) Mark Done

In `plans/implementation-backlog.md`:

- Set `Status` to `done` in the Status Tracker table.
- Tick the task checkbox in its detailed section.
- If the implementation changed assumptions, update the task description and/or add follow-up tasks.

---

## 8) If Blocked or Deferred

- `blocked`: record the concrete blocker (missing dependency, unclear spec, external repo change required).
- `deferred`: record the reason (not needed yet, too risky now, awaiting design decision) and what would un-defer it.

