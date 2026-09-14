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
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// Limit detection
//
// HONEST LIMITATION, READ BEFORE TRUSTING THIS
//
// QuicBaaS does not sit between the agent CLI and the provider. It spawns
// `claude` / `codex` / `opencode` as a subprocess and sees only what that
// subprocess chooses to print: a stderr tail, a stream-json error record, or
// the text of a failed turn. None of those is a structured API response, none
// of them carries the provider's HTTP status or Retry-After header verbatim by
// contract, and every one of them is free to change wording in the next CLI
// release.
//
// Classification here is therefore a HEURISTIC over free text. It is designed
// to fail in the cheap direction:
//
//   - A false negative costs one failed turn that the operator sees as a plain
//     turn_failed, exactly as it would today.
//   - A false positive parks a healthy account for one backoff window and moves
//     the session to another account in the pool — recoverable, and visible in
//     the transcript with the matched signal attached (AccountLimitedError.Signal)
//     so the misclassification can be identified rather than guessed at.
//
// The authoritative fix is a credential proxy: route the agent's provider
// traffic through QuicBaaS so the real status code and Retry-After header are
// observed instead of inferred. Nothing in this file pretends to be that, and
// the rule set is deliberately narrow — signals that are merely CORRELATED with
// exhaustion (overloaded_error, 5xx, context_length_exceeded) are excluded,
// because rotating to another account does not fix any of them and doing so
// would burn the whole pool on a provider-side incident.
// =============================================================================

// DefaultAccountLimitBackoff is how long an account is held out of the pool
// when the provider reported a limit but no machine-readable retry hint.
//
// One hour is chosen against the shape of the limits that actually exist: the
// consumer plans this feature targets meter in multi-hour windows, so a short
// backoff would simply re-select the account, burn another turn, and re-limit
// it. An account that recovers earlier loses at most the remainder of the hour,
// and only when the rest of the pool is also exhausted.
const DefaultAccountLimitBackoff = time.Hour

// MaxAccountLimitBackoff caps a parsed retry hint. A malformed or hostile
// Retry-After (a date in 2038, a nine-digit second count) must not be able to
// park an account permanently, which is what an uncapped hint would do.
const MaxAccountLimitBackoff = 24 * time.Hour

// maxLimitSignalLength bounds the evidence stored on an account record and
// echoed into a session transcript.
const maxLimitSignalLength = 240

// AccountLimitSignal is one classified provider limit.
type AccountLimitSignal struct {
	// Matched is the redacted, truncated evidence the classifier fired on.
	Matched string

	// RetryAfter is the provider's own hint, or zero when it supplied none.
	// A caller applies DefaultAccountLimitBackoff in that case; the zero is
	// preserved here so "the provider told us" and "we guessed" stay
	// distinguishable on the resulting AccountLimitedError.
	RetryAfter time.Duration
}

// AvailableAt returns the moment the account should rejoin the pool, applying
// fallback when the provider gave no hint.
func (s AccountLimitSignal) AvailableAt(now time.Time, fallback time.Duration) time.Time {
	wait := s.RetryAfter
	if wait <= 0 {
		wait = fallback
	}
	if wait <= 0 {
		wait = DefaultAccountLimitBackoff
	}
	if wait > MaxAccountLimitBackoff {
		wait = MaxAccountLimitBackoff
	}
	return now.Add(wait)
}

// AccountLimitClassifier decides whether a failed turn was the provider
// refusing an account for rate/quota/usage reasons.
//
// It is an interface so a deployment that DOES proxy provider traffic can
// replace the heuristic with an exact one without touching the rotation path.
type AccountLimitClassifier interface {
	// Classify reports whether err is a provider limit for provider. The bool
	// is false for every other failure, including authentication errors and
	// provider-side capacity errors, neither of which another account fixes.
	Classify(provider AccountProvider, err error) (AccountLimitSignal, bool)
}

// =============================================================================
// Rule set
// =============================================================================

// limitRules is one provider's matching vocabulary. Rules are lowercase
// substrings tested against the extracted failure text.
type limitRules struct {
	// strong are phrases that mean "limit" on their own.
	strong []string

	// negative are phrases that VETO a match. They exist because several
	// unrelated failures contain a strong phrase as a substring, and rotating
	// on them would be wrong: a provider-side overload is not this account's
	// fault, and a context-window overflow follows the conversation to
	// whichever account it moves to.
	negative []string
}

