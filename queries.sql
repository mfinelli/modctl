-- name: GetStoreById :one
SELECT * FROM stores WHERE id = ? LIMIT 1;

-- name: GetEnabledStoreById :one
SELECT * FROM stores WHERE id = ? AND enabled = TRUE LIMIT 1;

-- name: ListEnabledStores :many
SELECT * FROM stores WHERE enabled = TRUE ORDER BY id;

-- name: ListAllStores :many
SELECT * FROM stores ORDER BY id;

-- name: ListEnabledStoresForCompletion :many
SELECT id, display_name FROM stores WHERE enabled = TRUE ORDER BY id;

-- name: ListAllGameInstalls :many
SELECT * FROM game_installs
ORDER BY store_id, display_name, store_game_id, instance_id;

-- name: ListGameInstallsByStore :many
SELECT * FROM game_installs WHERE store_id = ?
ORDER BY display_name, store_game_id, instance_id;

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

-- name: GetGameInstallByID :one
SELECT * FROM game_installs WHERE id = ? LIMIT 1;

-- name: GetGameInstallBySelector :one
SELECT * FROM game_installs
WHERE store_id = ? AND store_game_id = ? AND instance_id = ? LIMIT 1;

-- name: ListGameInstallsByStoreGameID :many
SELECT * FROM game_installs
WHERE store_id = ? AND store_game_id = ?
ORDER BY instance_id;

-- name: CompleteGameInstallsByPrefix :many
SELECT
  id,
  store_id,
  store_game_id,
  instance_id,
  display_name,
  is_present
