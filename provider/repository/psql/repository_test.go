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

		record, err := db.CollectionGet(ctx, repository.CollectionKey{
			SessionID:       session.ID,
			CollectionID:    session.CurrentRAV.Message.CollectionID,
			Payer:           session.CurrentRAV.Message.Payer,
			ServiceProvider: session.CurrentRAV.Message.ServiceProvider,
			DataService:     session.CurrentRAV.Message.DataService,
		})
		require.NoError(t, err)
		assert.Equal(t, repository.CollectionStateCollectible, record.State)
		assert.Equal(t, 0, session.CurrentRAV.Message.ValueAggregate.Cmp(record.ValueAggregate))
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

func createTestCollectionSession(t *testing.T, ctx context.Context, db *Database, sessionID string) *repository.Session {
	t.Helper()

	pricingConfig := sds.PricingConfig{
		PricePerBlock: sds.MustNewGRT(100),
		PricePerByte:  sds.MustNewGRT(10),
	}
	payer := eth.MustNewAddress("0x1234567890123456789012345678901234567890")
	receiver := eth.MustNewAddress("0x2234567890123456789012345678901234567890")
	dataService := eth.MustNewAddress("0x3234567890123456789012345678901234567890")
	session := repository.NewSession(sessionID, payer, receiver, dataService, pricingConfig)
	require.NoError(t, db.SessionCreate(ctx, session))
	return session
}

func newPSQLCollectionRAV(payer, receiver, dataService eth.Address, value *big.Int) *horizon.SignedRAV {
	var sig eth.Signature
	for i := range sig {
		sig[i] = byte(i + 1)
	}

	return &horizon.SignedRAV{
		Message: &horizon.RAV{
			CollectionID: horizon.CollectionID{
				0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
				0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
				0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27,
				0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f,
			},
			Payer:           payer,
			ServiceProvider: receiver,
			DataService:     dataService,
			TimestampNs:     uint64(time.Now().UnixNano()),
			ValueAggregate:  new(big.Int).Set(value),
			Metadata:        []byte("collection-test"),
		},
		Signature: sig,
	}
}

func psqlCollectionKeyForRAV(sessionID string, rav *horizon.SignedRAV) repository.CollectionKey {
	return repository.CollectionKey{
		SessionID:       sessionID,
		CollectionID:    rav.Message.CollectionID,
		Payer:           rav.Message.Payer,
		ServiceProvider: rav.Message.ServiceProvider,
		DataService:     rav.Message.DataService,
	}
}

func TestSessionUpdateRAVAndBaseline_PreservesUsageTotals(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()

		pricingConfig := sds.PricingConfig{
			PricePerBlock: sds.MustNewGRT(100),
			PricePerByte:  sds.MustNewGRT(10),
		}

		payer := eth.MustNewAddress("0x1234567890123456789012345678901234567890")
		receiver := eth.MustNewAddress("0x2234567890123456789012345678901234567890")
		dataService := eth.MustNewAddress("0x3234567890123456789012345678901234567890")

		session := repository.NewSession("test-session-rav-baseline", payer, receiver, dataService, pricingConfig)
		require.NoError(t, db.SessionCreate(ctx, session))

		event := &repository.UsageEvent{
			Timestamp: time.Now(),
			Blocks:    100,
			Bytes:     2000,
			Requests:  10,
		}
		cost := pricingConfig.CalculateUsageCost(100, 2000).BigInt()
		require.NoError(t, db.SessionApplyUsage(ctx, session.ID, event, cost))

		var sig eth.Signature
		sig[0] = 1

		signedRAV := &horizon.SignedRAV{
			Message: &horizon.RAV{
				CollectionID:    horizon.CollectionID{1},
				Payer:           payer,
				ServiceProvider: receiver,
				DataService:     dataService,
				TimestampNs:     uint64(time.Now().UnixNano()),
				ValueAggregate:  big.NewInt(1234),
			},
			Signature: sig,
		}
		require.NoError(t, db.SessionUpdateRAVAndBaseline(ctx, session.ID, signedRAV, 100, 2000, 10, cost))

		retrieved, err := db.SessionGet(ctx, session.ID)
		require.NoError(t, err)
		require.NotNil(t, retrieved.CurrentRAV)
		assert.Equal(t, 0, big.NewInt(1234).Cmp(retrieved.CurrentRAV.Message.ValueAggregate))
		record, err := db.CollectionGet(ctx, repository.CollectionKey{
			SessionID:       session.ID,
			CollectionID:    signedRAV.Message.CollectionID,
			Payer:           signedRAV.Message.Payer,
			ServiceProvider: signedRAV.Message.ServiceProvider,
			DataService:     signedRAV.Message.DataService,
		})
		require.NoError(t, err)
		assert.Equal(t, repository.CollectionStateCollectible, record.State)
		assert.Equal(t, 0, big.NewInt(1234).Cmp(record.ValueAggregate))
		assert.Equal(t, uint64(100), retrieved.BlocksProcessed)
		assert.Equal(t, uint64(2000), retrieved.BytesTransferred)
		assert.Equal(t, uint64(10), retrieved.Requests)
		assert.Equal(t, 0, cost.Cmp(retrieved.TotalCost))
		assert.Equal(t, uint64(100), retrieved.BaselineBlocks)
		assert.Equal(t, uint64(2000), retrieved.BaselineBytes)
		assert.Equal(t, uint64(10), retrieved.BaselineReqs)
		assert.Equal(t, 0, cost.Cmp(retrieved.BaselineCost))
	})
}

