-- +goose Up
-- +goose StatementBegin
CREATE TABLE blobs
(
  -- lowercase hex sha256 digest (64 chars)
  sha256 TEXT PRIMARY KEY CHECK (LENGTH(sha256) = 64 AND sha256 GLOB '[0-9a-f]*'),
  -- where the blob belongs logically (which on-disk store to use)
  kind TEXT NOT NULL CHECK (kind in ('archive', 'backup', 'override')),
  -- size in bytes (from filesystem)
  size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
  -- what the blob was originally imported as (useful for orphaned blobs)
  original_name TEXT,
  -- timestamp of the last time we verified the blob on disk
  verified_at TEXT,

  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT, WITHOUT ROWID;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_blobs_kind ON blobs(kind);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_blobs_kind_size ON blobs(kind, size_bytes);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_blobs_kind_size;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_blobs_kind;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE blobs;
-- +goose StatementEnd
