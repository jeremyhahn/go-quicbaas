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
	"sort"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// The spend report — the READ side of the consumption ledger
// =============================================================================
//
// A PROJECTION, NOT A SECOND STORE, for the same reason ReadSessionHistory is
// one. Every figure here is derived on read from the ledger the turn recorder
// writes (account_ledger.go) and the account record the enforcement path
// judges (account_spend_policy.go). Keeping a rolled-up copy would create a
// second record of one account's spend that can disagree with the record a
// REFUSAL is decided from, and the dashboard would be the one that looked
// right.
//
// THE REPORT AND THE GATE READ THE SAME FACTS; THEY ANSWER DIFFERENT
// QUESTIONS. This reports what an allowance HAS LEFT; the quota authority
// decides whether a particular request FITS, which depends on what that
// request is about to spend. They cannot disagree about the spend, because
// there is one record of it. Where their answers differ — at used == max,
// where a zero-estimate spawn still fits — the difference is the question,
// not the data; see SpendWindowStatus.Exhausted.
//
// WHY THE OPERATOR SURFACE IS ONE VERB AND NOT FOUR. The chain is a path —
// tenant/project/task/job/session/round — so "the project's spend" and "the
// job's spend" are the same question asked at two depths, and the ledger
// answers both with one prefix scan. Four routes over one scan would be four
// places for the drill-down's arithmetic to stop agreeing with itself.
//
// WHAT THIS FILE REFUSES TO RENDER. A number the system does not have is
// ABSENT AND LABELLED, never a zero:
//
//   - COST. A turn on a model the price table does not price carries no
//     figure (go-quicbaas#791, #1058). Every total carries PricedEntries and
//     UnpricedEntries beside the money, and a reader that renders the number
//     without consulting them is reporting an unmeasured total as a measured
//     one. A subscription fee share is a pointer for the same reason: no fee
//     configured is absent, never zero.
//   - A SPEND POLICY NOBODY CONFIGURED. SpendPolicyNone says "this account
//     has no cap", which is a fact. It is not the same fact as
//     SpendPolicyUnknown, which says the deployment wires no account store so
//     nothing here could look. Collapsing the two would let an unwired
//     deployment report every account as unmetered.
//   - AN UNATTRIBUTED LEVEL. The ledger files a job with no task under the
//     consumptionUnattributed sentinel. A child row carrying it is marked
//     Unattributed so a reader says "no task" rather than inventing a task
//     whose id is "-".

// SpendUnattributed addresses the subtree of entries that have NO value at a
// chain level — a job that belongs to no task, a turn charged to no project.
//
// It is the ledger's own no-value sentinel, exported here because a reader
// that can SEE an unattributed row must be able to OPEN it. Supplying it as a
// scope level is unambiguous precisely because the write side refuses it as
// an entity id (see validateChainSegment): no real project, task, job or
// session can bear this identifier, so a scope naming it can only mean the
// unattributed subtree.
const SpendUnattributed = consumptionUnattributed

// SpendLevel names one level of the attribution chain.
type SpendLevel string

// The chain's levels, outermost first. They are the ledger's own vocabulary
// (see account_ledger.go), not a parallel hierarchy invented for reporting.
const (
	SpendLevelTenant  SpendLevel = "tenant"
	SpendLevelProject SpendLevel = "project"
	SpendLevelTask    SpendLevel = "task"
	SpendLevelJob     SpendLevel = "job"
	SpendLevelSession SpendLevel = "session"
	SpendLevelRound   SpendLevel = "round"
)

// spendLevelsByDepth maps a validated scope depth (1 = tenant) to the level
// the scope addresses. Indexed rather than switched so the depth arithmetic
// that produces it and the name it produces cannot drift apart.
var spendLevelsByDepth = []SpendLevel{
	SpendLevelTenant,
	SpendLevelProject,
	SpendLevelTask,
	SpendLevelJob,
	SpendLevelSession,
	SpendLevelRound,
}

// SpendPolicyStatus is what the report could establish about one account's
// allowance. Three values, because "no cap" and "could not look" are
// different facts and only one of them is reassuring.
type SpendPolicyStatus string

const (
	// SpendPolicyUnknown means nothing here could read the account: the
	// deployment wires no account store, or the record is gone. A reader
	// must say so rather than imply the account is unmetered.
	SpendPolicyUnknown SpendPolicyStatus = "unknown"

	// SpendPolicyNone means the account was read and meters nothing. This is
	// the honest "no spend policy" — not a budget of zero.
	SpendPolicyNone SpendPolicyStatus = "none"

	// SpendPolicyMetered means the account carries at least one quota window
	// or a breaker rule.
	SpendPolicyMetered SpendPolicyStatus = "metered"
)

