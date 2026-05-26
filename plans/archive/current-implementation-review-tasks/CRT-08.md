# CRT-08 - Refresh Operator and Demo Guidance

## Scope

This task covers operator-facing docs and generated demo guidance only:

- `README.md`
- `provider/gateway/REPOSITORY.md`
- `provider/plugin/register.go`
- `cmd/sds/demo_setup.go`

The goal is to make the documented startup flow match the currently implemented runtime shape, including the explicit transport and endpoint requirements that now exist in the CLI and startup wiring.

This task must not finalize any Plugin Gateway transport wording until `CRT-06` settles the transport contract. Any doc text that refers to the private plugin surface, plaintext, TLS, or endpoint layout should be treated as provisional until that dependency is resolved.

Backlog overlap note:
- directly overlaps with `MVP-026`
- partially adjacent to `MVP-021`, because transport wording must not be refreshed against a stale Plugin Gateway contract

## Current Behavior and Evidence

The repo has drifted in several operator-facing places.

### README

`README.md` still contains older endpoint guidance for the SDS plugin services:

- it says the firecore plugins connect to the provider gateway on `:9001`
- it also has demo and local-flow text that assumes the old transport split without explicitly distinguishing the private plugin gateway surface

The same file already shows the newer local/demo sidecar guidance elsewhere, so the docs are no longer internally consistent:

- the direct-provider quick start uses `--plaintext` explicitly for the consumer sidecar ingress
- the provider gateway section still describes the provider runtime in older terms

### Repository Docs

`provider/gateway/REPOSITORY.md` still teaches the old gateway bootstrap shape:

- the in-memory repository is presented as the default
- the example commands omit `--data-plane-endpoint`
- the examples still describe startup as if the old implicit wiring were enough

That is now stale because the provider gateway CLI requires an explicit data-plane endpoint and explicit transport selection.

### Plugin Registration Comments

`provider/plugin/register.go` still documents plugin DSNs as:

- `sds://localhost:9001?plaintext=true`

That comment no longer matches the current split between:

- the public provider payment gateway
- the private plugin gateway
- explicit transport configuration in startup code

The comment is also too specific for the transport decision that `CRT-06` is still settling.

### Demo Setup Printed Commands

`cmd/sds/demo_setup.go` prints startup commands that are not authoritative anymore:

- the provider gateway command it prints omits an explicit transport flag set
- the consumer sidecar command it prints omits the current ingress/runtime flags needed for the surrounding demo flow to work as written
- the Substreams ingress example is printed as if the earlier bootstrap contract still held

The file is generating operator guidance, so stale output here is effectively documentation drift, not just a cosmetic issue.

## Proposed Implementation Shape

Refresh the operator/demo guidance so it reflects the actual startup contract, but keep the transport-sensitive wording intentionally narrow until `CRT-06` lands.

Recommended shape:

- update `README.md` so it distinguishes the public provider gateway from the private plugin gateway and removes stale `:9001`-only plugin guidance
- update `provider/gateway/REPOSITORY.md` so the sample provider startup commands include the currently required `--data-plane-endpoint` and any other explicit flags needed by the CLI contract
- update `provider/plugin/register.go` comments so they describe the intended endpoint/transport relationship without hardcoding outdated localhost assumptions
- update `cmd/sds/demo_setup.go` so the printed provider/consumer/startup commands match the current local demo flow
- keep demo guidance honest even if that means simplifying the emitted commands or explicitly labeling any command that depends on local/demo-only transport

Implementation boundaries to prefer:

- do not restate transport policy in multiple places with different wording
- do not bake in `plaintext=true` as the universal plugin example
- do not update docs to a final transport posture until `CRT-06` decides whether the Plugin Gateway has explicit separate transport flags or reuses a shared transport model
- if the docs need to reference transport before `CRT-06` is complete, phrase that section as “current local/demo posture” or “pending transport finalization” rather than as a permanent contract

Suggested content changes by file:

- `README.md`
  - refresh the provider/plugin endpoint descriptions
  - make the local/demo flow explicitly distinguish public vs private surfaces
  - keep any local plaintext example clearly labeled as local/demo only
- `provider/gateway/REPOSITORY.md`
  - update the command examples to the current gateway startup contract
  - show the required `--data-plane-endpoint`
  - ensure the repository examples do not imply that `inmemory://` is a safe runtime default for multi-instance use
- `provider/plugin/register.go`
  - replace the hardcoded `:9001?plaintext=true` examples with wording that stays valid once `CRT-06` lands
  - keep the comment focused on what the plugin registration needs, not on a frozen transport guess
- `cmd/sds/demo_setup.go`
  - regenerate the printed commands so they are runnable as written
  - include explicit local/demo transport flags only if they are still the intended demo posture after `CRT-06`
  - if the provider/consumer demo is now too complex to summarize safely in one block, print fewer commands but make them authoritative

## Files Likely To Change

- `README.md`
- `provider/gateway/REPOSITORY.md`
- `provider/plugin/register.go`
- `cmd/sds/demo_setup.go`
- possibly `cmd/sds/impl/provider_gateway.go` only if the printed examples need to mirror exact flag names introduced or finalized by `CRT-06`

## Validation Plan

- review the updated docs for internal consistency, especially the endpoint/transport terminology
- verify that the printed example commands in `cmd/sds/demo_setup.go` match the actual CLI requirements after `CRT-06` settles transport
- if the doc examples include command blocks, copy them into a shell manually or in a smoke-test harness to confirm they are syntactically valid
- run `go test ./...` if any Go file changes are made
- run `gofmt` on any changed Go files

## Risks / Edge Cases

- if `CRT-06` changes the Plugin Gateway transport surface, this task can easily reintroduce stale wording unless the docs are updated after that contract is fixed
- the demo setup output can become misleading again if it tries to document every command instead of only the authoritative happy path
- the repository docs may need a distinction between “development default” and “runtime default” so `inmemory://` does not read like a production-safe recommendation
- if the README tries to cover too many startup permutations, it may become less useful than a shorter, explicit path
- plugin registration comments should avoid overfitting to localhost values that are only true for the current demo layout

## Open Questions

- Should the demo setup output remain a full step-by-step bootstrap script, or should it emit only the minimal authoritative commands?
- Should `provider/gateway/REPOSITORY.md` continue to mention `inmemory://` as the default, or should the wording be softened to emphasize local/dev use only?
- Should the plugin registration comment describe transport in terms of endpoint roles, or should it wait for `CRT-06` and only document the plugin DSN shape once that contract is fixed?
- Should the README keep a single canonical local demo path, or should it split operator guidance into separate local/dev and deployment-oriented sections?
- If `CRT-06` lands after this task, should the transport-sensitive text in this task be revisited immediately or treated as a follow-up doc patch?

## Cross-Task Interactions

`CRT-08` depends on `CRT-06` for the final transport wording.

That dependency is the main reason this task should not be finalized too early:

- if `CRT-06` changes the Plugin Gateway transport contract, `CRT-08` must inherit that wording rather than guessing
- the docs here should not hardcode a `:9001` / plaintext assumption for the plugin surface while the transport decision is still in motion
- `CRT-06` overlaps directly with `MVP-021`, so the operator docs should follow the same secure-default posture once that task lands

Additional interactions:

- `CRT-08` directly overlaps with `MVP-026`
- `CRT-08` should align with `CRT-06` before it is considered final
- if the demo command output needs to mention consumer ingress flags, it may also need a light check against the runtime assumptions in `CRT-07` and `CRT-09`
- this task should stay doc-focused and avoid dragging in runtime lifecycle fixes from the other workstreams
