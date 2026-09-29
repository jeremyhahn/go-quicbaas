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
	"context"
	"errors"
	"strconv"
	"time"
)

// =============================================================================
// The quota authority
// =============================================================================
//
// ONE place answers "may this account serve more work right now", and it
// answers from state every host can see. Before this, the question was answered
// twice and locally: the resolver checked the account's limit hold, and nothing
// checked consumption at all. Two hosts could therefore each believe they were
// under a cap that neither could measure.
//
// The authority composes two durable inputs and invents no third:
//
//   - the CONSUMPTION LEDGER, which is the cross-host record of what has
//     actually been spent, summed over the window a policy names;
//   - the ACCOUNT's own limit hold (Account.LimitHoldActive), which is what the
//     provider told us when it refused a turn, already durable and already
//     shared through AccountStore.
//
// Neither is re-derived here. The hold is read through the method that already
// defines it, so a change to what "held" means cannot leave this file
// disagreeing with the resolver about the same account.
//
// FAIL CLOSED, AND NEVER WITH A NUMBER. Every failure path returns an error and
// NO decision. A quota check that returned "allowed, 0 spent" when it could not
// read the ledger is the exact fallback this package refuses: it reports a
// healthy account with unlimited headroom, and the first evidence is the
// invoice. A caller that ignores the error gets a nil decision and a nil
// dereference, which is a louder failure than an over-spend.

// QuotaReason names what decided an admission.
type QuotaReason string

const (
	// QuotaReasonWithinLimits admits: no window is spent and no hold is live.
	QuotaReasonWithinLimits QuotaReason = "within_limits"

	// QuotaReasonProviderHold refuses because the provider itself refused this
	// account and the hold has not lapsed.
	QuotaReasonProviderHold QuotaReason = "provider_hold"

	// QuotaReasonTokenLimit refuses because a window's token allowance is spent.
	QuotaReasonTokenLimit QuotaReason = "token_limit"

	// QuotaReasonCostLimit refuses because a window's dollar allowance —
	// measured as the list-price equivalent of the priced turns in it — is
	// spent.
	QuotaReasonCostLimit QuotaReason = "cost_limit"
)

// Quota axes, as named in a decision and in every refusal built from one.
const (
	// QuotaAxisTokens meters every token the provider metered.
	QuotaAxisTokens = "tokens"

	// QuotaAxisEquivalentMicros meters the list-price equivalent, in
	// millionths of a US dollar, of the PRICED turns in the window.
	QuotaAxisEquivalentMicros = "equivalent_micros"
)

// QuotaWindow is one metered allowance interval.
//
// SLIDING, not tumbling: the window is [now-Size, now), recomputed on every
// question. That is how the plans this models actually meter — a "5-hour limit"
// frees up gradually as old turns age out, not all at once on a clock boundary.
// A tumbling window would admit a full allowance the instant the boundary
// passed, which is precisely the burst the provider is metering against.
type QuotaWindow struct {
	// Name identifies the window in decisions and errors ("five-hour",
	// "weekly"). Required: a refusal that cannot say which limit it hit is
	// not actionable.
	Name string `json:"name"`

	// Size is the window's length. Required and positive.
	Size time.Duration `json:"size"`

	// MaxTokens is the window's allowance in tokens, and MaxEquivalentMicros
	// its allowance in millionths of a US dollar at list price. Each is
	// optional and zero means "this axis is not metered", but a window must
	// meter at least one: a window that meters nothing can only ever say yes,
	// and its presence in a policy would read as a limit that is silently
	// absent. When both are set, both are limits and the first spent one
	// refuses.
	//
	// THE DOLLAR AXIS METERS PRICED TURNS ONLY. It sums the ledger's
	// EquivalentMicros, which is stamped from the operator's price table
	// (PriceBook) when a turn is recorded; a turn on a model the table does
	// not price carries no figure and does not move a dollar window. That is
	// the honest reading of "a $X limit" — an operator who wants every turn
	// counted prices every model, or caps tokens as well. go-quicbaas#791.
	MaxTokens           int64 `json:"max_tokens,omitempty"`
	MaxEquivalentMicros int64 `json:"max_equivalent_micros,omitempty"`
}

