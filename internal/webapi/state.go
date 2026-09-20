package webapi

import (
	"net/http"

	"peakvalley/internal/model"
)

// State is the full UI snapshot: runs, peaks, alignment, maps, families.
type State struct {
	Runs      []RunState          `json:"runs"`
	Alignment *model.Alignment    `json:"alignment,omitempty"`
	Anchors   []*model.Anchor     `json:"anchors,omitempty"`
	Points    []model.AnchorPoint `json:"points,omitempty"`
	Plan      *model.BuildPlan    `json:"plan,omitempty"`
	Maps      map[string][]MapRef `json:"maps,omitempty"`
	Families  *model.FamilySet    `json:"families,omitempty"`
	Consensus *model.Consensus    `json:"consensus,omitempty"`
}

// RunState bundles a run with its detection/working list state.
type RunState struct {
	Run        *model.Run        `json:"run"`
	DetParams  *model.DetParams  `json:"det_params,omitempty"`
	DetVer     int               `json:"det_ver"`
	Items      []*model.ViewPeak `json:"items"`
	EditCount  int               `json:"edit_count"`
	HasAligned bool              `json:"has_aligned"`
}

// MapRef is a compact mapping descriptor.
type MapRef struct {
	ID       string          `json:"id"`
	Ver      int             `json:"ver"`
	Frozen   bool            `json:"frozen"`
	PlanRev  int             `json:"plan_rev"`
	Segments []model.Segment `json:"segments"`
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	st := State{Maps: map[string][]MapRef{}}
	runs, err := s.App.Store.ListRuns()
	if err != nil {
		fail(w, err)
		return
	}
	for _, rn := range runs {
		rs := RunState{Run: rn, Items: []*model.ViewPeak{}}
		if d, err := s.App.Store.LatestDetection(rn.ID); err == nil {
			rs.DetParams = &d.Params
			rs.DetVer = d.Ver
			if ev, e := s.App.Store.LatestEditVer(rn.ID); e == nil && ev != nil {
				rs.Items = ev.Items
				rs.EditCount = ev.Ver
			} else {
				rs.Items = baseline(d)
			}
		}
		if evs, err := s.App.Store.ListEditVers(rn.ID); err == nil {
			rs.EditCount = len(evs)
		}
		st.Runs = append(st.Runs, rs)
	}
	al, err := s.App.Store.FirstAlignment()
	if err == nil {
		st.Alignment = al
		if an, e := s.App.Store.ListAnchors(al.ID); e == nil {
			st.Anchors = an
		}
		if pt, e := s.App.Store.ListAnchorPoints(al.ID); e == nil {
			st.Points = pt
		}
		if al.PlanRev > 0 {
			if p, e := s.App.Store.GetPlan(al.ID, al.PlanRev); e == nil {
				st.Plan = p
			}
		}
		for _, rn := range runs {
			if rn.ID == al.RefRunID {
				continue
			}
			if mvs, e := s.App.Store.ListMapVersions(al.ID, rn.ID); e == nil && len(mvs) > 0 {
				for _, m := range mvs {
					st.Maps[rn.ID] = append(st.Maps[rn.ID], MapRef{
						ID: m.ID, Ver: m.Ver, Frozen: m.Frozen, PlanRev: m.PlanRev, Segments: m.Segments,
					})
				}
				last := mvs[len(mvs)-1]
				if _, e := s.App.Store.GetAligned(last.ID, rn.ID); e == nil {
					for i := range st.Runs {
						if st.Runs[i].Run.ID == rn.ID {
							st.Runs[i].HasAligned = true
						}
					}
				}
			}
		}
		if fs, c, e := s.App.GetConsensusState(al.ID); e == nil {
			st.Families = fs
			st.Consensus = c
		}
	}
	writeJSON(w, http.StatusOK, st)
}
