// Package align implements monotonic cross-run time mappings.
package align

import (
	"fmt"
	"sort"

	"peakvalley/internal/model"
)

// PPoint is one participation: reference time vs run time for one anchor.
// SamePeak is true when two points at equal time bind the exact same run peak
// (a re-assignment conflict); distinct peaks coinciding on a plateau are fine.
type PPoint struct {
	AnchorID string
	RefT, T  float64
	SamePeak bool
}

// FindConflicts returns every incompatible anchor pair for one run.
// Anchor order must agree with reference order; equal times never cross.
func FindConflicts(runID string, pts []PPoint) []model.Conflict {
	ord := make([]PPoint, len(pts))
	copy(ord, pts)
	sort.SliceStable(ord, func(a, b int) bool {
		if ord[a].RefT != ord[b].RefT {
			return ord[a].RefT < ord[b].RefT
		}
		return ord[a].T < ord[b].T
	})
	var out []model.Conflict
	for i := 0; i < len(ord); i++ {
		for j := i + 1; j < len(ord); j++ {
			a, b := ord[i], ord[j]
			switch {
			case a.RefT == b.RefT:
				continue
			case a.T == b.T && (a.SamePeak || b.SamePeak):
				out = append(out, model.Conflict{
					RunID: runID, A: a.AnchorID, B: b.AnchorID,
					T_A: a.T, T_B: b.T, RefA: a.RefT, RefB: b.RefT,
					Description: fmt.Sprintf("anchors %s and %s both bind the same run peak at %.4g", a.AnchorID, b.AnchorID, a.T),
				})
			case a.T > b.T && a.RefT < b.RefT:
				out = append(out, model.Conflict{
					RunID: runID, A: a.AnchorID, B: b.AnchorID,
					T_A: a.T, T_B: b.T, RefA: a.RefT, RefB: b.RefT,
					Description: fmt.Sprintf("anchors %s(t=%.4g) and %s(t=%.4g) cross", a.AnchorID, a.T, b.AnchorID, b.T),
				})
			}
		}
	}
	return out
}

// MinRejectSet returns a smallest anchor set whose removal makes the run
// monotonic. It never sorts the conflict away: the caller must delete one of
// the returned anchors. Ties are broken deterministically (earlier reference
// time, then earlier run time, then anchor id).
func MinRejectSet(pts []PPoint) []string {
	if len(pts) == 0 {
		return nil
	}
	ord := make([]PPoint, len(pts))
	copy(ord, pts)
	sort.SliceStable(ord, func(a, b int) bool {
		if ord[a].RefT != ord[b].RefT {
			return ord[a].RefT < ord[b].RefT
		}
		if ord[a].T != ord[b].T {
			return ord[a].T < ord[b].T
		}
		return ord[a].AnchorID < ord[b].AnchorID
	})
	n := len(ord)
	// dp[i] = longest admissible run-time chain ending at i. Equal times are
	// allowed only for genuinely distinct peaks (plateau co-anchors); a
	// same-peak re-binding must not be part of the chain.
	admissible := func(j, i int) bool {
		if ord[j].T < ord[i].T {
			return true
		}
		return ord[j].T == ord[i].T && !(ord[j].SamePeak || ord[i].SamePeak)
	}
	dp := make([]int, n)
	prev := make([]int, n)
	for i := range dp {
		dp[i] = 1
		prev[i] = -1
	}
	for i := 0; i < n; i++ {
		for j := 0; j < i; j++ {
			if admissible(j, i) && dp[j]+1 > dp[i] {
				dp[i] = dp[j] + 1
				prev[i] = j
			}
		}
	}
	end := 0
	for i := 1; i < n; i++ {
		if dp[i] > dp[end] {
			end = i
		}
	}
	keep := map[int]bool{}
	for k := end; k >= 0; k = prev[k] {
		keep[k] = true
	}
	var reject []string
	for i, p := range ord {
		if !keep[i] {
			reject = append(reject, p.AnchorID)
		}
	}
	// Stable order: reference position, so diagnostics are reproducible.
	sort.SliceStable(reject, func(a, b int) bool {
		return reject[a] < reject[b]
	})
	return reject
}

// ValidatePlan checks all participating runs. It returns the conflicts found
// and one minimum reject set per run that is not yet monotonic.
func ValidatePlan(perRun map[string][]PPoint) (allConf []model.Conflict, rejects []model.RejectSet) {
	runIDs := make([]string, 0, len(perRun))
	for r := range perRun {
		runIDs = append(runIDs, r)
	}
	sort.Strings(runIDs)
	for _, r := range runIDs {
		pts := perRun[r]
		conf := FindConflicts(r, pts)
		allConf = append(allConf, conf...)
		if len(conf) > 0 {
			set := MinRejectSet(pts)
			rejects = append(rejects, model.RejectSet{
				RunID: r, AnchorIDs: set, Size: len(set),
				Description: fmt.Sprintf("remove or move %d anchor(s) in run %s to restore monotonicity", len(set), r),
			})
		}
	}
	return allConf, rejects
}
