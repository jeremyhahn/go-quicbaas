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
	"strconv"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Classification
//
// Two halves, and the negative half matters more. A false positive parks a
// healthy seat and moves a conversation off it; on a provider-side incident
// (overloaded, 5xx) a permissive rule would do that to EVERY account in the
// pool in turn, which is strictly worse than surfacing the failure.
// =============================================================================

func TestAccountLimitClassifier_ClassifiesRealProviderLimits(t *testing.T) {
	classifier := NewAccountLimitClassifier()

	tests := []struct {
		name     string
		provider AccountProvider
		err      error
	}{
		{
			name:     "claude usage limit on stderr",
			provider: AccountProviderClaude,
			err: &OneShotExecutionError{Command: "claude",
				Stderr: "Claude usage limit reached. Your limit will reset at 9pm."},
		},
		{
			name:     "claude five-hour window in a stream-json result",
			provider: AccountProviderClaude,
			err: &StreamJSONError{Command: "claude",
				Result: "You've reached your usage limit for this 5-hour limit window."},
		},
		{
			name:     "anthropic rate_limit_error body",
			provider: AccountProviderClaude,
			err: &OneShotExecutionError{Command: "claude",
				Stderr: `API Error: 429 {"type":"error","error":{"type":"rate_limit_error","message":"Number of requests has exceeded your rate limit"}}`},
		},
		{
			name:     "openai insufficient_quota",
			provider: AccountProviderCodex,
			err: &OneShotExecutionError{Command: "codex",
				Stderr: `{"error":{"code":"insufficient_quota","message":"You exceeded your current quota, please check your plan and billing details."}}`},
		},
		{
			name:     "bare HTTP 429 with a status word",
			provider: AccountProviderOpenCode,
			err: &StreamJSONProcessExitedError{Command: "opencode", ExitCode: 1,
				Stderr: "request failed with status 429"},
		},
		{
			name:     "too many requests, provider we ship no rules for",
			provider: AccountProvider("some-vendor"),
			err:      &OneShotExecutionError{Command: "x", Stderr: "HTTP 429 Too Many Requests"},
		},
		{
			name:     "wrapped in a PromptError, as the turn path delivers it",
			provider: AccountProviderClaude,
			err: &PromptError{SessionID: "s1", Cause: &OneShotExecutionError{
				Command: "claude", Stderr: "Claude usage limit reached"}},
		},
		{
			name:     "already-classified limit passes straight through",
			provider: AccountProviderClaude,
			err:      &AccountLimitedError{AccountID: "acct-1", RetryAfter: 30 * time.Second},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := classifier.Classify(tt.provider, tt.err); !ok {
				t.Fatalf("Classify() did not recognise a provider limit: %v", tt.err)
			}
		})
	}
}

func TestAccountLimitClassifier_RejectsFailuresAnotherAccountCannotFix(t *testing.T) {
	classifier := NewAccountLimitClassifier()

	tests := []struct {
		name     string
		provider AccountProvider
		err      error
	}{
		{"nil error", AccountProviderClaude, nil},
		{
			name:     "provider-side overload is not this account's allowance",
			provider: AccountProviderClaude,
			err: &OneShotExecutionError{Command: "claude",
				Stderr: `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`},
		},
		{
			name:     "503 service unavailable",
			provider: AccountProviderCodex,
			err:      &OneShotExecutionError{Command: "codex", Stderr: "503 Service Unavailable"},
		},
		{
			name:     "authentication failure needs a fixed credential, not a different one",
			provider: AccountProviderClaude,
			err: &OneShotExecutionError{Command: "claude",
				Stderr: `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`},
		},
		{
			name:     "context overflow travels with the conversation",
			provider: AccountProviderCodex,
			err: &OneShotExecutionError{Command: "codex",
				Stderr: `{"error":{"code":"context_length_exceeded","message":"maximum context length exceeded"}}`},
		},
		{
			name:     "prompt too long is not a quota",
			provider: AccountProviderClaude,
			err:      &StreamJSONError{Command: "claude", Result: "prompt is too long: 250000 tokens > 200000 maximum"},
		},
		{
			name:     "a CLI echoing its own rate limit configuration has not hit one",
			provider: AccountProviderClaude,
			err:      &OneShotExecutionError{Command: "claude", Stderr: "loaded rate limit configuration from ~/.claude/settings.json"},
		},
		{
			name:     "rate limiting disabled is the opposite of a limit",
			provider: AccountProviderOpenCode,
			err:      &OneShotExecutionError{Command: "opencode", Stderr: "warning: rate limiting disabled for this endpoint"},
		},
		{
			name:     "a bare 429 with no HTTP context is a number, not a status",
			provider: AccountProviderClaude,
			err:      &OneShotExecutionError{Command: "claude", Stderr: "wrote 429 bytes to /workspace/out.txt"},
		},
		{
			name:     "missing binary",
			provider: AccountProviderClaude,
			err:      &OneShotExecutionError{Command: "claude", Stderr: "Error: ENOENT: no such file or directory, open '/workspace/x'"},
		},
		{
			name:     "plain bad request",
			provider: AccountProviderCodex,
			err:      &OneShotExecutionError{Command: "codex", Stderr: "400 Bad Request: model not found"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := classifier.Classify(tt.provider, tt.err); ok {
				t.Fatalf("Classify() misclassified as a provider limit — rotating would not fix it: %v", tt.err)
			}
		})
	}
}

