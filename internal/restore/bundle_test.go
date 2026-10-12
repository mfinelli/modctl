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

package restore

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/database"
	"github.com/mfinelli/modctl/internal/exporter"
	"github.com/mfinelli/modctl/internal/nexusclient"
	"github.com/mfinelli/modctl/internal/testbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The bundles that are opened here are the ones that the exporter makes, so what
// is tested is the way there and back. They are in this package because the
// export migrates a database too, which needs the migrations that TestMain
// has set up for the tests of this package.

// exportOptions are the options of an export to a bundle in a directory of the
// test, whose snapshots are made in scratch.
func exportOptions(t *testing.T, scratch string) exporter.Options {
	t.Helper()

	return exporter.Options{
		ModctlVersion: "9.9.9",
		OutputPath:    filepath.Join(t.TempDir(), "bundle.tar.zst"),
		// there is no cache of what Nexus Mods said, unless a test makes one
		CacheDBPath: filepath.Join(t.TempDir(), "nexus_cache.db"),
		ScratchDir:  scratch,
	}
}

func storeInTestDir(t *testing.T) blobstore.Store {
	t.Helper()

	return blobstore.Store{
		ArchivesDir:  t.TempDir(),
		BackupsDir:   t.TempDir(),
		OverridesDir: t.TempDir(),
	}
}

// assertEmpty is that dir has nothing in it: that what was made in it has been
// cleaned up.
func assertEmpty(t *testing.T, dir string, msgAndArgs ...any) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	assert.Empty(t, names, msgAndArgs...)
}

// bundleEntry is a file in a bundle that is made by hand.
type bundleEntry struct {
	name string
	data []byte
}

// writeBundle makes a bundle (a tar that is compressed with zstd) that has
// what it is given, which is not necessarily what a bundle has.
func writeBundle(t *testing.T, entries ...bundleEntry) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "bundle.tar.zst")
	f, err := os.Create(path)
	require.NoError(t, err)
	zw, err := zstd.NewWriter(f)
	require.NoError(t, err)
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.data))}))
		_, err := tw.Write(e.data)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
	return path
}

