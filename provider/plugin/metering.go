package plugin

import (
	"context"
	"fmt"
	"os"
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
//	sds://host:port?plaintext=true&insecure=true&network=<network>&buffer=<size>&delay=<ms>
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
			case "insecure", "plaintext", "network", "buffer", "delay", "panic-on-drop", "panicOnDrop":
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

		panicOnDrop := vals.Get("panicOnDrop") == "true" || vals.Get("panic-on-drop") == "true"

		return newMeteringEmitter(baseCfg, network, bufferSize, time.Duration(delay)*time.Millisecond, panicOnDrop, logger)
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
	client      usagev1connect.UsageServiceClient
	network     string
	buffer      chan emittedEvent
	activeBatch []*usagev1.Event
	done        chan bool
	panicOnDrop bool
	delay       time.Duration
	logger      *zap.Logger
}

func newMeteringEmitter(cfg *baseConfig, network string, bufferSize uint64, delay time.Duration, panicOnDrop bool, logger *zap.Logger) (dmetering.EventEmitter, error) {
	httpClient := newHTTPClient(cfg)

	client := usagev1connect.NewUsageServiceClient(
		httpClient,
		cfg.baseURL(),
	)

	e := &meteringEmitter{
		Shutter:     shutter.New(),
		client:      client,
		network:     network,
		buffer:      make(chan emittedEvent, bufferSize),
		done:        make(chan bool, 1),
		panicOnDrop: panicOnDrop,
		delay:       delay,
		logger:      logger.Named("sds-metering"),
	}

	e.OnTerminating(func(err error) {
		e.logger.Info("received shutdown signal, waiting for launch loop to end", zap.Error(err))
		<-e.done
		e.flushAndClose()
	})

	go e.launch()

	return e, nil
}

func (e *meteringEmitter) launch() {
	ticker := time.NewTicker(e.delay)
	for {
		select {
		case <-e.Terminating():
			e.done <- true
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

func (e *meteringEmitter) flushAndClose() {
	close(e.buffer)

	t0 := time.Now()
	e.logger.Info("waiting for event flush to complete", zap.Int("count", len(e.buffer)))
	defer func() {
		e.logger.Info("event flushed", zap.Duration("elapsed", time.Since(t0)))
	}()

	for {
		ev, ok := <-e.buffer
		if !ok {
			e.logger.Info("sending last events", zap.Int("count", len(e.activeBatch)))
			e.emit(e.activeBatch)
			return
		}
		ev.Network = e.network
		e.activeBatch = append(e.activeBatch, e.eventToProto(ev))
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

	_, err := e.client.Report(context.Background(), req)
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
