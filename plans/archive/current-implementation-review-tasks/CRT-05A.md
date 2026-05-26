# CRT-05A: Repository Semantics and Runtime-Construction Posture

## Scope

This task covers the repository/runtime-construction slice of CRT-05 only.

In scope:

- the in-memory repository concurrency contract
- the mismatch between `provider/repository/repository.go` and `provider/repository/inmemory.go`
- the silent fallback to an in-memory repository in runtime construction
- how CLI/runtime bootstrap should fail fast when a repository is not provided explicitly

Out of scope:

- session/auth identity behavior
- quota policy details beyond the repository concurrency boundary
- worker/session borrow semantics themselves, except where they depend on repository correctness
- any transport or plugin-gateway work unrelated to repository construction

This slice must coordinate with CRT-05B for anything that touches session/auth identity or quota contract behavior.

## Current Behavior and Evidence

The repository interface already declares a strong contract: `GlobalRepository` says implementations must be safe for concurrent use in [provider/repository/repository.go](../../../provider/repository/repository.go).

The current in-memory backend does not fully satisfy that contract:

- `SessionGet` and `WorkerGet` return live pointers from shared maps instead of copies in [provider/repository/inmemory.go](../../../provider/repository/inmemory.go).
- `SessionUpdateRAVAndBaseline`, `SessionApplyUsage`, `QuotaIncrement`, and `QuotaDecrement` mutate shared objects in place and then write the same pointer back into the map.
- `SessionList` returns the stored session pointers directly, which exposes mutable shared state to callers.
- `QuotaGet` returns the stored `QuotaUsage` pointer directly when present.

That means the repository is concurrency-safe only in the narrow sense that the map container is protected by the underlying library. The values themselves are still shared mutable objects, so callers can observe and mutate the same structs concurrently.

The runtime-construction posture has a second issue:

- `gateway.New(...)` silently creates an in-memory repository if `config.Repository` is nil in [provider/gateway/gateway.go](../../../provider/gateway/gateway.go).
- the provider CLI still defaults `--repository-dsn` to `inmemory://` in [cmd/sds/impl/provider_gateway.go](../../../cmd/sds/impl/provider_gateway.go).

Those two behaviors combine into a weak fail-fast story. A caller can construct a runtime without making an explicit repository choice, and the system quietly falls back to in-memory state.

## Proposed Implementation Shape

The cleanest shape is to treat this as two linked but separate changes:

1. Make in-memory repository semantics honest and concurrency-safe.
2. Remove silent repository fallback from runtime construction.

For the repository itself, there are two viable implementation strategies:

- copy-on-read / copy-on-write for mutable entities, or
- internal locking around mutable entity state with explicit ownership boundaries

For this codebase, copy-on-read is the better fit for the repository slice because it keeps the repository contract simple:

- repository methods return snapshots, not live shared pointers
- mutating methods update the stored snapshot atomically
- callers cannot accidentally retain and mutate shared repository state outside the repository boundary

That means:

- `SessionGet`, `WorkerGet`, `QuotaGet`, and `SessionList` should return copies of stored values
- `SessionCreate`, `SessionUpdate`, `SessionUpdateRAVAndBaseline`, `SessionApplyUsage`, `QuotaIncrement`, and `QuotaDecrement` should operate on owned copies and then persist the updated copy
- any nested mutable fields such as `map[string]string`, `*big.Int`, or `*horizon.SignedRAV` need deep-copy handling, not shallow pointer reuse

For runtime construction:

- `gateway.New(...)` should stop inventing an in-memory repository when `config.Repository` is nil
- runtime creation should fail fast unless the caller has already selected a repository explicitly
- explicit repository choice should remain available through the CLI/DSN path, where the choice is visible and intentional

The practical shape is:

- keep explicit `inmemory://` as an operator/test choice at the DSN layer
- remove the hidden fallback inside the gateway constructor
- preserve repository selection as a conscious bootstrap decision rather than a default implementation convenience

This keeps the code aligned with the repo guidance about fail-fast configuration and avoids having two separate places where “default to in-memory” can appear.

## Files Likely to Change

- `provider/repository/inmemory.go`
- `provider/repository/repository.go`
- `provider/gateway/gateway.go`
- `provider/gateway/repository.go` if constructor semantics or error handling need to be tightened
- `provider/gateway/repository_test.go`
- repository concurrency tests under `provider/repository/*_test.go`
- possibly CLI bootstrap tests around `cmd/sds/impl/provider_gateway.go`

I would not expect this slice to require changes in `provider/session/service.go`; that belongs to CRT-05B unless repository API shape changes force a small compile-time adjustment.

## Validation Plan

Validation should prove both correctness and behavior change:

- `go test ./provider/repository ./provider/gateway`
- `go test -race ./provider/repository`
- tests proving repository reads do not leak mutable shared state
- tests proving repository updates remain correct under concurrent access
- tests proving `gateway.New(...)` fails or requires an explicit repository instead of silently creating an in-memory one
- tests proving the explicit `inmemory://` DSN path still works when selected intentionally

If repository snapshots are copied on read, add tests that mutate returned values and confirm the stored repository state does not change unless an explicit update method is called.

If the constructor semantics change to return an error on nil repository, the validation should explicitly cover that path so we do not accidentally reintroduce a hidden fallback later.

## Risks / Edge Cases

- Deep-copy behavior can be easy to miss for nested mutable fields.
  - `map[string]string`
  - `*big.Int`
  - `*horizon.SignedRAV`
- If callers currently rely on receiving live pointers from the in-memory repository, those implicit assumptions will break.
- Copy-on-read may add some overhead, but this is acceptable for the in-memory backend because correctness and contract clarity matter more than micro-optimizing test/runtime convenience.
- Removing the fallback in `gateway.New(...)` may expose places where upstream code was implicitly depending on hidden defaults.
- The explicit `inmemory://` DSN path should remain available so tests and local workflows still have a deliberate non-persistent option.

## Open Questions

- Should `gateway.New(...)` return `( *Gateway, error )` if `config.Repository` is nil, or should the nil path panic/assert internally?
  - Preferred shape is an error return, but that is a constructor API choice that may ripple through call sites.
- Should the in-memory repository return deep copies for all getters, or only for the mutable entity types with known shared-state risk?
  - Preferred shape is to copy all mutable repository values consistently, but we should confirm the implementation cost is acceptable.
- Should the CLI default stay `inmemory://` for local developer convenience, or should the default move to an explicit failure requiring the operator to choose?
  - This is a product/UX decision, but the runtime constructor should not have its own hidden fallback either way.
- Does any existing integration test or helper rely on the `gateway.New(...)` nil-repository fallback?
  - If yes, that test/helper will need an explicit repository injection after the constructor changes.

## Cross-Task Interactions

- CRT-05B must coordinate on `provider/session/service.go` and `provider/auth/service.go`.
  - This slice should not decide the session/auth identity contract.
  - Any repository API changes that affect quota/session lookups should be agreed with CRT-05B before implementation.
- This slice overlaps conceptually with `MVP-008` because repository/runtime-state semantics are part of durable provider storage.
- This slice also affects `MVP-032` indirectly because operator/runtime inspection work should build on honest repository semantics, not hidden fallback behavior.
- The repository contract here is foundational for `CRT-03` and `CRT-09` as well, because they both depend on predictable runtime state ownership.

