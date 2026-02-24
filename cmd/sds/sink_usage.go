package main

import (
	"fmt"
	"math/big"
	"os"
	"sync/atomic"

	sds "github.com/graphprotocol/substreams-data-service"
)

// PriceConverter converts GRT amounts to fiat currency
type PriceConverter interface {
	// ToFiat converts a GRT amount (in wei) to fiat value
	ToFiat(grtWei *big.Int) float64
	// Symbol returns the fiat currency symbol (e.g., "$")
	Symbol() string
}

// StaticPriceConverter uses a fixed GRT/USD exchange rate
type StaticPriceConverter struct {
	grtToUSD float64 // e.g., 0.15 means 1 GRT = $0.15
}

// NewStaticPriceConverter creates a converter with a fixed GRT/USD rate
func NewStaticPriceConverter(grtToUSD float64) *StaticPriceConverter {
	return &StaticPriceConverter{grtToUSD: grtToUSD}
}

func (c *StaticPriceConverter) ToFiat(grtWei *big.Int) float64 {
	if grtWei == nil || grtWei.Sign() == 0 {
		return 0
	}

	// Convert wei (10^18 base units) to GRT as float64
	// grtWei / 10^18 = GRT value
	grtFloat := new(big.Float).SetInt(grtWei)
	divisor := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	grtFloat.Quo(grtFloat, divisor)

	grtValue, _ := grtFloat.Float64()
	return grtValue * c.grtToUSD
}

func (c *StaticPriceConverter) Symbol() string {
	return "$"
}

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

// UsageReport represents a usage report snapshot
type UsageReport struct {
	BlocksReceived   uint64 // All blocks received from stream
	BlocksProcessed  uint64 // Blocks with actual output data
	BytesTransferred uint64
	Requests         uint64
	CostGRT          *big.Int
}

// PrintUsageReport prints the usage report to stderr
func PrintUsageReport(report UsageReport, ravValue sds.GRT, pricingConfig *sds.PricingConfig, priceConverter PriceConverter) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "📊 Usage Report")
	fmt.Fprintf(os.Stderr, " • Egress Bytes (uncompressed): %s\n", formatBytes(report.BytesTransferred))
	fmt.Fprintf(os.Stderr, " • Processed Blocks: %d blocks\n", report.BlocksProcessed)
	fmt.Fprintf(os.Stderr, " • Received Blocks: %d blocks\n", report.BlocksReceived)

	// Calculate and show cost
	var cost sds.GRT
	if pricingConfig != nil {
		cost = pricingConfig.CalculateUsageCost(report.BlocksProcessed, report.BytesTransferred)
	} else {
		cost = ravValue
	}

	if priceConverter != nil && !cost.IsZero() {
		fiat := priceConverter.ToFiat(cost.BigInt())
		fmt.Fprintf(os.Stderr, " • Cost: %s (%s%.4f)\n", cost.String(), priceConverter.Symbol(), fiat)
	} else {
		fmt.Fprintf(os.Stderr, " • Cost: %s\n", cost.String())
	}
}

// formatBytes formats bytes into human-readable format
func formatBytes(bytes uint64) string {
	const (
		KiB = 1024
		MiB = KiB * 1024
		GiB = MiB * 1024
		TiB = GiB * 1024
	)

	switch {
	case bytes >= TiB:
		return fmt.Sprintf("%.2f TiB", float64(bytes)/TiB)
	case bytes >= GiB:
		return fmt.Sprintf("%.2f GiB", float64(bytes)/GiB)
	case bytes >= MiB:
		return fmt.Sprintf("%.2f MiB", float64(bytes)/MiB)
	case bytes >= KiB:
		return fmt.Sprintf("%.1f KiB", float64(bytes)/KiB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
