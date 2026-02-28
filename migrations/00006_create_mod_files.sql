-- +goose Up
-- +goose StatementBegin
CREATE TABLE mod_files
-- mod_files: a downloadable "file" under a mod page (Nexus has many per page)
--
-- Examples:
-- - "Main File"
-- - "Optional - 2K Textures"
-- - "Patch"
--
-- Notes:
-- - For Nexus, nexus_file_id identifies the file on the mod page.
-- - For non-Nexus, keep it as a logical grouping so versions can still exist.
(
  id INTEGER PRIMARY KEY,
  mod_page_id INTEGER NOT NULL REFERENCES mod_pages(id) ON UPDATE CASCADE ON DELETE CASCADE,

  -- human label for this file/variant
  label TEXT NOT NULL CHECK (LENGTH(label) > 0),

  -- whether this file is intended at the "primary" one
  is_primary INTEGER NOT NULL DEFAULT FALSE CHECK (is_primary in (TRUE, FALSE)),

  -- nexus file id (optional)
  nexus_file_id INTEGER,

  -- generic source URL for this file entry (optional)
  source_url TEXT,

  metadata TEXT CHECK (json_valid(metadata)),

  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_mod_files_page ON mod_files(mod_page_id);
-- +goose StatementEnd

-- +goose StatementBegin
-- ensure only one primary file per mod page
CREATE UNIQUE INDEX uq_mod_files_primary_page
  ON mod_files(mod_page_id) WHERE is_primary = 1;
-- +goose StatementEnd

-- +goose StatementBegin
-- ensure nexus file ids are unique within a mod page
CREATE UNIQUE INDEX uq_mod_files_nexus_file
  ON mod_files(mod_page_id, nexus_file_id)
  WHERE nexus_file_id IS NOT NULL;
-- +goose StatementEnd

-- +goose StatementBegin
-- ensure uniqueness for local mod_files by label
CREATE UNIQUE INDEX uq_mod_files_label_per_page
  ON mod_files(mod_page_id, label);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX uq_mod_files_label_per_page;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX uq_mod_files_nexus_file;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX uq_mod_files_primary_page;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_mod_files_page;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE mod_files;
-- +goose StatementEnd
