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
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
)

const (
	ssoWSURL          = "wss://sso.nexusmods.com"
	ssoAuthURL        = "https://www.nexusmods.com/sso"
	AppSlug           = "modctl-modctl"
	DefaultSSOTimeout = 5 * time.Minute
)

type ssoEnvelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *string         `json:"error"`
}

type connectionTokenPayload struct {
	ConnectionToken string `json:"connection_token"`
}

type apiKeyPayload struct {
	APIKey string `json:"api_key"`
}

// Login runs the Nexus Mods SSO flow and returns the API key.
// Progress messages are written to w. The caller should wrap ctx with a
// timeout before calling (DefaultSSOTimeout is the recommended value)
func Login(ctx context.Context, w io.Writer) (string, error) {
	return login(ctx, w, ssoServer{
		wsURL:       ssoWSURL,
		authURL:     ssoAuthURL,
		openBrowser: openBrowser,
	})
}

// ssoServer is what the SSO flow talks to, and how it sends the user there. It
// is Nexus Mods and the browser of the user, which a test can't talk to (and
// must not open), so a test has one of its own.
type ssoServer struct {
	// wsURL is where the flow is run, which is a websocket.
	wsURL string

	// authURL is the page where the user authorizes modctl.
	authURL string

	// openBrowser opens url for the user, if it can.
	openBrowser func(url string)
}

// login is Login for the server s.
func login(ctx context.Context, w io.Writer, s ssoServer) (string, error) {
	conn, _, err := websocket.Dial(ctx, s.wsURL, nil)
	if err != nil {
		return "", fmt.Errorf("connecting to Nexus SSO: %w", err)
	}
	defer conn.CloseNow()

	id := uuid.NewString()

	if err := wsjson.Write(ctx, conn, map[string]any{
		"id":       id,
		"token":    nil,
		"protocol": 2,
	}); err != nil {
		return "", fmt.Errorf("sending SSO handshake: %w", err)
	}

	for {
		var envelope ssoEnvelope
		if err := wsjson.Read(ctx, conn, &envelope); err != nil {
			if ctx.Err() != nil {
				return "", fmt.Errorf("SSO login cancelled")
			}
			return "", fmt.Errorf("reading SSO message: %w", err)
		}

		if !envelope.Success {
			msg := "unknown error"
			if envelope.Error != nil {
				msg = *envelope.Error
			}
			return "", fmt.Errorf("SSO error: %s", msg)
		}

		var tok connectionTokenPayload
		if err := json.Unmarshal(envelope.Data, &tok); err == nil && tok.ConnectionToken != "" {
			authURL := fmt.Sprintf("%s?id=%s&application=%s", s.authURL, id, AppSlug)
			fmt.Fprintf(w, "\nOpen this URL in your browser to authorize modctl with Nexus Mods:\n\n  %s\n\nWaiting for authorization (Ctrl+C to cancel)...\n", authURL)
			s.openBrowser(authURL)
			continue
		}

		var key apiKeyPayload
		if err := json.Unmarshal(envelope.Data, &key); err == nil && key.APIKey != "" {
			conn.Close(websocket.StatusNormalClosure, "")
			return key.APIKey, nil
		}

		fmt.Fprintf(w, "warning: received unexpected SSO message, continuing to wait...\n")
	}
}

// openBrowser attempts to open url in the system browser. Errors are
// intentionally ignored since the URL has already been printed to the user.
// (Note: modctl is really Linux-only but it costs us basically nothing
// and there is no ongoing maintenance cost or burden to support and handle
// these other OS options...)
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	_ = cmd.Start()
}
