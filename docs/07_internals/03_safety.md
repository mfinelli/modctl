# Safety model

modctl is designed to never leave your game directory in an unexpected state.
This page explains the principles behind that guarantee and what modctl does
in practice to uphold it.

## Staging and safe move

modctl never extracts files directly into your game directory. Instead, every
archive is extracted to a temporary staging directory first. Only after the
extraction is complete and the files have been validated does modctl move them
into the game directory one at a time.

This means a failed or interrupted extraction cannot leave a partially extracted
archive scattered across your game directory; if something goes wrong during
staging, your game directory is untouched. However once modctl begins moving
files into the game directory an interrupted apply may leave it in a partial
state. If this happens, run `modctl unapply` to remove all tool-managed files
and return to a clean state, or run `modctl apply` again to complete the
operation.

## Backups

Before modctl overwrites any file it did not originally install (for example
a configuration file that shipped with the game) it makes a backup. Backups
are stored in the content-addressed backup blob store and restored
automatically when you unapply a profile or when a subsequent apply no longer
needs to overwrite them.

This guarantee applies even across applies. If you apply a profile, then apply
a different profile that touches the same game-owned file, modctl will not
overwrite the backup it already made.

## Drift detection

When modctl applies a profile it tracks the exact content of every file it
installs by recording its SHA-256 hash. On subsequent applies, before
overwriting any file it previously installed, modctl hashes the on-disk
content and compares it against what it originally wrote.

If the file has been modified externally (for example by a game update)
modctl detects the change, backs up the updated content, and then overwrites
it with the mod file. Nothing is lost silently.

You can skip drift detection with `modctl apply --no-recheck` for faster
applies when you are confident nothing has changed externally.

## Path validation

Before any file is moved into the game directory modctl validates its
destination path. Paths that attempt to escape the game directory (for example
using `../` traversal), absolute paths, symlinks, and special device files are
all rejected. Only regular files with paths that resolve within the target
directory are permitted.

This means a malicious or malformed mod archive cannot write files outside
your game directory, regardless of what paths it contains.

## Conflict prevention during operations

modctl locks the game install during an apply or unapply to prevent concurrent
modifications. If a previous operation did not complete (for example due to a
crash) modctl will detect the incomplete operation and refuse to proceed until
you explicitly resolve it. This prevents a second run from making changes on
top of an unknown partial state.

## What modctl does not do

It is worth being clear about the limits of these guarantees.

modctl tracks only the files it installs. Files you modify manually in your
game directory, outside of modctl, are not tracked and will not be backed up
unless they happen to be overwritten by a subsequent apply.

modctl also has no knowledge of what your game considers valid. It can ensure
your game directory is in a known, reproducible state, but it cannot verify
that the mods you have installed actually work correctly together or that they
are compatible with your version of the game.
