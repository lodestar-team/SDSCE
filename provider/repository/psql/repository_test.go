package psql

import (
	"context"
	"math/big"
	"testing"
	"time"

	sds "github.com/graphprotocol/substreams-data-service"
	"github.com/graphprotocol/substreams-data-service/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/streamingfast/eth-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionCreateAndGet(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()

		// Create pricing config
		pricingConfig := sds.PricingConfig{
			PricePerBlock: sds.MustNewGRT(100),
			PricePerByte:  sds.MustNewGRT(10),
		}

		// Create test addresses
		payer := eth.MustNewAddress("0x1234567890123456789012345678901234567890")
		receiver := eth.MustNewAddress("0x2234567890123456789012345678901234567890")
		dataService := eth.MustNewAddress("0x3234567890123456789012345678901234567890")

		// Create session
		session := repository.NewSession("test-session-1", payer, receiver, dataService, pricingConfig)
		session.Metadata["key1"] = "value1"
		session.Metadata["key2"] = "value2"

		// Insert session
		err := db.SessionCreate(ctx, session)
		require.NoError(t, err)

		// Retrieve session
		retrieved, err := db.SessionGet(ctx, "test-session-1")
		require.NoError(t, err)
		require.NotNil(t, retrieved)

		// Verify fields
		assert.Equal(t, session.ID, retrieved.ID)
		assert.Equal(t, session.Payer.Pretty(), retrieved.Payer.Pretty())
		assert.Equal(t, session.Receiver.Pretty(), retrieved.Receiver.Pretty())
		assert.Equal(t, session.DataService.Pretty(), retrieved.DataService.Pretty())
		assert.Equal(t, session.Status, retrieved.Status)
		assert.Equal(t, session.Metadata, retrieved.Metadata)
		assert.Nil(t, retrieved.CurrentRAV)
	})
}

func TestSessionWithRAV(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()

		pricingConfig := sds.PricingConfig{
			PricePerBlock: sds.MustNewGRT(100),
			PricePerByte:  sds.MustNewGRT(10),
		}

		payer := eth.MustNewAddress("0x1234567890123456789012345678901234567890")
		receiver := eth.MustNewAddress("0x2234567890123456789012345678901234567890")
		dataService := eth.MustNewAddress("0x3234567890123456789012345678901234567890")

		// Create session with RAV
		session := repository.NewSession("test-session-rav", payer, receiver, dataService, pricingConfig)

		// Create RAV
		collectionID := horizon.CollectionID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
		sig := eth.Signature{
			1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
			11, 12, 13, 14, 15, 16, 17, 18, 19, 20,
			21, 22, 23, 24, 25, 26, 27, 28, 29, 30,
			31, 32, 33, 34, 35, 36, 37, 38, 39, 40,
			41, 42, 43, 44, 45, 46, 47, 48, 49, 50,
			51, 52, 53, 54, 55, 56, 57, 58, 59, 60,
			61, 62, 63, 64, 65,
		}

		session.CurrentRAV = &horizon.SignedRAV{
			Message: &horizon.RAV{
				CollectionID:    collectionID,
				Payer:           payer,
				ServiceProvider: receiver,
				DataService:     dataService,
				TimestampNs:     uint64(time.Now().UnixNano()),
				ValueAggregate:  big.NewInt(1000000),
				Metadata:        []byte("test metadata"),
			},
			Signature: sig,
		}

		// Insert session with RAV
		err := db.SessionCreate(ctx, session)
		require.NoError(t, err)

		// Retrieve session
		retrieved, err := db.SessionGet(ctx, "test-session-rav")
		require.NoError(t, err)
		require.NotNil(t, retrieved)
		require.NotNil(t, retrieved.CurrentRAV)

		// Verify RAV
		assert.Equal(t, session.CurrentRAV.Message.CollectionID, retrieved.CurrentRAV.Message.CollectionID)
		assert.Equal(t, session.CurrentRAV.Message.ValueAggregate, retrieved.CurrentRAV.Message.ValueAggregate)
		assert.Equal(t, session.CurrentRAV.Signature, retrieved.CurrentRAV.Signature)
	})
}

