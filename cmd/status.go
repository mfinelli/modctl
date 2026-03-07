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
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/dbq"
	"github.com/mfinelli/modctl/internal"
	"github.com/spf13/cobra"
	"go.finelli.dev/util"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show a high-level status summary of all game installs",
	Long: `Show a high-level status summary of all game installs.

Displays each known game install with its currently applied profile,
number of tool-managed files, backup count, and any warnings such as
incomplete operations or installs that are no longer present on disk.

For detailed profile information including mod list and conflicts, use
'modctl profiles status'.`,
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
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

		installs, err := q.ListAllGameInstalls(ctx)
		if err != nil {
			return fmt.Errorf("list game installs: %w", err)
		}

		if len(installs) == 0 {
			subtle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
			fmt.Println(subtle.Render("  no game installs found - run 'modctl games refresh' to discover games"))
			return nil
		}

		// For each install gather supplementary data
		summaries := make([]installSummary, 0, len(installs))
		for _, gi := range installs {
			s := installSummary{install: gi}

			// Resolve applied profile name if set
			if gi.AppliedProfileID.Valid {
				p, err := q.GetProfileByID(ctx, gi.AppliedProfileID.Int64)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("get applied profile for install %d: %w", gi.ID, err)
				}
				if err == nil {
					s.appliedProfile = &p
				}
			}

			// File count
			count, err := q.GetInstalledFileCountForGameInstall(ctx, gi.ID)
			if err != nil {
				return fmt.Errorf("get file count for install %d: %w", gi.ID, err)
			}
			s.fileCount = count

			// Backup count
			bcount, err := q.GetBackupCountForGameInstall(ctx, gi.ID)
			if err != nil {
				return fmt.Errorf("get backup count for install %d: %w", gi.ID, err)
			}
			s.backupCount = bcount

			// Incomplete operation
			op, err := q.GetLastIncompleteOperationForGameInstall(ctx, gi.ID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("get incomplete operation for install %d: %w", gi.ID, err)
			}
			if err == nil {
				s.incompleteOp = &op
			}

			summaries = append(summaries, s)
		}

		fmt.Println(renderSystemStatus(summaries))

		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

type installSummary struct {
	install        dbq.GameInstall
	appliedProfile *dbq.Profile
	fileCount      int64
	backupCount    int64
	incompleteOp   *dbq.GetLastIncompleteOperationForGameInstallRow
}

func renderSystemStatus(summaries []installSummary) string {
	// TODO extract these
	boldStyle := lipgloss.NewStyle().Bold(true)
	subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	activeDot := greenStyle.Render("●")
	inactiveDot := dimStyle.Render("○")

	var b strings.Builder
	b.WriteString(boldStyle.Render("System Status"))
	b.WriteString("\n\n")

	for _, s := range summaries {
		// Dot: filled if something is applied, empty otherwise
		dot := inactiveDot
		if s.install.AppliedProfileID.Valid {
			dot = activeDot
		}

		// Header line: dot + game name + (not present) warning
		headerLine := fmt.Sprintf("%s %s", dot, boldStyle.Render(s.install.DisplayName))
		if s.install.InstanceID != "default" {
			headerLine += subtleStyle.Render(fmt.Sprintf(" (%s)", s.install.InstanceID))
		}
		if !util.SqliteIntToBool(s.install.IsPresent) {
			age := ""
			if s.install.LastSeenAt.Valid {
				t, err := time.Parse("2006-01-02T15:04:05.000Z", s.install.LastSeenAt.String)
				if err == nil {
					age = fmt.Sprintf(" - last seen %s", formatAge(t))
				}
			}
			headerLine += "  " + warnStyle.Render("(not present"+age+")")
		}
		b.WriteString("  " + headerLine + "\n")

		// Applied profile
		if s.appliedProfile != nil {
			appliedAgo := ""
			if s.install.AppliedAt.Valid {
				t, err := time.Parse("2006-01-02T15:04:05.000Z", s.install.AppliedAt.String)
				if err == nil {
					appliedAgo = subtleStyle.Render(fmt.Sprintf(" (applied %s)", formatAge(t)))
				}
			}
			writeKVIndented16(&b, "profile:", s.appliedProfile.Name+appliedAgo)
		} else {
			writeKVIndented16(&b, "profile:", subtleStyle.Render("(none applied)"))
		}

		// File and backup counts - only show if non-zero or something is applied
		if s.fileCount > 0 || s.install.AppliedProfileID.Valid {
			writeKVIndented16(&b, "managed:", fmt.Sprintf("%d files", s.fileCount))
		}
		if s.backupCount > 0 {
			writeKVIndented16(&b, "backups:", fmt.Sprintf("%d files", s.backupCount))
		}

		// Incomplete operation warning
		if s.incompleteOp != nil {
			b.WriteString("      " + warnStyle.Render(fmt.Sprintf(
				"⚠  incomplete %s operation #%d from %s - run 'modctl apply --abort' or 'modctl apply --force'",
				s.incompleteOp.OpType,
				s.incompleteOp.ID,
				s.incompleteOp.StartedAt,
			)) + "\n")
		}

		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}
