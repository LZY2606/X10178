package app

import (
	"sort"

	"peakvalley/internal/align"
	"peakvalley/internal/model"
)

// Suggest returns several candidate correspondences for one run, each with a
// shape, area and neighbour rationale. Nothing is applied automatically.
func (a *App) Suggest(alID, runID string, topK int) ([]model.Suggestion, error) {
	al, err := a.Store.GetAlignment(alID)
	if err != nil {
		return nil, err
	}
	_, refItems, err := a.CurrentItems(al.RefRunID)
	if err != nil {
		return nil, err
	}
	_, runItems, err := a.CurrentItems(runID)
	if err != nil {
		return nil, err
	}
	ref := toSug(refItems)
	run := toSug(runItems)
	if topK <= 0 {
		topK = 3
	}
	lite := align.Suggest(ref, run, topK)
	out := make([]model.Suggestion, len(lite))
	for i, s := range lite {
		out[i] = model.Suggestion{
			RefPeakID: s.RefPeakID, RefT: s.RefT, RunPeakID: s.RunPeakID, RunT: s.RunT,
			Score: s.Score, ShapeNote: s.ShapeNote, AreaNote: s.AreaNote,
			NeighborNote: s.NeighborNote, Selected: s.Selected,
		}
	}
	return out, nil
}

func toSug(items []*model.ViewPeak) []align.SugPeak {
	active := make([]*model.ViewPeak, 0, len(items))
	for _, v := range items {
		if v.Status == "active" {
			active = append(active, v)
		}
	}
	sort.SliceStable(active, func(i, j int) bool { return active[i].ApexT < active[j].ApexT })
	byArea := make([]float64, len(active))
	for i, v := range active {
		byArea[i] = v.Area
	}
	sort.Float64s(byArea)
	rank := map[string]int{}
	for i := len(byArea) - 1; i >= 0; i-- {
		_ = i
	}
	areaRank := map[float64]int{}
	r := 1
	for i := len(byArea) - 1; i >= 0; i-- {
		if _, ok := areaRank[byArea[i]]; !ok {
			areaRank[byArea[i]] = r
			r++
		}
	}
	for _, v := range active {
		rank[v.PeakID] = areaRank[v.Area]
	}
	out := make([]align.SugPeak, 0, len(active))
	for _, v := range active {
		out = append(out, align.SugPeak{
			ID: v.PeakID, T: v.ApexT, Height: v.Height, Area: v.Area,
			Width: v.Shape.RightT - v.Shape.LeftT, Plateau: v.Shape.Plateau,
			RankArea: rank[v.PeakID], LeftT: v.Shape.LeftT, RightT: v.Shape.RightT,
		})
	}
	return out
}
