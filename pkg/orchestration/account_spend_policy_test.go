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

package orchestration

import (
	stderrors "errors"
	"testing"
	"time"
)

// meteredPolicy is a policy that constrains both halves.
func meteredPolicy() AccountSpendPolicy {
	return AccountSpendPolicy{
		Quota:   fiveHourTokens(1000),
		Breaker: &BreakerPolicy{Window: time.Hour, FailureThreshold: 3, Cooldown: 10 * time.Minute},
	}
}

func TestAccountSpendPolicyMetered(t *testing.T) {
	cases := []struct {
		name   string
		policy AccountSpendPolicy
		want   bool
	}{
		{"the zero value meters nothing", AccountSpendPolicy{}, false},
		{"a quota window alone meters", AccountSpendPolicy{Quota: fiveHourTokens(10)}, true},
		{
			"a breaker alone meters",
			AccountSpendPolicy{Breaker: &BreakerPolicy{Window: time.Hour, FailureThreshold: 1, Cooldown: time.Minute}},
			true,
		},
		{"both meter", meteredPolicy(), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.policy.Metered(); got != tc.want {
				t.Errorf("Metered() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestAccountSpendPolicyValidateAcceptsAConfiguredPolicy(t *testing.T) {
	if err := meteredPolicy().Validate(); err != nil {
		t.Fatalf("Validate() on a well-formed policy = %v, want nil", err)
	}
	// The unmetered account is the ordinary one, and it must be storable:
	// refusing the zero value would make every account require a cap before
	// it could be created at all.
	if err := (AccountSpendPolicy{}).Validate(); err != nil {
		t.Fatalf("Validate() on the zero policy = %v, want nil", err)
	}
}

func TestAccountSpendPolicyValidateRefusesEachMalformedHalf(t *testing.T) {
	cases := []struct {
		name     string
		policy   AccountSpendPolicy
		sentinel error
	}{
		{
			name: "a window metering neither axis is not a limit",
			policy: AccountSpendPolicy{Quota: AccountQuotaPolicy{Windows: []QuotaWindow{
				{Name: "five-hour", Size: 5 * time.Hour},
			}}},
			sentinel: ErrQuotaInvalid,
		},
		{
			name: "an unnamed window cannot be reported in a refusal",
			policy: AccountSpendPolicy{Quota: AccountQuotaPolicy{Windows: []QuotaWindow{
				{Size: time.Hour, MaxTokens: 10},
			}}},
			sentinel: ErrQuotaInvalid,
		},
		{
			name: "a zero failure threshold trips an account that never failed",
			policy: AccountSpendPolicy{
				Breaker: &BreakerPolicy{Window: time.Hour, FailureThreshold: 0, Cooldown: time.Minute},
			},
			sentinel: ErrBreakerInvalid,
		},
		{
			name: "a zero cooldown makes an open breaker half-open at once",
			policy: AccountSpendPolicy{
				Breaker: &BreakerPolicy{Window: time.Hour, FailureThreshold: 1},
			},
			sentinel: ErrBreakerInvalid,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.policy.Validate()
			if err == nil {
				t.Fatal("Validate() = nil; a policy that cannot mean what it says must be refused at the write, " +
					"because by spawn time both answers are wrong")
			}
			if !stderrors.Is(err, tc.sentinel) {
				t.Errorf("Validate() = %v, want an error satisfying %v — the rule belongs to the half that owns it", err, tc.sentinel)
			}
		})
	}
}
