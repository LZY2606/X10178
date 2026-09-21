package consensus

import (
	"testing"

	"peakcompass/internal/domain"
	"peakcompass/internal/fixtures"
	"peakcompass/internal/warp"
)

func mp(id string, anchors []domain.Anchor) *domain.Mapping {
	res := warp.Build(id, "run-ref", id, 1, anchors, nil, "")
	m := res.Mapping
	m.Frozen = true
	return m
}

func active(runID string, peaks []domain.Peak) domain.PeakSet {
	ps := domain.PeakSet{ID: "ps-" + runID, RunID: runID, Version: 1, Frozen: true}
	for _, p := range peaks {
		if !p.Rejected {
			cp := p
			cp.Status = domain.PeakActive
			ps.Peaks = append(ps.Peaks, cp)
		}
	}
	return ps
}

func TestBuildFamiliesSupportMissing(t *testing.T) {
	runs := fixtures.All()
	ref := runs[0]

	// Build peak sets by detecting each fixture run.
	mkPeaks := func(r domain.Run) []domain.Peak {
		// detect via warp-independent path: reuse shapes manually from fixtures
		return nil
	}
	_ = mkPeaks

	// Use simple frozen maps that encode each fixture's monotone drift and
	// peak sets whose apex times match the run time axes.
	maps := map[string]*domain.Mapping{
		"run-shift": mp("run-shift", []domain.Anchor{
			{ID: "s1", X: 2.1, Y: 2.302, Kind: "manual"},
			{ID: "s2", X: 4.4, Y: 4.708, Kind: "manual"},
			{ID: "s3", X: 8.0, Y: 8.31, Kind: "manual"},
			{ID: "s4", X: 9.2, Y: 9.532, Kind: "manual"},
		}),
		"run-coarse": mp("run-coarse", []domain.Anchor{
			{ID: "c1", X: 2.1, Y: 2.059, Kind: "manual"},
			{ID: "c2", X: 4.4, Y: 4.336, Kind: "manual"},
			{ID: "c3", X: 8.0, Y: 7.8, Kind: "manual"},
			{ID: "c4", X: 9.2, Y: 9.0, Kind: "manual"},
		}),
		"run-partial": mp("run-partial", []domain.Anchor{
			{ID: "p2", X: 4.4, Y: 4.604, Kind: "manual"},
			{ID: "p3", X: 8.0, Y: 8.13, Kind: "manual"},
			{ID: "p4", X: 9.2, Y: 9.342, Kind: "manual"},
		}),
	}
	mkSet := func(runID string, apexes []float64) domain.PeakSet {
		ps := domain.PeakSet{ID: "ps-" + runID, RunID: runID, Version: 1, Frozen: true}
		for i, at := range apexes {
			ps.Peaks = append(ps.Peaks, domain.Peak{
				ID: string(rune('a'+i)) + runID, ApexTime: at,
				Area: 10, Height: 3, Status: domain.PeakActive,
			})
		}
		return ps
	}
	sets := []domain.PeakSet{
		mkSet("run-shift", []float64{2.302, 4.708, 8.31, 9.532}),
		mkSet("run-coarse", []float64{2.059, 4.336, 7.8, 9.0}),
		mkSet("run-partial", []float64{4.604, 8.13, 9.342}),
	}
	ordered := []domain.Run{runs[1], runs[2], runs[3]}
	mm := []*domain.Mapping{maps["run-shift"], maps["run-coarse"], maps["run-partial"]}

	c, err := Build("cons1", "B1", 1, ref, ordered, mm, sets, NormTotalArea, 0.6, "now")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Peaks) != 4 {
		t.Fatalf("families=%d want 4", len(c.Peaks))
	}
	var early *domain.ConsensusPeak
	for i := range c.Peaks {
		f := &c.Peaks[i]
		if f.RefTime < 3 {
			early = f
		}
		if len(f.Missing) != 0 && (i == 1 || i == 2) {
			t.Fatalf("family %s unexpectedly missing runs %v", f.ID, f.Missing)
		}
	}
	if early == nil {
		t.Fatal("no early family")
	}
	if len(early.Missing) != 1 || early.Missing[0] != "run-partial" {
		t.Fatalf("early family missing=%v want [run-partial]", early.Missing)
	}
	if len(early.Supporting) != 2 {
		t.Fatalf("early support=%d want 2", len(early.Supporting))
	}
	if c.NormRule != NormTotalArea {
		t.Fatal("norm rule must be fixed in the consensus version")
	}
}
