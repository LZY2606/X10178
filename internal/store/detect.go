package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"peakvalley/internal/model"
)

// InsertDetection persists a detection with all candidate peaks.
func (s *Store) InsertDetection(tx *sql.Tx, d *model.Detection) error {
	params := string(mustJSON(d.Params))
	peaks := string(mustJSON(d.Peaks))
	_, err := tx.Exec(`INSERT INTO detections(id,run_id,ver,created_at,params_json,peaks_json) VALUES(?,?,?,?,?,?)`,
		d.ID, d.RunID, d.Ver, d.CreatedAt, params, peaks)
	return err
}

// LatestDetection returns the highest-version detection for a run.
func (s *Store) LatestDetection(runID string) (*model.Detection, error) {
	row := s.db.QueryRow(`SELECT id,run_id,ver,created_at,params_json,peaks_json FROM detections WHERE run_id=? ORDER BY ver DESC LIMIT 1`, runID)
	d := &model.Detection{}
	var created, params, peaks string
	if err := row.Scan(&d.ID, &d.RunID, &d.Ver, &created, &params, &peaks); err != nil {
		return nil, err
	}
	d.CreatedAt = parseTime(created)
	if err := json.Unmarshal([]byte(params), &d.Params); err != nil {
		return nil, err
	}
	return d, json.Unmarshal([]byte(peaks), &d.Peaks)
}

// GetDetection fetches a specific detection.
func (s *Store) GetDetection(id string) (*model.Detection, error) {
	row := s.db.QueryRow(`SELECT id,run_id,ver,created_at,params_json,peaks_json FROM detections WHERE id=?`, id)
	d := &model.Detection{}
	var created, params, peaks string
	if err := row.Scan(&d.ID, &d.RunID, &d.Ver, &created, &params, &peaks); err != nil {
		return nil, err
	}
	d.CreatedAt = parseTime(created)
	_ = json.Unmarshal([]byte(params), &d.Params)
	return d, json.Unmarshal([]byte(peaks), &d.Peaks)
}

// NextDetVer returns the next detection version number for a run.
func (s *Store) NextDetVer(tx *sql.Tx, runID string) (int, error) {
	var v sql.NullInt64
	if err := tx.QueryRow(`SELECT MAX(ver) FROM detections WHERE run_id=?`, runID).Scan(&v); err != nil {
		return 0, err
	}
	return int(v.Int64) + 1, nil
}

// InsertMigration persists a redetection identity migration.
func (s *Store) InsertMigration(tx *sql.Tx, runID string, oldVer, newVer int, migs []*model.Mig) error {
	_, err := tx.Exec(`INSERT INTO migrations(run_id,old_det_ver,new_det_ver,mig_json,created_at) VALUES(?,?,?,?,?)`,
		runID, oldVer, newVer, string(mustJSON(migs)), time.Now().UTC())
	return err
}

// ListMigrations returns migrations for a run.
func (s *Store) ListMigrations(runID string) ([]struct {
	OldVer, NewVer int
	Migs           []*model.Mig
}, error) {
	rows, err := s.db.Query(`SELECT old_det_ver,new_det_ver,mig_json FROM migrations WHERE run_id=? ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		OldVer, NewVer int
		Migs           []*model.Mig
	}
	for rows.Next() {
		var mj string
		var v struct {
			OldVer, NewVer int
			Migs           []*model.Mig
		}
		if err := rows.Scan(&v.OldVer, &v.NewVer, &mj); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(mj), &v.Migs); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// InsertEditVer persists one edit version.
func (s *Store) InsertEditVer(tx *sql.Tx, v *model.EditVer) error {
	_, err := tx.Exec(`INSERT INTO edit_vers(id,run_id,ver,base_det_id,base_det_ver,created_at,event_json,undone_of_ver,items_json)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		v.ID, v.RunID, v.Ver, v.BaseDetID, v.BaseDetVer, v.CreatedAt,
		string(mustJSON(v.Event)), v.UndoneOfVer, string(mustJSON(v.Items)))
	return err
}

// ListEditVers returns every edit version of a run (version history).
func (s *Store) ListEditVers(runID string) ([]*model.EditVer, error) {
	rows, err := s.db.Query(`SELECT id,run_id,ver,base_det_id,base_det_ver,created_at,event_json,undone_of_ver,items_json FROM edit_vers WHERE run_id=? ORDER BY ver`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.EditVer
	for rows.Next() {
		v := &model.EditVer{}
		var created, ev, items string
		if err := rows.Scan(&v.ID, &v.RunID, &v.Ver, &v.BaseDetID, &v.BaseDetVer, &created, &ev, &v.UndoneOfVer, &items); err != nil {
			return nil, err
		}
		v.CreatedAt = parseTime(created)
		if err := json.Unmarshal([]byte(ev), &v.Event); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(items), &v.Items); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// LatestEditVer returns the newest edit version or nil.
func (s *Store) LatestEditVer(runID string) (*model.EditVer, error) {
	vs, err := s.ListEditVers(runID)
	if err != nil || len(vs) == 0 {
		return nil, err
	}
	return vs[len(vs)-1], nil
}

// NextEditVer returns the next edit version number.
func (s *Store) NextEditVer(tx *sql.Tx, runID string) (int, error) {
	var v sql.NullInt64
	if err := tx.QueryRow(`SELECT MAX(ver) FROM edit_vers WHERE run_id=?`, runID).Scan(&v); err != nil {
		return 0, err
	}
	return int(v.Int64) + 1, nil
}
