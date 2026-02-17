-- +goose Up
-- +goose StatementBegin
CREATE TABLE mod_file_versions
-- mod_file_versions: a specific archive blob for a mod_file
--
-- Notes:
-- - References blobs.sha256 (kind must be 'archive' by application invariant).
-- - Multiple versions can exist even if the source doesn’t provide versioning.
-- - Profiles will typically pin a specific mod_file_version_id.
(
  id INTEGER PRIMARY KEY,

  mod_file_id INTEGER NOT NULL REFERENCES mod_files(id) ON UPDATE CASCADE ON DELETE CASCADE,

  archive_sha256 TEXT NOT NULL REFERENCES blobs(sha256) ON UPDATE CASCADE ON DELETE RESTRICT,

  -- what the archive was called when imported/downloaded
  original_name TEXT,

  -- optioal version string (nexus might provide one; other sources may not)
  version_string TEXT,

  -- optional upstream timestamps (best-effort)
  uploaded_at TEXT,

  -- optional upstream notes or changelog
  upstream_notes TEXT,

  -- optional user notes
  notes TEXT,

  metadata TEXT CHECK (json_valid(metadata)),

  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_mod_file_versions_file ON mod_file_versions(mod_file_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_mod_file_versions_archive ON mod_file_versions(archive_sha256);
-- +goose StatementEnd

-- +goose StatementBegin
-- prevent duplicate attachment of the same blob archive to the same mod_file
CREATE UNIQUE INDEX uq_mod_file_versions_file_blob
  ON mod_file_versions(mod_file_id, archive_sha256);
-- +goose StatementEnd

-- +goose StatementBegin
-- enforce that mod_file_versions.archive_sha256 references a blob with kind=archive
CREATE TRIGGER trg_mfv_archive_blob_kind_ins
BEFORE INSERT ON mod_file_versions
FOR EACH ROW
BEGIN
  SELECT
  CASE
    WHEN (SELECT kind FROM blobs WHERE sha256 = NEW.archive_sha256) IS NULL
      THEN RAISE(ABORT, 'archive_sha256 does not reference an existing blob')
    WHEN (SELECT kind FROM blobs WHERE sha256 = NEW.archive_sha256) <> 'archive'
      THEN RAISE(ABORT, 'archive_sha256 must reference a blob with kind=archive')
  END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
-- enforce that mod_file_versions.archive_sha256 references a blob with kind=archive
CREATE TRIGGER trg_mfv_archive_blob_kind_upd
BEFORE UPDATE OF archive_sha256 ON mod_file_versions
FOR EACH ROW
BEGIN
  SELECT
  CASE
    WHEN (SELECT kind FROM blobs WHERE sha256 = NEW.archive_sha256) IS NULL
      THEN RAISE(ABORT, 'archive_sha256 does not reference an existing blob')
    WHEN (SELECT kind FROM blobs WHERE sha256 = NEW.archive_sha256) <> 'archive'
      THEN RAISE(ABORT, 'archive_sha256 must reference a blob with kind=archive')
  END;
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER trg_mfv_archive_blob_kind_upd;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER trg_mfv_archive_blob_kind_ins;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX uq_mod_file_versions_file_blob;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_mod_file_versions_archive;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_mod_file_versions_file;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE mod_file_versions;
-- +goose StatementEnd
