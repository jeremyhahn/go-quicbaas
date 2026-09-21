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
	"sort"
)

// =============================================================================
// What the CLI's own result object says about a failed turn
// =============================================================================
//
// A TURN THAT NEVER REACHED THE MODEL IS NOT A FAILED ATTEMPT AT THE WORK, AND
// THE CLI SAYS SO IN STRUCTURED FIELDS.
//
// Measured 2026-08-30 against Claude Code 2.1.251, spawned with an empty
// CLAUDE_CONFIG_DIR — the SDLC module's own directory #218 introduced, before it
// has a credential of its own:
//
//	{"type":"result","subtype":"success","is_error":true,"num_turns":1,
//	 "terminal_reason":"api_error","result":"Not logged in · Please run /login",
//	 "usage":{"input_tokens":0,"output_tokens":0,
//	          "cache_creation_input_tokens":0,"cache_read_input_tokens":0}}
//
// Three things about that record are worth stating, because each one shaped
// what this file does and does not read:
//
//   - `subtype` says "success" on a failing turn. It is useless as a
//     discriminator and is deliberately not consulted.
//   - `num_turns` is 1 even though nothing was sent to the model. It is carried
//     for the diagnosis and is NOT the test either.
//   - `usage` is PRESENT and every dimension is zero. That is the discriminator:
//     a turn that reached the model spends input tokens on the system prompt
//     alone, so a reported-and-zero usage record means the request never left
//     the CLI.
//
// THE EVIDENCE IS STRUCTURED, NEVER PROSE. Nothing here parses the `result`
// text, matches on an error message, or guesses from a stderr tail — the
// objection raised against #218/#234 was precisely that a fault class inferred
// from free text is a guess, and a guess that decides whether an item spends
// its attempt budget is a guess that costs money. Every field consulted below
// is a typed field of the CLI's own documented result schema.

// =============================================================================
// What may be rendered
// =============================================================================
//
// `terminal_reason` ARRIVES FROM A SUBPROCESS, SO IT IS NOT TRUSTED TEXT — it
// is a value read off a JSON document some other program wrote, and this
// package renders it into error strings that reach the daemon log, the SDLC
// finish record, and a public comment on a forge issue.
//
// This was first written as a SHAPE test — `^[a-z][a-z0-9_]{0,63}$` — on the
// reasoning that the CLI's vocabulary is lowercase identifiers and a shape
// check would still surface a reason the CLI adds tomorrow. Hosted review of
// #235 rejected that, correctly: a great many credential fragments are also
// lowercase identifiers under 64 characters. `sk_live_9f2a1c` passes that
// regexp. So does a bare hex token. A field whose CONTENT decides whether it is
// rendered has no protection at all against content chosen to pass the test,
// and it would have escaped the redaction deliberately applied one field over
// on Result/Stderr.
//
// SO THE RENDERED BYTES ARE COMPILED INTO THIS FILE. The wire value is used
// only as a map KEY, for exact equality against the enum measured below; what
// is rendered is the constant that lookup returns. Nothing an agent CLI writes
// can appear in an error string, whatever shape it takes.
//
// The cost is the one the shape test was trying to avoid: a reason a future CLI
// adds is not named until this list is updated. It is still NAMED — as
// TerminalReasonUnclassified, which says truthfully that the CLI reported
// something this build does not know, instead of echoing it — but it is
// CHARGED, not refunded. See environmentalTerminalReasons: only reasons this
// build has judged environmental bypass the attempt budget, so a newer CLI
// cannot quietly widen what stops being charged.

