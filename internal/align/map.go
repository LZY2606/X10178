package align

import (
	"errors"
	"fmt"
	"sort"

	"peakvalley/internal/dsp"
)

// ErrLockedSegment is returned when a change would alter a locked piece.
var ErrLockedSegment = errors.New("segment locked: adjust denied to preserve pointwise consistency")

// LockKey identifies a piece by its bounding anchors; "" means an extrapolated
// outer piece. The key is independent of segment array position.
func LockKey(s SegmentLike) string {
	return s.AnchorL() + "->" + s.AnchorR()
}

// SegmentLike exposes the identity-bearing fields of a segment.
type SegmentLike interface {
	AnchorL() string
	AnchorR() string
	X0() float64
	X1() float64
	U0() float64
	U1() float64
	Locked() bool
}

type seg struct {
	il, ir         string
	x0, x1, u0, u1 float64
	locked         bool
}

func (s seg) AnchorL() string { return s.il }
func (s seg) AnchorR() string { return s.ir }
func (s seg) X0() float64     { return s.x0 }
func (s seg) X1() float64     { return s.x1 }
func (s seg) U0() float64     { return s.u0 }
func (s seg) U1() float64     { return s.u1 }
func (s seg) Locked() bool    { return s.locked }

// Piece is a concrete segment ready for storage in model.Segment.
type Piece struct {
	IL, IR         string
	X0, X1, U0, U1 float64
	Slope          float64
	Locked         bool
}

// Build constructs the piecewise-linear monotonic mapping.
// Outside the anchors the map extends with slope 1, preserving drift.
func Build(pts []PPoint) ([]Piece, error) {
	if len(pts) == 0 {
		return nil, errors.New("no anchor points")
	}
	ord := make([]PPoint, len(pts))
	copy(ord, pts)
	sort.SliceStable(ord, func(a, b int) bool {
		if ord[a].T != ord[b].T {
			return ord[a].T < ord[b].T
		}
		return ord[a].AnchorID < ord[b].AnchorID
	})
	var pieces []Piece
	// Left outer piece: slope 1 anchored at the first anchor (X0/U0 equal the
	// anchor values); evaluation extends as U = RefT + (x - T).
	first := ord[0]
	pieces = append(pieces, Piece{IL: "", IR: first.AnchorID, X0: first.T, X1: first.T, U0: first.RefT, U1: first.RefT, Slope: 1})
	for i := 0; i < len(ord); i++ {
		p := Piece{IL: ord[i].AnchorID, Locked: false}
		if i+1 < len(ord) {
			a, b := ord[i], ord[i+1]
			dx := b.T - a.T
			if dx <= 0 {
				return nil, fmt.Errorf("non-increasing run times at anchors %s,%s", a.AnchorID, b.AnchorID)
			}
			p.IR = b.AnchorID
			p.X0, p.X1 = a.T, b.T
			p.U0, p.U1 = a.RefT, b.RefT
			p.Slope = (b.RefT - a.RefT) / dx
			if p.Slope < 0 {
				return nil, fmt.Errorf("negative slope between %s and %s", a.AnchorID, b.AnchorID)
			}
		} else {
			// Right outer piece: slope 1 anchored at the last anchor.
			p.IR = ""
			p.X0, p.X1 = ord[i].T, ord[i].T
			p.U0, p.U1 = ord[i].RefT, ord[i].RefT
			p.Slope = 1
		}
		pieces = append(pieces, p)
	}
	return pieces, nil
}

// Eval evaluates a built map at run time x. Interior pieces are zero-width
// anchors are skipped; outer pieces are slope-1 rays anchored at the endpoints.
func Eval(pieces []Piece, x float64) float64 {
	var left, right *Piece
	for i := range pieces {
		p := &pieces[i]
		if p.IL == "" {
			left = p
			continue
		}
		if p.IR == "" {
			right = p
			continue
		}
		if x >= p.X0 && x <= p.X1 {
			if p.X1 == p.X0 {
				return p.U0
			}
			f := (x - p.X0) / (p.X1 - p.X0)
			return p.U0 + f*(p.U1-p.U0)
		}
	}
	if left != nil && x <= left.X1 {
		return left.U1 + (x-left.X1)*left.Slope
	}
	if right != nil && x >= right.X0 {
		return right.U0 + (x-right.X0)*right.Slope
	}
	return x
}

