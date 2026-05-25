# MVP Operator Authentication Contract

This document is the canonical MVP reference for `MVP-028`.

It defines the authentication and authorization contract for provider operator APIs and the expected treatment of oracle governance during MVP.

## Scope

This contract applies to provider-side operator/admin surfaces, including:

- runtime/session/payment inspection
- low-funds and status inspection
- settlement-relevant accepted and collectible RAV retrieval
- collection lifecycle inspection
- future mutating provider operator/admin actions

For MVP, the existing minimal `GetSessionStatus` endpoint may remain a runtime-coordination surface used by the consumer sidecar. It should stay intentionally narrow: active state, terminal reason, payment-control pending state, and coarse payment values needed to resolve runtime behavior.

Richer provider inspection/status APIs should be separate authenticated operator surfaces. If `GetSessionStatus` grows into that richer inspection role, it should either require `operator.read` authentication or be split so consumer runtime coordination does not require operator credentials.

This contract does not apply to the public runtime payment protocol:

- `StartSession`
- `SubmitRAV`
- `PaymentSession`
- internal plugin auth/session/usage services

## Oracle Governance In MVP

For MVP, oracle whitelist and provider metadata management may remain a deployment-managed internal admin/council workflow rather than a public management API.

The requirement is that whitelist/provider metadata changes are not publicly writable. If a public oracle admin API is added later, it should reuse this same bearer-token role contract rather than inventing a new mechanism.

## Authentication Mechanism

Protected operator/admin endpoints use standard HTTP or Connect metadata:

- `Authorization: Bearer <token>`

Static configured bearer tokens are sufficient for MVP.

The reusable helper for this contract lives in [internal/operatorauth/operatorauth.go](/home/juan/GraphOps/substreams/data-service/internal/operatorauth/operatorauth.go).

## Provider Gateway Configuration

Provider operator APIs are configured separately from the public payment protocol. The provider gateway command accepts:

- `--operator-listen-addr`
- `--operator-read-token-env`
- `--admin-write-token-env`

The operator listener is disabled unless `--operator-listen-addr` is set. When it is set, the provider starts a separate private operator listener. Both token env-name flags are required, and startup fails if either environment variable is missing, empty, or contains whitespace that cannot be used as a bearer token.

There are no production token defaults. Local/dev workflows must set explicit test token environment variables before enabling the operator listener.

The checked-in reflex development stacks enable the private operator listener on `:9010` and set explicit local-only fallback tokens for that process:

- `SDS_OPERATOR_READ_TOKEN=local-operator-read-token`
- `SDS_ADMIN_WRITE_TOKEN=local-admin-write-token`

Operators may override those environment variables before running reflex. These values are only for local development and must not be reused in deployed environments.

The reflex fallback values are scoped to the provider process. Operator CLI commands running in a separate shell must either export the same values and use `--operator-token-env`, or pass the local token with `--operator-token`.

The resolved tokens are carried in `gateway.Config` as `operatorauth.Config`; provider operator handlers should call the gateway authorization helper before reading or mutating provider operator state.

The provider operator API is exposed as a separate `ProviderOperatorService` on the private operator listener. Session, accepted RAV, collection retrieval RPCs, and private operator `/metrics` require `operator.read`. Collection lifecycle mutation RPCs require `admin.write`, including manual collection CLI state updates before and after locally submitted collect transactions.

## Roles

- `operator.read`
  - inspection and retrieval endpoints
- `admin.write`
  - mutating operator/admin actions
  - also satisfies `operator.read`

## Authorization Rules

- Missing `Authorization` header: reject as unauthenticated
- Malformed bearer header: reject as unauthenticated
- Unknown bearer token: reject as unauthenticated
- Valid token without sufficient privilege: reject as permission denied
- `admin.write` token may access read-only operator endpoints
- `operator.read` token may not access mutating admin endpoints

## Transport Assumptions

This contract is intentionally separate from transport posture:

- `MVP-021` defines TLS as the default non-dev transport posture
- protected operator/admin endpoints should run over TLS outside local/dev usage
- the reflex devenv is the checked-in plaintext exception and passes plaintext flags explicitly with local test tokens
