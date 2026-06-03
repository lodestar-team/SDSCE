# NET-02 — Security Audit Brief: SubstreamsDataService

A hand-off-ready brief for an external auditor of the SDSCE on-chain contract. The
audit is the gate to any Arbitrum One mainnet deployment (`plans/network-readiness.md`).

## Frozen Commit

- **Audit target commit:** `f28962f` — git tag **`net-02-audit-freeze`** in
  `lodestar-team/SDSCE`.
- The implementation bytecode deployed to mainnet must match this commit (rebuild
  reproducibly — see Build / Reproduction).
- An internal review was already performed: `docs/security-audit-substreams-data-service.md`
  (0 critical/high; 3 low + 3 informational, with remediation status). This brief and
  that report describe the same frozen contract.

## Summary

SDSCE adds **one** SDSCE-owned smart contract, `SubstreamsDataService`, on top of
The Graph's existing, separately-audited Horizon payment stack. It is a Horizon
"data service" verifier that lets a provisioned provider collect GraphTally (TAP)
query-fee RAVs and routes payment through the shared Horizon contracts. It is
**UUPS-upgradeable** behind an ERC1967 proxy and applies a **fixed 1% burn tax** on
collected query fees (the data-service cut is burned; 0% retained by the deployer).

## In Scope

- `horizon/devenv/build/contracts/SubstreamsDataService.sol` at commit `f28962f`.
- Its integration with the shared Horizon contracts it calls: `GraphTallyCollector`,
  `PaymentsEscrow`, `GraphPayments`, `HorizonStaking` (via the `@graphprotocol/horizon`
  `DataService`/`ProvisionManager` base), and `L2GraphToken` (for the burn).

## Out of Scope

- The shared Horizon contracts themselves (HorizonStaking, PaymentsEscrow,
  GraphPayments, GraphTallyCollector, Controller, L2GraphToken) — deployed and
  audited by The Graph; reused unmodified on Arbitrum One.
- Off-chain components except where their on-chain assumptions bear on contract safety.

## Build / Reproduction

- Compiler: **solc 0.8.27**, **optimizer disabled**, **evmVersion cancun**.
- Dependencies: `@graphprotocol/horizon` + `@graphprotocol/interfaces` pinned at
  `graphprotocol/contracts@41fd64b1f27bd9dd3fc5ca818eba63e4dcf6c73e`, OZ (upgradeable) v5.1.0.
- Reproduce both artifacts (`SubstreamsDataService`, `ERC1967Proxy`) via the Dockerised
  build in `horizon/devenv/build` (`build.sh`). Deployed impl bytecode must match `f28962f`.

## Contract Overview

`SubstreamsDataService is Initializable, Ownable2StepUpgradeable, UUPSUpgradeable,
ReentrancyGuardUpgradeable, DataService` (where `DataService` is `GraphDirectory,
ProvisionManager, DataServiceV1Storage, IDataService`). Deployed behind an ERC1967
proxy.

Storage / immutables:
- `GRAPH_TALLY_COLLECTOR` (immutable), Controller (immutable via `GraphDirectory`) —
  set in the implementation constructor.
- `isRegistered`, `paymentsDestination` mappings; `uint256[50] __gap`.
- `BURN_TAX_PPM = 10_000` (1%, constant).

Lifecycle:
- Constructor sets immutables and calls `_disableInitializers()` (implementation
  cannot be initialized directly).
- `initialize(initialOwner, minimumProvisionTokens)` (`initializer`): `__Ownable_init`,
  `__Ownable2Step_init`, `__UUPSUpgradeable_init`, `__ReentrancyGuard_init`,
  `__DataService_init`, then sets the provision range.
- `_authorizeUpgrade(address)` is `onlyOwner`.

Entry points and guards:

| Function | Guard | Notes |
| --- | --- | --- |
| `initialize` | `initializer` | sets owner + provision range on the proxy |
| `setProvisionTokensRange` | `onlyOwner` | min provision; max unbounded |
| `register` | `onlyAuthorizedForProvision` + `onlyValidProvision` | sets `isRegistered`; decodes + sets `paymentsDestination` (rejects zero) |
| `acceptProvisionPendingParameters` | `onlyAuthorizedForProvision` | accepts pending provision params |
| `startService` / `stopService` | — | no-ops |
| `collect` | `nonReentrant` + `onlyAuthorizedForProvision` + `onlyValidProvision` + `onlyRegisteredIndexer` | QueryFee only; **fixed 1% data-service cut, burned** |
| `slash` | — | **intentional no-op** (trust model) |
| `setPaymentsDestination` | — | `msg.sender` sets its own (rejects zero) |
| `transferOwnership` / `acceptOwnership` | `onlyOwner` / pending | two-step (`Ownable2Step`) |

