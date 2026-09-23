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
	"strconv"
	"time"
)

// =============================================================================
// The consumption ledger
// =============================================================================
//
// One durable record per unit of provider consumption, written once and never
// edited. It is the single authority for two questions that used to be
// answered separately and could therefore disagree: what has been spent (the
// invoice) and what may still be spent (the quota).
//
// WHY APPEND-ONLY, AND NOT A COUNTER. The counters in cost_tracker.go are
// read-modify-write: two hosts recording turns for one account both read the
// same total and both write their own sum, and one turn's tokens vanish. That
// is tolerable for the advisory budget those counters report and is not
// tolerable here, because this is the number a limit decision is made against.
// An entry keyed by its own idempotency key is decided by the store's guarded
// create instead: of N writers presenting one entry exactly one stores it, the
// losers observe the stored bytes and agree, and no arithmetic is performed on
// a value another host is concurrently changing. Totals are derived by summing
// what is there, so a total can never be wrong in a way a re-read would not
// correct.
//
// THE CHAIN IS THE KEY PATH. tenant -> project -> task -> job -> session ->
// round appear in the key in that order, so a roll-up at any level is a prefix
// scan of that level's subtree rather than a filtered scan of everything. The
// per-account quota plane is a second key path over the same entries — see the
// two keyspaces below.
//
// THE CHAIN IS THE ENGINE'S OWN VOCABULARY. A task is the unit of work, a job
// is one attempt at it, a session is the agent process that attempt ran, and a
// round is one turn within that session. The ledger does not invent a parallel
// hierarchy for spend: attributing a charge to anything the orchestrator does
// not already name would make a roll-up unjoinable with the records an
// operator is actually looking at.
//
// A LEVEL THAT DOES NOT APPLY IS NOT ABSENT, IT IS UNATTRIBUTED. A job that
// belongs to no task still has a task segment in its key:
// consumptionUnattributed. Collapsing the segment instead would shorten the key
// and silently move that entry into the subtree of whatever sits at the next
// position, so a project roll-up would start counting session ids as tasks. The
// sentinel is refused as a caller-supplied value for exactly that reason.
//
// WHAT A FAILED READ MUST NOT RETURN. Zero. A roll-up that cannot read its
// subtree, or that reads a record it cannot decode, refuses with a typed error
// naming the key. The alternative — reporting the entries it managed to read —
// is a healthy-looking account with no spend on it, and the first evidence
// that it was wrong is the invoice.

// consumptionUnattributed fills a chain level the caller had no value for.
//
// It is refused as a supplied value (see validateChainSegment) so that "no
// task" and "a task literally named -" can never produce the same key. A
// sentinel that a caller can also spell is not a sentinel.
const consumptionUnattributed = "-"

// Keyspaces. Both are written for every entry and hold the identical record;
// they differ only in what a prefix of them selects.
const (
	// consumptionChainKeyspace is the attribution plane, keyed by the full
	// chain. A prefix of it is a roll-up.
	//
	//	orch/costchain/{tenant}/{project}/{task}/{job}/{session}/{round}/{entryID}
	consumptionChainKeyspace = "orch/costchain/"

	// consumptionWindowKeyspace is the quota plane, keyed by account and by
	// the moment consumption occurred. A prefix of it is a rate window.
	//
	//	orch/costwindow/{tenant}/{account}/{bucket}/{occurredAtNanos}/{entryID}
	consumptionWindowKeyspace = "orch/costwindow/"
)

// consumptionBucket is the fixed span each quota-plane key is filed under.
//
// It exists because interfaces.KVStore offers a prefix scan and no range scan.
// Without a bucket segment, reading a five-hour window would mean scanning an
// account's entire history and discarding all but the tail — a cost that grows
// with the account's age, paid on the path that decides whether the next spawn
// may run. With it, a window read scans only the buckets it overlaps: bounded
// by the window's length, not by the ledger's.
//
// One hour is small enough that the shortest windows worth modelling touch one
// or two buckets, and large enough that the longest (a weekly plan limit) is
// still a bounded number of scans. It is baked into stored keys, so changing it
// re-files nothing already written and must be treated as a keyspace change.
const consumptionBucket = time.Hour

// consumptionRoundWidth is the zero-padded width of the round segment, and
// consumptionNanosWidth that of the timestamp segment.
//
// Both are fixed-width so that the key layout's lexical order is the numeric
// order. A bare strconv would sort round 10 before round 2 and would put the
// year 2262 rollover between two ordinary timestamps, which matters because
// every window read here is an ordered range over these segments.
const (
	consumptionRoundWidth = 6
	consumptionNanosWidth = 19
)

