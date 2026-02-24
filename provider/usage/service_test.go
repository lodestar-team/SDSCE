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

	// Usage should have been stored under organization_id (since api_key_id is empty).
	total, err := repo.UsageGetTotal(context.Background(), "0xpayer1")
	require.NoError(t, err)
	assert.Equal(t, int64(50), total.TotalBlocks)
	assert.Equal(t, int64(1024), total.TotalBytes)
	assert.Equal(t, int64(1), total.TotalRequests)
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

	total, err := repo.UsageGetTotal(context.Background(), "0xpayer1")
	require.NoError(t, err)
	assert.Equal(t, int64(30), total.TotalBlocks)
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
					{Name: "blocks_count", Value: 30},
				},
			},
		},
	}))
	require.NoError(t, err)

	total1, err := repo.UsageGetTotal(context.Background(), "0xpayer1")
	require.NoError(t, err)
	assert.Equal(t, int64(10), total1.TotalBlocks)

	total2, err := repo.UsageGetTotal(context.Background(), "0xpayer2")
	require.NoError(t, err)
	assert.Equal(t, int64(30), total2.TotalBlocks)
}

func TestUsageService_Report_ApiKeyIdAsSessionID(t *testing.T) {
	// When api_key_id is set it is used as the session key for usage aggregation.
	repo := newTestRepo()
	svc := usage.NewUsageService(repo)

	_, err := svc.Report(context.Background(), connect.NewRequest(&usagev1.ReportRequest{
		Events: []*usagev1.Event{
			{
				OrganizationId: "0xpayer1",
				ApiKeyId:       "session-abc123",
				Metrics: []*usagev1.Metric{
					{Name: "bytes_count", Value: 512},
				},
			},
		},
	}))
	require.NoError(t, err)

	// Should be stored under api_key_id, not organization_id.
	total, err := repo.UsageGetTotal(context.Background(), "session-abc123")
	require.NoError(t, err)
	assert.Equal(t, int64(512), total.TotalBytes)

	// Nothing stored under organization_id.
	totalPayer, err := repo.UsageGetTotal(context.Background(), "0xpayer1")
	require.NoError(t, err)
	assert.Equal(t, int64(0), totalPayer.TotalBytes)
}

func TestUsageService_Report_NilEventSkipped(t *testing.T) {
	repo := newTestRepo()
	svc := usage.NewUsageService(repo)

	resp, err := svc.Report(context.Background(), connect.NewRequest(&usagev1.ReportRequest{
		Events: []*usagev1.Event{
			nil,
			{
				OrganizationId: "0xpayer1",
				Metrics: []*usagev1.Metric{
					{Name: "blocks_count", Value: 5},
				},
			},
		},
	}))
	require.NoError(t, err)
	assert.False(t, resp.Msg.Revoked)

	total, err := repo.UsageGetTotal(context.Background(), "0xpayer1")
	require.NoError(t, err)
	assert.Equal(t, int64(5), total.TotalBlocks)
}

func TestUsageService_Report_UnknownMetricsIgnored(t *testing.T) {
	repo := newTestRepo()
	svc := usage.NewUsageService(repo)

	_, err := svc.Report(context.Background(), connect.NewRequest(&usagev1.ReportRequest{
		Events: []*usagev1.Event{
			{
				OrganizationId: "0xpayer1",
				Metrics: []*usagev1.Metric{
					{Name: "unknown_metric", Value: 999},
					{Name: "blocks_count", Value: 7},
				},
			},
		},
	}))
	require.NoError(t, err)

	total, err := repo.UsageGetTotal(context.Background(), "0xpayer1")
	require.NoError(t, err)
	assert.Equal(t, int64(7), total.TotalBlocks)
	assert.Equal(t, int64(0), total.TotalBytes)
}
