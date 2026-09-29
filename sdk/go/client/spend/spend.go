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

// Package spend holds the spend-limit types an account, a project and a
// tenant carry (go-quicbaas#1058). It mirrors orchestration.SpendLimits and
// orchestration.AccountSpendPolicy so SDK callers need not import the
// server-side orchestration package.
package spend

import "time"

// Window is one sliding allowance: [now-Size, now). At least one of MaxTokens
// and MaxEquivalentMicros must be positive; zero leaves that axis unmetered.
type Window struct {
	// Name identifies the window in refusals ("five-hour", "weekly").
	Name string `json:"name"`
	// Size is the window's length.
	Size time.Duration `json:"size"`
	// MaxTokens caps every token the provider metered.
	MaxTokens int64 `json:"max_tokens,omitempty"`
	// MaxEquivalentMicros caps the list-price equivalent, in millionths of a
	// US dollar, of the window's priced turns.
	MaxEquivalentMicros int64 `json:"max_equivalent_micros,omitempty"`
}

// Limits is an ordered set of windows, every one binding where enforced. The zero value
// meters nothing.
type Limits struct {
	Windows []Window `json:"windows,omitempty"`
}

// Metered reports whether the limits meter anything.
func (l Limits) Metered() bool { return len(l.Windows) > 0 }

// BreakerPolicy takes an account out of service after FailureThreshold
// failures within Window, for Cooldown.
type BreakerPolicy struct {
	Window           time.Duration `json:"window"`
	FailureThreshold int           `json:"failure_threshold"`
	Cooldown         time.Duration `json:"cooldown"`
}

// AccountPolicy is what an account may spend and how many failures take it
// out of service.
type AccountPolicy struct {
	Quota Limits `json:"quota,omitzero"`
	// Breaker is nil when the account is never taken out of service for
	// failing.
	Breaker *BreakerPolicy `json:"breaker,omitempty"`
}

// Metered reports whether the policy meters or breaks anything.
func (p AccountPolicy) Metered() bool { return p.Quota.Metered() || p.Breaker != nil }
