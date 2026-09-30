package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS links (
			code TEXT PRIMARY KEY,
			destination_url TEXT NOT NULL,
			created_at TEXT NOT NULL,
			expires_at INTEGER NOT NULL,
			redirect_count INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE IF NOT EXISTS creation_tokens (
			id TEXT PRIMARY KEY,
			token_hash BLOB NOT NULL UNIQUE,
			label TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			max_uses INTEGER NOT NULL CHECK (max_uses > 0),
			use_count INTEGER NOT NULL DEFAULT 0,
			revoked_at INTEGER
		);
	`)
	if err != nil {
		return fmt.Errorf("create tables: %w", err)
	}

	return nil
}

// backupDatabase creates a compact, transactionally consistent SQLite snapshot.
// VACUUM INTO is SQLite-aware: unlike copying zibs.db directly, it accounts for
// any journal state while the application remains online.
func backupDatabase(databasePath, destinationPath string) error {
	if _, err := os.Lstat(destinationPath); err == nil {
		return fmt.Errorf("backup destination already exists: %s", destinationPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect backup destination: %w", err)
	}

	temporaryDir, err := os.MkdirTemp(filepath.Dir(destinationPath), ".zibs-backup-")
	if err != nil {
		return fmt.Errorf("create backup temporary directory: %w", err)
	}
	defer os.RemoveAll(temporaryDir)

	temporaryPath := filepath.Join(temporaryDir, "zibs.db")
	db, err := openDB(databasePath)
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.Exec("VACUUM INTO ?", temporaryPath); err != nil {
		return fmt.Errorf("create SQLite backup: %w", err)
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return fmt.Errorf("set backup permissions: %w", err)
	}
	if err := os.Rename(temporaryPath, destinationPath); err != nil {
		return fmt.Errorf("publish backup: %w", err)
	}

	return nil
}