func TestSessionUpdateRAVAndBaseline_SurvivesRepositoryRestart(t *testing.T) {
	withTestDBSchema(t, func(db *Database, dsn string, schema string) {
		ctx := context.Background()

		pricingConfig := sds.PricingConfig{
			PricePerBlock: sds.MustNewGRT(100),
			PricePerByte:  sds.MustNewGRT(10),
		}

		payer := eth.MustNewAddress("0x1234567890123456789012345678901234567890")
		receiver := eth.MustNewAddress("0x2234567890123456789012345678901234567890")
		dataService := eth.MustNewAddress("0x3234567890123456789012345678901234567890")

		session := repository.NewSession("test-session-rav-restart", payer, receiver, dataService, pricingConfig)
		session.Metadata["runtime"] = "postgres"
		require.NoError(t, db.SessionCreate(ctx, session))

		cost := pricingConfig.CalculateUsageCost(125, 4096).BigInt()
		require.NoError(t, db.SessionApplyUsage(ctx, session.ID, &repository.UsageEvent{
			Timestamp: time.Now(),
			Blocks:    125,
			Bytes:     4096,
			Requests:  7,
		}, cost))

		collectionID := horizon.CollectionID{
			0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
			0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
			0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27,
			0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f,
		}
		var sig eth.Signature
		for i := range sig {
			sig[i] = byte(i + 1)
		}

		acceptedRAV := &horizon.SignedRAV{
			Message: &horizon.RAV{
				CollectionID:    collectionID,
				Payer:           payer,
				ServiceProvider: receiver,
				DataService:     dataService,
				TimestampNs:     uint64(time.Now().UnixNano()),
				ValueAggregate:  cost,
				Metadata:        []byte("accepted-after-metering"),
			},
			Signature: sig,
		}
		require.NoError(t, db.SessionUpdateRAVAndBaseline(ctx, session.ID, acceptedRAV, 125, 4096, 7, cost))
		require.NoError(t, db.Close())

		restarted := reopenTestRepository(t, dsn, schema)
		defer restarted.Close()

		retrieved, err := restarted.SessionGet(ctx, session.ID)
		require.NoError(t, err)
		require.NotNil(t, retrieved.CurrentRAV)

		rav := retrieved.CurrentRAV
		assert.Equal(t, collectionID, rav.Message.CollectionID)
		assert.Equal(t, payer.Pretty(), rav.Message.Payer.Pretty())
		assert.Equal(t, receiver.Pretty(), rav.Message.ServiceProvider.Pretty())
		assert.Equal(t, dataService.Pretty(), rav.Message.DataService.Pretty())
		assert.Equal(t, acceptedRAV.Message.TimestampNs, rav.Message.TimestampNs)
		assert.Equal(t, 0, cost.Cmp(rav.Message.ValueAggregate))
		assert.Equal(t, acceptedRAV.Message.Metadata, rav.Message.Metadata)
		assert.Equal(t, sig, rav.Signature)

		assert.Equal(t, repository.SessionStatusActive, retrieved.Status)
		assert.Equal(t, session.Metadata, retrieved.Metadata)
		assert.Equal(t, uint64(125), retrieved.BlocksProcessed)
		assert.Equal(t, uint64(4096), retrieved.BytesTransferred)
		assert.Equal(t, uint64(7), retrieved.Requests)
		assert.Equal(t, 0, cost.Cmp(retrieved.TotalCost))
		assert.Equal(t, uint64(125), retrieved.BaselineBlocks)
		assert.Equal(t, uint64(4096), retrieved.BaselineBytes)
		assert.Equal(t, uint64(7), retrieved.BaselineReqs)
		assert.Equal(t, 0, cost.Cmp(retrieved.BaselineCost))
	})
}

