package main

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestLogRequests(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	metrics, _ := newTestMetrics(t)
	handler := logRequests(logger, metrics, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	for _, want := range []string{
		`msg="request completed"`,
		"method=GET",
		"path=/health",
		"status=204",
		"duration=",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log = %q, want it to contain %q", logs.String(), want)
		}
	}
}

func TestLogRequestsDoesNotLogBearerTokens(t *testing.T) {
	store := newTestStore(t)
	_, token, err := store.issueCreationToken("log test", 1)
	if err != nil {
		t.Fatalf("issue creation token: %v", err)
	}

	testCases := []struct {
		name       string
		method     string
		path       string
		body       string
		token      string
		wantStatus int
	}{
		{
			name:       "creation token",
			method:     http.MethodPost,
			path:       "/links",
			body:       `{"url":"https://example.com"}`,
			token:      token,
			wantStatus: http.StatusCreated,
		},
		{
			name:       "admin token",
			method:     http.MethodGet,
			path:       "/admin/links",
			token:      testAdminToken,
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			metrics, _ := newTestMetrics(t)
			handler := logRequests(
				slog.New(slog.NewTextHandler(&logs, nil)),
				metrics,
				newTestHandler(t, store),
			)
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+tc.token)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if strings.Contains(logs.String(), tc.token) {
				t.Errorf("request log contains bearer token")
			}
		})
	}
}

func TestLogRequestsRecordsHTTPMetrics(t *testing.T) {
	metrics, registry := newTestMetrics(t)
	handler := logRequests(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		metrics,
		http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusCreated)
		}),
	)

	handler.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/links", nil),
	)

	if got := httpRequestCounterValue(t, registry, "/links", http.MethodPost, "201"); got != 1 {
		t.Errorf("HTTP request counter = %v, want 1", got)
	}
	assertGaugeMetric(t, registry, "zibs_http_in_flight_requests", nil, 0)
}

func TestLogRequestsTracksInFlightRequestsUntilHandlerReturns(t *testing.T) {
	metrics, registry := newTestMetrics(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	handler := logRequests(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		metrics,
		http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			close(entered)
			<-release
			http.Error(w, "temporary failure", http.StatusInternalServerError)
		}),
	)

	finished := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))
		close(finished)
	}()

	<-entered
	assertGaugeMetric(t, registry, "zibs_http_in_flight_requests", nil, 1)
	close(release)
	<-finished
	assertGaugeMetric(t, registry, "zibs_http_in_flight_requests", nil, 0)
	assertCounterMetric(t, registry, "zibs_http_requests_total", map[string]string{"route": "/health", "method": http.MethodGet, "status": "500"}, 1)
}

func TestHTTPRequestDurationHistogramUsesSubFiveMillisecondBuckets(t *testing.T) {
	metrics, registry := newTestMetrics(t)
	metrics.httpRequestDuration.WithLabelValues("/health", http.MethodGet).Observe(0.0001)

	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	want := []float64{0.0005, 0.001, 0.002, 0.003, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2}
	for _, family := range metricFamilies {
		if family.GetName() != "zibs_http_request_duration_seconds" {
			continue
		}
		if len(family.Metric) != 1 {
			t.Fatalf("histogram series = %d, want 1", len(family.Metric))
		}
		buckets := family.Metric[0].GetHistogram().Bucket
		if len(buckets) != len(want) {
			t.Fatalf("histogram bucket count = %d, want %d", len(buckets), len(want))
		}
		for i, upperBound := range want {
			if got := buckets[i].GetUpperBound(); got != upperBound {
				t.Errorf("bucket %d upper bound = %v, want %v", i, got, upperBound)
			}
		}
		return
	}

	t.Fatal("HTTP request duration histogram was not registered")
}

func TestDBOperationDurationHistogramUsesSubFiveMillisecondBuckets(t *testing.T) {
	metrics, registry := newTestMetrics(t)
	metrics.dbOperationDuration.WithLabelValues(dbOperationCreate, "success").Observe(0.0001)

	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	want := []float64{0.0005, 0.001, 0.002, 0.003, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2}
	for _, family := range metricFamilies {
		if family.GetName() != "zibs_db_operation_duration_seconds" {
			continue
		}
		if len(family.Metric) != 1 {
			t.Fatalf("histogram series = %d, want 1", len(family.Metric))
		}
		buckets := family.Metric[0].GetHistogram().Bucket
		if len(buckets) != len(want) {
			t.Fatalf("histogram bucket count = %d, want %d", len(buckets), len(want))
		}
		for i, upperBound := range want {
			if got := buckets[i].GetUpperBound(); got != upperBound {
				t.Errorf("bucket %d upper bound = %v, want %v", i, got, upperBound)
			}
		}
		return
	}

	t.Fatal("DB operation duration histogram was not registered")
}

