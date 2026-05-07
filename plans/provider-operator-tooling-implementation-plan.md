# Provider Operator Tooling Implementation Plan

This document plans the provider-side operator tooling track that follows `MVP-018`.

It is written so a new implementation chat can start from this file and avoid redoing the initial codebase research.

## Scope

This track covers provider-side inspection, settlement data retrieval, collection lifecycle state, and manual collection CLI flows.

It intentionally does not reopen consumer/payer funding work from `MVP-018`, does not add runtime automatic collection, and does not implement a background settlement agent.

The work maps to these backlog items:

- `MVP-008`: complete durable provider runtime storage for sessions, usage, and accepted RAV state.
- `MVP-029`: implement provider collection lifecycle transitions and update surfaces.
- `MVP-022`: add authentication and authorization to provider admin/operator APIs.
- `MVP-009`: expose authenticated provider inspection and settlement-data retrieval APIs.
- `MVP-032`: expose authenticated runtime/session/payment inspection APIs and CLI/status flows.
- `MVP-019`: implement provider inspection CLI flows for accepted and collectible RAV data.
- `MVP-020`: implement manual collection CLI flow.

Primary source documents:

- [docs/mvp-scope.md](../docs/mvp-scope.md)
- [plans/mvp-implementation-backlog.md](../plans/mvp-implementation-backlog.md)
- [plans/mvp-gap-analysis.md](../plans/mvp-gap-analysis.md)
- [docs/mvp-implementation-sequencing.md](../docs/mvp-implementation-sequencing.md)
- [docs/operator-auth.md](../docs/operator-auth.md)
- [docs/operator-funding.md](../docs/operator-funding.md)

## Relevant Current Code

### CLI Entry Points

- [cmd/sds/main.go](../cmd/sds/main.go)
  - Current `provider` group only registers `impl.ProviderGatewayCommand`.
  - Consumer operator commands are in the main `cmd/sds` package, not `cmd/sds/impl`.
  - Recommended provider CLI shape is to add a new main-package command such as `providerOperatorCmd`, then register it under the existing `provider` group beside `impl.ProviderGatewayCommand`.

- [cmd/sds/impl/provider_gateway.go](../cmd/sds/impl/provider_gateway.go)
  - Starts the public Payment Gateway and private Plugin Gateway.
  - Current provider gateway flags include:
    - `--grpc-listen-addr`
    - `--plugin-listen-addr`
    - `--service-provider`
    - `--chain-id`
    - `--collector-address`
    - `--escrow-address`
    - `--rpc-endpoint`
    - `--data-plane-endpoint`
    - `--pricing-config`
    - TLS/plaintext flags
    - `--repository-dsn`
  - No operator read/admin token flags exist yet.

- [cmd/sds/consumer_common.go](../cmd/sds/consumer_common.go)
  - Contains useful CLI patterns from `MVP-018`:
    - address parsing helpers
    - payer key parsing
    - tx flags
    - RPC client lifecycle
    - GRT formatting
  - New provider CLI code can reuse these helpers if implemented in the main `cmd/sds` package.
  - Keep following repo CLI rules from [AGENTS.md](../AGENTS.md): `cli.Ensure` for required fields, non-Must parsing plus `cli.NoError` with contextual messages.

- [cmd/sds/consumer_funding.go](../cmd/sds/consumer_funding.go)
  - Good reference for production-facing chain command flags, timeout handling, `--dry-run`, `--no-wait`, and EIP-1559 defaults.

- [cmd/sds/tools_rav.go](../cmd/sds/tools_rav.go)
  - Existing local RAV create/inspect tooling.
  - Useful for formatting and RAV conversion examples.
  - Not provider-backed and must not be treated as satisfying `MVP-019` or `MVP-020`.

### Provider Gateway And Runtime State

- [proto/graph/substreams/data_service/provider/v1/gateway.proto](../proto/graph/substreams/data_service/provider/v1/gateway.proto)
  - Current public Payment Gateway service has:
    - `StartSession`
    - `GetSessionStatus`
    - `SubmitRAV`
    - `PaymentSession`
  - `GetSessionStatus` is intentionally narrow runtime coordination, not the richer authenticated operator API.
  - New operator APIs should not overload `GetSessionStatus` into a broad unauthenticated inspection surface.

