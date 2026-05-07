# MVP-018 Operator Funding CLI Plan

Drafted: 2026-05-06

Status note: this is the historical pre-implementation plan for `MVP-018`. The current implementation is complete and tracked in `plans/mvp-implementation-backlog.md`, with operator usage documented in `docs/operator-funding.md`.

## Purpose

This document captures the research and implementation plan for `MVP-018`: operator/payer funding CLI flows for consumer-side SDS usage.

The goal is to give a payer/operator enough tooling to prepare a consumer sidecar for paid streaming by managing:

- payer GRT allowance to `PaymentsEscrow`
- payer escrow balance for a provider/receiver
- explicit top-up workflows
- sidecar signer authorization and revocation against `GraphTallyCollector`

This is a planning artifact only. It does not implement code.

## Scope Boundary

In scope for `MVP-018`:

- Consumer/payer operator CLI commands for funding and signer setup.
- Read-only funding and authorization status commands.
- Explicit transaction submission for approve, deposit, top-up, signer authorize, signer thaw, and signer revoke.
- Offline signer authorization proof generation.
- Non-demo chain configuration with no deterministic local defaults.
- Secure key handling patterns for CLI usage, especially env-var based private key input.

Out of scope for `MVP-018`:

- Provider RAV collection CLI. That remains `MVP-019` / `MVP-020`.
- Runtime automatic funding decisions.
- Provider-side settlement lifecycle state.
- Wallet UI.
- Provider admin/operator APIs.
- Any local deterministic `devenv` fallback in production-facing commands.

## Planning Inputs Read

The plan was based on:

- `AGENTS.md`
- `docs/mvp-scope.md`
- `plans/mvp-implementation-backlog.md`
- `plans/mvp-gap-analysis.md`
- `docs/mvp-implementation-sequencing.md`
- `plans/current-implementation-review.md`
- current code under `cmd/sds/`, `sidecar/`, `horizon/`, `contracts/`, and `test/integration/`

Important scope notes from the MVP docs:

- `docs/mvp-scope.md` defines funding as an operator/developer workflow, not end-user wallet UI.
- `plans/mvp-implementation-backlog.md` marks `MVP-018` as missing and scoped to approve/deposit/top-up beyond local demo assumptions.
- `plans/mvp-gap-analysis.md` says current funding tooling is only partial and demo-oriented.
- `docs/mvp-implementation-sequencing.md` places `MVP-018` late in the operator tooling lane because it should link to operator runtime/low-funds visibility, but the funding CLI can still be planned independently.
- `plans/current-implementation-review.md` reinforces lessons relevant to this task: avoid hidden insecure defaults, avoid silent demo fallbacks, keep timeout/retry policy explicit, and do not make operator commands look production-safe while relying on local deterministic state.

## Current Code Inventory

### CLI Structure

The root `sds` CLI is assembled in `cmd/sds/main.go`.

Relevant references:

- `cmd/sds/main.go:17` starts the root command.
- `cmd/sds/main.go:26` registers `demo` helpers.
- `cmd/sds/main.go:32` registers `provider` commands.
- `cmd/sds/main.go:44` registers the `consumer` group, currently only `consumer sidecar`.
- `cmd/sds/main.go:50` registers `tools`, currently including RAV tools.

Current gap:

- There is no `consumer funding` or `consumer signer` command group.

### Existing CLI Parsing Pattern

The repo already uses the desired CLI parsing style.

Relevant references:

- `cmd/sds/tools_rav.go:81` uses `cli.Ensure` for required fields.
- `cmd/sds/tools_rav.go:89` parses addresses with `eth.NewAddress` and wraps failures with `cli.NoError`.
- `cmd/sds/tools_rav.go:106` parses GRT values with `sds.ParseGRT`.
- `cmd/sds/consumer_sidecar.go:109` validates required private key and collector flags.
- `cmd/sds/impl/provider_gateway.go:218` validates required provider flags.

Implementation should follow this pattern:

- Use `cli.Ensure` for required field presence.
- Use non-Must parse functions.
- Wrap parse failures with `cli.NoError` and contextual messages.
- Do not use `Must` parsing outside tests or static initialization.

### Demo Funding State

`cmd/sds/demo_setup.go` verifies deterministic local demo state and prints local startup commands.

Relevant references:

- `cmd/sds/demo_setup.go:18` defines `sds demo setup`.
- `cmd/sds/demo_setup.go:43` requires an RPC endpoint.
- `cmd/sds/demo_setup.go:45` uses `horizon/devenv.Connect`.
- `cmd/sds/demo_setup.go:47` verifies default demo state.
- `cmd/sds/demo_setup.go:52` writes deterministic demo env vars.
- `cmd/sds/demo_setup.go:76` prints local provider startup commands.
- `cmd/sds/demo_setup.go:84` prints local consumer sidecar commands.

