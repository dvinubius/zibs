package main

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

const testAdminToken = "test-admin-token"

func newTestStore(t *testing.T) *linkStore {
	t.Helper()

	metrics, _ := newTestMetrics(t)
	return newTestStoreWithMetrics(t, metrics)
}

func newTestStoreWithMetrics(t *testing.T, metrics *metrics) *linkStore {
	t.Helper()

	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})

	if err := migrate(db); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	return newLinkStore(db, metrics)
}

func newClosedTestStore(t *testing.T, metrics *metrics) *linkStore {
	t.Helper()

	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		t.Fatalf("migrate test database: %v", err)
	}
	store := newLinkStore(db, metrics)
	if err := db.Close(); err != nil {
		t.Fatalf("close test database: %v", err)
	}

	return store
}

func newTestMetrics(t *testing.T) (*metrics, *prometheus.Registry) {
	t.Helper()

	metrics, registry, err := newMetrics()
	if err != nil {
		t.Fatalf("create metrics: %v", err)
	}

	return metrics, registry
}

func newTestHandler(t *testing.T, store *linkStore) *http.ServeMux {
	t.Helper()

	return newHandler(store, testAdminToken)
}
