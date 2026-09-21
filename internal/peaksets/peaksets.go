// Package peaksets implements versioned manual peak operations and identity
// migration across re-detection. Peak identity is always a stable string id.
package peaksets

import (
	"fmt"
	"sort"

	"peakcompass/internal/domain"
)

// NewSet builds a working peak-set from detection output, copying only active
// (non-rejected) candidates and preserving stable ids.
func NewSet(runID string, det *domain.Detection) domain.PeakSet {
	ps := domain.PeakSet{
		ID:         "ps-" + det.ID,
		RunID:      runID,
		Version:    1,
		BasedOnDet: det.ID,
	}
	for _, pk := range det.Peaks {
		if pk.Rejected {
			continue
		}
		cp := pk
		cp.Status = domain.PeakActive
		ps.Peaks = append(ps.Peaks, cp)
	}
	return ps
}

// clone deep-copies a peak-set so revisions share no mutable state.
func clone(ps domain.PeakSet) domain.PeakSet {
	out := ps
	out.Peaks = make([]domain.Peak, len(ps.Peaks))
	for i, pk := range ps.Peaks {
		pk.Children = append([]string(nil), pk.Children...)
		out.Peaks[i] = pk
	}
	return out
}

// findActive returns the active peak with id or an error.
func findActive(ps *domain.PeakSet, id string) (*domain.Peak, error) {
	for i := range ps.Peaks {
		if ps.Peaks[i].ID == id {
			if ps.Peaks[i].Status != domain.PeakActive {
				return nil, fmt.Errorf("peak %s is not active (status %s)", id, ps.Peaks[i].Status)
			}
			return &ps.Peaks[i], nil
		}
	}
	return nil, fmt.Errorf("peak %s not found", id)
}

// Merge combines adjacent active peaks into one envelope peak. Source peaks are
// retained with status "merged" for traceability; the envelope is a new peak.
// Envelope geometry and area are recomputed directly from the raw samples.
func Merge(ps domain.PeakSet, times, values []float64, ids []string, newID string) (domain.PeakSet, error) {
	if len(ids) < 2 {
		return ps, fmt.Errorf("merge needs at least 2 peaks")
	}
	if ps.Frozen {
		return ps, fmt.Errorf("peak-set v%d is frozen; branch before editing", ps.Version)
	}
	out := clone(ps)
	chosen := make([]*domain.Peak, 0, len(ids))
	for _, id := range ids {
		pk, err := findActive(&out, id)
		if err != nil {
			return ps, err
		}
		chosen = append(chosen, pk)
	}
	sort.Slice(chosen, func(a, b int) bool { return chosen[a].ApexIdx < chosen[b].ApexIdx })
	left, right := chosen[0].LeftIdx, chosen[len(chosen)-1].RightIdx
	env := statsFor(times, values, newID, left, right, "merge")
	env.Note = fmt.Sprintf("merge of %v", ids)
	for _, pk := range chosen {
		pk.Status = domain.PeakMerged
		pk.MergedInto = newID
		env.Children = append(env.Children, pk.ID)
	}
	out.Peaks = append(out.Peaks, env)
	out.Version++
	return out, nil
}

// Split breaks a plateau peak into two peaks at the given sample boundary.
// The source peak is retained with status "split"; both children are new ids.
func Split(ps domain.PeakSet, times, values []float64, id, leftID, rightID string, boundaryIdx int) (domain.PeakSet, error) {
	if ps.Frozen {
		return ps, fmt.Errorf("peak-set v%d is frozen; branch before editing", ps.Version)
	}
	out := clone(ps)
	pk, err := findActive(&out, id)
	if err != nil {
		return ps, err
	}
	if !pk.Plateau {
		return ps, fmt.Errorf("split target %s is not a plateau peak", id)
	}
	if boundaryIdx < pk.ApexIdx || boundaryIdx > pk.PlateauEnd-1 {
		return ps, fmt.Errorf("boundary %d outside plateau [%d,%d]", boundaryIdx, pk.ApexIdx, pk.PlateauEnd)
	}
	var left, right domain.Peak
	leftL, leftR := pk.LeftIdx, boundaryIdx
	left = statsFor(times, values, leftID, leftL, leftR, "split")
	left.ParentID, left.Note = pk.ID, fmt.Sprintf("split-left from %s", pk.ID)
	rightL, rightR := boundaryIdx+1, pk.RightIdx
	right = statsFor(times, values, rightID, rightL, rightR, "split")
	right.ParentID, right.Note = pk.ID, fmt.Sprintf("split-right from %s", pk.ID)
	pk.Status = domain.PeakSplit
	pk.Children = []string{leftID, rightID}
	out.Peaks = append(out.Peaks, left, right)
	out.Version++
	return out, nil
}

// Ignore marks an active peak as analyst-ignored; Restore reverses it.
func Ignore(ps domain.PeakSet, id string) (domain.PeakSet, error) {
	if ps.Frozen {
		return ps, fmt.Errorf("peak-set v%d is frozen; branch before editing", ps.Version)
	}
	out := clone(ps)
	pk, err := findActive(&out, id)
	if err != nil {
		return ps, err
	}
	pk.Status = domain.PeakIgnored
	out.Version++
	return out, nil
}

// Restore revives an ignored peak.
func Restore(ps domain.PeakSet, id string) (domain.PeakSet, error) {
	if ps.Frozen {
		return ps, fmt.Errorf("peak-set v%d is frozen; branch before editing", ps.Version)
	}
	out := clone(ps)
	for i := range out.Peaks {
		if out.Peaks[i].ID == id && out.Peaks[i].Status == domain.PeakIgnored {
			out.Peaks[i].Status = domain.PeakActive
			out.Version++
			return out, nil
		}
	}
	return ps, fmt.Errorf("ignored peak %s not found", id)
}

