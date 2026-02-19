-- +goose Up
-- +goose StatementBegin
CREATE TABLE operation_changes
-- operation_changes: detailed per-path change log for an operation
--
-- Purpose:
-- - Explainability ("what happened?")
-- - Debugging failures/rollbacks
-- - Future features (diff UI, auditing, selective revert)
--
-- Notes:
-- - This is an append-only log; do not update rows in normal operation.
-- - relpath is always relative to the target root.
-- - old/new hashes reflect on-disk bytes when known.
-- - backup_blob_sha256 is set when the operation captured/used a backup blob.
--
-- For remove: set net_content_sha256/new_size_bytes to NULL
-- For write: set old_* to NULL
-- For restore_backup set backup_blob_sha256 and new_content_sha256 (and
--   optionally old_* if computed)
(
  id INTEGER PRIMARY KEY,
  operation_id INTEGER NOT NULL REFERENCES operations(id) ON UPDATE CASCADE ON DELETE CASCADE,
  game_install_id INTEGER NOT NULL REFERENCES game_installs(id) ON UPDATE CASCADE ON DELETE CASCADE,
  target_id INTEGER NOT NULL REFERENCES targets(id) ON UPDATE CASCADE ON DELETE CASCADE,
  relpath TEXT NOT NULL CHECK (LENGTH(relpath) > 0),

  -- What happened at this path during this operation.
  -- - write: created new file (did not exist or not tracked before)
  -- - overwrite: replaced existing file content
  -- - remove: deleted a file
  -- - restore_backup: restored file content from backups store
  -- - noop: planned but resulted in no change (optional; usually omit)
  action TEXT NOT NULL CHECK (action IN ('write', 'overwrite', 'remove', 'restore_backup', 'noop')),

  -- Content hashes of the on-disk bytes before and after (when available).
  old_content_sha256 TEXT CHECK(old_content_sha256 IS NULL OR
    (length(old_content_sha256) = 64 AND old_content_sha256 GLOB '[0-9a-f]*')),
  new_content_sha256 TEXT CHECK(new_content_sha256 IS NULL OR
    (length(new_content_sha256) = 64 AND new_content_sha256 GLOB '[0-9a-f]*')),

  -- Sizes before/after (when available)
  old_size_bytes INTEGER CHECK (old_size_bytes IS NULL OR old_size_bytes >= 0),
  new_size_bytes INTEGER CHECK (new_size_bytes IS NULL OR new_size_bytes >= 0),

  -- If this operation wrote content sourced from a mod version (winner)
  mod_file_version_id INTEGER REFERENCES mod_file_versions(id) ON UPDATE CASCADE ON DELETE SET NULL,

  -- If this operation captured or used a backup blob for this path
  backup_blob_sha256 TEXT REFERENCES blobs(sha256) ON UPDATE CASCADE ON DELETE SET NULL,

  -- Optional freeform notes (errors, decisions, conflict winner info, etc.)
  notes TEXT,

  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_operation_changes_op ON operation_changes(operation_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_operation_changes_game ON operation_changes(game_install_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_operation_changes_target ON operation_changes(target_id);
-- +goose StatementEnd

-- +goose StatementBegin
-- audit changes to a particular path over time
CREATE INDEX idx_operation_changes_path ON operation_changes(game_install_id, target_id, relpath);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_operation_changes_mod_file ON operation_changes(mod_file_version_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_operation_changes_backup_blob ON operation_changes(backup_blob_sha256);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_operation_changes_action ON operation_changes(action);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_opchg_backup_blob_kind_ins
BEFORE INSERT ON operation_changes
FOR EACH ROW
WHEN NEW.backup_blob_sha256 IS NOT NULL
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
CREATE TRIGGER trg_opchg_backup_blob_kind_upd
BEFORE UPDATE OF backup_blob_sha256 ON operation_changes
FOR EACH ROW
WHEN NEW.backup_blob_sha256 IS NOT NULL
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
DROP TRIGGER trg_opchg_backup_blob_kind_upd;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER trg_opchg_backup_blob_kind_ins;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_operation_changes_action;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_operaiont_changes_backup_blob;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_operation_changes_mod_file;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_operation_changes_path;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_operation_changes_target;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_operation_changes_game;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_operation_changes_op;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE operation_changes;
-- +goose StatementEnd