func TestAccountLimitClassifier_ParsesRetryHints(t *testing.T) {
	classifier := NewAccountLimitClassifier()

	tests := []struct {
		name   string
		stderr string
		want   time.Duration
	}{
		{"header seconds", "429 Too Many Requests\nretry-after: 120", 120 * time.Second},
		{"json snake case", `{"error":"rate limit exceeded","retry_after":45}`, 45 * time.Second},
		{"json camel case", `{"error":"rate limit exceeded","retryAfter": 90}`, 90 * time.Second},
		{"milliseconds", `{"error":"rate limit exceeded","retry_after_ms":2500}`, 2500 * time.Millisecond},
		{"no hint at all", "Claude usage limit reached", 0},
		{"zero is treated as absent", "rate limit exceeded, retry-after: 0", 0},
		{"absurd hint is treated as absent", "rate limit exceeded, retry-after: 9999999", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signal, ok := classifier.Classify(AccountProviderClaude,
				&OneShotExecutionError{Command: "claude", Stderr: tt.stderr})
			if !ok {
				t.Fatalf("Classify() did not recognise the limit: %s", tt.stderr)
			}
			if signal.RetryAfter != tt.want {
				t.Fatalf("RetryAfter = %s, want %s", signal.RetryAfter, tt.want)
			}
		})
	}
}

func TestAccountLimitClassifier_ParsesUnixResetTimestamp(t *testing.T) {
	classifier := NewAccountLimitClassifier()
	reset := time.Now().UTC().Add(30 * time.Minute).Unix()

	signal, ok := classifier.Classify(AccountProviderClaude, &OneShotExecutionError{
		Command: "claude",
		Stderr:  `{"error":"usage limit reached","resets_at":` + strconv.FormatInt(reset, 10) + `}`,
	})
	if !ok {
		t.Fatal("Classify() did not recognise the limit")
	}
	if signal.RetryAfter < 29*time.Minute || signal.RetryAfter > 31*time.Minute {
		t.Fatalf("RetryAfter = %s, want ~30m parsed from resets_at", signal.RetryAfter)
	}
}

