-- +goose Up
-- +goose StatementBegin
CREATE TABLE installed_files
-- installed_files: current tool-managed installed state per path
--
-- One row per (game_install, target, relpath) representing the current
-- content that modctl expects to exist on disk after the last successful apply.
--
-- Notes:
-- - content_sha256 hashes the actual bytes written to disk (final state).
-- - owner_mod_file_version_id identifies which mod version produced the file.
-- - last_operation_id points at the operation that last wrote/updated this path.
(
  id INTEGER PRIMARY KEY,
  game_install_id INTEGER NOT NULL REFERENCES game_installs(id) ON UPDATE CASCADE ON DELETE CASCADE,
  target_id INTEGER NOT NULL REFERENCES targets(id) ON UPDATE CASCADE ON DELETE CASCADE,
  relpath TEXT NOT NULL CHECK (LENGTH(relpath) > 0),
  -- file content identity (lowercase hex sha256)
  content_sha256 TEXT NOT NULL CHECK (LENGTH(content_sha256) = 64 AND content_sha256 GLOB '[0-9a-f]*'),
  size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
  -- owner: exactly one of these is set
  -- who "owns" this file in the plan (the winner that supplied it)
  owner_mod_file_version_id INTEGER REFERENCES mod_file_versions(id) ON UPDATE CASCADE ON DELETE RESTRICT,
  owner_override_id INTEGER REFERENCES overrides(id) ON UPDATE CASCADE ON DELETE RESTRICT,
  -- the profile that last applied this file
  owner_profile_id INTEGER REFERENCES profiles(id) ON UPDATE CASCADE ON DELETE SET NULL,
  -- operation that last wrote this path
  last_operation_id INTEGER REFERENCES operations(id) ON UPDATE CASCADE ON DELETE SET NULL,
  installed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  verified_at TEXT,

  -- one canonical row per path
  UNIQUE(game_install_id, target_id, relpath),

  CHECK (
    (owner_mod_file_version_id IS NOT NULL AND owner_override_id IS NULL)
    OR
    (owner_mod_file_version_id IS NULL AND owner_override_id IS NOT NULL)
  )
) STRICT;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_installed_files_game ON installed_files(game_install_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_installed_files_target ON installed_files(target_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_installed_files_owner ON installed_files(owner_mod_file_version_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_installed_files_owner_override ON installed_files(owner_override_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_installed_files_owner_profile ON installed_files(owner_profile_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_installed_files_operation ON installed_files(last_operation_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_installed_files_game;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_installed_files_target;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_installed_files_owner;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_installed_files_owner_override;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_installed_files_owner_profile;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_installed_files_operation;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE installed_files;
-- +goose StatementEnd
