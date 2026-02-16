# mod control (modctl)

Deterministic, profile-based mod manager for Linux.

## Overview

modctl installs mods by extracting archives directly into game directories
while tracking every installed file. It supports per-profile priority ordering,
conflict detection, and safe rollback via automatic backups.

Designed for Steam/Proton first, with a store-agnostic architecture ready for
Heroic, Lutris, and future GOG clients.

Metadata is stored in SQLite. Binary artifacts (archives and backups) are
stored in content-addressed blob stores.

**Disclaimer:** I'm using AI to help me plan/build this tool (especially for
writing the documentation). I'm not using an agent, just my the chat interface
from my browser and when I ask it to write functions I'm reviewing, testing,
and modifying (when necessary) all of its output.

---

## Goals

- Deterministic installs and uninstalls
- Profile-based mod sets with per-profile priority
- Explicit conflict resolution (highest priority wins)
- Backup of overwritten non-tool-owned files
- Safe rollback to tool-managed vanilla state
- Steam game discovery (no manual path management)
- Export/import of full state (database + blobs)
- Multi-store architecture from day one
- Nexus mod awareness (mod page + multiple files)

## Non-Goals (v1)

- No dependency resolution
- No virtual filesystem
- No `nxm://` protocol handler
- No in-process archive extraction (requires `bsdtar`)
- No binary merge support
- No GUI (I might add a TUI later)

---

## Architecture

- **Metadata:** SQLite
- **Blob Stores:** Content-addressed on disk
  - `archives/`
  - `backups/`
- **Extraction:** External `bsdtar` with staging + safe move
- **Profiles:** Per-game, per-store
- **Conflict Model:** Deterministic winner selection per path

---

## Roadmap

### v0.1
- Steam store integration
- Archive import
- Dry-run planner
- Apply engine with backups
- Profiles and priority ordering

### v0.2
- Drift detection
- Garbage collection
- Export/import bundle

### Future
- Additional stores (Heroic, Lutris, GOG)
- Structured overrides (INI/YAML/JSON)
- Text-based merge policies
- Optional TUI
- Game-specific integrations

---

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
