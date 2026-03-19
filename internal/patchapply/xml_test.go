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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApply_XML(t *testing.T) {
	t.Parallel()

	t.Run("set element text content", func(t *testing.T) {
		t.Parallel()
		input := `<?xml version="1.0" encoding="UTF-8"?>
<Settings>
  <Display>
    <Resolution>1080</Resolution>
  </Display>
</Settings>
`
		entries := []Entry{
			{PatchType: "xml_set", EntryKey: "//Display/Resolution", EntryValue: "1440"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 1, result.Applied)
		assert.Equal(t, 0, result.Skipped)
		assert.Contains(t, string(result.Output), "1440")
		assert.NotContains(t, string(result.Output), "1080")
	})

	t.Run("set attribute value", func(t *testing.T) {
		t.Parallel()
		input := `<?xml version="1.0" encoding="UTF-8"?>
<Settings>
  <Window width="1920" height="1080"/>
</Settings>
`
		entries := []Entry{
			{PatchType: "xml_set", EntryKey: "//Settings/Window/@width", EntryValue: "2560"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 1, result.Applied)
		assert.Contains(t, string(result.Output), `width="2560"`)
		assert.NotContains(t, string(result.Output), `width="1920"`)
		// unrelated attribute untouched
		assert.Contains(t, string(result.Output), `height="1080"`)
	})

	t.Run("set matches multiple nodes", func(t *testing.T) {
		t.Parallel()
		input := `<?xml version="1.0" encoding="UTF-8"?>
<Settings>
  <Item><Value>old</Value></Item>
  <Item><Value>old</Value></Item>
</Settings>
`
		entries := []Entry{
			{PatchType: "xml_set", EntryKey: "//Item/Value", EntryValue: "new"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 1, result.Applied)
		assert.Equal(t, 0, result.Skipped)
		assert.NotContains(t, string(result.Output), "old")
		// both nodes updated
		assert.Equal(t, 2, strings.Count(string(result.Output), "<Value>new</Value>"))
	})

	t.Run("set no match is skipped", func(t *testing.T) {
		t.Parallel()
		input := `<?xml version="1.0" encoding="UTF-8"?>
<Settings>
  <Display/>
</Settings>
`
		entries := []Entry{
			{PatchType: "xml_set", EntryKey: "//Audio/Volume", EntryValue: "0.5"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 0, result.Applied)
		assert.Equal(t, 1, result.Skipped)
	})

	t.Run("unset removes element node", func(t *testing.T) {
		t.Parallel()
		input := `<?xml version="1.0" encoding="UTF-8"?>
<Settings>
  <Display>
    <Vsync>true</Vsync>
    <Resolution>1080</Resolution>
  </Display>
</Settings>
`
		entries := []Entry{
			{PatchType: "xml_unset", EntryKey: "//Display/Vsync"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 1, result.Applied)
		assert.NotContains(t, string(result.Output), "Vsync")
		// sibling untouched
		assert.Contains(t, string(result.Output), "Resolution")
	})

	t.Run("unset removes attribute", func(t *testing.T) {
		t.Parallel()
		input := `<?xml version="1.0" encoding="UTF-8"?>
<Settings>
  <Window width="1920" height="1080"/>
</Settings>
`
		entries := []Entry{
			{PatchType: "xml_unset", EntryKey: "//Settings/Window/@width"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 1, result.Applied)
		assert.NotContains(t, string(result.Output), "width")
		// other attribute untouched
		assert.Contains(t, string(result.Output), `height="1080"`)
	})

	t.Run("unset no match is skipped", func(t *testing.T) {
		t.Parallel()
		input := `<?xml version="1.0" encoding="UTF-8"?>
<Settings>
  <Display/>
</Settings>
`
		entries := []Entry{
			{PatchType: "xml_unset", EntryKey: "//Audio/Volume"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 0, result.Applied)
		assert.Equal(t, 1, result.Skipped)
	})

	t.Run("clear empties element text content", func(t *testing.T) {
		t.Parallel()
		input := `<?xml version="1.0" encoding="UTF-8"?>
<Settings>
  <Display>
    <Resolution>1080</Resolution>
  </Display>
</Settings>
`
		entries := []Entry{
			{PatchType: "xml_clear", EntryKey: "//Display/Resolution"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 1, result.Applied)
		// node still present but empty
		assert.Contains(t, string(result.Output), "Resolution")
		assert.NotContains(t, string(result.Output), "1080")
	})

	t.Run("clear empties attribute value", func(t *testing.T) {
		t.Parallel()
		input := `<?xml version="1.0" encoding="UTF-8"?>
<Settings>
  <Window width="1920"/>
</Settings>
`
		entries := []Entry{
			{PatchType: "xml_clear", EntryKey: "//Settings/Window/@width"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 1, result.Applied)
		// attribute still present but empty
		assert.Contains(t, string(result.Output), "width")
		assert.NotContains(t, string(result.Output), "1920")
		assert.Contains(t, string(result.Output), `width=""`)
	})

	t.Run("clear no match is skipped", func(t *testing.T) {
		t.Parallel()
		input := `<?xml version="1.0" encoding="UTF-8"?>
<Settings/>
`
		entries := []Entry{
			{PatchType: "xml_clear", EntryKey: "//Display/Resolution"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 0, result.Applied)
		assert.Equal(t, 1, result.Skipped)
	})

	t.Run("comment preservation", func(t *testing.T) {
		t.Parallel()
		input := `<?xml version="1.0" encoding="UTF-8"?>
<!-- display settings -->
<Settings>
  <!-- controls resolution -->
  <Display>
    <Resolution>1080</Resolution>
  </Display>
</Settings>
`
		entries := []Entry{
			{PatchType: "xml_set", EntryKey: "//Display/Resolution", EntryValue: "1440"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Contains(t, string(result.Output), "display settings")
		assert.Contains(t, string(result.Output), "controls resolution")
	})

	t.Run("multiple entries applied in order", func(t *testing.T) {
		t.Parallel()
		input := `<?xml version="1.0" encoding="UTF-8"?>
<Settings>
  <Display>
    <Resolution>1080</Resolution>
    <Vsync>true</Vsync>
  </Display>
</Settings>
`
		entries := []Entry{
			{PatchType: "xml_set", EntryKey: "//Display/Resolution", EntryValue: "1440"},
			{PatchType: "xml_unset", EntryKey: "//Display/Vsync"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 2, result.Applied)
		assert.Contains(t, string(result.Output), "1440")
		assert.NotContains(t, string(result.Output), "Vsync")
	})

	t.Run("empty input produces valid document", func(t *testing.T) {
		t.Parallel()
		entries := []Entry{
			{PatchType: "xml_set", EntryKey: "//Settings/Resolution", EntryValue: "1080"},
		}
		result, err := Apply(entries, []byte{})
		require.NoError(t, err)
		// no match on empty doc: skipped, but no error and output is valid
		assert.Equal(t, 0, result.Applied)
		assert.Equal(t, 1, result.Skipped)
		assert.NotEmpty(t, result.Output)
	})

	t.Run("no entries returns input unchanged", func(t *testing.T) {
		t.Parallel()
		input := `<?xml version="1.0" encoding="UTF-8"?>
<Settings>
  <Display><Resolution>1080</Resolution></Display>
</Settings>
`
		result, err := Apply(nil, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, input, string(result.Output))
	})

	t.Run("malformed xml returns error", func(t *testing.T) {
		t.Parallel()
		input := `<Settings><unclosed>`
		entries := []Entry{
			{PatchType: "xml_set", EntryKey: "//Settings", EntryValue: "value"},
		}
		_, err := Apply(entries, []byte(input))
		assert.Error(t, err)
	})

	t.Run("predicate xpath targets single node", func(t *testing.T) {
		t.Parallel()
		input := `<?xml version="1.0" encoding="UTF-8"?>
<Settings>
  <Item id="1"><Value>old</Value></Item>
  <Item id="2"><Value>old</Value></Item>
</Settings>
`
		entries := []Entry{
			{PatchType: "xml_set", EntryKey: "//Item[@id='1']/Value", EntryValue: "new"},
		}
		result, err := Apply(entries, []byte(input))
		require.NoError(t, err)
		assert.Equal(t, 1, result.Applied)
		assert.Contains(t, string(result.Output), "new")
		// second Item untouched
		assert.Contains(t, string(result.Output), `id="2"`)
		out := string(result.Output)
		assert.Equal(t, 1, strings.Count(out, "new"))
		assert.Equal(t, 1, strings.Count(out, "old"))
	})
}