- [provider/gateway/gateway.go](../provider/gateway/gateway.go)
  - `Gateway` owns the public payment protocol and has access to:
    - service provider address
    - Horizon domain
    - collector and escrow addresses
    - escrow and collector queriers
    - repository
    - runtime payment manager
  - Any operator service attached to the gateway can reuse this repository and runtime state, but must be authenticated.

- [provider/gateway/handler_get_session_status.go](../provider/gateway/handler_get_session_status.go)
  - Shows current narrow status response:
    - active
    - payment status values
    - terminal end reason
    - payment-control pending
  - This endpoint remains public/runtime-adjacent unless intentionally split or protected.

- [provider/gateway/runtime_manager.go](../provider/gateway/runtime_manager.go)
  - Owns live `PaymentSession` stream bindings and pending provider-originated payment control.
  - Operator inspection may need read-only runtime snapshots from this manager, but avoid exposing or mutating internals under broad locks.

### Repository And Persistence

- [provider/repository/repository.go](../provider/repository/repository.go)
  - `GlobalRepository` currently supports:
    - session create/get/update/touch/list/count
    - runtime state updates
    - accepted RAV plus baseline updates
    - collection lifecycle create/list/get/update transitions
    - usage aggregation
    - workers
    - quotas
  - `Session` includes `CurrentRAV`, aggregate usage, baseline usage, payer, receiver, data service, status, metadata, and end reason.
  - `CollectionRecord` tracks settlement lifecycle state separately from runtime sessions.

- [provider/repository/inmemory.go](../provider/repository/inmemory.go)
  - In-memory implementation of current repository contract.
  - Includes collection lifecycle support in parallel with PostgreSQL.

- [provider/repository/psql/migrations/000001_init_schema.up.sql](../provider/repository/psql/migrations/000001_init_schema.up.sql)
  - Current tables: `sessions`, `ravs`, `collection_records`, `workers`, `quota_usage`, `usage_events`.
  - `ravs` is currently one-to-one with `sessions`.
  - `collection_records` stores settlement lifecycle state separately from runtime session rows.

- [provider/repository/psql/session.go](../provider/repository/psql/session.go)
  - Persists current accepted RAV in `ravs`.
  - `SessionUpdateRAVAndBaseline` updates baseline and upserts the latest RAV transactionally.

- [provider/repository/psql/sql/session/get_rav.sql](../provider/repository/psql/sql/session/get_rav.sql)
  - Fetches the latest RAV by `session_id`.

### Authentication

- [docs/operator-auth.md](../docs/operator-auth.md)
  - Canonical auth contract for `MVP-028`.
  - Protected provider operator/admin endpoints use `Authorization: Bearer <token>`.
  - Roles:
    - `operator.read`
    - `admin.write`
  - `admin.write` satisfies `operator.read`.
  - `GetSessionStatus` may remain a narrow runtime-coordination endpoint.

- [internal/operatorauth/operatorauth.go](../internal/operatorauth/operatorauth.go)
  - Reusable helper:
    - `Config`
    - `RoleOperatorRead`
    - `RoleAdminWrite`
    - `AuthorizeHeader`
  - Use this for provider operator endpoints rather than introducing a separate auth mechanism.

### Chain And Contract Helpers

- [contracts/chain/client.go](../contracts/chain/client.go)
  - Shared go-ethereum transaction helper from `MVP-018`.
  - Uses dynamic-fee EIP-1559 transactions by default and supports explicit legacy transactions.
  - Use this for manual collect transaction submission.

- [contracts/horizon/collector.go](../contracts/horizon/collector.go)
  - Loads `GraphTallyCollector` ABI.
  - Currently exposes signer authorization methods only.
  - May need read helpers such as `tokensCollected` queries.
  - Do not implement direct `GraphTallyCollector.collect` for MVP unless the production contract/caller model is revisited.

- [contracts/horizon/escrow.go](../contracts/horizon/escrow.go)
  - Loads `PaymentsEscrow` ABI and is already used by funding commands.

- [horizon/types.go](../horizon/types.go), [horizon/signed_message.go](../horizon/signed_message.go), [sidecar/convert.go](../sidecar/convert.go)
  - Domain RAV types and protobuf conversion helpers.
  - Use these for API payload conversions and CLI display.

