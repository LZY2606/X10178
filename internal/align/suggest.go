package align

import (
	"fmt"
	"math"
	"sort"
)

// SugPeak is the minimal peak info needed for suggestions.
type SugPeak struct {
	ID       string
	T        float64
	Height   float64
	Area     float64
	Width    float64 // time units
	Plateau  bool
	RankArea int // area rank among its own list, 1-based
	LeftT    float64
	RightT   float64
}

// Suggest returns up to topK reference candidates for every run peak.
// Multiple alternatives are preserved (no silent auto-pick); Selected marks
// the single globally monotone greedy choice used by "apply suggestions".
func Suggest(ref, run []SugPeak, topK int) []SuggestionLite {
	refAreaMax := areaMax(ref)
	runAreaMax := areaMax(run)
	type cand struct {
		rf, rn int
		score  float64
	}
	var all []cand
	for ri, rp := range run {
		for qi, qp := range ref {
			dt := math.Abs(rp.T-qp.T) / timeSpan(ref)
			shape := 1.0 - clamp(math.Abs(rp.Width-qp.Width)/math.Max(qp.Width, 1e-12), 0, 1)
			area := 1.0 - clamp(math.Abs(rp.Area/math.Max(runAreaMax, 1e-12)-qp.Area/math.Max(refAreaMax, 1e-12)), 0, 1)
			nb := neighborScore(run, ri, ref, qi)
			score := 0.35*(1-dt) + 0.2*shape + 0.2*area + 0.25*nb
			if rp.Plateau || qp.Plateau {
				if rp.Plateau == qp.Plateau {
					score += 0.02
				} else {
					score -= 0.05
				}
			}
			all = append(all, cand{qi, ri, score})
		}
	}
	sort.SliceStable(all, func(a, b int) bool {
		if all[a].rn != all[b].rn {
			return all[a].rn < all[b].rn
		}
		if all[a].score != all[b].score {
			return all[a].score > all[b].score
		}
		return ref[all[a].rf].T < ref[all[b].rf].T
	})
	type key struct{ ri, qi int }
	selected := map[key]bool{}
	// Greedy monotone assignment by run-time order using each peak's best ref.
	order := append([]SugPeak(nil), run...)
	sort.SliceStable(order, func(a, b int) bool { return order[a].T < order[b].T })
	runIdx := map[string]int{}
	for i, p := range run {
		runIdx[p.ID] = i
	}
	lastRefT := math.Inf(-1)
	for _, rp := range order {
		ri := runIdx[rp.ID]
		bestQi, bestScore := -1, -1.0
		for _, c := range all {
			if c.rn != ri {
				continue
			}
			if ref[c.rf].T < lastRefT {
				continue
			}
			if c.score > bestScore {
				bestQi, bestScore = c.rf, c.score
			}
		}
		if bestQi >= 0 && bestScore > 0.45 {
			selected[key{ri, bestQi}] = true
			lastRefT = ref[bestQi].T
		}
	}
	var out []SuggestionLite
	countPerRun := map[int]int{}
	for _, c := range all {
		if countPerRun[c.rn] >= topK {
			continue
		}
		countPerRun[c.rn]++
		rp, qp := run[c.rn], ref[c.rf]
		out = append(out, SuggestionLite{
			RefPeakID: qp.ID, RefT: qp.T, RunPeakID: rp.ID, RunT: rp.T,
			Score: c.score, Selected: selected[key{c.rn, c.rf}],
			ShapeNote: shapeNote(qp, rp),
			AreaNote: fmt.Sprintf("area ranks #%d vs #%d; norm area %.2f vs %.2f", qp.RankArea, rp.RankArea,
				rp.Area/math.Max(runAreaMax, 1e-12), qp.Area/math.Max(refAreaMax, 1e-12)),
			NeighborNote: neighborNote(run, c.rn, ref, c.rf),
		})
	}
	return out
}

// SuggestionLite is the transport-neutral suggestion; app converts to model.
type SuggestionLite struct {
	RefPeakID    string
	RefT         float64
	RunPeakID    string
	RunT         float64
	Score        float64
	ShapeNote    string
	AreaNote     string
	NeighborNote string
	Selected     bool
}

func areaMax(ps []SugPeak) float64 {
	m := 0.0
	for _, p := range ps {
		if p.Area > m {
			m = p.Area
		}
	}
	return m
}

func timeSpan(ps []SugPeak) float64 {
	if len(ps) == 0 {
		return 1
	}
	lo, hi := ps[0].T, ps[0].T
	for _, p := range ps[1:] {
		if p.T < lo {
			lo = p.T
		}
		if p.T > hi {
			hi = p.T
		}
	}
	if hi-lo <= 0 {
		return 1
	}
	return hi - lo
}

func clamp(x, a, b float64) float64 {
	if x < a {
		return a
	}
	if x > b {
		return b
	}
	return x
}

func neighborScore(run []SugPeak, ri int, ref []SugPeak, qi int) float64 {
	good := 0
	total := 0
	for d := -2; d <= 2; d++ {
		if d == 0 {
			continue
		}
		rj, qj := ri+d, qi+d
		if rj < 0 || rj >= len(run) || qj < 0 || qj >= len(ref) {
			continue
		}
		total++
		gapRun := run[rj].T - run[ri].T
		gapRef := ref[qj].T - ref[qi].T
		scale := math.Max(math.Abs(gapRef), 1e-9)
		if math.Abs(gapRun-gapRef)/scale < 0.25 {
			good++
		}
	}
	if total == 0 {
		return 0.5
	}
	return float64(good) / float64(total)
}

func shapeNote(a, b SugPeak) string {
	plateau := ""
	if a.Plateau || b.Plateau {
		if a.Plateau && b.Plateau {
			plateau = "; both plateau"
		} else if a.Plateau {
			plateau = "; ref is plateau"
		} else {
			plateau = "; run is plateau"
		}
	}
	return fmt.Sprintf("width %.3g vs %.3g%s", a.Width, b.Width, plateau)
}

func neighborNote(run []SugPeak, ri int, ref []SugPeak, qi int) string {
	return fmt.Sprintf("%d/%d nearest neighbours (±2) preserve spacing", int(neighborScore(run, ri, ref, qi)*4+0.5), 4)
}
