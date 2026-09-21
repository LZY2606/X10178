package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"peakcompass/internal/detect"
	"peakcompass/internal/domain"
	"peakcompass/internal/peaksets"
)

func newID(prefix string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return prefix + "-" + hex.EncodeToString(b)
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.Store.ListRuns(r.Context())
	if err != nil {
		s.mapErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"runs": runs})
}

// ingestRequest accepts a run with raw samples plus sample/instrument metadata.
type ingestRequest struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Sample     string            `json:"sample"`
	Instrument string            `json:"instrument"`
	Batch      string            `json:"batch"`
	Times      []float64         `json:"times"`
	Values     []float64         `json:"values"`
	Meta       map[string]string `json:"meta"`
}

func (s *Server) ingestRun(w http.ResponseWriter, r *http.Request) {
	var req ingestRequest
	if err := readJSON(r, &req); err != nil {
		s.errorf(w, 400, "invalid body: %v", err)
		return
	}
	if len(req.Times) != len(req.Values) || len(req.Times) < 3 {
		s.errorf(w, 400, "times/values must be equal length >= 3")
		return
	}
	if req.ID == "" {
		req.ID = newID("run")
	}
	if req.Batch == "" {
		req.Batch = "default"
	}
	run := domain.Run{
		ID: req.ID, Name: req.Name, Sample: req.Sample, Instrument: req.Instrument,
		Batch: req.Batch, Times: req.Times, Values: req.Values, Meta: req.Meta,
	}
	if err := s.Store.CreateRun(r.Context(), run); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "constraint") {
			s.errorf(w, 409, "run id %s already exists", req.ID)
			return
		}
		s.mapErr(w, err)
		return
	}
	writeJSON(w, 201, run)
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.Store.GetRun(r.Context(), r.PathValue("id"))
	if err != nil {
		s.mapErr(w, err)
		return
	}
	writeJSON(w, 200, run)
}

type detectRequest struct {
	Params *domain.DetParams `json:"params"`
}

type detectResponse struct {
	Detection domain.Detection `json:"detection"`
	PeakSet   domain.PeakSet   `json:"peakSet"`
	Migration json.RawMessage  `json:"migration,omitempty"`
}

func (s *Server) detect(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	run, err := s.Store.GetRun(r.Context(), runID)
	if err != nil {
		s.mapErr(w, err)
		return
	}
	var req detectRequest
	params := domain.DefaultParams()
	if r.ContentLength != 0 {
		if err := readJSON(r, &req); err != nil {
			s.errorf(w, 400, "invalid body: %v", err)
			return
		}
		if req.Params != nil {
			params = *req.Params
		}
	}
	peaks, err := detect.Detect(run.Times, run.Values, params)
	if err != nil {
		s.errorf(w, 400, "detect: %v", err)
		return
	}
	detID := newID("det")
	d := domain.Detection{ID: detID, RunID: runID, Params: params, Peaks: peaks}
	if err := s.Store.SaveDetection(r.Context(), d); err != nil {
		s.mapErr(w, err)
		return
	}
	ps := peaksets.NewSet(runID, &d)
	ps.ID = newID("ps")
	// Migrate identities from the previous head peak-set if one exists. A
	// re-detection produces a real new revision (so the migration is undoable);
	// if the head is frozen we branch an editable draft first.
	prev, err := s.Store.HeadPeakSet(r.Context(), runID)
	if err == nil {
		base := prev
		if prev.Frozen {
			branched, berr := s.Store.BranchPeakSet(r.Context(), runID, newID("ps"), prev.ID)
			if berr != nil {
				s.mapErr(w, berr)
				return
			}
			base = branched
		}
		mig := peaksets.Migrate(*base, peaks, 0.35, 0.35)
		migrated := peaksets.ApplyMigration(runID, newID("ps"), detID, *base, peaks, mig)
		saved, cerr := s.Store.CommitPeakSet(r.Context(), runID, migrated.ID, base.Version, migrated.Peaks, "re-detect identity migration")
		if cerr != nil {
			s.mapErr(w, cerr)
			return
		}
		writeJSON(w, 201, map[string]any{
			"detection": d, "peakSet": saved, "migration": mig,
		})
		return
	}
	if err := s.Store.SavePeakSet(r.Context(), ps); err != nil {
		s.mapErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"detection": d, "peakSet": ps})
}

