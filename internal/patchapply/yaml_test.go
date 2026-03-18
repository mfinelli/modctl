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

package patchapply

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApply_YAML(t *testing.T) {
	t.Parallel()

	t.Run("set existing key", func(t *testing.T) {
		t.Parallel()
		input := "graphics:\n  quality: low\nresolution: 1080\n"
		entries := []Entry{
			{PatchType: "yaml_set", EntryKey: "resolution", EntryValue: "1440"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 1, result.Applied)
		assert.Contains(t, string(result.Output), "1440")
		assert.NotContains(t, string(result.Output), "1080")
	})

	t.Run("set missing key appends it", func(t *testing.T) {
		t.Parallel()
		input := "resolution: 1080\n"
		entries := []Entry{
			{PatchType: "yaml_set", EntryKey: "fullscreen", EntryValue: "true"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 1, result.Applied)
		assert.Contains(t, string(result.Output), "fullscreen")
		assert.Contains(t, string(result.Output), "true")
	})

	t.Run("unset existing key", func(t *testing.T) {
		t.Parallel()
		input := "resolution: 1080\nfullscreen: true\n"
		entries := []Entry{
			{PatchType: "yaml_unset", EntryKey: "fullscreen"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 1, result.Applied)
		assert.NotContains(t, string(result.Output), "fullscreen")
		assert.Contains(t, string(result.Output), "resolution")
	})

	t.Run("unset missing key is skipped", func(t *testing.T) {
		t.Parallel()
		input := "resolution: 1080\n"
		entries := []Entry{
			{PatchType: "yaml_unset", EntryKey: "fullscreen"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 0, result.Applied)
		assert.Equal(t, 1, result.Skipped)
	})

	t.Run("comment preservation", func(t *testing.T) {
		t.Parallel()
		input := "# graphics config\nresolution: 1080 # current res\nfullscreen: false\n"
		entries := []Entry{
			{PatchType: "yaml_set", EntryKey: "resolution", EntryValue: "1440"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Contains(t, string(result.Output), "graphics config")
		assert.Contains(t, string(result.Output), "1440")
	})

	t.Run("key with special characters uses quoted path", func(t *testing.T) {
		t.Parallel()
		input := "normal: 1\n"
		entries := []Entry{
			{PatchType: "yaml_set", EntryKey: "key.with.dots", EntryValue: "42"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 1, result.Applied)
		assert.Contains(t, string(result.Output), "key.with.dots")
	})

	t.Run("empty input", func(t *testing.T) {
		t.Parallel()
		entries := []Entry{
			{PatchType: "yaml_set", EntryKey: "resolution", EntryValue: "1440"},
		}
		result, err := Apply(entries, []byte(""))
		require.NoError(t, err)
		assert.Equal(t, 1, result.Applied)
		assert.Contains(t, string(result.Output), "resolution")
	})

	t.Run("no entries returns input unchanged", func(t *testing.T) {
		t.Parallel()
		input := "resolution: 1080\n"
		result, err := Apply(nil, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, input, string(result.Output))
	})

	t.Run("multiple entries", func(t *testing.T) {
		t.Parallel()
		input := "resolution: 1080\nfullscreen: false\nvsync: true\n"
		entries := []Entry{
			{PatchType: "yaml_set", EntryKey: "resolution", EntryValue: "1440"},
			{PatchType: "yaml_unset", EntryKey: "vsync"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 2, result.Applied)
		assert.Contains(t, string(result.Output), "1440")
		assert.NotContains(t, string(result.Output), "vsync")
	})
}
