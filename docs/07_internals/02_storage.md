# Storage layout

modctl stores its data in three locations following the
[XDG Base Directory](https://specifications.freedesktop.org/basedir-spec/latest/)
specification. On a standard Linux desktop these are:

| Purpose                  | Default path              |
|--------------------------|---------------------------|
| Database and blob stores | `~/.local/share/modctl/`  |
| Configuration            | `~/.config/modctl/`       |
| Lock files               | `~/.local/state/modctl/`  |
| Staging directory        | `/run/user/<uid>/modctl/` |

If you have customised any of these via the configuration file, the paths
will differ. Run `modctl config list` to see the effective paths on your
system.

## Database

modctl's metadata (games, mods, profiles, installed files, backups, and
everything else) is stored in a single SQLite database at:
```
~/.local/share/modctl/modctl.db
```

This file is the authoritative record of everything modctl knows about your
mod setup. It is safe to copy as a lightweight backup, though a full export
with `modctl export` is the recommended way to back up your setup since it
includes the blob stores as well.

## Blob stores

Binary files (mod archives and backups of game files) are stored in
content-addressed blob stores under `~/.local/share/modctl/`:
```
~/.local/share/modctl/archives/
~/.local/share/modctl/backups/
~/.local/share/modctl/overrides/
```

Every file in these directories is named by its SHA-256 hash and stored
under a two-character subdirectory derived from the first two characters of
that hash:
```
archives/
  ab/
    abcdef1234...
  f3/
    f3a829bc...
```

This layout has a few useful properties. Because files are named by their
content, the same archive imported twice is only stored once; deduplication
is automatic. Blobs are immutable; once written they are never modified.
Unreferenced blobs (for example after removing a mod) are cleaned up by
`modctl gc`.

The blob stores can be copied directly as a backup, though as with the
database the recommended approach is `modctl export`, which produces a
consistent snapshot of both the database and the blobs together in a single
portable bundle.

You can verify the integrity of the blob store with `modctl doctor` command.

## Nexus cache

Nexus API responses are cached in a separate SQLite database at:
```
~/.cache/modctl/nexus_cache.db
```

This file is safe to delete at any time, it will be repopulated the next
time you run `modctl mods nexus check-updates`. It is intentionally excluded
from export bundles.

## Staging directory

During an apply, modctl extracts mod archives to a temporary staging
directory before moving files into the game directory:
```
/run/user/<uid>/modctl/staging/
```

This directory lives under `$XDG_RUNTIME_DIR`, which on most Linux desktops
is a tmpfs mount a fast, in-memory storage that is cleaned up automatically
on logout. The staging directory is removed after a successful apply. If you
work with very large mod archives or have a small tmpfs allocation you can
point `tmp_dir` at a different location in the configuration.

## Configuration file

The configuration file lives at:
```
~/.config/modctl/config.toml
```

It is optional, if it does not exist modctl uses built-in defaults for
everything. See the [Configuration reference](../../guides/configuration)
for available keys and their defaults.
