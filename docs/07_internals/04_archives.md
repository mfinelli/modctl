# Archive handling

modctl uses `bsdtar` for all archive operations. `bsdtar` supports a wide
range of archive and compression formats and detects the format automatically
from the file contents rather than the filename, so you do not need to worry
about which format a mod author used.

## Import

When you run `modctl mods import`, modctl first asks `bsdtar` to list the
archive contents to validate that it can be read. The archive is then copied
into the content-addressed archive store unchanged. The original file is left
untouched unless you passed `--rm`.

The archive is scanned at import time to record its contents in the database.
This inventory is what allows conflict detection and `modctl profiles status`
to operate without reading archives from disk on every run.

## Non-archive files

If `bsdtar` cannot recognise the file as a supported archive format, modctl
treats it as a plain file rather than an error. The file is wrapped in a
`.tar.gz` archive and that wrapper is what gets stored in the archive store.

When the mod is applied the wrapper is extracted, placing the original file
at the root of the game directory (subject to any remap rules you have
configured). This means modctl can manage arbitrary files as mods (not just
archives) which can be useful for adding individual configuration files or
other standalone files to a profile.

## Extraction and staging

When you run `modctl apply`, modctl extracts each winning mod archive to a
staging directory before touching your game directory. The entire archive is
extracted in a single pass, then only the files that are needed are moved into
the game directory. Files that lost their conflict are discarded from staging.

See [Safety model](../safety) for more on why staging is used and what
guarantees it provides.

## bsdtar dependency

modctl requires `bsdtar` to be installed and available on your `PATH`. If you
installed modctl from a package, `bsdtar` was installed automatically as a
dependency. If you installed from a precompiled binary you may need to install
it manually (see the [Installation](../../installation) guide for
distribution-specific instructions).

Run `modctl doctor` to verify that `bsdtar` is present and working correctly.
