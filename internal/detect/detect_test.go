package detect

import (
	"testing"

	"peakcompass/internal/domain"
	"peakcompass/internal/fixtures"
)

func activeIDs(pks []domain.Peak) []domain.Peak {
	var out []domain.Peak
	for _, p := range pks {
		if !p.Rejected {
			out = append(out, p)
		}
	}
	return out
}

var _ = activeIDs

func TestReferenceFixtureHasPlateauAndTies(t *testing.T) {
	r := fixtures.ReferenceRun()
	pks, err := Detect(r.Times, r.Values, domain.DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	var plateau *domain.Peak
	for i := range pks {
		if pks[i].Plateau {
			plateau = &pks[i]
		}
	}
	if plateau == nil {
		t.Fatal("expected a plateau peak")
	}
	// Three equal summit samples at indices 44,45,46.
	if plateau.ApexIdx != 44 || plateau.PlateauEnd != 46 {
		t.Fatalf("plateau span=[%d,%d] want [44,46]", plateau.ApexIdx, plateau.PlateauEnd)
	}
	if r.Values[44] != r.Values[45] || r.Values[45] != r.Values[46] {
		t.Fatal("raw plateau samples must remain equal and untouched")
	}
	// Two equal-prominence peaks near 8.0 and 9.2 must both carry a tie rank.
	var tied []domain.Peak
	for _, p := range pks {
		if !p.Rejected && p.TiedRank > 0 {
			tied = append(tied, p)
		}
	}
	if len(tied) != 2 || tied[0].TiedRank != 1 || tied[1].TiedRank != 2 {
		t.Fatalf("tied=%+v want two ranked peers", tied)
	}
}

func TestDifferentSamplingIntervals(t *testing.T) {
	cases := []domain.Run{fixtures.ShiftedRun(), fixtures.CoarseRun()}
	for _, r := range cases {
		pks, err := Detect(r.Times, r.Values, domain.DefaultParams())
		if err != nil {
			t.Fatalf("%s: %v", r.ID, err)
		}
		n := 0
		for _, p := range pks {
			if p.Rejected {
				continue
			}
			n++
			if p.LeftIdx < 0 || p.RightIdx >= len(r.Times) || p.LeftIdx >= p.RightIdx {
				t.Fatalf("%s bad boundaries %+v", r.ID, p)
			}
			if p.Area <= 0 {
				t.Fatalf("%s non-positive area", r.ID)
			}
		}
		if n < 3 {
			t.Fatalf("%s detected %d active peaks, want >=3", r.ID, n)
		}
	}
}

func TestNoiseAndProminenceReject(t *testing.T) {
	// A tiny hump on the baseline must be rejected as low prominence while the
	// tall peak survives.
	times := []float64{0, 1, 2, 3, 4, 5, 6}
	values := []float64{1, 1.05, 1, 4, 1, 1.05, 1}
	pks, err := Detect(times, values, domain.DetParams{NoiseFloor: 1.0, Prominence: 0.8, MinDistanceIdx: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pks {
		if p.Height < 2 && !p.Rejected {
			t.Fatalf("small hump %s should be rejected (%s)", p.ID, p.RejectWhy)
		}
	}
}