// SpendWindowStatus is one quota window's allowance and what has been drawn
// against it, as of the instant the report was taken.
type SpendWindowStatus struct {
	// Name is the window's operator-facing name ("five-hour", "weekly").
	Name string `json:"name"`

	// Size is the window's length. The window is sliding: the figures below
	// describe [now-Size, now).
	Size time.Duration `json:"size"`

	// MaxTokens is the allowance; UsedTokens what the ledger says was spent
	// inside the window; RemainingTokens the difference, clamped at zero for
	// the same reason QuotaDecision.Remaining is — a negative remainder is an
	// over-spend that already happened, not headroom the other way.
	MaxTokens       int64 `json:"max_tokens"`
	UsedTokens      int64 `json:"used_tokens"`
	RemainingTokens int64 `json:"remaining_tokens"`

	// Exhausted says the window has NO ALLOWANCE LEFT: UsedTokens has reached
	// MaxTokens, so RemainingTokens is zero. Derived here rather than by each
	// reader, so two panels cannot draw the edge differently.
	//
	// AT USED == MAX THIS READS EXHAUSTED WHILE A SPAWN IS STILL ADMITTED,
	// and the difference is real rather than a disagreement. The quota
	// authority answers a different question — "may THIS request proceed",
	// parameterised by what it is about to spend — and refuses when
	// used + estimated > max (account_quota.go). A spawn passes an estimate
	// of ZERO, because a spawn honestly cannot predict a session, so at
	// exactly used == max the comparison is max > max and it is let through.
	// A turn that estimated even one token would be refused.
	//
	// The report therefore says what is true of the ALLOWANCE (none left)
	// while the gate says what is true of the REQUEST (a zero-token request
	// still fits). Making Exhausted mirror the gate instead would print
	// "0 remaining" next to "not exhausted", which is the more confusing of
	// the two readings and would also make Exhausted depend on an estimate
	// the report does not have.
	//
	// With a dollar axis the window is exhausted when EITHER metered axis has
	// no allowance left.
	Exhausted bool `json:"exhausted"`

	// MaxEquivalentMicros, UsedEquivalentMicros and RemainingEquivalentMicros
	// are the dollar axis, in millionths of a US dollar at list price, over
	// the window's PRICED turns. MaxEquivalentMicros is zero when the window
	// does not meter dollars, and so is MaxTokens when it does not meter
	// tokens.
	MaxEquivalentMicros       int64 `json:"max_equivalent_micros,omitempty"`
	UsedEquivalentMicros      int64 `json:"used_equivalent_micros"`
	RemainingEquivalentMicros int64 `json:"remaining_equivalent_micros"`

	// FreesAt is when the window's oldest entry ages out, which is the
	// earliest the figures above can improve without new spend. NIL when the
	// window holds nothing, because there is nothing to age out.
	//
	// A POINTER BECAUSE `omitempty` DOES NOT OMIT A ZERO time.Time. For a
	// struct field encoding/json treats nothing as empty, so a zero instant
	// serialises as "0001-01-01T00:00:00Z" — a real date a client renders as
	// January of year 1. That is exactly the invented figure this file
	// refuses everywhere else, arriving through the encoder instead of
	// through the arithmetic. A nil pointer IS omitted.
	FreesAt *time.Time `json:"frees_at,omitempty"`
}

// SpendAccount is one pooled account's contribution to a scope, and the
// allowance it draws against.
type SpendAccount struct {
	// AccountID is the account the provider billed.
	AccountID string `json:"account_id"`

	// DisplayName and Provider come from the account record, and are empty
	// when PolicyStatus is SpendPolicyUnknown — there was nothing to read
	// them from.
	DisplayName string `json:"display_name,omitempty"`
	Provider    string `json:"provider,omitempty"`

	// ConsumptionTotals is what this account spent inside the report's
	// scope: its tokens by dimension, their list-price equivalent and what
	// they added to the bill.
	ConsumptionTotals

	// Billing is how the account is billed, from its record; empty when
	// PolicyStatus is SpendPolicyUnknown.
	Billing AccountBilling `json:"billing,omitzero"`

	// SubscriptionFeeMicros is the part of a subscription's monthly fee that
	// falls in the report's range (AccountBilling.FeeShare), set only on a
	// TENANT-level report and only for a subscription whose fee is
	// configured. Nil otherwise, which a reader renders as a dash.
	//
	// ONLY AT THE TENANT LEVEL: one subscription serves every project, and
	// charging its whole fee to whichever project is being read would count
	// it once per project.
	SubscriptionFeeMicros *int64 `json:"subscription_fee_micros,omitempty"`

	// PolicyStatus says whether the allowance below is a fact, an absence,
	// or unknown. See SpendPolicyStatus.
	PolicyStatus SpendPolicyStatus `json:"policy_status"`

	// Windows is the account's quota windows with live usage against them.
	// Empty unless PolicyStatus is SpendPolicyMetered.
	Windows []SpendWindowStatus `json:"windows,omitempty"`

	// Breaker is the account's derived circuit-breaker verdict, present only
	// when the account carries a breaker rule. Nil means no rule, which is
	// not the same as a closed breaker.
	Breaker *SpendBreaker `json:"breaker,omitempty"`
}

// SpendBreaker is the report's wire shape for a BreakerVerdict.
//
// IT EXISTS FOR THE TIMESTAMPS. BreakerVerdict is the enforcement layer's own
// type (account_breaker.go) and carries its instants as time.Time, which is
// right for a decision made in process — but this report is the only thing
// that serialises one, and a zero time.Time is not omitted by `omitempty`. A
// closed breaker that has never failed would put "0001-01-01T00:00:00Z" on the
// wire as its last failure. Projecting into pointers here fixes that without
// changing the type the gate is decided from.
type SpendBreaker struct {
	State     BreakerState `json:"state"`
	Failures  int          `json:"failures"`
	Successes int          `json:"successes"`

	// Each instant is nil when it never happened, rather than the epoch.
	LastFailureAt *time.Time `json:"last_failure_at,omitempty"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`

	// OpenUntil is when an open breaker becomes half-open; nil otherwise.
	OpenUntil *time.Time `json:"open_until,omitempty"`
}

// spendBreakerOf projects a verdict onto the report's shape.
func spendBreakerOf(verdict *BreakerVerdict) *SpendBreaker {
	if verdict == nil {
		return nil
	}
	return &SpendBreaker{
		State:         verdict.State,
		Failures:      verdict.Failures,
		Successes:     verdict.Successes,
		LastFailureAt: optionalInstant(verdict.LastFailureAt),
		LastSuccessAt: optionalInstant(verdict.LastSuccessAt),
		OpenUntil:     optionalInstant(verdict.OpenUntil),
	}
}

// optionalInstant renders a moment that may not have happened.
//
// Nil for the zero instant, so `omitempty` can actually omit it. Every
// optional time on this report goes through here rather than each site
// repeating the check, because the one site that forgot is the one that puts
// the year 1 on the wire.
func optionalInstant(at time.Time) *time.Time {
	if at.IsZero() {
		return nil
	}
	utc := at.UTC()
	return &utc
}

