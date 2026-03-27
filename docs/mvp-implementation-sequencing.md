# MVP Implementation Sequencing

This document derives a recommended implementation sequence from `plans/mvp-implementation-backlog.md`.

It is not a replacement for the backlog.

Use it to:

- understand which tasks are true prerequisites for others
- identify which work can proceed in parallel
- avoid prompting agents to implement downstream tasks before the required contracts are stable enough
- keep implementation sequencing aligned with the current backlog and scope, not an older snapshot

Use `docs/mvp-scope.md` as the target-state definition and `plans/mvp-implementation-backlog.md` as the source of truth for task definitions, dependencies, and status.

If `docs/mvp-scope.md`, `plans/mvp-implementation-backlog.md`, or other MVP architecture/planning docs change in a way that affects sequencing, status, or dependencies, this document should be updated in the same change.

## How To Read This Document

This is a dependency-driven sequencing guide, not a strict linear priority list.

The MVP backlog is a DAG with multiple work lanes. Some tasks are:

- **hard blockers**: downstream implementation should not proceed until they are resolved or narrowed enough
- **soft blockers**: they affect a lane, but some limited work can still proceed under the documented assumptions
- **parallelizable**: they can be worked on independently once their local prerequisites are stable enough

This document combines:

- explicit task dependencies from `plans/mvp-implementation-backlog.md`
- execution judgment about which tasks are safest or most useful to do earlier within those dependency constraints

When this document recommends an order that is not strictly enforced by the backlog, treat it as advisory rather than mandatory.

For `open_question` tasks, “done” means producing a concrete output that downstream work can reference:

- a documented decision
- a narrowed contract
- or an explicit recorded deferral

## Sequencing Principles

1. Freeze shared contracts before asking agents to implement multiple dependent tasks.
2. Unlock work by lane once the minimum required contracts for that lane are stable enough.
3. Do not wait for every open question to be fully closed before starting all implementation.
4. Do not start downstream implementation by embedding an implicit answer to an unresolved contract question.
5. Prefer prompting one `MVP-###` task at a time unless the supporting change is strictly required by that task’s `Done when` or `Verify`.

## Phase 0: Shared Contract And Decision Gate

These tasks define semantics or interfaces that multiple lanes depend on.

The grouping is recommended because these tasks have broad downstream impact. It is not a claim that every one of them must be fully closed before any implementation begins.

### Hard Blockers

- `MVP-004` Define and document the byte-billing and payment/header contract used in the real runtime path
  - Blocks most runtime payment, provider integration, and client integration work.
- `MVP-027` Freeze canonical payment identity, `collection_id` reuse, and session-vs-payment keying semantics
  - Blocks reconnect, provider state, settlement lifecycle, and operator retrieval/collection work.
- `MVP-028` Define the MVP authentication and authorization contract for oracle and provider operator surfaces
  - Blocks authenticated admin/operator implementation for oracle and provider APIs.

### Soft Blockers

- `MVP-001` Freeze the pricing exposure contract between oracle metadata and provider handshake
  - Primarily blocks discovery/oracle integration where pricing semantics could drift.
  - Some oracle work can proceed if pricing remains explicitly non-authoritative or advisory.
- `MVP-023` Define the final MVP observability floor beyond structured logs and status tooling
  - Primarily blocks final observability closure, not all operator visibility work.

### Guidance

- Start MVP execution by resolving as many hard blockers as possible.
- Do not require all soft blockers to be fully closed before any implementation begins.
- For `MVP-001` and `MVP-023`, allow limited implementation so long as the current assumptions remain explicit and no irreversible semantics are baked into code.
- `MVP-033` is already resolved enough for downstream discovery and client-integration work to rely on its contract.

## Phase 1: Lane Unlocks

Once enough of the Phase 0 gate is stable, work can proceed in parallel lanes.

The lane ordering below respects explicit backlog dependencies. Ordering within a lane is recommended unless the backlog dependency graph makes it mandatory.

### Lane A: Discovery And Consumer Entry

Minimum prerequisites:

- `MVP-033` resolved enough for implementation
- `MVP-001` stable enough for the chosen oracle/pricing exposure behavior

Recommended sequence:

1. `MVP-005` Implement a standalone oracle service with manual whitelist and recommended-provider response
2. `MVP-007` Integrate consumer sidecar with oracle discovery while preserving direct-provider fallback
3. `MVP-017` Integrate the real consumer/client path with consumer sidecar init, usage reporting, and session end
4. `MVP-030` Add runtime compatibility and preflight checks for real provider/plugin deployments

