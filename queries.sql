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
  ?, -- is_present
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
  ?1,
  'default',
  NULL,
  TRUE,
  strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
  strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE NOT EXISTS (
  SELECT 1 FROM profiles WHERE game_install_id = ?1
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
WITH
per_page_counts AS (
  SELECT
    mp.id AS mod_page_id,
    COUNT(DISTINCT mf.id)  AS files_count,
    COUNT(mfv.id)          AS versions_count
  FROM mod_pages mp
  LEFT JOIN mod_files mf
    ON mf.mod_page_id = mp.id
  LEFT JOIN mod_file_versions mfv
    ON mfv.mod_file_id = mf.id
  WHERE mp.game_install_id = ?1
  GROUP BY mp.id
),
joined AS (
  SELECT
    mp.id   AS mod_page_id,
    mp.name AS mod_name,
    mp.source_kind,
    mp.nexus_game_domain,
    mp.nexus_mod_id,
    mf.id   AS mod_file_id,
    mf.label AS mod_file_label,
    mfv.id  AS mod_file_version_id,
    mfv.version_string,
    mfv.nexus_file_id,
    mfv.archive_sha256,
    mfv.created_at AS imported_at,
    COALESCE(ppc.files_count, 0) AS files_count,
    COALESCE(ppc.versions_count, 0) AS versions_count,
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
  LEFT JOIN per_page_counts ppc
    ON ppc.mod_page_id = mp.id
  WHERE mp.game_install_id = ?1
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
VALUES (?, ?, ?, ?)
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
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE profile_id = sqlc.arg(profile_id);

-- name: ListUnscannedArchives :many
SELECT
    b.sha256,
    b.original_name,
    b.size_bytes
FROM blobs b
WHERE b.kind = 'archive'
  AND NOT EXISTS (
    SELECT TRUE
    FROM archive_inventory_entries aie
    WHERE aie.archive_sha256 = b.sha256
  );

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
    FROM archive_inventory_entries
    WHERE archive_sha256 = ?
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
    mf.is_primary,
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
    MIN(CAST(sqlc.arg(mod_page_id_a) AS INTEGER), CAST(sqlc.arg(mod_page_id_b) AS INTEGER)),
    MAX(CAST(sqlc.arg(mod_page_id_a) AS INTEGER), CAST(sqlc.arg(mod_page_id_b) AS INTEGER)),
    sqlc.arg(reason),
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
    owner_override_id,
    backup_blob_sha256,
    notes
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

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

-- name: GetInstalledFileCountForGameInstall :one
SELECT COUNT(*) AS count
FROM installed_files
WHERE game_install_id = ?;

-- name: GetBackupCountForGameInstall :one
SELECT COUNT(*) AS count
FROM backups
WHERE game_install_id = ?;

-- name: GetLastIncompleteOperationForGameInstall :one
SELECT id, op_type, started_at
FROM operations
WHERE game_install_id = ?
  AND status = 'running'
ORDER BY started_at DESC
LIMIT 1;

-- name: ListOperationsForGameInstall :many
SELECT
    o.id,
    o.op_type,
    o.status,
    o.started_at,
    o.finished_at,
    o.message,
    p.name AS profile_name
FROM operations o
LEFT JOIN profiles p ON p.id = o.profile_id
WHERE o.game_install_id = ?
ORDER BY o.started_at DESC
LIMIT ?;

-- name: ListAllOperations :many
SELECT
    o.id,
    o.op_type,
    o.status,
    o.started_at,
    o.finished_at,
    o.message,
    p.name  AS profile_name,
    gi.display_name AS game_name
FROM operations o
LEFT JOIN profiles p ON p.id = o.profile_id
LEFT JOIN game_installs gi ON gi.id = o.game_install_id
ORDER BY o.started_at DESC
LIMIT ?;

-- name: GetOperationByID :one
SELECT
    o.id,
    o.game_install_id,
    o.op_type,
    o.status,
    o.started_at,
    o.finished_at,
    o.message,
    p.name AS profile_name,
    gi.display_name AS game_name
FROM operations o
LEFT JOIN profiles p ON p.id = o.profile_id
LEFT JOIN game_installs gi ON gi.id = o.game_install_id
WHERE o.id = ?;

-- name: ListOperationChanges :many
SELECT *
FROM operation_changes
WHERE operation_id = ?
ORDER BY created_at ASC;

-- name: ListUnreferencedBlobs :many
-- Returns blobs of the given kind that are not referenced by any live row.
-- Archives are referenced by mod_file_versions.archive_sha256.
-- Backups are referenced by backups.backup_blob_sha256.
SELECT b.sha256, b.kind, b.size_bytes, b.original_name, b.created_at
FROM blobs b
WHERE b.kind = ?
  AND CASE b.kind
    WHEN 'archive' THEN NOT EXISTS (
      SELECT TRUE FROM mod_file_versions mfv WHERE mfv.archive_sha256 = b.sha256
    )
    WHEN 'backup' THEN NOT EXISTS (
      SELECT TRUE FROM backups bk WHERE bk.backup_blob_sha256 = b.sha256
    )
    WHEN 'override' THEN NOT EXISTS (
      SELECT TRUE FROM overrides ov WHERE ov.blob_sha256 = b.sha256
    )
    ELSE FALSE
  END;

-- name: DeleteBlob :exec
DELETE FROM blobs WHERE sha256 = ?;

-- name: GetBlobContext :many
-- Returns identifying context for a blob: its original name and, for archive
-- blobs, the mod page and file it belongs to.
SELECT
    b.sha256,
    b.original_name,
    mp.name        AS mod_page_name,
    mf.label       AS mod_file_label,
    mfv.version_string
FROM blobs b
LEFT JOIN mod_file_versions mfv ON mfv.archive_sha256 = b.sha256
LEFT JOIN mod_files mf          ON mf.id = mfv.mod_file_id
LEFT JOIN mod_pages mp          ON mp.id = mf.mod_page_id
WHERE b.sha256 = ?;

-- name: ListArchiveBlobsForGameInstall :many
SELECT DISTINCT b.sha256, b.kind, b.size_bytes, b.original_name, b.created_at
FROM blobs b
JOIN mod_file_versions mfv ON mfv.archive_sha256 = b.sha256
JOIN mod_files mf ON mf.id = mfv.mod_file_id
JOIN mod_pages mp ON mp.id = mf.mod_page_id
WHERE mp.game_install_id = ?;

-- name: ListBackupBlobsForGameInstall :many
SELECT DISTINCT b.sha256, b.kind, b.size_bytes, b.original_name, b.created_at
FROM blobs b
JOIN backups bk ON bk.backup_blob_sha256 = b.sha256
WHERE bk.game_install_id = ?;

-- name: ExportGetArchiveBlobsForGameInstall :many
SELECT DISTINCT b.sha256, b.kind, b.size_bytes, b.original_name, b.verified_at, b.created_at
FROM blobs b
JOIN mod_file_versions mfv ON mfv.archive_sha256 = b.sha256
JOIN mod_files mf ON mf.id = mfv.mod_file_id
JOIN mod_pages mp ON mp.id = mf.mod_page_id
WHERE mp.game_install_id = ?;

-- name: ExportGetBackupBlobsForGameInstall :many
SELECT DISTINCT b.sha256, b.kind, b.size_bytes, b.original_name, b.verified_at, b.created_at
FROM blobs b
JOIN backups bk ON bk.backup_blob_sha256 = b.sha256
WHERE bk.game_install_id = ?;

-- name: ExportGetModPagesForGameInstall :many
SELECT * FROM mod_pages WHERE game_install_id = ?;

-- name: ExportGetModFilesForGameInstall :many
SELECT mf.id, mf.mod_page_id, mf.label, mf.is_primary, mf.source_url,
       mf.metadata, mf.created_at, mf.updated_at
FROM mod_files mf
JOIN mod_pages mp ON mp.id = mf.mod_page_id
WHERE mp.game_install_id = ?;

-- name: ExportGetModFileVersionsForGameInstall :many
SELECT mfv.id, mfv.mod_file_id, mfv.archive_sha256, mfv.original_name,
       mfv.version_string, mfv.nexus_file_id, mfv.uploaded_at,
       mfv.inventory_scanned_at, mfv.upstream_notes, mfv.notes,
       mfv.metadata, mfv.created_at, mfv.updated_at
FROM mod_file_versions mfv
JOIN mod_files mf ON mf.id = mfv.mod_file_id
JOIN mod_pages mp ON mp.id = mf.mod_page_id
WHERE mp.game_install_id = ?;

-- name: ExportGetInventoryForGameInstall :many
SELECT DISTINCT aie.id, aie.archive_sha256, aie.raw_path, aie.entry_type,
       aie.size_bytes, aie.link_target, aie.content_sha256,
       aie.position, aie.parse_error, aie.created_at
FROM archive_inventory_entries aie
JOIN mod_file_versions mfv ON mfv.archive_sha256 = aie.archive_sha256
JOIN mod_files mf ON mf.id = mfv.mod_file_id
JOIN mod_pages mp ON mp.id = mf.mod_page_id
WHERE mp.game_install_id = ?;

-- name: ExportGetRemapConfigsForGameInstall :many
SELECT DISTINCT rc.id, rc.created_at, rc.updated_at
FROM remap_configs rc
JOIN profile_items pi ON pi.remap_config_id = rc.id
JOIN profiles p ON p.id = pi.profile_id
WHERE p.game_install_id = ?;

-- name: ExportGetRemapRulesForConfig :many
SELECT id, remap_config_id, position, rule_type, int_value, text_value,
       json_value, created_at, updated_at
FROM remap_rules WHERE remap_config_id = ?;

-- name: ExportGetProfileItemsForGameInstall :many
SELECT pi.id, pi.profile_id, pi.policy, pi.mod_file_version_id,
       pi.enabled, pi.priority, pi.remap_config_id, pi.notes,
       pi.created_at, pi.updated_at
FROM profile_items pi
JOIN profiles p ON p.id = pi.profile_id
WHERE p.game_install_id = ?;

-- name: ExportGetProfilePathPoliciesForGameInstall :many
SELECT ppp.id, ppp.profile_id, ppp.target_name, ppp.path_pattern,
       ppp.policy, ppp.metadata, ppp.created_at, ppp.updated_at
FROM profile_path_policies ppp
JOIN profiles p ON p.id = ppp.profile_id
WHERE p.game_install_id = ?;

-- name: ExportGetBackupsForGameInstall :many
SELECT * FROM backups WHERE game_install_id = ?;

-- name: ExportGetModIncompatibilitiesForGameInstall :many
SELECT mi.id, mi.mod_page_id_a, mi.mod_page_id_b, mi.reason, mi.source,
       mi.created_at, mi.updated_at
FROM mod_incompatibilities mi
JOIN mod_pages mp ON mp.id = mi.mod_page_id_a
WHERE mp.game_install_id = ?;

-- name: ExportInsertStore :exec
INSERT OR IGNORE INTO stores (id, display_name, implementation, enabled, config, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ExportInsertGameInstall :exec
INSERT INTO game_installs (
    id, store_id, store_game_id, display_name, instance_id,
    canonical_game_id, install_root, metadata,
    last_seen_at, is_present,
    applied_profile_id, applied_at, applied_operation_id,
    created_at, updated_at
) VALUES (
    ?, ?, ?, ?,
    ?, ?, ?,
    ?, ?, ?,
    NULL, NULL, NULL,
    ?, ?
);

-- name: ExportInsertTarget :exec
INSERT INTO targets (id, game_install_id, name, root_path, origin, metadata, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: ExportInsertBlob :exec
INSERT INTO blobs (sha256, kind, size_bytes, original_name, verified_at, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ExportInsertModPage :exec
INSERT INTO mod_pages (id, game_install_id, name, source_kind, source_url, source_ref,
                       nexus_game_domain, nexus_mod_id, notes, metadata, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?,
        ?, ?, ?, ?, ?);

-- name: ExportInsertModFile :exec
INSERT INTO mod_files (id, mod_page_id, label, is_primary, source_url, metadata, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: ExportInsertModFileVersion :exec
INSERT INTO mod_file_versions (id, mod_file_id, archive_sha256, original_name,
                                version_string, nexus_file_id, uploaded_at,
                                inventory_scanned_at, upstream_notes, notes,
                                metadata, created_at, updated_at)
VALUES (?, ?, ?, ?,
        ?, ?, ?,
        ?, ?, ?,
        ?, ?, ?);

-- name: ExportInsertInventoryEntry :exec
INSERT INTO archive_inventory_entries (id, archive_sha256, raw_path, entry_type,
                                       size_bytes, link_target, content_sha256,
                                       position, parse_error, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ExportInsertRemapConfig :exec
INSERT INTO remap_configs (id, created_at, updated_at)
VALUES (?, ?, ?);

-- name: ExportInsertRemapRule :exec
INSERT INTO remap_rules (id, remap_config_id, position, rule_type, int_value,
                         text_value, json_value, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ExportInsertProfile :exec
INSERT INTO profiles (id, game_install_id, name, description, is_active, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ExportInsertProfileItem :exec
INSERT INTO profile_items (id, profile_id, policy, mod_file_version_id,
                            enabled, priority, remap_config_id, notes,
                            created_at, updated_at)
VALUES (?, ?, ?, ?,
        ?, ?, ?, ?,
        ?, ?);

-- name: ExportInsertProfilePathPolicy :exec
INSERT INTO profile_path_policies (id, profile_id, target_name, path_pattern,
                                    policy, metadata, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: ExportInsertBackup :exec
INSERT INTO backups (id, game_install_id, target_id, relpath, backup_blob_sha256,
                     original_content_sha256, size_bytes, created_by_operation_id, created_at)
VALUES (?, ?, ?, ?, ?,
        ?, ?, NULL, ?);

-- name: ExportInsertModIncompatibility :exec
INSERT INTO mod_incompatibilities (id, mod_page_id_a, mod_page_id_b, reason, source, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ImportGetGameInstallByStoreKey :one
SELECT id FROM game_installs
WHERE store_id = ?
  AND store_game_id = ?
  AND instance_id = ?;

-- name: ImportCheckDBIsEmpty :one
-- Returns true if the DB has no meaningful user data beyond auto-seeded rows.
-- We consider the DB empty if the only store is 'steam' (auto-seeded) and
-- there are no game installs.
SELECT
    (SELECT COUNT(*) FROM game_installs) = 0
    AND
    (SELECT COUNT(*) FROM stores WHERE id != 'steam') = 0
AS is_empty;

-- name: ImportDeleteGameInstall :exec
DELETE FROM game_installs WHERE id = ?;

-- name: ImportGetStoreByID :one
SELECT id FROM stores WHERE id = ?;

-- name: ImportInsertGameInstall :one
INSERT INTO game_installs (
    store_id, store_game_id, display_name, instance_id,
    canonical_game_id, install_root, metadata,
    last_seen_at, is_present,
    applied_profile_id, applied_at, applied_operation_id,
    created_at, updated_at
) VALUES (
    ?, ?, ?,
    ?, ?, ?,
    ?, ?, ?,
    NULL, NULL, NULL,
    ?, ?
) RETURNING id;

-- name: ImportInsertTarget :one
INSERT INTO targets (game_install_id, name, root_path, origin, metadata, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: ImportInsertModPage :one
INSERT INTO mod_pages (game_install_id, name, source_kind, source_url, source_ref,
                       nexus_game_domain, nexus_mod_id, notes, metadata, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: ImportInsertModFile :one
INSERT INTO mod_files (mod_page_id, label, is_primary, source_url, metadata, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: ImportInsertModFileVersion :one
INSERT INTO mod_file_versions (mod_file_id, archive_sha256, original_name,
                                version_string, nexus_file_id, uploaded_at,
                                inventory_scanned_at, upstream_notes, notes,
                                metadata, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: ImportInsertRemapConfig :one
INSERT INTO remap_configs (created_at, updated_at)
VALUES (?, ?)
RETURNING id;

-- name: ImportInsertRemapRule :exec
INSERT INTO remap_rules (remap_config_id, position, rule_type, int_value,
                         text_value, json_value, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: ImportInsertProfile :one
INSERT INTO profiles (game_install_id, name, description, is_active, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: ImportInsertProfileItem :one
INSERT INTO profile_items (profile_id, policy, mod_file_version_id,
                            enabled, priority, remap_config_id, notes,
                            created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: ImportInsertProfilePathPolicy :one
INSERT INTO profile_path_policies (profile_id, target_name, path_pattern,
                                    policy, metadata, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: ImportInsertBackup :one
INSERT INTO backups (game_install_id, target_id, relpath, backup_blob_sha256,
                     original_content_sha256, size_bytes, created_by_operation_id, created_at)
VALUES (?, ?, ?, ?, ?, ?, NULL, ?)
RETURNING id;

-- name: ImportInsertModIncompatibility :exec
INSERT INTO mod_incompatibilities (mod_page_id_a, mod_page_id_b, reason, source, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ImportInsertInventoryEntry :exec
INSERT INTO archive_inventory_entries (archive_sha256, raw_path, entry_type,
                                       size_bytes, link_target, content_sha256,
                                       position, parse_error, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ExportGetGameInstalls :many
SELECT * FROM game_installs;

-- name: ListAllModFileVersions :many
SELECT * FROM mod_file_versions;

-- name: ExportUnmarkInventoried :exec
UPDATE mod_file_versions SET inventory_scanned_at = NULL;

-- name: CompleteModFileVersionsByGameInstall :many
SELECT
    mfv.id,
    mp.name    AS mod_page_name,
    mf.label   AS file_label,
    mfv.version_string
FROM mod_file_versions mfv
JOIN mod_files mf ON mf.id = mfv.mod_file_id
JOIN mod_pages mp  ON mp.id = mf.mod_page_id
WHERE mp.game_install_id = ?
  AND (
    (lower(mp.name) LIKE lower(sqlc.arg(prefix)) ESCAPE '\')
    OR (lower(mf.label) LIKE lower(sqlc.arg(prefix)) ESCAPE '\')
  )
ORDER BY mp.name COLLATE NOCASE, mf.label COLLATE NOCASE, mfv.id DESC
LIMIT 20;

-- name: GetModFileVersionsByName :many
SELECT
    mfv.id,
    mp.name   AS mod_page_name,
    mf.label  AS file_label,
    mfv.version_string
FROM mod_file_versions mfv
JOIN mod_files mf ON mf.id = mfv.mod_file_id
JOIN mod_pages mp  ON mp.id = mf.mod_page_id
WHERE mp.game_install_id = ?
  AND lower(mp.name) = lower(sqlc.arg(name))
ORDER BY mf.label COLLATE NOCASE, mfv.id DESC;

-- name: GetModFileVersionByID :one
SELECT
    mfv.id,
    mp.name   AS mod_page_name,
    mf.label  AS file_label,
    mfv.version_string,
    mfv.inventory_scanned_at AS inventory_scanned_at,
    mfv.archive_sha256 AS archive_sha256
FROM mod_file_versions mfv
JOIN mod_files mf ON mf.id = mfv.mod_file_id
JOIN mod_pages mp  ON mp.id = mf.mod_page_id
WHERE mfv.id = ?
  AND mp.game_install_id = ?;

-- name: CompleteModPagesByGameInstall :many
SELECT
    mp.id,
    mp.name,
    mp.source_kind
FROM mod_pages mp
WHERE mp.game_install_id = ?
  AND (lower(mp.name) LIKE lower(sqlc.arg(prefix)) ESCAPE '\')
ORDER BY mp.name COLLATE NOCASE
LIMIT 20;

-- name: GetModPagesByName :many
SELECT
    mp.id,
    mp.name,
    mp.source_kind
FROM mod_pages mp
WHERE mp.game_install_id = ?
  AND lower(mp.name) = lower(sqlc.arg(name))
ORDER BY mp.id;

-- name: GetModPageByIDForGame :one
SELECT
    mp.id,
    mp.name,
    mp.source_kind
FROM mod_pages mp
WHERE mp.id = ?
  AND mp.game_install_id = ?;

-- name: UpdateModFileVersionVersionString :exec
UPDATE mod_file_versions
SET version_string = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: GetGameInstallsByName :many
SELECT * FROM game_installs
WHERE LOWER(display_name) = LOWER(?)
ORDER BY store_id, store_game_id, instance_id;

-- name: UpsertStore :exec
INSERT INTO stores (id, display_name, implementation, enabled)
VALUES (?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  display_name = excluded.display_name,
  implementation = excluded.implementation,
  enabled = excluded.enabled,
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now');

-- name: GetProfilesReferencingModPage :many
SELECT DISTINCT
    p.id AS profile_id,
    p.name AS profile_name,
    p.game_install_id
FROM profile_items pi
JOIN profiles p ON p.id = pi.profile_id
JOIN mod_file_versions mfv ON mfv.id = pi.mod_file_version_id
JOIN mod_files mf ON mf.id = mfv.mod_file_id
WHERE mf.mod_page_id = ?
ORDER BY p.name ASC;

-- name: CountModFileVersionsForFile :one
SELECT COUNT(*) AS count
FROM mod_file_versions
WHERE mod_file_id = ?;

-- name: GetModFileVersionWithParentIDs :one
SELECT
    mfv.id,
    mfv.mod_file_id,
    mf.mod_page_id,
    mf.label AS file_label,
    mp.name  AS mod_page_name,
    mp.game_install_id
FROM mod_file_versions mfv
JOIN mod_files mf ON mf.id = mfv.mod_file_id
JOIN mod_pages mp ON mp.id = mf.mod_page_id
WHERE mfv.id = ?;

-- name: DeleteModFileVersion :exec
DELETE FROM mod_file_versions WHERE id = ?;

-- name: DeleteModFile :exec
DELETE FROM mod_files WHERE id = ?;

-- name: DeleteModPage :exec
DELETE FROM mod_pages WHERE id = ?;

-- name: CompleteModFileVersionsByPageAndGameInstall :many
SELECT
    mfv.id,
    mf.label   AS file_label,
    mfv.version_string
FROM mod_file_versions mfv
JOIN mod_files mf ON mf.id = mfv.mod_file_id
JOIN mod_pages mp  ON mp.id = mf.mod_page_id
WHERE mp.game_install_id = ?
  AND mp.id = ?
  AND (
    (lower(mf.label) LIKE lower(sqlc.arg(prefix)) ESCAPE '\')
    OR (lower(mfv.version_string) LIKE lower(sqlc.arg(prefix)) ESCAPE '\')
  )
ORDER BY mf.label COLLATE NOCASE, mfv.id DESC
LIMIT 20;

-- name: DeleteAllInstalledFiles :exec
DELETE FROM installed_files;

-- name: DeleteAllBackups :exec
DELETE FROM backups;

-- name: DeleteAllOperations :exec
-- nb: this cascades to operation_changes
DELETE FROM operations;

-- name: ZeroAllGameInstallsState :exec
UPDATE game_installs
  SET applied_profile_id = NULL,
    applied_at = NULL,
    applied_operation_id = NULL;

-- name: GetGameInstallsWithAppliedProfile :many
SELECT * FROM game_installs
WHERE applied_profile_id IS NOT NULL;

-- name: GetInstalledFilesForGameInstall :many
SELECT * FROM installed_files
WHERE game_install_id = ?;

-- name: InsertOverride :one
INSERT INTO overrides (
    profile_id,
    target_id,
    relpath,
    blob_sha256,
    override_type,
    source_archive_sha256,
    source_raw_path,
    source_content_sha256,
    notes
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateOverride :one
UPDATE overrides
SET
    blob_sha256           = ?,
    source_archive_sha256 = ?,
    source_raw_path       = ?,
    source_content_sha256 = ?,
    notes                 = ?,
    updated_at            = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE profile_id = ?
  AND target_id  = ?
  AND relpath    = ?
RETURNING *;

-- name: UpsertOverride :one
INSERT INTO overrides (
    profile_id,
    target_id,
    relpath,
    blob_sha256,
    override_type,
    source_archive_sha256,
    source_raw_path,
    source_content_sha256,
    notes
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (profile_id, target_id, relpath) DO UPDATE SET
    blob_sha256           = excluded.blob_sha256,
    source_archive_sha256 = excluded.source_archive_sha256,
    source_raw_path       = excluded.source_raw_path,
    source_content_sha256 = excluded.source_content_sha256,
    notes                 = excluded.notes,
    updated_at            = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
RETURNING *;

-- name: GetOverride :one
SELECT * FROM overrides
WHERE profile_id = ?
  AND target_id  = ?
  AND relpath    = ?;

-- name: ListOverridesByProfile :many
SELECT * FROM overrides
WHERE profile_id = ?
ORDER BY relpath ASC;

-- name: DeleteOverride :exec
DELETE FROM overrides
WHERE profile_id = ?
  AND target_id  = ?
  AND relpath    = ?;

-- name: DeleteOverridesByProfile :exec
DELETE FROM overrides
WHERE profile_id = ?;

-- name: InsertOverridePatchEntry :one
INSERT INTO override_patch_entries (
    override_id,
    position,
    patch_type,
    entry_section,
    entry_key,
    entry_value
) VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListOverridePatchEntries :many
SELECT * FROM override_patch_entries
WHERE override_id = ?
ORDER BY position ASC;

-- name: DeleteOverridePatchEntry :exec
DELETE FROM override_patch_entries
WHERE override_id = ?
  AND position    = ?;

-- name: DeleteOverridePatchEntriesByOverride :exec
DELETE FROM override_patch_entries
WHERE override_id = ?;

-- name: GetMaxOverridePatchPosition :one
SELECT CAST(COALESCE(MAX(position), -1) AS integer) AS max_position
FROM override_patch_entries
WHERE override_id = ?;

-- name: GetStalenessHeuristicForProfile :many
-- Returns overrides that are potentially stale or have no base mod.
-- For each override with a non-null source anchor, checks whether the
-- highest-priority enabled mod providing source_raw_path still comes
-- from the same archive sha256.
WITH winning_base AS (
    SELECT
        o.id          AS override_id,
        o.relpath,
        o.source_archive_sha256,
        o.source_raw_path,
        (
            SELECT mfv.archive_sha256
            FROM profile_items pi
            JOIN mod_file_versions mfv ON mfv.id = pi.mod_file_version_id
            JOIN archive_inventory_entries aie
                ON aie.archive_sha256 = mfv.archive_sha256
            WHERE pi.profile_id = o.profile_id
              AND pi.enabled = TRUE
              AND aie.raw_path = o.source_raw_path
              AND aie.entry_type = 'file'
            ORDER BY pi.priority DESC
            LIMIT 1
        ) AS current_archive_sha256
    FROM overrides o
    WHERE o.profile_id = ?1
),
classified AS (
    SELECT
        override_id,
        relpath,
        source_archive_sha256,
        source_raw_path,
        current_archive_sha256,
        CAST(COALESCE(CASE
            WHEN source_archive_sha256 IS NULL AND current_archive_sha256 IS NOT NULL
                THEN 'anchor_lost'
            WHEN source_archive_sha256 IS NOT NULL AND current_archive_sha256 IS NULL
                THEN 'no_base'
            WHEN source_archive_sha256 IS NOT NULL
                AND current_archive_sha256 != source_archive_sha256
                THEN 'stale'
        END, '') AS TEXT) AS staleness
    FROM winning_base
)
SELECT
    override_id,
    relpath,
    source_archive_sha256,
    source_raw_path,
    current_archive_sha256,
    staleness
FROM classified
WHERE staleness != '';

-- name: ListOverridesForApply :many
-- Fetches all overrides for a profile with blob info needed by the apply pipeline.
SELECT
    o.id,
    o.target_id,
    o.relpath,
    o.override_type,
    o.blob_sha256,
    o.source_archive_sha256,
    o.source_raw_path,
    b.size_bytes
FROM overrides o
LEFT JOIN blobs b ON b.sha256 = o.blob_sha256
WHERE o.profile_id = ?
ORDER BY o.relpath ASC;

-- name: CopyOverridesToProfile :exec
-- Copies all overrides from src_profile_id into dst_profile_id.
-- Caller is responsible for ensuring target_ids are valid for dst profile's
-- game install. Conflicts on (profile_id, target_id, relpath) are replaced.
INSERT INTO overrides (
    profile_id,
    target_id,
    relpath,
    blob_sha256,
    override_type,
    source_archive_sha256,
    source_raw_path,
    source_content_sha256,
    notes
)
SELECT
    ?2,
    src.target_id,
    src.relpath,
    src.blob_sha256,
    src.override_type,
    src.source_archive_sha256,
    src.source_raw_path,
    src.source_content_sha256,
    src.notes
FROM overrides src
WHERE src.profile_id = ?1
ON CONFLICT (profile_id, target_id, relpath) DO UPDATE SET
    blob_sha256           = excluded.blob_sha256,
    override_type         = excluded.override_type,
    source_archive_sha256 = excluded.source_archive_sha256,
    source_raw_path       = excluded.source_raw_path,
    source_content_sha256 = excluded.source_content_sha256,
    notes                 = excluded.notes,
    updated_at            = strftime('%Y-%m-%dT%H:%M:%fZ', 'now');

-- name: CopyOverridePatchEntriesToProfile :exec
-- Copies patch entries for all overrides copied from src to dst profile.
-- Must be called after CopyOverridesToProfile.
INSERT INTO override_patch_entries (
    override_id,
    position,
    patch_type,
    entry_section,
    entry_key,
    entry_value
)
SELECT
    dst.id,
    src_entries.position,
    src_entries.patch_type,
    src_entries.entry_section,
    src_entries.entry_key,
    src_entries.entry_value
FROM override_patch_entries src_entries
JOIN overrides src_o ON src_o.id = src_entries.override_id
JOIN overrides dst   ON dst.profile_id = ?2
                     AND dst.target_id  = src_o.target_id
                     AND dst.relpath    = src_o.relpath
WHERE src_o.profile_id = ?1
ON CONFLICT (override_id, position) DO UPDATE SET
    patch_type    = excluded.patch_type,
    entry_section = excluded.entry_section,
    entry_key     = excluded.entry_key,
    entry_value   = excluded.entry_value;

-- name: ListUnreferencedOverrideBlobs :many
-- Returns override blobs with no referencing overrides row.
SELECT b.sha256, b.size_bytes, b.original_name
FROM blobs b
WHERE b.kind = 'override'
  AND NOT EXISTS (
      SELECT 1 FROM overrides o WHERE o.blob_sha256 = b.sha256
  );

-- name: CountOverridesByProfile :one
SELECT COUNT(*) FROM overrides WHERE profile_id = ?;

-- name: GetCurrentWinnerForPath :one
-- Returns the archive and inventory details for the highest-priority
-- enabled mod in the profile that provides the given raw path.
-- Used to capture the source anchor when creating an override.
SELECT
    mfv.archive_sha256,
    aie.raw_path,
    aie.content_sha256
FROM profile_items pi
JOIN mod_file_versions mfv ON mfv.id = pi.mod_file_version_id
JOIN archive_inventory_entries aie
    ON aie.archive_sha256 = mfv.archive_sha256
WHERE pi.profile_id = ?1
  AND pi.enabled = TRUE
  AND aie.raw_path = sqlc.arg(raw_path)
  AND aie.entry_type = 'file'
ORDER BY pi.priority DESC
LIMIT 1;

-- name: GetOverrideStatusDetail :many
-- Full staleness detail for all overrides in a profile.
-- Returns all overrides with their current base mod info for display.
-- The redundant state isn't computed here": detecting it would require
-- comparing the override blob content against the base file content, which
-- means reading from the blob store at query time, which we obviously can't do.
-- That state would need to be computed in Go after fetching the rows, if we
-- want to support it. For now base_unchanged is the closest we get from the query alone.
WITH winning_base AS (
    SELECT
        o.id                    AS override_id,
        o.relpath,
        o.override_type,
        o.blob_sha256,
        o.source_archive_sha256,
        o.source_raw_path,
        o.source_content_sha256,
        o.notes,
        o.updated_at,
        CAST((
            SELECT mfv.archive_sha256
            FROM profile_items pi
            JOIN mod_file_versions mfv ON mfv.id = pi.mod_file_version_id
            JOIN archive_inventory_entries aie
                ON aie.archive_sha256 = mfv.archive_sha256
            WHERE pi.profile_id = o.profile_id
              AND pi.enabled = TRUE
              AND aie.raw_path = o.source_raw_path
              AND aie.entry_type = 'file'
            ORDER BY pi.priority DESC
            LIMIT 1
        ) AS TEXT) AS current_archive_sha256,
        CAST((
            SELECT aie.content_sha256
            FROM profile_items pi
            JOIN mod_file_versions mfv ON mfv.id = pi.mod_file_version_id
            JOIN archive_inventory_entries aie
                ON aie.archive_sha256 = mfv.archive_sha256
            WHERE pi.profile_id = o.profile_id
              AND pi.enabled = TRUE
              AND aie.raw_path = o.source_raw_path
              AND aie.entry_type = 'file'
            ORDER BY pi.priority DESC
            LIMIT 1
        ) AS TEXT) AS current_content_sha256
    FROM overrides o
    WHERE o.profile_id = ?1
)
SELECT
    override_id,
    relpath,
    override_type,
    blob_sha256,
    source_archive_sha256,
    source_raw_path,
    source_content_sha256,
    current_archive_sha256,
    current_content_sha256,
    notes,
    updated_at,
    CAST(COALESCE(CASE
        WHEN source_archive_sha256 IS NULL AND current_archive_sha256 IS NULL
            THEN 'net_new_no_anchor'
        WHEN source_archive_sha256 IS NULL AND current_archive_sha256 IS NOT NULL
            THEN 'anchor_lost'
        WHEN source_archive_sha256 IS NOT NULL AND current_archive_sha256 IS NULL
            THEN 'no_base'
        WHEN current_archive_sha256 != source_archive_sha256
            AND (current_content_sha256 IS NULL OR current_content_sha256 != source_content_sha256)
            THEN 'stale'
        WHEN current_archive_sha256 != source_archive_sha256
            AND current_content_sha256 = source_content_sha256
            THEN 'base_unchanged'
        WHEN current_archive_sha256 = source_archive_sha256
            THEN 'base_unchanged'
    END, 'unknown') AS TEXT) AS staleness_state
FROM winning_base
ORDER BY relpath ASC;

-- name: ExportGetOverridesForGameInstall :many
SELECT o.* FROM overrides o
JOIN profiles p ON p.id = o.profile_id
WHERE p.game_install_id = ?
ORDER BY o.id ASC;

-- name: ExportInsertOverride :exec
INSERT INTO overrides (
    id, profile_id, target_id, relpath, blob_sha256, override_type,
    source_archive_sha256, source_raw_path, source_content_sha256,
    notes, created_at, updated_at
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
);

-- name: ExportGetPatchEntriesForOverride :many
SELECT * FROM override_patch_entries
WHERE override_id = ?
ORDER BY position ASC;

-- name: ExportInsertPatchEntry :exec
INSERT INTO override_patch_entries (
    id, override_id, position, patch_type,
    entry_section, entry_key, entry_value
) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListOverrideBlobsForGameInstall :many
-- Returns override blobs referenced by the given game install's profiles.
SELECT DISTINCT b.* FROM blobs b
JOIN overrides o ON o.blob_sha256 = b.sha256
JOIN profiles p ON p.id = o.profile_id
WHERE p.game_install_id = ?
  AND b.kind = 'override';

-- name: ImportInsertOverride :one
INSERT INTO overrides (
    profile_id, target_id, relpath, blob_sha256, override_type,
    source_archive_sha256, source_raw_path, source_content_sha256,
    notes, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: ImportInsertPatchEntry :exec
INSERT INTO override_patch_entries (
    override_id, position, patch_type,
    entry_section, entry_key, entry_value
) VALUES (?, ?, ?, ?, ?, ?);

-- name: GetOverridePatchEntryByKey :one
-- Find an existing patch entry by override, key, and section (for upsert logic).
SELECT * FROM override_patch_entries
WHERE override_id    = ?1
  AND entry_key      = ?2
  AND (entry_section = ?3 OR (entry_section IS NULL AND ?3 IS NULL));

-- name: UpdateOverridePatchEntryValue :exec
UPDATE override_patch_entries
SET entry_value = ?
WHERE id = ?;

-- name: UpdateOverridePatchEntryTypeAndValue :exec
UPDATE override_patch_entries
SET patch_type  = ?,
    entry_value = ?
WHERE id = ?;

-- name: MoveModFileVersions :exec
UPDATE mod_file_versions
SET mod_file_id = sqlc.arg(new_mod_file_id)
WHERE mod_file_id = sqlc.arg(old_mod_file_id);

-- name: ClearModFilePrimary :exec
UPDATE mod_files
SET is_primary = 0, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: SetModFilePrimary :exec
UPDATE mod_files
SET is_primary = 1, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: GetInventoryParseErrorsForArchive :many
SELECT
    id,
    archive_sha256,
    raw_path,
    position,
    parse_error
FROM archive_inventory_entries
WHERE archive_sha256 = ?
  AND parse_error IS NOT NULL
ORDER BY position ASC;

-- name: GetProfileItems :many
SELECT
    pi.mod_file_version_id,
    pi.priority,
    pi.enabled,
    pi.remap_config_id,
    mp.name    AS mod_page_name,
    mf.label   AS file_label,
    mfv.version_string
FROM profile_items pi
JOIN mod_file_versions mfv ON mfv.id = pi.mod_file_version_id
JOIN mod_files mf ON mf.id = mfv.mod_file_id
JOIN mod_pages mp ON mp.id = mf.mod_page_id
WHERE pi.profile_id = ?
ORDER BY pi.priority DESC;

-- name: GetProfileItemsForModPage :many
SELECT
    pi.id,
    pi.mod_file_version_id,
    pi.priority,
    pi.enabled,
    pi.remap_config_id,
    mfv.mod_file_id,
    mf.label  AS file_label,
    mp.name   AS mod_page_name
FROM profile_items pi
JOIN mod_file_versions mfv ON mfv.id = pi.mod_file_version_id
JOIN mod_files mf ON mf.id = mfv.mod_file_id
JOIN mod_pages mp ON mp.id = mf.mod_page_id
WHERE pi.profile_id = ?
  AND mp.id = ?;

-- name: GetLatestUnusedModFileVersion :one
SELECT
    mfv.id,
    mfv.version_string,
    mfv.original_name
FROM mod_file_versions mfv
WHERE mfv.mod_file_id = ?
  AND mfv.id NOT IN (
    SELECT pi.mod_file_version_id
    FROM profile_items pi
    WHERE pi.profile_id = ?
  )
ORDER BY mfv.created_at DESC
LIMIT 1;

-- name: UpdateProfileItemModFileVersion :exec
UPDATE profile_items
SET mod_file_version_id = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?;

-- name: GetModFileVersionByIDForUpgrade :one
SELECT * FROM mod_file_versions WHERE id = ? LIMIT 1;
