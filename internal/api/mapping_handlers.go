package api

import (
	"net/http"
	"strconv"

	"peakcompass/internal/domain"
	"peakcompass/internal/warp"
)

func atoi(s string) (int, error) { return strconv.Atoi(s) }

type anchorDTO struct {
	ID     string  `json:"id"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Kind   string  `json:"kind"`
	PeakID string  `json:"peakId"`
	Locked bool    `json:"locked"`
	Note   string  `json:"note"`
}

type lockDTO struct {
	ID     string  `json:"id"`
	XStart float64 `json:"xStart"`
	XEnd   float64 `json:"xEnd"`
	Note   string  `json:"note"`
}

type mappingReq struct {
	BaseVersion int         `json:"baseVersion"`
	Anchors     []anchorDTO `json:"anchors"`
	Locks       []lockDTO   `json:"locks"`
}

func (m *mappingReq) toDomain() ([]domain.Anchor, []domain.SegmentLock) {
	as := make([]domain.Anchor, len(m.Anchors))
	for i, a := range m.Anchors {
		as[i] = domain.Anchor{ID: a.ID, X: a.X, Y: a.Y, Kind: a.Kind,
			PeakID: a.PeakID, Locked: a.Locked, Note: a.Note}
	}
	ls := make([]domain.SegmentLock, len(m.Locks))
	for i, l := range m.Locks {
		ls[i] = domain.SegmentLock{ID: l.ID, XStart: l.XStart, XEnd: l.XEnd, Note: l.Note}
	}
	return as, ls
}

func (s *Server) validateMapping(w http.ResponseWriter, r *http.Request) {
	refID, runID := r.PathValue("refID"), r.PathValue("runID")
	var req mappingReq
	if err := readJSON(r, &req); err != nil {
		s.errorf(w, 400, "invalid body: %v", err)
		return
	}
	anchors, locks := req.toDomain()
	res := warp.Build(newID("map"), refID, runID, req.BaseVersion, anchors, locks, "")
	status := 200
	resp := map[string]any{
		"valid":    res.Valid,
		"mapping":  res.Mapping,
		"rejected": res.Rejected,
	}
	if !res.Valid {
		status = 409
		resp["error"] = "anchor set is not monotone; resolve the minimum rejected set"
	}
	writeJSON(w, status, resp)
}

func (s *Server) commitMapping(w http.ResponseWriter, r *http.Request) {
	refID, runID := r.PathValue("refID"), r.PathValue("runID")
	var req mappingReq
	if err := readJSON(r, &req); err != nil {
		s.errorf(w, 400, "invalid body: %v", err)
		return
	}
	anchors, locks := req.toDomain()
	res := warp.Build(newID("map"), refID, runID, req.BaseVersion, anchors, locks, "")
	if !res.Valid {
		writeJSON(w, 409, map[string]any{
			"error":    "refusing to save: monotonicity destroyed",
			"code":     "non_monotone",
			"rejected": res.Rejected,
		})
		return
	}
	saved, err := s.Store.CommitMapping(r.Context(), *res.Mapping, req.BaseVersion)
	if err != nil {
		s.mapErr(w, err)
		return
	}
	writeJSON(w, 201, saved)
}

func (s *Server) loadMappingOrErr(w http.ResponseWriter, r *http.Request, refID, runID string, version int) *domain.Mapping {
	if version <= 0 {
		m, err := s.Store.HeadMapping(r.Context(), refID, runID)
		if err != nil {
			s.mapErr(w, err)
			return nil
		}
		return m
	}
	m, err := s.Store.GetMapping(r.Context(), refID, runID, version)
	if err != nil {
		s.mapErr(w, err)
		return nil
	}
	return m
}

type versionReq struct {
	Version int `json:"version"`
}

func (s *Server) align(w http.ResponseWriter, r *http.Request) {
	refID, runID := r.PathValue("refID"), r.PathValue("runID")
	var req versionReq
	if err := readJSON(r, &req); err != nil {
		s.errorf(w, 400, "invalid body: %v", err)
		return
	}
	m := s.loadMappingOrErr(w, r, refID, runID, req.Version)
	if m == nil {
		return
	}
	ref, err := s.Store.GetRun(r.Context(), refID)
	if err != nil {
		s.mapErr(w, err)
		return
	}
	run, err := s.Store.GetRun(r.Context(), runID)
	if err != nil {
		s.mapErr(w, err)
		return
	}
	pts, err := warp.Align(m, ref.Times, run.Times, run.Values)
	if err != nil {
		s.errorf(w, 400, "align: %v", err)
		return
	}
	writeJSON(w, 200, map[string]any{"aligned": pts, "mappingId": m.ID, "version": m.Version})
}

// recomputeReq moves one anchor and returns only the affected range. Locks that
// intersect the range block the recompute so locked points stay identical.
type recomputeReq struct {
	BaseVersion int         `json:"baseVersion"`
	Anchors     []anchorDTO `json:"anchors"`
	Locks       []lockDTO   `json:"locks"`
	AnchorID    string      `json:"anchorId"`
	NewX        float64     `json:"newX"`
	NewY        float64     `json:"newY"`
}

func (s *Server) recompute(w http.ResponseWriter, r *http.Request) {
	refID, runID := r.PathValue("refID"), r.PathValue("runID")
	var req recomputeReq
	if err := readJSON(r, &req); err != nil {
		s.errorf(w, 400, "invalid body: %v", err)
		return
	}

	tmp := mappingReq{Anchors: req.Anchors, Locks: req.Locks}
	anchors, locks := tmp.toDomain()
	var moved domain.Anchor
	for i := range anchors {
		if anchors[i].ID == req.AnchorID {
			anchors[i].X, anchors[i].Y = req.NewX, req.NewY
			moved = anchors[i]
		}
	}
	lo, hi := warp.AffectedRange(anchors, moved)
	if conflicts := warp.CheckLocks(locks, lo, hi); len(conflicts) > 0 {
		writeJSON(w, 409, map[string]any{
			"error":         "recompute intersects locked segment",
			"code":          "locked",
			"range":         map[string]float64{"lo": lo, "hi": hi},
			"lockConflicts": conflicts,
		})
		return
	}
	res := warp.Build(newID("map"), refID, runID, req.BaseVersion, anchors, locks, "")
	if !res.Valid {
		writeJSON(w, 409, map[string]any{
			"error": "moved anchor breaks monotonicity", "rejected": res.Rejected,
		})
		return
	}
	ref, err := s.Store.GetRun(r.Context(), refID)
	if err != nil {
		s.mapErr(w, err)
		return
	}
	run, err := s.Store.GetRun(r.Context(), runID)
	if err != nil {
		s.mapErr(w, err)
		return
	}
	all, err := warp.Align(res.Mapping, ref.Times, run.Times, run.Values)
	if err != nil {
		s.errorf(w, 400, "align: %v", err)
		return
	}
	seg := map[string]any{
		"range":     map[string]float64{"lo": lo, "hi": hi},
		"points":    sliceRange(all, lo, hi),
		"mappingId": res.Mapping.ID,
		"version":   req.BaseVersion,
		"candidate": res.Mapping,
	}
	writeJSON(w, 200, seg)
}

func sliceRange(pts []domain.LineagePoint, lo, hi float64) []domain.LineagePoint {
	out := make([]domain.LineagePoint, 0)
	for _, p := range pts {
		if p.RefTime >= lo && p.RefTime <= hi {
			out = append(out, p)
		}
	}
	return out
}

func (s *Server) residual(w http.ResponseWriter, r *http.Request) {
	refID, runID := r.PathValue("refID"), r.PathValue("runID")
	var req versionReq
	if err := readJSON(r, &req); err != nil {
		s.errorf(w, 400, "invalid body: %v", err)
		return
	}
	m := s.loadMappingOrErr(w, r, refID, runID, req.Version)
	if m == nil {
		return
	}
	ref, err := s.Store.GetRun(r.Context(), refID)
	if err != nil {
		s.mapErr(w, err)
		return
	}
	run, err := s.Store.GetRun(r.Context(), runID)
	if err != nil {
		s.mapErr(w, err)
		return
	}
	res, err := warp.Residual(m, ref.Times, ref.Values, run.Times, run.Values)
	if err != nil {
		s.errorf(w, 400, "residual: %v", err)
		return
	}
	writeJSON(w, 200, map[string]any{"residual": res, "version": m.Version})
}

type suggestReq struct {
	TopK int `json:"topK"`
}

func (s *Server) suggest(w http.ResponseWriter, r *http.Request) {
	refID, runID := r.PathValue("refID"), r.PathValue("runID")
	var req suggestReq
	if r.ContentLength != 0 {
		_ = readJSON(r, &req)
	}
	ref, err := s.Store.GetRun(r.Context(), refID)
	if err != nil {
		s.mapErr(w, err)
		return
	}
	run, err := s.Store.GetRun(r.Context(), runID)
	if err != nil {
		s.mapErr(w, err)
		return
	}
	refPS, err := s.Store.HeadPeakSet(r.Context(), refID)
	if err != nil {
		s.mapErr(w, err)
		return
	}
	runPS, err := s.Store.HeadPeakSet(r.Context(), runID)
	if err != nil {
		s.mapErr(w, err)
		return
	}
	props := warp.SuggestProposals(*ref, *run, refPS.Peaks, runPS.Peaks, req.TopK)
	writeJSON(w, 200, map[string]any{"suggestions": props})
}

func (s *Server) headMapping(w http.ResponseWriter, r *http.Request) {
	m, err := s.Store.HeadMapping(r.Context(), r.PathValue("refID"), r.PathValue("runID"))
	if err != nil {
		s.mapErr(w, err)
		return
	}
	writeJSON(w, 200, m)
}

func (s *Server) listMappingVersions(w http.ResponseWriter, r *http.Request) {
	out, err := s.Store.ListMappingVersions(r.Context(), r.PathValue("refID"), r.PathValue("runID"))
	if err != nil {
		s.mapErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"versions": out})
}

func (s *Server) getMapping(w http.ResponseWriter, r *http.Request) {
	v, err := atoi(r.PathValue("version"))
	if err != nil {
		s.errorf(w, 400, "bad version")
		return
	}
	m, err := s.Store.GetMapping(r.Context(), r.PathValue("refID"), r.PathValue("runID"), v)
	if err != nil {
		s.mapErr(w, err)
		return
	}
	writeJSON(w, 200, m)
}

type freezeReq struct {
	Version int `json:"version"`
}

func (s *Server) freezeMapping(w http.ResponseWriter, r *http.Request) {
	var req freezeReq
	if err := readJSON(r, &req); err != nil {
		s.errorf(w, 400, "invalid body: %v", err)
		return
	}
	if err := s.Store.FreezeMapping(r.Context(), r.PathValue("refID"), r.PathValue("runID"), req.Version); err != nil {
		s.mapErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"frozen": true, "version": req.Version})
}