Notes:

- `MVP-033` is resolved for MVP with the following contract:
  - consumer sidecar derives network from the Substreams package by default
  - if a package/module resolves a specific `networks` entry, that takes precedence over top-level `network`
  - explicit input remains supported only as fallback when package derivation is unavailable
  - mismatch between explicit and package-derived values fails fast after normalization
  - missing usable network also fails fast
- `MVP-017` also depends on `MVP-011`, so only the entry/lifecycle portion should move first.
- `MVP-030` is late in the lane because it depends on real-path integration existing.

### Lane B: Runtime Payment And Stream Control

Minimum prerequisites:

- `MVP-004`

Recommended sequence:

Completed foundation:

- `MVP-010` Implement session-local low-funds detection and provider terminal stop behavior during streaming

Recommended next sequence:

1. `MVP-012` Add deterministic RAV issuance thresholds suitable for real runtime behavior
2. `MVP-014` Integrate provider gateway validation into the real provider streaming path
3. `MVP-015` Wire real byte metering from the provider/plugin path into gateway payment state
4. `MVP-011` Propagate provider low-funds stop decisions through consumer sidecar into the real client path
5. `MVP-016` Enforce gateway Continue/Stop decisions in the live provider stream lifecycle
6. `MVP-031` Wire the live PaymentSession and RAV-control loop into the real client/provider runtime path

Notes:

- `MVP-010` is now the frozen low-funds foundation for this lane:
  - session-local exposure only
  - terminal stop on insufficient funds
  - fail-open if live escrow balance cannot be determined
- `MVP-014` remains the main integration foundation in this lane.
- `MVP-011` is partially advanced because the current sidecar wrapper path already stops on `NeedMoreFunds`, but the real client-facing ingress path is still unfinished.
- `MVP-031` is effectively the capstone runtime-payment task because it depends on real provider and consumer integration plus thresholding.

### Lane C: Provider State, Settlement, And Operator Retrieval

Minimum prerequisites:

- `MVP-027`

Recommended sequence:

1. `MVP-003` Define and document the provider-side runtime persistence boundary and settlement lifecycle ownership
2. `MVP-008` Add durable provider storage for sessions, usage, and latest accepted RAV runtime state
3. `MVP-029` Implement provider collection lifecycle transitions and update surfaces for `collectible`, `collect_pending`, `collected`, and retryable collection state
4. `MVP-009` Expose provider inspection and settlement-data retrieval APIs for accepted/collectible RAV state
5. `MVP-022` Add authentication and authorization to provider admin/operator APIs
6. `MVP-019` Implement provider inspection CLI flows for collectible/accepted RAV data
7. `MVP-020` Implement manual collection CLI flow that crafts/signs/submits collect transactions locally
8. `MVP-032` Expose operator runtime/session/payment inspection APIs and CLI/status flows
9. `MVP-018` Implement operator funding CLI flows for approve/deposit/top-up beyond local demo assumptions

Notes:

- `MVP-008` and `MVP-029` can begin in parallel once `MVP-003` and `MVP-027` are stable enough.
- `MVP-003` should freeze the runtime-versus-settlement boundary before either downstream task broadens its scope.
- `MVP-009` depends on `MVP-029`, so this part of the sequence is required by the backlog rather than just recommended.
- `MVP-018` comes late because the current backlog explicitly ties it to operator runtime/low-funds inspection surfaces.

### Lane D: Post-MVP Reconnect And Resume

This lane is historical context only and is not part of the current MVP rollout.

Minimum prerequisites:

- `MVP-027`

Recommended sequence:

1. `MVP-013` Implement provider-authoritative reconnect/resume semantics if reconnect becomes an in-scope post-MVP target
2. Re-evaluate durable state and handshake requirements against the then-current provider runtime before implementation starts

Notes:

- `MVP-002` is already resolved for MVP and freezes fresh-session semantics rather than resume behavior.
- `MVP-013` is explicitly deferred in the backlog and should not be used to drive current MVP sequencing.
- Any future reconnect/resume work should be treated as a new planning pass, not as an active MVP lane.

### Lane E: Security And Deployment

Minimum prerequisites:

- `MVP-028` for authenticated operator/admin surfaces

Recommended sequence:

1. `MVP-021` Make TLS the default non-dev runtime posture for oracle, sidecar, and provider integration paths
2. `MVP-006` Add authenticated oracle administration for whitelist and provider metadata management
3. `MVP-022` Add authentication and authorization to provider admin/operator APIs
4. `MVP-030` Add runtime compatibility and preflight checks for real provider/plugin deployments