FROM game_installs
WHERE
  (lower(store_id || ':' || store_game_id || '#' || instance_id) LIKE lower(sqlc.arg(prefix)) ESCAPE '\')
  OR (lower(display_name) LIKE lower(sqlc.arg(prefix)) ESCAPE '\')
ORDER BY
  is_present DESC,
  display_name,
  store_id,
  store_game_id,
  instance_id
LIMIT 10;

-- name: ListTargetsForGameInstall :many
SELECT * FROM targets WHERE game_install_id = ? ORDER BY name;

-- name: GetProfilesForGameInstall :many
SELECT * FROM profiles WHERE game_install_id = ? ORDER BY name;

-- name: GetBlob :one
SELECT * FROM blobs WHERE sha256 = ? LIMIT 1;

-- name: InsertBlob :exec
INSERT INTO blobs (sha256, kind, size_bytes, original_name, verified_at)
VALUES (?, ?, ?, ?, ?);

-- name: ListBlobsByKind :many
SELECT * FROM blobs WHERE kind = ? ORDER BY created_at;

-- name: TouchBlobVerifiedAt :exec
UPDATE blobs
SET verified_at = ?
WHERE sha256 = ?;

-- name: CreateModPage :one
INSERT INTO mod_pages (
  game_install_id, name, source_kind, source_url, source_ref,
  nexus_game_domain, nexus_mod_id,
  notes, metadata
) VALUES (
  ?, ?, ?, ?, ?,
  ?, ?,
  ?, ?
)
RETURNING id;

-- name: CreateModFile :one
INSERT INTO mod_files (
  mod_page_id, label, is_primary, nexus_file_id, source_url, metadata
) VALUES (
  ?, ?, ?, ?, ?, ?
)
RETURNING id;

-- name: CreateModFileVersion :one
INSERT INTO mod_file_versions (
  mod_file_id, archive_sha256, original_name, version_string,
  uploaded_at, upstream_notes, notes, metadata
) VALUES (
  ?, ?, ?, ?,
  ?, ?, ?, ?
)
RETURNING id;

-- name: ListModsByGameInstall :many
WITH joined AS (
  SELECT
    mp.id AS mod_page_id,
    mp.name AS mod_name,
    mp.source_kind,
    mp.nexus_game_domain,
    mp.nexus_mod_id,

    mf.id AS mod_file_id,
    mf.label AS mod_file_label,

    mfv.id AS mod_file_version_id,
    mfv.version_string,
    mfv.archive_sha256,
    mfv.created_at AS imported_at,

    COUNT(DISTINCT mf.id) OVER (PARTITION BY mp.id) AS files_count,
    COUNT(mfv.id) OVER (PARTITION BY mp.id) AS versions_count,

    ROW_NUMBER() OVER (
      PARTITION BY mp.id
      ORDER BY
        (mfv.created_at IS NULL) ASC,  -- prefer non-NULL versions
        mfv.created_at DESC,
        mfv.id DESC
    ) AS rn
  FROM mod_pages mp
  LEFT JOIN mod_files mf
    ON mf.mod_page_id = mp.id
  LEFT JOIN mod_file_versions mfv
    ON mfv.mod_file_id = mf.id
  WHERE mp.game_install_id = ?
)
SELECT
  mod_page_id,
  mod_name,
  source_kind,
  nexus_game_domain,
  nexus_mod_id,

  files_count,
  versions_count,

  mod_file_id,
  mod_file_label,
  mod_file_version_id,
  version_string,
  archive_sha256,
  imported_at
FROM joined
WHERE rn = 1
ORDER BY mod_name COLLATE NOCASE, mod_page_id;

-- name: ListModFilesByPage :many
SELECT id, mod_page_id, label, is_primary, nexus_file_id, source_url, created_at, updated_at
FROM mod_files
WHERE mod_page_id = ?
ORDER BY is_primary DESC, label COLLATE NOCASE, id;

-- name: ListModFileVersionsByFile :many
SELECT id, mod_file_id, archive_sha256, original_name, version_string, created_at
FROM mod_file_versions
WHERE mod_file_id = ?
ORDER BY created_at DESC, id DESC;

-- name: GetModPageForGame :one
SELECT id, game_install_id, name, source_kind, nexus_game_domain, nexus_mod_id
FROM mod_pages
WHERE id = ? AND game_install_id = ?;

-- name: GetModPageByNexus :one
SELECT id, game_install_id, name, source_kind, nexus_game_domain, nexus_mod_id
FROM mod_pages
WHERE game_install_id = ?
  AND source_kind = 'nexus'
  AND nexus_game_domain = ?
  AND nexus_mod_id = ?;

-- name: GetModFileByLabel :one
SELECT id, mod_page_id, label, is_primary, nexus_file_id
FROM mod_files
WHERE mod_page_id = ? AND label = ?;

-- name: CountModFilesForPage :one
SELECT COUNT(1)
FROM mod_files
WHERE mod_page_id = ?;

-- name: CreateProfile :one
INSERT INTO profiles (game_install_id, name, description, is_active)
VALUES (?, ?, ?, FALSE)
RETURNING id;

-- name: GetProfileByName :one
SELECT * FROM profiles WHERE game_install_id = ? AND name = ? LIMIT 1;

-- name: ListProfilesByGameInstall :many
SELECT id, name, description, is_active, created_at, updated_at
FROM profiles
WHERE game_install_id = ?
ORDER BY is_active DESC, name COLLATE NOCASE, id;

-- name: RenameProfile :exec
UPDATE profiles
SET name = ?, updated_at = (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
WHERE id = ?;

-- name: DeactivateProfilesForGame :exec
UPDATE profiles
SET is_active = FALSE,
    updated_at = (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
WHERE game_install_id = ? AND is_active = TRUE;

-- name: ActivateProfileByName :exec
UPDATE profiles
SET is_active = TRUE,
    updated_at = (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
WHERE game_install_id = ? AND name = ?;

-- name: ListProfilesForCompletion :many
SELECT name, is_active
FROM profiles
WHERE game_install_id = ?
ORDER BY is_active DESC, name COLLATE NOCASE;

-- name: IsPriorityTaken :one
SELECT TRUE
FROM profile_items
WHERE profile_id = ? AND priority = ?;

-- name: GetActiveProfileForGame :one
SELECT * FROM profiles WHERE game_install_id = ? AND is_active = TRUE LIMIT 1;

-- name: GetMaxPriorityForProfile :one
SELECT CAST(COALESCE(MAX(priority), 0) AS INTEGER) AS max_priority
FROM profile_items
WHERE profile_id = ?;

-- name: CreateProfileItem :one
INSERT INTO profile_items (
  profile_id,
  policy,
  mod_file_version_id,
  enabled,
  priority,
  remap_config_id,
  notes
) VALUES (?, 'pinned', ?, ?, ?, NULL, NULL)
RETURNING id;

-- name: ExistsModFileVersion :one
SELECT 1
FROM mod_file_versions
WHERE id = ? LIMIT 1;

-- name: GetProfileItemByVersion :one
SELECT id, enabled
FROM profile_items
WHERE profile_id = ? AND mod_file_version_id = ? LIMIT 1;

-- name: SetProfileItemEnabled :exec
UPDATE profile_items
SET enabled = ?,
    updated_at = (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
WHERE id = ?;
