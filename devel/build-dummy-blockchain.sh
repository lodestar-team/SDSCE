#!/usr/bin/env bash
#
# Rebuild the dummy-blockchain data-plane image used by the firehose streaming
# integration tests (TestFirecore / TestFirecoreStopsStreamOnLowFunds).
#
# The published dummy-blockchain images are stale for the current SDS plugin
# contract (missing x-sds-rav metadata; see docs/provider-runtime-compatibility.md).
# This rebuilds it on top of an SDS-compatible firehose-core image so the runtime
# path validates.
#
# Usage:
#   ./devel/build-dummy-blockchain.sh
#   FIRECORE_VERSION=latest ./devel/build-dummy-blockchain.sh
#
# Then point the integration tests at it:
#   export SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE=$(./devel/build-dummy-blockchain.sh --print-tag)
#   go test ./test/integration -run TestFirecore -timeout 540s
#
set -euo pipefail

FIRECORE_VERSION="${FIRECORE_VERSION:-latest}"
IMAGE_TAG="${IMAGE_TAG:-ghcr.io/streamingfast/dummy-blockchain:sds-upstream-firecore-latest}"
DUMMY_REPO="${DUMMY_REPO:-https://github.com/streamingfast/dummy-blockchain.git}"
DUMMY_REF="${DUMMY_REF:-main}"

if [ "${1:-}" = "--print-tag" ]; then
	echo "$IMAGE_TAG"
	exit 0
fi

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

echo "=== cloning dummy-blockchain ($DUMMY_REF) ==="
git clone --depth 1 --branch "$DUMMY_REF" "$DUMMY_REPO" "$workdir/dummy-blockchain" 2>&1 | tail -2 ||
	git clone "$DUMMY_REPO" "$workdir/dummy-blockchain"

echo "=== building $IMAGE_TAG on firehose-core:$FIRECORE_VERSION ==="
docker build --build-arg "FIRECORE_VERSION=$FIRECORE_VERSION" -t "$IMAGE_TAG" "$workdir/dummy-blockchain"

echo
echo "Built: $IMAGE_TAG"
echo "Run the streaming e2e with:"
echo "  export SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE=$IMAGE_TAG"
echo "  go test ./test/integration -run TestFirecore -timeout 540s"
