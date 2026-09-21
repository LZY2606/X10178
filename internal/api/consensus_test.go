package api

import (
	"net/http"
	"testing"

	"peakcompass/internal/domain"
)

func TestConsensusSupportMissingAndNormRule(t *testing.T) {
	e := newTestEnv(t)
	// Seeded mappings/peak-sets are frozen for all three non-reference runs.
	body := map[string]any{
		"baseVersion": 0,
		"refRunId":    "run-ref",
		"runIds":      []string{"run-shift", "run-coarse", "run-partial"},
		"normRule":    "total_area",
		"tol":         0.6,
	}
	var c domain.Consensus
	if code, raw := e.do(t, "POST", "/api/consensus/B1/build", body, &c); code != http.StatusCreated {
		t.Fatalf("build code=%d raw=%v", code, raw)
	}
	if c.NormRule != "total_area" {
		t.Fatalf("norm rule not fixed in version: %s", c.NormRule)
	}
	if len(c.Peaks) < 3 {
		t.Fatalf("families=%d want >=3", len(c.Peaks))
	}
	// The early peak (~2.1) is absent in run-partial: that family must list it
	// as missing while still supported by the other two runs.
	foundMissing := false
	for _, f := range c.Peaks {
		if f.RefTime < 3 {
			if len(f.Missing) != 1 || f.Missing[0] != "run-partial" {
				t.Fatalf("early family missing=%v want [run-partial]", f.Missing)
			}
			if len(f.Supporting) < 2 {
				t.Fatalf("early family support=%d want >=2", len(f.Supporting))
			}
			for _, sup := range f.Supporting {
				if sup.NormArea <= 0 || sup.NormArea > sup.RawArea*1.01 {
					// normalized by total area must be <= raw area here
					t.Fatalf("normalization suspicious: raw=%v norm=%v", sup.RawArea, sup.NormArea)
				}
			}
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Fatal("expected an early family with run-partial missing")
	}
	// Persisted and recoverable.
	var head domain.Consensus
	if code, _ := e.do(t, "GET", "/api/consensus/B1/head", nil, &head); code != 200 {
		t.Fatalf("head consensus code=%d", code)
	}
	if head.NormRule != c.NormRule || len(head.Peaks) != len(c.Peaks) {
		t.Fatal("consensus not persisted")
	}
}
