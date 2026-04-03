# Deployment rules

Deployment rules let you change how modctl handles specific files during apply,
on a per-mod basis. There are two kinds: skip-backup patterns and write-once
patterns.

Like remap rules, deployment rules are profile-scoped: the same mod version
can have different rules in different profiles. Patterns are evaluated against
the final remapped destination path (the path that will actually be written
to disk) so if you have remap rules configured, write your patterns against
the remapped result, not the raw archive path.

Only the winning mod's rules apply to a given path. A mod that loses a
conflict has no effect on how its file is handled.

## Skip-backup patterns

By default, modctl backs up any file before overwriting it: the original
game-owned file is saved before the first apply, and if the file is later
modified externally (for example by a game update), that modified version is
saved before being overwritten on the next apply. This is what makes unapply
safe; there is always something to restore.

For some files this behavior is more annoying than useful. Mods that ship
cache files, log files, or other frequently-regenerated content will cause
backups to accumulate on every apply run, consuming disk space and adding
noise to the operation log.

A skip-backup pattern tells modctl never to back up files matching that path
for this mod in this profile:
```bash
modctl profiles deploys skip-backup add "My Mod" "*.cache"
modctl profiles deploys skip-backup add "My Mod" "Cache/*"
```

**What changes with skip-backup active:**

- The initial backup of a pre-existing game-owned file is skipped.
- Drift backups (saving an externally modified file before overwriting it)
  are skipped.
- On unapply, matched paths have no backup to restore and are simply deleted.

The tradeoff is that if a game update writes something meaningful to a
skip-backup path, modctl will overwrite it silently on the next apply and
there will be nothing to restore on unapply. For genuinely ephemeral files
like caches this is fine. For anything the game writes intentionally you
should use write-once instead. If you need to recover a file covered by a
skip-backup pattern, Steam's "verify integrity of game files" will restore it.

## Write-once patterns

Some mods ship config files with sensible defaults that the game then modifies
as you change settings in-game. Without any rules, modctl would overwrite
those in-game changes on every apply run, backing up the modified version
each time, but still replacing it with the mod's original.

A write-once pattern tells modctl to deploy the file on the first apply and
then leave it alone on subsequent applies:
```bash
modctl profiles deploys write-once add "My Mod" "settings.ini"
modctl profiles deploys write-once add "My Mod" "Config/*.cfg"
```

**What write-once does:**

- First apply: the file is deployed normally. If a game-owned file was
  already at that path it is backed up first, so unapply can restore it.
- Subsequent applies: if the file is present on disk and tool-owned, it is
  skipped entirely regardless of whether its content has changed.
- Missing file: if the file has been deleted from disk it is re-deployed.
  Write-once means "don't overwrite changes the game has made", not "deploy
  exactly once and never again".

When drift detection is enabled (the default), modctl will emit an
informational warning if a write-once file has been modified since it was
deployed, so you are aware the file differs from the mod's version. No
action is taken.

## Combining both rules

You can apply both rules to the same path. The result is:

- First apply: the file is deployed, the initial backup is skipped.
- Subsequent applies: the file is skipped if present on disk.
- Unapply: the file is deleted (no backup to restore).

This combination makes sense for files that are both ephemeral and
game-modified, something modctl should seed once and then ignore entirely.

## Viewing and managing rules
```bash
# List current patterns
modctl profiles deploys skip-backup list "My Mod"
modctl profiles deploys write-once list "My Mod"

# Remove a pattern
modctl profiles deploys skip-backup remove "My Mod" "*.cache"
modctl profiles deploys write-once remove "My Mod" "settings.ini"
```

## Copying rules between mod versions

When you manually swap a mod version between two distinct profile items you
can copy deployment rules across:
```bash
modctl profiles deploys skip-backup copy "My Mod v1.0" "My Mod v1.1"
modctl profiles deploys write-once copy "My Mod v1.0" "My Mod v1.1"
```

If the destination already has patterns of that kind they will be replaced.
If the source has none this is a no-op.

Note that when you use `modctl profiles upgrade` to upgrade a mod, deployment
rules are preserved automatically — no copying is needed. The copy commands
are only useful when moving rules between two distinct profile items manually.

---

Next up is the [Configuration reference](../configuration), which covers all
available configuration keys and their defaults.
