package webapi

import (
	"math"
	"net/http"

	"peakvalley/internal/model"
)

// SeedData is the deterministic demo dataset. The reference contains a
// plateau peak and an equal-prominence tied pair; run B drifts and drops one
// peak; run C uses a non-uniform sampling grid.
type SeedData struct {
	Ref struct {
		Times, Signal []float64
	}
	B struct {
		Times, Signal []float64
	}
	C struct {
		Times, Signal []float64
	}
}

func gauss(x, c, w, h float64) float64 {
	d := (x - c) / w
	return h * math.Exp(-d*d)
}

// BuildSeed returns the deterministic three-run demo.
func BuildSeed() SeedData {
	var d SeedData
	n := 480
	d.Ref.Times = make([]float64, n)
	d.Ref.Signal = make([]float64, n)
	for i := 0; i < n; i++ {
		x := float64(i) * 0.05
		d.Ref.Times[i] = x
		y := 0.02
		y += gauss(x, 3.0, 0.18, 1.0)
		// Plateau peak around 8.0 (flat top).
		y += plateauPeak(x, 7.9, 8.1, 0.22, 0.9)
		y += gauss(x, 14.0, 0.20, 0.8)
		y += gauss(x, 20.0, 0.24, 0.8) // tied prominence pair with 14.0
		d.Ref.Signal[i] = y
	}
	// Run B: monotone drift stretch + shift, one missing peak (drops 14).
	d.B.Times = make([]float64, n)
	d.B.Signal = make([]float64, n)
	for i := 0; i < n; i++ {
		x := float64(i) * 0.05
		d.B.Times[i] = x
		m := 1.06*x + 0.25 // true retention drift (must remain visible)
		y := 0.02
		y += gauss(m, 3.0, 0.18, 1.0)
		y += plateauPeak(m, 7.9, 8.1, 0.22, 0.9)
		// peak at 14 deliberately absent
		y += gauss(m, 20.0, 0.24, 0.8)
		d.B.Signal[i] = y
	}
	// Run C: non-uniform sampling, drift, same four peaks.
	xs := nonUniform(n, 0.05)
	d.C.Times = xs
	d.C.Signal = make([]float64, len(xs))
	for i, x := range xs {
		m := 1.03*x + 0.10
		y := 0.02
		y += gauss(m, 3.0, 0.18, 1.0)
		y += plateauPeak(m, 7.9, 8.1, 0.22, 0.9)
		y += gauss(m, 14.0, 0.20, 0.8)
		y += gauss(m, 20.0, 0.24, 0.8)
		d.C.Signal[i] = y
	}
	return d
}

func plateauPeak(x, a, b, w, h float64) float64 {
	switch {
	case x < a:
		d := (x - a) / w
		return h * math.Exp(-d*d)
	case x > b:
		d := (x - b) / w
		return h * math.Exp(-d*d)
	default:
		return h
	}
}

func nonUniform(n int, base float64) []float64 {
	out := make([]float64, n)
	x := 0.0
	for i := 0; i < n; i++ {
		out[i] = x
		// Deterministic jittered, strictly increasing spacing.
		jit := 1.0 + 0.25*math.Sin(float64(i)*0.37)
		x += base * jit
	}
	return out
}

func (s *Server) handleSeed(w http.ResponseWriter, r *http.Request) {
	if n, _ := s.App.Store.CountRuns(); n > 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "runs already imported"})
		return
	}
	d := BuildSeed()
	type seeded struct {
		Runs []string `json:"runs"`
	}
	var out seeded
	mk := func(name string, t, y []float64, m model.Meta) {
		rn, err := s.App.ImportRun(name, t, y, m)
		if err != nil {
			fail(w, err)
			return
		}
		out.Runs = append(out.Runs, rn.ID)
	}
	mk("参考运行 R", d.Ref.Times, d.Ref.Signal, model.Meta{SampleID: "S-R", SampleName: "QC pool", InstrID: "LC-01", InstrModel: "AcmeLC", Column: "COL-A", Operator: "lab"})
	mk("批次 B", d.B.Times, d.B.Signal, model.Meta{SampleID: "S-B", SampleName: "batch-B", InstrID: "LC-02", InstrModel: "AcmeLC", Column: "COL-A", Operator: "lab"})
	mk("批次 C", d.C.Times, d.C.Signal, model.Meta{SampleID: "S-C", SampleName: "batch-C", InstrID: "LC-01", InstrModel: "AcmeLC", Column: "COL-B", Operator: "lab"})
	detParams := model.DetParams{SmoothWindow: 5, MinProminence: 0.15, MinWidthSamples: 3, PlateauMode: "mid"}
	for _, id := range out.Runs {
		if _, _, err := s.App.Detect(id, detParams); err != nil {
			fail(w, err)
			return
		}
	}
	refID := out.Runs[0]
	al, err := s.App.EnsureAlignment(refID, "批次对齐")
	if err != nil {
		fail(w, err)
		return
	}
	// Anchor automatically on matching detected peaks of B and C; suggestions
	// remain available in the UI for the missing/drifted peak in B.
	if err := seedAnchors(s, al.ID, out.Runs); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