- [test/integration/setup_test.go](../test/integration/setup_test.go)
  - Contains current test-only helpers for encoding `SubstreamsDataService.collect(indexer, paymentType, data)`.
  - The implementation should move the reusable collect calldata encoder out of tests into production code, likely under `contracts/horizon`.

- [horizon/devenv/build/contracts/SubstreamsDataService.sol](../horizon/devenv/build/contracts/SubstreamsDataService.sol)
  - Minimal SDS data service contract.
  - `collect(address indexer, IGraphPayments.PaymentTypes paymentType, bytes calldata data)` decodes `(SignedRAV, uint256 dataServiceCut)` and calls `GraphTallyCollector.collect(...)`.

### Protobuf Generation

- [buf.yaml](../buf.yaml)
- [buf.gen.yaml](../buf.gen.yaml)

Use `buf generate` after editing protobuf files. Generated Go lands under [pb/](../pb).

## Recommended Sequencing

Treat this as one provider-operator tooling track, but implement it in bounded tasks. The boundaries are strong enough that a single large task would be hard to review safely.

### Task 1: Close Durable Accepted RAV State For MVP-008

Status: complete.

Goal: prove accepted RAV state survives provider restart through the durable repository path.

Why first:

- `MVP-019` and `MVP-020` depend on provider-backed accepted RAV state.
- The backlog previously tracked current persistence as in progress because restart-focused accepted-state proof remained open.
- This is now closed by PostgreSQL-backed persistence coverage proving accepted RAV, settlement tuple, usage totals, and baseline state survive reopening the repository against the same durable schema.

Likely files:

- [provider/repository/repository.go](../provider/repository/repository.go)
- [provider/repository/inmemory.go](../provider/repository/inmemory.go)
- [provider/repository/psql/session.go](../provider/repository/psql/session.go)
- [provider/repository/psql/sql/session/get_rav.sql](../provider/repository/psql/sql/session/get_rav.sql)
- [test/integration/](../test/integration/)

Implementation shape:

- Add or unskip a restart-focused integration/persistence test.
- Create a session in the PostgreSQL repository path.
- Commit an accepted RAV using `SessionUpdateRAVAndBaseline`.
- Recreate the provider/repository instance against the same database.
- Assert the accepted RAV and settlement tuple are still retrievable.

Validation:

- `go test ./provider/repository/...`
- `go test ./test/integration -run '<restart accepted RAV test name>' -count=1 -v`
  - Use this only if the focused proof is added under `test/integration`.
  - A repository-level PostgreSQL persistence test does not require the local dummy-blockchain image.
  - If the proof is implemented through the real Firecore/dummy-blockchain runtime path, run it with the local SDS-compatible image:
    - `SDS_TEST_DUMMY_BLOCKCHAIN_IMAGE=ghcr.io/streamingfast/dummy-blockchain:sds-local go test ./test/integration -run '<Firecore-focused test name>' -timeout 3m -count=1 -v`
- `go test ./...`
- `go vet ./...`

### Task 2: Add Collection Lifecycle Persistence For MVP-029

Status: complete.

Goal: model and persist collection lifecycle independently from runtime session state.

Why second:

- `MVP-009` retrieval should expose accepted and collectible state, not invent lifecycle semantics in handlers.
- `MVP-020` needs idempotency and retry rules before it submits chain transactions.

Recommended lifecycle states:

- `collectible`
- `collect_pending`
- `collected`
- `collect_failed_retryable`

Recommended repository model:

- Add a `CollectionRecord` domain type.
- Key records by `session_id` plus the settlement tuple:
  - `collection_id`
  - `payer`
  - `receiver` / service provider
  - `data_service`
- Store:
  - latest signed RAV or reference to `ravs`
  - value aggregate
  - state
  - attempt count
  - last tx hash
  - last error
  - collected amount if known
  - created/updated timestamps

Recommended transition rules:

- New accepted RAV creates or updates a `collectible` record when there is no pending/collected record for the same exact accepted value.
- `collectible -> collect_pending` when an operator starts collection.
- `collect_pending -> collected` when a receipt succeeds.
- `collect_pending -> collect_failed_retryable` when tx submission or receipt fails in a retryable way.
- `collect_failed_retryable -> collect_pending` for retry.
- Avoid moving `collected` backward without an explicit admin-only corrective path.

Important race rule:

- By default, collect only terminated sessions or records with no active payment-control work.
- If collecting from active sessions is required, make it explicit with a flag or API field such as `allow_active`, and document the race with later RAV updates.

Likely files:

- [provider/repository/repository.go](../provider/repository/repository.go)
- [provider/repository/inmemory.go](../provider/repository/inmemory.go)
- [provider/repository/psql/migrations/](../provider/repository/psql/migrations/)
- [provider/repository/psql/mappings.go](../provider/repository/psql/mappings.go)
- [provider/repository/psql/sql/](../provider/repository/psql/sql/)
- [provider/repository/psql/statements.go](../provider/repository/psql/statements.go)
- [provider/repository/psql/repository_test.go](../provider/repository/psql/repository_test.go)
- [provider/repository/inmemory_test.go](../provider/repository/inmemory_test.go)

Validation:

- Unit tests covering all legal transitions.
- Tests rejecting illegal stale or backwards transitions.
- PostgreSQL migration test coverage.

### Task 3: Add Provider Operator Auth Wiring For MVP-022

Status: complete.

Goal: provider gateway can serve protected operator APIs with configured read/admin bearer tokens.

Why before API handlers:

- API handlers should not land as unauthenticated public surfaces.
- The auth role contract is already frozen in `MVP-028`.

Recommended provider gateway flags:

- `--operator-listen-addr`
- `--operator-read-token-env`
- `--admin-write-token-env`

Do not add token defaults. Local/dev must set explicit test tokens.

Recommended implementation:

- Serve provider operator APIs on a separate listener by default.
  - This gives operators a firewall/routing boundary and prevents expensive operator listing/export requests from sharing the same public runtime listener as `StartSession` and `PaymentSession`.
  - It costs another listener, another health/transport setup path, and potentially another TLS certificate/config block.
  - If a same-listener fallback is added for local/dev, keep it explicit.
- Add token env flags to `ProviderGatewayCommand`.
- Resolve env vars at startup and fail fast if protected operator service is enabled without usable tokens.
- Add an `operatorauth.Config` to `gateway.Config` and `Gateway`.
- Add a small helper in `provider/gateway`, for example `authorizeOperator(req.Header(), operatorauth.RoleOperatorRead)`.
- `operator.read` endpoints require `RoleOperatorRead`.
- lifecycle mutation endpoints require `RoleAdminWrite`.

Likely files:

- [cmd/sds/impl/provider_gateway.go](../cmd/sds/impl/provider_gateway.go)
- [provider/gateway/gateway.go](../provider/gateway/gateway.go)
- [internal/operatorauth/operatorauth.go](../internal/operatorauth/operatorauth.go)
- [internal/operatorauth/operatorauth_test.go](../internal/operatorauth/operatorauth_test.go)
- [docs/operator-auth.md](../docs/operator-auth.md)

Validation:

- Missing token rejects as unauthenticated.
- Malformed token rejects as unauthenticated.
- `operator.read` works on read endpoints.
- `operator.read` fails on mutating endpoints.
- `admin.write` works on read and mutating endpoints.

Implemented:

- Added disabled-by-default provider operator command flags:
  - `--operator-listen-addr`
  - `--operator-read-token-env`
  - `--admin-write-token-env`
- Startup resolves the configured read/admin token environment variables and fails fast when the operator listener is enabled without usable tokens.
- `gateway.Config` and `Gateway` now carry `operatorauth.Config`.
- `provider/gateway` exposes a small operator authorization helper for future read and admin handlers.
- The provider command starts a separate operator gateway listener when `--operator-listen-addr` is set.
- Focused tests cover command-level token resolution and gateway/operator-listener read/admin authorization behavior.

### Task 4: Add Provider Operator APIs For MVP-009 And MVP-032

Status: complete.

Goal: expose stable, authenticated provider APIs for runtime inspection and settlement retrieval.

Recommended protobuf shape:

- Prefer a new service rather than expanding `PaymentGatewayService`.
- Suggested file: `proto/graph/substreams/data_service/provider/v1/operator.proto`.
- Suggested service: `ProviderOperatorService`.
- Decision: implement a separate service if the wiring is not materially more complex than adding methods to `PaymentGatewayService`.

Recommended read RPCs:

- `ListSessions`
- `GetSession`
- `ListAcceptedRAVs`
- `GetAcceptedRAV`
- `ListCollections`
- `GetCollection`

Recommended mutation RPCs for lifecycle:

- `MarkCollectionPending`
- `MarkCollectionCollected`
- `MarkCollectionRetryable`

Recommended response contents:

- Session:
  - session id
  - status
  - payer
  - receiver/service provider
  - data service
  - created/updated/ended timestamps
  - end reason
  - usage aggregates
  - baseline aggregates
  - payment-control pending if available
  - current accepted RAV summary

- Accepted RAV:
  - session id
  - collection id
  - payer
  - receiver/service provider
  - data service
  - timestamp
  - value aggregate as `common.v1.GRT`
  - signed RAV as `common.v1.SignedRAV`
  - lifecycle state if a collection record exists

- Collection:
  - lifecycle state
  - settlement tuple
  - latest signed RAV
  - value aggregate
  - attempt count
  - last tx hash
  - last error
  - collected amount if known
  - timestamps

Pagination/filtering:

- MVP can use simple request fields:
  - `limit`
  - `page_token` if cheap to support, otherwise document no pagination for MVP and keep limits conservative.
  - `session_id`
  - `payer`
  - `receiver`
  - `data_service`
  - `collection_id`
  - `state`
  - `include_rav`

Security:

- All read RPCs require `operator.read`.
- All lifecycle mutation RPCs require `admin.write`.
- Do not require operator auth on the existing runtime protocol RPCs unless they are split or explicitly repurposed.
- Keep the operator API as a separate Connect service even if it shares internal repository/runtime objects with the payment gateway.
  - Organization benefit: it keeps generated clients, handler files, and docs separate from the consumer/provider runtime protocol.
  - Security benefit: it avoids accidentally exposing richer inspection or mutation RPCs through client code paths that were originally public runtime APIs.
  - Operational benefit: it can be bound to a separate listener and rate-limited/firewalled independently.
  - Cost: it adds one more proto/service/client surface and a bit more startup wiring.

Likely files:

- [proto/graph/substreams/data_service/provider/v1/](../proto/graph/substreams/data_service/provider/v1/)
- [pb/](../pb/)
- [provider/gateway/](../provider/gateway/)
- [cmd/sds/impl/provider_gateway.go](../cmd/sds/impl/provider_gateway.go)

Generation:

- Run `buf generate`.
- Then run `gofmt` on changed generated or hand-written Go files if needed.

Validation:

- Handler tests for auth and filtering.
- Integration coverage for listing/fetching settlement-relevant accepted state.
- `go test ./provider/gateway ./provider/repository/...`
- `go test ./...`
- `go vet ./...`

Implemented:

- Added a separate `ProviderOperatorService` in `proto/graph/substreams/data_service/provider/v1/operator.proto`.
- Registered the generated Connect service on the private provider operator listener.
- Added authenticated read RPCs for sessions, accepted RAVs, and collection lifecycle records.
- Added authenticated admin RPCs for marking collection records pending, collected, and retryable.
- Session inspection includes status, settlement tuple, timestamps, accumulated usage, baseline usage, accepted RAV summary when requested, and payment-control pending state.
- Accepted RAV and collection retrieval expose settlement tuple, signed RAV, aggregate value, lifecycle state, attempts, tx/error fields, collected amount, and timestamps.
- Focused Connect handler tests cover missing auth, read-token read success, read-token mutation denial, admin mutation success, retrieval paths, filters, stale expected-value rejection, and invalid collection keys.

### Task 5: Add Read-Only Provider CLI For MVP-019

Status: complete.

Goal: expose operator inspection CLI commands backed by provider APIs.

Recommended command group:

```text
sds provider operator ...
```

Recommended shared flags:

- `--provider-endpoint`
- `--operator-token-env`
- `--operator-token`
- `--plaintext`
- `--timeout`
- `--format=text|json`

Defaults:

- No provider endpoint default.
- No token default.
- TLS/secure transport by default.
- Plaintext must be explicit and documented as local/dev.

Recommended commands:

```text
sds provider operator sessions list
sds provider operator sessions get --session-id=<id>
sds provider operator ravs list
sds provider operator ravs get --session-id=<id>
sds provider operator collections list
sds provider operator collections get --collection-id=<hex> --payer-address=<addr> --receiver-address=<addr> --data-service-address=<addr>
```

