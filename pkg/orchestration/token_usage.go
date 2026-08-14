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

// TokenUsage is the token accounting one agent turn reported.
//
// Every field is a count the agent itself published; nothing here is inferred,
// estimated, or derived from response length. A backend whose CLI does not
// publish token counts leaves PromptResponse.Usage nil, and the turn is simply
// not recorded — an absent measurement is reported as absent rather than
// approximated, because an approximation would silently corrupt the per-account
// attribution that account selection and chargeback both read back.
//
// The four fields mirror the Anthropic/Claude Code accounting model, which is
// the shape every backend that reports usage in this codebase emits. Cache
// fields are carried separately from InputTokens because the provider bills
// them at different rates; Total sums them so a caller that only needs "how
// much did this turn consume" does not have to know the breakdown.
type TokenUsage struct {
	// InputTokens is the number of uncached prompt tokens consumed.
	InputTokens int64 `json:"input_tokens,omitempty"`

	// OutputTokens is the number of tokens the agent generated.
	OutputTokens int64 `json:"output_tokens,omitempty"`

	// CacheCreationInputTokens is the number of prompt tokens written into the
	// provider's prompt cache on this turn.
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`

	// CacheReadInputTokens is the number of prompt tokens served from the
	// provider's prompt cache on this turn.
	CacheReadInputTokens int64 `json:"cache_read_input_tokens,omitempty"`
}

// Total returns the sum of every token dimension the turn reported. It is the
// unit LeastUsageAccountStrategy balances on (see account_strategy.go). A nil
// receiver totals zero so callers need not nil-check before summing.
func (u *TokenUsage) Total() int64 {
	if u == nil {
		return 0
	}
	return u.InputTokens + u.OutputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
}

// Empty reports whether the turn carried no measured usage at all. It
// distinguishes "the agent reported zero" from "the agent reported nothing",
// both of which must leave the cost ledger untouched.
func (u *TokenUsage) Empty() bool {
	return u.Total() == 0
}

// Add accumulates other into u and returns u, so a caller streaming several
// per-message usage records over one turn can fold them into a single total.
// A nil other contributes nothing.
func (u *TokenUsage) Add(other *TokenUsage) *TokenUsage {
	if other == nil {
		return u
	}
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.CacheCreationInputTokens += other.CacheCreationInputTokens
	u.CacheReadInputTokens += other.CacheReadInputTokens
	return u
}
