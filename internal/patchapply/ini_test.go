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

func TestApply_INI(t *testing.T) {
	t.Parallel()

	t.Run("set existing key", func(t *testing.T) {
		t.Parallel()
		input := "[Display]\nfMaxTime=0.033\nfOther=1.0\n"
		entries := []Entry{
			{PatchType: "ini_set", EntrySection: "Display", EntryKey: "fMaxTime", EntryValue: "0.016"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 1, result.Applied)
		assert.Equal(t, 0, result.Skipped)
		assert.Contains(t, string(result.Output), "fMaxTime")
		assert.Contains(t, string(result.Output), "0.016")
		assert.NotContains(t, string(result.Output), "0.033")
	})

	t.Run("set missing key creates it", func(t *testing.T) {
		t.Parallel()
		input := "[Display]\nfOther=1.0\n"
		entries := []Entry{
			{PatchType: "ini_set", EntrySection: "Display", EntryKey: "fMaxTime", EntryValue: "0.016"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 1, result.Applied)
		assert.Contains(t, string(result.Output), "fMaxTime")
		assert.Contains(t, string(result.Output), "0.016")
	})

	t.Run("set key in missing section creates section", func(t *testing.T) {
		t.Parallel()
		input := "[Display]\nfOther=1.0\n"
		entries := []Entry{
			{PatchType: "ini_set", EntrySection: "Audio", EntryKey: "fVolume", EntryValue: "0.5"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 1, result.Applied)
		assert.Contains(t, string(result.Output), "[Audio]")
		assert.Contains(t, string(result.Output), "fVolume")
	})

	t.Run("set key in default section", func(t *testing.T) {
		t.Parallel()
		input := "globalKey=old\n[Section]\nkey=val\n"
		entries := []Entry{
			{PatchType: "ini_set", EntrySection: "", EntryKey: "globalKey", EntryValue: "new"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 1, result.Applied)
		assert.Contains(t, string(result.Output), "globalKey")
		assert.Contains(t, string(result.Output), "new")
		assert.NotContains(t, string(result.Output), "old")
	})

	t.Run("unset existing key", func(t *testing.T) {
		t.Parallel()
		input := "[Display]\nfMaxTime=0.033\nfOther=1.0\n"
		entries := []Entry{
			{PatchType: "ini_unset", EntrySection: "Display", EntryKey: "fMaxTime"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 1, result.Applied)
		assert.Equal(t, 0, result.Skipped)
		assert.NotContains(t, string(result.Output), "fMaxTime")
		assert.Contains(t, string(result.Output), "fOther")
	})

	t.Run("unset missing key is skipped", func(t *testing.T) {
		t.Parallel()
		input := "[Display]\nfOther=1.0\n"
		entries := []Entry{
			{PatchType: "ini_unset", EntrySection: "Display", EntryKey: "fMaxTime"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 0, result.Applied)
		assert.Equal(t, 1, result.Skipped)
	})

	t.Run("unset missing section is skipped", func(t *testing.T) {
		t.Parallel()
		input := "[Display]\nfOther=1.0\n"
		entries := []Entry{
			{PatchType: "ini_unset", EntrySection: "Audio", EntryKey: "fVolume"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 0, result.Applied)
		assert.Equal(t, 1, result.Skipped)
	})

	t.Run("multiple entries applied in order", func(t *testing.T) {
		t.Parallel()
		input := "[Display]\nfMaxTime=0.033\nfOther=1.0\n"
		entries := []Entry{
			{PatchType: "ini_set", EntrySection: "Display", EntryKey: "fMaxTime", EntryValue: "0.016"},
			{PatchType: "ini_unset", EntrySection: "Display", EntryKey: "fOther"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 2, result.Applied)
		assert.Contains(t, string(result.Output), "0.016")
		assert.NotContains(t, string(result.Output), "fOther")
	})

	t.Run("empty input", func(t *testing.T) {
		t.Parallel()
		entries := []Entry{
			{PatchType: "ini_set", EntrySection: "Display", EntryKey: "fMaxTime", EntryValue: "0.016"},
		}
		result, err := Apply(entries, []byte{})
		require.NoError(t, err)
		assert.Equal(t, 1, result.Applied)
		assert.Contains(t, string(result.Output), "fMaxTime")
	})

	t.Run("no entries returns input unchanged", func(t *testing.T) {
		t.Parallel()
		input := "[Display]\nfMaxTime=0.033\n"
		result, err := Apply(nil, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, input, string(result.Output))
	})

	t.Run("comment preservation", func(t *testing.T) {
		t.Parallel()
		input := "; display settings\n[Display]\n; controls frame timing\nfMaxTime=0.033\nfOther=1.0\n"
		entries := []Entry{
			{PatchType: "ini_set", EntrySection: "Display", EntryKey: "fMaxTime", EntryValue: "0.016"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Contains(t, string(result.Output), "display settings")
		assert.Contains(t, string(result.Output), "controls frame timing")
	})
}
