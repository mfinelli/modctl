# modctl changelog

This document keeps track of notable changes to `modctl`. The format is loosely
based on the [Keep a Changelog](https://keepachangelog.com/en/1.0.0/) format.

`modctl` adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
