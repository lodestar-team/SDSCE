# Operator Funding CLI

This document describes the payer/operator CLI flow for preparing consumer-side SDS usage on a real chain.

The commands are production-facing and intentionally do not default RPC endpoints, chain IDs, contract addresses, payer addresses, receiver addresses, or private keys. Demo-only deterministic values remain under `sds demo ...`.

## Funding Status

Check wallet balance, `PaymentsEscrow` allowance, and escrow balance for a payer/collector/receiver tuple:

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

`--receiver-address` is the provider/service-provider address in the escrow pair.

## Approve Escrow Spending

Approve `PaymentsEscrow` to spend payer GRT:

```bash
SDS_PAYER_KEY=0x... sds consumer funding approve \
  --rpc-endpoint=https://arb1.example/rpc \
  --chain-id=42161 \
  --grt-token-address=0x... \
  --escrow-address=0x... \
  --payer-private-key-env=SDS_PAYER_KEY \
  --amount="1000 GRT"
```

The command refuses non-zero to non-zero allowance replacement by default. Use `--reset-first` to submit `approve(escrow, 0)` before the requested allowance, or `--force` to submit the replacement directly.

## Deposit And Top Up

`deposit` is a direct action and always adds the requested amount:

```bash
SDS_PAYER_KEY=0x... sds consumer funding deposit \
  --rpc-endpoint=https://arb1.example/rpc \
  --chain-id=42161 \
  --escrow-address=0x... \
  --collector-address=0x... \
  --receiver-address=0x... \
  --payer-private-key-env=SDS_PAYER_KEY \
  --amount="100 GRT"
```

`top-up` is repeatable. It queries current escrow balance and deposits only the difference needed to reach the target:

```bash
SDS_PAYER_KEY=0x... sds consumer funding top-up \
  --rpc-endpoint=https://arb1.example/rpc \
  --chain-id=42161 \
  --grt-token-address=0x... \
  --escrow-address=0x... \
  --collector-address=0x... \
  --receiver-address=0x... \
  --payer-private-key-env=SDS_PAYER_KEY \
  --target-balance="500 GRT"
```

## Signer Authorization

The safer key-custody flow is to generate a sidecar signer proof offline, then submit it from the payer/operator environment.

Generate proof with the sidecar signer key:

```bash
SDS_SIGNER_KEY=0x... sds consumer signer proof \
  --chain-id=42161 \
  --collector-address=0x... \
  --payer-address=0x... \
  --signer-private-key-env=SDS_SIGNER_KEY \
  --deadline=1h
```

Submit proof with the payer key:

```bash
SDS_PAYER_KEY=0x... sds consumer signer authorize \
  --rpc-endpoint=https://arb1.example/rpc \
  --chain-id=42161 \
  --collector-address=0x... \
  --payer-private-key-env=SDS_PAYER_KEY \
  --signer-address=0x... \
  --proof=0x... \
  --proof-deadline=1778112000
```

A convenience mode can generate and submit in one command by providing both payer and signer key flags plus `--deadline`, but that places both keys on one machine.

Inspect signer state:

```bash
sds consumer signer status \
  --rpc-endpoint=https://arb1.example/rpc \
  --collector-address=0x... \
  --payer-address=0x... \
  --signer-address=0x...
```

Start thawing, revoke after the contract thawing period, or cancel a pending thaw:

```bash
SDS_PAYER_KEY=0x... sds consumer signer thaw ...
SDS_PAYER_KEY=0x... sds consumer signer revoke ...
SDS_PAYER_KEY=0x... sds consumer signer cancel-thaw ...
```

## Transaction Policy

Transaction commands use EIP-1559 dynamic-fee transactions by default.

Available controls:

- `--gas-limit`
- `--tx-type=dynamic|legacy`
- `--max-fee-per-gas-wei`
- `--max-priority-fee-per-gas-wei`
- `--gas-price-wei`
- `--receipt-timeout`
- `--receipt-poll-interval`
- `--dry-run`
- `--no-wait`

`dynamic` is the default. Use `--tx-type=legacy` for local or named networks that do not handle EIP-1559 transactions correctly.

This tooling is backward-compatible for external `firecore` and Substreams deployments. It adds operator CLI actions only; it does not change shared runtime/plugin contracts, protobufs, or provider runtime behavior.
