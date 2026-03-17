# Utility commands

## auth

Manage authentication with mod hosting services.

### auth nexus login

Authenticate with Nexus Mods using Single Sign-On. Opens your browser to
the Nexus Mods authorization page and waits for approval. The API key is
received and saved automatically once you approve the request.
```bash
modctl auth nexus login
```

In headless environments (SSH sessions, servers) the authorization URL is
printed to the terminal. Open it in a browser on any other device to
complete the flow; modctl will receive the key automatically once you do.

Pass `--force` to replace an existing key.

| Flag      | Description                   |
|-----------|-------------------------------|
| `--force` | Replace an existing API key   |

### auth nexus logout

Remove the stored Nexus Mods API key from the config file. The key remains
active on Nexus Mods; to fully revoke it visit your
[Nexus Mods API settings](https://www.nexusmods.com/users/myaccount?tab=api).
```bash
modctl auth nexus logout
```

### auth nexus status

Confirm your API key is valid and show current rate limit quota. Displays
your Nexus username, remaining daily and hourly requests, and the time until
each quota window resets. This call does not count against your rate limit.
```bash
modctl auth nexus status
```

---

## config

View and modify modctl's configuration without editing the config file by
hand. For a full list of available keys and their defaults see the
[Configuration reference](../../guides/configuration).

### config list

Show all configuration keys with their current effective values. Indicates
whether each value was explicitly set or is inheriting its default.
`nexus.apikey` is masked (only the last few characters are shown).
```bash
modctl config list
```

### config get

Show the effective value for a single key and whether it is set or
defaulting. `config get nexus.apikey` shows the full API key.
```bash
modctl config get tmp_dir
modctl config get nexus.apikey
```

### config set

Set a key in the config file. The file is created if it does not exist.
```bash
modctl config set tmp_dir /var/tmp/modctl
```

To set your Nexus API key manually (for scripted or non-interactive
environments; most users should use `modctl auth nexus login` instead):
```bash
modctl config set nexus.apikey YOUR_API_KEY
```

---

## status

Show a high-level status summary of all known game installs, including the
currently applied profile, number of tool-managed files, backup count, and
any warnings such as incomplete operations or games that are no longer found
on disk.
```bash
modctl status
```

For detailed information about a specific game's active profile (including
the full mod list, priority order, and conflicts) use `modctl profiles
status` instead.

---

## doctor

Run a read-only health check to confirm modctl can operate correctly.
```bash
modctl doctor
```

Doctor checks:

- State directories are present and writable
- The database is present, usable, and has no pending migrations
- SQLite integrity (quick check by default)
- `bsdtar` is installed and working
- All blobs in the store are present, the correct size, and hash correctly

Doctor never modifies your game installs or modctl state. It may read files
to validate integrity.

For a more thorough database check pass `--deep`, which runs a full integrity
check and foreign key check in addition to the default quick check:
```bash
modctl doctor --deep
```

Run `doctor` before exporting if you want to verify blob integrity ahead of
time, and after an unexpected crash to confirm nothing is in an inconsistent
state.

| Flag     | Description                                           |
|----------|-------------------------------------------------------|
| `--deep` | Run full SQLite integrity check and foreign key check |

---

## gc

Remove unreferenced blobs from the blob store to reclaim disk space. A blob
becomes eligible for garbage collection when no database row references it (for
example after a mod file version is removed from all profiles and deleted).
```bash
modctl gc
```

By default `gc` collects both archive and backup blobs and also removes
orphaned on-disk files (files present on disk with no corresponding database
row, which can appear if an import was interrupted before the database entry
was written).

Always a good idea to run with `--dry-run` first to see what would be removed:
```bash
modctl gc --dry-run
```

If a blob is recorded in the database but missing from disk, a warning is
printed. Pass `--clean-missing` to also remove those dangling database rows.
The `doctor` command will surface the same missing blobs independently.

| Flag                   | Description                                                                  |
|------------------------|------------------------------------------------------------------------------|
| `--dry-run`            | Preview what would be removed without making any changes                     |
| `--no-archives`        | Skip archive blob collection                                                 |
| `--no-backups`         | Skip backup blob collection                                                  |
| `--min-age <duration>` | Skip blobs created more recently than this duration (e.g. `7d`, `24h`, `2w`) |
| `--clean-missing`      | Remove database rows for blobs missing from disk                             |
| `--skip-orphans`       | Leave on-disk files with no database row in place                            |

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

Version warnings (if the bundle was produced by a newer version of modctl) are
printed but verify will still report the bundle as valid. Any other issue is
treated as a failure.

---

## extract

Extract raw mod archives from a modctl export bundle without performing a
full import. See [Import & Export](../../import-export) for full details and
examples.

Without `--mod`, lists all mods in the bundle:
```bash
modctl extract ./backup.tar.zst
```

With `--mod`, extracts a specific mod archive:
```bash
modctl extract ./backup.tar.zst --game steam:1091500 --mod "Appearance Menu Mod"
```

| Flag                  | Description                                            |
|-----------------------|--------------------------------------------------------|
| `--game <store-id>`   | Required for full bundles when extracting with `--mod` |
| `--mod <name>`        | Extract a specific mod by mod page name                |
| `--file <label>`      | Narrow selection by file label                         |
| `--version <version>` | Narrow selection by version string                     |
| `--output-dir <path>` | Directory to extract into (default: current directory) |
| `--overwrite`         | Replace existing files (default: skip with a warning)  |

---

## operations

The `operations` commands let you inspect the history of apply and unapply
runs for a game. This is useful for understanding what modctl did during a
previous run, diagnosing unexpected changes on disk, or investigating a
failed operation.

### operations list

List recent apply and unapply operations for the active game. By default
shows the last 20.
```bash
modctl operations list
```

| Flag          | Description                              |
|---------------|------------------------------------------|
| `--all`       | Show operations across all game installs |
| `--limit <n>` | Change the number of operations shown    |

### operations show

Show file-level detail for a specific operation, including every file change
recorded, the action taken, content hashes before and after, and any backup
references.
```bash
modctl operations show
```
