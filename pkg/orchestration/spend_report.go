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
//   - COST. Nothing in this tree prices a turn, so every ConsumptionEntry
//     carries CostMicros zero, meaning UNMEASURED (go-quicbaas#791). The
//     report therefore carries CostMeasured alongside the figure, and a
//     reader that renders the number without consulting it is reporting an
//     unmeasured total as a measured one.
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
	Exhausted bool `json:"exhausted"`

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

	// TotalTokens is what this account spent inside the report's scope.
	TotalTokens int64 `json:"total_tokens"`

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

// SpendModel is one engine/model pair's share of a scope's tokens.
type SpendModel struct {
	Engine      string `json:"engine"`
	Model       string `json:"model"`
	TotalTokens int64  `json:"total_tokens"`
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

	TotalTokens int64 `json:"total_tokens"`
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

	// Entries is how many ledger records were summed.
	Entries int `json:"entries"`

	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`

	// CostMicros is the summed charge in millionths of a currency unit, and
	// CostMeasured says whether ANY of it was actually priced.
	//
	// A READER MUST CONSULT CostMeasured BEFORE RENDERING CostMicros. Nothing
	// in this tree prices a turn (go-quicbaas#791), so today CostMeasured is
	// false on every production report and the figure must be shown as a
	// dash. Deriving the flag from the data rather than hard-coding false is
	// what lets the price source land without every reader changing: the day
	// a turn carries a price, the total stops being zero and starts being
	// measured.
	CostMicros   int64 `json:"cost_micros"`
	CostMeasured bool  `json:"cost_measured"`

	// Successes and Failures partition Entries. A failed turn spends tokens
	// too, which is why it is in the ledger at all.
	Successes int `json:"successes"`
	Failures  int `json:"failures"`

	// FirstAt and LastAt bound the entries summed; both NIL when Entries is
	// zero, because an empty scope has no first and no last. Pointers for the
	// reason FreesAt is one: `omitempty` does not omit a zero time.Time.
	FirstAt *time.Time `json:"first_at,omitempty"`
	LastAt  *time.Time `json:"last_at,omitempty"`

	// Accounts, Models and Children are sorted deterministically — by
	// identifier, then by name — so two reads of an unchanged ledger render
	// identically and a diff of two reports is a diff of the spend.
	Accounts []SpendAccount `json:"accounts,omitempty"`
	Models   []SpendModel   `json:"models,omitempty"`

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

	// Now is the instant the quota windows and the breaker are evaluated
	// against. Zero selects time.Now(), which is what every caller but a
	// test wants; a caller that passes it makes the report reproducible.
	Now time.Time
}

// scope renders the request as the ledger's own scope type.
func (r *SpendReportRequest) scope() ConsumptionScope {
	return ConsumptionScope{
		TenantID:  r.TenantID,
		ProjectID: r.ProjectID,
		TaskID:    r.TaskID,
		JobID:     r.JobID,
		SessionID: r.SessionID,
		Round:     r.Round,
		RoundSet:  r.RoundSet,
	}
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
func ReadSpendReport(
	ctx context.Context,
	ledger ConsumptionLedger,
	accounts AccountStore,
	req *SpendReportRequest,
) (*SpendReport, error) {
	if req == nil {
		return nil, &SpendReportError{Cause: ErrSpendRequestRequired}
	}
	if ledger == nil {
		return nil, &SpendReportError{TenantID: req.TenantID, Cause: ErrSpendLedgerRequired}
	}

	scope := req.scope()
	level, childLevel, err := spendLevels(scope)
	if err != nil {
		return nil, &SpendReportError{TenantID: req.TenantID, Cause: err}
	}

	rollup, err := ledger.Rollup(ctx, scope)
	if err != nil {
		return nil, &SpendReportError{TenantID: req.TenantID, Level: level, Cause: err}
	}

	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()

	report := &SpendReport{
		Scope:        scope,
		Level:        level,
		TenantID:     scope.TenantID,
		ProjectID:    scope.ProjectID,
		TaskID:       scope.TaskID,
		JobID:        scope.JobID,
		SessionID:    scope.SessionID,
		Entries:      rollup.Entries,
		InputTokens:  rollup.InputTokens,
		OutputTokens: rollup.OutputTokens,
		TotalTokens:  rollup.TotalTokens,
		CostMicros:   rollup.CostMicros,
		CostMeasured: rollup.CostMicros > 0,
		Successes:    rollup.Successes,
		Failures:     rollup.Failures,
		FirstAt:      optionalInstant(rollup.FirstAt),
		LastAt:       optionalInstant(rollup.LastAt),
		ChildLevel:   childLevel,
		Models:       spendModels(rollup.ByModel),
		Children:     spendChildren(rollup.ByChild, childLevel),
	}
	if scope.RoundSet {
		report.Round = scope.Round
	}

	report.Accounts, err = spendAccounts(ctx, ledger, accounts, scope.TenantID, rollup.ByAccount, now)
	if err != nil {
		return nil, &SpendReportError{TenantID: scope.TenantID, Level: level, Cause: err}
	}
	return report, nil
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
// the split is unambiguous because the ledger refuses a separator inside
// either half at the write (validateConsumptionEntry -> validateKeySegment).
// Cutting at the first separator rather than the last therefore recovers
// exactly what was recorded; a key with no separator at all was not written
// by this package and is reported whole rather than guessed apart.
func spendModels(byModel map[string]int64) []SpendModel {
	if len(byModel) == 0 {
		return nil
	}
	models := make([]SpendModel, 0, len(byModel))
	for key, tokens := range byModel {
		engine, model, found := strings.Cut(key, "/")
		if !found {
			// A key with no separator cannot be split honestly, so it is
			// reported whole as the model rather than guessed apart.
			engine, model = "", key
		}
		models = append(models, SpendModel{Engine: engine, Model: model, TotalTokens: tokens})
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Engine != models[j].Engine {
			return models[i].Engine < models[j].Engine
		}
		return models[i].Model < models[j].Model
	})
	return models
}

// spendChildren renders the drill-down rows, labelling the unattributed
// subtree and un-padding a round segment.
func spendChildren(byChild map[string]int64, level SpendLevel) []SpendChild {
	if level == "" || len(byChild) == 0 {
		return nil
	}
	children := make([]SpendChild, 0, len(byChild))
	for id, tokens := range byChild {
		child := SpendChild{Level: level, TotalTokens: tokens}
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
	byAccount map[string]int64,
	now time.Time,
) ([]SpendAccount, error) {
	if len(byAccount) == 0 {
		return nil, nil
	}
	out := make([]SpendAccount, 0, len(byAccount))
	for accountID, tokens := range byAccount {
		entry := SpendAccount{
			AccountID:    accountID,
			TotalTokens:  tokens,
			PolicyStatus: SpendPolicyUnknown,
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

	policy := account.Spend
	if !policy.Metered() {
		entry.PolicyStatus = SpendPolicyNone
		return nil
	}
	entry.PolicyStatus = SpendPolicyMetered

	for _, window := range policy.Quota.Windows {
		status, err := spendWindowStatus(ctx, ledger, account.TenantID, account.ID, window, now)
		if err != nil {
			return err
		}
		entry.Windows = append(entry.Windows, status)
	}
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

// spendWindowStatus reads one sliding window's live usage.
func spendWindowStatus(
	ctx context.Context,
	ledger ConsumptionLedger,
	tenantID, accountID string,
	window QuotaWindow,
	now time.Time,
) (SpendWindowStatus, error) {
	usage, err := ledger.WindowUsage(ctx, tenantID, accountID, now.Add(-window.Size), now)
	if err != nil {
		// No status. A window whose usage could not be read must not be
		// reported as an empty one: that is a full allowance drawn on a
		// store that is in trouble.
		return SpendWindowStatus{}, err
	}
	remaining := window.MaxTokens - usage.TotalTokens
	if remaining < 0 {
		remaining = 0
	}
	status := SpendWindowStatus{
		Name:            window.Name,
		Size:            window.Size,
		MaxTokens:       window.MaxTokens,
		UsedTokens:      usage.TotalTokens,
		RemainingTokens: remaining,
		Exhausted:       usage.TotalTokens >= window.MaxTokens,
	}
	// A sliding window frees up when its oldest entry ages out; with nothing
	// in it there is nothing to age out, and the caller is told so by an
	// ABSENT instant rather than by one it has to interpret.
	if !usage.FirstAt.IsZero() {
		status.FreesAt = optionalInstant(usage.FirstAt.Add(window.Size))
	}
	return status, nil
}
