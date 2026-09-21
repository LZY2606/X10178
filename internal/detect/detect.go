// Package detect performs candidate peak detection on raw chromatograms.
//
// The detector never overwrites the raw curve: it only reads sample values and
// emits peaks that reference sample indices. Detection is fully deterministic:
// given identical samples and DetParams the output is bit-identical.
package detect

import (
	"fmt"
	"sort"

	"peakcompass/internal/domain"
)

type candidate struct {
	start, end int // inclusive plateau indices
	apex       int // chosen apex (leftmost summit sample)
	height     float64
	plateau    bool
}

// Detect runs detection and returns immutable peaks ordered by sample index.
// Peaks rejected by the filters are still returned with Rejected=true and an
// explanation, preserving ties for the "并列候选" review requirement.
func Detect(times, values []float64, p domain.DetParams) ([]domain.Peak, error) {
	if len(times) != len(values) {
		return nil, fmt.Errorf("times/values length mismatch: %d vs %d", len(times), len(values))
	}
	if len(times) < 3 {
		return nil, fmt.Errorf("need at least 3 samples, got %d", len(times))
	}

	cands := findPlateaus(values, p.LevelTolerance)

	peaks := make([]domain.Peak, 0, len(cands))
	for idx, c := range cands {
		leftBase, rightBase, prom := prominence(values, c.start, c.end)
		left := leftBase
		right := rightBase
		area := trapzArea(times, values, left, right)
		pk := domain.Peak{
			ID:         fmt.Sprintf("pk-det-%03d", idx+1),
			ApexIdx:    c.apex,
			LeftIdx:    left,
			RightIdx:   right,
			ApexTime:   times[c.apex],
			Height:     c.height,
			Area:       area,
			Prominence: prom,
			Plateau:    c.plateau,
			PlateauEnd: 0,
			Status:     domain.PeakActive,
			Source:     "detect",
		}
		if c.plateau {
			pk.PlateauEnd = c.end
		}
		if c.height < p.NoiseFloor {
			pk.Rejected, pk.RejectWhy = true, "noise_floor"
		} else if prom < p.Prominence {
			pk.Rejected, pk.RejectWhy = true, "low_prominence"
		}
		peaks = append(peaks, pk)
	}

	// Minimum-distance filtering: survivors fight in prominence order; equal
	// prominence ties break by greater height, then earlier index (deterministic).
	order := make([]int, len(peaks))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		x, y := peaks[order[a]], peaks[order[b]]
		if x.Prominence != y.Prominence {
			return x.Prominence > y.Prominence
		}
		if x.Height != y.Height {
			return x.Height > y.Height
		}
		return x.ApexIdx < y.ApexIdx
	})
	surviving := map[int]bool{}
	for _, i := range order {
		if peaks[i].Rejected {
			continue
		}
		conflict := -1
		for j := range surviving {
			if absInt(peaks[i].ApexIdx-peaks[j].ApexIdx) < p.MinDistanceIdx {
				conflict = j
				break
			}
		}
		if conflict >= 0 {
			peaks[i].Rejected = true
			peaks[i].RejectWhy = fmt.Sprintf("min_distance:too close to %s", peaks[conflict].ID)
			continue
		}
		surviving[i] = true
	}

	annotateTies(peaks)
	return peaks, nil
}

// findPlateaus groups maximal runs of equal (within tol) samples that are local
// maxima with strictly lower values on both sides, plus ordinary sharp peaks.
func findPlateaus(v []float64, tol float64) []candidate {
	var cands []candidate
	n := len(v)
	for i := 1; i < n-1; {
		j := i
		for j+1 < n && absFloat(v[j+1]-v[i]) <= tol {
			j++
		}
		// A summit group [i,j]; strict neighbours at i-1 and j+1.
		leftLower := v[i-1] < v[i]-tol
		rightLower := j+1 < n && v[j+1] < v[i]-tol
		if leftLower && rightLower {
			cands = append(cands, candidate{
				start: i, end: j, apex: i, height: v[i],
				plateau: j > i,
			})
		}
		i = j + 1
	}
	return cands
}

// prominence walks outward from the summit until values stop decreasing on each
// side, returning base indices and the summit-above-higher-base prominence.
func prominence(v []float64, l, r int) (int, int, float64) {
	top := v[l]
	li := l - 1
	for li > 0 && v[li-1] <= v[li] {
		li--
	}
	ri := r + 1
	for ri < len(v)-1 && v[ri+1] <= v[ri] {
		ri++
	}
	base := v[li]
	if v[ri] > base {
		base = v[ri]
	}
	return li, ri, top - base
}

func trapzArea(times, values []float64, l, r int) float64 {
	a := 0.0
	for k := l; k < r; k++ {
		a += 0.5 * (values[k] + values[k+1]) * (times[k+1] - times[k])
	}
	return a
}

// tieTol is the relative tolerance used to recognise equal-prominence
// candidates that differ only by floating-point round-off.
const tieTol = 1e-6

func approxEqual(a, b float64) bool {
	scale := 1.0
	if m := absFloat(a); m > scale {
		scale = m
	}
	return absFloat(a-b) <= tieTol*scale
}

func annotateTies(peaks []domain.Peak) {
	for i := range peaks {
		if peaks[i].Rejected {
			continue
		}
		rank := 1
		for j := range peaks {
			if i == j || peaks[j].Rejected {
				continue
			}
			if approxEqual(peaks[j].Prominence, peaks[i].Prominence) && peaks[j].ApexIdx < peaks[i].ApexIdx {
				rank++
			}
		}
		if hasEqualPeer(peaks, i) {
			peaks[i].TiedRank = rank
		}
	}
}

func hasEqualPeer(peaks []domain.Peak, i int) bool {
	for j := range peaks {
		if j != i && !peaks[j].Rejected && approxEqual(peaks[j].Prominence, peaks[i].Prominence) {
			return true
		}
	}
	return false
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
