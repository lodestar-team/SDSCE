# Local Reflex SDS Demo

This runbook demonstrates the SDS local development flow with the reflex stack:

- consumer funding CLI
- consumer signer status
- Substreams through the consumer sidecar
- provider operator inspection
- manual provider collection

It assumes the stack was started with one of:

```bash
reflex -c .reflex
reflex -c .reflex.demo
```

Use `.reflex.demo` when you want to reach collectible RAVs quickly because it uses a lower RAV request threshold.

The reflex stack is the checked-in plaintext development environment. Its commands pass `--plaintext` and `--plugin-plaintext` explicitly; non-dev deployments should omit those flags and provide TLS certificate/key files instead.

## 1. Export Local Demo Variables

Use the values printed by `sds devenv`. For the deterministic reflex demo stack:

```bash
export RPC_ENDPOINT="http://localhost:58545"
export CHAIN_ID=1337

export GRT_TOKEN_ADDRESS="0xfa7a048544f86c11206afd89b40bc987e464cb58"
export ESCROW_ADDRESS="0xfc7487a37ca8eac2e64cba61277aa109e9b8631e"
export COLLECTOR_ADDRESS="0x1d01649b4f94722b55b5c3b3e10fe26cd90c1ba9"
export DATA_SERVICE_ADDRESS="0x37478fd2f5845e3664fe4155d74c00e1a4e7a5e2"

export SERVICE_PROVIDER_ADDRESS="0xa6f1845e54b1d6a95319251f1ca775b4ad406cdf"
export PAYER_ADDRESS="0xe90874856c339d5d3733c92ea5acadc6014b34d5"
export CONSUMER_SIGNER_ADDRESS="0x82b6f0bbbab50f0ddc249e5ff60c6dc64d55340e"

# Copy these from the `sds devenv` output. They are deterministic local-only
# development keys, but this runbook avoids publishing private-key literals.
export SERVICE_PROVIDER_PRIVATE_KEY="<service-provider-private-key-from-sds-devenv>"
export PAYER_PRIVATE_KEY="<payer-private-key-from-sds-devenv>"

export PROVIDER_OPERATOR_GATEWAY_URL="localhost:9010"
export SDS_OPERATOR_READ_TOKEN="local-operator-read-token"
export SDS_ADMIN_WRITE_TOKEN="local-admin-write-token"
```

These are local deterministic development keys and local-only operator tokens. Do not reuse them outside the local demo stack.

## 2. Check Local Services

```bash
nc -vz localhost 9001
nc -vz localhost 9002
nc -vz localhost 9010
nc -vz localhost 10016
```

Expected endpoints:

- `localhost:9001`: provider payment gateway
- `localhost:9002`: consumer sidecar ingress
- `localhost:9010`: provider operator gateway
- `localhost:10016`: Substreams tier1

## 3. Show Funding State

The reflex devenv already funds the payer escrow. Show the current wallet, allowance, and escrow state:

```bash
sds consumer funding status \
  --rpc-endpoint="$RPC_ENDPOINT" \
  --grt-token-address="$GRT_TOKEN_ADDRESS" \
  --escrow-address="$ESCROW_ADDRESS" \
  --collector-address="$COLLECTOR_ADDRESS" \
  --payer-address="$PAYER_ADDRESS" \
  --receiver-address="$SERVICE_PROVIDER_ADDRESS" \
  --min-escrow-balance="10000 GRT"
```

Optional: submit a tiny approval and top-up so the demo includes real funding transactions:

```bash
SDS_PAYER_KEY="$PAYER_PRIVATE_KEY" sds consumer funding approve \
  --rpc-endpoint="$RPC_ENDPOINT" \
  --chain-id="$CHAIN_ID" \
  --grt-token-address="$GRT_TOKEN_ADDRESS" \
  --escrow-address="$ESCROW_ADDRESS" \
  --payer-private-key-env=SDS_PAYER_KEY \
  --amount="1 GRT"
```

```bash
SDS_PAYER_KEY="$PAYER_PRIVATE_KEY" sds consumer funding top-up \
  --rpc-endpoint="$RPC_ENDPOINT" \
  --chain-id="$CHAIN_ID" \
  --grt-token-address="$GRT_TOKEN_ADDRESS" \
  --escrow-address="$ESCROW_ADDRESS" \
  --collector-address="$COLLECTOR_ADDRESS" \
  --receiver-address="$SERVICE_PROVIDER_ADDRESS" \
  --payer-private-key-env=SDS_PAYER_KEY \
  --target-balance="10001 GRT"
```

## 4. Show Signer Authorization

The reflex devenv already authorizes the demo signer for the payer:

```bash
sds consumer signer status \
  --rpc-endpoint="$RPC_ENDPOINT" \
  --collector-address="$COLLECTOR_ADDRESS" \
  --payer-address="$PAYER_ADDRESS" \
  --signer-address="$CONSUMER_SIGNER_ADDRESS"
```

Expected result:

```text
authorized: true
```

## 5. Run Substreams Through SDS

Run the normal Substreams CLI against the local consumer sidecar:

```bash
substreams run common@v0.1.0 map_clocks \
  -e localhost:9002 \
  --plaintext \
  -s 0 \
  -t +300
```

If no collectible RAV appears after this run, either run more blocks or restart with the lower-threshold demo stack:

```bash
reflex -c .reflex.demo
```

## 6. Inspect Provider State

List sessions and include the accepted RAV summary:

```bash
sds provider operator sessions list \
  --provider-endpoint="$PROVIDER_OPERATOR_GATEWAY_URL" \
  --plaintext \
  --operator-token-env=SDS_OPERATOR_READ_TOKEN \
  --include-rav
```

List accepted RAVs:

```bash
sds provider operator ravs list \
  --provider-endpoint="$PROVIDER_OPERATOR_GATEWAY_URL" \
  --plaintext \
  --operator-token-env=SDS_OPERATOR_READ_TOKEN \
  --include-rav
```

List collectible collection records:

```bash
sds provider operator collections list \
  --provider-endpoint="$PROVIDER_OPERATOR_GATEWAY_URL" \
  --plaintext \
  --operator-token-env=SDS_OPERATOR_READ_TOKEN \
  --state=collectible \
  --include-rav
```

Pick the `session_id` and `collection_id` from a collectible record:

```bash
export SESSION_ID="<from collections list>"
export COLLECTION_ID="<from collections list>"
```

## 7. Dry-Run Collection

Estimate the collection transaction without submitting it:

```bash
SDS_PROVIDER_KEY="$SERVICE_PROVIDER_PRIVATE_KEY" sds provider operator collect \
  --provider-endpoint="$PROVIDER_OPERATOR_GATEWAY_URL" \
  --plaintext \
  --operator-token-env=SDS_ADMIN_WRITE_TOKEN \
  --rpc-endpoint="$RPC_ENDPOINT" \
  --chain-id="$CHAIN_ID" \
  --provider-private-key-env=SDS_PROVIDER_KEY \
  --session-id="$SESSION_ID" \
  --collection-id="$COLLECTION_ID" \
  --payer-address="$PAYER_ADDRESS" \
  --receiver-address="$SERVICE_PROVIDER_ADDRESS" \
  --data-service-address="$DATA_SERVICE_ADDRESS" \
  --data-service-cut-ppm=0 \
  --dry-run
```

The dry-run should print the settlement data and estimated transaction information without changing collection state.

## 8. Submit Collection

Submit the collection transaction:

```bash
SDS_PROVIDER_KEY="$SERVICE_PROVIDER_PRIVATE_KEY" sds provider operator collect \
  --provider-endpoint="$PROVIDER_OPERATOR_GATEWAY_URL" \
  --plaintext \
  --operator-token-env=SDS_ADMIN_WRITE_TOKEN \
  --rpc-endpoint="$RPC_ENDPOINT" \
  --chain-id="$CHAIN_ID" \
  --provider-private-key-env=SDS_PROVIDER_KEY \
  --session-id="$SESSION_ID" \
  --collection-id="$COLLECTION_ID" \
  --payer-address="$PAYER_ADDRESS" \
  --receiver-address="$SERVICE_PROVIDER_ADDRESS" \
  --data-service-address="$DATA_SERVICE_ADDRESS" \
  --data-service-cut-ppm=0
```

## 9. Confirm Collection State

Confirm the record moved to `collected`:

```bash
sds provider operator collections list \
  --provider-endpoint="$PROVIDER_OPERATOR_GATEWAY_URL" \
  --plaintext \
  --operator-token-env=SDS_OPERATOR_READ_TOKEN \
  --state=collected
```

You can also list all collection records:

```bash
sds provider operator collections list \
  --provider-endpoint="$PROVIDER_OPERATOR_GATEWAY_URL" \
  --plaintext \
  --operator-token-env=SDS_OPERATOR_READ_TOKEN \
  --include-rav
```

## Notes

- The local devenv uses `MockStaking` and directly prepares provider provision state. This is different from public testnet, where the real Horizon provision flow must exist before `SubstreamsDataService.register` and `collect` can succeed.
- The operator gateway is authenticated even locally. Read-only commands use `SDS_OPERATOR_READ_TOKEN`; collection uses `SDS_ADMIN_WRITE_TOKEN`.
- The provider private key is used only by the local manual collection CLI. The provider gateway itself does not need the settlement key.
