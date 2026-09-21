// Package api exposes the peak-valley compass over HTTP and serves the UI.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"

	"peakcompass/internal/store"
	"peakcompass/internal/web"
)

// Server wires the store to HTTP handlers.
type Server struct {
	Store   *store.Store
	DataDir string
	Mux     *http.ServeMux
}

// New builds the router.
func New(st *store.Store, dataDir string) *Server {
	s := &Server{Store: st, DataDir: dataDir, Mux: http.NewServeMux()}
	s.routes()
	static, err := fs.Sub(web.Static, "static")
	if err == nil {
		s.Mux.Handle("GET /", http.FileServer(http.FS(static)))
	}
	return s
}

func (s *Server) routes() {
	s.Mux.HandleFunc("GET /api/health", s.health)
	s.Mux.HandleFunc("GET /api/runs", s.listRuns)
	s.Mux.HandleFunc("POST /api/runs", s.ingestRun)
	s.Mux.HandleFunc("GET /api/runs/{id}", s.getRun)
	s.Mux.HandleFunc("POST /api/runs/{id}/detect", s.detect)
	s.Mux.HandleFunc("GET /api/runs/{id}/detections/latest", s.latestDetection)

	s.Mux.HandleFunc("GET /api/runs/{id}/peakset", s.headPeakSet)
	s.Mux.HandleFunc("GET /api/runs/{id}/peakset/versions", s.listPeakSetVersions)
	s.Mux.HandleFunc("GET /api/runs/{id}/peakset/v/{version}", s.getPeakSet)
	s.Mux.HandleFunc("POST /api/runs/{id}/peakset/branch", s.branchPeakSet)
	s.Mux.HandleFunc("POST /api/runs/{id}/peakset/freeze", s.freezePeakSet)
	s.Mux.HandleFunc("POST /api/runs/{id}/peakset/merge", s.mergePeaks)
	s.Mux.HandleFunc("POST /api/runs/{id}/peakset/split", s.splitPeak)
	s.Mux.HandleFunc("POST /api/runs/{id}/peakset/ignore", s.ignorePeak)
	s.Mux.HandleFunc("POST /api/runs/{id}/peakset/restore", s.restorePeak)
	s.Mux.HandleFunc("POST /api/runs/{id}/peakset/revert", s.revertPeakSet)

	s.Mux.HandleFunc("GET /api/mappings/{refID}/{runID}/head", s.headMapping)
	s.Mux.HandleFunc("GET /api/mappings/{refID}/{runID}/versions", s.listMappingVersions)
	s.Mux.HandleFunc("GET /api/mappings/{refID}/{runID}/v/{version}", s.getMapping)
	s.Mux.HandleFunc("POST /api/mappings/{refID}/{runID}/validate", s.validateMapping)
	s.Mux.HandleFunc("POST /api/mappings/{refID}/{runID}/commit", s.commitMapping)
	s.Mux.HandleFunc("POST /api/mappings/{refID}/{runID}/align", s.align)
	s.Mux.HandleFunc("POST /api/mappings/{refID}/{runID}/recompute", s.recompute)
	s.Mux.HandleFunc("POST /api/mappings/{refID}/{runID}/residual", s.residual)
	s.Mux.HandleFunc("POST /api/mappings/{refID}/{runID}/suggest", s.suggest)
	s.Mux.HandleFunc("POST /api/mappings/{refID}/{runID}/freeze", s.freezeMapping)

	s.Mux.HandleFunc("GET /api/consensus/{batch}/head", s.headConsensus)
	s.Mux.HandleFunc("GET /api/consensus/{batch}/versions", s.listConsensusVersions)
	s.Mux.HandleFunc("POST /api/consensus/{batch}/build", s.buildConsensus)
	s.Mux.HandleFunc("POST /api/consensus/{batch}/freeze", s.freezeConsensus)

	s.Mux.HandleFunc("POST /api/demo/seed", s.seed)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func (s *Server) errorf(w http.ResponseWriter, status int, format string, args ...any) {
	writeJSON(w, status, map[string]any{"error": fmt.Sprintf(format, args...)})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// mapErr translates store/concurrency errors to HTTP status codes.
func (s *Server) mapErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrVersion):
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "code": "stale_version"})
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error(), "code": "not_found"})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
}
