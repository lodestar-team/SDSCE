#!/usr/bin/env bash
#
# NET-03 rehearsal: prove the SubstreamsDataService provisioning + registration
# path against a fork of REAL Arbitrum One Horizon contracts, spending no real GRT.
#
# It reproduces the documented gap (register() reverts with
# ProvisionManagerProvisionNotFound when no provision exists) and then closes the
# loop: stake -> provision -> register() succeeds.
#
# Requires: foundry (anvil, cast), python3, an Arbitrum One archive RPC.
# Usage:
#   ARB_ONE_RPC=https://your-arb-one-rpc ./devel/arb-one-fork-rehearsal.sh
#
set -euo pipefail

ARB_ONE_RPC="${ARB_ONE_RPC:?set ARB_ONE_RPC to an Arbitrum One archive RPC URL}"
FORK_BLOCK="${FORK_BLOCK:-469557443}"
RPC="http://localhost:8545"
ARTIFACT="contracts/artifacts/SubstreamsDataService.json"

# --- Verified Arbitrum One Horizon addresses (graphprotocol/contracts, horizon-arbitrumOne) ---
CONTROLLER=0x0a8491544221dd212964fbb96487467291b2C97e
COLLECTOR=0x8f69F5C07477Ac46FBc491B1E6D91E2bb0111A9e
STAKING=0x00669A4CF01450B64E8A2A20E9b1FCB71E61eF03
GRT=0x9623063377AD1B27544C965cCd7342f7EA7e88C7

# --- Anvil default account[1] as the provider ---
PROVIDER=0x70997970C51812dc3A010C7d01b50e0d17dc79C8
PROVIDER_PK=0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d
DEPLOYER_PK=0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80
TOKENS=1000000000000000000000 # 1000 GRT

ANVIL_PID=""
cleanup() { [ -n "$ANVIL_PID" ] && kill "$ANVIL_PID" 2>/dev/null || true; }
trap cleanup EXIT

step() { printf '\n=== %s ===\n' "$1"; }

step "Forking Arbitrum One at block $FORK_BLOCK"
anvil --fork-url "$ARB_ONE_RPC" --fork-block-number "$FORK_BLOCK" --port 8545 --silent &
ANVIL_PID=$!
for i in $(seq 1 30); do cast chain-id --rpc-url "$RPC" >/dev/null 2>&1 && break; sleep 1; done
[ "$(cast chain-id --rpc-url "$RPC")" = "42161" ] || { echo "fork is not Arb One"; exit 1; }
echo "fork up: chain 42161 @ $(cast block-number --rpc-url "$RPC")"

step "Deploy SubstreamsDataService(controller, collector) against live Horizon"
BYTECODE=$(python3 -c "import json;print(json.load(open('$ARTIFACT'))['bytecode']['object'])")
SDS=$(cast send --rpc-url "$RPC" --private-key "$DEPLOYER_PK" --create "$BYTECODE" \
  "constructor(address,address)" "$CONTROLLER" "$COLLECTOR" --json | python3 -c "import sys,json;print(json.load(sys.stdin)['contractAddress'])")
echo "deployed: $SDS"

step "setProvisionTokensRange access control (NET-11)"
NOTOWNER_SEL=$(cast sig "SubstreamsDataServiceNotOwner(address)")
echo "-- non-owner (provider) call -> expect SubstreamsDataServiceNotOwner ($NOTOWNER_SEL)"
if cast send --rpc-url "$RPC" --private-key "$PROVIDER_PK" "$SDS" "setProvisionTokensRange(uint256)" 0 2>/tmp/owner_err; then
  echo "UNEXPECTED: non-owner set the provision range"; exit 1
fi
grep -q "${NOTOWNER_SEL#0x}" /tmp/owner_err && echo "OK: non-owner rejected with SubstreamsDataServiceNotOwner" || { echo "reverted for the wrong reason:"; cat /tmp/owner_err; exit 1; }
echo "-- owner (deployer) call -> expect success"
cast send --rpc-url "$RPC" --private-key "$DEPLOYER_PK" "$SDS" "setProvisionTokensRange(uint256)" 0 >/dev/null
echo "range: $(cast call --rpc-url "$RPC" "$SDS" "getProvisionTokensRange()(uint256,uint256)" | tr '\n' ' ')"

step "register() with NO provision  -> expect ProvisionManagerProvisionNotFound"
DATA=$(cast abi-encode "f(address)" "$PROVIDER")
if cast send --rpc-url "$RPC" --private-key "$PROVIDER_PK" "$SDS" "register(address,bytes)" "$PROVIDER" "$DATA" 2>/tmp/reg_err; then
  echo "UNEXPECTED: register() succeeded without a provision"; exit 1
fi
grep -q "0x7b3c09bf\|ProvisionNotFound" /tmp/reg_err && echo "OK: reverted ProvisionManagerProvisionNotFound (selector 0x7b3c09bf)" || { echo "reverted for the wrong reason:"; cat /tmp/reg_err; exit 1; }

step "Fund provider with GRT (impersonate staking on the fork; net-neutral via stake())"
cast rpc --rpc-url "$RPC" anvil_setBalance "$PROVIDER" 0xde0b6b3a7640000 >/dev/null
cast rpc --rpc-url "$RPC" anvil_setBalance "$STAKING"  0xde0b6b3a7640000 >/dev/null
cast rpc --rpc-url "$RPC" anvil_impersonateAccount "$STAKING" >/dev/null
cast send --rpc-url "$RPC" --from "$STAKING" --unlocked "$GRT" "transfer(address,uint256)(bool)" "$PROVIDER" "$TOKENS" >/dev/null
cast rpc --rpc-url "$RPC" anvil_stopImpersonatingAccount "$STAKING" >/dev/null
echo "provider GRT: $(cast call --rpc-url "$RPC" "$GRT" "balanceOf(address)(uint256)" "$PROVIDER")"

step "Provider: approve -> stake -> provision toward SDS (cut=0, thaw=0)"
cast send --rpc-url "$RPC" --private-key "$PROVIDER_PK" "$GRT" "approve(address,uint256)(bool)" "$STAKING" "$TOKENS" >/dev/null
cast send --rpc-url "$RPC" --private-key "$PROVIDER_PK" "$STAKING" "stake(uint256)" "$TOKENS" >/dev/null
cast send --rpc-url "$RPC" --private-key "$PROVIDER_PK" "$STAKING" "provision(address,address,uint256,uint32,uint64)" "$PROVIDER" "$SDS" "$TOKENS" 0 0 >/dev/null
echo "provision tokens: $(cast call --rpc-url "$RPC" "$STAKING" "getProvision(address,address)((uint256,uint256,uint256,uint256,uint64,uint256,uint256,uint64,address))" "$PROVIDER" "$SDS" | head -1)"

step "register() WITH a real provision -> expect success"
cast send --rpc-url "$RPC" --private-key "$PROVIDER_PK" "$SDS" "register(address,bytes)" "$PROVIDER" "$DATA" >/dev/null
REGISTERED=$(cast call --rpc-url "$RPC" "$SDS" "isRegistered(address)(bool)" "$PROVIDER")
[ "$REGISTERED" = "true" ] || { echo "register() did not register provider"; exit 1; }

printf '\n=== RESULT ===\n'
echo "isRegistered:        $REGISTERED"
echo "paymentsDestination: $(cast call --rpc-url "$RPC" "$SDS" "paymentsDestination(address)(address)" "$PROVIDER")"
echo "PASS: SubstreamsDataService provisioning + registration verified against real Arbitrum One Horizon."
