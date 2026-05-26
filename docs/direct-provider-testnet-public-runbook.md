# Direct Provider Testnet Runbook

This runbook documents the direct-provider SDS testnet setup used during MVP validation. It covers the contract inputs, provider deployment shape, local consumer sidecar flow, and CLI workflows for funding, inspection, and manual collection.

The setup combines:

- Arbitrum Sepolia Horizon payment contracts
- a provider gateway on Kubernetes
- an SDS-compatible Firecore/Substreams data plane
- a local consumer sidecar
- authenticated provider operator APIs
- CLI-driven funding, inspection, and manual collection

This is the direct-provider flow, so the consumer sidecar connects to a configured provider gateway directly rather than discovering a provider through the oracle.

## Public Testnet Values

```bash
export CHAIN_ID=421614
export ARBITRUM_SEPOLIA_RPC_URL="https://sepolia-rollup.arbitrum.io/rpc"

export GRAPH_TALLY_COLLECTOR_ADDRESS="0x382863e7B662027117449bd2c49285582bbBd21B"
export PAYMENTS_ESCROW_ADDRESS="0x4b5D3Da463F7E076bb7CDF5030960bf123245681"
export GRAPH_PAYMENTS_ADDRESS="0x57E70eC8905E26341d40aF60Dca56cDBA8C166E5"
export GRAPH_CONTROLLER_ADDRESS="0x9DB3ee191681f092607035d9BDA6e59FbEaCa695"
export GRT_TOKEN_ADDRESS="0xf8c05dCF59E8B28BFD5eed176C562bEbcfc7Ac04"

export SUBSTREAMS_DATA_SERVICE_ADDRESS="0xEfaBE63f10C44B43586E54CD0d62C251b8Ee3E3B"
```

The values above are public testnet contracts and a public RPC endpoint. Payer, signer, and service provider accounts are intentionally not listed because they should be generated per test run.

## Demo Wallets

Create disposable wallets for a testnet run. Do not use production keys, and do not commit private keys.

```bash
cast wallet new
cast wallet new
cast wallet new
```

Use the generated private keys outside the repository:

```bash
export SERVICE_PROVIDER_PRIVATE_KEY="0x..."
export PAYER_PRIVATE_KEY="0x..."
export CONSUMER_SIGNER_PRIVATE_KEY="0x..."
```

Derive the account addresses expected by the commands in this runbook:

```bash
export SERVICE_PROVIDER_ADDRESS="$(
  cast wallet address --private-key "$SERVICE_PROVIDER_PRIVATE_KEY"
)"

export PAYER_ADDRESS="$(
  cast wallet address --private-key "$PAYER_PRIVATE_KEY"
)"

export CONSUMER_SIGNER_ADDRESS="$(
  cast wallet address --private-key "$CONSUMER_SIGNER_PRIVATE_KEY"
)"

echo "service provider: $SERVICE_PROVIDER_ADDRESS"
echo "payer:            $PAYER_ADDRESS"
echo "consumer signer:  $CONSUMER_SIGNER_ADDRESS"
```

Fund the generated accounts before running the demo:

- `SERVICE_PROVIDER_ADDRESS` needs Arbitrum Sepolia ETH for data service registration and manual collection.
- `PAYER_ADDRESS` needs Arbitrum Sepolia ETH for approval, deposit, and signer authorization transactions.
- `PAYER_ADDRESS` needs Arbitrum Sepolia L2 GRT for escrow deposits.
- `CONSUMER_SIGNER_ADDRESS` does not need ETH or GRT if it only signs RAVs locally.

## Private Inputs

Keep these values outside the repository:

```bash
export PAYER_PRIVATE_KEY="0x..."
export SERVICE_PROVIDER_PRIVATE_KEY="0x..."
export CONSUMER_SIGNER_PRIVATE_KEY="0x..."

export PAYER_ADDRESS="0x..."
export SERVICE_PROVIDER_ADDRESS="0x..."
export CONSUMER_SIGNER_ADDRESS="0x..."

export SDS_MIGRATE_DSN="postgres://sds:<password>@<postgres-host>:5432/sds?sslmode=require"
export SDS_REPOSITORY_DSN="psql://sds:<password>@<postgres-host>:5432/sds?sslmode=require"

export PROVIDER_PAYMENT_GATEWAY_URL="https://<provider-payment-gateway>"
export PROVIDER_SUBSTREAMS_ENDPOINT="https://<provider-substreams-tier1>:443"
export PROVIDER_OPERATOR_GATEWAY_URL="https://<private-provider-operator-gateway>"

export SDS_OPERATOR_READ_TOKEN="<operator-read-token>"
export SDS_ADMIN_WRITE_TOKEN="<admin-write-token>"
```

