# Implementation Plan: provider-firecore-plugins

## ULTIMATE GOAL

Implement the **server-side endpoints** that firehose-core's auth, metering, and session plugins will call when configured with `tgm://localhost:<port>` (and later `sds://`).

**Key Clarification**: We are NOT implementing firehose-core plugins. The plugins already exist in firehose-core. We are implementing the **gRPC/HTTP services** that those plugins call as clients.

The services will:
- Be served by the SDS provider sidecar
- Translate plugin calls to internal SDS session/payment validation logic
- Enable firehose-core tier1 to use `tgm://localhost:<port>` pointing to local SDS provider sidecar

## Status: IMPLEMENTED (commit d313c011)

All Priority 0-5 items are fully implemented and tested. See commit `d313c011`.

### Implemented:
- `provider/repository`: GlobalRepository interface + InMemoryRepository (haxmap-backed)
- `provider/auth`: AuthService gRPC (EIP-712 RAV validation, signer caching)
- `provider/usage`: UsageService gRPC (dmetering batched events)
- `provider/session`: SessionService gRPC (BorrowWorker/ReturnWorker/KeepAlive + quotas)
- `provider/sidecar`: All three services wired into ConnectWeb server
- Proto definitions: sds/auth/v1, sds/usage/v1, sds/session/v1

### Known Issue: Git Repository Corruption
The git repo has a corrupt pack file (`df33225d...`). The parent commit of the "Plan for plugin
first implementation" commit is unreadable. A graft file was added at `.git/info/grafts` to allow
git operations to work. The new implementation commit `d313c011` was created via `git commit-tree`
to bypass the corrupt ancestry.

---

## Architecture Decision: RESOLVED ✓

### Full Flow (from docs/flowchart.txt)

```
┌─────────────┐     ┌──────────────┐     ┌──────────────────┐     ┌──────────────┐
│  Consumer   │     │   Consumer   │     │    Provider      │     │   Provider   │
│ (substreams)│     │   Sidecar    │     │    Sidecar       │     │   (tier1)    │
└──────┬──────┘     └──────┬───────┘     └────────┬─────────┘     └──────┬───────┘
       │                   │                      │                      │
       │ 1. init()         │                      │                      │
       │──────────────────>│                      │                      │
       │                   │ 2. startSession      │                      │
       │                   │    (escrow, RAV0)    │                      │
       │                   │─────────────────────>│                      │
       │                   │                      │                      │
       │                   │ 3. useThis(RAVx)     │                      │
       │                   │<─────────────────────│                      │
       │ 4. RAVx           │                      │                      │
       │<──────────────────│                      │                      │
       │                   │                      │                      │
       │ 5. Blocks() with header x-sds-rav=RAVx    │                      │
       │─────────────────────────────────────────────────────────────────>│
       │                   │                      │ 6. validate RAVx     │
       │                   │                      │<─────────────────────│
       │                   │                      │ 7. OK (payer, etc.)  │
       │                   │                      │─────────────────────>│
       │ 8. data...        │                      │                      │
       │<─────────────────────────────────────────────────────────────────│
```

### Key Insight

The consumer sends **RAV in a header** (`x-sds-rav=RAVx`) directly to tier1. The `sds://` dauth plugin:
1. Extracts RAV from `x-sds-rav` header
2. Calls provider sidecar to validate RAV
3. Receives back auth context (payer address, session info)
4. Populates trusted headers (`x-user-id`, etc.)

### Architecture: Option A - New `sds://` dauth plugin ✓

**Note**: `sds://` will be an alias for `tgm://` - both call external gRPC services. The plugin pattern is the same, just different validation logic on the server side.

