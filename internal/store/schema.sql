CREATE TABLE IF NOT EXISTS runs (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    sample      TEXT NOT NULL,
    instrument  TEXT NOT NULL,
    batch       TEXT NOT NULL,
    times       BLOB NOT NULL,
    vals        BLOB NOT NULL,
    meta        TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS detections (
    id          TEXT PRIMARY KEY,
    run_id      TEXT NOT NULL REFERENCES runs(id),
    params      TEXT NOT NULL,
    peaks       TEXT NOT NULL,
    created_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_det_run ON detections(run_id);

CREATE TABLE IF NOT EXISTS peaksets (
    id             TEXT PRIMARY KEY,
    run_id         TEXT NOT NULL REFERENCES runs(id),
    version        INTEGER NOT NULL,
    parent_set_id  TEXT,
    based_on_det   TEXT,
    peaks          TEXT NOT NULL,
    frozen         INTEGER NOT NULL DEFAULT 0,
    note           TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ps_run ON peaksets(run_id, version);

CREATE TABLE IF NOT EXISTS mappings (
    id          TEXT PRIMARY KEY,
    ref_run_id  TEXT NOT NULL REFERENCES runs(id),
    run_id      TEXT NOT NULL REFERENCES runs(id),
    version     INTEGER NOT NULL,
    anchors     TEXT NOT NULL,
    accepted    TEXT NOT NULL,
    rejected    TEXT NOT NULL,
    locks       TEXT NOT NULL DEFAULT '[]',
    valid       INTEGER NOT NULL,
    frozen      INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL,
    UNIQUE(ref_run_id, run_id, version)
);
CREATE INDEX IF NOT EXISTS idx_map_run ON mappings(ref_run_id, run_id, version);

CREATE TABLE IF NOT EXISTS consensus (
    id          TEXT PRIMARY KEY,
    batch       TEXT NOT NULL,
    version     INTEGER NOT NULL,
    ref_run_id  TEXT NOT NULL,
    run_ids     TEXT NOT NULL,
    peaks       TEXT NOT NULL,
    norm_rule   TEXT NOT NULL,
    frozen      INTEGER NOT NULL DEFAULT 0,
    note        TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_cons_batch ON consensus(batch, version);
