package app

import (
	"fmt"

	"peakvalley/internal/align"
	"peakvalley/internal/consensus"
	"peakvalley/internal/model"
)

// BuildFamilies clusters active working-list peaks of all mapped runs into
// cross-run families using the latest map versions.
func (a *App) BuildFamilies(alID string, tolU float64, expectRev int) (*model.FamilySet, error) {
	a.Store.Lock().Lock()
	defer a.Store.Lock().Unlock()
	al, err := a.Store.GetAlignment(alID)
	if err != nil {
		return nil, err
	}
	if al.MapVer == 0 {
		return nil, fmt.Errorf("%w: build maps before families", errInput)
	}
	runs, err := a.Store.ListRuns()
	if err != nil {
		return nil, err
	}
	var members []consensus.MemberInput
	usedMapVer := 0
	for _, run := range runs {
		if run.ID == al.RefRunID {
			// Reference is its own identity map.
			_, items, err := a.CurrentItems(run.ID)
			if err != nil {
				return nil, err
			}
			for _, v := range items {
				if v.Status != "active" {
					continue
				}
				members = append(members, consensus.MemberInput{
					RunID: run.ID, PeakID: v.PeakID, DetVer: v.DetVer,
					EditVer: v.Ver, ApexU: v.ApexT, Area: v.Area, Height: v.Height, Status: v.Status,
				})
			}
			continue
		}
		mv, err := a.Store.LatestMapVersion(alID, run.ID)
		if err != nil || mv == nil {
			continue
		}
		usedMapVer = mv.Ver
		_, items, err := a.CurrentItems(run.ID)
		if err != nil {
			return nil, err
		}
		pieces := make([]align.Piece, len(mv.Segments))
		for i, s := range mv.Segments {
			pieces[i] = align.Piece{IL: s.AnchorL, IR: s.AnchorR, X0: s.X0, X1: s.X1, U0: s.U0, U1: s.U1, Slope: s.Slope}
		}
		for _, v := range items {
			if v.Status != "active" {
				continue
			}
			members = append(members, consensus.MemberInput{
				RunID: run.ID, PeakID: v.PeakID, DetVer: v.DetVer,
				EditVer: v.Ver, ApexU: align.Eval(pieces, v.ApexT), Area: v.Area, Height: v.Height, Status: v.Status,
			})
		}
	}
	groups := consensus.Cluster(members, tolU)
	families := make([]*model.Family, 0, len(groups))
	for i, g := range groups {
		f := &model.Family{ID: idGen.New(), Name: fmt.Sprintf("F%02d", i+1)}
		sum := 0.0
		for _, m := range g {
			sum += m.ApexU
			f.Members = append(f.Members, model.FamMember{
				RunID: m.RunID, PeakID: m.PeakID, DetVer: m.DetVer,
				EditVer: m.EditVer, ApexU: m.ApexU, Area: m.Area, Height: m.Height, Status: m.Status,
			})
		}
		f.CenterU = sum / float64(len(g))
		families = append(families, f)
	}
	tx, err := a.Store.BeginTx()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if existing, e := a.Store.LatestFamilySet(alID); e == nil && existing != nil && existing.Draft {
		existing.Families = families
		existing.TolU = tolU
		existing.Rev++
		if err := a.Store.UpdateFamilySetRev(tx, existing, existing.Rev-1); err != nil {
			return nil, err
		}
		return existing, tx.Commit()
	}
	nextVer := 1
	if existing, e := a.Store.LatestFamilySet(alID); e == nil && existing != nil {
		nextVer = existing.Ver + 1
	}
	fs := &model.FamilySet{
		ID: idGen.New(), Ver: nextVer, Draft: true, CreatedAt: nowUTC(),
		MapVer: usedMapVer, TolU: tolU, Families: families, Rev: 1,
	}
	fs.AlignmentID = alID
	if err := a.Store.InsertFamilySet(tx, fs); err != nil {
		return nil, err
	}
	return fs, tx.Commit()
}

