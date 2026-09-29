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

	"github.com/spf13/cobra"

	accountSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
)

// billingCommand builds a bare command named name carrying the billing flags,
// parsed from args, with the package flag variables reset first.
func billingCommand(t *testing.T, name string, args ...string) *cobra.Command {
	t.Helper()
	accountBillingMode, accountMonthlyFee = "", ""
	t.Cleanup(func() { accountBillingMode, accountMonthlyFee = "", "" })
	cmd := &cobra.Command{Use: name}
	addAccountBillingFlags(cmd, name == "update")
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	return cmd
}

func TestAccountBillingFromFlags(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		args []string
		want *accountSDK.Billing
	}{
		{"no flags leaves billing alone", "update", nil, nil},
		{"a subscription with its fee", "create", []string{"--billing", "subscription", "--monthly-fee", "200"},
			&accountSDK.Billing{Mode: accountSDK.BillingSubscription, MonthlyFeeMicros: 200_000_000}},
		{"a fee written with cents and a dollar sign", "create", []string{"--billing", "Subscription", "--monthly-fee", "$19.99"},
			&accountSDK.Billing{Mode: accountSDK.BillingSubscription, MonthlyFeeMicros: 19_990_000}},
		{"a metered key", "create", []string{"--billing", "api"}, &accountSDK.Billing{Mode: accountSDK.BillingAPI}},
		{"none clears on update", "update", []string{"--billing", "none"}, &accountSDK.Billing{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := accountBillingFromFlags(billingCommand(t, tc.cmd, tc.args...))
			if err != nil {
				t.Fatalf("accountBillingFromFlags: %v", err)
			}
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Fatalf("billing = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestAccountBillingFromFlagsRefuses(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		args []string
		flag string
	}{
		{"an unknown mode", "create", []string{"--billing", "prepaid"}, "billing"},
		{"none on create", "create", []string{"--billing", "none"}, "billing"},
		{"a fee with no mode", "update", []string{"--monthly-fee", "20"}, "billing"},
		{"a negative fee", "create", []string{"--billing", "subscription", "--monthly-fee", "-5"}, "monthly-fee"},
		{"a fee that is not a number", "create", []string{"--billing", "subscription", "--monthly-fee", "lots"}, "monthly-fee"},
		{"a fee while clearing", "update", []string{"--billing", "none", "--monthly-fee", "5"}, "monthly-fee"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := accountBillingFromFlags(billingCommand(t, tc.cmd, tc.args...))
			var invalid *InvalidAccountBillingError
			if !errors.As(err, &invalid) || invalid.Flag != tc.flag {
				t.Fatalf("err = %v, want an InvalidAccountBillingError on --%s", err, tc.flag)
			}
		})
	}
}

func TestAccountBillingLabel(t *testing.T) {
	tests := []struct {
		billing *accountSDK.Billing
		want    string
	}{
		{nil, "not configured"},
		{&accountSDK.Billing{}, "not configured"},
		{&accountSDK.Billing{Mode: accountSDK.BillingAPI}, "api"},
		{&accountSDK.Billing{Mode: accountSDK.BillingSubscription, MonthlyFeeMicros: 200_000_000}, "subscription ($200.00/month)"},
	}
	for _, tc := range tests {
		if got := accountBillingLabel(tc.billing); got != tc.want {
			t.Errorf("accountBillingLabel(%+v) = %q, want %q", tc.billing, got, tc.want)
		}
	}
}
