package warp

import "peakcompass/internal/domain"

// MatchPeaks performs order-preserving one-to-one matching between reference
// peaks and run peaks by apex time. Either side may leave peaks unmatched
// (anchors can appear in only some runs); the assignment maximises the number
// of matches and, among maximal matchings, minimises total absolute apex-time
// difference via dynamic programming. A missing early peak therefore cannot
// cause a greedy mis-pairing later in the sequence.
//
// It returns matched pairs as (reference index, run index) into the inputs.
func MatchPeaks(refPeaks, runPeaks []domain.Peak, maxDrift float64) [][2]int {
	n, m := len(refPeaks), len(runPeaks)
	// matchBonus dominates any accumulated drift penalty, guaranteeing the
	// primary objective is the number of pairs.
	const matchBonus = 1000.0
	dp := make([][]float64, n+1)
	op := make([][]byte, n+1)
	for i := range dp {
		dp[i] = make([]float64, m+1)
		op[i] = make([]byte, m+1)
	}
	for i := 1; i <= n; i++ {
		op[i][0] = 'r'
	}
	for j := 1; j <= m; j++ {
		op[0][j] = 'u'
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			// Skip reference peak or skip run peak both score 0 increment.
			best := dp[i-1][j]
			bop := byte('r')
			if v := dp[i][j-1]; v > best {
				best, bop = v, 'u'
			}
			d := absFloat(runPeaks[j-1].ApexTime - refPeaks[i-1].ApexTime)
			if d <= maxDrift {
				if v := dp[i-1][j-1] + matchBonus - d; v > best {
					best, bop = v, 'm'
				}
			}
			dp[i][j], op[i][j] = best, bop
		}
	}
	var pairs [][2]int
	i, j := n, m
	for i > 0 && j > 0 {
		switch op[i][j] {
		case 'm':
			pairs = append(pairs, [2]int{i - 1, j - 1})
			i--
			j--
		case 'r':
			i--
		default: // 'u'
			j--
		}
	}
	for a, b := 0, len(pairs)-1; a < b; a, b = a+1, b-1 {
		pairs[a], pairs[b] = pairs[b], pairs[a]
	}
	return pairs
}
