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

import "encoding/json"

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

	// malformed records that this value was decoded from accounting no token
	// count can have, and was repaired to make it representable.
	//
	// UNEXPORTED AND NEVER SERIALISED, deliberately. It is a property of THIS
	// decode — of what one producer sent on one turn — not of the accounting
	// itself, and forwarding it would let a downstream reader mistake it for
	// something a producer reported. It exists so a repaired record does not
	// become indistinguishable from a clean one the moment it is repaired.
	malformed bool
}

// normalize clamps every negative dimension to zero and reports whether it had
// to, marking the value when it did.
//
// A NEGATIVE TOKEN COUNT IS A BUG IN THE PRODUCER, not a quantity. It cannot
// mean "fewer than none were consumed", so there is no arithmetic that carries
// it forward faithfully — the only question is whether it is repaired loudly or
// silently, and silently is what made it invisible.
//
// Clamping repairs each dimension on its own, which is what stops one bad
// dimension from CANCELLING a good one: {"input_tokens":1,"output_tokens":-1}
// summed to zero, read as "the CLI reported nothing", and the turn was billed
// as nothing (#242).
func (u *TokenUsage) normalize() bool {
	if u == nil {
		return false
	}
	repaired := false
	for _, dimension := range []*int64{
		&u.InputTokens,
		&u.OutputTokens,
		&u.CacheCreationInputTokens,
		&u.CacheReadInputTokens,
	} {
		if *dimension < 0 {
			*dimension = 0
			repaired = true
		}
	}
	if repaired {
		u.malformed = true
	}
	return repaired
}

// nonNegative returns v, or zero when a producer reported a negative count,
// recording the repair in *repaired.
//
// RAW DIMENSIONS ARE NORMALIZED ONE AT A TIME, BEFORE THEY ARE COMBINED. A sum
// or difference computed first can hide a negative operand: output 50 plus
// reasoning -50 is a clean-looking zero that drops valid usage, and input 100
// minus cached -20 is a fresh prompt of 120 that was never sent. Repairing
// after the arithmetic sees neither.
func nonNegative(v int64, repaired *bool) int64 {
	if v < 0 {
		*repaired = true
		return 0
	}
	return v
}

// UnmarshalJSON decodes an agent CLI's token accounting, repairing any negative
// dimension as it arrives.
//
// THE DECODE IS THE BOUNDARY, so there is one answer rather than one per call
// site. These counts come from a subprocess's JSON and int64 accepts a negative
// literal without complaint; every path that reads a TokenUsage — Total, Empty,
// Add, collectStreamJSONUsage, the ledger — was written for counts, and putting
// the check in any one of them leaves the others holding a value none of them
// can represent.
//
// It does NOT return an error, and that is a deliberate limit rather than an
// oversight. An error here fails the whole message: the stream-json reader
// skips an unparseable line and the one-shot reader falls back to treating
// stdout as raw text, so rejecting malformed accounting would silently discard
// the turn's ANSWER over a defect in its bookkeeping. Whether a malformed
// record should instead fail the turn outright is the owner's call and is
// recorded as such in #242.
func (u *TokenUsage) UnmarshalJSON(data []byte) error {
	// A distinct type so the standard decoder does not find this method again.
	type rawTokenUsage TokenUsage
	var raw rawTokenUsage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*u = TokenUsage(raw)
	u.malformed = false
	u.normalize()
	return nil
}

// Malformed reports whether this record was repaired on the way in.
//
// It is the answer to "did this turn genuinely cost nothing, or did it only
// sum that way?" — a question that had no answer before, because a repaired
// record and a truly empty one are the same four zeroes. A caller that drops
// an empty record must consult this before concluding nothing happened.
func (u *TokenUsage) Malformed() bool {
	return u != nil && u.malformed
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
//
// EMPTY IS NOT THE SAME QUESTION AS "cost nothing" once a record can be
// repaired. A record clamped by normalize can be empty while the turn plainly
// ran, so a caller acting on emptiness — dropping the turn, treating it as
// unmeasured — must check Malformed too, or it silently re-creates #242 with
// the arithmetic merely moved.
//
// Its own arithmetic is left alone deliberately. Testing each dimension here
// instead would answer this question correctly and leave every OTHER reader of
// a negative count, Total and the ledger included, holding the same bad value;
// the repair belongs where the value enters.
func (u *TokenUsage) Empty() bool {
	return u.Total() == 0
}

// Add accumulates other into u and returns u, so a caller streaming several
// per-message usage records over one turn can fold them into a single total.
// A nil other contributes nothing.
//
// MALFORMEDNESS IS CONTAGIOUS, because the accumulated total is what gets
// billed when the terminal result carries no aggregate. One repaired message in
// a turn makes that turn's total a repaired number, and a total that forgot
// where it came from is exactly the silence this fix removes.
func (u *TokenUsage) Add(other *TokenUsage) *TokenUsage {
	if other == nil {
		return u
	}
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.CacheCreationInputTokens += other.CacheCreationInputTokens
	u.CacheReadInputTokens += other.CacheReadInputTokens
	if other.malformed {
		u.malformed = true
	}
	return u
}
