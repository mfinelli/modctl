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

package style

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBytes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024*1024 - 1, "1024.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{5 * 1024 * 1024, "5.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{3 * 1024 * 1024 * 1024 * 1024, "3.0 TB"},
		{1024 * 1024 * 1024 * 1024 * 1024, "1.0 PB"},
		{1 << 62, "4.0 EB"},
		{9223372036854775807, "8.0 EB"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, Bytes(tc.in), "Bytes(%d)", tc.in)
	}
}

func TestAgeAt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }

	cases := []struct {
		name string
		t    time.Time
		want string
	}{
		{"now", now, "just now"},
		{"in the future", now.Add(time.Hour), "just now"},
		{"seconds", ago(59 * time.Second), "just now"},
		{"one minute", ago(time.Minute), "1 minute ago"},
		{"just under two minutes", ago(119 * time.Second), "1 minute ago"},
		{"minutes", ago(5 * time.Minute), "5 minutes ago"},
		{"just under an hour", ago(59*time.Minute + 59*time.Second), "59 minutes ago"},
		{"one hour", ago(time.Hour), "1 hour ago"},
		{"hours", ago(5 * time.Hour), "5 hours ago"},
		{"just under a day", ago(23*time.Hour + 59*time.Minute), "23 hours ago"},
		{"one day", ago(24 * time.Hour), "1 day ago"},
		{"just under two days", ago(47 * time.Hour), "1 day ago"},
		{"days", ago(10 * 24 * time.Hour), "10 days ago"},
		{"a year", ago(365 * 24 * time.Hour), "365 days ago"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, AgeAt(now, tc.t))
		})
	}
}

func TestAge(t *testing.T) {
	t.Parallel()

	// uses the current time, so stay well away from the boundaries
	assert.Equal(t, "just now", Age(time.Now()))
	assert.Equal(t, "3 hours ago", Age(time.Now().Add(-3*time.Hour-time.Minute)))
}

func TestDuration(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   time.Duration
		want string
	}{
		{-time.Second, "now"},
		{0, "now"},
		{500 * time.Millisecond, "1s"},
		{time.Second, "1s"},
		{30 * time.Second, "30s"},
		{time.Minute, "1m"},
		{90 * time.Second, "1m 30s"},
		{59*time.Minute + 59*time.Second, "59m 59s"},
		{time.Hour, "1h"},
		{time.Hour + 5*time.Minute, "1h 5m"},
		{time.Hour + 5*time.Minute + 30*time.Second, "1h 5m"},
		{25 * time.Hour, "25h"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, Duration(tc.in), "Duration(%v)", tc.in)
	}
}

func TestShortSha(t *testing.T) {
	t.Parallel()

	full := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"abc", "abc"},
		{full[:16], full[:16]},
		{full[:17], full[:16] + "..."},
		{full, full[:16] + "..."},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, ShortSha(tc.in), "ShortSha(%q)", tc.in)
	}
}
