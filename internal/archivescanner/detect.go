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
	"strings"
)

// DetectFormat runs bsdtar -tvvf on the given archive and returns the
// "Archive Format: ..., Compression: ..." trailer line. This can be used
// to determine the appropriate file extension for a blob that has no
// original filename recorded.
func DetectFormat(ctx context.Context, bsdtarPath, archivePath string) (format, compression string, err error) {
	bin := bsdtarPath
	if bin == "" {
		bin = "bsdtar"
	}

	cmd := exec.CommandContext(ctx, bin, "-tvvf", archivePath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf(
			"bsdtar exited with error: %w\nstderr: %s",
			err, stderr.String(),
		)
	}

	// the trailer is always the last line of stdout
	output := strings.TrimSpace(stdout.String())
	lines := strings.Split(output, "\n")
	trailer := lines[len(lines)-1]

	if !strings.HasPrefix(trailer, "Archive Format:") {
		return "", "", fmt.Errorf("unexpected trailer line: %q", trailer)
	}

	// "Archive Format: POSIX ustar format,  Compression: gzip"
	parts := strings.SplitN(trailer, ",", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("could not parse trailer: %q", trailer)
	}

	format = strings.TrimSpace(strings.TrimPrefix(parts[0], "Archive Format:"))
	compression = strings.TrimSpace(strings.TrimPrefix(parts[1], "Compression:"))
	return format, compression, nil
}
