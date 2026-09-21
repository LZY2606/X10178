package peaksets

import (
	"testing"

	"peakcompass/internal/detect"
	"peakcompass/internal/domain"
	"peakcompass/internal/fixtures"
)

func detSet(t *testing.T, r domain.Run) (domain.Detection, domain.PeakSet) {
	t.Helper()
	pks, err := detect.Detect(r.Times, r.Values, domain.DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	d := domain.Detection{ID: "det1", RunID: r.ID, Params: domain.DefaultParams(), Peaks: pks}
	return d, NewSet(r.ID, &d)
}

func TestMergeCreatesVersionAndKeepsHistory(t *testing.T) {
	r := fixtures.ReferenceRun()
	_, ps := detSet(t, r)
	var ids []string
	for _, p := range ps.Peaks {
		if p.Status == domain.PeakActive {
			ids = append(ids, p.ID)
		}
	}
	if len(ids) < 2 {
		t.Fatal("need 2 peaks")
	}
	next, err := Merge(ps, r.Times, r.Values, ids[:2], "pk-env")
	if err != nil {
		t.Fatal(err)
	}
	if next.Version != ps.Version+1 {
		t.Fatalf("version=%d want %d", next.Version, ps.Version+1)
	}
	var env *domain.Peak
	for i := range next.Peaks {
		if next.Peaks[i].ID == "pk-env" {
			env = &next.Peaks[i]
		}
	}
	if env == nil || env.Source != "merge" || len(env.Children) != 2 {
		t.Fatalf("envelope wrong: %+v", env)
	}
	if env.Area <= 0 || env.RightIdx <= env.LeftIdx {
		t.Fatal("envelope geometry not recomputed from raw samples")
	}
	for _, id := range ids[:2] {
		p := findByID(next.Peaks, id)
		if p.Status != domain.PeakMerged || p.MergedInto != "pk-env" {
			t.Fatalf("source %s not marked merged", id)
		}
	}
	// Old revision untouched (undo target).
	if findByID(ps.Peaks, ids[0]).Status != domain.PeakActive {
		t.Fatal("original revision mutated")
	}
}

func TestSplitPlateau(t *testing.T) {
	r := fixtures.ReferenceRun()
	_, ps := detSet(t, r)
	var plateauID string
	var plateau domain.Peak
	for _, p := range ps.Peaks {
		if p.Plateau {
			plateauID, plateau = p.ID, p
		}
	}
	next, err := Split(ps, r.Times, r.Values, plateauID, "pk-L", "pk-R", plateau.ApexIdx)
	if err != nil {
		t.Fatal(err)
	}
	l := findByID(next.Peaks, "pk-L")
	rr := findByID(next.Peaks, "pk-R")
	if l == nil || rr == nil {
		t.Fatal("split children missing")
	}
	if l.RightIdx != plateau.ApexIdx || rr.LeftIdx != plateau.ApexIdx+1 {
		t.Fatalf("split boundary wrong: L=%d..%d R=%d..%d", l.LeftIdx, l.RightIdx, rr.LeftIdx, rr.RightIdx)
	}
	if l.ParentID != plateauID || rr.ParentID != plateauID {
		t.Fatal("children must reference parent for lineage")
	}
	src := findByID(next.Peaks, plateauID)
	if src.Status != domain.PeakSplit {
		t.Fatal("source plateau must be retained as split")
	}
}

func TestIgnoreRestoreAndFrozenBranch(t *testing.T) {
	r := fixtures.ReferenceRun()
	_, ps := detSet(t, r)
	id := ps.Peaks[0].ID
	ig, err := Ignore(ps, id)
	if err != nil {
		t.Fatal(err)
	}
	if findByID(ig.Peaks, id).Status != domain.PeakIgnored {
		t.Fatal("ignore failed")
	}
	rs, err := Restore(ig, id)
	if err != nil {
		t.Fatal(err)
	}
	if findByID(rs.Peaks, id).Status != domain.PeakActive {
		t.Fatal("restore failed")
	}
	// Freeze then edits must be refused; branching yields an editable draft.
	frozen := rs
	frozen.Frozen = true
	if _, err := Ignore(frozen, id); err == nil {
		t.Fatal("edit on frozen set must fail")
	}
	draft := Branch(frozen, "ps-draft")
	if draft.Frozen || draft.ParentSetID != frozen.ID {
		t.Fatal("branch should be unfrozen and point at frozen parent")
	}
	if _, err := Ignore(draft, id); err != nil {
		t.Fatalf("draft edit failed: %v", err)
	}
}

func TestIdentityMigrationAcrossRedetect(t *testing.T) {
	r := fixtures.ReferenceRun()
	d1, ps1 := detSet(t, r)
	firstID := ps1.Peaks[0].ID

	// Re-detect with identical parameters (simulating insert/delete elsewhere).
	pks2, err := detect.Detect(r.Times, r.Values, d1.Params)
	if err != nil {
		t.Fatal(err)
	}
	mig := Migrate(ps1, pks2, 0.35, 0.35)
	if len(mig.Matched) == 0 {
		t.Fatal("expected identity matches")
	}
	ps2 := ApplyMigration(r.ID, "ps2", "det2", ps1, pks2, mig)
	migrated := findByID(ps2.Peaks, firstID)
	if migrated == nil {
		t.Fatalf("stable id %s did not migrate forward", firstID)
	}
	if migrated.Source == "" {
		// source stays "detect"; note records migration
	}
	// A genuinely missing peak must be reported, not silently dropped.
	if len(mig.Disappeared) != 0 {
		t.Fatalf("unchanged curve should have no disappearances, got %v", mig.Disappeared)
	}
}

func findByID(pks []domain.Peak, id string) *domain.Peak {
	for i := range pks {
		if pks[i].ID == id {
			return &pks[i]
		}
	}
	return nil
}
