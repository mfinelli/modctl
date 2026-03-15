# modctl changelog

This document keeps track of notable changes to `modctl`. The format is loosely
based on the [Keep a Changelog](https://keepachangelog.com/en/1.0.0/) format.

`modctl` adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
