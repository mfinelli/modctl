# modctl changelog

This document keeps track of notable changes to `modctl`. The format is loosely
based on the [Keep a Changelog](https://keepachangelog.com/en/1.0.0/) format.

`modctl` adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## unreleased

### Changes

- Steam library instance ids are now stable. A library keeps the instance id
  (`default`, `library_2`, ...) that its installs already have, so adding,
  removing or unplugging a library no longer changes which install is which,
  or what is attached to it (profiles, mods, installed files). A new library is
  given an id that no install has had, and the library in the Steam
  installation is only the `default` when no install has that id yet. This
  finishes the change to the `default` library in v0.8.0. If your installs
  were already affected by it they are not put back: they are kept as they are
  now.
- Steam libraries are now recognized by the content id that Steam gives them
  (`contentid` in `libraryfolders.vdf`), so a library that moves to another
  path, for instance a disk that is mounted somewhere else, keeps its instance
  id, its installs, and what is attached to them, and `games refresh` says that
  it moved. A library that has no content id, or shares it with another, is
  recognized by its path as before. Installs that are already known get the
  content id of their library the next time they are found, and keep their
  instance.
- `apply` now warns when a file that a mod installs is a symlink in the game
  directory. The symlink is replaced by a regular file (what it points to is
  not changed), and `unapply` can't bring the symlink back.

## v0.8.0 - 2026-10-10

This is another pre-release that adds a few new features. It's also a very
large release with a lot of internal refactoring to increase code quality,
increase test coverage, and resolve lingering tasks.

**BREAKING CHANGE**: if you have more than one steam library you could be
affected by the last item in the changes list below, please read it carefully.
If you only have a single, default steam library then you are **NOT**
affected.

### Changes

- Add the `strip_whitespace` remap rule to remove leading and trailing
  whitespace from every path segment.
- Fix archive inventory scans dropping leading/trailing and repeated
  whitespace in entry paths. Archives inventoried before this fix keep the
  incorrect paths until rescanned with `modctl mods scan-inventory --rescan`.
  Overrides created against a file whose path was affected will no longer
  match their base file after the rescan.
- Add `mods scan-inventory --rescan` to re-read every archive and replace its
  recorded inventory.
- Add `mods notes set|clear` to attach freeform notes to a mod page. Notes are
  shown by `mods info`.
- Add `games targets disable|enable` to turn an install target off for games
  that only use one of `game_dir`/`proton_prefix`. Apply skips disabled
  targets, and a target can't be disabled while files are installed to it,
  profile items or overrides use it, or it is the only enabled target.
- `profiles add` and `profiles preview` default to the only enabled target
  when `game_dir` is disabled, and refuse disabled targets.
- Apply and unapply output now shows which target is being processed.
- Import, `verify` and `extract` now work with bundles exported by older
  versions: the bundle's database is migrated to the current schema in a
  temporary copy before it is read.
- `verify` now warns when a bundle's database schema is newer than this
  version of modctl supports, instead of only comparing modctl versions.
- `games list` now marks the active game install with an asterisk (`*`).
- `games backups restore` now requires `--force` when the file on disk can't
  be read to check it for drift, instead of silently skipping the check.
- `apply`, `unapply`, `profiles preview` and `games backups diff` now stop
  with an error when a file can't be examined (for example because of its
  permissions), instead of treating it as missing.
- Fix files restored from backups by `apply` and `unapply` being written with
  mode `0600`; they now get `0644`.
- `extract`, `import` and `games backups restore` now write their files
  atomically: a failure or Ctrl-C no longer leaves a partly written file
  behind, and an existing file is only replaced once the copy is complete.
- `mods list` now shows the latest version of Nexus-linked mods from the local
  Nexus cache, and marks the ones that have an update to import.