func (s *Server) latestDetection(w http.ResponseWriter, r *http.Request) {
	d, err := s.Store.LatestDetection(r.Context(), r.PathValue("id"))
	if err != nil {
		s.mapErr(w, err)
		return
	}
	writeJSON(w, 200, d)
}

func (s *Server) headPeakSet(w http.ResponseWriter, r *http.Request) {
	ps, err := s.Store.HeadPeakSet(r.Context(), r.PathValue("id"))
	if err != nil {
		s.mapErr(w, err)
		return
	}
	writeJSON(w, 200, ps)
}

func (s *Server) listPeakSetVersions(w http.ResponseWriter, r *http.Request) {
	out, err := s.Store.ListPeakSetVersions(r.Context(), r.PathValue("id"))
	if err != nil {
		s.mapErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"versions": out})
}

func (s *Server) getPeakSet(w http.ResponseWriter, r *http.Request) {
	v, err := atoi(r.PathValue("version"))
	if err != nil {
		s.errorf(w, 400, "bad version")
		return
	}
	ps, err := s.Store.GetPeakSet(r.Context(), r.PathValue("id"), v)
	if err != nil {
		s.mapErr(w, err)
		return
	}
	writeJSON(w, 200, ps)
}

type branchRequest struct {
	FromSetID string `json:"fromSetId"`
}

func (s *Server) branchPeakSet(w http.ResponseWriter, r *http.Request) {
	var req branchRequest
	if err := readJSON(r, &req); err != nil {
		s.errorf(w, 400, "invalid body: %v", err)
		return
	}
	runID := r.PathValue("id")
	if req.FromSetID == "" {
		head, err := s.Store.HeadPeakSet(r.Context(), runID)
		if err != nil {
			s.mapErr(w, err)
			return
		}
		req.FromSetID = head.ID
	}
	ps, err := s.Store.BranchPeakSet(r.Context(), runID, newID("ps"), req.FromSetID)
	if err != nil {
		s.mapErr(w, err)
		return
	}
	writeJSON(w, 201, ps)
}

func (s *Server) freezePeakSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version int `json:"version"`
	}
	if err := readJSON(r, &req); err != nil {
		s.errorf(w, 400, "invalid body: %v", err)
		return
	}
	if err := s.Store.FreezePeakSet(r.Context(), r.PathValue("id"), req.Version); err != nil {
		s.mapErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"frozen": true, "version": req.Version})
}

// editRequest carries the base version (optimistic concurrency) and edit args.
type editRequest struct {
	BaseVersion int      `json:"baseVersion"`
	IDs         []string `json:"ids"`
	ID          string   `json:"id"`
	BoundaryIdx int      `json:"boundaryIdx"`
}

func (s *Server) mergePeaks(w http.ResponseWriter, r *http.Request) {
	var req editRequest
	if err := readJSON(r, &req); err != nil || len(req.IDs) < 2 {
		s.errorf(w, 400, "merge needs baseVersion and >=2 ids")
		return
	}
	runID := r.PathValue("id")
	run, head, ok := s.loadRunAndHead(w, r, runID, req.BaseVersion)
	if !ok {
		return
	}
	next, err := peaksets.Merge(*head, run.Times, run.Values, req.IDs, newID("pk"))
	if err != nil {
		s.mapErr(w, err)
		return
	}
	s.commitPeaks(w, r, runID, next, head.Version, "merge")
}

func (s *Server) splitPeak(w http.ResponseWriter, r *http.Request) {
	var req editRequest
	if err := readJSON(r, &req); err != nil || req.ID == "" {
		s.errorf(w, 400, "split needs baseVersion and id")
		return
	}
	runID := r.PathValue("id")
	run, head, ok := s.loadRunAndHead(w, r, runID, req.BaseVersion)
	if !ok {
		return
	}
	next, err := peaksets.Split(*head, run.Times, run.Values, req.ID,
		newID("pk"), newID("pk"), req.BoundaryIdx)
	if err != nil {
		s.mapErr(w, err)
		return
	}
	s.commitPeaks(w, r, runID, next, head.Version, "split")
}

