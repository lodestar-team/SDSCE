package plugin

import (
	"context"
	"fmt"
	"os"
	"time"

	"connectrpc.com/connect"
	"github.com/alphadose/haxmap"
	sessionv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/session/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/session/v1/sessionv1connect"
	"github.com/streamingfast/dsession"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/durationpb"
)

// RegisterSession registers the "sds" scheme with dsession.
// The config URL format is:
//
//	sds://host:port?plaintext=true&insecure=true&keep-alive-delay=20s&minimal-worker-life-duration=5s
//
// The plugin connects to the provider sidecar's SessionService for worker pool management.
// All quota configuration is on the server side.
func RegisterSession() {
	dsession.Register("sds", func(config string, logger *zap.Logger) (dsession.SessionPool, error) {
		configExpanded := os.ExpandEnv(config)

		baseCfg, vals, err := parseBaseConfig(configExpanded)
		if err != nil {
			return nil, fmt.Errorf("failed to parse session config %q: %w", config, err)
		}

		// Validate known parameters
		for k := range vals {
			switch k {
			case "insecure", "plaintext", "keep-alive-delay", "minimal-worker-life-duration":
				// Known parameters
			default:
				return nil, fmt.Errorf("unknown query parameter: %s", k)
			}
		}

		keepAliveDelay, err := parseDuration(vals.Get("keep-alive-delay"), 20*time.Second)
		if err != nil {
			return nil, fmt.Errorf("invalid keep-alive-delay: %w", err)
		}

		minimalWorkerLifeDuration, err := parseDuration(vals.Get("minimal-worker-life-duration"), 5*time.Second)
		if err != nil {
			return nil, fmt.Errorf("invalid minimal-worker-life-duration: %w", err)
		}

		return newSessionPool(baseCfg, keepAliveDelay, minimalWorkerLifeDuration, logger)
	})
}

// sessionInfo tracks a borrowed session and its workers.
type sessionInfo struct {
	organizationID string
	apiKeyID       string
	traceID        string
	workers        *haxmap.Map[string, struct{}]
	closer         chan struct{}
}

// sessionPool implements dsession.SessionPool by calling the provider sidecar.
type sessionPool struct {
	client                    sessionv1connect.SessionServiceClient
	logger                    *zap.Logger
	keepAliveDelay            time.Duration
	minimalWorkerLifeDuration time.Duration

	sessions *haxmap.Map[string, *sessionInfo]
}

func newSessionPool(cfg *baseConfig, keepAliveDelay, minimalWorkerLifeDuration time.Duration, logger *zap.Logger) (dsession.SessionPool, error) {
	httpClient := newHTTPClient(cfg)

	client := sessionv1connect.NewSessionServiceClient(
		httpClient,
		cfg.baseURL(),
	)

	return &sessionPool{
		client:                    client,
		logger:                    logger.Named("sds-session"),
		keepAliveDelay:            keepAliveDelay,
		minimalWorkerLifeDuration: minimalWorkerLifeDuration,
		sessions:                  haxmap.New[string, *sessionInfo](),
	}, nil
}

