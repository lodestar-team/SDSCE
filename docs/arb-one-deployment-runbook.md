# Arbitrum One Deployment Runbook (SDSCE)

How to deploy the **Substreams Data Service Community Edition (SDSCE)** on
**Arbitrum One (chain id `42161`)** and onboard a provider and a consumer against
the live Graph Horizon payment contracts.

> [!WARNING]
> **Read this first.** SDSCE is a community edition, unaffiliated with the Graph
> Foundation or Edge & Node, and the `SubstreamsDataService` contract is
> **unaudited**. This runbook targets **mainnet with real GRT**. Do not deploy to
> Arbitrum One until the contract has passed a security audit
> (`plans/network-readiness.md` NET-02). Rehearse everything on a fork first
> (see [Pre-Flight: Fork Rehearsal](#pre-flight-fork-rehearsal)). The settlement
> and payer keys move real funds — treat them accordingly.

## Verified Arbitrum One Addresses

Shared Horizon contracts are already live on Arb One and reused as-is. Source of
truth: `graphprotocol/contracts`,
`packages/horizon/ignition/deployments/horizon-arbitrumOne/deployed_addresses.json`.
**Re-verify against that registry before deploying.**

| Contract | Arbitrum One address |
| --- | --- |
| HorizonStaking (proxy) | `0x00669A4CF01450B64E8A2A20E9b1FCB71E61eF03` |
| GraphTallyCollector | `0x8f69F5C07477Ac46FBc491B1E6D91E2bb0111A9e` |
| PaymentsEscrow (proxy) | `0xf6Fcc27aAf1fcD8B254498c9794451d82afC673E` |
| GraphPayments (proxy) | `0x7Aae8ae011927BC36Cb4d0d3e81f2E6E30daE06D` |
| Controller | `0x0a8491544221dd212964fbb96487467291b2C97e` |
| L2GraphToken (GRT) | `0x9623063377AD1B27544C965cCd7342f7EA7e88C7` |

`SubstreamsDataService` is SDSCE-owned and deployed by you (see below).

## Roles and Keys

- **Deployer / owner** — deploys `SubstreamsDataService`. Becomes the contract
  `OWNER` (set to `msg.sender` at construction) and is the only address allowed to
  call `setProvisionTokensRange`. Needs Arb One ETH for gas.
- **Service provider** — the indexer address that stakes, provisions, registers,
  and serves data. Needs Arb One ETH for gas, and GRT to provision.
- **Payer** — the consumer's funding address. Needs ETH for gas and GRT for the
  escrow deposit.
- **Signer** — signs RAVs. Can equal the payer (simplest) or be a separate key
  authorized by the payer via `GraphTallyCollector.authorizeSigner`.
- **Settlement key** — submits `collect()` transactions; this is the service
  provider key. **Never mount it into the provider gateway deployment** — it lives
  only with the manual collect CLI or the collection daemon process.

## Pre-Flight: Fork Rehearsal

Before spending real GRT, run the full flow against a fork of live Arb One. These
scripts deploy `SubstreamsDataService` against the real Horizon contracts and
exercise the exact paths below, spending nothing:

```bash
export ARB_ONE_RPC=https://<your-arb-one-archive-rpc>

# provisioning + registration (reproduces the ProvisionNotFound revert, then
# stake -> provision -> register succeeds)
./devel/arb-one-fork-rehearsal.sh

# money path: escrow deposit -> authorize signer -> signed RAV -> collect()
./devel/arb-one-collect-rehearsal.sh
```

Both must print `PASS` before you touch mainnet.

## Build The SDS Image

```bash
docker build --build-arg VERSION=$(git rev-parse --short HEAD) -t <registry>/sdsce:<sha> .
```

One binary (`sds`) contains the consumer sidecar, provider gateway, operator API,
and all CLI subcommands. `pricing.yaml`, TLS certs, tokens, and the DSN are not in
the image — mount/inject them.

## Deploy SubstreamsDataService

Gated behind the NET-02 audit. The constructor takes `(controller,
graphTallyCollector)`; the deployer becomes `OWNER`.

```bash
export ARB_ONE_RPC=https://<your-arb-one-rpc>
CONTROLLER=0x0a8491544221dd212964fbb96487467291b2C97e
COLLECTOR=0x8f69F5C07477Ac46FBc491B1E6D91E2bb0111A9e
IMPL_BYTECODE=$(python3 -c "import json;print(json.load(open('contracts/artifacts/SubstreamsDataService.json'))['bytecode']['object'])")
PROXY_BYTECODE=$(python3 -c "import json;print(json.load(open('contracts/artifacts/ERC1967Proxy.json'))['bytecode']['object'])")

# 1. Deploy the implementation. (_disableInitializers() means it cannot be
#    initialized directly — that is correct; it is only used behind the proxy.)
cast send --rpc-url "$ARB_ONE_RPC" --private-key "$DEPLOYER_KEY" --create "$IMPL_BYTECODE" \
  "constructor(address,address)" "$CONTROLLER" "$COLLECTOR"
# record the deployed address as IMPL_ADDRESS

# 2. Deploy the ERC1967 proxy, ATOMICALLY initialized to the implementation.
#    OWNER_ADDRESS controls parameters AND upgrades — use a multisig/timelock for
#    production (audit L-02). NEVER deploy the proxy with empty init data and call
#    initialize() in a separate tx — that is front-runnable (audit L-03).
INIT_DATA=$(cast calldata "initialize(address,uint256)" "$OWNER_ADDRESS" 0)
cast send --rpc-url "$ARB_ONE_RPC" --private-key "$DEPLOYER_KEY" --create "$PROXY_BYTECODE" \
  "constructor(address,bytes)" "$IMPL_ADDRESS" "$INIT_DATA"
# record the PROXY address as SDS_ADDRESS — this is the data-service address users
# and providers interact with. The implementation address is never used directly.

# 3. Verify ownership landed as intended (and is the multisig).
cast call --rpc-url "$ARB_ONE_RPC" "$SDS_ADDRESS" "owner()(address)"
# initialize() already set the provision range to 0; to change it later (owner-only):
# cast send ... "$SDS_ADDRESS" "setProvisionTokensRange(uint256)" <min>
```

> The deployed implementation bytecode must match the audited commit (NET-02). The
> artifact is built with solc 0.8.27, optimizer disabled, evmVersion cancun; rebuild
> via the Dockerised `horizon/devenv/build` to reproduce it. Ownership transfer is
> two-step (`Ownable2Step`): the new owner must call `acceptOwnership()`.

## Upgrading the Contract

`SubstreamsDataService` is UUPS-upgradeable; upgrades are owner-authorized. To
upgrade, deploy a new implementation and call `upgradeToAndCall` on the **proxy**
from the owner:

```bash
# deploy NEW implementation with IDENTICAL constructor args (see warning below)
cast send ... --create "$NEW_IMPL_BYTECODE" "constructor(address,address)" "$CONTROLLER" "$COLLECTOR"
# owner upgrades the proxy (empty calldata if no re-init needed)
cast send --rpc-url "$ARB_ONE_RPC" --private-key "$OWNER_KEY" "$SDS_ADDRESS" \
  "upgradeToAndCall(address,bytes)" "$NEW_IMPL_ADDRESS" 0x
```

Upgrade discipline (from the audit):

- **Preserve immutables (L-01).** `controller` and `GRAPH_TALLY_COLLECTOR` are
  `immutable` — they live in the implementation bytecode, not proxy storage. Each
  new implementation **must** be built with byte-identical constructor args, or the
  proxy will silently use different values. After upgrading, assert
  `GRAPH_TALLY_COLLECTOR()` is unchanged.
- **Keep UUPS.** New implementations must keep inheriting `UUPSUpgradeable` with an
  `onlyOwner` `_authorizeUpgrade`, or upgradeability is lost permanently.
- **Re-initialization.** If a new version needs init logic, use `reinitializer(N)`
  with an incrementing `N` — never the `initializer` modifier again.
- **Storage layout (I-03).** Only append new state (the `__gap` reserves space);
  never reorder/insert. Validate layout with the OpenZeppelin Upgrades plugin before
  shipping.

## Provider Onboarding (stake → provision → register)

This is the real Horizon provisioning path. `register()` reverts with
`ProvisionManagerProvisionNotFound` until a provision exists — that is expected,
not a bug.

```bash
GRT=0x9623063377AD1B27544C965cCd7342f7EA7e88C7
STAKING=0x00669A4CF01450B64E8A2A20E9b1FCB71E61eF03
TOKENS=<provision amount in wei>

# 1. stake GRT into HorizonStaking
cast send --rpc-url "$ARB_ONE_RPC" --private-key "$PROVIDER_KEY" "$GRT" \
  "approve(address,uint256)" "$STAKING" "$TOKENS"
cast send --rpc-url "$ARB_ONE_RPC" --private-key "$PROVIDER_KEY" "$STAKING" \
  "stake(uint256)" "$TOKENS"

# 2. provision stake toward the data service (verifier = SDS_ADDRESS)
cast send --rpc-url "$ARB_ONE_RPC" --private-key "$PROVIDER_KEY" "$STAKING" \
  "provision(address,address,uint256,uint32,uint64)" \
  "$PROVIDER_ADDRESS" "$SDS_ADDRESS" "$TOKENS" <maxVerifierCut> <thawingPeriod>

# 3. register with the data service (data = abi.encode(paymentsDestination))
REG_DATA=$(cast abi-encode "f(address)" "$PROVIDER_ADDRESS")
cast send --rpc-url "$ARB_ONE_RPC" --private-key "$PROVIDER_KEY" "$SDS_ADDRESS" \
  "register(address,bytes)" "$PROVIDER_ADDRESS" "$REG_DATA"

# verify
cast call --rpc-url "$ARB_ONE_RPC" "$SDS_ADDRESS" "isRegistered(address)(bool)" "$PROVIDER_ADDRESS"
```

`maxVerifierCut`/`thawingPeriod` must satisfy the data service's accepted ranges.
The fork rehearsal uses `0, 0`.

## Consumer Onboarding (escrow + signer)

Use the `sds consumer` CLI (see `docs/operator-funding.md` for full flag detail):

```bash
# fund escrow for (collector, provider): approve GRT then deposit
sds consumer funding deposit \
  --rpc-endpoint="$ARB_ONE_RPC" --chain-id=42161 \
  --grt-token-address=0x9623063377AD1B27544C965cCd7342f7EA7e88C7 \
  --escrow-address=0xf6Fcc27aAf1fcD8B254498c9794451d82afC673E \
  --collector-address=0x8f69F5C07477Ac46FBc491B1E6D91E2bb0111A9e \
  --receiver-address="$PROVIDER_ADDRESS" \
  --payer-private-key-env=PAYER_KEY --amount="<N> GRT"

# authorize a signer (skip if payer signs its own RAVs and is already authorized)
sds consumer signer authorize \
  --rpc-endpoint="$ARB_ONE_RPC" --chain-id=42161 \
  --collector-address=0x8f69F5C07477Ac46FBc491B1E6D91E2bb0111A9e \
  --payer-private-key-env=PAYER_KEY --signer-address="$SIGNER_ADDRESS" ...
```

## Provider Gateway Deployment

- Provision Postgres and run migrations before first start/upgrade
  (`devel/migrate.sh up` with the `postgres://` DSN). Do not use `inmemory://`
  outside local tests.
- Mount `pricing.yaml` (`--pricing-config`); the image does not create it.
- Generate operator tokens (`openssl rand -hex 32`) and pass them by env-var name
  (`--operator-read-token-env`, `--admin-write-token-env`).
- TLS is the default posture; supply cert/key files outside local/dev.
- **Run a single gateway replica** — live payment-control binding is process-local
  (`PMVP-003`). Do not load-balance active streams across replicas.
- **Do not mount the settlement private key into the gateway.**

Run with chain id `42161`, the RPC endpoint, the SDS/collector/escrow addresses,
the service-provider address, the data-plane endpoint, and an operator listen
address.

## Automated Collection (NET-04)

Run the collection daemon as a **separate process** holding the settlement key. It
polls the operator API for collectible RAVs and submits `collect()` automatically:

```bash
sds provider operator collect-daemon \
  --provider-endpoint=https://<provider-operator-endpoint> \
  --operator-token-env=SDS_ADMIN_WRITE_TOKEN \
  --rpc-endpoint="$ARB_ONE_RPC" --chain-id=42161 \
  --data-service-address="$SDS_ADDRESS" \
  --data-service-cut-ppm=<cut> \
  --provider-private-key-env=PROVIDER_KEY \
  --poll-interval=30s --max-attempts=5 --retry-backoff-base=1m \
  --min-collect-value-wei=<dust floor> \
  --reclaim-pending-after=15m
```

Failed collects retry with exponential backoff (capped at 1h) up to
`--max-attempts`; `--min-collect-value-wei` avoids burning gas on dust;
`--once` runs a single sweep (cron-friendly). `--reclaim-pending-after` re-attempts
records stuck in `collect_pending` (e.g. after a crash mid-collect) — set it
comfortably above `--receipt-timeout`; re-attempting an already-settled RAV is a
safe zero-delta no-op. For one-off manual settlement use
`sds provider operator collect` (see `docs/direct-provider-testnet-public-runbook.md`).

## Troubleshooting

### `ProvisionManagerProvisionNotFound(address)` (selector `0x7b3c09bf`)
The provider has no Horizon provision toward `SDS_ADDRESS`. Complete
[Provider Onboarding](#provider-onboarding-stake--provision--register) first.

### `SubstreamsDataServiceNotOwner(address)`
`setProvisionTokensRange` was called by a non-owner. Only the deploying address
(`OWNER`) may set provision parameters.

### `collect()` reverts
Confirm: provider registered and provisioned, payer escrow funded for
(collector, provider), signer authorized by the payer, and the RAV's
`serviceProvider`/`dataService` match the provider and `SDS_ADDRESS`.

## Public Repo Safety Checklist

- No private keys, DSNs, RPC URLs, or operator tokens committed.
- Settlement key never in the gateway deployment manifest.
- Contract deployed from an audited commit; deployed bytecode matches the audit.

## Status

This runbook reflects flows proven against a fork of real Arbitrum One
(`devel/arb-one-fork-rehearsal.sh`, `devel/arb-one-collect-rehearsal.sh`) and the
devenv integration suite. Outstanding before a real mainnet deployment:
the NET-02 audit and the NET-11 initializer/upgradeability review
(`plans/network-readiness.md`).
