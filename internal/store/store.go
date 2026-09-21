// Package store persists runs, detections, peak-sets, mappings and consensus
// in SQLite. Raw samples are kept as compact little-endian float64 blobs and
// are never rewritten; derived objects are JSON versions with optimistic
// concurrency so a restart restores both unfrozen drafts and frozen results.
package store

import (
	"database/sql"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// ErrVersion is returned when a commit is based on a stale head version.
var ErrVersion = errors.New("stale base version: concurrent commit detected")

// Store wraps the SQLite database and the project data directory.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at dsn and migrates the schema.
func Open(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

func nowISO() string { return time.Now().In(time.FixedZone("JST", 9*3600)).Format(time.RFC3339) }

func encodeFloats(xs []float64) []byte {
	buf := make([]byte, 8*len(xs))
	for i, x := range xs {
		binary.LittleEndian.PutUint64(buf[i*8:], math.Float64bits(x))
	}
	return buf
}

func decodeFloats(buf []byte) []float64 {
	xs := make([]float64, len(buf)/8)
	for i := range xs {
		xs[i] = math.Float64frombits(binary.LittleEndian.Uint64(buf[i*8:]))
	}
	return xs
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
