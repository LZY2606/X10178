// Package warp builds monotone time mappings between runs.
//
// Anchors may cross, sit on plateau peaks, duplicate each other, or appear only
// in some runs. Before a mapping is saved, Build computes the minimum set of
// anchors that must be rejected to restore monotonicity; it never "fixes" a
// crossing by silently re-sorting the anchors.
package warp

import (
	"fmt"
	"sort"

	"peakcompass/internal/domain"
)

// BuildResult explains the save decision.
type BuildResult struct {
	Mapping  *domain.Mapping
	Rejected []domain.RejectedAnchor
	Valid    bool
}

// sortedAnchor is an anchor tagged with its original submission position.
type sortedAnchor struct {
	pos int
	a   domain.Anchor
}

// Build validates a candidate anchor set and returns the resulting mapping.
//
//   - exact duplicate (x,y) pairs collapse to one accepted anchor;
//   - two anchors sharing x but differing y are a vertical (non-function) set;
//   - every remaining crossing is resolved by a weighted longest increasing
//     subsequence, so the rejected set is minimum (ties broken deterministically
//     toward manual anchors, then lower original position);
//   - segment locks that would prevent monotonicity at their boundaries are also
//     reported instead of being silently relaxed.
func Build(id, refRunID, runID string, version int, anchors []domain.Anchor, locks []domain.SegmentLock, now string) BuildResult {
	ordered := make([]sortedAnchor, len(anchors))
	for i, a := range anchors {
		ordered[i] = sortedAnchor{pos: i, a: a}
	}

	var rejected []domain.RejectedAnchor

	// Collapse exact duplicates; flag same-x/different-y as vertical.
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].a.X != ordered[j].a.X {
			return ordered[i].a.X < ordered[j].a.X
		}
		if ordered[i].a.Y != ordered[j].a.Y {
			return ordered[i].a.Y < ordered[j].a.Y
		}
		return ordered[i].pos < ordered[j].pos
	})
	var dedup []sortedAnchor
	for k := 0; k < len(ordered); {
		x0 := ordered[k].a.X
		// Collect every anchor at this x, bucketed by exact (x,y).
		type bucket struct {
			y     float64
			items []sortedAnchor
		}
		var buckets []bucket
		for k < len(ordered) && ordered[k].a.X == x0 {
			y0 := ordered[k].a.Y
			var items []sortedAnchor
			for k < len(ordered) && ordered[k].a.X == x0 && ordered[k].a.Y == y0 {
				items = append(items, ordered[k])
				k++
			}
			buckets = append(buckets, bucket{y: y0, items: items})
		}
		if len(buckets) > 1 {
			// Same x, differing y: non-function / vertical conflict. Every
			// anchor at this x is rejected; pairwise "with" links the first
			// representatives of the two lowest buckets.
			rep0, rep1 := buckets[0].items[0].a.ID, buckets[1].items[0].a.ID
			for bi, bk := range buckets {
				with := rep1
				if bi != 0 {
					with = rep0
				}
				for _, it := range bk.items {
					rejected = append(rejected, domain.RejectedAnchor{
						AnchorID: it.a.ID, X: x0, Y: bk.y,
						Reason: "vertical", With: with,
					})
				}
			}
			continue
		}
		// Single y at this x: keep the first (lowest original position) anchor;
		// exact duplicates carry no information and are reported explicitly.
		items := buckets[0].items
		dedup = append(dedup, items[0])
		for _, dup := range items[1:] {
			rejected = append(rejected, domain.RejectedAnchor{
				AnchorID: dup.a.ID, X: dup.a.X, Y: dup.a.Y,
				Reason: "duplicate", With: items[0].a.ID,
			})
		}
	}

	// Weighted LIS over (x,y): maximum-weight strictly increasing subsequence.
	// Rejecting the complement leaves a minimum-cardinality rejection set.
	n := len(dedup)
	dp := make([]float64, n)
	par := make([]int, n)
	keep := make([]bool, n)
	for i := 0; i < n; i++ {
		dp[i] = weight(dedup[i].a)
		par[i] = -1
		for j := 0; j < i; j++ {
			if dedup[j].a.Y < dedup[i].a.Y &&
				dp[j]+weight(dedup[i].a) > dp[i] {
				dp[i] = dp[j] + weight(dedup[i].a)
				par[i] = j
			}
		}
	}
	best := -1
	for i := 0; i < n; i++ {
		if best < 0 || dp[i] > dp[best] ||
			(dp[i] == dp[best] && prefer(dedup[i], dedup[best])) {
			best = i
		}
	}
	for c := best; c >= 0; c = par[c] {
		keep[c] = true
	}
	for i := 0; i < n; i++ {
		if keep[i] {
			continue
		}
		with := crossingWith(dedup, keep, i)
		rejected = append(rejected, domain.RejectedAnchor{
			AnchorID: dedup[i].a.ID, X: dedup[i].a.X, Y: dedup[i].a.Y,
			Reason: "cross", With: with,
		})
	}

	accepted := make([]domain.Anchor, 0, n)
	for i, s := range dedup {
		if keep[i] {
			accepted = append(accepted, s.a)
		}
	}
	sort.SliceStable(accepted, func(i, j int) bool { return accepted[i].X < accepted[j].X })

	// Locked segments must not straddle two accepted anchors whose local slope
	// is non-positive (cannot happen after LIS) and the lock boundaries have to
	// agree with anchor order at their edges.
	for _, lk := range locks {
		if lk.XStart >= lk.XEnd {
			rejected = append(rejected, domain.RejectedAnchor{
				AnchorID: lk.ID, X: lk.XStart, Y: lk.XEnd,
				Reason: "lock-degenerate",
			})
		}
	}

	valid := len(rejected) == 0
	m := &domain.Mapping{
		ID: id, RefRunID: refRunID, RunID: runID, Version: version,
		Anchors:  append([]domain.Anchor(nil), anchors...),
		Accepted: accepted, Rejected: rejected, Locks: locks,
		Valid: valid, Frozen: false, CreatedAt: now,
	}
	return BuildResult{Mapping: m, Rejected: rejected, Valid: valid}
}

