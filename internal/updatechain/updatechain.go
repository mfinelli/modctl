/*
 * mod control (modctl): command-line mod manager
 * Copyright © 2026 Mario Finelli
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program. If not, see <https://www.gnu.org/licenses/>.
 */

// Package updatechain works out where a file stands against the updates that
// Nexus Mods knows about for its mod: whether something newer has replaced it,
// and whether that newer file has already been imported.
//
// It only deals with the file ids and knows nothing about where the chain
// comes from (the Nexus cache) or what is done with the answer (showing it).
package updatechain

// Update is one link of a chain: NewFileID is the file that replaced
// OldFileID.
type Update struct {
	OldFileID int64
	NewFileID int64
}

// Chain is which file replaced which, for one mod. Its files can be of
// different kinds (main files, optional files, ...), each replaced by its own
// successors, but file ids are never shared so one Chain holds them all.
//
// The zero Chain knows of no updates: every file is the latest.
type Chain struct {
	next map[int64]int64
}

// New returns the Chain of the given updates. If several say what replaced the
// same file, the last one wins.
func New(updates []Update) Chain {
	next := make(map[int64]int64, len(updates))
	for _, u := range updates {
		next[u.OldFileID] = u.NewFileID
	}

	return Chain{next: next}
}

// HasSuccessor reports whether some file replaced fileID.
func (c Chain) HasSuccessor(fileID int64) bool {
	_, ok := c.next[fileID]
	return ok
}

// Head follows the updates from fileID and returns the latest file: fileID
// itself if nothing replaced it. A chain that loops back on itself (which
// Nexus should never give us) is followed until it closes the loop, and the
// file where it does is returned, so that this always ends.
func (c Chain) Head(fileID int64) int64 {
	current := fileID
	seen := make(map[int64]struct{})

	for {
		if _, visited := seen[current]; visited {
			return current
		}
		seen[current] = struct{}{}

		n, ok := c.next[current]
		if !ok {
			return current
		}
		current = n
	}
}

// State is where a file stands against the updates of its mod.
type State int

const (
	// UpToDate means that nothing has replaced the file.
	UpToDate State = iota
	// Superseded means that the file has been replaced by a newer one that is
	// already imported, so there is nothing to tell the user about.
	Superseded
	// UpdateAvailable means that the file has been replaced by a newer one
	// that is not imported (yet).
	UpdateAvailable
)

// Status is the outcome of Chain.Status.
type Status struct {
	State State

	// Head is the latest file of the chain: the file itself when it is
	// UpToDate.
	Head int64
}

// Status says where fileID stands: whether something replaced it and, if so,
// whether the latest file is among the imported file ids.
func (c Chain) Status(fileID int64, imported map[int64]struct{}) Status {
	head := c.Head(fileID)
	if head == fileID {
		return Status{State: UpToDate, Head: head}
	}

	if _, ok := imported[head]; ok {
		return Status{State: Superseded, Head: head}
	}

	return Status{State: UpdateAvailable, Head: head}
}
