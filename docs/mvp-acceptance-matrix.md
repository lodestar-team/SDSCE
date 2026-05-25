# MVP Acceptance Matrix

Last updated: 2026-05-25

## Purpose

This document records the MVP acceptance evidence for the scenarios defined in [docs/mvp-scope.md](./mvp-scope.md).

The local SDS stack is the MVP source of truth for acceptance. It exercises the SDS product boundary:

- oracle or direct provider selection
- consumer sidecar ingress
- provider payment gateway
- private provider plugin gateway
- Firecore/Substreams plugin calls
- provider-side metering and payment control
- durable provider payment and settlement state
- operator funding, inspection, and collection workflows
- secure-by-default transport configuration

The local stack uses dummy-blockchain as a controlled Substreams-compatible data plane. That is intentional. MVP acceptance validates that SDS enables metered consumer/provider Substreams interactions and on-chain settlement. It does not attempt to validate Substreams or Firehose as products, chain-specific indexing correctness, package execution semantics, or production provider infrastructure.

Real provider or public testnet runs remain useful deployment smoke tests, but they are not required to close MVP acceptance.

## Validation Commands

General local acceptance:

```bash
go test ./test/integration/... -count=1
```

Firecore/dummy-chain runtime acceptance is required for the runtime evidence in scenarios `A` and `C`. It currently uses a locally rebuilt dummy-chain image on top of the compatible published `firehose-core:latest` image because the default published dummy-chain tags are stale:

```bash
SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE=ghcr.io/streamingfast/dummy-blockchain:sds-upstream-firecore-latest \
  go test ./test/integration -run '^TestFirecore|TestFirecoreStopsStreamOnLowFunds$' -v -count=1
```

Security and operator/tooling acceptance:

```bash
go test ./cmd/sds ./cmd/sds/impl ./sidecar ./provider/plugin ./provider/gateway ./oracle -count=1
```

Full repository validation:

```bash
go test ./...
go vet ./...
```

## Scenario Matrix

| Scenario | Short title | MVP source of truth | Evidence | Status |
| --- | --- | --- | --- | --- |
| `A` | Discovery to paid streaming | Local oracle-backed sidecar ingress plus local Firecore/dummy-chain runtime | `TestConsumerIngress_UsesOracleSelectedProviderReceiver`; `TestFirecore`; oracle catalog tests; provider metering/payment-session tests | Covered |
| `B` | Fresh session after interruption | Local sidecar ingress with an interrupted first stream followed by a normal second request | `TestConsumerIngress_CreatesFreshSessionAfterInterruptedStream`; `TestInit_CreatesFreshSessionWithoutResumeSemantics` | Covered |
| `C` | Low funds during streaming | Local sidecar ingress and Firecore runtime paths with provider-originated low-funds stop | `TestConsumerIngress_StopsStreamOnLowFunds`; `TestFirecoreStopsStreamOnLowFunds`; `TestPaymentSession_StopsOnLowFunds`; `TestPaymentSession_FailsOpenWhenEscrowBalanceUnknown` | Covered |
| `D` | Restart persistence | PostgreSQL-backed provider repository restart coverage plus authenticated operator read surfaces | `TestSessionUpdateRAVAndBaseline_SurvivesRepositoryRestart`; `TestProviderOperatorService_ReadSurfaces`; `TestProviderOperatorTextFormatting` | Covered |
| `E` | Manual funding | Local Horizon/devenv contract interactions and CLI command validation | `TestAuthorizeSignerFlow`; `TestRejectAllowanceReplacement`; `TestRejectNoWaitResetFirst`; `TestTopUpDepositAmount`; [docs/operator-funding.md](./operator-funding.md) | Covered |
| `F` | Manual collection | Local Horizon/devenv collection transactions plus provider operator collection lifecycle APIs/CLI | `TestCollectRAV`; `TestCollectRAVIncremental`; `TestProviderOperatorService_CollectionMutationErrors`; `TestValidateProviderCollectRecord`; [docs/local-reflex-sds-demo.md](./local-reflex-sds-demo.md) | Covered |
| `G` | Secure deployment posture | TLS-by-default server/client parsing and authenticated operator surfaces, with reflex as explicit plaintext dev exception | `TestServerTransportConfigValidate`; `TestResolvePluginTransportConfig_RequiresExplicitPluginPlaintext`; `TestParseProviderOperatorEndpoint`; `TestProviderOperatorService_AuthEnforcedThroughHandlers`; [docs/direct-provider-testnet-public-runbook.md](./direct-provider-testnet-public-runbook.md) | Covered |

## Scenario Notes

### `A` Discovery To Paid Streaming

Acceptance requires SDS to discover or select a provider, open a provider payment session, route the Substreams request through the consumer sidecar, propagate SDS payment metadata, meter provider-side usage, and advance payment state.

The local oracle-backed ingress test validates provider selection and SDS metadata propagation with a controlled Substreams-compatible upstream. The Firecore runtime test validates the real provider plugin path against dummy-chain.

### `B` Fresh Session After Interruption

Acceptance requires a later normal request to create a fresh SDS payment session rather than reuse prior payment-session identity or RAV lineage.

The ingress interruption test validates this at the user-facing sidecar boundary. Substreams cursor or start-block continuation remains data-plane behavior and is not an SDS payment-session resume contract.

### `C` Low Funds During Streaming

Acceptance requires provider-side session-local funding logic to stop a live stream when funds become insufficient, while failing open when the provider cannot determine escrow balance.

The local ingress and Firecore tests validate that the stop decision reaches the client-facing path. Payment-session tests cover exact-balance and unknown-balance policy.

### `D` Provider Restart Without Losing Collectible State

Acceptance requires accepted RAV state to survive provider repository restart and remain inspectable for settlement.

The PostgreSQL repository restart test validates durable accepted RAV and baseline state. Operator API/CLI tests validate authenticated retrieval surfaces.

### `E` Manual Funding Flow

Acceptance requires operators to perform the Horizon funding prerequisites through CLI-supported flows and for the resulting on-chain state to be usable by SDS runtime flows.

Funding remains an operator/developer workflow for MVP; wallet UI and automatic funding are out of scope.

### `F` Manual Collection Flow

Acceptance requires the provider to expose settlement-relevant RAV state, the CLI to craft/sign/submit `collect()`, and provider-side collection state to distinguish pending, completed, and retryable states.

Automatic/background settlement collection is out of scope.

### `G` Secure Deployment Posture

Acceptance requires TLS as the non-dev default, explicit plaintext for local/dev workflows, and authenticated operator/admin actions.

The reflex stack is the checked-in plaintext development environment. Public or shared deployments should use TLS-capable ingress/load balancing and private operator/plugin surfaces.

## Out Of Scope For MVP Acceptance

- validating Substreams package execution semantics
- validating Firehose indexing correctness
- validating chain-specific data-plane behavior
- validating production provider ingress/load-balancer behavior beyond documented SDS requirements
- validating payer-global exposure across concurrent streams
- validating payment-session continuation across reconnects
- validating automated settlement or wallet-based funding UX
