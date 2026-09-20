package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"peakvalley/internal/dsp"
	"peakvalley/internal/model"
	"peakvalley/internal/store"
)

// ImportRun validates and ingests one run. Times need not be uniform, but must
// be strictly increasing. The original series is stored verbatim as content
// addressed blobs and never rewritten by downstream operations.
func (a *App) ImportRun(name string, times, signal []float64, meta model.Meta) (*model.Run, error) {
	if len(times) < 5 {
		return nil, fmt.Errorf("%w: need at least 5 samples", errInput)
	}
	if len(times) != len(signal) {
		return nil, fmt.Errorf("%w: times/signal length mismatch", errInput)
	}
	for i := 1; i < len(times); i++ {
		if times[i] <= times[i-1] {
			return nil, fmt.Errorf("%w: times must be strictly increasing at %d", errInput, i)
		}
	}
	digest, err := store.SeriesDigest(times, signal)
	if err != nil {
		return nil, err
	}
	tb := store.EncodeFloats(times)
	sb := store.EncodeFloats(signal)
	tName := "t-" + digest
	sName := "y-" + digest
	h := sha256.Sum256(tb)
	tName = "t-" + hex.EncodeToString(h[:])
	h = sha256.Sum256(sb)
	sName = "y-" + hex.EncodeToString(h[:])
	if _, err := a.Store.WriteBlob(tName, tb); err != nil {
		return nil, err
	}
	if _, err := a.Store.WriteBlob(sName, sb); err != nil {
		return nil, err
	}
	r := &model.Run{
		ID: idGen.New(), Name: name, CreatedAt: nowUTC(),
		TimesBlob: tName, SignalBlob: sName, NPoints: len(times),
		T0: times[0], T1: times[len(times)-1],
		DT:   (times[len(times)-1] - times[0]) / float64(len(times)-1),
		Meta: meta, RawDigest: digest,
	}
	a.Store.Lock().Lock()
	defer a.Store.Lock().Unlock()
	tx, err := a.Store.BeginTx()
	if err != nil {
		return nil, err
	}
	if err := a.Store.InsertRun(tx, r); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return r, tx.Commit()
}

// LoadSeries reads the immutable original samples of a run.
func (a *App) LoadSeries(runID string) (r *model.Run, times, signal []float64, err error) {
	r, err = a.Store.GetRun(runID)
	if err != nil {
		return nil, nil, nil, err
	}
	tb, err := a.Store.ReadBlob(r.TimesBlob)
	if err != nil {
		return nil, nil, nil, err
	}
	sb, err := a.Store.ReadBlob(r.SignalBlob)
	if err != nil {
		return nil, nil, nil, err
	}
	if times, err = store.DecodeFloats(tb); err != nil {
		return nil, nil, nil, err
	}
	if signal, err = store.DecodeFloats(sb); err != nil {
		return nil, nil, nil, err
	}
	got, err := store.SeriesDigest(times, signal)
	if err != nil {
		return nil, nil, nil, err
	}
	if got != r.RawDigest {
		return nil, nil, nil, fmt.Errorf("raw digest mismatch for run %s: provenance broken", runID)
	}
	return r, times, signal, nil
}

// Detect (re)runs candidate detection. On a run that already had a detection
// it stores an identity migration keyed by boundary overlap, never by index.
func (a *App) Detect(runID string, p model.DetParams) (*model.Detection, []*model.Mig, error) {
	_, times, signal, err := a.LoadSeries(runID)
	if err != nil {
		return nil, nil, err
	}
	if p.PlateauMode == "" {
		p.PlateauMode = "first"
	}
	if p.SmoothWindow < 0 {
		return nil, nil, fmt.Errorf("%w: smooth window", errInput)
	}
	a.Store.Lock().Lock()
	defer a.Store.Lock().Unlock()
	tx, err := a.Store.BeginTx()
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var old *model.Detection
	oldVer := 0
	if d, err := a.Store.LatestDetection(runID); err == nil {
		old = d
		oldVer = d.Ver
	}
	ver, err := a.Store.NextDetVer(tx, runID)
	if err != nil {
		return nil, nil, err
	}
	d := &model.Detection{ID: idGen.New(), RunID: runID, Ver: ver, CreatedAt: nowUTC(), Params: p}
	d.Peaks = dsp.Detect(idGen, d.ID, times, signal, p)
	var migs []*model.Mig
	if old != nil {
		migs = dsp.Migrate(old.Peaks, d.Peaks)
		if err := a.Store.InsertMigration(tx, runID, oldVer, ver, migs); err != nil {
			return nil, nil, err
		}
	}
	if err := a.Store.InsertDetection(tx, d); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return d, migs, nil
}

// CurrentItems returns the materialised working list (latest edit version or
// the detection baseline) for a run.
func (a *App) CurrentItems(runID string) (det *model.Detection, items []*model.ViewPeak, err error) {
	det, err = a.Store.LatestDetection(runID)
	if err != nil {
		return nil, nil, fmt.Errorf("no detection for run %s", runID)
	}
	if ev, e := a.Store.LatestEditVer(runID); e == nil && ev != nil {
		return det, exportItems(ev.Items), nil
	}
	items = exportItems(dsp.FromDetection(det.Peaks, det.Ver))
	return det, items, nil
}
