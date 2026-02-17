-- +goose Up
-- +goose StatementBegin
CREATE TABLE profile_items
-- profile_items: the pinned set of mod file versions within a profile
--
-- Notes:
-- - v1 uses policy='pinned' and mod_file_version_id is required.
-- - Future: policy can expand to things like 'latest', etc., with migrations.
-- - priority is per-profile (higher wins conflicts).
-- - enabled allows keeping an item in the profile but disabling it temporarily.
(
  id INTEGER PRIMARY KEY,
  profile_id INTEGER NOT NULL REFERENCES profiles(id) ON UPDATE CASCADE ON DELETE CASCADE,

  -- we might use this in the future for now just always set it to 'pinned'
  policy TEXT NOT NULL DEFAULT 'pinned' CHECK (policy IN ('pinned')),

  -- pinned version
  mod_file_version_id INTEGER NOT NULL REFERENCES mod_file_versions(id) ON UPDATE CASCADE ON DELETE RESTRICT,

  enabled INTEGER NOT NULL DEFAULT FALSE CHECK (enabled IN (TRUE, FALSE)),

  -- larger numbers = higher priority (wins conflicts)
  priority INTEGER NOT NULL DEFAULT 0,

  -- remap rules/configuration for this item
  -- we create this table in the next migration
  remap_config_id INTEGER,

  -- optional notes per item
  notes TEXT,

  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),

  -- prevent duplicates: same version shouldn't appear multiple times in the same profile
  UNIQUE(profile_id, mod_file_version_id)
) STRICT;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_profile_items_profile ON profile_items(profile_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_profile_items_profile_priority ON profile_items(profile_id, enabled, priority DESC);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_profile_items_mfv ON profile_items(mod_file_version_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_profile_items_mfv;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_profile_items_profile_priority;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_profile_items_profile;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE profile_items;
-- +goose StatementEnd