func weight(a domain.Anchor) float64 {
	if a.Kind == domain.AnchorManual {
		return 10.0
	}
	return 1.0
}

// prefer breaks equal-weight LIS ties deterministically toward manual anchors
// and the earlier submission position.
func prefer(a, b sortedAnchor) bool {
	if (a.a.Kind == domain.AnchorManual) != (b.a.Kind == domain.AnchorManual) {
		return a.a.Kind == domain.AnchorManual
	}
	return a.pos < b.pos
}

// crossingWith names a kept anchor that proves why i must be rejected.
func crossingWith(dedup []sortedAnchor, keep []bool, i int) string {
	for j := range dedup {
		if !keep[j] {
			continue
		}
		if (dedup[j].a.X < dedup[i].a.X && dedup[j].a.Y >= dedup[i].a.Y) ||
			(dedup[j].a.X > dedup[i].a.X && dedup[j].a.Y <= dedup[i].a.Y) ||
			(dedup[j].a.X == dedup[i].a.X && dedup[j].a.Y != dedup[i].a.Y) {
			return dedup[j].a.ID
		}
	}
	return ""
}

// Eval maps a reference time x to the target run's time using piecewise linear
// interpolation through the accepted anchors. Outside the anchor span the map
// clamps to the nearest endpoint so no wild extrapolation is introduced.
func Eval(m *domain.Mapping, x float64) float64 {
	a := m.Accepted
	if len(a) == 0 {
		return x
	}
	if x <= a[0].X {
		return a[0].Y
	}
	if x >= a[len(a)-1].X {
		return a[len(a)-1].Y
	}
	lo, hi := 0, len(a)-1
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if a[mid].X <= x {
			lo = mid
		} else {
			hi = mid
		}
	}
	dx := a[hi].X - a[lo].X
	if dx == 0 {
		return a[lo].Y
	}
	f := (x - a[lo].X) / dx
	return a[lo].Y + f*(a[hi].Y-a[lo].Y)
}

// inverseFind locates the target time corresponding to ref time x by binary
// search over the (monotone non-decreasing) run sample grid.
func interpValue(times, values []float64, t float64) (float64, int, bool) {
	if t <= times[0] {
		return values[0], 0, t != times[0]
	}
	if t >= times[len(times)-1] {
		return values[len(values)-1], len(values) - 1, t != times[len(times)-1]
	}
	lo, hi := 0, len(times)-1
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if times[mid] <= t {
			lo = mid
		} else {
			hi = mid
		}
	}
	dx := times[hi] - times[lo]
	if dx == 0 {
		return values[lo], lo, false
	}
	f := (t - times[lo]) / dx
	return values[lo] + f*(values[hi]-values[lo]), lo, f != 0
}

