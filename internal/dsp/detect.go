package dsp

import (
	"math"
	"sort"

	"peakvalley/internal/model"
)

// Detect finds candidate peaks. Identity (Peak.ID) is assigned here and must
// survive reordering; Idx is only the position inside this detection.
func Detect(idGen IDGen, detID string, times, signal []float64, p model.DetParams) []*model.Peak {
	raw := signal
	s := smooth(signal, p.SmoothWindow)
	n := len(times)
	type rapex struct {
		i, j    int // apex position(s) on the raw series
		si      int // apex index on the smoothed series (prominence anchor)
		plateau bool
	}
	var apexes []rapex
	// Candidate positions come from the smoothed series so noise and smoothing
	// shifts do not zero the prominence; raw geometry is recovered afterwards.
	for si := 1; si < n-1; si++ {
		if !(s[si] > s[si-1] && s[si] >= s[si+1]) {
			continue
		}
		// Recover the raw apex nearest to the smoothed apex.
		i := si
		if raw[si-1] > raw[i] {
			i = si - 1
		}
		if si+1 < n && raw[si+1] > raw[i] {
			i = si + 1
		}
		j := i
		for j+1 < n && raw[j+1] == raw[i] {
			j++
		}
		apexes = append(apexes, rapex{i: i, j: j, si: si, plateau: j > i})
	}

	var peaks []*model.Peak
	for _, a := range apexes {
		apexI := a.i
		at := "index"
		platFrom, platTo := -1, -1
		if a.plateau {
			platFrom, platTo = a.i, a.j
			at = "plateau"
			switch p.PlateauMode {
			case "mid":
				apexI = (a.i + a.j) / 2
				at = "mean"
			default: // "first"
				apexI = a.i
			}
		}
		prom, leftMinI, rightMinI := prominence(s, a.si)
		if prom < p.MinProminence {
			continue
		}
		leftI, rightI := halfBounds(s, a.si, prom, leftMinI, rightMinI)
		if rightI-leftI < p.MinWidthSamples {
			continue
		}
		height := raw[apexI]
		peaks = append(peaks, &model.Peak{
			ID:          idGen.New(),
			DetectionID: detID,
			Idx:         len(peaks),
			ApexI:       apexI,
			Height:      height,
			Area:        trapz(times, signal, leftI, rightI),
			Prominence:  prom,
			WidthI:      rightI - leftI,
			Shape: model.Shape{
				ApexAt: at, ApexT: times[apexI],
				Plateau: a.plateau, PlatFromI: platFrom, PlatToI: platTo,
				LeftI: leftI, RightI: rightI,
				LeftT: times[leftI], RightT: times[rightI],
			},
		})
	}

	// Tie marker: candidates whose prominence agrees within a small tolerance
	// are indistinguishable by rank; sorting must not invent an ordering.
	for i, a := range peaks {
		for j, b := range peaks {
			if i != j && math.Abs(a.Prominence-b.Prominence) <= 1e-6*math.Max(1, math.Abs(a.Prominence)) {
				a.Shape.Tied = true
				break
			}
		}
	}
	sort.SliceStable(peaks, func(a, b int) bool { return peaks[a].ApexI < peaks[b].ApexI })
	for i, pk := range peaks {
		pk.Idx = i
	}
	return peaks
}

func smooth(x []float64, w int) []float64 {
	if w < 3 {
		return x
	}
	if w%2 == 0 {
		w++
	}
	out := make([]float64, len(x))
	half := w / 2
	for i := range x {
		lo, hi := i-half, i+half
		if lo < 0 {
			lo = 0
		}
		if hi > len(x)-1 {
			hi = len(x) - 1
		}
		sum := 0.0
		for k := lo; k <= hi; k++ {
			sum += x[k]
		}
		out[i] = sum / float64(hi-lo+1)
	}
	return out
}

// prominence returns the topographic prominence: on each side the col is the
// lowest valley before reaching a signal as high as the apex (or the border).
func prominence(s []float64, apex int) (prom float64, leftMinI, rightMinI int) {
	h := s[apex]
	col := func(dir int) (float64, int) {
		low := h
		lowI := apex
		for i := apex + dir; i >= 0 && i < len(s); i += dir {
			if s[i] < low {
				low = s[i]
				lowI = i
			}
			if s[i] >= h {
				return low, lowI
			}
		}
		return low, lowI
	}
	lc, lmi := col(-1)
	rc, rmi := col(1)
	return h - math.Max(lc, rc), lmi, rmi
}

func halfBounds(s []float64, apex int, prom float64, lMin, rMin int) (int, int) {
	level := s[apex] - prom/2
	left := apex
	for i := apex - 1; i >= lMin; i-- {
		if s[i] <= level {
			break
		}
		left = i
	}
	right := apex
	for i := apex + 1; i <= rMin; i++ {
		if s[i] <= level {
			break
		}
		right = i
	}
	return left, right
}

func trapz(times, signal []float64, lo, hi int) float64 {
	a := 0.0
	for i := lo; i < hi; i++ {
		a += (times[i+1] - times[i]) * (signal[i] + signal[i+1]) / 2
	}
	return a
}