func TestSessionUpdate(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()

		pricingConfig := sds.PricingConfig{
			PricePerBlock: sds.MustNewGRT(100),
			PricePerByte:  sds.MustNewGRT(10),
		}

		payer := eth.MustNewAddress("0x1234567890123456789012345678901234567890")
		receiver := eth.MustNewAddress("0x2234567890123456789012345678901234567890")
		dataService := eth.MustNewAddress("0x3234567890123456789012345678901234567890")

		session := repository.NewSession("test-session-update", payer, receiver, dataService, pricingConfig)
		err := db.SessionCreate(ctx, session)
		require.NoError(t, err)

		// Update session
		session.AddUsage(100, 2000, 10, big.NewInt(5000))
		session.End(commonv1.EndReason_END_REASON_CLIENT_DISCONNECT)

		err = db.SessionUpdate(ctx, session)
		require.NoError(t, err)

		// Retrieve and verify
		retrieved, err := db.SessionGet(ctx, "test-session-update")
		require.NoError(t, err)

		assert.Equal(t, uint64(100), retrieved.BlocksProcessed)
		assert.Equal(t, uint64(2000), retrieved.BytesTransferred)
		assert.Equal(t, uint64(10), retrieved.Requests)
		assert.Equal(t, big.NewInt(5000), retrieved.TotalCost)
		assert.Equal(t, repository.SessionStatusTerminated, retrieved.Status)
		assert.NotNil(t, retrieved.EndedAt)
		assert.Equal(t, commonv1.EndReason_END_REASON_CLIENT_DISCONNECT, retrieved.EndReason)
	})
}

func TestSessionList(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()

		pricingConfig := sds.PricingConfig{
			PricePerBlock: sds.MustNewGRT(100),
			PricePerByte:  sds.MustNewGRT(10),
		}

		payer1 := eth.MustNewAddress("0x1234567890123456789012345678901234567890")
		payer2 := eth.MustNewAddress("0x2234567890123456789012345678901234567890")
		receiver := eth.MustNewAddress("0x3234567890123456789012345678901234567890")
		dataService := eth.MustNewAddress("0x4234567890123456789012345678901234567890")

		// Create multiple sessions
		session1 := repository.NewSession("session-1", payer1, receiver, dataService, pricingConfig)
		session2 := repository.NewSession("session-2", payer2, receiver, dataService, pricingConfig)
		session3 := repository.NewSession("session-3", payer1, receiver, dataService, pricingConfig)

		require.NoError(t, db.SessionCreate(ctx, session1))
		require.NoError(t, db.SessionCreate(ctx, session2))
		require.NoError(t, db.SessionCreate(ctx, session3))

		// List all sessions
		sessions, err := db.SessionList(ctx, repository.SessionFilter{})
		require.NoError(t, err)
		assert.Len(t, sessions, 3)

		// Filter by payer
		sessions, err = db.SessionList(ctx, repository.SessionFilter{
			Payer: &payer1,
		})
		require.NoError(t, err)
		assert.Len(t, sessions, 2)
	})
}

func TestSessionCount(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()

		pricingConfig := sds.PricingConfig{
			PricePerBlock: sds.MustNewGRT(100),
			PricePerByte:  sds.MustNewGRT(10),
		}

		payer := eth.MustNewAddress("0x1234567890123456789012345678901234567890")
		receiver := eth.MustNewAddress("0x2234567890123456789012345678901234567890")
		dataService := eth.MustNewAddress("0x3234567890123456789012345678901234567890")

		assert.Equal(t, 0, db.SessionCount(ctx))

		session1 := repository.NewSession("count-1", payer, receiver, dataService, pricingConfig)
		session2 := repository.NewSession("count-2", payer, receiver, dataService, pricingConfig)

		require.NoError(t, db.SessionCreate(ctx, session1))
		assert.Equal(t, 1, db.SessionCount(ctx))

		require.NoError(t, db.SessionCreate(ctx, session2))
		assert.Equal(t, 2, db.SessionCount(ctx))
	})
}