// Get implements dsession.SessionPool.
func (p *sessionPool) Get(ctx context.Context, serviceName string, organizationID string, apiKeyID string, traceID string, onError func(error)) (string, error) {
	req := connect.NewRequest(&sessionv1.BorrowWorkerRequest{
		Service:        serviceName,
		OrganizationId: organizationID,
		ApiKeyId:       apiKeyID,
		TraceId:        traceID,
	})

	resp, err := p.client.BorrowWorker(ctx, req)
	if err != nil {
		// Map connect errors to dsession errors
		switch connect.CodeOf(err) {
		case connect.CodeUnavailable:
			return "", fmt.Errorf("%w: %s", dsession.ErrUnavailable, err.Error())
		case connect.CodePermissionDenied:
			return "", fmt.Errorf("%w: %s", dsession.ErrPermissionDenied, err.Error())
		case connect.CodeResourceExhausted:
			return "", fmt.Errorf("%w: %s", dsession.ErrQuotaExceeded, err.Error())
		}
		return "", fmt.Errorf("failed to borrow session: %w", err)
	}

	workerKey := resp.Msg.WorkerKey
	workerStatus := resp.Msg.Status

	details := ""
	if maxWorkers := resp.Msg.WorkerState.GetMaxWorkers(); maxWorkers != 0 {
		details = fmt.Sprintf(" (active sessions: %d/%d)", resp.Msg.WorkerState.GetActiveWorkers(), maxWorkers)
	}

	if workerStatus == sessionv1.BorrowStatus_BORROW_STATUS_RESOURCE_EXHAUSTED {
		p.logger.Debug("session pool is exhausted", zap.String("status", workerStatus.String()), zap.String("details", details))
		return "", fmt.Errorf("%w%s", dsession.ErrConcurrentStreamLimitExceeded, details)
	}

	// Start keep-alive for borrowed sessions
	if workerStatus == sessionv1.BorrowStatus_BORROW_STATUS_BORROWED {
		done := make(chan struct{})
		p.sessions.Set(workerKey, &sessionInfo{
			organizationID: organizationID,
			apiKeyID:       apiKeyID,
			traceID:        traceID,
			workers:        haxmap.New[string, struct{}](),
			closer:         done,
		})

		p.startKeepAlive(ctx, done, workerKey, onError)
	}

	p.logger.Debug("borrowed request worker", zap.String("worker_key", workerKey))

	return workerKey, nil
}

// Release implements dsession.SessionPool.
func (p *sessionPool) Release(sessionKey string) {
	go func() {
		info, ok := p.sessions.Get(sessionKey)
		if !ok {
			return
		}

		// Collect workers to release
		var workersToRelease []string
		info.workers.ForEach(func(workerKey string, _ struct{}) bool {
			workersToRelease = append(workersToRelease, workerKey)
			return true
		})
		done := info.closer
		p.sessions.Del(sessionKey)

		// Close the done channel
		if done != nil {
			close(done)
		}

		// Release all workers
		for _, workerKey := range workersToRelease {
			p.releaseWorkerInternal(workerKey)
		}

		// Return the session worker
		req := connect.NewRequest(&sessionv1.ReturnWorkerRequest{
			WorkerKey:                 sessionKey,
			MinimalWorkerLifeDuration: durationpb.New(p.minimalWorkerLifeDuration),
		})
		resp, err := p.client.ReturnWorker(context.Background(), req)
		p.logger.Debug("returned request worker", zap.String("key", sessionKey), zap.Any("status", resp), zap.Error(err))
	}()
}

// GetWorker implements dsession.SessionPool.
func (p *sessionPool) GetWorker(ctx context.Context, serviceName string, sessionKey string, maxWorkersPerSession int) (string, error) {
	// Look up session info
	info, ok := p.sessions.Get(sessionKey)
	if !ok {
		return "", fmt.Errorf("%w: session key %s not found", dsession.ErrSessionNotFound, sessionKey)
	}
	organizationID := info.organizationID
	apiKeyID := info.apiKeyID
	traceID := info.traceID

	req := connect.NewRequest(&sessionv1.BorrowWorkerRequest{
		Service:             serviceName,
		OrganizationId:      organizationID,
		ApiKeyId:            apiKeyID,
		TraceId:             traceID,
		MaxWorkerForTraceId: int64(maxWorkersPerSession),
	})

	resp, err := p.client.BorrowWorker(ctx, req)
	if err != nil {
		// Map connect errors to dsession errors
		switch connect.CodeOf(err) {
		case connect.CodeNotFound:
			return "", fmt.Errorf("%w: session not found", dsession.ErrSessionNotFound)
		case connect.CodeResourceExhausted:
			return "", fmt.Errorf("%w: maximum workers per session exceeded", dsession.ErrWorkersLimitExceeded)
		}
		return "", fmt.Errorf("failed to borrow worker: %w", err)
	}

	workerKey := resp.Msg.WorkerKey
	workerStatus := resp.Msg.Status

	details := ""
	if maxWorkers := resp.Msg.WorkerState.GetMaxWorkers(); maxWorkers != 0 {
		details = fmt.Sprintf(" (active workers: %d/%d)", resp.Msg.WorkerState.GetActiveWorkers(), maxWorkers)
	}

	if workerStatus == sessionv1.BorrowStatus_BORROW_STATUS_RESOURCE_EXHAUSTED {
		p.logger.Debug("worker limit exceeded", zap.String("worker_key", workerKey), zap.String("status", workerStatus.String()))
		return "", fmt.Errorf("%w%s", dsession.ErrWorkersLimitExceeded, details)
	}

	// Track this worker under the session
	info, ok = p.sessions.Get(sessionKey)
	if !ok {
		// Session was released, immediately release the newly acquired worker
		go p.releaseWorkerInternal(workerKey)
		return "", fmt.Errorf("%w: session key %s was released", dsession.ErrSessionNotFound, sessionKey)
	}
	info.workers.Set(workerKey, struct{}{})

	p.logger.Info("borrowed worker",
		zap.String("organization_id", organizationID),
		zap.String("api_key_id", apiKeyID),
		zap.String("service_name", serviceName),
		zap.String("trace_id", traceID),
		zap.String("worker_key", workerKey),
		zap.String("session_key", sessionKey),
		zap.Int("max_workers", maxWorkersPerSession),
	)

	return workerKey, nil
}

