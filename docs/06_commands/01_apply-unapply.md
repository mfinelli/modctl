# apply and unapply

## apply

`modctl apply` deploys the active profile to your game directory. It computes
the desired file state for the profile, compares it against what is currently
on disk, and reconciles the difference. Only files that need to change are
touched; files that are already correctly deployed are skipped automatically.
```bash
modctl apply
```

Before any game-owned file is overwritten, modctl backs it up automatically.
Backups are restored when you run `modctl unapply` or when a subsequent apply
no longer needs to overwrite them.

### Previewing changes

Always a good idea before applying, especially after making several profile
changes at once:
```bash
modctl apply --dry-run
```

This runs the full planning step and prints every file operation that would be
performed, without touching anything on disk.

### Drift detection

By default, before overwriting any file it previously installed modctl hashes
the on-disk content and compares it against what it originally wrote. This
detects external modifications (for example, a game update that overwrote a
modded file) and backs up the updated content before overwriting it, so
nothing is lost.

If you have many mods and want a faster apply and are not concerned about
external modifications, you can skip these hash checks:
```bash
modctl apply --no-recheck
```

### Output format

By default apply uses a progress indicator. To print each file operation on
its own line instead — useful for scripting or when you want a full record of
what was done:
```bash
modctl apply --print-ops
```

### Cleaning up empty directories

modctl only manages files, not directories. Directories created implicitly
during apply are not removed automatically when their contents are uninstalled.
If you want modctl to clean up any directories it emptied during the operation,
pass `--prune-dirs`:
```bash
modctl apply --prune-dirs
```

Directories that still contain files modctl did not install are left alone.

### Flags

| Flag             | Description                                                                                |
|------------------|--------------------------------------------------------------------------------------------|
| `--dry-run`      | Preview the plan without making any changes                                                |
| `--no-recheck`   | Skip on-disk hash checks (faster but will not detect or back up externally modified files) |
| `--print-ops`    | Print each file operation on its own line instead of using a progress indicator            |
| `--keep-staging` | Keep the staging directory after apply (useful for debugging)                              |
| `--prune-dirs`   | Remove empty directories left behind after file removals                                   |

## unapply

`modctl unapply` removes all files that modctl installed and restores any
backed-up game files. The game directory is returned to the state it was in
before modctl touched it.
```bash
modctl unapply
```

unapply operates on the actual installed state recorded in modctl's database,
not on a profile. This means it works correctly even if the profile that
produced the current install has since been modified or deleted; modctl
always knows exactly which files it owns and where they are.

### Previewing changes
```bash
modctl unapply --dry-run
```

### Output format
```bash
modctl unapply --print-ops
```

### Cleaning up empty directories
modctl only manages files, not directories. To remove any directories that are
left empty after unapply completes, pass `--prune-dirs`:
```bash
modctl unapply --prune-dirs
```

Directories that still contain files modctl did not install are left alone.

### Flags

| Flag           | Description                                                                     |
|----------------|---------------------------------------------------------------------------------|
| `--dry-run`    | Preview the plan without making any changes                                     |
| `--print-ops`  | Print each file operation on its own line instead of using a progress indicator |
| `--prune-dirs` | Remove empty directories left behind after file removals                        |

---

For a conceptual overview of how apply and reconciliation work, see
[Overview](../../overview).
