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
	"encoding/json"
	"testing"
)

func TestTokenUsage_TotalSumsEveryDimension(t *testing.T) {
	usage := &TokenUsage{
		InputTokens:              10,
		OutputTokens:             20,
		CacheCreationInputTokens: 30,
		CacheReadInputTokens:     40,
	}
	if got := usage.Total(); got != 100 {
		t.Fatalf("Total() = %d, want 100 — cache tokens are billed and must not be dropped from the total", got)
	}
	if usage.Empty() {
		t.Fatal("Empty() = true for a turn that reported usage")
	}
}

func TestTokenUsage_NilAndZeroAreEmpty(t *testing.T) {
	var nilUsage *TokenUsage
	if got := nilUsage.Total(); got != 0 {
		t.Fatalf("(*TokenUsage)(nil).Total() = %d, want 0", got)
	}
	if !nilUsage.Empty() {
		t.Fatal("(*TokenUsage)(nil).Empty() = false, want true")
	}
	if !(&TokenUsage{}).Empty() {
		t.Fatal("zero TokenUsage must report Empty so an unmeasured turn is never recorded")
	}
}

func TestTokenUsage_AddAccumulates(t *testing.T) {
	total := &TokenUsage{}
	total.Add(&TokenUsage{InputTokens: 5, OutputTokens: 7})
	total.Add(&TokenUsage{InputTokens: 1, CacheReadInputTokens: 2})

	if total.InputTokens != 6 || total.OutputTokens != 7 || total.CacheReadInputTokens != 2 {
		t.Fatalf("Add() = %+v, want input=6 output=7 cacheRead=2", total)
	}
	if got := total.Total(); got != 15 {
		t.Fatalf("Total() = %d, want 15", got)
	}
}

func TestTokenUsage_AddNilContributesNothing(t *testing.T) {
	total := &TokenUsage{InputTokens: 3}
	if got := total.Add(nil); got != total {
		t.Fatal("Add(nil) must return the receiver so calls can be chained")
	}
	if total.Total() != 3 {
		t.Fatalf("Add(nil) changed the total to %d, want 3", total.Total())
	}
}

// TestTokenUsage_DecodesClaudeCodeShape pins the wire field names to the ones
// the Claude Code stream-json dialect actually emits. A rename here silently
// zeroes every recorded turn, which is exactly the failure this feature exists
// to prevent, so it is asserted against a literal payload rather than a struct
// round trip.
func TestTokenUsage_DecodesClaudeCodeShape(t *testing.T) {
	const payload = `{"input_tokens":4,"cache_creation_input_tokens":11,"cache_read_input_tokens":22,"output_tokens":33}`

	var usage TokenUsage
	if err := json.Unmarshal([]byte(payload), &usage); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if usage.InputTokens != 4 || usage.CacheCreationInputTokens != 11 ||
		usage.CacheReadInputTokens != 22 || usage.OutputTokens != 33 {
		t.Fatalf("decoded %+v, want the four counts from the payload", usage)
	}
	if got := usage.Total(); got != 70 {
		t.Fatalf("Total() = %d, want 70", got)
	}
}

func TestTokenUsage_RejectsMalformedPayload(t *testing.T) {
	var usage TokenUsage
	if err := json.Unmarshal([]byte(`{"input_tokens":"not-a-number"}`), &usage); err == nil {
		t.Fatal("Unmarshal accepted a non-numeric token count; a malformed usage record must be an error, not silently zero")
	}
}

func TestCollectStreamJSONUsage_PrefersResultAggregate(t *testing.T) {
	result := &TokenUsage{InputTokens: 100, OutputTokens: 50}
	accumulated := &TokenUsage{InputTokens: 100, OutputTokens: 50}

	got := collectStreamJSONUsage(result, accumulated)
	if got != result {
		t.Fatalf("collectStreamJSONUsage() = %+v, want the result aggregate — summing both would bill the turn twice", got)
	}
}

func TestCollectStreamJSONUsage_FallsBackToAccumulated(t *testing.T) {
	accumulated := &TokenUsage{InputTokens: 3, OutputTokens: 4}

	if got := collectStreamJSONUsage(nil, accumulated); got != accumulated {
		t.Fatalf("collectStreamJSONUsage(nil, acc) = %+v, want the accumulated per-message total", got)
	}
	if got := collectStreamJSONUsage(&TokenUsage{}, accumulated); got != accumulated {
		t.Fatalf("collectStreamJSONUsage(zero, acc) = %+v, want the accumulated per-message total", got)
	}
}

func TestCollectStreamJSONUsage_UnmeasuredTurnYieldsNil(t *testing.T) {
	if got := collectStreamJSONUsage(nil, nil); got != nil {
		t.Fatalf("collectStreamJSONUsage(nil, nil) = %+v, want nil — an unmeasured turn must stay distinguishable from a zero one", got)
	}
	if got := collectStreamJSONUsage(&TokenUsage{}, &TokenUsage{}); got != nil {
		t.Fatalf("collectStreamJSONUsage(zero, zero) = %+v, want nil", got)
	}
}