// SpendModel is one engine/model pair's share of a scope's spend.
type SpendModel struct {
	Engine string `json:"engine"`
	Model  string `json:"model"`
	ConsumptionTotals
}

// SpendHost is one host's share of a scope's spend: the node or worker host
// the turns ran on.
type SpendHost struct {
	// Host is the host's address as the fleet knows it ("gpu-host:9091"). It
	// is empty when Unattributed.
	Host string `json:"host,omitempty"`

	// Unattributed marks the entries that name no host. A caller drilling
	// into this row passes SpendUnattributed as the report's host.
	Unattributed bool `json:"unattributed,omitempty"`

	ConsumptionTotals
}

// SpendAgent is one tenant Agent's share of a scope's spend: the turns of the
// sessions that ran as it.
type SpendAgent struct {
	// AgentID is the Agent's catalog ID. It is empty when Unattributed.
	AgentID string `json:"agent_id,omitempty"`

	// Name is the Agent's name when it still exists.
	Name string `json:"name,omitempty"`

	// Unattributed marks the turns of sessions spawned on a raw backend. A
	// caller drilling into this row passes SpendUnattributed as the agent.
	Unattributed bool `json:"unattributed,omitempty"`

	ConsumptionTotals
}

// SpendChild is one row of the drill-down: the next chain level below the
// scope, and what it spent.
type SpendChild struct {
	// ID is the child's identifier at Level. It is empty when Unattributed.
	ID string `json:"id,omitempty"`

	// Level is the chain level ID names.
	Level SpendLevel `json:"level"`

	// Unattributed marks the subtree the ledger files under its
	// no-value sentinel — a job with no task, a turn with no project. It is
	// a real bucket of real spend with no parent at this level, and must be
	// labelled rather than shown as an entity whose id is a dash.
	//
	// ID IS LEFT EMPTY SO NO READER CAN PRINT THE SENTINEL BY ACCIDENT. A
	// caller drilling into this row passes SpendUnattributed as that level's
	// scope value, which is what addresses the subtree.
	Unattributed bool `json:"unattributed,omitempty"`

	// Name is the child's human name where the level has one — a project's
	// name — so a drill-down row reads as the thing it is rather than as an
	// identifier. Empty when the level has no names, when the row is
	// unattributed, or when the entity no longer exists: the ledger outlives
	// what it names, and a deleted project keeps its spend under its id.
	Name string `json:"name,omitempty"`

	ConsumptionTotals
}

// SpendReport is one scope's consumption, its drill-down, and the allowance
// status of every account that paid for it.
type SpendReport struct {
	// Scope and Level echo what was read.
	Scope ConsumptionScope `json:"-"`
	Level SpendLevel       `json:"level"`

	// The chain levels the scope named, echoed verbatim.
	//
	// A LEVEL MAY CARRY SpendUnattributed, when that is what the caller
	// asked for. It is echoed rather than blanked because the report has to
	// say which subtree it summed; a reader rendering these as names must
	// map the sentinel to "no project" / "no task" the same way it maps an
	// unattributed child row.
	TenantID  string `json:"tenant_id"`
	ProjectID string `json:"project_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	JobID     string `json:"job_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Round     int    `json:"round,omitempty"`

	// ProjectName is the name of the project the scope addresses, when it
	// addresses one that still exists.
	ProjectName string `json:"project_name,omitempty"`

	// ConsumptionTotals is what the scope spent: entries, tokens by
	// dimension, successes and failures, and the list-price equivalent.
	//
	// A READER MUST CONSULT PricedEntries BEFORE RENDERING THE MONEY. Zero
	// priced entries means nothing in the scope was priced — its models are
	// not in the price table — and the figure is a dash, never a zero.
	// UnpricedEntries > 0 alongside priced ones means the figure is a floor:
	// some turns ran on models the table does not price.
	ConsumptionTotals

	// Currency is what every money figure on this report is denominated in.
	Currency string `json:"currency"`

	// FirstAt and LastAt bound the entries summed; both NIL when Entries is
	// zero, because an empty scope has no first and no last. Pointers for the
	// reason FreesAt is one: `omitempty` does not omit a zero time.Time.
	FirstAt *time.Time `json:"first_at,omitempty"`
	LastAt  *time.Time `json:"last_at,omitempty"`

	// From and To echo the range the report was narrowed to, [From, To);
	// both nil for a report over the scope's whole history.
	From *time.Time `json:"from,omitempty"`
	To   *time.Time `json:"to,omitempty"`

	// SubscriptionFeeMicros sums the accounts' SubscriptionFeeMicros: what the
	// tenant's subscriptions cost over the range, beside the per-turn billed
	// figure (which is zero for every subscription turn). Nil when no
	// subscription account carries a fee, or below the tenant level.
	//
	// The range a fee is shared over is [From, To) when the report is
	// ranged, and otherwise from the first recorded turn to Now. Either way it
	// starts no earlier than the account's registration.
	SubscriptionFeeMicros *int64 `json:"subscription_fee_micros,omitempty"`

	// Accounts, Models and Children are sorted deterministically — by
	// identifier, then by name — so two reads of an unchanged ledger render
	// identically and a diff of two reports is a diff of the spend.
	Accounts []SpendAccount `json:"accounts,omitempty"`
	Models   []SpendModel   `json:"models,omitempty"`

	// Host echoes the host the report was narrowed to, empty for every host.
	// Hosts breaks the (narrowed) totals down by the host each turn ran on,
	// the unattributed row last.
	Host  string      `json:"host,omitempty"`
	Hosts []SpendHost `json:"hosts,omitempty"`

	// Agent echoes the tenant Agent the report was narrowed to, empty for
	// every Agent. Agents breaks the (narrowed) totals down by the Agent each
	// turn's session ran as, the raw-backend row last.
	Agent  string       `json:"agent,omitempty"`
	Agents []SpendAgent `json:"agents,omitempty"`

	// AgentPolicyStatus and AgentWindows are the narrowed-to Agent's OWN
	// spend limits, with usage against each window read from the Agent quota
	// plane — what a spawn of the Agent is checked against (agent_spend.go).
	// Unknown when no catalog is wired or the Agent no longer exists; empty
	// when the report is not narrowed to one Agent.
	AgentPolicyStatus SpendPolicyStatus   `json:"agent_policy_status,omitempty"`
	AgentWindows      []SpendWindowStatus `json:"agent_windows,omitempty"`

	// ScopePolicyStatus and ScopeWindows are the scope's OWN spend limits —
	// a project's at the project level, the tenant's at the tenant level —
	// with live usage against each window, read from the project or tenant
	// quota plane (go-quicbaas#1058, ruling F6). The status reads as an
	// account's does: none, metered, or unknown when the limits could not be
	// read. Both are empty at the deeper levels, which carry no limits.
	ScopePolicyStatus SpendPolicyStatus   `json:"scope_policy_status,omitempty"`
	ScopeWindows      []SpendWindowStatus `json:"scope_windows,omitempty"`

	// Children is the drill-down: one row per identifier at ChildLevel.
	// Empty at the deepest level, where there is nothing below to attribute
	// to.
	ChildLevel SpendLevel   `json:"child_level,omitempty"`
	Children   []SpendChild `json:"children,omitempty"`
}