- The Steam library that is part of the Steam installation is now the
  `default` instance, instead of the first library in order of path, so adding
  another library no longer changes which installs are `default`. The other
  libraries are still numbered (`library_2`, `library_3`, ...) in order of
  path, so adding or removing one can still renumber the ones after it. Installs
  are identified by game and instance, so if your `default` library was not the
  one in the Steam installation, the next `games refresh` will match your
  existing installs, and so the profiles and mods attached to them, to
  different libraries. Most likely the next release will make these ids stable,
  by keeping the id that a library already has.

## v0.7.1 - 2026-10-03

This is another pre-release that fixes some bugs found during testing.

### Changes

- Fix mod file versions shell completions.
- Fix segfault when planning with a new override file (i.e., a new file that
  doesn't override an existing game file).
- Fix game-scoped exports that include overrides.
- Fix profile remap preview error.
- Fix `profiles disable` erroring when a mod has multiple versions but only
  one is enabled.

## v0.7.0 - 2026-04-14

This is another pre-release that adds new features.

### Changes

- Start including the nexus cache in export bundles.
- Add `games notes` to save arbitrary freeform text to game installs.

## v0.6.0 - 2026-04-03

This is another pre-release that adds new features.

### Changes

- Support multiple targets: auto-discover the proton prefix and allow arbitrary
  custom-path targets.
- Add mod deploy rules: skip-backup and write-once.
- Add backups management commands.
- Add `profiles preview` to see the file contents that would be written on
  apply.
- Add `--all` flag to profiles `enable` and `disable`.
- Add progress output to nexus check-updates command.

## v0.5.0 - 2026-03-27

This is another pre-release that adds a few new features based on testing.

### Changes

- Add `--show-conflicts` option to `apply` command to print detailed file
  conflict information.
- Add `--compact` flag to `profiles status` command for a shorter
  one-line-per-mod output.

## v0.4.0 - 2026-03-21

This is another bugfix release leading up to the stable release.

**BREAKING CHANGE**: the internal database schema has been modified without
a corresponding migration. You'll need to unapply any profiles you have active
and then delete the local database before updating and then initialize it
again afterwards. Hopefully this is the last time that it's necessary.

### Changes

- Add a `profiles upgrade` command to help updating an existing mod in the
  load order.
- Add a `--show-inventory` flag to `mods info` to see which files are contained
  in a mod archive.

## v0.3.0 - 2026-03-19

This will hopefully be the final pre-release (barring other bugfixes or issues
that turn up during testing) before cutting a stable release.

**BREAKING CHANGE**: the internal database schema has been modified without
a corresponding migration. You'll need to unapply any profiles you have active
and then delete the local database before updating and then initialize it
again afterwards. I don't expect this to be necessary again, but will can't
make any promises until we reach a stable 1.0 release.

### Changes

- `apply` and `unapply` commands now clean up emptied directories if
  `--prune-dirs` is passed.
- Add `profiles remap preview` command to preview remap rule behavior without
  a full planner dry-run.
- Rework import/export to presume full export is used on a new machine (pass
  `--same-machine` for old behavior).
- Update import to allow importing a single game from a full export bundle.
- Implement SSO login for Nexus mods.
- Update `doctor` to check/verify installed files.
- Make cache directory configurable.
- Implement overrides system (full file and patches)

## v0.2.0 - 2026-03-15

This is another pre-release that fixes issues found during testing.

### Changes

- Save a canonicalized Nexus URL if passed during mod import
- When linking mods during import save the version from the nexus (if not
  provided manually)
- Allow resolving games by name (in addition to ID and selector)
- Add the `mods remove` command

## v0.1.1 - 2026-03-14

This is another pre-release leading up to the final initial stable release.

### Changes

- Add `Application-Version` and `Application-Name` headers to requests made
  to the Nexus API
- Fix apt/rpm repository packaging errors
- Fix default profile creation when refreshing games

## v0.1.0 - 2026-03-14

This is a pre-release to test initial functionality and distribution channels.
