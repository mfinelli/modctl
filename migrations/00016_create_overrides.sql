-- +goose Up
-- +goose StatementBegin
CREATE TABLE overrides (
  id INTEGER PRIMARY KEY,
  profile_id INTEGER NOT NULL REFERENCES profiles(id) ON UPDATE CASCADE ON DELETE CASCADE,
  target_id INTEGER NOT NULL REFERENCES targets(id) ON UPDATE CASCADE ON DELETE CASCADE,
  relpath TEXT NOT NULL CHECK (LENGTH(relpath) > 0),
  -- only latest override stored
  blob_sha256 TEXT REFERENCES blobs(sha256) ON UPDATE CASCADE ON DELETE RESTRICT,
  override_type TEXT NOT NULL CHECK (override_type IN ('full_file', 'ini_patch', 'json_patch', 'yaml_patch')),
  notes TEXT,

  -- source anchor columns
  source_archive_sha256 TEXT REFERENCES blobs(sha256) ON UPDATE CASCADE ON DELETE SET NULL,
  source_raw_path TEXT,
  source_content_sha256 TEXT CHECK (
    source_content_sha256 IS NULL OR (
      LENGTH(source_content_sha256) = 64 AND source_content_sha256 GLOB '[0-9a-f]*'
    )
  ),

  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  -- one override per (profile, target, relpath)
  UNIQUE (profile_id, target_id, relpath),

  -- blob/type consistency
  CHECK (
    (override_type = 'full_file' AND blob_sha256 IS NOT NULL)
    OR
    (override_type != 'full_file' AND blob_sha256 IS NULL)
  )
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
WHEN NEW.blob_sha256 IS NOT NULL
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
WHEN NEW.blob_sha256 IS NOT NULL
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