Useful filters:

- `--payer-address`
- `--receiver-address`
- `--data-service-address`
- `--collection-id`
- `--state`
- `--status`
- `--include-rav`
- `--limit`

Output:

- Text output should be stable and scan-friendly, similar to `consumer funding status`.
- JSON output should serialize the proto response or a small stable CLI struct.
- For RAVs, include base64/protobuf export only behind an explicit `--include-rav` or `--rav-output` option so normal lists stay readable.

Likely files:

- [cmd/sds/main.go](../cmd/sds/main.go)
- New files under [cmd/sds/](../cmd/sds/), likely:
  - `provider_operator.go`
  - `provider_operator_client.go`
  - `provider_operator_sessions.go`
  - `provider_operator_ravs.go`
  - `provider_operator_collections.go`

Validation:

- Unit tests for flag validation and output formatting where practical.
- Integration smoke test against an in-process provider gateway with test operator token.

Implemented:

- Added `sds provider operator sessions list|get`.
- Added `sds provider operator ravs list|get`.
- Added `sds provider operator collections list|get`.
- Commands use the generated `ProviderOperatorService` client and only call read RPCs.
- CLI requires explicit provider endpoint and exactly one operator token source.
- Schemeless endpoints default to HTTPS; plaintext HTTP requires explicit `--plaintext`.
- Text and JSON output are supported, with RAV protobuf payloads shown only behind explicit `--include-rav`.
- Focused tests cover endpoint security parsing, token resolution, and text formatting.

### Task 6: Add Manual Collection CLI For MVP-020

Status: complete.

Goal: fetch provider settlement state, craft/sign/submit collect transaction locally, and update provider lifecycle state safely.

Recommended command:

```text
sds provider operator collect \
  --provider-endpoint=https://provider.example \
  --operator-token-env=SDS_ADMIN_TOKEN \
  --rpc-endpoint=https://arb1.example/rpc \
  --chain-id=42161 \
  --data-service-address=0x... \
  --provider-private-key-env=SDS_PROVIDER_KEY \
  --collection-id=0x... \
  --payer-address=0x... \
  --receiver-address=0x... \
  --data-service-cut-ppm=100000
```

Required provider/API inputs:

- provider endpoint
- admin token
- collection identity or session id

Required chain inputs:

- RPC endpoint
- chain id
- data service address, unless accepted RAV data service is always trusted and enough
- provider/operator private key
- data service cut PPM when collecting through `SubstreamsDataService.collect`

Transaction flags:

- Reuse `MVP-018` tx flags:
  - `--tx-type=dynamic|legacy`
  - `--gas-limit`
  - `--max-fee-per-gas-wei`
  - `--max-priority-fee-per-gas-wei`
  - `--gas-price-wei`
  - `--receipt-timeout`
  - `--receipt-poll-interval`
  - `--dry-run`
  - `--no-wait`

Recommended behavior:

1. Fetch collection record from provider.
2. Validate state is `collectible` or `collect_failed_retryable`.
3. Allow collection from active sessions, but use the exact signed RAV snapshot returned by the provider and make the state update conditional on that RAV identity/value.
4. Validate the tx sender address matches or is expected to be authorized for the provider/indexer.
5. Mark collection pending through provider API before submitting, unless `--dry-run`.
6. Encode collection calldata for the selected collect target.
   - MVP target: `SubstreamsDataService.collect(indexer, QueryFee, abi.encode(signedRAV, dataServiceCut))`.
   - Do not add a direct `GraphTallyCollector.collect` CLI mode in MVP.
7. Submit EIP-1559 dynamic-fee tx by default through [contracts/chain/client.go](../contracts/chain/client.go).
8. If waiting for receipt and receipt succeeds, mark collection collected with tx hash/block/amount if available.
9. If tx submission or receipt wait fails after pending, mark retryable with tx hash/error if known.
10. If `--no-wait`, leave state as `collect_pending` and print explicit follow-up instructions.

Production code to add:

- Move collect calldata encoding out of [test/integration/setup_test.go](../test/integration/setup_test.go) into `contracts/horizon`.
- Add a wrapper for `SubstreamsDataService` artifact if needed, for example:
  - `contracts/horizon/data_service.go`
  - `PackCollect(indexer, paymentType, data)`
