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
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/argresolver"
	"github.com/mfinelli/modctl/internal/completion"
	"github.com/mfinelli/modctl/internal/state"
	"github.com/spf13/cobra"
)

var (
	operationsListGame  string
	operationsListAll   bool
	operationsListLimit int
)

var operationsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent apply and unapply operations",
	Long: `List recent apply and unapply operations.

By default shows the last 20 operations for the active game install.
Use --all to show operations across all game installs.
Use --limit to change the number of operations shown.`,
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: extract
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

		ctx := cmd.Context()

		if err := internal.EnsureDBExists(); err != nil {
			return err
		}
		db, err := internal.SetupDB()
		if err != nil {
			return fmt.Errorf("error setting up database: %w", err)
		}
		defer db.Close()
		if err := internal.MigrateDB(ctx, db); err != nil {
			return fmt.Errorf("error migrating database: %w", err)
		}

		q := dbq.New(db)

		if operationsListAll {
			ops, err := q.ListAllOperations(ctx, int64(operationsListLimit))
			if err != nil {
				return fmt.Errorf("list operations: %w", err)
			}
			if len(ops) == 0 {
				fmt.Println(subtleStyle.Render("  no operations found"))
				return nil
			}
			fmt.Println(renderAllOperations(ops))
			return nil
		}

		// Resolve game install
		if operationsListGame == "" {
			active, err := state.LoadActive()
			if err != nil {
				return fmt.Errorf("load active selection: %w", err)
			}
			if active.ActiveGameInstallID == 0 {
				return fmt.Errorf("no active game selected; run `modctl games set-active ...` or pass --game")
			}
			operationsListGame = strconv.FormatInt(active.ActiveGameInstallID, 10)
		}

		gi, err := argresolver.ResolveGameInstallArg(ctx, q, operationsListGame)
		if err != nil {
			return err
		}

		ops, err := q.ListOperationsForGameInstall(ctx, dbq.ListOperationsForGameInstallParams{
			GameInstallID: gi.ID,
			Limit:         int64(operationsListLimit),
		})
		if err != nil {
			return fmt.Errorf("list operations: %w", err)
		}

		if len(ops) == 0 {
			fmt.Println(subtleStyle.Render(fmt.Sprintf("  no operations found for %s", gi.DisplayName)))
			return nil
		}

		fmt.Println(renderGameOperations(ops, gi.DisplayName))
		return nil
	},
}

func init() {
	operationsCmd.AddCommand(operationsListCmd)

	operationsListCmd.Flags().StringVarP(&operationsListGame, "game", "g", "",
		"Override the currently active game")
	operationsListCmd.RegisterFlagCompletionFunc("game",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completion.GameInstallSelectors(cmd, toComplete)
		})
	operationsListCmd.Flags().BoolVarP(&operationsListAll, "all", "A", false,
		"Show operations across all game installs")
	operationsListCmd.Flags().IntVar(&operationsListLimit, "limit", 20,
		"Maximum number of operations to show")

	operationsListCmd.MarkFlagsMutuallyExclusive("all", "game")
}

func renderAllOperations(ops []dbq.ListAllOperationsRow) string {
	var b strings.Builder
	boldStyle := lipgloss.NewStyle().Bold(true)
	b.WriteString(boldStyle.Render("Operations (all games)"))
	b.WriteString("\n\n")
	for _, op := range ops {
		gameName := "(unknown game)"
		if op.GameName.Valid {
			gameName = op.GameName.String
		}
		b.WriteString(formatOperationLine(
			op.ID, op.OpType, op.Status,
			op.StartedAt, op.FinishedAt,
			op.ProfileName, op.Message,
			gameName,
		))
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderGameOperations(ops []dbq.ListOperationsForGameInstallRow, gameName string) string {
	var b strings.Builder
	boldStyle := lipgloss.NewStyle().Bold(true)
	b.WriteString(boldStyle.Render(fmt.Sprintf("Operations for %s", gameName)))
	b.WriteString("\n\n")
	for _, op := range ops {
		b.WriteString(formatOperationLine(
			op.ID, op.OpType, op.Status,
			op.StartedAt, op.FinishedAt,
			op.ProfileName, op.Message,
			"",
		))
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatOperationLine renders a single operation as a summary line.
// gameName is only shown when non-empty (used for --all output).
func formatOperationLine(
	id int64,
	opType, status string,
	startedAt string,
	finishedAt sql.NullString,
	profileName sql.NullString,
	message sql.NullString,
	gameName string,
) string {
	// TODO extract styles
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	failedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	runningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

	// Status symbol
	var statusStr string
	switch status {
	case "success":
		statusStr = successStyle.Render("✓")
	case "failed":
		statusStr = failedStyle.Render("✗")
	case "running":
		statusStr = runningStyle.Render("⟳")
	default:
		statusStr = subtleStyle.Render("?")
	}

	// Elapsed time
	elapsed := ""
	if finishedAt.Valid {
		t1, err1 := time.Parse("2006-01-02T15:04:05.000Z", startedAt)
		t2, err2 := time.Parse("2006-01-02T15:04:05.000Z", finishedAt.String)
		if err1 == nil && err2 == nil {
			elapsed = subtleStyle.Render(fmt.Sprintf("(%.1fs)", t2.Sub(t1).Seconds()))
		}
	}

	// Profile name
	profile := ""
	if profileName.Valid {
		profile = subtleStyle.Render(fmt.Sprintf("%q", profileName.String))
	}

	// Game name (only for --all)
	game := ""
	if gameName != "" {
		game = subtleStyle.Render(fmt.Sprintf("[%s]", gameName))
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("  #%-6d %s  %-7s %-8s %s",
		id, statusStr, opType, status, startedAt))
	if elapsed != "" {
		sb.WriteString("  " + elapsed)
	}
	if profile != "" {
		sb.WriteString("  " + profile)
	}
	if game != "" {
		sb.WriteString("  " + game)
	}
	sb.WriteString("\n")

	// Error message on next line if failed
	if status == "failed" && message.Valid && strings.TrimSpace(message.String) != "" {
		sb.WriteString("         " + warnStyle.Render(fmt.Sprintf("error: %s", message.String)) + "\n")
	}

	return sb.String()
}
