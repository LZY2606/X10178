// Package app orchestrates stores, algorithms and provenance bookkeeping.
package app

import (
	"errors"
	"time"

	"peakvalley/internal/store"
)

// App is the application service.
type App struct {
	Store   *store.Store
	DataDir string
}

// New creates an App over a data directory.
func New(dataDir string) (*App, error) {
	st, err := store.Open(dataDir)
	if err != nil {
		return nil, err
	}
	return &App{Store: st, DataDir: dataDir}, nil
}

// Close releases resources.
func (a *App) Close() error { return a.Store.Close() }

func nowUTC() time.Time { return time.Now().UTC() }

var errInput = errors.New("invalid input")
