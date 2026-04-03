# profiles

Manage profiles for the current game. A profile is a named set of mods with
a priority order. For a conceptual overview see
[Profiles and priority ordering](../../guides/profiles).

Most profile commands operate on the active profile and active game by default.
Pass `--profile <name>` to target a different profile, or `--game <store-id>`
to target a different game.

## Managing profiles

### profiles list

List all profiles for the current game. The active profile is marked with
an asterisk.
```bash
modctl profiles list
```

### profiles create

Create a new profile for the current game. New profiles start inactive.
```bash
modctl profiles create "Graphics Overhaul"
```

### profiles set-active

Set the active profile for the current game. Commands that operate on profile
contents default to the active profile unless `--profile` is provided.
```bash
modctl profiles set-active "Graphics Overhaul"
```

### profiles rename

Rename an existing profile. Profile names must be unique per game.
```bash
modctl profiles rename "Graphics Overhaul" "New Name"
```

### profiles delete

Delete a profile. This removes the profile definition (its mod list, priority
order, and remap rules) but does not change anything on disk.
```bash
modctl profiles delete "Graphics Overhaul"
```

If the profile is currently active, pass `--force`. If it is the currently
applied profile (its files are on disk), pass `--delete-applied`.

| Flag               | Description                                          |
|--------------------|------------------------------------------------------|
| `--force`          | Required if the profile is currently active          |
| `--delete-applied` | Required if the profile is currently applied to disk |

### profiles status

Show the mods in the active profile in priority order, along with their
enabled/disabled state, version information, and any warnings such as
incompatibilities or missing inventory scans. When the profile is currently
applied, also shows whether there are pending changes.
```bash
modctl profiles status
```

| Flag        | Description                     |
|-------------|---------------------------------|
| `--compact` | Shorter one-line-per-mod output |

### profiles diff

Compare two profiles for the current game. The comparison is directional:
the first profile is the source and the second is the target.
```bash
modctl profiles diff "Default" "Graphics Overhaul"
```

| Flag             | Description                                   |
|------------------|-----------------------------------------------|
| `--no-unchanged` | Hide mods that are identical in both profiles |

## Managing mods in a profile

### profiles add

Add a mod to the active profile. Mods are added enabled by default. If
`--priority` is not provided, modctl assigns the next highest priority
automatically.
```bash
modctl profiles add "Appearance Menu Mod"
```

Use `--target` to specify which install target the mod deploys to. If not
provided, defaults to `game_dir`. The target must already exist for the
current game; run `modctl games targets list` to see available targets.
```bash
modctl profiles add "ToyBox" --target unitymodmanager
```

| Flag              | Description                                               |
|-------------------|-----------------------------------------------------------|
| `--priority <n>`  | Assign a specific priority value                          |
| `--disabled`      | Add the mod without enabling it                           |
| `--target <name>` | Deploy to a specific install target (default: `game_dir`) |

### profiles remove

Remove a mod from the active profile. Does not change anything on disk.
```bash
modctl profiles remove "Appearance Menu Mod"
```

### profiles upgrade

Swap the mod file version currently in a profile for a newer one, preserving
the existing priority slot, enabled state, remap rules, and deployment rules

Without `--to`, modctl picks the most recently imported version of the same
mod file that is not already in the profile. With `--to`, the specified
version is used directly.

The old version is removed from the profile but its database record is
preserved. Run `modctl gc` afterwards to reclaim disk space if needed.
```bash
modctl profiles upgrade "Appearance Menu Mod"
modctl profiles upgrade "Appearance Menu Mod" --to 42
```

| Flag       | Description                                                               |
|------------|---------------------------------------------------------------------------|
| `--to <n>` | Specific mod file version ID to upgrade to instead of the latest imported |

### profiles enable / disable

Enable or disable a mod within the active profile. Disabled mods remain in
the profile but are excluded from planning entirely.
```bash
modctl profiles enable "Appearance Menu Mod"
modctl profiles disable "Appearance Menu Mod"
```

Pass `--all` to enable or disable every mod in the profile at once. `--all`
and a positional argument are mutually exclusive.
```bash
modctl profiles enable --all
modctl profiles disable --all
```

### profiles preview

Show a unified diff between the current on-disk file and what the active
profile's winning mod would write at that path if apply were run. Useful
for understanding exactly what apply would change at a specific path before
deciding whether to add a write-once or skip-backup rule.
```bash
modctl profiles preview "settings/game.ini"
```

Note: this command requires archive extraction which may be slow for large
archives.

Binary files are detected automatically and refused unless `--force` is
passed.

| Flag              | Description                                      |
|-------------------|--------------------------------------------------|
| `--target <name>` | Install target (default: `game_dir`)             |
| `--force`         | Diff binary files without refusing               |

## Priority order

