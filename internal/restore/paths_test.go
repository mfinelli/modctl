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
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal/blobstore"
	"github.com/mfinelli/modctl/internal/exporter"
	"github.com/mfinelli/modctl/internal/testbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bundleGame is a game in the database of a bundle (which is a database like
// any other), with a profile and a target that overrides can be for.
type bundleGame struct {
	t         *testing.T
	db        *sql.DB
	q         *dbq.Queries
	game      dbq.GameInstall
	profileID int64
	target    dbq.Target
}

func newBundleGame(t *testing.T, db *sql.DB, storeGameID string) *bundleGame {
	t.Helper()

	q := dbq.New(db)
	game := testbuilder.NewGame(t, db).WithStoreGameID(storeGameID).WithInstallRoot(t.TempDir()).Build()
	target, err := q.UpsertDiscoveredTarget(context.Background(), dbq.UpsertDiscoveredTargetParams{
		GameInstallID: game.ID,
		Name:          "game_dir",
		RootPath:      game.InstallRoot,
	})
	require.NoError(t, err)
	profile, err := q.GetActiveProfileForGame(context.Background(), game.ID)
	require.NoError(t, err)

	return &bundleGame{t: t, db: db, q: q, game: game, profileID: profile.ID, target: target}
}

// blobSeq makes the blobs of all the games different from each other, which a
// game that is in a bundle and in the database that it is imported into is
// otherwise not: they would be the same blob.
var blobSeq atomic.Int64

func (g *bundleGame) blob(kind string) string {
	g.t.Helper()

	sha := fmt.Sprintf("%064x", blobSeq.Add(1))
	require.NoError(g.t, g.q.InsertBlob(context.Background(), dbq.InsertBlobParams{Sha256: sha, Kind: kind, SizeBytes: 1}))
	return sha
}

// addTarget adds a target to the game.
func (g *bundleGame) addTarget(name, origin, root string, enabled int) {
	g.t.Helper()

	_, err := g.db.Exec(`INSERT INTO targets (game_install_id, name, root_path, origin, created_at, updated_at, enabled)
		VALUES (?, ?, ?, ?, '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:00.000Z', ?)`,
		g.game.ID, name, root, origin, enabled)
	require.NoError(g.t, err)
}

// rename gives the game another name, which tells it from another game that is
// the same as it in everything else.
func (g *bundleGame) rename(name string) {
	g.t.Helper()

	_, err := g.db.Exec(`UPDATE game_installs SET display_name = ? WHERE id = ?`, name, g.game.ID)
	require.NoError(g.t, err)
	g.game.DisplayName = name
}

func (g *bundleGame) override(relpath string) {
	g.t.Helper()

	_, err := g.db.Exec(`INSERT INTO overrides (profile_id, target_id, relpath, blob_sha256, override_type) VALUES (?, ?, ?, ?, 'full_file')`,
		g.profileID, g.target.ID, relpath, g.blob("override"))
	require.NoError(g.t, err)
}

func (g *bundleGame) backup(relpath string) {
	g.t.Helper()

	require.NoError(g.t, g.q.UpsertBackup(context.Background(), dbq.UpsertBackupParams{
		GameInstallID:    g.game.ID,
		TargetID:         g.target.ID,
		Relpath:          relpath,
		BackupBlobSha256: g.blob("backup"),
		SizeBytes:        1,
	}))
}

func (g *bundleGame) installed(relpath string) {
	g.t.Helper()

	require.NoError(g.t, g.q.UpsertInstalledFile(context.Background(), dbq.UpsertInstalledFileParams{
		GameInstallID: g.game.ID,
		TargetID:      g.target.ID,
		Relpath:       relpath,
		ContentSha256: g.blob("archive"),
		SizeBytes:     1,
	}))
}

