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

	// consumptionAgentKeyspace is the Agent quota plane: the entries of the
	// sessions a tenant Agent ran, keyed by Agent and by the moment they
	// occurred, so an Agent's own spend window is read as cheaply as a
	// project's.
	//
	//	orch/costagent/{tenant}/{agent}/{bucket}/{occurredAtNanos}/{entryID}
	consumptionAgentKeyspace = "orch/costagent/"

	// consumptionProjectKeyspace and consumptionTenantKeyspace are the
	// project and tenant quota planes (go-quicbaas#1058, ruling F2): the same
	// record, keyed by project or by tenant and by the moment it occurred, so
	// a project's or a tenant's window — and a time-ranged insight — costs
	// what the window is worth rather than what the history is worth.
	consumptionProjectKeyspace = "orch/costproject/"
	consumptionTenantKeyspace  = "orch/costtenant/"
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

	// AccountID is the pooled account the provider billed, EMPTY for a turn
	// that drew on no pooled account — local inference on an operator's own
	// hardware, or a CLI that authenticates itself.
	//
	// OPTIONAL SINCE go-quicbaas#1058, and deliberately so. The owner's
	// 2026-09-23 request is that every model the factory runs is tracked,
	// local inference included; an unpooled turn used to be dropped here and
	// the factory's triage and local review spend was invisible. Such an
	// entry is written to the attribution chain and is NOT written to the
	// account quota plane: an allowance belongs to an account, and a turn
	// that drew on none drew on no allowance. It is never filed under a
	// placeholder account, which would make local inference look like the
	// tenant's largest pooled spender and let an account cap refuse it.
	AccountID string `json:"account_id,omitempty"`

	// Engine and Model identify what served the turn. Both are required — a
	// spend report that cannot say which engine ran cannot be reconciled
	// against a provider's own invoice, which itemizes by model.
	Engine string `json:"engine"`
	Model  string `json:"model"`

	// Provider is the vendor that list-prices Model ("anthropic", "openai",
	// "local"), taken from the price entry that priced the turn. Empty when
	// the turn is unpriced: the vendor of a model the price table does not
	// know is not something this layer can state.
	Provider string `json:"provider,omitempty"`

	// Kind is the runner that drove the job the turn belongs to ("sdlc" for
	// a worker, "review", "triage", "workflow"). Empty for a turn run
	// outside any job.
	Kind string `json:"kind,omitempty"`

	// Host is the node or worker host the session ran on, and AgentID the
	// control-loop agent that ran the turn when there was one. Both are
	// recorded on the entry and exposed through the API; neither is ever a
	// metric label, because hosts and agents are unbounded.
	Host    string `json:"host,omitempty"`
	AgentID string `json:"agent_id,omitempty"`

	// ResolvedAgentID is the tenant Agent the session ran as (agent_catalog.go),
	// empty for a session spawned on a raw backend. It is NOT AgentID, which is
	// the control loop's per-job identity; this is the catalog Agent an
	// operator defines, and the one whose own spend limits the entry counts
	// against. An entry naming one is also written to the Agent quota plane.
	ResolvedAgentID string `json:"resolved_agent_id,omitempty"`

	// Outcome is required and must be one of validConsumptionOutcomes.
	Outcome ConsumptionOutcome `json:"outcome"`

	// The provider's own counts, one per way they price. InputTokens is the
	// FRESH (uncached) prompt; the two cache dimensions are prompt tokens
	// served from or written into the provider's prompt cache, which price at
	// a fraction and a multiple of a fresh token respectively. Kept apart,
	// because a price applied to a folded figure overstates a cached turn
	// several-fold.
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64 `json:"cache_write_tokens,omitempty"`

	// DurationMillis is how long the turn took, from the moment it was handed
	// to the backend to the moment its answer (or failure) came back.
	DurationMillis int64 `json:"duration_ms,omitempty"`

	// Unmeasured marks a turn whose backend reported no token counts — the
	// ACP JSON-RPC dialect and `codex exec` never do, and a failed turn often
	// does not. Its zero tokens are an ABSENCE, not a measurement, so a
	// roll-up counts it as incomplete money coverage (see
	// ConsumptionTotals.add) rather than letting a subtotal over it read as
	// complete (go-quicbaas#1058 review).
	Unmeasured bool `json:"unmeasured,omitempty"`

	// EquivalentMicros is what the turn's tokens cost at list price, in
	// millionths of a US dollar — what the same consumption would have been
	// billed on the provider's metered API, whatever the account actually
	// pays. For local inference it is the operator's configured hardware
	// rate. An integer, deliberately: money summed as float64 does not
	// associate, so two roll-ups over the same entries in different orders
	// could report different totals.
	//
	// STAMPED WHEN THE TURN IS RECORDED and never recomputed, so a price
	// changed next week does not re-price this week's turns. PriceRef names
	// the price entry it was stamped from; an entry with no PriceRef is
	// UNPRICED — its model is not in the price table — and zero on it means
	// unmeasured, never free.
	EquivalentMicros int64  `json:"equivalent_micros,omitempty"`
	PriceRef         string `json:"price_ref,omitempty"`

	// PriceBasis is how EquivalentMicros was worked out — PriceBasisTokenRate
	// or PriceBasisElectricity. It is empty on an unpriced entry, except one
	// that would have been costed from electricity but has no measured
	// duration: that carries the basis and no PriceRef, so its cost reads as
	// unknown, never as zero. EnergyMilliWh is
	// the energy an electricity-costed turn drew, in milliwatt-hours (a kWh
	// is 1e6), zero otherwise. Both are stamped with the price.
	PriceBasis    string `json:"price_basis,omitempty"`
	EnergyMilliWh int64  `json:"energy_mwh,omitempty"`

	// EnergyProvenance says how EnergyMilliWh came to be known: measured from
	// a device's power sensor, derived from its counters or from a measured
	// busy fraction scaled by the configured draw, or estimated from the
	// configured draw and the turn's duration alone. Unknown (omitted) on an
	// entry with no energy figure.
	EnergyProvenance Provenance `json:"energy_provenance,omitempty"`

	// Billing is how the paying account is billed, stamped when the turn is
	// recorded, and BilledMicros what this turn adds to the bill under it:
	// zero for a subscription (the fee does not move with use), the list
	// price for a metered API key, the operator's configured rate for an
	// unpooled turn. See BilledKnown for when the figure is a measurement.
	Billing      BillingMode `json:"billing,omitempty"`
	BilledMicros int64       `json:"billed_micros,omitempty"`

	// OccurredAt is when the provider served the consumption. REQUIRED and
	// non-zero: it is the window segment of the quota key, so an entry
	// without it would land at the epoch and be invisible to every live
	// window. Stored in UTC.
	OccurredAt time.Time `json:"occurred_at"`
}

