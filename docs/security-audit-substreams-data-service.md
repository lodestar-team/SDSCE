# Security Audit — SubstreamsDataService (SDSCE)

**Target:** `horizon/devenv/build/contracts/SubstreamsDataService.sol`
**Type:** UUPS-upgradeable Horizon data service (Solidity 0.8.27, Arbitrum One)
**Date:** 2026-06-03
**Auditor:** internal (SDSCE)
**Commit basis:** working tree at audit time; OZ v5.1.0, horizon-contracts @ `41fd64b`

> Internal audit. SDSCE is a community edition unaffiliated with The Graph. This
> complements, and does not replace, an external third-party audit before mainnet
> (`docs/net-02-audit-brief.md`).

## 1. Executive Summary

`SubstreamsDataService` is a small (~190 LOC) Horizon data-service verifier that
lets a provisioned provider collect GraphTally (TAP) query-fee RAVs and routes the
payment through the shared, separately-audited Horizon contracts. The contract is
now UUPS-upgradeable behind an ERC1967 proxy.

**No Critical or High severity issues were found.** The contract correctly
implements the OpenZeppelin UUPS pattern and the Horizon data-service gates; the
paths that move funds are bounded by consumer escrow and are provider-self-affecting,
not avenues to steal third-party funds. The findings are concentrated in
**upgrade governance and operational discipline** (Low) and **defensive hardening**
(Informational) — the new surface area introduced by upgradeability.

| ID | Title | Severity |
| --- | --- | --- |
| L-01 | Immutable `controller`/`GRAPH_TALLY_COLLECTOR` are re-set by each implementation and not preserved on-chain | Low |
| L-02 | Single-step ownership (no `Ownable2Step`) on an owner that controls upgrades and parameters | Low |
| L-03 | Initialization must be atomic with proxy deployment or `initialize` is front-runnable | Low |
| I-01 | `collect()` does not bound `dataServiceCut` or reject a zero `paymentsDestination` | Informational |
| I-02 | No reentrancy guard on `collect()` | Informational |
| I-03 | No automated storage-layout/upgrade-compatibility validation in CI | Informational |

## 2. Scope

**In scope:** `SubstreamsDataService.sol` and its integration assumptions with the
shared Horizon contracts it calls.

**Out of scope:** `GraphTallyCollector`, `PaymentsEscrow`, `GraphPayments`,
`HorizonStaking`, `Controller`, `L2GraphToken` — deployed and separately audited by
The Graph, reused unmodified on Arbitrum One.

## 3. Methodology

Manual line-by-line review plus pattern matching against the framework's Solidity
proxy (`fv-sol-7`) and access-control (`fv-sol-4`) references. Findings were
checked against the live behaviour proven by the existing harness on a fork of real
Arbitrum One (`devel/arb-one-fork-rehearsal.sh`, `devel/arb-one-collect-rehearsal.sh`)
and the devenv integration suite (`TestCollectRAV`, authorization tests,
`TestFirecore`).

## 4. Trust Model (as designed)

- Whitelisted soft-release; providers vetted off-chain.
- Consumer risk is bounded by their `PaymentsEscrow` balance.
- `slash()` is a deliberate no-op (documented trust-model decision; **not** treated
  as a finding).
- The owner is an SDSCE-controlled address (not the Graph governor) and controls
  both parameters and upgrades.

## 5. Positive Controls Verified

- Implementation constructor calls `_disableInitializers()` — the implementation
  cannot be initialized directly (mitigates the classic UUPS implementation-takeover).
- `initialize` is guarded by the OZ `initializer` modifier.
- `_authorizeUpgrade` is gated by `onlyOwner` — upgrades are access-controlled.
- `uint256[50] __gap` reserves space for safe future state appends.
- Collection is gated by `onlyAuthorizedForProvision` + `onlyValidProvision` +
  `onlyRegisteredIndexer`, and `_collectQueryFees` enforces
  `signedRav.rav.serviceProvider == indexer`. A provider cannot collect against
  another provider's provision, and RAVs are payer-signed and verified by the
  GraphTallyCollector. Double-collection is prevented by the collector's cumulative
  `tokensCollected` accounting (re-collect yields a zero delta).

## 6. Findings

### L-01 — Immutable `controller`/`GRAPH_TALLY_COLLECTOR` are re-set by each implementation

**Severity:** Low · **Impact:** Medium · **Probability:** Low · **Confidence:** High

**Locations:** `SubstreamsDataService.sol` — `GRAPH_TALLY_COLLECTOR` (immutable),
`controller` via `DataService`/`GraphDirectory` (immutable), constructor.

**Description:** Both `GRAPH_TALLY_COLLECTOR` and the Controller are `immutable`,
i.e. embedded in the *implementation* bytecode, not in proxy storage. Under UUPS,
every upgrade deploys a new implementation whose constructor re-establishes these
values. An upgrade that deploys an implementation built with different (or zero)
constructor arguments would silently change the collector/controller the proxy
uses, with no storage migration and no obvious signal. Because the user's goal is
ongoing upgradeability, this is a standing operational hazard rather than a one-off.

