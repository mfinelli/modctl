+++
title = "Introducing modctl"
date = 2026-03-19
description = "Introducing modctl, a package manager for your mods"
+++

Gaming on Linux has come a long way in the last few years. Unfortunately, the
same can't really be said for modding on Linux. The existing tools are built
for Windows, and while you can coax some of them into running under Wine, the
experience is quite painful. I've seen a few attempts at native Linux ports,
but as far as I can tell, none have gotten very far.

I found myself just extracting archives directly into the game directories,
which worked for the games I tried, but I quickly realized that I was missing
the things that actually make a mod manager useful: conflict detection, load
order management, a way to cleanly uninstall, and update checking. And when I
thought about it, all of those things are really just what a package manager
does: track what was installed and where, so it can be cleanly removed or
updated later. As a Linux user, I was already familiar with that model, I just
needed one for mods.

**modctl** is a command-line mod manager for Linux, or more precisely, a
package manager for your mods. I've been working on it for a little while now,
and I'm writing this to introduce it and invite some early feedback before the
stable 1.0 release, which I'm hoping to cut soon.

## What it does

modctl installs mods by extracting archives into your game directory and
keeping a precise record of every file it touches: which mod it came from,
which profile put it there, and what was there before.

Here's a quick tour:

- **Game discovery**: modctl reads your Steam library automatically. No
  manually entering install paths or app IDs.
- **Profiles**: define named mod sets and switch between them with a single
  command. Useful for keeping a stable playthrough separate from an
  experimental one, or testing different load orders.
- **Conflict resolution**: when two mods provide the same file, the priority
  order (that you set) decides the winner.
- **Safe apply**: nothing is written to your game directory until you
  explicitly ask for it, and you can always preview what will change before
  committing.
- **Backups and rollback**: before overwriting any file it didn't install,
  modctl backs it up. On uninstall, everything is restored automatically.
- **Nexus Mods integration**: modctl tracks updates per file, not just per mod,
  so whether a mod author updates the main file, an optional file, or a patch,
  you'll actually know about it.
- **Export and import**: export your entire mod setup, or just a single
  game, to a portable bundle you can restore on another machine, archive
  for safekeeping, or even share with a friend.

## Where things stand

modctl is in pre-release. It works, and I've tested it reasonably thoroughly,
but I'm the only person who has used it, and there's no substitute for
real-world usage across different setups and workflows. So `v0.3.0` will
probably be the last pre-release, pending any fixes that come out of testing.
I also expect the internal database schema to be stable from this point on,
though I'm not making any hard promises until 1.0 if something serious turns up.

## I'd love your feedback

If you're a Linux gamer who mods, or who has wanted to but found the tooling
to do so unsatisfactory, I would really appreciate it if you tried it out and
let me know what you think. I'm particularly interested in hearing from people
running setups that I haven't tested: different distros (I game on Debian),
different Steam library configurations, mods from sources other than Nexus, or
anything else really. Feedback of any kind is more than welcome.

It's worth noting that right now modctl only supports Steam, because that's
where I do most of my gaming, but I'm hoping to add support for other stores in
the future.

Anyway, this is a "here's what I've been working on" post not a "it shipped"
post (but I'll write one of those when I do finally release 1.0). I hope you'll
give it a try and let me know what you think as I work towards a stable
release.

If you'd like some more information about how modctl works or how to use it
I've written a whole lot of documentation the best place to start is probably
the [Overview](https://modctl.org/docs/overview/).

Native packages are available for all major Linux distributions you can follow
the [installation instructions](https://pkg.modctl.org) for your distribution.

modctl is written in Go and is open-source and so if you'd like to open an
issue, start a discussion, or just check out the source code you can do so on
[GitHub](https://github.com/mfinelli/modctl).