// The terminal reasons Claude Code declares, measured from the 2.1.251 binary:
// the result schema's `terminal_reason` is `enum(uP)` where `uP` is the union
// of the two internal lists. All nineteen are here so the map is auditable
// against the CLI one-for-one rather than trimmed to the ones seen in the wild.
const (
	TerminalReasonBlockingLimit                = "blocking_limit"
	TerminalReasonRapidRefillBreaker           = "rapid_refill_breaker"
	TerminalReasonPromptTooLong                = "prompt_too_long"
	TerminalReasonImageError                   = "image_error"
	TerminalReasonModelError                   = "model_error"
	TerminalReasonAPIError                     = "api_error"
	TerminalReasonMalformedToolUseExhausted    = "malformed_tool_use_exhausted"
	TerminalReasonAbortedStreaming             = "aborted_streaming"
	TerminalReasonAbortedTools                 = "aborted_tools"
	TerminalReasonStopHookPrevented            = "stop_hook_prevented"
	TerminalReasonHookStopped                  = "hook_stopped"
	TerminalReasonToolDeferred                 = "tool_deferred"
	TerminalReasonMaxTurns                     = "max_turns"
	TerminalReasonBackgroundRequested          = "background_requested"
	TerminalReasonCompleted                    = "completed"
	TerminalReasonBudgetExhausted              = "budget_exhausted"
	TerminalReasonStructuredOutputRetryExpired = "structured_output_retry_exhausted"
	TerminalReasonToolDeferredUnavailable      = "tool_deferred_unavailable"
	TerminalReasonTurnSetupFailed              = "turn_setup_failed"

	// TerminalReasonUnclassified is what a reason this build does not recognize
	// is called. It is NOT a value any CLI emits — it is this package saying
	// "the CLI named something, and naming it back would be echoing a
	// subprocess's bytes into a public comment".
	TerminalReasonUnclassified = "unclassified"
)

// environmentalTerminalReasons are the reasons that mean the ENVIRONMENT failed,
// not the work — the only ones a zero-usage turn is refunded for.
//
// SPLITTING THE SET IS THE POINT, and #235's review is the measurement that
// says so. "No model turn ran" was treated as sufficient on its own, which is
// wrong in one direction that matters: `prompt_too_long` also consumes no
// input tokens, and it is a DETERMINISTIC consequence of the prompt the SDLC module
// built from this work item. Refunding it means the item is parked, relabelled
// by a human, dispatched again into the identical failure, refunded again —
// the attempt budget, which exists precisely to stop that loop, bypassed.
//
// So the question is not "did a turn run" alone but "would a different
// environment have changed the answer". These two say yes:
//
//	api_error             the request never reached the API — no credential,
//	                      no network, a transport refusal. #235's measured case.
//	turn_setup_failed     the CLI could not set the turn up at all.
//
// `blocking_limit` and `rapid_refill_breaker` were listed here too, read as
// provider-side capacity. They are not; see contextExhaustedTerminalReasons.
//
// EVERYTHING ELSE IS CHARGED, including reasons this build does not recognise.
// A new reason from a newer CLI is charged and its class surfaced, so an
// operator sees the name while the budget stays honest — conservative in the
// direction that cannot silently defeat the wall. The work-derived members are
// listed in the test that pins this set, with why each one is charged.
var environmentalTerminalReasons = map[string]bool{
	TerminalReasonAPIError:        true,
	TerminalReasonTurnSetupFailed: true,
}

// contextExhaustedTerminalReasons are the reasons that mean the SESSION'S
// CONTEXT WINDOW IS FULL: the conversation can no longer take another prompt,
// whatever the prompt says.
//
// MEASURED, NOT GUESSED. `blocking_limit` and `rapid_refill_breaker` were
// first judged environmental — "the provider refused the request outright",
// "the provider's own breaker tripped" — on the strength of their names. The
// 2.1.278 CLI's own dispatch says otherwise: it routes all three of
// `blocking_limit`, `prompt_too_long` and `rapid_refill_breaker` to one
// outcome, `{kind: "stop", cause: "context_limit"}`, and emits
// `blocking_limit` from the same code path that surfaces "prompt too long"
// when the auto-compact window reports `blocked`. On the burn-in fleet
// (go-quicbaas #51 and #965, 2026-09-21) both arrived exactly there: a
// 20–25 minute delivery turn, verification, and then the round-1 re-prompt
// refused after three minutes with no text and no usage.
//
// Refunding them was wrong twice over. The attempt WAS spent — the turn ran
// and its work was pushed — and a redispatch re-runs the whole turn into a
// fresh session that fills the same way. What a full context calls for is a
// fresh SESSION on the same branch, which is the SDLC runner's decision to
// make (see its session rollover); what this package says is only that the
// reason is the session's, not the environment's, so nothing refunds it.
var contextExhaustedTerminalReasons = map[string]bool{
	TerminalReasonBlockingLimit:      true,
	TerminalReasonPromptTooLong:      true,
	TerminalReasonRapidRefillBreaker: true,
}

