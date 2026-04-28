package plugin

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sync"
	"time"

	"connectrpc.com/connect"
	sds "github.com/graphprotocol/substreams-data-service"
	usagev1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/usage/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/usage/v1/usagev1connect"
	"github.com/streamingfast/dauth"
	"github.com/streamingfast/dmetering"
	"github.com/streamingfast/shutter"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// RegisterMetering registers the "sds" scheme with dmetering.
// The config URL format is:
//
//	sds://host:port?plaintext=true&insecure=true&network=<network>&buffer=<size>&delay=<ms>&report-timeout=<duration>
//
// The plugin connects to the provider gateway's UsageService for metering.
func RegisterMetering() {
	dmetering.Register("sds", func(config string, logger *zap.Logger) (dmetering.EventEmitter, error) {
		configExpanded := os.ExpandEnv(config)

		baseCfg, vals, err := parseBaseConfig(configExpanded)
		if err != nil {
			return nil, fmt.Errorf("failed to parse metering config %q: %w", config, err)
		}

		// Validate known parameters
		for k := range vals {
			switch k {
			case "insecure", "plaintext", "network", "buffer", "delay", "report-timeout", "panic-on-drop", "panicOnDrop":
				// Known parameters
			default:
				return nil, fmt.Errorf("unknown query parameter: %s", k)
			}
		}

		network := vals.Get("network")
		if network == "" {
			return nil, fmt.Errorf("network is required (as query param)")
		}

		bufferSize, err := parseUint64(vals.Get("buffer"), 10000)
		if err != nil {
			return nil, fmt.Errorf("invalid buffer: %w", err)
		}

		delay, err := parseInt64(vals.Get("delay"), 100)
		if err != nil {
			return nil, fmt.Errorf("invalid delay: %w", err)
		}

		reportTimeout, err := parseMeteringReportTimeout(vals)
		if err != nil {
			return nil, fmt.Errorf("invalid report-timeout: %w", err)
		}

		panicOnDrop := vals.Get("panicOnDrop") == "true" || vals.Get("panic-on-drop") == "true"

		return newMeteringEmitter(
			baseCfg,
			network,
			bufferSize,
			time.Duration(delay)*time.Millisecond,
			reportTimeout,
			panicOnDrop,
			logger,
		)
	})
}

type emittedEvent struct {
	dmetering.Event

	// SessionID is the SDS session ID associated with this event, set by the dmetering middleware in firehose-core from the auth context.
	SessionID string
}

// meteringEmitter implements dmetering.EventEmitter by calling the provider gateway.
type meteringEmitter struct {
	*shutter.Shutter
	mu            sync.Mutex
	client        usagev1connect.UsageServiceClient
	network       string
	buffer        chan emittedEvent
	activeBatch   []*usagev1.Event
	done          chan struct{}
	shuttingDown  bool
	panicOnDrop   bool
	delay         time.Duration
	reportTimeout time.Duration
	logger        *zap.Logger
}

const defaultMeteringReportTimeout = 30 * time.Second

func parseMeteringReportTimeout(vals url.Values) (time.Duration, error) {
	return parseDuration(vals.Get("report-timeout"), defaultMeteringReportTimeout)
}

func newMeteringEmitter(cfg *baseConfig, network string, bufferSize uint64, delay, reportTimeout time.Duration, panicOnDrop bool, logger *zap.Logger) (dmetering.EventEmitter, error) {
	httpClient := newHTTPClient(cfg)

	if reportTimeout <= 0 {
		reportTimeout = defaultMeteringReportTimeout
	}

	client := usagev1connect.NewUsageServiceClient(
		httpClient,
		cfg.baseURL(),
	)

	e := &meteringEmitter{
		Shutter:       shutter.New(),
		client:        client,
		network:       network,
		buffer:        make(chan emittedEvent, bufferSize),
		done:          make(chan struct{}),
		panicOnDrop:   panicOnDrop,
		delay:         delay,
		reportTimeout: reportTimeout,
		logger:        logger.Named("sds-metering"),
	}

	e.OnTerminating(func(err error) {
		e.logger.Info("received shutdown signal, waiting for launch loop to end", zap.Error(err))
		<-e.done
	})

	go e.launch()

	return e, nil
}