Important boundary:

- This is explicitly local/demo-oriented.
- It should not be reused as the production funding CLI implementation.
- Production-facing commands must not inherit default local RPC endpoints, deterministic keys, or deterministic contract addresses.

### Dev-Only Chain Mutation Helpers

`horizon/devenv/helpers.go` contains useful references for contract calls, but it is a dev/test package.

Relevant references:

- `horizon/devenv/helpers.go:45` implements `SendTransaction`.
- `horizon/devenv/helpers.go:123` implements `MintGRT`.
- `horizon/devenv/helpers.go:132` implements `ApproveGRTFrom`.
- `horizon/devenv/helpers.go:146` implements `DepositEscrowFor`.
- `horizon/devenv/helpers.go:201` implements `AuthorizeSignerFor`.
- `horizon/devenv/helpers.go:227` implements `ThawSigner`.
- `horizon/devenv/helpers.go:236` implements `RevokeAuthorizedSigner`.
- `horizon/devenv/helpers.go:245` implements two-step `RevokeSigner`.
- `horizon/devenv/helpers.go:256` implements `IsAuthorized`.
- `horizon/devenv/helpers.go:402` implements `GetEscrowBalance`.

Important boundary:

- Do not import `horizon/devenv` from non-demo CLI commands.
- Extract or reimplement reusable chain utilities in a non-dev package.

### Current Transaction Helper Limitation

`horizon/devenv.SendTransaction` signs legacy EIP-155 transactions with `streamingfast/eth-go`.

Relevant references:

- `horizon/devenv/helpers.go:57` fetches nonce.
- `horizon/devenv/helpers.go:66` fetches legacy gas price.
- `horizon/devenv/helpers.go:71` uses fixed gas limit `500000`.
- `horizon/devenv/helpers.go:73` creates a `native.NewPrivateKeySigner`.
- `horizon/devenv/helpers.go:80` signs with `SignTransaction`.
- `horizon/devenv/helpers.go:88` submits with `SendRawTransaction`.
- `horizon/devenv/helpers.go:95` waits for receipt.

The installed `streamingfast/eth-go` signer interface only accepts legacy gas price parameters:

- `go.mod:21` currently depends on `github.com/streamingfast/eth-go`.
- The module does not currently depend on `github.com/ethereum/go-ethereum`.
- The local module cache for `eth-go` shows `signer/interface.go:29` has `SignTransaction(... gasPrice *big.Int ...)`.
- The local module cache for `eth-go` shows `signer/native/signer.go:28` documents EIP-155, not EIP-1559.
- The local module cache for `eth-go` shows `signer/native/signer.go:62` RLP encodes legacy transaction fields.

Implementation decision:

- Use `go-ethereum` for `MVP-018` transaction signing/submission so commands can submit EIP-1559 dynamic fee transactions by default.
- Keep `streamingfast/eth-go` for existing address/domain types where useful.
- Add a small conversion boundary if needed between `eth.Address` and `common.Address`.

### Signer Authorization Proof

`horizon/devenv/authorization.go` implements the proof required by `GraphTallyCollector.authorizeSigner`.

Relevant references:

- `horizon/devenv/authorization.go:26` implements `GenerateSignerProof`.
- `horizon/devenv/authorization.go:37` encodes chain ID as uint256.
- `horizon/devenv/authorization.go:42` appends collector address with packed ABI semantics.
- `horizon/devenv/authorization.go:45` appends the literal `authorizeSignerProof`.
- `horizon/devenv/authorization.go:48` encodes deadline as uint256.
- `horizon/devenv/authorization.go:53` appends authorizer/payer address.
- `horizon/devenv/authorization.go:59` applies the Ethereum signed-message prefix.
- `horizon/devenv/authorization.go:63` signs with the sidecar signer key.
- `horizon/devenv/authorization.go:70` converts `V+R+S` into Solidity `R+S+V`.

Implementation decision:

- Move this function into a non-dev package, for example `horizon/authz` or `contracts/horizon`.
- Keep `horizon/devenv` as a caller of the shared function to avoid duplicate proof logic.
- Add focused unit tests around proof generation and signature layout.

### Contract Artifacts

Shared artifact loading already exists.

Relevant references:

- `contracts/artifacts/embed.go:5` embeds `*.json`.
- `contracts/artifacts/loader.go:18` loads an artifact by name.
- `contracts/artifacts/loader.go:33` loads and parses an ABI.

Relevant embedded artifacts:

- `contracts/artifacts/PaymentsEscrow.json`
- `contracts/artifacts/GraphTallyCollector.json`
- `contracts/artifacts/GraphPayments.json`
- `contracts/artifacts/MockGRTToken.json`

Observed ABI functions:

- `PaymentsEscrow.deposit(address collector,address receiver,uint256 tokens)`
- `PaymentsEscrow.depositTo(address payer,address collector,address receiver,uint256 tokens)`
- `PaymentsEscrow.getBalance(address payer,address collector,address receiver)`
- `PaymentsEscrow.escrowAccounts(address payer,address collector,address receiver)`
- `GraphTallyCollector.authorizeSigner(address signer,uint256 proofDeadline,bytes proof)`
- `GraphTallyCollector.thawSigner(address signer)`
- `GraphTallyCollector.revokeAuthorizedSigner(address signer)`
- `GraphTallyCollector.cancelThawSigner(address signer)`
- `GraphTallyCollector.getThawEnd(address signer)`
- `GraphTallyCollector.isAuthorized(address authorizer,address signer)`
- `MockGRTToken.balanceOf(address)`
- `MockGRTToken.allowance(address owner,address spender)`
- `MockGRTToken.approve(address spender,uint256 value)`

Implementation decision:

- Use existing artifacts for `PaymentsEscrow` and `GraphTallyCollector`.
- Assume these artifacts match the target Horizon/Graph deployment for now.
- If target deployments drift, update/swap artifacts or introduce versioned artifact selection.
- For GRT token interactions, use a minimal real ERC20 ABI in code rather than depending on the mock artifact name.

### Existing Read-Only Escrow And Collector Helpers

The sidecar package has read-only helpers.

Relevant references:

- `sidecar/escrow_querier.go:16` lazily loads `PaymentsEscrow.getBalance`.
- `sidecar/escrow_querier.go:37` creates an `EscrowQuerier`.
- `sidecar/escrow_querier.go:44` calls `getBalance(payer, collector, receiver)`.
- `sidecar/collector_querier.go:13` defines `CollectorAuthorizer`.
- `sidecar/collector_querier.go:24` creates a `CollectorQuerier`.
- `sidecar/collector_querier.go:31` calls `isAuthorized(authorizer, signer)`.

Implementation options:

- Reuse these for read-only status if the CLI continues to use `streamingfast/eth-go/rpc`.
- Prefer a single `go-ethereum` chain client for the new operator CLI to avoid two RPC stacks in the same command.
- If using `go-ethereum`, keep these helpers unchanged for existing runtime code and add operator-specific wrappers elsewhere.

### GRT Domain Type

The project already has the correct GRT type.

Relevant references:

- `grt.go:39` defines `sds.GRT`.
- `grt.go:47` defines dynamic `NewGRT`.
- `grt.go:80` defines `MustNewGRT`, mostly for tests.
- `grt.go:106` defines `NewGRTFromUint64`.
- `grt.go:121` defines `NewGRTFromBigInt`.
- `grt.go:151` defines `ParseGRT`.
- `grt.go:260` defines `BigInt`.
- `grt.go:278` defines `String`.

Implementation rule:

- Parse CLI GRT amounts with `sds.ParseGRT`.
- Use `sds.GRT` inside CLI/business logic.
- Convert to `*big.Int` only at ABI, transaction, protobuf, or third-party API boundaries.
- Do not add local decimal parsing or formatting helpers.

### Provider Runtime Funding Signals

Provider-side low-funds state exists in runtime paths, but it is not an operator funding CLI.

Relevant references:

- `provider/gateway/funds.go:16` defines metadata keys such as `funds_status`.
- `provider/gateway/funds.go:35` defines the funding assessment.
- `provider/gateway/funds.go:75` applies assessment metadata to sessions.
- `provider/gateway/funds.go:104` builds `NeedMoreFunds`.
- `provider/gateway/handler_get_session_status.go:14` implements `GetSessionStatus`.
- `proto/graph/substreams/data_service/provider/v1/gateway.proto:17` documents `GetSessionStatus`.
- `proto/graph/substreams/data_service/provider/v1/gateway.proto:145` defines `NeedMoreFunds`.

Implementation boundary:

- `MVP-018` should not add runtime auto-funding.
- It may optionally print commands that help respond to a low-funds signal.
- Rich provider inspection remains `MVP-032` / `MVP-019`.

## Updated Decisions From Review

### ERC20 ABI

Decision:

- Use a real minimal ERC20 ABI, not `MockGRTToken`, for production-facing GRT token interactions.

Rationale:

- The mock token artifact appears ERC20-compatible, but the operator CLI should not be semantically tied to a mock contract name.
- Minimal required ABI:
  - `balanceOf(address) returns (uint256)`
  - `allowance(address,address) returns (uint256)`
  - `approve(address,uint256) returns (bool)`
  - optional `decimals() returns (uint8)` for sanity/status display only

### EIP-1559

Decision:

- Use `go-ethereum` for transaction submission in `MVP-018`.
- Default transaction type should be EIP-1559 dynamic fee.

Recommended APIs:

- `ethclient.DialContext`
- `Client.PendingNonceAt`
- `Client.EstimateGas`
- `Client.SuggestGasTipCap`
- `Client.HeaderByNumber(ctx, nil)` for latest base fee
- `types.DynamicFeeTx`
- `types.NewTx`
- `types.LatestSignerForChainID`
- `types.SignTx`
- `Client.SendTransaction`
- `bind.WaitMined` or a repo-owned receipt poller with explicit timeout

Default fee policy:

- Fetch latest base fee from the latest header.
- Fetch priority fee with `SuggestGasTipCap`.
- Default `maxFeePerGas = 2 * baseFee + maxPriorityFeePerGas`.
- Expose:
  - `--max-fee-per-gas-wei`
  - `--max-priority-fee-per-gas-wei`
  - `--gas-limit`
  - `--receipt-timeout`
  - `--receipt-poll-interval`
  - `--no-wait`
  - `--dry-run`

Legacy transactions:

- Do not default to legacy.
- Add legacy support only if a named target network requires it, and make it explicit, for example `--tx-type=legacy`.

### Contract ABI Compatibility

Decision:

- Assume current `PaymentsEscrow` and `GraphTallyCollector` artifacts match the target for MVP planning.

Fallback if wrong:

- Swap/update artifacts.
- Or add versioned artifact selection if multiple deployments need different ABIs.

### Allowance Semantics

Decision:

- Guard non-zero to non-zero `approve` replacements.

Default behavior:

- If current allowance is non-zero and requested approval amount is also non-zero, fail with a message explaining the ERC20 allowance race pattern.

Operator overrides:

- `--force` to submit replacement approval anyway.
- Optional `--reset-first` to submit `approve(spender, 0)` and then `approve(spender, amount)` as two explicit transactions.

### Deposit Function

Decision:

- Use plain `PaymentsEscrow.deposit(collector, receiver, amount)`.
- Do not expose `depositTo` in `MVP-018`.

Rationale:

- The normal payer/operator flow is that the payer signs and deposits their own GRT for the collector/receiver pair.
- `depositTo` has different semantics and should wait for a concrete operator need.

### Key Custody

Decision:

- Prefer env-var private key flags in examples.
- Support direct `--*-private-key` only as a convenience with clear help text.
- Support offline signer proof generation.

Recommended key flags:

- `--payer-private-key-env=SDS_PAYER_KEY`
- `--payer-private-key=0x...`
- `--signer-private-key-env=SDS_SIGNER_KEY`
- `--signer-private-key=0x...`

Validation:

- Exactly one of direct key or env-var key should be provided for each required key role.
- If `--payer-address` is also provided, derive the address from the key and fail if it differs.

### Idempotency

Decision:

- Keep both direct deposit and target-based top-up.

Command semantics:

- `deposit --amount`: direct action, not idempotent.
- `top-up --target-balance`: idempotent operator workflow. It queries current escrow balance and submits a deposit only when current balance is below target.

### No Production Defaults

Decision:

- Production-facing commands should have no defaults for:
  - RPC endpoint
  - chain ID
  - contract addresses
  - payer address
  - receiver/provider address
  - private keys

Acceptable defaults:

- Timeouts and poll intervals may have documented safe defaults.
- Demo-only commands may keep local deterministic defaults under `sds demo ...`.

## Proposed CLI Shape

Add two subgroups under `sds consumer`:

- `sds consumer funding ...`
- `sds consumer signer ...`

This places the commands next to `sds consumer sidecar`, which is the consumer-side SDS runtime boundary.

### Shared Funding Flags

Most funding commands should accept:

- `--rpc-endpoint` required
- `--chain-id` required for transactions
- `--grt-token-address` required for ERC20 balance/allowance/approve
- `--escrow-address` required
- `--collector-address` required for escrow pair
- `--payer-address` required for read-only status; optional for tx commands if derived from payer key
- `--receiver-address` required; this is the provider/service-provider address in the escrow pair
- `--rpc-timeout=30s`
- `--receipt-timeout=2m`
- `--receipt-poll-interval=1s`

Transaction commands should also accept:

- `--payer-private-key-env`
- `--payer-private-key`
- `--gas-limit`
- `--max-fee-per-gas-wei`
- `--max-priority-fee-per-gas-wei`
- `--dry-run`
- `--no-wait`

### `consumer funding status`

Example:

```bash
sds consumer funding status \
  --rpc-endpoint=https://arb1.example/rpc \
  --grt-token-address=0x... \
  --escrow-address=0x... \
  --collector-address=0x... \
  --payer-address=0x... \
  --receiver-address=0x... \
  --min-escrow-balance="100 GRT"
```

Behavior:

- Query ERC20 `balanceOf(payer)`.
- Query ERC20 `allowance(payer, escrow)`.
- Query `PaymentsEscrow.getBalance(payer, collector, receiver)`.
- Optionally compare escrow balance to `--min-escrow-balance`.
- Print:
  - payer GRT wallet balance
  - escrow allowance
  - escrow balance for payer/collector/receiver
  - amount needed to reach minimum, if supplied

