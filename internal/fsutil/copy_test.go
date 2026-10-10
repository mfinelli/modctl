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

package fsutil

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingWriter fails every write after the first limit bytes.
type failingWriter struct {
	limit int
	err   error
	buf   bytes.Buffer
}

func (w *failingWriter) Write(p []byte) (int, error) {
	room := w.limit - w.buf.Len()
	if room <= 0 {
		return 0, w.err
	}
	if len(p) > room {
		w.buf.Write(p[:room])
		return room, w.err
	}
	return w.buf.Write(p)
}

// shortWriter claims to have written less than it was given, without error.
type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

// cancelingReader cancels a context as soon as it has been read from once.
type cancelingReader struct {
	r      io.Reader
	cancel context.CancelFunc
}

func (c *cancelingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.cancel()
	return n, err
}

func TestCopyWithContext(t *testing.T) {
	t.Parallel()

	t.Run("copies everything", func(t *testing.T) {
		t.Parallel()

		var dst bytes.Buffer
		n, err := CopyWithContext(context.Background(), &dst, strings.NewReader("hello world"), make([]byte, 64))
		require.NoError(t, err)
		assert.Equal(t, int64(11), n)
		assert.Equal(t, "hello world", dst.String())
	})

	t.Run("empty source", func(t *testing.T) {
		t.Parallel()

		var dst bytes.Buffer
		n, err := CopyWithContext(context.Background(), &dst, strings.NewReader(""), make([]byte, 64))
		require.NoError(t, err)
		assert.Equal(t, int64(0), n)
		assert.Empty(t, dst.String())
	})

	t.Run("source larger than the buffer", func(t *testing.T) {
		t.Parallel()

		data := strings.Repeat("0123456789", 101) // 1010 bytes, 7 does not divide it
		var dst bytes.Buffer
		n, err := CopyWithContext(context.Background(), &dst, strings.NewReader(data), make([]byte, 7))
		require.NoError(t, err)
		assert.Equal(t, int64(len(data)), n)
		assert.Equal(t, data, dst.String())
	})

	t.Run("reader that returns the last bytes together with EOF", func(t *testing.T) {
		t.Parallel()

		var dst bytes.Buffer
		src := iotest.DataErrReader(strings.NewReader("last bytes"))
		n, err := CopyWithContext(context.Background(), &dst, src, make([]byte, 64))
		require.NoError(t, err)
		assert.Equal(t, int64(10), n)
		assert.Equal(t, "last bytes", dst.String())
	})

	t.Run("reader that returns one byte at a time", func(t *testing.T) {
		t.Parallel()

		var dst bytes.Buffer
		src := iotest.OneByteReader(strings.NewReader("slow"))
		n, err := CopyWithContext(context.Background(), &dst, src, make([]byte, 64))
		require.NoError(t, err)
		assert.Equal(t, int64(4), n)
		assert.Equal(t, "slow", dst.String())
	})

	t.Run("context canceled before the copy starts", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		var dst bytes.Buffer
		n, err := CopyWithContext(ctx, &dst, strings.NewReader("data"), make([]byte, 64))
		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, int64(0), n)
		assert.Empty(t, dst.String())
	})

	t.Run("context canceled part way through", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// the first chunk is copied, then the next loop iteration notices
		src := &cancelingReader{r: strings.NewReader(strings.Repeat("x", 100)), cancel: cancel}

		var dst bytes.Buffer
		n, err := CopyWithContext(ctx, &dst, src, make([]byte, 10))
		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, int64(10), n)
		assert.Equal(t, strings.Repeat("x", 10), dst.String())
	})

	t.Run("read error is returned along with what was copied", func(t *testing.T) {
		t.Parallel()

		boom := errors.New("boom")
		src := io.MultiReader(strings.NewReader("abc"), iotest.ErrReader(boom))

		var dst bytes.Buffer
		n, err := CopyWithContext(context.Background(), &dst, src, make([]byte, 64))
		require.ErrorIs(t, err, boom)
		assert.Equal(t, int64(3), n)
		assert.Equal(t, "abc", dst.String())
	})

	t.Run("write error is returned", func(t *testing.T) {
		t.Parallel()

		boom := errors.New("disk full")
		dst := &failingWriter{limit: 4, err: boom}

		n, err := CopyWithContext(context.Background(), dst, strings.NewReader("0123456789"), make([]byte, 64))
		require.ErrorIs(t, err, boom)
		assert.Equal(t, int64(4), n)
		assert.Equal(t, "0123", dst.buf.String())
	})

	t.Run("short write without an error", func(t *testing.T) {
		t.Parallel()

		n, err := CopyWithContext(context.Background(), shortWriter{}, strings.NewReader("data"), make([]byte, 64))
		require.ErrorIs(t, err, io.ErrShortWrite)
		assert.Equal(t, int64(3), n)
	})
}

