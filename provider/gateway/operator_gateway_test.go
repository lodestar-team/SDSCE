package gateway

import (
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	sds "github.com/graphprotocol/substreams-data-service"
	"github.com/graphprotocol/substreams-data-service/horizon"
	"github.com/graphprotocol/substreams-data-service/internal/operatorauth"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
	"github.com/streamingfast/eth-go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewOperatorGateway_RequiresConfig(t *testing.T) {
	_, err := NewOperatorGateway(nil, zap.NewNop())
	require.ErrorContains(t, err, "operator gateway config must not be nil")

	_, err = NewOperatorGateway(&OperatorGatewayConfig{}, zap.NewNop())
	require.ErrorContains(t, err, "operator gateway listen address must be provided")

	_, err = NewOperatorGateway(&OperatorGatewayConfig{ListenAddr: ":0"}, zap.NewNop())
	require.ErrorContains(t, err, "operator gateway payment gateway must be provided")
}

func TestOperatorGatewayAuthorizeUsesPaymentGatewayAuthConfig(t *testing.T) {
	paymentGateway, err := New(&Config{
		ListenAddr: ":0",
		OperatorAuthConfig: operatorauth.Config{
			ReadBearerToken:  "read-token",
			AdminBearerToken: "admin-token",
		},
		Repository: repository.NewInMemoryRepository(),
	}, zap.NewNop())
	require.NoError(t, err)

	operatorGateway, err := NewOperatorGateway(&OperatorGatewayConfig{
		ListenAddr:     ":0",
		PaymentGateway: paymentGateway,
	}, zap.NewNop())
	require.NoError(t, err)

	header := http.Header{}
	header.Set("Authorization", "Bearer admin-token")

	role, err := operatorGateway.authorize(header, operatorauth.RoleAdminWrite)
	require.NoError(t, err)
	require.Equal(t, operatorauth.RoleAdminWrite, role)

	header.Set("Authorization", "Bearer read-token")
	_, err = operatorGateway.authorize(header, operatorauth.RoleAdminWrite)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
}

func TestOperatorGatewayMetricsHandler(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	payer := eth.MustNewAddress("0x1111111111111111111111111111111111111111")
	serviceProvider := eth.MustNewAddress("0x2222222222222222222222222222222222222222")
	dataService := eth.MustNewAddress("0x3333333333333333333333333333333333333333")

	active := repository.NewSession("session-active", payer, serviceProvider, dataService, repository.PricingConfig{})
	active.BlocksProcessed = 7
	active.BytesTransferred = 1000
	active.Requests = 2
	active.TotalCost = big.NewInt(2000)
	active.Metadata = map[string]string{
		"funds_status":                    "insufficient",
		"funds_current_outstanding_wei":   "1000",
		"funds_projected_outstanding_wei": "2000",
		"funds_escrow_balance_wei":        "500",
		"funds_minimum_needed_wei":        "1500",
	}
	require.NoError(t, repo.SessionCreate(t.Context(), active))

	rav := testMetricsSignedRAV(payer, serviceProvider, dataService, 1000)
	require.NoError(t, repo.SessionUpdateRAVAndBaseline(t.Context(), active.ID, rav, 0, 0, 0, big.NewInt(0)))
	require.NoError(t, repo.SessionApplyUsage(t.Context(), active.ID, &repository.UsageEvent{Blocks: 1}, big.NewInt(1)))
	require.NoError(t, repo.WorkerCreate(t.Context(), &repository.Worker{
		Key:       "worker-1",
		SessionID: active.ID,
		Payer:     payer,
		CreatedAt: time.Now(),
		TraceID:   "trace-1",
	}))

	terminated := repository.NewSession("session-ended", payer, serviceProvider, dataService, repository.PricingConfig{})
	terminated.CurrentRAV = testMetricsSignedRAV(payer, serviceProvider, dataService, 1000)
	terminated.TotalCost = big.NewInt(2)
	terminated.End(commonv1.EndReason_END_REASON_PAYMENT_ISSUE)
	require.NoError(t, repo.SessionCreate(t.Context(), terminated))

	paymentGateway, err := New(&Config{
		ListenAddr: ":0",
		OperatorAuthConfig: operatorauth.Config{
			ReadBearerToken:  "read-token",
			AdminBearerToken: "admin-token",
		},
		Repository:          repo,
		RAVRequestThreshold: sds.NewGRTFromUint64(1),
	}, zap.NewNop())
	require.NoError(t, err)

	operatorGateway, err := NewOperatorGateway(&OperatorGatewayConfig{
		ListenAddr:     ":0",
		PaymentGateway: paymentGateway,
	}, zap.NewNop())
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, providerMetricsPath, nil)
	resp := httptest.NewRecorder()
	operatorGateway.metricsHandler().ServeHTTP(resp, req)
	require.Equal(t, http.StatusUnauthorized, resp.Code)

	req = httptest.NewRequest(http.MethodGet, providerMetricsPath, nil)
	req.Header.Set("Authorization", "Bearer read-token")
	resp = httptest.NewRecorder()
	operatorGateway.metricsHandler().ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Header().Get("Content-Type"), "text/plain")

	body := resp.Body.String()
	require.Contains(t, body, `sds_provider_sessions{status="active"} 1`)
	require.Contains(t, body, `sds_provider_sessions{status="terminated"} 1`)
	require.Contains(t, body, `sds_provider_session_end_reasons{reason="payment_issue"} 1`)
	require.Contains(t, body, `sds_provider_workers_active 1`)
	require.Contains(t, body, `sds_provider_accepted_ravs 2`)
	require.Contains(t, body, `sds_provider_collection_records{state="collectible"} 2`)
	require.Contains(t, body, `sds_provider_low_funds_sessions 2`)
	require.Contains(t, body, `sds_provider_payment_control_pending_sessions 1`)
	require.Contains(t, body, `sds_provider_rav_request_eligible_sessions 1`)
	require.Contains(t, body, `sds_provider_usage_blocks_processed 8`)
	require.Contains(t, body, `sds_provider_usage_cost_raw 2003`)

	require.False(t, strings.Contains(body, "session-active"), "metrics must not expose high-cardinality session labels")
}

func testMetricsSignedRAV(payer, serviceProvider, dataService eth.Address, value int64) *horizon.SignedRAV {
	var collectionID horizon.CollectionID
	for i := range collectionID {
		collectionID[i] = byte(i + 1)
	}
	var signature eth.Signature
	return &horizon.SignedRAV{
		Message: &horizon.RAV{
			CollectionID:    collectionID,
			Payer:           payer,
			ServiceProvider: serviceProvider,
			DataService:     dataService,
			TimestampNs:     uint64(time.Now().UnixNano()),
			ValueAggregate:  big.NewInt(value),
		},
		Signature: signature,
	}
}
