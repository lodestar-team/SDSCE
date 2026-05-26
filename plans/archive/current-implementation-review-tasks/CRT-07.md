# CRT-07: Enforce `Package.Networks` precedence in consumer discovery

## Scope

This task covers the consumer discovery path that selects the network used for oracle/provider discovery.

In scope:

- `consumer/sidecar/discovery.go` network resolution
- precedence between package-derived network data and explicit user input
- conflict handling after normalization
- test coverage for precedence, fallback, and failure paths

Frozen contract:

- the consumer sidecar derives network from the Substreams package by default
- if a package or module resolves a specific `networks` entry, that takes precedence over top-level `network`
- explicit user-supplied network input is only a fallback when package derivation is unavailable
- if both explicit input and package-derived network exist and differ after normalization, fail fast
- if neither source yields a usable network, fail fast

Out of scope:

- provider-side runtime/session behavior
- transport/security posture
- oracle selection behavior beyond the network string passed into discovery
- docs cleanup unless a wording clarification is needed to keep the frozen contract precise

This task directly overlaps with `MVP-033` contract enforcement and is adjacent to `MVP-007` because discovery is part of the consumer-side startup path.

## Current Behavior and Evidence

The current consumer discovery code does not implement the `Package.Networks` precedence rule.

Observed behavior in `consumer/sidecar/discovery.go`:

- `resolveRequestedNetwork` normalizes `pkg.GetNetwork()` and the explicit `requested` network.
- if both are present and differ, it fails with a conflict error.
- if `pkg.GetNetwork()` is present, it wins over explicit input.
- if `pkg.GetNetwork()` is absent, explicit input is accepted.
- `pkg.GetNetworks()` is not inspected at all.

That means the implementation currently only understands the top-level package `network` field and ignores the package-level `networks` map entirely.

Current tests in `consumer/sidecar/discovery_test.go` only cover:

- top-level package `network`
- fallback to explicit requested network
- explicit conflict with top-level `network`
- missing both inputs

There is no test coverage for:

- a package-derived `networks` entry taking precedence over top-level `network`
- explicit input being ignored when a derived `networks` entry exists
- explicit input conflicting with a derived `networks` entry
- any deterministic handling if the package exposes multiple `Networks` entries

## Proposed Implementation Shape

The clean shape is to separate "package-derived network resolution" from "explicit fallback handling".

1. Introduce a small helper that resolves the package-derived network first.
   - The helper should consider `pkg.GetNetworks()` before falling back to `pkg.GetNetwork()`.
   - It should return the effective package-derived network, not an arbitrary map entry.
   - It should not rely on map iteration order.

2. Keep normalization in one place.
   - Normalize both the derived network and the explicit requested network with the existing alias/canonicalization logic.
   - Compare only normalized values for conflict detection.

3. Apply precedence in this order:
   - resolved `Package.Networks` entry, if one is available for the current package/module context
   - top-level package `network`, if no specific `Networks` entry is resolved
   - explicit user input only when no package-derived network is available

4. Preserve the fail-fast conflict behavior.
   - If both a package-derived network and explicit input are present and differ after normalization, return an error.
   - If the package-derived network is present, it should win over explicit input when they match.
   - If neither source produces a usable network, return an error.

5. Add tests that pin the precedence rules.
   - package `network` still works
   - package `Networks` entry beats top-level `network`
   - explicit input is accepted only when package derivation is absent
   - explicit conflict with a package-derived network fails
   - missing sources still fail

The main implementation question is not the comparison logic, but how the consumer should identify the relevant `Networks` entry from the package metadata. That should be isolated in a helper so the precedence rule stays explicit and testable even if the lookup details need to be adjusted later.

## Files Likely To Change

- `consumer/sidecar/discovery.go`
- `consumer/sidecar/discovery_test.go`
- `docs/mvp-scope.md` only if we decide the frozen contract needs one more clarifying sentence about the package/module lookup key

## Validation Plan

- Add focused unit tests around `resolveRequestedNetwork`.
- Cover both precedence and conflict paths.
- Verify the existing top-level `network` behavior does not regress.
- Verify the explicit-input fallback path still works when package derivation is unavailable.
- Verify the error path when no usable network source exists.
- Run `go test ./consumer/sidecar` and then `go test ./...` if the change stays localized and the test suite remains stable.

## Risks / Edge Cases

- `Package.Networks` is a map, so the resolver must not depend on iteration order.
- The exact key used to resolve a package/module-specific entry may be ambiguous from the current consumer code alone.
- Normalization can collapse aliases, so conflict checks must happen after canonicalization, not before.
- If the derived network is present but malformed, the resolver should fail rather than silently falling back to explicit input.
- If multiple `Networks` entries exist, the implementation needs a deterministic rule for choosing the relevant one; guessing would create nondeterministic discovery behavior.

## Open Questions

- What exact package/module metadata should identify the authoritative `Networks` entry for this consumer path?
- If a derived `Networks` entry and top-level `network` disagree, should the top-level field be silently ignored or logged as a debug-level override?
- Do we want a dedicated test fixture for a package with multiple `Networks` entries, or is a single-entry case enough for this MVP step?

## Cross-Task Interactions

- Direct overlap with `MVP-033`, which freezes the chain/network discovery contract.
- Adjacent to `MVP-007`, because consumer startup and oracle discovery both depend on the same network-resolution semantics.
- Independent from provider-side lifecycle tasks such as `CRT-02`, `CRT-03`, `CRT-04`, `CRT-05`, and `CRT-06`.
- Low risk of merge conflict with operator/doc tasks unless we decide to clarify the frozen lookup wording in `docs/mvp-scope.md`.