func TestCollectionLifecycleTransitions(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()
		session := createTestCollectionSession(t, ctx, db, "test-collection-lifecycle")
		rav := newPSQLCollectionRAV(session.Payer, session.Receiver, session.DataService, big.NewInt(100))

		record, err := db.CollectionCreateOrUpdateCollectible(ctx, session.ID, rav)
		require.NoError(t, err)
		assert.Equal(t, repository.CollectionStateCollectible, record.State)
		assert.Equal(t, 0, big.NewInt(100).Cmp(record.ValueAggregate))
		assert.Equal(t, 0, record.AttemptCount)

		pendingAt := time.Now().Add(time.Minute).UTC().Truncate(time.Second)
		record, err = db.CollectionMarkPending(ctx, record.Key, big.NewInt(100), "0xabc", pendingAt)
		require.NoError(t, err)
		assert.Equal(t, repository.CollectionStateCollectPending, record.State)
		assert.Equal(t, 1, record.AttemptCount)
		assert.Equal(t, "0xabc", record.LastTxHash)

		record, err = db.CollectionMarkFailedRetryable(ctx, record.Key, big.NewInt(100), "0xabc", "receipt timeout", pendingAt.Add(time.Minute))
		require.NoError(t, err)
		assert.Equal(t, repository.CollectionStateCollectFailedRetryable, record.State)
		assert.Equal(t, "receipt timeout", record.LastError)

		record, err = db.CollectionMarkPending(ctx, record.Key, big.NewInt(100), "0xdef", pendingAt.Add(2*time.Minute))
		require.NoError(t, err)
		assert.Equal(t, repository.CollectionStateCollectPending, record.State)
		assert.Equal(t, 2, record.AttemptCount)
		assert.Empty(t, record.LastError)

		record, err = db.CollectionMarkCollected(ctx, record.Key, big.NewInt(100), "0xdef", big.NewInt(95), pendingAt.Add(3*time.Minute))
		require.NoError(t, err)
		assert.Equal(t, repository.CollectionStateCollected, record.State)
		assert.Equal(t, 0, big.NewInt(95).Cmp(record.CollectedAmount))
		assert.Equal(t, "0xdef", record.LastTxHash)

		got, err := db.CollectionGet(ctx, record.Key)
		require.NoError(t, err)
		assert.Equal(t, repository.CollectionStateCollected, got.State)
		assert.Equal(t, rav.Signature, got.SignedRAV.Signature)
		assert.Equal(t, rav.Message.CollectionID, got.SignedRAV.Message.CollectionID)

		state := repository.CollectionStateCollected
		listed, err := db.CollectionList(ctx, repository.CollectionFilter{State: &state})
		require.NoError(t, err)
		require.Len(t, listed, 1)
		assert.Equal(t, record.Key.SessionID, listed[0].Key.SessionID)
	})
}