// SpendReportRequest addresses one scope of the attribution chain.
//
// The levels are a PATH: each may be set only if every level above it is,
// exactly as ConsumptionScope requires, because a scope that skips a level is
// not a prefix of the keyspace and could only be answered by scanning a
// tenant's whole history on a dashboard's request.
type SpendReportRequest struct {
	TenantID  string
	ProjectID string
	TaskID    string
	JobID     string
	SessionID string

	// Round selects one round below the session, applied only when RoundSet
	// is true so round 0 — a real level — is distinguishable from "every
	// round".
	Round    int
	RoundSet bool

	// From and To narrow the report to the entries that occurred in
	// [From, To). From alone ranges up to Now; To alone is refused. Both zero
	// report the scope's whole history.
	From time.Time
	To   time.Time

	// Host narrows the report to the turns that ran on one host; empty
	// reports every host. SpendUnattributed selects the turns that name none.
	Host string

	// Agent narrows the report to the turns of the sessions one tenant Agent
	// ran; empty reports every Agent. SpendUnattributed selects the turns of
	// raw-backend sessions.
	Agent string

	// Now is the instant the quota windows and the breaker are evaluated
	// against. Zero selects time.Now(), which is what every caller but a
	// test wants; a caller that passes it makes the report reproducible.
	Now time.Time
}

// scope renders the request as the ledger's own scope type, with an open end
// of the range closed at now.
func (r *SpendReportRequest) scope(now time.Time) ConsumptionScope {
	scope := ConsumptionScope{
		TenantID:  r.TenantID,
		ProjectID: r.ProjectID,
		TaskID:    r.TaskID,
		JobID:     r.JobID,
		SessionID: r.SessionID,
		Round:     r.Round,
		RoundSet:  r.RoundSet,
		From:      r.From,
		To:        r.To,
		Host:      r.Host,
		Agent:     r.Agent,
	}
	if !scope.From.IsZero() && scope.To.IsZero() {
		scope.To = now
	}
	return scope
}

// SpendLedgerAccessor is implemented by an agent service that can hand out
// the consumption ledger it writes and the account records the allowances
// live on.
//
// It is the spend half of what SessionLogAccessor is for transcripts, and for
// the same reason: every transport reaches the ledger through ONE declaration
// rather than each type-asserting its own, so a deployment that wires the
// ledger or does not behaves identically on all six.
type SpendLedgerAccessor interface {
	// ConsumptionLedger returns the installed ledger, or nil when none is
	// wired.
	ConsumptionLedger() ConsumptionLedger

	// AccountStore returns the pooled account metadata store, or nil when
	// none is wired. Without it every account's allowance is reported as
	// SpendPolicyUnknown rather than as absent.
	AccountStore() AccountStore

	// ProjectStore returns the durable project store, or nil when none is
	// wired. Without it the report carries project ids and no names.
	ProjectStore() ProjectStore

	// TenantSpendPolicyAccessor supplies the tenant's own limits, reported
	// on a tenant-level report.
	TenantSpendPolicyAccessor

	// AgentDirectoryAccessor supplies Agent names and an Agent's own limits.
	// A nil directory reports them as unknown.
	AgentDirectoryAccessor
}

// SpendAgentCatalog is the slice of the tenant agent catalog a spend report
// reads: every Agent's name and limits in one read.
type SpendAgentCatalog interface {
	Catalogue(ctx context.Context, tenantID string) (AgentCatalogue, error)
}

// SpendSources is what a spend report is read from.
type SpendSources struct {
	// Ledger is the consumption record. Required.
	Ledger ConsumptionLedger

	// Accounts supplies each paying account's name and allowance. Optional.
	Accounts AccountStore

	// Projects supplies project names and a project's own limits. Optional.
	Projects ProjectStore

	// TenantPolicies supplies the tenant's own limits. Optional.
	TenantPolicies TenantSpendPolicyStore

	// Agents supplies Agent names and an Agent's own limits. Optional.
	Agents SpendAgentCatalog
}

// SpendSourcesOf gathers the sources an accessor exposes.
func SpendSourcesOf(accessor SpendLedgerAccessor) SpendSources {
	return SpendSources{
		Ledger:         accessor.ConsumptionLedger(),
		Accounts:       accessor.AccountStore(),
		Projects:       accessor.ProjectStore(),
		TenantPolicies: accessor.TenantSpendPolicies(),
		Agents:         spendAgentCatalogOf(accessor.AgentDirectory()),
	}
}

