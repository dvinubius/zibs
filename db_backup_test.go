package main

import (
	"path/filepath"
	"testing"
)

func TestBackupDatabaseCreatesConsistentCopy(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "source.db")
	source, err := openDB(sourcePath)
	if err != nil {
		t.Fatalf("open source database: %v", err)
	}
	if err := migrate(source); err != nil {
		source.Close()
		t.Fatalf("migrate source database: %v", err)
	}
	if _, err := source.Exec(`
		INSERT INTO links (code, destination_url, created_at, expires_at, redirect_count)
		VALUES ('backup01', 'https://example.com', '2026-08-31T00:00:00Z', 1780000000, 3)
	`); err != nil {
		source.Close()
		t.Fatalf("insert source link: %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatalf("close source database: %v", err)
	}

	backupPath := filepath.Join(t.TempDir(), "zibs-backup.db")
	if err := backupDatabase(sourcePath, backupPath); err != nil {
		t.Fatalf("backup database: %v", err)
	}

	backup, err := openDB(backupPath)
	if err != nil {
		t.Fatalf("open backup database: %v", err)
	}
	defer backup.Close()

	var destination string
	var redirects int
	if err := backup.QueryRow(`SELECT destination_url, redirect_count FROM links WHERE code = 'backup01'`).Scan(&destination, &redirects); err != nil {
		t.Fatalf("query backed-up link: %v", err)
	}
	if destination != "https://example.com" || redirects != 3 {
		t.Fatalf("backed-up link = (%q, %d), want (https://example.com, 3)", destination, redirects)
	}
}

func TestBackupDatabaseRefusesExistingDestination(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "source.db")
	source, err := openDB(sourcePath)
	if err != nil {
		t.Fatalf("open source database: %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatalf("close source database: %v", err)
	}

	destinationPath := filepath.Join(t.TempDir(), "existing.db")
	if err := backupDatabase(sourcePath, destinationPath); err != nil {
		t.Fatalf("create first backup: %v", err)
	}
	if err := backupDatabase(sourcePath, destinationPath); err == nil {
		t.Fatal("backupDatabase succeeded with an existing destination")
	}
}
