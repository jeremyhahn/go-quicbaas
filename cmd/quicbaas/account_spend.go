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
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
)

// =============================================================================
// Account spend-policy flags — go-quicbaas#781, #1058
// =============================================================================
//
// An account's spend policy is its windows (the shared --spend-window /
// --clear-spend) and its failure breaker:
//
//	--breaker-failures 3 --breaker-window 1h --breaker-cooldown 30m
//	--clear-breaker
//
// On update, the flags given change only what they name: new windows keep the
// breaker, and a new breaker keeps the windows.

var (
	accountBreakerFailures int
	accountBreakerWindow   time.Duration
	accountBreakerCooldown time.Duration
	accountClearBreaker    bool
)

// accountBreakerFlags are the breaker flag names, all three required together.
var accountBreakerFlags = []string{"breaker-failures", "breaker-window", "breaker-cooldown"}

// InvalidAccountBreakerError is an incomplete or contradictory breaker.
type InvalidAccountBreakerError struct {
	Message string
}

// Error implements the error interface.
func (e *InvalidAccountBreakerError) Error() string {
	return "invalid breaker flags: " + e.Message
}

// addAccountSpendFlags registers the spend-policy flags on an account command.
func addAccountSpendFlags(cmd *cobra.Command) {
	addSpendFlags(cmd)
	cmd.Flags().IntVar(&accountBreakerFailures, "breaker-failures", 0,
		"Take the account out of service after this many failed turns within --breaker-window")
	cmd.Flags().DurationVar(&accountBreakerWindow, "breaker-window", 0, "The breaker's failure-counting window, e.g. 1h")
	cmd.Flags().DurationVar(&accountBreakerCooldown, "breaker-cooldown", 0, "How long a tripped account stays out of service, e.g. 30m")
	cmd.Flags().BoolVar(&accountClearBreaker, "clear-breaker", false, "Remove the failure breaker")
}

// accountSpendFlagsChanged reports whether any spend-policy flag was given.
func accountSpendFlagsChanged(cmd *cobra.Command) bool {
	for _, name := range append([]string{spendWindowFlag, clearSpendFlag, "clear-breaker"}, accountBreakerFlags...) {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

// accountSpendFromFlags applies the spend-policy flags to current, returning
// the policy to send, or nil when no flag was given.
func accountSpendFromFlags(cmd *cobra.Command, current spend.AccountPolicy) (*spend.AccountPolicy, error) {
	if !accountSpendFlagsChanged(cmd) {
		return nil, nil
	}
	policy := current
	limits, err := spendLimitsFromFlags(cmd)
	if err != nil {
		return nil, err
	}
	if limits != nil {
		policy.Quota = *limits
	}
	breaker, err := accountBreakerFromFlags(cmd)
	if err != nil {
		return nil, err
	}
	switch {
	case cmd.Flags().Changed("clear-breaker") && accountClearBreaker:
		policy.Breaker = nil
	case breaker != nil:
		policy.Breaker = breaker
	}
	return &policy, nil
}

// accountBreakerFromFlags reads the breaker flags: nil when none was given,
// and refused when only some were or when they contradict --clear-breaker.
func accountBreakerFromFlags(cmd *cobra.Command) (*spend.BreakerPolicy, error) {
	given := 0
	for _, name := range accountBreakerFlags {
		if cmd.Flags().Changed(name) {
			given++
		}
	}
	switch {
	case given == 0:
		return nil, nil
	case cmd.Flags().Changed("clear-breaker") && accountClearBreaker:
		return nil, &InvalidAccountBreakerError{Message: "--clear-breaker cannot be combined with a breaker"}
	case given < len(accountBreakerFlags):
		return nil, &InvalidAccountBreakerError{Message: "--breaker-failures, --breaker-window and --breaker-cooldown are required together"}
	case accountBreakerFailures < 1 || accountBreakerWindow <= 0 || accountBreakerCooldown <= 0:
		return nil, &InvalidAccountBreakerError{Message: "the threshold must be at least 1 and both durations positive; got " +
			strconv.Itoa(accountBreakerFailures) + " failures, " + accountBreakerWindow.String() + ", " + accountBreakerCooldown.String()}
	}
	return &spend.BreakerPolicy{
		Window:           accountBreakerWindow,
		FailureThreshold: accountBreakerFailures,
		Cooldown:         accountBreakerCooldown,
	}, nil
}

// formatAccountSpend renders an account's spend policy for a detail view.
func formatAccountSpend(policy spend.AccountPolicy) string {
	out := formatSpendLimits(policy.Quota)
	if policy.Breaker != nil {
		out += "; breaker after " + strconv.Itoa(policy.Breaker.FailureThreshold) + " failures in " +
			policy.Breaker.Window.String() + ", cooldown " + policy.Breaker.Cooldown.String()
	}
	return out
}
