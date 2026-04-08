package plugin

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	sds "github.com/graphprotocol/substreams-data-service"
	usagev1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/usage/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/usage/v1/usagev1connect"
	"github.com/streamingfast/dauth"
	"github.com/streamingfast/dmetering"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type blockingUsageService struct {
	usagev1connect.UnimplementedUsageServiceHandler

	entered    chan struct{}
	reportSeen atomic.Int32
}

func (s *blockingUsageService) Report(ctx context.Context, req *connect.Request[usagev1.ReportRequest]) (*connect.Response[usagev1.ReportResponse], error) {
	s.reportSeen.Add(1)
	select {
	case s.entered <- struct{}{}:
	default:
	}

	<-ctx.Done()
	return nil, ctx.Err()
}

type recordingUsageService struct {
	usagev1connect.UnimplementedUsageServiceHandler

	mu         sync.Mutex
	batchSizes []int
}

func (s *recordingUsageService) Report(_ context.Context, req *connect.Request[usagev1.ReportRequest]) (*connect.Response[usagev1.ReportResponse], error) {
	s.mu.Lock()
	s.batchSizes = append(s.batchSizes, len(req.Msg.Events))
	s.mu.Unlock()
	return connect.NewResponse(&usagev1.ReportResponse{}), nil
}

func (s *recordingUsageService) snapshot() []int {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]int, len(s.batchSizes))
	copy(out, s.batchSizes)
	return out
}

func newTestMeteringEmitter(t *testing.T, reportTimeout time.Duration, handler usagev1connect.UsageServiceHandler) *meteringEmitter {
	t.Helper()

	mux := http.NewServeMux()
	path, h := usagev1connect.NewUsageServiceHandler(handler)
	mux.Handle(path, h)

	server := httptest.NewUnstartedServer(mux)
	server.EnableHTTP2 = true
	server.TLS = &tls.Config{NextProtos: []string{"h2"}}
	server.StartTLS()
	t.Cleanup(server.Close)

	parsedURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	emitter, err := newMeteringEmitter(
		&baseConfig{
			Endpoint:  parsedURL.Host,
			Insecure:  true,
			Plaintext: false,
		},
		"mainnet",
		32,
		5*time.Millisecond,
		reportTimeout,
		false,
		zap.NewNop(),
	)
	require.NoError(t, err)

	metering, ok := emitter.(*meteringEmitter)
	require.True(t, ok)

	t.Cleanup(func() {
		metering.Shutdown(nil)
	})

	return metering
}

func testMeteringEvent() dmetering.Event {
	return dmetering.Event{
		Endpoint:         "/sf.substreams.rpc.v4.Stream/Blocks",
		OrganizationID:   "0x1111111111111111111111111111111111111111",
		ApiKeyID:         "api-key",
		IpAddress:        "127.0.0.1",
		Meta:             "session-1",
		Timestamp:        time.Unix(1700000000, 0),
		Metrics:          map[string]float64{"blocks_count": 1, "bytes_count": 1024},
		OutputModuleHash: "0xdeadbeef",
	}
}

func TestMeteringEmitter_ShutdownCompletesWithBlockedReport(t *testing.T) {
	usageSvc := &blockingUsageService{
		entered: make(chan struct{}, 1),
	}
	emitter := newTestMeteringEmitter(t, 25*time.Millisecond, usageSvc)

	ctx := dauth.WithTrustedHeaders(context.Background(), dauth.TrustedHeaders{
		sds.HeaderSessionID: "session-1",
	})

	emitter.Emit(ctx, testMeteringEvent())

	select {
	case <-usageSvc.entered:
	case <-time.After(time.Second):
		t.Fatal("expected report RPC to start")
	}

	done := make(chan struct{})
	go func() {
		emitter.Shutdown(nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected metering emitter shutdown to complete")
	}
}

func TestMeteringEmitter_ShutdownFlushesQueuedEventsOnce(t *testing.T) {
	usageSvc := &recordingUsageService{}
	emitter := newTestMeteringEmitter(t, time.Second, usageSvc)

	ctx := dauth.WithTrustedHeaders(context.Background(), dauth.TrustedHeaders{
		sds.HeaderSessionID: "session-3",
	})

	emitter.Emit(ctx, testMeteringEvent())
	emitter.Emit(ctx, testMeteringEvent())

	emitter.Shutdown(nil)

	require.Eventually(t, func() bool {
		return len(usageSvc.snapshot()) == 1
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, []int{2}, usageSvc.snapshot())
}

func TestMeteringEmitter_EmitAfterShutdownDoesNotPanic(t *testing.T) {
	usageSvc := &blockingUsageService{
		entered: make(chan struct{}, 1),
	}
	emitter := newTestMeteringEmitter(t, 25*time.Millisecond, usageSvc)

	ctx := dauth.WithTrustedHeaders(context.Background(), dauth.TrustedHeaders{
		sds.HeaderSessionID: "session-2",
	})

	emitter.Emit(ctx, testMeteringEvent())

	select {
	case <-usageSvc.entered:
	case <-time.After(time.Second):
		t.Fatal("expected report RPC to start")
	}

	done := make(chan struct{})
	go func() {
		emitter.Shutdown(nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected metering emitter shutdown to complete")
	}

	require.NotPanics(t, func() {
		emitter.Emit(ctx, testMeteringEvent())
	})
}

func TestParseMeteringReportTimeout(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		timeout, err := parseMeteringReportTimeout(url.Values{})
		require.NoError(t, err)
		require.Equal(t, defaultMeteringReportTimeout, timeout)
	})

	t.Run("explicit", func(t *testing.T) {
		timeout, err := parseMeteringReportTimeout(url.Values{
			"report-timeout": []string{"750ms"},
		})
		require.NoError(t, err)
		require.Equal(t, 750*time.Millisecond, timeout)
	})
}
