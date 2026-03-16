# Remap rules

When you import a mod archive, the files inside it are not always laid out the
way your game expects them. Some archives have an extra top-level folder that
needs to be stripped. Others provide files that should land in a specific
subfolder of the game directory. Remap rules let you adjust how a mod's files
are mapped into the game directory without modifying the archive itself.

Remap rules are profile-scoped: the same mod version can be installed
differently in different profiles. They are applied during planning, before
conflict resolution runs, so the paths that conflict resolution operates on
are already the final destination paths.

## When you need remap rules

The most common situation is an archive that packages its files inside a
top-level folder:
```
MyMod-v1.0/
  some_file.pak
  another_file.cfg
```

Without a remap rule, modctl would try to install a folder called `MyMod-v1.0`
into your game directory, which is not what you want. A `strip_components`
rule removes that leading segment so the files land directly where they belong.

Another common case is an archive that contains multiple variants or optional
components in separate subdirectories, where you only want one of them.

## Adding remap rules

Remap rules are added per mod version within a profile. You identify the mod
version by name or internal ID — if the name is ambiguous modctl will show you
a list and ask you to rerun with the ID directly.

Rules are applied in order, and by default each new rule is appended after
any existing rules. Use `--position` to insert a rule at a specific position
instead.

### strip_components

Removes the first N leading path segments from every entry in the archive:
```bash
modctl profiles remap add "Appearance Menu Mod" strip_components 1
```

This is the most commonly needed rule. A value of `1` strips one leading
folder from every path; `2` strips two, and so on.

### select_subdir

Only installs entries that live under a specific subdirectory in the archive,
and strips that subdirectory prefix from the result:
```bash
modctl profiles remap add "My Mod" select_subdir Data
```

If the archive contains both a `Data/` folder and a `Docs/` folder and you
only want the contents of `Data/`, this rule selects just those entries and
installs them as if `Data/` were the root of the archive.

### dest_prefix

Installs all entries under a specific subfolder in the game directory:
```bash
modctl profiles remap add "My Mod" dest_prefix Data/mymod
```

Every file in the archive will be placed under `Data/mymod/` in your game
directory rather than at the root.

### include_glob and exclude_glob

Filter which entries from the archive are installed based on a glob pattern.

Install only entries matching a pattern:
```bash
modctl profiles remap add "My Mod" include_glob "*.esp"
```

Skip entries matching a pattern:
```bash
modctl profiles remap add "My Mod" exclude_glob "*.txt"
```

You can add multiple include and exclude rules. They are evaluated in position
order along with all other rules.

## Viewing and managing rules

To see the current remap rules for a mod version in the active profile:
```bash
modctl profiles remap list "Appearance Menu Mod"
```

Rules are shown in the order they will be applied.

To remove a single rule by its position:
```bash
modctl profiles remap remove "Appearance Menu Mod" 2
```

To remove all rules for a mod version at once:
```bash
modctl profiles remap clear "Appearance Menu Mod"
```

## Previewing the effect of your rules

Before running an apply it can be useful to see exactly what your remap rules
will do to a mod's archive entries. The `remap preview` command shows every
file entry in the archive alongside the destination path it would be installed
to after all rules have been applied:
```bash
modctl profiles remap preview "Appearance Menu Mod"
```

If no remap rules are configured, the command shows all entries as-is so you
can see the raw archive layout which can be useful for deciding which rules you
need to add.

By default entries that are filtered out by rules are hidden. Pass
`--show-filtered` to see them alongside the reason they were excluded:
```bash
modctl profiles remap preview --show-filtered "Appearance Menu Mod"
```

This is particularly helpful when a `select_subdir` or `exclude_glob` rule is
filtering more than you expected: you can see exactly which entries were
dropped and which rule caused each one to be skipped.

The archive must be inventoried before running this command. If it has not
been scanned yet, run `modctl mods scan-inventory` first.

## Copying rules when upgrading a mod

When you upgrade a mod to a new version you will typically want the same remap
rules to apply to the new version. Rather than recreating them by hand, use
`remap copy` to transfer the rules from the old version to the new one:
```bash
modctl profiles remap copy "Appearance Menu Mod v1.0" "Appearance Menu Mod v1.1"
```

If the destination already has remap rules they will be replaced. If the
source has no rules this is a no-op. Once you have verified the rules copied
correctly you can remove the old version from the profile.

## Remap rules and conflict resolution

It is worth being explicit about the order of operations during planning:

1. Remap rules are applied to each mod's file list to produce final destination paths.
2. Conflict resolution runs on those final paths.

This means that if two mods would land files at the same destination path
after remapping, that is a normal conflict and priority order decides the
winner. Remap rules cannot be used to avoid conflicts — they determine where
files go, not which mod wins.

---

Next up is [Nexus integration](../nexus), which covers linking your mods to
their Nexus Mods pages and checking for updates.
