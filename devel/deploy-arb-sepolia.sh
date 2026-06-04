#!/usr/bin/env bash
#
# Deploy SubstreamsDataService (UUPS proxy) to Arbitrum Sepolia (or a fork of it).
#
# Sibling of devel/deploy-arb-one.sh, wired to the Arbitrum Sepolia (chain 421614)
# Horizon contracts. Sepolia is a testnet, so the guardrails are lighter than the
# mainnet script — but secrets are still read from the environment and never logged.
# Run this yourself in your own terminal so your deployer key stays with you.
#
# Required env:
#   ARB_SEPOLIA_RPC    JSON-RPC endpoint (Arbitrum Sepolia, or a local fork of it)
#   SDS_DEPLOYER_KEY   deployer private key, funded with Sepolia ETH for gas
#   OWNER_ADDRESS      contract owner — controls parameters AND upgrades.
#                      (ownership is two-step; can be transferred later)
# Optional env:
#   MIN_PROVISION_TOKENS   minimum provision tokens (default 0)
#
# Usage (public testnet):
#   export ARB_SEPOLIA_RPC=https://sepolia-rollup.arbitrum.io/rpc
#   export SDS_DEPLOYER_KEY=0x...        # keep this out of shell history
#   export OWNER_ADDRESS=0x<your-owner>
#   ./devel/deploy-arb-sepolia.sh
#
# Usage (local fork rehearsal):
#   anvil --fork-url https://sepolia-rollup.arbitrum.io/rpc --port 8546 --chain-id 421614 &
#   export ARB_SEPOLIA_RPC=http://localhost:8546
#   export SDS_DEPLOYER_KEY=<one of anvil's prefunded keys>
#   export OWNER_ADDRESS=<your owner>
#   ./devel/deploy-arb-sepolia.sh
#
set -euo pipefail

: "${ARB_SEPOLIA_RPC:?set ARB_SEPOLIA_RPC}"
: "${SDS_DEPLOYER_KEY:?set SDS_DEPLOYER_KEY (deployer private key)}"
: "${OWNER_ADDRESS:?set OWNER_ADDRESS (contract owner)}"
MIN_PROVISION_TOKENS="${MIN_PROVISION_TOKENS:-0}"

# Verified Arbitrum Sepolia Horizon addresses (graphprotocol/contracts,
# horizon-arbitrumSepolia). See docs/direct-provider-testnet-public-runbook.md.
CONTROLLER=0x9DB3ee191681f092607035d9BDA6e59FbEaCa695
COLLECTOR=0x382863e7B662027117449bd2c49285582bbBd21B
ARTIFACT=contracts/artifacts/SubstreamsDataService.json
PROXY_ARTIFACT=contracts/artifacts/ERC1967Proxy.json

bc() { python3 -c "import json;print(json.load(open('$1'))['bytecode']['object'])"; }
addr_of() { python3 -c "import sys,json;print(json.load(sys.stdin)['contractAddress'])"; }

CHAIN=$(cast chain-id --rpc-url "$ARB_SEPOLIA_RPC")
echo "target chain id: $CHAIN"
echo "owner:           $OWNER_ADDRESS"
echo "min provision:   $MIN_PROVISION_TOKENS"

if [ "$CHAIN" != "421614" ]; then
  echo "REFUSING: expected Arbitrum Sepolia (chain 421614) but RPC reports $CHAIN." >&2
  echo "          Point ARB_SEPOLIA_RPC at Arbitrum Sepolia or a fork with --chain-id 421614." >&2
  exit 1
fi

# Sanity: the Horizon contracts must actually exist at the wired addresses.
for pair in "Controller:$CONTROLLER" "GraphTallyCollector:$COLLECTOR"; do
  name=${pair%%:*}; addr=${pair##*:}
  [ "$(cast codesize "$addr" --rpc-url "$ARB_SEPOLIA_RPC")" != "0" ] \
    || { echo "FAIL: no code at $name ($addr) on chain $CHAIN" >&2; exit 1; }
done

echo "=== 1/3 deploy implementation ==="
IMPL=$(cast send --rpc-url "$ARB_SEPOLIA_RPC" --private-key "$SDS_DEPLOYER_KEY" --create "$(bc "$ARTIFACT")" \
  "constructor(address,address)" "$CONTROLLER" "$COLLECTOR" --json | addr_of)
echo "implementation: $IMPL"

echo "=== 2/3 deploy ERC1967 proxy (atomic initialize) ==="
INIT_DATA=$(cast calldata "initialize(address,uint256)" "$OWNER_ADDRESS" "$MIN_PROVISION_TOKENS")
SDS=$(cast send --rpc-url "$ARB_SEPOLIA_RPC" --private-key "$SDS_DEPLOYER_KEY" --create "$(bc "$PROXY_ARTIFACT")" \
  "constructor(address,bytes)" "$IMPL" "$INIT_DATA" --json | addr_of)
echo "SubstreamsDataService (proxy): $SDS"

echo "=== 3/3 verify ==="
echo "owner:            $(cast call --rpc-url "$ARB_SEPOLIA_RPC" "$SDS" 'owner()(address)')"
echo "GRAPH_TALLY_COLLECTOR: $(cast call --rpc-url "$ARB_SEPOLIA_RPC" "$SDS" 'GRAPH_TALLY_COLLECTOR()(address)')"
echo "BURN_TAX_PPM:     $(cast call --rpc-url "$ARB_SEPOLIA_RPC" "$SDS" 'BURN_TAX_PPM()(uint256)')"
RANGE=$(cast call --rpc-url "$ARB_SEPOLIA_RPC" "$SDS" 'getProvisionTokensRange()(uint256,uint256)' | tr '\n' ' ')
echo "provision range:  $RANGE"

OWNER_GOT=$(cast call --rpc-url "$ARB_SEPOLIA_RPC" "$SDS" 'owner()(address)')
[ "$(echo "$OWNER_GOT" | tr 'A-F' 'a-f')" = "$(echo "$OWNER_ADDRESS" | tr 'A-F' 'a-f')" ] \
  || { echo "FAIL: owner mismatch (got $OWNER_GOT, expected $OWNER_ADDRESS)"; exit 1; }

echo
echo "DEPLOYED. Record these addresses:"
echo "  SubstreamsDataService (proxy, use this): $SDS"
echo "  implementation:                          $IMPL"
echo "Next: providers stake+provision+register; see docs/arb-one-deployment-runbook.md"
