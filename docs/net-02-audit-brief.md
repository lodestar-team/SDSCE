# NET-02 — Security Audit Brief: SubstreamsDataService

A hand-off-ready brief for an external auditor of the SDSCE on-chain contract. The
audit is the gate to any Arbitrum One mainnet deployment (`plans/network-readiness.md`).

## Summary

SDSCE (Substreams Data Service Community Edition) adds **one** SDSCE-owned smart
contract, `SubstreamsDataService`, on top of The Graph's existing, separately
audited Horizon payment stack. The contract is a minimal Horizon "data service"
verifier that lets a provisioned provider collect query-fee payments for
Substreams data via the GraphTally (TAP) Receipt Aggregate Voucher (RAV) flow.

This brief scopes the audit to that one contract and its interaction with the
shared Horizon contracts.

## In Scope

- `horizon/devenv/build/contracts/SubstreamsDataService.sol` (the only SDSCE-owned
  contract), at the reviewed/frozen commit handed to the auditor.
- Its integration surface with the shared Horizon contracts it calls:
  `GraphTallyCollector`, `PaymentsEscrow`, `GraphPayments`, `HorizonStaking`
  (via the `DataService` / `ProvisionManager` base from `@graphprotocol/horizon`).

## Out of Scope

- The shared Horizon contracts themselves (HorizonStaking, PaymentsEscrow,
  GraphPayments, GraphTallyCollector, Controller, L2GraphToken). These are
  deployed and maintained by The Graph and have been audited separately; SDSCE
  reuses them unmodified on Arbitrum One.
- Off-chain components (provider gateway, consumer sidecar, oracle, collection
  daemon) except where their on-chain assumptions bear on contract safety.

## Build / Reproduction

- Compiler: **solc 0.8.27**, **optimizer disabled**, **evmVersion cancun**.
- Dependencies: `@graphprotocol/horizon` and `@graphprotocol/interfaces` pinned at
  the commit in `horizon/devenv/build/Dockerfile`
  (`graphprotocol/contracts@41fd64b1f27bd9dd3fc5ca818eba63e4dcf6c73e`), OZ v5.1.0.
- Reproduce the artifact via the Dockerised build in `horizon/devenv/build`
  (`build.sh`). Deployed bytecode must match the audited commit.

## Contract Overview

`SubstreamsDataService is DataService` (which is `GraphDirectory, ProvisionManager,
DataServiceV1Storage, IDataService`). State it adds:

- `IGraphTallyCollector public immutable GRAPH_TALLY_COLLECTOR`
- `address public immutable OWNER` (set to `msg.sender` at construction)
- `mapping(address => bool) public isRegistered`
- `mapping(address => address) public paymentsDestination`

External/public entry points and their guards:

| Function | Guard | Notes |
| --- | --- | --- |
| `constructor(controller, graphTallyCollector)` | — | sets immutables, `OWNER = msg.sender`, `_disableInitializers()` |
| `setProvisionTokensRange(uint256)` | `onlyOwner` | sets `[min, type(uint256).max]` provision range |
| `register(indexer, data)` | `onlyAuthorizedForProvision` + `onlyValidProvision` | sets `isRegistered`, decodes `data` as `paymentsDestination` |
| `acceptProvisionPendingParameters(indexer, _)` | `onlyAuthorizedForProvision` | accepts pending provision params |
| `startService` / `stopService` | — | no-ops |
| `collect(indexer, paymentType, data)` | `onlyAuthorizedForProvision` + `onlyValidProvision` + `onlyRegisteredIndexer` | QueryFee only; decodes `(SignedRAV, dataServiceCut)`; calls collector |
| `slash(_, _)` | — | **intentional no-op** (see trust model) |
| `setPaymentsDestination(dest)` | — | `msg.sender` sets its own destination |

Payment path: `collect()` → `_collectQueryFees()` →
`GRAPH_TALLY_COLLECTOR.collect(QueryFee, abi.encode(signedRav, dataServiceCut, paymentsDestination[indexer]), 0)`.
It requires `signedRav.rav.serviceProvider == indexer`.

## Trust Model & Deliberate Decisions

The auditor should evaluate against this intended model, not assume a fully
trustless one:

1. **Whitelisted soft-release.** Providers are vetted off-chain (oracle whitelist),
   not permissionlessly. The contract does not gate provider eligibility itself.
