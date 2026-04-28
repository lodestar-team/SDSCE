# CRT-06 - Plugin Gateway Transport Posture

## Scope

This task covers the transport/security posture of the private Plugin Gateway only.

It includes:
- explicit transport configuration for the Plugin Gateway
- local/dev plaintext behavior when explicitly requested
- secure-default behavior for non-dev usage
- documentation and test updates needed to keep the transport contract honest

It does not change the public Payment Gateway transport contract except where shared startup wiring or docs need to reference both gateways side by side.

## Current Behavior and Evidence

The current Plugin Gateway startup path hardwires plaintext transport:

- `provider/plugin/gateway.go` calls `connectrpc.New(..., server.WithPlainTextServer(), ...)`
- the comment in `provider/plugin/gateway.go` warns that the gateway should stay private, but the code still makes plaintext the only possible runtime mode

The provider CLI already has explicit transport flags, but they are only applied to the public Payment Gateway:

- `cmd/sds/impl/provider_gateway.go` parses `--plaintext`, `--tls-cert-file`, and `--tls-key-file`
- those flags are validated into `sidecar.ServerTransportConfig`
- that transport config is passed to `gateway.Config.TransportConfig`
- the Plugin Gateway config currently receives no transport config at all

The docs and examples still reflect the older transport split in a way that can mislead operators:

- `provider/plugin/register.go` still documents `sds://localhost:9001?plaintext=true`
- `README.md` still has sample firecore text pointing SDS plugin services at `:9001`
- `cmd/sds/demo_setup.go` prints startup commands that need to stay aligned with the actual transport contract

The repo already has a shared transport helper that can be reused rather than reimplemented:

- `sidecar/server_transport.go` contains `ServerTransportConfig.Validate` and `DGRPCOption`
- that type already encodes the secure-vs-plaintext choice explicitly

## Proposed Implementation Shape

Add an explicit transport config to the Plugin Gateway instead of relying on unconditional plaintext.

Recommended shape:
- extend `plugin.PluginGatewayConfig` with a `TransportConfig sidecar.ServerTransportConfig`
- wire that config from the provider CLI and test helper paths, separate from the public Payment Gateway transport config
- update `provider/plugin/gateway.go` to call `TransportConfig.DGRPCOption("plugin gateway")` or the equivalent explicit server option path instead of `server.WithPlainTextServer()`
- keep plaintext as an explicit local/dev choice only, not an implicit fallback
- make the non-dev path secure-by-default by requiring valid TLS material when plaintext is not explicitly requested
- fail fast if the Plugin Gateway transport config is incomplete or internally inconsistent

Concrete implementation notes:
- prefer the existing `sidecar.ServerTransportConfig` type over adding a second transport abstraction
- do not infer Plugin Gateway transport from the public Payment Gateway transport without an explicit decision
- if the provider command exposes separate plugin transport flags, validate them independently from the payment gateway flags
- if the same cert/key set is intended to serve both gateways in a deployment, make that reuse explicit in wiring or docs rather than accidental
- keep the private Plugin Gateway private by policy, but do not encode that policy as plaintext-only code

## Files Likely To Change

- `provider/plugin/gateway.go`
- `cmd/sds/impl/provider_gateway.go`
- `provider/plugin/register.go`
- `README.md`
- `cmd/sds/demo_setup.go`
- `provider/gateway/REPOSITORY.md`
- `provider/plugin/*_test.go` or other plugin transport tests
- `cmd/sds/*_test.go` or provider-gateway command tests if transport validation is covered there

## Validation Plan

- add a unit test that proves the Plugin Gateway chooses plaintext only when plaintext is explicitly configured
- add a unit test that proves secure transport is selected when TLS cert/key material is configured
- add a unit test that rejects invalid transport combinations, especially plaintext combined with TLS material
- add or update a startup/config test that verifies the provider CLI passes a Plugin Gateway transport config separately from the Payment Gateway transport config
- update any integration or smoke tests that rely on the plugin endpoint so they exercise the intended transport mode explicitly
- run `go test ./...`
- run `go vet ./...`
- run `gofmt` on changed Go files

## Risks / Edge Cases

- if the Plugin Gateway and Payment Gateway are deployed together but configured differently, the startup wiring must keep their transport configs isolated
- if local/dev flows keep using plaintext, the docs must make that opt-in behavior obvious so it is not copied into non-dev examples
- if we require TLS material for non-dev startup, some existing demo or local integration commands will need explicit updates
- if transport validation is added too early in shared startup code, it could block legitimate test harnesses that still depend on localhost plaintext
- if the command-line surface grows separate plugin transport flags, the naming should stay consistent with the payment gateway transport flags to reduce operator confusion

## Open Questions

- Should the Plugin Gateway have its own explicit `--plugin-plaintext` / `--plugin-tls-cert-file` / `--plugin-tls-key-file` flag set, or should it reuse the existing payment gateway transport flags through a separate wiring path?
- Should the Plugin Gateway default to TLS whenever certificate material is present, and fail fast otherwise, or should some local-only fallback remain available in code?
- Should local/demo helpers be allowed to print plaintext Plugin Gateway commands by default, or should they print secure defaults and require a deliberate opt-in for plaintext examples?
- Should the Plugin Gateway and Payment Gateway be allowed to share the same transport config in some deployment modes, or should they always be configured independently?

## Cross-Task Interactions

CRT-06 must precede CRT-08.

CRT-08 is the docs/demo guidance task, and it should not finalize operator-facing start commands until the Plugin Gateway transport contract is settled. Otherwise the doc pass will likely bake in stale `:9001` / plaintext assumptions again.

CRT-06 also overlaps with `MVP-021` because both tasks are about the repo-wide TLS-by-default posture outside local/dev usage.

Additional coordination points:
- `CRT-08` should reuse the final CRT-06 transport wording for README, demo setup, and provider gateway repository docs
- any provider-runtime compatibility or operator-facing docs that mention the private Plugin Gateway should be updated from the same transport contract
- this task should stay isolated from the provider lifecycle and session ownership tasks unless shared startup code forces a narrow wiring change
