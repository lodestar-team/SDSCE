#!/usr/bin/env bash
#
# Deploy SubstreamsDataService (UUPS proxy) to Arbitrum One (or a fork of it).
#
# Secrets are read from the environment and never hardcoded or logged. Run this
# yourself in your own terminal so your deployer key stays with you.
#
# Required env:
#   ARB_ONE_RPC        JSON-RPC endpoint (Arbitrum One for the real deploy)
#   SDS_DEPLOYER_KEY   deployer private key, funded with ETH for gas
#   OWNER_ADDRESS      contract owner — controls parameters AND upgrades.
#                      USE A MULTISIG. (ownership is two-step; can be transferred later)
# Optional env:
#   MIN_PROVISION_TOKENS   minimum provision tokens (default 0)
#   CONFIRM=yes            REQUIRED to broadcast to Arbitrum One mainnet (chain 42161)
#
# Usage (mainnet):
#   export ARB_ONE_RPC=https://<your-arb-one-rpc>
#   export SDS_DEPLOYER_KEY=0x...        # keep this out of shell history
#   export OWNER_ADDRESS=0x<your-multisig>
#   CONFIRM=yes ./devel/deploy-arb-one.sh
#
set -euo pipefail

: "${ARB_ONE_RPC:?set ARB_ONE_RPC}"
: "${SDS_DEPLOYER_KEY:?set SDS_DEPLOYER_KEY (deployer private key)}"
: "${OWNER_ADDRESS:?set OWNER_ADDRESS (contract owner — use a multisig)}"
MIN_PROVISION_TOKENS="${MIN_PROVISION_TOKENS:-0}"

# Verified Arbitrum One Horizon addresses (graphprotocol/contracts, horizon-arbitrumOne)
CONTROLLER=0x0a8491544221dd212964fbb96487467291b2C97e
COLLECTOR=0x8f69F5C07477Ac46FBc491B1E6D91E2bb0111A9e
ARTIFACT=contracts/artifacts/SubstreamsDataService.json
PROXY_ARTIFACT=contracts/artifacts/ERC1967Proxy.json

bc() { python3 -c "import json;print(json.load(open('$1'))['bytecode']['object'])"; }
addr_of() { python3 -c "import sys,json;print(json.load(sys.stdin)['contractAddress'])"; }

CHAIN=$(cast chain-id --rpc-url "$ARB_ONE_RPC")
echo "target chain id: $CHAIN"
echo "owner:           $OWNER_ADDRESS"
echo "min provision:   $MIN_PROVISION_TOKENS"

if [ "$CHAIN" = "42161" ] && [ "${CONFIRM:-no}" != "yes" ]; then
  echo "REFUSING to broadcast to Arbitrum One mainnet (42161) without CONFIRM=yes." >&2
  exit 1
fi

# Owner sanity: warn loudly unless the owner is a real contract (multisig).
# Note: an EIP-7702-delegated EOA reports code prefixed with 0xef0100, so a non-empty
# codesize alone does not prove a multisig.
OWNER_CODE=$(cast code "$OWNER_ADDRESS" --rpc-url "$ARB_ONE_RPC" 2>/dev/null || echo 0x)
case "$OWNER_CODE" in
  0x|0x00|0xef0100*)
    echo "WARNING: OWNER_ADDRESS is an EOA (or a 7702-delegated EOA), not a multisig contract." >&2
    echo "         It will control upgrades over real funds. A multisig is strongly advised." >&2
    echo "         (Ownership is two-step; you can transferOwnership to a multisig later.)" >&2
    ;;
esac

echo "=== 1/3 deploy implementation ==="
IMPL=$(cast send --rpc-url "$ARB_ONE_RPC" --private-key "$SDS_DEPLOYER_KEY" --create "$(bc "$ARTIFACT")" \
  "constructor(address,address)" "$CONTROLLER" "$COLLECTOR" --json | addr_of)
echo "implementation: $IMPL"

echo "=== 2/3 deploy ERC1967 proxy (atomic initialize) ==="
INIT_DATA=$(cast calldata "initialize(address,uint256)" "$OWNER_ADDRESS" "$MIN_PROVISION_TOKENS")
SDS=$(cast send --rpc-url "$ARB_ONE_RPC" --private-key "$SDS_DEPLOYER_KEY" --create "$(bc "$PROXY_ARTIFACT")" \
  "constructor(address,bytes)" "$IMPL" "$INIT_DATA" --json | addr_of)
echo "SubstreamsDataService (proxy): $SDS"

echo "=== 3/3 verify ==="
echo "owner:            $(cast call --rpc-url "$ARB_ONE_RPC" "$SDS" 'owner()(address)')"
echo "GRAPH_TALLY_COLLECTOR: $(cast call --rpc-url "$ARB_ONE_RPC" "$SDS" 'GRAPH_TALLY_COLLECTOR()(address)')"
echo "BURN_TAX_PPM:     $(cast call --rpc-url "$ARB_ONE_RPC" "$SDS" 'BURN_TAX_PPM()(uint256)')"
RANGE=$(cast call --rpc-url "$ARB_ONE_RPC" "$SDS" 'getProvisionTokensRange()(uint256,uint256)' | tr '\n' ' ')
echo "provision range:  $RANGE"

OWNER_GOT=$(cast call --rpc-url "$ARB_ONE_RPC" "$SDS" 'owner()(address)')
[ "$(echo "$OWNER_GOT" | tr 'A-F' 'a-f')" = "$(echo "$OWNER_ADDRESS" | tr 'A-F' 'a-f')" ] \
  || { echo "FAIL: owner mismatch (got $OWNER_GOT, expected $OWNER_ADDRESS)"; exit 1; }

echo
echo "DEPLOYED. Record these addresses:"
echo "  SubstreamsDataService (proxy, use this): $SDS"
echo "  implementation:                          $IMPL"
echo "Next: providers stake+provision+register; see docs/arb-one-deployment-runbook.md"
