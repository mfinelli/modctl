-- +goose Up
-- +goose StatementBegin
CREATE TABLE targets
-- install roots within a game_install (v1: only 'game_dir')
(
  id INTEGER PRIMARY KEY,
  game_install_id INTEGER NOT NULL REFERENCES game_installs(id) ON UPDATE CASCADE ON DELETE CASCADE,

  -- stable per-install name v1: 'game_dir'
  -- in the future maybe 'proton_prefix', 'documents', etc
  name TEXT NOT NULL CHECK (LENGTH(name) > 0),
  -- Absolute path for this target root
  root_path TEXT NOT NULL CHECK(LENGTH(root_path) > 0),

  -- classify how the target was derived (useful for debugging)
  origin TEXT NOT NULL DEFAULT 'discovered' CHECK (origin IN ('discovered', 'user_override')),

  -- opaque json for target-specific metadata (prefix layout, etc)
  metadata TEXT CHECK (json_valid(metadata)),

  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),

  UNIQUE (game_install_id, name)
) STRICT;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_targets_install ON targets(game_install_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_targets_install_name ON targets(game_install_id, name);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_targets_install_name;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_targets_install;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE targets;
-- +goose StatementEnd