2. **Consumer risk is bounded by escrow.** A consumer can lose at most what it
   deposited into `PaymentsEscrow` for a (collector, provider) pair.
3. **No on-chain slashing — by design.** `slash()` is a deliberate no-op: SDSCE
   has no DisputeManager integration and no verifiable-output model for Substreams.
   This is acceptable under the whitelist model and is a known, documented decision
   (not an oversight). The audit should confirm the *absence* of slashing does not
   enable fund loss beyond the escrow bound, not flag the no-op itself.
4. **SDSCE-controlled owner.** Privileged parameters are controlled by `OWNER`
   (the deployer), deliberately *not* the Graph governor, because SDSCE is
   unaffiliated with The Graph.

## Areas of Particular Focus

Findings/questions already identified internally that the audit should resolve:

1. **Initializer / upgradeability (highest priority).** The constructor calls
   `_disableInitializers()` and the contract never calls `__DataService_init` /
   `__ProvisionManager_init_unchained`. It functions in **direct (non-proxy)
   deployment** because the controller is immutable and the provision range is set
   post-deploy via `setProvisionTokensRange`. Confirm: (a) no required base-contract
   state is left uninitialized in a way that affects `register`/`collect`/provision
   validation; (b) the intended deployment is direct and non-upgradeable, and the
   pattern is safe; (c) there is no path to (re)initialize or hijack the implementation.
2. **Access control on privileged setters.** `setProvisionTokensRange` is
   `onlyOwner` (`OWNER = msg.sender`, immutable). Confirm no other state-mutating
   function lacks appropriate authorization, and that the owner model is sufficient
   (no transfer/renounce is provided — is that intended?).
3. **`collect()` correctness & value flow.** Verify: the `(SignedRAV,
   dataServiceCut)` decoding; the `serviceProvider == indexer` check; that
   `paymentsDestination[indexer]` cannot be abused to misroute funds; behavior when
   `paymentsDestination` is unset (zero address); `dataServiceCut` bounds (the
   off-chain CLI caps at 1e6 PPM — is the contract itself safe for arbitrary cut
   values passed directly?); and reentrancy across the external collector call.
4. **Provision gates.** `onlyAuthorizedForProvision` + `onlyValidProvision` +
   `onlyRegisteredIndexer` — confirm these correctly bind collection to a real,
   in-range Horizon provision and a registered provider, with no bypass.
5. **Registration data handling.** `register` decodes `data` as a single address
   (`paymentsDestination`); confirm malformed/oversized `data` cannot cause
   unexpected behavior, and that re-registration semantics are safe.
6. **Interaction assumptions with shared Horizon contracts** on Arbitrum One
   (payment cuts, protocol tax, delegation routing) — confirm SDSCE makes no
   incorrect assumptions about GraphPayments distribution.

## Existing Test / PoC Harness (for the auditor)

The contract is exercised end-to-end; auditors can reuse these:

- `devel/arb-one-fork-rehearsal.sh` — provision + register against **real
  Arbitrum One** Horizon contracts (Anvil fork); reproduces the
  `ProvisionManagerProvisionNotFound` revert then a successful register.
- `devel/arb-one-collect-rehearsal.sh` — escrow deposit → authorize signer →
  signed RAV → `collect()` settling on a real-Arb-One fork.
- `go test ./test/integration -run 'TestCollectRAV'` — collect + incremental
  collect against the devenv.
- `go test ./test/integration -run 'TestAuthorize|TestRevoke|TestUnauthorized'` —
  signer authorization lifecycle.
- `go test ./test/integration -run TestFirecore` (with
  `SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE` from `devel/build-dummy-blockchain.sh`) —
  full streaming → metered RAV → on-chain collect.

## Deliverables Requested

- Findings classified by severity (critical/high/medium/low/informational) with
  remediation guidance.
- Explicit sign-off on the initializer/upgradeability pattern (focus area 1).
- Confirmation that consumer fund loss is bounded by escrow under the stated trust
  model.
- A reviewed/frozen commit hash whose bytecode matches the deployment artifact.

## References

- Contract: `horizon/devenv/build/contracts/SubstreamsDataService.sol`
- Roadmap & decisions: `plans/network-readiness.md` (NET-02, NET-11, NET-10)
- Contract roles & EIP-712 domain: `docs/contracts.md`
- Deployment & onboarding: `docs/arb-one-deployment-runbook.md`
- Verified Arbitrum One Horizon addresses: `docs/arb-one-deployment-runbook.md`
