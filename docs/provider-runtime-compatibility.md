# Provider Runtime Compatibility

Drafted: 2026-04-01
Revised: 2026-05-25

## Purpose

This document defines the MVP compatibility contract for SDS provider deployments that use external runtime components such as `firecore` and Substreams nodes.

It exists to make runtime compatibility explicit without relying on automatic startup probes that could create session, worker, or usage side effects against the underlying provider runtime.

## Scope

This document covers:

- the supported MVP deployment shape for provider/plugin runtime compatibility
- the compatibility rule between SDS provider/plugin code and embedded runtime plugins
- the operator workflow for validating runtime compatibility
- the contributor workflow for recording compatibility and breaking changes

This document does not define a generalized runtime capability-negotiation protocol.

## MVP Compatibility Rule

For MVP, a provider deployment is considered compatible only when:

- the provider payment gateway and private plugin gateway are built from the current SDS repo state being deployed
- the external runtime that calls those plugin gateways is built against an SDS-compatible auth/session/usage plugin contract
- the deployment uses configuration consistent with the validated runtime image state documented below

The important compatibility boundary is the SDS plugin/runtime contract, not a version label by itself.

In practice, this means:

- protobuf or contract changes to SDS auth/session/usage plugin surfaces may be runtime-breaking even when endpoint names remain the same
- older `firecore` or embedded plugin binaries may become incompatible with newer SDS provider/plugin gateways
- compatibility should be treated as an explicit release/deployment concern, not something inferred from "starts successfully"
- the additive `MVP-040` changes to `provider.v1.GetSessionStatusResponse` (`end_reason`, `payment_control_pending`) are payment control-plane only and backward-compatible for external `firecore` / Substreams runtime tuples because they do not modify the auth/session/usage plugin surfaces
- the provider operator API and CLI work from `MVP-009`, `MVP-019`, `MVP-020`, and `MVP-032` is also backward-compatible for external `firecore` / Substreams runtime tuples because it adds a separate authenticated operator listener/client path and does not change the auth/session/usage plugin surfaces

## Supported MVP Deployment Shape

The named MVP target environment is:

- a provider deployment that uses the SDS public Payment Gateway and private Plugin Gateway
- a Substreams / `firecore` runtime configured to call the SDS plugins through the `sds://` scheme
- the consumer sidecar ingress as the client-facing SDS boundary

This is the real deployment shape that the local-first `TestFirecore` harness approximates.

## Historical Local Runtime Tuple

The validated local-first tuple recorded in repo planning docs on 2026-03-28 was:

- SDS `f9bcdbfdccaa9bc1de9fd655c613a59699596c47`
- `firehose-core` `b574a98babcb0338198e0ff4db7ebd0e404f6529`
- `dummy-blockchain` `1cea671e78cbb069d64333fdbf4a6c9dd5502d58`
- `substreams` `8897dccff3e2f989867b7711be91d613d256a36a`
- local image tags `ghcr.io/streamingfast/firehose-core:sds-local` and `ghcr.io/streamingfast/dummy-blockchain:sds-local`

This tuple is retained as historical compatibility evidence. It is not the current published-image reference for `MVP-036`; use the 2026-05-25 image check below for current local validation and rebuild guidance.

## Known Incompatible Runtime

The published `ghcr.io/streamingfast/dummy-blockchain:v1.7.7` image is known to be stale for the current SDS provider/plugin contract because it embeds an older `firecore` binary linked against an older SDS snapshot.

That image remains useful as a concrete example of runtime drift, but it is not a supported compatibility target for the current repo state.

Current image check on 2026-05-25:

- `ghcr.io/streamingfast/dummy-blockchain:v1.7.7` and `:latest` resolve to the same published manifest and still do not validate the current SDS runtime path; the harness skips after observing missing `x-sds-rav` metadata.
- `ghcr.io/streamingfast/dummy-blockchain:1cea671` embeds `firecore` `ffc6ba2` and still does not validate the current SDS runtime path; the harness skips after observing missing `x-sds-rav` metadata.
- `ghcr.io/streamingfast/firehose-core:latest` is current enough for the SDS runtime path. It currently resolves to `firehose-core` `v1.14.4`, commit `4493c5ce0735c50c1b06591de99cf014123e2ae5`, created 2026-05-11.
- A locally rebuilt `dummy-blockchain` image using `--build-arg FIRECORE_VERSION=latest` passes the current `TestFirecore` runtime path.
- `ghcr.io/streamingfast/dummy-blockchain:sds-local`, built locally with an SDS-compatible `firecore` commit, also passes the current `TestFirecore` runtime path.