func TestCollectionLifecycleRejectsStaleAndBackwardsTransitions(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()
		session := createTestCollectionSession(t, ctx, db, "test-collection-invalid")
		rav := newPSQLCollectionRAV(session.Payer, session.Receiver, session.DataService, big.NewInt(100))

		record, err := db.CollectionCreateOrUpdateCollectible(ctx, session.ID, rav)
		require.NoError(t, err)

		_, err = db.CollectionMarkPending(ctx, record.Key, big.NewInt(99), "0xabc", time.Now())
		require.ErrorIs(t, err, repository.ErrCollectionConflict)

		record, err = db.CollectionMarkPending(ctx, record.Key, big.NewInt(100), "0xabc", time.Now())
		require.NoError(t, err)
		record, err = db.CollectionMarkCollected(ctx, record.Key, big.NewInt(100), "0xabc", big.NewInt(100), time.Now())
		require.NoError(t, err)

		_, err = db.CollectionMarkPending(ctx, record.Key, big.NewInt(100), "0xdef", time.Now())
		require.ErrorIs(t, err, repository.ErrInvalidCollectionTransition)

		same, err := db.CollectionCreateOrUpdateCollectible(ctx, session.ID, rav)
		require.NoError(t, err)
		assert.Equal(t, repository.CollectionStateCollected, same.State)

		lower := newPSQLCollectionRAV(session.Payer, session.Receiver, session.DataService, big.NewInt(90))
		_, err = db.CollectionCreateOrUpdateCollectible(ctx, session.ID, lower)
		require.ErrorIs(t, err, repository.ErrCollectionConflict)

		higher := newPSQLCollectionRAV(session.Payer, session.Receiver, session.DataService, big.NewInt(110))
		updated, err := db.CollectionCreateOrUpdateCollectible(ctx, session.ID, higher)
		require.NoError(t, err)
		assert.Equal(t, repository.CollectionStateCollectible, updated.State)
		assert.Equal(t, 0, big.NewInt(110).Cmp(updated.ValueAggregate))
	})
}

func TestSessionUpdate_DoesNotCommitRAVOnLifecycleConflict(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()
		session := createTestCollectionSession(t, ctx, db, "test-session-update-conflict")
		initial := newPSQLCollectionRAV(session.Payer, session.Receiver, session.DataService, big.NewInt(100))
		session.CurrentRAV = initial
		require.NoError(t, db.SessionUpdate(ctx, session))

		record, err := db.CollectionMarkPending(ctx, psqlCollectionKeyForRAV(session.ID, initial), big.NewInt(100), "0xabc", time.Now())
		require.NoError(t, err)
		require.Equal(t, repository.CollectionStateCollectPending, record.State)

		conflicting := newPSQLCollectionRAV(session.Payer, session.Receiver, session.DataService, big.NewInt(110))
		session.CurrentRAV = conflicting
		err = db.SessionUpdate(ctx, session)
		require.ErrorIs(t, err, repository.ErrCollectionConflict)

		got, err := db.SessionGet(ctx, session.ID)
		require.NoError(t, err)
		require.NotNil(t, got.CurrentRAV)
		assert.Equal(t, 0, big.NewInt(100).Cmp(got.CurrentRAV.Message.ValueAggregate))

		gotRecord, err := db.CollectionGet(ctx, psqlCollectionKeyForRAV(session.ID, initial))
		require.NoError(t, err)
		assert.Equal(t, repository.CollectionStateCollectPending, gotRecord.State)
		assert.Equal(t, 0, big.NewInt(100).Cmp(gotRecord.ValueAggregate))
	})
}

func TestSessionTouch_PreservesUsageTotals(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()

		pricingConfig := sds.PricingConfig{
			PricePerBlock: sds.MustNewGRT(100),
			PricePerByte:  sds.MustNewGRT(10),
		}

		payer := eth.MustNewAddress("0x1234567890123456789012345678901234567890")
		receiver := eth.MustNewAddress("0x2234567890123456789012345678901234567890")
		dataService := eth.MustNewAddress("0x3234567890123456789012345678901234567890")

		session := repository.NewSession("test-session-touch", payer, receiver, dataService, pricingConfig)
		require.NoError(t, db.SessionCreate(ctx, session))

		cost := big.NewInt(5000)
		require.NoError(t, db.SessionApplyUsage(ctx, session.ID, &repository.UsageEvent{
			Timestamp: time.Now(),
			Blocks:    100,
			Bytes:     2000,
			Requests:  10,
		}, cost))

		touchedAt := time.Now().Add(time.Minute)
		require.NoError(t, db.SessionTouch(ctx, session.ID, touchedAt))

		retrieved, err := db.SessionGet(ctx, session.ID)
		require.NoError(t, err)
		assert.Equal(t, touchedAt.Unix(), retrieved.LastKeepAlive.Unix())
		assert.Equal(t, uint64(100), retrieved.BlocksProcessed)
		assert.Equal(t, uint64(2000), retrieved.BytesTransferred)
		assert.Equal(t, uint64(10), retrieved.Requests)
		assert.Equal(t, 0, cost.Cmp(retrieved.TotalCost))
	})
}

