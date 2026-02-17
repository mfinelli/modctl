-- +goose Up
-- +goose StatementBegin
CREATE TABLE mod_pages
-- mod_pages: a mod "project" or page (e.g., a Nexus mod page, or a local/manual mod)
--
-- Notes:
-- - Nexus is optional: nexus_* columns are nullable and only used when source_kind='nexus'.
-- - For other sources, use source_kind + source_url/source_ref + metadata_json.
(
  id INTEGER PRIMARY KEY,

  game_install_id INTEGER NOT NULL REFERENCES game_installs(id) ON UPDATE CASCADE ON DELETE CASCADE,

  -- human-facing name (user can edit; maybe fetch from nexus)
  name TEXT NOT NULL CHECK (LENGTH(name) > 0),

  -- where this mod page came from (extensible, not nexus-only)
  source_kind TEXT NOT NULL CHECK (source_kind IN ('nexus', 'url', 'local', 'manual', 'other')),

  -- generic source fields
  source_url TEXT, -- e.g., a web page URL
  source_ref TEXT, -- e.g., "friend-email-2026-02", "discord", "usb-stick", etc

  -- nexus-specific identifiers (optional; used for version checks
  nexus_game_domain TEXT, -- e.g., "skyrimspecialedition"
  nexus_mod_id INTEGER,

  -- freeform notes and metadata for future use
  notes TEXT,
  metadata TEXT CHECK (json_valid(metadata)),

  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_mod_pages_game ON mod_pages(game_install_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_mod_pages_source ON mod_pages(source_kind);
-- +goose StatementEnd

-- +goose StatementBegin
-- ensure nexus pages are unique per game install
-- uses a partial index so non-nexus rows are unaffected
CREATE UNIQUE INDEX uq_mod_pages_nexus
  ON mod_pages(game_install_id, nexus_game_domain, nexus_mod_id)
  WHERE source_kind = 'nexus'
  AND nexus_game_domain IS NOT NULL
  AND nexus_mod_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX uq_mod_pages_nexus;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_mod_pages_source;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX idx_mod_pages_game;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE mod_pages;
-- +goose StatementEnd
