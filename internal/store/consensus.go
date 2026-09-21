package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"peakcompass/internal/domain"
)

const consCols = `id,batch,version,ref_run_id,run_ids,peaks,norm_rule,frozen,note,created_at`

func scanConsensus(row rowScanner) (*domain.Consensus, error) {
	var c domain.Consensus
	var runIDs, peaks, note string
	var frozen int
	if err := row.Scan(&c.ID, &c.Batch, &c.Version, &c.RefRunID,
		&runIDs, &peaks, &c.NormRule, &frozen, &note, &c.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(runIDs), &c.RunIDs); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(peaks), &c.Peaks); err != nil {
		return nil, err
	}
	c.Frozen, c.Note = frozen == 1, note
	return &c, nil
}

// CommitConsensus saves a new consensus revision if baseVer is the head.
func (s *Store) CommitConsensus(ctx context.Context, c domain.Consensus, baseVer int) (*domain.Consensus, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var headVer, frozen int
	err = tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version),0), COALESCE((SELECT frozen FROM consensus WHERE batch=? ORDER BY version DESC LIMIT 1),0)
		 FROM consensus WHERE batch=?`,
		c.Batch, c.Batch).Scan(&headVer, &frozen)
	if err != nil {
		return nil, err
	}
	if headVer != baseVer {
		return nil, ErrVersion
	}
	if frozen == 1 {
		return nil, errors.New("head consensus is frozen")
	}
	c.Version = headVer + 1
	if c.CreatedAt == "" {
		c.CreatedAt = nowISO()
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO consensus(id,batch,version,ref_run_id,run_ids,peaks,norm_rule,frozen,note,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.Batch, c.Version, c.RefRunID,
		string(mustJSON(c.RunIDs)), string(mustJSON(c.Peaks)), c.NormRule,
		boolInt(c.Frozen), c.Note, c.CreatedAt)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &c, nil
}

// FreezeConsensus marks a revision frozen.
func (s *Store) FreezeConsensus(ctx context.Context, batch string, version int) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE consensus SET frozen=1 WHERE batch=? AND version=?`, batch, version)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// HeadConsensus returns the newest consensus revision for a batch.
func (s *Store) HeadConsensus(ctx context.Context, batch string) (*domain.Consensus, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+consCols+` FROM consensus WHERE batch=? ORDER BY version DESC LIMIT 1`, batch)
	c, err := scanConsensus(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// GetConsensus loads a specific consensus revision.
func (s *Store) GetConsensus(ctx context.Context, batch string, version int) (*domain.Consensus, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+consCols+` FROM consensus WHERE batch=? AND version=?`, batch, version)
	c, err := scanConsensus(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// ListConsensusVersions lists revisions of a batch.
func (s *Store) ListConsensusVersions(ctx context.Context, batch string) ([]domain.Consensus, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+consCols+` FROM consensus WHERE batch=? ORDER BY version`, batch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Consensus
	for rows.Next() {
		c, err := scanConsensus(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}
