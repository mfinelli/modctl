# Quick Start

This guide walks you through setting up modctl for the first time: initializing
your local state, discovering your Steam library, importing a mod, and applying
it to a game. By the end you will have a working mod profile and a clear picture
of the basic workflow.

If you haven't installed modctl yet, start with the [Installation](../installation)
guide first.

# 1. Initialize modctl

The first thing to do after installing is initialize modctl's local state. This
creates the data directories modctl needs and sets up the internal database:
```bash
modctl init
```

This command is safe to run multiple times; it will never overwrite existing
data. If you have run it before, running it again will apply any pending
database upgrades and exit cleanly.

## 2. Discover your games

Next, tell modctl to scan your Steam library and find your installed games:
```bash
modctl games refresh
```

modctl will read your Steam library configuration, find every installed game,
and record it. You can run this command any time — for example after installing
a new game or moving your library to a different drive. Games that are
temporarily missing (unplugged drive, moved library) are never deleted from
modctl's records; your mod setup is always preserved.

To see what was found:
```bash
modctl games list
```

## 3. Set your active game

Most modctl commands operate on your currently active game, so you don't have
to specify it every time. Set it once and modctl remembers it:
```bash
modctl games set-active "Cyberpunk 2077"
```

You can switch the active game at any time by running this command again.

## 4. Import a mod

Download a mod archive from Nexus Mods or wherever you get your mods, then
import it into modctl:
```bash
modctl mods import ~/Downloads/Appearance\ Menu\ Mod-790-2-12-5-1749642728.zip
```

modctl copies the archive into its local store, scans its contents, and records
everything in the database. The original file in your Downloads folder is left
untouched unless you pass `--rm` to remove it after a successful import.

If you're importing from Nexus Mods, pass the mod page URL and modctl will
link the archive automatically:
```bash
modctl mods import ~/Downloads/Appearance\ Menu\ Mod-790-2-12-5-1749642728.zip \
  --nexus-url "https://www.nexusmods.com/cyberpunk2077/mods/790"
```

Note that modctl does not resolve dependencies. If a mod requires other mods or
frameworks to work (for example, Appearance Menu Mod requires Cyber Engine
Tweaks) you will need to import and add each one to your profile yourself.
Check the mod's page for any listed requirements before installing.

To see your imported mods:
```bash
modctl mods list
```

## 5. Add the mod to your profile

When modctl discovers a game for the first time it creates a **Default**
profile automatically. Add your imported mod to it:
```bash
modctl profiles add "Appearance Menu Mod"
```

To see the current state of your profile:
```bash
modctl profiles status
```

## 6. Apply the profile

Nothing has been written to your game directory yet. Before applying, it's a
good idea to preview what modctl will do with `--dry-run`:
```bash
modctl apply --dry-run
```

When you're happy with the plan, apply it for real:
```bash
modctl apply
```

modctl will back up any game-owned files that would be overwritten, then
extract the winning mod files into your game directory.

---

## What's next

With mods installed you might want to:

- Add more mods to your profile and adjust their priority order: see
  [Profiles and priority ordering](../guides/profiles)
- Understand what happens when two mods touch the same file: see
  [Conflict resolution](../guides/conflicts)
- Link your mods to their Nexus pages to check for updates: see
  [Nexus integration](../guides/nexus)
- Export your mod setup to take it to another machine or share it with a
  friend: see [Import & Export](../import-export)
