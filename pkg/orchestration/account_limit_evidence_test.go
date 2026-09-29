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
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// sessionLimitText is the provider's reply as the burn-in fleet saw it.
const sessionLimitText = "You've hit your session limit · resets 9pm (America/New_York)"

// decodeStreamLine parses one stream-json line the way the reader does.
func decodeStreamLine(t *testing.T, line string) *streamJSONMessage {
	t.Helper()
	msg := &streamJSONMessage{}
	if err := json.Unmarshal([]byte(line), msg); err != nil {
		t.Fatalf("decode %s: %v", line, err)
	}
	return msg
}

// =============================================================================
// Structured signals first, text only as a fallback
// =============================================================================

func TestStreamLimitSignals_Evidence(t *testing.T) {
	now := time.Date(2026, 9, 22, 14, 0, 0, 0, time.UTC) // 10:00 in New York
	resets := time.Date(2026, 9, 22, 19, 30, 0, 0, time.UTC)
	cases := []struct {
		name       string
		lines      []string
		result     string
		wantSource ProviderLimitSource
		wantResets time.Time
	}{
		{
			name: "a rejected rate_limit_event names the reset",
			lines: []string{
				`{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":` +
					itoa64(resets.Unix()) + `,"rateLimitType":"five_hour"}}`,
			},
			result:     "API Error",
			wantSource: ProviderLimitSourceRateLimitEvent,
			wantResets: resets,
		},
		{
			name:       "an assistant rate_limit category",
			lines:      []string{`{"type":"assistant","error":"rate_limit","message":{"role":"assistant"}}`},
			result:     sessionLimitText,
			wantSource: ProviderLimitSourceErrorCategory,
			// 9pm New York is 01:00 UTC the next day.
			wantResets: time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC),
		},
		{
			name:       "a billing_error category",
			lines:      []string{`{"type":"assistant","error":"billing_error","message":{"role":"assistant"}}`},
			wantSource: ProviderLimitSourceErrorCategory,
		},
		{
			name:       "an HTTP 429 on the result",
			lines:      []string{`{"type":"result","is_error":true,"api_error_status":429}`},
			wantSource: ProviderLimitSourceHTTPStatus,
		},
		{
			name:       "the text alone, when nothing structured fired",
			result:     sessionLimitText,
			wantSource: ProviderLimitSourceResultText,
			wantResets: time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC),
		},
		{
			name:  "an allowed_warning event is informational",
			lines: []string{`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning","resetsAt":1}}`},
		},
		{
			name:  "an overloaded category is provider-wide, not this account",
			lines: []string{`{"type":"assistant","error":"overloaded","message":{"role":"assistant"}}`},
		},
		{
			name:  "a 529 is not a limit",
			lines: []string{`{"type":"result","is_error":true,"api_error_status":529}`},
		},
		{
			name:   "an unrelated failure",
			result: "Not logged in · Please run /login",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			signals := &streamLimitSignals{}
			for _, line := range tc.lines {
				signals.observe(decodeStreamLine(t, line))
			}
			got := signals.evidence(AccountProviderClaude, tc.result, now)
			if tc.wantSource == "" {
				if got != nil {
					t.Fatalf("evidence = %+v, want none", got)
				}
				return
			}
			if got == nil {
				t.Fatal("evidence = nil, want a provider limit")
			}
			if got.Source != tc.wantSource {
				t.Errorf("Source = %q, want %q", got.Source, tc.wantSource)
			}
			if !got.ResetsAt.Equal(tc.wantResets) {
				t.Errorf("ResetsAt = %s, want %s", got.ResetsAt, tc.wantResets)
			}
		})
	}
}