// IsContextExhaustedTerminalReason reports whether class says the session's
// context window is full. Exported for the same reason as
// IsEnvironmentalTerminalReason: the SDLC module asks this package rather
// than keeping a copy of the set.
func IsContextExhaustedTerminalReason(class string) bool {
	return contextExhaustedTerminalReasons[class]
}

// ContextExhaustionOf reports whether err is a turn the CLI ended because the
// session's context was full, and names the reason class it gave.
//
// THE SINGLE SEAM, like PreFirstTurnFault: the SDLC runner decides on this
// answer whether to roll the session, and asks one question rather than
// type-switching over every carrier of turn-failure evidence.
func ContextExhaustionOf(err error) (string, bool) {
	evidence, carried := EvidenceOf(err)
	if !carried {
		return "", false
	}
	class := evidence.ReasonClass()
	if !contextExhaustedTerminalReasons[class] {
		return "", false
	}
	return class, true
}

// IsEnvironmentalTerminalReason reports whether class is one of the CLI reasons
// that refunds an attempt.
//
// EXPORTED SO THE SDLC MODULE CAN VALIDATE AGAINST THIS SET RATHER THAN A COPY OF
// IT. The SDLC finish path refuses a fault class it does not recognise, and the
// classes it accepts are its own resolver names plus exactly these. A second
// list over there would be a second place for the two to disagree, which is the
// mistake this branch has already made twice (the refund condition derived at
// two sites, the account fields rebuilt at a boundary).
func IsEnvironmentalTerminalReason(class string) bool {
	return environmentalTerminalReasons[class]
}

// EnvironmentalTerminalReasons lists the reasons that refund an attempt, sorted
// so the order is stable.
//
// The enumerating half of the predicate above, exported for the same reason: a
// caller that needs to cover this set — the SDLC module's own coverage test does,
// to prove its validator accepts every class its classifier can produce —
// should read the set rather than keep a copy of it in step by hand.
func EnvironmentalTerminalReasons() []string {
	out := make([]string, 0, len(environmentalTerminalReasons))
	for reason := range environmentalTerminalReasons {
		out = append(out, reason)
	}
	sort.Strings(out)
	return out
}

// knownTerminalReasons maps the wire value to the constant rendered for it.
//
// THE KEY IS COMPARED, THE VALUE IS RENDERED, and they are spelled identically
// on purpose: the point is not that the strings differ but that the returned
// one is a literal in this file rather than bytes from the wire. A reader
// checking this against the CLI is checking the keys; a reader checking what
// can reach a log is reading the values.
var knownTerminalReasons = map[string]string{
	TerminalReasonBlockingLimit:                TerminalReasonBlockingLimit,
	TerminalReasonRapidRefillBreaker:           TerminalReasonRapidRefillBreaker,
	TerminalReasonPromptTooLong:                TerminalReasonPromptTooLong,
	TerminalReasonImageError:                   TerminalReasonImageError,
	TerminalReasonModelError:                   TerminalReasonModelError,
	TerminalReasonAPIError:                     TerminalReasonAPIError,
	TerminalReasonMalformedToolUseExhausted:    TerminalReasonMalformedToolUseExhausted,
	TerminalReasonAbortedStreaming:             TerminalReasonAbortedStreaming,
	TerminalReasonAbortedTools:                 TerminalReasonAbortedTools,
	TerminalReasonStopHookPrevented:            TerminalReasonStopHookPrevented,
	TerminalReasonHookStopped:                  TerminalReasonHookStopped,
	TerminalReasonToolDeferred:                 TerminalReasonToolDeferred,
	TerminalReasonMaxTurns:                     TerminalReasonMaxTurns,
	TerminalReasonBackgroundRequested:          TerminalReasonBackgroundRequested,
	TerminalReasonCompleted:                    TerminalReasonCompleted,
	TerminalReasonBudgetExhausted:              TerminalReasonBudgetExhausted,
	TerminalReasonStructuredOutputRetryExpired: TerminalReasonStructuredOutputRetryExpired,
	TerminalReasonToolDeferredUnavailable:      TerminalReasonToolDeferredUnavailable,
	TerminalReasonTurnSetupFailed:              TerminalReasonTurnSetupFailed,
}