// MergeFamilies / SplitFamily / IgnoreFamilyMember adjust a draft family set
// manually; each adjustment bumps its rev (optimistic concurrency).
func (a *App) MergeFamilies(alID string, familyIDs []string, expectRev int) (*model.FamilySet, error) {
	return a.mutateFamilies(alID, expectRev, func(fs *model.FamilySet) error {
		if len(familyIDs) < 2 {
			return fmt.Errorf("%w: pick >=2 families", errInput)
		}
		byID := map[string]*model.Family{}
		for _, f := range fs.Families {
			byID[f.ID] = f
		}
		var keep *model.Family
		var members []model.FamMember
		sum, n := 0.0, 0
		for i, id := range familyIDs {
			f, ok := byID[id]
			if !ok {
				return fmt.Errorf("%w: family %s", errInput, id)
			}
			if i == 0 {
				keep = f
			}
			members = append(members, f.Members...)
			sum += f.CenterU * float64(len(f.Members))
			n += len(f.Members)
		}
		keep.Members = members
		if n > 0 {
			keep.CenterU = sum / float64(n)
		}
		merge := map[string]bool{}
		for _, id := range familyIDs[1:] {
			merge[id] = true
		}
		out := fs.Families[:0]
		for _, f := range fs.Families {
			if !merge[f.ID] {
				out = append(out, f)
			}
		}
		fs.Families = out
		return nil
	})
}

// IgnoreFamilyMember removes one run's peak from a family (and drops empty
// families). The member remains in history; consensus then marks that run
// missing for the family.
func (a *App) IgnoreFamilyMember(alID, familyID, runID, peakID string, expectRev int) (*model.FamilySet, error) {
	return a.mutateFamilies(alID, expectRev, func(fs *model.FamilySet) error {
		for _, f := range fs.Families {
			if f.ID != familyID {
				continue
			}
			kept := f.Members[:0]
			sum, n := 0.0, 0
			for _, m := range f.Members {
				if m.RunID == runID && m.PeakID == peakID {
					continue
				}
				kept = append(kept, m)
				sum += m.ApexU
				n++
			}
			f.Members = kept
			if n > 0 {
				f.CenterU = sum / float64(n)
			}
		}
		out := fs.Families[:0]
		for _, f := range fs.Families {
			if len(f.Members) > 0 {
				out = append(out, f)
			}
		}
		fs.Families = out
		return nil
	})
}

// FreezeFamilySet turns a draft into an immutable versioned snapshot.
func (a *App) FreezeFamilySet(alID string) (*model.FamilySet, error) {
	a.Store.Lock().Lock()
	defer a.Store.Lock().Unlock()
	fs, err := a.Store.LatestFamilySet(alID)
	if err != nil || fs == nil {
		return nil, fmt.Errorf("%w: no family set", errInput)
	}
	if fs.Frozen {
		return fs, nil
	}
	// Immutable snapshot: insert a new frozen version row with the same content.
	tx, err := a.Store.BeginTx()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	fs.Draft = false
	fs.Frozen = true
	if err := a.Store.UpdateFamilySetRev(tx, fs, fs.Rev); err != nil {
		return nil, err
	}
	return fs, tx.Commit()
}

func (a *App) mutateFamilies(alID string, expectRev int, fn func(*model.FamilySet) error) (*model.FamilySet, error) {
	a.Store.Lock().Lock()
	defer a.Store.Lock().Unlock()
	fs, err := a.Store.LatestFamilySet(alID)
	if err != nil {
		return nil, err
	}
	if !fs.Draft {
		return nil, fmt.Errorf("%w: family set frozen", errInput)
	}
	if fs.Rev != expectRev {
		return nil, storeErrConcurrent()
	}
	if err := fn(fs); err != nil {
		return nil, err
	}
	fs.Rev++
	tx, err := a.Store.BeginTx()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := a.Store.UpdateFamilySetRev(tx, fs, expectRev); err != nil {
		return nil, err
	}
	return fs, tx.Commit()
}
