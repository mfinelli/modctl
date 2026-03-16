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

| Flag             | Description                      |
|------------------|----------------------------------|
| `--priority <n>` | Assign a specific priority value |
| `--disabled`     | Add the mod without enabling it  |

### profiles remove

Remove a mod from the active profile. Does not change anything on disk.
```bash
modctl profiles remove "Appearance Menu Mod"
```

### profiles enable / disable

Enable or disable a mod within the active profile. Disabled mods remain in
the profile but are excluded from planning entirely.
```bash
modctl profiles enable "Appearance Menu Mod"
modctl profiles disable "Appearance Menu Mod"
```

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