func TestWorkerCreateAndGet(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()

		pricingConfig := sds.PricingConfig{
			PricePerBlock: sds.MustNewGRT(100),
			PricePerByte:  sds.MustNewGRT(10),
		}

		payer := eth.MustNewAddress("0x1234567890123456789012345678901234567890")
		receiver := eth.MustNewAddress("0x2234567890123456789012345678901234567890")
		dataService := eth.MustNewAddress("0x3234567890123456789012345678901234567890")

		// Create session first
		session := repository.NewSession("worker-session-1", payer, receiver, dataService, pricingConfig)
		require.NoError(t, db.SessionCreate(ctx, session))

		// Create worker
		worker := &repository.Worker{
			Key:       "worker-1",
			SessionID: "worker-session-1",
			Payer:     payer,
			CreatedAt: time.Now(),
			TraceID:   "trace-123",
		}

		err := db.WorkerCreate(ctx, worker)
		require.NoError(t, err)

		// Retrieve worker
		retrieved, err := db.WorkerGet(ctx, "worker-1")
		require.NoError(t, err)
		assert.Equal(t, worker.Key, retrieved.Key)
		assert.Equal(t, worker.SessionID, retrieved.SessionID)
		assert.Equal(t, worker.TraceID, retrieved.TraceID)
	})
}

func TestWorkerDelete(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()

		pricingConfig := sds.PricingConfig{
			PricePerBlock: sds.MustNewGRT(100),
			PricePerByte:  sds.MustNewGRT(10),
		}

		payer := eth.MustNewAddress("0x1234567890123456789012345678901234567890")
		receiver := eth.MustNewAddress("0x2234567890123456789012345678901234567890")
		dataService := eth.MustNewAddress("0x3234567890123456789012345678901234567890")

		session := repository.NewSession("worker-session-del", payer, receiver, dataService, pricingConfig)
		require.NoError(t, db.SessionCreate(ctx, session))

		worker := &repository.Worker{
			Key:       "worker-del",
			SessionID: "worker-session-del",
			Payer:     payer,
			CreatedAt: time.Now(),
		}

		require.NoError(t, db.WorkerCreate(ctx, worker))

		// Delete worker
		err := db.WorkerDelete(ctx, "worker-del")
		require.NoError(t, err)

		// Verify deletion
		_, err = db.WorkerGet(ctx, "worker-del")
		assert.ErrorIs(t, err, repository.ErrNotFound)
	})
}

func TestQuotaGetAndIncrement(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()

		payer := eth.MustNewAddress("0x1234567890123456789012345678901234567890")

		// Get initial quota (should be zero)
		quota, err := db.QuotaGet(ctx, payer)
		require.NoError(t, err)
		assert.Equal(t, 0, quota.ActiveSessions)
		assert.Equal(t, 0, quota.ActiveWorkers)

		// Increment quota
		err = db.QuotaIncrement(ctx, payer, 2, 5)
		require.NoError(t, err)

		// Verify quota
		quota, err = db.QuotaGet(ctx, payer)
		require.NoError(t, err)
		assert.Equal(t, 2, quota.ActiveSessions)
		assert.Equal(t, 5, quota.ActiveWorkers)

		// Increment again
		err = db.QuotaIncrement(ctx, payer, 1, 3)
		require.NoError(t, err)

		quota, err = db.QuotaGet(ctx, payer)
		require.NoError(t, err)
		assert.Equal(t, 3, quota.ActiveSessions)
		assert.Equal(t, 8, quota.ActiveWorkers)
	})
}

