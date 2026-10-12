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
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testAuthURL = "https://sso.example.test/authorize"

// ssoScript is what the server of a test does, once the client has said hello:
// it is given the connection, and what the client said.
type ssoScript func(ctx context.Context, conn *websocket.Conn, hello map[string]any)

// ssoTestServer is a server of the SSO flow that does what script says, and the
// address that the client talks to it at. A test can't talk to Nexus Mods, so
// this is what login is given.
func ssoTestServer(t *testing.T, script ssoScript) string {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		var hello map[string]any
		if err := wsjson.Read(r.Context(), conn, &hello); err != nil {
			return
		}
		script(r.Context(), conn, hello)
	}))
	t.Cleanup(srv.Close)

	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// browser is what opens the page of the user in a test: it keeps what it was
// asked to open, and has nothing to do with the browser of whoever runs the
// tests.
type browser struct {
	mu     sync.Mutex
	opened []string
	then   func()
}

func (b *browser) open(url string) {
	b.mu.Lock()
	b.opened = append(b.opened, url)
	b.mu.Unlock()
	if b.then != nil {
		b.then()
	}
}

func (b *browser) urls() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.opened...)
}

func sendToken(ctx context.Context, conn *websocket.Conn) error {
	return wsjson.Write(ctx, conn, map[string]any{"success": true, "data": map[string]any{"connection_token": "tok"}})
}

func sendKey(ctx context.Context, conn *websocket.Conn, key string) error {
	return wsjson.Write(ctx, conn, map[string]any{"success": true, "data": map[string]any{"api_key": key}})
}

func TestLogin(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("the user is sent to the page, and what they authorize is the key", func(t *testing.T) {
		t.Parallel()

		heard := make(chan map[string]any, 1)
		wsURL := ssoTestServer(t, func(ctx context.Context, conn *websocket.Conn, h map[string]any) {
			heard <- h
			_ = sendToken(ctx, conn)
			_ = sendKey(ctx, conn, "the-api-key")
		})
		b := &browser{}
		var out bytes.Buffer

		key, err := login(ctx, &out, ssoServer{wsURL: wsURL, authURL: testAuthURL, openBrowser: b.open})

		require.NoError(t, err)
		assert.Equal(t, "the-api-key", key)
		hello := <-heard
		id, _ := hello["id"].(string)
		require.NotEmpty(t, id, "it says who it is")
		assert.Equal(t, float64(2), hello["protocol"])
		assert.Contains(t, hello, "token")
		assert.Nil(t, hello["token"])
		want := testAuthURL + "?id=" + id + "&application=modctl-modctl"
		assert.Equal(t, []string{want}, b.urls(), "the page is opened once, with the id of the flow")
		assert.Contains(t, out.String(), want, "and it is said, for a browser that did not open")
	})

	t.Run("every flow has an id of its own", func(t *testing.T) {
		t.Parallel()

		ids := make([]string, 2)
		for i := range ids {
			heard := make(chan map[string]any, 1)
			wsURL := ssoTestServer(t, func(ctx context.Context, conn *websocket.Conn, h map[string]any) {
				heard <- h
				_ = sendKey(ctx, conn, "k")
			})
			_, err := login(ctx, &bytes.Buffer{}, ssoServer{wsURL: wsURL, authURL: testAuthURL, openBrowser: (&browser{}).open})
			require.NoError(t, err)
			ids[i], _ = (<-heard)["id"].(string)
		}

		assert.NotEmpty(t, ids[0])
		assert.NotEqual(t, ids[0], ids[1])
	})

	t.Run("a key that comes before there is a page to go to is still the key", func(t *testing.T) {
		t.Parallel()

		wsURL := ssoTestServer(t, func(ctx context.Context, conn *websocket.Conn, _ map[string]any) {
			_ = sendKey(ctx, conn, "early")
		})
		b := &browser{}

		key, err := login(ctx, &bytes.Buffer{}, ssoServer{wsURL: wsURL, authURL: testAuthURL, openBrowser: b.open})

		require.NoError(t, err)
		assert.Equal(t, "early", key)
		assert.Empty(t, b.urls())
	})

	t.Run("a message that is not understood is said, and waited past", func(t *testing.T) {
		t.Parallel()

		wsURL := ssoTestServer(t, func(ctx context.Context, conn *websocket.Conn, _ map[string]any) {
			_ = wsjson.Write(ctx, conn, map[string]any{"success": true, "data": map[string]any{"something": "else"}})
			_ = sendKey(ctx, conn, "the-api-key")
		})
		var out bytes.Buffer

		key, err := login(ctx, &out, ssoServer{wsURL: wsURL, authURL: testAuthURL, openBrowser: (&browser{}).open})

		require.NoError(t, err)
		assert.Equal(t, "the-api-key", key)
		assert.Contains(t, out.String(), "unexpected SSO message")
	})

	t.Run("an error of the server is an error that says what it is", func(t *testing.T) {
		t.Parallel()

		wsURL := ssoTestServer(t, func(ctx context.Context, conn *websocket.Conn, _ map[string]any) {
			_ = wsjson.Write(ctx, conn, map[string]any{"success": false, "error": "the application is not known"})
		})
		b := &browser{}

		key, err := login(ctx, &bytes.Buffer{}, ssoServer{wsURL: wsURL, authURL: testAuthURL, openBrowser: b.open})

		require.Error(t, err)
		assert.Empty(t, key)
		assert.Contains(t, err.Error(), "SSO error: the application is not known")
		assert.Empty(t, b.urls())
	})

	t.Run("an error of the server that does not say what it is, is not nothing", func(t *testing.T) {
		t.Parallel()

		wsURL := ssoTestServer(t, func(ctx context.Context, conn *websocket.Conn, _ map[string]any) {
			_ = wsjson.Write(ctx, conn, map[string]any{"success": false})
		})

		_, err := login(ctx, &bytes.Buffer{}, ssoServer{wsURL: wsURL, authURL: testAuthURL, openBrowser: (&browser{}).open})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "SSO error: unknown error")
	})

	t.Run("a server that hangs up before there is a key is an error", func(t *testing.T) {
		t.Parallel()

		wsURL := ssoTestServer(t, func(ctx context.Context, conn *websocket.Conn, _ map[string]any) {
			_ = sendToken(ctx, conn)
			conn.Close(websocket.StatusInternalError, "gone")
		})

		_, err := login(ctx, &bytes.Buffer{}, ssoServer{wsURL: wsURL, authURL: testAuthURL, openBrowser: (&browser{}).open})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "reading SSO message")
	})

	t.Run("a user who gives up is a login that is cancelled", func(t *testing.T) {
		t.Parallel()

		wsURL := ssoTestServer(t, func(ctx context.Context, conn *websocket.Conn, _ map[string]any) {
			_ = sendToken(ctx, conn)
			// no key comes: the server waits for the client to leave
			var ignored any
			_ = wsjson.Read(ctx, conn, &ignored)
		})
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		// the user gives up while they are looking at the page
		b := &browser{then: cancel}

		_, err := login(ctx, &bytes.Buffer{}, ssoServer{wsURL: wsURL, authURL: testAuthURL, openBrowser: b.open})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "SSO login cancelled")
		assert.Len(t, b.urls(), 1)
	})

	t.Run("a server that is not there is an error that says so", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.NotFoundHandler())
		wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
		srv.Close() // nothing is there any more
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		b := &browser{}

		_, err := login(ctx, &bytes.Buffer{}, ssoServer{wsURL: wsURL, authURL: testAuthURL, openBrowser: b.open})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "connecting to Nexus SSO")
		assert.Empty(t, b.urls())
	})
}