// TotalTokens returns the entry's combined token count: every dimension the
// provider metered, cached or not.
func (e ConsumptionEntry) TotalTokens() int64 {
	return e.InputTokens + e.OutputTokens + e.CacheReadTokens + e.CacheWriteTokens
}

// Priced reports whether the entry carries a list-price figure.
func (e ConsumptionEntry) Priced() bool {
	return e.PriceRef != ""
}

// BilledKnown reports whether BilledMicros is a measurement: the entry's
// billing mode is known, and — unless a subscription, whose per-turn figure
// is zero by definition — the turn was priced.
func (e ConsumptionEntry) BilledKnown() bool {
	switch e.Billing {
	case BillingSubscription:
		return true
	case BillingAPI, BillingLocal:
		return e.Priced()
	default:
		return false
	}
}

// Local reports whether the entry is local inference on the operator's own
// hardware: costed from electricity, or priced by an entry whose provider is
// "local".
func (e ConsumptionEntry) Local() bool {
	return e.PriceBasis == PriceBasisElectricity || e.Provider == localProvider
}

// Pooled reports whether the entry drew on a pooled provider account.
func (e ConsumptionEntry) Pooled() bool {
	return e.AccountID != ""
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

	// From and To, when set, narrow the roll-up to the entries that occurred
	// in [From, To). Both or neither: a range with one open end is refused,
	// because an open start is the tenant's whole history and an open end is
	// a clock this layer does not own.
	//
	// A RANGE IS A FILTER ON TIME, NOT ANOTHER PATH LEVEL. A tenant scope is
	// answered from the tenant quota plane and a project scope from the
	// project quota plane (go-quicbaas#1058, ruling F2), so its cost is
	// proportional to the range rather than to the history; a deeper scope is
	// already a small subtree and is scanned and filtered.
	From time.Time
	To   time.Time

	// Host, when set, narrows the roll-up to the entries whose turn ran on
	// that host — the host drill-down. consumptionUnattributed selects the
	// entries that name no host. Like a range it is a FILTER, not a path
	// level: the subtree read is the scope's, and entries on other hosts are
	// skipped.
	Host string

	// Agent, when set, narrows the roll-up to the entries of the sessions one
	// tenant Agent ran (ConsumptionEntry.ResolvedAgentID) — the Agent
	// drill-down. consumptionUnattributed selects the entries of sessions
	// spawned on a raw backend. A filter, like Host.
	Agent string
}

