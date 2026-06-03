package main

import (
	"context"
	"fmt"
	"math/big"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	chainclient "github.com/graphprotocol/substreams-data-service/contracts/chain"
	horizoncontracts "github.com/graphprotocol/substreams-data-service/contracts/horizon"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	providerv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/provider/v1"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
)

const (
	collectDaemonListLimit  = 500
	collectDaemonMaxBackoff = time.Hour
)

var providerOperatorCollectDaemonCmd = Command(
	runProviderOperatorCollectDaemon,
	"collect-daemon",
	"Continuously collect provider settlements (automated collection)",
	Description(`
		Runs a background settlement loop that discovers collectible RAVs through
		the provider operator API and submits SubstreamsDataService.collect()
		transactions automatically, replacing the manual one-shot 'collect' command.

		The daemon holds the provider settlement key (like the manual collect
		command) and talks to the operator API; it is intended to run as a separate
		process from the provider gateway so settlement keys are never mounted into
		the gateway deployment.

		Each sweep collects records in the 'collectible' state and retries records
		in 'collect_failed_retryable' with exponential backoff (up to --max-attempts).
		Records below --min-collect-value-wei are skipped so dust does not burn gas.
	`),
	Flags(func(flags *pflag.FlagSet) {
		addProviderOperatorFlags(flags)
		addRPCFlags(flags)
		addProviderCollectTxFlags(flags)
		flags.String("data-service-address", "", "SubstreamsDataService contract address (required)")
		flags.Uint64("data-service-cut-ppm", 0, "Data service cut in parts per million (required, 0 to 1000000)")
		flags.Duration("poll-interval", 30*time.Second, "Interval between collection sweeps")
		flags.Uint32("max-attempts", 5, "Maximum attempts before a retryable record is skipped")
		flags.Duration("retry-backoff-base", time.Minute, "Base backoff before retrying a failed record (doubles per attempt, capped at 1h)")
		flags.String("min-collect-value-wei", "0", "Skip collection records whose value is below this amount in wei")
		flags.Duration("reclaim-pending-after", 0, "Re-attempt collections stuck in collect_pending longer than this, e.g. after a crash mid-collect (0 disables; set comfortably above --receipt-timeout)")
		flags.Bool("once", false, "Run a single sweep and exit")
	}),
)

