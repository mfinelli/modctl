modctl(1) "v__VERSION__" "modctl __DATE__"

# NAME

modctl - a deterministic mod manager for Linux

# SYNOPSIS

*modctl* [OPTIONS] <command> [ARGS]

# DESCRIPTION

modctl is a command-line mod manager for Linux. It installs mods by extracting
archives into a game's installation directory and recording every file it
writes. That record makes safe uninstalls, conflict resolution, and profile
switching possible.

modctl organises state around four core concepts:

*Stores* are sources of game installations. Steam is supported in the current
version. modctl discovers games automatically by reading the store's library
configuration; no manual path entry is required.

*Games* are discovered game installations. Each game has one or more targets:
named locations where mods can be installed. In the current version the only
target is the game's installation directory.

*Mods* are organised in three levels: a mod page (a mod project), a mod file
(a downloadable file under a mod page), and a mod file version (a specific
imported archive). This mirrors the structure used by Nexus Mods.

*Profiles* are named sets of mod versions for a game, each with a priority
order. When two mods provide a file at the same destination path, the mod with
the higher priority number wins. Exactly one profile can be active per game
at a time.

Changes to profiles (adding, removing, enabling, disabling mods, or changing
priority) do not modify the game directory until *modctl apply* is run.
*modctl apply* reconciles the game directory to match the desired state
described by the active profile.

Before overwriting any file it did not install, modctl makes a backup.
Backups are restored automatically on unapply.

# FIRST RUN

Initialize modctl's local state:

	modctl init

Discover installed games from enabled stores:

	modctl games refresh

Set the active game:

	modctl games set-active <id>

# COMMANDS

## General

*modctl init*
	Initialize modctl's data directories and database. Safe to run multiple
	times.

*modctl status*
	Show a high-level status summary of all known game installs, including
	the applied profile, file counts, and any warnings.

## Authentication

*modctl auth nexus login* [--force]
	Authenticate with Nexus Mods using Single Sign-On. Opens the default
	browser to the Nexus Mods authorization page and waits for approval.
	The API key is received automatically and saved to the config file;
	no manual key copying is required. In headless environments the
	authorization URL is printed so it can be opened on another machine.
	Pass --force to replace an existing key.

*modctl auth nexus logout*
	Remove the stored Nexus Mods API key from the config file. The key
	remains active on Nexus Mods; to revoke it visit
	_https://www.nexusmods.com/settings/api-keys_

*modctl auth nexus status*
	Show Nexus Mods authentication status and current API quota. Makes a
	live call to confirm the stored key is valid and displays the
	authenticated username, daily and hourly requests remaining, and the
	time until each quota window resets. This call does not count against
	the rate limit.

## Games

*modctl games list* [--all] [--store <store>]
	List discovered game installs. By default shows only games from the
	active store.

*modctl games refresh*
	Scan all enabled stores and update the list of discovered game installs.
	Internal Steam titles (Proton Experimental, Steam Linux Runtime, Steamworks
	Common Redistributables) are silently filtered and never added to the
	database.

*modctl games info* <id>
	Show detailed information about a game install. Accepts a numeric ID,
	a selector such as _steam:1091500_ or _steam:1091500#default_, or a
	game title such as _Cyberpunk 2077_.

*modctl games set-active* <id>
	Set the active game install used by subsequent commands. Accepts a
	numeric ID, a selector, or a game title.

## Stores

*modctl stores list*
	List all configured stores and whether they are enabled.

*modctl stores set-active* <store>
	Set the active store used by commands that accept a --store flag.

## Mods

*modctl mods import* [--nexus-url <url>] [--rm] [--skip-inventory] <archive>
	Import a mod archive into the local store. If the file is not a
	recognised archive format it is wrapped in a .tar.gz archive
	automatically. Validates the archive using bsdtar before storing.

*modctl mods list* [--details]
	List imported mods for the current game. Pass --details to expand each
	mod page and show individual files and versions.

*modctl mods info* <mod>
	Show detailed information about a mod page including files, versions,
	Nexus link state, and profile membership.

*modctl mods remove* [--file-version <id>] [--force] <mod-page>
	Remove a mod page and all files and versions under it. With
	\--file-version, removes only that specific version; empty parent
	files and pages are removed automatically. Blobs are not deleted
	immediately; run *modctl gc* to reclaim disk space. If any version
	to be removed is referenced by a profile, the command refuses unless
	\--force is passed, which removes the profile items via cascade.

*modctl mods scan-inventory*
	Scan archive contents for all mods that have not yet been inventoried.

*modctl mods nexus link* [--version-id <id>] [--nexus-url <url>] [--label <label>] [--file-version <version>] [--file-name <name>]
	Attempt to link unlinked mods to their Nexus pages. Without
	\--version-id, processes all unlinked mods for the current game.

*modctl mods nexus unlink* <mod>
	Remove the Nexus link from a mod.