Generate operator tokens with high-entropy random values, for example:

```bash
openssl rand -hex 32
```

The provider gateway process receives those token values through environment variables. Operator CLI commands should pass token environment variable names, not token literals.

## Architecture

The direct-provider setup has five relevant surfaces:

1. Consumer sidecar
   - runs locally for the demo
   - exposes the user-facing Substreams endpoint on `localhost:9002`
   - starts SDS payment sessions with the provider gateway
   - signs RAVs and injects SDS metadata into upstream Substreams requests

2. Provider payment gateway
   - served by `sds provider gateway --grpc-listen-addr`
   - reachable by consumer sidecars
   - handles `StartSession`, `PaymentSession`, and minimal runtime status

3. Provider plugin gateway
   - served by the same `sds provider gateway` process on `--plugin-listen-addr`
   - private to the provider runtime
   - receives `sds://` auth, session, and metering calls from Firecore/Substreams

4. Provider operator gateway
   - served by the same `sds provider gateway` process when `--operator-listen-addr` is set
   - private and bearer-token protected
   - exposes session/RAV/collection inspection plus collection lifecycle mutation used by manual collect

5. Provider Substreams data plane
   - normal tier1 Substreams gRPC endpoint
   - must run an SDS-compatible runtime
   - must preserve `x-sds-rav` and `x-sds-session-id` metadata

For the MVP/demo shape, run one provider gateway replica. Live payment-control binding is process-local, so rolling or load-balancing multiple replicas without sticky behavior can interrupt active streams.

## Build The SDS Image

The Docker image only needs to include the current `sds` binary. The provider operator API is part of that binary; there is no separate operator image.

```bash
docker build \
  --build-arg VERSION="$(git rev-parse --short HEAD)" \
  -t "<registry>/sds-provider:$(git rev-parse --short HEAD)" \
  .

docker push "<registry>/sds-provider:$(git rev-parse --short HEAD)"
```

Sanity check that the rebuilt image includes the operator gateway flags:

```bash
docker run --rm "<registry>/sds-provider:$(git rev-parse --short HEAD)" \
  provider gateway --help | grep operator-listen-addr
```

The image does not create `pricing.yaml`. In Kubernetes, mount pricing through a ConfigMap or equivalent config volume.

## Provider Pricing

For a demo, use a low RAV threshold so accepted RAVs become visible quickly:

```yaml
price_per_block: "0.000115 GRT"
price_per_byte: "0.0000000061 GRT"
rav_request_threshold: "0.02 GRT"
```

For production-like testing, use a higher threshold to reduce RAV request frequency.

Example Kubernetes ConfigMap:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: sds-provider-pricing
data:
  pricing.yaml: |
    price_per_block: "0.000115 GRT"
    price_per_byte: "0.0000000061 GRT"
    rav_request_threshold: "0.02 GRT"
```

Mount it read-only at the path passed to `--pricing-config`, for example `/etc/sds/pricing/pricing.yaml`.

## Database

Use PostgreSQL for provider deployments. Do not use `inmemory://` outside local tests.

Run migrations before starting or upgrading the provider:

```bash
yes y | PG_DSN="$SDS_MIGRATE_DSN" ./devel/migrate.sh up
PG_DSN="$SDS_MIGRATE_DSN" ./devel/migrate.sh version
```

The gateway uses the repository DSN form:

```bash
export SDS_REPOSITORY_DSN="psql://sds:<password>@<postgres-host>:5432/sds?sslmode=require"
```

The repository stores sessions, latest accepted RAVs, collection lifecycle records, usage events, workers, and quota state.

## Provider Gateway Deployment

Example provider gateway command:

```bash
sds provider gateway \
  --repository-dsn="$SDS_REPOSITORY_DSN" \
  --grpc-listen-addr=:9001 \
  --plugin-listen-addr=:9003 \
  --operator-listen-addr=:9010 \
  --operator-read-token-env=SDS_OPERATOR_READ_TOKEN \
  --admin-write-token-env=SDS_ADMIN_WRITE_TOKEN \
  --service-provider="$SERVICE_PROVIDER_ADDRESS" \
  --chain-id="$CHAIN_ID" \
  --collector-address="$GRAPH_TALLY_COLLECTOR_ADDRESS" \
  --escrow-address="$PAYMENTS_ESCROW_ADDRESS" \
  --rpc-endpoint="$ARBITRUM_SEPOLIA_RPC_URL" \
  --data-plane-endpoint="$PROVIDER_SUBSTREAMS_ENDPOINT" \
  --pricing-config=/etc/sds/pricing/pricing.yaml \
  --tls-cert-file=/tls/tls.crt \
  --tls-key-file=/tls/tls.key
```

Deployment notes:

- Expose the payment gateway publicly through TLS-capable gRPC ingress or load balancer.
- Keep the plugin gateway private to the provider cluster.
- When no plugin-specific transport flags are supplied, the private plugin gateway uses the same TLS certificate/key configuration as the payment gateway.
- Plaintext is never implicit. Use `--plugin-plaintext` only for local/dev plugin gateway runs, and keep that listener unreachable from public networks.
- Keep the operator gateway private to operators. Prefer port-forward, VPN, or a restricted internal ingress.
- Store the repository DSN, RPC credentials, and operator tokens in Kubernetes Secrets.
- Store pricing in a ConfigMap or another explicit config source.
- Do not mount provider settlement private keys into the provider gateway deployment.

## Firecore/Substreams Runtime

The provider data plane must use an SDS-compatible runtime. Older Firecore/Substreams builds may start but fail to load or route `sds://` plugins. Use [provider runtime compatibility](./provider-runtime-compatibility.md) as the source of truth for current image state, local-image fallback guidance, and runtime bump criteria.

Configure the runtime to call the private provider plugin gateway:

```yaml
start:
  flags:
    common-auth-plugin: "sds://sds-provider-gateway.sds.svc.cluster.local:9003"
    common-session-plugin: "sds://sds-provider-gateway.sds.svc.cluster.local:9003"
    common-metering-plugin: "sds://sds-provider-gateway.sds.svc.cluster.local:9003?network=<substreams-network>&buffer=10000&delay=100&report-timeout=5s"
```

If using self-signed internal TLS in a controlled test environment, add `insecure=true`. If using plaintext for a local/dev plugin gateway, add `plaintext=true` and keep the plugin listener private.

Data-plane requirements:

- The advertised tier1 endpoint must be reachable by the consumer sidecar.
- The tier1 endpoint must accept HTTP/2 gRPC.
- Ingress/proxy layers must preserve `x-sds-rav` and `x-sds-session-id`.
- The runtime must be able to reach the private plugin gateway.

One common failure mode is advertising `http://localhost:<port>` as the data-plane endpoint when the consumer sidecar runs somewhere else. The consumer sidecar dials the endpoint returned by `StartSession`; `localhost` is always local to the consumer sidecar process, not the provider pod.

## On-Chain Preparation

The provider runtime can start sessions when:

- the payer has escrow balance for `(payer, GraphTallyCollector, serviceProvider)`
- the signer is authorized for the payer, unless the signer is the payer itself
- the provider gateway can query Arbitrum Sepolia RPC

Manual collection additionally requires:

- `SubstreamsDataService` is deployed
- the service provider is registered in `SubstreamsDataService`
- the service provider has a valid Horizon provision for the data service
- the provider database has a collectible accepted RAV
- the local operator has the service provider settlement key

The validation setup found that `SubstreamsDataService.register()` reverts with `ProvisionManagerProvisionNotFound(address)` when the service provider does not have a Horizon provision for the data service. That is expected contract behavior. Complete the real Horizon provision flow before expecting registration or collection to succeed on public testnet.

### Check Registration

```bash
cast call "$SUBSTREAMS_DATA_SERVICE_ADDRESS" \
  "isRegistered(address)(bool)" \
  "$SERVICE_PROVIDER_ADDRESS" \
  --rpc-url "$ARBITRUM_SEPOLIA_RPC_URL"
```

