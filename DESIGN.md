# mod control design document

This file captures the initial planning and design to make sure that we don't
lose anything.

## 1. Overview

A Linux CLI mod manager that performs deterministic deployment of archive
contents into game install roots. It supports multiple "stores" (Steam first,
then Heroic/Lutris/GOG) and uses profiles to define mod sets with per-profile
priority ordering. It tracks installed files and creates backups of
pre-existing non-tool-owned files to allow safe rollback and profile switching.
It stores metadata in SQLite and binary artifacts in on-disk content-addressed
stores.

### Primary goals

- Deterministic installs/uninstalls with safe rollback.
- Steam-aware game discovery (no user-managed install paths/appids).
- Profile-based mod sets with per-profile priority ordering.
- Safety: prevent path traversal and destructive uninstall behavior.
- Portable state via export/import bundle.

### Non-goals for v1

- No `nxm://` protocol handler.
- No downloading from Nexus or other mod sources (user downloads manually).
- No dependency resolution.
- No virtual filesystem.
- No in-process extraction (external bsdtar is required).
- No game-specific integrations beyond "generic extract-to-game-dir".

## 2. Core concepts

### Store

A source of game installations (e.g., Steam, Heroic, Lutris, future GOG). A
store integration is responsible for:
- discovering installed games
- providing store-specific identifiers
- resolving install roots/targets

Stores are **first-class** even if only Steam is implemented in v1.

### Game

Represents a Steam game installation:
- Steam appid
- name
- install directory
- (future) Proton prefix directory
- integration type (default `generic`)

#### Game vs. Game Install

Separate the idea of a "game identity" from an "install instance":
- **Game**: logical entry (name, maybe canonical ids)
- **GameInstall** : a concrete installation associated with a store, e.g.
  - Steam appid 1091500 installed in library X
  - Heroic game "cyberpunk2077" installed under some prefix

This allows:
- multiple stores
- multiple installs of same game (rare, but possible)

#### Discovery State

`GameInstall` tracks soft-delete/staleness state from store refresh:
  - `is_present` - set to `FALSE` when a game is not found during refresh;
    the install is never deleted so that profile and mod data is preserved
  - `last_seen_at` - timestamp of the last refresh that observed this install
  - `display_name` - human-facing name as reported by the store; stored on
    `GameInstall` rather than derived at query time

The decision not to hard-delete missing installs is intentional: a game
temporarily moved to a different drive or library should not lose its profiles
and mod configuration.

### Target

A named install root within a `GameInstall`. v1 supports:
- `game_dir` only

Future targets:
- `proton_prefix`
- `documents`, `appdata`, etc.

Track installed files as `(game_install_id, target_id, relpath)` so we can
extend beyond game directory later.

### Mods model

#### "Mod Page" vs "Mod File" vs "Mod File Version"

Here are the specific changes:

Section 2 - Mods model, update the "Mod Page" vs "Mod File" vs "Mod File Version" subsection:
markdown

#### "Mod Page" vs "Mod File" vs "Mod File Version"

Model it like Nexus does:
- **ModPage** (a mod "project")
  - Source: local/manual or Nexus
  - If Nexus: `nexus.mod_id`, maybe `nexus.game_domain`/slug
  - Human name, notes, tags
- **ModFile** (a downloadable file under a mod page)
  - If Nexus: `nexus.file_id` is NOT stored here - it belongs to ModFileVersion
  - Human label (e.g., "Main File", "Optional - 2K Textures", "Update",
    "Patch")
  - `is_primary` flag identifies the main file
- **ModFileVersion** (a specific archive blob)
  - archive blob hash
  - extracted inventory cache (optional)
  - observed version metadata (if available)
  - `nexus_file_id` (optional) - Nexus file IDs identify a specific uploaded
    archive, not a logical file slot. A new upload of the same logical file
    gets a new file_id. The Nexus `file_updates` chain links old to new
    file_ids and is used for update detection.
  - imported_at, original filename

Profiles should enable **ModFile** (with a chosen version policy) or directly
pin a **ModFileVersion**:
- v1 simplest: pin a ModFileVersion
- later: allow "track latest" policies if user provides API key

This cleanly distinguishes "multiple archives under one Nexus mod".

### Tracking vs. Desired State

`installed_files` is the authoritative record of what the tool has written
to disk. Profiles describe desired state only.

Removing or disabling a mod version from a profile changes desired state but
does not untrack anything. Files written by a previous apply remain recorded
in `installed_files` until an apply reconciles the difference. This means
the tool can always safely clean up, even if the profile that produced the
current install has been modified or deleted.

"Pending changes" (desired state diverges from installed state) is derived
at query time by the planner and surfaced by `status`. No additional schema
column tracks this.

The `installed_files` `owner_profile_id` is the profile that last applied this
file, not an exclusive ownership claim. If the same mod file version appears in
multiple profiles, owner_profile_id will reflect whichever profile most
recently ran a successful apply.

### Profile