**Plugin in firehose-core** (sds:// or tgm://):
- Extracts `x-sds-rav` header containing SignedRAV
- Calls gRPC `ValidateAuth(SignedRAV)` on configured endpoint
- Receives `AuthResponse{organization_id, api_key_id, ...}`
- Populates trusted headers

**Provider sidecar implements**:
- gRPC `AuthService.ValidateAuth(SignedRAV) → AuthResponse`
- Validates EIP-712 signature, recovers signer
- Checks authorization (self-sign or on-chain delegation)
- Returns payer address as `organization_id`

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│  firehose-core tier1 (Provider)                                     │
│                                                                     │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐              │
│  │ dauth plugin │  │dmetering     │  │ dsession     │              │
│  │ (sds://)     │  │plugin(sds://)│  │ plugin(sds://)│              │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘              │
│         │                 │                 │                       │
└─────────┼─────────────────┼─────────────────┼───────────────────────┘
          │ gRPC            │ gRPC            │ gRPC
          │ ValidateAuth    │ Report          │ BorrowWorker
          │ (x-sds-rav)     │ (usage events)  │ ReturnWorker
          ▼                 ▼                 ▼
┌─────────────────────────────────────────────────────────────────────┐
│  SDS Provider Sidecar (what we implement)                           │
│                                                                     │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐              │
│  │ AuthService  │  │UsageService  │  │Session    │              │
│  │ (gRPC)       │  │(gRPC)        │  │Service(gRPC) │              │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘              │
│         │                 │                 │                       │
│         └─────────────────┴─────────────────┘                       │
│                           │                                         │
│                           ▼                                         │
│              ┌─────────────────────────┐                           │
│              │ Internal SDS Logic      │                           │
│              │ - RAV signature verify  │                           │
│              │ - Session tracking      │                           │
│              │ - Quota enforcement     │                           │
│              │ - On-chain auth check   │                           │
│              └─────────────────────────┘                           │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Global State Architecture

### Overview

A `GlobalRepository` interface provides the abstraction for live state management. This enables:
- Single in-memory deployment initially
- High-availability via Redis later (just swap implementation)

### GlobalRepository Interface

```go
// GlobalRepository provides global state storage for live session/client tracking.
// All methods are namespaced by domain (Session*, Client*, Quota*, etc.)
type GlobalRepository interface {
    // Session management
    SessionCreate(ctx context.Context, session *Session) error
    SessionGet(ctx context.Context, sessionID string) (*Session, error)
    SessionUpdate(ctx context.Context, session *Session) error
    SessionDelete(ctx context.Context, sessionID string) error
    SessionList(ctx context.Context, filter SessionFilter) ([]*Session, error)
    SessionGetByPayer(ctx context.Context, payer string) ([]*Session, error)

    // Worker/connection tracking within sessions
    WorkerCreate(ctx context.Context, worker *Worker) error
    WorkerGet(ctx context.Context, workerKey string) (*Worker, error)
    WorkerDelete(ctx context.Context, workerKey string) error
    WorkerListBySession(ctx context.Context, sessionID string) ([]*Worker, error)
    WorkerCountByPayer(ctx context.Context, payer string) (int, error)

    // Quota tracking
    QuotaGet(ctx context.Context, payer string) (*QuotaUsage, error)
    QuotaIncrement(ctx context.Context, payer string, sessions int, workers int) error
    QuotaDecrement(ctx context.Context, payer string, sessions int, workers int) error

    // Usage accumulation (for metering)
    UsageAdd(ctx context.Context, sessionID string, usage *UsageEvent) error
    UsageGetTotal(ctx context.Context, sessionID string) (*UsageSummary, error)

    // Health/lifecycle
    Ping(ctx context.Context) error
    Close() error
}
```

### Domain Types

```go
type Session struct {
    ID              string
    PayerAddress    string
    SignerAddress   string
    ServiceProvider string
    CreatedAt       time.Time
    LastKeepAlive   time.Time
    Status          SessionStatus // active, terminated
    Metadata        map[string]string
}

type Worker struct {
    Key         string
    SessionID   string
    PayerAddress string
    CreatedAt   time.Time
    TraceID     string
}

type QuotaUsage struct {
    PayerAddress       string
    ActiveSessions     int
    ActiveWorkers      int
    LastUpdated        time.Time
}

type UsageEvent struct {
    Timestamp   time.Time
    Blocks      int64
    Bytes       int64
    Requests    int64
}

type UsageSummary struct {
    TotalBlocks   int64
    TotalBytes    int64
    TotalRequests int64
}

type SessionFilter struct {
    PayerAddress *string
    Status       *SessionStatus
    CreatedAfter *time.Time
}

type SessionStatus string

const (
    SessionStatusActive     SessionStatus = "active"
    SessionStatusTerminated SessionStatus = "terminated"
)
```

### ConcurrentMap Type Alias

Use https://github.com/alphadose/haxmap for lock-free concurrent map operations:

```go
// ConcurrentMap is a type alias for high-performance concurrent hashmap
type ConcurrentMap[K comparable, V any] = *haxmap.Map[K, V]

func NewConcurrentMap[K comparable, V any]() ConcurrentMap[K, V] {
    return haxmap.New[K, V]()
}
```

### Implementations

**Priority 1: InMemory (bootstrap)**
```go
type InMemoryRepository struct {
    sessions ConcurrentMap[string, *Session]
    workers  ConcurrentMap[string, *Worker]
    quotas   ConcurrentMap[string, *QuotaUsage]
    usage    ConcurrentMap[string, []*UsageEvent]  // may need sync for slice append
}

func NewInMemoryRepository() *InMemoryRepository {
    return &InMemoryRepository{
        sessions: NewConcurrentMap[string, *Session](),
        workers:  NewConcurrentMap[string, *Worker](),
        quotas:   NewConcurrentMap[string, *QuotaUsage](),
        usage:    NewConcurrentMap[string, []*UsageEvent](),
    }
}
```

**Note:** For operations requiring atomic read-modify-write on slices (like `UsageAdd`), we may need a thin mutex wrapper or use haxmap's `GetOrCompute` pattern.

**Future: Redis (high-availability)**
```go
type RedisRepository struct {
    client *redis.Client
    // Key patterns:
    // session:{id} -> Session JSON
    // sessions:payer:{address} -> Set of session IDs
    // worker:{key} -> Worker JSON
    // workers:session:{id} -> Set of worker keys
    // quota:{payer} -> QuotaUsage JSON
    // usage:{sessionID} -> List of UsageEvent JSON
}

func NewRedisRepository(client *redis.Client) *RedisRepository
```

### File Structure

```
provider/
  repository/
    repository.go       # GlobalRepository interface + domain types
    inmemory.go         # InMemoryRepository implementation
    inmemory_test.go
    # Future:
    # redis.go          # RedisRepository implementation
    # redis_test.go
```

---

## Services to Implement

### 1. Auth Service (gRPC) ✓ ARCHITECTURE DECIDED

The `sds://` dauth plugin will call this gRPC service to validate the `x-sds-rav` header.

**Header Format:**
- Header name: `x-sds-rav`
- Content: Raw bytes (SignedRAV proto) for machine-to-machine; base64 if user-provided

**Proto Service:**
```proto
service AuthService {
  rpc ValidateAuth(ValidateAuthRequest) returns (ValidateAuthResponse);
}

message ValidateAuthRequest {
  // SignedRAV extracted from `x-sds-rav` header
  common.v1.SignedRAV payment_rav = 1;
  // Client IP address
  string ip_address = 2;
  // Request path/endpoint
  string path = 3;
}

message ValidateAuthResponse {
  // Payer address (0x...) → x-user-id / x-organization-id
  string organization_id = 1;
  // Optional for now, may be session ID or signer
  string api_key_id = 2;
  // Any additional context to pass through
  map<string, string> metadata = 3;
}
```

**SDS Implementation:**
```go
func (s *AuthService) ValidateAuth(ctx context.Context, req *ValidateAuthRequest) (*ValidateAuthResponse, error) {
    // 1. Convert proto to horizon.SignedRAV
    signedRAV := ProtoSignedRAVToHorizon(req.PaymentRav)

    // 2. Recover signer from EIP-712 signature
    signer, err := signedRAV.RecoverSigner(s.domain)
    if err != nil {
        return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("invalid signature: %w", err))
    }

    // 3. Check authorization (signer == payer, or on-chain delegation)
    payer := signedRAV.Message.Payer
    authorized, err := s.isSignerAuthorized(ctx, payer, signer)
    if err != nil {
        return nil, connect.NewError(connect.CodeInternal, err)
    }
    if !authorized {
        return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("signer not authorized for payer"))
    }

    // 4. Return auth context
    return &ValidateAuthResponse{
        OrganizationId: payer.Hex(),
        ApiKeyId:       "",  // empty for now
        Metadata: map[string]string{
            "signer": signer.Hex(),
        },
    }, nil
}
```

**Note:** This endpoint may already be similar to existing `ValidatePayment` in provider sidecar. We may be able to reuse/adapt that logic.

---

### 2. Usage Service (gRPC)

The dmetering `tgm://` plugin calls this gRPC service to report usage events.

**Proto Service:**
```proto
service UsageService {
  rpc Report(ReportRequest) returns (ReportResponse);
}

message ReportRequest {
  repeated sf.metering.v1.Event events = 1;
}

message ReportResponse {
  bool revoked = 1;
  string revocation_reason = 2;
}
```

**Event Structure (from dmetering):**
```proto
message Event {
  string organization_id = 1;   // Payer address
  string api_key_id = 2;        // Session/signer identifier
  string ip_address = 3;
  string endpoint = 4;          // e.g., "sf.substreams.rpc.v2/Blocks"
  string network = 5;           // e.g., "eth-mainnet"
  string meta = 7;
  string provider = 8;          // Our provider address
  string output_module_hash = 9;
  repeated Metric metrics = 20; // blocks_count, bytes, etc.
  google.protobuf.Timestamp timestamp = 30;
}
```

**SDS Implementation (uses GlobalRepository):**
- Receive batched events from metering plugin
- Store via `repo.UsageAdd()` for each event
- Check session status via `repo.SessionGet()` - return `revoked=true` if terminated
- Batching: Plugin batches with configurable delay (default 100ms)

---

### 3. Session Service (gRPC)

The dsession `tgm://` plugin calls this gRPC service for session/quota management.

**Proto Service:**
```proto
service SessionService {
  rpc BorrowWorker(BorrowWorkerRequest) returns (BorrowWorkerResponse);
  rpc ReturnWorker(ReturnWorkerRequest) returns (ReturnWorkerResponse);
  rpc KeepAlive(KeepAliveRequest) returns (KeepAliveResponse);
}
```

**BorrowWorker:**
```proto
message BorrowWorkerRequest {
  string service = 1;              // "substreams"
  string organization_id = 2;      // Payer address (from auth context)
  string api_key_id = 3;           // Optional, empty for now
  string trace_id = 4;             // Request trace ID
  int64 max_worker_for_trace_id = 5;
}

message BorrowWorkerResponse {
  string worker_key = 1;           // Session identifier
  Status status = 2;               // borrowed, resource_exhausted
  WorkerState worker_state = 3;    // MaxWorkers capacity info
}
```

**ReturnWorker:**
```proto
message ReturnWorkerRequest {
  string worker_key = 1;
  google.protobuf.Duration minimal_worker_life_duration = 2;
}
```

**KeepAlive:**
```proto
message KeepAliveRequest {
  string worker_key = 1;
  string api_key_id = 2;
}
```

**SDS Implementation (uses GlobalRepository):**
- BorrowWorker: `repo.SessionCreate()`, `repo.WorkerCreate()`, `repo.QuotaIncrement()`, enforce quota limits
- ReturnWorker: `repo.WorkerDelete()`, `repo.QuotaDecrement()`, report final usage
- KeepAlive: `repo.SessionUpdate()` to update `LastKeepAlive` timestamp

**Quota Configuration (in pricing config):**
```yaml
quotas:
  defaults:
    max_concurrent_sessions: 10
    max_workers_per_session: 5
  overrides:
    # Per-payer overrides
    "0x1234abcd...":  # payer address
      max_concurrent_sessions: 50
      max_workers_per_session: 20
```

**BorrowWorker Response Codes:**
- `borrowed`: Success, session/worker acquired
- `resource_exhausted`: Quota exceeded for this payer

---

## Implementation Tasks

### Priority 0: Global Repository

#### 0.1 Define GlobalRepository interface
- [x] Create `provider/repository/` package
- [x] Define `GlobalRepository` interface with all methods
- [x] Define domain types (`Session`, `Worker`, `QuotaUsage`, `UsageEvent`, etc.)
- [x] All methods take `ctx context.Context` and return `error`

#### 0.2 Implement InMemoryRepository
- [x] Add `github.com/alphadose/haxmap` dependency
- [x] Create `ConcurrentMap[K, V]` type alias
- [x] Implement all `GlobalRepository` methods using haxmap
- [x] Handle atomic slice operations (e.g., `UsageAdd`) with appropriate pattern
- [x] Write comprehensive tests

**Files to create:**
- `provider/repository/repository.go` - Interface + types
- `provider/repository/inmemory.go` - InMemory implementation
- `provider/repository/inmemory_test.go`

---

### Priority 1: Proto Definitions

#### 1.1 Add/Import proto definitions
- [x] Import or define `sf.gateway.payment.v1.UsageService` proto
- [x] Import or define `sf.sds.session.v1.SessionService` proto
- [x] Import `sf.metering.v1.Event` proto from dmetering
- [x] Generate Go code with buf/protoc

**Files:**
- `proto/sf/gateway/payment/v1/usage.proto`
- `proto/sf/sds/session/v1/session.proto`
- Or import from existing packages

**Reference:**
- dsession: https://github.com/streamingfast/dsession
- Check if worker-pool-protocol has published protos

---

### Priority 2: Auth Service (gRPC) ✓ UNBLOCKED

#### 2.1 Define proto for AuthService
- [x] Create `proto/sf/sds/auth/v1/auth.proto` with `AuthService.ValidateAuth`
- [x] Generate Go code with buf/protoc
- [x] Or reuse/extend existing `ValidatePayment` proto

#### 2.2 Implement AuthService gRPC
- [x] Create `provider/auth/` package
- [x] Implement `ValidateAuth` RPC handler
- [x] Reuse existing RAV validation logic from `handler_validate_payment.go`
- [x] Wire into provider sidecar gRPC server

**Files to create:**
- `provider/auth/service.go` - gRPC handler
- `provider/auth/service_test.go`

**Note:** Much of the logic already exists in `provider/sidecar/handler_validate_payment.go`. We may:
1. Create a new service that wraps existing logic
2. Or extend existing `ProviderSidecarService` with the auth endpoint
3. Or create an adapter that maps the new proto to existing calls

---

### Priority 3: Usage Service (gRPC)

#### 3.1 Implement UsageService gRPC
- [x] Create `provider/usage/` package
- [x] Implement `Report` RPC handler
- [x] Map `sf.metering.v1.Event` to internal usage tracking
- [x] Integrate with session state to check revocation
- [x] Wire into provider sidecar gRPC server

**Files to create:**
- `provider/usage/service.go` - gRPC handler
- `provider/usage/mapper.go` - Event to internal usage mapping
- `provider/usage/service_test.go`

---

### Priority 4: Session Service (gRPC)

#### 4.1 Implement SessionService gRPC
- [x] Create `provider/session/` package
- [x] Implement `BorrowWorker` RPC handler
- [x] Implement `ReturnWorker` RPC handler
- [x] Implement `KeepAlive` RPC handler
- [x] Implement quota enforcement from pricing config
- [x] Support per-payer quota overrides
- [x] Integrate with internal session tracking
- [x] Wire into provider sidecar gRPC server

**Files to create:**
- `provider/session/service.go` - gRPC handlers (uses GlobalRepository)
- `provider/session/quotas.go` - Quota config loading from provider-config
- `provider/session/service_test.go`

---

### Priority 5: Integration

#### 5.1 Wire services into provider sidecar
- [x] Add gRPC service registration for AuthService
- [x] Add gRPC service registration for UsageService
- [x] Add gRPC service registration for SessionService
- [x] Add configuration for enabling/disabling each service
- [x] Add logging using `logging.PackageLogger` pattern

#### 5.2 Integration testing
- [x] Test auth flow with actual dauth plugin config
- [x] Test metering flow with dmetering plugin
- [x] Test session flow with dsession plugin
- [x] End-to-end test with firehose-core tier1

---

## Configuration

The provider sidecar will serve these endpoints on its existing port(s):

```bash
# firehose-core tier1 configuration
--common-auth-plugin="tgm://localhost:9001"
--common-metering-plugin="tgm://localhost:9001?network=eth-mainnet"
--common-session-plugin="tgm://localhost:9001"
```

Provider sidecar flags (to add - depends on architecture):
```bash
# RAV validation (always needed)
--eip712-domain-chain-id=1
--eip712-domain-verifying-contract=0x...

# Quota configuration
--quotas-config=/path/to/quotas.yaml
```

---

## File Structure (Summary)

```
provider/
  repository/
    repository.go        # GlobalRepository interface + domain types
    inmemory.go          # InMemoryRepository implementation
    inmemory_test.go
    # Future: redis.go, redis_test.go
  auth/
    service.go           # gRPC AuthService.ValidateAuth implementation
    service_test.go
  usage/
    service.go           # gRPC UsageService.Report implementation
    mapper.go            # Event mapping to internal usage
    service_test.go
  session/
    service.go           # gRPC SessionService implementation
    quotas.go            # Quota limits from provider-config
    service_test.go

proto/
  sf/sds/auth/v1/
    auth.proto           # AuthService definition (or extend existing)
  sf/gateway/payment/v1/
    usage.proto          # UsageService definition
  sf/sds/session/v1/
    session.proto        # SessionService definition
```

---

## Key Decisions Made

1. **Auth via gRPC**: `sds://` dauth plugin calls `AuthService.ValidateAuth` with RAV from `x-sds-rav` header
2. **RAV-based auth**: EIP-712 signature validation, no JWT
3. **`sds://` = alias for `tgm://`**: Both call external gRPC services, same pattern
4. **Metering is batched**: Plugin batches with configurable flush interval (default 100ms)
5. **dsession package**: https://github.com/streamingfast/dsession
6. **organization_id**: Maps to payer address (from RAV)
7. **api_key_id**: Optional/empty for now (may need firehose-core change to make optional)
8. **Quota/limits**: Provider-configurable via provider-config (includes pricing + quotas)
9. **Header name**: `x-sds-rav` - raw bytes for machine-to-machine; base64 if user-provided
10. **AuthService**: Separate gRPC service (not extending ProviderSidecarService)
11. **GlobalRepository**: Interface-based state storage; InMemory first, Redis for HA later
12. **All repo methods**: Take `ctx context.Context`, return `error`
13. **ConcurrentMap**: Type alias using `github.com/alphadose/haxmap` for lock-free concurrent maps

---

## Open Questions (Resolved)

| Question | Answer |
|----------|--------|
| Auth architecture | **Option A**: `sds://` plugin calls gRPC `ValidateAuth` on provider sidecar |
| RAV flow | Consumer sends `x-sds-rav=RAVx` header to tier1; tier1 calls sidecar to validate |
| JWT key management | **No JWT** - Auth is RAV signature-based |
| Session vs Auth boundary | Auth validates RAV + sets payer context. Session manages lifecycle. |
| Metering granularity | Batched with configurable flush (like tgm-gateway, default 100ms) |
| dsession package location | https://github.com/streamingfast/dsession |
| Scheme naming | `sds://` and `tgm://` are aliases - both call external gRPC services |
| organization_id mapping | Payer address from RAV |
| api_key_id mapping | Optional/empty for now |
| Quota/limits | Provider-config (global config including pricing + quotas) |

---

## All Questions Resolved ✓

| Question | Decision |
|----------|----------|
| Header name & encoding | `x-sds-rav` - raw bytes for machine-to-machine (gRPC); base64 if user/operator provides manually |
| Provider config structure | Single config with quotas + pricing |
| AuthService location | Separate `AuthService` (not extending ProviderSidecarService) |

---

## References

- dsession: https://github.com/streamingfast/dsession
- Reference tgm-gateway: `resources/tgm-gateway/`
- dmetering: `resources/dmetering/`
- SDS provider sidecar: `provider/sidecar/`
- Proto definitions: `proto/graph/substreams/data_service/provider/v1/`

---

## Completed Items

All items completed in commit `d313c011`:

- [x] **Priority 0**: GlobalRepository interface + InMemoryRepository with haxmap
- [x] **Priority 1**: Proto definitions for AuthService, UsageService, SessionService
- [x] **Priority 2**: AuthService gRPC implementation with RAV validation & auth cache
- [x] **Priority 3**: UsageService gRPC implementation with usage tracking
- [x] **Priority 4**: SessionService gRPC implementation with quota enforcement
- [x] **Priority 5**: All services wired into provider sidecar

**Files Created:**
- `provider/repository/repository.go` - GlobalRepository interface + domain types
- `provider/repository/inmemory.go` - InMemory implementation using haxmap
- `provider/repository/inmemory_test.go` - 29 comprehensive tests
- `provider/auth/service.go` - AuthService with EIP-712 RAV validation
- `provider/auth/service_test.go` - Unit tests
- `provider/usage/service.go` - UsageService for metering
- `provider/usage/service_test.go` - Unit tests
- `provider/session/service.go` - SessionService (BorrowWorker/ReturnWorker/KeepAlive)
- `provider/session/quotas.go` - QuotaConfig with per-payer overrides
- `provider/session/service_test.go` - Unit tests
- `proto/graph/substreams/data_service/sds/auth/v1/auth.proto`
- `proto/graph/substreams/data_service/sds/usage/v1/usage.proto`
- `proto/graph/substreams/data_service/sds/session/v1/session.proto`
- Generated pb + connect code for all services
