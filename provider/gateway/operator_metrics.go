package gateway

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"sort"
	"strings"

	"connectrpc.com/connect"
	commonv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/common/v1"
	"github.com/graphprotocol/substreams-data-service/provider/repository"
)

const providerMetricsPath = "/metrics"

func (s *OperatorGateway) metricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := s.authorizeRead(r.Header); err != nil {
			writeOperatorHTTPError(w, err)
			return
		}

		body, err := s.prometheusMetrics(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(body))
	})
}

func writeOperatorHTTPError(w http.ResponseWriter, err error) {
	status := http.StatusUnauthorized
	if connect.CodeOf(err) == connect.CodePermissionDenied {
		status = http.StatusForbidden
	}
	http.Error(w, err.Error(), status)
}

func (s *OperatorGateway) prometheusMetrics(ctx context.Context) (string, error) {
	snapshot, err := s.operatorMetricsSnapshot(ctx)
	if err != nil {
		return "", err
	}
	return snapshot.prometheusText(), nil
}

func (s *OperatorGateway) operatorMetricsSnapshot(ctx context.Context) (*operatorMetricsSnapshot, error) {
	sessions, err := s.paymentGateway.repo.SessionList(ctx, repository.SessionFilter{})
	if err != nil {
		return nil, fmt.Errorf("listing sessions for metrics: %w", err)
	}
	collections, err := s.paymentGateway.repo.CollectionList(ctx, repository.CollectionFilter{})
	if err != nil {
		return nil, fmt.Errorf("listing collections for metrics: %w", err)
	}

	snapshot := &operatorMetricsSnapshot{
		sessionsByStatus:   map[string]int{},
		endedByReason:      map[string]int{},
		collectionsByState: map[string]int{},
	}

	for _, session := range sessions {
		status := string(session.Status)
		if status == "" {
			status = "unknown"
		}
		snapshot.sessionsByStatus[status]++

		if !session.IsActive() {
			snapshot.endedByReason[endReasonLabel(session.EndReason)]++
		}
		if session.CurrentRAV != nil && session.CurrentRAV.Message != nil {
			snapshot.acceptedRAVs++
		}
		if sessionFundsStatus(session) == fundsStatusInsufficient {
			snapshot.lowFundsSessions++
		}
		if session.IsActive() && s.paymentGateway.shouldRequestRAV(session) {
			snapshot.ravRequestEligibleSessions++
		}

		workers, err := s.paymentGateway.repo.WorkerCountBySession(ctx, session.ID)
		if err != nil {
			return nil, fmt.Errorf("counting workers for session %q: %w", session.ID, err)
		}
		snapshot.activeWorkers += workers

		pending, err := s.paymentGateway.paymentControlPending(ctx, session)
		if err != nil {
			return nil, fmt.Errorf("checking payment control pending for session %q: %w", session.ID, err)
		}
		if pending {
			snapshot.paymentControlPendingSessions++
		}

		snapshot.blocksProcessed += session.BlocksProcessed
		snapshot.bytesTransferred += session.BytesTransferred
		snapshot.requests += session.Requests
		snapshot.usageCostRaw.Add(&snapshot.usageCostRaw, cloneOrZero(session.TotalCost))
	}

	for _, collection := range collections {
		state := string(collection.State)
		if state == "" {
			state = "unknown"
		}
		snapshot.collectionsByState[state]++
	}

	return snapshot, nil
}

type operatorMetricsSnapshot struct {
	sessionsByStatus              map[string]int
	endedByReason                 map[string]int
	collectionsByState            map[string]int
	activeWorkers                 int
	acceptedRAVs                  int
	lowFundsSessions              int
	paymentControlPendingSessions int
	ravRequestEligibleSessions    int
	blocksProcessed               uint64
	bytesTransferred              uint64
	requests                      uint64
	usageCostRaw                  big.Int
}

