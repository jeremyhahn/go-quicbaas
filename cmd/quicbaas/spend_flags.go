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
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
)

// =============================================================================
// Spend-limit flags — go-quicbaas#1058, ruling F5
// =============================================================================
//
// One limits shape is carried by an account, a project and a tenant, so one
// flag spelling sets it everywhere:
//
//	--spend-window name=five-hour,size=5h,tokens=2000000
//	--spend-window name=monthly,size=720h,usd=500
//
// Each --spend-window is one sliding window, repeatable, in order; together
// they REPLACE the limits. --clear-spend removes them. A window meters tokens,
// list-price dollars (usd) or both.

// spendWindowFlag is the repeatable window flag; clearSpendFlag clears.
const (
	spendWindowFlag = "spend-window"
	clearSpendFlag  = "clear-spend"
)

var (
	spendWindowValues []string
	spendClear        bool
)

// InvalidSpendWindowError is a --spend-window value that cannot be read.
type InvalidSpendWindowError struct {
	Value   string
	Message string
}

// Error implements the error interface.
func (e *InvalidSpendWindowError) Error() string {
	return "invalid --" + spendWindowFlag + " " + strconv.Quote(e.Value) + ": " + e.Message
}

// addSpendFlags registers the spend-limit flags on a command that sets limits.
func addSpendFlags(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&spendWindowValues, spendWindowFlag, nil,
		"A spend window, repeatable: name=NAME,size=DURATION[,tokens=N][,usd=DOLLARS] (the set replaces the limits)")
	cmd.Flags().BoolVar(&spendClear, clearSpendFlag, false, "Remove every spend window")
}

// spendLimitsFromFlags reads the spend-limit flags. It returns nil when
// neither was given, which leaves limits unchanged on an update.
func spendLimitsFromFlags(cmd *cobra.Command) (*spend.Limits, error) {
	windows := cmd.Flags().Changed(spendWindowFlag)
	clear := cmd.Flags().Changed(clearSpendFlag) && spendClear
	switch {
	case windows && clear:
		return nil, &InvalidSpendWindowError{Value: strings.Join(spendWindowValues, " "),
			Message: "cannot be combined with --" + clearSpendFlag}
	case clear:
		return &spend.Limits{}, nil
	case !windows:
		return nil, nil
	}
	limits := &spend.Limits{}
	for _, value := range spendWindowValues {
		window, err := parseSpendWindow(value)
		if err != nil {
			return nil, err
		}
		limits.Windows = append(limits.Windows, window)
	}
	return limits, nil
}

// spendWindowFields dispatches each key of a --spend-window value.
var spendWindowFields = map[string]func(w *spend.Window, value string) error{
	"name": func(w *spend.Window, value string) error { w.Name = value; return nil },
	"size": func(w *spend.Window, value string) error {
		size, err := time.ParseDuration(value)
		if err != nil || size <= 0 {
			return &InvalidSpendWindowError{Value: value, Message: "size must be a positive duration such as 5h or 168h"}
		}
		w.Size = size
		return nil
	},
	"tokens": func(w *spend.Window, value string) error {
		tokens, err := strconv.ParseInt(value, 10, 64)
		if err != nil || tokens < 0 {
			return &InvalidSpendWindowError{Value: value, Message: "tokens must be a non-negative integer"}
		}
		w.MaxTokens = tokens
		return nil
	},
	"usd": func(w *spend.Window, value string) error {
		micros, err := dollarsToMicros(value)
		if err != nil {
			return &InvalidSpendWindowError{Value: value, Message: "usd must be a non-negative dollar amount"}
		}
		w.MaxEquivalentMicros = micros
		return nil
	},
}

// parseSpendWindow reads one name=...,size=...[,tokens=...][,usd=...] value.
func parseSpendWindow(value string) (spend.Window, error) {
	var window spend.Window
	for _, pair := range strings.Split(value, ",") {
		key, field, ok := strings.Cut(strings.TrimSpace(pair), "=")
		set, known := spendWindowFields[strings.ToLower(key)]
		if !ok || !known {
			return spend.Window{}, &InvalidSpendWindowError{Value: value,
				Message: "expected name=NAME,size=DURATION[,tokens=N][,usd=DOLLARS]"}
		}
		if err := set(&window, strings.TrimSpace(field)); err != nil {
			return spend.Window{}, err
		}
	}
	if window.Name == "" || window.Size == 0 {
		return spend.Window{}, &InvalidSpendWindowError{Value: value, Message: "name and size are required"}
	}
	if window.MaxTokens == 0 && window.MaxEquivalentMicros == 0 {
		return spend.Window{}, &InvalidSpendWindowError{Value: value, Message: "set tokens, usd or both; a window that meters nothing is not a limit"}
	}
	return window, nil
}

// formatSpendLimits renders limits on one line for a detail view.
func formatSpendLimits(limits spend.Limits) string {
	if !limits.Metered() {
		return "none"
	}
	parts := make([]string, 0, len(limits.Windows))
	for _, w := range limits.Windows {
		axes := make([]string, 0, 2)
		if w.MaxTokens > 0 {
			axes = append(axes, strconv.FormatInt(w.MaxTokens, 10)+" tokens")
		}
		if w.MaxEquivalentMicros > 0 {
			axes = append(axes, "$"+strconv.FormatFloat(float64(w.MaxEquivalentMicros)/1_000_000, 'f', 2, 64))
		}
		parts = append(parts, w.Name+" ("+w.Size.String()+"): "+strings.Join(axes, " + "))
	}
	return strings.Join(parts, "; ")
}
