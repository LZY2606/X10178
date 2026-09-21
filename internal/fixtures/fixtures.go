// Package fixtures builds deterministic chromatogram test/demo data.
package fixtures

import (
	"math"

	"peakcompass/internal/domain"
)

type gauss struct {
	Mu, Sigma, Amp float64
}

func eval(gs []gauss, noiseFloor, t float64) float64 {
	v := noiseFloor
	for _, g := range gs {
		d := (t - g.Mu) / g.Sigma
		v += g.Amp * math.Exp(-0.5*d*d)
	}
	return v
}

func sample(gs []gauss, noiseFloor, start, end float64, n int) ([]float64, []float64) {
	times := make([]float64, n)
	vals := make([]float64, n)
	step := (end - start) / float64(n-1)
	for i := range n {
		t := start + step*float64(i)
		times[i] = t
		vals[i] = eval(gs, noiseFloor, t)
	}
	return times, vals
}

// ReferenceRun returns the reference chromatogram. It contains a genuine flat
// plateau peak near 4.55 (three equal samples) and two equally prominent peaks
// near 8.0/9.2 which act as tied candidates.
func ReferenceRun() domain.Run {
	const n = 120
	times := make([]float64, n)
	vals := make([]float64, n)
	step := 0.1 // 0.1 min sampling
	gs := []gauss{
		{Mu: 2.1, Sigma: 0.18, Amp: 3.0},
		{Mu: 4.5, Sigma: 0.22, Amp: 2.2},
		{Mu: 8.0, Sigma: 0.20, Amp: 2.6},
		{Mu: 9.2, Sigma: 0.20, Amp: 2.6},
	}
	for i := range n {
		t := step * float64(i)
		times[i] = t
		vals[i] = eval(gs, 1.0, t)
	}
	// Stamp an exact three-sample flat plateau on the summit of the 4.5 peak:
	// indices 44..46 (t=4.4,4.5,4.6) all take the exact summit value while the
	// neighbouring samples at 4.3 and 4.7 stay strictly lower.
	summit := vals[45]
	vals[44] = summit
	vals[46] = summit
	return domain.Run{
		ID:         "run-ref",
		Name:       "reference-R0",
		Sample:     "S-REF",
		Instrument: "HPLC-A",
		Batch:      "B1",
		Times:      times,
		Values:     vals,
		Meta:       map[string]string{"operator": "lab", "vial": "A1"},
		CreatedAt:  "2026-09-21T09:00:00+09:00",
	}
}

// warped evaluates the reference mixture with all peak means shifted by w(t).
func warped(name, id, instrument string, n int, step float64, w func(float64) float64, createdAt string) domain.Run {
	times := make([]float64, n)
	vals := make([]float64, n)
	gs := []gauss{
		{Mu: 2.1, Sigma: 0.18, Amp: 3.0},
		{Mu: 4.5, Sigma: 0.22, Amp: 2.2},
		{Mu: 8.0, Sigma: 0.20, Amp: 2.6},
		{Mu: 9.2, Sigma: 0.20, Amp: 2.6},
	}
	for i := range n {
		t := step * float64(i)
		times[i] = t
		shifted := make([]gauss, len(gs))
		for k, g := range gs {
			g.Mu = g.Mu + w(g.Mu)
			shifted[k] = g
		}
		vals[i] = eval(shifted, 1.0, t)
	}
	return domain.Run{
		ID: id, Name: name, Sample: "S-" + id, Instrument: instrument,
		Batch: "B1", Times: times, Values: vals,
		Meta:      map[string]string{"operator": "lab"},
		CreatedAt: createdAt,
	}
}

// ShiftedRun is a later run on HPLC-A with a smooth, monotone retention drift.
// It uses the same 0.1 min grid as the reference.
func ShiftedRun() domain.Run {
	return warped("shifted-R1", "run-shift", "HPLC-A", 120, 0.1,
		func(t float64) float64 { return 0.02*t + 0.15 },
		"2026-09-21T10:00:00+09:00")
}

// CoarseRun comes from a different instrument with a wider 0.15 min grid and a
// slightly different drift, exercising unequal sampling intervals.
func CoarseRun() domain.Run {
	return warped("coarse-R2", "run-coarse", "HPLC-B", 80, 0.15,
		func(t float64) float64 { return -0.01*t - 0.08 },
		"2026-09-21T11:00:00+09:00")
}

// PartialRun misses the early peak (it degraded before injection) but keeps the
// other three; used to exercise anchors that only appear in some runs.
func PartialRun() domain.Run {
	r := warped("partial-R3", "run-partial", "HPLC-A", 120, 0.1,
		func(t float64) float64 { return 0.01*t + 0.05 },
		"2026-09-21T12:00:00+09:00")
	// Subtract the first Gaussian contribution from the stored samples.
	g0 := gauss{Mu: 2.1 + (0.01*2.1 + 0.05), Sigma: 0.18, Amp: 3.0}
	for i, t := range r.Times {
		d := (t - g0.Mu) / g0.Sigma
		r.Values[i] -= g0.Amp * math.Exp(-0.5*d*d)
	}
	return r
}

// All returns the demo/test fixture set.
func All() []domain.Run {
	return []domain.Run{ReferenceRun(), ShiftedRun(), CoarseRun(), PartialRun()}
}