// admits reports whether entry passes the scope's host and Agent filters.
func (s ConsumptionScope) admits(entry ConsumptionEntry) bool {
	return (s.Host == "" || chainSegment(entry.Host) == s.Host) &&
		(s.Agent == "" || chainSegment(entry.ResolvedAgentID) == s.Agent)
}

// Ranged reports whether the scope narrows the roll-up to a time range.
func (s ConsumptionScope) Ranged() bool {
	return !s.From.IsZero() || !s.To.IsZero()
}

// ConsumptionTotals is the additive part of a roll-up: what a set of entries
// sums to. It is the shape of the roll-up itself and of every breakdown row
// under it, so a project row and the tenant total are the same arithmetic and
// cannot disagree about what a column means.
type ConsumptionTotals struct {
	Entries          int   `json:"entries"`
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	TotalTokens      int64 `json:"total_tokens"`

	// EquivalentMicros sums the PRICED entries' list-price figures.
	// PricedEntries and UnpricedEntries count the entries that consumed
	// tokens with and without a price, so a reader can tell a complete figure
	// from a partial one and an unmeasured one: no priced entries is a dash,
	// never a zero. An entry that consumed no tokens is neither.
	EquivalentMicros int64 `json:"equivalent_micros"`
	PricedEntries    int   `json:"priced_entries"`
	UnpricedEntries  int   `json:"unpriced_entries"`

	// BilledMicros sums what the entries whose bill is known add to it.
	// BilledEntries and UnbilledEntries count the entries that consumed
	// tokens with and without a known billed figure, under the same rule as
	// the equivalent: no billed entries is a dash, never a zero.
	BilledMicros    int64 `json:"billed_micros"`
	BilledEntries   int   `json:"billed_entries"`
	UnbilledEntries int   `json:"unbilled_entries"`

	// DurationMillis sums the entries' turn durations.
	DurationMillis int64 `json:"duration_ms"`

	// EnergyMilliWh sums the energy drawn by the entries costed from
	// electricity, in milliwatt-hours, and ElectricityEntries counts them.
	// Consult ElectricityEntries before rendering the energy: none is a dash.
	EnergyMilliWh      int64 `json:"energy_mwh"`
	ElectricityEntries int   `json:"electricity_entries"`

	// MeasuredEnergyEntries counts the electricity entries whose energy came
	// from the fleet's device telemetry (measured or derived) rather than
	// from a configured draw. ElectricityEntries - MeasuredEnergyEntries were
	// estimated.
	MeasuredEnergyEntries int `json:"measured_energy_entries"`

	// Successes and Failures partition Entries.
	Successes int `json:"successes"`
	Failures  int `json:"failures"`
}

// add folds one entry into the totals.
func (t *ConsumptionTotals) add(entry ConsumptionEntry) {
	t.Entries++
	t.InputTokens += entry.InputTokens
	t.OutputTokens += entry.OutputTokens
	t.CacheReadTokens += entry.CacheReadTokens
	t.CacheWriteTokens += entry.CacheWriteTokens
	t.TotalTokens += entry.TotalTokens()
	t.DurationMillis += entry.DurationMillis
	// An unmeasured turn cost something nobody knows, so it counts against
	// coverage like an unpriced one: a figure beside it is a floor. A
	// measured turn that consumed nothing is neither priced nor unpriced.
	if entry.PriceBasis == PriceBasisElectricity && entry.Priced() {
		t.ElectricityEntries++
		t.EnergyMilliWh += entry.EnergyMilliWh
		if entry.EnergyProvenance.FromTelemetry() {
			t.MeasuredEnergyEntries++
		}
	}
	switch {
	case entry.Priced():
		t.PricedEntries++
		t.EquivalentMicros += entry.EquivalentMicros
	case entry.Unmeasured, entry.TotalTokens() > 0, entry.PriceBasis == PriceBasisElectricity:
		// The last case is a turn costed from electricity whose duration
		// was not measured: its cost is unknown, which is missing coverage.
		t.UnpricedEntries++
	}
	// The billed figure has one exception: a subscription turn bills zero
	// whatever it consumed, so even an unmeasured one is a known bill.
	switch {
	case entry.TotalTokens() == 0 && !entry.Unmeasured && !entry.Priced():
	case entry.BilledKnown():
		t.BilledEntries++
		t.BilledMicros += entry.BilledMicros
	default:
		t.UnbilledEntries++
	}
	switch entry.Outcome {
	case ConsumptionOutcomeSuccess:
		t.Successes++
	case ConsumptionOutcomeFailure:
		t.Failures++
	}
}