func (e *meteringEmitter) launch() {
	defer close(e.done)

	ticker := time.NewTicker(e.delay)
	defer ticker.Stop()

	for {
		select {
		case <-e.Terminating():
			e.beginShutdown()
			e.flushAndClose()
			return
		case <-ticker.C:
			e.emit(e.activeBatch)
			e.activeBatch = nil
		case ev := <-e.buffer:
			ev.Network = e.network
			e.activeBatch = append(e.activeBatch, e.eventToProto(ev))
		}
	}
}

func (e *meteringEmitter) beginShutdown() {
	e.mu.Lock()
	e.shuttingDown = true
	e.mu.Unlock()
}

func (e *meteringEmitter) flushAndClose() {
	t0 := time.Now()
	e.logger.Info("waiting for event flush to complete", zap.Int("count", len(e.buffer)))
	defer func() {
		e.logger.Info("event flushed", zap.Duration("elapsed", time.Since(t0)))
	}()

	for {
		select {
		case ev := <-e.buffer:
			ev.Network = e.network
			e.activeBatch = append(e.activeBatch, e.eventToProto(ev))
		default:
			e.logger.Info("sending last events", zap.Int("count", len(e.activeBatch)))
			e.emit(e.activeBatch)
			return
		}
	}
}

// Emit implements dmetering.EventEmitter.
func (e *meteringEmitter) Emit(ctx context.Context, ev dmetering.Event) {
	if ev.Endpoint == "" {
		e.logger.Warn("events must contain endpoint, dropping event", zap.Object("event", ev))
		return
	}

	if e.IsTerminating() {
		e.logger.Warn("emitter is shutting down cannot track event", zap.Object("event", ev))
		return
	}

	trustedHeaders := dauth.FromContext(ctx)

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.shuttingDown {
		e.logger.Debug("emitter is shutting down, dropping event", zap.Object("event", ev))
		return
	}

	select {
	case e.buffer <- emittedEvent{Event: ev, SessionID: trustedHeaders.Get(sds.HeaderSessionID)}:
	default:
		if e.panicOnDrop {
			panic(fmt.Errorf("failed to queue metric channel is full"))
		}
		e.logger.Warn("dropping event, buffer full")
	}
}

func (e *meteringEmitter) emit(events []*usagev1.Event) {
	if len(events) == 0 {
		return
	}
	e.logger.Debug("tracking events", zap.Int("count", len(events)))

	req := connect.NewRequest(&usagev1.ReportRequest{
		Events: events,
	})

	// Note: We don't have a context with trusted headers here since metering
	// events are collected asynchronously in a background goroutine.
	// The usage service will read the session ID from each event's Meta field,
	// which is populated by the dmetering middleware in firehose-core.

	ctx := context.Background()
	if e.reportTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.reportTimeout)
		defer cancel()
	}

	_, err := e.client.Report(ctx, req)
	if err != nil {
		e.logger.Warn("failed to emit events", zap.Error(err))
	}
}

// eventToProto converts a dmetering.Event to our usagev1.Event.
func (e *meteringEmitter) eventToProto(ev emittedEvent) *usagev1.Event {
	protoEvent := &usagev1.Event{
		OrganizationId: ev.OrganizationID,
		ApiKeyId:       ev.ApiKeyID,
		Endpoint:       ev.Endpoint,
		Network:        ev.Network,
		SdsSessionId:   ev.SessionID,
		Timestamp:      timestamppb.New(ev.Timestamp),
		Metrics:        make([]*usagev1.Metric, 0, len(ev.Metrics)),
	}

	for name, value := range ev.Metrics {
		protoEvent.Metrics = append(protoEvent.Metrics, &usagev1.Metric{
			Name:  name,
			Value: int64(value),
		})
	}

	return protoEvent
}