// dirNames returns the names of the entries in dir, sorted.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func TestCopyFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("copies the content", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			data []byte
		}{
			{"empty file", []byte{}},
			{"short content", []byte("hello")},
			{"exactly the smallest buffer", patterned(minBufferSize)},
			{"larger than the largest buffer", patterned(3*maxBufferSize + 17)},
		}

		for _, tc := range tests {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				dir := t.TempDir()
				src := filepath.Join(dir, "src")
				dst := filepath.Join(dir, "dst")
				require.NoError(t, os.WriteFile(src, tc.data, 0o644))

				require.NoError(t, CopyFile(ctx, src, dst))

				got, err := os.ReadFile(dst)
				require.NoError(t, err)
				assert.Equal(t, tc.data, got)
				assert.Equal(t, tc.data, mustRead(t, src), "the source is untouched")
			})
		}
	})

	t.Run("new file has mode 0644 whatever the mode of the source", func(t *testing.T) {
		t.Parallel()

		for _, mode := range []os.FileMode{0o600, 0o755, 0o444} {
			dir := t.TempDir()
			src := filepath.Join(dir, "src")
			dst := filepath.Join(dir, "dst")
			require.NoError(t, os.WriteFile(src, []byte("x"), mode))
			require.NoError(t, os.Chmod(src, mode)) // WriteFile is subject to the umask

			require.NoError(t, CopyFile(ctx, src, dst))

			info, err := os.Stat(dst)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "source mode %v", mode)
		}
	})

	t.Run("replaces an existing file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		dst := filepath.Join(dir, "dst")
		require.NoError(t, os.WriteFile(src, []byte("new"), 0o644))
		require.NoError(t, os.WriteFile(dst, []byte("a much longer old content"), 0o600))

		require.NoError(t, CopyFile(ctx, src, dst))

		assert.Equal(t, []byte("new"), mustRead(t, dst))
		info, err := os.Stat(dst)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
	})

	t.Run("leaves no temporary files behind", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		require.NoError(t, os.WriteFile(src, []byte("x"), 0o644))

		require.NoError(t, CopyFile(ctx, src, filepath.Join(dir, "dst")))

		assert.Equal(t, []string{"dst", "src"}, dirNames(t, dir))
	})

	t.Run("source and destination can be the same file", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(path, []byte("keep me"), 0o644))

		require.NoError(t, CopyFile(ctx, path, path))

		assert.Equal(t, []byte("keep me"), mustRead(t, path))
	})

	t.Run("a destination that is a symlink is replaced and not followed", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		target := filepath.Join(dir, "target")
		link := filepath.Join(dir, "link")
		require.NoError(t, os.WriteFile(src, []byte("new"), 0o644))
		require.NoError(t, os.WriteFile(target, []byte("original"), 0o644))
		require.NoError(t, os.Symlink(target, link))

		require.NoError(t, CopyFile(ctx, src, link))

		info, err := os.Lstat(link)
		require.NoError(t, err)
		assert.True(t, info.Mode().IsRegular(), "the link was replaced by a regular file")
		assert.Equal(t, []byte("new"), mustRead(t, link))
		assert.Equal(t, []byte("original"), mustRead(t, target))
	})

	t.Run("source that does not exist", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		dst := filepath.Join(dir, "dst")

		err := CopyFile(ctx, filepath.Join(dir, "missing"), dst)
		require.Error(t, err)
		assert.ErrorIs(t, err, os.ErrNotExist)
		assert.Contains(t, err.Error(), "open src")
		assert.Empty(t, dirNames(t, dir))
	})

	t.Run("source that can't be read leaves the destination alone", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		dst := filepath.Join(dir, "dst")
		require.NoError(t, os.WriteFile(dst, []byte("old"), 0o644))

		// a directory can be opened but not read, whoever is running the test
		src := filepath.Join(dir, "srcdir")
		require.NoError(t, os.Mkdir(src, 0o755))

		err := CopyFile(ctx, src, dst)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "copy")
		assert.Equal(t, []byte("old"), mustRead(t, dst))
		assert.Equal(t, []string{"dst", "srcdir"}, dirNames(t, dir))
	})

	t.Run("canceled context leaves the destination alone", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		existing := filepath.Join(dir, "existing")
		fresh := filepath.Join(dir, "fresh")
		require.NoError(t, os.WriteFile(src, []byte("new"), 0o644))
		require.NoError(t, os.WriteFile(existing, []byte("old"), 0o644))

		canceled, cancel := context.WithCancel(ctx)
		cancel()

		for _, dst := range []string{existing, fresh} {
			err := CopyFile(canceled, src, dst)
			require.Error(t, err)
			assert.ErrorIs(t, err, context.Canceled)
		}

		assert.Equal(t, []byte("old"), mustRead(t, existing))
		assert.NoFileExists(t, fresh)
		assert.Equal(t, []string{"existing", "src"}, dirNames(t, dir))
	})

	t.Run("destination directory that does not exist", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		require.NoError(t, os.WriteFile(src, []byte("x"), 0o644))

		err := CopyFile(ctx, src, filepath.Join(dir, "missing", "dst"))
		require.Error(t, err)
		assert.ErrorIs(t, err, os.ErrNotExist)
		assert.Contains(t, err.Error(), "create temp")
	})

	t.Run("destination that is a directory", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		dst := filepath.Join(dir, "dstdir")
		require.NoError(t, os.WriteFile(src, []byte("x"), 0o644))
		require.NoError(t, os.Mkdir(dst, 0o755))

		err := CopyFile(ctx, src, dst)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rename into place")
		assert.DirExists(t, dst)
		assert.Equal(t, []string{"dstdir", "src"}, dirNames(t, dir))
	})
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}
