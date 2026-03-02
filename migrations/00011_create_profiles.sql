-- +goose Up
-- +goose StatementBegin
CREATE TABLE profiles
-- named mod sets for a game install
(
  id INTEGER PRIMARY KEY,
  game_install_id INTEGER NOT NULL REFERENCES game_installs(id) ON UPDATE CASCADE ON DELETE CASCADE,
  name TEXT NOT NULL CHECK (LENGTH(name) > 0),
  description TEXT,
  -- only one profile per game_install should be active at a time
  is_active INTEGER NOT NULL DEFAULT FALSE CHECK (is_active IN (TRUE, FALSE)),

  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),

  -- profile names should be unique per game install
  UNIQUE(game_install_id, name)
) STRICT;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_profiles_game ON profiles(game_install_id);
-- +goose StatementEnd

-- +goose StatementBegin
-- enforce exactly one active profile per game install (or zero if none set)
CREATE UNIQUE INDEX uq_profiles_one_active_per_install
  ON profiles(game_install_id) WHERE is_active = TRUE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX uq_profiles_one_active_per_install;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_profiles_game;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE profiles;
-- +goose StatementEnd
