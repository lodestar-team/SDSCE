# Network Readiness Plan

This document tracks the work required to take the **Substreams Data Service
Community Edition (SDSCE)** from its current MVP state — proven end-to-end against
a local Anvil/Horizon devenv — to a service that real providers and consumers can
use on **Arbitrum One (chain id `42161`)**.

SDSCE is a community-maintained edition, unaffiliated with the Graph Foundation or
Edge & Node; the name deliberately leaves room for an official deployment later.

It is the strategic successor to `plans/post-mvp-backlog.md`. Where the post-MVP
backlog tracks isolated follow-ups, this plan sequences the full path to a live
network deployment and names the critical path.

## Strategic Context

The MVP was scoped as a **whitelisted soft-release first, permissionless later**
(per product direction). That split matters because it determines what is on the
critical path:

- A **whitelisted, query-fee-only** SDS needs the shared Horizon payment
  contracts (already live on Arb One), one new audited service contract, a
  working provisioning path, automated settlement, and an operated discovery
  oracle. It does **not** require indexing rewards/issuance, and therefore does
  **not** require a GIP or Graph Council issuance vote.
- A **permissionless** SDS additionally needs on-chain registry-sourced
  discovery, a provider trust/dispute model, and (if rewards are in scope)
  council approval. This is deliberately deferred to Track B.

This plan therefore has two tracks. **Track A is the shippable target.** Track B
is documented so Track A decisions do not foreclose it.

## Target and Key Decisions

- **Target chain:** Arbitrum One (`42161`).
- **Shared Horizon contracts** are already deployed on Arb One and are reused
  as-is. SDS does not redeploy these. **Confirmed live** via the canonical
  `graphprotocol/contracts` Ignition deployment
  (`packages/horizon/ignition/deployments/horizon-arbitrumOne`). A
  `subgraph-service-arbitrumOne` deployment also exists — the direct precedent
  for an SDS-owned DataService on Arb One.
- **New contract:** `SubstreamsDataService` (extends Horizon `DataService`) is
  the only SDS-owned contract and must be deployed to Arb One.

### Verified Arbitrum One Addresses

Source of truth: `graphprotocol/contracts`,
`packages/horizon/ignition/deployments/horizon-arbitrumOne/deployed_addresses.json`.
Re-verify against that file before any deployment; addresses below are recorded
for working reference, not as a substitute for the registry.

| Contract | Arbitrum One address |
| --- | --- |
| HorizonStaking (proxy) | `0x00669A4CF01450B64E8A2A20E9b1FCB71E61eF03` |
| GraphTallyCollector | `0x8f69F5C07477Ac46FBc491B1E6D91E2bb0111A9e` |
| PaymentsEscrow (proxy) | `0xf6Fcc27aAf1fcD8B254498c9794451d82afC673E` |
| GraphPayments (proxy) | `0x7Aae8ae011927BC36Cb4d0d3e81f2E6E30daE06D` |
| Controller | `0x0a8491544221dd212964fbb96487467291b2C97e` |
| L2GraphToken (GRT) | `0x9623063377AD1B27544C965cCd7342f7EA7e88C7` |

Note: the earlier per-layer research draft quoted a GRT address
(`0x9623063677CD4f0fb4EbC29f1E7668326F095236`) that is **incorrect**; the address
above is from the deployment registry.

### DECISION-REQUIRED: skip Arbitrum Sepolia?

Current direction is to go **straight to Arb One and skip Arbitrum Sepolia**.
This is recorded here as an explicit decision so it is taken deliberately, not by
omission.

- **Risk:** the first time `register()` and `collect()` ever exercise a *real*
  Horizon data-service provision will be on mainnet with real GRT. The local
  devenv fakes provisioning with `MockStaking`, so a green local suite does not
  prove the real-chain provisioning path. The testnet runbook already documents
  that `register()` reverts with `ProvisionManagerProvisionNotFound` without a
  real provision.
- **Mitigation if skipping:** the contract audit (NET-02) becomes the sole
  pre-mainnet correctness gate, and the first Arb One deployment should be
  rehearsed against an Arb One fork (e.g. Anvil `--fork-url`) so the real
  provisioning/registration/collection path is exercised without spending real
  funds.
- **Owner sign-off:** Juan / product to confirm. Until confirmed, NET tasks treat
  an Arb One mainnet-fork rehearsal as the substitute for a Sepolia dry run.

