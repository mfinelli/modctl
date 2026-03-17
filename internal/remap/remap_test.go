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

package remap

import (
	"database/sql"
	"testing"

	"github.com/mfinelli/modctl/dbq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helpers to build RemapRule values without noise in table cases

func stripRule(pos int64, n int64) dbq.RemapRule {
	return dbq.RemapRule{
		Position: pos,
		RuleType: "strip_components",
		IntValue: sql.NullInt64{Int64: n, Valid: true},
	}
}

func subdirRule(pos int64, subdir string) dbq.RemapRule {
	return dbq.RemapRule{
		Position:  pos,
		RuleType:  "select_subdir",
		TextValue: sql.NullString{String: subdir, Valid: true},
	}
}

func prefixRule(pos int64, prefix string) dbq.RemapRule {
	return dbq.RemapRule{
		Position:  pos,
		RuleType:  "dest_prefix",
		TextValue: sql.NullString{String: prefix, Valid: true},
	}
}

func includeRule(pos int64, pattern string) dbq.RemapRule {
	return dbq.RemapRule{
		Position:  pos,
		RuleType:  "include_glob",
		TextValue: sql.NullString{String: pattern, Valid: true},
	}
}

func excludeRule(pos int64, pattern string) dbq.RemapRule {
	return dbq.RemapRule{
		Position:  pos,
		RuleType:  "exclude_glob",
		TextValue: sql.NullString{String: pattern, Valid: true},
	}
}
func TestApply(t *testing.T) {
	t.Parallel()

	t.Run("no rules", func(t *testing.T) {
		t.Parallel()
		result, err := Apply(nil, "foo/bar/baz.esp")
		require.NoError(t, err)
		assert.False(t, result.Skip)
		assert.Equal(t, "foo/bar/baz.esp", result.Path)
	})

	t.Run("strip_components", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name     string
			n        int64
			input    string
			wantPath string
			wantSkip bool
		}{
			{
				name:     "strip one",
				n:        1,
				input:    "ModName/Data/textures/rock.dds",
				wantPath: "Data/textures/rock.dds",
			},
			{
				name:     "strip two",
				n:        2,
				input:    "ModName/Data/textures/rock.dds",
				wantPath: "textures/rock.dds",
			},
			{
				name:     "strip zero is noop",
				n:        0,
				input:    "foo/bar.esp",
				wantPath: "foo/bar.esp",
			},
			{
				name:     "strip exactly all segments skips",
				n:        2,
				input:    "foo/bar",
				wantSkip: true,
			},
			{
				name:     "strip more than segments skips",
				n:        5,
				input:    "foo/bar/baz.esp",
				wantSkip: true,
			},
			{
				name:     "strip one from flat file skips",
				n:        1,
				input:    "bar.esp",
				wantSkip: true,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				result, err := Apply([]dbq.RemapRule{stripRule(0, tc.n)}, tc.input)
				require.NoError(t, err)
				assert.Equal(t, tc.wantSkip, result.Skip)
				if !tc.wantSkip {
					assert.Equal(t, tc.wantPath, result.Path)
				}
			})
		}
	})

	t.Run("select_subdir", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name     string
			subdir   string
			input    string
			wantPath string
			wantSkip bool
		}{
			{
				name:     "entry under subdir is included and stripped",
				subdir:   "Data",
				input:    "Data/textures/rock.dds",
				wantPath: "textures/rock.dds",
			},
			{
				name:     "entry not under subdir is skipped",
				subdir:   "Data",
				input:    "Docs/readme.txt",
				wantSkip: true,
			},
			{
				name:     "entry is the subdir itself is skipped",
				subdir:   "Data",
				input:    "Data",
				wantSkip: true,
			},
			{
				name:     "partial prefix match is not a match",
				subdir:   "Data",
				input:    "DataExtra/foo.esp",
				wantSkip: true,
			},
			{
				name:     "nested subdir",
				subdir:   "Data/textures",
				input:    "Data/textures/rock.dds",
				wantPath: "rock.dds",
			},
			{
				name:     "entry directly in subdir",
				subdir:   "Data",
				input:    "Data/plugin.esp",
				wantPath: "plugin.esp",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				result, err := Apply([]dbq.RemapRule{subdirRule(0, tc.subdir)}, tc.input)
				require.NoError(t, err)
				assert.Equal(t, tc.wantSkip, result.Skip)
				if !tc.wantSkip {
					assert.Equal(t, tc.wantPath, result.Path)
				}
			})
		}
	})

	t.Run("dest_prefix", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name     string
			prefix   string
			input    string
			wantPath string
		}{
			{
				name:     "prepends prefix",
				prefix:   "Data",
				input:    "textures/rock.dds",
				wantPath: "Data/textures/rock.dds",
			},
			{
				name:     "nested prefix",
				prefix:   "Data/textures",
				input:    "rock.dds",
				wantPath: "Data/textures/rock.dds",
			},
			{
				name:     "prefix with trailing slash is normalised",
				prefix:   "Data/",
				input:    "foo.esp",
				wantPath: "Data/foo.esp",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				result, err := Apply([]dbq.RemapRule{prefixRule(0, tc.prefix)}, tc.input)
				require.NoError(t, err)
				assert.False(t, result.Skip)
				assert.Equal(t, tc.wantPath, result.Path)
			})
		}
	})

	t.Run("include_glob", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name     string
			pattern  string
			input    string
			wantSkip bool
		}{
			{
				name:    "matching entry is kept",
				pattern: "*.esp",
				input:   "plugin.esp",
			},
			{
				name:     "non-matching entry is skipped",
				pattern:  "*.esp",
				input:    "textures/rock.dds",
				wantSkip: true,
			},
			{
				name:    "wildcard matches nested path segment",
				pattern: "textures/*",
				input:   "textures/rock.dds",
			},
			{
				name:     "wildcard does not match across separators",
				pattern:  "*.dds",
				input:    "textures/rock.dds",
				wantSkip: true,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				result, err := Apply([]dbq.RemapRule{includeRule(0, tc.pattern)}, tc.input)
				require.NoError(t, err)
				assert.Equal(t, tc.wantSkip, result.Skip)
			})
		}
	})

	t.Run("exclude_glob", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name     string
			pattern  string
			input    string
			wantSkip bool
		}{
			{
				name:     "matching entry is skipped",
				pattern:  "*.txt",
				input:    "readme.txt",
				wantSkip: true,
			},
			{
				name:    "non-matching entry is kept",
				pattern: "*.txt",
				input:   "plugin.esp",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				result, err := Apply([]dbq.RemapRule{excludeRule(0, tc.pattern)}, tc.input)
				require.NoError(t, err)
				assert.Equal(t, tc.wantSkip, result.Skip)
			})
		}
	})

	t.Run("composition", func(t *testing.T) {
		t.Parallel()

		t.Run("strip then select_subdir sees stripped path", func(t *testing.T) {
			t.Parallel()
			// archive has: ModName/Data/textures/rock.dds
			// strip 1 -> Data/textures/rock.dds
			// select_subdir Data -> textures/rock.dds
			rules := []dbq.RemapRule{
				stripRule(0, 1),
				subdirRule(1, "Data"),
			}
			result, err := Apply(rules, "ModName/Data/textures/rock.dds")
			require.NoError(t, err)
			assert.False(t, result.Skip)
			assert.Equal(t, "textures/rock.dds", result.Path)
		})

		t.Run("select_subdir then dest_prefix", func(t *testing.T) {
			t.Parallel()
			rules := []dbq.RemapRule{
				subdirRule(0, "Data"),
				prefixRule(1, "MyMod"),
			}
			result, err := Apply(rules, "Data/plugin.esp")
			require.NoError(t, err)
			assert.False(t, result.Skip)
			assert.Equal(t, "MyMod/plugin.esp", result.Path)
		})

		t.Run("include then exclude narrows set", func(t *testing.T) {
			t.Parallel()
			// include all .dds, then exclude specific file
			rules := []dbq.RemapRule{
				includeRule(0, "*.dds"),
				excludeRule(1, "bad.dds"),
			}

			kept, err := Apply(rules, "good.dds")
			require.NoError(t, err)
			assert.False(t, kept.Skip)

			skipped, err := Apply(rules, "bad.dds")
			require.NoError(t, err)
			assert.True(t, skipped.Skip)
		})

		t.Run("strip leaves empty path after select_subdir skips", func(t *testing.T) {
			t.Parallel()
			rules := []dbq.RemapRule{
				stripRule(0, 1),
				subdirRule(1, "Data"),
			}
			// after strip: "readme.txt" - not under Data
			result, err := Apply(rules, "ModName/readme.txt")
			require.NoError(t, err)
			assert.True(t, result.Skip)
		})

		t.Run("full typical mod layout", func(t *testing.T) {
			t.Parallel()
			// ModName/Data/textures/rock.dds -> Data/textures/rock.dds
			// strip 1, select Data, dest_prefix Data reinstated
			// more realistic: strip wrapper dir, install as-is
			rules := []dbq.RemapRule{
				stripRule(0, 1),
			}
			result, err := Apply(rules, "ModName/Data/textures/rock.dds")
			require.NoError(t, err)
			assert.False(t, result.Skip)
			assert.Equal(t, "Data/textures/rock.dds", result.Path)
		})
	})

	t.Run("invalid glob returns error", func(t *testing.T) {
		t.Parallel()
		rules := []dbq.RemapRule{includeRule(0, "[")}
		_, err := Apply(rules, "foo.esp")
		require.Error(t, err)
		var globErr *InvalidGlobError
		assert.ErrorAs(t, err, &globErr)
		assert.Equal(t, "[", globErr.Pattern)
	})
}
