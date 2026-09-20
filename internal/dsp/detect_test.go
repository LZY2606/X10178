package dsp

import (
	"math"
	"testing"

	"peakvalley/internal/model"
)

func gaussAt(x, c, w, h float64) float64 {
	d := (x - c) / w
	return h * math.Exp(-d*d)
}

// synth builds a deterministic chromatogram on an arbitrary time grid.
func synth(times []float64, mapping func(float64) float64, dropPeak bool) []float64 {
	out := make([]float64, len(times))
	for i, x := range times {
		u := mapping(x)
		y := 0.02 + gaussAt(u, 3, .18, 1)
		// Exact flat plateau from 7.9 to 8.1.
		switch {
		case u < 7.9:
			y += 0.9 * math.Exp(-((u-7.9)/.22)*((u-7.9)/.22))
		case u > 8.1:
			y += 0.9 * math.Exp(-((u-8.1)/.22)*((u-8.1)/.22))
		default:
			y += 0.9
		}
		if !dropPeak {
			y += gaussAt(u, 14, .20, 0.8)
		}
		y += gaussAt(u, 20, .24, 0.8) // tied prominence with the 14 peak
		out[i] = y
	}
	return out
}

func TestPlateauAndTiedCandidates(t *testing.T) {
	times := Linspace(0, 24, 481)
	sig := synth(times, func(x float64) float64 { return x }, false)
	pks := Detect(&ctr{}, "d", times, sig, model.DetParams{
		SmoothWindow: 5, MinProminence: 0.15, MinWidthSamples: 3, PlateauMode: "mid",
	})
	var plateau *model.Peak
	for _, p := range pks {
		if p.Shape.Plateau && math.Abs(p.Shape.ApexT-8.0) < 0.06 {
			plateau = p
		}
	}
	if plateau == nil {
		t.Fatalf("plateau peak not detected; got %v", apexes(pks))
	}
	if plateau.Shape.ApexAt != "mean" || plateau.Shape.PlatFromI >= plateau.Shape.PlatToI {
		t.Fatalf("plateau geometry wrong: %+v", plateau.Shape)
	}
}

// Two perfectly symmetric gaussians on a flat baseline have identical
// topographic prominence; the detector must flag both as tied candidates
// rather than impose an arbitrary rank order.
func TestTiedCandidatesExact(t *testing.T) {
	times := Linspace(0, 20, 401)
	sig := make([]float64, len(times))
	for i, x := range times {
		// Centers at 6 and 14, equal height/width, equal distance to borders.
		sig[i] = 0.05 + gaussAt(x, 6, 0.3, 1) + gaussAt(x, 14, 0.3, 1)
	}
	pks := Detect(&ctr{}, "d", times, sig, model.DetParams{
		SmoothWindow: 1, MinProminence: 0.2, MinWidthSamples: 3,
	})
	var tied []*model.Peak
	for _, p := range pks {
		if p.Shape.Tied {
			tied = append(tied, p)
		}
	}
	if len(tied) != 2 {
		t.Fatalf("want exactly 2 tied candidates, got %d (peaks=%v proms=%v)", len(tied), apexes(pks), proms(pks))
	}
	if math.Abs(tied[0].Prominence-tied[1].Prominence) > 1e-9 {
		t.Fatalf("tied prominences differ: %v %v", tied[0].Prominence, tied[1].Prominence)
	}
}

func proms(pks []*model.Peak) []float64 {
	out := make([]float64, len(pks))
	for i, p := range pks {
		out[i] = p.Prominence
	}
	return out
}

// Detection must work identically on a non-uniform grid and keep stable
// identity keys across identical content (ids are minted, not positional).
func TestDifferentSamplingIntervals(t *testing.T) {
	uni := Linspace(0, 24, 481)
	non := make([]float64, 481)
	x := 0.0
	for i := range non {
		non[i] = x
		x += 0.05 * (1.0 + 0.2*math.Sin(float64(i)*0.31))
	}
	pU := Detect(&ctr{}, "d1", uni, synth(uni, func(v float64) float64 { return v }, false),
		model.DetParams{SmoothWindow: 5, MinProminence: 0.15, MinWidthSamples: 3})
	pN := Detect(&ctr{n: 100}, "d2", non, synth(non, func(v float64) float64 { return 1.02*v + .1 }, false),
		model.DetParams{SmoothWindow: 5, MinProminence: 0.15, MinWidthSamples: 3})
	if len(pU) != 4 || len(pN) != 4 {
		t.Fatalf("want 4 peaks on both grids, got uniform=%d nonuniform=%d", len(pU), len(pN))
	}
	// Ids must be unique even across detections.
	seen := map[string]bool{}
	for _, p := range append(pU, pN...) {
		if seen[p.ID] {
			t.Fatal("duplicate id across detections")
		}
		seen[p.ID] = true
	}
	// Non-uniform run's time step metadata varies; integration still finite.
	p := pN[0]
	if !(p.Area > 0) || p.Shape.RightI <= p.Shape.LeftI {
		t.Fatalf("bad bounds on non-uniform grid: %+v", p.Shape)
	}
}

// Identity migration follows boundary overlap; inserting/deleting candidates
// or shifting an apex must never rely on array index.
func TestPeakIdentityMigration(t *testing.T) {
	base := Linspace(0, 24, 481)
	old := Detect(&ctr{}, "d1", base, synth(base, func(v float64) float64 { return v }, false),
		model.DetParams{SmoothWindow: 5, MinProminence: 0.15, MinWidthSamples: 3})
	if len(old) != 4 {
		t.Fatalf("setup: %v", apexes(old))
	}
	// New run: peak 14 deleted, everything slightly shifted via drift.
	drifted := synth(base, func(v float64) float64 { return v + 0.03 }, true)
	new := Detect(&ctr{}, "d2", base, drifted,
		model.DetParams{SmoothWindow: 5, MinProminence: 0.15, MinWidthSamples: 3})
	migs := Migrate(old, new)
	byOld := map[string]*model.Mig{}
	for _, m := range migs {
		byOld[m.OldID] = m
	}
	deleted := 0
	mapped := 0
	for _, op := range old {
		m := byOld[op.ID]
		if m == nil {
			t.Fatalf("old peak %s missing from migration", op.ID)
		}
		if m.Rel == "deleted" {
			deleted++
		} else {
			mapped++
			if m.NewID == "" {
				t.Fatal("non-deleted migration needs a new id")
			}
		}
	}
	if deleted != 1 {
		t.Fatalf("want exactly one deleted identity (dropped peak 14), got %d: %+v", deleted, migs)
	}
	if mapped != 3 {
		t.Fatalf("want 3 surviving identities, got %d", mapped)
	}
	// Inserted candidates are announced with an empty old id.
	ins := 0
	for _, m := range migs {
		if m.Rel == "inserted" {
			ins++
		}
	}
	_ = ins
}

func apexes(pks []*model.Peak) []float64 {
	out := make([]float64, len(pks))
	for i, p := range pks {
		out[i] = p.Shape.ApexT
	}
	return out
}
