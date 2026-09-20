package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"

	"peakvalley/internal/align"
	"peakvalley/internal/model"
)

// EnsureAlignment lazily creates the project alignment against a reference run.
func (a *App) EnsureAlignment(refRunID, name string) (*model.Alignment, error) {
	if al, err := a.Store.FirstAlignment(); err == nil {
		return al, nil
	}
	if _, err := a.Store.GetRun(refRunID); err != nil {
		return nil, fmt.Errorf("reference run: %w", err)
	}
	al := &model.Alignment{
		ID: idGen.New(), Name: name, RefRunID: refRunID, CreatedAt: nowUTC(),
	}
	a.Store.Lock().Lock()
	defer a.Store.Lock().Unlock()
	tx, err := a.Store.BeginTx()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := a.Store.InsertAlignment(tx, al); err != nil {
		return nil, err
	}
	return al, tx.Commit()
}

func (a *App) activeItemsByPeak(runID string) (map[string]*model.ViewPeak, error) {
	_, items, err := a.CurrentItems(runID)
	if err != nil {
		return nil, err
	}
	m := map[string]*model.ViewPeak{}
	for _, v := range items {
		m[v.PeakID] = v
	}
	return m, nil
}

// AddAnchor creates an anchor from a reference peak and optional per-run peaks.
func (a *App) AddAnchor(alID, label, refPeakID string, points []model.AnchorPoint, expectRev int, auto bool) (*model.Anchor, *model.BuildPlan, error) {
	al, err := a.Store.GetAlignment(alID)
	if err != nil {
		return nil, nil, err
	}
	if al.Locked {
		return nil, nil, fmt.Errorf("%w: alignment frozen", errInput)
	}
	refItems, err := a.activeItemsByPeak(al.RefRunID)
	if err != nil {
		return nil, nil, err
	}
	refPk, ok := refItems[refPeakID]
	if !ok || refPk.Status != "active" {
		return nil, nil, fmt.Errorf("%w: reference peak %s", errInput, refPeakID)
	}
	resolved := make([]model.AnchorPoint, 0, len(points))
	for _, p := range points {
		if p.RunID == al.RefRunID {
			continue
		}
		items, err := a.activeItemsByPeak(p.RunID)
		if err != nil {
			return nil, nil, err
		}
		pk, ok := items[p.PeakID]
		if !ok || pk.Status != "active" {
			return nil, nil, fmt.Errorf("%w: run %s peak %s", errInput, p.RunID, p.PeakID)
		}
		p.T = pk.ApexT
		resolved = append(resolved, p)
	}
	a.Store.Lock().Lock()
	defer a.Store.Lock().Unlock()
	tx, err := a.Store.BeginTx()
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := a.Store.CASDraftRev(tx, alID, expectRev); err != nil {
		return nil, nil, err
	}
	an := &model.Anchor{
		ID: idGen.New(), AlignmentID: alID, Label: label,
		RefPeakID: refPeakID, RefT: refPk.ApexT, Auto: auto, CreatedAt: nowUTC(),
	}
	if an.Label == "" {
		an.Label = "A" + an.ID[:6]
	}
	if err := a.Store.InsertAnchor(tx, an); err != nil {
		return nil, nil, err
	}
	for _, p := range resolved {
		p.AnchorID = an.ID
		if err := a.Store.UpsertAnchorPoint(tx, p); err != nil {
			return nil, nil, err
		}
	}
	plan, err := a.replanLocked(tx, al)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return an, plan, nil
}

// DeleteAnchor removes an anchor (and participation). Frozen alignments reject.
func (a *App) DeleteAnchor(alID, anchorID string, expectRev int) (*model.BuildPlan, error) {
	al, err := a.Store.GetAlignment(alID)
	if err != nil {
		return nil, err
	}
	if al.Locked {
		return nil, fmt.Errorf("%w: alignment frozen", errInput)
	}
	a.Store.Lock().Lock()
	defer a.Store.Lock().Unlock()
	tx, err := a.Store.BeginTx()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := a.Store.CASDraftRev(tx, alID, expectRev); err != nil {
		return nil, err
	}
	if err := a.Store.DeleteAnchor(tx, alID, anchorID); err != nil {
		return nil, err
	}
	plan, err := a.replanLocked(tx, al)
	if err != nil {
		return nil, err
	}
	return plan, tx.Commit()
}

// MoveAnchorPoint drags one run participation to a different peak (or clears
// it when peakID == ""). Only affected segments are recomputed at build time;
// locked segments touching the anchor reject the move.
func (a *App) MoveAnchorPoint(alID, anchorID, runID, peakID string, expectRev int) (*model.BuildPlan, error) {
	al, err := a.Store.GetAlignment(alID)
	if err != nil {
		return nil, err
	}
	if al.Locked {
		return nil, fmt.Errorf("%w: alignment frozen", errInput)
	}
	var newT float64
	if peakID != "" {
		items, err := a.activeItemsByPeak(runID)
		if err != nil {
			return nil, err
		}
		pk, ok := items[peakID]
		if !ok || pk.Status != "active" {
			return nil, fmt.Errorf("%w: peak %s", errInput, peakID)
		}
		newT = pk.ApexT
	}
	a.Store.Lock().Lock()
	defer a.Store.Lock().Unlock()
	tx, err := a.Store.BeginTx()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := a.Store.CASDraftRev(tx, alID, expectRev); err != nil {
		return nil, err
	}
	if peakID == "" {
		if err := a.Store.DeleteAnchorPoint(tx, anchorID, runID); err != nil {
			return nil, err
		}
	} else if err := a.Store.UpsertAnchorPoint(tx, model.AnchorPoint{
		AnchorID: anchorID, RunID: runID, PeakID: peakID, T: newT, Manual: true,
	}); err != nil {
		return nil, err
	}
	plan, err := a.replanLocked(tx, al)
	if err != nil {
		return nil, err
	}
	return plan, tx.Commit()
}