No transaction.

### `consumer funding approve`

Example:

```bash
sds consumer funding approve \
  --rpc-endpoint=https://arb1.example/rpc \
  --chain-id=42161 \
  --grt-token-address=0x... \
  --escrow-address=0x... \
  --payer-private-key-env=SDS_PAYER_KEY \
  --amount="1000 GRT"
```

Behavior:

- Derive payer address from key.
- Query current allowance.
- If current allowance is non-zero and requested amount is non-zero, fail unless `--force` or `--reset-first` is provided.
- Submit ERC20 `approve(escrow, amount)`.
- Wait for receipt unless `--no-wait`.
- Print tx hash and final allowance if waiting.

Flags:

- `--amount` required.
- `--force` optional.
- `--reset-first` optional.

### `consumer funding deposit`

Example:

```bash
sds consumer funding deposit \
  --rpc-endpoint=https://arb1.example/rpc \
  --chain-id=42161 \
  --escrow-address=0x... \
  --collector-address=0x... \
  --receiver-address=0x... \
  --payer-private-key-env=SDS_PAYER_KEY \
  --amount="100 GRT"
```

Behavior:

- Derive payer address from key.
- Optionally query current escrow balance before transaction.
- Submit `PaymentsEscrow.deposit(collector, receiver, amount)`.
- Wait for receipt unless `--no-wait`.
- If waiting, query and print final escrow balance.

No automatic approval by default.

Optional validation:

- If `--grt-token-address` is provided, pre-check allowance and fail early when insufficient.
- If omitted, rely on contract failure.

### `consumer funding top-up`

Example:

```bash
sds consumer funding top-up \
  --rpc-endpoint=https://arb1.example/rpc \
  --chain-id=42161 \
  --grt-token-address=0x... \
  --escrow-address=0x... \
  --collector-address=0x... \
  --receiver-address=0x... \
  --payer-private-key-env=SDS_PAYER_KEY \
  --target-balance="500 GRT"
```

Behavior:

- Query current escrow balance.
- If current balance is at or above target, print no-op and exit success.
- Otherwise calculate `deposit_amount = target - current`.
- Check allowance when `--grt-token-address` is supplied.
- Submit `deposit(collector, receiver, deposit_amount)`.
- Wait for receipt unless `--no-wait`.
- Print final escrow balance if waiting.

Flags:

- `--target-balance` required.
- Optional `--approve-if-needed` should not be included in first MVP slice unless the team explicitly wants a combined flow. Keeping approve separate is clearer and safer.

### Shared Signer Flags

Read-only signer commands:

- `--rpc-endpoint` required
- `--collector-address` required
- `--payer-address` required
- `--signer-address` required
- `--rpc-timeout=30s`

Transaction signer commands:

- `--rpc-endpoint` required
- `--chain-id` required
- `--collector-address` required
- payer key flags required
- `--signer-address` required except convenience authorization form with signer key
- fee/receipt/dry-run flags

Proof generation:

- `--chain-id` required
- `--collector-address` required
- `--payer-address` required
- signer key flags required
- `--deadline` or `--proof-deadline` required

### `consumer signer status`

Example:

```bash
sds consumer signer status \
  --rpc-endpoint=https://arb1.example/rpc \
  --collector-address=0x... \
  --payer-address=0x... \
  --signer-address=0x...
```

Behavior:

- Query `GraphTallyCollector.isAuthorized(payer, signer)`.
- Query `GraphTallyCollector.getThawEnd(signer)` if available in ABI.
- Optionally query `authorizations(signer)` if useful for revoked/thaw status.
- Print authorization state and thaw end timestamp.

### `consumer signer proof`

Example:

```bash
sds consumer signer proof \
  --chain-id=42161 \
  --collector-address=0x... \
  --payer-address=0x... \
  --signer-private-key-env=SDS_SIDECAR_SIGNER_KEY \
  --deadline=2026-05-07T00:00:00Z
```

Behavior:

- Generate the signer proof offline.
- Print:
  - signer address
  - payer address
  - proof deadline
  - proof hex
- No RPC and no transaction.

Rationale:

- Allows the sidecar signer to create proof without putting the payer key on the same machine.

### `consumer signer authorize`

Two supported modes:

Mode A: payer submits externally generated proof.

```bash
sds consumer signer authorize \
  --rpc-endpoint=https://arb1.example/rpc \
  --chain-id=42161 \
  --collector-address=0x... \
  --payer-private-key-env=SDS_PAYER_KEY \
  --signer-address=0x... \
  --proof=0x... \
  --proof-deadline=1778112000
```

Mode B: convenience local proof generation and submit.

