package app

import (
	"peakvalley/internal/model"
)

// ViewPeakExport is the API view of a working-list peak.
type ViewPeakExport = model.ViewPeak

func exportItems(in []*model.ViewPeak) []*model.ViewPeak {
	out := make([]*model.ViewPeak, len(in))
	copy(out, in)
	return out
}
