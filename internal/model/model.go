// Package model contains the domain types of the peak-valley compass.
package model

import "time"

// Meta holds run-level sample and instrument metadata.
type Meta struct {
	SampleID    string `json:"sample_id"`
	SampleName  string `json:"sample_name"`
	InstrID     string `json:"instr_id"`
	InstrModel  string `json:"instr_model"`
	Column      string `json:"column"`
	Operator    string `json:"operator"`
	Description string `json:"description"`
}

// Run is one chromatographic acquisition.
type Run struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"created_at"`
	TimesBlob  string    `json:"times_blob"`
	SignalBlob string    `json:"signal_blob"`
	NPoints    int       `json:"n_points"`
	T0         float64   `json:"t0"`
	T1         float64   `json:"t1"`
	DT         float64   `json:"dt"`
	Meta       Meta      `json:"meta"`
	// SHA-256 of canonical "times|signal" content; roots the provenance chain.
	RawDigest string `json:"raw_digest"`
}

// DetParams are the exact parameters used to produce a detection.
type DetParams struct {
	SmoothWindow    int     `json:"smooth_window"`
	MinProminence   float64 `json:"min_prominence"`
	MinWidthSamples int     `json:"min_width_samples"`
	PlateauMode     string  `json:"plateau_mode"` // "first" | "mid"
}

// Detection is an immutable candidate-peak detection on one run.
type Detection struct {
	ID        string    `json:"id"`
	RunID     string    `json:"run_id"`
	Ver       int       `json:"ver"`
	CreatedAt time.Time `json:"created_at"`
	Params    DetParams `json:"params"`
	Peaks     []*Peak   `json:"peaks"`
}

// Shape is the geometry description of a candidate peak.
type Shape struct {
	ApexAt    string  `json:"apex_at"` // index | mean | plateau
	ApexT     float64 `json:"apex_t"`
	Plateau   bool    `json:"plateau"`
	PlatFromI int     `json:"plat_from_i"`
	PlatToI   int     `json:"plat_to_i"`
	LeftI     int     `json:"left_i"`
	RightI    int     `json:"right_i"`
	LeftT     float64 `json:"left_t"`
	RightT    float64 `json:"right_t"`
	Tied      bool    `json:"tied"` // candidate tied with another during ranking
}

// Peak is a detected candidate with identity independent of array position.
type Peak struct {
	ID          string  `json:"id"`
	DetectionID string  `json:"-"`
	Idx         int     `json:"-"` // index inside the detection that created it
	ApexI       int     `json:"apex_i"`
	Height      float64 `json:"height"`
	Area        float64 `json:"area"`
	Prominence  float64 `json:"prominence"`
	WidthI      int     `json:"width_i"`
	Shape       Shape   `json:"shape"`
}

// ViewPeak is one row of the materialised working list of a run.
type ViewPeak struct {
	PeakID      string  `json:"peak_id"`
	DetectionID string  `json:"-"`
	Ver         int     `json:"ver"`
	DetVer      int     `json:"det_ver"`
	Status      string  `json:"status"` // active | ignored | merged | split
	ApexI       int     `json:"apex_i"`
	ApexT       float64 `json:"apex_t"`
	Height      float64 `json:"height"`
	Area        float64 `json:"area"`
	Prom        float64 `json:"prom"`
	WidthI      int     `json:"width_i"`
	Shape       Shape   `json:"shape"`
	GroupID     string  `json:"group_id,omitempty"` // merge/split family
	Note        string  `json:"note,omitempty"`
}

const (
	OpMerge   = "merge"
	OpSplit   = "split"
	OpIgnore  = "ignore"
	OpRestore = "restore"
	OpUndo    = "undo"
)

// EditEvent records one manual operation against the working list.
type EditEvent struct {
	Op      string   `json:"op"`
	PeakIDs []string `json:"peak_ids"`
	AtI     int      `json:"at_i,omitempty"` // split location
	NewIDs  []string `json:"new_ids,omitempty"`
	GroupID string   `json:"group_id,omitempty"`
	Note    string   `json:"note,omitempty"`
}

// EditVer is an append-only, undoable version of the working list.
type EditVer struct {
	ID          string      `json:"id"`
	RunID       string      `json:"run_id"`
	Ver         int         `json:"ver"`
	BaseDetID   string      `json:"base_det_id"`
	BaseDetVer  int         `json:"base_det_ver"`
	CreatedAt   time.Time   `json:"created_at"`
	Event       EditEvent   `json:"event"`
	UndoneOfVer int         `json:"undone_of_ver,omitempty"`
	Items       []*ViewPeak `json:"items"`
}

// Mig maps an old stable peak id to its identity fate after redetection.
type Mig struct {
	OldID string  `json:"old_id"`
	NewID string  `json:"new_id"` // "" when deleted
	Rel   string  `json:"rel"`    // same | moved | split | merged | inserted(deleted?)
	Score float64 `json:"score"`
	Note  string  `json:"note,omitempty"`
}

// Alignment is the project-level container for anchors and a mapping version.
type Alignment struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	RefRunID     string    `json:"ref_run_id"`
	CreatedAt    time.Time `json:"created_at"`
	MapVer       int       `json:"map_ver"` // committed map versions (0 if none)
	FrozenMapVer int       `json:"frozen_map_ver,omitempty"`
	Locked       bool      `json:"locked"` // true once a map is frozen
	PlanRev      int       `json:"plan_rev"`
	DraftRev     int       `json:"draft_rev"` // optimistic-concurrency token
}

// Anchor is a cross-run correspondence of peaks.
type Anchor struct {
	ID          string    `json:"id"`
	AlignmentID string    `json:"-"`
	Label       string    `json:"label"`
	RefPeakID   string    `json:"ref_peak_id"`
	RefT        float64   `json:"ref_t"`
	Auto        bool      `json:"auto"`
	CreatedAt   time.Time `json:"created_at"`
}

// AnchorPoint is one run's participation in an anchor.
type AnchorPoint struct {
	AnchorID string  `json:"anchor_id"`
	RunID    string  `json:"run_id"`
	PeakID   string  `json:"peak_id"`
	T        float64 `json:"t"`
	Manual   bool    `json:"manual"`
}

// Conflict is an incompatible anchor pair for a run.
type Conflict struct {
	RunID       string  `json:"run_id"`
	A           string  `json:"a"`
	B           string  `json:"b"`
	T_A         float64 `json:"t_a"`
	T_B         float64 `json:"t_b"`
	RefA        float64 `json:"ref_a"`
	RefB        float64 `json:"ref_b"`
	Description string  `json:"description"`
}

// BuildPlan is the monotonicity-checked candidate plan for a map.
type BuildPlan struct {
	Rev        int         `json:"rev"`
	ParamsHash string      `json:"params_hash"`
	AnchorIDs  []string    `json:"anchor_ids"`
	Conflicts  []Conflict  `json:"conflicts"`
	RejectSet  []RejectSet `json:"reject_set"`
}

// RejectSet is a minimum set of anchors whose removal removes all conflicts.
type RejectSet struct {
	RunID       string   `json:"run_id"`
	AnchorIDs   []string `json:"anchor_ids"`
	Size        int      `json:"size"`
	Description string   `json:"description"`
}

// Segment is one piecewise-linear piece of a mapping.
type Segment struct {
	Idx     int     `json:"idx"`
	X0      float64 `json:"x0"`
	X1      float64 `json:"x1"`
	U0      float64 `json:"u0"`
	U1      float64 `json:"u1"`
	Slope   float64 `json:"slope"`
	AnchorL string  `json:"anchor_l"`
	AnchorR string  `json:"anchor_r"`
	Locked  bool    `json:"locked"`
}

// MapVersion is a committed monotonic time mapping for one run.
type MapVersion struct {
	ID          string    `json:"id"`
	AlignmentID string    `json:"-"`
	RunID       string    `json:"run_id"`
	Ver         int       `json:"ver"`
	CreatedAt   time.Time `json:"created_at"`
	Frozen      bool      `json:"frozen"`
	PlanRev     int       `json:"plan_rev"`
	Segments    []Segment `json:"segments"`
	AnchorsHash string    `json:"anchors_hash"`
}

// Aligned stores the derived series produced by applying a map.
type Aligned struct {
	ID             string    `json:"id"`
	MapVerID       string    `json:"map_ver_id"`
	RunID          string    `json:"run_id"`
	Ver            int       `json:"ver"`
	CreatedAt      time.Time `json:"created_at"`
	UBlob          string    `json:"u_blob"`
	SignalBlob     string    `json:"signal_blob"`
	NPoints        int       `json:"n_points"`
	SourceDigest   string    `json:"source_digest"` // must match run raw digest
	MapAnchorsHash string    `json:"map_anchors_hash"`
}

// Suggestion is one auto-proposed correspondence, with rationale.
type Suggestion struct {
	RefPeakID    string  `json:"ref_peak_id"`
	RefT         float64 `json:"ref_t"`
	RunPeakID    string  `json:"run_peak_id"`
	RunT         float64 `json:"run_t"`
	Score        float64 `json:"score"`
	ShapeNote    string  `json:"shape_note"`
	AreaNote     string  `json:"area_note"`
	NeighborNote string  `json:"neighbor_note"`
	Selected     bool    `json:"selected"` // greedy monotonic pick
}

// FamMember is one run's peak inside a family.
type FamMember struct {
	RunID   string  `json:"run_id"`
	PeakID  string  `json:"peak_id"`
	DetVer  int     `json:"det_ver"`
	EditVer int     `json:"edit_ver"`
	ApexU   float64 `json:"apex_u"`
	Area    float64 `json:"area"`
	Height  float64 `json:"height"`
	Status  string  `json:"status"`
}

// Family is a cross-run peak family.
type Family struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	CenterU float64     `json:"center_u"`
	Members []FamMember `json:"members"`
}

// FamilySet is a versioned grouping aligned onto reference time.
type FamilySet struct {
	ID          string    `json:"id"`
	AlignmentID string    `json:"-"`
	Ver         int       `json:"ver"`
	Draft       bool      `json:"draft"`
	Frozen      bool      `json:"frozen"`
	CreatedAt   time.Time `json:"created_at"`
	MapVer      int       `json:"map_ver"`
	TolU        float64   `json:"tol_u"`
	Families    []*Family `json:"families"`
	Rev         int       `json:"rev"`
}

// NormRule pins the area normalization rule to a version.
type NormRule struct {
	Method string  `json:"method"` // "tic" | "median_peak" | "none"
	Scale  float64 `json:"scale"`
	Note   string  `json:"note"`
}

// ConsensusPeak is one peak of the batch consensus.
type ConsensusPeak struct {
	FamilyID       string             `json:"family_id"`
	Name           string             `json:"name"`
	CenterU        float64            `json:"center_u"`
	AreaMean       float64            `json:"area_mean"`
	AreaCV         float64            `json:"area_cv"`
	Supporting     []string           `json:"supporting"`
	Missing        []string           `json:"missing"`
	MemberNormArea map[string]float64 `json:"member_norm_area"`
}

// Consensus is a versioned, freeze-able batch consensus.
type Consensus struct {
	ID          string          `json:"id"`
	FamilySetID string          `json:"family_set_id"`
	Ver         int             `json:"ver"`
	Draft       bool            `json:"draft"`
	Frozen      bool            `json:"frozen"`
	CreatedAt   time.Time       `json:"created_at"`
	Rule        NormRule        `json:"rule"`
	Peaks       []ConsensusPeak `json:"peaks"`
	RunIDs      []string        `json:"run_ids"`
	Rev         int             `json:"rev"`
}
