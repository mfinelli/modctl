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
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mfinelli/modctl/internal/nexusclient"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var authNexusStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Nexus Mods authentication status and API quota",
	Long: `Show whether modctl is authenticated with Nexus Mods.

Makes a live call to the Nexus Mods validate endpoint to confirm the stored
API key is valid and retrieves current rate limit quota. This endpoint does
not count against your API request quota.`,
	Args:         cobra.ExactArgs(0),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: extract
		subtleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7")).Width(16)
		okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
		errStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

		apiKey := viper.GetString("nexus.apikey")
		if apiKey == "" {
			fmt.Println(warnStyle.Render("  ⚠ not authenticated"))
			fmt.Println(subtleStyle.Render("    run `modctl auth nexus login` to authenticate"))
			fmt.Println()
			return nil
		}

		ctx := cmd.Context()
		client, err := nexusclient.New(ctx, apiKey, logger, rootCmd.Version)
		if err != nil {
			return fmt.Errorf("initializing nexus client: %w", err)
		}
		defer client.Close()

		info, err := client.ValidateUser()
		if err != nil {
			fmt.Println(errStyle.Render("  ✗ API key is invalid or has been revoked"))
			fmt.Println(subtleStyle.Render("    run `modctl auth nexus login` to re-authenticate"))
			fmt.Println()
			// Not returning the raw error - it's not useful to the user here
			return fmt.Errorf("nexus API key validation failed")
		}

		fmt.Println(okStyle.Render("  ✓ authenticated with Nexus Mods"))
		fmt.Println()

		var b strings.Builder
		b.WriteString("  " + labelStyle.Render("username:") + " " + info.Name + "\n")

		// Rate limit state was updated as a side effect of ValidateUser
		state, err := nexusclient.LoadRateLimitState()
		if err != nil {
			// Non-fatal: we already have the username, just skip quota display
			logger.Warn("failed to load rate limit state", "error", err)
			fmt.Print(b.String())
			return nil
		}

		hourly, daily := state.EffectiveRemaining()

		b.WriteString("  " + labelStyle.Render("daily quota:") + " " +
			fmt.Sprintf("%s / %d requests remaining",
				formatQuota(daily, state.DailyLimit),
				state.DailyLimit,
			) + "\n")
		b.WriteString("  " + labelStyle.Render("") + "   " +
			subtleStyle.Render(fmt.Sprintf("resets in %s", formatDuration(time.Until(state.DailyReset)))) + "\n")

		b.WriteString("  " + labelStyle.Render("hourly quota:") + " " +
			fmt.Sprintf("%s / %d requests remaining",
				formatQuota(hourly, state.HourlyLimit),
				state.HourlyLimit,
			) + "\n")
		b.WriteString("  " + labelStyle.Render("") + "   " +
			subtleStyle.Render(fmt.Sprintf("resets in %s", formatDuration(time.Until(state.HourlyReset)))) + "\n")

		fmt.Print(b.String())
		fmt.Println()
		return nil
	},
}

func init() {
	authNexusCmd.AddCommand(authNexusStatusCmd)
}

// formatQuota renders the remaining count, coloring it yellow when below 20%
// and red when at zero.
func formatQuota(remaining, limit int) string {
	s := fmt.Sprintf("%d", remaining)
	switch {
	case remaining == 0:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")).Render(s)
	case limit > 0 && remaining*100/limit < 20:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render(s)
	default:
		return s
	}
}

// formatDuration renders a duration as a human-readable string, e.g.
// "23h 4m", "47m", "30s". Negative durations (reset already passed) return
// "now".
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "now"
	}
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	case m > 0 && s > 0:
		return fmt.Sprintf("%dm %ds", m, s)
	case m > 0:
		return fmt.Sprintf("%dm", m)
	default:
		return fmt.Sprintf("%ds", s)
	}
}
