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
	"database/sql"
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"runtime"
	"time"

	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/nexusclient/dbc"
	"github.com/spf13/viper"
	"golang.org/x/time/rate"
)

const (
	baseURL            = "https://api.nexusmods.com"
	cacheSchemaVersion = "1"
)

//go:embed schema.sql
var cacheSchema string

type Client struct {
	CacheReader
	apiKey     string
	userAgent  string
	version    string
	httpClient *http.Client
	limiter    *rate.Limiter
}

func New(ctx context.Context, apiKey string, logger *slog.Logger, version string) (*Client, error) {
	ua := buildUserAgent(version)

	reader, err := NewCacheReader(ctx, logger)
	if err != nil {
		return nil, fmt.Errorf("opening nexus cache db: %w", err)
	}

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	// 30 req/sec with a burst of 10
	limiter := rate.NewLimiter(rate.Limit(30), 10)

	return &Client{
		CacheReader: *reader,
		apiKey:      apiKey,
		userAgent:   ua,
		version:     version,
		httpClient:  httpClient,
		limiter:     limiter,
	}, nil
}

func (c *Client) RateLimitState() (*RateLimitState, error) {
	return LoadRateLimitState()
}

func buildUserAgent(version string) string {
	return fmt.Sprintf("modctl/%s (%s; %s) https://github.com/mfinelli/modctl",
		version,
		runtime.GOOS,
		runtime.GOARCH,
	)
}

func openCacheDB(ctx context.Context) (*sql.DB, error) {
	path := filepath.Join(viper.GetString("cache_dir"), "nexus_cache.db")

	db, err := sql.Open("sqlite3", fmt.Sprintf("%s%s", path, internal.DB_PRAGMAS))
	if err != nil {
		return nil, fmt.Errorf("opening cache db: %w", err)
	}

	if err := initCacheDB(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("initializing cache db: %w", err)
	}

	return db, nil
}

// InitCacheDB initializes or resets the nexus cache database schema.
// It is exported for use by the exporter when constructing scoped cache
// databases for export bundles.
func InitCacheDB(ctx context.Context, db *sql.DB) error {
	return initCacheDB(ctx, db)
}

func initCacheDB(ctx context.Context, db *sql.DB) error {
	if err := applyCacheSchema(ctx, db); err != nil {
		return err
	}

	q := dbc.New(db)
	version, err := q.ReadSchemaVersion(ctx)
	if err != nil {
		return fmt.Errorf("reading cache schema version: %w", err)
	}

	if version != cacheSchemaVersion {
		if err := resetCacheDB(ctx, db); err != nil {
			return fmt.Errorf("resetting stale cache db: %w", err)
		}
	}

	return nil
}

func applyCacheSchema(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, cacheSchema); err != nil {
		return fmt.Errorf("applying cache schema: %w", err)
	}
	return nil
}

func resetCacheDB(ctx context.Context, db *sql.DB) error {
	drops := []string{
		`DROP TABLE IF EXISTS nexus_file_updates`,
		`DROP TABLE IF EXISTS nexus_file_info`,
		`DROP TABLE IF EXISTS nexus_mod_info`,
		`DROP TABLE IF EXISTS cache_meta`,
	}
	for _, stmt := range drops {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("dropping table: %w", err)
		}
	}
	return applyCacheSchema(ctx, db)
}
