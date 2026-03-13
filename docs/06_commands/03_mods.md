# mods

Manage mod archives for the current game. For a conceptual overview of how
modctl organises mods into pages, files, and versions see
[Overview](../../overview).

## Importing mods

### mods import

Import a mod archive into modctl's local store. modctl copies the archive,
validates it using `bsdtar`, records its contents in the database, and makes
it available to add to profiles.
```bash
modctl mods import ~/Downloads/Appearance\ Menu\ Mod-790-2-12-5-1749642728.zip
```

If the file is not a supported archive format, modctl will wrap it into a
`.tar.gz` archive automatically so it can be stored and extracted consistently.

If you are importing from Nexus Mods, pass `--nexus-url` to link the archive
to its mod page at import time. This is the most reliable way to ensure a
clean Nexus link (see [Nexus integration](../../guides/nexus) for details).
```bash
modctl mods import ~/Downloads/AppearanceMenuMod-790-2-6-2-1728496438.7z \
  --nexus-url "https://www.nexusmods.com/cyberpunk2077/mods/790"
```

modctl does not perform any dependency resolution. If a mod requires other
mods or frameworks to function, you are responsible for importing and adding
each one to your profile yourself.

| Flag                | Description                                                                         |
|---------------------|-------------------------------------------------------------------------------------|
| `--nexus-url <url>` | Link the archive to a Nexus mod page at import time                                 |
| `--rm`              | Delete the original file after a successful import                                  |
| `--skip-inventory`  | Skip scanning archive contents (can be backfilled later with `mods scan-inventory`) |

## Viewing mods

### mods list

List all imported mods for the current game. By default shows one line per
mod page with file counts and the latest imported version.
```bash
modctl mods list
```

Pass `--details` to expand each mod page and show its individual files and
versions:
```bash
modctl mods list --details
```

### mods info

Show detailed information about a specific mod page, including all files,
versions, Nexus link state, cached update information, and which profiles
the mod appears in.
```bash
modctl mods info "Appearance Menu Mod"
```

Nexus data is read from the local cache only. Run `modctl mods nexus
check-updates` to refresh it.

## Scanning inventory

### mods scan-inventory

Scan archive contents for all mods that have not yet been inventoried.
modctl runs `bsdtar` against each un-inventoried archive and records its
contents in the database. This allows conflict planning and status checks
to operate without re-reading archives from disk.
```bash
modctl mods scan-inventory
```

Inventory scanning runs automatically during `mods import`. Use this command
to backfill archives that were imported with `--skip-inventory`, or to resume
a scan that was interrupted.

## Nexus integration

### mods nexus link

Attempt to link unlinked mods for the current game to their Nexus pages.
Pass `--version-id` to operate on a specific mod file version. Additional
flags can help modctl disambiguate when automatic identification is uncertain.
```bash
modctl mods nexus link
modctl mods nexus link --version-id 42 --nexus-url "https://www.nexusmods.com/cyberpunk2077/mods/790"
```

| Flag                       | Description                                |
|----------------------------|--------------------------------------------|
| `--version-id <id>`        | Operate on a specific mod file version     |
| `--nexus-url <url>`        | Nexus mod page URL                         |
| `--label <label>`          | Nexus file display name to match against   |
| `--file-version <version>` | Nexus file version string to match against |
| `--file-name <name>`       | Exact Nexus filename to match against      |

### mods nexus unlink

Remove the Nexus link from a mod.
```bash
modctl mods nexus unlink "Appearance Menu Mod"
```

### mods nexus check-updates

Check all Nexus-linked mods for the current game for available updates.
Results are cached locally and shown in `modctl profiles status` and
`modctl mods info` without additional API calls.
```bash
modctl mods nexus check-updates
```

| Flag           | Description                                                 |
|----------------|-------------------------------------------------------------|
| `--ignore-ttl` | Force a fresh fetch even if cached data has not yet expired |
| `--force`      | Proceed even if Nexus API quota may be insufficient         |

## Incompatibilities

### mods incompatible add

Flag two mods as incompatible with each other. Incompatibilities are
user-defined; the reason is freeform and entirely up to you. Flagged pairs
are shown as warnings in `modctl profiles status` when both mods are enabled
in the same profile. They never block an apply.
```bash
modctl mods incompatible add "Mod A" "Mod B" --reason "Conflicting economy overhauls"
```

### mods incompatible remove

Remove an incompatibility flag between two mods. The order of the two names
does not matter.
```bash
modctl mods incompatible remove "Mod A" "Mod B"
```

### mods incompatible list

List all incompatibility flags for the current game, regardless of which
profiles the mods appear in.
```bash
modctl mods incompatible list
```
