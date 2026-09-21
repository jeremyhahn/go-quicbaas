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
	"errors"
	"slices"
	"strings"
	"testing"
)

// measuredCredentiallessResult is the VERBATIM result object Claude Code 2.1.251
// emits when it is spawned with an empty CLAUDE_CONFIG_DIR — the SDLC module's
// own directory #218 introduced, before it has a credential of its own. Captured
// 2026-08-30 by running:
//
//	env -i PATH=… HOME=<empty> CLAUDE_CONFIG_DIR=<empty> \
//	  claude --print --output-format json --model sonnet 'say hi'
//
// It exits 1 and prints this. The fixture is the measurement; a hand-written
// approximation of it would be the SDLC module agreeing with itself about a schema
// it does not own, which is exactly the test that proves nothing.
const measuredCredentiallessResult = `{"is_error":true,"duration_api_ms":0,"num_turns":1,` +
	`"stop_reason":"stop_sequence","session_id":"17e37618-7874-4f5d-9645-dcdf927f1ac6",` +
	`"total_cost_usd":0,"usage":{"output_tokens_details":{"thinking_tokens":0},` +
	`"input_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,` +
	`"output_tokens":0,"service_tier":"standard"},"modelUsage":{},"permission_denials":[],` +
	`"terminal_reason":"api_error","subtype":"success","api_error_status":null,` +
	`"result":"Not logged in · Please run /login","type":"result","duration_ms":59}`

// TestStreamJSONMessage_ParsesMeasuredPreFirstTurnFailure is the asymmetric half
// of the evidence contract: the fields are read out of a payload this package
// did not write.
func TestStreamJSONMessage_ParsesMeasuredPreFirstTurnFailure(t *testing.T) {
	var msg streamJSONMessage
	if err := json.Unmarshal([]byte(measuredCredentiallessResult), &msg); err != nil {
		t.Fatalf("unmarshal measured CLI result: %v", err)
	}

	if msg.Type != "result" || !msg.IsError {
		t.Fatalf("fixture is not a failing result message: type=%q is_error=%v", msg.Type, msg.IsError)
	}
	if msg.TerminalReason != "api_error" {
		t.Errorf("terminal_reason = %q, want api_error", msg.TerminalReason)
	}
	if msg.NumTurns != 1 {
		t.Errorf("num_turns = %d, want 1", msg.NumTurns)
	}
	// PRESENT AND ZERO, which is the whole discriminator. A nil here would mean
	// the CLI reported nothing and the fault could not be classified at all.
	if msg.Usage == nil {
		t.Fatal("usage must be parsed as present: 'reported zero' is the evidence, " +
			"and it is indistinguishable from 'reported nothing' if this is nil")
	}
	if total := msg.Usage.Total(); total != 0 {
		t.Errorf("usage total = %d, want 0", total)
	}
	// subtype says "success" on a failing turn. Asserted so that anyone who
	// later reaches for it as a discriminator finds out here.
	if msg.Subtype != "success" {
		t.Errorf("subtype = %q, want success — the CLI really does report this", msg.Subtype)
	}
}