// TurnFailureEvidence is the structured account a CLI gives of a turn that
// ended in error, carried on the error rather than logged and discarded.
//
// It is embedded in the error types the agent CLIs produce so that ONE
// classification (PreFirstTurn) answers for all of them; see PreFirstTurnFault.
type TurnFailureEvidence struct {
	// TerminalReason is the CLI's own name for why its query loop terminated,
	// verbatim. Empty when the CLI reported none.
	//
	// IT IS NEVER RENDERED AS IT ARRIVES. #235's third defect was that the
	// burn-in's operator saw "(34 bytes of result withheld)" and nothing else,
	// so an authentication failure was indistinguishable from the work item
	// being hard; the answer is to render the constant this value MATCHES, not
	// the value. See ReasonClass and the file comment above it.
	TerminalReason string

	// NumTurns is the CLI's own turn count for the run. Diagnostic only — see
	// the file comment for why it is not the test.
	NumTurns int

	// Usage is the CLI's token accounting for the turn, or nil when it
	// reported none. The nil/zero distinction is load-bearing: "reported
	// nothing" is not evidence of anything, while "reported zero" is evidence
	// that no request reached the model.
	Usage *TokenUsage
}

// usageProvesNoTurn reports whether the CLI's token accounting says, in its own
// numbers, that no request reached the model.
//
// EVERY DIMENSION MUST BE EXACTLY ZERO — not the sum. This was first written as
// Total() == 0, which hosted review of #235 rejected: the counts come from a
// subprocess's JSON, int64 decodes a negative literal without complaint, and
// {"input_tokens":1,"output_tokens":-1} sums to zero. A turn that really did
// reach the model would then be refunded its attempt and parked as a
// configuration fault, on accounting that is self-evidently malformed.
//
// Testing each dimension answers the negative case and the mixed case together:
// -1 is not 0, so a record carrying one fails here and is charged as the
// ordinary failure it is. MALFORMED ACCOUNTING IS NOT EVIDENCE OF ANYTHING, and
// the safe direction for a number nobody can vouch for is to charge the attempt
// — the budget is what stops a broken configuration retrying forever.
//
// Overflow needs no separate test: a value outside int64 fails json.Unmarshal,
// so the message never becomes a TokenUsage at all — the stream-json reader
// skips the line and the one-shot reader falls back to raw text, and both are
// ordinary failures. A value inside int64 but absurdly large is simply not
// zero.
//
// THE MALFORMED CHECK IS WHAT KEEPS THE ABOVE TRUE AFTER #242. Negative counts
// are now clamped to zero at the decode boundary, so {"input_tokens":-5} — which
// this test used to reject because -5 is not 0 — arrives here as four zeroes and
// would read as proof that no request reached the model. The evidence would be
// manufactured by the repair: the very record #235 refused to trust would start
// refunding attempts. Asking whether the record was repaired restores the
// reading the dimension test used to give, on a value that no longer carries it.
//
// This is the coupling to watch when changing either: #235 depends on a
// negative SURVIVING to prove malformedness, and #242 exists to stop negatives
// surviving. Malformed is where the two meet.
func (e TurnFailureEvidence) usageProvesNoTurn() bool {
	u := e.Usage
	if u == nil {
		return false
	}
	if u.Malformed() {
		return false
	}
	return u.InputTokens == 0 &&
		u.OutputTokens == 0 &&
		u.CacheCreationInputTokens == 0 &&
		u.CacheReadInputTokens == 0
}