### Register Provider

Only run this after the Horizon provision exists:

```bash
export REGISTER_DATA="$(cast abi-encode 'f(address)' "$SERVICE_PROVIDER_ADDRESS")"

cast send "$SUBSTREAMS_DATA_SERVICE_ADDRESS" \
  "register(address,bytes)" \
  "$SERVICE_PROVIDER_ADDRESS" \
  "$REGISTER_DATA" \
  --rpc-url "$ARBITRUM_SEPOLIA_RPC_URL" \
  --private-key "$SERVICE_PROVIDER_PRIVATE_KEY"
```

The `REGISTER_DATA` payload is `abi.encode(paymentsDestination)`. For a simple demo, use the service provider address as the payments destination.

## Funding CLI

Check payer wallet, allowance, and escrow:

```bash
sds consumer funding status \
  --rpc-endpoint="$ARBITRUM_SEPOLIA_RPC_URL" \
  --grt-token-address="$GRT_TOKEN_ADDRESS" \
  --escrow-address="$PAYMENTS_ESCROW_ADDRESS" \
  --collector-address="$GRAPH_TALLY_COLLECTOR_ADDRESS" \
  --payer-address="$PAYER_ADDRESS" \
  --receiver-address="$SERVICE_PROVIDER_ADDRESS" \
  --min-escrow-balance="5 GRT"
```

Approve escrow spending:

```bash
SDS_PAYER_KEY="$PAYER_PRIVATE_KEY" sds consumer funding approve \
  --rpc-endpoint="$ARBITRUM_SEPOLIA_RPC_URL" \
  --chain-id="$CHAIN_ID" \
  --grt-token-address="$GRT_TOKEN_ADDRESS" \
  --escrow-address="$PAYMENTS_ESCROW_ADDRESS" \
  --payer-private-key-env=SDS_PAYER_KEY \
  --amount="5 GRT"
```

Top up escrow to a target balance:

```bash
SDS_PAYER_KEY="$PAYER_PRIVATE_KEY" sds consumer funding top-up \
  --rpc-endpoint="$ARBITRUM_SEPOLIA_RPC_URL" \
  --chain-id="$CHAIN_ID" \
  --grt-token-address="$GRT_TOKEN_ADDRESS" \
  --escrow-address="$PAYMENTS_ESCROW_ADDRESS" \
  --collector-address="$GRAPH_TALLY_COLLECTOR_ADDRESS" \
  --receiver-address="$SERVICE_PROVIDER_ADDRESS" \
  --payer-private-key-env=SDS_PAYER_KEY \
  --target-balance="5 GRT"
```

## Signer CLI

Check whether the consumer signer is authorized:

```bash
sds consumer signer status \
  --rpc-endpoint="$ARBITRUM_SEPOLIA_RPC_URL" \
  --collector-address="$GRAPH_TALLY_COLLECTOR_ADDRESS" \
  --payer-address="$PAYER_ADDRESS" \
  --signer-address="$CONSUMER_SIGNER_ADDRESS"
```

Generate an offline signer proof:

```bash
SDS_SIGNER_KEY="$CONSUMER_SIGNER_PRIVATE_KEY" sds consumer signer proof \
  --chain-id="$CHAIN_ID" \
  --collector-address="$GRAPH_TALLY_COLLECTOR_ADDRESS" \
  --payer-address="$PAYER_ADDRESS" \
  --signer-private-key-env=SDS_SIGNER_KEY \
  --deadline=1h
```

Authorize the signer from the payer account using the generated proof:

```bash
SDS_PAYER_KEY="$PAYER_PRIVATE_KEY" sds consumer signer authorize \
  --rpc-endpoint="$ARBITRUM_SEPOLIA_RPC_URL" \
  --chain-id="$CHAIN_ID" \
  --collector-address="$GRAPH_TALLY_COLLECTOR_ADDRESS" \
  --payer-private-key-env=SDS_PAYER_KEY \
  --signer-address="$CONSUMER_SIGNER_ADDRESS" \
  --proof="$SIGNER_PROOF" \
  --proof-deadline="$PROOF_DEADLINE"
```

For the shortest demo path, the payer can also be the signer. In that case, separate signer authorization is not required.

