package main

import (
	"math/big"
	"testing"
	"time"

	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/stretchr/testify/require"
)

func daemonTestRecord(state providerv1.CollectionState, valueWei int64, attempt uint64, updatedAt time.Time) *providerv1.CollectionRecord {
	return &providerv1.CollectionRecord{
		State:          state,
		AttemptCount:   attempt,
		UpdatedAtNs:    uint64(updatedAt.UnixNano()),
		ValueAggregate: commonv1.GRTFromBigInt(big.NewInt(valueWei)),
	}
}

func TestAutoCollectorShouldCollect(t *testing.T) {
	c := &autoCollector{
		minValue:    big.NewInt(10),
		maxAttempts: 3,
		backoffBase: time.Minute,
	}
	now := time.Now()
	collectible := providerv1.CollectionState_COLLECTION_STATE_COLLECTIBLE
	retryable := providerv1.CollectionState_COLLECTION_STATE_COLLECT_FAILED_RETRYABLE

	tests := []struct {
		name   string
		record *providerv1.CollectionRecord
		want   bool
	}{
		{"collectible above threshold", daemonTestRecord(collectible, 100, 0, now), true},
		{"collectible at threshold", daemonTestRecord(collectible, 10, 0, now), true},
		{"collectible below threshold", daemonTestRecord(collectible, 9, 0, now), false},
		{"collectible nil value", &providerv1.CollectionRecord{State: collectible}, false},
		{"retryable past backoff", daemonTestRecord(retryable, 100, 1, now.Add(-5*time.Minute)), true},
		{"retryable within backoff", daemonTestRecord(retryable, 100, 2, now.Add(-30*time.Second)), false},
		{"retryable attempts exhausted", daemonTestRecord(retryable, 100, 3, now.Add(-time.Hour)), false},
		{"retryable below threshold", daemonTestRecord(retryable, 5, 1, now.Add(-time.Hour)), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, c.shouldCollect(tt.record))
		})
	}
}

func TestAutoCollectorShouldReclaimPending(t *testing.T) {
	now := time.Now()
	pending := providerv1.CollectionState_COLLECTION_STATE_COLLECT_PENDING
	collectible := providerv1.CollectionState_COLLECTION_STATE_COLLECTIBLE

	t.Run("disabled when reclaimAfter is zero", func(t *testing.T) {
		c := &autoCollector{reclaimAfter: 0}
		require.False(t, c.shouldReclaimPending(daemonTestRecord(pending, 1, 1, now.Add(-time.Hour)), now))
	})

	c := &autoCollector{reclaimAfter: 10 * time.Minute}
	t.Run("stale pending is reclaimed", func(t *testing.T) {
		require.True(t, c.shouldReclaimPending(daemonTestRecord(pending, 1, 1, now.Add(-15*time.Minute)), now))
	})
	t.Run("fresh pending is left alone", func(t *testing.T) {
		require.False(t, c.shouldReclaimPending(daemonTestRecord(pending, 1, 1, now.Add(-5*time.Minute)), now))
	})
	t.Run("non-pending is ignored", func(t *testing.T) {
		require.False(t, c.shouldReclaimPending(daemonTestRecord(collectible, 1, 1, now.Add(-time.Hour)), now))
	})
}

func TestAutoCollectorBackoffFor(t *testing.T) {
	c := &autoCollector{backoffBase: time.Minute}

	require.Equal(t, time.Duration(0), c.backoffFor(0))
	require.Equal(t, time.Minute, c.backoffFor(1))
	require.Equal(t, 2*time.Minute, c.backoffFor(2))
	require.Equal(t, 4*time.Minute, c.backoffFor(3))
	require.Equal(t, 8*time.Minute, c.backoffFor(4))
	// Exponential growth is capped at one hour.
	require.Equal(t, collectDaemonMaxBackoff, c.backoffFor(100))
}
