package warp

import (
	"testing"

	"peakcompass/internal/domain"
)

func man(id string, x, y float64) domain.Anchor {
	return domain.Anchor{ID: id, X: x, Y: y, Kind: domain.AnchorManual}
}

func TestBuildAcceptsMonotone(t *testing.T) {
	as := []domain.Anchor{man("a", 1, 1), man("b", 3, 4), man("c", 6, 5)}
	res := Build("m", "ref", "run", 1, as, nil, "now")
	if !res.Valid {
		t.Fatalf("expected valid, got rejected=%v", res.Rejected)
	}
	if len(res.Mapping.Accepted) != 3 {
		t.Fatalf("accepted=%d want 3", len(res.Mapping.Accepted))
	}
}

func TestBuildMinimumCrossingSet(t *testing.T) {
	// Three anchors form an increasing subsequence; exactly one must be
	// rejected, and it must be the crossing anchor c. No global re-sort.
	as := []domain.Anchor{
		man("a", 1, 1), man("b", 2, 3), man("c", 3, 2), man("d", 4, 4),
	}
	res := Build("m", "ref", "run", 1, as, nil, "now")
	if res.Valid {
		t.Fatal("expected invalid mapping")
	}
	if len(res.Rejected) != 1 || res.Rejected[0].AnchorID != "c" || res.Rejected[0].Reason != "cross" {
		t.Fatalf("rejected=%+v, want single {c,cross}", res.Rejected)
	}
	if res.Rejected[0].With != "d" && res.Rejected[0].With != "b" {
		t.Fatalf("rejection should name a crossing peer, got %q", res.Rejected[0].With)
	}
	// Accepted anchors must keep submission x order (no hidden sorting fix).
	for i := 1; i < len(res.Mapping.Accepted); i++ {
		if res.Mapping.Accepted[i].X <= res.Mapping.Accepted[i-1].X {
			t.Fatal("accepted set not strictly increasing in x")
		}
	}
}

func TestBuildTwoCrossingsMinimum(t *testing.T) {
	// Sequence 4,1,3,2 by y: the longest increasing subsequence has length 2
	// (1,3 or 1,2), so the minimum rejection set has size 2.
	as := []domain.Anchor{
		man("a", 1, 4), man("b", 2, 1),
		man("c", 3, 3), man("d", 4, 2),
	}
	res := Build("m", "ref", "run", 1, as, nil, "now")
	if len(res.Rejected) != 2 {
		t.Fatalf("rejected=%d want 2 (minimum): %+v", len(res.Rejected), res.Rejected)
	}
	if len(res.Mapping.Accepted) != 2 {
		t.Fatalf("accepted=%d want 2", len(res.Mapping.Accepted))
	}
}

func TestBuildVerticalAndDuplicate(t *testing.T) {
	as := []domain.Anchor{
		man("a", 1, 1), man("dup", 1, 1), man("v1", 2, 3), man("v2", 2, 9),
	}
	res := Build("m", "ref", "run", 1, as, nil, "now")
	reasons := map[string]string{}
	for _, r := range res.Rejected {
		reasons[r.AnchorID] = r.Reason
	}
	if reasons["dup"] != "duplicate" {
		t.Fatalf("dup reason=%q want duplicate", reasons["dup"])
	}
	if reasons["v1"] != "vertical" || reasons["v2"] != "vertical" {
		t.Fatalf("vertical reasons=%v", reasons)
	}
}

func TestManualWeightPreferredInTie(t *testing.T) {
	// Two equally sized increasing subsequences: [auto0,auto2] (weight 2) vs
	// [man1] (weight 10). The manual anchor must win despite occupying a
	// position that loses a pure cardinality tie-break.
	as := []domain.Anchor{
		{ID: "auto0", X: 1, Y: 2, Kind: domain.AnchorAuto},
		man("man1", 1.5, 10),
		{ID: "auto2", X: 2, Y: 3, Kind: domain.AnchorAuto},
	}
	res := Build("m", "ref", "run", 1, as, nil, "now")
	kept := map[string]bool{}
	for _, a := range res.Mapping.Accepted {
		kept[a.ID] = true
	}
	if !kept["man1"] {
		t.Fatalf("manual anchor should win weighted tie, kept=%v rejected=%+v", kept, res.Rejected)
	}
}

func TestEvalPiecewiseAndClamp(t *testing.T) {
	m := &domain.Mapping{Accepted: []domain.Anchor{man("a", 0, 1), man("b", 10, 11)}}
	if got := Eval(m, 5); got != 6 {
		t.Fatalf("Eval(5)=%v want 6", got)
	}
	if got := Eval(m, -5); got != 1 {
		t.Fatalf("left clamp=%v want 1", got)
	}
	if got := Eval(m, 50); got != 11 {
		t.Fatalf("right clamp=%v want 11", got)
	}
}

func TestAffectedRangeAndLocks(t *testing.T) {
	as := []domain.Anchor{man("a", 0, 0), man("b", 10, 10), man("c", 20, 20)}
	lo, hi := AffectedRange(as, man("b", 10, 10))
	if lo != 5 || hi != 15 {
		t.Fatalf("affected=[%v,%v] want [5,15]", lo, hi)
	}
	locks := []domain.SegmentLock{{ID: "L1", XStart: 14, XEnd: 16}}
	if len(CheckLocks(locks, lo, hi)) != 1 {
		t.Fatal("moving b should conflict with lock L1")
	}
	locksFar := []domain.SegmentLock{{ID: "L2", XStart: 16, XEnd: 18}}
	if len(CheckLocks(locksFar, lo, hi)) != 0 {
		t.Fatal("range [5,15] must not touch lock [16,18]")
	}
}

func TestAlignProvenanceAndUnequalGrid(t *testing.T) {
	m := &domain.Mapping{ID: "m1", Version: 7, Accepted: []domain.Anchor{
		{ID: "a", X: 0, Y: 0, Kind: "manual"}, {ID: "b", X: 10, Y: 10, Kind: "manual"},
	}}
	// target sampled on a coarser, unequally spaced grid
	runT := []float64{0, 1.5, 4.0, 10}
	runV := []float64{1, 2, 5, 11}
	refT := []float64{0, 2, 4, 8, 10}
	pts, err := Align(m, refT, runT, runV)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != len(refT) {
		t.Fatalf("aligned points=%d want %d", len(pts), len(refT))
	}
	for _, p := range pts {
		if p.MappingID != "m1" || p.MapVer != 7 {
			t.Fatalf("provenance lost: %+v", p)
		}
		if p.SrcIdx < 0 || p.SrcIdx >= len(runT) {
			t.Fatalf("source index out of range: %d", p.SrcIdx)
		}
	}
	// identity map at t=2 interpolates target between samples 0..1
	if pts[1].Aligned != 2 || !pts[1].Interp {
		t.Fatalf("point at ref 2: %+v", pts[1])
	}
	if got := pts[1].Value; got != 2+(2-1.5)/(4-1.5)*(5-2) {
		t.Fatalf("interpolated value=%v", got)
	}
}
