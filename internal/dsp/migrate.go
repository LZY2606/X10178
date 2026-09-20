package dsp

import (
	"sort"

	"peakvalley/internal/model"
)

// Migrate assigns fates to old stable peak ids after redetection. Identity is
// based on boundary overlap (IoU), never on array index.
func Migrate(oldPks, newPks []*model.Peak) []*model.Mig {
	type pair struct {
		oi, ni     int
		iou, apexW float64
		score      float64
	}
	var pairs []pair
	for oi, o := range oldPks {
		for ni, nw := range newPks {
			iou := intervalIoU(o.Shape.LeftI, o.Shape.RightI, nw.Shape.LeftI, nw.Shape.RightI)
			if iou < 0.2 {
				continue
			}
			w := o.WidthI
			if nw.WidthI > w {
				w = nw.WidthI
			}
			apexW := 1.0 - float64(absInt(o.ApexI-nw.ApexI))/float64(w+1)
			if apexW < 0 {
				apexW = 0
			}
			pairs = append(pairs, pair{oi, ni, iou, apexW, 0.7*iou + 0.3*apexW})
		}
	}
	// Deterministic greedy assignment: highest score first, index tie-breaks.
	sort.SliceStable(pairs, func(a, b int) bool {
		if pairs[a].score != pairs[b].score {
			return pairs[a].score > pairs[b].score
		}
		if pairs[a].oi != pairs[b].oi {
			return pairs[a].oi < pairs[b].oi
		}
		return pairs[a].ni < pairs[b].ni
	})
	oldUsed := map[int]bool{}
	newUsed := map[int]bool{}
	best := map[int]pair{}
	var out []*model.Mig
	for _, pr := range pairs {
		if pr.iou < 0.5 {
			continue
		}
		if oldUsed[pr.oi] || newUsed[pr.ni] {
			continue
		}
		oldUsed[pr.oi] = true
		newUsed[pr.ni] = true
		best[pr.oi] = pr
		o, nw := oldPks[pr.oi], newPks[pr.ni]
		rel := "same"
		note := ""
		if o.ApexI != nw.ApexI {
			rel = "moved"
			note = "apex shifted"
		}
		out = append(out, &model.Mig{OldID: o.ID, NewID: nw.ID, Rel: rel, Score: pr.score, Note: note})
	}
	// Split/merge annotations via weaker overlaps that lost the greedy match.
	for _, pr := range pairs {
		if pr.iou < 0.35 {
			continue
		}
		o, nw := oldPks[pr.oi], newPks[pr.ni]
		if oldUsed[pr.oi] && !newUsed[pr.ni] {
			out = append(out, &model.Mig{OldID: o.ID, NewID: nw.ID, Rel: "split", Score: pr.score, Note: "additional new peak inside old bounds"})
			newUsed[pr.ni] = true
		} else if !oldUsed[pr.oi] && newUsed[pr.ni] {
			out = append(out, &model.Mig{OldID: o.ID, NewID: nw.ID, Rel: "merged", Score: pr.score, Note: "old peak absorbed into neighbour"})
			oldUsed[pr.oi] = true
		}
	}
	for oi, o := range oldPks {
		if !oldUsed[oi] {
			out = append(out, &model.Mig{OldID: o.ID, NewID: "", Rel: "deleted", Score: 0, Note: "no overlapping candidate"})
		}
	}
	for ni, nw := range newPks {
		if !newUsed[ni] {
			out = append(out, &model.Mig{OldID: "", NewID: nw.ID, Rel: "inserted", Score: 0, Note: "new candidate"})
		}
	}
	return out
}

func intervalIoU(aL, aR, bL, bR int) float64 {
	lo := aL
	if bL > lo {
		lo = bL
	}
	hi := aR
	if bR < hi {
		hi = bR
	}
	inter := hi - lo
	if inter < 0 {
		inter = 0
	}
	union := (aR - aL) + (bR - bL) - inter
	if union <= 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
