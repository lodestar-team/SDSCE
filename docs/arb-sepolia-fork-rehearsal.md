# Arbitrum Sepolia Fork Rehearsal — SubstreamsDataService

A full dry-run of deploying and exercising the **mainnet-hardened**
`SubstreamsDataService` (commit lineage `f28962f` / `net-02-audit-freeze`) against
an **anvil fork of Arbitrum Sepolia**, i.e. the real Sepolia Horizon contracts.
This is the prescribed substitute for a live testnet dry run
(`plans/network-readiness.md`, DECISION-REQUIRED) and proves the
provision → register → collect path (NET-03 / NET-04) without spending real funds.

## How to reproduce

```bash
# 1. Fork Arbitrum Sepolia
anvil --fork-url https://sepolia-rollup.arbitrum.io/rpc --port 8546 --chain-id 421614 &

# 2. Deploy the hardened contract via the parameterized script
export ARB_SEPOLIA_RPC=http://localhost:8546
export SDS_DEPLOYER_KEY=0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80  # anvil #0
export OWNER_ADDRESS=0x70997970C51812dc3A010C7d01b50e0d17dc79C8                          # anvil #1
./devel/deploy-arb-sepolia.sh

# 3. Drive register + collect (see commands in this doc / devel/forktest)
```

## Sepolia Horizon addresses (chain 421614)

| Contract | Address |
| --- | --- |
| Controller | `0x9DB3ee191681f092607035d9BDA6e59FbEaCa695` |
| GraphTallyCollector | `0x382863e7B662027117449bd2c49285582bbBd21B` |
| PaymentsEscrow | `0x4b5D3Da463F7E076bb7CDF5030960bf123245681` |
| GraphPayments | `0x57E70eC8905E26341d40aF60Dca56cDBA8C166E5` |
| GRT (L2) | `0xf8c05dCF59E8B28BFD5eed176C562bEbcfc7Ac04` |
| HorizonStaking (resolved via Controller) | `0x865365C425f3A593Ffe698D9c4E6707D14d51e08` |

## Results — all green

### 1. Deploy + hardening invariants
- impl + ERC1967 proxy deployed; atomic `initialize(owner, minProvision=0)`.
- `owner` == supplied owner; `GRAPH_TALLY_COLLECTOR` correct; `BURN_TAX_PPM == 10000` (1%).
- provision tokens range `0 .. type(uint256).max`.
- ERC1967 impl slot matches deployed implementation.
- implementation `initialize()` reverts `InvalidInitialization` (`0xf92ee8a9`) — initializers disabled.
- proxy re-`initialize()` reverts `InvalidInitialization`.
- `setProvisionTokensRange` from non-owner reverts `OwnableUnauthorizedAccount` (`0x118cdaa7`).

### 2. Real Horizon provision + register (NET-03)
- Provider `stake(100k GRT)` then `provision(provider, SDS, 100k, cut=0, thaw=0)` on real HorizonStaking.
- `getProviderTokensAvailable(provider, SDS)` == 100k GRT.
- `register(provider, abi.encode(dest))` → `isRegistered == true`, `paymentsDestination` set.

### 3. Real settlement / collect (NET-04)
Payer deposited 10k GRT into escrow `(payer, collector, provider)`, authorized a
signer, signed an EIP-712 RAV (value 1000 GRT) via the repo's own `horizon.Sign`,
provider called `collect(QueryFee, ...)`:

| Quantity | Value |
| --- | --- |
| Escrow drained | 1000 GRT (RAV value) |
| Protocol tax burned (GraphPayments, 1%) | 10 GRT |
| SDS data-service cut burned (`BURN_TAX_PPM`, 1% of remainder) | 9.9 GRT (`BurnTaxApplied` event) |
| Provider received | 980.1 GRT |
| SDS retained | 0 (burns its entire cut) |

`10 + 9.9 = 19.9` GRT burned (matched `totalSupply` delta); `980.1 + 19.9 = 1000`. Exact.

### 4. Negative guards
- `register()` with no provision → `ProvisionManagerProvisionNotFound` (`0x7b3c09bf`).
- `collect()` for non-provisioned indexer → `ProvisionManagerProvisionNotFound`.

## Note

These are local fork addresses (ephemeral anvil state) and are intentionally not
recorded as canonical deployments. The live public-Sepolia broadcast is a separate
step run with a funded deployer key — see the deploy script header.
