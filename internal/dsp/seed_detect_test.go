package dsp

import (
	"fmt"
	"math"
	"testing"

	"peakvalley/internal/model"
)

type ctr struct{ n int }

func (c *ctr) New() string { c.n++; return fmt.Sprintf("p%d", c.n) }

func TestDebugC(t *testing.T) {
	n := 480
	xs := make([]float64, n)
	x := 0.0
	for i := 0; i < n; i++ {
		xs[i] = x
		jit := 1.0 + 0.25*math.Sin(float64(i)*0.37)
		x += 0.05 * jit
	}
	sig := make([]float64, n)
	gauss := func(x, c, w, h float64) float64 { d := (x - c) / w; return h * math.Exp(-d*d) }
	for i, xx := range xs {
		m := 1.03*xx + 0.10
		y := 0.02 + gauss(m, 3, .18, 1) + gauss(m, 14, .2, .8) + gauss(m, 20, .24, .8)
		y += plateauPeakDbg(m, 7.9, 8.1, .22, .9)
		sig[i] = y
	}
	pks := Detect(&ctr{}, "d", xs, sig, model.DetParams{SmoothWindow: 5, MinProminence: 0.15, MinWidthSamples: 3, PlateauMode: "mid"})
	for _, p := range pks {
		fmt.Printf("peak t=%.3f h=%.3f prom=%.3f width=%d plateau=%v at=%s tied=%v\n",
			p.Shape.ApexT, p.Height, p.Prominence, p.WidthI, p.Shape.Plateau, p.Shape.ApexAt, p.Shape.Tied)
	}
	fmt.Println("count:", len(pks))
}

func plateauPeakDbg(x, a, b, w, h float64) float64 {
	switch {
	case x < a:
		d := (x - a) / w
		return h * math.Exp(-d*d)
	case x > b:
		d := (x - b) / w
		return h * math.Exp(-d*d)
	default:
		return h
	}
}
