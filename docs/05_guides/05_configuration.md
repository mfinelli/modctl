# Configuration reference

modctl's configuration is stored in a TOML file at
`$XDG_CONFIG_HOME/modctl/config.toml`. The file is optional — if it does not
exist, modctl uses built-in defaults for everything except `nexus.apikey`,
which has no default and is required to use Nexus features.

You can edit the file directly or use the `config` commands to read and write
values without touching it by hand. Note that `config set` rewrites the entire
file, so any comments or custom formatting you have added will not be
preserved.

## Configuration keys

| Key             | Default                                   | Description                                      |
|-----------------|-------------------------------------------|--------------------------------------------------|
| `bsdtar`        | `bsdtar`                                  | Name or path of the bsdtar binary                |
| `database`      | `$XDG_DATA_HOME/modctl/modctl.db`         | Path to the SQLite database                      |
| `archives_dir`  | `$XDG_DATA_HOME/modctl/archives`          | Blob store for mod archives                      |
| `backups_dir`   | `$XDG_DATA_HOME/modctl/backups`           | Blob store for pre-existing file backups         |
| `overrides_dir` | `$XDG_DATA_HOME/modctl/overrides`         | Blob store for user overrides                    |
| `locks_dir`     | `$XDG_STATE_HOME/modctl/locks`            | Per-game lockfiles                               |
| `tmp_dir`       | `$XDG_RUNTIME_DIR/modctl`                 | Staging directory used during archive extraction |
| `nexus.apikey`  | (none)                                    | Nexus Mods API key                               |

Most users will never need to change anything other than `nexus.apikey`. The
defaults follow XDG Base Directory conventions and work correctly on any
standard Linux desktop.

### A note on tmp_dir

The default staging directory is under `$XDG_RUNTIME_DIR`, which is typically
a tmpfs mount — fast, in-memory, and cleaned up automatically on logout. If
you work with very large mod archives or have a small tmpfs allocation you may
want to point `tmp_dir` at a directory on a regular filesystem with more
headroom:
```bash
modctl config set tmp_dir /var/tmp/modctl
```

## Viewing configuration

To list all keys with their current effective values:
```bash
modctl config list
```

Each value is shown alongside an indication of whether it was explicitly set
or is inheriting its default. For `nexus.apikey`, only the last few characters
of the key are shown.

To check the value of a single key:
```bash
modctl config get tmp_dir
```

`config get nexus.apikey` will show the full API key, unlike `config list`
which masks it.

## Setting configuration values

To set a key:
```bash
modctl config set tmp_dir /var/tmp/modctl
```

The config file is created if it does not exist. A notice is printed when
setting `nexus.apikey` as a reminder that the key is stored in plain text.

## Nexus API key

The recommended way to configure your Nexus API key is via SSO login, which
handles everything automatically:
```bash
modctl auth nexus login
```

If you prefer to set the key manually (for example in a scripted or
non-interactive environment) you can write it directly to the config file:
```bash
modctl config set nexus.apikey YOUR_API_KEY
```

Either way the key ends up in the same place in your config file and works
identically. See [Nexus integration](../nexus) for more on authenticating
and using modctl with Nexus Mods.
