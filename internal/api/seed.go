package api

import (
	"context"
	"net/http"

	"peakcompass/internal/detect"
	"peakcompass/internal/domain"
	"peakcompass/internal/fixtures"
	"peakcompass/internal/peaksets"
	"peakcompass/internal/warp"
)

// SeedResult summarizes what was created (or already present).
type SeedResult struct {
	Created   bool     `json:"created"`
	RunIDs    []string `json:"runIds"`
	Reference string   `json:"reference"`
	Mappings  []string `json:"mappings"`
	Consensus string   `json:"consensus,omitempty"`
}

func (s *Server) seed(w http.ResponseWriter, r *http.Request) {
	res, err := s.seedDo(r.Context(), true)
	if err != nil {
		s.errorf(w, 400, "seed: %v", err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) seedDo(ctx context.Context, withConsensus bool) (*SeedResult, error) {
	runs := fixtures.All()
	exists, err := s.Store.RunExists(ctx, runs[0].ID)
	if err != nil {
		return nil, err
	}
	out := &SeedResult{Created: !exists, Reference: runs[0].ID}
	for _, run := range runs {
		out.RunIDs = append(out.RunIDs, run.ID)
		ok, err := s.Store.RunExists(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		if ok {
			continue
		}
		if err := s.Store.CreateRun(ctx, run); err != nil {
			return nil, err
		}
	}

	params := domain.DefaultParams()
	dets := map[string]*domain.Detection{}
	pss := map[string]*domain.PeakSet{}
	for _, run := range runs {
		if _, err := s.Store.LatestDetection(ctx, run.ID); err == nil {
			d, _ := s.Store.LatestDetection(ctx, run.ID)
			ps, _ := s.Store.HeadPeakSet(ctx, run.ID)
			dets[run.ID], pss[run.ID] = d, ps
			continue
		}
		pks, err := detect.Detect(run.Times, run.Values, params)
		if err != nil {
			return nil, err
		}
		d := &domain.Detection{ID: "det-" + run.ID, RunID: run.ID, Params: params, Peaks: pks}
		if err := s.Store.SaveDetection(ctx, *d); err != nil {
			return nil, err
		}
		ps := peaksets.NewSet(run.ID, d)
		ps.ID = "ps-" + run.ID + "-v1"
		if err := s.Store.SavePeakSet(ctx, ps); err != nil {
			return nil, err
		}
		dets[run.ID], pss[run.ID] = d, &ps
	}

	// Build and freeze a valid monotone mapping for every non-reference run,
	// with anchors at detected peak apex times (mapped through the known drift).
	ref := runs[0]
	for _, run := range runs[1:] {
		id := ref.ID + "->" + run.ID
		out.Mappings = append(out.Mappings, id)
		if _, err := s.Store.HeadMapping(ctx, ref.ID, run.ID); err == nil {
			continue
		}
		anchors := defaultAnchors(ref, run, pss[ref.ID], pss[run.ID])
		res := warp.Build("map-"+run.ID, ref.ID, run.ID, 1, anchors, nil, "")
		if !res.Valid {
			// fixtures guarantee monotone anchors; guard anyway
			continue
		}
		m := res.Mapping
		m.Frozen = true
		if _, err := s.Store.CommitMapping(ctx, *m, 0); err != nil {
			return nil, err
		}
		if err := s.Store.FreezeMapping(ctx, ref.ID, run.ID, 1); err != nil {
			return nil, err
		}
		if err := s.Store.FreezePeakSet(ctx, run.ID, pss[run.ID].Version); err != nil {
			return nil, err
		}
	}
	if err := s.Store.FreezePeakSet(ctx, ref.ID, pss[ref.ID].Version); err != nil {
		return nil, err
	}
	return out, nil
}

// defaultAnchors uses order-preserving peak sequence alignment so anchors can
// legitimately be absent in some runs (e.g. run-partial misses the first peak).
func defaultAnchors(ref, run domain.Run, refPS, runPS *domain.PeakSet) []domain.Anchor {
	var ra, ta []domain.Peak
	for _, p := range refPS.Peaks {
		if p.Status == domain.PeakActive {
			ra = append(ra, p)
		}
	}
	for _, p := range runPS.Peaks {
		if p.Status == domain.PeakActive {
			ta = append(ta, p)
		}
	}
	// Allow generous drift; order-preservation decides the pairing.
	pairs := warp.MatchPeaks(ra, ta, 3.0)
	out := make([]domain.Anchor, 0, len(pairs))
	for n, pr := range pairs {
		rp, tp := ra[pr[0]], ta[pr[1]]
		out = append(out, domain.Anchor{
			ID:     "anc-" + run.ID + "-" + itoa(n+1),
			X:      rp.ApexTime,
			Y:      tp.ApexTime,
			Kind:   domain.AnchorManual,
			PeakID: tp.ID,
		})
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	b := []byte{}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// SeedIfEmpty runs demo seeding only when no runs exist yet. Safe on restart.
func (s *Server) SeedIfEmpty(ctx context.Context) (*SeedResult, error) {
	runs, err := s.Store.ListRuns(ctx)
	if err != nil {
		return nil, err
	}
	if len(runs) > 0 {
		return &SeedResult{Created: false, RunIDs: runIDs(runs), Reference: "run-ref"}, nil
	}
	return s.seedDo(ctx, false)
}

func runIDs(runs []domain.Run) []string {
	out := make([]string, 0, len(runs))
	for _, r := range runs {
		out = append(out, r.ID)
	}
	return out
}
