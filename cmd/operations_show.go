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
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/mfinelli/modctl/internal/style"
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
		ctx := cmd.Context()

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

		style.Println(renderOperationDetail(op, changes))
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
	kv := style.KV{Indent: 2, Width: 16}

	var b strings.Builder

	// Header
	gameName := "(unknown game)"
	if op.GameName.Valid {
		gameName = op.GameName.String
	}
	b.WriteString(style.Bold.Render(fmt.Sprintf("Operation #%d — %s %s", op.ID, op.OpType, gameName)))
	b.WriteString("\n\n")

	kv.Write(&b, "Status:", op.Status)
	kv.Write(&b, "Started:", op.StartedAt)
	if op.FinishedAt.Valid {
		t1, err1 := time.Parse("2006-01-02T15:04:05.000Z", op.StartedAt)
		t2, err2 := time.Parse("2006-01-02T15:04:05.000Z", op.FinishedAt.String)
		if err1 == nil && err2 == nil {
			kv.Write(&b, "Finished:", fmt.Sprintf("%s  %s",
				op.FinishedAt.String,
				style.Subtle.Render(fmt.Sprintf("(%.1fs)", t2.Sub(t1).Seconds()))))
		} else {
			kv.Write(&b, "Finished:", op.FinishedAt.String)
		}
	}
	if op.ProfileName.Valid {
		kv.Write(&b, "Profile:", op.ProfileName.String)
	}
	if op.Message.Valid && strings.TrimSpace(op.Message.String) != "" {
		kv.Write(&b, "Message:", style.Warning.Render(op.Message.String))
	}

	b.WriteString("\n")

	// Changes
	b.WriteString(style.Section.Render(fmt.Sprintf("Changes (%d)", len(changes))))
	b.WriteString("\n\n")

	if len(changes) == 0 {
		b.WriteString(style.Subtle.Render("  (none recorded)") + "\n")
		return strings.TrimRight(b.String(), "\n")
	}

	for _, c := range changes {
		// Symbol
		var symbol string
		switch c.Action {
		case "write":
			symbol = style.Added.Render("+")
		case "overwrite":
			symbol = style.Changed.Render("~")
		case "remove":
			symbol = style.Removed.Render("-")
		case "restore_backup":
			symbol = style.Restored.Render("↩")
		case "noop":
			symbol = style.Subtle.Render("=")
		default:
			symbol = style.Subtle.Render("?")
		}

		b.WriteString(fmt.Sprintf("  %s %s\n", symbol, c.Relpath))

		// Content hashes
		if c.OldContentSha256.Valid {
			b.WriteString(fmt.Sprintf("      %s %s → ",
				style.Subtle.Render("hash:"),
				style.ShortSha(c.OldContentSha256.String)))
			if c.NewContentSha256.Valid {
				b.WriteString(style.ShortSha(c.NewContentSha256.String))
			} else {
				b.WriteString(style.Subtle.Render("(removed)"))
			}
			b.WriteString("\n")
		} else if c.NewContentSha256.Valid {
			b.WriteString(fmt.Sprintf("      %s %s\n",
				style.Subtle.Render("hash:"),
				style.ShortSha(c.NewContentSha256.String)))
		}

		// Sizes
		if c.OldSizeBytes.Valid && c.NewSizeBytes.Valid {
			b.WriteString(fmt.Sprintf("      %s %s → %s\n",
				style.Subtle.Render("size:"),
				style.Bytes(c.OldSizeBytes.Int64),
				style.Bytes(c.NewSizeBytes.Int64)))
		} else if c.NewSizeBytes.Valid {
			b.WriteString(fmt.Sprintf("      %s %s\n",
				style.Subtle.Render("size:"),
				style.Bytes(c.NewSizeBytes.Int64)))
		}

		// Backup reference
		if c.BackupBlobSha256.Valid {
			b.WriteString(fmt.Sprintf("      %s %s\n",
				style.Subtle.Render("backup:"),
				style.ShortSha(c.BackupBlobSha256.String)))
		}

		// Override reference
		if c.OwnerOverrideID.Valid {
			b.WriteString(fmt.Sprintf("      %s %s\n",
				style.Subtle.Render("override:"),
				style.Subtle.Render(fmt.Sprintf("(id %d)", c.OwnerOverrideID.Int64))))
		}

		// Notes
		if c.Notes.Valid && strings.TrimSpace(c.Notes.String) != "" {
			b.WriteString(fmt.Sprintf("      %s %s\n",
				style.Subtle.Render("notes:"),
				c.Notes.String))
		}
	}

	return strings.TrimRight(b.String(), "\n")
}
