// Package store keeps Softgaze's focus stretches in SQLite (WAL mode).
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting WAL mode: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS focus_stretches (
			session_id TEXT NOT NULL,
			started_at TEXT NOT NULL,
			focused_seconds INTEGER NOT NULL,
			prompts INTEGER NOT NULL,
			PRIMARY KEY (session_id, started_at)
		);
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating database: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// Prune drops stretches that started more than retentionDays ago.
func (s *Store) Prune(retentionDays int) error {
	cutoff := time.Now().AddDate(0, 0, -retentionDays).UTC().Format(time.RFC3339)
	if _, err := s.db.Exec("DELETE FROM focus_stretches WHERE started_at < ?", cutoff); err != nil {
		return fmt.Errorf("pruning focus stretches: %w", err)
	}
	return nil
}
