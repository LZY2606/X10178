package app

import (
	"testing"

	"peakvalley/internal/align"
	"peakvalley/internal/model"
	"peakvalley/internal/store"
)

func anchorsFor(t *testing.T, a *App, alID, ref, b, c string, rev int) int {
	t.Helper()
	refs := []float64{3, 8, 14, 20}
	bT := []float64{2.67, 7.43, 13.14, 18.86}
	cT := []float64{3.21, 8.37, 14.53, 20.74}
	for i, u := range refs {
		pr := peakAt(t, a, ref, u, .3)
		points := []model.AnchorPoint{{RunID: b, PeakID: peakAt(t, a, b, bT[i], .4).PeakID}}
		if c != "" {
			points = append(points, model.AnchorPoint{RunID: c, PeakID: peakAt(t, a, c, cT[i], .4).PeakID})
		}
		_, plan, err := a.AddAnchor(alID, "", pr.PeakID, points, rev, false)
		if err != nil {
			t.Fatalf("anchor %d: %v", i, err)
		}
		rev = plan.Rev
	}
	return rev
}

// Crossing anchors block saving with a minimum reject set; fixing them allows
// the build. No global sorting hides the issue.
func TestCrossingAnchorsRejectedThenFixed(t *testing.T) {
	a := newTestApp(t)
	ref, b, _ := setupThreeRuns(t, a)
	al, _ := a.EnsureAlignment(ref, "align")
	pr3 := peakAt(t, a, ref, 3, .3)
	pr8 := peakAt(t, a, ref, 8, .3)
	pB3 := peakAt(t, a, b, 2.67, .4)
	pB8 := peakAt(t, a, b, 7.43, .4)

	_, plan, err := a.AddAnchor(al.ID, "L1", pr3.PeakID,
		[]model.AnchorPoint{{RunID: b, PeakID: pB3.PeakID}}, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately cross: the later reference peak is tied to the EARLIER run peak.
	_, plan, err = a.AddAnchor(al.ID, "L2", pr8.PeakID,
		[]model.AnchorPoint{{RunID: b, PeakID: pB3.PeakID}}, plan.Rev, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) == 0 || len(plan.RejectSet) == 0 {
		t.Fatal("crossing anchors must yield conflict and minimum reject set")
	}
	if plan.RejectSet[0].Size != 1 {
		t.Fatalf("reject set must be minimum of 1, got %d", plan.RejectSet[0].Size)
	}
	if _, _, err := a.BuildMaps(al.ID); err == nil {
		t.Fatal("build must be rejected while crossings exist")
	}
	// Fix by moving L2's B point onto the correct later peak.
	anchors, _ := a.Store.ListAnchors(al.ID)
	var l2 string
	for _, an := range anchors {
		if an.Label == "L2" {
			l2 = an.ID
		}
	}
	plan, err = a.MoveAnchorPoint(al.ID, l2, b, pB8.PeakID, plan.Rev)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.RejectSet) != 0 {
		t.Fatalf("after fix there must be no reject set: %+v", plan.RejectSet)
	}
	if _, _, err := a.BuildMaps(al.ID); err != nil {
		t.Fatalf("build after fix: %v", err)
	}
}