func TestCheckRelpaths(t *testing.T) {
	t.Parallel()

	t.Run("nothing to check", func(t *testing.T) {
		t.Parallel()

		assert.NoError(t, checkRelpaths("overrides", nil))
	})

	t.Run("paths that are inside", func(t *testing.T) {
		t.Parallel()

		assert.NoError(t, checkRelpaths("overrides", []string{"a.cfg", "Data/b.cfg", "Data/../c.cfg"}))
	})

	t.Run("a path that is not is an error that says which and why", func(t *testing.T) {
		t.Parallel()

		err := checkRelpaths("overrides", []string{"a.cfg", "../escape.cfg", "b.cfg"})

		require.ErrorIs(t, err, ErrUnsafePaths)
		assert.Contains(t, err.Error(), "1 of its overrides")
		assert.Contains(t, err.Error(), `"../escape.cfg" (path traversal not allowed)`)
		assert.NotContains(t, err.Error(), "a.cfg")
	})

	t.Run("every kind of path that is not inside", func(t *testing.T) {
		t.Parallel()

		err := checkRelpaths("backups", []string{"/abs/x.cfg", "a/../../x.cfg", "."})

		require.ErrorIs(t, err, ErrUnsafePaths)
		assert.Contains(t, err.Error(), "3 of its backups")
		assert.Contains(t, err.Error(), "absolute path not allowed")
		assert.Contains(t, err.Error(), "path traversal not allowed")
		assert.Contains(t, err.Error(), "empty or dot path not allowed")
	})

	t.Run("only some of them are listed", func(t *testing.T) {
		t.Parallel()

		var paths []string
		for i := range 8 {
			paths = append(paths, fmt.Sprintf("../escape%d.cfg", i))
		}

		err := checkRelpaths("installed files", paths)

		require.ErrorIs(t, err, ErrUnsafePaths)
		assert.Contains(t, err.Error(), "8 of its installed files")
		assert.Contains(t, err.Error(), "escape0.cfg")
		assert.Contains(t, err.Error(), "escape4.cfg")
		assert.NotContains(t, err.Error(), "escape5.cfg")
		assert.Contains(t, err.Error(), "and 3 more")
	})
}

func TestValidateGamePaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("a game with overrides that are inside", func(t *testing.T) {
		t.Parallel()
		g := newBundleGame(t, testbuilder.SetupDB(t), "1")
		g.override("settings/user.cfg")
		g.override("Data/../Data/other.cfg")

		assert.NoError(t, validateGamePaths(ctx, g.q, g.game.ID))
	})

	t.Run("a game with no overrides", func(t *testing.T) {
		t.Parallel()
		g := newBundleGame(t, testbuilder.SetupDB(t), "1")

		assert.NoError(t, validateGamePaths(ctx, g.q, g.game.ID))
	})

	t.Run("an override that is not inside", func(t *testing.T) {
		t.Parallel()
		g := newBundleGame(t, testbuilder.SetupDB(t), "1")
		g.override("settings/user.cfg")
		g.override("../escape.cfg")

		err := validateGamePaths(ctx, g.q, g.game.ID)

		require.ErrorIs(t, err, ErrUnsafePaths)
		assert.Contains(t, err.Error(), "../escape.cfg")
	})

	t.Run("only the overrides of that game count", func(t *testing.T) {
		t.Parallel()
		db := testbuilder.SetupDB(t)
		fine := newBundleGame(t, db, "1")
		other := newBundleGame(t, db, "2")
		fine.override("user.cfg")
		other.override("../escape.cfg")

		assert.NoError(t, validateGamePaths(ctx, fine.q, fine.game.ID))
		assert.ErrorIs(t, validateGamePaths(ctx, other.q, other.game.ID), ErrUnsafePaths)
	})

	t.Run("backups and installed files are not what a game bundle has", func(t *testing.T) {
		t.Parallel()
		g := newBundleGame(t, testbuilder.SetupDB(t), "1")
		g.backup("../escape.cfg")
		g.installed("../escape.cfg")

		assert.NoError(t, validateGamePaths(ctx, g.q, g.game.ID))
	})
}

func TestValidateFullPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("everything inside", func(t *testing.T) {
		t.Parallel()
		g := newBundleGame(t, testbuilder.SetupDB(t), "1")
		g.override("user.cfg")
		g.backup("Data/a.dll")
		g.installed("Data/b.dll")

		assert.NoError(t, validateFullPaths(ctx, g.q, true))
		assert.NoError(t, validateFullPaths(ctx, g.q, false))
	})

	t.Run("an override that is not inside, whatever the kind of restore", func(t *testing.T) {
		t.Parallel()
		g := newBundleGame(t, testbuilder.SetupDB(t), "1")
		g.override("../escape.cfg")

		assert.ErrorIs(t, validateFullPaths(ctx, g.q, false), ErrUnsafePaths)
		assert.ErrorIs(t, validateFullPaths(ctx, g.q, true), ErrUnsafePaths)
	})

	t.Run("overrides of any of the games are looked at", func(t *testing.T) {
		t.Parallel()
		db := testbuilder.SetupDB(t)
		fine := newBundleGame(t, db, "1")
		other := newBundleGame(t, db, "2")
		fine.override("user.cfg")
		other.override("../escape.cfg")

		assert.ErrorIs(t, validateFullPaths(ctx, fine.q, false), ErrUnsafePaths)
	})

	t.Run("a backup that is not inside, which is only kept on the same machine", func(t *testing.T) {
		t.Parallel()
		g := newBundleGame(t, testbuilder.SetupDB(t), "1")
		g.backup("../escape.cfg")

		assert.NoError(t, validateFullPaths(ctx, g.q, false), "it is zeroed")
		err := validateFullPaths(ctx, g.q, true)
		require.ErrorIs(t, err, ErrUnsafePaths)
		assert.Contains(t, err.Error(), "backups")
	})

	t.Run("a file that is installed and not inside, which is only kept on the same machine", func(t *testing.T) {
		t.Parallel()
		g := newBundleGame(t, testbuilder.SetupDB(t), "1")
		g.installed("../../etc/escape.cfg")

		assert.NoError(t, validateFullPaths(ctx, g.q, false), "it is zeroed")
		err := validateFullPaths(ctx, g.q, true)
		require.ErrorIs(t, err, ErrUnsafePaths)
		assert.Contains(t, err.Error(), "installed files")
	})
}

func testBundle(t *testing.T, bundleDB *sql.DB, game dbq.GameInstall, kind exporter.ExportKind) *Bundle {
	t.Helper()

	latest, err := testbuilder.Schema().LatestVersion()
	require.NoError(t, err)

	return &Bundle{
		Manifest: exporter.Manifest{
			ExportFormatVersion: 1,
			ExportKind:          kind,
			SchemaVersion:       latest,
			Game: &exporter.ManifestGame{
				StoreID:     game.StoreID,
				StoreGameID: game.StoreGameID,
				DisplayName: game.DisplayName,
			},
		},
		BundleDir: t.TempDir(),
		BundleDB:  bundleDB,
	}
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// A restore has to refuse a bundle like this before it changes anything.
func TestGameRefusesAnUnsafeBundle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// the game is already there, with something of its own
	dest := testbuilder.SetupDB(t)
	existing := newBundleGame(t, dest, "1")
	existing.override("my-own.cfg")

	// the bundle has the same game, and an override that gets out of it
	bundleDB := testbuilder.SetupDB(t)
	inBundle := newBundleGame(t, bundleDB, "1")
	inBundle.override("../escape.cfg")
	bundle := testBundle(t, bundleDB, inBundle.game, exporter.ExportKindGame)

	for _, opts := range []Options{{Force: true}, {Force: true, DryRun: true}, {}} {
		opts.Schema = testbuilder.Schema()
		_, err := Game(ctx, dest, existing.q, blobstore.Store{}, bundle, opts, "test", quietLogger())

		require.ErrorIs(t, err, ErrUnsafePaths, "%+v", opts)
		assert.Contains(t, err.Error(), "../escape.cfg")

		// and nothing was done: the game that was there is still there, as it was
		_, getErr := existing.q.ImportGetGameInstallByStoreKey(ctx, dbq.ImportGetGameInstallByStoreKeyParams{
			StoreID:     existing.game.StoreID,
			StoreGameID: existing.game.StoreGameID,
			InstanceID:  existing.game.InstanceID,
		})
		require.NoError(t, getErr, "the game was deleted")
		var n int
		require.NoError(t, dest.QueryRow(`SELECT COUNT(*) FROM overrides WHERE relpath = 'my-own.cfg'`).Scan(&n))
		assert.Equal(t, 1, n, "what the game had was taken away")
	}
}