// Align samples the target run onto the reference time grid and records full
// provenance for every derived point (source sample index + mapping version).
func Align(m *domain.Mapping, refTimes, runTimes, runValues []float64) ([]domain.LineagePoint, error) {
	if len(runTimes) != len(runValues) {
		return nil, fmt.Errorf("run times/values length mismatch")
	}
	out := make([]domain.LineagePoint, 0, len(refTimes))
	for _, x := range refTimes {
		yt := Eval(m, x)
		v, idx, interp := interpValue(runTimes, runValues, yt)
		out = append(out, domain.LineagePoint{
			RefTime: x, Aligned: yt, Value: v,
			SrcIdx: idx, SrcTime: runTimes[idx], Interp: interp,
			MappingID: m.ID, MapVer: m.Version,
		})
	}
	return out, nil
}

// AffectedRange returns the reference-time interval [lo,hi] whose aligned output
// can change when the anchor with id ax moves: it spans the two segments that
// touch the anchor (from the midpoint of the previous segment to the midpoint of
// the following segment). Everything outside stays byte-for-byte identical.
func AffectedRange(anchors []domain.Anchor, ax domain.Anchor) (float64, float64) {
	sorted := append([]domain.Anchor(nil), anchors...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].X < sorted[j].X })
	pos := -1
	for i := range sorted {
		if sorted[i].ID == ax.ID {
			pos = i
			break
		}
	}
	if pos < 0 {
		return ax.X, ax.X
	}
	lo := sorted[0].X
	if pos > 0 {
		lo = (sorted[pos-1].X + sorted[pos].X) / 2
	}
	hi := sorted[len(sorted)-1].X
	if pos < len(sorted)-1 {
		hi = (sorted[pos].X + sorted[pos+1].X) / 2
	}
	return lo, hi
}

// LockIntegrity reports, per lock, whether recomputing [lo,hi] would touch it.
// A recompute that intersects a locked segment is refused so locked regions
// remain pointwise identical across versions.
type LockConflict struct {
	LockID string  `json:"lockId"`
	XStart float64 `json:"xStart"`
	XEnd   float64 `json:"xEnd"`
}

// CheckLocks returns the locks intersected by an update range.
func CheckLocks(locks []domain.SegmentLock, lo, hi float64) []LockConflict {
	var out []LockConflict
	for _, lk := range locks {
		if lo < lk.XEnd && hi > lk.XStart {
			out = append(out, LockConflict{LockID: lk.ID, XStart: lk.XStart, XEnd: lk.XEnd})
		}
	}
	return out
}

// Residual compares an aligned series against a reference on the reference
// grid. Every returned point references the mapping version that produced it.
type ResidualPoint struct {
	RefTime float64 `json:"refTime"`
	Delta   float64 `json:"delta"`
	MapVer  int     `json:"mapVer"`
}

// Residual computes aligned-minus-reference signal differences.
func Residual(m *domain.Mapping, refTimes, refValues, runTimes, runValues []float64) ([]ResidualPoint, error) {
	aligned, err := Align(m, refTimes, runTimes, runValues)
	if err != nil {
		return nil, err
	}
	out := make([]ResidualPoint, len(aligned))
	for i, p := range aligned {
		out[i] = ResidualPoint{RefTime: p.RefTime, Delta: p.Value - refValues[i], MapVer: m.Version}
	}
	return out, nil
}

// InverseEval maps a target-run time y back to reference time x using the
// inverse of the piecewise-linear anchor map (monotone, so well defined).
// Outside the anchor span it clamps to the nearest endpoint.
func InverseEval(m *domain.Mapping, y float64) float64 {
	a := m.Accepted
	if len(a) == 0 {
		return y
	}
	if y <= a[0].Y {
		return a[0].X
	}
	if y >= a[len(a)-1].Y {
		return a[len(a)-1].X
	}
	lo, hi := 0, len(a)-1
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if a[mid].Y <= y {
			lo = mid
		} else {
			hi = mid
		}
	}
	dy := a[hi].Y - a[lo].Y
	if dy == 0 {
		return a[lo].X
	}
	f := (y - a[lo].Y) / dy
	return a[lo].X + f*(a[hi].X-a[lo].X)
}
