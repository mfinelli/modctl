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

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dirsIn are the directories of a user whose home is home, which are the ones
// of the specification.
func dirsIn(home string) Dirs {
	return Dirs{
		ConfigHome: filepath.Join(home, ".config"),
		DataHome:   filepath.Join(home, ".local", "share"),
		CacheHome:  filepath.Join(home, ".cache"),
		StateHome:  filepath.Join(home, ".local", "state"),
		RuntimeDir: filepath.Join("/run", "user", "1000"),
	}
}

// writeFile writes a config file at path (and the directories that lead to it).
func writeFile(t *testing.T, path, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// entries is what there is in dir, all the way down: what a test has not
// asked for must not be there.
func entries(t *testing.T, dir string) []string {
	t.Helper()

	var found []string
	require.NoError(t, filepath.WalkDir(dir, func(p string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != dir {
			rel, _ := filepath.Rel(dir, p)
			found = append(found, rel)
		}
		return nil
	}))
	return found
}

func TestDirsState(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/home/me/.local/state/modctl", dirsIn("/home/me").State())
}

func TestDefaultPath(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/home/me/.config/modctl/config.toml", DefaultPath(dirsIn("/home/me")))
}

func TestKeys(t *testing.T) {
	t.Parallel()

	t.Run("they are listed in the order of the documentation", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, []string{
			"bsdtar", "database", "archives_dir", "backups_dir", "cache_dir",
			"locks_dir", "overrides_dir", "tmp_dir", "nexus.apikey",
		}, Keys())
	})

	t.Run("there is a value for each of them", func(t *testing.T) {
		t.Parallel()

		c := Defaults(dirsIn("/home/me"))
		for _, key := range Keys() {
			_, ok := c.Value(key)
			assert.True(t, ok, key)
		}
	})

	t.Run("a list that is changed is not what the next one is", func(t *testing.T) {
		t.Parallel()

		keys := Keys()
		keys[0] = "something else"

		assert.Equal(t, "bsdtar", Keys()[0])
	})
}

func TestDefaults(t *testing.T) {
	t.Parallel()

	t.Run("they are in the directories of the user", func(t *testing.T) {
		t.Parallel()

		c := Defaults(dirsIn("/home/me"))

		assert.Equal(t, "bsdtar", c.Bsdtar)
		assert.Equal(t, "/home/me/.local/share/modctl/modctl.db", c.Database)
		assert.Equal(t, "/home/me/.local/share/modctl/archives", c.ArchivesDir)
		assert.Equal(t, "/home/me/.local/share/modctl/backups", c.BackupsDir)
		assert.Equal(t, "/home/me/.local/share/modctl/overrides", c.OverridesDir)
		assert.Equal(t, "/home/me/.local/state/modctl/locks", c.LocksDir)
		assert.Equal(t, "/home/me/.cache/modctl", c.CacheDir)
		assert.Equal(t, "/run/user/1000/modctl", c.TmpDir)
		assert.Empty(t, c.NexusAPIKey)
	})

	t.Run("nothing is said by a file, and there is none", func(t *testing.T) {
		t.Parallel()

		c := Defaults(dirsIn("/home/me"))

		assert.Empty(t, c.File)
		for _, key := range Keys() {
			assert.False(t, c.FromFile(key), key)
		}
	})

	t.Run("it makes nothing", func(t *testing.T) {
		t.Parallel()

		home := t.TempDir()

		Defaults(dirsIn(home))

		assert.Empty(t, entries(t, home))
	})
}

func TestConfigValue(t *testing.T) {
	t.Parallel()

	t.Run("a key has the value that it is for", func(t *testing.T) {
		t.Parallel()

		c := Config{
			Bsdtar: "a", Database: "b", ArchivesDir: "c", BackupsDir: "d", CacheDir: "e",
			LocksDir: "f", OverridesDir: "g", TmpDir: "h", NexusAPIKey: "i",
		}

		for key, want := range map[string]string{
			"bsdtar": "a", "database": "b", "archives_dir": "c", "backups_dir": "d", "cache_dir": "e",
			"locks_dir": "f", "overrides_dir": "g", "tmp_dir": "h", "nexus.apikey": "i",
		} {
			got, ok := c.Value(key)
			assert.True(t, ok, key)
			assert.Equal(t, want, got, key)
		}
	})

	t.Run("a key that there is not is not there", func(t *testing.T) {
		t.Parallel()

		got, ok := Defaults(dirsIn("/home/me")).Value("not_a_key")

		assert.False(t, ok)
		assert.Empty(t, got)
	})

	t.Run("the keys are not case sensitive in the file, and are in the commands", func(t *testing.T) {
		t.Parallel()

		_, ok := Defaults(dirsIn("/home/me")).Value("DATABASE")

		assert.False(t, ok)
	})
}