// SpendLimits is the set of sliding windows a spender is metered by. It is the
// one limits type (go-quicbaas#1058, ruling F5): an account carries it as
// AccountSpendPolicy.Quota, a project as Project.Spend, and a tenant as its
// TenantSpendPolicy.
//
// Windows are evaluated IN ORDER and the first refusal is returned, so a policy
// listing its tightest window first produces the most immediately useful
// answer. Every window is still a hard limit; the ordering decides only which
// one a refusal names.
type SpendLimits struct {
	Windows []QuotaWindow `json:"windows"`
}

// QuotaDecision is the authority's answer.
type QuotaDecision struct {
	Allowed bool        `json:"allowed"`
	Reason  QuotaReason `json:"reason"`

	// Window names the window that decided, empty for a hold or an admission
	// against a policy with no metered window.
	Window string `json:"window,omitempty"`

	// Axis names which of the window's allowances decided a refusal:
	// QuotaAxisTokens or QuotaAxisEquivalentMicros.
	Axis string `json:"axis,omitempty"`

	// Used, Limit and Remaining describe the deciding axis. Remaining is
	// clamped at zero: a negative remainder is an over-spend that already
	// happened, and reporting it as a negative number invites arithmetic that
	// treats it as headroom in the other direction.
	Used      int64 `json:"used"`
	Limit     int64 `json:"limit"`
	Remaining int64 `json:"remaining"`

	// RetryAt is when the decision would change. For a provider hold it is
	// the account's own AvailableAt; for a spent window it is when the oldest
	// entry in that window ages out. Zero when the answer is "now".
	RetryAt time.Time `json:"retry_at,omitempty"`
}

// QuotaAuthority decides whether an account may serve more consumption.
type QuotaAuthority interface {
	// Admit answers for one account at one instant. It returns an error and
	// NO decision whenever it could not establish the answer.
	Admit(ctx context.Context, req QuotaAdmitRequest, now time.Time) (*QuotaDecision, error)

	// AdmitScope answers for a project or a tenant: whether its own limits
	// admit more consumption, from the project or tenant quota plane. It
	// returns an error and NO decision whenever it could not establish the
	// answer.
	AdmitScope(ctx context.Context, req ScopeAdmitRequest, now time.Time) (*QuotaDecision, error)
}

// ScopeAdmitRequest is one admission question about a project or a tenant.
type ScopeAdmitRequest struct {
	// TenantID is required. ProjectID, when set, asks about that project;
	// AgentID, when set, about that tenant Agent; neither asks about the
	// whole tenant. Naming both is refused: a window belongs to one spender.
	TenantID  string
	ProjectID string
	AgentID   string

	// Limits are the scope's windows. None admits.
	Limits SpendLimits

	// EstimatedTokens is what the caller is about to spend; see
	// QuotaAdmitRequest.EstimatedTokens.
	EstimatedTokens int64
}

// QuotaAdmitRequest is one admission question.
type QuotaAdmitRequest struct {
	// Account is the pooled account being asked about. Required. It is passed
	// in rather than loaded here so the caller's already-normalized record is
	// the one judged — reloading it would let this decision disagree with the
	// resolver's about an account whose hold lapsed between the two reads.
	Account *Account

	// Policy is the account's allowances. A policy with no windows meters
	// nothing and admits on the ledger axis, which is the honest answer for
	// an account nobody has configured a limit for.
	Policy SpendLimits

	// EstimatedTokens is what the caller is about to spend. It is added to
	// the window's usage before the comparison, so a turn that would cross
	// the limit is refused BEFORE it runs rather than after. Zero asks only
	// whether the account is already over.
	EstimatedTokens int64
}

// quotaAuthority is the ledger-backed QuotaAuthority.
type quotaAuthority struct {
	ledger ConsumptionLedger
}

// Compile-time interface satisfaction check.
var _ QuotaAuthority = (*quotaAuthority)(nil)

// NewQuotaAuthority returns the QuotaAuthority backed by ledger.
func NewQuotaAuthority(ledger ConsumptionLedger) QuotaAuthority {
	return &quotaAuthority{ledger: ledger}
}

