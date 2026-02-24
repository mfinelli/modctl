-- name: GetStoreById :one
SELECT * FROM stores WHERE id = ? LIMIT 1;

-- name: ListEnabledStores :many
SELECT * FROM stores WHERE enabled = TRUE ORDER BY id;

-- name: MarkStoreInstallsNotPresent :exec
UPDATE game_installs
SET
  is_present = FALSE,
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE store_id = ?;

-- name: UpsertGameInstall :one
INSERT INTO game_installs (
  store_id,
  store_game_id,
  instance_id,
  canonical_game_id,
  display_name,
  install_root,
  metadata,
  last_seen_at,
  is_present,
  created_at,
  updated_at
)
VALUES (
  ?, -- store_id
  ?, -- store_game_id
  ?, -- instance_id
  ?, -- canonical_game_id (nullable)
  ?, -- display_name
  ?, -- install_root (canonical)
  ?, -- metadata (json text, nullable)
  ?, -- last_seen_at (iso8601z, nullable)
  TRUE,
  strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
  strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
)
ON CONFLICT (store_id, store_game_id, instance_id) DO UPDATE SET
  canonical_game_id = excluded.canonical_game_id,
  display_name      = excluded.display_name,
  install_root      = excluded.install_root,
  metadata          = excluded.metadata,
  last_seen_at      = excluded.last_seen_at,
  is_present        = TRUE,
  updated_at        = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
RETURNING id;

-- name: GetTargetByName :one
SELECT * FROM targets WHERE game_install_id = ? AND name = ? LIMIT 1;

-- name: UpsertDiscoveredTarget :exec
INSERT INTO targets (
  game_install_id,
  name,
  root_path,
  origin,
  metadata,
  created_at,
  updated_at
)
VALUES (
  ?, -- game_install_id
  ?, -- name (e.g. 'game_dir')
  ?, -- root_path (canonical)
  'discovered',
  ?, -- metadata (nullable)
  strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
  strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
)
ON CONFLICT (game_install_id, name) DO UPDATE SET
  -- IMPORTANT: caller must avoid calling this if origin='user_override'
  root_path = excluded.root_path,
  origin    = 'discovered',
  metadata  = excluded.metadata,
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
RETURNING id;

-- name: EnsureDefaultProfile :exec
INSERT INTO profiles (
  game_install_id,
  name,
  description,
  is_active,
  created_at,
  updated_at
)
SELECT
  ?,
  'default',
  NULL,
  TRUE,
  strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
  strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE NOT EXISTS (
  SELECT 1 FROM profiles WHERE game_install_id = ?
);
