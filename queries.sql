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
  mod_page_id, label, is_primary, source_url, metadata
) VALUES (
  ?, ?, ?, ?, ?
)
RETURNING id;

-- name: CreateModFileVersion :one
INSERT INTO mod_file_versions (
  mod_file_id, archive_sha256, original_name, version_string,
  nexus_file_id, uploaded_at, upstream_notes, notes, metadata
) VALUES (
  ?, ?, ?, ?,
  ?, ?, ?, ?, ?
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
    mfv.nexus_file_id,
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
SELECT id, mod_page_id, label, is_primary, source_url, created_at, updated_at
FROM mod_files
WHERE mod_page_id = ?
ORDER BY is_primary DESC, label COLLATE NOCASE, id;

-- name: ListModFileVersionsByFile :many
SELECT id, mod_file_id, archive_sha256, original_name, version_string, nexus_file_id, created_at
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
SELECT id, mod_page_id, label, is_primary
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

-- name: GetAppliedProfileIDForGame :one
SELECT applied_profile_id
FROM game_installs
WHERE id = ? LIMIT 1;

-- name: DeleteProfileByID :exec
DELETE FROM profiles
WHERE id = ?;

-- name: GetProfileItemIDByVersion :one
SELECT id
FROM profile_items
WHERE profile_id = ? AND mod_file_version_id = ?;

-- name: DeleteProfileItemByID :exec
DELETE FROM profile_items
WHERE id = ?;

-- name: GetProfileItemByVersionForOrder :one
SELECT id, priority
FROM profile_items
WHERE profile_id = ? AND mod_file_version_id = ?;

-- name: SetProfileItemPriorityByID :exec
UPDATE profile_items
SET priority = ?,
    updated_at = (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
WHERE id = ?;

-- name: ListProfileItemsForOrder :many
SELECT id, mod_file_version_id, priority
FROM profile_items
WHERE profile_id = ?
ORDER BY priority ASC;

-- name: BumpPrioritiesForProfile :exec
UPDATE profile_items
SET priority = priority + sqlc.arg(offset),
    updated_at = (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
WHERE profile_id = ?;

-- name: ListUnscannedArchives :many
SELECT DISTINCT
    b.sha256,
    b.original_name,
    b.size_bytes
FROM blobs b
JOIN mod_file_versions mfv ON mfv.archive_sha256 = b.sha256
WHERE b.kind = 'archive'
  AND mfv.inventory_scanned_at IS NULL;

-- name: InsertArchiveInventoryEntry :exec
INSERT INTO archive_inventory_entries (
    archive_sha256,
    raw_path,
    entry_type,
    size_bytes,
    link_target,
    content_sha256,
    position,
    parse_error
) VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: MarkArchiveInventoryScanned :exec
UPDATE mod_file_versions
SET inventory_scanned_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE archive_sha256 = ?
  AND inventory_scanned_at IS NULL;

-- name: IsArchiveInventoried :one
SELECT EXISTS (
    SELECT TRUE
    FROM mod_file_versions
    WHERE archive_sha256 = ?
      AND inventory_scanned_at IS NOT NULL
) AS inventoried;

-- name: GetProfileStatusItems :many
SELECT
    pi.id                       AS item_id,
    pi.priority,
    pi.enabled,
    pi.notes                    AS item_notes,
    mp.id                       AS mod_page_id,
    mp.name                     AS mod_page_name,
    mp.source_kind,
    mp.nexus_game_domain,
    mp.nexus_mod_id,
    mf.id                       AS mod_file_id,
    mf.label                    AS file_label,
    mfv.id                      AS mod_file_version_id,
    mfv.version_string,
    mfv.nexus_file_id,
    mfv.archive_sha256,
    mfv.inventory_scanned_at,
    b.size_bytes,
    CAST(COALESCE((
        SELECT COUNT(*)
        FROM remap_rules rr
        WHERE rr.remap_config_id = pi.remap_config_id
    ), 0) AS INTEGER)           AS remap_rule_count
FROM profile_items pi
JOIN mod_file_versions mfv ON mfv.id = pi.mod_file_version_id
JOIN mod_files mf           ON mf.id = mfv.mod_file_id
JOIN mod_pages mp            ON mp.id = mf.mod_page_id
JOIN blobs b                 ON b.sha256 = mfv.archive_sha256
WHERE pi.profile_id = ?
ORDER BY pi.priority ASC;

-- name: GetGameInstallAppliedState :one
SELECT
    applied_profile_id,
    applied_at,
    applied_operation_id
FROM game_installs
WHERE id = ?;

-- name: GetIncompatibleModPairsForProfile :many
SELECT
    mi.id,
    mi.reason,
    mpa.name AS mod_page_name_a,
    mpb.name AS mod_page_name_b
FROM mod_incompatibilities mi
JOIN mod_pages mpa ON mpa.id = mi.mod_page_id_a
JOIN mod_pages mpb ON mpb.id = mi.mod_page_id_b
WHERE mpa.game_install_id = (SELECT game_install_id FROM profiles p WHERE p.id = sqlc.arg(profile_id))
  AND mi.mod_page_id_a IN (
      SELECT mp.id
      FROM profile_items pi
      JOIN mod_file_versions mfv ON mfv.id = pi.mod_file_version_id
      JOIN mod_files mf ON mf.id = mfv.mod_file_id
      JOIN mod_pages mp ON mp.id = mf.mod_page_id
      WHERE pi.profile_id = sqlc.arg(profile_id)
        AND pi.enabled = TRUE
  )
  AND mi.mod_page_id_b IN (
      SELECT mp.id
      FROM profile_items pi
      JOIN mod_file_versions mfv ON mfv.id = pi.mod_file_version_id
      JOIN mod_files mf ON mf.id = mfv.mod_file_id
      JOIN mod_pages mp ON mp.id = mf.mod_page_id
      WHERE pi.profile_id = sqlc.arg(profile_id)
        AND pi.enabled = TRUE
  );

-- name: UpdateModPageName :exec
UPDATE mod_pages
SET name = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: UpdateModFileLabel :exec
UPDATE mod_files
SET label = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: UpdateModFileVersionNexusFileID :exec
UPDATE mod_file_versions
SET nexus_file_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: UpdateModPageNexusInfo :exec
UPDATE mod_pages
SET
    nexus_game_domain = ?,
    nexus_mod_id = ?,
    source_kind = 'nexus',
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: GetModFileVersionLinkState :one
SELECT
    mfv.id,
    mfv.nexus_file_id,
    mfv.archive_sha256,
    mf.id as mod_file_id,
    mf.label,
    mf.mod_page_id,
    mp.nexus_game_domain,
    mp.nexus_mod_id,
    mp.source_kind,
    b.size_bytes as archive_size
FROM mod_file_versions mfv
JOIN mod_files mf ON mf.id = mfv.mod_file_id
JOIN mod_pages mp ON mp.id = mf.mod_page_id
JOIN blobs b ON b.sha256 = mfv.archive_sha256
WHERE mfv.id = ?
AND mp.game_install_id = ?;

-- name: GetUnlinkedNexusModFileVersions :many
SELECT
    mfv.id as version_id,
    mfv.original_name,
    mfv.archive_sha256,
    mf.id as mod_file_id,
    mf.label,
    mp.id as mod_page_id,
    mp.name as mod_page_name,
    mp.nexus_game_domain,
    mp.nexus_mod_id,
    b.size_bytes as archive_size
FROM mod_file_versions mfv
JOIN mod_files mf ON mf.id = mfv.mod_file_id
JOIN mod_pages mp ON mp.id = mf.mod_page_id
JOIN blobs b ON b.sha256 = mfv.archive_sha256
WHERE mp.game_install_id = ?
AND mp.source_kind = 'nexus'
AND mp.nexus_game_domain IS NOT NULL
AND mp.nexus_mod_id IS NOT NULL
AND mfv.nexus_file_id IS NULL;

-- name: GetSkippableModFileVersions :many
SELECT
    mfv.id as version_id,
    mf.label,
    mp.name as mod_page_name
FROM mod_file_versions mfv
JOIN mod_files mf ON mf.id = mfv.mod_file_id
JOIN mod_pages mp ON mp.id = mf.mod_page_id
WHERE mp.game_install_id = ?
AND (
    mp.source_kind != 'nexus'
    OR mp.nexus_game_domain IS NULL
    OR mp.nexus_mod_id IS NULL
)
AND mfv.nexus_file_id IS NULL;

-- name: GetModFileVersionNexusFileID :one
SELECT mfv.nexus_file_id
FROM mod_file_versions mfv
JOIN mod_files mf ON mf.id = mfv.mod_file_id
JOIN mod_pages mp ON mp.id = mf.mod_page_id
WHERE mfv.id = ?
AND mp.game_install_id = ?;

-- name: UnlinkModFileVersionNexus :exec
UPDATE mod_file_versions
SET nexus_file_id = NULL,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE mod_file_versions.id = ?
AND mod_file_versions.id IN (
    SELECT mfv.id
    FROM mod_file_versions mfv
    JOIN mod_files mf ON mf.id = mfv.mod_file_id
    JOIN mod_pages mp ON mp.id = mf.mod_page_id
    WHERE mp.game_install_id = ?
);

-- name: GetNexusLinkedModPages :many
SELECT DISTINCT
    mp.id as mod_page_id,
    mp.nexus_game_domain,
    mp.nexus_mod_id
FROM mod_pages mp
JOIN mod_files mf ON mf.mod_page_id = mp.id
JOIN mod_file_versions mfv ON mfv.mod_file_id = mf.id
WHERE mp.game_install_id = ?
AND mp.source_kind = 'nexus'
AND mp.nexus_game_domain IS NOT NULL
AND mp.nexus_mod_id IS NOT NULL
AND mfv.nexus_file_id IS NOT NULL;

-- name: GetLinkedModFileVersionsForPage :many
SELECT
    mfv.id as version_id,
    mfv.nexus_file_id,
    mfv.version_string,
    mf.id as mod_file_id,
    mf.label as file_label,
    mp.name as mod_page_name
FROM mod_file_versions mfv
JOIN mod_files mf ON mf.id = mfv.mod_file_id
JOIN mod_pages mp ON mp.id = mf.mod_page_id
WHERE mf.mod_page_id = ?
AND mfv.nexus_file_id IS NOT NULL;

-- name: GetModPageByID :one
SELECT
    mp.id,
    mp.name,
    mp.source_kind,
    mp.source_url,
    mp.source_ref,
    mp.nexus_game_domain,
    mp.nexus_mod_id,
    mp.notes,
    mp.created_at,
    mp.updated_at
FROM mod_pages mp
WHERE mp.id = ?
AND mp.game_install_id = ?;

-- name: GetModPage :one
SELECT * FROM mod_pages
WHERE id = ?;

-- name: GetModFilesWithVersions :many
SELECT
    mf.id                   AS mod_file_id,
    mf.label                AS file_label,
    mf.is_primary,
    mfv.id                  AS mod_file_version_id,
    mfv.version_string,
    mfv.archive_sha256,
    mfv.nexus_file_id,
    mfv.inventory_scanned_at,
    mfv.original_name,
    b.size_bytes
FROM mod_files mf
JOIN mod_file_versions mfv ON mfv.mod_file_id = mf.id
JOIN blobs b ON b.sha256 = mfv.archive_sha256
WHERE mf.mod_page_id = ?
ORDER BY mf.id ASC, mfv.id ASC;

-- name: GetModFileVersionProfiles :many
SELECT
    p.id        AS profile_id,
    p.name      AS profile_name,
    pi.enabled,
    pi.priority
FROM profile_items pi
JOIN profiles p ON p.id = pi.profile_id
WHERE pi.mod_file_version_id = ?
AND p.game_install_id = ?
ORDER BY p.name ASC;

-- name: AddModIncompatibility :exec
INSERT INTO mod_incompatibilities (
    mod_page_id_a,
    mod_page_id_b,
    reason,
    source
) VALUES (
    -- enforce canonical ordering so (A,B) and (B,A) are always the same row
    MIN(sqlc.arg(mod_page_id_a), sqlc.arg(mod_page_id_b)),
    MAX(sqlc.arg(mod_page_id_a), sqlc.arg(mod_page_id_b)),
    ?,
    'user'
);

-- name: RemoveModIncompatibility :execrows
DELETE FROM mod_incompatibilities
WHERE mod_page_id_a = MIN(sqlc.arg(mod_page_id_a), sqlc.arg(mod_page_id_b))
  AND mod_page_id_b = MAX(sqlc.arg(mod_page_id_a), sqlc.arg(mod_page_id_b));

-- name: ListModIncompatibilities :many
SELECT
    mi.id,
    mi.mod_page_id_a,
    mi.mod_page_id_b,
    mpa.name AS mod_page_name_a,
    mpb.name AS mod_page_name_b,
    mi.reason,
    mi.created_at
FROM mod_incompatibilities mi
JOIN mod_pages mpa ON mpa.id = mi.mod_page_id_a
JOIN mod_pages mpb ON mpb.id = mi.mod_page_id_b
WHERE mpa.game_install_id = ?
ORDER BY mi.created_at DESC;

-- name: GetProfileItemForPlanning :many
SELECT
    pi.id                       AS item_id,
    pi.priority,
    pi.enabled,
    pi.remap_config_id,
    mfv.id                      AS mod_file_version_id,
    mfv.archive_sha256,
    mfv.inventory_scanned_at,
    mfv.version_string,
    mf.label                    AS file_label,
    mp.name                     AS mod_page_name
FROM profile_items pi
JOIN mod_file_versions mfv ON mfv.id = pi.mod_file_version_id
JOIN mod_files mf           ON mf.id = mfv.mod_file_id
JOIN mod_pages mp            ON mp.id = mf.mod_page_id
WHERE pi.profile_id = ?
  AND pi.enabled = TRUE
ORDER BY pi.priority DESC;

-- name: GetInventoryEntriesForArchive :many
SELECT
    id,
    archive_sha256,
    raw_path,
    entry_type,
    size_bytes,
    position
FROM archive_inventory_entries
WHERE archive_sha256 = ?
  AND entry_type = 'file'
  AND parse_error IS NULL
ORDER BY position ASC;

-- name: GetRemapRulesForConfig :many
SELECT *
FROM remap_rules
WHERE remap_config_id = ?
ORDER BY position ASC;

-- name: GetInstalledFilesForTarget :many
SELECT * FROM installed_files
WHERE game_install_id = ?
  AND target_id = ?;

-- name: GetBackupForPath :one
SELECT
    id,
    backup_blob_sha256,
    original_content_sha256,
    size_bytes
FROM backups
WHERE game_install_id = ?
  AND target_id = ?
  AND relpath = ?;

-- name: GetMaxRemapRulePosition :one
SELECT CAST(COALESCE(MAX(position), -1) AS INTEGER) AS max_position
FROM remap_rules
WHERE remap_config_id = ?;

-- name: CreateRemapConfig :one
INSERT INTO remap_configs (created_at, updated_at)
VALUES (
    strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
)
RETURNING id;

-- name: CreateRemapRule :one
INSERT INTO remap_rules (
    remap_config_id,
    position,
    rule_type,
    int_value,
    text_value,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
RETURNING id, position;

-- name: SetProfileItemRemapConfig :exec
UPDATE profile_items
SET remap_config_id = ?,
    updated_at = (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
WHERE id = ?;

-- name: DeleteRemapRule :exec
DELETE FROM remap_rules
WHERE remap_config_id = ? AND position = ?;

-- name: DeleteRemapConfig :exec
DELETE FROM remap_configs
WHERE id = ?;

-- name: ListRemapRulesForProfileItem :many
SELECT
    rr.id,
    rr.position,
    rr.rule_type,
    rr.int_value,
    rr.text_value
FROM remap_rules rr
JOIN profile_items pi ON pi.remap_config_id = rr.remap_config_id
WHERE pi.id = ?
ORDER BY rr.position ASC;

-- name: GetProfileItemRemapConfigID :one
SELECT remap_config_id
FROM profile_items
WHERE id = ?;

-- name: GetModFileVersionLabel :one
SELECT
    mp.name  AS mod_page_name,
    mf.label AS file_label
FROM mod_file_versions mfv
JOIN mod_files mf  ON mf.id = mfv.mod_file_id
JOIN mod_pages mp  ON mp.id = mf.mod_page_id
WHERE mfv.id = ?;

-- name: SetInventoryEntryContentSha256 :exec
UPDATE archive_inventory_entries
SET content_sha256 = ?
WHERE archive_sha256 = ? AND position = ?;

-- name: UpsertBackup :exec
INSERT OR REPLACE INTO backups (
    game_install_id,
    target_id,
    relpath,
    backup_blob_sha256,
    original_content_sha256,
    size_bytes,
    created_by_operation_id,
    created_at
) VALUES (
    ?, ?, ?, ?, ?, ?,
    ?,
    strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
);

-- name: CreateOperation :one
INSERT INTO operations (
    game_install_id,
    profile_id,
    op_type,
    status,
    started_at
) VALUES (
    ?,
    ?,
    ?,
    'running',
    strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
)
RETURNING id, started_at;

-- name: FinishOperation :exec
UPDATE operations
SET status = ?,
    finished_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    message = ?
WHERE id = ?;

-- name: InsertOperationChange :exec
INSERT INTO operation_changes (
    operation_id,
    game_install_id,
    target_id,
    relpath,
    action,
    old_content_sha256,
    new_content_sha256,
    old_size_bytes,
    new_size_bytes,
    mod_file_version_id,
    backup_blob_sha256,
    notes
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpsertInstalledFile :exec
INSERT INTO installed_files (
    game_install_id,
    target_id,
    relpath,
    content_sha256,
    size_bytes,
    owner_mod_file_version_id,
    owner_override_id,
    owner_profile_id,
    last_operation_id,
    installed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
ON CONFLICT(game_install_id, target_id, relpath) DO UPDATE SET
    content_sha256 = excluded.content_sha256,
    size_bytes = excluded.size_bytes,
    owner_mod_file_version_id = excluded.owner_mod_file_version_id,
    owner_override_id = excluded.owner_override_id,
    owner_profile_id = excluded.owner_profile_id,
    last_operation_id = excluded.last_operation_id,
    installed_at = excluded.installed_at;

-- name: DeleteInstalledFile :exec
DELETE FROM installed_files
WHERE game_install_id = ? AND target_id = ? AND relpath = ?;

-- name: UpdateGameInstallAppliedState :exec
UPDATE game_installs
SET applied_profile_id = ?,
    applied_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    applied_operation_id = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: ClearGameInstallAppliedState :exec
UPDATE game_installs
SET applied_profile_id = NULL,
    applied_at = NULL,
    applied_operation_id = NULL,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: UpdateInventoryEntryContentSha256 :exec
UPDATE archive_inventory_entries
SET content_sha256 = ?
WHERE archive_sha256 = ? AND position = ?
  AND content_sha256 IS NULL;

-- name: GetLastOperationForGameInstall :one
SELECT id, op_type, status, started_at, finished_at, message
FROM operations
WHERE game_install_id = ?
ORDER BY started_at DESC
LIMIT 1;

-- name: GetCompletedPathsForOperation :many
SELECT relpath
FROM operation_changes
WHERE operation_id = ?
  AND action != 'noop';

-- name: DeleteBackup :exec
DELETE FROM backups
WHERE game_install_id = ? AND target_id = ? AND relpath = ?;

-- name: GetProfileByID :one
SELECT *
FROM profiles
WHERE id = ?;

-- name: GetProfileHasPendingChanges :one
WITH desired AS (
    SELECT mod_file_version_id
    FROM profile_items
    WHERE profile_id = ?
      AND enabled = TRUE
),
current AS (
    SELECT owner_mod_file_version_id AS mod_file_version_id
    FROM installed_files
    WHERE game_install_id = ?
      AND owner_mod_file_version_id IS NOT NULL
)
SELECT
    EXISTS (
        SELECT 1 FROM desired
        WHERE mod_file_version_id NOT IN (SELECT mod_file_version_id FROM current)
    ) OR
    EXISTS (
        SELECT 1 FROM current
        WHERE mod_file_version_id NOT IN (SELECT mod_file_version_id FROM desired)
    ) AS has_pending_changes;
