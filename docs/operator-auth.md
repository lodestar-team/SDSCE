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

For MVP, the current `GetSessionStatus` endpoint should be treated as an `operator.read` surface.

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

- `MVP-021` owns TLS-by-default rollout
- protected operator/admin endpoints should be expected to run over TLS outside local/dev usage
- local/dev may still use explicit plaintext transport with local test tokens where needed