**Impact:** A botched upgrade could point fee collection at the wrong collector or a
zero address, breaking settlement or misrouting the data-service's own collection
calls. It does not let an attacker steal third-party funds (collection remains
gated and escrow-bounded), but it can brick or misdirect the service.

**Recommendation:** Either (a) treat the immutable constructor args as part of the
release contract — verify in the upgrade procedure that each new implementation is
built with byte-identical `controller`/`collector` args, and assert post-upgrade
that `GRAPH_TALLY_COLLECTOR`/controller are unchanged; or (b) move these to
initialized storage (set in `initialize`, with a setter gated by `onlyOwner`) so the
values survive upgrades independent of implementation bytecode. (a) preserves the
Graph `GraphDirectory` convention; (b) is safer for a contract intended to be
upgraded repeatedly.

### L-02 — Single-step ownership on an upgrade- and parameter-controlling owner

**Severity:** Low · **Impact:** High · **Probability:** Low · **Confidence:** High

**Locations:** inherits `OwnableUpgradeable` (`transferOwnership`, single-step);
owner gates `setProvisionTokensRange` and `_authorizeUpgrade`.

**Description:** The owner is the sole authority for both parameters and contract
upgrades. `OwnableUpgradeable.transferOwnership` is single-step: a transfer to a
mistyped or uncontrolled address immediately and irrevocably hands over (or loses)
all parameter and upgrade authority. There is no acceptance step.

**Recommendation:** Use `Ownable2StepUpgradeable` so ownership transfer requires the
new owner to accept, eliminating fat-finger loss of upgrade control. For production,
consider making the owner a multisig/timelock.

### L-03 — Initialization must be atomic with proxy deployment

**Severity:** Low · **Impact:** High · **Probability:** Low · **Confidence:** High

**Locations:** deployment procedure; `initialize`.

**Description:** `initialize` sets the owner. If the ERC1967 proxy is deployed with
empty constructor data and `initialize` is sent in a separate transaction, an
attacker can front-run it to become owner — and thereby control upgrades (full
compromise). The current tooling deploys the proxy *with* the init calldata in its
constructor (`devel/arb-one-*-rehearsal.sh`, `horizon/devenv/devenv.go`), which is
atomic and safe.

**Recommendation:** Mandate atomic deployment (init calldata passed to the proxy
constructor) in the deployment runbook and never expose a window where the proxy is
live but uninitialized. Post-deploy, assert `owner()` is the intended address.

### I-01 — `collect()` does not bound `dataServiceCut` or reject zero `paymentsDestination`

**Severity:** Informational · **Confidence:** High

**Description:** `collect()` forwards `dataServiceCut` to the GraphTallyCollector
without an explicit `<= 1_000_000` PPM check (the off-chain CLI caps it; a direct
caller is not bound by the contract), and `_collectQueryFees` passes
`paymentsDestination[indexer]` even when it is the zero address. Both are
provider-controlled and provider-self-affecting (a bad cut or zero destination harms
only the calling provider's own payout; PPM bounds are enforced downstream by the
collector/`PPMMath`). No third party is harmed.

**Recommendation:** Defensive only: bound `dataServiceCut <= 1_000_000` and require a
non-zero `paymentsDestination` in-contract to fail fast with clear errors.

### I-02 — No reentrancy guard on `collect()`

**Severity:** Informational · **Confidence:** High

**Description:** `collect()` makes an external call to the (trusted) GraphTallyCollector.
No contract-local mutable state is written in `collect`/`_collectQueryFees`, so there
is no state to corrupt via reentry, and the collector is a trusted Horizon contract.

**Recommendation:** Optional `nonReentrant` for defence-in-depth if future versions
add post-call state mutations.

### I-03 — No automated storage-layout / upgrade-compatibility validation

**Severity:** Informational · **Confidence:** High

**Description:** Storage layout is sound today (OZ v5 namespaced storage for
Ownable/UUPS/Initializable, sequential `DataServiceV1Storage` then the contract's two
mappings then `__gap`). But there is no CI gate validating storage-layout
compatibility across upgrades, and future versions must use `reinitializer(N)` (not
`initializer`) and must keep inheriting `UUPSUpgradeable` with an `onlyOwner`
`_authorizeUpgrade`.

**Recommendation:** Add the OZ Upgrades plugin storage-layout validation to CI, and
document the upgrade checklist (reinitializer versioning, preserve inheritance order,
preserve immutables per L-01).

## 7. Conclusion

The contract is minimal, correctly follows the OZ UUPS pattern, and gates collection
such that the fund-moving paths cannot be turned against third parties — consumer
exposure is escrow-bounded and provider actions are self-affecting. No Critical/High
issues. The residual risk is governance/operational, introduced by upgradeability
itself: protect the owner key (L-02), keep deployments atomic (L-03), and treat
implementation immutables and storage layout as part of a disciplined upgrade
process (L-01, I-03). Addressing L-01–L-03 before mainnet is recommended; the
Informational items are defensive.
