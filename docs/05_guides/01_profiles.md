# Profiles and priority ordering

A **profile** is a named set of mods for a game, along with a priority order
that determines which mod wins when two of them provide the same file. If you
have used a mod manager on Windows before, profiles are equivalent to what
tools like Vortex and Mod Organizer 2 call load order. modctl allows a
different priority list per-profile.

This guide covers creating and managing profiles, controlling the priority
order of mods within a profile, and switching between profiles.

## The Default profile

When modctl discovers a game for the first time it automatically creates a
profile called **Default**. For most users with a single mod setup this is
the only profile they will ever need. You can rename it, add mods to it, and
use it as-is without ever creating another profile.

## Creating a profile

To create a new profile for the current game:
```bash
modctl profiles create "Graphics Overhaul"
```

New profiles start inactive. To make a profile the one that commands operate
on by default, set it as active:
```bash
modctl profiles set-active "Graphics Overhaul"
```

Only one profile can be active per game at a time. Setting a new active profile
does not apply it to your game directory — that still requires `modctl apply`.

To see all profiles for the current game:
```bash
modctl profiles list
```

The active profile is marked with an asterisk.

## Adding and removing mods

To add a mod to the active profile:
```bash
modctl profiles add "Appearance Menu Mod"
```

You can refer to a mod by name. If the name is ambiguous modctl will show you
a list with internal IDs and ask you to rerun with the ID directly. Mods are
added enabled by default. Use `--disabled` to add a mod without enabling it
right away.

To remove a mod from the active profile:
```bash
modctl profiles remove "Appearance Menu Mod"
```

Removing a mod from a profile does not change anything on disk. The change
takes effect the next time you run `modctl apply`.

To operate on a profile other than the active one, pass `--profile`:
```bash
modctl profiles add "Appearance Menu Mod" --profile "Graphics Overhaul"
```

## Enabling and disabling mods

Rather than removing a mod entirely you can disable it temporarily. Disabled
mods stay in the profile but are ignored when computing what gets installed:
```bash
modctl profiles disable "Appearance Menu Mod"
modctl profiles enable "Appearance Menu Mod"
```

This is useful for testing whether a mod is causing a problem without losing
its position in the priority order.

## Priority order

Priority order (also known as load order in other mod managers) determines
which mod wins when two mods in the same profile provide the same file.
**Higher numbers win.** If mod A has priority 100 and mod B has priority 200,
and both provide the same file, mod B's version of that file is the one that
gets installed.

When you add a mod without specifying a priority, modctl assigns it the next
highest priority automatically; so the most recently added mod wins by
default.

To see the current priority order of your active profile:
```bash
modctl profiles status
```

This shows all mods in the profile in priority order, along with their
enabled/disabled state and any warnings.

### Changing the order

The `profiles order` subcommands give you precise control over priorities.

**Move a mod relative to another**: the simplest way to reorder:
```bash
modctl profiles order move "Appearance Menu Mod" --after "Cyber Engine Tweaks"
modctl profiles order move "Appearance Menu Mod" --before "Cyber Engine Tweaks"
```

`move` rewrites priorities to a compact sequence starting at 1, so you
don't need to think about numbers at all.

**Swap two mods**: exchange the priorities of two mods without touching
anything else:
```bash
modctl profiles order swap "Mod A" "Mod B"
```

**Set a specific priority**: assign an exact priority number to a mod:
```bash
modctl profiles order set "Appearance Menu Mod" 500
```

Priority values must be unique within a profile.

**Compact the sequence**: if priorities have become sparse after many edits,
renumber them to a clean sequence while preserving the current order:
```bash
modctl profiles order compact
```

By default priorities are reassigned as 1, 2, 3, and so on. Pass `--multiple`
to leave gaps between them (useful if you want room to insert mods at specific
positions later):
```bash
modctl profiles order compact --multiple 10
```

This assigns priorities as 10, 20, 30, and so on.

## Comparing profiles

To see the differences between two profiles:
```bash
modctl profiles diff "Default" "Graphics Overhaul"
```

This shows mods that have been added, removed, or changed between the two
profiles. The comparison is directional: the first profile is the source and
the second is the target. Pass `--no-unchanged` to hide mods that are
identical in both.

## Switching profiles

To switch to a different profile, set it as active and apply it:
```bash
modctl profiles set-active "Graphics Overhaul"
modctl apply
```

modctl works out the difference between what is currently installed and what
the new profile requires, and applies only the necessary changes. Files owned
by the previous profile that are no longer needed are removed, and backed-up
game files are restored where appropriate.

## Renaming and deleting profiles

To rename a profile:
```bash
modctl profiles rename "Graphics Overhaul" "New Name"
```

To delete a profile:
```bash
modctl profiles delete "Graphics Overhaul"
```

Deleting a profile removes its definition (the list of mods, their order, and
any remap rules) but does not change anything on disk. If the profile is
currently active you must pass `--force`. If it is the currently applied
profile (the one whose files are on disk) you must pass `--delete-applied`,
with the understanding that the profile definition will be gone even though
its files remain installed until the next apply or unapply.

---

For more detail on what happens when two mods compete for the same file, see
[Conflict resolution](../conflicts). For help adjusting how a mod's files are
mapped into the game directory (for example stripping leading path segments or
installing files into a subfolder) see [Remap rules](../remap).
