#!/usr/bin/env bash
#
# NET-03 (money path): prove SubstreamsDataService.collect() settles a signed RAV
# against a fork of REAL Arbitrum One Horizon contracts (HorizonStaking,
# PaymentsEscrow, GraphTallyCollector, GraphPayments), spending no real GRT.
#
# Flow: deploy -> provision+register provider -> fund payer + escrow deposit ->
# authorize signer -> sign RAV -> collect() -> assert tokensCollected increased.
#
# EIP-712 artifacts (auth proof, signed-RAV collect data) come from the SDSCE
# signtool (devel/sdsce-signtool), so we exercise the real encoders; cast does
# all on-chain submission.
#
# Requires: foundry (anvil, cast), go, python3, an Arbitrum One archive RPC.
# Usage:
#   ARB_ONE_RPC=https://your-arb-one-rpc ./devel/arb-one-collect-rehearsal.sh
#
set -euo pipefail

ARB_ONE_RPC="${ARB_ONE_RPC:?set ARB_ONE_RPC to an Arbitrum One archive RPC URL}"
FORK_BLOCK="${FORK_BLOCK:-469557443}"
RPC="http://localhost:8545"
ARTIFACT="contracts/artifacts/SubstreamsDataService.json"
PROXY_ARTIFACT="contracts/artifacts/ERC1967Proxy.json"

# --- Verified Arbitrum One Horizon addresses (graphprotocol/contracts, horizon-arbitrumOne) ---
CONTROLLER=0x0a8491544221dd212964fbb96487467291b2C97e
COLLECTOR=0x8f69F5C07477Ac46FBc491B1E6D91E2bb0111A9e
STAKING=0x00669A4CF01450B64E8A2A20E9b1FCB71E61eF03
ESCROW=0xf6Fcc27aAf1fcD8B254498c9794451d82afC673E
GRT=0x9623063377AD1B27544C965cCd7342f7EA7e88C7

# --- Anvil default accounts ---
DEPLOYER_PK=0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80
PROVIDER=0x70997970C51812dc3A010C7d01b50e0d17dc79C8
PROVIDER_PK=0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d
PAYER=0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC
PAYER_PK=0x5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a

STAKE_TOKENS=1000000000000000000000   # 1000 GRT provisioned
ESCROW_TOKENS=10000000000000000000    # 10 GRT escrow
RAV_VALUE=1000000000000000000         # 1 GRT collect
CUT_PPM=100000                        # 10%
DEADLINE=1893456000                   # 2030-01-01, well past fork block time
COLLECTION_ID=0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef

ANVIL_PID=""
cleanup() { [ -n "$ANVIL_PID" ] && kill "$ANVIL_PID" 2>/dev/null || true; }
trap cleanup EXIT
step() { printf '\n=== %s ===\n' "$1"; }
send() { cast send --rpc-url "$RPC" "$@" >/dev/null; }

step "Build SDSCE signtool"
go build -o /tmp/sdsce-signtool ./devel/sdsce-signtool
echo "ok"

step "Fork Arbitrum One at block $FORK_BLOCK"
anvil --fork-url "$ARB_ONE_RPC" --fork-block-number "$FORK_BLOCK" --port 8545 --silent &
ANVIL_PID=$!
for i in $(seq 1 30); do cast chain-id --rpc-url "$RPC" >/dev/null 2>&1 && break; sleep 1; done
[ "$(cast chain-id --rpc-url "$RPC")" = "42161" ] || { echo "fork is not Arb One"; exit 1; }
echo "fork up @ $(cast block-number --rpc-url "$RPC")"

step "Deploy SubstreamsDataService (UUPS proxy) + set provision range"
OWNER=$(cast wallet address "$DEPLOYER_PK")
IMPL_BYTECODE=$(python3 -c "import json;print(json.load(open('$ARTIFACT'))['bytecode']['object'])")
PROXY_BYTECODE=$(python3 -c "import json;print(json.load(open('$PROXY_ARTIFACT'))['bytecode']['object'])")
IMPL=$(cast send --rpc-url "$RPC" --private-key "$DEPLOYER_PK" --create "$IMPL_BYTECODE" \
  "constructor(address,address)" "$CONTROLLER" "$COLLECTOR" --json | python3 -c "import sys,json;print(json.load(sys.stdin)['contractAddress'])")
INIT_DATA=$(cast calldata "initialize(address,uint256)" "$OWNER" 0)
SDS=$(cast send --rpc-url "$RPC" --private-key "$DEPLOYER_PK" --create "$PROXY_BYTECODE" \
  "constructor(address,bytes)" "$IMPL" "$INIT_DATA" --json | python3 -c "import sys,json;print(json.load(sys.stdin)['contractAddress'])")
# owner-gated (NET-11): deployer is OWNER (set in initialize), so this re-assert is authorized
send --private-key "$DEPLOYER_PK" "$SDS" "setProvisionTokensRange(uint256)" 0
echo "SDS proxy: $SDS (impl $IMPL, owner $OWNER)"

