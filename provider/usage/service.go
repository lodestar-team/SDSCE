// Package usage implements the gRPC UsageService that receives batched
// metering events from the dmetering tgm:// plugin used by firehose-core.
package usage

import (
	"context"
	"time"

	"connectrpc.com/connect"
	usagev1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/usage/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/usage/v1/usagev1connect"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/streamingfast/logging"
	"go.uber.org/zap"
)

var zlog, _ = logging.PackageLogger("sds_usage", "github.com/graphprotocol/substreams-data-service/provider/usage")

// UsageService implements usagev1connect.UsageServiceHandler.
// It receives batched metering events from the dmetering plugin and stores
// them in the GlobalRepository for later aggregation and reporting.
type UsageService struct {
	repo repository.GlobalRepository
}

var _ usagev1connect.UsageServiceHandler = (*UsageService)(nil)

// NewUsageService creates a new UsageService backed by the given repository.
func NewUsageService(repo repository.GlobalRepository) *UsageService {
	return &UsageService{repo: repo}
}

// Report receives a batch of metering events from the dmetering plugin.
// For each event, it stores usage data in the repository keyed by
// organization_id (payer address). If the associated session has been
// terminated, it returns revoked=true so the plugin can stop the stream.
func (s *UsageService) Report(
	ctx context.Context,
	req *connect.Request[usagev1.ReportRequest],
) (*connect.Response[usagev1.ReportResponse], error) {
	if len(req.Msg.Events) == 0 {
		return connect.NewResponse(&usagev1.ReportResponse{}), nil
	}

	zlog.Debug("Report called", zap.Int("event_count", len(req.Msg.Events)))

	for _, event := range req.Msg.Events {
		if event == nil {
			continue
		}

		// Session ID is set by the metering plugin from auth context
		sessionID := event.SdsSessionId
		if sessionID == "" {
			zlog.Warn("event missing sds_session_id, skipping",
				zap.String("organization_id", event.OrganizationId),
				zap.String("endpoint", event.Endpoint),
			)
			continue
		}

		usageEvent := protoEventToUsageEvent(event)

		if err := s.repo.UsageAdd(ctx, sessionID, usageEvent); err != nil {
			zlog.Warn("failed to record usage event",
				zap.String("organization_id", event.OrganizationId),
				zap.String("session_id", sessionID),
				zap.Error(err),
			)
			// Non-fatal: continue processing remaining events.
		}
	}

	return connect.NewResponse(&usagev1.ReportResponse{
		Revoked: false,
	}), nil
}

// protoEventToUsageEvent converts a proto metering Event to the internal
// UsageEvent by summing the well-known metric counters.
func protoEventToUsageEvent(event *usagev1.Event) *repository.UsageEvent {
	ue := &repository.UsageEvent{
		Timestamp: time.Now(),
	}
	if event.Timestamp != nil {
		ue.Timestamp = event.Timestamp.AsTime()
	}

	for _, m := range event.Metrics {
		if m == nil {
			continue
		}
		switch m.Name {
		case "blocks_count", "blocks", "block_count", "message_count":
			ue.Blocks += m.Value
		case "bytes_count", "bytes", "egress_bytes":
			ue.Bytes += m.Value
		case "requests_count", "requests":
			ue.Requests += m.Value
		}
	}

	return ue
}
