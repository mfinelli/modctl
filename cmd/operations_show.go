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
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/spf13/cobra"
)

var operationsShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show file-level detail for a specific operation",
	Long: `Show file-level detail for a specific operation.

Displays every file change recorded during the operation including
action taken, content hashes before and after, and backup references.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()

		opID, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil || opID <= 0 {
			return fmt.Errorf("invalid operation id %q (expected a positive integer)", args[0])
		}

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

		op, err := q.GetOperationByID(ctx, opID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("operation #%d not found", opID)
			}
			return fmt.Errorf("get operation: %w", err)
		}

		changes, err := q.ListOperationChanges(ctx, opID)
		if err != nil {
			return fmt.Errorf("list operation changes: %w", err)
		}

		fmt.Println(renderOperationDetail(op, changes))
		return nil
	},
}

func init() {
	operationsCmd.AddCommand(operationsShowCmd)
}

func renderOperationDetail(
	op dbq.GetOperationByIDRow,
	changes []dbq.OperationChange,
) string {
	// TODO extract
	boldStyle := lipgloss.NewStyle().Bold(true)
	subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	redStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	yellowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	cyanStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	sectionTitleStyle := lipgloss.NewStyle().Bold(true).MarginTop(1)

	var b strings.Builder

	// Header
	gameName := "(unknown game)"
	if op.GameName.Valid {
		gameName = op.GameName.String
	}
	b.WriteString(boldStyle.Render(fmt.Sprintf("Operation #%d — %s %s", op.ID, op.OpType, gameName)))
	b.WriteString("\n\n")

	writeKV16(&b, "Status:", op.Status)
	writeKV16(&b, "Started:", op.StartedAt)
	if op.FinishedAt.Valid {
		t1, err1 := time.Parse("2006-01-02T15:04:05.000Z", op.StartedAt)
		t2, err2 := time.Parse("2006-01-02T15:04:05.000Z", op.FinishedAt.String)
		if err1 == nil && err2 == nil {
			writeKV16(&b, "Finished:", fmt.Sprintf("%s  %s",
				op.FinishedAt.String,
				subtleStyle.Render(fmt.Sprintf("(%.1fs)", t2.Sub(t1).Seconds()))))
		} else {
			writeKV16(&b, "Finished:", op.FinishedAt.String)
		}
	}
	if op.ProfileName.Valid {
		writeKV16(&b, "Profile:", op.ProfileName.String)
	}
	if op.Message.Valid && strings.TrimSpace(op.Message.String) != "" {
		writeKV16(&b, "Message:", warnStyle.Render(op.Message.String))
	}

	b.WriteString("\n")

	// Changes
	b.WriteString(sectionTitleStyle.Render(fmt.Sprintf("Changes (%d)", len(changes))))
	b.WriteString("\n\n")

	if len(changes) == 0 {
		b.WriteString(subtleStyle.Render("  (none recorded)") + "\n")
		return strings.TrimRight(b.String(), "\n")
	}

	for _, c := range changes {
		// Symbol
		var symbol string
		switch c.Action {
		case "write":
			symbol = greenStyle.Render("+")
		case "overwrite":
			symbol = yellowStyle.Render("~")
		case "remove":
			symbol = redStyle.Render("-")
		case "restore_backup":
			symbol = cyanStyle.Render("↩")
		case "noop":
			symbol = subtleStyle.Render("=")
		default:
			symbol = subtleStyle.Render("?")
		}

		b.WriteString(fmt.Sprintf("  %s %s\n", symbol, c.Relpath))

		// Content hashes
		if c.OldContentSha256.Valid {
			b.WriteString(fmt.Sprintf("      %s %s → ",
				subtleStyle.Render("hash:"),
				truncateSha(c.OldContentSha256.String)))
			if c.NewContentSha256.Valid {
				b.WriteString(truncateSha(c.NewContentSha256.String))
			} else {
				b.WriteString(subtleStyle.Render("(removed)"))
			}
			b.WriteString("\n")
		} else if c.NewContentSha256.Valid {
			b.WriteString(fmt.Sprintf("      %s %s\n",
				subtleStyle.Render("hash:"),
				truncateSha(c.NewContentSha256.String)))
		}

		// Sizes
		if c.OldSizeBytes.Valid && c.NewSizeBytes.Valid {
			b.WriteString(fmt.Sprintf("      %s %s → %s\n",
				subtleStyle.Render("size:"),
				formatBytes(c.OldSizeBytes.Int64),
				formatBytes(c.NewSizeBytes.Int64)))
		} else if c.NewSizeBytes.Valid {
			b.WriteString(fmt.Sprintf("      %s %s\n",
				subtleStyle.Render("size:"),
				formatBytes(c.NewSizeBytes.Int64)))
		}

		// Backup reference
		if c.BackupBlobSha256.Valid {
			b.WriteString(fmt.Sprintf("      %s %s\n",
				subtleStyle.Render("backup:"),
				truncateSha(c.BackupBlobSha256.String)))
		}

		// Notes
		if c.Notes.Valid && strings.TrimSpace(c.Notes.String) != "" {
			b.WriteString(fmt.Sprintf("      %s %s\n",
				subtleStyle.Render("notes:"),
				c.Notes.String))
		}
	}

	return strings.TrimRight(b.String(), "\n")
}