func (s *operatorMetricsSnapshot) prometheusText() string {
	var b strings.Builder
	writeMetricHelp(&b, "sds_provider_sessions", "Current provider sessions by repository status.")
	writeMetricType(&b, "sds_provider_sessions", "gauge")
	for _, status := range sortedKeys(s.sessionsByStatus) {
		writeMetric(&b, "sds_provider_sessions", map[string]string{"status": status}, float64(s.sessionsByStatus[status]))
	}

	writeMetricHelp(&b, "sds_provider_session_end_reasons", "Current terminated provider sessions by end reason.")
	writeMetricType(&b, "sds_provider_session_end_reasons", "gauge")
	for _, reason := range sortedKeys(s.endedByReason) {
		writeMetric(&b, "sds_provider_session_end_reasons", map[string]string{"reason": reason}, float64(s.endedByReason[reason]))
	}

	writeMetricHelp(&b, "sds_provider_workers_active", "Current active provider runtime workers.")
	writeMetricType(&b, "sds_provider_workers_active", "gauge")
	writeMetric(&b, "sds_provider_workers_active", nil, float64(s.activeWorkers))

	writeMetricHelp(&b, "sds_provider_accepted_ravs", "Current sessions with an accepted RAV snapshot.")
	writeMetricType(&b, "sds_provider_accepted_ravs", "gauge")
	writeMetric(&b, "sds_provider_accepted_ravs", nil, float64(s.acceptedRAVs))

	writeMetricHelp(&b, "sds_provider_collection_records", "Current provider collection lifecycle records by state.")
	writeMetricType(&b, "sds_provider_collection_records", "gauge")
	for _, state := range sortedKeys(s.collectionsByState) {
		writeMetric(&b, "sds_provider_collection_records", map[string]string{"state": state}, float64(s.collectionsByState[state]))
	}

	writeMetricHelp(&b, "sds_provider_low_funds_sessions", "Current sessions whose last provider funds assessment is insufficient.")
	writeMetricType(&b, "sds_provider_low_funds_sessions", "gauge")
	writeMetric(&b, "sds_provider_low_funds_sessions", nil, float64(s.lowFundsSessions))

	writeMetricHelp(&b, "sds_provider_payment_control_pending_sessions", "Current sessions with provider-side payment control pending.")
	writeMetricType(&b, "sds_provider_payment_control_pending_sessions", "gauge")
	writeMetric(&b, "sds_provider_payment_control_pending_sessions", nil, float64(s.paymentControlPendingSessions))

	writeMetricHelp(&b, "sds_provider_rav_request_eligible_sessions", "Current sessions whose unbaselined usage has reached the RAV request threshold.")
	writeMetricType(&b, "sds_provider_rav_request_eligible_sessions", "gauge")
	writeMetric(&b, "sds_provider_rav_request_eligible_sessions", nil, float64(s.ravRequestEligibleSessions))

	writeMetricHelp(&b, "sds_provider_usage_blocks_processed", "Total blocks processed across retained provider sessions.")
	writeMetricType(&b, "sds_provider_usage_blocks_processed", "gauge")
	writeMetric(&b, "sds_provider_usage_blocks_processed", nil, float64(s.blocksProcessed))

	writeMetricHelp(&b, "sds_provider_usage_bytes_transferred", "Total bytes transferred across retained provider sessions.")
	writeMetricType(&b, "sds_provider_usage_bytes_transferred", "gauge")
	writeMetric(&b, "sds_provider_usage_bytes_transferred", nil, float64(s.bytesTransferred))

	writeMetricHelp(&b, "sds_provider_usage_requests", "Total metered requests across retained provider sessions.")
	writeMetricType(&b, "sds_provider_usage_requests", "gauge")
	writeMetric(&b, "sds_provider_usage_requests", nil, float64(s.requests))

	writeMetricHelp(&b, "sds_provider_usage_cost_raw", "Total metered cost across retained provider sessions in raw GRT base units.")
	writeMetricType(&b, "sds_provider_usage_cost_raw", "gauge")
	writeMetricRaw(&b, "sds_provider_usage_cost_raw", nil, s.usageCostRaw.String())

	return b.String()
}

func sortedKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func writeMetricHelp(b *strings.Builder, name string, help string) {
	fmt.Fprintf(b, "# HELP %s %s\n", name, help)
}

func writeMetricType(b *strings.Builder, name string, typ string) {
	fmt.Fprintf(b, "# TYPE %s %s\n", name, typ)
}

func writeMetric(b *strings.Builder, name string, labels map[string]string, value float64) {
	writeMetricRaw(b, name, labels, fmt.Sprintf("%g", value))
}

func writeMetricRaw(b *strings.Builder, name string, labels map[string]string, value string) {
	b.WriteString(name)
	if len(labels) != 0 {
		b.WriteString("{")
		keys := sortedLabelKeys(labels)
		for i, key := range keys {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(b, "%s=%q", key, escapePrometheusLabel(labels[key]))
		}
		b.WriteString("}")
	}
	b.WriteString(" ")
	b.WriteString(value)
	b.WriteString("\n")
}

func sortedLabelKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func escapePrometheusLabel(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func endReasonLabel(reason commonv1.EndReason) string {
	switch reason {
	case commonv1.EndReason_END_REASON_COMPLETE:
		return "complete"
	case commonv1.EndReason_END_REASON_CLIENT_DISCONNECT:
		return "client_disconnect"
	case commonv1.EndReason_END_REASON_PROVIDER_STOP:
		return "provider_stop"
	case commonv1.EndReason_END_REASON_ERROR:
		return "error"
	case commonv1.EndReason_END_REASON_PAYMENT_ISSUE:
		return "payment_issue"
	default:
		return "unspecified"
	}
}
