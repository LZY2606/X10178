package dsp

// Linspace builds n evenly spaced samples from a to b (n>=2).
func Linspace(a, b float64, n int) []float64 {
	out := make([]float64, n)
	step := (b - a) / float64(n-1)
	for i := range out {
		out[i] = a + step*float64(i)
	}
	return out
}
