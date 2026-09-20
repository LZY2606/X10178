package store

import (
	"database/sql"
	"encoding/json"

	"peakvalley/internal/model"
)

// InsertMapVersion stores a committed mapping and its segment lock flags.
func (s *Store) InsertMapVersion(tx *sql.Tx, m *model.MapVersion) error {
	_, err := tx.Exec(`INSERT INTO map_vers(id,alignment_id,run_id,ver,created_at,frozen,plan_rev,segments_json,anchors_hash)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		m.ID, m.AlignmentID, m.RunID, m.Ver, m.CreatedAt, boolI(m.Frozen), m.PlanRev,
		string(mustJSON(m.Segments)), m.AnchorsHash)
	if err != nil {
		return err
	}
	for i, seg := range m.Segments {
		key := seg.AnchorL + "->" + seg.AnchorR
		if _, err := tx.Exec(`INSERT INTO map_locks(alignment_id,run_id,ver,seg_idx,key) VALUES(?,?,?,?,?)`,
			m.AlignmentID, m.RunID, m.Ver, i, key); err != nil {
			return err
		}
	}
	return nil
}

// SetMapFrozen marks a mapping version frozen (immutable thereafter).
func (s *Store) SetMapFrozen(tx *sql.Tx, alignmentID, runID string, ver int, frozen bool) error {
	_, err := tx.Exec(`UPDATE map_vers SET frozen=? WHERE alignment_id=? AND run_id=? AND ver=?`,
		boolI(frozen), alignmentID, runID, ver)
	return err
}

// LatestMapVersion returns the highest committed mapping for a run.
func (s *Store) LatestMapVersion(alignmentID, runID string) (*model.MapVersion, error) {
	row := s.db.QueryRow(`SELECT id,run_id,ver,created_at,frozen,plan_rev,segments_json,anchors_hash
		FROM map_vers WHERE alignment_id=? AND run_id=? ORDER BY ver DESC LIMIT 1`, alignmentID, runID)
	return scanMapVer(row, alignmentID)
}

// GetMapVersion fetches a specific mapping version.
func (s *Store) GetMapVersion(alignmentID, runID string, ver int) (*model.MapVersion, error) {
	row := s.db.QueryRow(`SELECT id,run_id,ver,created_at,frozen,plan_rev,segments_json,anchors_hash
		FROM map_vers WHERE alignment_id=? AND run_id=? AND ver=?`, alignmentID, runID, ver)
	m, err := scanMapVer(row, alignmentID)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return m, err
}

func scanMapVer(row *sql.Row, alignmentID string) (*model.MapVersion, error) {
	m := &model.MapVersion{AlignmentID: alignmentID}
	var created, segs string
	var frozen int
	if err := row.Scan(&m.ID, &m.RunID, &m.Ver, &created, &frozen, &m.PlanRev, &segs, &m.AnchorsHash); err != nil {
		return nil, err
	}
	m.CreatedAt = parseTime(created)
	m.Frozen = frozen != 0
	if err := json.Unmarshal([]byte(segs), &m.Segments); err != nil {
		return nil, err
	}
	return m, nil
}

// ListMapVersions returns all mapping versions for a run (history/compare).
func (s *Store) ListMapVersions(alignmentID, runID string) ([]*model.MapVersion, error) {
	rows, err := s.db.Query(`SELECT id,run_id,ver,created_at,frozen,plan_rev,segments_json,anchors_hash
		FROM map_vers WHERE alignment_id=? AND run_id=? ORDER BY ver`, alignmentID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.MapVersion
	for rows.Next() {
		m := &model.MapVersion{AlignmentID: alignmentID}
		var created, segs string
		var frozen int
		if err := rows.Scan(&m.ID, &m.RunID, &m.Ver, &created, &frozen, &m.PlanRev, &segs, &m.AnchorsHash); err != nil {
			return nil, err
		}
		m.CreatedAt = parseTime(created)
		m.Frozen = frozen != 0
		if err := json.Unmarshal([]byte(segs), &m.Segments); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// LockedSegmentKeys returns locked piece keys of a mapping version.
func (s *Store) LockedSegmentKeys(alignmentID, runID string, ver int) (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT key FROM map_locks WHERE alignment_id=? AND run_id=? AND ver=?`, alignmentID, runID, ver)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

// InsertAligned stores a derived aligned series.
func (s *Store) InsertAligned(tx *sql.Tx, a *model.Aligned) error {
	_, err := tx.Exec(`INSERT INTO aligneds(id,map_ver_id,run_id,ver,created_at,u_blob,signal_blob,n_points,source_digest,map_anchors_hash)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.MapVerID, a.RunID, a.Ver, a.CreatedAt, a.UBlob, a.SignalBlob, a.NPoints, a.SourceDigest, a.MapAnchorsHash)
	return err
}

// GetAligned fetches a derived series by its mapping version id.
func (s *Store) GetAligned(mapVerID, runID string) (*model.Aligned, error) {
	row := s.db.QueryRow(`SELECT id,map_ver_id,run_id,ver,created_at,u_blob,signal_blob,n_points,source_digest,map_anchors_hash
		FROM aligneds WHERE map_ver_id=? AND run_id=?`, mapVerID, runID)
	a := &model.Aligned{}
	var created string
	if err := row.Scan(&a.ID, &a.MapVerID, &a.RunID, &a.Ver, &created, &a.UBlob, &a.SignalBlob, &a.NPoints, &a.SourceDigest, &a.MapAnchorsHash); err != nil {
		return nil, err
	}
	a.CreatedAt = parseTime(created)
	return a, nil
}
