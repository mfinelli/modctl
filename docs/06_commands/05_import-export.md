# Import & Export commands

## export

Export modctl state to a portable bundle. See [Import & Export](../../import-export)
for a full overview of the export and import workflow.

By default performs a full export of all games, mods, profiles, and blobs:
```bash
modctl export
```

To scope the export to a single game install:
```bash
modctl export --game steam:1091500
```

The output filename defaults to `modctl-export-<date>.tar.zst` for full
exports and `modctl-export-<slug>-<date>.tar.zst` for game-scoped exports.
Use `--output` to override:
```bash
modctl export --game steam:1091500 --output ~/backups/cyberpunk.tar.zst
```

Game-scoped bundles do not include backup blobs. Backups describe on-disk
state on the source machine and have no meaning on the destination.

By default all blobs are hashed and verified before the bundle is written.
Pass `--no-verify` to skip this step if you have already verified integrity
with `modctl doctor` and want a faster export.

| Flag                  | Description                                                        |
|-----------------------|--------------------------------------------------------------------|
| `--game <store-id>`   | Scope the export to a single game install                          |
| `--output <path>`     | Output file path (default: `modctl-export-<date>.tar.zst`)         |
| `--skip-inventory`    | Omit archive inventory entries from the bundle                     |
| `--no-verify`         | Skip blob integrity verification before exporting (not recommended)|

---

## import

Import a modctl export bundle. Accepts both full and game-scoped bundles.
See [Import & Export](../../import-export) for a full overview of the
workflow and common scenarios.
```bash
modctl import <bundle>
```

### Full bundles

The destination database must be empty (beyond the initial setup rows created
by `modctl init`). Pass `--force` to wipe an existing installation and
restore into it:
```bash
modctl import --force modctl-backup.tar.zst
```

By default a full import clears all on-disk state so the destination machine
starts clean: installed files, backups, and operation history are removed and
applied profile state is reset on all games. Pass `--same-machine` to restore
all state verbatim instead, which is only appropriate when restoring to the
same machine with game directories intact:
```bash
modctl import --same-machine modctl-backup.tar.zst
```

`--force` and `--same-machine` are independent and can be combined.

### Game-scoped bundles

A game-scoped bundle is imported into an existing installation without
touching any other games. The game must not already exist unless `--force`
is passed:
```bash
modctl import cyberpunk-backup.tar.zst
modctl import --force cyberpunk-backup.tar.zst
```

`--same-machine` is not valid for game-scoped bundles.

### Importing a single game from a full bundle

Pass `--game` to extract only one game's data from a full bundle:
```bash
modctl import --game steam:1091500 modctl-full-backup.tar.zst
```

modctl will print an informational message and run the game-scoped import
path, adding only that game to your existing installation.

If `--game` is passed with a game-scoped bundle and matches the bundle's
game, a warning is printed and the flag is ignored. If it does not match,
the command errors.

### After importing

Import never applies any profiles automatically. Once the import completes,
set the active profile and deploy your mods:
```bash
modctl profiles set-active "My Mods"
modctl apply
```

| Flag                      | Description                                                                  |
|---------------------------|------------------------------------------------------------------------------|
| `--force`                 | Overwrite existing data (wipe DB for full, overwrite game for game-scoped)   |
| `--dry-run`               | Preview what would be imported without making any changes                    |
| `--skip-inventory`        | Skip scanning archives that have no inventory in the bundle                  |
| `--game <store:game-id>`  | Import only this game from a full bundle                                     |
| `--same-machine`          | Restore all state verbatim, including installed files and operation history   |

---

## verify

Check the integrity of a modctl export bundle without importing it. Useful
for validating a stored backup before you need to rely on it.
```bash
modctl verify cyberpunk-backup.tar.zst
```

Checks performed:

- Bundle manifest checksum matches the database snapshot
- SQLite quick check and foreign key check on the bundle database
- Every blob in the bundle hashes correctly against its filename
- Every blob referenced in the database has a corresponding file in the bundle
- Every file in the bundle has a corresponding database row

Version warnings (if the bundle was produced by a newer version of modctl)
are printed but do not affect the result. Any other issue causes a non-zero
exit.

---

## extract

Extract raw mod archives from a modctl export bundle without performing a
full import. Useful for recovering a specific archive or re-importing a mod
into a different installation.

Without `--mod`, lists all mods in the bundle grouped by game and mod page:
```bash
modctl extract ./backup.tar.zst
```

With `--mod`, extracts a specific mod archive. For full bundles `--game` is
also required:
```bash
modctl extract ./backup.tar.zst --game steam:1091500 --mod "Unofficial Skyrim Patch"
modctl extract ./game-export.tar.zst --mod "Appearance Menu Mod" --output-dir ~/mods/
```

If a mod page has multiple files or versions, use `--file` and `--version`
to narrow the selection. If either is omitted and only one option exists it
is selected automatically; otherwise the available options are listed and
the command exits.

Extracted files are named using the original filename where available. If
the original filename was not recorded, the file is named by its sha256
prefix with the detected archive format as the extension.

| Flag                  | Description                                            |
|-----------------------|--------------------------------------------------------|
| `--game <store-id>`   | Required for full bundles when extracting with `--mod` |
| `--mod <name>`        | Extract a specific mod by mod page name                |
| `--file <label>`      | Narrow selection by file label                         |
| `--version <version>` | Narrow selection by version string                     |
| `--output-dir <path>` | Directory to extract into (default: current directory) |
| `--overwrite`         | Replace existing files (default: skip with a warning)  |