*modctl mods nexus check-updates* [--ignore-ttl] [--force]
	Check all Nexus-linked mods for available updates.

*modctl mods incompatible add* [--reason <text>] <mod-a> <mod-b>
	Flag two mods as incompatible. Incompatibilities are user-defined and
	surfaced as warnings in *modctl profiles status*. They do not prevent
	applying a profile.

*modctl mods incompatible remove* <mod-a> <mod-b>
	Remove an incompatibility flag between two mods.

*modctl mods incompatible list*
	List all incompatibility flags for the current game.

## Profiles

*modctl profiles list*
	List all profiles for the current game. The active profile is marked
	with an asterisk.

*modctl profiles create* <name>
	Create a new profile for the current game.

*modctl profiles delete* [--force] [--delete-applied] <name>
	Delete a profile. Pass --force if the profile is currently active.
	Pass --delete-applied if its files are currently on disk.

*modctl profiles rename* <old-name> <new-name>
	Rename a profile.

*modctl profiles set-active* <name>
	Set the active profile for the current game.

*modctl profiles status*
	Show the mods in the active profile in priority order, their
	enabled/disabled state, and any warnings. When the profile is applied,
	also reports pending changes.

*modctl profiles diff* [--no-unchanged] <profile-a> <profile-b>
	Compare two profiles. The comparison is directional: profile-a is the
	source, profile-b is the target.

*modctl profiles add* [--priority <n>] [--disabled] <mod>
	Add a mod to the active profile. Assigned the next highest priority by
	default.

*modctl profiles remove* <mod>
	Remove a mod from the active profile.

*modctl profiles enable* <mod>
	Enable a mod in the active profile.

*modctl profiles disable* <mod>
	Disable a mod in the active profile. Disabled mods are excluded from
	planning.

*modctl profiles order move* [--before <mod>|--after <mod>] <mod>
	Move a mod before or after another in the priority order.

*modctl profiles order swap* <mod-a> <mod-b>
	Swap the priorities of two mods.

*modctl profiles order set* <mod> <priority>
	Set an exact priority value for a mod.

*modctl profiles order compact* [--multiple <n>]
	Renumber priorities to a clean sequence while preserving order.

*modctl profiles remap list* <mod>
	List remap rules for a mod version in the active profile.

*modctl profiles remap add* [--position <n>] <mod> <rule-type> <value>
	Add a remap rule. Rule types: _strip_components_, _select_subdir_,
	_dest_prefix_, _include_glob_, _exclude_glob_.

*modctl profiles remap remove* <mod> <position>
	Remove a remap rule at a specific position.

*modctl profiles remap clear* <mod>
	Remove all remap rules for a mod version.

*modctl profiles remap copy* <src-mod> <dst-mod>
	Copy remap rules from one mod version to another.

*modctl profiles remap preview* [--show-filtered] <mod>
	Preview how remap rules transform a mod's archive entries. Shows each
	file entry in the archive and the destination path it would be installed
	to after all rules have been applied. Entries filtered out by rules are
	hidden by default; pass --show-filtered to see them alongside the reason
	they were excluded. The archive must be inventoried first; run
	*modctl mods scan-inventory* if needed.

*modctl profiles overrides list*
	List all overrides for the active profile with a summary staleness
	status. Run *modctl profiles overrides status* for full detail.

*modctl profiles overrides status* [<path>]
	Show detailed staleness information for all overrides in the active
	profile, or for a single path if specified.

*modctl profiles overrides set* [--notes <text>] [--force] <path> <file>
	Set a full-file override for a path in the active profile. The file
	is imported from disk and stored in the override blob store. The path
	must be relative to the game directory. If an override already exists for
	this path the command refuses unless --force is passed, in which case the
	existing override is replaced.

*modctl profiles overrides edit* [--reset] <path>
	Edit a file override in your preferred editor. If no override exists,
	the base file is extracted from the current winning mod's archive and
	opened as a starting point; on save, a new override is created. If an
	override already exists, the current override content is opened; on save,
	the override blob is updated. Pass --reset to discard the existing override
	and start fresh from the current base file. Respects _$VISUAL_ then
	_$EDITOR_, falling back to _vi_. Note: requires archive extraction which may
	be slow for large archives.

*modctl profiles overrides unset* <path>
	Remove the override for a path in the active profile. If the override was
	applied, run *modctl apply* to reconcile the game directory.

*modctl profiles overrides copy* [--force] <src-profile>
	Copy all overrides from a source profile into the active profile.
	Errors if the active profile already has overrides at any of the same paths
	unless --force is passed, in which case conflicting overrides are replaced.

## Apply