// gatherPoints collects per-run anchor points keyed for validation.
func (a *App) gatherPoints(al *model.Alignment, anchors []*model.Anchor, pts []model.AnchorPoint) (map[string][]align.PPoint, map[string]string) {
	refT := map[string]float64{}
	for _, an := range anchors {
		refT[an.ID] = an.RefT
	}
	perRun := map[string][]align.PPoint{}
	anchorPeak := map[string]string{} // runID|anchorID -> peakID
	// Detect two anchors re-bound to the exact same peak in the same run.
	peakUsed := map[string]string{} // runID|peakID -> first anchorID
	for _, p := range pts {
		pp := align.PPoint{AnchorID: p.AnchorID, RefT: refT[p.AnchorID], T: p.T}
		key := p.RunID + "|" + p.PeakID
		if first, ok := peakUsed[key]; ok && first != p.AnchorID {
			pp.SamePeak = true
			// Mark the earlier one too.
			for k := range perRun[p.RunID] {
				if perRun[p.RunID][k].AnchorID == first {
					perRun[p.RunID][k].SamePeak = true
				}
			}
		}
		peakUsed[key] = p.AnchorID
		perRun[p.RunID] = append(perRun[p.RunID], pp)
		anchorPeak[p.RunID+"|"+p.AnchorID] = p.PeakID
	}
	return perRun, anchorPeak
}

// anchorsHash deterministically digests the anchor configuration that a map
// was built from. It versions the mapping input.
func anchorsHash(anchors []*model.Anchor, pts []model.AnchorPoint) string {
	h := sha256.New()
	ordA := append([]*model.Anchor(nil), anchors...)
	sort.Slice(ordA, func(i, j int) bool { return ordA[i].ID < ordA[j].ID })
	for _, an := range ordA {
		fmt.Fprintf(h, "A %s %.17g %s|", an.ID, an.RefT, an.RefPeakID)
	}
	ordP := append([]model.AnchorPoint(nil), pts...)
	sort.Slice(ordP, func(i, j int) bool {
		if ordP[i].RunID != ordP[j].RunID {
			return ordP[i].RunID < ordP[j].RunID
		}
		return ordP[i].AnchorID < ordP[j].AnchorID
	})
	for _, p := range ordP {
		fmt.Fprintf(h, "P %s %s %s %.17g|", p.RunID, p.AnchorID, p.PeakID, p.T)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// replanLocked validates monotonicity and stores a plan revision. It refuses to
// present a saveable plan while minimum reject sets remain.
func (a *App) replanLocked(tx *sql.Tx, al *model.Alignment) (*model.BuildPlan, error) {
	anchors, err := a.Store.ListAnchorsQ(tx, al.ID)
	if err != nil {
		return nil, err
	}
	pts, err := a.Store.ListAnchorPointsQ(tx, al.ID)
	if err != nil {
		return nil, err
	}
	perRun, _ := a.gatherPoints(al, anchors, pts)
	conflicts, rejects := align.ValidatePlan(perRun)
	al.PlanRev++
	var ids []string
	for _, an := range anchors {
		ids = append(ids, an.ID)
	}
	if err := a.Store.InsertPlan(tx, al.ID, al.PlanRev, anchorsHash(anchors, pts), ids, conflicts, rejects); err != nil {
		return nil, err
	}
	al.DraftRev++
	if err := a.Store.UpdateAlignmentCounters(tx, al); err != nil {
		return nil, err
	}
	return &model.BuildPlan{
		Rev: al.PlanRev, ParamsHash: anchorsHash(anchors, pts),
		AnchorIDs: ids, Conflicts: conflicts, RejectSet: rejects,
	}, nil
}

// Plan returns the latest plan with conflicts and minimum reject sets.
func (a *App) Plan(alID string) (*model.BuildPlan, []*model.Anchor, []model.AnchorPoint, error) {
	al, err := a.Store.GetAlignment(alID)
	if err != nil {
		return nil, nil, nil, err
	}
	anchors, err := a.Store.ListAnchors(alID)
	if err != nil {
		return nil, nil, nil, err
	}
	pts, err := a.Store.ListAnchorPoints(alID)
	if err != nil {
		return nil, nil, nil, err
	}
	if al.PlanRev == 0 {
		return &model.BuildPlan{}, anchors, pts, nil
	}
	plan, err := a.Store.GetPlan(alID, al.PlanRev)
	if err != nil {
		return nil, nil, nil, err
	}
	return plan, anchors, pts, nil
}