func TestQuotaDecrement(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()

		payer := eth.MustNewAddress("0x1234567890123456789012345678901234567890")

		// Set initial quota
		err := db.QuotaIncrement(ctx, payer, 5, 10)
		require.NoError(t, err)

		// Decrement quota
		err = db.QuotaDecrement(ctx, payer, 2, 3)
		require.NoError(t, err)

		// Verify quota
		quota, err := db.QuotaGet(ctx, payer)
		require.NoError(t, err)
		assert.Equal(t, 3, quota.ActiveSessions)
		assert.Equal(t, 7, quota.ActiveWorkers)

		// Decrement below zero (should not go negative)
		err = db.QuotaDecrement(ctx, payer, 10, 20)
		require.NoError(t, err)

		quota, err = db.QuotaGet(ctx, payer)
		require.NoError(t, err)
		assert.Equal(t, 0, quota.ActiveSessions)
		assert.Equal(t, 0, quota.ActiveWorkers)
	})
}

func TestUsageAddAndGetTotal(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()

		pricingConfig := sds.PricingConfig{
			PricePerBlock: sds.MustNewGRT(100),
			PricePerByte:  sds.MustNewGRT(10),
		}

		payer := eth.MustNewAddress("0x1234567890123456789012345678901234567890")
		receiver := eth.MustNewAddress("0x2234567890123456789012345678901234567890")
		dataService := eth.MustNewAddress("0x3234567890123456789012345678901234567890")

		session := repository.NewSession("usage-session", payer, receiver, dataService, pricingConfig)
		require.NoError(t, db.SessionCreate(ctx, session))

		// Add usage events
		event1 := &repository.UsageEvent{
			Timestamp: time.Now(),
			Blocks:    100,
			Bytes:     2000,
			Requests:  10,
		}
		event2 := &repository.UsageEvent{
			Timestamp: time.Now(),
			Blocks:    200,
			Bytes:     3000,
			Requests:  15,
		}

		require.NoError(t, db.UsageAdd(ctx, "usage-session", event1))
		require.NoError(t, db.UsageAdd(ctx, "usage-session", event2))

		// Verify events were added successfully
		// (UsageGetTotal was removed as unused; we just verify Add works)
	})
}

func TestSessionApplyUsage(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()

		pricingConfig := sds.PricingConfig{
			PricePerBlock: sds.MustNewGRT(100),
			PricePerByte:  sds.MustNewGRT(10),
		}

		payer := eth.MustNewAddress("0x1234567890123456789012345678901234567890")
		receiver := eth.MustNewAddress("0x2234567890123456789012345678901234567890")
		dataService := eth.MustNewAddress("0x3234567890123456789012345678901234567890")

		session := repository.NewSession("session-apply-usage", payer, receiver, dataService, pricingConfig)
		require.NoError(t, db.SessionCreate(ctx, session))

		event := &repository.UsageEvent{
			Timestamp: time.Now(),
			Blocks:    100,
			Bytes:     2000,
			Requests:  10,
		}
		cost := pricingConfig.CalculateUsageCost(100, 2000).BigInt()

		require.NoError(t, db.SessionApplyUsage(ctx, "session-apply-usage", event, cost))

		retrieved, err := db.SessionGet(ctx, "session-apply-usage")
		require.NoError(t, err)
		assert.Equal(t, uint64(100), retrieved.BlocksProcessed)
		assert.Equal(t, uint64(2000), retrieved.BytesTransferred)
		assert.Equal(t, uint64(10), retrieved.Requests)
		assert.Equal(t, 0, retrieved.TotalCost.Cmp(cost))

		var usageEventCount int
		err = db.GetContext(ctx, &usageEventCount, `SELECT COUNT(*) FROM usage_events WHERE session_id = $1`, "session-apply-usage")
		require.NoError(t, err)
		assert.Equal(t, 1, usageEventCount)
	})
}

// TestCascadeDelete was removed because SessionDelete is not used in production code