// ConsumptionOutcome records whether the consumption was productive. Failed
// turns cost tokens too, so they belong in the ledger; keeping them
// distinguishable is what lets the circuit breaker be derived from the ledger
// rather than kept as its own contested state.
type ConsumptionOutcome string

const (
	// ConsumptionOutcomeSuccess marks consumption that produced a result.
	ConsumptionOutcomeSuccess ConsumptionOutcome = "success"

	// ConsumptionOutcomeFailure marks consumption whose turn failed. The
	// tokens were still spent and the provider still counted them.
	ConsumptionOutcomeFailure ConsumptionOutcome = "failure"
)

// validConsumptionOutcomes is the exhaustive set, as a map so an unrecognized
// outcome is refused rather than defaulted. A default branch here would let a
// typo be stored and then be counted as neither a success nor a failure.
var validConsumptionOutcomes = map[ConsumptionOutcome]struct{}{
	ConsumptionOutcomeSuccess: {},
	ConsumptionOutcomeFailure: {},
}

// ConsumptionEntry is one immutable record of provider consumption, carrying
// the full attribution chain plus the account and engine/model that served it.
type ConsumptionEntry struct {
	// EntryID is the caller's idempotency key and the leaf of both keys.
	//
	// It is REQUIRED and is never generated here. Only the caller knows what
	// "the same consumption" means — one provider response, one turn of one
	// session — and a ledger that invented the key on the caller's behalf
	// would turn every retry of a request whose response was lost into a
	// second charge. Recording the same entry twice with the same id is a
	// no-op; recording two different entries under one id is refused.
	//
	// THIS IS THE SAME CONTRACT AS THE INBOX DEDUP KEY, and the two are meant
	// to be recognizably one rule rather than two conventions that happen to
	// rhyme. peer_messaging_store.go takes the message id from the sender for
	// the same reason this takes the entry id from the caller: the identity of
	// "the same thing happening twice" is knowledge the originator has and the
	// store does not. There, inventing it would deliver a message twice; here,
	// it would charge for a turn twice. Both refuse rather than guess, and
	// both make the retry of an ambiguous failure converge instead of
	// duplicating.
	EntryID string `json:"entry_id"`

	// The attribution chain, outermost first. TenantID is required; each
	// level below it may be empty, which stores consumptionUnattributed.
	TenantID  string `json:"tenant_id"`
	ProjectID string `json:"project_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	JobID     string `json:"job_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`

	// Round is the turn ordinal within the session. Zero means the
	// consumption belongs to no numbered round, which is a level value like
	// any other rather than a missing one.
	Round int `json:"round"`

	// AccountID is the pooled account the provider billed. REQUIRED: this is
	// what the quota plane is keyed by, and an entry that cannot say which
	// allowance it drew on cannot be counted against one.
	AccountID string `json:"account_id"`

	// Engine and Model identify what served the turn. Both are required — a
	// spend report that cannot say which engine ran cannot be reconciled
	// against a provider's own invoice, which itemizes by model.
	Engine string `json:"engine"`
	Model  string `json:"model"`

	// Outcome is required and must be one of validConsumptionOutcomes.
	Outcome ConsumptionOutcome `json:"outcome"`

	// The provider's own counts, one per way they price. InputTokens is the
	// FRESH (uncached) prompt; CacheReadTokens and CacheWriteTokens are prompt
	// tokens served from and written into the provider's prompt cache, which
	// price at a fraction and a multiple of a fresh token. Kept apart rather
	// than pre-summed because they price differently: before go-quicbaas#1058
	// both cache dimensions were folded into InputTokens, which left a cached
	// turn impossible to price.
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64 `json:"cache_write_tokens,omitempty"`

	// CostMicros is the charge in millionths of the account's currency unit.
	// An integer, deliberately: money summed as float64 does not associate,
	// so two roll-ups over the same entries in different orders could report
	// different totals.
	//
	// NOTHING IN THIS TREE POPULATES IT YET, so ZERO MEANS UNMEASURED AND
	// NOT FREE. No turn is priced — there is no per-model price source — and
	// the recording path (recordConsumption) therefore leaves this at its
	// zero value on every production entry.
	//
	// A READER MUST NOT PRESENT IT AS A MEASURED FIGURE. That is why the
	// quota authority has no cost axis: a window metering micros would sum
	// zero and admit every turn forever, which is a cap that looks like
	// protection and is not one. The same rule binds anything that reports
	// the roll-up — an unmeasured figure is a dash, never a zero.
	//
	// The field is kept rather than deleted because it is the slot a price
	// lands in, and the entry is the durable record of what the provider
	// billed. go-quicbaas#791 adds the price source, the cost axis and the
	// reported column together, so no one of the three can ship claiming
	// the other two.
	CostMicros int64 `json:"cost_micros"`

	// OccurredAt is when the provider served the consumption. REQUIRED and
	// non-zero: it is the window segment of the quota key, so an entry
	// without it would land at the epoch and be invisible to every live
	// window. Stored in UTC.
	OccurredAt time.Time `json:"occurred_at"`
}

