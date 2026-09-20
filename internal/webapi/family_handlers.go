package webapi

import (
	"net/http"

	"peakvalley/internal/model"
)

type famBuildReq struct {
	TolU      float64 `json:"tol_u"`
	ExpectRev int     `json:"expect_rev"`
}

func (s *Server) handleFamiliesBuild(w http.ResponseWriter, r *http.Request) {
	var req famBuildReq
	if err := readJSON(r, &req); err != nil {
		fail(w, err)
		return
	}
	if req.TolU <= 0 {
		req.TolU = 0.15
	}
	al, err := s.App.Store.FirstAlignment()
	if err != nil {
		fail(w, err)
		return
	}
	fs, err := s.App.BuildFamilies(al.ID, req.TolU, req.ExpectRev)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, fs)
}

type famMergeReq struct {
	FamilyIDs []string `json:"family_ids"`
	ExpectRev int      `json:"expect_rev"`
}

func (s *Server) handleFamiliesMerge(w http.ResponseWriter, r *http.Request) {
	var req famMergeReq
	if err := readJSON(r, &req); err != nil {
		fail(w, err)
		return
	}
	al, err := s.App.Store.FirstAlignment()
	if err != nil {
		fail(w, err)
		return
	}
	fs, err := s.App.MergeFamilies(al.ID, req.FamilyIDs, req.ExpectRev)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, fs)
}

type famIgnoreReq struct {
	FamilyID  string `json:"family_id"`
	RunID     string `json:"run_id"`
	PeakID    string `json:"peak_id"`
	ExpectRev int    `json:"expect_rev"`
}

func (s *Server) handleFamilyIgnore(w http.ResponseWriter, r *http.Request) {
	var req famIgnoreReq
	if err := readJSON(r, &req); err != nil {
		fail(w, err)
		return
	}
	al, err := s.App.Store.FirstAlignment()
	if err != nil {
		fail(w, err)
		return
	}
	fs, err := s.App.IgnoreFamilyMember(al.ID, req.FamilyID, req.RunID, req.PeakID, req.ExpectRev)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, fs)
}

func (s *Server) handleFamiliesFreeze(w http.ResponseWriter, r *http.Request) {
	al, err := s.App.Store.FirstAlignment()
	if err != nil {
		fail(w, err)
		return
	}
	fs, err := s.App.FreezeFamilySet(al.ID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, fs)
}

type consReq struct {
	Rule      model.NormRule `json:"rule"`
	ExpectRev int            `json:"expect_rev"`
}

func (s *Server) handleConsensusBuild(w http.ResponseWriter, r *http.Request) {
	var req consReq
	if err := readJSON(r, &req); err != nil {
		fail(w, err)
		return
	}
	al, err := s.App.Store.FirstAlignment()
	if err != nil {
		fail(w, err)
		return
	}
	c, factors, err := s.App.BuildConsensusVersion(al.ID, req.Rule, req.ExpectRev)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"consensus": c, "factors": factors})
}

func (s *Server) handleConsensusFreeze(w http.ResponseWriter, r *http.Request) {
	al, err := s.App.Store.FirstAlignment()
	if err != nil {
		fail(w, err)
		return
	}
	c, err := s.App.FreezeConsensus(al.ID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}
