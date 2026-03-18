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

func TestApply_EmptyEntries(t *testing.T) {
	t.Parallel()

	input := "[Display]\nfMaxTime=0.033\n"
	result, err := Apply([]Entry{}, []byte(input))
	require.NoError(t, err)
	assert.Equal(t, input, string(result.Output))
	assert.Equal(t, 0, result.Applied)
	assert.Equal(t, 0, result.Skipped)
}

func TestApply_UnknownPatchType(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{PatchType: "toml_set", EntryKey: "key", EntryValue: "value"},
	}
	_, err := Apply(entries, []byte("key = \"value\""))
	assert.Error(t, err)
}
