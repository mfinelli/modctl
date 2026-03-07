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
	"fmt"
	"strconv"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var (
	profilesOrderMoveGame    string
	profilesOrderMoveProfile string

	profilesOrderMoveAfter  int64
	profilesOrderMoveBefore int64
)

var profilesOrderMoveCmd = &cobra.Command{
	Use:   "move",
	Short: "Move a mod version within the profile order",
	Long: `Move a mod file version before or after another version within a profile.

This changes ordering and rewrites priorities to a compact sequence starting
at 1. Use this when you care about relative order, not specific priority numbers.

Exactly one of --before or --after is required.`,
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		moveID, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil || moveID <= 0 {
			return fmt.Errorf("invalid mod_file_version_id %q (expected a positive integer)", args[0])
		}

		anchorID := profilesOrderMoveBefore
		placeAfter := false
		if profilesOrderMoveAfter != 0 {
			anchorID = profilesOrderMoveAfter
			placeAfter = true
		}

		if moveID == anchorID {
			return fmt.Errorf("cannot move a version relative to itself")
		}

		if profilesOrderMoveAfter != 0 && profilesOrderMoveBefore != 0 {
			return fmt.Errorf("exactly one of --before or --after is required")
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

		// Resolve game install id: --game overrides active selection
		if profilesOrderMoveGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			profilesOrderMoveGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := internal.ResolveGameInstallArg(ctx, q, profilesOrderMoveGame)
		if err != nil {
			return err
		}

		p, err := internal.ResolveProfileArg(ctx, q, &gi, profilesOrderMoveProfile)
		if err != nil {
			return err
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("error starting transaction: %w", err)
		}
		defer tx.Rollback()
		qtx := q.WithTx(tx)

		items, err := qtx.ListProfileItemsForOrder(ctx, p.ID)
		if err != nil {
			return fmt.Errorf("list profile items: %w", err)
		}
		if len(items) == 0 {
			return fmt.Errorf("profile %q has no items", p.Name)
		}

		// Find indices of moveID and anchorID, and build a new ordering
		moveIdx := -1
		anchorIdx := -1
		for i, it := range items {
			if it.ModFileVersionID == moveID {
				moveIdx = i
			}
			if it.ModFileVersionID == anchorID {
				anchorIdx = i
			}
		}
		if moveIdx == -1 {
			return fmt.Errorf("version %d is not in profile %q", moveID, p.Name)
		}
		if anchorIdx == -1 {
			return fmt.Errorf("anchor version %d is not in profile %q", anchorID, p.Name)
		}

		// Compute desired insertion index after removing move item -- we
		// remove first, then compute insertion position in the shortened slice
		moveItem := items[moveIdx]
		short := make([]dbq.ListProfileItemsForOrderRow, 0, len(items)-1)
		short = append(short, items[:moveIdx]...)
		short = append(short, items[moveIdx+1:]...)

		// Find anchor index in the shortened slice
		newAnchorIdx := -1
		for i, it := range short {
			if it.ModFileVersionID == anchorID {
				newAnchorIdx = i
				break
			}
		}
		if newAnchorIdx == -1 {
			// This should never happen
			return fmt.Errorf("internal error: anchor not found after removal")
		}

		insertAt := newAnchorIdx
		if placeAfter {
			insertAt = newAnchorIdx + 1
		}

		// If moving item is already at the desired spot: no-op
		// We can check by comparing current order to what would happen:
		// compute the would-be list and compare positions
		reordered := make([]dbq.ListProfileItemsForOrderRow, 0, len(items))
		reordered = append(reordered, short[:insertAt]...)
		reordered = append(reordered, moveItem)
		reordered = append(reordered, short[insertAt:]...)

		// Detect no-op: same sequence of mod_file_version_id
		noop := true
		for i := range items {
			if items[i].ModFileVersionID != reordered[i].ModFileVersionID {
				noop = false
				break
			}
		}
		if noop {
			fmt.Printf("No change: version %d is already in the requested position in profile %q\n", moveID, p.Name)
			return nil
		}

		// Rewrite priorities to 1..N (step=1) in the new order
		if err := internal.RewriteProfilePriorities(ctx, qtx, p.ID, reordered, 1, 1); err != nil {
			return err
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}

		if placeAfter {
			fmt.Printf("Moved version %d after %d in profile %q\n", moveID, anchorID, p.Name)
		} else {
			fmt.Printf("Moved version %d before %d in profile %q\n", moveID, anchorID, p.Name)
		}

		return nil
	},
}

func init() {
	profilesOrderCmd.AddCommand(profilesOrderMoveCmd)

	profilesOrderMoveCmd.Flags().StringVarP(&profilesOrderMoveGame, "game", "g", "",
		"Override the currently active game")
	profilesOrderMoveCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})

	profilesOrderMoveCmd.Flags().StringVar(&profilesOrderMoveProfile, "profile", "p",
		"Override the currently active profile")
	profilesOrderMoveCmd.RegisterFlagCompletionFunc("profile",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.ProfileNames(cmd, toComplete)
		})

	profilesOrderMoveCmd.Flags().Int64VarP(&profilesOrderMoveAfter, "after", "a", 0,
		"Move the mod after the specified mod")
	profilesOrderMoveCmd.Flags().Int64VarP(&profilesOrderMoveBefore, "before", "b", 0,
		"Move the mod before the specified mod")
}
