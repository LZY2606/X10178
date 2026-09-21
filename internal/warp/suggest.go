package warp

import (
	"fmt"
	"sort"

	"peakcompass/internal/domain"
)

// Suggestion is one proposed anchor candidate with a human/API explanation.
type Suggestion struct {
	RefPeakID   string  `json:"refPeakId"`
	CandidateID string  `json:"candidateId"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Score       float64 `json:"score"`
	Shape       string  `json:"shape"`
	AreaRatio   float64 `json:"areaRatio"`
	Neighbor    string  `json:"neighbor"`
	Ambiguous   bool    `json:"ambiguous"`
	Reason      string  `json:"reason"`
}

// Shape describes the coarse morphology of a peak.
func Shape(p domain.Peak) string {
	switch {
	case p.Plateau:
		return "plateau"
	case p.RightIdx-p.ApexIdx > p.ApexIdx-p.LeftIdx+1:
		return "tailing"
	case p.ApexIdx-p.LeftIdx > p.RightIdx-p.ApexIdx+1:
		return "fronting"
	default:
		return "symmetric"
	}
}

// SuggestProposals returns up to topK ranked anchor candidates per reference
// peak. Several alternatives are retained instead of a single winner so a
// researcher can adjudicate ties and ambiguous families.
func SuggestProposals(ref, run domain.Run, refPeaks, runPeaks []domain.Peak, topK int) []Suggestion {
	if topK <= 0 {
		topK = 3
	}
	var active []domain.Peak
	for _, p := range runPeaks {
		if !p.Rejected && p.Status == domain.PeakActive {
			active = append(active, p)
		}
	}
	var out []Suggestion
	for _, rp := range refPeaks {
		if rp.Rejected || rp.Status != domain.PeakActive {
			continue
		}
		type scored struct {
			p     domain.Peak
			score float64
			ratio float64
			neigh string
			amb   bool
		}
		var ranked []scored
		for _, cp := range active {
			dt := cp.ApexTime - rp.ApexTime
			ratio := 0.0
			if rp.Area != 0 {
				ratio = cp.Area / rp.Area
			}
			areaPenalty := absFloat(ratio - 1.0)
			score := -absFloat(dt) - 0.5*areaPenalty
			s := scored{p: cp, score: score, ratio: ratio,
				neigh: neighborRelation(rp, cp, refPeaks, active), amb: false}
			ranked = append(ranked, s)
		}
		sort.SliceStable(ranked, func(a, b int) bool {
			if ranked[a].score != ranked[b].score {
				return ranked[a].score > ranked[b].score
			}
			return ranked[a].p.ID < ranked[b].p.ID
		})
		if len(ranked) > topK {
			ranked = ranked[:topK]
		}
		for ri, sc := range ranked {
			amb := ri == 0 && len(ranked) > 1 && ranked[1].score-sc.score > -0.05
			reason := fmt.Sprintf("shape=%s areaRatio=%.2f %s", Shape(rp), sc.ratio, sc.neigh)
			if amb {
				reason += " | near-tie with runner-up"
			}
			out = append(out, Suggestion{
				RefPeakID: rp.ID, CandidateID: sc.p.ID,
				X: rp.ApexTime, Y: sc.p.ApexTime, Score: sc.score,
				Shape: Shape(sc.p), AreaRatio: sc.ratio, Neighbor: sc.neigh,
				Ambiguous: amb, Reason: reason,
			})
		}
	}
	return out
}

// neighborRelation summarizes whether the nearest flanking peaks agree in order.
func neighborRelation(rp, cp domain.Peak, refAll, runAll []domain.Peak) string {
	ri, ci := indexOf(rp.ID, refAll), indexOf(cp.ID, runAll)
	ok := true
	if ri > 0 && ci > 0 {
		// previous-peak gap ordering
		rg := rp.ApexTime - refAll[ri-1].ApexTime
		cg := cp.ApexTime - runAll[ci-1].ApexTime
		if (rg < 0) != (cg < 0) {
			ok = false
		}
	}
	if ok {
		return "neighbors-preserved"
	}
	return "neighbor-order-changed"
}

func indexOf(id string, ps []domain.Peak) int {
	for i := range ps {
		if ps[i].ID == id {
			return i
		}
	}
	return -1
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
