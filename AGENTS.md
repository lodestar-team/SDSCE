# Agent Operational Guide

## Build and Test Commands

```bash
# Build the project
go build ./...

# Run go vet checks
go vet ./...

# Run tests (note: no test files exist yet)
go test ./...

# Format
gofmt -w .

# Update dependencies
go get -u ./...
go mod tidy
```

- ALWAYS Use `gofmt` after finish creating/editing a Golang file once you are ready to run tests or make any other external validations but after it compiles correctly.

## Project Structure

- Main package: root directory
- Commands: `cmd/sf_analyzer/` and `cmd/sf_comparator/`
- Metrics: `metrics/`

## Environment

- Go Version: 1.24.0 (toolchain go1.24.4)
- Build Status: PASSING
- Test Status: PASSING (21 tests)
- Only use latest Golang features instead of older idioms (slices, maps, iter, any, generics, etc.)

## CLI Flag Parsing and Error Handling

When parsing CLI flags that require validation:

- Use `cli.Ensure` for required field presence checks (preferred)
- Use non-Must parsing functions and handle errors with `cli.NoError`
- Provide contextual error messages - adjust based on whether field is required or optional

```go
// Preferred - check required fields with cli.Ensure
cli.Ensure(signerKeyHex != "", "<signer-key> is required")

// Good - parsing with contextual error for required field
addr, err := parseAddress(addrHex)
cli.NoError(err, "invalid <service-provider> address %q, it is required", addrHex)

// Good - parsing with contextual error for optional field
if configPath != "" {
    cfg, err := loadConfig(configPath)
    cli.NoError(err, "unable to load config from %q", configPath)
}

// Avoid - Must functions panic without context
addr := MustParseAddress(hex)

// Avoid - returns error without user-friendly context
if err != nil {
    return err
}
```

## Domain Types and Boundaries

- When working with GRT-denominated values, use the project `sds.GRT` type and helpers (`ParseGRT`, `BigInt`, etc.) instead of adding local decimal parsing or formatting helpers.
- Keep `*big.Int` usage at explicit boundaries only: ABI encoding, contract calls, protobuf conversion, or third-party APIs that require it.
- Before introducing a new helper for money/addresses/signatures, check whether the repo already has a project-level type or utility for that domain.
- If contract ABIs/artifacts are needed outside development-only code, move them to a shared package instead of importing from `devenv`.

## Concurrency and Stream Ownership

- Do not lock another struct’s mutex from outside the owning type.
- Public methods should be the synchronization boundary; `*Locked` helpers are acceptable only when fully internal to the owning type.
- Avoid holding mutexes across blocking network I/O unless that serialization is deliberate and clearly documented.
- For long-lived bidi streams, prefer a dedicated owner/manager over goroutine-per-operation wrappers.
- Treat timeouts/retries for control-plane communication as explicit policy, not hidden constants in handlers.

## Demo and Dev Orchestration

- For reproducible demo/dev workflows, prefer fail-fast required environment/config over silent hardcoded fallbacks.
- Use defaults only when they are an intentional part of the user-facing UX, not as hidden implementation conveniences.
- Add a short comment for non-obvious transport/network setup (for example, h2c/plaintext HTTP/2 client configuration).
- Do not make insecure transport the default for code paths that may later be used outside local/demo workflows.
- If plaintext or insecure TLS behavior is needed for local development, gate it behind explicit configuration and keep production-oriented defaults secure.
## Coding Patterns

Use the simplest abstraction form when creating new instance of "semi-primitives" types like GRT, Address, etc.

**GOOD**:

```
sds.NewGRTFromUint64(100)
```

**BAD**:

```
sds.NewGRTFromBigInt(big.NewInt(100))
```

For tests, and "infrequent paths" (like flags parsing, one shot CLI tools, etc.) use the dynamic form when present and the Must version for tests:

```
grt, err := sds.NewGRT(<accepts all types>)

# For tests
sds.MustNewGRT(<accepts all types>)
```

## MVP Planning References

For MVP-scoped work:

- Use `docs/mvp-scope.md` as the target-state definition.
- Use `plans/mvp-gap-analysis.md` for current-state assessment.
- Use `plans/mvp-implementation-backlog.md` as the active execution backlog.
- Treat `plans/implementation-backlog.md` as historical context unless explicitly requested.

## Runtime Compatibility Workflow

- If a change affects shared SDS runtime/plugin contracts, protobufs, or deployment compatibility for external runtimes such as `firecore`, update the compatibility docs in the same change.
- Treat `docs/provider-runtime-compatibility.md` as the operator-facing source of truth for supported runtime tuples, known incompatible runtimes, and compatibility assumptions.
- Call out whether the change is runtime-breaking or backward-compatible for external `firecore` / Substreams deployments.
- Do not add automatic compatibility probes that create runtime side effects unless the user explicitly asks for that tradeoff; prefer explicit documentation and validated tuples for MVP.

## Commit Messages

- When asked to create a commit, first inspect recent commits with `git log --format='%s%n%b' -n <N>` and follow the prevailing repo style instead of inventing a new format.
- In this repo, the expected format is:
  - one short imperative subject line
  - a blank line
  - a flat bullet list in the commit body, with each bullet starting with `- `
- The commit body must contain real newlines. Never pass a single shell-escaped string containing literal `\n` sequences as the body.
- Prefer either multiple `-m` flags or a temporary commit message file so Git receives the intended paragraph and bullet formatting verbatim.
- Do not create a commit until `go vet ./...` and `go test ./...` pass unless the user explicitly asks otherwise.

## Notes

- All builds must pass before committing
- Run `go vet` to ensure code quality
- Use `go mod tidy` after updating dependencies
- Test coverage exists for event.go and utils.go
- Known bug: utils.go line 49 uses `count` before it's set (results in +Inf for average)
