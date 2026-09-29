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
	"net/http"
	"strings"
	"time"
)

// =============================================================================
// The provider's limit reply, read from the CLI's own structured output
// =============================================================================
//
// THE CLI SAYS SO IN STRUCTURE BEFORE IT SAYS SO IN PROSE. When a subscription
// account is spent, Claude Code ends the turn with the text
//
//	You've hit your session limit · resets 9pm (America/New_York)
//
// and, alongside it, three structured signals, read from the 2.1.280 binary's
// own message schema:
//
//   - a `rate_limit_event` message whose `rate_limit_info.status` is
//     "rejected", with `resetsAt` in epoch seconds;
//   - an assistant message whose `error` is the category "rate_limit" (the
//     CLI's own mapping of the API failure);
//   - `api_error_status` on the result (and the assistant message), the HTTP
//     status of the API error that ended the turn: 429.
//
// They are read in that order, and the text only when none of them is
// present. account_limit_classifier.go explains why text is a heuristic; the
// structured signals are not, which is why they win.
//
// THE CLASSIFICATION HAPPENS WHERE THE PROCESS RAN. Only the backend that
// read the stream saw the events and the result text. So it decides once,
// attaches the verdict to the turn's TurnFailureEvidence, and the verdict
// crosses the daemon wire with the rest of that evidence
// (turn_failure_wire.go). The control plane never has to guess from an error
// string that no longer contains the text.

// ProviderLimitSource names which signal established a provider limit.
type ProviderLimitSource string

// The sources, strongest first.
const (
	// ProviderLimitSourceRateLimitEvent is a rate_limit_event reporting
	// status "rejected".
	ProviderLimitSourceRateLimitEvent ProviderLimitSource = "rate_limit_event"

	// ProviderLimitSourceErrorCategory is an assistant message whose `error`
	// category is a limit (rate_limit, billing_error).
	ProviderLimitSourceErrorCategory ProviderLimitSource = "error_category"

	// ProviderLimitSourceHTTPStatus is an API error status of 429.
	ProviderLimitSourceHTTPStatus ProviderLimitSource = "http_status"

	// ProviderLimitSourceResultText is the text fallback: the classifier's
	// phrase rules matched the result text.
	ProviderLimitSourceResultText ProviderLimitSource = "result_text"

	// ProviderLimitSourceUnclassified is a source a newer daemon named that
	// this build does not know. The limit still stands; only the name is not
	// echoed.
	ProviderLimitSourceUnclassified ProviderLimitSource = "unclassified"
)

// knownProviderLimitSources is the allowlist a source read off the wire is
// checked against. The wire value is a key; the rendered value is this file's.
var knownProviderLimitSources = map[ProviderLimitSource]ProviderLimitSource{
	ProviderLimitSourceRateLimitEvent: ProviderLimitSourceRateLimitEvent,
	ProviderLimitSourceErrorCategory:  ProviderLimitSourceErrorCategory,
	ProviderLimitSourceHTTPStatus:     ProviderLimitSourceHTTPStatus,
	ProviderLimitSourceResultText:     ProviderLimitSourceResultText,
}

// limitErrorCategories are the CLI's assistant `error` categories that mean
// THIS ACCOUNT cannot serve. "rate_limit" is the subscription window; the CLI
// renders "billing_error" as "usage limit reached — check plan". Every other
// category (overloaded, server_error, authentication_failed, ...) is either
// provider-wide or not a limit, and another account does not fix it.
var limitErrorCategories = map[string]bool{
	"rate_limit":    true,
	"billing_error": true,
}

// ProviderLimitEvidence is a backend's verdict that the provider refused the
// account for a rate or usage limit.
type ProviderLimitEvidence struct {
	// Source is the signal that established the limit.
	Source ProviderLimitSource

	// ResetsAt is when the provider said the limit lifts, zero when it did not
	// say. A caller applies DefaultAccountLimitBackoff to a zero.
	ResetsAt time.Time

	// Signal is the provider's own refusal text, redacted and truncated by
	// redactLimitSignal, or empty when there was none. It is what an operator
	// reads on the account's last_error.
	Signal string
}