// LocalRebuild recomputes only the pieces touched by changing points, while
// previously locked pieces must remain byte-identical (pointwise consistency).
// touchedAnchors names anchors whose point moved, were added, or deleted.
func LocalRebuild(old []Piece, pts []PPoint, touched map[string]bool) ([]Piece, error) {
	fresh, err := Build(pts)
	if err != nil {
		return nil, err
	}
	// A locked segment is defined by its time/reference interval (geometry),
	// not by array index. After a rebuild every previously locked interval must
	// reappear byte-identical; if an edit changed it, the edit is rejected.
	lockedGeom := []Piece{}
	for _, p := range old {
		if p.Locked {
			lockedGeom = append(lockedGeom, p)
		}
	}
	out := make([]Piece, len(fresh))
	copy(out, fresh)
	preserve := map[string]bool{}
	for _, lp := range lockedGeom {
		matched := false
		for i := range out {
			if out[i].X0 == lp.X0 && out[i].X1 == lp.X1 && out[i].U0 == lp.U0 && out[i].U1 == lp.U1 {
				out[i].Locked = true
				out[i].IL, out[i].IR = lp.IL, lp.IR
				out[i].Slope = lp.Slope
				preserve[lp.IL+"->"+lp.IR] = true
				matched = true
				break
			}
		}
		if !matched {
			return nil, fmt.Errorf("%w: locked interval [%.6g,%.6g] changed", ErrLockedSegment, lp.X0, lp.X1)
		}
	}
	return out, nil
}

// Apply evaluates the map onto the original samples; signal is passed through
// unchanged, so every derived point traces to the same sample index.
func Apply(pieces []Piece, times, signal []float64) (u []float64, y []float64) {
	u = make([]float64, len(times))
	for i, x := range times {
		u[i] = Eval(pieces, x)
	}
	return u, signal
}

// Resample renders the aligned curve on a uniform grid (for overlays).
func Resample(u, signal []float64, n int) (ug []float64, yg []float64) {
	if len(u) < 2 || n < 2 {
		return nil, nil
	}
	ug = dsp.Linspace(u[0], u[len(u)-1], n)
	yg = make([]float64, n)
	k := 0
	for i, uu := range ug {
		for k+1 < len(u)-1 && u[k+1] < uu {
			k++
		}
		x0, x1 := u[k], u[k+1]
		f := 0.0
		if x1 > x0 {
			f = (uu - x0) / (x1 - x0)
		}
		yg[i] = signal[k] + f*(signal[k+1]-signal[k])
	}
	return ug, yg
}

// Residual is the local residual of one map at the anchor points: warp shift
// u - x, plus a piecewise chord error measured at sample midpoints.
type Residual struct {
	AnchorID string  `json:"anchor_id"`
	RefT     float64 `json:"ref_t"`
	RunT     float64 `json:"run_t"`
	Shift    float64 `json:"shift"`
	ChordMAE float64 `json:"chord_mae"`
	ChordMax float64 `json:"chord_max"`
}

// Residuals computes per-anchor shifts. ChordMAE/ChordMax measure how much the
// adjacent piece departs from the identity (no-warp) chord; zero means the run
// needed no local shift there, so real drift remains visible.
func Residuals(pieces []Piece, times, signal []float64, pts []PPoint) []Residual {
	depart := map[string]float64{}
	width := map[string]float64{}
	for _, p := range pieces {
		if p.IL == "" || p.IR == "" || p.X1 <= p.X0 {
			continue
		}
		d := absF(p.Slope-1) * (p.X1 - p.X0)
		depart[p.IL] = d
		width[p.IL] = p.X1 - p.X0
	}
	out := make([]Residual, 0, len(pts))
	ord := append([]PPoint(nil), pts...)
	sort.SliceStable(ord, func(a, b int) bool { return ord[a].RefT < ord[b].RefT })
	for _, p := range ord {
		r := Residual{AnchorID: p.AnchorID, RefT: p.RefT, RunT: p.T, Shift: p.RefT - p.T}
		if w := width[p.AnchorID]; w > 0 {
			r.ChordMAE = depart[p.AnchorID] / w
			r.ChordMax = depart[p.AnchorID]
		}
		out = append(out, r)
	}
	return out
}

func absF(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
