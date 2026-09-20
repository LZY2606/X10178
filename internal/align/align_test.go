package align

import (
	"math"
	"testing"

	"peakvalley/internal/model"
)

// Deterministic fixture: two crossing anchors must be reported as a conflict,
// and the minimum reject set must name exactly one anchor (not hide the issue
// behind a global sort).
func TestCrossingAnchorsMinReject(t *testing.T) {
	pts := []PPoint{
		{AnchorID: "a1", RefT: 1, T: 2},
		{AnchorID: "a2", RefT: 2, T: 1}, // crosses a1
		{AnchorID: "a3", RefT: 3, T: 3},
	}
	conf := FindConflicts("run-B", pts)
	if len(conf) != 1 {
		t.Fatalf("want 1 conflict, got %d: %+v", len(conf), conf)
	}
	if conf[0].A != "a1" || conf[0].B != "a2" {
		t.Fatalf("unexpected conflict pair %+v", conf[0])
	}
	rej := MinRejectSet(pts)
	if len(rej) != 1 {
		t.Fatalf("minimum reject set size must be 1, got %v", rej)
	}
	if rej[0] != "a2" {
		// LIS keeps a1,a3; a2 (the inversion) is deterministically rejected.
		t.Fatalf("want a2 rejected, got %v", rej)
	}
	// After removing the rejected anchor there must be no conflicts.
	kept := []PPoint{pts[0], pts[2]}
	if len(FindConflicts("run-B", kept)) != 0 {
		t.Fatal("reject set did not restore monotonicity")
	}
}

// A chain of three reversed anchors needs two removals minimum.
func TestCrossingChain(t *testing.T) {
	pts := []PPoint{
		{AnchorID: "a", RefT: 1, T: 3},
		{AnchorID: "b", RefT: 2, T: 2},
		{AnchorID: "c", RefT: 3, T: 1},
	}
	rej := MinRejectSet(pts)
	if len(rej) != 2 {
		t.Fatalf("want 2 removals, got %v", rej)
	}
	if rej[0] != "b" || rej[1] != "c" {
		t.Fatalf("deterministic choice broken: %v", rej)
	}
}

// Equal times never count as a crossing (plateau-safe anchors).
func TestEqualTimesDoNotCross(t *testing.T) {
	pts := []PPoint{
		{AnchorID: "a", RefT: 1, T: 2},
		{AnchorID: "b", RefT: 2, T: 2},
		{AnchorID: "c", RefT: 3, T: 3},
	}
	if cs := FindConfertsSafe(pts); len(cs) != 0 {
		t.Fatalf("equal times must not cross, got %+v", cs)
	}
}

// Build must be monotone, extrapolate with slope 1, and map anchor endpoints.
func TestBuildMonotoneAndExtrapolation(t *testing.T) {
	pts := []PPoint{
		{AnchorID: "a", RefT: 1.2, T: 1.0},
		{AnchorID: "b", RefT: 3.4, T: 3.0},
	}
	pieces, err := Build(pts)
	if err != nil {
		t.Fatal(err)
	}
	if len(pieces) != 3 {
		t.Fatalf("want outer+inner+outer = 3 pieces, got %d", len(pieces))
	}
	if pieces[0].Slope != 1 || pieces[2].Slope != 1 {
		t.Fatal("outer pieces must extend with slope 1 to preserve drift")
	}
	// Endpoints map exactly.
	if got := Eval(pieces, 1.0); math.Abs(got-1.2) > 1e-9 {
		t.Fatalf("anchor a: got %v", got)
	}
	if got := Eval(pieces, 3.0); math.Abs(got-3.4) > 1e-9 {
		t.Fatalf("anchor b: got %v", got)
	}
	// Interior is linear with slope 1.1.
	if got := Eval(pieces, 2.0); math.Abs(got-2.3) > 1e-9 {
		t.Fatalf("interior: got %v want 2.3", got)
	}
}

// LocalRebuild keeps untouched locked pieces byte-identical and rejects a
// change whose touched anchor borders a locked piece.
func TestLocalRebuildLocking(t *testing.T) {
	pts := []PPoint{
		{AnchorID: "a", RefT: 1.0, T: 1.0},
		{AnchorID: "b", RefT: 2.0, T: 2.0},
		{AnchorID: "c", RefT: 3.0, T: 3.0},
	}
	pieces, err := Build(pts)
	if err != nil {
		t.Fatal(err)
	}
	// Lock the interior a->b.
	for i := range pieces {
		if pieces[i].IL == "a" && pieces[i].IR == "b" {
			pieces[i].Locked = true
		}
	}
	lockedGeom := pieces[1]

	// Move only anchor c: locked a->b must be retained exactly.
	moved := []PPoint{
		{AnchorID: "a", RefT: 1.0, T: 1.0},
		{AnchorID: "b", RefT: 2.0, T: 2.0},
		{AnchorID: "c", RefT: 3.2, T: 3.0},
	}
	out, err := LocalRebuild(pieces, moved, map[string]bool{"c": true})
	if err != nil {
		t.Fatalf("moving c must not touch locked a->b: %v", err)
	}
	var found bool
	for _, p := range out {
		if p.IL == "a" && p.IR == "b" {
			found = true
			if !p.Locked || p.U0 != lockedGeom.U0 || p.U1 != lockedGeom.U1 ||
				p.X0 != lockedGeom.X0 || p.X1 != lockedGeom.X1 {
				t.Fatalf("locked piece changed: %+v vs %+v", p, lockedGeom)
			}
		}
	}
	if !found {
		t.Fatal("locked piece disappeared")
	}

	// Move anchor b: must be rejected because it borders the locked piece.
	movedB := []PPoint{
		{AnchorID: "a", RefT: 1.0, T: 1.0},
		{AnchorID: "b", RefT: 2.3, T: 2.0},
		{AnchorID: "c", RefT: 3.0, T: 3.0},
	}
	if _, err := LocalRebuild(pieces, movedB, map[string]bool{"b": true}); err == nil {
		t.Fatal("moving a locked-piece boundary must be rejected")
	}
}

func FindConfertsSafe(pts []PPoint) []model.Conflict { return FindConflicts("x", pts) }