// TotalTokens returns the entry's combined token count: every dimension the
// provider metered, cached or not. It is the quantity every quota window sums,
// and it is unchanged by the split of the cache dimensions out of InputTokens.
func (e ConsumptionEntry) TotalTokens() int64 {
	return e.InputTokens + e.OutputTokens + e.CacheReadTokens + e.CacheWriteTokens
}

// ModelKey is the engine/model pair as one breakdown key. Two engines serving
// a model of the same name are different things and must not merge.
func (e ConsumptionEntry) ModelKey() string {
	return e.Engine + "/" + e.Model
}

// normalize returns a copy with the chain levels resolved to their stored form
// and the timestamp in UTC. It is applied before both marshaling and key
// construction so the record and the key it lives at can never disagree about
// which subtree the entry belongs to.
func (e ConsumptionEntry) normalize() ConsumptionEntry {
	e.ProjectID = chainSegment(e.ProjectID)
	e.TaskID = chainSegment(e.TaskID)
	e.JobID = chainSegment(e.JobID)
	e.SessionID = chainSegment(e.SessionID)
	e.OccurredAt = e.OccurredAt.UTC()
	return e
}

// chainSegment maps an empty chain level to the unattributed sentinel.
func chainSegment(value string) string {
	if value == "" {
		return consumptionUnattributed
	}
	return value
}

// roundSegment renders a round as a fixed-width, lexically ordered segment.
func roundSegment(round int) string {
	return padSegment(strconv.Itoa(round), consumptionRoundWidth)
}

// nanosSegment renders an instant as a fixed-width, lexically ordered segment.
func nanosSegment(at time.Time) string {
	return padSegment(strconv.FormatInt(at.UTC().UnixNano(), 10), consumptionNanosWidth)
}

// padSegment left-pads value with zeroes to width, leaving anything already at
// or over width untouched. Truncating instead would map two distinct values
// onto one segment, which is the one outcome a key encoder may never produce.
func padSegment(value string, width int) string {
	if len(value) >= width {
		return value
	}
	pad := make([]byte, width-len(value))
	for i := range pad {
		pad[i] = '0'
	}
	return string(pad) + value
}

// =============================================================================
// Scopes and roll-ups
// =============================================================================

// ConsumptionScope names one level of the attribution chain to roll up.
//
// The levels are a PATH, not a filter set: each one may be set only if every
// level above it is. Asking for "task 7 in any project" is not a prefix of
// this keyspace and is refused rather than answered by a scan of everything,
// because the scan that would answer it grows with the tenant's whole history.
type ConsumptionScope struct {
	TenantID  string
	ProjectID string
	TaskID    string
	JobID     string
	SessionID string

	// Round selects one round below the session. It is applied only when
	// RoundSet is true, so round 0 — a real level — is distinguishable from
	// "every round", which an int alone cannot express.
	Round    int
	RoundSet bool
}