## Consumer Sidecar

Create a local direct-provider config:

```yaml
payer_address: "<PAYER_ADDRESS>"
receiver_address: "<SERVICE_PROVIDER_ADDRESS>"
data_service_address: "0xEfaBE63f10C44B43586E54CD0d62C251b8Ee3E3B"
provider_control_plane_endpoint: "https://<provider-payment-gateway>"
```

Run the local consumer sidecar:

```bash
sds consumer sidecar \
  --grpc-listen-addr=:9002 \
  --config=consumer-sidecar.direct-testnet.yaml \
  --signer-private-key="$CONSUMER_SIGNER_PRIVATE_KEY" \
  --chain-id="$CHAIN_ID" \
  --collector-address="$GRAPH_TALLY_COLLECTOR_ADDRESS" \
  --plaintext
```

The sidecar `--plaintext` flag above only controls the local user-facing endpoint on `localhost:9002`. The sidecar still uses the URL schemes in its provider config and the provider handshake for provider-facing traffic.

## Run A Substream

Point normal Substreams tooling at the consumer sidecar:

```bash
substreams run <package> <module> \
  -e localhost:9002 \
  --plaintext \
  -s <start-block> \
  -t +<block-count>
```

Example used during validation:

```bash
substreams run bold-sql-substream-v0.1.0.spkg map_unified_events \
  -e localhost:9002 \
  --plaintext \
  -s 22483043 \
  -t +10000
```

Choose a package/module compatible with the provider chain. If no RAV appears, either run more blocks, use a module that emits more data, or lower `rav_request_threshold` for the demo.

## Provider Operator CLI

The operator API requires a bearer token even for local/private access.
The examples below assume the operator endpoint is the HTTPS URL from
`PROVIDER_OPERATOR_GATEWAY_URL`. If you reach the operator API through a local
plaintext port-forward, set `PROVIDER_OPERATOR_GATEWAY_URL` to that local
`http://localhost:<port>` URL and add `--plaintext` to the CLI commands.

List sessions:

```bash
sds provider operator sessions list \
  --provider-endpoint="$PROVIDER_OPERATOR_GATEWAY_URL" \
  --operator-token-env=SDS_OPERATOR_READ_TOKEN \
  --include-rav
```

List accepted RAVs:

```bash
sds provider operator ravs list \
  --provider-endpoint="$PROVIDER_OPERATOR_GATEWAY_URL" \
  --operator-token-env=SDS_OPERATOR_READ_TOKEN \
  --include-rav
```

List collectible records:

```bash
sds provider operator collections list \
  --provider-endpoint="$PROVIDER_OPERATOR_GATEWAY_URL" \
  --operator-token-env=SDS_OPERATOR_READ_TOKEN \
  --state=collectible \
  --include-rav
```

Fetch private operator metrics:

```bash
curl -H "Authorization: Bearer $SDS_OPERATOR_READ_TOKEN" \
  "$PROVIDER_OPERATOR_GATEWAY_URL/metrics"
```

## Manual Collection CLI

Pick the `session_id` and `collection_id` from a collectible record:

```bash
export SESSION_ID="<session-id>"
export COLLECTION_ID="<collection-id>"
```

Dry-run collection first:

```bash
SDS_PROVIDER_KEY="$SERVICE_PROVIDER_PRIVATE_KEY" sds provider operator collect \
  --provider-endpoint="$PROVIDER_OPERATOR_GATEWAY_URL" \
  --operator-token-env=SDS_ADMIN_WRITE_TOKEN \
  --rpc-endpoint="$ARBITRUM_SEPOLIA_RPC_URL" \
  --chain-id="$CHAIN_ID" \
  --provider-private-key-env=SDS_PROVIDER_KEY \
  --session-id="$SESSION_ID" \
  --collection-id="$COLLECTION_ID" \
  --payer-address="$PAYER_ADDRESS" \
  --receiver-address="$SERVICE_PROVIDER_ADDRESS" \
  --data-service-address="$SUBSTREAMS_DATA_SERVICE_ADDRESS" \
  --data-service-cut-ppm=0 \
  --dry-run
```

Submit collection by removing `--dry-run`:

