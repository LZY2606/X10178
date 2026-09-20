package store

import (
	"database/sql"
	"encoding/json"
	"errors"

	"peakvalley/internal/model"
)

// ErrNotFound is returned for missing rows.
var ErrNotFound = errors.New("not found")

// InsertAlignment creates an alignment.
func (s *Store) InsertAlignment(tx *sql.Tx, a *model.Alignment) error {
	_, err := tx.Exec(`INSERT INTO alignments(id,name,ref_run_id,created_at,map_ver,frozen_map_ver,locked,plan_rev,draft_rev)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		a.ID, a.Name, a.RefRunID, a.CreatedAt, a.MapVer, a.FrozenMapVer, boolI(a.Locked), a.PlanRev, a.DraftRev)
	return err
}

// GetAlignment fetches the single alignment by id.
func (s *Store) GetAlignment(id string) (*model.Alignment, error) {
	row := s.db.QueryRow(`SELECT id,name,ref_run_id,created_at,map_ver,frozen_map_ver,locked,plan_rev,draft_rev FROM alignments WHERE id=?`, id)
	return scanAlignment(row)
}

// FirstAlignment returns any alignment (the project uses one by default).
func (s *Store) FirstAlignment() (*model.Alignment, error) {
	row := s.db.QueryRow(`SELECT id,name,ref_run_id,created_at,map_ver,frozen_map_ver,locked,plan_rev,draft_rev FROM alignments ORDER BY created_at LIMIT 1`)
	a, err := scanAlignment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

// UpdateAlignmentCounters writes mutable counters under optimistic control.
func (s *Store) UpdateAlignmentCounters(tx *sql.Tx, a *model.Alignment) error {
	res, err := tx.Exec(`UPDATE alignments SET map_ver=?,frozen_map_ver=?,locked=?,plan_rev=?,draft_rev=? WHERE id=?`,
		a.MapVer, a.FrozenMapVer, boolI(a.Locked), a.PlanRev, a.DraftRev, a.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// CASDraftRev increments draft_rev only when the expected revision matches.
func (s *Store) CASDraftRev(tx *sql.Tx, id string, expect int) error {
	res, err := tx.Exec(`UPDATE alignments SET draft_rev=draft_rev+1 WHERE id=? AND draft_rev=?`, id, expect)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrConcurrent
	}
	return nil
}

// ErrConcurrent is returned on stale optimistic-concurrency submissions.
var ErrConcurrent = errors.New("stale revision: concurrent commit")

func scanAlignment(row *sql.Row) (*model.Alignment, error) {
	a := &model.Alignment{}
	var created string
	var locked int
	if err := row.Scan(&a.ID, &a.Name, &a.RefRunID, &created, &a.MapVer, &a.FrozenMapVer, &locked, &a.PlanRev, &a.DraftRev); err != nil {
		return nil, err
	}
	a.CreatedAt = parseTime(created)
	a.Locked = locked != 0
	return a, nil
}

// InsertAnchor persists an anchor definition.
func (s *Store) InsertAnchor(tx *sql.Tx, a *model.Anchor) error {
	_, err := tx.Exec(`INSERT INTO anchors(id,alignment_id,label,ref_peak_id,ref_t,auto,created_at)
		VALUES(?,?,?,?,?,?,?)`, a.ID, a.AlignmentID, a.Label, a.RefPeakID, a.RefT, boolI(a.Auto), a.CreatedAt)
	return err
}

// DeleteAnchor removes an anchor and its points.
func (s *Store) DeleteAnchor(tx *sql.Tx, alignmentID, anchorID string) error {
	if _, err := tx.Exec(`DELETE FROM anchor_points WHERE anchor_id=?`, anchorID); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM anchors WHERE id=? AND alignment_id=?`, anchorID, alignmentID)
	return err
}

// Querier is satisfied by both *sql.DB and *sql.Tx.
type Querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// ListAnchors returns all anchors of an alignment ordered by reference time.
func (s *Store) ListAnchors(alignmentID string) ([]*model.Anchor, error) {
	return s.ListAnchorsQ(s.db, alignmentID)
}

