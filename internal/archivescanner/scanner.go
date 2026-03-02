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

package archivescanner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// Scanner invokes bsdtar to list archive contents
type Scanner struct {
	// BsdtarPath is the path to the bsdtar binary. If empty, "bsdtar" is
	// looked up via PATH
	BsdtarPath string
}

// ScanResult is the output of a successful scan
type ScanResult struct {
	Entries []Entry
	// Warnings contains any output bsdtar wrote to stderr. A non-empty
	// Warnings does not indicate failure - the scan succeeded but bsdtar
	// had something to say (e.g. unsupported extended attributes).
	Warnings string
}

// Scan runs `bsdtar -tvvf <archivePath>` and returns the parsed entries
// A non-zero exit from bsdtar is treated as a hard error; the stderr output
// is included in the returned error
func (s Scanner) Scan(ctx context.Context, archivePath string) (ScanResult, error) {
	bin := s.BsdtarPath
	if bin == "" {
		bin = "bsdtar"
	}

	cmd := exec.CommandContext(ctx, bin, "-tvvf", archivePath)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return ScanResult{}, fmt.Errorf(
			"bsdtar exited with error scanning %q: %w\nstderr: %s",
			archivePath, err, stderr.String(),
		)
	}

	entries, err := Parse(&stdout)
	if err != nil {
		return ScanResult{}, fmt.Errorf("parsing bsdtar output for %q: %w", archivePath, err)
	}

	return ScanResult{
		Entries:  entries,
		Warnings: stderr.String(),
	}, nil
}
