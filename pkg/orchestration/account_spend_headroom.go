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
	"errors"
	"time"
)

// =============================================================================
// The spend verdict on the wire (go-quicbaas#1058, ruling F6)
// =============================================================================
//
// A worker daemon selects accounts with the SAME resolver the control plane
// runs, but the ledger its spend is judged from lives on the control plane. So
// the daemon learns each candidate's verdict from the usage call it already
// makes for live sessions, and rebuilds the typed refusal the resolver's spend
// filter understands. SpendHeadroom is that verdict as data: what Check
// answered, projected so it can cross a process boundary and come back as the
// same error.

// Spend refusals, as named on the wire.
const (
	// SpendRefusalQuota is a window whose allowance is spent.
	SpendRefusalQuota = "quota_exhausted"

	// SpendRefusalBreaker is an account out of service for failing.
	SpendRefusalBreaker = "breaker_open"
)

// SpendHeadroom is the spend gate's read-only verdict on one account.
type SpendHeadroom struct {
	// Admits is true when the account's spend policy admits more work.
	Admits bool `json:"admits"`

	// Refusal names why not: SpendRefusalQuota or SpendRefusalBreaker.
	Refusal string `json:"refusal,omitempty"`

	// Window, Axis, Used and Limit describe a spent window.
	Window string `json:"window,omitempty"`
	Axis   string `json:"axis,omitempty"`
	Used   int64  `json:"used,omitempty"`
	Limit  int64  `json:"limit,omitempty"`

	// Failures is how many failures tripped an open breaker.
	Failures int `json:"failures,omitempty"`

	// FreesAt is when the refusal lifts; nil when nothing can say.
	FreesAt *time.Time `json:"frees_at,omitempty"`
}

// SpendHeadroomOf projects a spend gate's answer onto the wire shape. An
// answer that is neither an admission nor a refusal — the gate could not read
// the ledger — is returned as the error it is, never as a verdict.
func SpendHeadroomOf(err error) (SpendHeadroom, error) {
	if err == nil {
		return SpendHeadroom{Admits: true}, nil
	}
	if exhausted, ok := asQuotaExhausted(err); ok {
		return SpendHeadroom{
			Refusal: SpendRefusalQuota,
			Window:  exhausted.Window,
			Axis:    exhausted.Axis,
			Used:    exhausted.Used,
			Limit:   exhausted.Limit,
			FreesAt: optionalInstant(exhausted.RetryAt),
		}, nil
	}
	if open, ok := asBreakerOpen(err); ok {
		return SpendHeadroom{
			Refusal:  SpendRefusalBreaker,
			Failures: open.Failures,
			FreesAt:  optionalInstant(open.OpenUntil),
		}, nil
	}
	return SpendHeadroom{}, err
}

// Err rebuilds the typed refusal a verdict stands for, nil when it admits. A
// refusal this build does not know is refused rather than admitted: the safe
// direction for an unrecognized spend verdict is to stop.
func (h SpendHeadroom) Err(account *Account) error {
	if h.Admits {
		return nil
	}
	freesAt := time.Time{}
	if h.FreesAt != nil {
		freesAt = h.FreesAt.UTC()
	}
	switch h.Refusal {
	case SpendRefusalQuota:
		return &AccountQuotaExhaustedError{
			AccountID: account.ID, Provider: account.Provider,
			Window: h.Window, Axis: h.Axis, Used: h.Used, Limit: h.Limit, RetryAt: freesAt,
		}
	case SpendRefusalBreaker:
		return &AccountBreakerOpenError{
			AccountID: account.ID, Provider: account.Provider,
			Failures: h.Failures, OpenUntil: freesAt,
		}
	default:
		return &AccountSpendUnknownVerdictError{AccountID: account.ID, Reason: h.Refusal}
	}
}

func asQuotaExhausted(err error) (*AccountQuotaExhaustedError, bool) {
	var exhausted *AccountQuotaExhaustedError
	ok := errors.As(err, &exhausted)
	return exhausted, ok
}

func asBreakerOpen(err error) (*AccountBreakerOpenError, bool) {
	var open *AccountBreakerOpenError
	ok := errors.As(err, &open)
	return open, ok
}