// sharedLimitRules apply to every provider. They are the phrases that appear in
// the HTTP layer itself rather than in any one vendor's prose.
var sharedLimitRules = limitRules{
	strong: []string{
		"too many requests",
		"rate limit exceeded",
		"rate_limit_exceeded",
		"rate limit reached",
		"rate limited",
		"quota exceeded",
		"quota_exceeded",
		"insufficient_quota",
		"usage limit reached",
		"usage_limit_reached",
		"usage limit exceeded",
	},
	negative: []string{
		// Provider-side capacity, not this account's allowance. Every account
		// in the pool would fail identically, so rotating burns the pool.
		"overloaded_error",
		"service unavailable",
		// The conversation is too long for the model. It travels with the
		// session, so another account changes nothing.
		"context_length_exceeded",
		"context window",
		"prompt is too long",
		// Configuration echoes, not failures. A CLI that prints its own
		// rate-limit settings must not be read as having hit one.
		"rate limit configuration",
		"rate limiting disabled",
	},
}

// providerLimitRules holds the per-provider additions, keyed for O(1) lookup.
// A provider absent from the map gets sharedLimitRules alone, which is the
// correct default for an operator-defined provider QuicBaaS knows nothing about.
var providerLimitRules = map[AccountProvider]limitRules{
	AccountProviderClaude: {
		strong: []string{
			// Claude Code prints these when a plan's rolling window is spent.
			"claude usage limit reached",
			"5-hour limit",
			"five-hour limit",
			"weekly limit reached",
			"you've reached your usage limit",
			"upgrade to increase your usage limit",
		},
	},
	AccountProviderCodex: {
		strong: []string{
			// OpenAI's canonical quota exhaustion body.
			"exceeded your current quota",
			"check your plan and billing",
		},
	},
}

// httpStatusLimitContext are the words that turn a bare "429" in free text into
// evidence of a status code. "429" alone is not enough: it appears in token
// counts, byte offsets, and file paths, and a rotation triggered by a line
// number would be indefensible.
var httpStatusLimitContext = []string{"status", "http", "code", "rate", "limit", "quota", "retry"}

// httpStatusLimitPattern requires the 429 to STAND ALONE AS A NUMBER rather
// than merely occur somewhere in the buffer.
//
// THE CONTEXT WORDS ABOVE WERE NEVER ENOUGH ON THEIR OWN, and the comment on
// them says what they were meant to exclude — token counts, byte offsets, file
// paths. A substring test cannot exclude any of those, because the digits it
// looks for are inside a longer number every time: measured on the unanchored
// version, "4294967296" and "4291" both matched, as did an API key with 429 in
// its body (#500). The guard stated an intent the implementation did not have.
//
// A 429 INSIDE A TOKEN IS NOT A STATUS CODE NO MATTER WHAT IS NEAR IT, which is
// why this is a boundary rather than a proximity window. Requiring the context
// word to sit within N characters would still accept a credential whose body
// happens to contain 429 as long as an error sentence mentioned a limit — and
// it would start REJECTING genuine cases where the status and its explanation
// are on different lines, which is the ordinary shape of an HTTP error dump.
//
// Written as explicit delimiters rather than \b because \b treats "-" as a
// boundary: sk-ant-429-xyz would satisfy \b429\b, and hyphens are exactly how
// provider keys are segmented. Underscores segment the other half of the same
// space — request ids, trace ids, and the snake_case field names in a JSON
// error body — so the class excludes both. "-" AND "_" ARE IDENTIFIER
// CHARACTERS HERE, NOT SEPARATORS: the first version of this class was
// [^a-z0-9], which readmitted every token the paragraph above rules out and
// left the comment stating an intent the pattern did not have (#530).
//
// Every separator a status code legitimately appears beside — space, colon,
// comma, quote, "=", "/", ".", newline, parenthesis — still qualifies, and
// every character that would make the digits part of a longer identifier does
// not.
//
// Operates on the same lowered text the rest of the rules use, so the class
// needs no upper-case half; see the caller.
var httpStatusLimitPattern = regexp.MustCompile(`(?:^|[^a-z0-9_-])429(?:[^a-z0-9_-]|$)`)