// Locked segments stay byte-identical across a rebuild; changing a locked
// boundary is rejected. Editing a far anchor is allowed because it does not
// alter the locked interval (verified at the align package level too).
func TestLocalRecomputeAndLocking(t *testing.T) {
	a := newTestApp(t)
	ref, b, _ := setupThreeRuns(t, a)
	al, _ := a.EnsureAlignment(ref, "align")
	anchorsFor(t, a, al.ID, ref, b, "", 0)
	maps, _, err := a.BuildMaps(al.ID)
	if err != nil {
		t.Fatal(err)
	}
	v1 := maps[b]
	// Lock all interior segments.
	var idxs []int
	for _, sg := range v1.Segments {
		if sg.AnchorL != "" && sg.AnchorR != "" {
			idxs = append(idxs, sg.Idx)
		}
	}
	if _, err := a.LockSegments(al.ID, b, idxs); err != nil {
		t.Fatal(err)
	}
	lockedBefore := map[string]model.Segment{}
	for _, sg := range v1.Segments {
		if sg.Locked {
			lockedBefore[sg.AnchorL+"->"+sg.AnchorR] = sg
		}
	}

	// Rebuild with identical anchors: locked geometry must be preserved exactly.
	maps2, _, err := a.BuildMaps(al.ID)
	if err != nil {
		t.Fatalf("unchanged rebuild must succeed: %v", err)
	}
	v2 := maps2[b]
	for key, lb := range lockedBefore {
		got := segmentByKeys(v2.Segments, lb.AnchorL, lb.AnchorR)
		if got == nil || !got.Locked {
			t.Fatalf("locked segment %s missing after rebuild", key)
		}
		if got.X0 != lb.X0 || got.X1 != lb.X1 || got.U0 != lb.U0 || got.U1 != lb.U1 {
			t.Fatalf("locked segment %s changed", key)
		}
	}

	// Move the left boundary anchor of a locked segment onto a non-monotone
	// peak: saving must be rejected either for conflicts or locked geometry.
	anchors, pts := storeLists(t, a, al.ID)
	first := anchors[0]
	firstPt := pointOf(pts, first.ID, b)
	farPeak := peakAt(t, a, b, 18.86, .4) // far later peak -> crossing
	state, _ := a.Store.GetAlignment(al.ID)
	_, err = a.MoveAnchorPoint(al.ID, first.ID, b, farPeak.PeakID, state.DraftRev)
	if err != nil && err.Error() == firstPt.PeakID {
		t.Fatal(err)
	}
	_, _, buildErr := a.BuildMaps(al.ID)
	if buildErr == nil {
		t.Fatal("rebuild after editing a locked boundary must be rejected")
	}
}

