package webapi

import (
	"net/http"

	"peakvalley/internal/align"
	"peakvalley/internal/model"
)

type ensureReq struct {
	RefRunID string `json:"ref_run_id"`
	Name     string `json:"name"`
}

func (s *Server) handleEnsureAlignment(w http.ResponseWriter, r *http.Request) {
	var req ensureReq
	if err := readJSON(r, &req); err != nil {
		fail(w, err)
		return
	}
	if req.Name == "" {
		req.Name = "批次对齐"
	}
	al, err := s.App.EnsureAlignment(req.RefRunID, req.Name)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, al)
}

type pointReq struct {
	RunID  string `json:"run_id"`
	PeakID string `json:"peak_id"`
	Manual bool   `json:"manual"`
}

type addAnchorReq struct {
	Label     string     `json:"label"`
	RefPeakID string     `json:"ref_peak_id"`
	Points    []pointReq `json:"points"`
	Auto      bool       `json:"auto"`
	ExpectRev int        `json:"expect_rev"`
}

func (s *Server) handleAddAnchor(w http.ResponseWriter, r *http.Request) {
	var req addAnchorReq
	if err := readJSON(r, &req); err != nil {
		fail(w, err)
		return
	}
	al, err := s.App.Store.FirstAlignment()
	if err != nil {
		fail(w, err)
		return
	}
	var pts []model.AnchorPoint
	for _, p := range req.Points {
		pts = append(pts, model.AnchorPoint{RunID: p.RunID, PeakID: p.PeakID, Manual: p.Manual})
	}
	an, plan, err := s.App.AddAnchor(al.ID, req.Label, req.RefPeakID, pts, req.ExpectRev, req.Auto)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"anchor": an, "plan": plan})
}

func (s *Server) handleDeleteAnchor(w http.ResponseWriter, r *http.Request) {
	aid := r.PathValue("aid")
	var req struct {
		ExpectRev int `json:"expect_rev"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, err)
		return
	}
	al, err := s.App.Store.FirstAlignment()
	if err != nil {
		fail(w, err)
		return
	}
	plan, err := s.App.DeleteAnchor(al.ID, aid, req.ExpectRev)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plan": plan})
}

type moveReq struct {
	RunID     string `json:"run_id"`
	PeakID    string `json:"peak_id"`
	ExpectRev int    `json:"expect_rev"`
}

func (s *Server) handleMovePoint(w http.ResponseWriter, r *http.Request) {
	aid := r.PathValue("aid")
	var req moveReq
	if err := readJSON(r, &req); err != nil {
		fail(w, err)
		return
	}
	al, err := s.App.Store.FirstAlignment()
	if err != nil {
		fail(w, err)
		return
	}
	plan, err := s.App.MoveAnchorPoint(al.ID, aid, req.RunID, req.PeakID, req.ExpectRev)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plan": plan})
}

func (s *Server) handleSuggest(w http.ResponseWriter, r *http.Request) {
	rid := r.PathValue("rid")
	al, err := s.App.Store.FirstAlignment()
	if err != nil {
		fail(w, err)
		return
	}
	sugs, err := s.App.Suggest(al.ID, rid, 3)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": sugs})
}

func (s *Server) handleBuild(w http.ResponseWriter, r *http.Request) {
	al, err := s.App.Store.FirstAlignment()
	if err != nil {
		fail(w, err)
		return
	}
	maps, plan, err := s.App.BuildMaps(al.ID)
	if err != nil {
		if plan != nil {
			writeJSON(w, errCode(err), map[string]any{"error": err.Error(), "plan": plan})
			return
		}
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"maps": maps, "plan": plan})
}

type lockReq struct {
	RunID   string `json:"run_id"`
	SegIdxs []int  `json:"seg_idxs"`
}

func (s *Server) handleLock(w http.ResponseWriter, r *http.Request) {
	var req lockReq
	if err := readJSON(r, &req); err != nil {
		fail(w, err)
		return
	}
	al, err := s.App.Store.FirstAlignment()
	if err != nil {
		fail(w, err)
		return
	}
	m, err := s.App.LockSegments(al.ID, req.RunID, req.SegIdxs)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

type freezeReq struct {
	RunID string `json:"run_id"`
}

func (s *Server) handleFreeze(w http.ResponseWriter, r *http.Request) {
	var req freezeReq
	if r.ContentLength > 0 {
		if err := readJSON(r, &req); err != nil {
			fail(w, err)
			return
		}
	}
	al, err := s.App.Store.FirstAlignment()
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.App.FreezeMap(al.ID, req.RunID); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "frozen"})
}

func (s *Server) handleMaps(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	al, err := s.App.Store.FirstAlignment()
	if err != nil {
		fail(w, err)
		return
	}
	mvs, err := s.App.Store.ListMapVersions(al.ID, id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": mvs})
}

func (s *Server) handleResiduals(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	al, err := s.App.Store.FirstAlignment()
	if err != nil {
		fail(w, err)
		return
	}
	m, err := s.App.Store.LatestMapVersion(al.ID, id)
	if err != nil {
		fail(w, err)
		return
	}
	_, times, signal, err := s.App.LoadSeries(id)
	if err != nil {
		fail(w, err)
		return
	}
	anchors, err := s.App.Store.ListAnchors(al.ID)
	if err != nil {
		fail(w, err)
		return
	}
	pts, err := s.App.Store.ListAnchorPoints(al.ID)
	if err != nil {
		fail(w, err)
		return
	}
	refT := map[string]float64{}
	for _, a := range anchors {
		refT[a.ID] = a.RefT
	}
	var pps []align.PPoint
	pieces := make([]align.Piece, len(m.Segments))
	for i, seg := range m.Segments {
		pieces[i] = align.Piece{IL: seg.AnchorL, IR: seg.AnchorR, X0: seg.X0, X1: seg.X1, U0: seg.U0, U1: seg.U1, Slope: seg.Slope}
	}
	for _, p := range pts {
		if p.RunID != id {
			continue
		}
		pps = append(pps, align.PPoint{AnchorID: p.AnchorID, RefT: refT[p.AnchorID], T: p.T})
	}
	res := align.Residuals(pieces, times, signal, pps)
	writeJSON(w, http.StatusOK, map[string]any{"residuals": res, "map_ver": m.Ver})
}

type deriveReq struct {
	Ver int `json:"ver"`
}

func (s *Server) handleDerive(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req deriveReq
	if err := readJSON(r, &req); err != nil {
		fail(w, err)
		return
	}
	al, err := s.App.Store.FirstAlignment()
	if err != nil {
		fail(w, err)
		return
	}
	if req.Ver == 0 {
		m, e := s.App.Store.LatestMapVersion(al.ID, id)
		if e != nil {
			fail(w, e)
			return
		}
		req.Ver = m.Ver
	}
	al2, err := s.App.Derive(al.ID, id, req.Ver)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, al2)
}

func (s *Server) handleAligned(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	al, err := s.App.Store.FirstAlignment()
	if err != nil {
		fail(w, err)
		return
	}
	ver := 0
	if v := r.URL.Query().Get("ver"); v != "" {
		if _, err := parseInt(v, &ver); err != nil {
			fail(w, err)
			return
		}
	}
	if ver == 0 {
		m, e := s.App.Store.LatestMapVersion(al.ID, id)
		if e != nil {
			fail(w, e)
			return
		}
		ver = m.Ver
	}
	rec, u, y, err := s.App.LoadAligned(al.ID, id, ver)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"aligned": rec, "u": u, "signal": y})
}
