// Package store persists metadata in SQLite and raw/derived series as blobs.
package store

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

// Store is the SQLite + blob persistence layer.
type Store struct {
	db  *sql.DB
	dir string
	mu  sync.Mutex // serialises writes; SQLite is opened single-connection
}

// Open opens (creating if needed) the project database and blob directory.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dataDir, "blobs"), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "peakvalley.db")+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(6)
	s := &Store{db: db, dir: dataDir}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the handle for package-local repositories.
func (s *Store) DB() *sql.DB { return s.db }

// Lock returns the write mutex; App holds it across multi-table transactions.
func (s *Store) Lock() *sync.Mutex { return &s.mu }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS schema_meta (key TEXT PRIMARY KEY, val TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS runs (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, created_at TEXT NOT NULL,
			times_blob TEXT NOT NULL, signal_blob TEXT NOT NULL, n_points INTEGER NOT NULL,
			t0 REAL NOT NULL, t1 REAL NOT NULL, dt REAL NOT NULL,
			meta_json TEXT NOT NULL, raw_digest TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS detections (
			id TEXT PRIMARY KEY, run_id TEXT NOT NULL, ver INTEGER NOT NULL,
			created_at TEXT NOT NULL, params_json TEXT NOT NULL,
			peaks_json TEXT NOT NULL,
			UNIQUE(run_id, ver))`,
		`CREATE TABLE IF NOT EXISTS migrations (
			id INTEGER PRIMARY KEY AUTOINCREMENT, run_id TEXT NOT NULL,
			old_det_ver INTEGER NOT NULL, new_det_ver INTEGER NOT NULL,
			mig_json TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS edit_vers (
			id TEXT PRIMARY KEY, run_id TEXT NOT NULL, ver INTEGER NOT NULL,
			base_det_id TEXT NOT NULL, base_det_ver INTEGER NOT NULL,
			created_at TEXT NOT NULL, event_json TEXT NOT NULL,
			undone_of_ver INTEGER NOT NULL DEFAULT 0, items_json TEXT NOT NULL,
			UNIQUE(run_id, ver))`,
		`CREATE TABLE IF NOT EXISTS alignments (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, ref_run_id TEXT NOT NULL,
			created_at TEXT NOT NULL, map_ver INTEGER NOT NULL DEFAULT 0,
			frozen_map_ver INTEGER NOT NULL DEFAULT 0, locked INTEGER NOT NULL DEFAULT 0,
			plan_rev INTEGER NOT NULL DEFAULT 0, draft_rev INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS anchors (
			id TEXT PRIMARY KEY, alignment_id TEXT NOT NULL, label TEXT NOT NULL,
			ref_peak_id TEXT NOT NULL, ref_t REAL NOT NULL, auto INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(alignment_id, ref_peak_id))`,
		`CREATE TABLE IF NOT EXISTS anchor_points (
			anchor_id TEXT NOT NULL, run_id TEXT NOT NULL, peak_id TEXT NOT NULL,
			t REAL NOT NULL, manual INTEGER NOT NULL,
			PRIMARY KEY(anchor_id, run_id))`,
		`CREATE TABLE IF NOT EXISTS plans (
			alignment_id TEXT NOT NULL, rev INTEGER NOT NULL,
			params_hash TEXT NOT NULL, anchor_ids_json TEXT NOT NULL,
			conflicts_json TEXT NOT NULL, reject_json TEXT NOT NULL,
			PRIMARY KEY(alignment_id, rev))`,
		`CREATE TABLE IF NOT EXISTS map_vers (
			id TEXT PRIMARY KEY, alignment_id TEXT NOT NULL, run_id TEXT NOT NULL,
			ver INTEGER NOT NULL, created_at TEXT NOT NULL, frozen INTEGER NOT NULL,
			plan_rev INTEGER NOT NULL, segments_json TEXT NOT NULL,
			anchors_hash TEXT NOT NULL,
			UNIQUE(alignment_id, run_id, ver))`,
		`CREATE TABLE IF NOT EXISTS map_locks (
			alignment_id TEXT NOT NULL, run_id TEXT NOT NULL, ver INTEGER NOT NULL,
			seg_idx INTEGER NOT NULL, key TEXT NOT NULL,
			PRIMARY KEY(alignment_id, run_id, ver, seg_idx))`,
		`CREATE TABLE IF NOT EXISTS aligneds (
			id TEXT PRIMARY KEY, map_ver_id TEXT NOT NULL, run_id TEXT NOT NULL,
			ver INTEGER NOT NULL, created_at TEXT NOT NULL,
			u_blob TEXT NOT NULL, signal_blob TEXT NOT NULL, n_points INTEGER NOT NULL,
			source_digest TEXT NOT NULL, map_anchors_hash TEXT NOT NULL,
			UNIQUE(map_ver_id, run_id))`,
		`CREATE TABLE IF NOT EXISTS family_sets (
			id TEXT PRIMARY KEY, alignment_id TEXT NOT NULL, ver INTEGER NOT NULL,
			draft INTEGER NOT NULL, frozen INTEGER NOT NULL, created_at TEXT NOT NULL,
			map_ver INTEGER NOT NULL, tol_u REAL NOT NULL,
			families_json TEXT NOT NULL, rev INTEGER NOT NULL,
			UNIQUE(alignment_id, ver))`,
		`CREATE TABLE IF NOT EXISTS consensuses (
			id TEXT PRIMARY KEY, family_set_id TEXT NOT NULL, ver INTEGER NOT NULL,
			draft INTEGER NOT NULL, frozen INTEGER NOT NULL, created_at TEXT NOT NULL,
			rule_json TEXT NOT NULL, peaks_json TEXT NOT NULL,
			run_ids_json TEXT NOT NULL, rev INTEGER NOT NULL,
			UNIQUE(family_set_id, ver))`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("migrate: %w\n%s", err, q)
		}
	}
	return nil
}

// WriteBlob stores content addressed by its sha256 hex digest.
func (s *Store) WriteBlob(digest string, data []byte) (string, error) {
	p := s.blobPath(digest)
	if _, err := os.Stat(p); err == nil {
		return digest, nil
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	return digest, os.Rename(tmp, p)
}

// ReadBlob returns content-addressed bytes.
func (s *Store) ReadBlob(digest string) ([]byte, error) {
	return os.ReadFile(s.blobPath(digest))
}

// BlobExists reports whether a blob is present.
func (s *Store) BlobExists(digest string) bool {
	_, err := os.Stat(s.blobPath(digest))
	return err == nil
}

func (s *Store) blobPath(digest string) string {
	var b []byte
	var err error
	if b, err = hex.DecodeString(digest); err != nil || len(b) != 32 {
		return filepath.Join(s.dir, "blobs", "_"+digest)
	}
	return filepath.Join(s.dir, "blobs", digest[:4], digest)
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