Burn-tax flow in `collect()` → `_collectQueryFees()`: the provider-supplied cut is
ignored; the call passes `BURN_TAX_PPM` to `GraphTallyCollector.collect`. GraphPayments
routes `tokensDataService` (1% of the post-protocol-tax amount) to this contract; the
contract measures the GRT it received (balance delta) and **burns it** via
`IGraphToken.burn` (emitting `BurnTaxApplied`), retaining nothing.

## Trust Model & Deliberate Decisions

- Whitelisted soft-release; providers vetted off-chain.
- Consumer risk bounded by `PaymentsEscrow` balance.
- `slash()` is a deliberate no-op (documented; **not** a finding).
- Owner is an SDSCE-controlled address (intended multisig), controlling parameters
  **and** upgrades — not the Graph governor (SDSCE is unaffiliated).
- Fixed 1% data-service cut is burned (deflationary); deployer retains 0%.

## Areas of Particular Focus

1. **UUPS upgrade safety.** `_authorizeUpgrade` is `onlyOwner` — confirm no upgrade
   path bypasses it; confirm `_disableInitializers()` prevents implementation
   takeover; confirm storage layout (OZ namespaced for Ownable/UUPS/ReentrancyGuard/
   Initializable; sequential `DataServiceV1Storage` then the two mappings then `__gap`)
   supports safe future appends; confirm future re-init must use `reinitializer(N)`.
2. **Immutables across upgrades (L-01).** `controller`/`GRAPH_TALLY_COLLECTOR` live in
   implementation bytecode, not proxy storage — an upgrade with different args silently
   changes them. Assess the documented mitigation (identical args + post-upgrade assert)
   vs. moving them to initialized storage.
3. **Burn-tax accounting.** `collect()` burns the balance delta received during the
   external collector call. Confirm the balance-before/after accounting cannot be
   gamed (e.g. by direct GRT transfers into the contract or reentry — `collect` is
   `nonReentrant`), and that burning the received cut cannot revert/lock collection.
4. **Collection authorization & value flow.** `onlyAuthorizedForProvision` +
   `onlyValidProvision` + `onlyRegisteredIndexer`, `serviceProvider == indexer`, and
   payer-signed RAVs verified by the collector — confirm no unauthorized or
   double-collection path; confirm zero-`paymentsDestination` rejection is complete.
5. **Ownership.** Two-step (`Ownable2Step`) transfer; owner governs params + upgrades.
   Confirm there is no renounce/lock footgun and that a multisig owner is supported.
6. **Atomic initialization (L-03).** Confirm the deployment is only safe when the proxy
   is constructed with init calldata (front-running of a separate `initialize` would
   seize ownership). Deployment tooling and the runbook enforce this.

## Existing Test / PoC Harness

- `devel/arb-one-fork-rehearsal.sh` — provision + register against **real Arbitrum One**
  Horizon (Anvil fork); also exercises owner-gated `setProvisionTokensRange`.
- `devel/arb-one-collect-rehearsal.sh` — escrow → authorize signer → signed RAV →
  `collect()` → **burn** on a real-Arb-One fork (asserts data service retains 0 GRT and
  total supply drops).
- `go test ./test/integration -run 'TestCollectRAV'` and `'TestAuthorize|TestRevoke|TestUnauthorized'`.
- `go test ./test/integration -run TestFirecore` (with `SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE`
  from `devel/build-dummy-blockchain.sh`) — full streaming → metered RAV → collect → burn.

## Deliverables Requested

- Severity-classified findings with remediation guidance.
- Explicit sign-off on the UUPS upgrade pattern, storage layout, and the burn-tax
  accounting.
- Confirmation that consumer fund loss is bounded by escrow under the stated trust model.
- A reviewed/frozen commit hash whose bytecode matches the deployment artifact (expected: `f28962f`).

## References

- Contract: `horizon/devenv/build/contracts/SubstreamsDataService.sol`
- Internal audit: `docs/security-audit-substreams-data-service.md`
- Roadmap & decisions: `plans/network-readiness.md` (NET-02, NET-11, NET-10)
- Deployment, onboarding & upgrade procedure: `docs/arb-one-deployment-runbook.md`
- Contract roles & EIP-712 domain: `docs/contracts.md`
