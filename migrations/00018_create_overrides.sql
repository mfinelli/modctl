-- +goose Up
-- +goose StatementBegin
CREATE TABLE overrides (
  id INTEGER PRIMARY KEY,
  profile_id INTEGER NOT NULL REFERENCES profiles(id) ON UPDATE CASCADE ON DELETE CASCADE,
  target_id INTEGER NOT NULL REFERENCES targets(id) ON UPDATE CASCADE ON DELETE CASCADE,
  relpath TEXT NOT NULL CHECK (LENGTH(relpath) > 0),
  -- only latest override stored
  blob_sha256 TEXT NOT NULL REFERENCES blobs(sha256) ON UPDATE CASCADE ON DELETE RESTRICT,
  -- reserved for future structured patch types, for v1 only full_file is allowed
  override_type TEXT NOT NULL DEFAULT 'full_file' CHECK (override_type IN ('full_file')),
  notes TEXT,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  -- one override per (profile, target, relpath)
  UNIQUE (profile_id, target_id, relpath)
) STRICT;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_overrides_profile ON overrides(profile_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_overrides_target ON overrides(target_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_overrides_blob ON overrides(blob_sha256);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_overrides_blob_kind_ins
BEFORE INSERT ON overrides
FOR EACH ROW
BEGIN
  SELECT
  CASE
    WHEN (SELECT kind FROM blobs WHERE sha256 = NEW.blob_sha256) IS NULL
      THEN RAISE(ABORT, 'override blob_sha256 does not reference an existing blob')
    WHEN (SELECT kind FROM blobs WHERE sha256 = NEW.blob_sha256) <> 'override'
      THEN RAISE(ABORT, 'override blob_sha256 must reference a blob with kind=override')
  END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_overrides_blob_kind_upd
BEFORE UPDATE OF blob_sha256 ON overrides
FOR EACH ROW
BEGIN
  SELECT
  CASE
    WHEN (SELECT kind FROM blobs WHERE sha256 = NEW.blob_sha256) IS NULL
      THEN RAISE(ABORT, 'override blob_sha256 does not reference an existing blob')
    WHEN (SELECT kind FROM blobs WHERE sha256 = NEW.blob_sha256) <> 'override'
      THEN RAISE(ABORT, 'override blob_sha256 must reference a blob with kind=override')
  END;
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER trg_overrides_blob_kind_upd;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER trg_overrides_blob_kind_ins;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_overrides_blob;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_overrides_target;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_overrides_profile;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE overrides;
-- +goose StatementEnd
