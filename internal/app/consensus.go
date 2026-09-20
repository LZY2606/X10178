package app

import (
	"fmt"

	"peakvalley/internal/consensus"
	"peakvalley/internal/model"
	"peakvalley/internal/store"
)

// BuildConsensusVersion creates/updates the draft consensus. The normalization
// rule is copied into the version and never changes retroactively.
func (a *App) BuildConsensusVersion(alID string, rule model.NormRule, expectRev int) (*model.Consensus, map[string]float64, error) {
	a.Store.Lock().Lock()
	defer a.Store.Lock().Unlock()
	al, err := a.Store.GetAlignment(alID)
	if err != nil {
		return nil, nil, err
	}
	fs, err := a.Store.LatestFamilySet(alID)
	if err != nil || fs == nil {
		return nil, nil, fmt.Errorf("%w: build families first", errInput)
	}
	runs, err := a.Store.ListRuns()
	if err != nil {
		return nil, nil, err
	}
	var runIDs []string
	for _, r := range runs {
		if r.ID == al.RefRunID {
			continue
		}
		if _, err := a.Store.LatestMapVersion(alID, r.ID); err == nil {
			runIDs = append(runIDs, r.ID)
		}
	}
	runIDs = append([]string{al.RefRunID}, runIDs...)
	if rule.Method == "" {
		rule.Method = "tic"
	}
	if rule.Scale == 0 {
		rule.Scale = 1
	}
	peaks, factors := consensus.BuildConsensus(fs.Families, runIDs, rule)

	tx, err := a.Store.BeginTx()
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if existing, e := a.Store.LatestConsensus(fs.ID); e == nil && existing != nil && existing.Draft {
		if existing.Rev != expectRev {
			return nil, nil, store.ErrConcurrent
		}
		existing.Rule = rule
		existing.Peaks = peaks
		existing.RunIDs = runIDs
		existing.Rev++
		if err := a.Store.UpdateConsensusRev(tx, existing, expectRev); err != nil {
			return nil, nil, err
		}
		return existing, factors, tx.Commit()
	}
	c := &model.Consensus{
		ID: idGen.New(), FamilySetID: fs.ID, Ver: 1, Draft: true,
		CreatedAt: nowUTC(), Rule: rule, Peaks: peaks, RunIDs: runIDs, Rev: 1,
	}
	if err := a.Store.InsertConsensus(tx, c); err != nil {
		return nil, nil, err
	}
	return c, factors, tx.Commit()
}

// FreezeConsensus immutably freezes the current consensus draft.
func (a *App) FreezeConsensus(alID string) (*model.Consensus, error) {
	fs, err := a.Store.LatestFamilySet(alID)
	if err != nil {
		return nil, err
	}
	c, err := a.Store.LatestConsensus(fs.ID)
	if err != nil {
		return nil, err
	}
	a.Store.Lock().Lock()
	defer a.Store.Lock().Unlock()
	tx, err := a.Store.BeginTx()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	c.Draft = false
	c.Frozen = true
	if err := a.Store.UpdateConsensusRev(tx, c, c.Rev); err != nil {
		return nil, err
	}
	return c, tx.Commit()
}

// GetConsensusState loads the newest consensus for the UI.
func (a *App) GetConsensusState(alID string) (*model.FamilySet, *model.Consensus, error) {
	fs, err := a.Store.LatestFamilySet(alID)
	if err != nil {
		return nil, nil, err
	}
	c, _ := a.Store.LatestConsensus(fs.ID)
	return fs, c, nil
}
