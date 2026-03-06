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

Suggested layout with directory fanout:
- `archives/sha256/ab/<fullhash>`
- `backups/sha256/ab/<fullhash>`

Rationale:
- simplicity (one storage mode)
- dedupe
- filesystem-friendly backups
- clean GC

### Export/import bundle

A single file (tar + zstd) containing:
- `meta.sqlite`
- `archives/`
- `backups/`
- `manifest.json` including versions (bundle version, schema version), counts,
  and optional hashes

Import verifies integrity and schema compatibility.

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
`archive_sha256` (not `mod_file_version_id`), scanning one version updates
all versions that reference the same blob.

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
- `export|import`
- `gc archives|gc backups`

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
