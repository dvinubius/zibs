package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClassifyTraffic(t *testing.T) {
	cases := []struct {
		name, method, path, userAgent string
		want                          trafficClass
	}{
		{"synthetic follow", http.MethodGet, "/abc123", "zibs-traffic-lab/1.0 (+synthetic portfolio traffic)", trafficSynthetic},
		{"synthetic marker wins over probe path", http.MethodPost, "/xmlrpc.php", "zibs-traffic-lab/1.0", trafficSynthetic},
		{"WordPress PHP probe", http.MethodPost, "/xmlrpc.php", "", trafficSuspectedScan},
		{"WordPress admin probe", http.MethodGet, "/wp-admin/install.php", "", trafficSuspectedScan},
		{"mixed case PHP probe", http.MethodGet, "/foo.PHP", "", trafficSuspectedScan},
		{"observed framework probe", http.MethodPost, "/graphql", "", trafficSuspectedScan},
		{"dotfile probe", http.MethodGet, "/.env", "", trafficSuspectedScan},
		{"unknown admin path", http.MethodGet, "/admin", "", trafficSuspectedScan},
		{"crawler file", http.MethodGet, "/robots.txt", "", trafficSuspectedScan},
		{"unknown single-segment GET", http.MethodGet, "/api", "", trafficSuspectedScan},
		{"code-length path with a hyphen", http.MethodGet, "/wp-login", "", trafficSuspectedScan},
		{"too short for a code", http.MethodGet, "/AbCd123", "", trafficSuspectedScan},
		{"wrong method on a code", http.MethodPut, "/AbCd1234", "", trafficSuspectedScan},
		{"wrong method on the page", http.MethodPost, "/", "", trafficSuspectedScan},
		{"wrong method on creation", http.MethodGet, "/links", "", trafficSuspectedScan},
		{"ordinary short link", http.MethodGet, "/AbCd1234", "", trafficOther},
		{"HEAD short link", http.MethodHead, "/AbCd1234", "", trafficOther},
		{"creation page", http.MethodGet, "/", "", trafficOther},
		{"static asset", http.MethodGet, "/static/app.js", "", trafficOther},
		{"health request", http.MethodGet, "/health", "", trafficOther},
		{"link creation", http.MethodPost, "/links", "", trafficOther},
		{"admin link list", http.MethodGet, "/admin/links", "", trafficOther},
		{"admin link deletion", http.MethodDelete, "/admin/links/AbCd1234", "", trafficOther},
		{"admin token issue", http.MethodPost, "/admin/tokens", "", trafficOther},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("User-Agent", tc.userAgent)
			if got := classifyTraffic(req); got != tc.want {
				t.Errorf("classifyTraffic(%s %s) = %q, want %q", tc.method, tc.path, got, tc.want)
			}
		})
	}
}

func TestTrafficClassPartitionsRequestAndLinkMetrics(t *testing.T) {
	metrics, registry := newTestMetrics(t)
	store := newTestStoreWithMetrics(t, metrics)
	link, err := store.create("https://example.com")
	if err != nil {
		t.Fatalf("create seed link: %v", err)
	}
	var logs bytes.Buffer
	handler := logRequests(slog.New(slog.NewJSONHandler(&logs, nil)), metrics, newHandler(store, testAdminToken))

	for _, tc := range []struct {
		path, userAgent string
		status          int
	}{
		{"/" + link.Code, "zibs-traffic-lab/1.0", http.StatusFound},
		{"/" + link.Code, "", http.StatusFound},
		{"/wp-login.php", "", http.StatusNotFound},
		{"/robots.txt", "", http.StatusNotFound},
		{"/Zz000000", "", http.StatusNotFound},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Header.Set("User-Agent", tc.userAgent)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != tc.status {
			t.Fatalf("GET %s status = %d, want %d", tc.path, response.Code, tc.status)
		}
	}
	_, token, err := store.issueCreationToken("synthetic test", 1)
	if err != nil {
		t.Fatalf("issue creation token: %v", err)
	}
	create := httptest.NewRequest(http.MethodPost, "/links", strings.NewReader(`{"url":"https://example.org"}`))
	create.Header.Set("Authorization", "Bearer "+token)
	create.Header.Set("User-Agent", "zibs-traffic-lab/1.0")
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("synthetic create status = %d, want %d", created.Code, http.StatusCreated)
	}
	assertCounterMetric(t, registry, "zibs_link_operations_total", map[string]string{
		"operation": "create", "result": "success", "traffic_class": string(trafficSynthetic),
	}, 1)

	for _, tc := range []struct {
		class  trafficClass
		status string
		result string
		count  float64
	}{
		{trafficSynthetic, "302", "success", 1},
		{trafficOther, "302", "success", 1},
		{trafficSuspectedScan, "404", "not_found", 2},
	} {
		assertCounterMetric(t, registry, "zibs_http_requests_total", map[string]string{
			"route": "/{code}", "method": "GET", "status": tc.status, "traffic_class": string(tc.class),
		}, tc.count)
		assertCounterMetric(t, registry, "zibs_link_operations_total", map[string]string{
			"operation": "follow", "result": tc.result, "traffic_class": string(tc.class),
		}, tc.count)
	}
	// A code-shaped miss stays in "other": it may be an expired or deleted link.
	assertCounterMetric(t, registry, "zibs_link_operations_total", map[string]string{
		"operation": "follow", "result": "not_found", "traffic_class": string(trafficOther),
	}, 1)
	for _, class := range []trafficClass{trafficSynthetic, trafficOther, trafficSuspectedScan} {
		if !strings.Contains(logs.String(), `"traffic_class":"`+string(class)+`"`) {
			t.Errorf("request logs omit traffic class %q", class)
		}
	}
}