Notes:

- `MVP-021` can proceed relatively early even though it has no hard dependency on `MVP-028`.
- `MVP-030` overlaps discovery and runtime work and should land once the real deployment path is concrete enough to validate.

### Lane F: Observability, Validation, And Docs

Minimum prerequisites:

- `MVP-023` for final observability scope

Recommended sequence:

1. `MVP-024` Implement basic operator-facing inspection/status surfaces and log correlation
2. `MVP-025` Add MVP acceptance coverage for the primary end-to-end scenarios in docs/tests/manual verification
3. `MVP-026` Refresh protocol/runtime docs so they match the MVP architecture and explicit open questions

Notes:

- `MVP-024` can begin in a limited way before `MVP-023` is fully closed if it stays within the current “basic visibility” assumption.
- `MVP-025` should be updated incrementally throughout implementation, but its final closure belongs near the end.
- `MVP-026` should be completed after the key open-question outputs it depends on are stable.

## Suggested Implementation Phases

This is the most practical high-level order to use when prompting agents.

It is a recommended rollout sequence, not a canonical priority order embedded in the backlog itself.

### Phase 0: Resolve Or Narrow Shared Contracts

- `MVP-028`
- `MVP-023`

Already resolved:

- `MVP-001`
- `MVP-002`
- `MVP-003`
- `MVP-004`
- `MVP-010`
- `MVP-027`
- `MVP-033`

### Phase 1: Start The First Implementable Lanes

- Discovery foundation:
  - `MVP-005`
  - `MVP-007`
- Runtime foundation:
  - `MVP-012`
  - `MVP-014`
- Provider state foundation:
  - `MVP-008`
  - `MVP-029`
- Security foundation:
  - `MVP-021`

### Phase 2: Integrate Runtime And Retrieval Paths

- `MVP-015`
- `MVP-011`
- `MVP-016`
- `MVP-017`
- `MVP-009`
- `MVP-022`

### Phase 3: Complete Runtime Control And Operator Flows

- `MVP-031`
- `MVP-006`
- `MVP-019`
- `MVP-020`
- `MVP-032`
- `MVP-018`
- `MVP-030`

### Phase 4: Finalize Visibility, Acceptance, And Documentation

- `MVP-024`
- `MVP-025`
- `MVP-026`

## Tasks That Can Safely Start Before Every Open Question Is Closed

These are useful to know when sequencing agent work.

This section is interpretive guidance based on the assumptions register and dependency graph. It is not a direct restatement of the backlog.

### Safe To Start Early

- `MVP-021`
  - TLS default posture is broadly independent of most unresolved protocol questions.
- `MVP-024`
  - Basic log correlation and status surfaces can begin before observability scope is finalized.
- `MVP-025`
  - Acceptance coverage scaffolding can be built incrementally while implementation proceeds.

### Safe To Start If Assumptions Remain Explicit

- `MVP-005`
  - Can begin before `MVP-001` is fully closed if pricing authority remains clearly non-final in the API/implementation.
- `MVP-024`
  - Can proceed in a reduced/basic form before `MVP-023` is fully closed.

### Should Usually Wait

- `MVP-007`
  - Should wait until the chain/network and pricing exposure contracts are stable enough.
- `MVP-019` and `MVP-020`
  - Should wait until retrieval APIs, auth, and collection lifecycle semantics are in place.

## Prompting Guidance For Sequenced Work

When prompting an agent, reference both the task and its place in the sequencing.

Recommended pattern:

1. State the current phase or lane.
2. State the exact `MVP-###` task.
3. Name the resolved prerequisites the agent is allowed to rely on.
4. Name any unresolved upstream questions the agent must not answer implicitly in code.

Example:

```text
We are currently in Phase 1, Runtime foundation.
Implement MVP-012 only.
You may rely on MVP-004 as the frozen runtime billing/payment contract and MVP-010 as the frozen low-funds control contract.
Do not broaden into MVP-011 or MVP-016 except for strictly necessary supporting edits.
If you find that MVP-012 still requires unresolved semantics beyond those contracts, mark it blocked instead of choosing an implicit contract in code.
```

## Notes

- This document derives sequence from the current dependency structure in `plans/mvp-implementation-backlog.md`.
- Treat this document as a maintained companion to the backlog, not a one-time planning artifact.
- When MVP status, dependencies, or scope wording changes elsewhere, update this document in the same documentation pass if the sequencing view is affected.
- If task dependencies change, this document should be updated to match.
- When the backlog and this document disagree, the backlog is the source of truth.
