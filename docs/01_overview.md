# Overview

modctl is a command-line mod manager for Linux. It installs mods by extracting
archives into your game directory and keeping a precise record of every file it
touches. That record is what makes safe uninstalls, conflict resolution, and
profile switching possible.

If you have used a mod manager on Windows before, the core ideas will feel
familiar. If you haven't, think of modctl like a package manager: the same
way `apt` or `pacman` tracks what files a package owns so it can cleanly
remove them later, modctl tracks what files each mod owns so you're never left
with a broken game directory.

The sections below walk through the main concepts behind modctl — how it thinks
about games, mods, and profiles — to help you build a mental model before you
start using it. If you'd rather learn by doing, feel free to jump straight to
[Installation](../installation) or the [Quick Start](../quickstart) guide and
come back here when something needs more context.

## Stores and game discovery

A **store** is a source of game installations. modctl currently supports Steam,
with support for Heroic, Lutris, and GOG planned for the future.

Rather than asking you to manually enter installation paths or app IDs, modctl
discovers your games automatically by reading your Steam library. Run
`modctl games refresh` and modctl will find every installed game and record it.
From that point on you refer to games by name, not by path.

If a game moves to a different drive or library, refreshing will update its
location automatically. modctl never deletes a game's configuration just
because it wasn't found during a refresh; your profiles and mod setup are
always preserved.

## Games and installs

modctl distinguishes between a **game** (a title, like Cyberpunk 2077) and a
**game install** (a specific installation of that title in a specific store).
In practice most people have one install per game, but the distinction matters
for the future when multiple stores are supported.

Each game install has one or more **targets**: named locations where mods can
be deployed. The `game_dir` target (the game's installation directory) is
always present. For games that run under Proton, modctl also creates a
`proton_prefix` target automatically, pointing at the Wine C: drive root
inside the Proton prefix. For games that expect mods in a specific
subdirectory you can define additional, arbitrary named targets. See the
[games command reference](../commands/games-stores#targets) for details.

## Mods

modctl organises mods in three levels:

- A **mod page** represents a mod project (equivalent to a page on Nexus Mods).
  It has a name and optionally a link to its Nexus page for update checking.
- A **mod file** is a downloadable file under a mod page, for example "Main
  File" or "Optional - 2K Textures". A mod page can have multiple files.
- A **mod file version** is a specific archive you have imported. When a mod
  author uploads a new version of a file, that becomes a new mod file version.

This mirrors the way Nexus Mods organises things, which makes linking mods for
update checking straightforward.

When you import a mod archive with `modctl mods import`, modctl stores the
archive in a local content-addressed store and records its contents. Your
archives are never modified and never deleted unless you explicitly remove them.

## Profiles and priority

A **profile** is a named set of mods for a game, along with a priority order.
You can have as many profiles as you like: a "vanilla playthrough" profile,
a "graphics overhaul" profile, a "testing" profile, and so on.

When two mods in a profile provide the same file, **priority order** (also
known as load order) decides which one wins. Higher priority mods take
precedence. You set the order yourself; modctl never silently resolves
conflicts on your behalf.

Only one profile can be active per game at a time. Switching profiles is a
single command — modctl works out what needs to change and applies only the
differences.

## Apply and reconciliation

Enabling or disabling mods, changing priority, and switching profiles are all
cheap operations that only change modctl's internal state. Nothing is written
to your game directory until you run `modctl apply`.

When you apply a profile, modctl compares the desired state (what the profile
says should be installed) against the current state (what is actually on disk)
and reconciles the difference. Only the files that need to change are touched.
You can always preview what will happen first with `--dry-run`.

## Backups and rollback

Before modctl overwrites any file it did not originally install (for example
a configuration file that shipped with the game) it makes a backup. Backups
are stored in the same content-addressed store as your mod archives.

When you unapply a profile or uninstall a mod, any backed-up files are
restored automatically. Your game directory is returned to exactly the state
it was in before modctl touched it.

If a file has been modified externally since modctl last wrote it (for
example by a game update) modctl will detect the change and back up the
updated file before overwriting it, so nothing is lost.

---

With these concepts in mind you're ready to move on to
[Installation](../installation) and the [Quick Start](../quickstart) guide.
