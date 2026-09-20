package app

import (
	"encoding/json"

	"peakvalley/internal/store"
)

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

func storeErrConcurrent() error { return store.ErrConcurrent }