*modctl apply* [--dry-run] [--no-recheck] [--print-ops] [--keep-staging] [--prune-dirs]
	Reconcile the game directory to match the active profile. Backs up
	game-owned files before overwriting them. Files already correctly
	deployed are skipped. Use --dry-run to preview without making changes.
	Use --no-recheck to skip on-disk hash checks for faster applies. Use
	\--prune-dirs to remove empty directories left behind after file removals.

*modctl unapply* [--dry-run] [--print-ops] [--prune-dirs]
	Remove all tool-managed files and restore backed-up game files.
	Operates on the recorded installed state, not the profile definition. Use
	\--prune-dirs to remove empty directories left behind after file removals.

## Export and import

*modctl export* [--game <store:game-id>] [--output <path>] [--skip-inventory]
	Export modctl state to a portable bundle. By default performs a full
	export. Use --game to scope the export to a single game install.
	Game-scoped bundles do not include backup blobs.

*modctl import* [--force] [--dry-run] [--skip-inventory] [--game <store:game-id>] [--same-machine] <bundle>
	Import a modctl export bundle. For full bundles the database must be
	empty unless --force is passed; by default on-disk state (installed
	files, backups, and operation history) is cleared so the destination
	machine starts clean. Pass --same-machine to restore all state verbatim,
	which is only appropriate when restoring to the same machine with game
	directories intact. --force and --same-machine are independent and can
	be combined. For game-scoped bundles the game must not already exist
	unless --force is passed; --same-machine is not valid for game-scoped
	bundles. Pass --game to import a single game from a full bundle. Does
	not apply any profiles automatically.

*modctl verify* <bundle>
	Check the integrity of an export bundle without importing it.

*modctl extract* [--game <store-id>] [--mod <name>] [--file <label>] [--version <version>] [--output-dir <path>] [--overwrite] <bundle>
	Extract mod archives from a bundle. Without --mod, lists all mods in
	the bundle. With --mod, extracts the matching archive.

## Maintenance

*modctl gc* [--dry-run] [--no-archives] [--no-backups] [--min-age <duration>] [--clean-missing] [--skip-orphans]
	Remove unreferenced blobs from the blob store. Use --dry-run to preview
	without making changes. Use --min-age to protect recently created blobs
	(e.g. _7d_, _24h_, _2w_).

*modctl doctor* [--deep] [--recheck] [--check-installs] [--rehash-installs]
	Run a read-only health check. Verifies state directories, database
	integrity, bsdtar availability, game install target writability, and
	blob store integrity (presence and size). Pass --deep for a more
	thorough database check. Pass --recheck to rehash all blobs and verify
	content against their stored SHA-256. Pass --check-installs to verify
	that all recorded installed files exist on disk for applied game
	installs. Pass --rehash-installs to also hash those files and compare
	against recorded checksums (implies --check-installs).

*modctl operations list* [--all] [--limit <n>]
	List recent apply and unapply operations for the current game.

*modctl operations show* <operation-id>
	Show file-level detail for a specific operation.

*modctl config list*
	Show all configuration keys with their effective values.

*modctl config get* <key>
	Show the effective value for a single configuration key.

*modctl config set* <key> <value>
	Set a configuration key.

# FILES

_~/.config/modctl/config.toml_
	Configuration file. Optional; built-in defaults are used if absent.
	Created with mode 0600.

_~/.local/share/modctl/modctl.db_
	SQLite database containing all modctl metadata.

_~/.local/share/modctl/archives/_
	Content-addressed store for mod archives.

_~/.local/share/modctl/backups/_
	Content-addressed store for backed-up game files.

_~/.local/share/modctl/overrides/_
	Content-addressed store for full-file override blobs.

_~/.local/state/modctl/locks/_
	Per-game lock files.

_~/.cache/modctl/nexus_cache.db_
	Nexus API response cache. Safe to delete; repopulated automatically.

_~/.local/state/modctl/nexus.json_
	Persisted Nexus Mods API rate limit state. Updated after every API
	call. Safe to delete; repopulated on the next API call.

# ENVIRONMENT

_XDG_CONFIG_HOME_
	Base directory for the configuration file. Defaults to _~/.config_.

_XDG_DATA_HOME_
	Base directory for the database and blob stores. Defaults to
	_~/.local/share_.

_XDG_STATE_HOME_
	Base directory for lock files and Nexus rate limit state. Defaults
	to _~/.local/state_.

_XDG_RUNTIME_DIR_
	Base directory for the staging directory. Typically a tmpfs mount,
	cleaned up on logout.

_XDG_CACHE_HOME_
	Base directory for the Nexus API cache. Defaults to _~/.cache_.

# DEPENDENCIES

*bsdtar*(1)
	Required for archive listing and extraction. Provided by
	_libarchive-tools_ on Debian/Ubuntu, _bsdtar_ on Fedora/RHEL, and
	_libarchive_ on Arch Linux. Installed automatically when using a
	native package.

# SEE ALSO

Full documentation is available at https://modctl.org/docs

# AUTHORS

Mario Finelli
