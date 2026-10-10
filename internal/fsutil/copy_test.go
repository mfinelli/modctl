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