func TestSessionTouch_IsMonotonic(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()

		pricingConfig := sds.PricingConfig{
			PricePerBlock: sds.MustNewGRT(100),
			PricePerByte:  sds.MustNewGRT(10),
		}

		payer := eth.MustNewAddress("0x1234567890123456789012345678901234567890")
		receiver := eth.MustNewAddress("0x2234567890123456789012345678901234567890")
		dataService := eth.MustNewAddress("0x3234567890123456789012345678901234567890")

		session := repository.NewSession("test-session-touch-monotonic", payer, receiver, dataService, pricingConfig)
		require.NoError(t, db.SessionCreate(ctx, session))

		newer := time.Now().Add(time.Minute).UTC().Truncate(time.Second)
		older := newer.Add(-time.Hour)
		require.NoError(t, db.SessionTouch(ctx, session.ID, newer))
		afterNewer, err := db.SessionGet(ctx, session.ID)
		require.NoError(t, err)
		require.NoError(t, db.SessionTouch(ctx, session.ID, older))

		retrieved, err := db.SessionGet(ctx, session.ID)
		require.NoError(t, err)
		assert.Equal(t, newer.Unix(), retrieved.LastKeepAlive.Unix())
		assert.Equal(t, afterNewer.UpdatedAt.UnixNano(), retrieved.UpdatedAt.UnixNano())
	})
}

func TestSessionTouch_NotFound(t *testing.T) {
	withTestDB(t, func(db *Database) {
		err := db.SessionTouch(context.Background(), "missing", time.Now())
		require.ErrorIs(t, err, repository.ErrNotFound)
	})
}

func TestSessionUpdateRuntimeState_PreservesUsageTotals(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()

		pricingConfig := sds.PricingConfig{
			PricePerBlock: sds.MustNewGRT(100),
			PricePerByte:  sds.MustNewGRT(10),
		}

		payer := eth.MustNewAddress("0x1234567890123456789012345678901234567890")
		receiver := eth.MustNewAddress("0x2234567890123456789012345678901234567890")
		dataService := eth.MustNewAddress("0x3234567890123456789012345678901234567890")

		session := repository.NewSession("test-session-runtime-state", payer, receiver, dataService, pricingConfig)
		require.NoError(t, db.SessionCreate(ctx, session))

		cost := big.NewInt(5000)
		require.NoError(t, db.SessionApplyUsage(ctx, session.ID, &repository.UsageEvent{
			Timestamp: time.Now(),
			Blocks:    100,
			Bytes:     2000,
			Requests:  10,
		}, cost))

		endedAt := time.Now().Add(time.Minute)
		metadata := map[string]string{"funds_check_error": "timeout"}
		require.NoError(t, db.SessionUpdateRuntimeState(ctx, session.ID, repository.SessionStatusTerminated, metadata, &endedAt, commonv1.EndReason_END_REASON_PAYMENT_ISSUE, endedAt))

		retrieved, err := db.SessionGet(ctx, session.ID)
		require.NoError(t, err)
		assert.Equal(t, repository.SessionStatusTerminated, retrieved.Status)
		assert.Equal(t, metadata, retrieved.Metadata)
		require.NotNil(t, retrieved.EndedAt)
		assert.Equal(t, endedAt.Unix(), retrieved.EndedAt.Unix())
		assert.Equal(t, commonv1.EndReason_END_REASON_PAYMENT_ISSUE, retrieved.EndReason)
		assert.Equal(t, uint64(100), retrieved.BlocksProcessed)
		assert.Equal(t, uint64(2000), retrieved.BytesTransferred)
		assert.Equal(t, uint64(10), retrieved.Requests)
		assert.Equal(t, 0, cost.Cmp(retrieved.TotalCost))
	})
}