func TestLogRequestsUsesOneRouteLabelForShortCodes(t *testing.T) {
	metrics, registry := newTestMetrics(t)
	handler := logRequests(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		metrics,
		http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusFound)
		}),
	)

	for _, path := range []string{"/first-code", "/second-code"} {
		handler.ServeHTTP(
			httptest.NewRecorder(),
			httptest.NewRequest(http.MethodGet, path, nil),
		)
	}

	if got := httpRequestCounterValue(t, registry, "/{code}", http.MethodGet, "302"); got != 2 {
		t.Errorf("HTTP request counter = %v, want 2", got)
	}
}

func TestMetricsEndpointServesPrivateRegistry(t *testing.T) {
	metrics, registry := newTestMetrics(t)
	handler := logRequests(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		metrics,
		newHandler(newTestStore(t), testAdminToken),
	)

	healthResponse := httptest.NewRecorder()
	handler.ServeHTTP(healthResponse, httptest.NewRequest(http.MethodGet, "/health", nil))

	metricsResponse := httptest.NewRecorder()
	newMetricsHandler(registry).ServeHTTP(metricsResponse, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if metricsResponse.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", metricsResponse.Code, http.StatusOK)
	}
	if contentType := metricsResponse.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/plain") {
		t.Errorf("Content-Type = %q, want Prometheus text format", contentType)
	}
	if body := metricsResponse.Body.String(); !strings.Contains(body, "zibs_http_requests_total") {
		t.Errorf("metrics response does not contain the HTTP request counter")
	}
	if got := httpRequestCounterValue(t, registry, "/health", http.MethodGet, "200"); got != 1 {
		t.Errorf("HTTP request counter = %v, want 1", got)
	}
}

func TestMetricsRegistryIncludesGoAndProcessCollectors(t *testing.T) {
	_, registry := newTestMetrics(t)

	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	found := make(map[string]bool, len(metricFamilies))
	for _, family := range metricFamilies {
		found[family.GetName()] = true
	}
	for _, name := range []string{"go_gc_duration_seconds", "process_start_time_seconds"} {
		if !found[name] {
			t.Errorf("metric registry does not include %q", name)
		}
	}
}

func TestMetricsRegistryIncludesDatabaseStats(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	_, registry := newTestMetrics(t)
	if err := registerDBStatsMetrics(registry, db); err != nil {
		t.Fatalf("register database stats metrics: %v", err)
	}

	stats := db.Stats()
	want := map[string]float64{
		"zibs_db_open_connections":            float64(stats.OpenConnections),
		"zibs_db_in_use_connections":          float64(stats.InUse),
		"zibs_db_idle_connections":            float64(stats.Idle),
		"zibs_db_wait_count_total":            float64(stats.WaitCount),
		"zibs_db_wait_duration_seconds_total": stats.WaitDuration.Seconds(),
	}

	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range metricFamilies {
		wantValue, ok := want[family.GetName()]
		if !ok {
			continue
		}
		if len(family.GetMetric()) != 1 {
			t.Errorf("%s has %d metrics, want 1", family.GetName(), len(family.GetMetric()))
			continue
		}

		metric := family.GetMetric()[0]
		got := metric.GetGauge().GetValue()
		if metric.GetCounter() != nil {
			got = metric.GetCounter().GetValue()
		}
		if got != wantValue {
			t.Errorf("%s = %v, want %v", family.GetName(), got, wantValue)
		}
		delete(want, family.GetName())
	}
	for name := range want {
		t.Errorf("metric registry does not include %q", name)
	}
}

func TestMetricsHandlerOnlyServesMetricsRoute(t *testing.T) {
	_, registry := newTestMetrics(t)
	response := httptest.NewRecorder()

	newMetricsHandler(registry).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if response.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestPublicHandlerDoesNotServeMetrics(t *testing.T) {
	publicResponse := httptest.NewRecorder()
	newTestHandler(t, newTestStore(t)).ServeHTTP(
		publicResponse,
		httptest.NewRequest(http.MethodGet, "/metrics", nil),
	)

	if publicResponse.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", publicResponse.Code, http.StatusNotFound)
	}
	if strings.Contains(publicResponse.Body.String(), "zibs_http_requests_total") {
		t.Error("public handler exposes Prometheus metrics")
	}
}

