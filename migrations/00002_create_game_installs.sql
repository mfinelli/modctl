-- +goose Up
-- +goose StatementBegin
CREATE TABLE game_installs
-- a concrete installation of a game under a specific store
(
  id INTEGER PRIMARY KEY,
  store_id TEXT NOT NULL REFERENCES stores(id) ON UPDATE CASCADE ON DELETE RESTRICT,
  -- store-specific game identifier (e.g., steam appid "1091500", heroic slug, etc.)
  store_game_id TEXT NOT NULL CHECK (LENGTH(store_game_id) > 0),

  -- allows multiple installs per store_game_id if needed
  -- examples: 'default', 'library_2', 'sdcard', etc
  instance_id TEXT NOT NULL DEFAULT "default" CHECK (LENGTH(instance_id) > 0),

  -- canonical identifiers (for future): steam may populate appid into store_game_id
  -- but other stores might provide multiple identifiers
  canonical_game_id TEXT,

  -- where the game is installed (absolute path). for steam this is the game directory.
  -- for some stores this might be the "install root" used to derive targets
  install_root TEXT NOT NULL CHECK (LENGTH(install_root) > 0),

  -- opaque JSON for store-provided metadata (build id, branch, platform, etc)
  metadata TEST CHECK (json_valid(metadata)),


  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),

  -- prevent duplicate installs
  UNIQUE (store_id, store_game_id, instance_id)
) STRICT;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_game_installs_store_lookup ON game_installs(store_id, store_game_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX ids_game_installs_store_lookup;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE game_installs;
-- +goose StatementEnd
