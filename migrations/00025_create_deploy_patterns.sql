-- +goose Up
-- +goose StatementBegin
CREATE TABLE profile_item_skip_backup_patterns (
  id INTEGER PRIMARY KEY,
  profile_item_id INTEGER NOT NULL REFERENCES profile_items(id) ON UPDATE CASCADE ON DELETE CASCADE,
  pattern TEXT NOT NULL CHECK (LENGTH(pattern) > 0),
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  UNIQUE (profile_item_id, pattern)
) STRICT;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_pisbp_profile_item ON profile_item_skip_backup_patterns(profile_item_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE profile_item_write_once_patterns (
  id INTEGER PRIMARY KEY,
  profile_item_id INTEGER NOT NULL REFERENCES profile_items(id) ON UPDATE CASCADE ON DELETE CASCADE,
  pattern TEXT NOT NULL CHECK (LENGTH(pattern) > 0),
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  UNIQUE (profile_item_id, pattern)
) STRICT;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_piwop_profile_item ON profile_item_write_once_patterns(profile_item_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_piwop_profile_item;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE profile_item_write_once_patterns;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_pisbp_profile_item;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE profile_item_skip_backup_patterns;
-- +goose StatementEnd