func httpRequestCounterValue(t *testing.T, registry *prometheus.Registry, route, method, status string) float64 {
	t.Helper()

	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	for _, family := range metricFamilies {
		if family.GetName() != "zibs_http_requests_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["route"] == route && labels["method"] == method && labels["status"] == status {
				return metric.GetCounter().GetValue()
			}
		}
	}

	t.Fatalf("HTTP request counter with route=%q, method=%q, status=%q was not found", route, method, status)
	return 0
}

func counterMetricValue(registry *prometheus.Registry, name string, wantLabels map[string]string) (float64, bool) {
	metricFamilies, err := registry.Gather()
	if err != nil {
		return 0, false
	}
	for _, family := range metricFamilies {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if labelsMatch(metric.GetLabel(), wantLabels) && metric.GetCounter() != nil {
				return metric.GetCounter().GetValue(), true
			}
		}
	}
	return 0, false
}

func gaugeMetricValue(registry *prometheus.Registry, name string, wantLabels map[string]string) (float64, bool) {
	metricFamilies, err := registry.Gather()
	if err != nil {
		return 0, false
	}
	for _, family := range metricFamilies {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if labelsMatch(metric.GetLabel(), wantLabels) && metric.GetGauge() != nil {
				return metric.GetGauge().GetValue(), true
			}
		}
	}
	return 0, false
}

func histogramMetricCount(registry *prometheus.Registry, name string, wantLabels map[string]string) (uint64, bool) {
	metricFamilies, err := registry.Gather()
	if err != nil {
		return 0, false
	}
	for _, family := range metricFamilies {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if labelsMatch(metric.GetLabel(), wantLabels) && metric.GetHistogram() != nil {
				return metric.GetHistogram().GetSampleCount(), true
			}
		}
	}
	return 0, false
}

func assertCounterMetric(t *testing.T, registry *prometheus.Registry, name string, labels map[string]string, want float64) {
	t.Helper()

	got, ok := counterMetricValue(registry, name, labels)
	if !ok || got != want {
		t.Errorf("%s%v = %v, found %t; want %v", name, labels, got, ok, want)
	}
}

func assertGaugeMetric(t *testing.T, registry *prometheus.Registry, name string, labels map[string]string, want float64) {
	t.Helper()

	got, ok := gaugeMetricValue(registry, name, labels)
	if !ok || got != want {
		t.Errorf("%s%v = %v, found %t; want %v", name, labels, got, ok, want)
	}
}

func assertHistogramMetricCount(t *testing.T, registry *prometheus.Registry, name string, labels map[string]string, want uint64) {
	t.Helper()

	got, ok := histogramMetricCount(registry, name, labels)
	if !ok || got != want {
		t.Errorf("%s%v sample count = %d, found %t; want %d", name, labels, got, ok, want)
	}
}

func labelsMatch(labels []*dto.LabelPair, want map[string]string) bool {
	if len(labels) != len(want) {
		return false
	}
	for _, label := range labels {
		if want[label.GetName()] != label.GetValue() {
			return false
		}
	}
	return true
}

func TestCreateMetrics(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		metrics, registry := newTestMetrics(t)
		if _, err := newTestStoreWithMetrics(t, metrics).create("https://example.com"); err != nil {
			t.Fatalf("create link: %v", err)
		}

		assertCounterMetric(t, registry, "zibs_link_operations_total", map[string]string{"operation": "create", "result": "success"}, 1)
		assertHistogramMetricCount(t, registry, "zibs_db_operation_duration_seconds", map[string]string{"operation": "create", "result": "success"}, 1)
	})

	t.Run("database error", func(t *testing.T) {
		metrics, registry := newTestMetrics(t)
		if _, err := newClosedTestStore(t, metrics).create("https://example.com"); err == nil {
			t.Fatal("create link error = nil, want database error")
		}

		assertCounterMetric(t, registry, "zibs_link_operations_total", map[string]string{"operation": "create", "result": "error"}, 1)
		assertHistogramMetricCount(t, registry, "zibs_db_operation_duration_seconds", map[string]string{"operation": "create", "result": "error"}, 1)
		assertCounterMetric(t, registry, "zibs_db_errors_total", map[string]string{"operation": "create", "kind": "other"}, 1)
	})
}