// canonical returns the evidence with its source mapped through the
// allowlist, so a source read off the wire is never rendered verbatim.
func (e *ProviderLimitEvidence) canonical() *ProviderLimitEvidence {
	if e == nil {
		return nil
	}
	out := *e
	if known, ok := knownProviderLimitSources[e.Source]; ok {
		out.Source = known
	} else {
		out.Source = ProviderLimitSourceUnclassified
	}
	out.Signal = redactLimitSignal(e.Signal)
	return &out
}

// streamJSONRateLimitInfo is the payload of a `rate_limit_event` message.
// resetsAt is epoch SECONDS; it is decoded as a float so a CLI that emits a
// fractional value is still read rather than dropping the whole line.
type streamJSONRateLimitInfo struct {
	Status   string  `json:"status"`
	ResetsAt float64 `json:"resetsAt,omitempty"`
}

// rateLimitRejected is the rate_limit_info status that means the request was
// refused. "allowed" and "allowed_warning" are informational.
const rateLimitRejected = "rejected"

// streamLimitSignals accumulates the structured limit signals of one turn as
// its stream is read.
type streamLimitSignals struct {
	rejected       bool
	resetsAt       time.Time
	errorCategory  string
	apiErrorStatus int
}

// observe records whatever limit signal msg carries.
func (s *streamLimitSignals) observe(msg *streamJSONMessage) {
	if msg == nil {
		return
	}
	if msg.Type == "rate_limit_event" && msg.RateLimitInfo != nil {
		if msg.RateLimitInfo.Status == rateLimitRejected {
			s.rejected = true
			if msg.RateLimitInfo.ResetsAt > 0 {
				s.resetsAt = time.Unix(int64(msg.RateLimitInfo.ResetsAt), 0).UTC()
			}
		}
		return
	}
	if category := msg.errorCategory(); category != "" {
		s.errorCategory = category
	}
	if msg.APIErrorStatus != 0 {
		s.apiErrorStatus = msg.APIErrorStatus
	}
}

// evidence returns the provider-limit verdict for a turn that ended in error,
// or nil when nothing says the provider refused the account.
//
// resultText is the CLI's result text, consulted for the reset time and, when
// no structured signal fired, as the text fallback.
func (s *streamLimitSignals) evidence(provider AccountProvider, resultText string, now time.Time) *ProviderLimitEvidence {
	var source ProviderLimitSource
	switch {
	case s.rejected:
		source = ProviderLimitSourceRateLimitEvent
	case limitErrorCategories[s.errorCategory]:
		source = ProviderLimitSourceErrorCategory
	case s.apiErrorStatus == http.StatusTooManyRequests:
		source = ProviderLimitSourceHTTPStatus
	default:
		if _, limited := classifyLimitText(provider, resultText); !limited {
			return nil
		}
		source = ProviderLimitSourceResultText
	}

	evidence := &ProviderLimitEvidence{Source: source, Signal: redactLimitSignal(resultText)}
	evidence.ResetsAt = s.resetsAt
	if evidence.ResetsAt.IsZero() {
		if wait := parseLimitReset(resultText, now); wait > 0 {
			evidence.ResetsAt = now.Add(wait)
		}
	}
	return evidence
}

// errorCategory returns the CLI's error category on an assistant message, or
// empty. The field is decoded lazily from raw JSON because other message types
// use the same key with other shapes, and a strict string field would make
// the whole line undecodable.
func (m *streamJSONMessage) errorCategory() string {
	if m.Type != "assistant" || len(m.Error) == 0 {
		return ""
	}
	var category string
	if err := json.Unmarshal(m.Error, &category); err != nil {
		return ""
	}
	return strings.TrimSpace(category)
}
