#!/usr/bin/env bash
#
# Convenience wrapper: load disposable keys/addresses from .env (gitignored) and
# run devel/deploy-arb-sepolia.sh with the names it expects. Testnet only.
#
# Populate .env first (see .env layout: DEPLOYER_KEY, OWNER_ADDR, ARB_SEPOLIA_RPC).
# The deployer address must hold Arbitrum Sepolia ETH for gas.
set -euo pipefail
cd "$(dirname "$0")/.."

[ -f .env ] || { echo "no .env found — create it with DEPLOYER_KEY / OWNER_ADDR / ARB_SEPOLIA_RPC" >&2; exit 1; }
set -a; . ./.env; set +a

export ARB_SEPOLIA_RPC="${ARB_SEPOLIA_RPC:?set ARB_SEPOLIA_RPC in .env}"
export SDS_DEPLOYER_KEY="${DEPLOYER_KEY:?set DEPLOYER_KEY in .env}"
export OWNER_ADDRESS="${OWNER_ADDR:?set OWNER_ADDR in .env}"
export MIN_PROVISION_TOKENS="${MIN_PROVISION_TOKENS:-0}"

exec ./devel/deploy-arb-sepolia.sh
