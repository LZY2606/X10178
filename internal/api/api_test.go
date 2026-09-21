package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"peakcompass/internal/domain"
	"peakcompass/internal/store"
)

type testEnv struct {
	srv     *Server
	ts      *httptest.Server
	dataDir string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open("file:" + filepath.Join(dir, "test.db") + "?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := New(st, dir)
	ts := httptest.NewServer(srv.Mux)
	t.Cleanup(ts.Close)
	if _, err := srv.SeedIfEmpty(context.Background()); err != nil {
		t.Fatal(err)
	}
	return &testEnv{srv: srv, ts: ts, dataDir: dir}
}

func (e *testEnv) do(t *testing.T, method, path string, body any, out any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, e.ts.URL+path, &buf)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	dec := json.NewDecoder(res.Body)
	var raw map[string]any
	_ = dec.Decode(&raw)
	if out != nil && res.StatusCode < 300 {
		b2, _ := json.Marshal(raw)
		_ = json.Unmarshal(b2, out)
	}
	return res.StatusCode, raw
}

func TestSeedAndPageTitle(t *testing.T) {
	e := newTestEnv(t)
	var runs struct {
		Runs []domain.Run `json:"runs"`
	}
	if code, _ := e.do(t, "GET", "/api/runs", nil, &runs); code != 200 || len(runs.Runs) != 4 {
		t.Fatalf("runs code=%d n=%d", code, len(runs.Runs))
	}
	res, err := http.Get(e.ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if !bytes.Contains(body, []byte("峰谷罗盘")) {
		t.Fatal("page must contain 峰谷罗盘")
	}
}

func TestCrossingAnchorsRejectedAndMinimum(t *testing.T) {
	e := newTestEnv(t)
	body := map[string]any{
		"baseVersion": -1,
		"anchors": []map[string]any{
			{"id": "a1", "x": 1, "y": 1, "kind": "manual"},
			{"id": "a2", "x": 2, "y": 3, "kind": "manual"},
			{"id": "a3", "x": 3, "y": 2, "kind": "manual"},
			{"id": "a4", "x": 4, "y": 4, "kind": "manual"},
		},
	}
	code, raw := e.do(t, "POST", "/api/mappings/run-ref/run-partial/commit", body, nil)
	if code != http.StatusConflict {
		t.Fatalf("commit code=%d want 409", code)
	}
	rejected, _ := raw["rejected"].([]any)
	if len(rejected) != 1 {
		t.Fatalf("rejected=%v want exactly 1 minimum anchor", rejected)
	}
	r0 := rejected[0].(map[string]any)
	if r0["anchorId"] != "a3" || r0["reason"] != "cross" {
		t.Fatalf("wrong rejection: %v", r0)
	}
	// Nothing was saved: head stays at the frozen seed v1.
	var head domain.Mapping
	if code, _ := e.do(t, "GET", "/api/mappings/run-ref/run-partial/head", nil, &head); code != 200 || head.Version != 1 || !head.Frozen {
		t.Fatalf("head must remain frozen v1, code=%d head=%+v", code, head)
	}
}

func TestValidMappingAlignAndLineage(t *testing.T) {
	e := newTestEnv(t)
	var ps domain.PeakSet
	e.do(t, "GET", "/api/runs/run-coarse/peakset", nil, &ps)
	body := map[string]any{
		"baseVersion": -1,
		"anchors": []map[string]any{
			{"id": "c1", "x": 2.1, "y": 2.0, "kind": "manual"},
			{"id": "c2", "x": 4.5, "y": 4.4, "kind": "manual"},
			{"id": "c3", "x": 8.0, "y": 7.8, "kind": "manual"},
			{"id": "c4", "x": 9.2, "y": 9.0, "kind": "manual"},
		},
	}
	var savedMap domain.Mapping
	if code, raw := e.do(t, "POST", "/api/mappings/run-ref/run-coarse/commit", body, &savedMap); code != 201 {
		t.Fatalf("commit code=%d raw=%v", code, raw)
	}
	// Coarse run has 0.15 grid; aligned still has provenance on every point.
	var aligned struct {
		Aligned []domain.LineagePoint `json:"aligned"`
	}
	if code, raw := e.do(t, "POST", "/api/mappings/run-ref/run-coarse/align", map[string]any{"version": savedMap.Version}, &aligned); code != 200 {
		t.Fatalf("align code=%d raw=%v", code, raw)
	}
	if len(aligned.Aligned) != 120 {
		t.Fatalf("aligned len=%d want 120 (reference grid)", len(aligned.Aligned))
	}
	for _, p := range aligned.Aligned {
		if p.MappingID == "" || p.MappingID != savedMap.ID || p.MapVer != savedMap.Version {
			t.Fatalf("lineage missing mapping/version: %+v want %s v%d", p, savedMap.ID, savedMap.Version)
		}
	}
}

func TestLocalRecomputeRespectsLock(t *testing.T) {
	e := newTestEnv(t)
	anchors := []map[string]any{
		{"id": "l1", "x": 1, "y": 1, "kind": "manual"},
		{"id": "l2", "x": 5, "y": 5, "kind": "manual"},
		{"id": "l3", "x": 9, "y": 9, "kind": "manual"},
	}
	body := map[string]any{"baseVersion": -1, "anchors": anchors}
	var sm domain.Mapping
	if code, raw := e.do(t, "POST", "/api/mappings/run-ref/run-partial/commit", body, &sm); code != 201 {
		t.Fatalf("commit=%d %v", code, raw)
	}
	// Moving the middle anchor without a lock succeeds and reports a sub-range.
	rec := map[string]any{
		"baseVersion": sm.Version, "anchors": anchors,
		"locks":    []any{},
		"anchorId": "l2", "newX": 5, "newY": 5.5,
	}
	code, raw := e.do(t, "POST", "/api/mappings/run-ref/run-partial/recompute", rec, nil)
	if code != 200 {
		t.Fatalf("recompute code=%d raw=%v", code, raw)
	}
	rng := raw["range"].(map[string]any)
	if rng["lo"].(float64) != 3 || rng["hi"].(float64) != 7 {
		t.Fatalf("affected range=%v want [3,7]", rng)
	}
	// Add a lock covering [4,6]; same move must now be refused.
	rec["locks"] = []map[string]any{{"id": "lockA", "xStart": 4, "xEnd": 6}}
	code, raw = e.do(t, "POST", "/api/mappings/run-ref/run-partial/recompute", rec, nil)
	if code != http.StatusConflict || raw["code"] != "locked" {
		t.Fatalf("locked recompute code=%d raw=%v", code, raw)
	}
}

func TestRedetectMigratesIdentities(t *testing.T) {
	e := newTestEnv(t)
	var before domain.PeakSet
	e.do(t, "GET", "/api/runs/run-shift/peakset", nil, &before)
	var res struct {
		PeakSet   domain.PeakSet `json:"peakSet"`
		Migration map[string]any `json:"migration"`
	}
	code, raw := e.do(t, "POST", "/api/runs/run-shift/detect",
		map[string]any{"params": domain.DefaultParams()}, &res)
	if code != 201 {
		t.Fatalf("redetect code=%d raw=%v", code, raw)
	}
	if res.Migration == nil {
		t.Fatal("re-detection must return a migration plan")
	}
	// Stable ids must survive: at least the first peak keeps its id.
	beforeIDs := map[string]bool{}
	for _, p := range before.Peaks {
		beforeIDs[p.ID] = true
	}
	kept := 0
	for _, p := range res.PeakSet.Peaks {
		if beforeIDs[p.ID] {
			kept++
		}
	}
	if kept == 0 {
		t.Fatalf("no stable ids migrated: after=%v", raw)
	}
}

func TestStaleConcurrentCommitRejected(t *testing.T) {
	e := newTestEnv(t)
	// run-coarse has a frozen seed v1; opening a new series produces v2.
	anchors := []map[string]any{
		{"id": "p1", "x": 2, "y": 2, "kind": "manual"},
		{"id": "p2", "x": 6, "y": 6, "kind": "manual"},
	}
	if code, raw := e.do(t, "POST", "/api/mappings/run-ref/run-coarse/commit",
		map[string]any{"baseVersion": -1, "anchors": anchors}, nil); code != 201 {
		t.Fatalf("new-series commit=%d %v", code, raw)
	}
	// An old frozen base v1 committing again concurrently must be rejected.
	anchors2 := []map[string]any{
		{"id": "q1", "x": 2, "y": 2.5, "kind": "manual"},
		{"id": "q2", "x": 6, "y": 6.5, "kind": "manual"},
	}
	code, raw := e.do(t, "POST", "/api/mappings/run-ref/run-coarse/commit",
		map[string]any{"baseVersion": 1, "anchors": anchors2}, nil)
	if code != http.StatusConflict || raw["code"] != "stale_version" {
		t.Fatalf("stale commit code=%d raw=%v", code, raw)
	}
	// Current base v2 advances to v3 normally.
	if code, _ := e.do(t, "POST", "/api/mappings/run-ref/run-coarse/commit",
		map[string]any{"baseVersion": 2, "anchors": anchors2}, nil); code != 201 {
		t.Fatalf("fresh base commit should succeed, code=%d", code)
	}
}

func TestPeakSetMergeAndUndoHistory(t *testing.T) {
	e := newTestEnv(t)
	var ps domain.PeakSet
	e.do(t, "GET", "/api/runs/run-shift/peakset", nil, &ps)
	ids := []string{}
	for _, p := range ps.Peaks {
		if p.Status == domain.PeakActive {
			ids = append(ids, p.ID)
		}
	}
	// Seeded peak-set is frozen; branch first, then merge.
	var draft domain.PeakSet
	if code, raw := e.do(t, "POST", "/api/runs/run-shift/peakset/branch",
		map[string]any{"fromSetId": ps.ID}, &draft); code != 201 {
		t.Fatalf("branch code=%d raw=%v", code, raw)
	}
	var merged domain.PeakSet
	if code, raw := e.do(t, "POST", "/api/runs/run-shift/peakset/merge",
		map[string]any{"baseVersion": draft.Version, "ids": ids[:2]}, &merged); code != 201 {
		t.Fatalf("merge code=%d raw=%v", code, raw)
	}
	if merged.Version <= draft.Version {
		t.Fatal("merge must create a new version")
	}
	var versions struct {
		Versions []domain.PeakSet `json:"versions"`
	}
	e.do(t, "GET", "/api/runs/run-shift/peakset/versions", nil, &versions)
	if len(versions.Versions) < 3 {
		t.Fatalf("versions=%d, history must retain seed+branch+merge", len(versions.Versions))
	}
}

func TestRestartRecoversDraftsAndFrozen(t *testing.T) {
	e := newTestEnv(t)
	var frozenMap domain.Mapping
	e.do(t, "GET", "/api/mappings/run-ref/run-shift/head", nil, &frozenMap)
	if !frozenMap.Frozen {
		t.Fatal("seed mapping should be frozen")
	}
	// Reopen the same data directory with a brand-new store/server (restart).
	st2, err := store.Open("file:" + filepath.Join(e.dataDir, "test.db") + "?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	srv2 := New(st2, e.dataDir)
	ts2 := httptest.NewServer(srv2.Mux)
	defer ts2.Close()
	defer st2.Close()
	// SeedIfEmpty must not overwrite existing data.
	res, _ := srv2.SeedIfEmpty(context.Background())
	if res.Created {
		t.Fatal("restart must detect existing data and skip seeding")
	}
	req, _ := http.NewRequest("GET", ts2.URL+"/api/mappings/run-ref/run-shift/head", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got domain.Mapping
	json.NewDecoder(resp.Body).Decode(&got)
	if got.Version != frozenMap.Version || !got.Frozen || len(got.Accepted) != len(frozenMap.Accepted) {
		t.Fatalf("frozen mapping not recovered: %+v", got)
	}
}

func TestRevertRestoresPreviousAsNewRevision(t *testing.T) {
	e := newTestEnv(t)
	var head domain.PeakSet
	e.do(t, "GET", "/api/runs/run-shift/peakset", nil, &head)
	// Seeded head is frozen: branch, ignore a peak, then revert to the branch.
	var draft domain.PeakSet
	e.do(t, "POST", "/api/runs/run-shift/peakset/branch", map[string]any{"fromSetId": head.ID}, &draft)
	ignoreID := ""
	for _, p := range draft.Peaks {
		if p.Status == domain.PeakActive {
			ignoreID = p.ID
			break
		}
	}
	var ignored domain.PeakSet
	if code, raw := e.do(t, "POST", "/api/runs/run-shift/peakset/ignore",
		map[string]any{"baseVersion": draft.Version, "id": ignoreID}, &ignored); code != 201 {
		t.Fatalf("ignore=%d %v", code, raw)
	}
	if findStatus(ignored.Peaks, ignoreID) != string(domain.PeakIgnored) {
		t.Fatal("peak not ignored in new revision")
	}
	// Revert to the branch revision (where the peak was still active).
	var reverted domain.PeakSet
	if code, raw := e.do(t, "POST", "/api/runs/run-shift/peakset/revert",
		map[string]any{"baseVersion": ignored.Version, "targetVersion": draft.Version}, &reverted); code != 201 {
		t.Fatalf("revert=%d %v", code, raw)
	}
	if reverted.Version != ignored.Version+1 {
		t.Fatalf("revert version=%d want %d", reverted.Version, ignored.Version+1)
	}
	if findStatus(reverted.Peaks, ignoreID) != string(domain.PeakActive) {
		t.Fatal("revert must restore the peak to active")
	}
}

func findStatus(pks []domain.Peak, id string) string {
	for _, p := range pks {
		if p.ID == id {
			return p.Status
		}
	}
	return ""
}

func TestRedetectPersistsMigratedPeakSet(t *testing.T) {
	e := newTestEnv(t)
	var before domain.PeakSet
	e.do(t, "GET", "/api/runs/run-coarse/peakset", nil, &before)
	var res struct {
		PeakSet domain.PeakSet `json:"peakSet"`
	}
	if code, raw := e.do(t, "POST", "/api/runs/run-coarse/detect",
		map[string]any{"params": domain.DefaultParams()}, &res); code != 201 {
		t.Fatalf("redetect=%d %v", code, raw)
	}
	// The migrated peak-set must now be the stored head (and > frozen seed v1).
	var stored domain.PeakSet
	e.do(t, "GET", "/api/runs/run-coarse/peakset", nil, &stored)
	if stored.Version <= before.Version || stored.Frozen {
		t.Fatalf("migrated set not persisted as new draft: %+v", stored)
	}
	if stored.ID != res.PeakSet.ID {
		t.Fatal("head id differs from the redetect response; migration was not saved")
	}
}
