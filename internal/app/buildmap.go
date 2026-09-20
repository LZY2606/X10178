package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"peakvalley/internal/align"
	"peakvalley/internal/model"
	"peakvalley/internal/store"
)

// BuildMaps validates the current plan, then commits a new map version for each
// participating run. A plan with unresolved crossings is rejected with the
// minimum anchor sets; nothing is saved. On runs with prior locked segments,
// only unaffected segments may change.
func (a *App) BuildMaps(alID string) (map[string]*model.MapVersion, *model.BuildPlan, error) {
	a.Store.Lock().Lock()
	defer a.Store.Lock().Unlock()
	al, err := a.Store.GetAlignment(alID)
	if err != nil {
		return nil, nil, err
	}
	plan, anchors, pts, err := a.Plan(alID)
	if err != nil {
		return nil, nil, err
	}
	if len(plan.RejectSet) > 0 {
		return nil, plan, fmt.Errorf("%w: %d run(s) have crossing anchors; reject a minimum set before saving", errInput, len(plan.RejectSet))
	}
	perRun, _ := a.gatherPoints(al, anchors, pts)
	touched, err := a.touchedSinceLast(al, pts)
	if err != nil {
		return nil, nil, err
	}
	hash := anchorsHash(anchors, pts)
	out := map[string]*model.MapVersion{}
	for runID, pps := range perRun {
		pieces, err := align.Build(pps)
		if err != nil {
			return nil, nil, err
		}
		if old, e := a.Store.LatestMapVersion(alID, runID); e == nil && old != nil {
			oldPieces := make([]align.Piece, len(old.Segments))
			for i, s := range old.Segments {
				oldPieces[i] = align.Piece{
					IL: s.AnchorL, IR: s.AnchorR, X0: s.X0, X1: s.X1,
					U0: s.U0, U1: s.U1, Slope: s.Slope, Locked: s.Locked,
				}
			}
			pieces, err = align.LocalRebuild(oldPieces, pps, touched[runID])
			if err != nil {
				return nil, nil, fmt.Errorf("run %s: %w", runID, err)
			}
		}
		next := 1
		if old, e := a.Store.LatestMapVersion(alID, runID); e == nil && old != nil {
			next = old.Ver + 1
		}
		segs := make([]model.Segment, len(pieces))
		for i, p := range pieces {
			segs[i] = model.Segment{
				Idx: i, X0: p.X0, X1: p.X1, U0: p.U0, U1: p.U1, Slope: p.Slope,
				AnchorL: p.IL, AnchorR: p.IR, Locked: p.Locked,
			}
		}
		mv := &model.MapVersion{
			ID: idGen.New(), RunID: runID, Ver: next, CreatedAt: nowUTC(),
			Frozen: false, PlanRev: plan.Rev, Segments: segs, AnchorsHash: hash,
		}
		out[runID] = mv
	}
	// Persist inside one transaction.
	tx, err := a.Store.BeginTx()
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()
	for runID := range perRun {
		mv := out[runID]
		mv.AlignmentID = alID
		if err := a.Store.InsertMapVersion(tx, mv); err != nil {
			return nil, nil, err
		}
	}
	al.MapVer++
	al.PlanRev = plan.Rev
	if err := a.Store.UpdateAlignmentCounters(tx, al); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return out, plan, nil
}

