package plugin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/alphadose/haxmap"
	sds "github.com/graphprotocol/substreams-data-service"
	sessionv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/session/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/session/v1/sessionv1connect"
	"github.com/streamingfast/dauth"
	"github.com/streamingfast/dsession"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type stubSessionService struct {
	sessionv1connect.UnimplementedSessionServiceHandler

	borrowWorker func(context.Context, *connect.Request[sessionv1.BorrowWorkerRequest]) (*connect.Response[sessionv1.BorrowWorkerResponse], error)
	keepAlive    func(context.Context, *connect.Request[sessionv1.KeepAliveRequest]) (*connect.Response[sessionv1.KeepAliveResponse], error)
}

func (s stubSessionService) BorrowWorker(ctx context.Context, req *connect.Request[sessionv1.BorrowWorkerRequest]) (*connect.Response[sessionv1.BorrowWorkerResponse], error) {
	if s.borrowWorker != nil {
		return s.borrowWorker(ctx, req)
	}
	return connect.NewResponse(&sessionv1.BorrowWorkerResponse{}), nil
}

func (s stubSessionService) KeepAlive(ctx context.Context, req *connect.Request[sessionv1.KeepAliveRequest]) (*connect.Response[sessionv1.KeepAliveResponse], error) {
	if s.keepAlive != nil {
		return s.keepAlive(ctx, req)
	}
	return connect.NewResponse(&sessionv1.KeepAliveResponse{}), nil
}

func newTestSessionPool(t *testing.T, svc sessionv1connect.SessionServiceHandler, keepAliveDelay time.Duration) *sessionPool {
	t.Helper()

	mux := http.NewServeMux()
	path, handler := sessionv1connect.NewSessionServiceHandler(svc)
	mux.Handle(path, handler)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return &sessionPool{
		client:                    sessionv1connect.NewSessionServiceClient(server.Client(), server.URL),
		logger:                    zap.NewNop(),
		keepAliveDelay:            keepAliveDelay,
		minimalWorkerLifeDuration: 10 * time.Millisecond,
		sessions:                  haxmap.New[string, *sessionInfo](),
	}
}

func TestSessionPoolGetWorker_MapsPermissionDenied(t *testing.T) {
	pool := newTestSessionPool(t, stubSessionService{
		borrowWorker: func(context.Context, *connect.Request[sessionv1.BorrowWorkerRequest]) (*connect.Response[sessionv1.BorrowWorkerResponse], error) {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("session is not allowed"))
		},
	}, 20*time.Millisecond)

	pool.sessions.Set("session-key", &sessionInfo{
		organizationID: "0x1111111111111111111111111111111111111111",
		apiKeyID:       "api-key",
		traceID:        "trace",
		workers:        haxmap.New[string, struct{}](),
		closer:         make(chan struct{}),
	})

	ctx := dauth.WithTrustedHeaders(context.Background(), dauth.TrustedHeaders{
		sds.HeaderSessionID: "sds-session-id",
	})

	_, err := pool.GetWorker(ctx, "substreams", "session-key", 1)
	require.ErrorIs(t, err, dsession.ErrPermissionDenied)
}

func TestSessionPoolKeepAlive_MapsResourceExhaustedToQuotaExceeded(t *testing.T) {
	pool := newTestSessionPool(t, stubSessionService{
		keepAlive: func(context.Context, *connect.Request[sessionv1.KeepAliveRequest]) (*connect.Response[sessionv1.KeepAliveResponse], error) {
			return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("payment budget exhausted"))
		},
	}, 10*time.Millisecond)

	done := make(chan struct{})
	pool.sessions.Set("session-key", &sessionInfo{
		apiKeyID: "api-key",
		workers:  haxmap.New[string, struct{}](),
		closer:   done,
	})

	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool.startKeepAlive(ctx, done, "session-key", func(err error) {
		errCh <- err
	})

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, dsession.ErrQuotaExceeded)
	case <-time.After(time.Second):
		t.Fatal("expected keepalive to surface a quota exceeded error")
	}
}
