# Import & Export

modctl can pack your entire mod setup into a single portable bundle. Use it to
back up your configuration, migrate to a new machine, or share a game's mod
setup with a friend.

## Bundle contents

A bundle is a zstd-compressed tar archive containing:

- a snapshot of the modctl database
- all referenced mod archives and backup blobs
- a manifest with metadata and integrity checksums

Everything modctl needs to restore your setup is self-contained in the bundle.
The Nexus API cache is not included; it is safe to discard and will be
repopulated automatically when needed.

## Exporting

### Full export

A full export includes all games, profiles, mods, and blobs:
```bash
modctl export
```

The output file is named `modctl-export-<date>.tar.zst` by default and written
to the current directory. Use `--output` to specify a different path:
```bash
modctl export --output ~/backups/modctl-backup.tar.zst
```

### Game-scoped export

To export only the data for a single game, pass its store ID with `--game`:
```bash
modctl export --game steam:1091500
```

The store ID for a game is shown by `modctl games list`. A game-scoped bundle
contains only that game's profiles, mods, and blobs — no other games' data is
included. The default output filename for a game-scoped export is
`modctl-export-<slug>-<date>.tar.zst`.

Game-scoped exports are the right choice for sharing a mod setup with someone
else. They're smaller and contain exactly what the recipient needs to get
started with that game.

### A note on applied state

A game-scoped bundle does not capture the current state of your game directory
— only modctl's records of mods and profiles. The applied profile is
intentionally cleared in the bundle, since the destination machine will have
its own game installation. After importing, the recipient will need to run
`modctl apply` to deploy the mods.

### Skipping inventory

By default the bundle includes the archive inventory (modctl's cached record
of every file inside each mod archive). This speeds up the first `apply` on the
destination machine. If you are exporting many large archives and want a
smaller bundle, pass `--skip-inventory` to omit it. Archives without inventory
can be scanned later with `modctl mods scan-inventory`.

## Importing

### Importing a full bundle

To restore from a full bundle on a fresh installation:
```bash
modctl import modctl-backup.tar.zst
```

The destination database must be empty (beyond the initial setup rows created
by `modctl init`). If you want to restore into an existing installation and
wipe what's there, pass `--force`:
```bash
modctl import --force modctl-backup.tar.zst
```

By default, importing a full bundle clears all on-disk state: installed
files, backups, and operation history are removed and applied profile state
is reset. This means the destination machine starts clean, and you will need
to run `modctl apply` to deploy mods after importing. This is the right
behavior when migrating to a new machine.

### Restoring to the same machine

If you are restoring a backup to the same machine where your game directories
are still intact, pass `--same-machine` to preserve all state verbatim:
```bash
modctl import --same-machine modctl-backup.tar.zst
```

`--same-machine` and `--force` are independent and can be combined:
```bash
modctl import --force --same-machine modctl-backup.tar.zst
```

`--same-machine` is not valid for game-scoped bundles.

### Importing a game-scoped bundle

Importing a game-scoped bundle works the same way and adds the game to your
existing modctl installation without touching any other games:
```bash
modctl import cyberpunk-backup.tar.zst
```

If that game already exists in your database, the import will refuse to
proceed unless you pass `--force`, which will overwrite the existing game and
all its data.

### Importing a single game from a full bundle

You can import a single game out of a full bundle by passing `--game`:
```bash
modctl import --game steam:1091500 modctl-full-backup.tar.zst
```

modctl will extract only the relevant data for that game and add it to your
existing installation. This is useful if you want to restore one game's setup
without affecting anything else.

### After importing

Import never applies any profiles automatically. Once the import completes,
set the active profile and deploy your mods:
```bash
modctl profiles set-active "My Mods"
modctl apply
```

### Previewing an import

Use `--dry-run` to see what would be imported without making any changes:
```bash
modctl import --dry-run cyberpunk-backup.tar.zst
```

This is useful for inspecting an unfamiliar bundle before committing to the
import.

---

## Verifying a bundle

The `verify` command checks the integrity of a bundle without importing it:
```bash
modctl verify cyberpunk-backup.tar.zst
```

This is useful for checking a stored backup for corruption before you actually
need to use it.

## Extracting mod archives from a bundle

The `extract` command lets you pull raw mod archives out of a bundle without
performing a full import. This is useful if you want to re-import a specific
mod into a different installation, or recover an archive you no longer have
on disk.

Without `--mod`, `extract` lists all mods in the bundle grouped by game and
mod page:
```bash
modctl extract ./backup.tar.zst
```

To extract a specific mod, pass `--mod` with the mod page name. For full
bundles you must also specify `--game`:
```bash
modctl extract ./backup.tar.zst --game steam:489830 --mod "Unofficial Skyrim Patch"
```

For game-scoped bundles `--game` is not required:
```bash
modctl extract ./game-export.tar.zst --mod "Appearance Menu Mod" --output-dir ~/mods/
```

If a mod page has multiple files or versions, use `--file` and `--version` to
narrow the selection. If either flag is omitted and only one option exists it
is selected automatically; otherwise the command lists the available options
and exits.
```bash
modctl extract ./backup.tar.zst \
  --mod "Cyber Engine Tweaks" \
  --file "Main File" \
  --version "1.2.3"
```

Extracted files are named using the original filename where available. If the
original filename was not recorded, the file is named by its sha256 prefix with
the archive format used as the extension.