### profiles order move

Move a mod before or after another mod in the priority order. Rewrites
priorities to a compact sequence starting at 1.
```bash
modctl profiles order move "Mod A" --after "Mod B"
modctl profiles order move "Mod A" --before "Mod B"
```

### profiles order swap

Swap the priorities of two mods.
```bash
modctl profiles order swap "Mod A" "Mod B"
```

### profiles order set

Set an exact priority value for a mod. Priority values must be unique within
a profile.
```bash
modctl profiles order set "Mod A" 500
```

### profiles order compact

Renumber priorities to a clean sequence while preserving the current order.
```bash
modctl profiles order compact
```

| Flag             | Description                                                              |
|------------------|--------------------------------------------------------------------------|
| `--multiple <n>` | Assign priorities as multiples of N (e.g. 10, 20, 30) instead of 1, 2, 3 |

## Remap rules

For a full explanation of remap rules see [Remap rules](../../guides/remap).

### profiles remap list

List all remap rules for a mod version in the active profile, in the order
they will be applied.
```bash
modctl profiles remap list "Appearance Menu Mod"
```

### profiles remap add

Add a remap rule for a mod version. Rules are appended by default; use
`--position` to insert at a specific position.
```bash
modctl profiles remap add "Appearance Menu Mod" strip_components 1
modctl profiles remap add "Appearance Menu Mod" select_subdir Data
modctl profiles remap add "Appearance Menu Mod" dest_prefix Data/mymod
modctl profiles remap add "Appearance Menu Mod" include_glob "*.esp"
modctl profiles remap add "Appearance Menu Mod" exclude_glob "*.txt"
```

| Flag             | Description                                                 |
|------------------|-------------------------------------------------------------|
| `--position <n>` | Insert the rule at a specific position instead of appending |

### profiles remap remove

Remove a remap rule at a specific position. Use `remap list` to see current
positions.
```bash
modctl profiles remap remove "Appearance Menu Mod" 2
```

### profiles remap clear

Remove all remap rules for a mod version.
```bash
modctl profiles remap clear "Appearance Menu Mod"
```

### profiles remap copy

Copy remap rules from one mod version to another within the same profile.
Useful when upgrading a mod to a new version. If the destination already has
rules they will be replaced.
```bash
modctl profiles remap copy "Appearance Menu Mod v1.0" "Appearance Menu Mod v1.1"
```

### profiles remap preview

Preview how remap rules transform a mod's archive entries. Shows each file
entry in the archive alongside the destination path it would be installed to
after all rules have been applied. Useful for verifying that your remap
configuration does what you expect before running an apply.

If no remap rules are configured, all archive entries are shown as-is.
```bash
modctl profiles remap preview "Appearance Menu Mod"
```

Entries filtered out by rules are hidden by default. Pass `--show-filtered`
to see them alongside the reason they were excluded:
```bash
modctl profiles remap preview --show-filtered "Appearance Menu Mod"
```

The archive must be inventoried before running this command. If it has not
been scanned yet, run `modctl mods scan-inventory` first.

| Flag              | Description                                                            |
|-------------------|------------------------------------------------------------------------|
| `--show-filtered` | Show entries excluded by rules alongside the reason they were filtered |

## Overrides

For a full explanation of overrides see [Overrides](../../guides/overrides).

### profiles overrides set

Create or replace a full-file override for a path in the active profile. The
path is relative to the game directory. The file is ingested into modctl's
override store so you can delete the original afterwards, if you'd like.
```bash
modctl profiles overrides set textures/ui/crosshair.dds my-crosshair.dds
```

### profiles overrides edit

Open an override's content in your editor (`$VISUAL` or `$EDITOR`, falling
back to `vi`). If no override exists for the path, modctl extracts the base
file from the current winning mod's archive and opens that instead; saving
creates a new override. If an override already exists, its current content is
opened for editing.
```bash
modctl profiles overrides edit textures/ui/crosshair.dds
```

| Flag      | Description                                                              |
|-----------|--------------------------------------------------------------------------|
| `--reset` | Discard the existing override and start fresh from the current base file |

### profiles overrides unset

Remove an override for a path. The next apply will restore whatever the
winning mod provides, or remove the file if no mod claims the path.
```bash
modctl profiles overrides unset textures/ui/crosshair.dds
```

### profiles overrides list

List all overrides for the active profile with a summary staleness status.
```bash
modctl profiles overrides list
```

### profiles overrides status

Show full staleness detail for all overrides in the active profile, or for a
specific path. Includes source anchor information and a human-readable
explanation of each override's state.
```bash
modctl profiles overrides status
modctl profiles overrides status textures/ui/crosshair.dds
```

### profiles overrides copy