```bash
SDS_PROVIDER_KEY="$SERVICE_PROVIDER_PRIVATE_KEY" sds provider operator collect \
  --provider-endpoint="$PROVIDER_OPERATOR_GATEWAY_URL" \
  --operator-token-env=SDS_ADMIN_WRITE_TOKEN \
  --rpc-endpoint="$ARBITRUM_SEPOLIA_RPC_URL" \
  --chain-id="$CHAIN_ID" \
  --provider-private-key-env=SDS_PROVIDER_KEY \
  --session-id="$SESSION_ID" \
  --collection-id="$COLLECTION_ID" \
  --payer-address="$PAYER_ADDRESS" \
  --receiver-address="$SERVICE_PROVIDER_ADDRESS" \
  --data-service-address="$SUBSTREAMS_DATA_SERVICE_ADDRESS" \
  --data-service-cut-ppm=0
```

If the provider operator endpoint is reached through a local plaintext port-forward, add:

```bash
--plaintext
```

Collection can fail during gas estimation if `SubstreamsDataService` registration or Horizon provision setup is incomplete. A known revert selector from this setup is `0x7b3c09bf`, which corresponds to `ProvisionManagerProvisionNotFound(address)`.

## Database Inspection

When using `psql`, address and signature fields are stored as `bytea`, so values display as `\x...`. That is normal Postgres hex bytea output. For human-readable addresses:

```sql
select
  session_id,
  '0x' || encode(collection_id, 'hex') as collection_id,
  '0x' || encode(payer, 'hex') as payer,
  '0x' || encode(service_provider, 'hex') as service_provider,
  '0x' || encode(data_service, 'hex') as data_service,
  timestamp_ns,
  value_aggregate,
  created_at
from ravs
order by created_at desc;
```

Collection lifecycle:

```sql
select
  session_id,
  state,
  '0x' || encode(collection_id, 'hex') as collection_id,
  '0x' || encode(payer, 'hex') as payer,
  '0x' || encode(service_provider, 'hex') as service_provider,
  '0x' || encode(data_service, 'hex') as data_service,
  value_aggregate,
  attempt_count,
  last_tx_hash,
  last_error,
  updated_at
from collection_records
order by updated_at desc;
```

If using `pgweb`, run it with hex binary rendering when available:

```bash
pgweb --bind=0.0.0.0 --listen=8081 --binary-codec=hex
```

## Troubleshooting

### `dial tcp 127.0.0.1:<port>: connect: connection refused`

The provider probably advertised a data-plane endpoint using `localhost`, but the consumer sidecar is running in a different environment. Advertise an endpoint reachable from the consumer sidecar process.

### `ProvisionManagerProvisionNotFound(address)`

The service provider is not provisioned for the `SubstreamsDataService` in Horizon staking. Complete the real Horizon provision flow, then retry registration and collection.

### RAVs remain at zero

The stream may not have crossed `rav_request_threshold`. Lower the threshold for demos or run a longer/heavier Substreams workload.

### Usage does not appear

Check:

- Firecore runtime includes SDS plugin support.
- `common-metering-plugin` has the expected `network=...`.
- Firecore can reach the private plugin gateway.
- Ingress preserves `x-sds-rav` and `x-sds-session-id`.

### Collection dry-run reverts

Check:

- `SubstreamsDataService.isRegistered(SERVICE_PROVIDER_ADDRESS)` returns `true`.
- payer escrow has enough balance.
- the RAV signer is authorized.
- the collection record is in `collectible` or `collect_failed_retryable`.
- the provider private key address matches the RAV service provider.

## Public Repo Safety Checklist

Before publishing operational docs:

- No private keys.
- No bearer token literals.
- No database passwords or DSNs with real credentials.
- No private cluster hostnames unless intentionally public.
- Only public testnet addresses, public RPC URLs, placeholder hostnames, and placeholder secrets.
- Any copied Kubernetes Secret examples use placeholder values.

## Current Known Gap

The direct-provider runtime, funding CLI, signer CLI, authenticated operator API, read-only operator CLI, and manual collect CLI are implemented. The remaining external dependency for full public testnet settlement is the real Horizon data-service provisioning path for `SubstreamsDataService`. Until that provision exists, provider registration and collection can correctly revert even when payment sessions and RAV acceptance work.