// ConsumptionRollup is the aggregate over one scope's subtree.
//
// Every field is derived from entries actually read. There is no cached total
// and no partial result: a roll-up either summed the whole subtree or returned
// an error.
type ConsumptionRollup struct {
	Scope ConsumptionScope `json:"scope"`

	Entries          int   `json:"entries"`
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	TotalTokens      int64 `json:"total_tokens"`

	// CostMicros sums the entries' charges. It is ZERO ON EVERY PRODUCTION
	// ROLL-UP because nothing prices a turn yet — see
	// ConsumptionEntry.CostMicros — and a caller that rendered it would be
	// reporting an unmeasured figure as a measured zero. go-quicbaas#791.
	CostMicros int64 `json:"cost_micros"`

	// Successes and Failures partition Entries.
	Successes int `json:"successes"`
	Failures  int `json:"failures"`

	// ByAccount and ByModel break the total tokens down by the two
	// dimensions that are not part of the chain.
	ByAccount map[string]int64 `json:"by_account,omitempty"`
	ByModel   map[string]int64 `json:"by_model,omitempty"`

	// ByChild breaks the total tokens down by the next chain level below the
	// scope — the drill-down step. It is empty at the deepest level, where
	// there is no level below to attribute to.
	ByChild map[string]int64 `json:"by_child,omitempty"`

	// FirstAt and LastAt bound the entries summed. Both are zero only when
	// Entries is zero.
	FirstAt time.Time `json:"first_at,omitempty"`
	LastAt  time.Time `json:"last_at,omitempty"`

	// LastSuccessAt and LastFailureAt are when each outcome was most recently
	// recorded, and they are what the circuit breaker is derived from.
	//
	// They are kept separately from LastAt because their ORDER carries the
	// meaning: a success after the latest failure says the account is working
	// again, whatever the window's failure count is. A single "last activity"
	// timestamp cannot express that, and a breaker built on one would keep
	// tripping on failures the account had already recovered from for as long
	// as they sat inside the window.
	LastSuccessAt time.Time `json:"last_success_at,omitempty"`
	LastFailureAt time.Time `json:"last_failure_at,omitempty"`
}

// newConsumptionRollup returns an empty roll-up for scope with its maps ready.
func newConsumptionRollup(scope ConsumptionScope) *ConsumptionRollup {
	return &ConsumptionRollup{
		Scope:     scope,
		ByAccount: make(map[string]int64),
		ByModel:   make(map[string]int64),
		ByChild:   make(map[string]int64),
	}
}

// add folds one entry into the roll-up, attributing it to child when the scope
// has a level below it (an empty child adds no breakdown row).
func (r *ConsumptionRollup) add(entry ConsumptionEntry, child string) {
	r.Entries++
	r.InputTokens += entry.InputTokens
	r.OutputTokens += entry.OutputTokens
	r.CacheReadTokens += entry.CacheReadTokens
	r.CacheWriteTokens += entry.CacheWriteTokens
	r.TotalTokens += entry.TotalTokens()
	r.CostMicros += entry.CostMicros

	occurred := entry.OccurredAt.UTC()
	switch entry.Outcome {
	case ConsumptionOutcomeSuccess:
		r.Successes++
		if occurred.After(r.LastSuccessAt) {
			r.LastSuccessAt = occurred
		}
	case ConsumptionOutcomeFailure:
		r.Failures++
		if occurred.After(r.LastFailureAt) {
			r.LastFailureAt = occurred
		}
	}

	r.ByAccount[entry.AccountID] += entry.TotalTokens()
	r.ByModel[entry.ModelKey()] += entry.TotalTokens()
	if child != "" {
		r.ByChild[child] += entry.TotalTokens()
	}

	if r.FirstAt.IsZero() || occurred.Before(r.FirstAt) {
		r.FirstAt = occurred
	}
	if r.LastAt.IsZero() || occurred.After(r.LastAt) {
		r.LastAt = occurred
	}
}

// =============================================================================
// The ledger contract
// =============================================================================

// ConsumptionLedger is the durable, cross-host record of provider consumption
// and the authority every spend question is answered from.
type ConsumptionLedger interface {
	// Record durably stores one entry, exactly once.
	//
	// Recording an entry whose id is already present is a NO-OP when the
	// stored record is identical, so a caller that retries after an
	// ambiguous failure converges instead of double-charging. It is a typed
	// refusal (ConsumptionConflictError) when the stored record differs:
	// two different consumptions sharing one idempotency key is a defect in
	// the caller, and silently keeping either one hides it behind a number
	// that looks plausible.
	Record(ctx context.Context, entry ConsumptionEntry) error

	// Rollup aggregates every entry in scope's subtree. It never returns a
	// partial sum: any unreadable or undecodable record fails the call.
	Rollup(ctx context.Context, scope ConsumptionScope) (*ConsumptionRollup, error)

	// WindowUsage aggregates one account's consumption over the half-open
	// interval [from, to). It reads the quota plane, so its cost is
	// proportional to the window rather than to the account's history.
	WindowUsage(ctx context.Context, tenantID, accountID string, from, to time.Time) (*ConsumptionRollup, error)
}
