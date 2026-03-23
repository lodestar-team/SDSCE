package sds

import "sync/atomic"

// UsageTracker tracks sink usage metrics with atomic operations for concurrent access
type UsageTracker struct {
	blocksReceived   atomic.Uint64 // All blocks received from stream
	blocksProcessed  atomic.Uint64 // Blocks with actual output data
	bytesTransferred atomic.Uint64
	requests         atomic.Uint64

	// Cumulative totals (not reset on report)
	totalBlocksReceived   atomic.Uint64
	totalBlocksProcessed  atomic.Uint64
	totalBytesTransferred atomic.Uint64
	totalRequests         atomic.Uint64

	priceConverter PriceConverter
}

// NewUsageTracker creates a new usage tracker with the given price converter
func NewUsageTracker(priceConverter PriceConverter) *UsageTracker {
	if priceConverter == nil {
		priceConverter = NewStaticPriceConverter(0.15) // Default: 1 GRT = $0.15
	}
	return &UsageTracker{
		priceConverter: priceConverter,
	}
}

// AddBlock records a received block with its data size.
// If dataBytes > 0, the block is counted as both received and processed.
// If dataBytes == 0, the block is only counted as received (no output data).
func (t *UsageTracker) AddBlock(dataBytes uint64) {
	t.blocksReceived.Add(1)
	t.totalBlocksReceived.Add(1)

	if dataBytes > 0 {
		t.blocksProcessed.Add(1)
		t.bytesTransferred.Add(dataBytes)
		t.totalBlocksProcessed.Add(1)
		t.totalBytesTransferred.Add(dataBytes)
	}

	t.requests.Add(1)
	t.totalRequests.Add(1)
}

// SwapAndGetUsage atomically swaps current usage counters and returns the values
// This is used for periodic reporting where we want to report deltas
func (t *UsageTracker) SwapAndGetUsage() (blocksReceived, blocksProcessed, bytes, requests uint64) {
	blocksReceived = t.blocksReceived.Swap(0)
	blocksProcessed = t.blocksProcessed.Swap(0)
	bytes = t.bytesTransferred.Swap(0)
	requests = t.requests.Swap(0)
	return
}

// GetTotalUsage returns the cumulative usage totals
func (t *UsageTracker) GetTotalUsage() (blocksReceived, blocksProcessed, bytes, requests uint64) {
	blocksReceived = t.totalBlocksReceived.Load()
	blocksProcessed = t.totalBlocksProcessed.Load()
	bytes = t.totalBytesTransferred.Load()
	requests = t.totalRequests.Load()
	return
}