// ListAnchorsQ lists anchors using an arbitrary querier (e.g. a transaction).
func (s *Store) ListAnchorsQ(q Querier, alignmentID string) ([]*model.Anchor, error) {
	rows, err := q.Query(`SELECT id,label,ref_peak_id,ref_t,auto,created_at FROM anchors WHERE alignment_id=? ORDER BY ref_t,id`, alignmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Anchor
	for rows.Next() {
		a := &model.Anchor{AlignmentID: alignmentID}
		var created string
		var auto int
		if err := rows.Scan(&a.ID, &a.Label, &a.RefPeakID, &a.RefT, &auto, &created); err != nil {
			return nil, err
		}
		a.Auto = auto != 0
		a.CreatedAt = parseTime(created)
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpsertAnchorPoint inserts or replaces a run participation.
func (s *Store) UpsertAnchorPoint(tx *sql.Tx, p model.AnchorPoint) error {
	_, err := tx.Exec(`INSERT INTO anchor_points(anchor_id,run_id,peak_id,t,manual) VALUES(?,?,?,?,?)
		ON CONFLICT(anchor_id,run_id) DO UPDATE SET peak_id=excluded.peak_id,t=excluded.t,manual=excluded.manual`,
		p.AnchorID, p.RunID, p.PeakID, p.T, boolI(p.Manual))
	return err
}

// DeleteAnchorPoint removes one run's participation.
func (s *Store) DeleteAnchorPoint(tx *sql.Tx, anchorID, runID string) error {
	_, err := tx.Exec(`DELETE FROM anchor_points WHERE anchor_id=? AND run_id=?`, anchorID, runID)
	return err
}

// ListAnchorPoints returns every participation for an alignment.
func (s *Store) ListAnchorPoints(alignmentID string) ([]model.AnchorPoint, error) {
	return s.ListAnchorPointsQ(s.db, alignmentID)
}

// ListAnchorPointsQ lists participation using an arbitrary querier.
func (s *Store) ListAnchorPointsQ(q Querier, alignmentID string) ([]model.AnchorPoint, error) {
	rows, err := q.Query(`SELECT ap.anchor_id,ap.run_id,ap.peak_id,ap.t,ap.manual FROM anchor_points ap
		JOIN anchors an ON an.id=ap.anchor_id WHERE an.alignment_id=? ORDER BY ap.anchor_id,ap.run_id`, alignmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AnchorPoint
	for rows.Next() {
		var p model.AnchorPoint
		var manual int
		if err := rows.Scan(&p.AnchorID, &p.RunID, &p.PeakID, &p.T, &manual); err != nil {
			return nil, err
		}
		p.Manual = manual != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

// InsertPlan stores a build plan snapshot.
func (s *Store) InsertPlan(tx *sql.Tx, alignmentID string, rev int, paramsHash string, anchorIDs []string, conflicts []model.Conflict, rejects []model.RejectSet) error {
	_, err := tx.Exec(`INSERT INTO plans(alignment_id,rev,params_hash,anchor_ids_json,conflicts_json,reject_json) VALUES(?,?,?,?,?,?)`,
		alignmentID, rev, paramsHash, string(mustJSON(anchorIDs)), string(mustJSON(conflicts)), string(mustJSON(rejects)))
	return err
}

// GetPlan returns the plan at a revision.
func (s *Store) GetPlan(alignmentID string, rev int) (*model.BuildPlan, error) {
	row := s.db.QueryRow(`SELECT rev,params_hash,anchor_ids_json,conflicts_json,reject_json FROM plans WHERE alignment_id=? AND rev=?`, alignmentID, rev)
	p := &model.BuildPlan{}
	var ids, conf, rej string
	if err := row.Scan(&p.Rev, &p.ParamsHash, &ids, &conf, &rej); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(ids), &p.AnchorIDs); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(conf), &p.Conflicts); err != nil {
		return nil, err
	}
	return p, json.Unmarshal([]byte(rej), &p.RejectSet)
}

func boolI(b bool) int {
	if b {
		return 1
	}
	return 0
}
