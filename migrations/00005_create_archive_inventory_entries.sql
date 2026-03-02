-- +goose Up
-- +goose StatementBegin
CREATE TABLE archive_inventory_entries (
  id INTEGER PRIMARY KEY,
  archive_sha256 TEXT NOT NULL REFERENCES blobs(sha256) ON UPDATE CASCADE ON DELETE CASCADE,

  -- path as it appears inside the archive (no normalization)
  raw_path TEXT CHECK (parse_error IS NOT NULL OR (raw_path IS NOT NULL AND LENGTH(raw_path) > 0)),

  -- entry type
  entry_type TEXT NOT NULL DEFAULT 'file' CHECK (entry_type IN ('file', 'dir', 'symlink', 'other')),

  -- for files: size in bytes as reported by the archive header
  size_bytes INTEGER CHECK (size_bytes IS NULL OR size_bytes >= 0),

  -- for symlink entries: the link target as stored in the archive
  link_target TEXT,

  -- sha256 of the entry's content, if we hashed it at import time (optional
  -- but useful for dedup and drift detection)
  content_sha256 TEXT CHECK (
    content_sha256 IS NULL OR (
      LENGTH(content_sha256) = 64 AND content_sha256 GLOB '[0-9a-f]*'
    )
  ),

  -- position in the archive (useful for preserving bsdtar -t order and
  -- for debugging "which entry won" in duplicate-entry archives)
  position INTEGER NOT NULL CHECK (position >= 0),

  -- nullable, only set when the line could not be fully parsed
  parse_error TEXT,

  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),

  UNIQUE (archive_sha256, position)
) STRICT;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_aie_archive ON archive_inventory_entries(archive_sha256);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_aie_entry_type ON archive_inventory_entries(archive_sha256, entry_type);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_aie_archive;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_aie_entry_type;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE archive_inventory_entries;
-- +goose StatementEnd
