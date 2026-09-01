package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetLinkByCode(t *testing.T) {
	store := newTestStore(t)
	created, err := store.create("https://example.com")
	if err != nil {
		t.Fatalf("create link: %v", err)
	}
	handler := newTestHandler(t, store)
	req := httptest.NewRequest(http.MethodGet, "/"+created.Code, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if location := rec.Header().Get("Location"); location != created.DestinationURL {
		t.Errorf("Location = %q, want %q", location, created.DestinationURL)
	}

	link, err := store.get(created.Code)
	if err != nil {
		t.Fatalf("get stored link: %v", err)
	}
	if link.RedirectCount != 1 {
		t.Errorf("redirect count = %d, want 1", link.RedirectCount)
	}
}

func TestGetLinkByCodeNotFound(t *testing.T) {
	handler := newTestHandler(t, newTestStore(t))
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestGetLinkByCodeExpired(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	created, err := store.create("https://example.com")
	if err != nil {
		t.Fatalf("create link: %v", err)
	}
	store.now = func() time.Time { return created.ExpiresAt }

	handler := newTestHandler(t, store)
	req := httptest.NewRequest(http.MethodGet, "/"+created.Code, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if location := rec.Header().Get("Location"); location != "" {
		t.Errorf("Location = %q, want empty", location)
	}
}

func TestHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	health(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if body := rec.Body.String(); body != "OK, I'm healthy\n" {
		t.Errorf("body = %q, want %q", body, "OK, I'm healthy\n")
	}
}

func TestCreateLink(t *testing.T) {
	store := newTestStore(t)
	handler := createLink(store)
	req := httptest.NewRequest(http.MethodPost, "/links", bytes.NewBufferString(`{"url":"https://example.com"}`))
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	if contentType := rec.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}

	var response struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Code == "" {
		t.Fatal("response code is empty")
	}

	link, err := store.get(response.Code)
	if err != nil {
		t.Fatalf("get stored link: %v", err)
	}
	if link.DestinationURL != "https://example.com" {
		t.Errorf("destination URL = %q, want %q", link.DestinationURL, "https://example.com")
	}
	if link.CreatedAt.IsZero() {
		t.Error("creation time was not set")
	}
	if want := link.CreatedAt.Add(defaultLinkTTL); !link.ExpiresAt.Equal(want) {
		t.Errorf("expiry time = %v, want %v", link.ExpiresAt, want)
	}
	if link.RedirectCount != 0 {
		t.Errorf("redirect count = %d, want 0", link.RedirectCount)
	}
}

func TestCreateLinkAddsHTTPSWhenTheSchemeIsOmitted(t *testing.T) {
	store := newTestStore(t)
	handler := createLink(store)
	req := httptest.NewRequest(http.MethodPost, "/links", bytes.NewBufferString(`{"url":"example.com/articles?tag=go"}`))
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %q", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var response struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	link, err := store.get(response.Code)
	if err != nil {
		t.Fatalf("get stored link: %v", err)
	}
	if link.DestinationURL != "https://example.com/articles?tag=go" {
		t.Errorf("destination URL = %q, want %q", link.DestinationURL, "https://example.com/articles?tag=go")
	}
}

func TestCreateLinkRejectsInvalidJSON(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "malformed JSON", body: `{"url":`},
		{name: "unknown field", body: `{"url":"https://example.com","code":"custom"}`},
		{name: "multiple JSON values", body: `{"url":"https://example.com"} {"url":"https://go.dev"}`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t)
			handler := createLink(store)
			req := httptest.NewRequest(http.MethodPost, "/links", bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()

			handler(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}

			links, err := store.list()
			if err != nil {
				t.Fatalf("list links: %v", err)
			}
			if len(links) != 0 {
				t.Errorf("stored links = %d, want 0", len(links))
			}
		})
	}
}

func TestCreateLinkRejectsInvalidURL(t *testing.T) {
	testCases := []struct {
		name    string
		body    string
		message string
	}{
		{name: "non-http(s)-url", body: `{"url":"ftp://example.com"}`, message: "use a web link beginning with http:// or https://\n"},
		{name: "empty-url", body: `{"url":""}`, message: "enter a link to shorten\n"},
		{name: "invalid-url", body: `{"url":"zeta33"}`, message: "that doesn't look like a web address; try example.com or https://example.com\n"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t)
			handler := createLink(store)
			req := httptest.NewRequest(http.MethodPost, "/links", bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()

			handler(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
			if rec.Body.String() != tc.message {
				t.Errorf("response body = %q, want %q", rec.Body.String(), tc.message)
			}

			links, err := store.list()
			if err != nil {
				t.Fatalf("list links: %v", err)
			}
			if len(links) != 0 {
				t.Errorf("stored links = %d, want 0", len(links))
			}
		})
	}
}

func TestCreationTokenUseLimit(t *testing.T) {
	store := newTestStore(t)
	_, token, err := store.issueCreationToken("two links", 2)
	if err != nil {
		t.Fatalf("issue creation token: %v", err)
	}
	handler := newTestHandler(t, store)

	for attempt, wantStatus := range []int{http.StatusCreated, http.StatusCreated, http.StatusUnauthorized} {
		req := httptest.NewRequest(
			http.MethodPost,
			"/links",
			bytes.NewBufferString(`{"url":"https://example.com"}`),
		)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != wantStatus {
			t.Fatalf("attempt %d status = %d, want %d", attempt+1, rec.Code, wantStatus)
		}
	}
}

func TestCreationTokenDoesNotExpire(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	_, token, err := store.issueCreationToken("patient demo", 1)
	if err != nil {
		t.Fatalf("issue creation token: %v", err)
	}
	now = now.Add(5 * 365 * 24 * time.Hour)

	handler := newTestHandler(t, store)
	req := httptest.NewRequest(http.MethodPost, "/links", bytes.NewBufferString(`{"url":"https://example.com"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
}

func TestCreateRouteRequiresCreationToken(t *testing.T) {
	handler := newTestHandler(t, newTestStore(t))
	for _, authorization := range []string{"", "Bearer wrong-token", "Basic wrong-token"} {
		req := httptest.NewRequest(http.MethodPost, "/links", bytes.NewBufferString(`{"url":"https://example.com"}`))
		req.Header.Set("Authorization", authorization)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Authorization %q status = %d, want %d", authorization, rec.Code, http.StatusUnauthorized)
		}
	}
}

func TestPublicManagementRoutesAreUnavailable(t *testing.T) {
	handler := newTestHandler(t, newTestStore(t))

	for _, methodAndPath := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/links"},
		{method: http.MethodDelete, path: "/links/example"},
	} {
		req := httptest.NewRequest(methodAndPath.method, methodAndPath.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK || rec.Code == http.StatusNoContent {
			t.Errorf("%s %s status = %d, want route unavailable", methodAndPath.method, methodAndPath.path, rec.Code)
		}
	}
}

func TestErrorsAreHandledWithCleanExit(t *testing.T) {
	store := newTestStore(t)
	err := store.db.Close()
	if err != nil {
		t.Fatalf("close test database: %v", err)
	}

	handler := createLink(store)
	req := httptest.NewRequest(http.MethodPost, "/links", bytes.NewBufferString(`{"url":"https://example.com"}`))
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if body := rec.Body.String(); body != "internal server error\n" {
		t.Errorf("body = %q, want %q", body, "internal server error\n")
	}
}
