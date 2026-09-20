package consensus

import (
	"math"
	"testing"

	"peakvalley/internal/model"
)

func TestSingleLinkageClustering(t *testing.T) {
	members := []MemberInput{
		{RunID: "ref", PeakID: "a", ApexU: 1.00, Area: 10, Height: 1, Status: "active"},
		{RunID: "b", PeakID: "b", ApexU: 1.08, Area: 12, Height: 1, Status: "active"},
		{RunID: "c", PeakID: "c", ApexU: 1.16, Area: 11, Height: 1, Status: "active"}, // chains to b
		{RunID: "ref", PeakID: "d", ApexU: 5.00, Area: 20, Height: 1, Status: "active"},
		{RunID: "b", PeakID: "e", ApexU: 5.05, Area: 19, Height: 1, Status: "active"},
	}
	groups := Cluster(members, 0.1)
	if len(groups) != 2 {
		t.Fatalf("want 2 transitive clusters, got %d", len(groups))
	}
	if len(groups[0]) != 3 {
		t.Fatalf("chain cluster must have 3 members, got %d", len(groups[0]))
	}
	// Deterministic ordering by apex.
	if groups[0][0].ApexU != 1.0 || groups[1][0].ApexU != 5.0 {
		t.Fatal("clusters must be apex-ordered")
	}
	// Outside tolerance they stay separate.
	if g := Cluster(members, 0.05); len(g) != 4 {
		t.Fatalf("tight tolerance yields singletons/pairs, got %d", len(g))
	}
}

func TestConsensusSupportMissingAndNorm(t *testing.T) {
	families := []*model.Family{
		{ID: "f1", Name: "F01", CenterU: 1, Members: []model.FamMember{
			{RunID: "ref", ApexU: 1, Area: 100, Status: "active"},
			{RunID: "b", ApexU: 1.05, Area: 50, Status: "active"},
			{RunID: "b", ApexU: 1.06, Area: 0, Status: "ignored"},
		}},
		{ID: "f2", Name: "F02", CenterU: 5, Members: []model.FamMember{
			{RunID: "ref", ApexU: 5, Area: 200, Status: "active"},
			{RunID: "c", ApexU: 5.02, Area: 200, Status: "active"},
		}},
	}
	runIDs := []string{"ref", "b", "c"}
	tic := model.NormRule{Method: "tic", Scale: 1}
	peaks, factors := BuildConsensus(families, runIDs, tic)
	if len(peaks) != 2 {
		t.Fatalf("want 2 consensus peaks, got %d", len(peaks))
	}
	if peaks[0].Name != "F01" {
		t.Fatal("consensus order by center")
	}
	if len(peaks[0].Supporting) != 2 || len(peaks[0].Missing) != 1 || peaks[0].Missing[0] != "c" {
		t.Fatalf("F01 support/missing wrong: +%v -%v", peaks[0].Supporting, peaks[0].Missing)
	}
	if len(peaks[1].Missing) != 1 || peaks[1].Missing[0] != "b" {
		t.Fatalf("F02 missing must list run b, got %v", peaks[1].Missing)
	}
	// TIC factors: ref=300 (100+200), b=50, c=200. Normalised F02 ref area 200/300, c 200/200=1.
	if math.Abs(factors["ref"]-300) > 1e-9 || math.Abs(factors["b"]-50) > 1e-9 || math.Abs(factors["c"]-200) > 1e-9 {
		t.Fatalf("tic factors wrong: %+v", factors)
	}
	if v := peaks[1].MemberNormArea["c"]; math.Abs(v-1.0) > 1e-9 {
		t.Fatalf("c normalised F02 area should be 1, got %v", v)
	}
	if math.Abs(peaks[1].AreaMean-(200.0/300.0+1.0)/2) > 1e-9 {
		t.Fatalf("mean normalised area wrong: %v", peaks[1].AreaMean)
	}
	// A different, pinned rule changes factors deterministically (versioned).
	med := model.NormRule{Method: "median_peak", Scale: 2}
	_, factors2 := BuildConsensus(families, runIDs, med)
	if math.Abs(factors2["ref"]-150) > 1e-9 { // median of 100,200
		t.Fatalf("median factor for ref should be 150, got %v", factors2["ref"])
	}
	none := NormalizationFactors(families, runIDs, model.NormRule{Method: "none"})
	for _, f := range none {
		if f != 1 {
			t.Fatal("none rule must give factor 1")
		}
	}
}
