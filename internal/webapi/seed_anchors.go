package webapi

import "peakvalley/internal/model"

// seedAnchors creates anchors from reference peaks that find a selected
// suggestion in each other run.
func seedAnchors(s *Server, alID string, runIDs []string) error {
	_, refItems, err := s.App.CurrentItems(runIDs[0])
	if err != nil {
		return err
	}
	rev := 0
	for _, pk := range refItems {
		if pk.Status != "active" {
			continue
		}
		var points []model.AnchorPoint
		for _, rid := range runIDs[1:] {
			sugs, err := s.App.Suggest(alID, rid, 3)
			if err != nil {
				return err
			}
			for _, sg := range sugs {
				if sg.RefPeakID == pk.PeakID && sg.Selected {
					points = append(points, model.AnchorPoint{RunID: rid, PeakID: sg.RunPeakID, Manual: false})
					break
				}
			}
		}
		if len(points) == 0 {
			continue
		}
		if _, plan, err := s.App.AddAnchor(alID, "", pk.PeakID, points, rev, true); err != nil {
			return err
		} else if plan != nil {
			rev = plan.Rev
		}
	}
	return nil
}
