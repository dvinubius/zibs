package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFrontendPageServedAtRoot(t *testing.T) {
	handler := newTestHandler(t, newTestStore(t))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", contentType)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="shorten-form"`) {
		t.Errorf("page does not contain the creation form")
	}
	if !strings.Contains(body, `type="password"`) {
		t.Errorf("token input is not a password field")
	}
}

func TestFrontendFormInputsHaveNoNameAttributes(t *testing.T) {
	// Without name attributes a native (non-JS) form submission carries no
	// fields, so the token can never leak into a request body or URL.
	handler := newTestHandler(t, newTestStore(t))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, tag := range strings.Split(body, "<input")[1:] {
		tag = tag[:strings.Index(tag, ">")]
		if strings.Contains(tag, "name=") {
			t.Errorf("form input has a name attribute: <input%s>", tag)
		}
	}
}

func TestFrontendPageDoesNotShadowRedirects(t *testing.T) {
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
}

func TestStaticAssetServed(t *testing.T) {
	handler := newTestHandler(t, newTestStore(t))

	tests := []struct {
		path        string
		contentType string
	}{
		{"/static/styles.css", "text/css"},
		{"/static/app.js", "javascript"},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want %d", tt.path, rec.Code, http.StatusOK)
		}
		if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, tt.contentType) {
			t.Errorf("GET %s Content-Type = %q, want %q", tt.path, contentType, tt.contentType)
		}
	}
}

func TestStaticAssetMissing(t *testing.T) {
	handler := newTestHandler(t, newTestStore(t))
	req := httptest.NewRequest(http.MethodGet, "/static/missing.css", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestStaticDirectoryListingBlocked(t *testing.T) {
	handler := newTestHandler(t, newTestStore(t))
	for _, path := range []string{"/static/", "/static/fonts/"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want %d", path, rec.Code, http.StatusNotFound)
		}
	}
}