```bash
sds consumer signer authorize \
  --rpc-endpoint=https://arb1.example/rpc \
  --chain-id=42161 \
  --collector-address=0x... \
  --payer-private-key-env=SDS_PAYER_KEY \
  --signer-private-key-env=SDS_SIDECAR_SIGNER_KEY \
  --deadline=1h
```

Behavior:

- In Mode A, use supplied proof and deadline.
- In Mode B, generate proof locally from signer key and deadline.
- Submit `GraphTallyCollector.authorizeSigner(signer, proofDeadline, proof)` from payer.
- If waiting, query `isAuthorized` after receipt.

Validation:

- Require either `(signer-address, proof, proof-deadline)` or `(signer-private-key, deadline)`.
- Fail if both modes are mixed ambiguously.

### `consumer signer thaw`

Example:

```bash
sds consumer signer thaw \
  --rpc-endpoint=https://arb1.example/rpc \
  --chain-id=42161 \
  --collector-address=0x... \
  --payer-private-key-env=SDS_PAYER_KEY \
  --signer-address=0x...
```

Behavior:

- Submit `GraphTallyCollector.thawSigner(signer)` from payer.
- If waiting, query `getThawEnd(signer)` and print timestamp.

### `consumer signer revoke`

Example:

```bash
sds consumer signer revoke \
  --rpc-endpoint=https://arb1.example/rpc \
  --chain-id=42161 \
  --collector-address=0x... \
  --payer-private-key-env=SDS_PAYER_KEY \
  --signer-address=0x...
```

Behavior:

- Submit `GraphTallyCollector.revokeAuthorizedSigner(signer)` from payer.
- Do not automatically thaw unless a separate `--thaw-first` flag is explicitly added later.
- If the thawing period has not elapsed, let the contract failure surface with context.
- If waiting, query `isAuthorized` after receipt.

### Optional `consumer signer cancel-thaw`

Example:

```bash
sds consumer signer cancel-thaw \
  --rpc-endpoint=https://arb1.example/rpc \
  --chain-id=42161 \
  --collector-address=0x... \
  --payer-private-key-env=SDS_PAYER_KEY \
  --signer-address=0x...
```

Behavior:

- Submit `GraphTallyCollector.cancelThawSigner(signer)` from payer.

This can be deferred if the first MVP slice should stay minimal.

## Proposed File And Package Layout

Likely new files:

- `contracts/erc20/erc20.go`
  - Minimal ERC20 ABI.
  - Methods to pack `balanceOf`, `allowance`, and `approve`.

- `contracts/horizon/escrow.go`
  - `PaymentsEscrow` ABI wrappers.
  - `GetEscrowBalance`.
  - `PackDeposit`.

- `contracts/horizon/collector.go`
  - `GraphTallyCollector` ABI wrappers.
  - `GenerateSignerProof`.
  - `IsAuthorized`.
  - `GetThawEnd`.
  - `PackAuthorizeSigner`.
  - `PackThawSigner`.
  - `PackRevokeAuthorizedSigner`.
  - Optional `PackCancelThawSigner`.

- `contracts/chain/client.go`
  - `go-ethereum` client wrapper.
  - Address conversion helpers.
  - `CallContract`.
  - `SendDynamicFeeTransaction`.
  - `WaitReceipt`.
  - Fee and gas estimation policy.

- `cmd/sds/consumer_funding.go`
  - Command definitions and run functions for funding.

- `cmd/sds/consumer_signer.go`
  - Command definitions and run functions for signer lifecycle.

- `cmd/sds/consumer_common.go`
  - Shared flag parsing for addresses, keys, GRT amounts, tx options.

Possible edits:

- `cmd/sds/main.go`
  - Register `consumerFundingCmd` and `consumerSignerCmd` under the `consumer` group.

- `horizon/devenv/authorization.go`
  - Replace local proof implementation with call to shared proof function, or leave until a cleanup step if minimizing churn.

- `docs/operator-funding.md`
  - User-facing funding and signer setup guide.

- `plans/mvp-implementation-backlog.md`
  - Clarify that `MVP-018` includes signer authorization lifecycle.

## Detailed Implementation Steps

### Step 1: Add `go-ethereum`

Add dependency:

```bash
go get github.com/ethereum/go-ethereum@latest
go mod tidy
```

Implementation should use official `go-ethereum` packages for dynamic fee transactions.

Validation:

- `go mod tidy`
- `go test ./contracts/...` after contract packages exist

### Step 2: Build Shared Chain Transaction Utility

Create a package for transaction options and sending.

Suggested types:

```go
type TxOptions struct {
    ChainID *big.Int
    From common.Address
    PrivateKey *ecdsa.PrivateKey
    GasLimit uint64
    MaxFeePerGas *big.Int
    MaxPriorityFeePerGas *big.Int
    ReceiptTimeout time.Duration
    ReceiptPollInterval time.Duration
    NoWait bool
    DryRun bool
}

type TxResult struct {
    Hash common.Hash
    Receipt *types.Receipt
    DryRun bool
}
```

