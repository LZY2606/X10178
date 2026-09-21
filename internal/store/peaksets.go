package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"peakcompass/internal/domain"
)

// ErrNotFound is returned for missing rows.
var ErrNotFound = errors.New("not found")

// peakSetRow is the full revision, addressed by run + version.
func insertPeakSet(ctx context.Context, tx *sql.Tx, ps domain.PeakSet) error {
	if ps.CreatedAt == "" {
		ps.CreatedAt = nowISO()
	}
	frozen := 0
	if ps.Frozen {
		frozen = 1
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO peaksets(id,run_id,version,parent_set_id,based_on_det,peaks,frozen,note,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		ps.ID, ps.RunID, ps.Version, nullable(ps.ParentSetID), nullable(ps.BasedOnDet),
		string(mustJSON(ps.Peaks)), frozen, ps.Note, ps.CreatedAt)
	return err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// SavePeakSet inserts a brand-new peak-set revision. The caller must allocate
// the version via NextPeakSetVersion inside the same transaction when it needs
// optimistic concurrency; this convenience is only for seeded first revisions.
func (s *Store) SavePeakSet(ctx context.Context, ps domain.PeakSet) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := insertPeakSet(ctx, tx, ps); err != nil {
		return err
	}
	return tx.Commit()
}

// NextPeakSetVersion allocates the next version number for a run inside tx.
func nextPeakSetVersion(ctx context.Context, tx *sql.Tx, runID string) (int, error) {
	var v int
	row := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version),0) FROM peaksets WHERE run_id=?`, runID)
	if err := row.Scan(&v); err != nil {
		return 0, err
	}
	return v + 1, nil
}

// CommitPeakSet applies edit to the run whose current head must equal baseVer.
// The next version is allocated transactionally, defeating old-version commits.
// Frozen heads refuse new revisions; use BranchPeakSet to fork first.
func (s *Store) CommitPeakSet(ctx context.Context, runID, id string, baseVer int, peaks []domain.Peak, note string) (*domain.PeakSet, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	head, err := headPeakSetTx(ctx, tx, runID)
	if err != nil {
		return nil, err
	}
	if head.Version != baseVer {
		return nil, ErrVersion
	}
	if head.Frozen {
		return nil, errors.New("head peak-set is frozen; branch a new draft before editing")
	}
	v, err := nextPeakSetVersion(ctx, tx, runID)
	if err != nil {
		return nil, err
	}
	ps := domain.PeakSet{
		ID: id, RunID: runID, Version: v, ParentSetID: head.ID,
		BasedOnDet: head.BasedOnDet, Peaks: peaks, Note: note,
		CreatedAt: nowISO(),
	}
	if err := insertPeakSet(ctx, tx, ps); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &ps, nil
}

// BranchPeakSet forks any revision (including frozen) into a new unfrozen draft.
func (s *Store) BranchPeakSet(ctx context.Context, runID, newID, fromSetID string) (*domain.PeakSet, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var ps domain.PeakSet
	var id, peaks, note string
	var parent, det sql.NullString
	var frozen int
	err = tx.QueryRowContext(ctx,
		`SELECT id,parent_set_id,based_on_det,peaks,frozen,note FROM peaksets WHERE id=?`,
		fromSetID).Scan(&id, &parent, &det, &peaks, &frozen, &note)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(peaks), &ps.Peaks); err != nil {
		return nil, err
	}
	ps.ID = id
	v, err := nextPeakSetVersion(ctx, tx, runID)
	if err != nil {
		return nil, err
	}
	ps.ID, ps.RunID, ps.Version = newID, runID, v
	ps.ParentSetID = fromSetID
	ps.Frozen, ps.Note = false, "branch from "+fromSetID
	ps.CreatedAt = nowISO()
	if err := insertPeakSet(ctx, tx, ps); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &ps, nil
}

// FreezePeakSet marks a specific revision frozen.
func (s *Store) FreezePeakSet(ctx context.Context, runID string, version int) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE peaksets SET frozen=1 WHERE run_id=? AND version=?`, runID, version)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanPeakSet(row rowScanner) (*domain.PeakSet, error) {
	var ps domain.PeakSet
	var peaks, note string
	var parent, det sql.NullString
	var frozen int
	if err := row.Scan(&ps.ID, &ps.RunID, &ps.Version, &parent, &det,
		&peaks, &frozen, &note, &ps.CreatedAt); err != nil {
		return nil, err
	}
	ps.ParentSetID, ps.BasedOnDet, ps.Note = parent.String, det.String, note
	ps.Frozen = frozen == 1
	if err := json.Unmarshal([]byte(peaks), &ps.Peaks); err != nil {
		return nil, err
	}
	return &ps, nil
}

const peakSetCols = `id,run_id,version,parent_set_id,based_on_det,peaks,frozen,note,created_at`

func headPeakSetTx(ctx context.Context, tx *sql.Tx, runID string) (*domain.PeakSet, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT `+peakSetCols+` FROM peaksets WHERE run_id=? ORDER BY version DESC LIMIT 1`, runID)
	ps, err := scanPeakSet(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return ps, err
}

// HeadPeakSet returns the latest revision for a run.
func (s *Store) HeadPeakSet(ctx context.Context, runID string) (*domain.PeakSet, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+peakSetCols+` FROM peaksets WHERE run_id=? ORDER BY version DESC LIMIT 1`, runID)
	ps, err := scanPeakSet(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return ps, err
}

// GetPeakSet loads a specific revision.
func (s *Store) GetPeakSet(ctx context.Context, runID string, version int) (*domain.PeakSet, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+peakSetCols+` FROM peaksets WHERE run_id=? AND version=?`, runID, version)
	ps, err := scanPeakSet(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return ps, err
}

// ListPeakSetVersions lists all revisions of a run.
func (s *Store) ListPeakSetVersions(ctx context.Context, runID string) ([]domain.PeakSet, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+peakSetCols+` FROM peaksets WHERE run_id=? ORDER BY version`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PeakSet
	for rows.Next() {
		ps, err := scanPeakSet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *ps)
	}
	return out, rows.Err()
}

// RevertPeakSet appends a new revision whose peaks are copied from an older
// version. History is preserved (nothing is deleted), so the action is itself
// undoable. Frozen heads are rejected; branch first.
func (s *Store) RevertPeakSet(ctx context.Context, runID string, baseVer, targetVer int, note string) (*domain.PeakSet, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	head, err := headPeakSetTx(ctx, tx, runID)
	if err != nil {
		return nil, err
	}
	if head.Version != baseVer {
		return nil, ErrVersion
	}
	if head.Frozen {
		return nil, errors.New("head peak-set is frozen; branch before reverting")
	}
	target, err := getPeakSetTx(ctx, tx, runID, targetVer)
	if err != nil {
		return nil, err
	}
	v, err := nextPeakSetVersion(ctx, tx, runID)
	if err != nil {
		return nil, err
	}
	ps := domain.PeakSet{
		ID: newPSID(), RunID: runID, Version: v, ParentSetID: head.ID,
		BasedOnDet: target.BasedOnDet, Peaks: append([]domain.Peak(nil), target.Peaks...),
		Note: note, CreatedAt: nowISO(),
	}
	if err := insertPeakSet(ctx, tx, ps); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &ps, nil
}

func getPeakSetTx(ctx context.Context, tx *sql.Tx, runID string, version int) (*domain.PeakSet, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT `+peakSetCols+` FROM peaksets WHERE run_id=? AND version=?`, runID, version)
	ps, err := scanPeakSet(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return ps, err
}