func TestSessionUpdateRuntimeState_DoesNotRegressTerminatedSession(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()

		pricingConfig := sds.PricingConfig{
			PricePerBlock: sds.MustNewGRT(100),
			PricePerByte:  sds.MustNewGRT(10),
		}

		payer := eth.MustNewAddress("0x1234567890123456789012345678901234567890")
		receiver := eth.MustNewAddress("0x2234567890123456789012345678901234567890")
		dataService := eth.MustNewAddress("0x3234567890123456789012345678901234567890")

		session := repository.NewSession("test-session-runtime-state-monotonic", payer, receiver, dataService, pricingConfig)
		endedAt := time.Now().UTC().Truncate(time.Second)
		session.Status = repository.SessionStatusTerminated
		session.EndedAt = &endedAt
		session.EndReason = commonv1.EndReason_END_REASON_PAYMENT_ISSUE
		session.Metadata = map[string]string{"state": "terminated"}
		require.NoError(t, db.SessionCreate(ctx, session))
		before, err := db.SessionGet(ctx, session.ID)
		require.NoError(t, err)

		require.NoError(t, db.SessionUpdateRuntimeState(ctx, session.ID, repository.SessionStatusActive, map[string]string{"state": "active"}, nil, commonv1.EndReason_END_REASON_UNSPECIFIED, endedAt.Add(time.Minute)))

		retrieved, err := db.SessionGet(ctx, session.ID)
		require.NoError(t, err)
		assert.Equal(t, repository.SessionStatusTerminated, retrieved.Status)
		assert.Equal(t, map[string]string{"state": "terminated"}, retrieved.Metadata)
		require.NotNil(t, retrieved.EndedAt)
		assert.Equal(t, endedAt.Unix(), retrieved.EndedAt.Unix())
		assert.Equal(t, commonv1.EndReason_END_REASON_PAYMENT_ISSUE, retrieved.EndReason)
		assert.Equal(t, before.UpdatedAt.UnixNano(), retrieved.UpdatedAt.UnixNano())
	})
}

