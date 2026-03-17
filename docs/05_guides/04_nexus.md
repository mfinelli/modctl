# Nexus integration

modctl can link your imported mods to their pages on Nexus Mods and check
whether newer versions of your installed files are available. Downloading
updates is always manual; modctl tells you what is out of date and you
decide what to do about it.

A Nexus Mods API key is required to use any of the features described on
this page. If you do not have one, you can generate it from your Nexus Mods
account settings.

## Setting up your API key

The easiest way to authenticate is with the built-in SSO login:
```bash
modctl auth nexus login
```

This opens your browser to a Nexus Mods authorization page. Once you approve
the request your API key is saved automatically — no manual copying required.

In headless environments the authorization URL is printed to the terminal
instead. Open it in a browser on any other device and approve the request;
modctl will receive the key automatically once you do.

The key is stored in plain text in your config file at
`$XDG_CONFIG_HOME/modctl/config.toml`, which is created with permissions
that allow only your user account to read it. There is no keyring
integration in v1.

If you prefer to manage your key manually, you can set it directly:
```bash
modctl config set nexus.apikey YOUR_API_KEY
```

### Checking your authentication status

To confirm your key is valid and see your current API quota:
```bash
modctl auth nexus status
```

This shows your Nexus username and how many API requests remain in your
current daily and hourly windows, along with the time until each resets.

### Removing your API key

To remove the stored key from your config file:
```bash
modctl auth nexus logout
```

This only removes the key locally. The key remains active on Nexus Mods; to
fully revoke it visit your
[Nexus Mods API settings](https://www.nexusmods.com/settings/api-keys).


# Linking mods at import time

The easiest way to link a mod to its Nexus page is to pass `--nexus-url`
when importing:
```bash
modctl mods import ~/Downloads/Appearance\ Menu\ Mod-790-2-12-5-1749642728.zip \
  --nexus-url "https://www.nexusmods.com/cyberpunk2077/mods/790"
```

modctl will use the filename, size, and timestamp of the archive to identify
which specific file on the mod page this archive corresponds to and record
the Nexus file ID alongside it. If the match is unambiguous the link is made
automatically. If modctl cannot determine the match with confidence it will
warn you and suggest using `mods nexus link` to set it manually.

## Linking mods after import

If you imported mods without a Nexus URL, you can link any unlinked mods for
the current game in one go:
```bash
modctl mods nexus link
```

modctl will attempt to identify each unlinked mod using the same filename,
size, and timestamp matching used at import time. If it cannot match a mod
with confidence it will warn you and leave it unlinked.

To operate on a specific mod file version rather than all unlinked mods, pass
`--version-id`. You can also provide additional hints to help modctl
disambiguate: `--nexus-url` for the mod page URL, `--label` for the Nexus
file display name, `--file-version` for the version string, or `--file-name`
for the exact filename as it appears on Nexus:
```bash
modctl mods nexus link --version-id 42 \
  --nexus-url "https://www.nexusmods.com/cyberpunk2077/mods/790" \
  --label "Main File"
```

The most reliable way to ensure a clean link is always to pass `--nexus-url`
at import time with the original filename intact.

To remove a Nexus link from a mod:
```bash
modctl mods nexus unlink "Appearance Menu Mod"
```

## Checking for updates

To check all mods for the current game against Nexus for available updates:
```bash
modctl mods nexus check-updates
```

modctl walks the Nexus file update chain from your installed file ID forward
to determine whether a newer file exists. If an update is available it will
be shown in the output alongside the current and available version information.

modctl respects Nexus API rate limits automatically. If your quota may be
insufficient for a batch check it will warn you before proceeding. You can
pass `--force` to proceed anyway.

Update information is cached locally so that `modctl profiles status` and
`modctl mods info` can display it without making additional API calls. The
cache is refreshed when you run `check-updates`. Pass `--ignore-ttl` to force
a fresh fetch even if the cached data has not yet expired.

## A note on version strings

modctl does not use version strings to determine whether an update is
available. Mod authors on Nexus do not follow a consistent versioning scheme,
so version string comparison would produce unreliable results. Instead,
modctl walks the file update chain that Nexus maintains — when a mod author
uploads a new file as a replacement for an old one, Nexus links the old file
ID to the new one, and modctl follows that chain to find the current version.

## Rate limits

Nexus enforces per-user API rate limits. modctl tracks your remaining quota
after every API call and will warn you if a batch operation might exhaust it.
Rate limit state is persisted locally between sessions so modctl always has
an accurate picture of your current quota without needing to make an extra
API call to check.

---

Next up is the [Configuration reference](../configuration), which covers all
available configuration keys and their defaults.