// =============================================================================
// Classifier
// =============================================================================

// defaultAccountLimitClassifier is the heuristic AccountLimitClassifier
// described at the top of this file.
type defaultAccountLimitClassifier struct{}

// Compile-time interface satisfaction check.
var _ AccountLimitClassifier = defaultAccountLimitClassifier{}

// NewAccountLimitClassifier returns the built-in heuristic classifier.
func NewAccountLimitClassifier() AccountLimitClassifier {
	return defaultAccountLimitClassifier{}
}

// Classify implements AccountLimitClassifier.
func (defaultAccountLimitClassifier) Classify(provider AccountProvider, err error) (AccountLimitSignal, bool) {
	if err == nil {
		return AccountLimitSignal{}, false
	}

	// An already-classified limit (a pinned resolve that refused a held
	// account, or a remote daemon that classified it first) is authoritative:
	// re-deriving it from prose would only lose the timing it already carries.
	var limited *AccountLimitedError
	if errors.As(err, &limited) {
		return AccountLimitSignal{
			Matched:    redactLimitSignal(limited.Signal),
			RetryAfter: limited.RetryAfter,
		}, true
	}

	text := limitFailureText(err)
	if text == "" {
		return AccountLimitSignal{}, false
	}
	lowered := strings.ToLower(text)

	rules := providerLimitRules[provider]
	for _, veto := range sharedLimitRules.negative {
		if strings.Contains(lowered, veto) {
			return AccountLimitSignal{}, false
		}
	}
	for _, veto := range rules.negative {
		if strings.Contains(lowered, veto) {
			return AccountLimitSignal{}, false
		}
	}

	if !limitPhrasePresent(lowered, sharedLimitRules.strong) &&
		!limitPhrasePresent(lowered, rules.strong) &&
		!matchHTTPStatusLimit(lowered) {
		return AccountLimitSignal{}, false
	}

	return AccountLimitSignal{
		Matched:    redactLimitSignal(text),
		RetryAfter: parseRetryAfter(lowered, time.Now().UTC()),
	}, true
}

// limitPhrasePresent reports whether any phrase occurs in lowered.
func limitPhrasePresent(lowered string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(lowered, phrase) {
			return true
		}
	}
	return false
}

// matchHTTPStatusLimit reports a standalone 429 that appears alongside at least
// one word placing it in an HTTP/limit context. See httpStatusLimitContext and
// httpStatusLimitPattern.
//
// BOTH NARROWINGS ARE REQUIRED AND NEITHER SUBSUMES THE OTHER. The pattern says
// the digits are a number rather than part of an identifier; the context words
// say the number is a status code rather than a count. A file path ending
// ":429:" satisfies the first and can still satisfy the second, so this is not
// the whole of the problem the context words were written for — see #500.
func matchHTTPStatusLimit(lowered string) bool {
	if !httpStatusLimitPattern.MatchString(lowered) {
		return false
	}
	for _, word := range httpStatusLimitContext {
		if strings.Contains(lowered, word) {
			return true
		}
	}
	return false
}

// =============================================================================
// Failure text extraction
// =============================================================================

// limitFailureText pulls the provider's own words out of an error chain.
//
// The typed backend errors carry the raw provider output in a named field; the
// wrapper text around them ("acp: one-shot execution failed: claude: ...")
// is QuicBaaS's, not the provider's, and matching on it would classify our own
// vocabulary. Preferring the innermost payload keeps the rules pointed at what
// the CLI actually printed.
func limitFailureText(err error) string {
	var oneShot *OneShotExecutionError
	if errors.As(err, &oneShot) && oneShot.Stderr != "" {
		return oneShot.Stderr
	}
	var streamJSON *StreamJSONError
	if errors.As(err, &streamJSON) && streamJSON.Result != "" {
		return streamJSON.Result
	}
	var exited *StreamJSONProcessExitedError
	if errors.As(err, &exited) && exited.Stderr != "" {
		return exited.Stderr
	}
	// The peer's JSON-RPC message, asked for BY NAME. Its Error() withholds the
	// text (review 1330), and this classifier is precisely the caller the field
	// is kept for — a provider limit reported through JSON-RPC rather than
	// through stderr would otherwise stop being classifiable the moment the
	// text stopped being rendered.
	var peer *PeerRPCError
	if errors.As(err, &peer) && peer.Message != "" {
		return peer.Message
	}
	return err.Error()
}