- Add collector read helpers if useful for verification:
  - `PackTokensCollected(dataService, collectionID, receiver, payer)`
- Add helper to convert `horizon.SignedRAV` signature into the Solidity expected `R || S || V` bytes. The test helper currently performs this conversion manually.
- Treat `--data-service-cut-ppm` as an explicit CLI placeholder input for MVP.
  - The current integration tests use `100000` PPM.
  - Use `100000` as the documented placeholder value because it represents 10% in PPM.
  - Do not infer a production default.
  - The value can later move into provider config, persisted collection state, or an on-chain query once the production source is chosen.

Idempotency policy:

- Collection command should be safe to rerun.
- If state is `collect_pending`, default should be to refuse and show pending tx metadata.
- Add explicit `--retry-pending` only if the previous pending record is old enough or the operator confirms replacement.
- If state is `collected`, default should be no-op and show collected tx metadata.
- For active-session collection, lifecycle mutations should carry an expected RAV identity or expected aggregate value so a late state update cannot mark a newer accepted RAV as collected because an older transaction succeeded.

Validation:

- Unit tests for calldata encoding against the existing integration helper expectations.
- Repository lifecycle transition tests.
- Integration scenario:
  - run a paid session or create accepted RAV state
  - list collectible state
  - run CLI collection
  - verify on-chain tokens collected
  - verify provider collection state is `collected`
  - retry command and verify safe no-op

Implementation evidence:

- Added production `SubstreamsDataService` calldata helpers under `contracts/horizon`.
- Moved the integration test helper for `SubstreamsDataService.collect` payload encoding onto the production encoder.
- Added `sds provider operator collect` using the authenticated `ProviderOperatorService`, required admin bearer token, explicit provider key flags, explicit chain/RPC flags, and required `--data-service-cut-ppm`.
- The command fetches provider collection state, validates the exact signed RAV snapshot, validates the provider key and data service address, marks pending before submission unless `--dry-run`, submits through `contracts/chain`, marks collected after a successful waited receipt, and marks retryable after submission/receipt failures when pending was set.
- `--no-wait` leaves the record pending and prints follow-up instructions; already-collected records return as an idempotent no-op.
- Focused coverage exercises collect calldata packing, required collect record validation, and the pending transition without a pre-submission tx hash.

## Proposed Task Boundaries

Do not implement this as one giant change.

Recommended PR/task boundaries:

1. `MVP-022`: provider operator auth wiring.
2. `MVP-009` + core `MVP-032`: provider operator API service and handlers.
3. `MVP-019`: read-only provider operator CLI.
4. `MVP-020`: manual collect CLI and collect calldata helpers.

`MVP-008` is complete for the accepted RAV restart proof, and `MVP-029` is complete for the repository lifecycle model. Everything else should wait until auth is wired.

## Cross-Cutting Rules

- Use existing project domain types:
  - `sds.GRT` for GRT values.
  - `horizon.SignedRAV` and `common.v1.SignedRAV` for RAVs.
  - existing address wrappers/conversion helpers where possible.
- Keep `*big.Int` at explicit boundaries only:
  - ABI encoding
  - contract calls
  - protobuf conversion
  - third-party APIs
- Do not add hidden local deterministic defaults to production commands.
- Plaintext/insecure transport must be explicit local/dev config.
- Provider operator endpoints must be authenticated.
- Do not hold broad locks across network or chain I/O.
- CLI networking, signing, and chain commands need explicit timeout policy.
- Manual collection must remain operator-triggered only; do not add background collection.

## Documentation Updates Required During Implementation

Update these as each task lands:

- [plans/mvp-implementation-backlog.md](../plans/mvp-implementation-backlog.md)
  - Status and evidence for `MVP-008`, `MVP-029`, `MVP-022`, `MVP-009`, `MVP-032`, `MVP-019`, `MVP-020`.

- [plans/mvp-gap-analysis.md](../plans/mvp-gap-analysis.md)
  - Update acceptance scenario `D` and `F` as provider restart/collection tooling lands.

- [docs/mvp-implementation-sequencing.md](../docs/mvp-implementation-sequencing.md)
  - Remove completed tasks from future sequence as each closes.

- [docs/operator-auth.md](../docs/operator-auth.md)
  - Add concrete provider gateway flags and endpoints once implemented.