// spendAgentCatalogOf keeps a nil directory a nil interface, so "no catalog"
// reads as unknown rather than as a catalog that panics.
func spendAgentCatalogOf(directory *AgentDirectory) SpendAgentCatalog {
	if directory == nil {
		return nil
	}
	return directory
}

// =============================================================================
// Errors
// =============================================================================

var (
	// ErrSpendLedgerRequired indicates a spend report asked of a deployment
	// that records no consumption. It is deliberately NOT answered with an
	// empty report: "nothing has been spent" and "nothing is being recorded"
	// are opposite facts, and the empty report is the reassuring one.
	ErrSpendLedgerRequired = errors.New("orchestration spend: consumption ledger is not configured")

	// ErrSpendRequestRequired indicates a nil request.
	ErrSpendRequestRequired = errors.New("orchestration spend: request is required")
)

// SpendReportError reports a scope whose spend could not be established.
type SpendReportError struct {
	// TenantID and Level address what was asked for.
	TenantID string
	Level    SpendLevel

	// Cause is the underlying failure.
	Cause error
}

// Error implements the error interface.
func (e *SpendReportError) Error() string {
	msg := "orchestration spend report: tenant=" + e.TenantID
	if e.Level != "" {
		msg += " level=" + string(e.Level)
	}
	if e.Cause == nil {
		return msg
	}
	return msg + ": " + e.Cause.Error()
}

// Unwrap exposes the underlying failure to errors.Is/As.
func (e *SpendReportError) Unwrap() error { return e.Cause }

// =============================================================================
// The read
// =============================================================================

// ReadSpendReport rolls one scope of the consumption ledger up into the report
// an operator reads, with the drill-down to the level below it and the
// allowance status of every account that paid.
//
// FAIL, DO NOT HALF-ANSWER. A subtree that cannot be scanned, or a record that
// cannot be decoded, fails the call — the ledger's own rule, restated here
// because a partial roll-up presented as a total is a healthy-looking account
// whose first contradiction is the invoice.
//
// The accounts half is allowed to be INCOMPLETE BUT LABELLED: an account the
// store cannot produce is reported SpendPolicyUnknown rather than failing the
// whole report, because the tokens are a fact the ledger established on its
// own and withholding them over a missing allowance would hide the spend the
// operator came to read.
func ReadSpendReport(ctx context.Context, sources SpendSources, req *SpendReportRequest) (*SpendReport, error) {
	if req == nil {
		return nil, &SpendReportError{Cause: ErrSpendRequestRequired}
	}
	ledger := sources.Ledger
	if ledger == nil {
		return nil, &SpendReportError{TenantID: req.TenantID, Cause: ErrSpendLedgerRequired}
	}

	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()

	scope := req.scope(now)
	level, childLevel, err := spendLevels(scope)
	if err != nil {
		return nil, &SpendReportError{TenantID: req.TenantID, Cause: err}
	}

	rollup, err := ledger.Rollup(ctx, scope)
	if err != nil {
		return nil, &SpendReportError{TenantID: req.TenantID, Level: level, Cause: err}
	}

	report := &SpendReport{
		Scope:             scope,
		Level:             level,
		TenantID:          scope.TenantID,
		ProjectID:         scope.ProjectID,
		TaskID:            scope.TaskID,
		JobID:             scope.JobID,
		SessionID:         scope.SessionID,
		ConsumptionTotals: rollup.ConsumptionTotals,
		Currency:          PriceCurrency(),
		FirstAt:           optionalInstant(rollup.FirstAt),
		LastAt:            optionalInstant(rollup.LastAt),
		From:              optionalInstant(scope.From),
		To:                optionalInstant(scope.To),
		ChildLevel:        childLevel,
		Models:            spendModels(rollup.ByModel),
		Host:              scope.Host,
		Hosts:             spendHosts(rollup.ByHost),
		Agent:             scope.Agent,
		Agents:            spendAgents(rollup.ByAgent),
		Children:          spendChildren(rollup.ByChild, childLevel),
	}
	if scope.RoundSet {
		report.Round = scope.Round
	}

	report.Accounts, err = spendAccounts(ctx, ledger, sources.Accounts, scope.TenantID, rollup.ByAccount, now)
	if err != nil {
		return nil, &SpendReportError{TenantID: scope.TenantID, Level: level, Cause: err}
	}
	// A subscription fee belongs to an account, not to a host, so a report
	// narrowed to one host carries no share of it.
	if level == SpendLevelTenant && scope.Host == "" {
		if err := shareSubscriptionFees(ctx, ledger, sources.Accounts, report, rollup.FirstAt, now); err != nil {
			return nil, &SpendReportError{TenantID: scope.TenantID, Level: level, Cause: err}
		}
	}
	if err := nameSpendProjects(ctx, sources.Projects, report); err != nil {
		return nil, &SpendReportError{TenantID: scope.TenantID, Level: level, Cause: err}
	}
	if err := describeScopeLimits(ctx, sources, report, now); err != nil {
		return nil, &SpendReportError{TenantID: scope.TenantID, Level: level, Cause: err}
	}
	if err := describeAgents(ctx, sources, report, now); err != nil {
		return nil, &SpendReportError{TenantID: scope.TenantID, Level: level, Cause: err}
	}
	return report, nil
}