// Branch creates a new unfrozen draft rooted at any (possibly frozen) revision.
func Branch(ps domain.PeakSet, newID string) domain.PeakSet {
	out := clone(ps)
	out.ID = newID
	out.ParentSetID = ps.ID
	out.Frozen = false
	return out
}

// Migration reports how stable ids move from an old active set to fresh
// detection candidates. Matching is deterministic: nearest apex time, then
// nearest area, then nearest index; unmatched old peaks and new candidates are
// reported explicitly instead of being silently dropped.
type Migration struct {
	Matched     []Match  `json:"matched"`
	Disappeared []string `json:"disappeared"`
	New         []string `json:"new"`
}

// Match pairs an existing stable id with a fresh candidate index/peak id.
type Match struct {
	OldID       string  `json:"oldId"`
	CandidateID string  `json:"candidateId"`
	ApexDt      float64 `json:"apexDt"`
	AreaRelErr  float64 `json:"areaRelErr"`
}

// Migrate matches old active peaks against fresh candidates. The caller decides
// the tolerance; this function only proposes deterministic pairings.
func Migrate(old domain.PeakSet, fresh []domain.Peak, maxDt, maxAreaRel float64) Migration {
	var m Migration
	used := map[string]bool{}
	oldActives := make([]domain.Peak, 0, len(old.Peaks))
	for _, pk := range old.Peaks {
		if pk.Status == domain.PeakActive {
			oldActives = append(oldActives, pk)
		}
	}
	var newCands []domain.Peak
	for _, pk := range fresh {
		if !pk.Rejected {
			newCands = append(newCands, pk)
		}
	}
	// Greedy deterministic assignment ordered by oldest id, nearest candidate.
	sort.Slice(oldActives, func(a, b int) bool { return oldActives[a].ID < oldActives[b].ID })
	for _, op := range oldActives {
		best := -1
		var bestDt, bestRel float64
		for ci, cp := range newCands {
			if used[cp.ID] {
				continue
			}
			dt := absFloat(cp.ApexTime - op.ApexTime)
			rel := 0.0
			if op.Area != 0 {
				rel = absFloat(cp.Area-op.Area) / op.Area
			}
			if dt > maxDt || rel > maxAreaRel {
				continue
			}
			if best < 0 || dt < bestDt || (dt == bestDt && rel < bestRel) ||
				(dt == bestDt && rel == bestRel && newCands[ci].ID < newCands[best].ID) {
				best, bestDt, bestRel = ci, dt, rel
			}
		}
		if best >= 0 {
			used[newCands[best].ID] = true
			m.Matched = append(m.Matched, Match{
				OldID: op.ID, CandidateID: newCands[best].ID,
				ApexDt: bestDt, AreaRelErr: bestRel,
			})
		} else {
			m.Disappeared = append(m.Disappeared, op.ID)
		}
	}
	for _, cp := range newCands {
		if !used[cp.ID] {
			m.New = append(m.New, cp.ID)
		}
	}
	return m
}

// ApplyMigration constructs a peak-set for a new detection that preserves the
// stable ids of matched peaks, carries forward ignored/merged history, and
// registers genuinely new candidates under their detection ids.
func ApplyMigration(runID, newSetID, detID string, old domain.PeakSet, fresh []domain.Peak, mig Migration) domain.PeakSet {
	byCand := map[string]domain.Peak{}
	for _, pk := range fresh {
		byCand[pk.ID] = pk
	}
	out := domain.PeakSet{
		ID: newSetID, RunID: runID, Version: 1,
		ParentSetID: old.ID, BasedOnDet: detID,
	}
	candUsed := map[string]bool{}
	for _, mt := range mig.Matched {
		cp := byCand[mt.CandidateID]
		cp.ID = mt.OldID // stable identity migrates forward
		cp.Status = domain.PeakActive
		cp.Note = fmt.Sprintf("migrated from candidate %s", mt.CandidateID)
		out.Peaks = append(out.Peaks, cp)
		candUsed[mt.CandidateID] = true
	}
	// Preserve non-active history from the previous revision.
	for _, pk := range old.Peaks {
		if pk.Status != domain.PeakActive {
			out.Peaks = append(out.Peaks, pk)
		}
	}
	// Genuinely new candidates appear under fresh ids and are flagged.
	for _, pk := range fresh {
		if pk.Rejected || candUsed[pk.ID] {
			continue
		}
		cp := pk
		cp.Status = domain.PeakActive
		cp.Note = "new candidate after re-detection"
		out.Peaks = append(out.Peaks, cp)
	}
	sort.SliceStable(out.Peaks, func(a, b int) bool {
		return out.Peaks[a].ApexIdx < out.Peaks[b].ApexIdx
	})
	return out
}

// statsFor builds a peak over [l,r] directly from raw samples: apex is the
// highest sample, area uses the trapezoid rule, prominence uses the higher base.
func statsFor(times, values []float64, id string, l, r int, source string) domain.Peak {
	apex := l
	for k := l + 1; k <= r; k++ {
		if values[k] > values[apex] {
			apex = k
		}
	}
	base := values[l]
	if values[r] > base {
		base = values[r]
	}
	area := 0.0
	for k := l; k < r; k++ {
		area += 0.5 * (values[k] + values[k+1]) * (times[k+1] - times[k])
	}
	return domain.Peak{
		ID: id, ApexIdx: apex, LeftIdx: l, RightIdx: r,
		ApexTime: times[apex], Height: values[apex], Area: area,
		Prominence: values[apex] - base, Status: domain.PeakActive, Source: source,
	}
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