func TestAccountLimitSignal_AvailableAt(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		signal   AccountLimitSignal
		fallback time.Duration
		want     time.Time
	}{
		{"provider hint wins", AccountLimitSignal{RetryAfter: 5 * time.Minute}, time.Hour, now.Add(5 * time.Minute)},
		{"fallback when no hint", AccountLimitSignal{}, 20 * time.Minute, now.Add(20 * time.Minute)},
		{"default when neither", AccountLimitSignal{}, 0, now.Add(DefaultAccountLimitBackoff)},
		{"capped", AccountLimitSignal{RetryAfter: 72 * time.Hour}, 0, now.Add(MaxAccountLimitBackoff)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.signal.AvailableAt(now, tt.fallback); !got.Equal(tt.want) {
				t.Fatalf("AvailableAt() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestAccountLimitClassifier_RedactsCredentialShapedEvidence(t *testing.T) {
	classifier := NewAccountLimitClassifier()

	signal, ok := classifier.Classify(AccountProviderCodex, &OneShotExecutionError{
		Command: "codex",
		Stderr:  `insufficient_quota for key sk-proj-AbCdEf0123456789XYZ (Bearer sk-ant-oat01-abcdefghijklmnop)`,
	})
	if !ok {
		t.Fatal("Classify() did not recognise the limit")
	}
	if strings.Contains(signal.Matched, "AbCdEf0123456789XYZ") || strings.Contains(signal.Matched, "abcdefghijklmnop") {
		t.Fatalf("evidence retained credential-shaped material: %q", signal.Matched)
	}
	if !strings.Contains(signal.Matched, "[redacted]") {
		t.Fatalf("evidence was not redacted: %q", signal.Matched)
	}
}

func TestAccountLimitClassifier_TruncatesLongEvidence(t *testing.T) {
	classifier := NewAccountLimitClassifier()
	signal, ok := classifier.Classify(AccountProviderClaude, &OneShotExecutionError{
		Command: "claude",
		Stderr:  "usage limit reached " + strings.Repeat("x", 5000),
	})
	if !ok {
		t.Fatal("Classify() did not recognise the limit")
	}
	if len([]rune(signal.Matched)) > maxLimitSignalLength+1 {
		t.Fatalf("evidence length = %d runes, want <= %d", len([]rune(signal.Matched)), maxLimitSignalLength+1)
	}
}

func TestAccountLimitedError_UnwrapsToSentinel(t *testing.T) {
	err := &AccountLimitedError{
		AccountID:   "acct-1",
		Provider:    AccountProviderClaude,
		RetryAfter:  time.Minute,
		AvailableAt: time.Now().UTC(),
		Signal:      "usage limit reached",
	}
	if !errors.Is(err, ErrAccountLimited) {
		t.Fatal("errors.Is(err, ErrAccountLimited) = false")
	}
	msg := err.Error()
	for _, want := range []string{"acct-1", "claude", "usage limit reached"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("Error() = %q, missing %q", msg, want)
		}
	}
}

// TestAccountLimitClassifier_A429InsideATokenIsNotAStatusCode is the regression
// test for #500.
//
// `matchHTTPStatusLimit` tested `strings.Contains(lowered, "429")` — an
// unanchored substring — plus any context word anywhere in the same buffer. So
// digits belonging to a credential, a byte offset or a token count classified a
// non-limit failure as a provider rate limit, which is not a mislabel: a limit
// classification selects a different failure response, so the real error is
// backed off and retried behind a plausible-looking limit.
//
// THE PAIRS ARE THE TEST. Each rejection below is matched by an acceptance that
// a too-aggressive anchor would break, because the failure mode of this fix is
// a genuine provider limit that stops being recognised — silent, and worse than
// what it replaces.
func TestAccountLimitClassifier_A429InsideATokenIsNotAStatusCode(t *testing.T) {
	classifier := NewAccountLimitClassifier()

	// Every case carries a context word ("limit", "rate", "status", …), so the
	// context check cannot be what decides them. The anchor is.
	rejected := []struct{ name, stderr string }{
		{
			// The case #500 was filed for. #191 measured that agent stderr
			// carries the injected credential verbatim, so this is the
			// ordinary content of a failed spawn, not a contrived string.
			name:   "a credential whose body contains 429",
			stderr: "fatal: auth failed using key sk-ant-api03-x429yzqq (check your limit settings)",
		},
		{
			// 4294967296 is 2^32 — the single likeliest large number to appear
			// in a buffer offset, and it contains 429.
			name:   "a byte offset that contains 429",
			stderr: "read failed at offset 4294967296: limit exceeded on buffer",
		},
		{
			name:   "a token count that contains 429",
			stderr: "prompt used 4291 tokens, over the configured limit",
		},
		{
			// The exact string the doc comment on httpStatusLimitPattern cites
			// as the reason it is not written with \b. A character class of
			// [^a-z0-9] accepts "-" as a delimiter just as \b does, so the
			// pattern refuted its own stated rationale (#530).
			name:   "a hyphen-segmented provider key, the shape the comment names",
			stderr: "fatal: auth failed using key sk-ant-429-xyzq (check your limit settings)",
		},
		{
			name:   "a hyphen-segmented trace identifier",
			stderr: "trace error-429-foo recorded; see the status page",
		},
		{
			name:   "an underscore-segmented request identifier",
			stderr: "request req_429_7f failed: rate unavailable",
		},
		{
			// The false positive with no separator at all on one side, kept
			// beside the segmented cases so a fix that only handles "-" is
			// still caught.
			name:   "a version string whose minor component is 429",
			stderr: "gateway build v1.429-rc2 rejected the request; check status",
		},
	}
	for _, tc := range rejected {
		t.Run("rejected: "+tc.name, func(t *testing.T) {
			// Asserted, not assumed: without a context word these would be
			// refused by httpStatusLimitContext and the anchor would never be
			// consulted, so the rejection would prove nothing about it.
			lowered := strings.ToLower(tc.stderr)
			if !limitPhrasePresent(lowered, httpStatusLimitContext) {
				t.Fatalf("fixture carries no context word, so it is refused before the anchor is reached: %q", tc.stderr)
			}

			_, ok := classifier.Classify(AccountProviderClaude,
				&OneShotExecutionError{Command: "claude", Stderr: tc.stderr})
			if ok {
				t.Errorf("Classify() = true; digits inside a longer token are not a status code: %q", tc.stderr)
			}
		})
	}

	// The other direction. A status code appears beside every kind of
	// separator, and an anchor that demanded whitespace — or that required the
	// context word to sit near the number — would reject these.
	//
	// NONE OF THESE MAY CARRY A STRONG PHRASE, and the subtest asserts it
	// rather than trusting the wording. Classify reaches the status path only
	// when no provider phrase has already matched, so a case containing one
	// would pass however the anchor behaved — a green assertion proving
	// nothing. The first draft of this test had exactly that: two of four
	// accepted cases matched a strong phrase, and the over-tightening mutation
	// below failed only one subtest instead of all four.
	accepted := []struct{ name, stderr string }{
		{
			name:   "whitespace-delimited, the ordinary shape",
			stderr: "http 429 returned by the upstream gateway",
		},
		{
			name:   "structured, delimited by : and ,",
			stderr: `{"status_code":429,"message":"slow down"}`,
		},
		{
			name:   "status and its explanation on different lines",
			stderr: "http/1.1 429\nretry-after: 30\nthe account rate was reached",
		},
		{
			name:   "parenthesised",
			stderr: "upstream returned (429) — retry later",
		},
		{
			// The ^ arm of the alternation: nothing precedes the digits.
			name:   "at the very start of the buffer",
			stderr: "429 status returned by the upstream gateway",
		},
		{
			// The $ arm: nothing follows them.
			name:   "at the very end of the buffer",
			stderr: "upstream gateway http status 429",
		},
		{
			// Space on both sides, the plainest delimiter there is, asserted
			// explicitly so a class tightened past usefulness fails here first.
			name:   "space on both sides",
			stderr: "the gateway http code 429 came back again",
		},
	}
	for _, tc := range accepted {
		t.Run("accepted: "+tc.name, func(t *testing.T) {
			lowered := strings.ToLower(tc.stderr)
			if limitPhrasePresent(lowered, sharedLimitRules.strong) ||
				limitPhrasePresent(lowered, providerLimitRules[AccountProviderClaude].strong) {
				t.Fatalf("fixture carries a strong provider phrase, so it would classify without the anchor and asserts nothing: %q", tc.stderr)
			}

			_, ok := classifier.Classify(AccountProviderClaude,
				&OneShotExecutionError{Command: "claude", Stderr: tc.stderr})
			if !ok {
				t.Errorf("Classify() = false; a genuine provider limit stopped being recognised: %q", tc.stderr)
			}
		})
	}
}
