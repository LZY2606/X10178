package webapi

import (
	"encoding/json"
	"net/http"

	"peakvalley/internal/dsp"
	"peakvalley/internal/model"
)

func baseline(d *model.Detection) []*model.ViewPeak {
	return dsp.FromDetection(d.Peaks, d.Ver)
}

type importReq struct {
	Name   string     `json:"name"`
	Times  []float64  `json:"times"`
	Signal []float64  `json:"signal"`
	Meta   model.Meta `json:"meta"`
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	var req importReq
	if err := readJSON(r, &req); err != nil {
		fail(w, err)
		return
	}
	rn, err := s.App.ImportRun(req.Name, req.Times, req.Signal, req.Meta)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, rn)
}

func (s *Server) handleDetect(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var p model.DetParams
	if err := readJSON(r, &p); err != nil {
		fail(w, err)
		return
	}
	d, migs, err := s.App.Detect(id, p)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"detection": d, "migrations": migs})
}

func (s *Server) handleSeries(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rn, times, signal, err := s.App.LoadSeries(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": rn, "times": times, "signal": signal})
}

func (s *Server) handlePeaks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, items, err := s.App.CurrentItems(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"detection": d, "items": items})
}

func (s *Server) handleEdit(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var ev model.EditEvent
	if err := readJSON(r, &ev); err != nil {
		fail(w, err)
		return
	}
	v, err := s.App.ApplyEdit(id, ev)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

type undoReq struct {
	Ver int `json:"ver"`
}

func (s *Server) handleUndo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req undoReq
	if err := readJSON(r, &req); err != nil {
		fail(w, err)
		return
	}
	v, err := s.App.Undo(id, req.Ver)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	evs, err := s.App.Store.ListEditVers(id)
	if err != nil {
		fail(w, err)
		return
	}
	if evs == nil {
		evs = []*model.EditVer{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": evs})
}

func (s *Server) handleMigrations(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ms, err := s.App.Store.ListMigrations(id)
	if err != nil {
		fail(w, err)
		return
	}
	if ms == nil {
		ms = nil
	}
	b, _ := json.Marshal(map[string]any{"migrations": ms})
	_, _ = w.Write(b)
}
