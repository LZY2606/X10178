package webapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"peakvalley/internal/app"
)

func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	a, err := app.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return New(a), dir
}

func do(t *testing.T, h http.Handler, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	out := map[string]any{}
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec.Code, out
}

func TestHTTPEndToEnd(t *testing.T) {
	srv, dir := newTestServer(t)
	// Seed deterministic demo.
	if code, body := do(t, srv.Mux, "POST", "/api/demo/seed", nil); code != 201 {
		t.Fatalf("seed: %d %v", code, body)
	}
	code, state := do(t, srv.Mux, "GET", "/api/state", nil)
	if code != 200 {
		t.Fatalf("state: %d", code)
	}
	if state["runs"] == nil || len(state["runs"].([]any)) != 3 {
		t.Fatal("want 3 seeded runs")
	}
	al := state["alignment"].(map[string]any)
	alID := al["id"].(string)
	plan := state["plan"].(map[string]any)
	if plan["reject_set"] != nil {
		t.Fatalf("seeded anchors must be monotone: %v", plan["reject_set"])
	}
	// Build maps.
	if code, body := do(t, srv.Mux, "POST", "/api/alignment/build", nil); code != 200 {
		t.Fatalf("build: %d %v", code, body)
	}
	// Stale concurrent anchor commit -> 409.
	anchors := state["anchors"].([]any)
	refA := anchors[0].(map[string]any)
	_ = refA
	runs := state["runs"].([]any)
	refRun := runs[0].(map[string]any)["run"].(map[string]any)["id"].(string)
	otherRun := runs[1].(map[string]any)["run"].(map[string]any)["id"].(string)
	refItems := runs[0].(map[string]any)["items"].([]any)
	otherItems := runs[1].(map[string]any)["items"].([]any)
	if len(refItems) < 2 || len(otherItems) < 2 {
		t.Fatal("need >=2 peaks for concurrency test")
	}
	pickRef := refItems[len(refItems)-1].(map[string]any)["peak_id"].(string)
	pickOther := otherItems[len(otherItems)-1].(map[string]any)["peak_id"].(string)
	body := map[string]any{
		"ref_peak_id": pickRef,
		"points":      []any{map[string]any{"run_id": otherRun, "peak_id": pickOther}},
		"expect_rev":  9999,
	}
	if code, _ := do(t, srv.Mux, "POST", "/api/alignment/anchors", body); code != http.StatusConflict {
		t.Fatalf("stale revision must be 409, got %d", code)
	}
	// Derive aligned curves for both non-reference runs.
	for i := 1; i < 3; i++ {
		rid := runs[i].(map[string]any)["run"].(map[string]any)["id"].(string)
		if code, b := do(t, srv.Mux, "POST", "/api/runs/"+rid+"/derive", map[string]any{}); code != 200 {
			t.Fatalf("derive %s: %d %v", rid, code, b)
		}
	}
	// Families + consensus.
	if code, b := do(t, srv.Mux, "POST", "/api/families/build", map[string]any{"tol_u": 0.2}); code != 200 {
		t.Fatalf("families: %d %v", code, b)
	}
	if code, b := do(t, srv.Mux, "POST", "/api/consensus/build", map[string]any{
		"rule": map[string]any{"method": "median_peak", "scale": 1},
	}); code != 200 {
		t.Fatalf("consensus: %d %v", code, b)
	}
	// Freeze everything; afterwards edits are rejected.
	do(t, srv.Mux, "POST", "/api/families/freeze", nil)
	do(t, srv.Mux, "POST", "/api/consensus/freeze", nil)
	if code, _ := do(t, srv.Mux, "POST", "/api/alignment/freeze", map[string]any{}); code != 200 {
		t.Fatalf("freeze map")
	}
	if code, b := do(t, srv.Mux, "POST", "/api/alignment/anchors", map[string]any{
		"ref_peak_id": pickRef, "points": []any{}, "expect_rev": 0,
	}); code == 200 {
		t.Fatalf("frozen alignment must reject edits, got 200 %v", b)
	}
	_ = alID
	_ = refRun

	// Restart: reopen the same data directory and confirm frozen state survives.
	if err := srv.App.Close(); err != nil {
		t.Fatal(err)
	}
	a2, err := app.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv2 := New(a2)
	defer a2.Close()
	_, state2 := do(t, srv2.Mux, "GET", "/api/state", nil)
	al2 := state2["alignment"].(map[string]any)
	if al2["locked"] != true {
		t.Fatal("frozen alignment must survive restart")
	}
	if state2["families"] == nil || state2["consensus"] == nil {
		t.Fatal("frozen families/consensus must survive restart")
	}
}

// The index page must expose the Chinese title.
func TestIndexTitle(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	srv.Mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("index: %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("峰谷罗盘")) {
		t.Fatal("page must contain 峰谷罗盘")
	}
}