`MVP-036` is complete for repo-side verification and documentation. The remaining external housekeeping is to republish `dummy-blockchain` on top of the current `firehose-core` image, or to update the repo default once StreamingFast publishes a dummy-chain tag that passes the same validation without local override tags.

## Why There Is No Automatic Startup Probe

For MVP, SDS does not require an automatic startup compatibility probe against the underlying runtime.

The reason is simple:

- a strong compatibility probe would need to exercise auth/session/usage plugin behavior
- realistic session and usage checks can create side effects such as session state, worker state, logs, metrics, or usage records
- the current system does not expose a dedicated read-only runtime capability/version handshake that would allow a strong side-effect-free compatibility check

Because of that, MVP intentionally prefers:

- explicit compatibility documentation
- known-good image or version pinning
- known-bad runtime warnings in docs
- manual validation against the documented supported runtime image state

over:

- side-effectful startup smoke tests
- false confidence from weak "health-only" compatibility probes

## Operator Workflow

Before rolling out SDS against a real provider runtime:

1. Confirm the runtime shape matches the supported SDS deployment model documented here.
2. Pin the runtime to a known-good tuple or to a newer tuple that has been explicitly validated and documented.
3. Review the SDS release notes / changelog for auth/session/usage plugin contract changes.
4. If using a newer runtime tuple, validate it manually before treating it as supported for rollout.

For local-first validation:

- use the current published `firehose-core` image when it has already been validated against the SDS plugin contract
- rebuild only the chain runtime image when the published chain image lags a compatible published `firehose-core`
- point integration at the rebuilt image with `SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE`
- use `TestFirecore` / `TestFirecoreStopsStreamOnLowFunds` as the compatibility validation reference

If the published chain image is stale but `firehose-core:latest` is compatible, rebuild `dummy-blockchain` with `--build-arg FIRECORE_VERSION=latest` and use a local tag such as `ghcr.io/streamingfast/dummy-blockchain:sds-upstream-firecore-latest` until a published dummy-chain tag passes the compatibility test without skips.

If `firehose-core:latest` is also stale, update the SDS dependency in `firehose-core`, rebuild `firehose-core`, then rebuild the chain runtime image that embeds that local `firecore` image.

## Contributor Workflow

When a change affects shared SDS runtime/plugin contracts, contributors must:

- update this document when the supported tuple, compatibility assumptions, or known-incompatible runtimes change
- call out whether the change is backward-compatible or runtime-breaking for external `firecore` / Substreams deployments
- update user-facing docs such as `README.md` when operator guidance changes
- record breaking runtime-compatibility changes in release notes or changelog entries when applicable

Examples of changes that may require compatibility updates:

- protobuf changes under `proto/graph/substreams/data_service/sds/...`
- changes to auth/session/usage plugin request or response semantics
- changes to required plugin configuration or `sds://` URI behavior
- changes that rely on newer embedded SDS plugin binaries in external runtimes
- payment gateway control-plane protobuf changes such as `GetSessionStatusResponse` fields are normally backward-compatible for `firecore` when plugin surfaces are unchanged, but generated clients, tests, and compatibility docs should still be refreshed in the same change

In practical terms, `firehose-core` and downstream runtime images must be bumped when any of these SDS plugin communication boundaries change:

- SDS plugin communication protos under `proto/graph/substreams/data_service/sds/...`
- auth plugin behavior that is transitive to SDS gRPC client calls, currently represented by `provider/plugin/auth.go`
- session plugin behavior that is transitive to SDS gRPC client calls, currently represented by `provider/plugin/session.go`
- metering plugin behavior that is transitive to SDS gRPC client calls, currently represented by `provider/plugin/metering.go`

Changes outside those surfaces, such as provider operator APIs or additive payment control-plane fields, do not normally require a `firehose-core` bump unless they also change the plugin contract used by the runtime.

## Non-Goals

For MVP, SDS does not promise:

- automatic detection of every incompatible external runtime
- side-effect-free protocol negotiation against arbitrary provider runtimes
- compatibility with every historical `firecore` or Substreams release

The MVP promise is narrower:

- one documented supported runtime shape
- explicit documentation of known compatibility requirements
- clear guidance when contract drift is known or introduced
