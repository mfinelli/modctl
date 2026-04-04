# Export bundle format

A modctl export bundle is a zstd-compressed tar archive. This page describes
what is inside it, which is useful if you want to inspect a bundle manually
or understand what is and is not included in an export.

You do not need to understand the bundle format to use `modctl export` and
`modctl import`. See [Import & Export](../../import-export) for usage
instructions.

## Structure

A bundle contains the following files:
```
manifest.json
modctl.db
nexus_cache.db
archives/<xx>/<sha256>
backups/<xx>/<sha256>
overrides/<xx>/<sha256>
```

`manifest.json` contains metadata about the bundle. `modctl.db` is a snapshot
of the modctl database at the time of export. `nexus_cache.db` is a snapshot
of the Nexus API cache, included when it was present on disk at export time.
The `archives/`, `backups/`, and `overrides/` directories mirror the layout of
the local blob stores, containing only the blobs referenced by the exported
data.

Note that game-scoped bundles never include a `backups/` directory. Backup
blobs describe on-disk state on the source machine and have no meaning on the
destination, so they are excluded from game-scoped exports.

## Manifest

`manifest.json` contains:

- `export_format_version`: an integer used by import to handle future format
  changes. The current version is `1`.
- `export_kind`: either `full` or `game`, indicating whether this is a full
  export or a game-scoped export.
- `exported_at`: the timestamp when the bundle was produced.
- `modctl_version`: the version of modctl that produced the bundle.
- `schema_version`: the database schema version of the snapshot.
- `db_sha256`: the SHA-256 hash of `modctl.db` as it appears in the bundle,
  used to verify integrity on import.
- `nexus_cache_sha256`: the SHA-256 hash of `nexus_cache.db` as it appears in
  the bundle. Absent or empty if the Nexus cache was not present on disk at
  export time.
- `counts`: the number of archive and backup blobs included.
- `game`: for game-scoped bundles only: the store ID, store game ID, and
  display name of the exported game.

## Database snapshot

For full exports the database snapshot is produced using SQLite's `VACUUM
INTO`, which creates a clean, consistent copy without requiring the database
to be offline.

For game-scoped exports a fresh database is created and populated with only
the rows relevant to the exported game. No other games' data is included.
Backup records are excluded along with their blobs.

The applied profile state is intentionally cleared in game-scoped bundles
since the destination machine will have its own game installation. Operation
history is also not included in game-scoped bundles.

## Nexus cache snapshot

The Nexus API cache is included in bundles when it was present on disk at
export time. It contains cached mod page metadata, file info, and update chain
data, which is what modctl uses to detect available updates without making
additional API calls.

For full exports the cache snapshot is produced using `VACUUM INTO`, mirroring
the approach used for the main database.

For game-scoped exports a fresh cache database is created and populated with
only the rows relevant to the exported game's mod pages. No other games' cache
data is included.

On full import the cache database is copied into place directly. On
game-scoped import the rows are merged into the existing cache database using
`INSERT OR REPLACE`, so cache data for other games already on the destination
machine is not affected.

The `fetched_at` timestamps in the cache are preserved verbatim. The TTL logic
in `modctl mods nexus check-updates` will naturally treat stale entries as cold
and refresh them on the next run.

If `nexus_cache_sha256` is absent or empty in the manifest, the cache was not
included in the bundle. This can happen with bundles produced by older versions
of modctl, or when the cache database was not present on disk at export time.
In both cases import silently skips the cache restore step.

## Blob verification

Before writing a bundle, modctl hashes every blob and compares it against its
stored SHA-256. If any mismatch is detected the export is aborted. This means
a successfully produced bundle is guaranteed to have consistent, verified
blobs at the time it was created.

If a blob is missing from disk at export time a warning is printed and the
blob is omitted. The bundle is not aborted; run `modctl doctor` before
exporting to identify any missing blobs ahead of time.

## Integrity checking

The `db_sha256` field in the manifest covers the database snapshot and is
verified on import before anything is written to disk. Blob files are verified
by hashing their content against their filename, which is their SHA-256 hash,
before ingestion.

Use `modctl verify` to check a bundle's integrity without importing it. See
[Utility commands](../../commands/utility) for details.

## Format versioning

The `export_format_version` field allows the bundle format to evolve without
silent data corruption. Import checks this field first and refuses bundles
with an unrecognised version. This means an older version of modctl will
always fail explicitly rather than silently misinterpret a bundle produced
by a newer version.
