package store

import (
	"database/sql"
	"encoding/json"

	"peakvalley/internal/model"
)

// InsertFamilySet stores a family-set version.
func (s *Store) InsertFamilySet(tx *sql.Tx, f *model.FamilySet) error {
	_, err := tx.Exec(`INSERT INTO family_sets(id,alignment_id,ver,draft,frozen,created_at,map_ver,tol_u,families_json,rev)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		f.ID, f.AlignmentID, f.Ver, boolI(f.Draft), boolI(f.Frozen), f.CreatedAt, f.MapVer, f.TolU,
		string(mustJSON(f.Families)), f.Rev)
	return err
}

// UpdateFamilySetRev replaces mutable draft state under optimistic control.
func (s *Store) UpdateFamilySetRev(tx *sql.Tx, f *model.FamilySet, expectRev int) error {
	res, err := tx.Exec(`UPDATE family_sets SET draft=?,frozen=?,families_json=?,rev=? WHERE id=? AND rev=?`,
		boolI(f.Draft), boolI(f.Frozen), string(mustJSON(f.Families)), f.Rev, f.ID, expectRev)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrConcurrent
	}
	return nil
}

// GetFamilySet fetches a family set version.
func (s *Store) GetFamilySet(alignmentID string, ver int) (*model.FamilySet, error) {
	row := s.db.QueryRow(`SELECT id,ver,draft,frozen,created_at,map_ver,tol_u,families_json,rev
		FROM family_sets WHERE alignment_id=? AND ver=?`, alignmentID, ver)
	return scanFamilySet(row, alignmentID)
}

// LatestFamilySet returns the highest family-set version.
func (s *Store) LatestFamilySet(alignmentID string) (*model.FamilySet, error) {
	row := s.db.QueryRow(`SELECT id,ver,draft,frozen,created_at,map_ver,tol_u,families_json,rev
		FROM family_sets WHERE alignment_id=? ORDER BY ver DESC LIMIT 1`, alignmentID)
	f, err := scanFamilySet(row, alignmentID)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return f, err
}

func scanFamilySet(row *sql.Row, alignmentID string) (*model.FamilySet, error) {
	f := &model.FamilySet{AlignmentID: alignmentID}
	var created, fams string
	var draft, frozen int
	if err := row.Scan(&f.ID, &f.Ver, &draft, &frozen, &created, &f.MapVer, &f.TolU, &fams, &f.Rev); err != nil {
		return nil, err
	}
	f.Draft = draft != 0
	f.Frozen = frozen != 0
	f.CreatedAt = parseTime(created)
	return f, json.Unmarshal([]byte(fams), &f.Families)
}

// InsertConsensus stores a consensus version.
func (s *Store) InsertConsensus(tx *sql.Tx, c *model.Consensus) error {
	_, err := tx.Exec(`INSERT INTO consensuses(id,family_set_id,ver,draft,frozen,created_at,rule_json,peaks_json,run_ids_json,rev)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.FamilySetID, c.Ver, boolI(c.Draft), boolI(c.Frozen), c.CreatedAt,
		string(mustJSON(c.Rule)), string(mustJSON(c.Peaks)), string(mustJSON(c.RunIDs)), c.Rev)
	return err
}

// UpdateConsensusRev replaces mutable consensus state under optimistic control.
func (s *Store) UpdateConsensusRev(tx *sql.Tx, c *model.Consensus, expectRev int) error {
	res, err := tx.Exec(`UPDATE consensuses SET draft=?,frozen=?,rule_json=?,peaks_json=?,run_ids_json=?,rev=? WHERE id=? AND rev=?`,
		boolI(c.Draft), boolI(c.Frozen), string(mustJSON(c.Rule)), string(mustJSON(c.Peaks)),
		string(mustJSON(c.RunIDs)), c.Rev, c.ID, expectRev)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrConcurrent
	}
	return nil
}

// LatestConsensus returns the highest consensus version for a family set.
func (s *Store) LatestConsensus(familySetID string) (*model.Consensus, error) {
	row := s.db.QueryRow(`SELECT id,family_set_id,ver,draft,frozen,created_at,rule_json,peaks_json,run_ids_json,rev
		FROM consensuses WHERE family_set_id=? ORDER BY ver DESC LIMIT 1`, familySetID)
	c := &model.Consensus{}
	var created, rule, peaks, runs string
	var draft, frozen int
	if err := row.Scan(&c.ID, &c.FamilySetID, &c.Ver, &draft, &frozen, &created, &rule, &peaks, &runs, &c.Rev); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.Draft = draft != 0
	c.Frozen = frozen != 0
	c.CreatedAt = parseTime(created)
	if err := json.Unmarshal([]byte(rule), &c.Rule); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(peaks), &c.Peaks); err != nil {
		return nil, err
	}
	return c, json.Unmarshal([]byte(runs), &c.RunIDs)
}

// BeginTx starts a database transaction.
func (s *Store) BeginTx() (*sql.Tx, error) { return s.db.Begin() }
