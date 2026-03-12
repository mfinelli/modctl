# Conflict resolution

A conflict occurs when two or more mods in the same profile provide a file at
the same destination path. This is normal and expected — many mods touch
overlapping parts of a game's directory. modctl resolves every conflict
deterministically using priority order, so the outcome is always predictable
and under your control.

## How conflicts are resolved

When modctl plans an apply, it looks at every file provided by every enabled
mod in the profile. For each destination path, the mod with the highest
priority number wins. The winning mod's version of the file is installed; the
losing mods' versions are ignored for that path.

The resolution is always one mod or the other, modctl never tries to merge
conflicting files. Whichever mod loses a conflict has its version of that file
ignored entirely for that path. There is no partial merge, no line-by-line
combination, and no binary merge. If you need a customised version of a file
that draws from multiple mods, the overrides system (coming in a future
version) will allow you to define the final file content yourself.

There are no hidden tiebreakers. If two mods have different priorities, the
higher one always wins. Priority values within a profile are unique, so there
are never two mods at the same level competing for the same file.

## Seeing conflicts before you apply

`modctl profiles status` shows the current state of your profile including any
conflicts, so you can review them before committing to an apply:
```bash
modctl profiles status
```

For a more precise picture of exactly what will be written to disk, use
`--dry-run`:
```bash
modctl apply --dry-run
```

This runs the full planning step and shows every file operation that would be
performed, including which mod wins each contested path.

## Resolving a conflict

If the wrong mod is winning a conflict, the fix is to adjust the priority order
so the mod you want takes precedence. See [Profiles and priority
ordering](../profiles) for the full set of tools available for reordering.

As a quick example, if "Mod B" should win over "Mod A":
```bash
modctl profiles order move "Mod B" --after "Mod A"
```

Then verify the result looks right before applying:
```bash
modctl apply --dry-run
modctl apply
```

## Enabling and disabling mods

If you want to temporarily remove a mod from conflict resolution without
losing its place in the priority order, disable it:
```bash
modctl profiles disable "Mod A"
```

Disabled mods are excluded from planning entirely — they neither win nor lose
conflicts while disabled. Re-enable them with `modctl profiles enable` when
you want them back in the mix.

## Incompatible mods

Some mods are fundamentally incompatible with each other regardless of priority
— for example, two mods that overhaul the same game system in conflicting ways
where having both active would cause problems even if one's files take
precedence over the other's.

Incompatibilities in modctl are entirely user-defined. modctl has no knowledge
of whether two mods actually conflict at a gameplay or technical level; you
decide which pairs of mods are problematic and why, and you provide whatever
reason makes sense to you. The purpose is to give yourself a persistent
reminder that shows up in modctl profiles status whenever both mods are active
in the same profile.

You can flag pairs of mods as incompatible so modctl will warn you whenever
both are active in the same profile:
```bash
modctl mods incompatible add "Mod A" "Mod B" --reason "Conflicting economy overhauls"
```

Incompatibility warnings are shown in `modctl profiles status`. They never
block an apply — the decision of whether to proceed is always yours.

To remove an incompatibility flag:
```bash
modctl mods incompatible remove "Mod A" "Mod B"
```

To see all flagged incompatibilities for the current game:
```bash
modctl mods incompatible list
```

## What conflicts are not

It is worth being clear about what modctl's conflict resolution does and does
not cover.

modctl resolves **file-level conflicts**: when two mods provide a file at the
same path, one version is installed and the other is not. This is the only kind
of conflict modctl handles automatically.

modctl does not resolve **content-level conflicts**: if two mods both modify
the same configuration file in different ways, installing one mod's version of
that file means the other mod's changes are lost entirely. No merging happens.
For situations like this, the overrides system (coming in a future version)
will allow you to define the final file content yourself on top of the base mod
layer.

modctl also does not perform any game-specific conflict detection; it has no
knowledge of load order rules for specific game engines, plugin systems, or
mod frameworks. For games with their own plugin or load order systems, you will
need to manage that separately.

---

With conflict resolution understood, the next topic worth reading is [Remap
rules](../remap), which controls how mod archives are mapped into the game
directory before conflict resolution even runs.
