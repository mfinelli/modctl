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
