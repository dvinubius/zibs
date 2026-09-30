package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetLinks(t *testing.T) {
	store := newTestStore(t)
	first, err := store.create("https://example.com")
	if err != nil {
		t.Fatalf("create first link: %v", err)
	}
	second, err := store.create("https://go.dev")
	if err != nil {
		t.Fatalf("create second link: %v", err)
	}
	handler := getLinks(store)
	req := httptest.NewRequest(http.MethodGet, "/links", nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if contentType := rec.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}

	var links []Link
	if err := json.NewDecoder(rec.Body).Decode(&links); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("number of links = %d, want 2", len(links))
	}

	linksByCode := make(map[string]Link, len(links))
	for _, link := range links {
		linksByCode[link.Code] = link
	}
	if got := linksByCode[first.Code].DestinationURL; got != first.DestinationURL {
		t.Errorf("first destination URL = %q, want %q", got, first.DestinationURL)
	}
	if got := linksByCode[second.Code].DestinationURL; got != second.DestinationURL {
		t.Errorf("second destination URL = %q, want %q", got, second.DestinationURL)
	}
}

func TestGetLinksEmpty(t *testing.T) {
	handler := getLinks(newTestStore(t))
	req := httptest.NewRequest(http.MethodGet, "/links", nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if contentType := rec.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}
	if body := rec.Body.String(); body != "[]\n" {
		t.Errorf("body = %q, want %q", body, "[]\n")
	}
}

func TestDeleteLinkByCode(t *testing.T) {
	store := newTestStore(t)
	created, err := store.create("https://example.com")
	if err != nil {
		t.Fatalf("create link: %v", err)
	}
	handler := deleteLinkByCode(store)
	req := httptest.NewRequest(http.MethodDelete, "/links/"+created.Code, nil)
	req.SetPathValue("code", created.Code)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("response body = %q, want empty", rec.Body.String())
	}
	if _, err := store.get(created.Code); !errors.Is(err, ErrLinkNotFound) {
		t.Errorf("get deleted link error = %v, want ErrLinkNotFound", err)
	}
}

func TestDeleteLinkByCodeNotFound(t *testing.T) {
	handler := newTestHandler(t, newTestStore(t))
	req := httptest.NewRequest(http.MethodDelete, "/admin/links/missing", nil)
	req.Header.Set("Authorization", "Bearer "+testAdminToken)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestAdminRoutesRequireBearerToken(t *testing.T) {
	authorizationCases := []struct {
		name          string
		authorization string
	}{
		{name: "missing"},
		{name: "wrong scheme", authorization: "Basic " + testAdminToken},
		{name: "missing token", authorization: "Bearer"},
		{name: "wrong token", authorization: "Bearer wrong-token"},
	}
	routes := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "list links", method: http.MethodGet, path: "/admin/links"},
		{name: "delete link", method: http.MethodDelete, path: "/admin/links/example"},
		{name: "issue token", method: http.MethodPost, path: "/admin/tokens", body: `{"label":"demo","maxUses":1}`},
		{name: "list tokens", method: http.MethodGet, path: "/admin/tokens"},
		{name: "revoke token", method: http.MethodDelete, path: "/admin/tokens/example"},
	}

	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			for _, authorizationCase := range authorizationCases {
				t.Run(authorizationCase.name, func(t *testing.T) {
					handler := newTestHandler(t, newTestStore(t))
					req := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
					req.Header.Set("Authorization", authorizationCase.authorization)
					rec := httptest.NewRecorder()

					handler.ServeHTTP(rec, req)

					if rec.Code != http.StatusUnauthorized {
						t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
					}
					if challenge := rec.Header().Get("WWW-Authenticate"); challenge != "Bearer" {
						t.Errorf("WWW-Authenticate = %q, want %q", challenge, "Bearer")
					}
				})
			}
		})
	}
}

func TestAdminRoutesAcceptBearerToken(t *testing.T) {
	store := newTestStore(t)
	created, err := store.create("https://example.com")
	if err != nil {
		t.Fatalf("create link: %v", err)
	}
	handler := newTestHandler(t, store)

	listRequest := httptest.NewRequest(http.MethodGet, "/admin/links", nil)
	listRequest.Header.Set("Authorization", "Bearer "+testAdminToken)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listResponse.Code, http.StatusOK)
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/admin/links/"+created.Code, nil)
	deleteRequest.Header.Set("Authorization", "Bearer "+testAdminToken)
	deleteResponse := httptest.NewRecorder()
	handler.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d", deleteResponse.Code, http.StatusNoContent)
	}
}