func manifestEntry(t *testing.T, m exporter.Manifest) bundleEntry {
	t.Helper()

	data, err := json.Marshal(m)
	require.NoError(t, err)
	return bundleEntry{"manifest.json", data}
}

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestExportThenOpenAndValidate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("a full export is a bundle that is opened with everything in it", func(t *testing.T) {
		t.Parallel()

		db, _ := testbuilder.SetupDBFile(t)
		testbuilder.NewGame(t, db).WithName("Skyrim").WithStoreGameID("72850").Build()
		testbuilder.NewGame(t, db).WithName("Fallout 4").WithStoreGameID("377160").Build()
		scratch := t.TempDir()
		opts := exportOptions(t, scratch)

		_, err := exporter.Full(ctx, db, dbq.New(db), storeInTestDir(t), opts)
		require.NoError(t, err)
		assertEmpty(t, scratch, "what the export made in the scratch directory is gone")

		bundle, err := OpenAndValidate(ctx, opts.OutputPath, scratch)
		require.NoError(t, err)
		defer bundle.Close()

		latest, err := database.LatestSchemaVersion()
		require.NoError(t, err)
		assert.Equal(t, exporter.ExportKindFull, bundle.Manifest.ExportKind)
		assert.Equal(t, "9.9.9", bundle.Manifest.ModctlVersion)
		assert.Equal(t, latest, bundle.Manifest.SchemaVersion)
		assert.Equal(t, scratch, filepath.Dir(bundle.BundleDir), "it is extracted where it was told")
		assert.ElementsMatch(t, []string{"Skyrim", "Fallout 4"}, gameNames(t, bundle.BundleDB))
	})

	t.Run("a game export is a bundle that has only that game", func(t *testing.T) {
		t.Parallel()

		db, _ := testbuilder.SetupDBFile(t)
		skyrim := testbuilder.NewGame(t, db).WithName("Skyrim").WithStoreGameID("72850").Build()
		testbuilder.NewGame(t, db).WithName("Fallout 4").WithStoreGameID("377160").Build()
		scratch := t.TempDir()
		opts := exportOptions(t, scratch)

		_, err := exporter.Game(ctx, db, dbq.New(db), storeInTestDir(t), skyrim, opts)
		require.NoError(t, err)
		assertEmpty(t, scratch, "what the export made in the scratch directory is gone")

		bundle, err := OpenAndValidate(ctx, opts.OutputPath, scratch)
		require.NoError(t, err)
		defer bundle.Close()

		assert.Equal(t, exporter.ExportKindGame, bundle.Manifest.ExportKind)
		require.NotNil(t, bundle.Manifest.Game)
		assert.Equal(t, "Skyrim", bundle.Manifest.Game.DisplayName)
		assert.Equal(t, []string{"Skyrim"}, gameNames(t, bundle.BundleDB))
	})

	t.Run("what Nexus Mods said is in the bundle, when there is a cache of it", func(t *testing.T) {
		t.Parallel()

		db, _ := testbuilder.SetupDBFile(t)
		testbuilder.NewGame(t, db).Build()
		cacheDir := t.TempDir()
		cache, err := nexusclient.NewCacheReader(ctx, quietLogger(), cacheDir)
		require.NoError(t, err)
		require.NoError(t, cache.Close())
		scratch := t.TempDir()
		opts := exportOptions(t, scratch)
		opts.CacheDBPath = nexusclient.CacheDBPath(cacheDir)

		_, err = exporter.Full(ctx, db, dbq.New(db), storeInTestDir(t), opts)
		require.NoError(t, err)
		assertEmpty(t, scratch, "the snapshot of the cache is gone, too")

		bundle, err := OpenAndValidate(ctx, opts.OutputPath, scratch)
		require.NoError(t, err)
		defer bundle.Close()

		assert.NotEmpty(t, bundle.Manifest.NexusCacheSha256)
		assert.FileExists(t, filepath.Join(bundle.BundleDir, "nexus_cache.db"))
	})

	t.Run("closing the bundle takes away what was extracted", func(t *testing.T) {
		t.Parallel()

		db, _ := testbuilder.SetupDBFile(t)
		testbuilder.NewGame(t, db).Build()
		scratch := t.TempDir()
		opts := exportOptions(t, scratch)
		_, err := exporter.Full(ctx, db, dbq.New(db), storeInTestDir(t), opts)
		require.NoError(t, err)
		bundle, err := OpenAndValidate(ctx, opts.OutputPath, scratch)
		require.NoError(t, err)
		require.DirExists(t, bundle.BundleDir)

		bundle.Close()

		assertEmpty(t, scratch)
	})
}

func TestExportNeedsAScratchDirectory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	for _, tc := range []struct {
		name   string
		export func(t *testing.T, opts exporter.Options) error
	}{
		{"full", func(t *testing.T, opts exporter.Options) error {
			db, _ := testbuilder.SetupDBFile(t)
			_, err := exporter.Full(ctx, db, dbq.New(db), storeInTestDir(t), opts)
			return err
		}},
		{"game", func(t *testing.T, opts exporter.Options) error {
			db, _ := testbuilder.SetupDBFile(t)
			gi := testbuilder.NewGame(t, db).Build()
			_, err := exporter.Game(ctx, db, dbq.New(db), storeInTestDir(t), gi, opts)
			return err
		}},
	} {
		tc := tc
		t.Run(tc.name+" without one is an error, and nothing is made", func(t *testing.T) {
			t.Parallel()

			opts := exportOptions(t, "")

			err := tc.export(t, opts)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "no scratch directory")
			assert.NoFileExists(t, opts.OutputPath)
		})

		t.Run(tc.name+" with one that is not there is an error, and no output is left", func(t *testing.T) {
			t.Parallel()

			opts := exportOptions(t, filepath.Join(t.TempDir(), "not-there"))

			err := tc.export(t, opts)

			require.Error(t, err)
			assert.NoFileExists(t, opts.OutputPath, "a partial bundle is not kept")
		})
	}
}

