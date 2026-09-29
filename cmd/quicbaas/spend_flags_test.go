// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package main

import (
	"errors"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
)

// spendCommand builds a bare command carrying the spend flags, parsed from args.
func spendCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	spendWindowValues, spendClear = nil, false
	t.Cleanup(func() { spendWindowValues, spendClear = nil, false })
	cmd := &cobra.Command{Use: "update"}
	addSpendFlags(cmd)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	return cmd
}

func TestSpendLimitsFromFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want *spend.Limits
	}{
		{"no flags leaves limits alone", nil, nil},
		{"--clear-spend clears", []string{"--clear-spend"}, &spend.Limits{}},
		{"windows in order", []string{
			"--spend-window", "name=five-hour,size=5h,tokens=2000000",
			"--spend-window", "name=monthly, size=720h, usd=$500",
		}, &spend.Limits{Windows: []spend.Window{
			{Name: "five-hour", Size: 5 * time.Hour, MaxTokens: 2_000_000},
			{Name: "monthly", Size: 720 * time.Hour, MaxEquivalentMicros: 500_000_000},
		}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := spendLimitsFromFlags(spendCommand(t, tc.args...))
			if err != nil {
				t.Fatalf("spendLimitsFromFlags: %v", err)
			}
			if (got == nil) != (tc.want == nil) {
				t.Fatalf("limits = %+v, want %+v", got, tc.want)
			}
			if got != nil && len(got.Windows) != len(tc.want.Windows) {
				t.Fatalf("limits = %+v, want %+v", got, tc.want)
			}
			for i := range windowsOf(tc.want) {
				if got.Windows[i] != tc.want.Windows[i] {
					t.Fatalf("window %d = %+v, want %+v", i, got.Windows[i], tc.want.Windows[i])
				}
			}
		})
	}
}

func TestSpendLimitsFromFlagsRefuses(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"windows and clear together", []string{"--spend-window", "name=a,size=1h,tokens=1", "--clear-spend"}},
		{"an unknown key", []string{"--spend-window", "name=a,size=1h,dollars=5"}},
		{"no name", []string{"--spend-window", "size=1h,tokens=1"}},
		{"a bad size", []string{"--spend-window", "name=a,size=forever,tokens=1"}},
		{"a negative token cap", []string{"--spend-window", "name=a,size=1h,tokens=-1"}},
		{"a bad dollar cap", []string{"--spend-window", "name=a,size=1h,usd=lots"}},
		{"a window metering nothing", []string{"--spend-window", "name=a,size=1h"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := spendLimitsFromFlags(spendCommand(t, tc.args...))
			var invalid *InvalidSpendWindowError
			if !errors.As(err, &invalid) {
				t.Fatalf("err = %v, want an InvalidSpendWindowError", err)
			}
		})
	}
}

func TestFormatSpendLimits(t *testing.T) {
	if got := formatSpendLimits(spend.Limits{}); got != "none" {
		t.Fatalf("empty = %q, want none", got)
	}
	got := formatSpendLimits(spend.Limits{Windows: []spend.Window{
		{Name: "daily", Size: 24 * time.Hour, MaxTokens: 1000, MaxEquivalentMicros: 12_500_000},
	}})
	if want := "daily (24h0m0s): 1000 tokens + $12.50"; got != want {
		t.Fatalf("formatSpendLimits = %q, want %q", got, want)
	}
}

// windowsOf is the windows of limits, or none for nil.
func windowsOf(limits *spend.Limits) []spend.Window {
	if limits == nil {
		return nil
	}
	return limits.Windows
}

// accountSpendCommand builds a bare account command with the spend-policy
// flags, parsed from args.
func accountSpendCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	spendWindowValues, spendClear = nil, false
	accountBreakerFailures, accountBreakerWindow, accountBreakerCooldown, accountClearBreaker = 0, 0, 0, false
	cmd := &cobra.Command{Use: "update"}
	addAccountSpendFlags(cmd)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	return cmd
}

func TestAccountSpendFromFlags(t *testing.T) {
	current := spend.AccountPolicy{
		Quota:   spend.Limits{Windows: []spend.Window{{Name: "daily", Size: 24 * time.Hour, MaxTokens: 5}}},
		Breaker: &spend.BreakerPolicy{Window: time.Hour, FailureThreshold: 3, Cooldown: time.Minute},
	}
	if got, err := accountSpendFromFlags(accountSpendCommand(t), current); got != nil || err != nil {
		t.Fatalf("no flags = %+v, %v; want nil", got, err)
	}
	windows, err := accountSpendFromFlags(accountSpendCommand(t, "--spend-window", "name=weekly,size=168h,usd=10"), current)
	if err != nil || windows.Quota.Windows[0].Name != "weekly" || windows.Breaker == nil {
		t.Fatalf("new windows = %+v, %v; want the breaker kept", windows, err)
	}
	breaker, err := accountSpendFromFlags(accountSpendCommand(t, "--breaker-failures", "5", "--breaker-window", "2h", "--breaker-cooldown", "10m"), current)
	if err != nil || breaker.Breaker.FailureThreshold != 5 || len(breaker.Quota.Windows) != 1 {
		t.Fatalf("new breaker = %+v, %v; want the windows kept", breaker, err)
	}
	cleared, err := accountSpendFromFlags(accountSpendCommand(t, "--clear-breaker", "--clear-spend"), current)
	if err != nil || cleared.Metered() {
		t.Fatalf("clear = %+v, %v; want unmetered", cleared, err)
	}
}

func TestAccountSpendFromFlagsRefuses(t *testing.T) {
	for _, args := range [][]string{
		{"--breaker-failures", "3"},
		{"--breaker-failures", "0", "--breaker-window", "1h", "--breaker-cooldown", "1m"},
		{"--clear-breaker", "--breaker-failures", "3", "--breaker-window", "1h", "--breaker-cooldown", "1m"},
	} {
		_, err := accountSpendFromFlags(accountSpendCommand(t, args...), spend.AccountPolicy{})
		var invalid *InvalidAccountBreakerError
		if !errors.As(err, &invalid) {
			t.Fatalf("%v: err = %v, want an InvalidAccountBreakerError", args, err)
		}
	}
	_, err := accountSpendFromFlags(accountSpendCommand(t, "--spend-window", "name=a,size=1h"), spend.AccountPolicy{})
	var window *InvalidSpendWindowError
	if !errors.As(err, &window) {
		t.Fatalf("err = %v, want an InvalidSpendWindowError", err)
	}
}

func TestFormatAccountSpend(t *testing.T) {
	got := formatAccountSpend(spend.AccountPolicy{Breaker: &spend.BreakerPolicy{Window: time.Hour, FailureThreshold: 3, Cooldown: time.Minute}})
	if want := "none; breaker after 3 failures in 1h0m0s, cooldown 1m0s"; got != want {
		t.Fatalf("formatAccountSpend = %q, want %q", got, want)
	}
}