Recommended behavior:

- Use `PendingNonceAt`.
- Use `EstimateGas` unless `--gas-limit` is set.
- Use dynamic fee defaults unless fee flags are set.
- Use context deadlines for RPC calls.
- Return tx hash immediately on `--no-wait`.
- Poll receipt until timeout if waiting.
- Treat receipt `Status == 0` as failure.

Timeout/retry policy:

- No hidden retries for transaction submission.
- Receipt polling is explicit through `--receipt-timeout` and `--receipt-poll-interval`.
- RPC calls should use `--rpc-timeout` contexts.

### Step 3: Build Minimal Contract Wrappers

ERC20:

- Encode/decode `balanceOf`.
- Encode/decode `allowance`.
- Encode `approve`.

Escrow:

- Encode/decode `getBalance`.
- Encode `deposit`.
- Do not expose `depositTo`.

Collector:

- Encode/decode `isAuthorized`.
- Encode/decode `getThawEnd`.
- Encode `authorizeSigner`.
- Encode `thawSigner`.
- Encode `revokeAuthorizedSigner`.
- Optional encode `cancelThawSigner`.
- Move signer proof generation here or to an adjacent package.

Do not use ad hoc selectors when ABI loaders are available. `sidecar/collector_querier.go` currently uses a hardcoded selector, but new code should prefer ABI-based wrappers unless there is a clear reason not to.

### Step 4: Add CLI Common Parsing

Add helpers for:

- required string flag trimming
- optional string flag trimming
- address parse
- GRT parse
- hex bytes parse
- private key parse from direct flag or env var
- signer/payer address derivation and validation
- tx option parsing

Rules:

- `cli.Ensure` for required presence.
- `cli.NoError` for parse errors.
- Exact contextual messages, for example:
  - `invalid <payer-address> %q`
  - `invalid <amount> %q, expected GRT amount like "10 GRT"`
  - `<rpc-endpoint> is required`
  - `exactly one of <payer-private-key> or <payer-private-key-env> is required`

### Step 5: Implement Funding Commands

Order:

1. `status`
2. `approve`
3. `deposit`
4. `top-up`

Keep output plain and script-readable enough:

- Display addresses.
- Display human GRT and raw wei/base-unit amounts.
- Display tx hash.
- Display final status when waiting.

Avoid:

- Automatically combining approve and deposit by default.
- Hidden default contract addresses.
- Local deterministic key fallbacks.

### Step 6: Implement Signer Commands

Order:

1. `proof`
2. `status`
3. `authorize`
4. `thaw`
5. `revoke`
6. Optional `cancel-thaw`

Important detail:

- `authorize` requires the sidecar signer proof. This proof is generated by the signer key and submitted by the payer key.
- The offline `proof` command is the safer operational workflow.
- The convenience `authorize` mode that accepts both payer key and signer key should be clearly documented as less isolated key custody.

### Step 7: Tests

Unit tests:

- GRT flag parsing uses `sds.ParseGRT`.
- Address parsing errors are contextual.
- Private key direct/env mutual exclusion.
- Payer key derives expected payer address.
- Dynamic fee option defaults and overrides.
- ERC20 calldata and decode fixtures.
- Escrow calldata and decode fixtures.
- Collector calldata and signer proof fixtures.
- `approve` rejects non-zero-to-non-zero allowance without `--force`.
- `top-up` computes zero/no-op when current >= target.
- `top-up` computes exact difference when current < target.

Integration tests:

- Use existing `horizon/devenv` in `test/integration`.
- Start from isolated payer/provider where possible.
- Verify:
  - `funding approve` updates allowance.
  - `funding deposit` increases escrow balance.
  - `funding top-up` reaches target and is no-op on second run.
  - `signer proof` output can be passed to `signer authorize`.
  - `signer authorize` makes `isAuthorized` true.
  - `signer thaw` sets thaw timestamp.
  - `signer revoke` makes `isAuthorized` false when thawing period is zero in local setup.

Command-level tests:

- Prefer testing run functions with fake contract/client interfaces where practical.
- Keep full chain tests in integration to avoid brittle CLI subprocess setup.

Validation commands:

```bash
go test ./cmd/sds ./contracts/... ./horizon/...
go test ./test/integration -run 'Test.*Funding|Test.*Signer' -count=1 -v
go test ./...
go vet ./...
```

Run `gofmt` on changed Go files after implementation compiles and before external validation.

## Manual Validation Flow

Against local demo:

1. Start local chain:

```bash
sds devenv
```

2. Capture contract addresses from output or `sds demo setup`.

3. Check funding:

```bash
sds consumer funding status \
  --rpc-endpoint=http://localhost:58545 \
  --grt-token-address=0x... \
  --escrow-address=0x... \
  --collector-address=0x... \
  --payer-address=0x... \
  --receiver-address=0x...
```

4. Approve:

```bash
SDS_PAYER_KEY=0x... sds consumer funding approve \
  --rpc-endpoint=http://localhost:58545 \
  --chain-id=1337 \
  --grt-token-address=0x... \
  --escrow-address=0x... \
  --payer-private-key-env=SDS_PAYER_KEY \
  --amount="100 GRT"
```

5. Top up:

```bash
SDS_PAYER_KEY=0x... sds consumer funding top-up \
  --rpc-endpoint=http://localhost:58545 \
  --chain-id=1337 \
  --grt-token-address=0x... \
  --escrow-address=0x... \
  --collector-address=0x... \
  --receiver-address=0x... \
  --payer-private-key-env=SDS_PAYER_KEY \
  --target-balance="100 GRT"
```

6. Generate signer proof offline:

```bash
SDS_SIGNER_KEY=0x... sds consumer signer proof \
  --chain-id=1337 \
  --collector-address=0x... \
  --payer-address=0x... \
  --signer-private-key-env=SDS_SIGNER_KEY \
  --deadline=1h
```

7. Authorize signer:

```bash
SDS_PAYER_KEY=0x... sds consumer signer authorize \
  --rpc-endpoint=http://localhost:58545 \
  --chain-id=1337 \
  --collector-address=0x... \
  --payer-private-key-env=SDS_PAYER_KEY \
  --signer-address=0x... \
  --proof=0x... \
  --proof-deadline=...
```

8. Verify:

```bash
sds consumer signer status \
  --rpc-endpoint=http://localhost:58545 \
  --collector-address=0x... \
  --payer-address=0x... \
  --signer-address=0x...
```

Against non-demo:

- Same commands, but every endpoint, chain ID, contract address, payer, receiver, and key must be explicitly supplied.
- Do not document deterministic default addresses outside local/demo sections.

## Risks And Open Questions

### ERC20 Token Address

Risk:

- Operators must supply the correct GRT token address for the target chain.

Mitigation:

- No default in production command.
- `funding status` can call `decimals()` and warn if not `18`, but should not rely on this for correctness.

### Dynamic Fee Chain Compatibility

Risk:

- Some local or non-Ethereum-compatible chains may not support EIP-1559.

Mitigation:

- Default to EIP-1559.
- Add explicit legacy support later only if a named target needs it.
- Local Anvil should support EIP-1559 by default; validate in integration.

### ABI Drift

Risk:

- Target deployments could differ from embedded artifacts.

Mitigation:

- Assume current artifacts match for MVP.
- If wrong, update artifacts or add versioned selection.
- Keep CLI wrappers thin so ABI changes do not leak into command semantics.

### Allowance Replacement

Risk:

- ERC20 approval race when changing a non-zero allowance directly to another non-zero value.

Mitigation:

- Default fail.
- Require `--force` or `--reset-first`.

### Deposit Idempotency

Risk:

- `deposit --amount` always adds funds and is not idempotent.

Mitigation:

- Document it as a direct action.
- Promote `top-up --target-balance` as the safer repeatable workflow.

### Key Custody

Risk:

- Combining payer and signer keys on one operator machine weakens key separation.

Mitigation:

- Env-var key flags in examples.
- Offline `signer proof` command.
- Separate proof-generation and payer-submission workflow.

### Receipt Timeout Policy

Risk:

- Hidden timeout values can produce unclear operator behavior.

Mitigation:

- Expose `--receipt-timeout` and `--receipt-poll-interval`.
- Print tx hash before waiting.
- Support `--no-wait`.

### Runtime Integration Assumptions

Risk:

- Funding CLI may be mistaken for runtime authority.

Mitigation:

- Docs should clearly state the provider remains authoritative for live low-funds decisions.
- CLI performs operator actions only.
- No automatic runtime funding decisions.

## Documentation Updates Needed

Before or with implementation:

- Update `plans/mvp-implementation-backlog.md` so `MVP-018` explicitly includes signer authorization/status/thaw/revoke as payer setup.
- Add `docs/operator-funding.md` with:
  - funding flow
  - top-up flow
  - signer proof and authorize flow
  - revoke flow
  - key custody guidance
  - fee/timeout policy
  - demo vs non-demo examples
- Update `docs/mvp-scope.md` only if the team wants signer authorization called out explicitly in the funding workflow.
- Update `plans/mvp-gap-analysis.md` after implementation to move Funding CLI from partial to implemented.

Runtime compatibility note:

- This task is backward-compatible for external `firecore` / Substreams deployments.
- It adds operator CLI tooling and does not change shared runtime/plugin contracts, protobufs, or provider runtime behavior.