func TestLoad(t *testing.T) {
	t.Parallel()

	t.Run("without a file it is the defaults, and nothing is made", func(t *testing.T) {
		t.Parallel()

		home := t.TempDir()
		d := dirsIn(home)

		c, err := Load(d, "")

		require.NoError(t, err)
		want := Defaults(d)
		assert.Equal(t, want, c)
		assert.Empty(t, c.File)
		assert.Empty(t, entries(t, home), "not even the directory that the config file would be in")
	})

	t.Run("the default file is read when it is there", func(t *testing.T) {
		t.Parallel()

		home := t.TempDir()
		d := dirsIn(home)
		writeFile(t, DefaultPath(d), "database = \"/elsewhere/db\"\n")

		c, err := Load(d, "")

		require.NoError(t, err)
		assert.Equal(t, "/elsewhere/db", c.Database)
		assert.Equal(t, DefaultPath(d), c.File)
		assert.Equal(t, Defaults(d).ArchivesDir, c.ArchivesDir, "what it does not say is what it is by default")
	})

	t.Run("the file that is asked for is read, and not the default one", func(t *testing.T) {
		t.Parallel()

		home := t.TempDir()
		d := dirsIn(home)
		writeFile(t, DefaultPath(d), "database = \"/default/db\"\n")
		asked := filepath.Join(t.TempDir(), "mine.toml")
		writeFile(t, asked, "database = \"/mine/db\"\n")

		c, err := Load(d, asked)

		require.NoError(t, err)
		assert.Equal(t, "/mine/db", c.Database)
		assert.Equal(t, asked, c.File)
	})

	t.Run("a file that is asked for and is not there is an error", func(t *testing.T) {
		t.Parallel()

		missing := filepath.Join(t.TempDir(), "nothing.toml")

		_, err := Load(dirsIn(t.TempDir()), missing)

		require.Error(t, err)
		assert.ErrorIs(t, err, os.ErrNotExist)
		assert.Equal(t, "open "+missing+": no such file or directory", err.Error())
	})

	t.Run("every key can be said", func(t *testing.T) {
		t.Parallel()

		d := dirsIn(t.TempDir())
		path := filepath.Join(t.TempDir(), "all.toml")
		writeFile(t, path, `
bsdtar = "/usr/bin/bsdtar"
database = "/x/db"
archives_dir = "/x/archives"
backups_dir = "/x/backups"
cache_dir = "/x/cache"
locks_dir = "/x/locks"
overrides_dir = "/x/overrides"
tmp_dir = "/x/tmp"

[nexus]
apikey = "the-key"
`)

		c, err := Load(d, path)

		require.NoError(t, err)
		for key, want := range map[string]string{
			"bsdtar": "/usr/bin/bsdtar", "database": "/x/db", "archives_dir": "/x/archives",
			"backups_dir": "/x/backups", "cache_dir": "/x/cache", "locks_dir": "/x/locks",
			"overrides_dir": "/x/overrides", "tmp_dir": "/x/tmp", "nexus.apikey": "the-key",
		} {
			got, _ := c.Value(key)
			assert.Equal(t, want, got, key)
			assert.True(t, c.FromFile(key), key)
		}
	})

	t.Run("a key that is a dotted key is the key of a table, as it is in toml", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "dotted.toml")
		writeFile(t, path, "nexus.apikey = \"the-key\"\n")

		c, err := Load(dirsIn(t.TempDir()), path)

		require.NoError(t, err)
		assert.Equal(t, "the-key", c.NexusAPIKey)
	})

	t.Run("what is said is from the file, and what is not is not", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "some.toml")
		writeFile(t, path, "database = \"/x/db\"\n")

		c, err := Load(dirsIn(t.TempDir()), path)

		require.NoError(t, err)
		assert.True(t, c.FromFile("database"))
		assert.False(t, c.FromFile("archives_dir"))
	})

	t.Run("a key that is said to be what it is by default is still said", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "same.toml")
		writeFile(t, path, "bsdtar = \"bsdtar\"\n")

		c, err := Load(dirsIn(t.TempDir()), path)

		require.NoError(t, err)
		assert.True(t, c.FromFile("bsdtar"))
	})

	t.Run("the keys are not case sensitive", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "case.toml")
		writeFile(t, path, "Database = \"/up/db\"\nARCHIVES_DIR = \"/up/archives\"\n[Nexus]\nAPIKey = \"secret\"\n")

		c, err := Load(dirsIn(t.TempDir()), path)

		require.NoError(t, err)
		assert.Equal(t, "/up/db", c.Database)
		assert.Equal(t, "/up/archives", c.ArchivesDir)
		assert.Equal(t, "secret", c.NexusAPIKey)
	})

	t.Run("a value that is empty is a value, and not what is by default", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "empty.toml")
		writeFile(t, path, "tmp_dir = \"\"\n")

		c, err := Load(dirsIn(t.TempDir()), path)

		require.NoError(t, err)
		assert.Empty(t, c.TmpDir)
		assert.True(t, c.FromFile("tmp_dir"))
	})

	t.Run("a key that there is not is left as it is", func(t *testing.T) {
		t.Parallel()

		d := dirsIn(t.TempDir())
		path := filepath.Join(t.TempDir(), "unknown.toml")
		writeFile(t, path, "something_else = 1\n[nexus]\nother = \"x\"\n[other_table]\nx = 1\n")

		c, err := Load(d, path)

		require.NoError(t, err)
		assert.Equal(t, Defaults(d).Database, c.Database)
		assert.Equal(t, path, c.File)
	})

	t.Run("a table where there should be a key says nothing about it", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "table.toml")
		writeFile(t, path, "nexus = \"not a table\"\n")

		c, err := Load(dirsIn(t.TempDir()), path)

		require.NoError(t, err)
		assert.Empty(t, c.NexusAPIKey)
		assert.False(t, c.FromFile("nexus.apikey"))
	})

	t.Run("a file that is not toml is an error, and says what it was doing", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "broken.toml")
		writeFile(t, path, "database = \"/x/db\"\n[nexus\napikey = ")

		_, err := Load(dirsIn(t.TempDir()), path)

		require.Error(t, err)
		var parse viper.ConfigParseError
		require.ErrorAs(t, err, &parse)
		assert.True(t, strings.HasPrefix(err.Error(), "While parsing config: toml: "), err.Error())
	})

	t.Run("the default file that is not toml is an error too", func(t *testing.T) {
		t.Parallel()

		d := dirsIn(t.TempDir())
		writeFile(t, DefaultPath(d), "[nexus\n")

		_, err := Load(d, "")

		var parse viper.ConfigParseError
		assert.ErrorAs(t, err, &parse)
	})

	t.Run("a default file that can't be read is an error, and not the defaults", func(t *testing.T) {
		t.Parallel()

		d := dirsIn(t.TempDir())
		// a directory where the file should be
		require.NoError(t, os.MkdirAll(DefaultPath(d), 0o755))

		_, err := Load(d, "")

		require.Error(t, err)
		assert.False(t, errors.Is(err, os.ErrNotExist))
	})

	t.Run("a default file that can't be looked for is an error, and not the defaults", func(t *testing.T) {
		t.Parallel()

		home := t.TempDir()
		d := dirsIn(home)
		// there is a file where the directory of the configuration should be
		require.NoError(t, os.WriteFile(d.ConfigHome, nil, 0o644))

		_, err := Load(d, "")

		require.Error(t, err)
		assert.False(t, errors.Is(err, os.ErrNotExist))
	})

	t.Run("a value that is not text is the text of what it is", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct{ content, want string }{
			"a number": {"database = 5\n", "5"},
			"a float":  {"database = 1.5\n", "1.5"},
			"a bool":   {"database = true\n", "true"},
			"a date":   {"database = 2026-10-13\n", "2026-10-13"},
		} {
			tc := tc
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				path := filepath.Join(t.TempDir(), "type.toml")
				writeFile(t, path, tc.content)

				c, err := Load(dirsIn(t.TempDir()), path)

				require.NoError(t, err)
				assert.Equal(t, tc.want, c.Database)
				assert.True(t, c.FromFile("database"))
			})
		}
	})

	t.Run("reading a file makes nothing", func(t *testing.T) {
		t.Parallel()

		home := t.TempDir()
		d := dirsIn(home)
		writeFile(t, DefaultPath(d), "database = \"/x/db\"\n")
		before := entries(t, home)

		_, err := Load(d, "")

		require.NoError(t, err)
		assert.Equal(t, before, entries(t, home))
	})

	t.Run("a configuration is its own, whichever way it was got", func(t *testing.T) {
		t.Parallel()

		d := dirsIn(t.TempDir())
		path := filepath.Join(t.TempDir(), "a.toml")
		writeFile(t, path, "database = \"/x/db\"\n")

		first, err := Load(d, path)
		require.NoError(t, err)
		second, err := Load(d, "")
		require.NoError(t, err)

		assert.Equal(t, "/x/db", first.Database)
		assert.Equal(t, Defaults(d).Database, second.Database)
		assert.False(t, second.FromFile("database"))
	})
}