// PreFirstTurn reports whether the CLI failed before any model turn ran.
//
// BOTH SIGNALS ARE REQUIRED, and the conjunction is the conservative
// direction. A CLI that reported no usage at all tells us nothing about
// whether a turn happened, so it is charged as an ordinary failed attempt
// exactly as it was before; only a CLI that reported its accounting AND
// reported it as zero has said, in its own structured output, that the work
// never started.
//
// THE REASON TEST IS ReasonClass, NOT A BARE NON-EMPTY CHECK, and the
// difference is not cosmetic. A reason that fails the shape guard is one this
// package will not render, so classifying on it would produce a fault with no
// name: the caller stores an empty class, the reason it composes reads "exited
// on “ before its first turn", and every downstream test of "is this a fault"
// — all of which ask whether the class is non-empty — silently answers no. A
// half-classified fault is worse than an unclassified one, because it looks
// like it worked.
//
// AND THE REASON MUST BE AN ENVIRONMENTAL ONE. Zero usage says no model turn
// ran; it does not say why, and several reasons that consume no input tokens
// are deterministic properties of the work — see environmentalTerminalReasons.
// A reason this build does not recognise is charged, so a newer CLI cannot
// quietly widen what gets refunded.
func (e TurnFailureEvidence) PreFirstTurn() bool {
	return environmentalTerminalReasons[e.TerminalReason] && e.usageProvesNoTurn()
}

// ReasonClass is the constant this package renders for the CLI's reason.
//
// Three answers, and they are three different facts:
//
//	""                             the CLI reported no reason at all
//	<a knownTerminalReasons value>  it reported one this build knows
//	TerminalReasonUnclassified     it reported one this build does not know
//
// The third is what keeps an unrecognized reason from being either echoed or
// silently dropped: the fault is still named, and the name is this package's
// rather than the subprocess's. See the comment above knownTerminalReasons.
func (e TurnFailureEvidence) ReasonClass() string {
	if e.TerminalReason == "" {
		return ""
	}
	if class, known := knownTerminalReasons[e.TerminalReason]; known {
		return class
	}
	return TerminalReasonUnclassified
}

// EvidenceOf extracts a CLI's structured turn-failure account from err, if any
// error in the chain carries one.
//
// It exists so a transport can FORWARD the evidence rather than re-deriving it:
// the daemon that ran the process is the only party that saw the CLI's result
// object, and everything downstream has to be handed what it found.
func EvidenceOf(err error) (TurnFailureEvidence, bool) {
	var (
		stream  *StreamJSONError
		oneShot *OneShotExecutionError
		remote  *RemoteTurnFailureError
	)
	switch {
	case errors.As(err, &stream):
		return stream.TurnFailureEvidence, true
	case errors.As(err, &oneShot):
		return oneShot.TurnFailureEvidence, true
	case errors.As(err, &remote):
		return remote.TurnFailureEvidence, true
	}
	return TurnFailureEvidence{}, false
}

// PreFirstTurnFault reports whether err is a turn that failed before reaching
// the model, and names the reason class the CLI gave for it.
//
// THE SINGLE SEAM. Callers outside this package — the SDLC module decides on
// its strength whether an item's attempt budget was spent — ask this one
// question rather than type-switching over every CLI error type, so a backend
// that starts carrying evidence is answered without the caller changing.
//
// A false answer means "this is an ordinary failure, treat it as you always
// have": either the error carries no evidence, or the evidence says a model
// turn ran.
//
// A TRUE ANSWER ALWAYS CARRIES A NON-EMPTY CLASS. PreFirstTurn tests
// ReasonClass for exactly this reason, so a caller may use the class as the
// fault's identity without checking it twice.
func PreFirstTurnFault(err error) (string, bool) {
	evidence, carried := EvidenceOf(err)
	if !carried || !evidence.PreFirstTurn() {
		return "", false
	}
	return evidence.ReasonClass(), true
}
