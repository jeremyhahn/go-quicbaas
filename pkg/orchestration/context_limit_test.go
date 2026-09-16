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
	"math"
	"strings"
	"sync"
	"testing"
)

// TestContextLimit_UnknownAllowsEverything: a limit that refused what it had
// not measured would turn "nothing learned yet" into an outage.
func TestContextLimit_UnknownAllowsEverything(t *testing.T) {
	var zero ContextLimit
	for _, n := range []int{1, 1024, 1 << 20} {
		if !zero.Allows(n) {
			t.Fatalf("the zero value refused %d tokens; it knows no limit", n)
		}
	}
	if zero.Bound() != 0 {
		t.Fatalf("Bound() = %d, want 0 when nothing is known", zero.Bound())
	}
}

// TestContextLimit_ADeclarationHoldsBeforeAnyRequest is the rung that makes
// this portable: an operator on any server states their number and it is in
// force immediately, with no probe and no vendor knowledge.
func TestContextLimit_ADeclarationHoldsBeforeAnyRequest(t *testing.T) {
	limit := NewContextLimit(4096)
	if limit.Bound() != 4096 {
		t.Fatalf("Bound() = %d, want the declaration", limit.Bound())
	}
	if !limit.Allows(4095) {
		t.Fatal("a prompt inside the declared bound was refused")
	}
	if limit.Allows(4096) || limit.Allows(9000) {
		t.Fatal("a prompt at or over the declared bound was allowed")
	}
}

// TestContextLimit_ARefusalNarrowsAndOnlyNarrows pins the ratchet: the
// smallest refusal is the tightest evidence, and a later larger refusal says
// nothing new. Letting the bound rise would let one anomaly widen the ceiling.
func TestContextLimit_ARefusalNarrowsAndOnlyNarrows(t *testing.T) {
	limit := NewContextLimit(0)

	if !limit.Refused(8000) {
		t.Fatal("the first refusal taught nothing")
	}
	if limit.Bound() != 8000 {
		t.Fatalf("Bound() = %d, want the refused size", limit.Bound())
	}
	if limit.Refused(9000) {
		t.Fatal("a larger refusal was treated as new information")
	}
	if limit.Bound() != 8000 {
		t.Fatalf("Bound() = %d, want the bound to hold against a larger refusal", limit.Bound())
	}
	if !limit.Refused(2000) {
		t.Fatal("a smaller refusal did not narrow the bound")
	}
	if limit.Bound() != 2000 {
		t.Fatalf("Bound() = %d, want the tightest refusal", limit.Bound())
	}
}

// TestContextLimit_TheServerOutranksTheOperator: an operator who declares 32768
// against a server compiled for 1024 is wrong, and trusting them would refuse
// nothing and fail every request instead.
func TestContextLimit_TheServerOutranksTheOperator(t *testing.T) {
	limit := NewContextLimit(32768)
	limit.Refused(1024)
	if limit.Bound() != 1024 {
		t.Fatalf("Bound() = %d, want the server's refusal to win over a looser declaration", limit.Bound())
	}
	// The reverse must NOT hold: a refusal larger than the declaration leaves
	// the tighter declared bound in force.
	other := NewContextLimit(2048)
	other.Refused(8192)
	if other.Bound() != 2048 {
		t.Fatalf("Bound() = %d, want the tighter declaration to hold", other.Bound())
	}
}

// TestContextLimit_RefusedIsRaceFree runs the ratchet concurrently, because a
// backend serves many sessions and the bound is read on every request.
func TestContextLimit_RefusedIsRaceFree(t *testing.T) {
	limit := NewContextLimit(0)
	var wg sync.WaitGroup
	for i := 1; i <= 64; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			limit.Refused(n * 100)
			_ = limit.Allows(n)
		}(i)
	}
	wg.Wait()
	if got := limit.Bound(); got != 100 {
		t.Fatalf("Bound() = %d, want the smallest refusal (100)", got)
	}
}

// TestIsLengthRefusal_RecognisesServersWithoutKnowingThem is the portability
// claim under test: the phrasings below come from different servers, and none
// of them is matched by name.
func TestIsLengthRefusal_RecognisesServersWithoutKnowingThem(t *testing.T) {
	tooLong := []struct {
		name   string
		status int
		body   string
	}{
		{"OVMS mediapipe", 400, `{"error":"Mediapipe execution failed. MP status - INVALID_ARGUMENT: CalculatorGraph::Run() failed: Calculator::Process() for node \"LLMExecutor\" failed: Input length exceeds the maximum allowed length"}`},
		{"vLLM style", 400, `{"object":"error","message":"This model's maximum context length is 4096 tokens. However, you requested 5000 tokens."}`},
		{"llama.cpp style", 400, `{"error":{"message":"the request exceeds the available context size"}}`},
		{"TGI style", 422, `{"error":"Input validation error: inputs must have less than 1024 tokens"}`},
		{"payload too large", 413, `request entity too large: prompt exceeds maximum size`},
	}
	for _, c := range tooLong {
		t.Run(c.name, func(t *testing.T) {
			if !IsLengthRefusal(c.status, c.body) {
				t.Fatalf("not recognised as a length refusal: %s", c.body)
			}
		})
	}
}