func TestSet(t *testing.T) {
	t.Parallel()

	t.Run("the file is made, and the directory that it is in", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "config", "modctl", "config.toml")

		require.NoError(t, Set(path, "database", "/x/db"))

		assert.FileExists(t, path)
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "there can be an API key in it")
		dir, err := os.Stat(filepath.Dir(path))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), dir.Mode().Perm())
	})

	t.Run("what is set is what is read", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, Set(path, "archives_dir", "/x/archives"))
		require.NoError(t, Set(path, "nexus.apikey", "the-key"))

		c, err := Load(dirsIn(t.TempDir()), path)

		require.NoError(t, err)
		assert.Equal(t, "/x/archives", c.ArchivesDir)
		assert.Equal(t, "the-key", c.NexusAPIKey)
	})

	t.Run("a key is set over what it was", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, Set(path, "database", "/first"))

		require.NoError(t, Set(path, "database", "/second"))

		c, err := Load(dirsIn(t.TempDir()), path)
		require.NoError(t, err)
		assert.Equal(t, "/second", c.Database)
	})

	t.Run("what else the file says is kept", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "config.toml")
		writeFile(t, path, "tmp_dir = \"/x/tmp\"\nunknown_key = \"kept\"\n\n[nexus]\napikey = \"old\"\nother = \"kept too\"\n\n[other]\nx = 1\n")

		require.NoError(t, Set(path, "nexus.apikey", "new"))

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		text := string(data)
		assert.Contains(t, text, `tmp_dir = '/x/tmp'`)
		assert.Contains(t, text, `unknown_key = 'kept'`)
		assert.Contains(t, text, `other = 'kept too'`)
		assert.Contains(t, text, `apikey = 'new'`)
		assert.NotContains(t, text, "old")
		assert.Contains(t, text, "[other]")
	})

	t.Run("a file that has nothing in it is a file that says nothing", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "config.toml")
		writeFile(t, path, "\n  \n")

		require.NoError(t, Set(path, "database", "/x/db"))

		c, err := Load(dirsIn(t.TempDir()), path)
		require.NoError(t, err)
		assert.Equal(t, "/x/db", c.Database)
	})

	t.Run("a key that there is not is an error that says which are, and changes nothing", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")

		err := Set(path, "not_a_key", "x")

		var unknown UnknownKeyError
		require.ErrorAs(t, err, &unknown)
		assert.Equal(t, "not_a_key", unknown.Key)
		assert.Contains(t, err.Error(), `unknown config key "not_a_key"`)
		assert.Contains(t, err.Error(), "valid keys: bsdtar, database, archives_dir")
		assert.Empty(t, entries(t, dir), "not even the file")
	})

	t.Run("a file that is not toml is an error, and is left as it is", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "config.toml")
		writeFile(t, path, "[nexus\n")

		err := Set(path, "database", "/x/db")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read config file")
		data, _ := os.ReadFile(path)
		assert.Equal(t, "[nexus\n", string(data))
	})

	t.Run("a directory that can't be made is an error that says so", func(t *testing.T) {
		t.Parallel()

		blocker := filepath.Join(t.TempDir(), "config")
		require.NoError(t, os.WriteFile(blocker, nil, 0o644))

		err := Set(filepath.Join(blocker, "modctl", "config.toml"), "database", "/x/db")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "create config file")
	})

	t.Run("a key that is in the file in another case is the one that is set", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "config.toml")
		writeFile(t, path, "Database = \"/old\"\nDATABASE = \"/older\"\ntmp_dir = \"/x/tmp\"\n")

		require.NoError(t, Set(path, "database", "/new"))

		c, err := Load(dirsIn(t.TempDir()), path)
		require.NoError(t, err)
		assert.Equal(t, "/new", c.Database)
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, 1, strings.Count(strings.ToLower(string(data)), "database"), "it is said once")
		assert.Equal(t, "/x/tmp", c.TmpDir)
	})

	t.Run("a table that is in the file in another case is the one that is set in", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "config.toml")
		writeFile(t, path, "[Nexus]\nAPIKey = \"old\"\nother = \"kept\"\n")

		require.NoError(t, Set(path, "nexus.apikey", "new"))

		c, err := Load(dirsIn(t.TempDir()), path)
		require.NoError(t, err)
		assert.Equal(t, "new", c.NexusAPIKey)
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, 1, strings.Count(strings.ToLower(string(data)), "[nexus]"), "it is one table")
		assert.Contains(t, string(data), "kept", "with what else was in it")
	})

	t.Run("a table that is not a table is not kept, where there is to be one", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "config.toml")
		writeFile(t, path, "nexus = \"not a table\"\n")

		require.NoError(t, Set(path, "nexus.apikey", "the-key"))

		c, err := Load(dirsIn(t.TempDir()), path)
		require.NoError(t, err)
		assert.Equal(t, "the-key", c.NexusAPIKey)
	})
}

