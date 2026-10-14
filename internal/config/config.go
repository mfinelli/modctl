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

// Package config is the configuration of modctl: what it is when nothing has
// been said, what a config file says, and how the file is changed.
//
// Nothing in it finds anything by itself. Where the directories of the user
// are, and which file is the config file, are given to it, and so it makes
// nothing that it was not asked to: reading a configuration does not make the
// directories that it names, or the one that the config file is in.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/viper"
)

// Dirs are the directories of the user that the defaults are in, which are the
// XDG ones.
type Dirs struct {
	// ConfigHome is where the config file is ($XDG_CONFIG_HOME).
	ConfigHome string
	// DataHome is where what modctl keeps is ($XDG_DATA_HOME).
	DataHome string
	// CacheHome is where what can be made again is ($XDG_CACHE_HOME).
	CacheHome string
	// StateHome is where what is about what has been done is
	// ($XDG_STATE_HOME).
	StateHome string
	// RuntimeDir is where what is only needed while modctl runs is
	// ($XDG_RUNTIME_DIR).
	RuntimeDir string
	// TempDir is the temporary directory of the system ($TMPDIR, or /tmp).
	TempDir string
}

// State is the directory that modctl keeps its state in, which is the active
// selection and what has to be there while something is being done (the locks,
// in a directory of their own).
func (d Dirs) State() string {
	return filepath.Join(d.StateHome, "modctl")
}

// DefaultPath is the config file that is read when there is not one that has
// been asked for, and the one that is made when there is not any.
func DefaultPath(d Dirs) string {
	return filepath.Join(d.ConfigHome, "modctl", "config.toml")
}

// field is a key of the configuration, and where its value is in a Config.
type field struct {
	key   string
	value func(*Config) *string
}

// fields are the keys, in the order that they are listed in.
var fields = []field{
	{"bsdtar", func(c *Config) *string { return &c.Bsdtar }},
	{"database", func(c *Config) *string { return &c.Database }},
	{"archives_dir", func(c *Config) *string { return &c.ArchivesDir }},
	{"backups_dir", func(c *Config) *string { return &c.BackupsDir }},
	{"cache_dir", func(c *Config) *string { return &c.CacheDir }},
	{"locks_dir", func(c *Config) *string { return &c.LocksDir }},
	{"overrides_dir", func(c *Config) *string { return &c.OverridesDir }},
	{"tmp_dir", func(c *Config) *string { return &c.TmpDir }},
	{"scratch_dir", func(c *Config) *string { return &c.ScratchDir }},
	{"nexus.apikey", func(c *Config) *string { return &c.NexusAPIKey }},
}

// Keys are the keys that there are, in the order that they are listed in. A key
// in the second level is named with a dot ("nexus.apikey"), which is how it is
// in the commands, and in the file it is a key of a table.
func Keys() []string {
	keys := make([]string, len(fields))
	for i, f := range fields {
		keys[i] = f.key
	}
	return keys
}

// A Config is what modctl is configured to: the defaults, with what the config
// file says on top of them.
type Config struct {
	// Bsdtar is the program that reads and extracts archives. It is found in
	// the PATH if it is not a path.
	Bsdtar string
	// Database is the file of the database.
	Database string
	// ArchivesDir, BackupsDir and OverridesDir are where the blobs of each kind
	// are kept.
	ArchivesDir  string
	BackupsDir   string
	OverridesDir string
	CacheDir     string
	LocksDir     string
	TmpDir       string
	// ScratchDir is where there is room for a copy of the data: the snapshots
	// of the databases that an export makes, and the bundle that is extracted
	// to import, verify or extract it. It is not TmpDir, which is for the
	// staging of the archives that are applied, and which is by default on
	// tmpfs.
	ScratchDir  string
	NexusAPIKey string
	File        string // the config file that was read, if one was
	fromFile    map[string]bool
}

// Defaults is the configuration of a user whose directories are dirs, who has
// not said anything.
func Defaults(d Dirs) Config {
	return Config{
		Bsdtar:       "bsdtar",
		Database:     filepath.Join(d.DataHome, "modctl", "modctl.db"),
		ArchivesDir:  filepath.Join(d.DataHome, "modctl", "archives"),
		BackupsDir:   filepath.Join(d.DataHome, "modctl", "backups"),
		OverridesDir: filepath.Join(d.DataHome, "modctl", "overrides"),
		LocksDir:     filepath.Join(d.State(), "locks"),
		CacheDir:     filepath.Join(d.CacheHome, "modctl"),
		TmpDir:       filepath.Join(d.RuntimeDir, "modctl"),
		ScratchDir:   d.TempDir,
	}
}

// Value is the value of key, and whether there is such a key.
func (c Config) Value(key string) (string, bool) {
	for _, f := range fields {
		if f.key == key {
			return *f.value(&c), true
		}
	}
	return "", false
}

// FromFile is whether the config file says what key is, and not the defaults.
// A key that the file says is what it is by default is still said.
func (c Config) FromFile(key string) bool {
	return c.fromFile[key]
}

