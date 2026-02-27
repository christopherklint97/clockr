package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
}

func Open() (*DB, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("finding home directory: %w", err)
	}

	dir := filepath.Join(home, ".config", "clockr")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}

	dbPath := filepath.Join(dir, "clockr.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	store := &DB{db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return store, nil
}

func (db *DB) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			clockify_id TEXT,
			project_id TEXT NOT NULL,
			project_name TEXT NOT NULL,
			description TEXT NOT NULL,
			start_time DATETIME NOT NULL,
			end_time DATETIME NOT NULL,
			minutes INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'logged',
			raw_input TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS state (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	}

	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			return fmt.Errorf("executing migration: %w", err)
		}
	}

	return nil
}

func (db *DB) GetState(key string) (string, error) {
	var value string
	err := db.QueryRow("SELECT value FROM state WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (db *DB) SetState(key, value string) error {
	_, err := db.Exec(
		"INSERT INTO state (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value,
	)
	return err
}
