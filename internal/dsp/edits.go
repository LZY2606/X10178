package dsp

import (
	"fmt"
	"sort"

	"peakvalley/internal/model"
)

// Apply materialises one edit event against the current working list.
// times/signal are required because split and merge recompute geometry from
// the original samples; indices always stay anchored to raw sample positions.
func Apply(idGen IDGen, times, signal []float64, items []*model.ViewPeak, ev model.EditEvent) ([]*model.ViewPeak, error) {
	out := make([]*model.ViewPeak, len(items))
	for i, v := range items {
		cp := *v
		out[i] = &cp
	}
	idx := map[string]int{}
	for i, v := range out {
		idx[v.PeakID] = i
	}
	byID := func(id string) (*model.ViewPeak, error) {
		k, ok := idx[id]
		if !ok {
			return nil, fmt.Errorf("peak %s not found", id)
		}
		return out[k], nil
	}

	switch ev.Op {
	case model.OpIgnore:
		if len(ev.PeakIDs) == 0 {
			return nil, fmt.Errorf("ignore needs peaks")
		}
		for _, id := range ev.PeakIDs {
			v, err := byID(id)
			if err != nil {
				return nil, err
			}
			if v.Status != "active" {
				return nil, fmt.Errorf("peak %s is %s", id, v.Status)
			}
			v.Status = "ignored"
			if ev.Note != "" {
				v.Note = ev.Note
			}
		}
	case model.OpRestore:
		for _, id := range ev.PeakIDs {
			v, err := byID(id)
			if err != nil {
				return nil, err
			}
			v.Status = "active"
			v.Note = ""
		}
	case model.OpMerge:
		if len(ev.PeakIDs) < 2 {
			return nil, fmt.Errorf("merge needs >=2 peaks")
		}
		group := ev.GroupID
		if group == "" {
			group = idGen.New()
		}
		var members []*model.ViewPeak
		for _, id := range ev.PeakIDs {
			v, err := byID(id)
			if err != nil {
				return nil, err
			}
			if v.Status != "active" {
				return nil, fmt.Errorf("peak %s is %s", id, v.Status)
			}
			members = append(members, v)
		}
		sort.Slice(members, func(a, b int) bool { return members[a].ApexI < members[b].ApexI })
		areaSum, weight := 0.0, 0.0
		apexNum, apexDen := 0.0, 0.0
		height, prom := -1.0, -1.0
		left, right := members[0].Shape.LeftI, 0
		detVer := members[0].DetVer
		for _, v := range members {
			areaSum += v.Area
			weight += v.Height
			apexNum += float64(v.ApexI) * v.Height
			apexDen += v.Height
			if v.Height > height {
				height = v.Height
			}
			if v.Prom > prom {
				prom = v.Prom
			}
			if v.Shape.LeftI < left {
				left = v.Shape.LeftI
			}
			if v.Shape.RightI > right {
				right = v.Shape.RightI
			}
			v.Status = "merged"
			v.GroupID = group
		}
		apexI := int(apexNum/apexDen + 0.5)
		if apexI < left {
			apexI = left
		}
		if apexI > right {
			apexI = right
		}
		merged := &model.ViewPeak{
			PeakID: idGen.New(), DetVer: detVer, Ver: members[0].Ver,
			Status: "active", ApexI: apexI, ApexT: times[apexI],
			Height: height, Area: areaSum, Prom: prom, WidthI: right - left,
			Shape: model.Shape{
				ApexAt: "merged", ApexT: times[apexI],
				LeftI: left, RightI: right, LeftT: times[left], RightT: times[right],
			},
			GroupID: group, Note: ev.Note,
		}
		out = append(out, merged)
	case model.OpSplit:
		if len(ev.PeakIDs) != 1 {
			return nil, fmt.Errorf("split needs exactly one peak")
		}
		v, err := byID(ev.PeakIDs[0])
		if err != nil {
			return nil, err
		}
		if v.Status != "active" {
			return nil, fmt.Errorf("peak %s is %s", v.PeakID, v.Status)
		}
		at := ev.AtI
		if at <= v.Shape.LeftI || at >= v.Shape.RightI {
			return nil, fmt.Errorf("split point %d outside bounds [%d,%d]", at, v.Shape.LeftI, v.Shape.RightI)
		}
		group := ev.GroupID
		if group == "" {
			group = idGen.New()
		}
		mkChild := func(lo, hi int) *model.ViewPeak {
			apexI := lo
			for i := lo + 1; i <= hi; i++ {
				if signal[i] > signal[apexI] {
					apexI = i
				}
			}
			return &model.ViewPeak{
				PeakID: idGen.New(), DetVer: v.DetVer, Ver: v.Ver,
				Status: "active", ApexI: apexI, ApexT: times[apexI],
				Height: signal[apexI], Area: trapz(times, signal, lo, hi),
				Prom: v.Prom, WidthI: hi - lo,
				Shape: model.Shape{
					ApexAt: "split", ApexT: times[apexI],
					LeftI: lo, RightI: hi, LeftT: times[lo], RightT: times[hi],
				},
				GroupID: group,
			}
		}
		left := mkChild(v.Shape.LeftI, at)
		right := mkChild(at, v.Shape.RightI)
		v.Status = "split"
		v.GroupID = group
		out = append(out, left, right)
	default:
		return nil, fmt.Errorf("unknown op %q", ev.Op)
	}
	return out, nil
}

// FromDetection builds the initial working list from a detection.
func FromDetection(pks []*model.Peak, ver int) []*model.ViewPeak {
	out := make([]*model.ViewPeak, 0, len(pks))
	for _, pk := range pks {
		out = append(out, &model.ViewPeak{
			PeakID: pk.ID, DetectionID: pk.DetectionID, DetVer: ver, Ver: ver,
			Status: "active", ApexI: pk.ApexI, ApexT: pk.Shape.ApexT,
			Height: pk.Height, Area: pk.Area, Prom: pk.Prominence,
			WidthI: pk.WidthI, Shape: pk.Shape,
		})
	}
	return out
}
