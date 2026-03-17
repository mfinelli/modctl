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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	t.Parallel()

	t.Run("empty input", func(t *testing.T) {
		t.Parallel()
		entries, err := Parse(strings.NewReader(""))
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("skips blank lines", func(t *testing.T) {
		t.Parallel()
		input := "\n-rw-r--r--  0 0      0        1024 Jan  1 00:00 Data/plugin.esp\n\n"
		entries, err := Parse(strings.NewReader(input))
		require.NoError(t, err)
		require.Len(t, entries, 1)
		// Blank lines must not consume positions
		assert.Equal(t, 0, entries[0].Position)
	})

	t.Run("skips archive format trailer", func(t *testing.T) {
		t.Parallel()

		// bsdtar always prints a trailer as the last line across all archive types.
		cases := []struct {
			desc    string
			trailer string
		}{
			{
				desc:    "tar.gz",
				trailer: "Archive Format: POSIX ustar format,  Compression: gzip",
			},
			{
				desc:    "RAR",
				trailer: "Archive Format: RAR,  Compression: none",
			},
			{
				desc:    "7z",
				trailer: "Archive Format: 7-Zip,  Compression: none",
			},
			{
				desc:    "zip",
				trailer: "Archive Format: ZIP 1.0 (uncompressed),  Compression: none",
			},
		}

		for _, tc := range cases {
			t.Run(tc.desc, func(t *testing.T) {
				t.Parallel()
				input := "-rw-r--r--  0 0      0           6 Mar  2 21:21 hello.txt\n" + tc.trailer
				entries, err := Parse(strings.NewReader(input))
				require.NoError(t, err)
				require.Len(t, entries, 1)
				assert.Equal(t, "hello.txt", entries[0].RawPath)
			})
		}
	})

	t.Run("multiple entries", func(t *testing.T) {
		t.Parallel()
		input := strings.Join([]string{
			"drwxr-xr-x  0 0      0           0 Jan  1 00:00 Data/",
			"-rw-r--r--  0 0      0        1024 Jan  1 00:00 Data/plugin.esp",
			"-rw-r--r--  0 0      0       98765 Jan  1 00:00 Data/textures/foo.dds",
			"lrwxrwxrwx  0 0      0           0 Jan  1 00:00 Data/link.esp -> Data/plugin.esp",
		}, "\n")

		entries, err := Parse(strings.NewReader(input))
		require.NoError(t, err)
		require.Len(t, entries, 4)

		assert.Equal(t, EntryTypeDir, entries[0].Type)
		assert.Equal(t, EntryTypeSymlink, entries[3].Type)
		assert.Equal(t, "Data/plugin.esp", entries[3].LinkTarget)
	})

	t.Run("duplicate paths are preserved", func(t *testing.T) {
		t.Parallel()
		// Archives may contain duplicate paths - last entry wins during
		// extraction. The parser records all entries faithfully; position
		// is the tiebreaker used by the planner
		input := strings.Join([]string{
			"-rw-r--r--  0 0      0        1024 Jan  1 00:00 Data/plugin.esp",
			"-rw-r--r--  0 0      0        2048 Jan  1 00:00 Data/plugin.esp",
		}, "\n")

		entries, err := Parse(strings.NewReader(input))
		require.NoError(t, err)
		require.Len(t, entries, 2, "both entries must be preserved")

		assert.Equal(t, 0, entries[0].Position)
		assert.Equal(t, 1, entries[1].Position)
		assert.EqualValues(t, 1024, entries[0].SizeBytes)
		assert.EqualValues(t, 2048, entries[1].SizeBytes)
	})

	t.Run("bad line does not abort parse", func(t *testing.T) {
		t.Parallel()
		input := strings.Join([]string{
			"-rw-r--r--  0 0      0        1024 Jan  1 00:00 Data/first.esp",
			"this is not a valid bsdtar line",
			"-rw-r--r--  0 0      0        2048 Jan  1 00:00 Data/third.esp",
		}, "\n")

		entries, err := Parse(strings.NewReader(input))
		require.NoError(t, err)
		require.Len(t, entries, 3)

		assert.Empty(t, entries[0].ParseError)
		assert.NotEmpty(t, entries[1].ParseError)
		assert.Empty(t, entries[2].ParseError)
	})

	t.Run("positions are sequential across valid and invalid lines", func(t *testing.T) {
		t.Parallel()
		// Position must be stable regardless of parse success so that DB
		// inserts faithfully reflect archive order
		input := strings.Join([]string{
			"-rw-r--r--  0 0      0        1024 Jan  1 00:00 Data/a.esp",
			"bad line",
			"-rw-r--r--  0 0      0        1024 Jan  1 00:00 Data/b.esp",
		}, "\n")

		entries, err := Parse(strings.NewReader(input))
		require.NoError(t, err)
		require.Len(t, entries, 3)

		for i, e := range entries {
			assert.Equal(t, i, e.Position)
		}
	})
}

func TestParseLine(t *testing.T) {
	t.Parallel()

	t.Run("entry types", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			desc     string
			line     string
			wantType EntryType
		}{
			{
				desc:     "regular file",
				line:     "-rw-r--r--  0 0      0      123456 Jan  1 00:00 Data/textures/foo.dds",
				wantType: EntryTypeFile,
			},
			{
				desc:     "directory",
				line:     "drwxr-xr-x  0 0      0           0 Jan  1 00:00 Data/textures/",
				wantType: EntryTypeDir,
			},
			{
				desc:     "symlink",
				line:     "lrwxrwxrwx  0 0      0           0 Jan  1 00:00 Data/link.esp -> Data/plugin.esp",
				wantType: EntryTypeSymlink,
			},
			{
				desc:     "character device",
				line:     "crw-r--r--  0 0      0           0 Jan  1 00:00 dev/null",
				wantType: EntryTypeOther,
			},
			{
				desc:     "block device",
				line:     "brw-r--r--  0 0      0           0 Jan  1 00:00 dev/sda",
				wantType: EntryTypeOther,
			},
		}

		for _, tc := range cases {
			t.Run(tc.desc, func(t *testing.T) {
				t.Parallel()
				entry := parseLine(tc.line, 0)
				assert.Equal(t, tc.wantType, entry.Type)
				assert.Empty(t, entry.ParseError)
			})
		}
	})

	t.Run("uid/gid formats", func(t *testing.T) {
		t.Parallel()

		// uid/gid varies by archive type and creating platform:
		//   RAR/7z:   numeric zeros        "0 0"
		//   zip:      real numeric uid/gid "501 20"
		//   tar.gz:   named                "root root"
		// We don't use uid/gid for anything; strings.Fields absorbs the variation
		cases := []struct {
			desc string
			line string
		}{
			{
				desc: "RAR and 7z numeric zeros",
				line: "-rw-r--r--  0 0      0           6 Mar  2 21:21 hello.txt",
			},
			{
				desc: "zip real uid/gid",
				line: "-rw-r--r--  0 501    20          6 Mar  2 21:21 hello.txt",
			},
			{
				desc: "tar.gz named uid/gid",
				line: "-rw-r--r--  0 root   root        6 Feb 24 20:58 hello.txt",
			},
		}

		for _, tc := range cases {
			t.Run(tc.desc, func(t *testing.T) {
				t.Parallel()
				entry := parseLine(tc.line, 0)
				assert.Empty(t, entry.ParseError)
				assert.Equal(t, "hello.txt", entry.RawPath)
				assert.EqualValues(t, 6, entry.SizeBytes)
			})
		}
	})

	t.Run("paths", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			desc       string
			line       string
			wantPath   string
			wantTarget string
		}{
			{
				desc:     "simple path",
				line:     "-rw-r--r--  0 0      0      123456 Jan  1 00:00 Data/textures/foo.dds",
				wantPath: "Data/textures/foo.dds",
			},
			{
				desc:     "path with spaces",
				line:     "-rw-r--r--  0 0      0       98765 Jan  1 00:00 Data/meshes/My Mod Folder/cool mesh.nif",
				wantPath: "Data/meshes/My Mod Folder/cool mesh.nif",
			},
			{
				desc:       "symlink with simple paths",
				line:       "lrwxrwxrwx  0 0      0           0 Jan  1 00:00 Data/link.esp -> ../other/real.esp",
				wantPath:   "Data/link.esp",
				wantTarget: "../other/real.esp",
			},
			{
				desc:       "symlink with spaces on both sides",
				line:       "lrwxrwxrwx  0 0      0           0 Jan  1 00:00 My Link -> My Target/file.esp",
				wantPath:   "My Link",
				wantTarget: "My Target/file.esp",
			},
		}

		for _, tc := range cases {
			t.Run(tc.desc, func(t *testing.T) {
				t.Parallel()
				entry := parseLine(tc.line, 0)
				assert.Empty(t, entry.ParseError)
				assert.Equal(t, tc.wantPath, entry.RawPath)
				assert.Equal(t, tc.wantTarget, entry.LinkTarget)
			})
		}
	})

	t.Run("adversarial paths", func(t *testing.T) {
		t.Parallel()

		// These are faithfully recorded and rejected later by the planner
		// the parser's job is to be an accurate mirror of the archive contents
		cases := []struct {
			desc     string
			line     string
			wantPath string
		}{
			{
				desc:     "path traversal",
				line:     "-rw-r--r--  0 0      0        1024 Jan  1 00:00 ../../etc/passwd",
				wantPath: "../../etc/passwd",
			},
			{
				desc:     "absolute path",
				line:     "-rw-r--r--  0 0      0        1024 Jan  1 00:00 /etc/passwd",
				wantPath: "/etc/passwd",
			},
		}

		for _, tc := range cases {
			t.Run(tc.desc, func(t *testing.T) {
				t.Parallel()
				entry := parseLine(tc.line, 0)
				assert.Equal(t, tc.wantPath, entry.RawPath)
				// No parse error - valid parse, just a dangerous path
				assert.Empty(t, entry.ParseError)
			})
		}
	})

	t.Run("parse errors", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			desc     string
			line     string
			wantType EntryType
			wantPath string
		}{
			{
				desc:     "too few fields",
				line:     "-rw-r--r--  0 0 0",
				wantType: EntryTypeOther,
			},
			{
				desc:     "bad size field still extracts path",
				line:     "-rw-r--r--  0 0      0      notanum Jan  1 00:00 Data/file.esp",
				wantType: EntryTypeFile,
				wantPath: "Data/file.esp",
			},
			{
				desc:     "symlink missing arrow still extracts path",
				line:     "lrwxrwxrwx  0 0      0           0 Jan  1 00:00 Data/link.esp",
				wantType: EntryTypeSymlink,
				wantPath: "Data/link.esp",
			},
		}

		for _, tc := range cases {
			t.Run(tc.desc, func(t *testing.T) {
				t.Parallel()
				entry := parseLine(tc.line, 0)
				assert.NotEmpty(t, entry.ParseError)
				assert.Equal(t, tc.wantType, entry.Type)
				if tc.wantPath != "" {
					assert.Equal(t, tc.wantPath, entry.RawPath)
				}
			})
		}
	})

	t.Run("position is set correctly", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			position int
		}{
			{position: 0},
			{position: 1},
			{position: 99},
		}

		for _, tc := range cases {
			t.Run("", func(t *testing.T) {
				t.Parallel()
				line := "-rw-r--r--  0 0      0        1024 Jan  1 00:00 Data/file.esp"
				entry := parseLine(line, tc.position)
				assert.Equal(t, tc.position, entry.Position)
			})
		}
	})
}