// ConsumptionRollup is the aggregate over one scope's subtree.
//
// Every field is derived from entries actually read. There is no cached total
// and no partial result: a roll-up either summed the whole subtree or returned
// an error.
type ConsumptionRollup struct {
	Scope ConsumptionScope `json:"scope"`

	ConsumptionTotals

	// ByAccount and ByModel break the totals down by the two dimensions that
	// are not part of the chain. ByAccount holds pooled entries only: an
	// unpooled entry has no account to be attributed to (see
	// ConsumptionEntry.AccountID).
	ByAccount map[string]*ConsumptionTotals `json:"by_account,omitempty"`
	ByModel   map[string]*ConsumptionTotals `json:"by_model,omitempty"`

	// ByHost breaks the totals down by the host each turn ran on. An entry
	// that names no host is filed under consumptionUnattributed.
	ByHost map[string]*ConsumptionTotals `json:"by_host,omitempty"`

	// ByAgent breaks the totals down by the tenant Agent each turn's session
	// ran as. An entry of a raw-backend session is filed under
	// consumptionUnattributed.
	ByAgent map[string]*ConsumptionTotals `json:"by_agent,omitempty"`

	// ByChild breaks the totals down by the next chain level below the scope
	// — the drill-down step. It is empty at the deepest level, where there is
	// no level below to attribute to.
	ByChild map[string]*ConsumptionTotals `json:"by_child,omitempty"`

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
		ByAccount: make(map[string]*ConsumptionTotals),
		ByModel:   make(map[string]*ConsumptionTotals),
		ByHost:    make(map[string]*ConsumptionTotals),
		ByAgent:   make(map[string]*ConsumptionTotals),
		ByChild:   make(map[string]*ConsumptionTotals),
	}
}

// addTo folds entry into the row named key of breakdown, creating the row on
// first sight.
func addTo(breakdown map[string]*ConsumptionTotals, key string, entry ConsumptionEntry) {
	row, ok := breakdown[key]
	if !ok {
		row = &ConsumptionTotals{}
		breakdown[key] = row
	}
	row.add(entry)
}

// add folds one entry into the roll-up. child is the entry's segment at the
// level below the scope, or empty when the scope is already the deepest level.
func (r *ConsumptionRollup) add(entry ConsumptionEntry, child string) {
	if !r.Scope.admits(entry) {
		return
	}
	r.ConsumptionTotals.add(entry)

	occurred := entry.OccurredAt.UTC()
	switch entry.Outcome {
	case ConsumptionOutcomeSuccess:
		if occurred.After(r.LastSuccessAt) {
			r.LastSuccessAt = occurred
		}
	case ConsumptionOutcomeFailure:
		if occurred.After(r.LastFailureAt) {
			r.LastFailureAt = occurred
		}
	}

	if entry.Pooled() {
		addTo(r.ByAccount, entry.AccountID, entry)
	}
	addTo(r.ByModel, entry.ModelKey(), entry)
	addTo(r.ByHost, chainSegment(entry.Host), entry)
	addTo(r.ByAgent, chainSegment(entry.ResolvedAgentID), entry)
	if child != "" {
		addTo(r.ByChild, child, entry)
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

	// ProjectWindowUsage sums one project's consumption in [from, to) —
	// pooled and unpooled alike — from the project quota plane.
	ProjectWindowUsage(ctx context.Context, tenantID, projectID string, from, to time.Time) (*ConsumptionRollup, error)

	// TenantWindowUsage sums a tenant's whole consumption in [from, to) from
	// the tenant quota plane.
	TenantWindowUsage(ctx context.Context, tenantID string, from, to time.Time) (*ConsumptionRollup, error)

	// AgentWindowUsage sums one tenant Agent's consumption in [from, to) from
	// the Agent quota plane: every turn of every session that ran as it,
	// pooled or not, whichever project it served.
	AgentWindowUsage(ctx context.Context, tenantID, agentID string, from, to time.Time) (*ConsumptionRollup, error)
}