// describeAgents names the Agent rows and, when the report is narrowed to one
// Agent, reports that Agent's own limits and the usage against each window,
// read from the Agent quota plane a spawn is checked against.
//
// The rule describeScopeLimits follows: no catalog wired, or an Agent that no
// longer exists, is UNKNOWN; a catalog that fails fails the report.
func describeAgents(ctx context.Context, sources SpendSources, report *SpendReport, now time.Time) error {
	narrowed := report.Agent != "" && report.Agent != SpendUnattributed
	if narrowed {
		report.AgentPolicyStatus = SpendPolicyUnknown
	}
	if sources.Agents == nil || (len(report.Agents) == 0 && !narrowed) {
		return nil
	}
	catalogue, err := sources.Agents.Catalogue(ctx, report.TenantID)
	if err != nil {
		return err
	}
	for i := range report.Agents {
		if agent, ok := catalogue.Agent(report.Agents[i].AgentID); ok {
			report.Agents[i].Name = agent.Name
		}
	}
	if !narrowed {
		return nil
	}
	agent, ok := catalogue.Agent(report.Agent)
	if !ok {
		return nil
	}
	if !agent.Spend.Metered() {
		report.AgentPolicyStatus = SpendPolicyNone
		return nil
	}
	windows, err := windowStatuses(agent.Spend, now, func(from, to time.Time) (*ConsumptionRollup, error) {
		return sources.Ledger.AgentWindowUsage(ctx, report.TenantID, report.Agent, from, to)
	})
	if err != nil {
		return err
	}
	report.AgentPolicyStatus = SpendPolicyMetered
	report.AgentWindows = windows
	return nil
}

// describeScopeLimits reports the scope's own spend limits and the usage
// against each window: the project's at the project level, the tenant's at
// the tenant level. They are what admission defers a spent project or tenant
// on (admission_spend.go), read from the same quota planes.
//
// The rule describeSpendPolicy follows: limits that could not be read because
// nothing is wired are UNKNOWN, never "none"; a project that no longer exists
// has no limits to report and is unknown too; a store that fails fails the
// report.
func describeScopeLimits(ctx context.Context, sources SpendSources, report *SpendReport, now time.Time) error {
	var (
		limits SpendLimits
		read   windowReader
	)
	switch {
	case report.Level == SpendLevelTenant:
		report.ScopePolicyStatus = SpendPolicyUnknown
		if sources.TenantPolicies == nil {
			return nil
		}
		policy, err := sources.TenantPolicies.Get(ctx, report.TenantID)
		if err != nil {
			return err
		}
		limits = policy.Limits
		read = func(from, to time.Time) (*ConsumptionRollup, error) {
			return sources.Ledger.TenantWindowUsage(ctx, report.TenantID, from, to)
		}
	case report.Level == SpendLevelProject && report.ProjectID != SpendUnattributed:
		report.ScopePolicyStatus = SpendPolicyUnknown
		if sources.Projects == nil {
			return nil
		}
		project, err := sources.Projects.Get(ctx, report.TenantID, report.ProjectID)
		if errors.Is(err, ErrProjectNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		limits = project.Spend
		read = func(from, to time.Time) (*ConsumptionRollup, error) {
			return sources.Ledger.ProjectWindowUsage(ctx, report.TenantID, report.ProjectID, from, to)
		}
	default:
		return nil
	}
	if !limits.Metered() {
		report.ScopePolicyStatus = SpendPolicyNone
		return nil
	}
	windows, err := windowStatuses(limits, now, read)
	if err != nil {
		return err
	}
	report.ScopePolicyStatus = SpendPolicyMetered
	report.ScopeWindows = windows
	return nil
}

// nameSpendProjects puts project names on the report: on the scope when it
// addresses a project, and on every drill-down row when the rows ARE
// projects.
//
// THE SAME RULE AS loadSpendAccount. A project that is gone keeps its spend
// under its id with no name — the ledger outlives what it names — but a
// store that could not answer fails the report, because a fault rendered as
// "this project has no name" is a fault presented as a fact.
func nameSpendProjects(ctx context.Context, projects ProjectStore, report *SpendReport) error {
	if projects == nil {
		return nil
	}
	if report.ProjectID != "" && report.ProjectID != SpendUnattributed {
		name, err := spendProjectName(ctx, projects, report.TenantID, report.ProjectID)
		if err != nil {
			return err
		}
		report.ProjectName = name
	}
	if report.ChildLevel != SpendLevelProject {
		return nil
	}
	for i := range report.Children {
		child := &report.Children[i]
		if child.Unattributed {
			continue
		}
		name, err := spendProjectName(ctx, projects, report.TenantID, child.ID)
		if err != nil {
			return err
		}
		child.Name = name
	}
	return nil
}

// shareSubscriptionFees pro-rates every subscription's monthly fee over the
// report's range (go-quicbaas#1058, ruling F4) and totals it on the report.
//
// EVERY FEE-CARRYING SUBSCRIPTION OF THE TENANT, used or not. A subscription
// that served no turn in the range still cost its fee, and an idle plan is
// exactly what this figure exists to show; so such an account gets a row with
// no consumption beside its fee share.
//
// The range is the report's own when it was ranged, and otherwise the span
// from the first recorded turn to now — the period the ledger can speak for.
// An unranged report over an empty scope has no span and shares no fee. Each
// account's share starts no earlier than its registration.
func shareSubscriptionFees(
	ctx context.Context,
	ledger ConsumptionLedger,
	store AccountStore,
	report *SpendReport,
	firstAt, now time.Time,
) error {
	if store == nil {
		return nil
	}
	from, to := firstAt, now
	if report.From != nil && report.To != nil {
		from, to = *report.From, *report.To
	}
	if from.IsZero() {
		return nil
	}
	accounts, err := store.ListByTenant(ContextWithTenant(ctx, report.TenantID), report.TenantID, "", "")
	if err != nil {
		return err
	}
	rows := make(map[string]int, len(report.Accounts))
	for i := range report.Accounts {
		rows[report.Accounts[i].AccountID] = i
	}
	var total int64
	shared := false
	for _, account := range accounts {
		if account == nil {
			continue
		}
		start := from
		if account.CreatedAt.After(start) {
			start = account.CreatedAt
		}
		share, known := account.Billing.FeeShare(start, to)
		if !known {
			continue
		}
		index, present := rows[account.ID]
		if !present {
			row := SpendAccount{AccountID: account.ID, PolicyStatus: SpendPolicyUnknown}
			if err := describeSpendPolicy(ctx, ledger, account, &row, now); err != nil {
				return err
			}
			report.Accounts = append(report.Accounts, row)
			index = len(report.Accounts) - 1
		}
		report.Accounts[index].SubscriptionFeeMicros = &share
		total += share
		shared = true
	}
	if shared {
		report.SubscriptionFeeMicros = &total
		sort.Slice(report.Accounts, func(i, j int) bool { return report.Accounts[i].AccountID < report.Accounts[j].AccountID })
	}
	return nil
}

// spendProjectName reads one project's name; empty when the project no longer
// exists.
func spendProjectName(ctx context.Context, projects ProjectStore, tenantID, projectID string) (string, error) {
	project, err := projects.Get(ctx, tenantID, projectID)
	if err == nil {
		return project.Name, nil
	}
	if errors.Is(err, ErrProjectNotFound) {
		return "", nil
	}
	return "", err
}

// spendLevels names the level a scope addresses and the level below it,
// refusing a scope the ledger could not answer as a prefix.
func spendLevels(scope ConsumptionScope) (level, child SpendLevel, err error) {
	depth, err := validateConsumptionScope(scope)
	if err != nil {
		return "", "", err
	}
	level = spendLevelsByDepth[depth-1]
	if depth < len(spendLevelsByDepth) {
		child = spendLevelsByDepth[depth]
	}
	return level, child, nil
}

// spendModels renders the roll-up's model breakdown, splitting the composite
// engine/model key back into its two parts.
//
// The key is built by ConsumptionEntry.ModelKey as engine + "/" + model, and
// the split is unambiguous because the ledger refuses a separator inside the
// ENGINE at the write (validateConsumptionEntry -> validateKeySegment). The
// model may carry separators of its own — "ollama/review-q8:latest" — so the
// cut is at the FIRST separator, which recovers exactly what was recorded; a
// key with no separator at all was not written by this package and is
// reported whole rather than guessed apart.
func spendModels(byModel map[string]*ConsumptionTotals) []SpendModel {
	if len(byModel) == 0 {
		return nil
	}
	models := make([]SpendModel, 0, len(byModel))
	for key, totals := range byModel {
		engine, model, found := strings.Cut(key, "/")
		if !found {
			// A key with no separator cannot be split honestly, so it is
			// reported whole as the model rather than guessed apart.
			engine, model = "", key
		}
		models = append(models, SpendModel{Engine: engine, Model: model, ConsumptionTotals: *totals})
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Engine != models[j].Engine {
			return models[i].Engine < models[j].Engine
		}
		return models[i].Model < models[j].Model
	})
	return models
}