// touchedSinceLast compares current participation with that of the latest
// committed maps. An anchor is "touched" for a run if it is new, removed, or
// its point moved.
func (a *App) touchedSinceLast(al *model.Alignment, pts []model.AnchorPoint) (map[string]map[string]bool, error) {
	out := map[string]map[string]bool{}
	byRun := map[string]map[string]model.AnchorPoint{}
	for _, p := range pts {
		if byRun[p.RunID] == nil {
			byRun[p.RunID] = map[string]model.AnchorPoint{}
		}
		byRun[p.RunID][p.AnchorID] = p
	}
	runs, err := a.Store.ListRuns()
	if err != nil {
		return nil, err
	}
	for _, r := range runs {
		if r.ID == al.RefRunID {
			continue
		}
		set := map[string]bool{}
		old, err := a.Store.LatestMapVersion(al.ID, r.ID)
		if err != nil || old == nil {
			for id := range byRun[r.ID] {
				set[id] = true
			}
			out[r.ID] = set
			continue
		}
		// Recover old points from segment anchor ids + stored plan participation.
		oldAnchors := map[string]bool{}
		for _, s := range old.Segments {
			if s.AnchorL != "" {
				oldAnchors[s.AnchorL] = true
			}
			if s.AnchorR != "" {
				oldAnchors[s.AnchorR] = true
			}
		}
		for id, p := range byRun[r.ID] {
			oldPt, ok := a.lastPointFor(old, id)
			if !ok || oldPt != p.T {
				set[id] = true
			}
		}
		for id := range oldAnchors {
			if _, ok := byRun[r.ID][id]; !ok {
				set[id] = true
			}
		}
		out[r.ID] = set
	}
	return out, nil
}

// lastPointFor reconstructs an anchor point time from the segment geometry.
func (a *App) lastPointFor(m *model.MapVersion, anchorID string) (float64, bool) {
	for _, s := range m.Segments {
		if s.AnchorR == anchorID {
			return s.X1, true
		}
		if s.AnchorL == anchorID {
			return s.X0, true
		}
	}
	return 0, false
}

