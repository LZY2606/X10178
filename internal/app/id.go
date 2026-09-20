package app

import (
	"crypto/rand"
	"encoding/hex"
)

// randIDGen mints unpredictable short hex ids.
type randIDGen struct{}

func (randIDGen) New() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// IDGenerator mints stable ids.
type IDGenerator interface{ New() string }

// idGen is the package-wide generator; tests can replace it.
var idGen IDGenerator = randIDGen{}
