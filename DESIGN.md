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

Model it like Nexus does:
- **ModPage** (a mod "project")
  - Source: local/manual or Nexus
  - If Nexus: `nexus.mod_id`, maybe `nexus.game_domain`/slug
  - Human name, notes, tags
- **ModFile** (a downloadable file under a mod page)
  - If Nexus: `nexus.file_id`
  - Human label (e.g., "Main File", "Optional - 2K Textures", "Update",
    "Patch")
  - May have its own version string, or none
- **ModFileVersion** (a specific archive blob)
  - archive blob hash
  - extracted inventory cache (optional)
  - observed version metadata (if available)
  - imported_at, original filename

Profiles should enable **ModFile** (with a chosen version policy) or directly
pin a **ModFileVersion**:
- v1 simplest: pin a ModFileVersion
- later: allow "track latest" policies if user provides API key

This cleanly distinguishes "multiple archives under one Nexus mod".

### Profile

A named set of enabled mod versions for a `GameInstall`, with:
- set of enabled/disabled mod file versions (or mod files with pinned version
- per-profile priority order (higher priority wins conflicts)
- remap rules per mod (possibly per version)
- (future) per-path merge policy or override rules

Exactly one profile can be active/applied at a time per `GameInstall`.

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

## 4. Extraction model

### v1 extraction: external `bsdtar`

- Inventory: `bsdtar -t` to list entries (best-effort metadata).
- Apply: extract to staging dir; never directly to the game directory.

### In-process extraction (unlikely future)

Possible future backends:
- pure-Go zip/tar
- libarchive via CGO
- fallback to bsdtar/7z

To keep this option open, extraction is an interface with multiple backends.

## 5. Safety model

### Staging + safe move

All extraction goes to staging, then the tool:
- validates destination paths
- rejects traversal and absolute paths
- enforces "within target root"
- applies remap rules deterministically
- moves files into place

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

Never blindly delete:
- Only delete a file if its hash matches what the tool installed (unless
  `--force`).
- If changed externally, mark "drifted" and require explicit action.

## 6. Conflict and priority rules

### Winner selection

For each destination path:
- winner = enabled mod with highest priority that provides that path

### Apply semantics

Apply reconciles filesystem to profile state:
- write/overwrite winners
- remove files that are no longer winners and are tool-owned (hash match)
- restore backups when "rolling back to tool vanilla" where applicable

Reordering priorities is supported by recalculating winners and applying plan;
implementation may be "unapply + apply" in v1.

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

## 7. Remap rules

v1 remap capabilities (stored as structured data):
- strip-components (remove N leading path segments)
- select-subdir (only install entries under a subpath)
- destination-prefix (install everything under a subfolder in target)
- include/exclude patterns (optional but recommended)

Remap rules are per profile + mod version (or mayber per mod version with
profile overrides later).

## 8. User overrides / editable files

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

## 9. Backups strategy

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

## 10. Multi-store support

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

## 11. Extensibility for game-specific integrations

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

## 12. Commands

- `doctor` (environment checks, bsdtar presence, store health)
- `stores list` (supported integrations)
- `games list|refresh|info`
- `mods import|list|info|remove`
- `nexus link` (attach mod_id/file_id metadata)
- `profiles
  create|list|delete|set-active|apply|diff|add|remove|enable|disable|order`
- `overrides set|unset|list` (v2 behavior; schema ready in v1)
- `policy set` (future: merge/manual policy)
- `status` (conflicts, drift, missing)
- `unapply` (remove tool-installed, restore backups)
- `export|import`
- `gc archives|gc backups`

Key behavior:
- "intent changes" (enable/disable/order) are cheap
- apply performs reconciliation
- always support --dry-run where destructive

## 13. Testing strategy

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

## 14. Operational considerations

- lock per game during apply to avoid concurrent changes
- refuse to operate if game is running (optional v1, but helpful)
- friendly errors if `bsdtar` missing or unsupported format
- logging with operation IDs for debugging
