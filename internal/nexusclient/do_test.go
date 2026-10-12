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
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rateLimitHeaders are the headers of a response of the API that say where the
// limits are: 90 of 100 are left for the hour, and 2400 of 2500 for the day.
func rateLimitHeaders() map[string]string {
	return map[string]string{
		"X-RL-Hourly-Limit":     "100",
		"X-RL-Hourly-Remaining": "90",
		"X-RL-Hourly-Reset":     "2099-01-01 15:00:00 +0000",
		"X-RL-Daily-Limit":      "2500",
		"X-RL-Daily-Remaining":  "2400",
		"X-RL-Daily-Reset":      "2099-01-02 00:00:00 +0000",
	}
}

func TestDoRequest(t *testing.T) {
	t.Parallel()

	t.Run("the request says who is asking, and what for", func(t *testing.T) {
		t.Parallel()

		c, api := newTestClient(t, respondWith(`{}`))

		var dst struct{}
		require.NoError(t, c.doRequest(http.MethodGet, "/v1/games/skyrim/mods/1.json", &dst, nil))

		req := api.last()
		assert.Equal(t, http.MethodGet, req.Method)
		assert.Equal(t, "https://api.nexusmods.com/v1/games/skyrim/mods/1.json", req.URL.String())
		assert.Equal(t, "the-key", req.Header.Get("apikey"))
		assert.Equal(t, "application/json", req.Header.Get("Accept"))
		assert.Equal(t, buildUserAgent("1.2.3"), req.Header.Get("User-Agent"))
		assert.Equal(t, "modctl", req.Header.Get("Application-Name"))
		assert.Equal(t, "1.2.3", req.Header.Get("Application-Version"))
	})

	t.Run("the answer is decoded, and what it was is kept", func(t *testing.T) {
		t.Parallel()

		c, _ := newTestClient(t, respondWith(modBody))

		var dst ModInfo
		var raw []byte
		require.NoError(t, c.doRequest(http.MethodGet, "/x", &dst, &raw))

		assert.Equal(t, "SkyUI", dst.Name)
		assert.Equal(t, modBody, string(raw))
	})

	t.Run("what the API says about the limits is kept, whatever the answer is", func(t *testing.T) {
		t.Parallel()

		for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusInternalServerError} {
			status := status
			t.Run(http.StatusText(status), func(t *testing.T) {
				t.Parallel()

				c, _ := newTestClient(t, func(*http.Request) (*http.Response, error) {
					return answer(status, `{}`, rateLimitHeaders()), nil
				})

				_ = c.doRequest(http.MethodGet, "/x", &struct{}{}, nil)

				got, err := c.RateLimitState()
				require.NoError(t, err)
				assert.Equal(t, 90, got.HourlyRemaining)
				assert.Equal(t, 2400, got.DailyRemaining)
			})
		}
	})

	t.Run("a limit that can't be kept is not an error of the request", func(t *testing.T) {
		t.Parallel()

		c, _ := newTestClient(t, func(*http.Request) (*http.Response, error) {
			return answer(http.StatusOK, `{}`, rateLimitHeaders()), nil
		})
		// there is a directory where the file should be
		require.NoError(t, os.Mkdir(filepath.Join(c.stateDir, "nexus.json"), 0o755))

		err := c.doRequest(http.MethodGet, "/x", &struct{}{}, nil)

		assert.NoError(t, err)
	})

	t.Run("a request that is too many says how many are left", func(t *testing.T) {
		t.Parallel()

		headers := rateLimitHeaders()
		headers["X-RL-Hourly-Remaining"] = "0"
		headers["X-RL-Daily-Remaining"] = "12"
		c, _ := newTestClient(t, func(*http.Request) (*http.Response, error) {
			return answer(http.StatusTooManyRequests, ``, headers), nil
		})

		err := c.doRequest(http.MethodGet, "/x", &struct{}{}, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "rate limit exceeded")
		assert.Contains(t, err.Error(), "hourly remaining: 0")
		assert.Contains(t, err.Error(), "daily remaining: 12")
	})

	t.Run("a limit that is over is all of it again", func(t *testing.T) {
		t.Parallel()

		headers := rateLimitHeaders()
		headers["X-RL-Hourly-Remaining"] = "0"
		headers["X-RL-Hourly-Reset"] = "2001-01-01 00:00:00 +0000"
		c, _ := newTestClient(t, func(*http.Request) (*http.Response, error) {
			return answer(http.StatusTooManyRequests, ``, headers), nil
		})

		err := c.doRequest(http.MethodGet, "/x", &struct{}{}, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "hourly remaining: 100")
	})

	t.Run("a status that is not a success is an error that says which", func(t *testing.T) {
		t.Parallel()

		c, _ := newTestClient(t, func(*http.Request) (*http.Response, error) {
			return answer(http.StatusForbidden, `{}`, nil), nil
		})

		err := c.doRequest(http.MethodGet, "/x", &struct{}{}, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 403")
	})

	t.Run("an answer that is not what was asked for is an error", func(t *testing.T) {
		t.Parallel()

		c, _ := newTestClient(t, respondWith(`<html>not json</html>`))

		err := c.doRequest(http.MethodGet, "/x", &struct{}{}, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "decoding response")
	})

	t.Run("a request that can't be made is an error that says so", func(t *testing.T) {
		t.Parallel()

		boom := errors.New("connection refused")
		c, _ := newTestClient(t, func(*http.Request) (*http.Response, error) { return nil, boom })

		err := c.doRequest(http.MethodGet, "/x", &struct{}{}, nil)

		require.ErrorIs(t, err, boom)
		assert.Contains(t, err.Error(), "executing request")
	})
}