// ReleaseWorker implements dsession.SessionPool.
func (p *sessionPool) ReleaseWorker(workerKey string) {
	// Remove worker from session tracking
	p.sessions.ForEach(func(_ string, info *sessionInfo) bool {
		info.workers.Del(workerKey)
		return true
	})

	// Release worker in a goroutine
	go p.releaseWorkerInternal(workerKey)
}

func (p *sessionPool) releaseWorkerInternal(workerKey string) {
	req := connect.NewRequest(&sessionv1.ReturnWorkerRequest{
		WorkerKey: workerKey,
	})
	resp, err := p.client.ReturnWorker(context.Background(), req)
	p.logger.Debug("returned worker", zap.String("key", workerKey), zap.Any("status", resp), zap.Error(err))
}

// startKeepAlive starts the keep-alive goroutine for a borrowed session.
func (p *sessionPool) startKeepAlive(ctx context.Context, done <-chan struct{}, sessionKey string, onError func(error)) {
	go func() {
		ticker := time.NewTicker(p.keepAliveDelay)
		defer ticker.Stop()

		errorMode := false

		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				info, ok := p.sessions.Get(sessionKey)
				if !ok {
					return
				}
				apiKeyID := info.apiKeyID
				var workerKeys []string
				info.workers.ForEach(func(workerKey string, _ struct{}) bool {
					workerKeys = append(workerKeys, workerKey)
					return true
				})

				hadError := false

				// Keep session alive
				req := connect.NewRequest(&sessionv1.KeepAliveRequest{
					WorkerKey: sessionKey,
					ApiKeyId:  apiKeyID,
				})
				_, err := p.client.KeepAlive(ctx, req)
				if err != nil {
					hadError = true
					p.logger.Error("failed to call keep session alive", zap.String("session_key", sessionKey), zap.Error(err))
					if onError != nil {
						switch connect.CodeOf(err) {
						case connect.CodePermissionDenied:
							onError(fmt.Errorf("%w: %s", dsession.ErrPermissionDenied, err.Error()))
							return
						case connect.CodeResourceExhausted:
							onError(fmt.Errorf("%w: %s", dsession.ErrQuotaExceeded, err.Error()))
							return
						}
					}
				}

				// Keep workers alive
				for _, workerKey := range workerKeys {
					req := connect.NewRequest(&sessionv1.KeepAliveRequest{
						WorkerKey: workerKey,
						ApiKeyId:  apiKeyID,
					})
					_, err := p.client.KeepAlive(ctx, req)
					if err != nil {
						hadError = true
						p.logger.Error("failed to call keep worker alive", zap.String("worker_key", workerKey), zap.Error(err))
					}
				}

				// On error, switch to 1-second retry interval
				if hadError && !errorMode {
					ticker.Reset(time.Second)
					errorMode = true
					p.logger.Info("switched to error recovery mode with 1 second interval", zap.String("session_key", sessionKey))
				} else if !hadError && errorMode {
					ticker.Reset(p.keepAliveDelay)
					errorMode = false
					p.logger.Info("recovered from error, switched back to normal interval", zap.String("session_key", sessionKey))
				}
			}
		}
	}()
}
