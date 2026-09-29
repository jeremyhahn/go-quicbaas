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

/**
 * SpendLimitWindow is one sliding allowance: [now - size, now). At least one
 * of max_tokens and max_equivalent_micros must be positive; an absent or zero
 * one leaves that axis unmetered. Mirrors orchestration.QuotaWindow.
 */
export interface SpendLimitWindow {
  /** name identifies the window in refusals ("five-hour", "weekly"). */
  name: string;
  /** size is the window's length in NANOSECONDS (a Go time.Duration). */
  size: number;
  /** max_tokens caps every token the provider metered. */
  max_tokens?: number;
  /**
   * max_equivalent_micros caps the list-price equivalent, in millionths of a
   * US dollar, of the window's priced turns.
   */
  max_equivalent_micros?: number;
}

/**
 * SpendLimits is the one limits shape an account, a project and a tenant
 * carry (go-quicbaas#1058): an ordered set of windows, every one binding where enforced.
 * No windows meters nothing.
 */
export interface SpendLimits {
  windows?: SpendLimitWindow[];
}

/** SpendBreakerPolicy takes an account out of service after repeated failures. */
export interface SpendBreakerPolicy {
  /** window, failure_threshold failures within it trip the breaker (nanoseconds). */
  window: number;
  failure_threshold: number;
  /** cooldown is how long a tripped account stays out of service (nanoseconds). */
  cooldown: number;
}

/** AccountSpendPolicy is what an account may spend and how failures take it out of service. */
export interface AccountSpendPolicy {
  quota?: SpendLimits;
  breaker?: SpendBreakerPolicy;
}
