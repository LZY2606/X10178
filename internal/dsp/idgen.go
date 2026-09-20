package dsp

// IDGen mints stable identities. Production passes a random generator; tests
// can pass a deterministic counter.
type IDGen interface {
	New() string
}