func TestGameLetsASafeBundleBeLookedAt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	dest := testbuilder.SetupDB(t)
	bundleDB := testbuilder.SetupDB(t)
	inBundle := newBundleGame(t, bundleDB, "1")
	inBundle.override("user.cfg")
	bundle := testBundle(t, bundleDB, inBundle.game, exporter.ExportKindGame)

	_, err := Game(ctx, dest, dbq.New(dest), blobstore.Store{}, bundle, Options{Schema: testbuilder.Schema(), DryRun: true}, "test", quietLogger())

	assert.NoError(t, err)
}

func TestFullRefusesAnUnsafeBundle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	for _, tc := range []struct {
		name string
		fill func(g *bundleGame)
		opts Options
	}{
		{"an override", func(g *bundleGame) { g.override("../escape.cfg") }, Options{Schema: testbuilder.Schema(), Force: true}},
		{"an override, in a dry run", func(g *bundleGame) { g.override("../escape.cfg") }, Options{Schema: testbuilder.Schema(), DryRun: true}},
		{"a backup that is kept", func(g *bundleGame) { g.backup("../escape.cfg") }, Options{Schema: testbuilder.Schema(), Force: true, SameMachine: true}},
		{"a file that is installed and kept", func(g *bundleGame) { g.installed("../escape.cfg") }, Options{Schema: testbuilder.Schema(), Force: true, SameMachine: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dest := testbuilder.SetupDB(t)
			bundleDB := testbuilder.SetupDB(t)
			g := newBundleGame(t, bundleDB, "1")
			tc.fill(g)
			bundle := testBundle(t, bundleDB, g.game, exporter.ExportKindFull)

			// the database that is there is a file, which a full restore replaces
			dbPath := filepath.Join(t.TempDir(), "modctl.db")
			require.NoError(t, os.WriteFile(dbPath, []byte("the database as it was"), 0o644))

			_, err := Full(ctx, dest, dbq.New(dest), blobstore.Store{}, bundle, tc.opts, dbPath, "test", quietLogger())

			require.ErrorIs(t, err, ErrUnsafePaths)
			data, readErr := os.ReadFile(dbPath)
			require.NoError(t, readErr)
			assert.Equal(t, "the database as it was", string(data), "it was removed or replaced")
			assert.NoError(t, dest.PingContext(ctx), "it was closed")
		})
	}
}

func TestCheckPaths(t *testing.T) {
	t.Parallel()

	t.Run("a bundle that is fine", func(t *testing.T) {
		t.Parallel()
		db := testbuilder.SetupDB(t)
		g := newBundleGame(t, db, "1")
		g.override("user.cfg")
		g.backup("Data/a.dll")
		g.installed("Data/b.dll")

		assert.NoError(t, CheckPaths(context.Background(), &Bundle{BundleDB: db}))
	})

	t.Run("as strict as a restore on the same machine", func(t *testing.T) {
		t.Parallel()
		for name, fill := range map[string]func(g *bundleGame){
			"an override":       func(g *bundleGame) { g.override("../escape.cfg") },
			"a backup":          func(g *bundleGame) { g.backup("../escape.cfg") },
			"an installed file": func(g *bundleGame) { g.installed("../escape.cfg") },
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				db := testbuilder.SetupDB(t)
				fill(newBundleGame(t, db, "1"))

				assert.ErrorIs(t, CheckPaths(context.Background(), &Bundle{BundleDB: db}), ErrUnsafePaths)
			})
		}
	})
}