func TestOpenAndValidate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbBytes := []byte("not really a database, but the bytes that the manifest hashes")

	t.Run("without a scratch directory it is an error", func(t *testing.T) {
		t.Parallel()

		bundle := writeBundle(t)

		got, err := OpenAndValidate(ctx, bundle, "")

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "no scratch directory")
	})

	t.Run("a scratch directory that is not there is an error that says so", func(t *testing.T) {
		t.Parallel()

		bundle := writeBundle(t)

		got, err := OpenAndValidate(ctx, bundle, filepath.Join(t.TempDir(), "not-there"))

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "create temp dir")
	})

	t.Run("a bundle that is not there is an error, and nothing is left", func(t *testing.T) {
		t.Parallel()

		scratch := t.TempDir()

		got, err := OpenAndValidate(ctx, filepath.Join(t.TempDir(), "nothing.tar.zst"), scratch)

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "extract bundle")
		assertEmpty(t, scratch)
	})

	t.Run("a file that is not a bundle is an error, and nothing is left", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "bundle.tar.zst")
		require.NoError(t, os.WriteFile(path, []byte("this is not zstd"), 0o644))
		scratch := t.TempDir()

		got, err := OpenAndValidate(ctx, path, scratch)

		require.Error(t, err)
		assert.Nil(t, got)
		assertEmpty(t, scratch)
	})

	for _, tc := range []struct {
		name    string
		entries func(t *testing.T) []bundleEntry
		want    string
	}{
		{
			"a bundle that has no manifest",
			func(t *testing.T) []bundleEntry { return []bundleEntry{{"modctl.db", dbBytes}} },
			"read manifest",
		},
		{
			"a manifest that is not json",
			func(t *testing.T) []bundleEntry { return []bundleEntry{{"manifest.json", []byte("{ nope")}} },
			"parse manifest",
		},
		{
			"a bundle of a format that is newer than this one",
			func(t *testing.T) []bundleEntry {
				return []bundleEntry{manifestEntry(t, exporter.Manifest{ExportFormatVersion: supportedFormatVersion + 1})}
			},
			"newer than supported",
		},
		{
			"a manifest that has no database in the bundle",
			func(t *testing.T) []bundleEntry {
				return []bundleEntry{manifestEntry(t, exporter.Manifest{ExportFormatVersion: 1, DBSha256: sha(dbBytes)})}
			},
			"hash bundle database",
		},
		{
			"a database that is not the one that the manifest says",
			func(t *testing.T) []bundleEntry {
				return []bundleEntry{
					manifestEntry(t, exporter.Manifest{ExportFormatVersion: 1, DBSha256: sha([]byte("another"))}),
					{"modctl.db", dbBytes},
				}
			},
			"bundle database integrity check failed",
		},
		{
			"a cache that is not the one that the manifest says",
			func(t *testing.T) []bundleEntry {
				return []bundleEntry{
					manifestEntry(t, exporter.Manifest{ExportFormatVersion: 1, DBSha256: sha(dbBytes), NexusCacheSha256: sha([]byte("another"))}),
					{"modctl.db", dbBytes},
					{"nexus_cache.db", []byte("the cache")},
				}
			},
			"bundle nexus cache integrity check failed",
		},
		{
			"a cache that the manifest says is there and is not",
			func(t *testing.T) []bundleEntry {
				return []bundleEntry{
					manifestEntry(t, exporter.Manifest{ExportFormatVersion: 1, DBSha256: sha(dbBytes), NexusCacheSha256: sha([]byte("the cache"))}),
					{"modctl.db", dbBytes},
				}
			},
			"hash bundle nexus cache",
		},
	} {
		tc := tc
		t.Run(tc.name+" is an error, and nothing is left", func(t *testing.T) {
			t.Parallel()

			scratch := t.TempDir()

			got, err := OpenAndValidate(ctx, writeBundle(t, tc.entries(t)...), scratch)

			require.Error(t, err)
			assert.Nil(t, got)
			assert.Contains(t, err.Error(), tc.want)
			assertEmpty(t, scratch)
		})
	}

	t.Run("a file of a bundle that is somewhere that is not in it is put in it", func(t *testing.T) {
		t.Parallel()

		parent := t.TempDir()
		scratch := filepath.Join(parent, "scratch")
		require.NoError(t, os.Mkdir(scratch, 0o755))
		// the way out of the directory that it is extracted in
		bundle := writeBundle(t,
			bundleEntry{"../../escaped.txt", []byte("not here")},
			bundleEntry{"../../../escaped-too.txt", []byte("not here")},
		)

		_, err := OpenAndValidate(ctx, bundle, scratch)

		require.Error(t, err, "there is no manifest in it")
		assert.NoFileExists(t, filepath.Join(parent, "escaped.txt"))
		assert.NoFileExists(t, filepath.Join(filepath.Dir(parent), "escaped-too.txt"))
		assert.NoFileExists(t, filepath.Join(scratch, "escaped.txt"))
		assertEmpty(t, scratch)
	})
}