func TestSessionUpdateRuntimeState_NotFound(t *testing.T) {
	withTestDB(t, func(db *Database) {
		err := db.SessionUpdateRuntimeState(context.Background(), "missing", repository.SessionStatusActive, nil, nil, commonv1.EndReason_END_REASON_UNSPECIFIED, time.Now())
		require.ErrorIs(t, err, repository.ErrNotFound)
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

func TestWorkerCreateAndReserveQuota(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()

		pricingConfig := sds.PricingConfig{
			PricePerBlock: sds.MustNewGRT(100),
			PricePerByte:  sds.MustNewGRT(10),
		}

		payer := eth.MustNewAddress("0x1234567890123456789012345678901234567890")
		receiver := eth.MustNewAddress("0x2234567890123456789012345678901234567890")
		dataService := eth.MustNewAddress("0x3234567890123456789012345678901234567890")

		session := repository.NewSession("atomic-worker-session-1", payer, receiver, dataService, pricingConfig)
		require.NoError(t, db.SessionCreate(ctx, session))

		worker := &repository.Worker{
			Key:       "atomic-worker-1",
			SessionID: session.ID,
			Payer:     payer,
			CreatedAt: time.Now(),
			TraceID:   "trace-atomic-1",
		}

		quota, err := db.WorkerCreateAndReserveQuota(ctx, worker, 3)
		require.NoError(t, err)
		require.NotNil(t, quota)
		assert.Equal(t, 1, quota.ActiveWorkers)

		retrievedWorker, err := db.WorkerGet(ctx, worker.Key)
		require.NoError(t, err)
		assert.Equal(t, worker.Key, retrievedWorker.Key)

		currentQuota, err := db.QuotaGet(ctx, payer)
		require.NoError(t, err)
		assert.Equal(t, 1, currentQuota.ActiveWorkers)
	})
}

func TestWorkerCreateAndReserveQuota_RollsBackOnWorkerCreateFailure(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()

		pricingConfig := sds.PricingConfig{
			PricePerBlock: sds.MustNewGRT(100),
			PricePerByte:  sds.MustNewGRT(10),
		}

		payer := eth.MustNewAddress("0x1234567890123456789012345678901234567890")
		receiver := eth.MustNewAddress("0x2234567890123456789012345678901234567890")
		dataService := eth.MustNewAddress("0x3234567890123456789012345678901234567890")

		session := repository.NewSession("atomic-worker-session-dup", payer, receiver, dataService, pricingConfig)
		require.NoError(t, db.SessionCreate(ctx, session))

		worker := &repository.Worker{
			Key:       "atomic-worker-dup",
			SessionID: session.ID,
			Payer:     payer,
			CreatedAt: time.Now(),
		}

		quota, err := db.WorkerCreateAndReserveQuota(ctx, worker, 3)
		require.NoError(t, err)
		require.NotNil(t, quota)
		assert.Equal(t, 1, quota.ActiveWorkers)

		quota, err = db.WorkerCreateAndReserveQuota(ctx, worker, 3)
		require.Error(t, err)
		require.Nil(t, quota)

		currentQuota, err := db.QuotaGet(ctx, payer)
		require.NoError(t, err)
		assert.Equal(t, 1, currentQuota.ActiveWorkers)
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

func TestWorkerCountBySession(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()

		pricingConfig := sds.PricingConfig{
			PricePerBlock: sds.MustNewGRT(100),
			PricePerByte:  sds.MustNewGRT(10),
		}

		payer := eth.MustNewAddress("0x1234567890123456789012345678901234567890")
		receiver := eth.MustNewAddress("0x2234567890123456789012345678901234567890")
		dataService := eth.MustNewAddress("0x3234567890123456789012345678901234567890")

		session1 := repository.NewSession("worker-count-session-1", payer, receiver, dataService, pricingConfig)
		session2 := repository.NewSession("worker-count-session-2", payer, receiver, dataService, pricingConfig)
		require.NoError(t, db.SessionCreate(ctx, session1))
		require.NoError(t, db.SessionCreate(ctx, session2))

		require.NoError(t, db.WorkerCreate(ctx, &repository.Worker{Key: "worker-count-1", SessionID: session1.ID, Payer: payer, CreatedAt: time.Now()}))
		require.NoError(t, db.WorkerCreate(ctx, &repository.Worker{Key: "worker-count-2", SessionID: session1.ID, Payer: payer, CreatedAt: time.Now()}))
		require.NoError(t, db.WorkerCreate(ctx, &repository.Worker{Key: "worker-count-3", SessionID: session2.ID, Payer: payer, CreatedAt: time.Now()}))

		count, err := db.WorkerCountBySession(ctx, session1.ID)
		require.NoError(t, err)
		assert.Equal(t, 2, count)

		count, err = db.WorkerCountBySession(ctx, "missing")
		require.NoError(t, err)
		assert.Equal(t, 0, count)
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

func TestQuotaReserve(t *testing.T) {
	withTestDB(t, func(db *Database) {
		ctx := context.Background()

		payer := eth.MustNewAddress("0x1234567890123456789012345678901234567890")

		quota, err := db.QuotaReserve(ctx, payer, 3, 1)
		require.NoError(t, err)
		assert.Equal(t, 0, quota.ActiveSessions)
		assert.Equal(t, 1, quota.ActiveWorkers)

		quota, err = db.QuotaReserve(ctx, payer, 3, 2)
		require.NoError(t, err)
		assert.Equal(t, 0, quota.ActiveSessions)
		assert.Equal(t, 3, quota.ActiveWorkers)

		quota, err = db.QuotaReserve(ctx, payer, 3, 1)
		require.ErrorIs(t, err, repository.ErrQuotaExceeded)
		assert.Equal(t, 0, quota.ActiveSessions)
		assert.Equal(t, 3, quota.ActiveWorkers)

		current, err := db.QuotaGet(ctx, payer)
		require.NoError(t, err)
		assert.Equal(t, 0, current.ActiveSessions)
		assert.Equal(t, 3, current.ActiveWorkers)
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
