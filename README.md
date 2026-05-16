# mod control (modctl)

![CI](https://github.com/mfinelli/modctl/actions/workflows/default.yml/badge.svg)

A deterministic mod manager for Linux.

> [!TIP]
>
> **NOTE**: this is the developer documentation, if you just want to install
> and get going pre-built packages are available for most distributions:
> https://pkg.modctl.org and the usage documentation is available at:
> https://modctl.org/docs/overview/.

## Overview

modctl installs mods by extracting archives directly into game directories
while tracking every installed file. It supports per-profile priority
ordering, conflict detection, and safe rollback via automatic backups.

Designed for Steam first, with a store-agnostic architecture ready for
Heroic, Lutris, and future GOG clients.

Metadata is stored in SQLite. Binary artifacts (archives and backups) are
stored in content-addressed blob stores on disk.

## Architecture

- **Metadata**: SQLite (via `mattn/go-sqlite3`)
- **Blob stores**: Content-addressed on disk (`archives/`, `backups/`, `overrides/`)
- **Extraction**: External `bsdtar` with staging directory and safe move into game directory
- **Profiles**: Per-game, per-store, with unique priority ordering
- **Conflict resolution**: Deterministic winner selection per destination path
- **Export format**: zstd-compressed tar archive (database snapshot + blobs + manifest)

## Motivation

Gaming on Linux is a real thing now, at least for the kinds of games that I
play. But modding seems to still be stuck in Windows-only land. In theory
you can run Vortex or Mod Organizer 2 on Linux using Wine, but in my
experience it's clunky; I was able to get the tools running but performance
was abysmal even just starting the game through the mod launcher with no
mods enabled. There are a few attempts to make real Linux ports of these
tools but none have made meaningful progress.

For the games I tried, just dumping files into the game directory worked
without issue. But I missed the useful parts of a mod manager: conflict
detection, load order management, update checking against Nexus. I realised
that at the end of the day all I really need is a package manager for mods:
something that tracks which archives have been extracted and which files they
created, so that install and uninstall are possible. Keep everything in SQLite
and you're most of the way there.

So a few conversations with your friendly, neighborhood LLM to think through
the design and edge cases, and here we are.

## AI disclaimer

I used AI assistance while planning and building this project, primarily
for design discussions, thinking through edge cases, and writing
documentation. I did not use an autonomous agent. Every line of code was
reviewed, tested, and where necessary modified before being committed. This
is not a vibe-coded project.

## Goals

- Deterministic installs and uninstalls
- Profile-based mod sets with per-profile priority ordering
- Explicit conflict resolution (highest priority wins)
- Backup of overwritten non-tool-owned files
- Safe rollback to a clean game directory state
- Steam game discovery (no manual path management)
- Export and import of full state (database + blobs)
- Multi-store architecture from day one
- Nexus mod awareness (mod page + multiple files + update checking)

## Non-goals (v1)

- No dependency resolution
- No virtual filesystem
- No `nxm://` protocol handler
- No in-process archive extraction (requires `bsdtar`)
- No binary merge support
- No GUI (I might add a TUI later)

## Requirements

- Go (see `go.mod` for the minimum version; currently 1.25)
- A C compiler (`gcc` is sufficient; CGO is required for `mattn/go-sqlite3`,
  and `klauspost/compress/zstd`)
- `bsdtar` at compile time and at runtime (provided by `libarchive-tools` on
  Debian/Ubuntu, `bsdtar` on Fedora/RHEL, `libarchive` on Arch Linux)

## Building from source

Clone the repository and run make:
```bash
git clone https://github.com/mfinelli/modctl
cd modctl
make
```

This produces a `modctl` binary in the project root. `CGO_ENABLED=1` is set
automatically by the Makefile.

To run the tests:
```bash
go test ./...
```

To format the code:
```bash
go fmt ./...
```

## Repository layout
```
.github/       CI workflows
cmd/           Cobra command definitions
docs/          User-facing documentation (also published to the website)
internal/      Internal Go packages
migrations/    Goose database migration files
pkg/           Package build scripts, specs, PKGBUILDs, signing keys
www/           Public website (Zola)
```

## Development

### Database migrations

modctl uses [goose](https://github.com/pressly/goose) for database
migrations. Migration files live in `migrations/`. To create a new
migration:
```bash
goose create MIGRATION_NAME sql -dir migrations -s
```

### Code generation

modctl uses [sqlc](https://sqlc.dev) to generate type-safe Go code from SQL
queries. To regenerate after modifying queries or schema:
```bash
sqlc generate
```

This is also run automatically by `make` when rebuilding the binary.

### Adding a new command

Commands are defined in `cmd/` using [Cobra](https://github.com/spf13/cobra).
Each command or subcommand group has its own file following the naming
convention already established in the package (e.g. `profiles_create.go`,
`mods_nexus_link.go`).

## Future ideas

- Additional stores (Heroic, Lutris, GOG)
- Structured overrides (INI/YAML/JSON patches on top of base mod layer)
- Text-based file merge policies
- Optional TUI
- Game-specific integrations (load order generation, Proton prefix targets)

## Contributing

Contributions are welcome. For bug reports and questions please use the
[issue tracker](https://github.com/mfinelli/modctl/issues) and
[GitHub Discussions](https://github.com/mfinelli/modctl/discussions).

For code contributions, please open an issue first to discuss what you would
like to change. There are no formal contribution guidelines yet: use common
sense, follow the existing code style, and please make sure the tests pass (I
would love it if all new new functionality came with supporting tests even if
I've been pretty sparse so far myself).

I am focused on Linux for this tool. Patches for macOS or Windows support
may be considered if they do not raise overall complexity significantly.

## License

    mod control (modctl): command-line mod manager
    Copyright (C) 2026  Mario Finelli

    This program is free software: you can redistribute it and/or modify
    it under the terms of the GNU General Public License as published by
    the Free Software Foundation, either version 3 of the License, or
    (at your option) any later version.

    This program is distributed in the hope that it will be useful,
    but WITHOUT ANY WARRANTY; without even the implied warranty of
    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
    GNU General Public License for more details.

    You should have received a copy of the GNU General Public License
    along with this program.  If not, see <https://www.gnu.org/licenses/>.
