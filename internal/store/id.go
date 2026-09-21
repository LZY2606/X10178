package store

import (
	"crypto/rand"
	"encoding/hex"
)

func newPSID() string { return "ps-" + randHex(6) }

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