// Admit implements QuotaAuthority.
func (a *quotaAuthority) Admit(ctx context.Context, req QuotaAdmitRequest, now time.Time) (*QuotaDecision, error) {
	if a.ledger == nil {
		return nil, &QuotaInvalidError{Field: "ledger", Message: "must not be nil"}
	}
	if req.Account == nil {
		return nil, &QuotaInvalidError{Field: "account", Message: "must not be nil"}
	}
	if req.EstimatedTokens < 0 {
		return nil, &QuotaInvalidError{Field: "estimated_tokens", Message: "must not be negative"}
	}
	if err := req.Policy.validate(); err != nil {
		return nil, err
	}

	// The provider's own refusal outranks anything computed. Our arithmetic
	// says what we believe was spent; the hold says what the provider will
	// accept, and only one of those two is authoritative about the next call.
	if req.Account.LimitHoldActive(now) {
		return &QuotaDecision{
			Allowed: false,
			Reason:  QuotaReasonProviderHold,
			RetryAt: req.Account.AvailableAt,
		}, nil
	}

	account := req.Account
	return evaluateWindows(req.Policy, req.EstimatedTokens, now, func(from, to time.Time) (*ConsumptionRollup, error) {
		return a.ledger.WindowUsage(ctx, account.TenantID, account.ID, from, to)
	})
}

// AdmitScope implements QuotaAuthority.
func (a *quotaAuthority) AdmitScope(ctx context.Context, req ScopeAdmitRequest, now time.Time) (*QuotaDecision, error) {
	if a.ledger == nil {
		return nil, &QuotaInvalidError{Field: "ledger", Message: "must not be nil"}
	}
	if req.TenantID == "" {
		return nil, &QuotaInvalidError{Field: "tenant_id", Message: "must not be empty"}
	}
	if req.EstimatedTokens < 0 {
		return nil, &QuotaInvalidError{Field: "estimated_tokens", Message: "must not be negative"}
	}
	if req.ProjectID != "" && req.AgentID != "" {
		return nil, &QuotaInvalidError{Field: "agent_id", Message: "a window belongs to a project or an Agent, not both"}
	}
	if err := req.Limits.validate(); err != nil {
		return nil, err
	}
	read := func(from, to time.Time) (*ConsumptionRollup, error) {
		return a.ledger.TenantWindowUsage(ctx, req.TenantID, from, to)
	}
	switch {
	case req.ProjectID != "":
		read = func(from, to time.Time) (*ConsumptionRollup, error) {
			return a.ledger.ProjectWindowUsage(ctx, req.TenantID, req.ProjectID, from, to)
		}
	case req.AgentID != "":
		read = func(from, to time.Time) (*ConsumptionRollup, error) {
			return a.ledger.AgentWindowUsage(ctx, req.TenantID, req.AgentID, from, to)
		}
	}
	return evaluateWindows(req.Limits, req.EstimatedTokens, now, read)
}

// windowReader sums a spender's consumption in [from, to).
type windowReader func(from, to time.Time) (*ConsumptionRollup, error)

// evaluateWindows answers for a set of windows IN ORDER, returning the first
// refusal. It is the one comparison every spender — account, project,
// tenant — is judged by; only where the usage is read from differs.
func evaluateWindows(limits SpendLimits, estimatedTokens int64, now time.Time, read windowReader) (*QuotaDecision, error) {
	for _, window := range limits.Windows {
		usage, err := read(now.Add(-window.Size), now)
		if err != nil {
			// No decision. See the fail-closed note at the top of this file.
			return nil, err
		}
		// A zero allowance is an axis the window does not meter; the policy's
		// validation guarantees at least one is positive.
		if window.MaxTokens > 0 && usage.TotalTokens+estimatedTokens > window.MaxTokens {
			decision := quotaRefusal(QuotaReasonTokenLimit, window, usage.TotalTokens, window.MaxTokens, usage.FirstAt, now)
			decision.Axis = QuotaAxisTokens
			return decision, nil
		}
		// The dollar axis refuses once the allowance is SPENT, not when an
		// estimate would cross it: a turn's price is not known before it runs.
		if window.MaxEquivalentMicros > 0 && usage.EquivalentMicros >= window.MaxEquivalentMicros {
			decision := quotaRefusal(QuotaReasonCostLimit, window, usage.EquivalentMicros, window.MaxEquivalentMicros, usage.FirstAt, now)
			decision.Axis = QuotaAxisEquivalentMicros
			return decision, nil
		}
	}
	return &QuotaDecision{Allowed: true, Reason: QuotaReasonWithinLimits}, nil
}

