package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
)

// SeriesDigest computes the content digest of a time/signal pair. Canonical
// form: JSON {"t":[...],"y":[...]} over exact float64 bits.
func SeriesDigest(times, signal []float64) (string, error) {
	if len(times) != len(signal) {
		return "", fmt.Errorf("length mismatch %d vs %d", len(times), len(signal))
	}
	h := sha256.New()
	var buf [8]byte
	put := func(x float64) {
		binary.LittleEndian.PutUint64(buf[:], math.Float64bits(x))
		h.Write(buf[:])
	}
	for _, v := range times {
		put(v)
	}
	for _, v := range signal {
		put(v)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// EncodeFloats serialises a float slice for blob storage (little-endian binary
// with a JSON sidecar header in the first 64 bytes).
func EncodeFloats(v []float64) []byte {
	var b bytes.Buffer
	hdr, _ := json.Marshal(map[string]any{"kind": "f64le", "n": len(v)})
	hb := make([]byte, 64)
	copy(hb, hdr)
	b.Write(hb)
	var buf [8]byte
	for _, x := range v {
		binary.LittleEndian.PutUint64(buf[:], math.Float64bits(x))
		b.Write(buf[:])
	}
	return b.Bytes()
}

// DecodeFloats reads EncodeFloats output.
func DecodeFloats(data []byte) ([]float64, error) {
	if len(data) < 64 {
		return nil, fmt.Errorf("blob too short")
	}
	hdr := map[string]any{}
	end := bytes.IndexByte(data[:64], 0)
	if end < 0 {
		end = 64
	}
	if err := json.Unmarshal(data[:end], &hdr); err != nil {
		return nil, err
	}
	nf, ok := hdr["n"].(float64)
	if !ok {
		return nil, fmt.Errorf("bad header")
	}
	n := int(nf)
	if len(data) != 64+8*n {
		return nil, fmt.Errorf("blob size mismatch")
	}
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		bits := binary.LittleEndian.Uint64(data[64+8*i:])
		out[i] = math.Float64frombits(bits)
	}
	return out, nil
}
