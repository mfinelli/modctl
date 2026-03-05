-- name: ReadSchemaVersion :one
SELECT value FROM cache_meta WHERE key = 'schema_version' LIMIT 1;

-- name: UpsertNexusModInfo :exec
INSERT INTO nexus_mod_info (
    nexus_game_domain,
    nexus_mod_id,
    fetched_at,
    name,
    summary,
    author,
    is_available,
    raw_json
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?
)
ON CONFLICT (nexus_game_domain, nexus_mod_id) DO UPDATE SET
    fetched_at   = excluded.fetched_at,
    name         = excluded.name,
    summary      = excluded.summary,
    author       = excluded.author,
    is_available = excluded.is_available,
    raw_json     = excluded.raw_json;

-- name: UpsertNexusFileInfo :exec
INSERT INTO nexus_file_info (
    nexus_game_domain,
    nexus_mod_id,
    nexus_file_id,
    fetched_at,
    name,
    version,
    category_name,
    is_primary,
    file_name,
    size_in_bytes,
    uploaded_timestamp,
    raw_json
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
ON CONFLICT (nexus_game_domain, nexus_mod_id, nexus_file_id) DO UPDATE SET
    fetched_at         = excluded.fetched_at,
    name               = excluded.name,
    version            = excluded.version,
    category_name      = excluded.category_name,
    is_primary         = excluded.is_primary,
    file_name          = excluded.file_name,
    size_in_bytes      = excluded.size_in_bytes,
    uploaded_timestamp = excluded.uploaded_timestamp,
    raw_json           = excluded.raw_json;

-- name: UpsertNexusFileUpdate :exec
INSERT INTO nexus_file_updates (
    nexus_game_domain,
    nexus_mod_id,
    old_file_id,
    new_file_id,
    uploaded_timestamp,
    fetched_at
) VALUES (
    ?, ?, ?, ?, ?, ?
)
ON CONFLICT (nexus_game_domain, nexus_mod_id, old_file_id) DO UPDATE SET
    new_file_id        = excluded.new_file_id,
    uploaded_timestamp = excluded.uploaded_timestamp,
    fetched_at         = excluded.fetched_at;

-- name: DeleteNexusFileInfoForMod :exec
DELETE FROM nexus_file_info
WHERE nexus_game_domain = ? AND nexus_mod_id = ?;

-- name: DeleteNexusFileUpdatesForMod :exec
DELETE FROM nexus_file_updates
WHERE nexus_game_domain = ? AND nexus_mod_id = ?;

-- name: GetNexusModInfo :one
SELECT * FROM nexus_mod_info
WHERE nexus_game_domain = ? AND nexus_mod_id = ?;

-- name: GetNexusFileInfoForMod :many
SELECT * FROM nexus_file_info
WHERE nexus_game_domain = ? AND nexus_mod_id = ?;

-- name: GetNexusFileUpdatesForMod :many
SELECT * FROM nexus_file_updates
WHERE nexus_game_domain = ? AND nexus_mod_id = ?;

-- name: GetNexusFileInfo :one
-- TODO: think about making this use IN() to avoid needing to run a query
-- per mod, but unless you have 100s of mods installed it's probably fine
SELECT
    version,
    fetched_at
FROM nexus_file_info
WHERE nexus_game_domain = ?
AND nexus_mod_id = ?
AND nexus_file_id = ?;

-- name: GetNexusFileUpdateChain :many
SELECT old_file_id, new_file_id
FROM nexus_file_updates
WHERE nexus_game_domain = ?
AND nexus_mod_id = ?;

-- name: GetNexusFileInfoFetchedAt :one
-- We use LIMIT 1 since all rows for a mod page share the same fetched_at from
-- the atomic write in cacheModFiles
SELECT fetched_at
FROM nexus_file_info
WHERE nexus_game_domain = ?
AND nexus_mod_id = ?
LIMIT 1;
