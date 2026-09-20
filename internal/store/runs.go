package store

import (
	"database/sql"
	"encoding/json"

	"peakvalley/internal/model"
)

// InsertRun persists a run.
func (s *Store) InsertRun(tx *sql.Tx, r *model.Run) error {
	meta := string(mustJSON(r.Meta))
	_, err := tx.Exec(`INSERT INTO runs(id,name,created_at,times_blob,signal_blob,n_points,t0,t1,dt,meta_json,raw_digest)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Name, r.CreatedAt, r.TimesBlob, r.SignalBlob, r.NPoints, r.T0, r.T1, r.DT, meta, r.RawDigest)
	return err
}

// ListRuns returns all runs ordered by creation time.
func (s *Store) ListRuns() ([]*model.Run, error) {
	rows, err := s.db.Query(`SELECT id,name,created_at,times_blob,signal_blob,n_points,t0,t1,dt,meta_json,raw_digest FROM runs ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRuns(rows)
}

// GetRun fetches one run.
func (s *Store) GetRun(id string) (*model.Run, error) {
	row := s.db.QueryRow(`SELECT id,name,created_at,times_blob,signal_blob,n_points,t0,t1,dt,meta_json,raw_digest FROM runs WHERE id=?`, id)
	r := &model.Run{}
	var meta, created string
	if err := row.Scan(&r.ID, &r.Name, &created, &r.TimesBlob, &r.SignalBlob, &r.NPoints, &r.T0, &r.T1, &r.DT, &meta, &r.RawDigest); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(meta), &r.Meta)
	r.CreatedAt = parseTime(created)
	return r, nil
}

// CountRuns returns the number of imported runs.
func (s *Store) CountRuns() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM runs`).Scan(&n)
	return n, err
}

func scanRuns(rows *sql.Rows) ([]*model.Run, error) {
	var out []*model.Run
	for rows.Next() {
		r := &model.Run{}
		var meta, created string
		if err := rows.Scan(&r.ID, &r.Name, &created, &r.TimesBlob, &r.SignalBlob, &r.NPoints, &r.T0, &r.T1, &r.DT, &meta, &r.RawDigest); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(meta), &r.Meta)
		r.CreatedAt = parseTime(created)
		out = append(out, r)
	}
	return out, rows.Err()
}
