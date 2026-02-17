-- +goose Up
-- +goose StatementBegin
CREATE TABLE profile_path_policies
-- Optional: per-path conflict policy overrides (we'll probably implement this
-- in v2+)
--
-- Default behavior remains "priority winner".
-- This table allows lets us define that *some paths* should use different
-- resolution later (e.g., merge_text, manual), without changing profile_items.
(
  id INTEGER PRIMARY KEY,
  profile_id INTEGER NOT NULL REFERENCES profiles(id) ON UPDATE CASCADE ON DELETE CASCADE,

  -- target the policy applies to (default's to 'game_dir' if null)
  target_name TEXT,

  -- path selector: for now keep it simple: exact relative path or a glob pattern
  path_pattern TEXT NOT NULL CHECK (LENGTH(path_pattern) > 0),

  -- reserved for future use, for now we'll probably always just use priority
  policy TEXT NOT NULL DEFAULT 'priority' CHECK (policy IN ('priority', 'merge_text', 'manual')),

  metadata TEXT CHECK (json_valid(metadata)),

  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),

  UNIQUE (profile_id, target_name, path_pattern)
) STRICT;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_profile_path_policies_profile ON profile_path_policies(profile_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_profile_path_policies_profile;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE profile_path_policies;
-- +goose StatementEnd
