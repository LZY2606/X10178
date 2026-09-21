// Package domain holds the persisted value objects of the peak-valley compass.
package domain

// Run is one chromatographic acquisition. Times and values are the raw samples;
// every derived curve must remain traceable back to these sample indices.
type Run struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Sample     string            `json:"sample"`
	Instrument string            `json:"instrument"`
	Batch      string            `json:"batch"`
	Times      []float64         `json:"times"`
	Values     []float64         `json:"values"`
	Meta       map[string]string `json:"meta,omitempty"`
	CreatedAt  string            `json:"createdAt"`
}

// DetParams records the exact detection configuration so that re-detection is
// reproducible and candidate peaks carry their producing parameter version.
type DetParams struct {
	NoiseFloor     float64 `json:"noiseFloor"`
	Prominence     float64 `json:"prominence"`
	MinDistanceIdx int     `json:"minDistanceIdx"`
	LevelTolerance float64 `json:"levelTolerance"`
}

// DefaultParams returns the parameters used by the deterministic fixtures.
func DefaultParams() DetParams {
	return DetParams{
		NoiseFloor:     1.0,
		Prominence:     0.8,
		MinDistanceIdx: 3,
		LevelTolerance: 1e-9,
	}
}

// PeakStatus enumerates the lifecycle of a peak inside a peak-set revision.
const (
	PeakActive  = "active"
	PeakIgnored = "ignored"
	PeakMerged  = "merged"
	PeakSplit   = "split"
)

// Peak is a candidate (or manually edited) peak. Identity is a stable string id,
// never an array index; LeftIdx/RightIdx are sample indices into the owning run.
type Peak struct {
	ID         string   `json:"id"`
	ApexIdx    int      `json:"apexIdx"`
	LeftIdx    int      `json:"leftIdx"`
	RightIdx   int      `json:"rightIdx"`
	ApexTime   float64  `json:"apexTime"`
	Height     float64  `json:"height"`
	Area       float64  `json:"area"`
	Prominence float64  `json:"prominence"`
	Plateau    bool     `json:"plateau"`
	PlateauEnd int      `json:"plateauEnd,omitempty"`
	TiedRank   int      `json:"tiedRank,omitempty"`
	Status     string   `json:"status"`
	Rejected   bool     `json:"rejected,omitempty"`
	RejectWhy  string   `json:"rejectWhy,omitempty"`
	ParentID   string   `json:"parentId,omitempty"`
	Children   []string `json:"children,omitempty"`
	MergedInto string   `json:"mergedInto,omitempty"`
	Source     string   `json:"source"` // "detect" | "merge" | "split"
	Note       string   `json:"note,omitempty"`
}

// Detection is an immutable record of one detector execution on the raw curve.
type Detection struct {
	ID        string    `json:"id"`
	RunID     string    `json:"runId"`
	Params    DetParams `json:"params"`
	Peaks     []Peak    `json:"peaks"`
	CreatedAt string    `json:"createdAt"`
}

// PeakSet is a versioned, undoable working set of peaks for one run.
type PeakSet struct {
	ID          string `json:"id"`
	RunID       string `json:"runId"`
	Version     int    `json:"version"`
	ParentSetID string `json:"parentSetId,omitempty"`
	BasedOnDet  string `json:"basedOnDet,omitempty"`
	Peaks       []Peak `json:"peaks"`
	Frozen      bool   `json:"frozen"`
	Note        string `json:"note,omitempty"`
	CreatedAt   string `json:"createdAt"`
}

// AnchorKind distinguishes researcher anchors from system suggestions.
const (
	AnchorManual = "manual"
	AnchorAuto   = "auto"
)

// Anchor maps reference time X to this run's time Y.
type Anchor struct {
	ID     string  `json:"id"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Kind   string  `json:"kind"`
	PeakID string  `json:"peakId,omitempty"`
	Locked bool    `json:"locked,omitempty"`
	Note   string  `json:"note,omitempty"`
}

// RejectedAnchor explains why an anchor could not participate in the mapping.
type RejectedAnchor struct {
	AnchorID string  `json:"anchorId"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Reason   string  `json:"reason"` // "cross" | "vertical" | "duplicate"
	With     string  `json:"with,omitempty"`
}

// SegmentLock pins a reference-time interval to pointwise-identical behaviour.
type SegmentLock struct {
	ID       string  `json:"id"`
	XStart   float64 `json:"xStart"`
	XEnd     float64 `json:"xEnd"`
	Revision int     `json:"revision"`
	Note     string  `json:"note,omitempty"`
}

// Mapping is a versioned monotone time mapping from reference to one target run.
type Mapping struct {
	ID        string           `json:"id"`
	RefRunID  string           `json:"refRunId"`
	RunID     string           `json:"runId"`
	Version   int              `json:"version"`
	Anchors   []Anchor         `json:"anchors"`
	Accepted  []Anchor         `json:"accepted"`
	Rejected  []RejectedAnchor `json:"rejected"`
	Locks     []SegmentLock    `json:"locks"`
	Valid     bool             `json:"valid"`
	Frozen    bool             `json:"frozen"`
	CreatedAt string           `json:"createdAt"`
}

// LineagePoint is one aligned sample with full provenance.
type LineagePoint struct {
	RefTime   float64 `json:"refTime"`
	Aligned   float64 `json:"aligned"`
	Value     float64 `json:"value"`
	SrcIdx    int     `json:"srcIdx"`
	SrcTime   float64 `json:"srcTime"`
	Interp    bool    `json:"interp"`
	MappingID string  `json:"mappingId"`
	MapVer    int     `json:"mapVer"`
}

// ConsensusPeak is one family peak in the batch consensus.
type ConsensusPeak struct {
	ID         string        `json:"id"`
	RefTime    float64       `json:"refTime"`
	Area       float64       `json:"area"`
	Height     float64       `json:"height"`
	Supporting []PeakSupport `json:"supporting"`
	Missing    []string      `json:"missing"`
	Note       string        `json:"note,omitempty"`
}

// PeakSupport links a consensus peak to a concrete run peak.
type PeakSupport struct {
	RunID    string  `json:"runId"`
	PeakID   string  `json:"peakId"`
	RefTime  float64 `json:"refTime"`
	RawArea  float64 `json:"rawArea"`
	NormArea float64 `json:"normArea"`
}

// Consensus is a versioned batch-level agreement built from frozen mappings.
type Consensus struct {
	ID        string          `json:"id"`
	Batch     string          `json:"batch"`
	Version   int             `json:"version"`
	RefRunID  string          `json:"refRunId"`
	RunIDs    []string        `json:"runIds"`
	Peaks     []ConsensusPeak `json:"peaks"`
	NormRule  string          `json:"normRule"` // fixed inside the version
	Frozen    bool            `json:"frozen"`
	Note      string          `json:"note,omitempty"`
	CreatedAt string          `json:"createdAt"`
}
