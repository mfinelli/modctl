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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// ModInfo represents the fields we care about from
// /v1/games/{domain}/mods/{id}.json
type ModInfo struct {
	ModID       int    `json:"mod_id"`
	Name        string `json:"name"`
	Summary     string `json:"summary"`
	Author      string `json:"author"`
	DomainName  string `json:"domain_name"`
	Version     string `json:"version"`
	IsAvailable bool   `json:"available"`
	RawJSON     []byte `json:"-"`
}

// ModFilesResponse represents the fields we care about from
// /v1/games/{domain}/mods/{id}/files.json
type ModFilesResponse struct {
	Files       []ModFileInfo    `json:"files"`
	FileUpdates []FileUpdateInfo `json:"file_updates"`
	RawJSON     []byte           `json:"-"`
}

type ModFileInfo struct {
	FileID            int    `json:"file_id"`
	Name              string `json:"name"`
	Version           string `json:"version"`
	CategoryName      string `json:"category_name"`
	IsPrimary         bool   `json:"is_primary"`
	FileName          string `json:"file_name"`
	SizeInBytes       int64  `json:"size_in_bytes"`
	UploadedTimestamp int64  `json:"uploaded_timestamp"`
}

type FileUpdateInfo struct {
	OldFileID         int    `json:"old_file_id"`
	NewFileID         int    `json:"new_file_id"`
	OldFileName       string `json:"old_file_name"`
	NewFileName       string `json:"new_file_name"`
	UploadedTimestamp int64  `json:"uploaded_timestamp"`
}

// doRequest executes an authenticated request against the Nexus API, updates
// rate limit state from response headers, and decodes the JSON body into dst.
func (c *Client) doRequest(method, path string, dst any, rawBody *[]byte) error {
	if err := c.limiter.Wait(c.ctx); err != nil {
		return fmt.Errorf("rate limiter: %w", err)
	}

	req, err := http.NewRequestWithContext(c.ctx, method, baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}

	req.Header.Set("apikey", c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	// Always try to update rate limit state if the headers are present,
	// regardless of response status code
	if err := c.updateRateLimitState(resp); err != nil {
		// Non-fatal
		c.logger.Warn("failed to save rate limit state", "error", err)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		state, err := LoadRateLimitState()
		if err != nil {
			c.logger.Warn("failed to load rate limit state for 429 error", "error", err)
		}
		hourly, daily := state.EffectiveRemaining()
		return fmt.Errorf("nexus api rate limit exceeded (hourly remaining: %d, daily remaining: %d)", hourly, daily)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("nexus api error: status %d", resp.StatusCode)
	}

	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	if rawBody != nil {
		*rawBody = body
	}

	return nil
}

func (c *Client) updateRateLimitState(resp *http.Response) error {
	hourlyLimit, err := strconv.Atoi(resp.Header.Get("X-RL-Hourly-Limit"))
	if err != nil {
		return nil // headers not present, nothing to update
	}
	hourlyRemaining, err := strconv.Atoi(resp.Header.Get("X-RL-Hourly-Remaining"))
	if err != nil {
		return nil
	}
	dailyLimit, err := strconv.Atoi(resp.Header.Get("X-RL-Daily-Limit"))
	if err != nil {
		return nil
	}
	dailyRemaining, err := strconv.Atoi(resp.Header.Get("X-RL-Daily-Remaining"))
	if err != nil {
		return nil
	}
	hourlyReset, err := time.Parse(time.RFC3339, resp.Header.Get("X-RL-Hourly-Reset"))
	if err != nil {
		return nil
	}
	dailyReset, err := time.Parse(time.RFC3339, resp.Header.Get("X-RL-Daily-Reset"))
	if err != nil {
		return nil
	}

	return SaveRateLimitState(&RateLimitState{
		HourlyLimit:     hourlyLimit,
		HourlyRemaining: hourlyRemaining,
		HourlyReset:     hourlyReset,
		DailyLimit:      dailyLimit,
		DailyRemaining:  dailyRemaining,
		DailyReset:      dailyReset,
	})
}

func (c *Client) GetMod(gameDomain string, modID int) (*ModInfo, error) {
	path := fmt.Sprintf("/v1/games/%s/mods/%d.json", gameDomain, modID)
	var result ModInfo
	var raw []byte
	if err := c.doRequest(http.MethodGet, path, &result, &raw); err != nil {
		return nil, fmt.Errorf("fetching mod %d (%s): %w", modID, gameDomain, err)
	}
	result.RawJSON = raw
	if err := c.cacheModInfo(&result); err != nil {
		c.logger.Warn("failed to cache nexus mod info", "error", err)
	}
	return &result, nil
}

func (c *Client) GetModFiles(gameDomain string, modID int) (*ModFilesResponse, error) {
	path := fmt.Sprintf("/v1/games/%s/mods/%d/files.json", gameDomain, modID)
	var result ModFilesResponse
	var raw []byte
	if err := c.doRequest(http.MethodGet, path, &result, &raw); err != nil {
		return nil, fmt.Errorf("fetching files for mod %d (%s): %w", modID, gameDomain, err)
	}
	if err := c.cacheModFiles(gameDomain, modID, &result); err != nil {
		c.logger.Warn("failed to cache nexus mod files", "error", err)
	}
	result.RawJSON = raw
	return &result, nil
}