- New doc recommended: `docs/provider-operator-tooling.md`
  - Operator-facing guide for session inspection, RAV inspection, collection state, and manual collection.

- [docs/operator-funding.md](../docs/operator-funding.md)
  - Link provider collection tooling once `MVP-020` lands so the payer/provider operator workflow is connected.

- [docs/provider-runtime-compatibility.md](../docs/provider-runtime-compatibility.md)
  - Only update if protobuf/runtime/plugin compatibility changes.

## Open Questions Requiring Input

1. Should the new provider operator API be a separate `ProviderOperatorService` in a new `operator.proto`, or should it be added to the existing `PaymentGatewayService`?
   - Decision: separate service, unless implementation discovers that service registration/generation complexity is materially higher than expected.
   - This is not only organizational. It keeps public consumer runtime protocol and authenticated operator/admin APIs distinct, makes accidental exposure less likely, and allows a separate listener/routing/rate-limit policy.
   - Cost: one more proto service, generated client, handler registration path, and docs surface.

2. Should manual collection go through `SubstreamsDataService.collect` only, or should the CLI also support direct `GraphTallyCollector.collect`?
   - Decision: use `SubstreamsDataService.collect` for MVP.
   - `SubstreamsDataService.collect` appears to be a minimal wrapper that validates the indexer and forwards encoded RAV data to `GraphTallyCollector.collect`.
   - Direct collector mode can be revisited later after confirming which caller is valid on the real contract. The collector ABI includes caller/data-service authorization errors such as `GraphTallyCollectorCallerNotDataService`.

3. What should the MVP source of `dataServiceCut` be for manual collection?
   - Decision: use the easiest explicit placeholder for now.
   - Current tests pass `100000` PPM directly.
   - Use required CLI flag `--data-service-cut-ppm` for `MVP-020`, with examples using `100000` for 10%.
   - Do not silently default this value in production-facing commands.
   - Later options: provider config persisted with collection records, or an on-chain query if a canonical source is available.

4. Should collection be allowed for active sessions?
   - Decision: yes, but implement carefully.
   - Rationale: an operator should not be blocked forever if a client never closes a connection.
   - Implementation requirement: collection must operate on a specific accepted RAV snapshot and state transitions must be conditional on that snapshot, because a later RAV may be accepted while the collection transaction is pending.
   - If this adds too much scope during `MVP-020`, ship terminated-session collection first and track active-session collection as an explicit follow-up.

5. Should collection lifecycle mutation require `admin.write`, or should the collect CLI use an `operator.collect` role distinct from full admin?
   - Current `MVP-028` only defines `operator.read` and `admin.write`.
   - Implication of `admin.write`: simplest implementation, no auth-contract change, but collection operators receive the same role as other future mutating provider-admin actions.
   - Implication of `operator.collect`: better least-privilege key custody for production, but requires expanding `internal/operatorauth`, docs, startup config, tests, and API role checks beyond the frozen two-role MVP contract.
   - Decision: use `admin.write` for MVP. Add `operator.collect` later only if production deployment wants collection operators separated from broader admin operators.

6. Should provider operator APIs live on the same public payment gateway listener, or on a separate operator listener?
   - Decision: separate listener.
   - Rationale: if operator list/export endpoints share the main public runtime listener, expensive or abusive operator requests can compete with `StartSession` and `PaymentSession` traffic.
   - A separate listener lets operators put the API behind private networking, stricter firewall rules, independent rate limits, and separate TLS material.
   - Cost: more config and startup wiring. A same-listener local/dev mode is acceptable only if explicit.

## Next Implementation Prompt

Suggested next prompt for a new chat:

```text
We are in /home/juan/GraphOps/substreams/data-service.
Read AGENTS.md and plans/provider-operator-tooling-implementation-plan.md.
The provider operator tooling track through `MVP-008`, `MVP-029`, `MVP-022`, `MVP-009`, `MVP-019`, `MVP-020`, and `MVP-032` is complete.
Continue with the next MVP backlog item outside this track, likely `MVP-024` observability metrics/log correlation or `MVP-025` acceptance coverage.
Do not add automatic background collection, automatic runtime funding, or unauthenticated operator APIs.
Run focused tests for the chosen task, then go test ./... and go vet ./...
```