// quotaRefusal builds a refusing decision, including when the window next
// frees up.
func quotaRefusal(reason QuotaReason, window QuotaWindow, used, limit int64, oldest, now time.Time) *QuotaDecision {
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}
	// A sliding window frees up when its oldest entry ages out. With no
	// entries there is nothing to age out, so the caller is told to retry now
	// rather than being handed a zero it has to interpret.
	//
	// The IsZero check is DELIBERATELY REDUNDANT with the staleness suppression
	// below, and mutation testing says so: replacing it with `true` survives,
	// because a zero time plus any window length is still in the past and the
	// suppression zeroes it anyway. The two conditions coincide only by an
	// accident of where the epoch sits, not by design — remove the suppression
	// later and this guard becomes load-bearing again. It is kept because it
	// states the actual reason (there is nothing to age out) at the point where
	// a reader asks, and the surviving mutation is recorded here rather than
	// left as an unexplained gap in the score.
	retryAt := time.Time{}
	if !oldest.IsZero() {
		retryAt = oldest.Add(window.Size)
		if !retryAt.After(now) {
			retryAt = time.Time{}
		}
	}
	return &QuotaDecision{
		Allowed:   false,
		Reason:    reason,
		Window:    window.Name,
		Used:      used,
		Limit:     limit,
		Remaining: remaining,
		RetryAt:   retryAt,
	}
}

// Validate refuses limits that cannot mean what they appear to. Every write
// of an account, project or tenant policy calls it, so a malformed window is
// refused where the operator wrote it rather than discovered at the gate.
func (p SpendLimits) Validate() error {
	return p.validate()
}

// Metered reports whether the limits meter anything.
func (p SpendLimits) Metered() bool {
	return len(p.Windows) > 0
}

// validate refuses a policy that cannot mean what it appears to.
func (p SpendLimits) validate() error {
	seen := make(map[string]struct{}, len(p.Windows))
	for _, window := range p.Windows {
		if window.Name == "" {
			return &QuotaInvalidError{Field: "window.name", Message: "must not be empty"}
		}
		if _, duplicate := seen[window.Name]; duplicate {
			return &QuotaInvalidError{
				Field:   "window.name",
				Message: "duplicated: " + window.Name + "; a refusal must name exactly one window",
			}
		}
		seen[window.Name] = struct{}{}

		if window.Size <= 0 {
			return &QuotaInvalidError{
				Field:   "window.size",
				Message: window.Name + ": must be positive",
			}
		}
		if window.MaxTokens < 0 {
			return &QuotaInvalidError{Field: "window.max_tokens", Message: window.Name + ": must not be negative"}
		}
		if window.MaxEquivalentMicros < 0 {
			return &QuotaInvalidError{Field: "window.max_equivalent_micros", Message: window.Name + ": must not be negative"}
		}
		// A window with no allowance can only ever admit. Listed in a policy
		// it reads as a limit, so it is refused rather than kept as a limit
		// that silently is not one.
		if window.MaxTokens == 0 && window.MaxEquivalentMicros == 0 {
			return &QuotaInvalidError{
				Field:   "window.max_tokens",
				Message: window.Name + ": set max_tokens, max_equivalent_micros or both; a window that meters nothing is not a limit",
			}
		}
	}
	return nil
}

// =============================================================================
// Errors
// =============================================================================

// ErrQuotaInvalid indicates an admission question this authority refuses to
// answer, as distinct from an account it refuses to admit. The two must stay
// distinguishable: a caller that treated a malformed policy as a refusal would
// park a healthy account, and one that treated it as an admission would run
// unmetered.
var ErrQuotaInvalid = errors.New("quota: invalid admission request")

// QuotaInvalidError reports a request or policy that cannot be evaluated.
type QuotaInvalidError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *QuotaInvalidError) Error() string {
	return ErrQuotaInvalid.Error() + ": " + e.Field + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *QuotaInvalidError) Unwrap() error { return ErrQuotaInvalid }

// String renders a decision for logs and audit records.
func (d *QuotaDecision) String() string {
	if d == nil {
		return "quota: no decision"
	}
	base := "quota: " + string(d.Reason)
	if d.Window != "" {
		base += " (" + d.Window + ")"
	}
	if d.Limit > 0 {
		base += " used " + strconv.FormatInt(d.Used, 10) + " of " + strconv.FormatInt(d.Limit, 10)
	}
	return base
}