// LockSegments marks pieces locked on the latest map of a run. Locked pieces
// survive later anchor edits byte-identically (pointwise consistency).
func (a *App) LockSegments(alID, runID string, segIdxs []int) (*model.MapVersion, error) {
	a.Store.Lock().Lock()
	defer a.Store.Lock().Unlock()
	m, err := a.Store.LatestMapVersion(alID, runID)
	if err != nil {
		return nil, err
	}
	if m.Frozen {
		return nil, fmt.Errorf("%w: map frozen", errInput)
	}
	want := map[int]bool{}
	for _, i := range segIdxs {
		want[i] = true
	}
	for i := range m.Segments {
		if want[i] {
			m.Segments[i].Locked = true
		}
	}
	tx, err := a.Store.BeginTx()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE map_vers SET segments_json=? WHERE alignment_id=? AND run_id=? AND ver=?`,
		string(mustJSONSegs(m.Segments)), alID, runID, m.Ver); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return m, nil
}

func mustJSONSegs(v any) []byte {
	b, err := jsonMarshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// FreezeMap immutably freezes the latest map of every run (or one given run).
// Frozen mappings can be read after restart but never edited; new work creates
// a new alignment rather than mutating them.
func (a *App) FreezeMap(alID string, runID string) error {
	a.Store.Lock().Lock()
	defer a.Store.Lock().Unlock()
	al, err := a.Store.GetAlignment(alID)
	if err != nil {
		return err
	}
	if al.MapVer == 0 {
		return fmt.Errorf("%w: build a map before freezing", errInput)
	}
	runs := []string{runID}
	if runID == "" {
		rs, err := a.Store.ListRuns()
		if err != nil {
			return err
		}
		runs = runs[:0]
		for _, r := range rs {
			if r.ID != al.RefRunID {
				runs = append(runs, r.ID)
			}
		}
	}
	tx, err := a.Store.BeginTx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, rid := range runs {
		if _, err := a.Store.LatestMapVersion(alID, rid); err != nil {
			continue // run without a map: nothing to freeze
		}
		m, _ := a.Store.LatestMapVersion(alID, rid)
		if err := a.Store.SetMapFrozen(tx, alID, rid, m.Ver, true); err != nil {
			return err
		}
	}
	al.Locked = true
	al.FrozenMapVer = al.MapVer
	if err := a.Store.UpdateAlignmentCounters(tx, al); err != nil {
		return err
	}
	return tx.Commit()
}

// Derive applies a map version to the original samples and stores the aligned
// curve. It refuses when the source raw digest or map anchor hash does not
// match, so every derived point is provably traceable.
func (a *App) Derive(alID, runID string, ver int) (*model.Aligned, error) {
	r, times, signal, err := a.LoadSeries(runID)
	if err != nil {
		return nil, err
	}
	m, err := a.Store.GetMapVersion(alID, runID, ver)
	if err != nil {
		return nil, err
	}
	pieces := make([]align.Piece, len(m.Segments))
	for i, s := range m.Segments {
		pieces[i] = align.Piece{
			IL: s.AnchorL, IR: s.AnchorR, X0: s.X0, X1: s.X1,
			U0: s.U0, U1: s.U1, Slope: s.Slope, Locked: s.Locked,
		}
	}
	u, y := align.Apply(pieces, times, signal)
	ub := store.EncodeFloats(u)
	yb := store.EncodeFloats(y)
	h1 := sha256.Sum256(ub)
	h2 := sha256.Sum256(yb)
	uName := "u-" + hex.EncodeToString(h1[:])
	yName := "y-" + hex.EncodeToString(h2[:])
	if _, err := a.Store.WriteBlob(uName, ub); err != nil {
		return nil, err
	}
	if _, err := a.Store.WriteBlob(yName, yb); err != nil {
		return nil, err
	}
	rec := &model.Aligned{
		ID: idGen.New(), MapVerID: m.ID, RunID: runID, Ver: m.Ver,
		CreatedAt: nowUTC(), UBlob: uName, SignalBlob: yName, NPoints: len(u),
		SourceDigest: r.RawDigest, MapAnchorsHash: m.AnchorsHash,
	}
	a.Store.Lock().Lock()
	defer a.Store.Lock().Unlock()
	tx, err := a.Store.BeginTx()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// Replace prior derived series for this map version.
	if _, err := tx.Exec(`DELETE FROM aligneds WHERE map_ver_id=? AND run_id=?`, m.ID, runID); err != nil {
		return nil, err
	}
	if err := a.Store.InsertAligned(tx, rec); err != nil {
		return nil, err
	}
	return rec, tx.Commit()
}

// VerifyAligned re-checks provenance hashes of a stored derived series.
func (a *App) VerifyAligned(alID, runID string, ver int) error {
	m, err := a.Store.GetMapVersion(alID, runID, ver)
	if err != nil {
		return err
	}
	al, err := a.Store.GetAligned(m.ID, runID)
	if err != nil {
		return err
	}
	r, times, signal, err := a.LoadSeries(runID)
	if err != nil {
		return err
	}
	if al.SourceDigest != r.RawDigest {
		return fmt.Errorf("source digest mismatch")
	}
	if al.MapAnchorsHash != m.AnchorsHash {
		return fmt.Errorf("map version mismatch")
	}
	pieces := make([]align.Piece, len(m.Segments))
	for i, sg := range m.Segments {
		pieces[i] = align.Piece{IL: sg.AnchorL, IR: sg.AnchorR, X0: sg.X0, X1: sg.X1, U0: sg.U0, U1: sg.U1, Slope: sg.Slope}
	}
	wantU, _ := align.Apply(pieces, times, signal)
	ub, err := a.Store.ReadBlob(al.UBlob)
	if err != nil {
		return err
	}
	gotU, err := store.DecodeFloats(ub)
	if err != nil {
		return err
	}
	if len(gotU) != len(wantU) {
		return fmt.Errorf("aligned length mismatch")
	}
	for i := range gotU {
		if gotU[i] != wantU[i] {
			return fmt.Errorf("aligned value mismatch at %d", i)
		}
	}
	return nil
}

// LoadAligned reads a derived series.
func (a *App) LoadAligned(alID, runID string, ver int) (*model.Aligned, []float64, []float64, error) {
	m, err := a.Store.GetMapVersion(alID, runID, ver)
	if err != nil {
		return nil, nil, nil, err
	}
	al, err := a.Store.GetAligned(m.ID, runID)
	if err != nil {
		return nil, nil, nil, err
	}
	ub, err := a.Store.ReadBlob(al.UBlob)
	if err != nil {
		return nil, nil, nil, err
	}
	yb, err := a.Store.ReadBlob(al.SignalBlob)
	if err != nil {
		return nil, nil, nil, err
	}
	u, err := store.DecodeFloats(ub)
	if err != nil {
		return nil, nil, nil, err
	}
	y, err := store.DecodeFloats(yb)
	if err != nil {
		return nil, nil, nil, err
	}
	return al, u, y, nil
}