## Reference Model

What the network demands of any Horizon data service, by layer, with SDS status:

| Layer | Requirement | SDS today | Track |
| --- | --- | --- | --- |
| Service contract | Audited contract on Arb One, provision params set | Custom contract, devenv-only | A (NET-01, NET-02) |
| Provisioning | Real Horizon provision; `register()` succeeds | Reverts without provision (faked by MockStaking) | A (NET-03) |
| Payments (TAP) | Automated RAV collection/settlement | Manual collection via CLI only | A (NET-04) |
| Discovery | Operated provider/pricing source | Curated YAML oracle, not hosted | A (NET-05) |
| Provider ops | Shippable runtime, monitoring, key custody | One binary; single replica; manual compat | A (NET-06) |
| Consumer entry | Onboarding path for consumers | Self-hosted sidecar, BYO escrow + keys | A (NET-07) |
| Economic security | Slashing/disputes or trusted whitelist | Whitelist (trust); no disputes | A whitelist / B trustless |
| On-chain registry | Permissionless discovery sourcing | Not started | B (NET-08) |
| Rewards/issuance | Council/GIP approval | Out of scope for soft-release | B (NET-09) |

## Critical Path

Everything else parallelizes around this spine:

1. **NET-11** Harden the contract (must precede the audit — it is currently a
   testing-grade artifact).
2. **NET-02** Audit `SubstreamsDataService` (longest lead time — start scoping in
   parallel with NET-11).
3. **NET-01** Deploy contract config + addresses for Arb One (shared Horizon
   addresses already verified — see above).
4. **NET-03** Real Horizon provisioning path (the load-bearing blocker:
   `register()`/`collect()` must succeed against a real provision).
5. **NET-04** Automated collection (manual CLI does not survive real traffic).

NET-05 through NET-07 can proceed in parallel with NET-02–04. NET-08+ are Track B.

## Status Values

- `not_started`
- `in_progress`
- `blocked`
- `done`
- `deferred`

## Tasks

| ID | Status | Track | Area | Task |
| --- | --- | --- | --- | --- |
| NET-01 | `in_progress` | A | contracts | Arb One contract addresses, chain config, and deployment of `SubstreamsDataService` |
| NET-02 | `not_started` | A | contracts | Security audit of `SubstreamsDataService` (hard gate before any mainnet deploy) |
| NET-03 | `in_progress` | A | contracts | Real Horizon data-service provisioning path (register + provision) |
| NET-04 | `in_progress` | A | settlement | Automated/background RAV collection daemon |
| NET-05 | `not_started` | A | discovery | Operate the discovery oracle as a hosted service with curated whitelist |
| NET-06 | `not_started` | A | provider-ops | Provider runtime hardening: image/runtime compat, monitoring, key custody, replica story |
| NET-07 | `not_started` | A | consumer | Consumer onboarding path (sidecar vs. hosted gateway, funding UX) |
| NET-08 | `deferred` | B | discovery | Permissionless on-chain registry sourcing in the oracle |
| NET-09 | `deferred` | B | governance | Issuance/rewards via GIP + Graph Council (only if rewards are in scope) |
| NET-10 | `deferred` | B | trust | Provider trust/dispute/verifiability model for trustless operation |
| NET-11 | `in_progress` | A | contracts | Harden `SubstreamsDataService` before audit (access control, upgradeability, slashing) |

---

## NET-01 Arb One Contract Configuration and Deployment

Context:

- The shared Horizon stack (PaymentsEscrow, GraphTallyCollector, GraphPayments,
  GraphController, GRT) is already live on Arb One; SDS reuses these addresses.
- `SubstreamsDataService` is SDS-owned and must be deployed to Arb One.
- Today, real-chain addresses are passed via CLI flags with no Arb One defaults;
  only Arb Sepolia addresses are documented (in the testnet runbook).

Done when:

- Canonical Arb One contract addresses (shared Horizon + deployed
  `SubstreamsDataService`) are documented in a single source of truth.
- The EIP-712 signing domain is confirmed against chain id `42161` and the
  deployed collector address.
- A repeatable, reviewed deployment procedure for `SubstreamsDataService` exists
  (script + parameters), gated behind NET-02.

Verify:

- Dry-run the deployment against an Arb One fork (Anvil `--fork-url`) and confirm
  the contract reads/writes against the real shared Horizon contracts.
- Confirm consumer signer proof + funding CLI succeed end-to-end on the fork.

Progress:

- Arb One addresses verified and recorded (see above) and in
  `docs/arb-one-deployment-runbook.md`.
- The deployment procedure (deploy `SubstreamsDataService`, set provision range,
  onboard provider/consumer) is documented in that runbook and proven on an Arb
  One fork via `devel/arb-one-fork-rehearsal.sh` and
  `devel/arb-one-collect-rehearsal.sh`. The actual mainnet deploy stays gated
  behind NET-02 (audit).

## NET-02 Security Audit of SubstreamsDataService

Context:

- `SubstreamsDataService` collects funds (`collect()` routes value through
  GraphPayments). It is the only SDS-owned on-chain surface and is currently
  unaudited.
- This is a hard gate: no mainnet deployment before audit sign-off, regardless of
  the Sepolia decision.

Done when:

- The contract has been audited by a reputable firm and all critical/high
  findings are resolved.
- The audited bytecode matches the deployment artifact used in NET-01.

Verify:

- Audit report published/internally available.
- Deployed bytecode hash matches the audited commit.

## NET-03 Real Horizon Provisioning Path

Context:

- This is the documented "Current Known Gap": `SubstreamsDataService.register()`
  reverts with `ProvisionManagerProvisionNotFound` until a real Horizon provision
  exists. The devenv hides this with `MockStaking`.
- Without this, providers cannot register and `collect()` reverts even though
  payment sessions and RAV acceptance work.

Done when:

- ProvisionManager parameters (min/max provision tokens, thawing period, verifier
  cut, max slashing) are set for `SubstreamsDataService` on Arb One. For
  soft-launch, evaluate a low or zero minimum provision to lower the provider
  onboarding barrier.
- A whitelisted provider can create a Horizon provision toward
  `SubstreamsDataService` and call `register()` successfully.
- `collect()` succeeds against a real provision and routes funds correctly.

Verify:

- Full register → fund → stream → RAV → collect cycle succeeds against an Arb One
  fork with a real (non-mock) provision.
- Document the exact provider onboarding steps (stake, provision, register).

Findings (fork rehearsal, `devel/arb-one-fork-rehearsal.sh`):

- Validated against a fork of **real Arbitrum One** Horizon contracts (not the
  devenv mocks). `SubstreamsDataService` deploys cleanly wired to the live
  Controller + GraphTallyCollector.
- Reproduced the documented gap: `register()` reverts
  `ProvisionManagerProvisionNotFound` (selector `0x7b3c09bf`) with no provision.
- Closed the loop: `stake` → `provision(provider, dataService, tokens, 0, 0)` →
  `register()` **succeeds** (`isRegistered = true`). The "gap" is therefore
  **operational provider onboarding, not a code blocker** — identical to the
  subgraph-service onboarding shape.
- **Money path proven** (`devel/arb-one-collect-rehearsal.sh`): escrow deposit →
  authorize signer → signed RAV → `SubstreamsDataService.collect()` settles
  against the **real** GraphTallyCollector / PaymentsEscrow / GraphPayments stack
  on Arb One (`tokensCollected` increments by the RAV value). EIP-712 artifacts
  produced by `devel/sdsce-signtool`, reusing SDSCE's own signing code.
- **Provider onboarding runbook written:** `docs/arb-one-deployment-runbook.md`
  documents the exact stake → provision → register → fund → authorize → collect
  steps for Arb One, derived from the proven rehearsals.
- Remaining for full NET-03: exercise the full streaming → metered RAV → collect
  path end-to-end (vs. the hand-built RAV used here), which needs the firehose
  data plane (dummy-blockchain runtime).

## NET-04 Automated Collection Daemon

Context:

- Collection is currently manual: an operator runs `sds provider operator collect`
  per collectible RAV. There is no background settlement loop, threshold, or
  retry automation. This does not scale to real traffic.
- This carries forward the post-MVP "automated/background settlement" follow-up.

Done when:

- A background process discovers collectible RAVs and submits `collect()`
  transactions on a configurable cadence/threshold.
- Failed transactions transition to `collect_failed_retryable` and are retried
  with backoff; settlement key custody remains outside the public gateway
  process.
