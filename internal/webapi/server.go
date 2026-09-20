// Package webapi serves the HTTP API and the browser page.
package webapi

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"

	"peakvalley/internal/app"
	"peakvalley/internal/model"
	"peakvalley/internal/store"
)

//go:embed static/index.html
var indexHTML embed.FS

// Server wires the app service to HTTP.
type Server struct {
	App *app.App
	Mux *http.ServeMux
}

// New constructs the HTTP server.
func New(a *app.App) *Server {
	s := &Server{App: a, Mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.Mux.HandleFunc("GET /api/state", s.handleState)
	s.Mux.HandleFunc("POST /api/runs", s.handleImport)
	s.Mux.HandleFunc("POST /api/runs/{id}/detect", s.handleDetect)
	s.Mux.HandleFunc("GET /api/runs/{id}/series", s.handleSeries)
	s.Mux.HandleFunc("GET /api/runs/{id}/peaks", s.handlePeaks)
	s.Mux.HandleFunc("POST /api/runs/{id}/edits", s.handleEdit)
	s.Mux.HandleFunc("POST /api/runs/{id}/undo", s.handleUndo)
	s.Mux.HandleFunc("GET /api/runs/{id}/history", s.handleHistory)
	s.Mux.HandleFunc("GET /api/runs/{id}/migrations", s.handleMigrations)

	s.Mux.HandleFunc("POST /api/alignment", s.handleEnsureAlignment)
	s.Mux.HandleFunc("POST /api/alignment/anchors", s.handleAddAnchor)
	s.Mux.HandleFunc("DELETE /api/alignment/anchors/{aid}", s.handleDeleteAnchor)
	s.Mux.HandleFunc("POST /api/alignment/anchors/{aid}/points", s.handleMovePoint)
	s.Mux.HandleFunc("GET /api/alignment/suggest/{rid}", s.handleSuggest)
	s.Mux.HandleFunc("POST /api/alignment/build", s.handleBuild)
	s.Mux.HandleFunc("POST /api/alignment/lock", s.handleLock)
	s.Mux.HandleFunc("POST /api/alignment/freeze", s.handleFreeze)
	s.Mux.HandleFunc("GET /api/runs/{id}/maps", s.handleMaps)
	s.Mux.HandleFunc("GET /api/runs/{id}/residuals", s.handleResiduals)
	s.Mux.HandleFunc("POST /api/runs/{id}/derive", s.handleDerive)
	s.Mux.HandleFunc("GET /api/runs/{id}/aligned", s.handleAligned)

	s.Mux.HandleFunc("POST /api/families/build", s.handleFamiliesBuild)
	s.Mux.HandleFunc("POST /api/families/merge", s.handleFamiliesMerge)
	s.Mux.HandleFunc("POST /api/families/ignore-member", s.handleFamilyIgnore)
	s.Mux.HandleFunc("POST /api/families/freeze", s.handleFamiliesFreeze)
	s.Mux.HandleFunc("POST /api/consensus/build", s.handleConsensusBuild)
	s.Mux.HandleFunc("POST /api/consensus/freeze", s.handleConsensusFreeze)
	s.Mux.HandleFunc("POST /api/demo/seed", s.handleSeed)

	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	s.Mux.Handle("GET /", http.FileServer(http.FS(sub)))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func errCode(err error) int {
	switch {
	case errors.Is(err, store.ErrConcurrent):
		return http.StatusConflict
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound
	default:
		return http.StatusBadRequest
	}
}

func fail(w http.ResponseWriter, err error) {
	writeJSON(w, errCode(err), map[string]string{"error": err.Error()})
}

var _ = model.Meta{}