func TestTurnFailureEvidence_PreFirstTurn(t *testing.T) {
	tests := []struct {
		name     string
		evidence TurnFailureEvidence
		want     bool
	}{
		{
			name: "measured credential-less failure: reason and a reported-zero usage",
			evidence: TurnFailureEvidence{
				TerminalReason: "api_error",
				NumTurns:       1,
				Usage:          &TokenUsage{},
			},
			want: true,
		},
		{
			// THE ASYMMETRIC COUNTERPART. A real turn ran, the model answered,
			// and the turn then failed. Its input tokens are non-zero because
			// the system prompt alone costs some, so this is charged exactly as
			// it always was.
			name: "a turn that reached the model and then failed",
			evidence: TurnFailureEvidence{
				TerminalReason: "model_error",
				NumTurns:       3,
				Usage:          &TokenUsage{InputTokens: 4812, OutputTokens: 96},
			},
			want: false,
		},
		{
			// A turn served entirely from the prompt cache still reached the
			// model. Total() covers every dimension for exactly this reason.
			name: "a cached turn is still a turn",
			evidence: TurnFailureEvidence{
				TerminalReason: "api_error",
				Usage:          &TokenUsage{CacheReadInputTokens: 20000},
			},
			want: false,
		},
		{
			// The conservative direction: no accounting at all is not evidence
			// that nothing happened.
			name: "no usage reported at all",
			evidence: TurnFailureEvidence{
				TerminalReason: "api_error",
				NumTurns:       1,
			},
			want: false,
		},
		{
			// A reason is required as well: zero usage with nothing to call the
			// fault by would park an item under a class nobody can act on.
			name: "zero usage but no terminal reason",
			evidence: TurnFailureEvidence{
				NumTurns: 1,
				Usage:    &TokenUsage{},
			},
			want: false,
		},
		{
			// A REASON THIS BUILD DOES NOT KNOW IS CHARGED. It used to be
			// refunded on the strength of the zero usage alone, which is the
			// wrong direction to be wrong in: a newer CLI could quietly widen
			// what the SDLC module stops charging for. The class is still surfaced
			// so an operator sees the name.
			name: "zero usage and a reason the allowlist does not carry",
			evidence: TurnFailureEvidence{
				TerminalReason: "quota_wedged",
				NumTurns:       1,
				Usage:          &TokenUsage{},
			},
			want: false,
		},

		// ===== Malformed accounting is not evidence =====
		//
		// Every case below sums to zero or looks zero-ish under Total(), which
		// is what this used to test. Each would have earned a refund and a
		// configuration park on numbers that cannot be true.
		{
			name: "a negative dimension cancelling a positive one",
			evidence: TurnFailureEvidence{
				TerminalReason: "api_error",
				NumTurns:       1,
				Usage:          &TokenUsage{InputTokens: 1, OutputTokens: -1},
			},
			want: false,
		},
		{
			name: "a single negative dimension",
			evidence: TurnFailureEvidence{
				TerminalReason: "api_error",
				Usage:          &TokenUsage{OutputTokens: -5},
			},
			want: false,
		},
		{
			name: "negatives cancelling across the cache dimensions",
			evidence: TurnFailureEvidence{
				TerminalReason: "api_error",
				Usage: &TokenUsage{
					CacheReadInputTokens:     9223372036854775807,
					CacheCreationInputTokens: -9223372036854775807,
				},
			},
			want: false,
		},
		{
			// The overflow boundary FROM THE OTHER SIDE: two int64 maxima sum
			// to -2 in wrapped arithmetic, so Total() is not merely wrong here,
			// it is negative. Per-dimension testing does not care.
			name: "two maxima that wrap the sum negative",
			evidence: TurnFailureEvidence{
				TerminalReason: "api_error",
				Usage: &TokenUsage{
					InputTokens:  9223372036854775807,
					OutputTokens: 9223372036854775807,
				},
			},
			want: false,
		},
		{
			name: "the largest representable count is still not zero",
			evidence: TurnFailureEvidence{
				TerminalReason: "api_error",
				Usage:          &TokenUsage{InputTokens: 9223372036854775807},
			},
			want: false,
		},
		{
			// The genuine article, stated last: every dimension exactly zero.
			name: "a genuine all-zero record",
			evidence: TurnFailureEvidence{
				TerminalReason: "api_error",
				NumTurns:       1,
				Usage: &TokenUsage{
					InputTokens:              0,
					OutputTokens:             0,
					CacheCreationInputTokens: 0,
					CacheReadInputTokens:     0,
				},
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.evidence.PreFirstTurn(); got != tc.want {
				t.Errorf("PreFirstTurn() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTurnFailureEvidence_ReasonClass(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		want   string
	}{
		{"a CLI enum member", "api_error", TerminalReasonAPIError},
		{"another the CLI declares", "turn_setup_failed", TerminalReasonTurnSetupFailed},
		{"one from the extended list", "budget_exhausted", TerminalReasonBudgetExhausted},
		{"none reported", "", ""},

		// EVERY VALUE BELOW SATISFIED THE REGEXP THIS ALLOWLIST REPLACED
		// (`^[a-z][a-z0-9_]{0,63}$`), which is the whole of the hosted
		// review's objection: a shape test on a subprocess-supplied field
		// protects nothing against content chosen to pass it, and these
		// would have been rendered into the daemon log, the finish record
		// and a PUBLIC forge comment — escaping the redaction applied to the
		// result text one field over.
		{"a secret-shaped lowercase token", "sk_live_9f2a1c4e8b7d0356", TerminalReasonUnclassified},
		{"a bare hex credential fragment", "a3f9b2c1d4e5f6a7b8c9d0e1f2a3b4c5", TerminalReasonUnclassified},
		{"an oauth-ish refresh fragment", "rt_01hqz9m4k7n2p8v3w6x1y5", TerminalReasonUnclassified},
		{"a lowercase basic-auth pair", "root_hunter2_prod", TerminalReasonUnclassified},

		// And an ordinary unknown: a reason a future CLI adds. Same answer,
		// which is the point — the class is named, the bytes are not.
		{"a reason this build has never heard of", "quota_wedged", TerminalReasonUnclassified},

		// Shapes the old regexp already rejected, kept so the allowlist is
		// not a regression on them.
		{"a sentence", "Not logged in · Please run /login", TerminalReasonUnclassified},
		{"a bearer token", "Bearer sk-ant-0123456789abcdef", TerminalReasonUnclassified},
		{"a header", "authorization: token abc123", TerminalReasonUnclassified},
		{"an over-long blob", strings.Repeat("a", 200), TerminalReasonUnclassified},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := TurnFailureEvidence{TerminalReason: tc.reason}.ReasonClass()
			if got != tc.want {
				t.Errorf("ReasonClass() = %q, want %q", got, tc.want)
			}
			// THE INVARIANT UNDERNEATH ALL OF IT: whatever comes back is a
			// string this package owns, never the one that arrived. Asserted
			// for every case rather than only the adversarial ones, so a
			// future "just pass it through when it looks fine" cannot slip in.
			if got != "" && got != TerminalReasonUnclassified {
				if _, known := knownTerminalReasons[got]; !known {
					t.Errorf("ReasonClass() returned %q, which is not a value from the "+
						"allowlist: only compiled-in constants may be rendered", got)
				}
			}
		})
	}
}

// TestReasonClass_RendersOnlyCompiledConstants is the property stated directly:
// no input, of any shape, can make ReasonClass return its own argument unless
// that argument is one of the measured enum members.
func TestReasonClass_RendersOnlyCompiledConstants(t *testing.T) {
	adversarial := []string{
		"sk_live_9f2a1c4e8b7d0356",
		"ghp_0123456789abcdefghijklmnopqrstuvwxyz",
		"a3f9b2c1d4e5f6a7",
		"password_is_correcthorsebatterystaple",
		"eyjhbgcioijiuzi1niisinr5cci6ikpxvcj9",
		"x",
		"api_error_but_not_really",
	}
	for _, raw := range adversarial {
		t.Run(raw, func(t *testing.T) {
			got := TurnFailureEvidence{TerminalReason: raw}.ReasonClass()
			if got == raw {
				t.Fatalf("ReasonClass echoed its subprocess-supplied argument %q", raw)
			}
			if got != TerminalReasonUnclassified {
				t.Errorf("ReasonClass() = %q, want %q", got, TerminalReasonUnclassified)
			}
		})
	}
}

// TestKnownTerminalReasons_MatchesTheMeasuredCLIEnum pins the allowlist to the
// binary it was measured from.
//
// The list is Claude Code 2.1.251's `terminal_reason` enum — the union of the
// two internal arrays its result schema draws from — read out of the shipped
// binary rather than from documentation. This test is what makes a later
// trimming or typo visible: the allowlist is a security boundary, and a
// boundary nobody checks against its source drifts.
func TestKnownTerminalReasons_MatchesTheMeasuredCLIEnum(t *testing.T) {
	measured := []string{
		// k3, in the CLI's own order.
		"blocking_limit", "rapid_refill_breaker", "prompt_too_long", "image_error",
		"model_error", "api_error", "malformed_tool_use_exhausted", "aborted_streaming",
		"aborted_tools", "stop_hook_prevented", "hook_stopped", "tool_deferred",
		"max_turns", "background_requested", "completed",
		// w3.
		"budget_exhausted", "structured_output_retry_exhausted",
		"tool_deferred_unavailable", "turn_setup_failed",
	}

	if len(knownTerminalReasons) != len(measured) {
		t.Errorf("allowlist holds %d reasons, the measured CLI enum holds %d",
			len(knownTerminalReasons), len(measured))
	}
	for _, want := range measured {
		class, known := knownTerminalReasons[want]
		if !known {
			t.Errorf("the CLI declares %q and the allowlist does not carry it; a reason "+
				"missing here parks as unclassified and the operator loses the name", want)
			continue
		}
		if class != want {
			t.Errorf("allowlist maps %q to %q; the rendered constant must name the same reason",
				want, class)
		}
	}
	for key := range knownTerminalReasons {
		if !slices.Contains(measured, key) {
			t.Errorf("the allowlist carries %q, which the measured CLI enum does not declare", key)
		}
	}
	if _, present := knownTerminalReasons[TerminalReasonUnclassified]; present {
		t.Error("unclassified is this package's own answer, never a value the CLI emits")
	}
}

// TestStreamJSONError_RendersClassWithholdsResult is #235's third defect as a
// test: the class a human needs is rendered, the payload a credential would be
// in is not.
func TestStreamJSONError_RendersClassWithholdsResult(t *testing.T) {
	err := &StreamJSONError{
		Command: "claude",
		// A secret-shaped string in the same payload, per the issue.
		Result: "auth failed for Bearer sk-ant-api03-0123456789abcdefghij",
		TurnFailureEvidence: TurnFailureEvidence{
			TerminalReason: "api_error",
			NumTurns:       1,
			Usage:          &TokenUsage{},
		},
	}

	msg := err.Error()
	if !strings.Contains(msg, "terminal_reason=api_error") {
		t.Errorf("the reason class must be rendered — withholding it is what left an "+
			"operator reading %q for an authentication failure; got: %s",
			"(N bytes of result withheld)", msg)
	}
	if strings.Contains(msg, "sk-ant-api03") || strings.Contains(msg, "auth failed for") {
		t.Errorf("the result payload must stay withheld, got: %s", msg)
	}
	if !strings.Contains(msg, "bytes of result withheld") {
		t.Errorf("the withheld byte count must still be reported, got: %s", msg)
	}
}

// TestStreamJSONError_RendersUnclassifiedRatherThanTheWireValue is the
// asymmetric half of the rendering contract: the error string names the fault
// without ever carrying what the subprocess wrote.
func TestStreamJSONError_RendersUnclassifiedRatherThanTheWireValue(t *testing.T) {
	err := &StreamJSONError{
		Command:             "claude",
		TurnFailureEvidence: TurnFailureEvidence{TerminalReason: "token_sk_ant_api03_secret"},
	}
	msg := err.Error()
	if strings.Contains(msg, "token_sk_ant_api03_secret") {
		t.Errorf("the error rendered the subprocess's own bytes, got: %s", msg)
	}
	if !strings.Contains(msg, "terminal_reason="+TerminalReasonUnclassified) {
		t.Errorf("an unrecognized reason must still be named, as unclassified; got: %s", msg)
	}
}

func TestPreFirstTurnFault(t *testing.T) {
	preTurn := TurnFailureEvidence{
		TerminalReason: "api_error",
		NumTurns:       1,
		Usage:          &TokenUsage{},
	}
	realTurn := TurnFailureEvidence{
		TerminalReason: "model_error",
		NumTurns:       2,
		Usage:          &TokenUsage{InputTokens: 3000},
	}

	tests := []struct {
		name      string
		err       error
		wantClass string
		wantOK    bool
	}{
		{
			// Wrapped exactly as the persistent path produces it, so the test
			// exercises the chain the SDLC module actually sees.
			name: "stream-json pre-first-turn, wrapped in a PromptError",
			err: &PromptError{SessionID: "s-1", Cause: &StreamJSONError{
				Command: "claude", Result: "Not logged in", TurnFailureEvidence: preTurn,
			}},
			wantClass: "api_error",
			wantOK:    true,
		},
		{
			name: "one-shot pre-first-turn",
			err: &PromptError{SessionID: "s-2", Cause: &OneShotExecutionError{
				Command: "claude", Stderr: "Not logged in", TurnFailureEvidence: preTurn,
			}},
			wantClass: "api_error",
			wantOK:    true,
		},
		{
			name: "a turn that ran and then failed is not a fault",
			err: &PromptError{SessionID: "s-3", Cause: &StreamJSONError{
				Command: "claude", TurnFailureEvidence: realTurn,
			}},
			wantOK: false,
		},
		{
			// The pre-#235 error, carrying no evidence at all. It must answer
			// exactly as it did before: an ordinary failure.
			name:   "an error with no evidence on it",
			err:    &PromptError{SessionID: "s-4", Cause: &StreamJSONError{Command: "claude"}},
			wantOK: false,
		},
		{
			name:   "an unrelated error",
			err:    errors.New("something else entirely"),
			wantOK: false,
		},
		{
			// A reason the allowlist does not carry is CHARGED. It used to be
			// refunded on the zero usage alone, which let a newer CLI quietly
			// widen what bypasses the attempt budget.
			name: "zero usage under a reason the allowlist does not carry",
			err: &PromptError{SessionID: "s-5", Cause: &StreamJSONError{
				Command: "claude",
				TurnFailureEvidence: TurnFailureEvidence{
					TerminalReason: "sk_live_9f2a1c4e8b7d0356",
					Usage:          &TokenUsage{},
				},
			}},
			wantOK: false,
		},
		{
			// A work-derived reason with zero usage: also charged, because
			// the same work fails the same way on every retry.
			name: "a prompt too long for the model",
			err: &PromptError{SessionID: "s-7", Cause: &StreamJSONError{
				Command: "claude",
				TurnFailureEvidence: TurnFailureEvidence{
					TerminalReason: "prompt_too_long",
					Usage:          &TokenUsage{},
				},
			}},
			wantOK: false,
		},
		{
			name: "malformed accounting is an ordinary failure",
			err: &PromptError{SessionID: "s-6", Cause: &StreamJSONError{
				Command: "claude",
				TurnFailureEvidence: TurnFailureEvidence{
					TerminalReason: "api_error",
					Usage:          &TokenUsage{InputTokens: 1, OutputTokens: -1},
				},
			}},
			wantOK: false,
		},
		{
			name:   "nil",
			err:    nil,
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			class, ok := PreFirstTurnFault(tc.err)
			if ok && class == "" {
				t.Fatal("a classified fault must name itself: an empty class reads as " +
					"'not a fault' to every caller")
			}
			if ok && class != TerminalReasonUnclassified {
				if _, known := knownTerminalReasons[class]; !known {
					t.Fatalf("PreFirstTurnFault returned %q, which is not a compiled-in "+
						"constant; only the allowlist's values may leave this package", class)
				}
			}
			if ok != tc.wantOK {
				t.Fatalf("PreFirstTurnFault ok = %v, want %v", ok, tc.wantOK)
			}
			if class != tc.wantClass {
				t.Errorf("PreFirstTurnFault class = %q, want %q", class, tc.wantClass)
			}
		})
	}
}

// TestPreFirstTurn_OnlyEnvironmentalReasonsAreRefunded is go-quicbaas#235's
// round-5 blocker: "no model turn ran" is not sufficient on its own.
//
// Several reasons consume no input tokens and are DETERMINISTIC properties of
// the work. Refunding one means the item parks, a human re-applies the trigger
// label, it dispatches into the identical failure, and is refunded again — the
// attempt budget, which exists precisely to stop that loop, bypassed by
// relabelling.
//
// Every reason the measured CLI enum declares is listed, with why it falls the
// way it does, so the set is auditable rather than a list somebody trimmed.
func TestPreFirstTurn_OnlyEnvironmentalReasonsAreRefunded(t *testing.T) {
	tests := []struct {
		reason string
		refund bool
		why    string
	}{
		// Environmental: a different environment would have changed the answer.
		{"api_error", true, "the request never reached the API — #235's measured credential-less case"},
		{"turn_setup_failed", true, "the CLI could not set the turn up at all"},

		// Work-derived: the same work will fail the same way on every retry.
		// The first three are the CLI's own context_limit group; the SDLC
		// runner rolls the SESSION on them, and nothing refunds them.
		{"prompt_too_long", false, "the session's context is full"},
		{"blocking_limit", false, "the session's context is full; the CLI files it under context_limit"},
		{"rapid_refill_breaker", false, "the session's context is full; the CLI files it under context_limit"},
		{"image_error", false, "an image in this item's input"},
		{"stop_hook_prevented", false, "a hook stopped it; policy, not environment"},
		{"hook_stopped", false, "same"},
		{"max_turns", false, "the turn budget for this work was spent"},
		{"budget_exhausted", false, "the CLI's own budget for this work"},
		{"model_error", false, "the model errored on this input"},
		{"malformed_tool_use_exhausted", false, "the model could not produce usable tool calls"},
		{"structured_output_retry_exhausted", false, "this item's output schema was never satisfied"},
		{"tool_deferred", false, "a tool this work asked for was deferred"},
		{"tool_deferred_unavailable", false, "same"},
		{"aborted_streaming", false, "somebody or something aborted this run"},
		{"aborted_tools", false, "same"},
		{"background_requested", false, "the run was moved to the background, not failed by the SDLC module"},
		{"completed", false, "not a failure at all"},

		// Unknown: charged, conservatively.
		{"quota_wedged", false, "a reason this build has never heard of"},
		{"", false, "no reason reported"},
	}

	// Zero usage throughout: the OTHER half of the test is already true for
	// every row, so the reason is the only thing deciding the outcome.
	for _, tc := range tests {
		t.Run(tc.reason, func(t *testing.T) {
			got := TurnFailureEvidence{
				TerminalReason: tc.reason, NumTurns: 1, Usage: &TokenUsage{},
			}.PreFirstTurn()
			if got != tc.refund {
				t.Errorf("PreFirstTurn() = %v, want %v — %s", got, tc.refund, tc.why)
			}
		})
	}

	// Every environmental reason must be one the CLI actually declares, or the
	// set refunds on a string nothing can send.
	for reason := range environmentalTerminalReasons {
		if _, declared := knownTerminalReasons[reason]; !declared {
			t.Errorf("%q is refunded but is not a reason the measured CLI enum declares", reason)
		}
	}
	// And the two sets together must not have grown a member the table above
	// does not judge.
	judged := make(map[string]bool, len(tests))
	for _, tc := range tests {
		judged[tc.reason] = true
	}
	for reason := range knownTerminalReasons {
		if !judged[reason] {
			t.Errorf("the CLI declares %q and this test does not say whether it is "+
				"refunded; every reason must be judged deliberately", reason)
		}
	}
}

// TestPreFirstTurn_AnEnvironmentalReasonStillNeedsZeroUsage is the other axis:
// splitting the reasons did not make the usage evidence optional.
func TestPreFirstTurn_AnEnvironmentalReasonStillNeedsZeroUsage(t *testing.T) {
	if (TurnFailureEvidence{
		TerminalReason: "api_error",
		Usage:          &TokenUsage{InputTokens: 4812},
	}).PreFirstTurn() {
		t.Error("a turn that reached the model was refunded because its reason was environmental")
	}
	if (TurnFailureEvidence{TerminalReason: "api_error"}).PreFirstTurn() {
		t.Error("a turn that reported no accounting at all was refunded")
	}
}

// TestContextExhaustionOf_NamesTheThreeContextReasons pins the set the SDLC
// runner rolls a session on: exactly the reasons the measured CLI routes to
// context_limit, carried on any of the three evidence types, and nothing else.
func TestContextExhaustionOf_NamesTheThreeContextReasons(t *testing.T) {
	for _, reason := range []string{"blocking_limit", "prompt_too_long", "rapid_refill_breaker"} {
		err := &RemoteTurnFailureError{Op: "Prompt", TurnFailureEvidence: TurnFailureEvidence{TerminalReason: reason}, Err: errors.New("x")}
		class, ok := ContextExhaustionOf(err)
		if !ok || class != reason {
			t.Errorf("%s: ContextExhaustionOf = (%q, %v), want (%q, true)", reason, class, ok, reason)
		}
		if !IsContextExhaustedTerminalReason(reason) {
			t.Errorf("%s: not reported as context exhaustion", reason)
		}
		if IsEnvironmentalTerminalReason(reason) {
			t.Errorf("%s: a full context is the session's, not the environment's; it must not refund", reason)
		}
	}
	for _, reason := range []string{"api_error", "max_turns", "completed", "quota_wedged", ""} {
		err := &StreamJSONError{Command: "claude", TurnFailureEvidence: TurnFailureEvidence{TerminalReason: reason}}
		if class, ok := ContextExhaustionOf(err); ok {
			t.Errorf("%q: reported as context exhaustion (%q)", reason, class)
		}
	}
	if _, ok := ContextExhaustionOf(errors.New("no evidence at all")); ok {
		t.Error("an error carrying no evidence was reported as context exhaustion")
	}
	for reason := range contextExhaustedTerminalReasons {
		if _, declared := knownTerminalReasons[reason]; !declared {
			t.Errorf("%q rolls a session but is not a reason the measured CLI enum declares", reason)
		}
	}
}
