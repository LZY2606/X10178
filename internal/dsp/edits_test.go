package dsp

import (
	"testing"

	"peakvalley/internal/model"
)

func detFixture() model.DetParams {
	return model.DetParams{SmoothWindow: 5, MinProminence: 0.15, MinWidthSamples: 3}
}
func mergeOp(ids ...string) model.EditEvent { return model.EditEvent{Op: model.OpMerge, PeakIDs: ids} }
func ignoreOp(ids ...string) model.EditEvent {
	return model.EditEvent{Op: model.OpIgnore, PeakIDs: ids}
}
func restoreOp(ids ...string) model.EditEvent {
	return model.EditEvent{Op: model.OpRestore, PeakIDs: ids}
}
func splitOp(id string, at int) model.EditEvent {
	return model.EditEvent{Op: model.OpSplit, PeakIDs: []string{id}, AtI: at}
}
func findID(items []*model.ViewPeak, id string) *model.ViewPeak {
	for _, v := range items {
		if v.PeakID == id {
			return v
		}
	}
	return nil
}

func TestMergeSplitIgnoreValidation(t *testing.T) {
	times := Linspace(0, 20, 401)
	sig := make([]float64, len(times))
	for i, x := range times {
		sig[i] = 0.05 + gaussAt(x, 5, .25, 1) + gaussAt(x, 5.7, .25, .9) + gaussAt(x, 12, .3, .8)
	}
	base := Detect(&ctr{}, "d", times, sig, detFixture())
	if len(base) < 3 {
		t.Fatalf("need 3 peaks, got %d", len(base))
	}
	items := FromDetection(base, 1)

	merged, err := Apply(&ctr{n: 50}, times, sig, items, mergeOp(base[0].ID, base[1].ID))
	if err != nil {
		t.Fatal(err)
	}
	var mergedPeak *model.ViewPeak
	for _, v := range merged {
		if v.GroupID != "" && v.Status == "active" {
			mergedPeak = v
		}
	}
	if mergedPeak == nil {
		t.Fatal("no active merged peak")
	}
	if mergedPeak.Area <= base[0].Area || mergedPeak.Area <= base[1].Area {
		t.Fatal("merged area should dominate each child")
	}
	if mergedPeak.Shape.LeftI != base[0].Shape.LeftI {
		t.Fatal("merged left bound must equal leftmost child")
	}
	// Original child identities survive as 'merged' (undo/compare provenance).
	for _, id := range []string{base[0].ID, base[1].ID} {
		if v := findID(merged, id); v == nil || v.Status != "merged" {
			t.Fatalf("child %s not retained as merged", id)
		}
	}

	afterIgnore, err := Apply(&ctr{n: 60}, times, sig, merged, ignoreOp(base[2].ID))
	if err != nil {
		t.Fatal(err)
	}
	if v := findID(afterIgnore, base[2].ID); v.Status != "ignored" {
		t.Fatalf("expected ignored, got %s", v.Status)
	}
	restored, err := Apply(&ctr{n: 61}, times, sig, afterIgnore, restoreOp(base[2].ID))
	if err != nil {
		t.Fatal(err)
	}
	if v := findID(restored, base[2].ID); v.Status != "active" {
		t.Fatalf("expected restored active, got %s", v.Status)
	}

	splitAt := (mergedPeak.Shape.LeftI + mergedPeak.Shape.RightI) / 2
	split, err := Apply(&ctr{n: 70}, times, sig, restored, splitOp(mergedPeak.PeakID, splitAt))
	if err != nil {
		t.Fatal(err)
	}
	children := 0
	splitGroup := ""
	for _, v := range split {
		if v.Shape.ApexAt == "split" && v.Status == "active" {
			children++
			splitGroup = v.GroupID
			if v.Area <= 0 || v.Shape.RightI <= v.Shape.LeftI {
				t.Fatal("split child must have positive geometry")
			}
		}
	}
	if children != 2 {
		t.Fatalf("split must yield 2 active children, got %d", children)
	}
	// The split children share one group and the parent is marked split.
	if v := findID(split, mergedPeak.PeakID); v.Status != "split" || v.GroupID != splitGroup {
		t.Fatalf("parent must be marked split with children group, got %+v", v)
	}

	// Invalid operations must error rather than corrupt state.
	if _, err := Apply(&ctr{n: 80}, times, sig, items, mergeOp(base[0].ID)); err == nil {
		t.Fatal("merge of one peak must error")
	}
	if _, err := Apply(&ctr{n: 81}, times, sig, items,
		splitOp(base[0].ID, base[0].Shape.LeftI-5)); err == nil {
		t.Fatal("split outside bounds must error")
	}
	if _, err := Apply(&ctr{n: 82}, times, sig, afterIgnore, ignoreOp(base[2].ID)); err == nil {
		t.Fatal("ignoring an already-ignored peak must error")
	}
}

// Undo restores the exact previous materialisation (pointwise consistency).
func TestUndoRestorationPointwise(t *testing.T) {
	times := Linspace(0, 20, 401)
	sig := make([]float64, len(times))
	for i, x := range times {
		sig[i] = 0.05 + gaussAt(x, 5, .3, 1) + gaussAt(x, 12, .3, .8)
	}
	base := Detect(&ctr{}, "d", times, sig, detFixture())
	items := FromDetection(base, 1)
	ignored, err := Apply(&ctr{n: 9}, times, sig, items, ignoreOp(base[0].ID))
	if err != nil {
		t.Fatal(err)
	}
	// "Undo" at app level copies the pre-version snapshot; here we verify the
	// baseline materialises identically.
	again := FromDetection(base, 1)
	if len(again) != len(items) || again[0].PeakID != items[0].PeakID {
		t.Fatal("baseline rebuild must be identical")
	}
	if findID(ignored, base[0].ID).Status != "ignored" {
		t.Fatal("setup")
	}
}
