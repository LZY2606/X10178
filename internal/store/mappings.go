package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"peakcompass/internal/domain"
)

const mapCols = `id,ref_run_id,run_id,version,anchors,accepted,rejected,locks,valid,frozen,created_at`

func scanMapping(row rowScanner) (*domain.Mapping, error) {
	var m domain.Mapping
	var anchors, accepted, rejected, locks string
	var valid, frozen int
	if err := row.Scan(&m.ID, &m.RefRunID, &m.RunID, &m.Version,
		&anchors, &accepted, &rejected, &locks, &valid, &frozen, &m.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(anchors), &m.Anchors); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(accepted), &m.Accepted); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(rejected), &m.Rejected); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(locks), &m.Locks); err != nil {
		return nil, err
	}
	m.Valid, m.Frozen = valid == 1, frozen == 1
	return &m, nil
}

// CommitMapping validates-and-saves a mapping revision when the base version is
// the current head. Invalid mappings (rejected anchors present) are refused.
func (s *Store) CommitMapping(ctx context.Context, m domain.Mapping, baseVer int) (*domain.Mapping, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// baseVer==-1 means "open a new series after a frozen head": the new
	// revision continues the global version number but edits are allowed.
	var headVer, headFrozen int
	row := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version),0), COALESCE((SELECT frozen FROM mappings
		 WHERE ref_run_id=? AND run_id=? ORDER BY version DESC LIMIT 1),0)
		 FROM mappings WHERE ref_run_id=? AND run_id=?`,
		m.RefRunID, m.RunID, m.RefRunID, m.RunID)
	if err := row.Scan(&headVer, &headFrozen); err != nil {
		return nil, err
	}
	if baseVer == -1 {
		if headFrozen != 1 {
			return nil, errors.New("new series can only follow a frozen head")
		}
	} else if headVer != baseVer {
		return nil, ErrVersion
	}
	// Refuse to save a mapping that destroys monotonicity.
	if baseVer != -1 && headFrozen == 1 {
		return nil, errors.New("head mapping is frozen; commit baseVersion=-1 to open a new series")
	}
	if !m.Valid {
		return nil, errors.New("mapping rejected: anchor set is not monotone")
	}
	m.Version = headVer + 1
	if m.CreatedAt == "" {
		m.CreatedAt = nowISO()
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO mappings(id,ref_run_id,run_id,version,anchors,accepted,rejected,locks,valid,frozen,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.RefRunID, m.RunID, m.Version,
		string(mustJSON(m.Anchors)), string(mustJSON(m.Accepted)),
		string(mustJSON(m.Rejected)), string(mustJSON(m.Locks)),
		boolInt(m.Valid), boolInt(m.Frozen), m.CreatedAt)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &m, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// FreezeMapping marks a revision frozen.
func (s *Store) FreezeMapping(ctx context.Context, refRunID, runID string, version int) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE mappings SET frozen=1 WHERE ref_run_id=? AND run_id=? AND version=?`,
		refRunID, runID, version)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// HeadMapping returns the newest revision for a run pair.
func (s *Store) HeadMapping(ctx context.Context, refRunID, runID string) (*domain.Mapping, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+mapCols+` FROM mappings WHERE ref_run_id=? AND run_id=? ORDER BY version DESC LIMIT 1`,
		refRunID, runID)
	m, err := scanMapping(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

// GetMapping loads a specific mapping revision.
func (s *Store) GetMapping(ctx context.Context, refRunID, runID string, version int) (*domain.Mapping, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+mapCols+` FROM mappings WHERE ref_run_id=? AND run_id=? AND version=?`,
		refRunID, runID, version)
	m, err := scanMapping(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

// ListMappingVersions lists revisions of a run pair.
func (s *Store) ListMappingVersions(ctx context.Context, refRunID, runID string) ([]domain.Mapping, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+mapCols+` FROM mappings WHERE ref_run_id=? AND run_id=? ORDER BY version`,
		refRunID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Mapping
	for rows.Next() {
		m, err := scanMapping(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}