// spendHosts renders the host breakdown, sorted by host with the unattributed
// row last.
func spendHosts(byHost map[string]*ConsumptionTotals) []SpendHost {
	if len(byHost) == 0 {
		return nil
	}
	hosts := make([]SpendHost, 0, len(byHost))
	for host, totals := range byHost {
		row := SpendHost{ConsumptionTotals: *totals}
		if host == consumptionUnattributed {
			row.Unattributed = true
		} else {
			row.Host = host
		}
		hosts = append(hosts, row)
	}
	sort.Slice(hosts, func(i, j int) bool {
		if hosts[i].Unattributed != hosts[j].Unattributed {
			return hosts[j].Unattributed
		}
		return hosts[i].Host < hosts[j].Host
	})
	return hosts
}

// spendAgents renders the Agent breakdown, sorted by Agent with the
// raw-backend row last.
func spendAgents(byAgent map[string]*ConsumptionTotals) []SpendAgent {
	if len(byAgent) == 0 {
		return nil
	}
	agents := make([]SpendAgent, 0, len(byAgent))
	for agentID, totals := range byAgent {
		row := SpendAgent{ConsumptionTotals: *totals}
		if agentID == consumptionUnattributed {
			row.Unattributed = true
		} else {
			row.AgentID = agentID
		}
		agents = append(agents, row)
	}
	sort.Slice(agents, func(i, j int) bool {
		if agents[i].Unattributed != agents[j].Unattributed {
			return agents[j].Unattributed
		}
		return agents[i].AgentID < agents[j].AgentID
	})
	return agents
}

// spendChildren renders the drill-down rows, labelling the unattributed
// subtree and un-padding a round segment.
func spendChildren(byChild map[string]*ConsumptionTotals, level SpendLevel) []SpendChild {
	if level == "" || len(byChild) == 0 {
		return nil
	}
	children := make([]SpendChild, 0, len(byChild))
	for id, totals := range byChild {
		child := SpendChild{Level: level, ConsumptionTotals: *totals}
		switch {
		case id == consumptionUnattributed:
			// The ledger's no-value sentinel. Reported as an absence, never
			// as an entity whose identifier is a dash.
			child.Unattributed = true
		case level == SpendLevelRound:
			child.ID = unpadRoundSegment(id)
		default:
			child.ID = id
		}
		children = append(children, child)
	}
	sort.Slice(children, func(i, j int) bool {
		// The unattributed bucket sorts last: it is a residue, and putting it
		// at the head of a drill-down reads as the largest contributor.
		if children[i].Unattributed != children[j].Unattributed {
			return children[j].Unattributed
		}
		return children[i].ID < children[j].ID
	})
	return children
}

// unpadRoundSegment turns the ledger's fixed-width round segment back into the
// decimal an operator reads. A segment that is not a number is returned as it
// was stored rather than replaced by a guess.
func unpadRoundSegment(segment string) string {
	n, err := strconv.Atoi(segment)
	if err != nil {
		return segment
	}
	return strconv.Itoa(n)
}

