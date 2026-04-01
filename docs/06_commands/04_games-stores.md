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
