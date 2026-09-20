// Package consensus builds cross-run peak families and the batch consensus.
package consensus

import (
	"math"
	"sort"

	"peakvalley/internal/model"
)

// MemberInput is one active working-list peak mapped into reference time.
type MemberInput struct {
	RunID   string
	PeakID  string
	DetVer  int
	EditVer int
	ApexU   float64
	Area    float64
	Height  float64
	Status  string
}

// Cluster groups members with single-linkage clustering: two members join the
// same family when their aligned apex distance is <= tolU. Chains merge
// clusters transitively; ties in processing order are index-deterministic.
func Cluster(members []MemberInput, tolU float64) [][]MemberInput {
	ord := make([]MemberInput, len(members))
	copy(ord, members)
	sort.SliceStable(ord, func(a, b int) bool {
		if ord[a].ApexU != ord[b].ApexU {
			return ord[a].ApexU < ord[b].ApexU
		}
		if ord[a].RunID != ord[b].RunID {
			return ord[a].RunID < ord[b].RunID
		}
		return ord[a].PeakID < ord[b].PeakID
	})
	parent := make([]int, len(ord))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}
	for i := 0; i < len(ord); i++ {
		for j := i + 1; j < len(ord); j++ {
			if math.Abs(ord[i].ApexU-ord[j].ApexU) <= tolU {
				union(i, j)
			}
		}
	}
	groups := map[int][]MemberInput{}
	var roots []int
	for i := range ord {
		r := find(i)
		if _, ok := groups[r]; !ok {
			roots = append(roots, r)
		}
		groups[r] = append(groups[r], ord[i])
	}
	sort.Ints(roots)
	out := make([][]MemberInput, 0, len(roots))
	for _, r := range roots {
		out = append(out, groups[r])
	}
	return out
}

// BuildConsensus turns families into consensus peaks with fixed rule metadata.
// Missing lists the runs that participate in the alignment but have no member.
func BuildConsensus(families []*model.Family, runIDs []string, rule model.NormRule) ([]model.ConsensusPeak, map[string]float64) {
	factors := NormalizationFactors(families, runIDs, rule)
	peaks := make([]model.ConsensusPeak, 0, len(families))
	sorted := append([]*model.Family(nil), families...)
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].CenterU < sorted[b].CenterU })
	for _, f := range sorted {
		present := map[string]bool{}
		norm := map[string]float64{}
		var sum float64
		for _, m := range f.Members {
			if m.Status != "active" {
				continue
			}
			present[m.RunID] = true
			v := m.Area
			if fac, ok := factors[m.RunID]; ok && fac != 0 {
				v = m.Area / fac * rule.Scale
			}
			norm[m.RunID] = v
			sum += v
		}
		var supporting, missing []string
		for _, r := range runIDs {
			if present[r] {
				supporting = append(supporting, r)
			} else {
				missing = append(missing, r)
			}
		}
		mean := 0.0
		cv := 0.0
		if len(supporting) > 0 {
			mean = sum / float64(len(supporting))
			ss := 0.0
			for _, r := range supporting {
				d := norm[r] - mean
				ss += d * d
			}
			if len(supporting) > 1 {
				sd := math.Sqrt(ss / float64(len(supporting)-1))
				if mean > 0 {
					cv = sd / mean
				}
			}
		}
		peaks = append(peaks, model.ConsensusPeak{
			FamilyID: f.ID, Name: f.Name, CenterU: f.CenterU,
			AreaMean: mean, AreaCV: cv,
			Supporting: supporting, Missing: missing, MemberNormArea: norm,
		})
	}
	return peaks, factors
}

// NormalizationFactors pins, per version, one factor per run.
// "tic": sum of active member areas; "median_peak": median active area;
// "none": 1.
func NormalizationFactors(families []*model.Family, runIDs []string, rule model.NormRule) map[string]float64 {
	areas := map[string][]float64{}
	tic := map[string]float64{}
	for _, f := range families {
		for _, m := range f.Members {
			if m.Status != "active" {
				continue
			}
			areas[m.RunID] = append(areas[m.RunID], m.Area)
			tic[m.RunID] += m.Area
		}
	}
	out := map[string]float64{}
	for _, r := range runIDs {
		switch rule.Method {
		case "median_peak":
			a := append([]float64(nil), areas[r]...)
			sort.Float64s(a)
			if len(a) == 0 {
				out[r] = 1
			} else if len(a)%2 == 1 {
				out[r] = a[len(a)/2]
			} else {
				out[r] = (a[len(a)/2-1] + a[len(a)/2]) / 2
			}
		case "tic":
			if tic[r] == 0 {
				out[r] = 1
			} else {
				out[r] = tic[r]
			}
		default:
			out[r] = 1
		}
	}
	return out
}
