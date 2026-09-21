package warp

import (
	"testing"

	"peakcompass/internal/domain"
)

func pkAt(id string, t float64) domain.Peak {
	return domain.Peak{ID: id, ApexTime: t, Status: domain.PeakActive}
}

func TestMatchPeaksMissingEarly(t *testing.T) {
	// Reference has 4 peaks; run lost the first one. Order-preserving match must
	// align the remaining three correctly instead of greedy-shifting.
	ref := []domain.Peak{pkAt("r1", 2.1), pkAt("r2", 4.4), pkAt("r3", 8.0), pkAt("r4", 9.2)}
	run := []domain.Peak{pkAt("t1", 4.6), pkAt("t2", 8.1), pkAt("t3", 9.3)}
	pairs := MatchPeaks(ref, run, 3.0)
	want := [][2]int{{1, 0}, {2, 1}, {3, 2}}
	if len(pairs) != 3 {
		t.Fatalf("pairs=%v want 3", pairs)
	}
	for i := range want {
		if pairs[i] != want[i] {
			t.Fatalf("pair %d=%v want %v (full %v)", i, pairs[i], want[i], pairs)
		}
	}
}

func TestMatchPeaksMonotoneAnchors(t *testing.T) {
	// Shifted run: every ref peak present, pairs must stay monotone.
	ref := []domain.Peak{pkAt("r1", 2.1), pkAt("r2", 4.4), pkAt("r3", 8.0), pkAt("r4", 9.2)}
	run := []domain.Peak{pkAt("t1", 2.3), pkAt("t2", 4.7), pkAt("t3", 8.3), pkAt("t4", 9.5)}
	pairs := MatchPeaks(ref, run, 1.0)
	if len(pairs) != 4 {
		t.Fatalf("pairs=%v", pairs)
	}
	for i := 1; i < len(pairs); i++ {
		if run[pairs[i][1]].ApexTime <= run[pairs[i-1][1]].ApexTime {
			t.Fatal("matched run peaks must be strictly increasing")
		}
	}
}
