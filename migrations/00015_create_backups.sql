-- +goose Up
-- +goose StatementBegin
CREATE TABLE backups
-- backups: mapping of pre-existing (non-tool-owned) files that were backed up
--
-- One row per (game_install, target, relpath) indicating that prior content
-- existed and was preserved in the backup store before being overwritten.
--
-- Notes:
-- - backup_blob_sha256 references blobs(kind='backup') (enforced via trigger below).
-- - original_content_sha256 is optional but useful to validate restore.
-- - created_by_operation_id tracks when the backup was captured.
(
  id INTEGER PRIMARY KEY,
  game_install_id INTEGER NOT NULL REFERENCES game_installs(id) ON UPDATE CASCADE ON DELETE CASCADE,
  target_id INTEGER NOT NULL REFERENCES targets(id) ON UPDATE CASCADE ON DELETE CASCADE,
  relpath TEXT NOT NULL CHECK (LENGTH(relpath) > 0),
  backup_blob_sha256 TEXT NOT NULL REFERENCES blobs(sha256) ON UPDATE CASCADE ON DELETE RESTRICT,
  -- hash of the original bytes at backup time (can match backup blob hash,
  -- but keeping it separate makes intent explicit and allows future variants).
  original_content_sha256 TEXT CHECK(original_content_sha256 IS NULL OR (
    (length(original_content_sha256) = 64 AND original_content_sha256 GLOB '[0-9a-f]*'))),
  size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
  created_by_operation_id INTEGER REFERENCES operations(id) ON UPDATE CASCADE ON DELETE SET NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),

  -- only one backup mapping per path (latest wins; app may overwrite row on new backup)
  UNIQUE(game_install_id, target_id, relpath)
) STRICT;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_backups_game ON backups(game_install_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_backups_target ON backups(target_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_backups_blob ON backups(backup_blob_sha256);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_backups_operation ON backups(created_by_operation_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_backups_blob_kind_ins
BEFORE INSERT ON backups
FOR EACH ROW
BEGIN
  SELECT
  CASE
    WHEN (SELECT kind FROM blobs WHERE sha256 = NEW.backup_blob_sha256) IS NULL
      THEN RAISE(ABORT, 'backup_blob_sha256 does not reference an existing blob')
    WHEN (SELECT kind FROM blobs WHERE sha256 = NEW.backup_blob_sha256) <> 'backup'
      THEN RAISE(ABORT, 'backup_blob_sha256 must reference a blob with kind=backup')
  END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_backups_blob_kind_upd
BEFORE UPDATE OF backup_blob_sha256 ON backups
FOR EACH ROW
BEGIN
  SELECT
  CASE
    WHEN (SELECT kind FROM blobs WHERE sha256 = NEW.backup_blob_sha256) IS NULL
      THEN RAISE(ABORT, 'backup_blob_sha256 does not reference an existing blob')
    WHEN (SELECT kind FROM blobs WHERE sha256 = NEW.backup_blob_sha256) <> 'backup'
      THEN RAISE(ABORT, 'backup_blob_sha256 must reference a blob with kind=backup')
  END;
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER trg_backups_blob_kind_upd;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER trg_backups_blob_kind_ins;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_backups_operation;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_backups_blob;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_backups_target;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_backups_game;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE backups;
-- +goose StatementEnd
