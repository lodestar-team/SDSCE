# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## Unreleased

### Added

- Added the standalone oracle service with deployment-managed provider metadata, canonical MVP pricing, recommended-provider selection, and consumer-side oracle discovery.
- Added consumer sidecar Substreams-compatible ingress as the primary SDS runtime entrypoint, including direct-provider fallback and provider-returned data-plane endpoint handling.
- Added provider-originated runtime payment control behind sidecar ingress through long-lived provider `PaymentSession` streams, deterministic RAV request thresholds, exact RAV response validation, and low-funds stream stops.
- Added real provider runtime coverage for the public Payment Gateway, private Plugin Gateway, Firecore/Substreams plugins, dummy-blockchain runtime, provider metering, and low-funds runtime control.
- Added durable provider runtime state and settlement state using PostgreSQL-backed sessions, usage, accepted RAV state, collection lifecycle records, and restart-focused coverage.
- Added authenticated provider operator APIs, private operator listener, `/metrics`, read-only inspection CLI commands, and manual provider collection CLI flow.
- Added operator funding and signer-authorization CLI flows for approve, deposit, top-up, and signer authorization against Horizon contracts.
- Added the public direct-provider testnet runbook, local reflex SDS demo guide, operator auth guide, operator funding guide, provider runtime compatibility guide, MVP acceptance matrix, and public-safe provider deployment documentation.
- Added a post-MVP backlog to preserve follow-up work discovered during MVP execution, starting with repository snapshot/runtime-construction hardening.

### Changed

- Made TLS the default non-dev runtime posture for oracle, sidecar, provider payment gateway, and provider plugin gateway surfaces. Plaintext now requires explicit local/dev flags, including explicit `--plugin-plaintext`.
- Replaced wrapper-era runtime orchestration with sidecar-ingress runtime flow. SDS-specific discovery, session bootstrap, payment metadata, metering-driven control, and provider coordination now happen behind the Substreams-compatible sidecar endpoint.
- Defined local-stack MVP acceptance as the source of truth for acceptance scenarios, with dummy-blockchain used as a controlled Substreams-compatible data plane.
- Documented the current runtime image state: published `firehose-core:latest` is compatible with the SDS plugin contract when embedded in a rebuilt dummy-chain image, while published dummy-chain tags remain stale until refreshed upstream.
- Clarified MVP payment/session identity semantics around fresh sessions, non-reused RAV lineage, no reconnect/resume semantics for MVP, and deferred provider-wide concurrent-stream liability accounting.
- Updated planning docs to mark `MVP-021`, `MVP-025`, `MVP-026`, `MVP-036`, and related validation/runtime tasks complete for the MVP implementation scope.
- Archived superseded historical plans, MVP planning records, the MVP-specific agent workflow, and completed review tasks under `docs/archive/` and `plans/archive/`.

### Fixed

- Fixed full-suite integration stability around local Anvil port allocation, PostgreSQL migration path resolution, shared Firecore test state, and Firecore low-funds validation.
- Fixed auth/header propagation and payment-session control handling so provider-originated stops win over ambiguous upstream EOF and active worker state keeps payment control pending correctly.
- Fixed delayed RAV acceptance, exact-snapshot handling, stale request behavior, and under/overpayment validation in provider-originated `RavRequest` flows.
- Fixed operator funding approval reset behavior so reset-first flows cannot be skipped accidentally when `--no-wait` is used.
- Fixed documentation drift around provider public/private surfaces, plugin gateway port/transport, runtime compatibility, TLS/plaintext posture, and local reflex operator flows.

### Removed

- Removed the deprecated wrapper-era `ReportUsage` runtime path and protobuf surfaces.
- Removed wrapper-only `sds sink run` and `sds demo flow` runtime paths from the supported MVP architecture.
- Removed stale historical docs from the active docs surface by moving completed plans and obsolete flow sketches into archive folders.
