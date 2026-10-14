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

package nexusclient

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeNexus is what a client talks to instead of Nexus Mods: it answers what
// the test says, and keeps what it was asked. A request that the test did not
// say what to answer to is an error of the request, so nothing that a test does
// can reach the real API.
type fakeNexus struct {
	mu       sync.Mutex
	requests []*http.Request
	respond  func(r *http.Request) (*http.Response, error)
}

func (f *fakeNexus) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.requests = append(f.requests, r)
	f.mu.Unlock()

	if f.respond == nil {
		return nil, errors.New("no answer for the request: tests do not talk to Nexus Mods")
	}
	return f.respond(r)
}

// asked is how many requests the client made.
func (f *fakeNexus) asked() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// last is the last request that the client made.
func (f *fakeNexus) last() *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

// answer is a response of the API with the status and the body, and the headers
// that the API sets (the rate limits, usually).
func answer(status int, body string, headers map[string]string) *http.Response {
	h := http.Header{}
	for k, v := range headers {
		h.Set(k, v)
	}
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// newTestClient is a client with a cache directory and a state directory of the
// test, that talks to the fake API that it returns, which answers what respond
// says (nothing, if it is nil: any request is an error).
func newTestClient(t *testing.T, respond func(r *http.Request) (*http.Response, error)) (*Client, *fakeNexus) {
	t.Helper()

	c, err := New(context.Background(), "the-key", quietLogger(), "1.2.3", t.TempDir(), t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })

	api := &fakeNexus{respond: respond}
	c.httpClient = &http.Client{Transport: api}
	return c, api
}
