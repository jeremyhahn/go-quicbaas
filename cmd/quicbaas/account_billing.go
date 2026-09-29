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
	"math"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	accountSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
)

// =============================================================================
// Account billing flags — go-quicbaas#1058
// =============================================================================
//
// --billing says how the account is billed, which decides the billed figure
// every turn on it carries beside its list-price equivalent: "subscription" (a
// turn adds nothing to the bill), "api" (a turn bills its list price), or, on
// update only, "none" to clear it back to not configured. --monthly-fee is a
// subscription's flat fee in US dollars, recorded on the account (reports do
// not pro-rate it yet).

// accountBillingNone clears an account's billing on update.
const accountBillingNone = "none"

var (
	accountBillingMode string
	accountMonthlyFee  string
)

// accountBillingModes is the canonical --billing lookup.
var accountBillingModes = map[string]accountSDK.BillingMode{
	string(accountSDK.BillingSubscription): accountSDK.BillingSubscription,
	string(accountSDK.BillingAPI):          accountSDK.BillingAPI,
}

// InvalidAccountBillingError indicates an unusable --billing or --monthly-fee.
type InvalidAccountBillingError struct {
	Flag    string
	Value   string
	Message string
}

// Error implements the error interface.
func (e *InvalidAccountBillingError) Error() string {
	return "invalid --" + e.Flag + " " + strconv.Quote(e.Value) + ": " + e.Message
}

// addAccountBillingFlags registers the billing flags on a create or update
// command.
func addAccountBillingFlags(cmd *cobra.Command, update bool) {
	usage := "How the account is billed: subscription or api"
	if update {
		usage += " (none clears it)"
	}
	cmd.Flags().StringVar(&accountBillingMode, "billing", "", usage)
	cmd.Flags().StringVar(&accountMonthlyFee, "monthly-fee", "",
		"A subscription's flat monthly fee in US dollars, e.g. 200 (requires --billing subscription)")
}

// accountBillingFromFlags reads the billing flags. It returns nil when neither
// was set, which leaves an account's billing unchanged on update and not
// configured on create.
func accountBillingFromFlags(cmd *cobra.Command) (*accountSDK.Billing, error) {
	if !cmd.Flags().Changed("billing") && !cmd.Flags().Changed("monthly-fee") {
		return nil, nil
	}
	mode := strings.ToLower(strings.TrimSpace(accountBillingMode))
	if mode == accountBillingNone && cmd.Name() == "update" {
		if accountMonthlyFee != "" {
			return nil, &InvalidAccountBillingError{Flag: "monthly-fee", Value: accountMonthlyFee,
				Message: "cannot be set while clearing the billing"}
		}
		return &accountSDK.Billing{}, nil
	}
	billing := &accountSDK.Billing{}
	resolved, ok := accountBillingModes[mode]
	if !ok {
		return nil, &InvalidAccountBillingError{Flag: "billing", Value: accountBillingMode,
			Message: "must be subscription or api"}
	}
	billing.Mode = resolved
	if accountMonthlyFee == "" {
		return billing, nil
	}
	fee, err := dollarsToMicros(accountMonthlyFee)
	if err != nil {
		return nil, err
	}
	billing.MonthlyFeeMicros = fee
	return billing, nil
}

// dollarsToMicros parses a dollar amount into integer micro-dollars.
func dollarsToMicros(value string) (int64, error) {
	dollars, err := strconv.ParseFloat(strings.TrimPrefix(strings.TrimSpace(value), "$"), 64)
	if err != nil || math.IsNaN(dollars) || math.IsInf(dollars, 0) || dollars < 0 {
		return 0, &InvalidAccountBillingError{Flag: "monthly-fee", Value: value,
			Message: "must be a non-negative dollar amount"}
	}
	return int64(math.Round(dollars * 1_000_000)), nil
}

// accountBillingLabel renders an account's billing for a person.
func accountBillingLabel(billing *accountSDK.Billing) string {
	if billing == nil || billing.Mode == "" {
		return "not configured"
	}
	if billing.MonthlyFeeMicros == 0 {
		return string(billing.Mode)
	}
	return string(billing.Mode) + " ($" + strconv.FormatFloat(float64(billing.MonthlyFeeMicros)/1_000_000, 'f', 2, 64) + "/month)"
}