func TestUpdateRateLimitState(t *testing.T) {
	t.Parallel()

	t.Run("the headers are the limits that are kept", func(t *testing.T) {
		t.Parallel()

		c, _ := newTestClient(t, nil)

		require.NoError(t, c.updateRateLimitState(answer(http.StatusOK, ``, rateLimitHeaders())))

		got, err := c.RateLimitState()
		require.NoError(t, err)
		assert.Equal(t, 100, got.HourlyLimit)
		assert.Equal(t, 90, got.HourlyRemaining)
		assert.Equal(t, 2500, got.DailyLimit)
		assert.Equal(t, 2400, got.DailyRemaining)
		assert.True(t, time.Date(2099, 1, 1, 15, 0, 0, 0, time.UTC).Equal(got.HourlyReset))
		assert.True(t, time.Date(2099, 1, 2, 0, 0, 0, 0, time.UTC).Equal(got.DailyReset))
	})

	t.Run("a reset has the time zone that it says", func(t *testing.T) {
		t.Parallel()

		c, _ := newTestClient(t, nil)
		headers := rateLimitHeaders()
		headers["X-RL-Hourly-Reset"] = "2099-01-01 15:00:00 +0200"

		require.NoError(t, c.updateRateLimitState(answer(http.StatusOK, ``, headers)))

		got, err := c.RateLimitState()
		require.NoError(t, err)
		assert.True(t, time.Date(2099, 1, 1, 13, 0, 0, 0, time.UTC).Equal(got.HourlyReset))
	})

	t.Run("a header that is not there, or is not what it should be, keeps nothing", func(t *testing.T) {
		t.Parallel()

		for _, header := range []string{
			"X-RL-Hourly-Limit", "X-RL-Hourly-Remaining", "X-RL-Hourly-Reset",
			"X-RL-Daily-Limit", "X-RL-Daily-Remaining", "X-RL-Daily-Reset",
		} {
			header := header
			for name, value := range map[string]string{"missing": "", "invalid": "soon"} {
				value := value
				t.Run(header+" "+name, func(t *testing.T) {
					t.Parallel()

					c, _ := newTestClient(t, nil)
					headers := rateLimitHeaders()
					if value == "" {
						delete(headers, header)
					} else {
						headers[header] = value
					}

					err := c.updateRateLimitState(answer(http.StatusOK, ``, headers))

					assert.NoError(t, err, "it is not an error that the API did not say")
					assert.NoFileExists(t, filepath.Join(c.stateDir, "nexus.json"))
				})
			}
		}
	})
}

func TestGetMod(t *testing.T) {
	t.Parallel()

	t.Run("it is the mod that the API says, with what it said", func(t *testing.T) {
		t.Parallel()

		c, api := newTestClient(t, respondWith(modBody))

		got, err := c.GetMod("skyrim", 1)

		require.NoError(t, err)
		assert.Equal(t, "/v1/games/skyrim/mods/1.json", api.last().URL.Path)
		assert.Equal(t, "SkyUI", got.Name)
		assert.Equal(t, "5.2", got.Version)
		assert.Equal(t, modBody, string(got.RawJSON))
	})

	t.Run("an error says which mod it was", func(t *testing.T) {
		t.Parallel()

		c, _ := newTestClient(t, nil)

		_, err := c.GetMod("skyrim", 42)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "fetching mod 42 (skyrim)")
	})
}

func TestGetModFiles(t *testing.T) {
	t.Parallel()

	t.Run("they are the files that the API says, with what it said", func(t *testing.T) {
		t.Parallel()

		c, api := newTestClient(t, respondWith(filesBody))

		got, err := c.GetModFiles("skyrim", 1)

		require.NoError(t, err)
		assert.Equal(t, "/v1/games/skyrim/mods/1/files.json", api.last().URL.Path)
		assert.Len(t, got.Files, 2)
		assert.Len(t, got.FileUpdates, 1)
		assert.Equal(t, filesBody, string(got.RawJSON))
	})

	t.Run("an error says which mod it was", func(t *testing.T) {
		t.Parallel()

		c, _ := newTestClient(t, nil)

		_, err := c.GetModFiles("skyrim", 42)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "fetching files for mod 42 (skyrim)")
	})
}
