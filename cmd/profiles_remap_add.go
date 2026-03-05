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

package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"strconv"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var (
	profilesRemapAddGame     string
	profilesRemapAddProfile  string
	profilesRemapAddPosition int64
)

var profilesRemapAddCmd = &cobra.Command{
	Use:   "add", // <mod_file_version_id> <rule_type> <value>
	Short: "Add a remap rule for a mod version in a profile",
	Long: `Add a remap rule for a mod version in a profile.

Rule types and their expected values:

  strip_components <N>   Remove the first N leading path segments.
  select_subdir <path>   Only install entries under the given subpath.
                         The subpath prefix is stripped from the result.
  dest_prefix <path>     Install all entries under the given subfolder.
  include_glob <pattern> Only install entries matching the glob pattern.
  exclude_glob <pattern> Skip entries matching the glob pattern.

Rules are applied in position order. By default a new rule is appended
after all existing rules. Use --position to insert at a specific position.

Examples:
  modctl profiles remap add 42 strip_components 1
  modctl profiles remap add 42 select_subdir Data
  modctl profiles remap add 42 dest_prefix Data/mymod
  modctl profiles remap add 42 include_glob "*.esp"
  modctl profiles remap add 42 exclude_glob "*.txt"`,
	Args: cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()

		versionID, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil || versionID <= 0 {
			return fmt.Errorf("invalid mod_file_version_id %q (expected a positive integer)", args[0])
		}

		ruleType := args[1]
		rawValue := args[2]

		intVal, textVal, err := parseRemapRuleValue(ruleType, rawValue)
		if err != nil {
			return err
		}

		err = internal.EnsureDBExists()
		if err != nil {
			return err
		}
		db, err := internal.SetupDB()
		if err != nil {
			return fmt.Errorf("error setting up database: %w", err)
		}
		defer db.Close()
		err = internal.MigrateDB(ctx, db)
		if err != nil {
			return fmt.Errorf("error migrating database: %w", err)
		}

		q := dbq.New(db)

		if profilesRemapAddGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesRemapAddGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := internal.ResolveGameInstallArg(ctx, q, profilesRemapAddGame)
		if err != nil {
			return err
		}

		p, err := internal.ResolveProfileArg(ctx, q, &gi, profilesRemapAddProfile)
		if err != nil {
			return err
		}

		itemID, err := internal.ResolveProfileItemByVersion(ctx, &p, q, versionID)
		if err != nil {
			return err
		}

		return internal.AddRemapRule(ctx, db, q, itemID, ruleType, intVal, textVal, profilesRemapAddPosition)
	},
}

func init() {
	profilesRemapCmd.AddCommand(profilesRemapAddCmd)

	profilesRemapAddCmd.PersistentFlags().StringVarP(&profilesRemapAddGame, "game", "g", "",
		"Override the currently active game")
	profilesRemapAddCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
	profilesRemapAddCmd.PersistentFlags().StringVarP(&profilesRemapAddProfile, "profile", "p", "",
		"Override the currently active profile")
	profilesRemapAddCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})

	profilesRemapAddCmd.Flags().Int64Var(&profilesRemapAddPosition, "position", -1,
		"Insert rule at this position instead of appending")
}

// parseRemapRuleValue validates the rule type and parses the value into the
// correct typed fields for the DB.
func parseRemapRuleValue(ruleType, rawValue string) (sql.NullInt64, sql.NullString, error) {
	switch ruleType {
	case "strip_components":
		n, err := strconv.ParseInt(rawValue, 10, 64)
		if err != nil || n < 0 {
			return sql.NullInt64{}, sql.NullString{},
				fmt.Errorf("strip_components requires a non-negative integer, got %q", rawValue)
		}
		return sql.NullInt64{Int64: n, Valid: true}, sql.NullString{}, nil

	case "select_subdir", "dest_prefix", "include_glob", "exclude_glob":
		if rawValue == "" {
			return sql.NullInt64{}, sql.NullString{},
				fmt.Errorf("%s requires a non-empty string value", ruleType)
		}
		return sql.NullInt64{}, sql.NullString{String: rawValue, Valid: true}, nil

	default:
		return sql.NullInt64{}, sql.NullString{},
			fmt.Errorf("unknown rule type %q; valid types are: strip_components, select_subdir, dest_prefix, include_glob, exclude_glob", ruleType)
	}
}