func TestUnset(t *testing.T) {
	t.Parallel()

	t.Run("a key that was said is not said any more", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, Set(path, "database", "/x/db"))
		require.NoError(t, Set(path, "tmp_dir", "/x/tmp"))

		require.NoError(t, Unset(path, "database"))

		d := dirsIn(t.TempDir())
		c, err := Load(d, path)
		require.NoError(t, err)
		assert.Equal(t, Defaults(d).Database, c.Database)
		assert.False(t, c.FromFile("database"))
		assert.Equal(t, "/x/tmp", c.TmpDir, "and the others are")
	})

	t.Run("a table that has nothing else in it is not left", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, Set(path, "nexus.apikey", "the-key"))

		require.NoError(t, Unset(path, "nexus.apikey"))

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "nexus")
	})

	t.Run("a table that has something else in it is kept", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "config.toml")
		writeFile(t, path, "[nexus]\napikey = \"the-key\"\nother = \"kept\"\n")

		require.NoError(t, Unset(path, "nexus.apikey"))

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(data), "[nexus]")
		assert.Contains(t, string(data), "other = 'kept'")
		assert.NotContains(t, string(data), "apikey")
	})

	t.Run("a key that is in the file in another case is the one that is not said any more", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "config.toml")
		writeFile(t, path, "Database = \"/x/db\"\n[Nexus]\nAPIKey = \"the-key\"\n")

		require.NoError(t, Unset(path, "database"))
		require.NoError(t, Unset(path, "nexus.apikey"))

		d := dirsIn(t.TempDir())
		c, err := Load(d, path)
		require.NoError(t, err)
		assert.Equal(t, Defaults(d).Database, c.Database)
		assert.Empty(t, c.NexusAPIKey)
		assert.False(t, c.FromFile("nexus.apikey"))
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.NotContains(t, strings.ToLower(string(data)), "nexus", "and the table that has nothing in it is not left")
	})

	t.Run("a key that was not said is not a problem", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "config.toml")
		writeFile(t, path, "database = \"/x/db\"\n")

		require.NoError(t, Unset(path, "nexus.apikey"))
		require.NoError(t, Unset(path, "tmp_dir"))

		c, err := Load(dirsIn(t.TempDir()), path)
		require.NoError(t, err)
		assert.Equal(t, "/x/db", c.Database)
	})

	t.Run("a table that is not a table is left as it is", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "config.toml")
		writeFile(t, path, "nexus = \"not a table\"\n")

		require.NoError(t, Unset(path, "nexus.apikey"))

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(data), "not a table")
	})

	t.Run("a key that there is not is an error, and changes nothing", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		err := Unset(filepath.Join(dir, "config.toml"), "not_a_key")

		var unknown UnknownKeyError
		assert.ErrorAs(t, err, &unknown)
		assert.Empty(t, entries(t, dir))
	})
}

func TestMask(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, key, want string }{
		{"nothing is nothing", "", ""},
		{"a key of four is all of it", "abcd", "****"},
		{"a key of less is all of it", "ab", "**"},
		{"a key that is longer has its last four", "abcdefgh", "****efgh"},
		{"as many stars as what is not shown", "0123456789", "******6789"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, Mask(tc.key))
		})
	}
}