- Operator CLI remains available for manual intervention.

Verify:

- Integration test: accumulated RAVs are collected automatically and reach
  `collected` without operator action.
- Test retry behaviour on simulated transaction failure.

Progress:

- **Daemon implemented** as `sds provider operator collect-daemon`
  (`cmd/sds/provider_operator_collect_daemon.go`). It polls the operator API for
  `collectible` (and `collect_failed_retryable`) records and submits
  `collect()` automatically, wrapping the same validated logic as the manual
  `collect` command.
- **Key custody preserved:** the daemon holds the settlement key and runs as a
  separate process from the gateway (talks to the operator API), so settlement
  keys are never mounted into the gateway — consistent with the deployment
  boundary.
- **Retry/backoff:** failed records retry with exponential backoff
  (`--retry-backoff-base`, doubling, capped at 1h) up to `--max-attempts`;
  `--min-collect-value-wei` skips dust; `--poll-interval` controls cadence;
  `--once` runs a single sweep (cron-friendly). Bookkeeping marks use a detached
  context so an in-flight tx is always recorded even during shutdown.
- **Tested:** unit tests cover the selection/backoff logic
  (`provider_operator_collect_daemon_test.go`); `go build`/`go vet` clean.
- Remaining: a full streaming → metered RAV → auto-collect integration test
  against a running gateway + Postgres, and pending-record reconciliation
  (records stuck in `collect_pending` after a crash mid-collect).

## NET-05 Operate the Discovery Oracle

Context:

- Discovery uses a standalone oracle returning a curated provider whitelist and
  canonical pricing. Whitelist governance is admin/council-only YAML config,
  which is acceptable for soft-release.
- For a live service this oracle must actually be hosted with uptime, not just
  runnable locally.

Done when:

- The oracle is deployed as a reliable hosted service for Arb One with the
  curated provider set and canonical pricing.
- Whitelist/pricing update procedure is documented (deployment-managed config).
- Consumers can resolve a provider control-plane endpoint against it.

Verify:

- Consumer sidecar discovers and connects to a whitelisted Arb One provider via
  the hosted oracle.
- Document failover/availability expectations for the oracle.

## NET-06 Provider Runtime Hardening

Context:

- The provider ships as one distroless `sds` binary (gateway + plugin + operator
  + CLI) plus Postgres. Live payment binding is process-local, so only a single
  gateway replica is supported today (`PMVP-003`).
- There is no automatic runtime compatibility probe; the published
  `dummy-blockchain` images are stale and fail the SDS runtime path. Settlement
  keys are deliberately kept out of the gateway and used only by the collect CLI.

Done when:

- A compatible firehose-core/Substreams provider runtime profile is documented or
  published (resolve the stale `dummy-blockchain` image dependency for real
  chains).
- Provider monitoring/alerting (beyond basic Prometheus metrics) is defined.
- Settlement key custody for automated collection (NET-04) uses KMS/HSM or
  equivalent, not plaintext env vars.
- The single-replica constraint is documented for operators, or NET (PMVP-003)
  decoupling is scoped if multi-replica is required for launch.

Verify:

- A provider runs against a real Substreams runtime (not the devenv dummy chain)
  and serves a paid stream.
- Key-custody procedure reviewed.

## NET-07 Consumer Onboarding Path

Context:

- Consumers currently self-host the sidecar, fund their own escrow, and hold
  their own signer keys. This is technically demanding and unlike the centralized
  Gateway model used for subgraphs.
- For a soft-release with design partners, BYO sidecar may be acceptable; this
  task decides and documents the path.

Done when:

- A decision is recorded: self-hosted sidecar vs. a hosted gateway that pays on
  consumers' behalf.
- Consumer onboarding (escrow funding, signer authorization, sidecar config) is
  documented end-to-end for Arb One.

Verify:

- A new consumer can follow the docs to fund, authorize, and stream against a
  whitelisted provider on Arb One.

## NET-08 Permissionless On-Chain Registry Sourcing (Track B)

Context:

- Permissionless discovery requires the oracle (or the sidecar) to source the
  provider set from on-chain SDS registry data rather than a curated YAML
  whitelist. Named as a follow-up in MVP docs but not yet scoped as work.

Done when:

- Provider eligibility/metadata is read from on-chain registry data.
- Selection logic operates over the permissionless set with at least basic
  ranking.

