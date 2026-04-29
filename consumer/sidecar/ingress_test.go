package sidecar

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIsAmbiguousIngressUpstreamError(t *testing.T) {
	t.Parallel()

	require.True(t, isAmbiguousIngressUpstreamError(context.Canceled))
	require.True(t, isAmbiguousIngressUpstreamError(status.Error(codes.Canceled, "upstream canceled")))
	require.True(t, isAmbiguousIngressUpstreamError(status.Error(codes.Unavailable, "transport closed")))
	require.False(t, isAmbiguousIngressUpstreamError(io.EOF))
	require.False(t, isAmbiguousIngressUpstreamError(status.Error(codes.InvalidArgument, "bad request")))
}

func TestAwaitAmbiguousIngressTerminationResolution_MapsInactiveSessionEndReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		endReason  commonv1.EndReason
		wantCode   codes.Code
		wantErr    bool
		wantSubstr string
	}{
		{
			name:      "payment issue becomes resource exhausted",
			endReason: commonv1.EndReason_END_REASON_PAYMENT_ISSUE,
			wantCode:  codes.ResourceExhausted,
			wantErr:   true,
		},
		{
			name:      "complete becomes clean eof",
			endReason: commonv1.EndReason_END_REASON_COMPLETE,
			wantErr:   false,
		},
		{
			name:      "client disconnect becomes clean eof",
			endReason: commonv1.EndReason_END_REASON_CLIENT_DISCONNECT,
			wantErr:   false,
		},
		{
			name:       "provider stop becomes unavailable",
			endReason:  commonv1.EndReason_END_REASON_PROVIDER_STOP,
			wantCode:   codes.Unavailable,
			wantErr:    true,
			wantSubstr: "provider stop",
		},
		{
			name:       "error becomes unavailable",
			endReason:  commonv1.EndReason_END_REASON_ERROR,
			wantCode:   codes.Unavailable,
			wantErr:    true,
			wantSubstr: "error",
		},
		{
			name:       "unspecified becomes unavailable",
			endReason:  commonv1.EndReason_END_REASON_UNSPECIFIED,
			wantCode:   codes.Unavailable,
			wantErr:    true,
			wantSubstr: "END_REASON_UNSPECIFIED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := awaitAmbiguousIngressTerminationResolution(
				context.Background(),
				"session-1",
				250*time.Millisecond,
				nil,
				nil,
				func(context.Context, string) (*providerv1.GetSessionStatusResponse, error) {
					return &providerv1.GetSessionStatusResponse{
						Active:    false,
						EndReason: tt.endReason,
					}, nil
				},
			)

			if !tt.wantErr {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			require.Equal(t, tt.wantCode, status.Code(err))
			if tt.wantSubstr != "" {
				require.Contains(t, err.Error(), tt.wantSubstr)
			}
		})
	}
}

func TestAwaitAmbiguousIngressTerminationResolution_TimesOutOnUnresolvedStatusError(t *testing.T) {
	t.Parallel()

	err := awaitAmbiguousIngressTerminationResolution(
		context.Background(),
		"session-1",
		25*time.Millisecond,
		nil,
		nil,
		func(context.Context, string) (*providerv1.GetSessionStatusResponse, error) {
			return nil, errors.New("temporary transport failure")
		},
	)

	require.Error(t, err)
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Contains(t, err.Error(), "temporary transport failure")
}

func TestAwaitAmbiguousIngressTerminationResolution_CoordinatorSemanticStopPreemptsStatusFallback(t *testing.T) {
	t.Parallel()

	coordinator := newIngressTerminationCoordinator(func() {})
	getterCalled := make(chan struct{}, 1)

	go func() {
		<-getterCalled
		coordinator.setSemanticStop(status.Error(codes.ResourceExhausted, "need more funds"))
	}()

	err := awaitAmbiguousIngressTerminationResolution(
		context.Background(),
		"session-1",
		250*time.Millisecond,
		coordinator,
		nil,
		func(context.Context, string) (*providerv1.GetSessionStatusResponse, error) {
			select {
			case getterCalled <- struct{}{}:
			default:
			}
			return &providerv1.GetSessionStatusResponse{Active: true}, nil
		},
	)

	require.Error(t, err)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	require.Contains(t, err.Error(), "need more funds")
}

func TestAwaitFiniteIngressPostStreamControl_ReturnsPromptlyWithoutPendingControl(t *testing.T) {
	t.Parallel()

	coordinator := newIngressTerminationCoordinator(func() {})

	start := time.Now()
	err := awaitFiniteIngressPostStreamControl(context.Background(), "session-1", time.Second, coordinator, nil, func(context.Context, string) (*providerv1.GetSessionStatusResponse, error) {
		return &providerv1.GetSessionStatusResponse{Active: true}, nil
	})
	require.NoError(t, err)
	require.Less(t, time.Since(start), 100*time.Millisecond)
}

func TestAwaitFiniteIngressPostStreamControl_WaitsForPendingControlResolution(t *testing.T) {
	t.Parallel()

	coordinator := newIngressTerminationCoordinator(func() {})
	coordinator.setPaymentControlPending(true)

	go func() {
		time.Sleep(25 * time.Millisecond)
		coordinator.setPaymentControlPending(false)
	}()

	err := awaitFiniteIngressPostStreamControl(context.Background(), "session-1", time.Second, coordinator, nil, func(context.Context, string) (*providerv1.GetSessionStatusResponse, error) {
		return &providerv1.GetSessionStatusResponse{Active: true}, nil
	})
	require.NoError(t, err)
}

func TestAwaitFiniteIngressPostStreamControl_PendingSemanticStopWins(t *testing.T) {
	t.Parallel()

	coordinator := newIngressTerminationCoordinator(func() {})
	coordinator.setPaymentControlPending(true)

	go func() {
		time.Sleep(25 * time.Millisecond)
		coordinator.setSemanticStop(status.Error(codes.ResourceExhausted, "need more funds"))
	}()

	err := awaitFiniteIngressPostStreamControl(context.Background(), "session-1", time.Second, coordinator, nil, func(context.Context, string) (*providerv1.GetSessionStatusResponse, error) {
		return &providerv1.GetSessionStatusResponse{Active: true}, nil
	})
	require.Error(t, err)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
}

func TestAwaitFiniteIngressPostStreamControl_TimesOutWhenPendingControlDoesNotResolve(t *testing.T) {
	t.Parallel()

	coordinator := newIngressTerminationCoordinator(func() {})
	coordinator.setPaymentControlPending(true)

	err := awaitFiniteIngressPostStreamControl(context.Background(), "session-1", 25*time.Millisecond, coordinator, nil, func(context.Context, string) (*providerv1.GetSessionStatusResponse, error) {
		return &providerv1.GetSessionStatusResponse{Active: true, PaymentControlPending: true}, nil
	})
	require.Error(t, err)
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Contains(t, err.Error(), "payment control remained pending")
}

func TestAwaitFiniteIngressPostStreamControl_WaitsForProviderPendingControl(t *testing.T) {
	t.Parallel()

	coordinator := newIngressTerminationCoordinator(func() {})
	statusPending := make(chan struct{})

	err := awaitFiniteIngressPostStreamControl(context.Background(), "session-1", time.Second, coordinator, nil, func(context.Context, string) (*providerv1.GetSessionStatusResponse, error) {
		select {
		case <-statusPending:
			return &providerv1.GetSessionStatusResponse{Active: true}, nil
		default:
			close(statusPending)
			return &providerv1.GetSessionStatusResponse{Active: true, PaymentControlPending: true}, nil
		}
	})
	require.NoError(t, err)
}
