package app

import (
	"fmt"

	"peakvalley/internal/dsp"
	"peakvalley/internal/model"
)

// ApplyEdit appends an undoable edit version. Identities of untouched peaks
// are preserved; new ids are minted for merged/split results.
func (a *App) ApplyEdit(runID string, ev model.EditEvent) (*model.EditVer, error) {
	_, times, signal, err := a.LoadSeries(runID)
	if err != nil {
		return nil, err
	}
	det, err := a.Store.LatestDetection(runID)
	if err != nil {
		return nil, fmt.Errorf("detect peaks first")
	}
	a.Store.Lock().Lock()
	defer a.Store.Lock().Unlock()
	tx, err := a.Store.BeginTx()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var items []*model.ViewPeak
	if latest, e := a.Store.LatestEditVer(runID); e == nil && latest != nil {
		items = latest.Items
	} else {
		items = dsp.FromDetection(det.Peaks, det.Ver)
	}
	next, err := dsp.Apply(idGen, times, signal, items, ev)
	if err != nil {
		return nil, err
	}
	ver, err := a.Store.NextEditVer(tx, runID)
	if err != nil {
		return nil, err
	}
	rec := &model.EditVer{
		ID: idGen.New(), RunID: runID, Ver: ver,
		BaseDetID: det.ID, BaseDetVer: det.Ver, CreatedAt: nowUTC(), Event: ev,
		Items: next,
	}
	if err := a.Store.InsertEditVer(tx, rec); err != nil {
		return nil, err
	}
	return rec, tx.Commit()
}

// Undo appends a compensating version that restores the state before the
// target version. Nothing is deleted, so history comparisons remain valid.
func (a *App) Undo(runID string, targetVer int) (*model.EditVer, error) {
	vers, err := a.Store.ListEditVers(runID)
	if err != nil {
		return nil, err
	}
	if targetVer <= 0 || targetVer > len(vers) {
		return nil, fmt.Errorf("%w: version %d", errInput, targetVer)
	}
	target := vers[targetVer-1]
	// Keep the exact detection baseline that the target operated on, even if a
	// newer detection exists (identities are attached to that detection).
	det, err := a.Store.GetDetection(target.BaseDetID)
	if err != nil {
		return nil, err
	}
	var restored []*model.ViewPeak
	if targetVer == 1 || (targetVer > 1 && vers[targetVer-2].BaseDetID != target.BaseDetID) {
		// First edit on this detection baseline -> restore that baseline.
		restored = dsp.FromDetection(det.Peaks, det.Ver)
	} else {
		restored = vers[targetVer-2].Items
	}
	a.Store.Lock().Lock()
	defer a.Store.Lock().Unlock()
	tx, err := a.Store.BeginTx()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	ver, err := a.Store.NextEditVer(tx, runID)
	if err != nil {
		return nil, err
	}
	rec := &model.EditVer{
		ID: idGen.New(), RunID: runID, Ver: ver,
		BaseDetID: det.ID, BaseDetVer: det.Ver, CreatedAt: nowUTC(),
		Event:       model.EditEvent{Op: model.OpUndo, Note: fmt.Sprintf("undo v%d", targetVer)},
		UndoneOfVer: targetVer, Items: restored,
	}
	if err := a.Store.InsertEditVer(tx, rec); err != nil {
		return nil, err
	}
	return rec, tx.Commit()
}
