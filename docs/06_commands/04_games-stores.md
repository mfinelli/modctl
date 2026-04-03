# games and stores

## games

Manage discovered game installs. Run `modctl games refresh` to scan your
stores and populate the list before using other game commands.

Most modctl commands operate on the active game by default. Set it once with
`games set-active` and you won't need to specify it again unless you switch
games.

### games refresh

Scan all enabled stores and update the list of discovered game installs.
Detects new installs, updates paths for games that have moved, and marks
games that are no longer found as not present. Your profiles and mod
configuration are never deleted when a game goes missing.
```bash
modctl games refresh
```

Some internal Steam titles are filtered automatically and never added to
the database, as they are not moddable games: Proton Experimental, Steam
Linux Runtime, and Steamworks Common Redistributables.

During refresh, modctl also creates or updates install targets for each
game. The `game_dir` target is always created. For games that run under
Proton, a `proton_prefix` target is also created automatically, pointing
at the Wine C: drive root inside the Proton prefix. User-defined targets
are never overwritten by refresh.

Safe to run at any time and as often as you like.

### games list

List all discovered game installs. By default shows only games from the
active store.
```bash
modctl games list
```

| Flag              | Description                |
|-------------------|----------------------------|
| `--all`           | Show games from all stores |
| `--store <store>` | Filter by a specific store |

### games info

Show detailed information about a specific game install. You can identify
the game by its numeric install ID, a store selector, or its title:
```bash
modctl games info "Cyberpunk 2077"
modctl games info steam:1091500
```

If multiple installs exist for the same game, you must specify the instance
explicitly:
```bash
modctl games info steam:1091500#default
```

### games set-active

Set the active game install used by modctl commands. Accepts a numeric
install ID, a store selector, or a game title:
```bash
modctl games set-active "Cyberpunk 2077"
modctl games set-active steam:1091500
```

If the title matches more than one install, modctl will list the
candidates and ask you to be more specific using a selector.

If multiple installs exist for the same game you must specify the instance
explicitly.

## Targets

Each game install has one or more named targets: locations where mods can
be deployed. Targets are the bridge between a profile item and the
filesystem; when you add a mod to a profile you specify which target it
deploys to.

Two targets are managed automatically by modctl:

- `game_dir`: the game's installation directory. Present for every game.
- `proton_prefix`: the Wine C: drive root inside the Proton prefix
  (`compatdata/<appid>/pfx/drive_c`). Created automatically during refresh
  for games that run under Proton. Not present for native Linux games.

For games that expect mods in a specific subdirectory (for example, a Unity
mod manager folder deep inside the Proton prefix), define a named custom
target once and all mods that belong there can reference it by name without
remap rules.

### games targets list

List all install targets for the current game, showing the name, root path,
and whether each target was auto-discovered or user-defined.
```bash
modctl games targets list
```

### games targets add

Add a user-defined install target. The path must be absolute, or relative
to an existing target using `--relative-to`.
```bash
modctl games targets add saves ~/.local/share/MyGame/saves
```

Use `--relative-to` to build on top of an existing target without needing
to know the full path. The path is resolved to an absolute path at creation
time. If the base target moves later, this target will not update
automatically.
```bash
modctl games targets add unitymodmanager \
  "users/steamuser/AppData/LocalLow/Owlcat Games/Rogue Trader/UnityModManager" \
  --relative-to proton_prefix
```

| Flag                   | Description                                                 |
|------------------------|-------------------------------------------------------------|
| `--relative-to <name>` | Resolve the path relative to an existing target's root path |

### games targets remove

Remove a user-defined install target. Refuses if any files are currently
installed to that target (unapply the profile first). Auto-discovered
targets (`game_dir`, `proton_prefix`) cannot be removed.
```bash
modctl games targets remove saves
```

## Backups

modctl automatically backs up game-owned files before overwriting them during
apply. These backups are restored automatically on unapply. The backup commands
let you inspect and manage individual backup entries without running a full
unapply.

Backups are scoped to the game install rather than a specific profile, since
they describe the pre-mod state of the filesystem regardless of which profile
caused the overwrite.

### games backups list

List all backed-up files for the current game. Shows the target, path, size,
when the backup was taken, and which operation created it.
```bash
modctl games backups list
```

Pass `--target` to filter to a specific install target:
```bash
modctl games backups list --target proton_prefix
```

| Flag              | Description                                     |
|-------------------|-------------------------------------------------|
| `--target <name>` | Filter by install target (default: all targets) |

### games backups view

Print the content of a backed-up file to the terminal. The path is relative
to the target root.
```bash
modctl games backups view "settings/game.ini"
```

Binary files are detected automatically and refused. Pass `--force` to print
them anyway.

| Flag              | Description                          |
|-------------------|--------------------------------------|
| `--target <name>` | Install target (default: `game_dir`) |
| `--force`         | Print binary files without refusing  |

### games backups delete

Delete a backup entry. The blob is not immediately removed from disk; run
`modctl gc` to reclaim space.
```bash
modctl games backups delete "settings/game.ini"
```

Note that deleting a backup means modctl cannot restore the original file at
this path on unapply; the file will be deleted instead of restored.

| Flag              | Description                          |
|-------------------|--------------------------------------|
| `--target <name>` | Install target (default: `game_dir`) |

### games backups restore

Restore a backed-up file to disk immediately without running a full unapply.
Useful for reverting a single file to its pre-mod state while leaving
everything else in place.
```bash
modctl games backups restore "settings/game.ini"
```

If the active profile is currently applied, modctl warns that running apply
again will overwrite this path. If you want the restored file to be
preserved across future applies, add a write-once or skip-backup rule for
this path.

If the on-disk file has drifted from what modctl last installed, `--force`
is required to proceed.

| Flag              | Description                                                             |
|-------------------|-------------------------------------------------------------------------|
| `--target <name>` | Install target (default: `game_dir`)                                    |
| `--force`         | Restore even if the on-disk file has drifted from what modctl installed |

### games backups diff

Show a unified diff between the backed-up content and the current on-disk
file. Useful for understanding what has changed since the backup was taken,
for example to decide whether to restore the backup or add a deploy rule.
```bash
modctl games backups diff "settings/game.ini"
```

If the on-disk file is missing, the backup content is shown as a full
deletion with a warning. Binary files are detected automatically and refused
unless `--force` is passed.

| Flag              | Description                          |
|-------------------|------------------------------------- |
| `--target <name>` | Install target (default: `game_dir`) |
| `--force`         | Diff binary files without refusing   |

---

## stores

Manage configured stores. A store is a source of game installations such as
Steam. Only enabled stores are scanned during `games refresh`.

Steam is the only supported store in the current version. It is configured
and enabled automatically when you run `modctl init`, so most users will
never need to interact with the `stores` commands directly.

### stores list

Display all configured stores and whether they are enabled.
```bash
modctl stores list
```

### stores set-active

Set the active store used by commands that accept a `--store` flag. When an
active store is set those commands can omit the flag entirely.
```bash
modctl stores set-active steam
```