// Load is the configuration of a user whose directories are dirs. It is the
// defaults, and what the config file says over them: the file at path, which
// has to be there, or the default one (see DefaultPath) if path is empty, which
// does not have to be. A file that can't be read or is not a config file is an
// error, whichever one it is.
//
// The file is read by an instance of viper of its own, which is what knows how a
// config file is read (the keys are not case sensitive, and a value is the text
// of what it is), and which nothing else in the program shares.
func Load(d Dirs, path string) (Config, error) {
	c := Defaults(d)

	if path == "" {
		path = DefaultPath(d)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			// a config file that was not asked for, and is not there: the
			// defaults are what it is
			return c, nil
		}
	}

	v := viper.New()
	for _, f := range fields {
		v.SetDefault(f.key, *f.value(&c))
	}
	v.SetConfigFile(path)
	v.SetConfigType("toml")
	if err := v.ReadInConfig(); err != nil {
		return Config{}, err
	}

	c.File = v.ConfigFileUsed()
	c.fromFile = make(map[string]bool)
	for _, f := range fields {
		*f.value(&c) = v.GetString(f.key)
		c.fromFile[f.key] = v.InConfig(f.key)
	}

	return c, nil
}

// UnknownKeyError is a key that there is not.
type UnknownKeyError struct {
	Key string
}

func (e UnknownKeyError) Error() string {
	return fmt.Sprintf("unknown config key %q\n  valid keys: %s", e.Key, strings.Join(Keys(), ", "))
}

func checkKey(key string) error {
	if !slices.Contains(Keys(), key) {
		return UnknownKeyError{Key: key}
	}
	return nil
}

// Set makes the config file at path say that key is value. The file is made if
// it is not there (and so is the directory that it is in, which only the user
// can read), and what else it says is kept as it is, apart from how it is
// written: the comments and the formatting of a file that was written by hand
// are not.
func Set(path, key, value string) error {
	if err := checkKey(key); err != nil {
		return err
	}

	return edit(path, func(m map[string]any) {
		setIn(m, strings.Split(key, "."), value)
	})
}

// Unset makes the config file at path not say anything about key, so that it is
// what it is by default. A table that has nothing else in it is not left.
func Unset(path, key string) error {
	if err := checkKey(key); err != nil {
		return err
	}

	return edit(path, func(m map[string]any) {
		unsetIn(m, strings.Split(key, "."))
	})
}

// foldKeys are the keys of table that are key, whatever their case is (a
// config file is read that way, so it is changed that way: a key that is
// written next to another that is the same key in another case would not be
// the one that is read).
func foldKeys(table map[string]any, key string) []string {
	var names []string
	for name := range table {
		if strings.EqualFold(name, key) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// takeTable removes the tables that are key from table, and returns what was in
// them (as one table, if there was more than one), or nil if there was none.
// What is called key and is not a table is left where it is.
func takeTable(table map[string]any, key string) map[string]any {
	var merged map[string]any
	for _, name := range foldKeys(table, key) {
		t, ok := table[name].(map[string]any)
		if !ok {
			continue
		}
		if merged == nil {
			merged = make(map[string]any)
		}
		for k, v := range t {
			if _, has := merged[k]; !has {
				merged[k] = v
			}
		}
		delete(table, name)
	}
	return merged
}

func setIn(table map[string]any, path []string, value string) {
	if len(path) == 1 {
		for _, name := range foldKeys(table, path[0]) {
			delete(table, name)
		}
		table[path[0]] = value
		return
	}

	next := takeTable(table, path[0])
	// what is not a table where there is to be one is not kept
	for _, name := range foldKeys(table, path[0]) {
		delete(table, name)
	}
	if next == nil {
		next = make(map[string]any)
	}
	setIn(next, path[1:], value)
	table[path[0]] = next
}

func unsetIn(table map[string]any, path []string) {
	if len(path) == 1 {
		for _, name := range foldKeys(table, path[0]) {
			delete(table, name)
		}
		return
	}

	next := takeTable(table, path[0])
	if next == nil {
		return
	}
	unsetIn(next, path[1:])
	if len(next) > 0 {
		table[path[0]] = next
	}
}

// edit changes the config file at path, which is made if it is not there, by
// what change does to what it says.
func edit(path string, change func(map[string]any)) error {
	if err := ensureFile(path); err != nil {
		return fmt.Errorf("create config file: %w", err)
	}

	m, err := readTable(path)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}

	change(m)

	data, err := toml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}

// ensureFile makes the file at path, if it is not there, so that only the user
// can read it (there can be an API key in it).
func ensureFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	return f.Close()
}

// readTable is what the file at path says, as it is in the file. A file that
// has nothing in it says nothing.
func readTable(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := make(map[string]any)
	if len(strings.TrimSpace(string(data))) == 0 {
		return out, nil
	}
	if err := toml.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Mask is key (an API key) as it is shown: with all of it but the last four
// characters not there.
func Mask(key string) string {
	if len(key) <= 4 {
		return strings.Repeat("*", len(key))
	}
	return strings.Repeat("*", len(key)-4) + key[len(key)-4:]
}
