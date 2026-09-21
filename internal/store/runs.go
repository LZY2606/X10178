package store

import (
	"context"
	"encoding/json"

	"peakcompass/internal/domain"
)

// CreateRun inserts a run with its immutable raw samples.
func (s *Store) CreateRun(ctx context.Context, r domain.Run) error {
	meta, err := json.Marshal(r.Meta)
	if err != nil {
		return err
	}
	if r.CreatedAt == "" {
		r.CreatedAt = nowISO()
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO runs(id,name,sample,instrument,batch,times,vals,meta,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Name, r.Sample, r.Instrument, r.Batch,
		encodeFloats(r.Times), encodeFloats(r.Values), string(meta), r.CreatedAt)
	return err
}

// RunExists reports whether id is present.
func (s *Store) RunExists(ctx context.Context, id string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM runs WHERE id=?`, id).Scan(&n)
	return n > 0, err
}

// GetRun loads a run including raw samples.
func (s *Store) GetRun(ctx context.Context, id string) (*domain.Run, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id,name,sample,instrument,batch,times,vals,meta,created_at FROM runs WHERE id=?`, id)
	return scanRun(row)
}

// ListRuns returns all runs without large sample payloads (Times/Values nil).
func (s *Store) ListRuns(ctx context.Context) ([]domain.Run, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,name,sample,instrument,batch,meta,created_at FROM runs ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Run
	for rows.Next() {
		var r domain.Run
		var meta string
		if err := rows.Scan(&r.ID, &r.Name, &r.Sample, &r.Instrument, &r.Batch, &meta, &r.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(meta), &r.Meta)
		out = append(out, r)
	}
	return out, rows.Err()
}

// rowScanner is satisfied by *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanRun(row rowScanner) (*domain.Run, error) {
	var r domain.Run
	var times, vals []byte
	var meta string
	if err := row.Scan(&r.ID, &r.Name, &r.Sample, &r.Instrument, &r.Batch,
		&times, &vals, &meta, &r.CreatedAt); err != nil {
		return nil, err
	}
	r.Times = decodeFloats(times)
	r.Values = decodeFloats(vals)
	_ = json.Unmarshal([]byte(meta), &r.Meta)
	return &r, nil
}

// SaveDetection stores an immutable detection record.
func (s *Store) SaveDetection(ctx context.Context, d domain.Detection) error {
	if d.CreatedAt == "" {
		d.CreatedAt = nowISO()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO detections(id,run_id,params,peaks,created_at) VALUES(?,?,?,?,?)`,
		d.ID, d.RunID, string(mustJSON(d.Params)), string(mustJSON(d.Peaks)), d.CreatedAt)
	return err
}

// GetDetection loads a detection by id.
func (s *Store) GetDetection(ctx context.Context, id string) (*domain.Detection, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id,run_id,params,peaks,created_at FROM detections WHERE id=?`, id)
	var d domain.Detection
	var params, peaks string
	if err := row.Scan(&d.ID, &d.RunID, &params, &peaks, &d.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(params), &d.Params); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(peaks), &d.Peaks); err != nil {
		return nil, err
	}
	return &d, nil
}

// LatestDetection returns the most recently created detection for a run.
func (s *Store) LatestDetection(ctx context.Context, runID string) (*domain.Detection, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id,run_id,params,peaks,created_at FROM detections
		 WHERE run_id=? ORDER BY created_at DESC, id DESC LIMIT 1`, runID)
	var d domain.Detection
	var params, peaks string
	if err := row.Scan(&d.ID, &d.RunID, &params, &peaks, &d.CreatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(params), &d.Params)
	_ = json.Unmarshal([]byte(peaks), &d.Peaks)
	return &d, nil
}