// spendAccounts renders each paying account's tokens and allowance status.
//
// It reads the account record to learn the POLICY and the ledger to learn the
// USAGE, which is the same pair the enforcement path judges (see
// account_spend_authority.go). Nothing is re-derived: the windows come from
// the account's own AccountSpendPolicy and the breaker from the ledger, so a
// report and a refusal cannot disagree about the same account.
func spendAccounts(
	ctx context.Context,
	ledger ConsumptionLedger,
	store AccountStore,
	tenantID string,
	byAccount map[string]*ConsumptionTotals,
	now time.Time,
) ([]SpendAccount, error) {
	if len(byAccount) == 0 {
		return nil, nil
	}
	out := make([]SpendAccount, 0, len(byAccount))
	for accountID, totals := range byAccount {
		entry := SpendAccount{
			AccountID:         accountID,
			ConsumptionTotals: *totals,
			PolicyStatus:      SpendPolicyUnknown,
		}
		account, err := loadSpendAccount(ctx, store, tenantID, accountID)
		if err != nil {
			return nil, err
		}
		if account != nil {
			if err := describeSpendPolicy(ctx, ledger, account, &entry, now); err != nil {
				return nil, err
			}
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AccountID < out[j].AccountID })
	return out, nil
}

// loadSpendAccount reads one account. It returns (nil, nil) when the account
// legitimately is not there, and an error when the store could not answer.
//
// THE TWO CASES ARE NOT THE SAME AND MUST NOT RENDER THE SAME.
//
// A MISSING ACCOUNT IS NOT A FAILED REPORT. The ledger is append-only and
// outlives the accounts it names — an account deleted after its turns were
// recorded still owns that spend — so the tokens are reported with the
// allowance marked unknown instead of the whole scope failing. That is a
// permanent, benign fact.
//
// A STORE THAT COULD NOT ANSWER IS A FAILED REPORT, and this is the half that
// was wrong. Collapsing every error to "unknown" meant that while the account
// store's KV was failing, EVERY account in the report read "spend policy
// unknown" beside healthy-looking token totals drawn from the separate ledger
// store — a fault rendered as a fact, which is the exact shape of lie the
// rest of this file exists to remove. It is also inconsistent with the
// ledger's own rule one function up: a subtree that cannot be scanned fails
// the call rather than reporting the part it managed to read.
func loadSpendAccount(ctx context.Context, store AccountStore, tenantID, accountID string) (*Account, error) {
	if store == nil {
		// No store is wired at all. A deployment-shaped absence, reported as
		// an unknown allowance for every account rather than as a failure.
		return nil, nil
	}
	account, err := store.Get(ctx, tenantID, accountID)
	if err == nil {
		return account, nil
	}
	if errors.Is(err, ErrAccountNotFound) {
		return nil, nil
	}
	return nil, err
}

// describeSpendPolicy fills in one account's allowance status from its policy
// and the ledger.
func describeSpendPolicy(
	ctx context.Context,
	ledger ConsumptionLedger,
	account *Account,
	entry *SpendAccount,
	now time.Time,
) error {
	entry.DisplayName = account.DisplayName
	entry.Provider = string(account.Provider)
	entry.Billing = account.Billing

	policy := account.Spend
	if !policy.Metered() {
		entry.PolicyStatus = SpendPolicyNone
		return nil
	}
	entry.PolicyStatus = SpendPolicyMetered

	windows, err := windowStatuses(policy.Quota, now, func(from, to time.Time) (*ConsumptionRollup, error) {
		return ledger.WindowUsage(ctx, account.TenantID, account.ID, from, to)
	})
	if err != nil {
		return err
	}
	entry.Windows = windows
	if policy.Breaker == nil {
		return nil
	}
	verdict, err := EvaluateBreaker(ctx, ledger, account.TenantID, account.ID, *policy.Breaker, now)
	if err != nil {
		return err
	}
	entry.Breaker = spendBreakerOf(verdict)
	return nil
}

// windowStatuses reads every window of limits, in order.
func windowStatuses(limits SpendLimits, now time.Time, read windowReader) ([]SpendWindowStatus, error) {
	var out []SpendWindowStatus
	for _, window := range limits.Windows {
		status, err := spendWindowStatus(window, now, read)
		if err != nil {
			return nil, err
		}
		out = append(out, status)
	}
	return out, nil
}

// spendWindowStatus reads one sliding window's live usage.
func spendWindowStatus(window QuotaWindow, now time.Time, read windowReader) (SpendWindowStatus, error) {
	usage, err := read(now.Add(-window.Size), now)
	if err != nil {
		// No status. A window whose usage could not be read must not be
		// reported as an empty one: that is a full allowance drawn on a
		// store that is in trouble.
		return SpendWindowStatus{}, err
	}
	status := SpendWindowStatus{
		Name:                 window.Name,
		Size:                 window.Size,
		MaxTokens:            window.MaxTokens,
		UsedTokens:           usage.TotalTokens,
		MaxEquivalentMicros:  window.MaxEquivalentMicros,
		UsedEquivalentMicros: usage.EquivalentMicros,
	}
	if window.MaxTokens > 0 {
		status.RemainingTokens = max(window.MaxTokens-usage.TotalTokens, 0)
		status.Exhausted = usage.TotalTokens >= window.MaxTokens
	}
	if window.MaxEquivalentMicros > 0 {
		status.RemainingEquivalentMicros = max(window.MaxEquivalentMicros-usage.EquivalentMicros, 0)
		status.Exhausted = status.Exhausted || usage.EquivalentMicros >= window.MaxEquivalentMicros
	}
	// A sliding window frees up when its oldest entry ages out; with nothing
	// in it there is nothing to age out, and the caller is told so by an
	// ABSENT instant rather than by one it has to interpret.
	if !usage.FirstAt.IsZero() {
		status.FreesAt = optionalInstant(usage.FirstAt.Add(window.Size))
	}
	return status, nil
}
