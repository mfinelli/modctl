-- +goose Up
-- +goose StatementBegin
CREATE TABLE stores
-- stores system catalog of known store types + per-install configuration
(
  -- Stable identifier; examples: 'steam', 'heroic', 'gog'
  id TEXT PRIMARY KEY CHECK (LENGTH(id) > 0),
  -- Human-facing name (we'll localize this in-app later if needed)
  display_name TEXT NOT NULL CHECK(LENGTH(display_name) > 0),
  -- Integration handler name in the binary (usually the same as "id" but
  -- allows aliasing/renames or new/updated behavior without changing the PK
  implementation TEXT NOT NULL CHECK(LENGTH(implementation) > 0),
  -- Whether this store should be scanned/refreshed
  enabled INTEGER NOT NULL DEFAULT FALSE CHECK (enabled IN (TRUE, FALSE)),
  -- Optional JSON for store-specific configuration (root overrides, auth, etc)
  config TEXT CHECK (json_valid(config)),

  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT, WITHOUT ROWID;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_stores_enabled ON stores(enabled);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_stores_enabled;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE stores;
-- +goose StatementEnd