func TestSQLiteErrorKind(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want string
	}{
		{name: "busy", err: sqlite3.Error{Code: sqlite3.ErrBusy}, want: "busy"},
		{name: "locked", err: sqlite3.Error{Code: sqlite3.ErrLocked}, want: "busy"},
		{name: "constraint", err: sqlite3.Error{Code: sqlite3.ErrConstraint}, want: "constraint"},
		{name: "non SQLite", err: errors.New("database unavailable: host=db.internal"), want: "other"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sqliteErrorKind(tc.err); got != tc.want {
				t.Errorf("sqliteErrorKind(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func TestDBErrorMetricUsesBoundedLabels(t *testing.T) {
	metrics, registry := newTestMetrics(t)
	newTestStoreWithMetrics(t, metrics).observeDBOperation("test_operation", "error", time.Now(), errors.New("unbounded detail: https://secret.example/abc"))

	assertCounterMetric(t, registry, "zibs_db_errors_total", map[string]string{"operation": "test_operation", "kind": "other"}, 1)

	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range metricFamilies {
		if family.GetName() != "zibs_db_errors_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if strings.Contains(label.GetValue(), "secret.example") {
					t.Fatalf("database error metric includes raw error text in label %q", label.GetName())
				}
			}
		}
	}
}

func TestFollowMetrics(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		metrics, registry := newTestMetrics(t)
		store := newTestStoreWithMetrics(t, metrics)
		link, err := store.create("https://example.com")
		if err != nil {
			t.Fatalf("create link: %v", err)
		}
		if _, err := store.follow(link.Code); err != nil {
			t.Fatalf("follow link: %v", err)
		}

		assertCounterMetric(t, registry, "zibs_link_operations_total", map[string]string{"operation": "follow", "result": "success"}, 1)
		assertHistogramMetricCount(t, registry, "zibs_db_operation_duration_seconds", map[string]string{"operation": "follow", "result": "success"}, 1)
	})

	t.Run("not found", func(t *testing.T) {
		metrics, registry := newTestMetrics(t)
		if _, err := newTestStoreWithMetrics(t, metrics).follow("missing"); !errors.Is(err, ErrLinkNotFound) {
			t.Fatalf("follow error = %v, want ErrLinkNotFound", err)
		}

		assertCounterMetric(t, registry, "zibs_link_operations_total", map[string]string{"operation": "follow", "result": "not_found"}, 1)
		assertHistogramMetricCount(t, registry, "zibs_db_operation_duration_seconds", map[string]string{"operation": "follow", "result": "not_found"}, 1)
	})

	t.Run("database error", func(t *testing.T) {
		metrics, registry := newTestMetrics(t)
		if _, err := newClosedTestStore(t, metrics).follow("example"); err == nil {
			t.Fatal("follow error = nil, want database error")
		}

		assertCounterMetric(t, registry, "zibs_link_operations_total", map[string]string{"operation": "follow", "result": "error"}, 1)
		assertHistogramMetricCount(t, registry, "zibs_db_operation_duration_seconds", map[string]string{"operation": "follow", "result": "error"}, 1)
	})
}

func TestDeleteMetrics(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		metrics, registry := newTestMetrics(t)
		store := newTestStoreWithMetrics(t, metrics)
		link, err := store.create("https://example.com")
		if err != nil {
			t.Fatalf("create link: %v", err)
		}
		if err := store.delete(link.Code); err != nil {
			t.Fatalf("delete link: %v", err)
		}

		assertCounterMetric(t, registry, "zibs_link_operations_total", map[string]string{"operation": "delete", "result": "success"}, 1)
		assertHistogramMetricCount(t, registry, "zibs_db_operation_duration_seconds", map[string]string{"operation": "delete", "result": "success"}, 1)
	})

	t.Run("not found", func(t *testing.T) {
		metrics, registry := newTestMetrics(t)
		if err := newTestStoreWithMetrics(t, metrics).delete("missing"); !errors.Is(err, ErrLinkNotFound) {
			t.Fatalf("delete error = %v, want ErrLinkNotFound", err)
		}

		assertCounterMetric(t, registry, "zibs_link_operations_total", map[string]string{"operation": "delete", "result": "not_found"}, 1)
		assertHistogramMetricCount(t, registry, "zibs_db_operation_duration_seconds", map[string]string{"operation": "delete", "result": "not_found"}, 1)
	})

	t.Run("database error", func(t *testing.T) {
		metrics, registry := newTestMetrics(t)
		if err := newClosedTestStore(t, metrics).delete("example"); err == nil {
			t.Fatal("delete error = nil, want database error")
		}

		assertCounterMetric(t, registry, "zibs_link_operations_total", map[string]string{"operation": "delete", "result": "error"}, 1)
		assertHistogramMetricCount(t, registry, "zibs_db_operation_duration_seconds", map[string]string{"operation": "delete", "result": "error"}, 1)
	})
}