// TestIsLengthRefusal_LeavesOtherFailuresAlone is the other half, and the more
// important one: narrowing the ceiling on an unrelated failure would teach it a
// number from an outage and refuse healthy prompts for ever after.
func TestIsLengthRefusal_LeavesOtherFailuresAlone(t *testing.T) {
	notTooLong := []struct {
		name   string
		status int
		body   string
	}{
		{"a server fault", 500, `{"error":"the model exceeded its maximum input length"}`},
		{"an auth failure", 400, `{"error":"invalid token"}`},
		{"a quota failure", 400, `{"error":"you have exceeded your quota"}`},
		{"a missing model", 404, `{"error":"model not found"}`},
		{"a bad request with no verdict", 400, `{"error":"messages is required"}`},
		{"success", 200, `{"usage":{"prompt_tokens":10}}`},
	}
	for _, c := range notTooLong {
		t.Run(c.name, func(t *testing.T) {
			if IsLengthRefusal(c.status, c.body) {
				t.Fatalf("wrongly read as a length refusal: %s", c.body)
			}
		})
	}
}

// TestEstimateTokens_IsComparableWithItself. The estimate need not match a
// tokenizer — it must only order the same way, because the size that was
// refused and the size about to be sent are produced by this same function.
func TestEstimateTokens_IsComparableWithItself(t *testing.T) {
	if EstimateTokens("") != 0 {
		t.Fatal("the empty string is not zero tokens")
	}
	if EstimateTokens("a") < 1 {
		t.Fatal("a non-empty string estimated at zero tokens would never be refused")
	}
	small, large := EstimateTokens(strings.Repeat("word ", 10)), EstimateTokens(strings.Repeat("word ", 1000))
	if small >= large {
		t.Fatalf("estimates do not order by size: %d >= %d", small, large)
	}
}

// TestContextLimit_CalibrateRelatesTheEstimateToRealTokens: the bound is
// learned in estimated tokens but an operator declares real ones, and every
// OpenAI-compatible server reports usage.prompt_tokens on success.
func TestContextLimit_CalibrateRelatesTheEstimateToRealTokens(t *testing.T) {
	limit := NewContextLimit(0)
	if limit.Ratio() != 0 {
		t.Fatal("a ratio was claimed before anything was observed")
	}
	limit.Calibrate(1000, 800)
	if math.Abs(limit.Ratio()-0.8) > 1e-9 {
		t.Fatalf("Ratio() = %v, want 0.8 after one observation", limit.Ratio())
	}
	// A second observation averages rather than replaces, so one pasted binary
	// cannot move the ratio far.
	limit.Calibrate(1000, 1000)
	if math.Abs(limit.Ratio()-0.9) > 1e-9 {
		t.Fatalf("Ratio() = %v, want the running mean 0.9", limit.Ratio())
	}
	// A response that carried no count teaches nothing and is not an error.
	limit.Calibrate(1000, 0)
	if math.Abs(limit.Ratio()-0.9) > 1e-9 {
		t.Fatalf("Ratio() = %v, want an absent count to be ignored", limit.Ratio())
	}
}

// TestPromptTooLongError_NamesBothSizes: "too long" without them tells an
// operator neither whether to raise the server limit nor by how much.
func TestPromptTooLongError_NamesBothSizes(t *testing.T) {
	err := &PromptTooLongError{Backend: "openvino", Model: "qwen3-8b", Estimated: 1400, Bound: 1024}
	msg := err.Error()
	for _, want := range []string{"openvino", "qwen3-8b", "1400", "1024"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the error does not name %q: %s", want, msg)
		}
	}
	if !errors.Is(err, ErrPromptTooLong) {
		t.Fatal("the error does not unwrap to its sentinel, so a caller cannot tell it from a transient fault")
	}
}

// TestContextLimit_ADeclarationIsConvertedNotCompared pins the unit rule. A
// learned bound is in estimate units and a declaration is in real tokens; a
// prompt's real token count cannot be known before sending it, so the
// declaration is divided by the observed ratio to reach the same unit.
// Comparing them directly would mean two different things by one number.
func TestContextLimit_ADeclarationIsConvertedNotCompared(t *testing.T) {
	// With no ratio observed, the prior is 1:1 and the declaration stands as
	// given.
	limit := NewContextLimit(1000)
	if got := limit.Bound(); got != 1000 {
		t.Fatalf("Bound() = %d, want the declaration at the default ratio", got)
	}

	// A server that counts 0.5 real tokens per estimated token means a
	// 1000-real-token budget is 2000 estimated tokens' worth of prompt.
	limit.Calibrate(1000, 500)
	if got := limit.Bound(); got != 2000 {
		t.Fatalf("Bound() = %d, want the declaration converted into estimate units", got)
	}
	if !limit.Allows(1999) || limit.Allows(2000) {
		t.Fatalf("the converted bound is not enforced: allows(1999)=%v allows(2000)=%v",
			limit.Allows(1999), limit.Allows(2000))
	}

	// A learned refusal tighter than the converted declaration still wins.
	limit.Refused(800)
	if got := limit.Bound(); got != 800 {
		t.Fatalf("Bound() = %d, want the tighter learned bound", got)
	}
}