func TestAdminCanIssueCreationToken(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	handler := newTestHandler(t, store)
	req := httptest.NewRequest(
		http.MethodPost,
		"/admin/tokens",
		bytes.NewBufferString(`{"label":"employer demo","maxUses":2}`),
	)
	req.Header.Set("Authorization", "Bearer "+testAdminToken)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", cacheControl, "no-store")
	}
	var response struct {
		CreationToken
		Token string `json:"token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID == "" {
		t.Error("token ID is empty")
	}
	if !strings.HasPrefix(response.Token, "zib_") {
		t.Errorf("token = %q, want zib_ prefix", response.Token)
	}
	if len(response.Token) != len("zib_")+creationTokenLength {
		t.Errorf("token length = %d, want %d", len(response.Token), len("zib_")+creationTokenLength)
	}
	if response.Label != "employer demo" || response.MaxUses != 2 {
		t.Errorf("token metadata = %+v, want label and max uses from request", response.CreationToken)
	}
	var storedHash []byte
	if err := store.db.QueryRow(`SELECT token_hash FROM creation_tokens WHERE id = ?`, response.ID).Scan(&storedHash); err != nil {
		t.Fatalf("query stored token hash: %v", err)
	}
	wantHash := sha256.Sum256([]byte(response.Token))
	if !bytes.Equal(storedHash, wantHash[:]) {
		t.Errorf("stored token hash does not match SHA-256 of issued token")
	}
}

func TestIssueCreationTokenRejectsInvalidInput(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{name: "empty label", body: `{"label":"","maxUses":1}`},
		{name: "zero uses", body: `{"label":"demo","maxUses":0}`},
		{name: "uses above maximum", body: `{"label":"demo","maxUses":101}`},
		{name: "unknown field", body: `{"label":"demo","maxUses":1,"scope":"admin"}`},
		{name: "removed ttlMinutes field", body: `{"label":"demo","ttlMinutes":30,"maxUses":1}`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t)
			handler := newTestHandler(t, store)
			req := httptest.NewRequest(http.MethodPost, "/admin/tokens", bytes.NewBufferString(tc.body))
			req.Header.Set("Authorization", "Bearer "+testAdminToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
			var count int
			if err := store.db.QueryRow(`SELECT COUNT(*) FROM creation_tokens`).Scan(&count); err != nil {
				t.Fatalf("count creation tokens: %v", err)
			}
			if count != 0 {
				t.Errorf("stored tokens = %d, want 0", count)
			}
		})
	}
}

func TestAdminCanRevokeCreationToken(t *testing.T) {
	store := newTestStore(t)
	creationToken, token, err := store.issueCreationToken("revoke me", 1)
	if err != nil {
		t.Fatalf("issue creation token: %v", err)
	}
	handler := newTestHandler(t, store)

	revokeRequest := httptest.NewRequest(http.MethodDelete, "/admin/tokens/"+creationToken.ID, nil)
	revokeRequest.Header.Set("Authorization", "Bearer "+testAdminToken)
	revokeResponse := httptest.NewRecorder()
	handler.ServeHTTP(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want %d", revokeResponse.Code, http.StatusNoContent)
	}

	createRequest := httptest.NewRequest(http.MethodPost, "/links", bytes.NewBufferString(`{"url":"https://example.com"}`))
	createRequest.Header.Set("Authorization", "Bearer "+token)
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusUnauthorized {
		t.Fatalf("create status = %d, want %d", createResponse.Code, http.StatusUnauthorized)
	}
}

func TestAdminCanListCreationTokenMetadata(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	usedToken, usedSecret, err := store.issueCreationToken("used token", 3)
	if err != nil {
		t.Fatalf("issue used token: %v", err)
	}
	if _, err := store.consumeCreationToken(usedSecret); err != nil {
		t.Fatalf("consume token: %v", err)
	}

	now = now.Add(time.Minute)
	revokedToken, revokedSecret, err := store.issueCreationToken("revoked token", 5)
	if err != nil {
		t.Fatalf("issue revoked token: %v", err)
	}
	if err := store.revokeCreationToken(revokedToken.ID); err != nil {
		t.Fatalf("revoke token: %v", err)
	}

	handler := newTestHandler(t, store)
	req := httptest.NewRequest(http.MethodGet, "/admin/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+testAdminToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if strings.Contains(body, usedSecret) || strings.Contains(body, revokedSecret) {
		t.Fatal("token listing contains a token secret")
	}
	if strings.Contains(body, `"token":`) || strings.Contains(body, `"tokenHash":`) || strings.Contains(body, `"token_hash":`) {
		t.Fatal("token listing contains a secret or hash field")
	}
	var tokens []CreationToken
	if err := json.Unmarshal([]byte(body), &tokens); err != nil {
		t.Fatalf("decode token listing: %v", err)
	}
	if len(tokens) != 2 {
		t.Fatalf("listed tokens = %d, want 2", len(tokens))
	}
	if tokens[0].ID != revokedToken.ID {
		t.Errorf("first token ID = %q, want newest token %q", tokens[0].ID, revokedToken.ID)
	}
	if tokens[0].RevokedAt == nil || !tokens[0].RevokedAt.Equal(now) {
		t.Errorf("revoked at = %v, want %v", tokens[0].RevokedAt, now)
	}
	if tokens[1].ID != usedToken.ID || tokens[1].UseCount != 1 {
		t.Errorf("used token metadata = %+v, want ID %q and use count 1", tokens[1], usedToken.ID)
	}
}

func TestAdminCreationTokenListIsEmptyArray(t *testing.T) {
	handler := newTestHandler(t, newTestStore(t))
	req := httptest.NewRequest(http.MethodGet, "/admin/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+testAdminToken)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body != "[]\n" {
		t.Errorf("body = %q, want %q", body, "[]\n")
	}
}
