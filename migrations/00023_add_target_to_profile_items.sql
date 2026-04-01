-- +goose Up
-- +goose StatementBegin
-- Step 1: create the new table with target_id NOT NULL
CREATE TABLE profile_items_new
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

  target_id INTEGER NOT NULL REFERENCES targets(id) ON UPDATE CASCADE ON DELETE CASCADE,
  enabled INTEGER NOT NULL DEFAULT FALSE CHECK (enabled IN (TRUE, FALSE)),

  -- larger numbers = higher priority (wins conflicts)
  priority INTEGER NOT NULL DEFAULT 0,

  -- remap rules/configuration for this item
  remap_config_id INTEGER REFERENCES remap_configs(id) ON UPDATE CASCADE ON DELETE CASCADE,

   -- optional notes per item
  notes TEXT,

  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),

  -- prevent duplicates: same version shouldn't appear multiple times in the same profile
  UNIQUE(profile_id, mod_file_version_id),

  -- priority is unique per profile
  UNIQUE(profile_id, priority)
) STRICT;
-- +goose StatementEnd

-- +goose StatementBegin
-- Step 2: copy existing rows, backfilling target_id from the game_dir target
INSERT INTO profile_items_new (
  id, profile_id, policy, mod_file_version_id, target_id,
  enabled, priority, remap_config_id, notes, created_at, updated_at
)
SELECT
  pi.id,
  pi.profile_id,
  pi.policy,
  pi.mod_file_version_id,
  t.id AS target_id,
  pi.enabled,
  pi.priority,
  pi.remap_config_id,
  pi.notes,
  pi.created_at,
  pi.updated_at
FROM profile_items pi
JOIN profiles p ON p.id = pi.profile_id
JOIN targets t ON t.game_install_id = p.game_install_id AND t.name = 'game_dir';
-- +goose StatementEnd

-- +goose StatementBegin
-- Step 3: drop old table
DROP TABLE profile_items;
-- +goose StatementEnd

-- +goose StatementBegin
-- Step 4: rename new table into place
ALTER TABLE profile_items_new RENAME TO profile_items;
-- +goose StatementEnd

-- Step 5: recreate indexes
-- +goose StatementBegin
CREATE INDEX idx_profile_items_profile ON profile_items(profile_id);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_profile_items_remap_config ON profile_items(remap_config_id);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_profile_items_profile_priority ON profile_items(profile_id, enabled, priority DESC);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_profile_items_mfv ON profile_items(mod_file_version_id);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_profile_items_target ON profile_items(target_id);
-- +goose StatementEnd

-- +goose Down
SELECT 'TODO: do the rebuild in reverse...';
