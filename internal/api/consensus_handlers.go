package api

import (
	"net/http"

	"peakcompass/internal/consensus"
	"peakcompass/internal/domain"
)

type buildConsensusReq struct {
	BaseVersion int      `json:"baseVersion"`
	RefRunID    string   `json:"refRunId"`
	RunIDs      []string `json:"runIds"`
	NormRule    string   `json:"normRule"`
	Tol         float64  `json:"tol"`
	Freeze      bool     `json:"freeze"`
}

func (s *Server) buildConsensus(w http.ResponseWriter, r *http.Request) {
	batch := r.PathValue("batch")
	var req buildConsensusReq
	if err := readJSON(r, &req); err != nil {
		s.errorf(w, 400, "invalid body: %v", err)
		return
	}
	if req.NormRule == "" {
		req.NormRule = consensus.NormTotalArea
	}
	if req.Tol == 0 {
		req.Tol = 0.4
	}
	ref, err := s.Store.GetRun(r.Context(), req.RefRunID)
	if err != nil {
		s.mapErr(w, err)
		return
	}
	runs := make([]domain.Run, 0, len(req.RunIDs))
	maps := make([]*domain.Mapping, 0, len(req.RunIDs))
	sets := make([]domain.PeakSet, 0, len(req.RunIDs))
	for _, id := range req.RunIDs {
		run, err := s.Store.GetRun(r.Context(), id)
		if err != nil {
			s.mapErr(w, err)
			return
		}
		m, err := s.Store.HeadMapping(r.Context(), req.RefRunID, id)
		if err != nil {
			s.errorf(w, 400, "run %s has no head mapping (freeze mappings first): %v", id, err)
			return
		}
		if !m.Frozen {
			s.errorf(w, 409, "run %s mapping v%d is not frozen", id, m.Version)
			return
		}
		ps, err := s.Store.HeadPeakSet(r.Context(), id)
		if err != nil {
			s.mapErr(w, err)
			return
		}
		runs = append(runs, *run)
		maps = append(maps, m)
		sets = append(sets, *ps)
	}
	c, err := consensus.Build(newID("cons"), batch, 0, *ref, runs, maps, sets,
		req.NormRule, req.Tol, "")
	if err != nil {
		s.errorf(w, 400, "consensus: %v", err)
		return
	}
	c.Frozen = req.Freeze
	saved, err := s.Store.CommitConsensus(r.Context(), *c, req.BaseVersion)
	if err != nil {
		s.mapErr(w, err)
		return
	}
	if req.Freeze {
		if err := s.Store.FreezeConsensus(r.Context(), batch, saved.Version); err != nil {
			s.mapErr(w, err)
			return
		}
		saved.Frozen = true
	}
	writeJSON(w, 201, saved)
}

func (s *Server) headConsensus(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.HeadConsensus(r.Context(), r.PathValue("batch"))
	if err != nil {
		s.mapErr(w, err)
		return
	}
	writeJSON(w, 200, c)
}

func (s *Server) listConsensusVersions(w http.ResponseWriter, r *http.Request) {
	out, err := s.Store.ListConsensusVersions(r.Context(), r.PathValue("batch"))
	if err != nil {
		s.mapErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"versions": out})
}

type consFreezeReq struct {
	Version int `json:"version"`
}

func (s *Server) freezeConsensus(w http.ResponseWriter, r *http.Request) {
	var req consFreezeReq
	if err := readJSON(r, &req); err != nil {
		s.errorf(w, 400, "invalid body: %v", err)
		return
	}
	if err := s.Store.FreezeConsensus(r.Context(), r.PathValue("batch"), req.Version); err != nil {
		s.mapErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"frozen": true, "version": req.Version})
}
