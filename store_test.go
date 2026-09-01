package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateShortCode(t *testing.T) {
	for range 100 {
		code, err := generateShortCode()
		if err != nil {
			t.Fatalf("generate short code: %v", err)
		}
		if len(code) != shortCodeLength {
			t.Fatalf("code length = %d, want %d", len(code), shortCodeLength)
		}
		for _, character := range code {
			if !strings.ContainsRune(shortCodeAlphabet, character) {
				t.Fatalf("code %q contains character %q outside base-62 alphabet", code, character)
			}
		}
	}
}

func TestCreateRetriesCodeCollision(t *testing.T) {
	store := newTestStore(t)
	codes := []string{"AAAAAAAA", "AAAAAAAA", "BBBBBBBB"}
	store.generateCode = func() (string, error) {
		code := codes[0]
		codes = codes[1:]
		return code, nil
	}

	first, err := store.create("https://first.example")
	if err != nil {
		t.Fatalf("create first link: %v", err)
	}
	second, err := store.create("https://second.example")
	if err != nil {
		t.Fatalf("create second link after collision: %v", err)
	}

	if first.Code != "AAAAAAAA" {
		t.Errorf("first code = %q, want %q", first.Code, "AAAAAAAA")
	}
	if second.Code != "BBBBBBBB" {
		t.Errorf("second code = %q, want %q", second.Code, "BBBBBBBB")
	}
}

func TestCreateReturnsCodeGenerationError(t *testing.T) {
	store := newTestStore(t)
	wantErr := errors.New("random source failed")
	store.generateCode = func() (string, error) {
		return "", wantErr
	}

	_, err := store.create("https://example.com")
	if !errors.Is(err, wantErr) {
		t.Fatalf("create error = %v, want wrapped generator error", err)
	}
}

func TestListEmpty(t *testing.T) {
	store := newTestStore(t)

	links, err := store.list()
	if err != nil {
		t.Fatalf("list links: %v", err)
	}
	if links == nil {
		t.Fatal("links = nil, want non-nil empty slice")
	}
	if len(links) != 0 {
		t.Fatalf("number of links = %d, want 0", len(links))
	}

	encoded, err := json.Marshal(links)
	if err != nil {
		t.Fatalf("marshal links: %v", err)
	}
	if got := string(encoded); got != "[]" {
		t.Errorf("JSON = %q, want %q", got, "[]")
	}
}

func TestFollowAndDeleteMissingLink(t *testing.T) {
	store := newTestStore(t)

	if _, err := store.follow("missing"); !errors.Is(err, ErrLinkNotFound) {
		t.Errorf("follow missing link error = %v, want ErrLinkNotFound", err)
	}
	if err := store.delete("missing"); !errors.Is(err, ErrLinkNotFound) {
		t.Errorf("delete missing link error = %v, want ErrLinkNotFound", err)
	}
}

func TestFollowRejectsExpiredLink(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	link, err := store.create("https://example.com")
	if err != nil {
		t.Fatalf("create link: %v", err)
	}

	now = link.ExpiresAt
	if _, err := store.follow(link.Code); !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("follow expired link error = %v, want ErrLinkNotFound", err)
	}

	stored, err := store.get(link.Code)
	if err != nil {
		t.Fatalf("get expired link before cleanup: %v", err)
	}
	if stored.RedirectCount != 0 {
		t.Errorf("redirect count = %d, want 0", stored.RedirectCount)
	}
}

func TestDeleteExpiredPreservesLiveLinks(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	expired, err := store.create("https://expired.example")
	if err != nil {
		t.Fatalf("create expired link: %v", err)
	}

	now = now.Add(defaultLinkTTL)
	live, err := store.create("https://live.example")
	if err != nil {
		t.Fatalf("create live link: %v", err)
	}

	deleted, err := store.deleteExpired(context.Background())
	if err != nil {
		t.Fatalf("delete expired links: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted links = %d, want 1", deleted)
	}
	if _, err := store.get(expired.Code); !errors.Is(err, ErrLinkNotFound) {
		t.Errorf("get expired link error = %v, want ErrLinkNotFound", err)
	}
	if _, err := store.get(live.Code); err != nil {
		t.Errorf("get live link: %v", err)
	}
}

func TestLinkPersistsAcrossDatabaseReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	db, err := openDB(path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		t.Fatalf("migrate database: %v", err)
	}

	metrics, _ := newTestMetrics(t)
	store := newLinkStore(db, metrics)
	created, err := store.create("https://example.com")
	if err != nil {
		db.Close()
		t.Fatalf("create link: %v", err)
	}
	updated, err := store.follow(created.Code)
	if err != nil {
		db.Close()
		t.Fatalf("follow link: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	db, err = openDB(path)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer db.Close()
	if err := migrate(db); err != nil {
		t.Fatalf("migrate reopened database: %v", err)
	}

	got, err := newLinkStore(db, metrics).get(created.Code)
	if err != nil {
		t.Fatalf("get persisted link: %v", err)
	}
	if got.Code != updated.Code {
		t.Errorf("code = %q, want %q", got.Code, updated.Code)
	}
	if got.DestinationURL != updated.DestinationURL {
		t.Errorf("destination URL = %q, want %q", got.DestinationURL, updated.DestinationURL)
	}
	if !got.CreatedAt.Equal(updated.CreatedAt) {
		t.Errorf("created at = %v, want %v", got.CreatedAt, updated.CreatedAt)
	}
	if got.RedirectCount != 1 {
		t.Errorf("redirect count = %d, want 1", got.RedirectCount)
	}
}

func TestDB(t *testing.T) {
	temp := t.TempDir()
	path := filepath.Join(temp, "test.db")

	db, err := openDB(path)
	if err != nil {
		t.Fatalf("DB could not be opened: %v", err)
	}
	defer db.Close()

	if err = migrate(db); err != nil {
		t.Fatalf("DB migration failed: %v", err)
	}

	const (
		code           = "12234234"
		destinationURL = "https://home.com"
		createdAt      = "2026-08-21T14:16:40.715272+03:00"
		expiresAt      = int64(1797765400)
		redirectCount  = 0
	)
	_, err = db.Exec(`
		INSERT INTO links (code, destination_url, created_at, expires_at, redirect_count)
		VALUES (?, ?, ?, ?, ?)
	`, code, destinationURL, createdAt, expiresAt, redirectCount)
	if err != nil {
		t.Fatalf("DB INSERT failed: %v", err)
	}

	var gotCode, gotDestinationURL, gotCreatedAt string
	var gotExpiresAt int64
	var gotRedirectCount int
	err = db.QueryRow(`
		SELECT code, destination_url, created_at, expires_at, redirect_count
		FROM links
		WHERE code = ?
	`, code).Scan(&gotCode, &gotDestinationURL, &gotCreatedAt, &gotExpiresAt, &gotRedirectCount)
	if err != nil {
		t.Fatalf("DB SELECT failed: %v", err)
	}

	if gotCode != code {
		t.Errorf("code = %q, want %q", gotCode, code)
	}
	if gotDestinationURL != destinationURL {
		t.Errorf("destination URL = %q, want %q", gotDestinationURL, destinationURL)
	}
	if gotCreatedAt != createdAt {
		t.Errorf("created at = %q, want %q", gotCreatedAt, createdAt)
	}
	if gotExpiresAt != expiresAt {
		t.Errorf("expires at = %d, want %d", gotExpiresAt, expiresAt)
	}
	if gotRedirectCount != redirectCount {
		t.Errorf("redirect count = %d, want %d", gotRedirectCount, redirectCount)
	}
}