func (s *Server) ignorePeak(w http.ResponseWriter, r *http.Request) {
	s.simpleEdit(w, r, true)
}

func (s *Server) restorePeak(w http.ResponseWriter, r *http.Request) {
	s.simpleEdit(w, r, false)
}

func (s *Server) simpleEdit(w http.ResponseWriter, r *http.Request, ignore bool) {
	var req editRequest
	if err := readJSON(r, &req); err != nil || req.ID == "" {
		s.errorf(w, 400, "need baseVersion and id")
		return
	}
	runID := r.PathValue("id")
	_, head, ok := s.loadRunAndHead(w, r, runID, req.BaseVersion)
	if !ok {
		return
	}
	var (
		next domain.PeakSet
		err  error
	)
	if ignore {
		next, err = peaksets.Ignore(*head, req.ID)
	} else {
		next, err = peaksets.Restore(*head, req.ID)
	}
	if err != nil {
		s.mapErr(w, err)
		return
	}
	verb := "ignore"
	if !ignore {
		verb = "restore"
	}
	s.commitPeaks(w, r, runID, next, head.Version, verb)
}

func (s *Server) loadRunAndHead(w http.ResponseWriter, r *http.Request, runID string, baseVer int) (*domain.Run, *domain.PeakSet, bool) {
	run, err := s.Store.GetRun(r.Context(), runID)
	if err != nil {
		s.mapErr(w, err)
		return nil, nil, false
	}
	head, err := s.Store.HeadPeakSet(r.Context(), runID)
	if err != nil {
		s.mapErr(w, err)
		return nil, nil, false
	}
	if head.Version != baseVer {
		writeJSON(w, 409, map[string]any{
			"error":         fmt.Sprintf("base v%d is stale, head is v%d", baseVer, head.Version),
			"code":          "stale_version",
			"serverVersion": head.Version,
		})
		return nil, nil, false
	}
	return run, head, true
}

func (s *Server) commitPeaks(w http.ResponseWriter, r *http.Request, runID string, next domain.PeakSet, baseVer int, note string) {
	saved, err := s.Store.CommitPeakSet(r.Context(), runID, newID("ps"), baseVer, next.Peaks, note)
	if err != nil {
		s.mapErr(w, err)
		return
	}
	writeJSON(w, 201, saved)
}

type revertRequest struct {
	BaseVersion   int `json:"baseVersion"`
	TargetVersion int `json:"targetVersion"`
}

// revertPeakSet creates a new revision that restores an older version's peaks.
func (s *Server) revertPeakSet(w http.ResponseWriter, r *http.Request) {
	var req revertRequest
	if err := readJSON(r, &req); err != nil || req.TargetVersion == 0 {
		s.errorf(w, 400, "need baseVersion and targetVersion")
		return
	}
	runID := r.PathValue("id")
	base := req.BaseVersion
	if _, err := s.Store.GetPeakSet(r.Context(), runID, req.TargetVersion); err != nil {
		s.mapErr(w, err)
		return
	}
	// Frozen head: branch into a draft, then revert on top of that branch.
	head, err := s.Store.HeadPeakSet(r.Context(), runID)
	if err != nil {
		s.mapErr(w, err)
		return
	}
	if head.Frozen {
		branched, err := s.Store.BranchPeakSet(r.Context(), runID, newID("ps"), head.ID)
		if err != nil {
			s.mapErr(w, err)
			return
		}
		base = branched.Version
	} else if base == 0 {
		base = head.Version
	}
	saved, err := s.Store.RevertPeakSet(r.Context(), runID, base, req.TargetVersion,
		fmt.Sprintf("revert to v%d", req.TargetVersion))
	if err != nil {
		s.mapErr(w, err)
		return
	}
	writeJSON(w, 201, saved)
}