Verify:

- A provider that self-registers on-chain becomes discoverable without manual
  whitelist edits.

## NET-09 Issuance / Rewards Governance (Track B)

Context:

- Only required if SDS providers are to earn indexing rewards/issuance. The
  whitelisted soft-release explicitly avoids this to ship without a council vote.

Done when:

- A GIP is drafted and approved by the Graph Council to include SDS in the
  issuance/rewards path.

Verify:

- SDS recognized in the rewards path on Arb One.

## NET-10 Provider Trust / Dispute / Verifiability Model (Track B)

Context:

- Permissionless operation removes the whitelist's implicit trust. Substreams
  currently have no POI/dispute equivalent, so a provider can serve incorrect
  data and still collect. Trustless operation depends on solving this.
- Related to broader ecosystem work on trustless sync/verification markets.

Done when:

- A verifiability or dispute/slashing mechanism for substreams output is designed
  and integrated, or an explicit alternative trust model is adopted.

Verify:

- A misbehaving permissionless provider can be challenged/penalized.

## NET-11 Harden SubstreamsDataService Before Audit

Context:

- The current `SubstreamsDataService` is, by its own header, "the strict minimum
  required for a DataService to work with GraphTallyCollector" — a testing-grade
  artifact, not a mainnet contract. It must be hardened before the NET-02 audit so
  the audit reviews production-intent code.
- Concrete issues found on review of
  `horizon/devenv/build/contracts/SubstreamsDataService.sol`:
  - `setProvisionTokensRange(uint256)` is `external` with **no access control**
    (comment: "for testing, we allow calling this directly"). On mainnet anyone
    could rewrite the data service's min/max provision bounds. Critical.
  - `slash(address, bytes)` is a **no-op** ("Slashing not implemented... Would
    require DisputeManager integration"). There is no economic security / no way
    to penalize a misbehaving provider. Acceptable under the whitelist trust model
    but must be a conscious decision, and is a hard blocker for permissionless
    (links to NET-10).
  - Upgradeability/initializer story is unclear: the constructor calls
    `_disableInitializers()` and no `initialize` is exposed beyond
    `setProvisionTokensRange`. Confirm whether it is deployed directly or behind a
    proxy, and that the chosen pattern is safe.

Done when:

- Privileged setters (`setProvisionTokensRange`, any parameter mutators) are
  access-controlled to a governance/owner role.
- A deliberate decision is recorded on slashing: implement DisputeManager
  integration, or explicitly accept no-slashing under the whitelist model.
- The deployment/upgradeability pattern is confirmed and safe.

Verify:

- Tests prove unauthorized callers cannot mutate provision parameters.
- Contract is frozen at a reviewed commit before handing to the NET-02 auditor.

Progress:

- **Access control added & verified.** `setProvisionTokensRange` is now gated by
  an `onlyOwner` modifier; `OWNER` is set to `msg.sender` (the deployer) at
  construction — no constructor/ABI change, so devenv (which deploys and calls
  the setter as the deployer) is unaffected. SDSCE controls this rather than the
  Graph governor, since SDSCE is unaffiliated. Proven on the Arb One fork: a
  non-owner call reverts `SubstreamsDataServiceNotOwner`; the owner succeeds; the
  full provision+register+collect flow still passes
  (`devel/arb-one-fork-rehearsal.sh`, `devel/arb-one-collect-rehearsal.sh`).
- **Slashing: deliberate no-op (decided).** `slash()` stays a no-op by design —
  SDSCE uses a whitelist trust model and bounds consumer risk to escrow, rather
  than on-chain slashing/disputes (which would need DisputeManager + a verifiable
  substreams-output model). Documented in the contract; revisit only for Track B
  / permissionless (NET-10).
- **Still open for the audit:** the initializer/upgradeability story. The
  constructor calls `_disableInitializers()` and never calls `__DataService_init`;
  it works in direct (non-proxy) deployment because the controller is immutable
  and the provision range is set post-deploy, but the deployment/upgrade pattern
  should be confirmed and frozen before audit.

Note: the contract artifact (`contracts/artifacts/SubstreamsDataService.json` and
the `horizon/devenv/contracts/` copy) was recompiled with solc 0.8.27, optimizer
disabled, evmVersion cancun — matching the original build settings — so the only
delta is the source change.
