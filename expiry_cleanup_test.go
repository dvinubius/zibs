package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestExpiryCleanupRunsAtStartupAndStopsWhenCanceled(t *testing.T) {
	metrics, registry := newTestMetrics(t)
	store := newTestStoreWithMetrics(t, metrics)
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	link, err := store.create("https://example.com")
	if err != nil {
		t.Fatalf("create link: %v", err)
	}
	store.now = func() time.Time { return link.ExpiresAt }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runExpiryCleanup(ctx, store, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)), metrics)
	}()

	deadline := time.Now().Add(time.Second)
	for {
		if _, err := store.get(link.Code); errors.Is(err, ErrLinkNotFound) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("startup cleanup did not delete expired link")
		}
		time.Sleep(time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expiry cleanup did not stop after cancellation")
	}

	if got, ok := counterMetricValue(registry, "zibs_expired_links_deleted_total", nil); !ok || got != 1 {
		t.Errorf("expired links deleted = %v, found %t; want 1", got, ok)
	}
	if got, ok := histogramMetricCount(registry, "zibs_expiry_cleanup_duration_seconds", map[string]string{"result": "success"}); !ok || got != 1 {
		t.Errorf("successful cleanup observations = %d, found %t; want 1", got, ok)
	}
}

func TestExpiryCleanupRunsOnSchedule(t *testing.T) {
	metrics, _ := newTestMetrics(t)
	store := newTestStoreWithMetrics(t, metrics)
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	var currentUnix atomic.Int64
	currentUnix.Store(now.Unix())
	store.now = func() time.Time { return time.Unix(currentUnix.Load(), 0).UTC() }
	link, err := store.create("https://example.com")
	if err != nil {
		t.Fatalf("create link: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runExpiryCleanup(ctx, store, time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)), metrics)
	}()
	defer func() {
		cancel()
		<-done
	}()

	currentUnix.Store(link.ExpiresAt.Unix())
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := store.get(link.Code); errors.Is(err, ErrLinkNotFound) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("scheduled cleanup did not delete expired link")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestExpiryCleanupRecordsSuccessfulZeroDelete(t *testing.T) {
	metrics, registry := newTestMetrics(t)
	store := newTestStoreWithMetrics(t, metrics)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runExpiryCleanup(ctx, store, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)), metrics)
	}()

	deadline := time.Now().Add(time.Second)
	for {
		if got, ok := histogramMetricCount(registry, "zibs_expiry_cleanup_duration_seconds", map[string]string{"result": "success"}); ok && got == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("zero-delete cleanup did not record a success duration")
		}
		time.Sleep(time.Millisecond)
	}

	cancel()
	<-done
	if got, ok := counterMetricValue(registry, "zibs_expired_links_deleted_total", nil); !ok || got != 0 {
		t.Errorf("expired links deleted = %v, found %t; want 0", got, ok)
	}
}

func TestExpiryCleanupRecordsError(t *testing.T) {
	metrics, registry := newTestMetrics(t)
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	store := newLinkStore(db, metrics)
	if err := db.Close(); err != nil {
		t.Fatalf("close test database: %v", err)
	}

	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runExpiryCleanup(ctx, store, time.Hour, slog.New(slog.NewTextHandler(&logs, nil)), metrics)
	}()

	deadline := time.Now().Add(time.Second)
	for {
		if got, ok := histogramMetricCount(registry, "zibs_expiry_cleanup_duration_seconds", map[string]string{"result": "error"}); ok && got == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("failed cleanup did not record an error duration")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done

	if !strings.Contains(logs.String(), "expiry cleanup failed") {
		t.Errorf("logs = %q, want cleanup failure", logs.String())
	}
}

func TestExpiryCleanupDoesNotRecordCanceledRun(t *testing.T) {
	metrics, registry := newTestMetrics(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var logs bytes.Buffer

	runExpiryCleanup(ctx, newTestStoreWithMetrics(t, metrics), time.Hour, slog.New(slog.NewTextHandler(&logs, nil)), metrics)

	if _, ok := histogramMetricCount(registry, "zibs_expiry_cleanup_duration_seconds", map[string]string{"result": "error"}); ok {
		t.Error("canceled cleanup recorded an error duration")
	}
	if logs.Len() != 0 {
		t.Errorf("logs = %q, want no cleanup log", logs.String())
	}
}