Copy all overrides from another profile into the active profile. Source anchor
fields are preserved so staleness detection works correctly after copying.
```bash
modctl profiles overrides copy "Other Profile"
```

| Flag      | Description                                         |
|-----------|-----------------------------------------------------|
| `--force` | Replace conflicting overrides in the active profile |

### profiles overrides patch set

Add or update a patch entry for a path in the active profile. The patch type
is inferred from the file extension or can be specified with `--type`. If no
patch override exists for the path yet, one is created automatically.
```bash
modctl profiles overrides patch set settings.ini MaxFramerate 60 --section Display
modctl profiles overrides patch set config.yaml graphics.quality ultra
modctl profiles overrides patch set config.json renderDistance 128
modctl profiles overrides patch set config.xml "//Display/Resolution" 1440
```

| Flag               | Description                                                                            |
|--------------------|----------------------------------------------------------------------------------------|
| `--section <name>` | Section to target (INI overrides only)                                                 |
| `--type <type>`    | Patch type: `ini`, `yaml`, `json`, or `xml` (inferred from extension if not specified) |

### profiles overrides patch unset

Mark a key for removal so it is deleted from the file when the patch is
applied. If a set entry already exists for this key it is converted to an
unset entry.
```bash
modctl profiles overrides patch unset settings.ini SomeKey --section Display
modctl profiles overrides patch unset config.xml "//Window/@legacy" --clear
```

| Flag               | Description                                                                            |
|--------------------|----------------------------------------------------------------------------------------|
| `--section <name>` | Section to target (INI overrides only)                                                 |
| `--type <type>`    | Patch type: `ini`, `yaml`, `json`, or `xml` (inferred from extension if not specified) |
| `--clear`          | Empty the node's content instead of removing it (XML overrides only)                   |

### profiles overrides patch remove

Remove a patch entry entirely so modctl no longer patches that key. Different
from `patch unset`, which marks the key for removal from the file on apply.
If no patch entries remain after removal, the override itself is also removed.
```bash
modctl profiles overrides patch remove settings.ini MaxFramerate --section Display
```

| Flag               | Description                            |
|--------------------|----------------------------------------|
| `--section <name>` | Section to target (INI overrides only) |

### profiles overrides patch list

List all patch entries for a path in the active profile, in the order they
will be applied.
```bash
modctl profiles overrides patch list settings.ini
```

### profiles overrides patch preview

Extract the base file from the current winning mod's archive, apply all patch
entries in memory, and display a unified diff of the result. Requires archive
extraction and may be slow for large archives.
```bash
modctl profiles overrides patch preview settings.ini
```

## Deployment rules

For a full explanation of deployment rules see
[Deployment rules](../../guides/deploy-rules).

### profiles deploys skip-backup list

List all skip-backup patterns for a mod version in the active profile.
Patterns are evaluated against the final remapped destination path.
```bash
modctl profiles deploys skip-backup list "My Mod"
```

### profiles deploys skip-backup add

Add a skip-backup pattern for a mod version in the active profile. Files
matching the pattern are never backed up during apply, including the initial
backup of any pre-existing game-owned file. On unapply, matched paths are
deleted rather than restored.
```bash
modctl profiles deploys skip-backup add "My Mod" "*.cache"
modctl profiles deploys skip-backup add "My Mod" "Cache/*"
```

### profiles deploys skip-backup remove

Remove a skip-backup pattern. Use `skip-backup list` to see current patterns.
```bash
modctl profiles deploys skip-backup remove "My Mod" "*.cache"
```

### profiles deploys skip-backup copy

Copy skip-backup patterns from one mod version to another within the same
profile. Existing patterns on the destination are replaced. Useful when
manually swapping mod versions.
```bash
modctl profiles deploys skip-backup copy "My Mod v1.0" "My Mod v1.1"
```

### profiles deploys write-once list

List all write-once patterns for a mod version in the active profile.
Patterns are evaluated against the final remapped destination path.
```bash
modctl profiles deploys write-once list "My Mod"
```

### profiles deploys write-once add

Add a write-once pattern for a mod version in the active profile. Files
matching the pattern are deployed on first apply and left untouched on
subsequent applies, preserving any in-game changes. If a matched file is
missing from disk it is re-deployed.
```bash
modctl profiles deploys write-once add "My Mod" "settings.ini"
modctl profiles deploys write-once add "My Mod" "Config/*.cfg"
```

### profiles deploys write-once remove

Remove a write-once pattern. Use `write-once list` to see current patterns.
```bash
modctl profiles deploys write-once remove "My Mod" "settings.ini"
```

### profiles deploys write-once copy

Copy write-once patterns from one mod version to another within the same
profile. Existing patterns on the destination are replaced. Useful when
manually swapping mod versions.
```bash
modctl profiles deploys write-once copy "My Mod v1.0" "My Mod v1.1"
```
