package app

import (
	"math"
	"path/filepath"
	"testing"

	"peakvalley/internal/align"
	"peakvalley/internal/model"
)

type ctr struct{ n int }

func (c *ctr) New() string { c.n++; return "id" + itoa(c.n) }
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	b := []byte{}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func gauss(x, c, w, h float64) float64 { d := (x - c) / w; return h * math.Exp(-d*d) }

func lin(n int, tmax float64) []float64 {
	t := make([]float64, n)
	for i := range t {
		t[i] = tmax * float64(i) / float64(n-1)
	}
	return t
}

func sig(times []float64, f func(float64) float64) []float64 {
	y := make([]float64, len(times))
	for i, x := range times {
		y[i] = f(x)
	}
	return y
}

func refShape(u float64) float64 {
	return 0.02 + gauss(u, 3, .18, 1) + gauss(u, 8, .2, .9) + gauss(u, 14, .2, .8) + gauss(u, 20, .22, .8)
}

// setupThreeRuns creates ref + B (drifted) + C (drifted, non-uniform spacing).
func setupThreeRuns(t *testing.T, a *App) (ref, b, c string) {
	t.Helper()
	old := idGen
	idGen = &ctr{}
	defer func() { idGen = old }()

	tr := lin(481, 24)
	mk := func(name string, times []float64, f func(float64) float64) string {
		rn, err := a.ImportRun(name, times, sig(times, f),
			model.Meta{SampleID: name, InstrID: "LC", Column: "COL"})
		if err != nil {
			t.Fatalf("import %s: %v", name, err)
		}
		return rn.ID
	}
	ref = mk("ref", tr, refShape)
	tb := lin(481, 24)
	b = mk("B", tb, func(x float64) float64 { u := 1.05*x + .2; return refShape(u) })
	// Non-uniform C.
	tc := make([]float64, 481)
	x := 0.0
	for i := range tc {
		tc[i] = x
		x += 24.0 / 480.0 * (1.0 + 0.15*math.Sin(float64(i)*0.31))
	}
	tc[len(tc)-1] = 24
	c = mk("C", tc, func(x float64) float64 { u := 0.97*x - .1; return refShape(u) })
	params := model.DetParams{SmoothWindow: 5, MinProminence: 0.15, MinWidthSamples: 3}
	for _, id := range []string{ref, b, c} {
		if _, _, err := a.Detect(id, params); err != nil {
			t.Fatalf("detect %s: %v", id, err)
		}
	}
	return ref, b, c
}

func newTestApp(t *testing.T) *App {
	t.Helper()
	a, err := New(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

// peakAt returns the active view peak nearest time u for a run.
func peakAt(t *testing.T, a *App, runID string, u, tol float64) *model.ViewPeak {
	t.Helper()
	_, items, err := a.CurrentItems(runID)
	if err != nil {
		t.Fatal(err)
	}
	var best *model.ViewPeak
	bd := math.Inf(1)
	for _, p := range items {
		if p.Status != "active" {
			continue
		}
		if d := math.Abs(p.ApexT - u); d < bd && d < tol {
			bd, best = d, p
		}
	}
	if best == nil {
		t.Fatalf("no active peak near %v in %s", u, runID)
	}
	return best
}

// Full alignment workflow with cross-run anchors, build, derive and provenance.
func TestAlignmentEndToEnd(t *testing.T) {
	a := newTestApp(t)
	ref, b, c := setupThreeRuns(t, a)
	al, err := a.EnsureAlignment(ref, "align")
	if err != nil {
		t.Fatal(err)
	}
	// Anchor all four ref peaks; B drifts all of them to larger times.
	refs := []float64{3, 8, 14, 20}
	bT := []float64{2.67, 7.43, 13.14, 18.86}
	cT := []float64{3.21, 8.37, 14.53, 20.74}
	rev := 0
	for i, u := range refs {
		pr := peakAt(t, a, ref, u, .3)
		points := []model.AnchorPoint{
			{RunID: b, PeakID: peakAt(t, a, b, bT[i], .4).PeakID},
			{RunID: c, PeakID: peakAt(t, a, c, cT[i], .4).PeakID},
		}
		_, plan, err := a.AddAnchor(al.ID, "", pr.PeakID, points, rev, false)
		if err != nil {
			t.Fatalf("anchor %d: %v", i, err)
		}
		rev = plan.Rev
		if len(plan.RejectSet) != 0 {
			t.Fatalf("anchor %d unexpected reject: %+v", i, plan.RejectSet)
		}
	}
	maps, plan, err := a.BuildMaps(al.ID)
	if err != nil {
		t.Fatalf("build: %v plan=%+v", err, plan)
	}
	if len(maps) != 2 {
		t.Fatalf("want maps for B and C, got %d", len(maps))
	}
	// Derive + verify provenance.
	if _, err := a.Derive(al.ID, b, maps[b].Ver); err != nil {
		t.Fatalf("derive b: %v", err)
	}
	if err := a.VerifyAligned(al.ID, b, maps[b].Ver); err != nil {
		t.Fatalf("provenance: %v", err)
	}
	// Map must undo the drift: aligned apex of B should approach reference.
	mb := maps[b]
	pieces := make([]align.Piece, len(mb.Segments))
	for i, s := range mb.Segments {
		pieces[i] = align.Piece{IL: s.AnchorL, IR: s.AnchorR, X0: s.X0, X1: s.X1, U0: s.U0, U1: s.U1, Slope: s.Slope}
	}
	// At each run anchor point the map must equal the reference anchor exactly.
	firstSeg := mb.Segments[1]
	if got := align.Eval(pieces, firstSeg.X0); math.Abs(got-firstSeg.U0) > 1e-9 {
		t.Fatalf("anchor endpoint mapped wrong: %v want %v", got, firstSeg.U0)
	}
	// Left extrapolation preserves drift slope 1: u(0) = u0 - x0.
	if got := align.Eval(pieces, 0); math.Abs(got-(firstSeg.U0-firstSeg.X0)) > 1e-9 {
		t.Fatalf("left extrapolation must preserve slope 1, got %v want %v", got, firstSeg.U0-firstSeg.X0)
	}
}
