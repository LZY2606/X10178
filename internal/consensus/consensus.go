// Package consensus merges multiple frozen run alignments into batch-level
// peak families. The area normalization rule is recorded inside the version so
// historical consensus results remain interpretable.
package consensus

import (
	"fmt"
	"sort"

	"peakcompass/internal/domain"
	"peakcompass/internal/warp"
)

// Norm rules fixed per version.
const (
	NormTotalArea = "total_area" // divide each peak area by run total peak area
	NormMedian    = "median"     // divide by the median peak area of the run
	NormNone      = "none"       // raw areas only
)

// runInput carries one run's frozen mapping and active peaks.
type runInput struct {
	run   domain.Run
	m     *domain.Mapping
	peaks []domain.Peak
	scale float64
}

// member is one run's support for a consensus family.
type member struct {
	runID, peakID string
	refTime       float64
	raw, norm     float64
	height        float64
}

// Build constructs a consensus over refTimes. tol is the reference-time cluster
// radius; peaks are assigned greedily in (time, run) order.
func Build(id, batch string, version int, ref domain.Run, runs []domain.Run, maps []*domain.Mapping, peakSets []domain.PeakSet, normRule string, tol float64, now string) (*domain.Consensus, error) {
	if len(runs) != len(maps) || len(maps) != len(peakSets) {
		return nil, fmt.Errorf("runs/maps/peaksets length mismatch")
	}
	inputs := make([]runInput, 0, len(runs))
	for i := range runs {
		if !maps[i].Frozen {
			return nil, fmt.Errorf("mapping for run %s v%d is not frozen", runs[i].ID, maps[i].Version)
		}
		var active []domain.Peak
		for _, pk := range peakSets[i].Peaks {
			if pk.Status == domain.PeakActive {
				active = append(active, pk)
			}
		}
		inputs = append(inputs, runInput{
			run: runs[i], m: maps[i], peaks: active,
			scale: normScale(normRule, active),
		})
	}
	// Map every peak onto the reference axis, then cluster in time order using a
	// running centroid with single-linkage tolerance. This stays correct when
	// the contributing runs arrive in arbitrary order or miss some families.
	var points []member
	for _, in := range inputs {
		for _, pk := range in.peaks {
			rt := warp.InverseEval(in.m, pk.ApexTime)
			points = append(points, member{runID: in.run.ID, peakID: pk.ID, refTime: rt,
				raw: pk.Area, norm: pk.Area / in.scale, height: pk.Height})
		}
	}
	sort.SliceStable(points, func(a, b int) bool {
		if points[a].refTime != points[b].refTime {
			return points[a].refTime < points[b].refTime
		}
		return points[a].runID < points[b].runID
	})
	var clusters [][]member
	var centroid float64
	for _, mem := range points {
		k := len(clusters) - 1
		if k >= 0 && absFloat(mem.refTime-centroid) <= tol {
			clusters[k] = append(clusters[k], mem)
			sum := 0.0
			for _, mm := range clusters[k] {
				sum += mm.refTime
			}
			centroid = sum / float64(len(clusters[k]))
		} else {
			clusters = append(clusters, []member{mem})
			centroid = mem.refTime
		}
	}

	c := &domain.Consensus{
		ID: id, Batch: batch, Version: version, RefRunID: ref.ID,
		NormRule: normRule, Frozen: false, CreatedAt: now,
	}
	for _, r := range runs {
		c.RunIDs = append(c.RunIDs, r.ID)
	}
	for ci, cl := range clusters {
		sort.SliceStable(cl, func(a, b int) bool { return cl[a].runID < cl[b].runID })
		cp := domain.ConsensusPeak{
			ID:      fmt.Sprintf("fam-%03d", ci+1),
			RefTime: medianTime(cl),
		}
		present := map[string]bool{}
		for _, mem := range cl {
			cp.Supporting = append(cp.Supporting, domain.PeakSupport{
				RunID: mem.runID, PeakID: mem.peakID, RefTime: mem.refTime,
				RawArea: mem.raw, NormArea: mem.norm,
			})
			cp.Area += mem.norm
			cp.Height += mem.height
			present[mem.runID] = true
		}
		cp.Area /= float64(len(cl))
		cp.Height /= float64(len(cl))
		for _, r := range runs {
			if !present[r.ID] {
				cp.Missing = append(cp.Missing, r.ID)
			}
		}
		sort.Strings(cp.Missing)
		c.Peaks = append(c.Peaks, cp)
	}
	return c, nil
}

func normScale(rule string, peaks []domain.Peak) float64 {
	switch rule {
	case NormTotalArea:
		t := 0.0
		for _, p := range peaks {
			t += p.Area
		}
		if t == 0 {
			return 1
		}
		return t
	case NormMedian:
		if len(peaks) == 0 {
			return 1
		}
		a := make([]float64, len(peaks))
		for i, p := range peaks {
			a[i] = p.Area
		}
		sort.Float64s(a)
		return a[len(a)/2]
	default:
		return 1
	}
}

func medianTime(m []member) float64 {
	a := make([]float64, len(m))
	for i := range m {
		a[i] = m[i].refTime
	}
	sort.Float64s(a)
	return a[len(a)/2]
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