// =============================================================================
// Retry hints
// =============================================================================

var (
	// retryAfterSecondsPattern matches "retry-after: 120", "retry_after=120",
	// and the JSON spelling "\"retryAfter\": 120".
	retryAfterSecondsPattern = regexp.MustCompile(`retry[-_ ]?after"?\s*[:=]\s*"?(\d{1,7})\b`)

	// retryAfterMillisPattern matches the millisecond spelling some SDKs emit.
	retryAfterMillisPattern = regexp.MustCompile(`retry[-_ ]?after[-_ ]?ms"?\s*[:=]\s*"?(\d{1,10})\b`)

	// retryAfterDatePattern matches an HTTP-date Retry-After. The date itself
	// is parsed by net/http, which already implements all three legal formats.
	retryAfterDatePattern = regexp.MustCompile(`retry[-_ ]?after"?\s*[:=]\s*"?([a-z]{3},\s*\d{1,2}\s+[a-z]{3}\s+\d{4}[^"\n]*gmt)`)

	// resetAtUnixPattern matches the epoch-seconds reset timestamp several
	// providers publish alongside a limit ("resets_at", "reset_at",
	// "rate_limit_reset").
	resetAtUnixPattern = regexp.MustCompile(`(?:resets?[-_ ]?at|rate[-_ ]?limit[-_ ]?reset)"?\s*[:=]\s*"?(\d{10})\b`)
)

// parseRetryAfter extracts the provider's own wait hint from lowered, or
// returns zero when there is none.
//
// Every result is clamped to (0, MaxAccountLimitBackoff]: a hint that parses to
// a non-positive or absurd duration is treated as absent, so the caller's
// default backoff applies instead of a value that would either re-select the
// account immediately or retire it forever.
func parseRetryAfter(lowered string, now time.Time) time.Duration {
	if m := retryAfterMillisPattern.FindStringSubmatch(lowered); m != nil {
		if ms, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			return clampLimitBackoff(time.Duration(ms) * time.Millisecond)
		}
	}
	if m := retryAfterSecondsPattern.FindStringSubmatch(lowered); m != nil {
		if secs, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			return clampLimitBackoff(time.Duration(secs) * time.Second)
		}
	}
	if m := retryAfterDatePattern.FindStringSubmatch(lowered); m != nil {
		if when, err := http.ParseTime(strings.ToUpper(m[1])); err == nil {
			return clampLimitBackoff(when.Sub(now))
		}
	}
	if m := resetAtUnixPattern.FindStringSubmatch(lowered); m != nil {
		if epoch, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			return clampLimitBackoff(time.Unix(epoch, 0).UTC().Sub(now))
		}
	}
	return 0
}

// clampLimitBackoff maps a parsed duration into the usable range, returning
// zero for anything outside it so the caller falls back to its default.
func clampLimitBackoff(d time.Duration) time.Duration {
	if d <= 0 || d > MaxAccountLimitBackoff {
		return 0
	}
	return d
}

// =============================================================================
// Redaction
// =============================================================================

// credentialShapedPattern matches the token shapes a provider error body may
// echo back. The evidence string is stored on the account record and written
// into a session transcript, so it goes through the same "a provider that
// echoes the presented token must not turn metadata into a copy of the
// credential" rule Account.LastError already documents.
var credentialShapedPattern = regexp.MustCompile(`(?i)\b(?:sk-[a-z0-9_-]{8,}|sk_[a-z0-9_-]{8,}|bearer\s+[a-z0-9._~+/=-]{12,}|[a-z0-9_-]*(?:oat|ant)-[a-z0-9._-]{12,})`)

// redactLimitSignal strips credential-shaped substrings, collapses whitespace,
// and truncates the evidence to maxLimitSignalLength.
func redactLimitSignal(text string) string {
	if text == "" {
		return ""
	}
	cleaned := credentialShapedPattern.ReplaceAllString(text, "[redacted]")
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if len(cleaned) > maxLimitSignalLength {
		cleaned = cleaned[:maxLimitSignalLength] + "…"
	}
	return cleaned
}