// TestStreamJSONMessage_AnErrorObjectDoesNotDropTheLine guards the decode: a
// message type that uses "error" for an object must still decode, or the
// reader would skip a line it needs.
func TestStreamJSONMessage_AnErrorObjectDoesNotDropTheLine(t *testing.T) {
	msg := decodeStreamLine(t, `{"type":"result","is_error":true,"result":"x","error":{"type":"overloaded_error"}}`)
	if msg.Type != "result" || !msg.IsError {
		t.Fatalf("decoded %+v", msg)
	}
	if msg.errorCategory() != "" {
		t.Fatalf("errorCategory = %q on a non-assistant message", msg.errorCategory())
	}
	assistant := decodeStreamLine(t, `{"type":"assistant","error":{"nested":true},"message":{"role":"assistant"}}`)
	if assistant.errorCategory() != "" {
		t.Fatalf("errorCategory = %q for a non-string error", assistant.errorCategory())
	}
}

// =============================================================================
// The wall-clock reset
// =============================================================================

func TestParseResetClock(t *testing.T) {
	// 14:00 UTC is 10:00 in New York (EDT) and 16:00 in Berlin (CEST).
	now := time.Date(2026, 9, 22, 14, 0, 0, 0, time.UTC)
	cases := []struct {
		text string
		want time.Duration
	}{
		{sessionLimitText, 11 * time.Hour},
		{"resets 9:30am (Europe/Berlin)", 17*time.Hour + 30*time.Minute},
		{"5-hour limit reached ∙ resets 3PM (America/New_York)", 5 * time.Hour},
		// Already past today in that zone: the next occurrence is tomorrow.
		{"resets 8am (America/New_York)", 22 * time.Hour},
		{"resets Sep 23, 11am (UTC)", 21 * time.Hour},
		// An explicit date already past is next year's, which the clamp refuses.
		{"resets Sep 22, 11am (UTC)", 0},
		// A weekly reset beyond the clamp is not honoured as stated.
		{"resets Sep 29, 10am (UTC)", 0},
		{"resets 9pm", 0},
		{"resets 9pm (Not/AZone)", 0},
		{"resets 13pm (UTC)", 0},
		{"resets 9:75pm (UTC)", 0},
		{"no reset here", 0},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			if got := parseResetClock(tc.text, now); got != tc.want {
				t.Fatalf("parseResetClock = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestParseLimitReset_PrefersAMachineReadableHint(t *testing.T) {
	now := time.Date(2026, 9, 22, 14, 0, 0, 0, time.UTC)
	if got := parseLimitReset("retry-after: 120 · resets 9pm (America/New_York)", now); got != 2*time.Minute {
		t.Fatalf("parseLimitReset = %s, want 2m", got)
	}
	if got := parseLimitReset("", now); got != 0 {
		t.Fatalf("parseLimitReset(empty) = %s, want 0", got)
	}
}

// =============================================================================
// The classifier reads the backend's verdict first
// =============================================================================

func TestClassify_StructuredEvidenceOutranksText(t *testing.T) {
	resets := time.Now().UTC().Add(3 * time.Hour).Truncate(time.Second)
	err := &RemoteTurnFailureError{Op: "Prompt", Err: errors.New("transport"), TurnFailureEvidence: TurnFailureEvidence{
		TerminalReason: TerminalReasonAPIError,
		ProviderLimit:  &ProviderLimitEvidence{Source: ProviderLimitSourceRateLimitEvent, ResetsAt: resets, Signal: sessionLimitText},
	}}
	signal, limited := NewAccountLimitClassifier().Classify(AccountProviderClaude, err)
	if !limited {
		t.Fatal("a structured limit was not classified")
	}
	if signal.Matched != sessionLimitText {
		t.Errorf("Matched = %q, want the provider's own words", signal.Matched)
	}
	if signal.RetryAfter <= 2*time.Hour || signal.RetryAfter > 3*time.Hour {
		t.Errorf("RetryAfter = %s, want about 3h", signal.RetryAfter)
	}
}

func TestClassify_StructuredEvidenceWithNoTextStillNamesTheSource(t *testing.T) {
	err := &RemoteTurnFailureError{Op: "Prompt", TurnFailureEvidence: TurnFailureEvidence{
		ProviderLimit: &ProviderLimitEvidence{Source: ProviderLimitSourceHTTPStatus},
	}}
	signal, limited := NewAccountLimitClassifier().Classify(AccountProviderClaude, err)
	if !limited || !strings.Contains(signal.Matched, string(ProviderLimitSourceHTTPStatus)) {
		t.Fatalf("Classify = (%+v, %v), want a limit naming http_status", signal, limited)
	}
	if signal.RetryAfter != 0 {
		t.Errorf("RetryAfter = %s, want 0 so the default window applies", signal.RetryAfter)
	}
}

func TestClassify_TheSessionLimitTextIsALimit(t *testing.T) {
	err := &PromptError{Cause: &StreamJSONError{Command: "claude", Result: sessionLimitText}}
	signal, limited := NewAccountLimitClassifier().Classify(AccountProviderClaude, err)
	if !limited {
		t.Fatal("the session-limit reply was not classified as a limit")
	}
	if signal.RetryAfter <= 0 {
		t.Errorf("RetryAfter = %s, want the parsed reset", signal.RetryAfter)
	}
}

// TestClassify_AHeldLimitKeepsItsInstant: rotation re-classifies an error the
// turn path already held; the second hold must land on the same instant.
func TestClassify_AHeldLimitKeepsItsInstant(t *testing.T) {
	held := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	signal, limited := NewAccountLimitClassifier().Classify(AccountProviderClaude,
		&TurnAccountLimitedError{Limit: &AccountLimitedError{AccountID: "a", AvailableAt: held}})
	if !limited {
		t.Fatal("a held limit was not classified")
	}
	if got := signal.AvailableAt(time.Now().UTC(), 0); !got.Equal(held) {
		t.Fatalf("AvailableAt = %s, want the original %s", got, held)
	}
}

func TestAccountLimitSignal_AWeeklyInstantIsHonoured(t *testing.T) {
	now := time.Date(2026, 9, 22, 14, 0, 0, 0, time.UTC)
	weekly := now.Add(7 * 24 * time.Hour)
	if got := (AccountLimitSignal{ResetsAt: weekly}).AvailableAt(now, 0); !got.Equal(weekly) {
		t.Fatalf("AvailableAt = %s, want the stated weekly reset %s", got, weekly)
	}
}

func TestAccountLimitSignal_AnInstantBeyondTheClampIsNotHonoured(t *testing.T) {
	now := time.Date(2026, 9, 22, 14, 0, 0, 0, time.UTC)
	signal := AccountLimitSignal{ResetsAt: now.Add(MaxProviderLimitReset + time.Hour)}
	if got := signal.AvailableAt(now, 0); !got.Equal(now.Add(DefaultAccountLimitBackoff)) {
		t.Fatalf("AvailableAt = %s, want the default window", got)
	}
}

func TestProviderLimitSignal_KeepsAWeeklyReset(t *testing.T) {
	now := time.Date(2026, 9, 22, 14, 0, 0, 0, time.UTC)
	weekly := now.Add(5 * 24 * time.Hour)
	signal := providerLimitSignal(&ProviderLimitEvidence{ResetsAt: weekly}, now)
	if !signal.ResetsAt.Equal(weekly) || signal.RetryAfter != 5*24*time.Hour {
		t.Fatalf("signal = %+v, want the weekly reset kept", signal)
	}
	if beyond := providerLimitSignal(&ProviderLimitEvidence{ResetsAt: now.Add(MaxProviderLimitReset + time.Hour)}, now); !beyond.ResetsAt.IsZero() || beyond.RetryAfter != 0 {
		t.Fatalf("signal = %+v, want a reset past the bound dropped", beyond)
	}
}

func TestAccountLimitSignal_DefaultWindowIsFiveHours(t *testing.T) {
	now := time.Date(2026, 9, 22, 14, 0, 0, 0, time.UTC)
	if got := (AccountLimitSignal{}).AvailableAt(now, 0); !got.Equal(now.Add(5 * time.Hour)) {
		t.Fatalf("AvailableAt = %s, want five hours on", got)
	}
}

// =============================================================================
// The verdict crosses the daemon wire
// =============================================================================

func TestTurnFailureEvidence_ProviderLimitRoundTrip(t *testing.T) {
	resets := time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC)
	in := TurnFailureEvidence{
		TerminalReason: TerminalReasonAPIError,
		ProviderLimit: &ProviderLimitEvidence{
			Source:   ProviderLimitSourceErrorCategory,
			ResetsAt: resets,
			Signal:   sessionLimitText + " bearer abcdefghijklmnopqrstuvwxyz",
		},
	}
	onTheWire := AttachTurnFailureEvidence(status.Error(codes.Internal, "turn failed"), &StreamJSONError{TurnFailureEvidence: in})
	out, ok := TurnFailureEvidenceFromStatus(onTheWire)
	if !ok || out.ProviderLimit == nil {
		t.Fatalf("evidence lost on the wire: %+v", out)
	}
	if out.ProviderLimit.Source != ProviderLimitSourceErrorCategory || !out.ProviderLimit.ResetsAt.Equal(resets) {
		t.Errorf("ProviderLimit = %+v", out.ProviderLimit)
	}
	if strings.Contains(out.ProviderLimit.Signal, "abcdefghijklmnop") {
		t.Errorf("Signal %q crossed the wire unredacted", out.ProviderLimit.Signal)
	}
}

func TestTurnFailureEvidence_AnUnknownSourceIsStillALimit(t *testing.T) {
	proto := turnFailureEvidenceToProto(TurnFailureEvidence{TerminalReason: TerminalReasonAPIError})
	proto.ProviderLimitSource = "a_future_signal"
	out, ok := turnFailureEvidenceFromProto(proto)
	if !ok || out.ProviderLimit == nil {
		t.Fatal("an unknown source dropped the limit")
	}
	if out.ProviderLimit.Source != ProviderLimitSourceUnclassified {
		t.Fatalf("Source = %q, want it renamed, never echoed", out.ProviderLimit.Source)
	}
}

func TestTurnFailureEvidence_NoSourceIsNoLimit(t *testing.T) {
	out, ok := turnFailureEvidenceFromProto(turnFailureEvidenceToProto(TurnFailureEvidence{TerminalReason: TerminalReasonAPIError}))
	if !ok || out.ProviderLimit != nil {
		t.Fatalf("ProviderLimit = %+v, want nil", out.ProviderLimit)
	}
}

// =============================================================================
// A remote turn's usage crosses the wire
// =============================================================================

func TestPromptUsageWire_RoundTrip(t *testing.T) {
	usage := &TokenUsage{InputTokens: 1200, OutputTokens: 300, CacheReadInputTokens: 40}
	data, err := PromptUsageToWire(usage)
	if err != nil {
		t.Fatalf("PromptUsageToWire: %v", err)
	}
	got, err := promptUsageFromWire(data)
	if err != nil {
		t.Fatalf("promptUsageFromWire: %v", err)
	}
	if got.Total() != usage.Total() || got.InputTokens != 1200 {
		t.Fatalf("round trip = %+v, want %+v", got, usage)
	}
}

func TestPromptUsageWire_NothingReportedStaysNothing(t *testing.T) {
	data, err := PromptUsageToWire(nil)
	if err != nil || data != nil {
		t.Fatalf("PromptUsageToWire(nil) = (%v, %v), want nothing", data, err)
	}
	got, err := promptUsageFromWire(nil)
	if err != nil || got != nil {
		t.Fatalf("promptUsageFromWire(nil) = (%v, %v), want nil", got, err)
	}
}

func TestPromptUsageWire_MalformedIsAnError(t *testing.T) {
	_, err := promptUsageFromWire([]byte("{not json"))
	var wireErr *TurnUsageWireError
	if !errors.As(err, &wireErr) || wireErr.Op != "decode" {
		t.Fatalf("err = %v, want a decode TurnUsageWireError", err)
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error %q should name the operation", err)
	}
}