func runProviderOperatorCollectDaemon(cmd *cobra.Command, args []string) error {
	dataServiceAddress := parseAddressFlag(cmd, "data-service-address")
	cutPPM := parseDataServiceCutPPM(cmd)
	pollInterval := sflags.MustGetDuration(cmd, "poll-interval")
	cli.Ensure(pollInterval > 0, "--poll-interval must be greater than 0")
	cli.Ensure(!sflags.MustGetBool(cmd, "dry-run"), "--dry-run is not supported by collect-daemon")
	cli.Ensure(!sflags.MustGetBool(cmd, "no-wait"), "--no-wait is not supported by collect-daemon; it must wait for receipts to mark collections collected")
	once := sflags.MustGetBool(cmd, "once")
	minValue := parseMinCollectValueWei(cmd)

	providerKey := parseKeyPair(cmd, "provider")
	txOpts := txOptionsFromFlags(cmd, providerKey)
	txOpts.NoWait = false

	collector := &autoCollector{
		operatorClient:     providerOperatorClientFromFlags(cmd),
		dataService:        horizoncontracts.MustNewDataService(),
		dataServiceAddress: dataServiceAddress,
		providerAddress:    providerKey.Address,
		cutPPM:             cutPPM,
		txOpts:             txOpts,
		minValue:           minValue,
		maxAttempts:        sflags.MustGetUint32(cmd, "max-attempts"),
		backoffBase:        sflags.MustGetDuration(cmd, "retry-backoff-base"),
		reclaimAfter:       sflags.MustGetDuration(cmd, "reclaim-pending-after"),
		opTimeout:          sflags.MustGetDuration(cmd, "timeout"),
		txTimeout:          sflags.MustGetDuration(cmd, "rpc-timeout") + sflags.MustGetDuration(cmd, "receipt-timeout"),
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	rpcEndpoint := requiredStringFlag(cmd, "rpc-endpoint")
	dialCtx, dialCancel := context.WithTimeout(ctx, sflags.MustGetDuration(cmd, "rpc-timeout"))
	rpcClient, err := chainclient.DialContext(dialCtx, rpcEndpoint)
	dialCancel()
	if err != nil {
		return fmt.Errorf("dialing RPC endpoint: %w", err)
	}
	defer rpcClient.Close()
	collector.rpcClient = rpcClient

	logDaemon("auto-collector started: data_service=%s cut_ppm=%d poll=%s max_attempts=%d",
		dataServiceAddress.Hex(), cutPPM, pollInterval, collector.maxAttempts)

	for {
		collector.sweep(ctx)
		if once {
			return nil
		}
		select {
		case <-ctx.Done():
			logDaemon("shutdown signal received, exiting")
			return nil
		case <-time.After(pollInterval):
		}
	}
}

// autoCollector wraps the manual collect logic in a poll-and-retry loop.
type autoCollector struct {
	operatorClient     providerOperatorClient
	rpcClient          *chainclient.Client
	dataService        *horizoncontracts.DataService
	dataServiceAddress common.Address
	providerAddress    common.Address
	cutPPM             uint64
	txOpts             chainclient.TxOptions
	minValue           *big.Int
	maxAttempts        uint32
	backoffBase        time.Duration
	reclaimAfter       time.Duration
	opTimeout          time.Duration
	txTimeout          time.Duration
}

func (c *autoCollector) sweep(ctx context.Context) {
	records, err := c.listEligible(ctx)
	if err != nil {
		if ctx.Err() == nil {
			logDaemon("sweep: list collections failed: %v", err)
		}
		return
	}
	if len(records) == 0 {
		return
	}
	logDaemon("sweep: %d eligible collection(s)", len(records))
	for _, record := range records {
		if ctx.Err() != nil {
			return
		}
		c.collectOne(ctx, record)
	}

	c.reclaimStalePending(ctx)
}

// reclaimStalePending bounces collect_pending records that have been stuck longer
// than reclaimAfter (e.g. the daemon crashed between marking pending and
// confirming the receipt) back to retryable, so the next sweep re-attempts them.
// Re-attempting is safe: collect() on an already-settled RAV collects a zero
// delta, so a duplicate attempt is a no-op rather than a double charge.
func (c *autoCollector) reclaimStalePending(ctx context.Context) {
	if c.reclaimAfter <= 0 {
		return
	}

	listCtx, cancel := context.WithTimeout(ctx, c.opTimeout)
	resp, err := c.operatorClient.client.ListCollections(listCtx, providerOperatorRequest(c.operatorClient, &providerv1.ListCollectionsRequest{
		Limit: collectDaemonListLimit,
		State: providerv1.CollectionState_COLLECTION_STATE_COLLECT_PENDING,
	}))
	cancel()
	if err != nil {
		if ctx.Err() == nil {
			logDaemon("reclaim: list pending failed: %v", err)
		}
		return
	}

	now := time.Now()
	for _, record := range resp.Msg.GetCollections() {
		if ctx.Err() != nil {
			return
		}
		if !c.shouldReclaimPending(record, now) {
			continue
		}
		id := collectionRecordID(record)
		markCtx, markCancel := context.WithTimeout(ctx, c.opTimeout)
		err := markCollectionRetryable(markCtx, c.operatorClient, record, record.GetLastTxHash(),
			fmt.Errorf("reclaimed stale collect_pending after %s", c.reclaimAfter))
		markCancel()
		if err != nil {
			logDaemon("reclaim %s: mark retryable failed: %v", id, err)
			continue
		}
		logDaemon("reclaim %s: stale collect_pending -> retryable (re-attempt next sweep)", id)
	}
}

func (c *autoCollector) shouldReclaimPending(record *providerv1.CollectionRecord, now time.Time) bool {
	if c.reclaimAfter <= 0 {
		return false
	}
	if record.GetState() != providerv1.CollectionState_COLLECTION_STATE_COLLECT_PENDING {
		return false
	}
	updatedAt := time.Unix(0, int64(record.GetUpdatedAtNs()))
	return now.Sub(updatedAt) >= c.reclaimAfter
}

func (c *autoCollector) listEligible(ctx context.Context) ([]*providerv1.CollectionRecord, error) {
	states := []providerv1.CollectionState{
		providerv1.CollectionState_COLLECTION_STATE_COLLECTIBLE,
		providerv1.CollectionState_COLLECTION_STATE_COLLECT_FAILED_RETRYABLE,
	}

	var eligible []*providerv1.CollectionRecord
	for _, state := range states {
		callCtx, cancel := context.WithTimeout(ctx, c.opTimeout)
		resp, err := c.operatorClient.client.ListCollections(callCtx, providerOperatorRequest(c.operatorClient, &providerv1.ListCollectionsRequest{
			Limit: collectDaemonListLimit,
			State: state,
		}))
		cancel()
		if err != nil {
			return nil, err
		}
		for _, record := range resp.Msg.GetCollections() {
			if c.shouldCollect(record) {
				eligible = append(eligible, record)
			}
		}
	}
	return eligible, nil
}

// shouldCollect filters by value threshold and, for retryable records, by attempt
// count and exponential backoff so failures are not hammered every sweep.
func (c *autoCollector) shouldCollect(record *providerv1.CollectionRecord) bool {
	value := protoGRTBigInt(record.GetValueAggregate())
	if value == nil || value.Cmp(c.minValue) < 0 {
		return false
	}
	if record.GetState() == providerv1.CollectionState_COLLECTION_STATE_COLLECT_FAILED_RETRYABLE {
		if uint32(record.GetAttemptCount()) >= c.maxAttempts {
			return false
		}
		updatedAt := time.Unix(0, int64(record.GetUpdatedAtNs()))
		if time.Since(updatedAt) < c.backoffFor(record.GetAttemptCount()) {
			return false
		}
	}
	return true
}

func (c *autoCollector) backoffFor(attempt uint64) time.Duration {
	if attempt < 1 {
		return 0
	}
	backoff := c.backoffBase
	for i := uint64(1); i < attempt; i++ {
		backoff *= 2
		if backoff >= collectDaemonMaxBackoff {
			return collectDaemonMaxBackoff
		}
	}
	return backoff
}

func (c *autoCollector) collectOne(ctx context.Context, record *providerv1.CollectionRecord) {
	id := collectionRecordID(record)

	signedRAV, err := validateProviderCollectRecord(record, c.providerAddress, c.dataServiceAddress)
	if err != nil {
		logDaemon("skip %s: invalid record: %v", id, err)
		return
	}
	calldata, err := c.dataService.PackQueryFeeCollect(signedRAV, c.cutPPM)
	if err != nil {
		logDaemon("skip %s: encoding collect calldata: %v", id, err)
		return
	}

	pendingCtx, pendingCancel := context.WithTimeout(ctx, c.opTimeout)
	_, err = markCollectionPending(pendingCtx, c.operatorClient, record)
	pendingCancel()
	if err != nil {
		logDaemon("skip %s: mark pending: %v", id, err)
		return
	}

	txCtx, txCancel := context.WithTimeout(ctx, c.txTimeout)
	result, txErr := c.rpcClient.SendDynamicFeeTransaction(txCtx, c.dataServiceAddress, big.NewInt(0), calldata, c.txOpts)
	txCancel()

	// Bookkeeping must complete even during shutdown: once a transaction is in
	// flight the record state must reflect it, so these marks use a detached
	// context rather than the (possibly cancelled) sweep context.
	if txErr != nil {
		markCtx, markCancel := context.WithTimeout(context.Background(), c.opTimeout)
		markErr := markCollectionRetryable(markCtx, c.operatorClient, record, resultTxHash(result), txErr)
		markCancel()
		if markErr != nil {
			logDaemon("collect %s failed: %v; additionally failed to mark retryable: %v", id, txErr, markErr)
			return
		}
		logDaemon("collect %s failed (attempt %d): %v -> retryable", id, record.GetAttemptCount()+1, txErr)
		return
	}

	markCtx, markCancel := context.WithTimeout(context.Background(), c.opTimeout)
	_, markErr := markCollectionCollected(markCtx, c.operatorClient, record, resultTxHash(result))
	markCancel()
	if markErr != nil {
		logDaemon("collect %s: tx %s succeeded but mark collected failed: %v", id, resultTxHash(result), markErr)
		return
	}
	logDaemon("collected %s: value=%s tx=%s", id, formatProtoGRT(record.GetValueAggregate()), resultTxHash(result))
}

func parseMinCollectValueWei(cmd *cobra.Command) *big.Int {
	raw := optionalStringFlag(cmd, "min-collect-value-wei")
	if raw == "" {
		return big.NewInt(0)
	}
	value, ok := new(big.Int).SetString(raw, 10)
	cli.Ensure(ok, "invalid --min-collect-value-wei %q, expected a base-10 integer in wei", raw)
	cli.Ensure(value.Sign() >= 0, "--min-collect-value-wei must not be negative")
	return value
}

func protoGRTBigInt(value *commonv1.GRT) *big.Int {
	if value == nil {
		return nil
	}
	return value.ToNative().BigInt()
}

func collectionRecordID(record *providerv1.CollectionRecord) string {
	key := record.GetKey()
	id := formatCollectionID(key.GetCollectionId())
	if session := key.GetSessionId(); session != "" {
		return id + " (session " + session + ")"
	}
	return id
}

func logDaemon(format string, args ...any) {
	prefixed := append([]any{time.Now().UTC().Format(time.RFC3339)}, args...)
	fmt.Printf("%s "+format+"\n", prefixed...)
}
