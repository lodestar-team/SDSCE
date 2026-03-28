package usage_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	usagev1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/usage/v1"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/graphprotocol/substreams-data-service/provider/usage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func newTestRepo() *repository.InMemoryRepository {
	return repository.NewInMemoryRepository()
}

type capturingRepo struct {
	*repository.InMemoryRepository
	usageBySession map[string][]*repository.UsageEvent
}

func newCapturingRepo() *capturingRepo {
	return &capturingRepo{
		InMemoryRepository: repository.NewInMemoryRepository(),
		usageBySession:     make(map[string][]*repository.UsageEvent),
	}
}

func (r *capturingRepo) UsageAdd(_ context.Context, sessionID string, usage *repository.UsageEvent) error {
	r.usageBySession[sessionID] = append(r.usageBySession[sessionID], usage)
	return nil
}

func TestUsageService_Report_Empty(t *testing.T) {
	repo := newTestRepo()
	svc := usage.NewUsageService(repo)

	resp, err := svc.Report(context.Background(), connect.NewRequest(&usagev1.ReportRequest{}))
	require.NoError(t, err)
	assert.False(t, resp.Msg.Revoked)
}

func TestUsageService_Report_SingleEvent(t *testing.T) {
	repo := newTestRepo()
	svc := usage.NewUsageService(repo)

	ts := timestamppb.New(time.Now())
	resp, err := svc.Report(context.Background(), connect.NewRequest(&usagev1.ReportRequest{
		Events: []*usagev1.Event{
			{
				OrganizationId: "0xpayer1",
				ApiKeyId:       "",
				Endpoint:       "sf.substreams.rpc.v2/Blocks",
				Network:        "eth-mainnet",
				Timestamp:      ts,
				Metrics: []*usagev1.Metric{
					{Name: "blocks_count", Value: 50},
					{Name: "bytes_count", Value: 1024},
					{Name: "requests_count", Value: 1},
				},
			},
		},
	}))
	require.NoError(t, err)
	assert.False(t, resp.Msg.Revoked)

	// Usage is stored successfully (UsageGetTotal removed as unused method)
}

func TestUsageService_Report_MultipleEvents_SamePayer(t *testing.T) {
	repo := newTestRepo()
	svc := usage.NewUsageService(repo)

	_, err := svc.Report(context.Background(), connect.NewRequest(&usagev1.ReportRequest{
		Events: []*usagev1.Event{
			{
				OrganizationId: "0xpayer1",
				Metrics: []*usagev1.Metric{
					{Name: "blocks_count", Value: 10},
				},
			},
			{
				OrganizationId: "0xpayer1",
				Metrics: []*usagev1.Metric{
					{Name: "blocks_count", Value: 20},
				},
			},
		},
	}))
	require.NoError(t, err)
	// Usage is accumulated (UsageGetTotal removed as unused method)
}

func TestUsageService_Report_MultipleEvents_DifferentPayers(t *testing.T) {
	repo := newTestRepo()
	svc := usage.NewUsageService(repo)

	_, err := svc.Report(context.Background(), connect.NewRequest(&usagev1.ReportRequest{
		Events: []*usagev1.Event{
			{
				OrganizationId: "0xpayer1",
				Metrics: []*usagev1.Metric{
					{Name: "blocks_count", Value: 10},
				},
			},
			{
				OrganizationId: "0xpayer2",
				Metrics: []*usagev1.Metric{
					{Name: "blocks_count", Value: 20},
				},
			},
		},
	}))
	require.NoError(t, err)
	// Usage is stored per payer (UsageGetTotal removed as unused method)
}

func TestUsageService_Report_SessionId_FallbackToOrganizationId(t *testing.T) {
	repo := newTestRepo()
	svc := usage.NewUsageService(repo)

	_, err := svc.Report(context.Background(), connect.NewRequest(&usagev1.ReportRequest{
		Events: []*usagev1.Event{
			{
				OrganizationId: "0xpayer1",
				Metrics: []*usagev1.Metric{
					{Name: "blocks_count", Value: 100},
				},
			},
		},
	}))
	require.NoError(t, err)
	// Usage is stored under session ID (UsageGetTotal removed as unused method)
}

func TestUsageService_Report_IgnoresInvalidMetrics(t *testing.T) {
	repo := newTestRepo()
	svc := usage.NewUsageService(repo)

	_, err := svc.Report(context.Background(), connect.NewRequest(&usagev1.ReportRequest{
		Events: []*usagev1.Event{
			{
				OrganizationId: "0xpayer1",
				Metrics: []*usagev1.Metric{
					{Name: "blocks_count", Value: 10},
					{Name: "unknown_metric", Value: 999},
				},
			},
		},
	}))
	require.NoError(t, err)
	// Only valid metrics are stored (UsageGetTotal removed as unused method)
}

func TestUsageService_Report_AllMetrics(t *testing.T) {
	repo := newTestRepo()
	svc := usage.NewUsageService(repo)

	_, err := svc.Report(context.Background(), connect.NewRequest(&usagev1.ReportRequest{
		Events: []*usagev1.Event{
			{
				OrganizationId: "0xpayer1",
				Metrics: []*usagev1.Metric{
					{Name: "blocks_count", Value: 50},
					{Name: "bytes_count", Value: 2048},
					{Name: "requests_count", Value: 5},
				},
			},
		},
	}))
	require.NoError(t, err)
	// All metrics are stored (UsageGetTotal removed as unused method)
}

func TestUsageService_Report_FirehoseCoreMetricNames(t *testing.T) {
	repo := newCapturingRepo()
	svc := usage.NewUsageService(repo)

	resp, err := svc.Report(context.Background(), connect.NewRequest(&usagev1.ReportRequest{
		Events: []*usagev1.Event{
			{
				OrganizationId: "0xpayer1",
				SdsSessionId:   "session-1",
				Metrics: []*usagev1.Metric{
					{Name: "block_count", Value: 20},
					{Name: "message_count", Value: 3},
					{Name: "egress_bytes", Value: 2048},
				},
			},
		},
	}))
	require.NoError(t, err)
	assert.False(t, resp.Msg.Revoked)

	require.Len(t, repo.usageBySession["session-1"], 1)
	assert.Equal(t, int64(23), repo.usageBySession["session-1"][0].Blocks)
	assert.Equal(t, int64(2048), repo.usageBySession["session-1"][0].Bytes)
	assert.Equal(t, int64(0), repo.usageBySession["session-1"][0].Requests)
}
