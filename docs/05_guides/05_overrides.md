# Overrides

Sometimes you want a specific file in your game directory to have content that
no mod provides exactly, or you want to make a small adjustment to a
mod-provided file without replacing it entirely. Overrides let you do this
without creating a separate single-file mod or manually editing files after
every apply.

Overrides sit above the normal mod priority layer. When an override exists for
a path, it is the final word on that file's content regardless of which mods
are in the profile or what priority order they have.

Overrides are profile-scoped: an override in one profile has no effect on
any other profile.

## Full-file overrides

A full-file override replaces a file's content entirely with a file you
provide. Every time the profile is applied, modctl writes your file to that
path instead of whatever the winning mod would have written.

### Creating a full-file override
```bash
modctl profiles overrides set textures/ui/crosshair.dds my-crosshair.dds
```

The path is relative to the game directory. The file you provide is stored
in modctl's override blob store so you can delete or move the original file
afterwards.

When you create an override, modctl records which mod was providing that file
at the time and what its content was. This is the *source anchor*, and it is
used later to tell you if the underlying mod file has changed.

If no mod in the profile currently provides that path, the override writes a
net-new file into the game directory on the next apply.

### Editing an override in place

If you want to make further changes to an existing override, you can open it
directly in your editor:
```bash
modctl profiles overrides edit settings.config
```

If no override exists yet for that path, modctl extracts the base file from
the current winning mod's archive and opens that instead. Saving creates a
new override with the source anchor captured automatically.

Use `--reset` to discard your existing override and start fresh from the
current base file:
```bash
modctl profiles overrides edit --reset settings.config
```

### Removing a full-file override
```bash
modctl profiles overrides unset textures/ui/crosshair.dds
```

The override is removed and the blob becomes eligible for garbage collection.
The next apply will restore whatever the winning mod provides, or remove the
file entirely if no mod claims that path.

## Patch overrides

A patch override applies a set of structured mutations on top of the base
mod's file rather than replacing it entirely. This is useful for config
files where you want to change one or two values without losing the rest of
the mod's settings, and without having to update your override every time the
mod ships a new version with other changes.

Patch overrides are supported for `.ini`, `.json`, `.xml`, and `.yaml`/`.yml`
files.

When a patch override exists for a path, modctl extracts the base file from
the winning mod's archive at apply time, applies your patch entries in order,
and writes the result to disk.

### Adding patch entries

Use `profiles overrides patch set` to add or update a patch entry:
```bash
# ini
modctl profiles overrides patch set settings.ini MaxFramerate 60 --section Display

# yaml
modctl profiles overrides patch set config.yaml graphics.quality ultra

# json
modctl profiles overrides patch set config.json renderDistance 128

# xml (the key is an XPath expression)
modctl profiles overrides patch set config.xml "//Display/Resolution" 1440
modctl profiles overrides patch set config.xml "//Window/@width" 2560
```

The patch type is inferred from the file extension automatically. If modctl
cannot determine the type from the extension, pass `--type ini`, `--type yaml`,
`--type json`, or `--type xml` explicitly. You only need to pass `--type` once;
after that the type is recorded on the override and subsequent patch commands
use it automatically.

For INI files, `--section` scopes the entry to a specific section. Without
`--section` the entry targets the default (global) section.

For XML files, the key is an XPath expression. Expressions ending in `/@name`
target an attribute; all others target the text content of the matched
element. If the expression matches multiple nodes, all of them are updated.

If you run `patch set` for a key that already has a patch entry, its value
is updated in place rather than creating a duplicate.

If no patch override exists for the path yet, one is created automatically
on the first `patch set` or `patch unset`.

### Removing a key from the file on apply

To mark a key for removal so it is deleted from the file when the patch is
applied:
```bash
modctl profiles overrides patch unset settings.ini SomeObsoleteKey --section Display
```

For XML overrides, `patch unset` removes the matched node entirely. If you
want to empty the node's content without removing it, pass `--clear` instead:
```bash
modctl profiles overrides patch unset config.xml "//Window/@legacy" --clear
```

`--clear` is only valid for XML overrides. `--section` is only valid for INI
overrides.

### Removing a patch entry entirely

`patch unset` marks a key for removal on apply. If you want modctl to stop
patching a key altogether (neither setting nor removing it) use
`patch remove` instead:
```bash
modctl profiles overrides patch remove settings.ini MaxFramerate --section Display
```

If removing the entry leaves the override with no remaining patch entries,
the override itself is also removed.

### Listing patch entries
```bash
modctl profiles overrides patch list settings.ini
```

Entries are shown in the order they will be applied.

### Previewing the result

Before running an apply you can see exactly what the patched file will look
like:
```bash
modctl profiles overrides patch preview settings.ini
```

modctl extracts the base file from the current winning mod's archive, applies
all patch entries in memory, and shows a unified diff of the changes. This
requires archive extraction and may be slow for large archives.

If no mod in the profile provides the file, the patch is applied against an
empty document and the full result is shown with a warning.

## Listing and copying overrides

To see all overrides for the active profile:
```bash
modctl profiles overrides list
```

To copy all overrides from another profile into the active one:
```bash
modctl profiles overrides copy "Other Profile"
```

If the active profile already has overrides at any of the same paths, the
command will refuse unless you pass `--force`, which replaces the conflicting
overrides.

Copying overrides preserves the source anchor from the original profile, so
staleness detection continues to work correctly after the copy.

## Staleness and status

When modctl creates an override it records a snapshot of the base file's
content at that moment: the *source anchor*. If the mod is later updated and
the base file changes, modctl can tell you that your override may need to be
revisited.

To see the full staleness status for all overrides in the active profile:
```bash
modctl profiles overrides status
```

Each override is reported in one of these states:

| State               | Meaning                                                                                                                                   |
|---------------------|-------------------------------------------------------------------------------------------------------------------------------------------|
| `base_unchanged`    | The base mod file has not changed since the override was created.                                                                         |
| `stale`             | The base mod file has changed. You should review whether your override still makes sense.                                                 |
| `redundant`         | Your override content is identical to what the base mod provides. It may no longer be necessary.                                          |
| `no_base`           | No mod in the profile currently provides this path. The override is writing a net-new file, or the base mod was removed from the profile. |
| `anchor_lost`       | The source archive was removed from modctl's store. Staleness cannot be determined.                                                       |
| `net_new_no_anchor` | The override was created for a path no mod has ever provided. No staleness concept applies.                                               |

A lightweight staleness summary is also shown as part of `profiles status`
without requiring a full check.

Staleness is a signal, not a block. A stale override is still applied normally;
modctl will never refuse to apply because an override is stale. It is up to
you to decide whether to update or remove the override after reviewing the
change in the base file.

---

Next is the [Deploy rules](../deploy-rules) guide, which covers how top stop
modctl from backing up or overwriting certain mod-shipped files.