A named set of enabled mod versions for a `GameInstall`, with:
- set of enabled/disabled mod file versions (or mod files with pinned version
- per-profile priority order (higher priority wins conflicts)
- remap rules per mod (possibly per version)
- (future) per-path merge policy or override rules

Exactly one profile can be active/applied at a time per `GameInstall`.

### Applied State

`GameInstall` tracks the currently-applied profile as denormalized state:
- `applied_profile_id` - the profile whose file set is currently on disk
- `applied_at` - timestamp of the last successful apply
- `applied_operation_id` - the operation that produced the current on-disk state

All three fields are NULL until a real apply has been performed. They are
never set during game refresh or profile creation. On unapply, all three
fields are set back to NULL.

This is intentionally denormalized for fast status queries without joining
through operations. It is updated atomically at the end of a successful
`apply` or `unapply`.

A trigger enforces that `applied_profile_id` refers to a profile belonging
to the same `game_install_id`. This is enforced on UPDATE only, since the
fields are always NULL at insert time.

### Plan

A computed desired state: the union of enabled mods in a profile with conflicts
resolved by priority.

Outputs:
- winner for each destination path
- list of file ops: write/overwrite/remove
- list of required backups

### Operation

A logged apply/switch/unapply run:
- used for auditing, crash recovery, and debugging.

## 3. Storage model

### Metadata: SQLite

SQLite stores:
- stores, game installs, targets
- mode pages, mode files, mod file versions
- profiles and their enabled mod file versions + priority
- remap configurations
- file manifests (planned + installed)
- installed file hashes and ownership
- backup mappings
- operation journal/logs
- override/merge policy data structures (even if unused in v1)
- blob references

Version schema from day 1.

### Nexus cache: separate SQLite in XDG cache

A separate SQLite database at `$XDG_CACHE_HOME/modctl/nexus_cache.db` stores
cached Nexus API responses. This is intentionally separate from the main DB:
- It is safe to delete (will be repopulated on next `mods nexus check-updates`)
- It is excluded from export/import bundles
- It uses a simple internal version number; if the schema version does not
  match the expected version the cache is blown away and recreated

### Blob stores: on-disk, content-addressed

Two separate stores:
- `archives/` for imported mod archives
- `backups/` for backed-up pre-existing files

No per-game partitioning; per-game accounting derived from references. Blobs
are keyed by sha256 and immutable.

Layout with directory fanout:
- `archives/<fan2>/<fullhash>`
- `backups/<fan2>/<fullhash>`

Where `<fan2>` is the first two characters of the sha256 hex digest
(e.g. `archives/ab/abcdef1234...`). Note: there is no intermediate `sha256/`
directory level.

The blob store directories are purely blob content. Files placed there
manually are unsupported and may be silently removed by `gc`.

Rationale:
- simplicity (one storage mode)
- dedupe
- filesystem-friendly backups
- clean GC

### Export/import bundle

A single file (tar + zstd) containing:
- `manifest.json` - bundle metadata (see below)
- `modctl.db` - database snapshot
- `archives/<fan2>/<fullhash>` - referenced archive blobs
- `backups/<fan2>/<fullhash>` - referenced backup blobs

The bundle is compressed using zstd at the default compression level.

Import verifies integrity and schema compatibility.

#### Manifest

`manifest.json` contains:
- `export_format_version`: integer, currently `1`. Used by import to handle
  future format changes.
- `export_kind`: `"full"` or `"game"`
- `exported_at`: ISO 8601 timestamp
- `modctl_version`: version of modctl that produced the bundle
- `schema_version`: goose migration version of the database snapshot
- `db_sha256`: sha256 of `modctl.db` as it appears in the bundle, used
  to verify integrity on import
- `counts`: `{ "archives": N, "backups": N }`
- `game` (game-scoped only): `{ "store_id", "store_game_id", "display_name" }`

#### Full export

Includes the complete database snapshot (via `VACUUM INTO`) and all blobs
of all kinds. Suitable for full machine migration or complete backup.

#### Game-scoped export

Includes only data relevant to a single game install:
- The store row for that game's store
- The game install, targets, profiles, mod pages, mod files, mod file
  versions, remap configs and rules, profile items, profile path policies,
  backups, and mod incompatibilities for that game
- Only blobs referenced by that game's mod file versions and backups
- Archive inventory entries (unless `--skip-inventory` is passed)
- `applied_profile_id`, `applied_at`, and `applied_operation_id` are
  nulled out - disk state is not valid on the destination machine
- Operation history is not included

The game-scoped database is constructed as a fresh SQLite file with
migrations applied, then populated with only the relevant rows. This
ensures no other games' data leaks into the bundle.

If any blob is missing from disk at export time, a warning is printed and
the blob is skipped. The bundle will be incomplete; `doctor` can identify
missing blobs before exporting.

## 4. Archive Inventory

### Purpose

The archive inventory caches the contents of mod archives in the database so
that conflict planning and status checks can operate without reading archives
from disk. It is the authoritative record of what files a mod version provides,
before any remap rules are applied.

### Storage

`archive_inventory_entries` stores one row per entry in an archive:
- Keyed on `(archive_sha256, position)` - position is the zero-based index
  of the entry in the `bsdtar -tvvf` listing and is the canonical key since
  archives may contain duplicate paths (last entry wins during extraction,
  matching bsdtar behavior)
- `raw_path` stores the path exactly as it appears in the archive with no
  normalization; path validation and remap rule application are deferred to
  the planner
- `entry_type` is one of `file`, `dir`, `symlink`, `other` - derived from
  the first character of the bsdtar permission string
- `parse_error` is non-null when a line could not be fully parsed; the entry
  is still recorded with whatever fields were extractable so the inventory
  is a faithful mirror of the archive
- `raw_path` is nullable only when `parse_error` is non-null (enforced by
  CHECK constraint); a fully parsed entry always has a non-empty path
- `content_sha256` stores the sha256 of the entry's extracted content. It is
  NULL until the file is first deployed by apply, at which point it is
  populated from the hash computed during staging. It is never overwritten
  once set.

`mod_file_versions` has an `inventory_scanned_at` timestamp that is NULL
until the archive has been scanned. Since the inventory is keyed on
`archive_sha256` (not `mod_file_version_id`), scanning updates all
`mod_file_versions` rows that share the same `archive_sha256` in a single
pass - not just the version that triggered the scan.

### Scanning

Scanning uses `bsdtar -tvvf <archive>` via a subprocess. libarchive
normalises the listing format across zip, rar, 7z, tar.gz and other archive
types so the parser is format-agnostic. The parser faithfully records all
entries including dangerous paths (traversal, absolute paths, symlinks) -
rejection of unsafe entries is deferred to the planner.

The `archivescanner` package provides two functions:
- `ScanOne` - scans a single archive by sha256; no-op if already scanned.
  Used by `mods import` to scan the just-imported archive only, avoiding
  the surprising behavior of scanning unrelated archives as a side effect.
- `ScanAll` - scans all unscanned archives by iterating `ScanOne`; used
  by `mods scan-inventory`. Each archive is committed in its own
  transaction so progress is saved as we go.

### Import behavior

`mods import` scans the imported archive by default immediately after the
import transaction commits, before the `--rm` cleanup step. The `--skip-inventory`
flag opts out for users importing many archives in bulk. Archives imported
with `--skip-inventory` can be backfilled with `mods scan-inventory`.

### bsdtar output format

`bsdtar -tvvf` produces one line per entry in `ls -l` style:

    <perms> <links> <uid> <gid> <size> <month> <day> <time|year> <path>

Field notes:
- uid/gid may be numeric or named depending on archive type and platform
- A summary trailer is always printed as the final line:
  `Archive Format: <format>,  Compression: <compression>`
  This line is skipped by the parser and does not produce an entry.
- Verified against: tar.gz, zip, 7z, RAR

## 5. Extraction model

Directory entries in archives are ignored during apply. Directories are
created implicitly as parent paths when files are written to disk. Explicit
empty directory installation is not supported in v1. If a mod requires an
empty directory to exist, the recommended workaround is to place a
placeholder file at that path using the overrides system.

### v1 extraction: external `bsdtar`

- Inventory: `bsdtar -t` to list entries (best-effort metadata).
- Apply: extract to staging dir; never directly to the game directory.

### In-process extraction (unlikely future)

Possible future backends:
- pure-Go zip/tar
- libarchive via CGO
- fallback to bsdtar/7z

To keep this option open, extraction is an interface with multiple backends.

### Staging directory

All extraction uses a per-archive subdirectory under the configured `tmp_dir`:
```
<tmp_dir>/staging/<archive_sha256>/
```

The entire archive is extracted in a single `bsdtar -x` pass. Only winning
files are moved into the target directory; losers are discarded when staging
is cleaned up. Staging is cleared at the start of each apply run and removed
on success unless `--keep-staging` is set.

The default `tmp_dir` is under `$XDG_RUNTIME_DIR`. This is intentional since
`$XDG_RUNTIME_DIR` is typically on tmpfs (fast, no persistent writes) and is
cleaned up on logout. Users with large mod archives or small tmpfs allocations
can override `tmp_dir` in config to point elsewhere.

## 6. Safety model

Only entries with `entry_type = 'file'` are considered during planning.
Directory, symlink, and other entry types are filtered out before remap
rules are applied. Symlink and special file support may be added in a
future version behind an explicit opt-in flag.

### Staging + safe move

All extraction goes to staging, then the tool:
- validates destination paths
- rejects traversal and absolute paths
- enforces "within target root"
- applies remap rules deterministically
- moves files into place

### Drift detection on overwrite

During apply, before overwriting any tool-owned file the planner hashes the
on-disk content and compares it against `installed_files.content_sha256`. This
detects external modifications (e.g. a game update overwriting a modded file)
and handles them as follows:

- Same owner, content drifted -> back up current on-disk content before
  overwriting, preserving the updated game file in the backup store
- Different owner, any content -> plain overwrite (tool owns the file, no
  backup needed)
- Same owner, content matches -> noop, file is already correct, skip entirely

Noop ops are never shown to the user but are logged at debug level.
Use `--no-recheck` to skip hash checks for faster applies at the cost of
not detecting external modifications.

### Symlinks and special files

Default v1 policy:
- reject symlinks/hardlinks/special device files
- require explicit override flags in future if supported

### Limits

Configurable safety limits:
- max number of files per operation
- max total extracted size
- max path length / nesting depth
- optional "denylist" patterns

### Uninstall safety

During remove ops, the file is deleted from disk and its `installed_files`
row is removed. The on-disk hash is computed before deletion and recorded
in `operation_changes.old_content_sha256` for auditing. Hash verification
before deletion (to detect external modifications) is not performed by
default - use `apply --recheck` to detect drift before applying.

Never blindly delete:
- Only delete a file if its hash matches what the tool installed (unless
  `--force`).
- If changed externally, mark "drifted" and require explicit action.

## 7. Conflict and priority rules

### Winner selection

For each destination path:
- winner = enabled mod with highest priority that provides that path

### Apply semantics

Apply reconciles filesystem to profile state:
- write/overwrite winners (extracting from staging)
- remove files that are no longer winners and are tool-owned
- restore backups when a previously overwritten non-tool-owned file has no
  new winner
- promote the highest-priority loser when the current winner is removed from
  the profile but other mods still provide the same path

When a pre-existing non-tool-owned file would be overwritten, it is backed
up to the backup blob store before being replaced. Backups are restored
automatically during unapply or when no mod claims the path.

Apply detects four filesystem states for each planned path:
1. Tool-owned and present on disk -> overwrite, no backup needed
2. Tool-owned but missing from disk -> drift warning, treat as fresh write
3. Not tool-owned but present on disk -> back up then write
4. Not tool-owned and not present on disk -> clean write

### Future conflict resolution types

For each destination path (or pattern), allow policy:
- `priority` (default)
- `merge_text` (v2+)
- `manual (v2+)` – user chooses winner
- (never) binary merge without external specialized tool

The planner should produce a plan consisting of "desired final content per
path", where the "content source" can eventually be:
- a file from a mod version (normal)
- a merged result (future)
- an overridden result (user edit)

Even if v1 only supports "file from mod", designing the plan structure this way
keeps it extensible.

### Priority Uniqueness

Priority values within a profile are unique (enforced by a unique index on
`(profile_id, priority)`). This avoids ambiguous tie-breaking in the conflict
engine but requires care during reorder operations:

- Swapping two items or inserting at an occupied priority cannot be done with
  naive sequential updates - a second update would temporarily collide.
- Recommended approach: use a sentinel priority value (e.g. a large value
  outside the normal range) as a temporary placeholder within a single
  transaction to vacate the target slot before writing final values. This
  avoids transient unique constraint violations without requiring a full bulk
  rewrite of the profile's priority sequence.
- The `profiles order` command must account for this.

## 8. Remap rules

Remap configs are attached to `profile_items`, meaning remaps are
profile-scoped by default. There is currently no mechanism for version-level
remaps that apply across profiles; this is a future extension point.

v1 remap capabilities (stored as structured data):
- strip-components (remove N leading path segments)
- select-subdir (only install entries under a subpath)
- destination-prefix (install everything under a subfolder in target)
- include/exclude patterns (optional but recommended)

Remap rules are per profile + mod version (or mayber per mod version with
profile overrides later).

### Remap evaluation

Remap configs are profile-scoped and are re-evaluated on every apply. Because
the same `mod_file_version_id` can appear in multiple profiles with different
remap configs, there is no caching of remap results. The planner always derives
the final destination path set fresh from the active profile's remap rules at
plan time.

## 9. User overrides / editable files

### Goal

Allow users to apply local modifications to mod-provided config files
(ini/yaml/json/etc.) without manually editing files after each
reinstall/profile switch.

### Design approach

Treat overrides as an additional layer applied after base mod deployment:
- Base layer: files from winning mods
- Override layer: user-defined changes applied to specific files

Two plausible override representations:
1. **Full-file override**: store the complete desired file content as a blob
   (simplest, most robust)
2. **Structured patch override**: store an "ini key/value" or "yaml path =
   value" patch (nice UX, more parsing logic)

Recommendation for readiness:
- Schema supports both, but you can implement full-file override first.
- Later add structured patch types.

### Storage

Overrides are stored in the `overrides` table:
- Scoped to a `(profile_id, target_id, relpath)` triple - one override per path per profile
- References a blob of `kind='override'` in the content-addressed store
- `override_type` is always `full_file` in v1; reserved for future structured
  patch types (ini patches, yaml merges, etc.)
- Only the latest override is stored per path; updating an override replaces
  the row (and the old blob becomes eligible for garbage collection)

### Override ownership and drift

When a file has an override:
- The tool becomes the "owner" of the final content.
- Drift detection should indicate:
  - base file differs from expected mod content
  - override differs from expected override result
  - external edits occurred

### Apply ordering

During apply:
1. deploy base mod files (priority winner)
2. apply overrides (write final file content)
3. hash and record final file hashes in installed_files

`installed_files` tracks ownership via mutually exclusive columns:
- `owner_mod_file_version_id` - set when a mod version owns the file
- `owner_override_id` - set when an override owns the file
Exactly one must be non-NULL (enforced by CHECK constraint).

### Drift Detection

When a file has an active override, drift detection distinguishes:
- Base file differs from expected mod content (mod was changed externally)
- Override result differs from expected hash (override was changed externally)

## 10. Mod Incompatibilities

### Purpose

Users can flag pairs of mod pages as incompatible with each other. The
reason is freeform - it might be known crashes, conflicting game mechanics,
or anything else. Incompatibilities are surfaced as warnings in
`profiles status` but never block apply.

### Scope

Incompatibilities are at the **mod page** level, not the version or profile
level. "Mod A and Mod B don't work together" is a property of the mods
themselves independent of which version or profile is in use.

### Storage

`mod_incompatibilities` stores pairs with canonical ordering enforced by
`CHECK (mod_page_id_a < mod_page_id_b)` plus a unique index, preventing
both `(A,B)` and `(B,A)` from being recorded. The `MIN`/`MAX` trick in
the insert and delete queries means argument order doesn't matter at the
call site.

A `source` column (`'user'` only in v1) is a forward-looking hook for
future Nexus-sourced or community-sourced incompatibility data.

Cross-game incompatibilities are prevented by a trigger
(`trg_mod_incompatibilities_same_game_ins/upd`) that verifies both mod
pages share the same `game_install_id`. This is enforced at the DB layer
for consistency with other cross-referential triggers in the schema, even
though the application layer also checks before inserting.

### Commands

`mods incompatible add <mod-page-id-a> <mod-page-id-b> [--reason "..."]`
`mods incompatible remove <mod-page-id-a> <mod-page-id-b>`
`mods incompatible list`

Mod pages are identified by numeric ID for now; fuzzy name matching is a
planned future improvement. For `add` and `remove` the game install is
implicit since mod page IDs are globally unique and carry their own
`game_install_id`. For `list` the current game install context is used to
scope results.

## 11. Backups strategy

### When to back up

Before overwriting a destination path:
- if destination is NOT currently tool-owned, back it up

### How to back up
- hash file content
- store blob in backups store
- record mapping in DB: (game, target, relpath) -> backup_hash
- dedupe naturally via content addressing

### Restore

On unapply/rollback:
- restore backups where they exist (and where it is safe to do so)
- if user changed file since backup, require explicit choice (or use hash
  checks)

## 12. Multi-store support

### Store integration responsibilities

A store integration must provide:
- discovery of installed games (list of `GameInstall`)
- for each install: resolved Targets (at least `game_dir`)
- stable store IDs (e.g., `steam:1091500`, `heroic:<slug>`)
- optional metadata (display name, icon, etc.)

### Store neutrality in the rest of the system

Everything after discovery should operate on `game_install_id` and `target_id`,
not on Steam-specific paths.

### Steam discovery

Requirements
- detect Steam installation root
- parse library folder config
- locate game install dirs from app manifests
- map appid → name + install dir
- Store games in DB and allow refresh.

## 13. Extensibility for game-specific integrations

### Integration type

Store `game.integration` (default generic).

### Hook points

Design apply as pipeline:
1. discover context (paths, targets)
2. plan
3. execute (file operations)
4. post-steps (future: generate load order, patch configs, deploy to prefix,
   run tools)

Game-specific integrations add/override:
- target definitions
- planner transformations
- post steps

This preserves a clean v1 while allowing richer v2.

## 14. Commands

- `doctor` performs environment checks including bsdtar presence, store health,
  and blob verification (presence + size check). A `--rehash` flag is reserved
  for future full content integrity verification via sha256 rehash.
- `stores list` (supported integrations)
- `games list|refresh|info`
- `mods import|list|info|remove`
- `mods scan-inventory`
- `mods incompatible add|remove|list`
- `nexus link` (attach mod_id/file_id metadata)
- `profiles
  create|list|delete|set-active|apply|diff|add|remove|enable|disable|order|status`
  - Items are added to a profile enabled by default. The schema default is `FALSE`
    but the CLI overrides this at insert time. Use `--disabled` to explicitly add
    an item without enabling it.
- `profiles order compact|move|set|swap`
- `profiles remap add|remove|list|clear|copy` - manage remap rules for a
  mod version within a profile. Rules are appended by default; use
  `--position` on `add` to insert at a specific position. Use `copy` to
  transfer remap rules from one mod version to another (e.g. when upgrading
  a mod).
- `overrides set|unset|list` (v2 behavior; schema ready in v1)
- `policy set` (future: merge/manual policy)
- `status` (conflicts, drift, missing)
- `apply` (top-level) - apply the active profile to the game directory.
  Supports `--dry-run`, `--no-recheck`, `--keep-staging`, `--print-ops`,
  `--force`, `--abort`. By default, files already correctly deployed are
  skipped (noop) and externally modified files are detected and backed up
  before overwriting. `--no-recheck` skips on-disk hash checks for faster
  applies.
- `unapply` (top-level) - remove all tool-managed files and restore backups.
  Supports `--dry-run`, `--print-ops`, `--force`, `--abort`.
- `export` - export modctl state to a portable bundle. Defaults to a full
  export of all games and blobs. Use `--game` to scope to a single game
  install. Supports `--output`/`-o` to override the output filename,
  `--skip-inventory` to omit archive inventory entries, and `--no-verify`
  to skip blob integrity verification before exporting (not recommended).
  By default all blobs are hashed and verified against their stored sha256
  before the bundle is written; `verified_at` is updated on each blob as
  part of this process. Default output filename: `modctl-export-<date>.tar.zst`
  (full) or `modctl-export-<slug>-<date>.tar.zst` (game-scoped).
- `import <bundle>` - import a modctl export bundle. Accepts both full and
  game-scoped bundles. For full bundles the database must be empty beyond
  auto-seeded rows; use --force to wipe and restore. For game-scoped bundles
  the game must not already exist; use --force to overwrite. Supports
  --dry-run to preview without making changes. Use --skip-inventory to skip
  scanning archives that have no inventory in the bundle (they can be
  scanned later with 'mods scan-inventory'). Note: no profiles are applied
  automatically after import - run 'profiles set-active' and 'apply' to
  deploy mods. Requires temporary disk space roughly equal to the bundle
  size.
- `gc` - garbage collect unreferenced blobs from the blob store
- `verify <bundle>` - verify the integrity of a modctl export bundle without
  importing it. Checks the database snapshot against the manifest sha256,
  runs `quick_check` and `foreign_key_check` on the bundle database, verifies
  every blob file hashes correctly against its filename, and checks that blob
  files and database rows are consistent with each other (no missing files,
  no orphaned files). Warns about version compatibility issues but does not
  error on them. Exits non-zero if any integrity issues are found.
- `extract <bundle>` - extract mod archives from a modctl export bundle.
  Without `--mod`, lists all mods in the bundle grouped by game and mod
  page, including version strings and Nexus file IDs where available.
  With `--mod <name>` (exact match), extracts the matching mod archive to
  the output directory. Use `--file` and `--version` to narrow the
  selection when multiple files or versions exist; if omitted and only one
  option exists it is selected automatically. For full bundles, `--game`
  is required when extracting; it has no effect on game-scoped bundles and
  a warning is printed if passed. Extracted files are named using the
  original filename if available, otherwise the archive format is detected
  via bsdtar and the file is named `<sha256prefix>.<ext>`. Use
  `--output-dir`/`-o` to specify the output directory (default: current
  directory). Use `--overwrite` to replace existing files; by default
  existing files are skipped with a warning. Nexus mod and file IDs are
  printed after each extraction if available.

Key behavior:
- "intent changes" (enable/disable/order) are cheap
- apply performs reconciliation
- always support --dry-run where destructive

### command-specifc information

#### `profiles delete`

`profiles delete` enforces two independent guards:

- If the profile is the active editing profile (`is_active = TRUE`), deletion
  requires `--force`.
- If the profile is the currently applied profile
  (`game_installs.applied_profile_id`), deletion requires `--delete-applied`,
  with an explicit warning that the profile definition will be unrecoverable
  even though `installed_files` remains intact and the disk state can still
  be reconciled by a future apply or unapply.
- If both conditions are true, both flags are required.

The active profile is never automatically switched to default on deletion.
When the applied profile is deleted, `applied_profile_id` is set to NULL
automatically via the FK `ON DELETE SET NULL` behavior.

### Incomplete operations

If a previous `apply` or `unapply` did not complete (e.g. due to a crash or
cancellation), the next run will detect `operations.status = 'running'` and
refuse to proceed. Two flags are available:

- `--abort`: marks the incomplete operation as failed and exits. The disk
  state is left as-is. The user can then run `apply` or `unapply` to
  reconcile.
- `--force`: marks the incomplete operation as failed and starts a fresh
  run from scratch.

Resume of a partial operation is not supported in v1 because the plan is
not persisted. This may be added in v2 by storing the serialized plan in
`operations.metadata`.

### Pending changes detection in `profiles status`

When a profile is currently applied, `profiles status` shows whether pending
changes exist by comparing the set of enabled mod file versions in the profile
against the set of `owner_mod_file_version_id` values in `installed_files`.
This check detects added, removed, or swapped mod versions but does not
detect priority reordering between mods that conflict on the same path. Run
`apply --dry-run` for a precise diff.

## 15. Testing strategy

### Unit tests

- remap rule application
- conflict engine and winner selection
- path normalization and safety gate
- DB invariant checks (unique constraints etc.)

### Integration tests

- run apply/unapply in temp dir "fake game"
- staging extraction tests (use small sample archives)
- drift detection behavior

### Adversarial test archives

Include in `testdata/`:
- `../` traversal
- absolute paths
- symlink entries
- duplicate entries
- deep nesting / many files

### Add fixtures for:

- "one mod page with two mod files and different archives"
- profile switching between variants
- override application on top of base deployment
- (future) merge-text tests with simple line-based merge or structured merge

### Testing conventions

- Use `github.com/stretchr/testify` throughout: `assert` for non-fatal
  checks, `require` for checks where failure makes subsequent assertions
  meaningless (wrong slice length, unexpected error, etc.)
- One top-level `TestFunctionName` per function under test, with all cases
  nested via `t.Run()`
- Use table-driven subtests where cases share the same assertion shape;
  use separate named subtests where they don't
- Call `t.Parallel()` at every level - top-level test, subtest group, and
  individual table case
- Capture loop variables with `tc := tc` before spawning parallel subtests
- Adversarial test inputs (path traversal, absolute paths, symlinks, duplicate
  entries, malformed lines) should be tested at the unit level and included
  in `testdata/` fixture archives for integration tests

## 16. Operational considerations

- lock per game during apply to avoid concurrent changes
- refuse to operate if game is running (optional v1, but helpful)
- friendly errors if `bsdtar` missing or unsupported format
- logging with operation IDs for debugging
- detect and surface incomplete operations (`status = 'running'`) on startup
  of any apply/unapply command, refusing to proceed without `--force` or
  `--abort`

## 17. Nexus Mods integration

### Overview

The Nexus integration is intentionally limited in v1: there is no download
manager or `nxm://` handler. The user downloads files manually and the tool
handles identification, linking, and update checking.

### Rate limiting

Nexus enforces per-user rate limits (2,500 requests/24h, 100 requests/hour
once the daily limit is exceeded). Rate limit state is persisted to
`$XDG_STATE_HOME/modctl/nexus_rate_limits.json` and updated after every API
call from response headers. Batch operations perform a pre-flight check and
warn the user if quota may be insufficient, with `--force` to proceed anyway.
The client also enforces a local 30 req/sec limit via a token bucket rate
limiter to avoid nginx-level 429s.

### Caching

API responses are cached in a separate SQLite database at
`$XDG_CACHE_HOME/modctl/nexus_cache.db`. The cache stores:
- `nexus_mod_info`: mod page metadata (name, author, summary). TTL: 7 days.
- `nexus_file_info`: per-file metadata (version, size, filename, category).
  TTL: 24 hours.
- `nexus_file_updates`: the file update chain (old_file_id -> new_file_id).
  TTL: same as file info (fetched together in one API call).

`profiles status` and `mods info` are read-only and always read from cache.
`mods nexus check-updates` fetches fresh data (respecting TTL unless
`--ignore-ttl` is passed) and updates the cache.

### File identification

When importing a mod archive, the tool attempts to identify the corresponding
Nexus `file_id` using the following strategy in priority order:

1. Exact filename match against `ModFileInfo.FileName` (certain)
2. `--label` pre-filter applied to candidate pool (case-insensitive)
3. `--file-version` pre-filter applied to candidate pool (case-insensitive)
4. Timestamp parsed from filename matched against `uploaded_timestamp` (confident)
5. Size + timestamp match (confident)
6. Label/version filter + single candidate + size match (confident)
7. Size only, single unambiguous match (not confident - skipped with warning)
8. Ambiguous or no match - skipped with warning, user directed to
   `mods nexus link`

Once identified, `nexus_file_id` is stored on `mod_file_versions`. The
`mods nexus link` command can be used to identify or correct links after
import.

### Update detection

Update availability is determined by walking the Nexus `file_updates` chain
forward from the installed `nexus_file_id`. If the chain leads to a different
(newer) `file_id` the mod is considered to have an update available. Version
string comparison is not used as mod authors do not consistently follow any
versioning scheme.

For display purposes, only the "head" version (one not appearing as an
`old_file_id` in the update chain) is checked for updates. Older retained
versions are shown as "old version" without an update indicator.

### Client architecture

The `internal/nexusclient` package provides:
- `Client`: full client with API key, HTTP client, rate limiter, and cache DB.
  Used for commands that make API calls.
- `CacheReader`: lightweight read-only cache accessor. Used by `profiles
  status` and `mods info` which need cached data but should not make API calls.
  `Client` embeds `CacheReader`.

The API key is read from config (`nexus.apikey`). If not configured, Nexus
linking is silently skipped at import time; commands that require it error
with a helpful message.

## 18. Garbage Collection

### Purpose

The `gc` command removes unreferenced blobs from the blob store to reclaim
disk space. It is run explicitly by the user and never triggered automatically
by other commands.

### Eligibility

A blob is eligible for collection when no live database row references it:
- **Archives**: not referenced by any `mod_file_versions.archive_sha256`
- **Backups**: not referenced by any `backups.backup_blob_sha256`

Removing a mod file version or backup row from the database does not
immediately delete the blob. The blob remains on disk until the next `gc` run
reconciles the difference.

### Orphaned on-disk files

On-disk files with no corresponding database row (orphans) are also removed
by default. Orphans can appear when an import was interrupted before the
database entry was written (e.g. process killed mid-import). Use
`--skip-orphans` to leave them in place.

Note: there is a small TOCTOU window between detecting an orphan and deleting
it - a concurrent import could have written the DB row in between. `gc` should
not be run while imports are in progress. If a race does occur the affected
blob will simply appear as a normal referenced blob on the next run.

### Missing blobs

If a blob is recorded in the database but missing from disk, `gc` prints a
warning identifying the blob by original filename and, for archives, by mod
page and file label. These dangling rows are left in place by default.
Pass `--clean-missing` to also remove them. The `doctor` command also surfaces
missing blobs.

### Fan-out directory cleanup

After removing a blob file, `gc` attempts to remove the two-character fan-out
directory (e.g. `archives/ab/`). This is best-effort: if the directory is
non-empty the removal is silently skipped.

### Commands

`gc [--dry-run] [--no-archives] [--no-backups] [--min-age <duration>] [--clean-missing] [--skip-orphans]`

Flags:
- `--dry-run`: preview what would be removed without making any changes
- `--no-archives`: skip archive blob collection
- `--no-backups`: skip backup blob collection
- `--min-age <duration>`: skip blobs created more recently than this duration;
  supports `h` (hours), `d` (days), `w` (weeks) - e.g. `7d`, `24h`, `2w`
- `--clean-missing`: remove database rows for blobs missing from disk
- `--skip-orphans`: skip on-disk files with no database row (orphans are
  removed by default)

## 19. Export and Import

### Purpose

Export produces a portable bundle that can be used to migrate modctl state
to a new machine, share a mod setup with another user, or archive a game's
mod configuration before uninstalling.

### Format version

The `export_format_version` field in `manifest.json` is `1` for all bundles
produced by the current implementation. Import checks this field first and
rejects bundles with an unrecognized version, allowing the format to evolve
without silent data corruption.

### Full vs. game-scoped

A full export includes the entire database and all blobs. It is intended for
machine migration and full backup.

A game-scoped export includes only data for a single game install and is
suitable for sharing or archiving. It never includes other games' data.

### Database snapshot

For full exports, `VACUUM INTO` is used to produce a clean, consistent
SQLite snapshot without requiring the backup C API.

For game-scoped exports, a fresh SQLite database is created, migrations are
run to bring it to the current schema, and only the relevant rows are
inserted in foreign key dependency order.

### Blob verification

Before writing the bundle, all blobs are hashed and compared against their
stored sha256 (which is also their filename in the content-addressed store).
If any mismatch is detected, export aborts with an error directing the user
to run `doctor` to investigate. `verified_at` is updated on each blob that
passes verification.

Verification can be skipped with `--no-verify` for large collections where
the user is confident in blob integrity, but this is not recommended. Use
`doctor --recheck` to verify blobs independently of export.

The `db_sha256` field in the manifest covers the database snapshot, which is
verified on import.

### Partial exports

If a blob is missing from disk at export time, a warning is printed to
stderr and the blob is omitted from the bundle. The export is not aborted.
Run `doctor` before exporting to identify missing blobs.

If the export is interrupted (e.g. Ctrl+C or any error), the partial output
file is deleted automatically.

### Import

Import validates the bundle before touching the destination database:
1. Verifies `export_format_version` is supported (refuses if newer)
2. Extracts and verifies `meta.sqlite` against `db_sha256` in the manifest
3. Warns if `modctl_version` in the bundle is newer than the running binary
4. Refuses if `schema_version` is newer than the current binary's schema

Blob files are verified by hashing their content against their filename
(which is their sha256) before ingestion. A mismatch causes import to abort.

**Full import** copies `meta.sqlite` from the bundle directly into place as
the new database, then runs migrations to bring it up to the current schema
version if needed. The database must be empty (beyond auto-seeded store
rows) unless `--force` is passed, in which case the existing database is
wiped first.

**Game-scoped import** inserts rows into the existing database with fresh
IDs assigned by SQLite. A remapping table tracks old→new IDs for each table
so foreign key references are correctly updated as rows are inserted in
dependency order. The game must not already exist (matched by
`store_id + store_game_id + instance_id`) unless `--force` is passed, in
which case the existing game install and all its dependent rows are deleted
first (cascading via FK). Orphaned blobs from the deleted install are left
for `gc` to clean up.

**Inventory scanning** is handled as follows:
- If the bundle contains inventory entries (i.e. `inventory_scanned_at` is
  non-null on the mod file version), they are imported directly
- If the bundle does not contain inventory entries and `--skip-inventory` is
  not passed, the archive is scanned immediately after import
- If `--skip-inventory` is passed, unscanned archives are left for the user
  to scan later with `mods scan-inventory`

**ID remapping** (game-scoped only): all integer primary keys are reassigned
by SQLite autoincrement on insert. The following tables require remapping:
`game_installs`, `targets`, `mod_pages`, `mod_files`, `mod_file_versions`,
`remap_configs`, `profiles`. `archive_inventory_entries` and `blobs` do not
require remapping as they are keyed by content hash.

No profiles are applied automatically after import. The user must run
`modctl profiles set-active` and `modctl apply` to deploy mods.

### Bundle verification

The `verify` command performs a full integrity check on a bundle without
importing it. It is useful for validating a bundle before importing, or for
checking a stored backup for corruption.

Checks performed:
- `db_sha256` in the manifest matches the actual sha256 of `meta.sqlite`
- `export_format_version` is supported (hard error if newer)
- `PRAGMA quick_check` on the bundle database
- `PRAGMA foreign_key_check` on the bundle database
- Every blob file in the bundle hashes correctly against its filename
- Every blob referenced in the bundle database has a corresponding file
- Every blob file in the bundle has a corresponding database row

Version warnings (`modctl_version` or `schema_version` newer than the
running binary) are printed but do not affect the exit code. All other
issues cause a non-zero exit.

All blob issues are collected before reporting so the user gets a complete
picture of the bundle's state in a single run.

### Archive extraction

The `extract` command extracts raw mod archive files from a bundle so they
can be re-imported with `mods import` into a different installation, without
performing a full restore.

#### Listing

Without `--mod`, the command lists all mods in the bundle grouped by game
and mod page:

    Mods in bundle (game: Cyberpunk 2077  steam:1091500):

      Mod Page: Appearance Menu Mod
        File: Main File
          v1.0.0  abc123def456...  (nexus file_id=456)

For full bundles each game is printed as a separate section. For
game-scoped bundles there is only one section.

#### Selection

Mods are selected with `--mod <name>` (exact match against mod page name).
`--file` and `--version` narrow the selection further. If either is omitted
and only one option exists it is used automatically; if multiple options
exist the command lists them and exits with an error.

For full bundles, `--game` is required for extraction to avoid ambiguity
when multiple games contain mods with the same name.

#### Output filename

Files are named using `original_name` from `mod_file_versions` if
available. Otherwise the archive format is detected by running
`bsdtar -tvvf` on the blob and parsing the summary trailer line
(`Archive Format: ..., Compression: ...`), which is mapped to a file
extension. The file is then named `<sha256prefix>.<ext>`. If format
detection fails the sha256 prefix is used with no extension.

#### Blob integrity

The specific blob being extracted is hashed and verified against its
sha256 before being copied to the output directory. This is a targeted
check rather than a full bundle verification - use `verify` for a
complete integrity check.

#### Nexus information

If the extracted version has a `nexus_file_id` and the mod page has
`nexus_mod_id` and `nexus_game_domain` recorded, the Nexus URL and IDs
are printed after extraction. The Nexus cache is not consulted since it
is not included in bundles.
