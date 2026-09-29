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

// =============================================================================
// The spend policy lives on the account
// =============================================================================
//
// AN ALLOWANCE BELONGS TO THE THING THAT IS BILLED. A provider meters a
// LOGIN — a seat, a plan, an API key — and that is exactly what an Account
// records. Keeping the policy anywhere else (a project, a deployment config)
// would mean two accounts on one plan could each be admitted up to the plan's
// whole allowance, which is not a cap, and would need a second read on the
// path that decides whether a credential may be released.
//
// It costs no extra read where it is enforced: the resolver and the
// materialize verb both already have the Account record in hand when they ask.
//
// AN ACCOUNT WITH NO POLICY IS UNMETERED, AND SAYS SO. The zero value meters
// nothing and admits, because that is the only honest answer for an account
// nobody has configured a limit for — inventing a default token allowance
// would be inventing a fact about someone's plan, and a wrong one refuses work
// that the provider would have served. What the deployment's shipped default
// should be is an operator decision; see docs/agent-orchestration/cost-enforcement.md.

// AccountSpendPolicy is what one pooled account may spend and how many
// failures it may accumulate before it is taken out of service.
//
// Both halves are optional and independent: an account may be metered without
// a breaker (a plan with a hard cap and a reliable provider), or broken
// without a quota (an unmetered key on a flaky endpoint).
type AccountSpendPolicy struct {
	// Quota is the set of sliding windows this account is metered by. A
	// policy with no windows meters nothing.
	Quota SpendLimits `json:"quota,omitzero"`

	// Breaker is the tripping rule derived from the account's failure
	// entries. Nil means the account is never taken out of service for
	// failing — the zero BreakerPolicy is not a valid one (a zero window and
	// a zero threshold would trip an account that has never failed), so the
	// absence has to be expressed by the pointer rather than by the value.
	Breaker *BreakerPolicy `json:"breaker,omitempty"`
}

// Metered reports whether the policy constrains anything at all. It exists so
// a caller can say "this account is unmetered" without reaching into both
// halves and re-deriving the rule in two places.
func (p AccountSpendPolicy) Metered() bool {
	return len(p.Quota.Windows) > 0 || p.Breaker != nil
}

// Validate refuses a policy that cannot mean what it appears to.
//
// It delegates to the two halves rather than restating their rules: the quota
// authority and the breaker each own what a valid window and a valid
// threshold are, and a second copy here would be the copy that goes stale.
func (p AccountSpendPolicy) Validate() error {
	if err := p.Quota.validate(); err != nil {
		return err
	}
	if p.Breaker == nil {
		return nil
	}
	return p.Breaker.validate()
}