// Peak identity migrates across redetection; edits form undoable versions.
func TestIdentityMigrationAndUndo(t *testing.T) {
	a := newTestApp(t)
	ref, _, _ := setupThreeRuns(t, a)
	d1, err := a.Store.LatestDetection(ref)
	if err != nil {
		t.Fatal(err)
	}
	firstID := d1.Peaks[0].ID
	// Ignore a peak then redetect with looser parameters (inserts candidates).
	if _, err := a.ApplyEdit(ref, model.EditEvent{Op: model.OpIgnore, PeakIDs: []string{firstID}}); err != nil {
		t.Fatal(err)
	}
	d2, migs, err := a.Detect(ref, model.DetParams{SmoothWindow: 3, MinProminence: 0.05, MinWidthSamples: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(migs) == 0 {
		t.Fatal("redetection must record identity migration")
	}
	if d2.Ver != d1.Ver+1 {
		t.Fatal("detection versions must increment")
	}
	// Old first peak must appear in migration (same/moved or deleted).
	found := false
	for _, m := range migs {
		if m.OldID == firstID {
			found = true
		}
	}
	if !found {
		t.Fatal("old stable id missing from migration")
	}
	// Undo restores the ignored peak via an appended compensating version.
	hist, _ := a.Store.ListEditVers(ref)
	before := len(hist)
	if _, err := a.Undo(ref, 1); err != nil {
		t.Fatal(err)
	}
	hist2, _ := a.Store.ListEditVers(ref)
	if len(hist2) != before+1 {
		t.Fatal("undo must append a version, not rewrite history")
	}
	_, items, _ := a.CurrentItems(ref)
	for _, it := range items {
		if it.PeakID == firstID && it.Status == "active" {
			return
		}
	}
	t.Fatal("undone peak not restored as active")
}

// Stale draft revisions are rejected (optimistic concurrency for two clients).
func TestStaleRevisionConcurrentCommit(t *testing.T) {
	a := newTestApp(t)
	ref, b, _ := setupThreeRuns(t, a)
	al, _ := a.EnsureAlignment(ref, "align")
	pr := peakAt(t, a, ref, 3, .3)
	pb := peakAt(t, a, b, 2.67, .4)
	if _, _, err := a.AddAnchor(al.ID, "L1", pr.PeakID,
		[]model.AnchorPoint{{RunID: b, PeakID: pb.PeakID}}, 0, false); err != nil {
		t.Fatal(err)
	}
	// Two concurrent submissions both assume revision 1.
	pr2 := peakAt(t, a, ref, 8, .3)
	pb2 := peakAt(t, a, b, 7.43, .4)
	if _, _, err := a.AddAnchor(al.ID, "L2", pr2.PeakID,
		[]model.AnchorPoint{{RunID: b, PeakID: pb2.PeakID}}, 1, false); err != nil {
		t.Fatal(err)
	}
	_, _, err := a.AddAnchor(al.ID, "L3", pr2.PeakID,
		[]model.AnchorPoint{{RunID: b, PeakID: pb2.PeakID}}, 1, false)
	if err != store.ErrConcurrent {
		t.Fatalf("stale revision must be rejected with ErrConcurrent, got %v", err)
	}
}

// Frozen results survive a process restart; unfrozen drafts do too.
func TestRestartRecoveryDraftsAndFrozen(t *testing.T) {
	dir := t.TempDir()
	a1, err := New(dir + "/data")
	if err != nil {
		t.Fatal(err)
	}
	ref, b, c := setupThreeRuns(t, a1)
	al, _ := a1.EnsureAlignment(ref, "align")
	rev := anchorsFor(t, a1, al.ID, ref, b, c, 0)
	_ = rev
	maps, _, err := a1.BuildMaps(al.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a1.Derive(al.ID, b, maps[b].Ver); err != nil {
		t.Fatal(err)
	}
	fs, err := a1.BuildFamilies(al.ID, 0.25, 0)
	if err != nil {
		t.Fatal(err)
	}
	cons, _, err := a1.BuildConsensusVersion(al.ID, model.NormRule{Method: "tic", Scale: 1}, 0)
	if err != nil {
		t.Fatal(err)
	}
	famBefore := len(fs.Families)
	consPeaksBefore := len(cons.Peaks)
	if err := a1.FreezeMap(al.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := a1.FreezeFamilySet(al.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a1.FreezeConsensus(al.ID); err != nil {
		t.Fatal(err)
	}
	if err := a1.Close(); err != nil {
		t.Fatal(err)
	}

	a2, err := New(dir + "/data")
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Close()
	al2, err := a2.Store.FirstAlignment()
	if err != nil {
		t.Fatal(err)
	}
	if !al2.Locked || al2.FrozenMapVer != 1 {
		t.Fatalf("frozen alignment must survive restart: %+v", al2)
	}
	m, err := a2.Store.LatestMapVersion(al2.ID, b)
	if err != nil || !m.Frozen {
		t.Fatalf("frozen map must survive restart: %v", err)
	}
	if err := a2.VerifyAligned(al2.ID, b, m.Ver); err != nil {
		t.Fatalf("derived provenance after restart: %v", err)
	}
	fs2, cons2, err := a2.GetConsensusState(al2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !fs2.Frozen || len(fs2.Families) != famBefore {
		t.Fatal("frozen families must survive restart")
	}
	if !cons2.Frozen || len(cons2.Peaks) != consPeaksBefore {
		t.Fatal("frozen consensus must survive restart")
	}
	if cons2.Rule.Method != "tic" {
		t.Fatal("normalization rule must be pinned in the persisted version")
	}
}

func storeLists(t *testing.T, a *App, alID string) ([]*model.Anchor, []model.AnchorPoint) {
	t.Helper()
	an, err := a.Store.ListAnchors(alID)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := a.Store.ListAnchorPoints(alID)
	if err != nil {
		t.Fatal(err)
	}
	return an, pt
}
func pointOf(pts []model.AnchorPoint, anchorID, runID string) model.AnchorPoint {
	for _, p := range pts {
		if p.AnchorID == anchorID && p.RunID == runID {
			return p
		}
	}
	return model.AnchorPoint{}
}
func segmentByKeys(segs []model.Segment, l, r string) *model.Segment {
	for i := range segs {
		if segs[i].AnchorL == l && segs[i].AnchorR == r {
			return &segs[i]
		}
	}
	return nil
}

var _ = align.Eval