step "Fund provider + payer with GRT (impersonate staking on the fork)"
for acct in "$PROVIDER" "$PAYER" "$STAKING"; do
  cast rpc --rpc-url "$RPC" anvil_setBalance "$acct" 0xde0b6b3a7640000 >/dev/null
done
cast rpc --rpc-url "$RPC" anvil_impersonateAccount "$STAKING" >/dev/null
send --from "$STAKING" --unlocked "$GRT" "transfer(address,uint256)(bool)" "$PROVIDER" "$STAKE_TOKENS"
send --from "$STAKING" --unlocked "$GRT" "transfer(address,uint256)(bool)" "$PAYER" "$ESCROW_TOKENS"
cast rpc --rpc-url "$RPC" anvil_stopImpersonatingAccount "$STAKING" >/dev/null
echo "provider GRT: $(cast call --rpc-url "$RPC" "$GRT" 'balanceOf(address)(uint256)' "$PROVIDER")"
echo "payer GRT:    $(cast call --rpc-url "$RPC" "$GRT" 'balanceOf(address)(uint256)' "$PAYER")"

step "Provider: stake -> provision -> register"
send --private-key "$PROVIDER_PK" "$GRT" "approve(address,uint256)(bool)" "$STAKING" "$STAKE_TOKENS"
send --private-key "$PROVIDER_PK" "$STAKING" "stake(uint256)" "$STAKE_TOKENS"
send --private-key "$PROVIDER_PK" "$STAKING" "provision(address,address,uint256,uint32,uint64)" "$PROVIDER" "$SDS" "$STAKE_TOKENS" 0 0
REG_DATA=$(cast abi-encode "f(address)" "$PROVIDER")
send --private-key "$PROVIDER_PK" "$SDS" "register(address,bytes)" "$PROVIDER" "$REG_DATA"
echo "isRegistered: $(cast call --rpc-url "$RPC" "$SDS" 'isRegistered(address)(bool)' "$PROVIDER")"

step "Payer: approve + deposit escrow (receiver = provider)"
send --private-key "$PAYER_PK" "$GRT" "approve(address,uint256)(bool)" "$ESCROW" "$ESCROW_TOKENS"
send --private-key "$PAYER_PK" "$ESCROW" "deposit(address,address,uint256)" "$COLLECTOR" "$PROVIDER" "$ESCROW_TOKENS"
echo "escrow balance: $(cast call --rpc-url "$RPC" "$ESCROW" 'getBalance(address,address,address)(uint256)' "$PAYER" "$COLLECTOR" "$PROVIDER")"

step "Authorize signer (payer self-signs: signer = payer)"
PROOF=$(/tmp/sdsce-signtool proof --chain-id 42161 --collector "$COLLECTOR" \
  --deadline "$DEADLINE" --authorizer "$PAYER" --signer-key "$PAYER_PK")
send --private-key "$PAYER_PK" "$COLLECTOR" "authorizeSigner(address,uint256,bytes)" "$PAYER" "$DEADLINE" "$PROOF"
echo "isAuthorized(payer,payer): $(cast call --rpc-url "$RPC" "$COLLECTOR" 'isAuthorized(address,address)(bool)' "$PAYER" "$PAYER")"

step "Build signed-RAV collect data + read tokensCollected before"
COLLECT_DATA=$(/tmp/sdsce-signtool collect-data --chain-id 42161 --collector "$COLLECTOR" \
  --collection-id "$COLLECTION_ID" --payer "$PAYER" --service-provider "$PROVIDER" \
  --data-service "$SDS" --value "$RAV_VALUE" --data-service-cut "$CUT_PPM" --signer-key "$PAYER_PK")
BEFORE=$(cast call --rpc-url "$RPC" "$COLLECTOR" "tokensCollected(address,bytes32,address,address)(uint256)" "$SDS" "$COLLECTION_ID" "$PROVIDER" "$PAYER" | awk '{print $1}')
echo "tokensCollected before: $BEFORE"

step "SubstreamsDataService.collect(provider, QueryFee=0, data)"
send --private-key "$PROVIDER_PK" "$SDS" "collect(address,uint8,bytes)" "$PROVIDER" 0 "$COLLECT_DATA"
AFTER=$(cast call --rpc-url "$RPC" "$COLLECTOR" "tokensCollected(address,bytes32,address,address)(uint256)" "$SDS" "$COLLECTION_ID" "$PROVIDER" "$PAYER" | awk '{print $1}')
echo "tokensCollected after:  $AFTER"

printf '\n=== RESULT ===\n'
DELTA=$(python3 -c "print($AFTER - $BEFORE)")
echo "collected delta: $DELTA (expected $RAV_VALUE)"
[ "$DELTA" = "$RAV_VALUE" ] || { echo "FAIL: collect did not settle the expected amount"; exit 1; }
echo "PASS: collect() settled a signed RAV against real Arbitrum One Horizon contracts."
