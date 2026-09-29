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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Plane names, used in errors so a partial or conflicting write says which of
// the two key paths it happened on.
const (
	consumptionPlaneWindow  = "quota"
	consumptionPlaneProject = "project-quota"
	consumptionPlaneTenant  = "tenant-quota"
	consumptionPlaneAgent   = "agent-quota"
	consumptionPlaneChain   = "attribution"
)

// consumptionChainDepth is how many chain levels a key carries: tenant,
// project, task, job, session, round.
const consumptionChainDepth = 6

// kvConsumptionLedger is the KV-backed ConsumptionLedger.
type kvConsumptionLedger struct {
	kv interfaces.KVStore

	// creator is kv's guarded-create capability, asserted once at
	// construction. Nil when the store cannot decide a contested write, in
	// which case Record refuses instead of probing and hoping.
	creator guardedCreator
}

// Compile-time interface satisfaction check.
var _ ConsumptionLedger = (*kvConsumptionLedger)(nil)

// NewConsumptionLedger returns a ConsumptionLedger backed by kv.
//
// The ledger holds no state of its own. Durability, replication and
// barrier encryption are properties of kv, and two ledgers built over one kv —
// on two hosts — see exactly the same entries. That is what makes this a single
// authority rather than a per-host tally.
func NewConsumptionLedger(kv interfaces.KVStore) ConsumptionLedger {
	ledger := &kvConsumptionLedger{kv: kv}
	if creator, ok := kv.(guardedCreator); ok {
		ledger.creator = creator
	}
	return ledger
}

// NewInMemoryConsumptionLedger returns a ledger over go-qrdb's memory KVStore.
//
// That store rather than kvstore.NewMemory(): recording an entry is a guarded
// create, and the raw storage adapter has no conditional write to delegate to,
// so a ledger over it would refuse every write.
func NewInMemoryConsumptionLedger() ConsumptionLedger {
	return NewConsumptionLedger(NewMemoryTaskKV())
}

// =============================================================================
// Record
// =============================================================================

// Record implements ConsumptionLedger.
func (l *kvConsumptionLedger) Record(ctx context.Context, entry ConsumptionEntry) error {
	if l.kv == nil {
		return &ConsumptionInvalidError{Field: "store", Message: "kv store must not be nil"}
	}
	if l.creator == nil {
		return &ConsumptionUnguardedError{
			Message: "exactly-once accounting needs a store that can decide a create; this one cannot",
		}
	}
	if err := validateConsumptionEntry(entry); err != nil {
		return err
	}

	normalized := entry.normalize()
	data, err := json.Marshal(normalized)
	if err != nil {
		return &ConsumptionWriteError{EntryID: entry.EntryID, Plane: consumptionPlaneChain, Cause: err}
	}

	// The quota plane is written FIRST, and the ordering is a deliberate
	// choice about which way a partial write fails.
	//
	// A partial write leaves spend visible to the LIMIT and missing from the
	// INVOICE, never the reverse. Over-refusing is recoverable; silent
	// overspend is not — the account is briefly charged for something the
	// invoice has not yet counted, which costs at worst a refused turn, while
	// the opposite order would leave the spend counted on the invoice and
	// invisible to the limit, which is how an account keeps spending past its
	// cap. Both planes are idempotent on the same entry id, so the caller's
	// retry completes the pair rather than duplicating either half.
	// AN UNPOOLED ENTRY HAS NO QUOTA PLANE. The plane is keyed by account
	// because an allowance belongs to an account; a turn that drew on none is
	// attributed and reported, never counted against an account's cap.
	if normalized.Pooled() {
		if err := l.put(ctx, consumptionPlaneWindow, l.windowKey(normalized), data, normalized.EntryID); err != nil {
			return err
		}
	}
	// The Agent quota plane is a limit's plane too, so it is written before
	// the invoice as well. A turn of a raw-backend session belongs to no
	// Agent and has none.
	if normalized.ResolvedAgentID != "" {
		if err := l.put(ctx, consumptionPlaneAgent, l.agentWindowKey(normalized), data, normalized.EntryID); err != nil {
			return err
		}
	}
	// The project and tenant quota planes come next, for the same reason:
	// every plane a limit is decided from is written before the invoice. An
	// entry attributed to no project has no project plane — a project's
	// allowance is not drawn on by work that belongs to none — and is still
	// counted against its tenant.
	if normalized.ProjectID != consumptionUnattributed {
		if err := l.put(ctx, consumptionPlaneProject, l.projectWindowKey(normalized), data, normalized.EntryID); err != nil {
			return err
		}
	}
	if err := l.put(ctx, consumptionPlaneTenant, l.tenantWindowKey(normalized), data, normalized.EntryID); err != nil {
		return err
	}
	return l.put(ctx, consumptionPlaneChain, l.chainKey(normalized), data, normalized.EntryID)
}

// put stores data at key if absent, and otherwise decides whether what is
// already there is the same record.
//
// Losing the guarded create is the ORDINARY case, not an error: it is what a
// retry of an entry that was already stored looks like, and what the loser of a
// two-host race sees. It becomes an error only when the stored bytes differ,
// which means one idempotency key was used for two different consumptions.
func (l *kvConsumptionLedger) put(ctx context.Context, plane, key string, data []byte, entryID string) error {
	stored, err := l.creator.PutIfAbsent(ctx, key, data)
	if err != nil {
		return &ConsumptionWriteError{EntryID: entryID, Plane: plane, Key: key, Cause: err}
	}
	if stored {
		return nil
	}

	existing, err := l.kv.Get(ctx, key)
	if err != nil {
		return &ConsumptionReadError{
			Key:     key,
			Message: "entry " + entryID + " was already present on the " + plane + " plane but could not be read back",
			Cause:   err,
		}
	}
	if !bytes.Equal(existing, data) {
		return &ConsumptionConflictError{EntryID: entryID, Plane: plane, Key: key}
	}
	return nil
}

// =============================================================================
// Roll-ups
// =============================================================================

// Rollup implements ConsumptionLedger.
func (l *kvConsumptionLedger) Rollup(ctx context.Context, scope ConsumptionScope) (*ConsumptionRollup, error) {
	if l.kv == nil {
		return nil, &ConsumptionInvalidError{Field: "store", Message: "kv store must not be nil"}
	}
	depth, err := validateConsumptionScope(scope)
	if err != nil {
		return nil, err
	}

	if scope.Ranged() {
		return l.rangedRollup(ctx, scope, depth)
	}

	prefix := l.chainPrefix(scope, depth)
	rollup := newConsumptionRollup(scope)

	// The child breakdown exists only where there is a chain level below the
	// scope. At the deepest level the next segment is the entry id itself,
	// and attributing tokens to entry ids would be a listing wearing the name
	// of a breakdown.
	wantChild := depth < consumptionChainDepth

	if err := l.scanEntries(ctx, prefix, func(key string, entry ConsumptionEntry) error {
		child := ""
		if wantChild {
			child = childSegment(key, prefix)
		}
		rollup.add(entry, child)
		return nil
	}); err != nil {
		return nil, err
	}
	return rollup, nil
}

// rangedRollup answers a scope narrowed to [scope.From, scope.To).
//
// The two shallow scopes read a quota plane, whose keys are ordered by time
// below their owner, so the scan touches only the hourly buckets the range
// overlaps. Their drill-down row is read off the entry itself — its project
// below a tenant, its task below a project — because a quota-plane key holds a
// timestamp where the chain key holds the next level. The stored entry carries
// the same normalized segment the chain key was built from, so the two answers
// agree row for row.
//
// Every deeper scope, and the no-project subtree (which has no project plane),
// is a bounded subtree of the chain and is scanned and filtered on time.
func (l *kvConsumptionLedger) rangedRollup(ctx context.Context, scope ConsumptionScope, depth int) (*ConsumptionRollup, error) {
	from, to := scope.From.UTC(), scope.To.UTC()
	rollup := newConsumptionRollup(scope)
	var base string
	var child func(ConsumptionEntry) string
	switch {
	case depth == 1:
		base = consumptionTenantKeyspace + scope.TenantID + keySeparator
		child = func(entry ConsumptionEntry) string { return chainSegment(entry.ProjectID) }
	case depth == 2 && scope.ProjectID != consumptionUnattributed:
		base = consumptionProjectKeyspace + scope.TenantID + keySeparator + scope.ProjectID + keySeparator
		child = func(entry ConsumptionEntry) string { return chainSegment(entry.TaskID) }
	}
	if child != nil {
		if err := l.scanPlane(ctx, base, from, to, func(entry ConsumptionEntry) {
			rollup.add(entry, child(entry))
		}); err != nil {
			return nil, err
		}
		return rollup, nil
	}
	prefix := l.chainPrefix(scope, depth)
	wantChild := depth < consumptionChainDepth
	if err := l.scanEntries(ctx, prefix, func(key string, entry ConsumptionEntry) error {
		if !occurredIn(entry, from, to) {
			return nil
		}
		child := ""
		if wantChild {
			child = childSegment(key, prefix)
		}
		rollup.add(entry, child)
		return nil
	}); err != nil {
		return nil, err
	}
	return rollup, nil
}

// occurredIn reports whether entry occurred in the half-open [from, to).
func occurredIn(entry ConsumptionEntry, from, to time.Time) bool {
	at := entry.OccurredAt.UTC()
	return !at.Before(from) && at.Before(to)
}

// WindowUsage implements ConsumptionLedger.
func (l *kvConsumptionLedger) WindowUsage(ctx context.Context, tenantID, accountID string, from, to time.Time) (*ConsumptionRollup, error) {
	if err := requireSegment("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := requireSegment("account_id", accountID); err != nil {
		return nil, err
	}
	return l.scanWindow(ctx, consumptionWindowKeyspace+tenantID+keySeparator+accountID+keySeparator, tenantID, from, to)
}

// ProjectWindowUsage implements ConsumptionLedger.
func (l *kvConsumptionLedger) ProjectWindowUsage(ctx context.Context, tenantID, projectID string, from, to time.Time) (*ConsumptionRollup, error) {
	if err := requireSegment("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := validateChainSegment("project_id", projectID); err != nil {
		return nil, err
	}
	if projectID == "" {
		return nil, &ConsumptionInvalidError{Field: "project_id", Message: "must be set; a project window belongs to one project"}
	}
	return l.scanWindow(ctx, consumptionProjectKeyspace+tenantID+keySeparator+projectID+keySeparator, tenantID, from, to)
}

// TenantWindowUsage implements ConsumptionLedger.
func (l *kvConsumptionLedger) TenantWindowUsage(ctx context.Context, tenantID string, from, to time.Time) (*ConsumptionRollup, error) {
	if err := requireSegment("tenant_id", tenantID); err != nil {
		return nil, err
	}
	return l.scanWindow(ctx, consumptionTenantKeyspace+tenantID+keySeparator, tenantID, from, to)
}

// AgentWindowUsage implements ConsumptionLedger.
func (l *kvConsumptionLedger) AgentWindowUsage(ctx context.Context, tenantID, agentID string, from, to time.Time) (*ConsumptionRollup, error) {
	if err := requireSegment("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := requireSegment("agent_id", agentID); err != nil {
		return nil, err
	}
	return l.scanWindow(ctx, consumptionAgentKeyspace+tenantID+keySeparator+agentID+keySeparator, tenantID, from, to)
}

// scanWindow sums the entries of one time-bucketed quota plane under base that
// occurred in [from, to).
func (l *kvConsumptionLedger) scanWindow(ctx context.Context, base, tenantID string, from, to time.Time) (*ConsumptionRollup, error) {
	rollup := newConsumptionRollup(ConsumptionScope{TenantID: tenantID})
	// No child breakdown: a quota plane is keyed by time below its owner, so
	// the segment below the scan prefix is a timestamp. The dimensions worth
	// breaking a window down by — account, model, outcome — are already fields
	// on the roll-up.
	if err := l.scanPlane(ctx, base, from, to, func(entry ConsumptionEntry) {
		rollup.add(entry, "")
	}); err != nil {
		return nil, err
	}
	return rollup, nil
}

// scanPlane hands fn every entry of one time-bucketed quota plane under base
// that occurred in [from, to).
func (l *kvConsumptionLedger) scanPlane(ctx context.Context, base string, from, to time.Time, fn func(ConsumptionEntry)) error {
	if l.kv == nil {
		return &ConsumptionInvalidError{Field: "store", Message: "kv store must not be nil"}
	}

	// A window that cannot contain anything is refused rather than answered
	// with zero. The caller of a limit check reads a zero as headroom, so an
	// inverted or empty interval — which is a bug in whoever computed it —
	// would present itself as an account with nothing spent.
	if !to.After(from) {
		return &ConsumptionInvalidError{
			Field:   "window",
			Message: "end must be after start; an empty window has no usable answer",
		}
	}

	from = from.UTC()
	to = to.UTC()

	// Scan only the buckets the window overlaps. The last bucket is the one
	// holding to-1ns: using to itself would scan a bucket the half-open
	// interval cannot reach whenever to lands exactly on a boundary.
	last := to.Add(-time.Nanosecond).Truncate(consumptionBucket)
	for bucket := from.Truncate(consumptionBucket); !bucket.After(last); bucket = bucket.Add(consumptionBucket) {
		prefix := base + nanosSegment(bucket) + keySeparator
		if err := l.scanEntries(ctx, prefix, func(_ string, entry ConsumptionEntry) error {
			if occurredIn(entry, from, to) {
				fn(entry)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// scanEntries walks prefix and hands each decoded entry to fn.
//
// A record that fails to decode fails the whole scan. Skipping it would make
// every total silently smaller than the truth, which is the shape of error that
// nobody notices until the numbers are compared against a provider's own.
func (l *kvConsumptionLedger) scanEntries(ctx context.Context, prefix string, fn func(key string, entry ConsumptionEntry) error) error {
	err := l.kv.Scan(ctx, prefix, func(key string, value []byte) error {
		var entry ConsumptionEntry
		if decodeErr := json.Unmarshal(value, &entry); decodeErr != nil {
			return &ConsumptionReadError{
				Key:     key,
				Message: "stored consumption record could not be decoded",
				Cause:   decodeErr,
			}
		}
		return fn(key, entry)
	})
	if err == nil {
		return nil
	}
	// A read error raised by the callback is already typed and already names
	// the record it came from; re-wrapping it would bury that key.
	var readErr *ConsumptionReadError
	if errors.As(err, &readErr) {
		return err
	}
	return &ConsumptionReadError{Key: prefix, Message: "subtree scan failed", Cause: err}
}

// =============================================================================
// Key construction
// =============================================================================

// chainKey builds the attribution-plane key for a normalized entry.
func (l *kvConsumptionLedger) chainKey(entry ConsumptionEntry) string {
	return consumptionChainKeyspace + strings.Join([]string{
		entry.TenantID,
		entry.ProjectID,
		entry.TaskID,
		entry.JobID,
		entry.SessionID,
		roundSegment(entry.Round),
		entry.EntryID,
	}, keySeparator)
}

// windowKey builds the quota-plane key for a normalized entry.
// timeKeySuffix is the bucket/instant/entry tail every quota plane shares.
func timeKeySuffix(entry ConsumptionEntry) string {
	return nanosSegment(entry.OccurredAt.UTC().Truncate(consumptionBucket)) + keySeparator +
		nanosSegment(entry.OccurredAt) + keySeparator +
		entry.EntryID
}

// projectWindowKey is the entry's key on the project quota plane.
func (l *kvConsumptionLedger) projectWindowKey(entry ConsumptionEntry) string {
	return consumptionProjectKeyspace + entry.TenantID + keySeparator + entry.ProjectID + keySeparator + timeKeySuffix(entry)
}

// agentWindowKey is the entry's key on the Agent quota plane.
func (l *kvConsumptionLedger) agentWindowKey(entry ConsumptionEntry) string {
	return consumptionAgentKeyspace + entry.TenantID + keySeparator + entry.ResolvedAgentID + keySeparator + timeKeySuffix(entry)
}

// tenantWindowKey is the entry's key on the tenant quota plane.
func (l *kvConsumptionLedger) tenantWindowKey(entry ConsumptionEntry) string {
	return consumptionTenantKeyspace + entry.TenantID + keySeparator + timeKeySuffix(entry)
}

func (l *kvConsumptionLedger) windowKey(entry ConsumptionEntry) string {
	return consumptionWindowKeyspace + entry.TenantID + keySeparator + entry.AccountID + keySeparator + timeKeySuffix(entry)
}

// chainPrefix builds the scan prefix for a scope validated to depth levels.
func (l *kvConsumptionLedger) chainPrefix(scope ConsumptionScope, depth int) string {
	levels := []string{
		scope.TenantID,
		scope.ProjectID,
		scope.TaskID,
		scope.JobID,
		scope.SessionID,
		roundSegment(scope.Round),
	}
	var b strings.Builder
	b.WriteString(consumptionChainKeyspace)
	for i := 0; i < depth; i++ {
		b.WriteString(levels[i])
		b.WriteString(keySeparator)
	}
	return b.String()
}

// childSegment returns the segment immediately below prefix in key.
func childSegment(key, prefix string) string {
	rest := strings.TrimPrefix(key, prefix)
	if idx := strings.Index(rest, keySeparator); idx >= 0 {
		return rest[:idx]
	}
	return rest
}

// =============================================================================
// Validation
// =============================================================================

// validateConsumptionEntry refuses an entry that cannot be stored honestly.
func validateConsumptionEntry(entry ConsumptionEntry) error {
	required := []struct {
		field string
		value string
	}{
		{"entry_id", entry.EntryID},
		{"tenant_id", entry.TenantID},
		{"engine", entry.Engine},
	}
	for _, r := range required {
		if err := requireSegment(r.field, r.value); err != nil {
			return err
		}
	}
	// THE MODEL IS A PAYLOAD VALUE, NOT A KEY SEGMENT. No key is built from
	// it, and backends report provider-qualified names — opencode's
	// "ollama/review-q8:latest" — which a segment rule refused, so exactly
	// the local turns this ledger now records were dropped (go-quicbaas#1058
	// review). It must only be present.
	if strings.TrimSpace(entry.Model) == "" {
		return &ConsumptionInvalidError{Field: "model", Message: "must not be empty"}
	}

	// The optional chain levels are validated only when supplied, and are
	// additionally forbidden from spelling the unattributed sentinel: a task
	// literally named "-" must not become indistinguishable from an entry
	// that has no task.
	optional := []struct {
		field string
		value string
	}{
		{"project_id", entry.ProjectID},
		{"task_id", entry.TaskID},
		{"job_id", entry.JobID},
		{"session_id", entry.SessionID},
		{"resolved_agent_id", entry.ResolvedAgentID},
	}
	for _, o := range optional {
		if err := validateChainSegment(o.field, o.value); err != nil {
			return err
		}
	}

	if _, ok := validConsumptionOutcomes[entry.Outcome]; !ok {
		return &ConsumptionInvalidError{
			Field:   "outcome",
			Message: "must be one of success, failure; got " + string(entry.Outcome),
		}
	}
	if entry.Round < 0 {
		return &ConsumptionInvalidError{Field: "round", Message: "must not be negative"}
	}
	// The account is optional (an unpooled turn has none) but, when named,
	// it is a key segment of the quota plane and must be able to be one.
	if entry.AccountID != "" {
		if err := requireSegment("account_id", entry.AccountID); err != nil {
			return err
		}
	}
	counts := []struct {
		field string
		value int64
	}{
		{"input_tokens", entry.InputTokens},
		{"output_tokens", entry.OutputTokens},
		{"cache_read_tokens", entry.CacheReadTokens},
		{"cache_write_tokens", entry.CacheWriteTokens},
		{"duration_ms", entry.DurationMillis},
		{"equivalent_micros", entry.EquivalentMicros},
		{"billed_micros", entry.BilledMicros},
		{"energy_mwh", entry.EnergyMilliWh},
	}
	for _, c := range counts {
		if c.value < 0 {
			return &ConsumptionInvalidError{Field: c.field, Message: "must not be negative"}
		}
	}
	// A FIGURE WITHOUT ITS SOURCE IS REFUSED. A cost nobody can trace to a
	// price entry cannot be audited, and cannot be told apart from one a bug
	// invented.
	if entry.Billing != "" {
		if err := entry.Billing.validate("billing"); err != nil {
			return err
		}
	}
	if entry.EquivalentMicros > 0 && entry.PriceRef == "" {
		return &ConsumptionInvalidError{
			Field:   "price_ref",
			Message: "must name the price entry a non-zero equivalent_micros was stamped from",
		}
	}
	if entry.OccurredAt.IsZero() {
		return &ConsumptionInvalidError{
			Field:   "occurred_at",
			Message: "must be set; an entry with no time lands at the epoch and is invisible to every live window",
		}
	}
	return nil
}

// requireSegment refuses a required identifier that cannot form a key segment,
// restating the reason in this package's vocabulary rather than nesting the
// store's sentence inside the ledger's.
func requireSegment(field, value string) error {
	err := validateKeySegment(field, value)
	if err == nil {
		return nil
	}
	var segment *KeySegmentError
	if errors.As(err, &segment) {
		return &ConsumptionInvalidError{Field: field, Message: segment.Message}
	}
	return &ConsumptionInvalidError{Field: field, Message: err.Error()}
}

// validateChainSegment checks an optional chain level. An empty value is legal
// and becomes the unattributed sentinel; a supplied one may be neither a
// separator-bearing string nor the sentinel itself.
func validateChainSegment(field, value string) error {
	if value == "" {
		return nil
	}
	if err := requireSegment(field, value); err != nil {
		return err
	}
	if value == consumptionUnattributed {
		return &ConsumptionInvalidError{
			Field:   field,
			Message: "must not be " + consumptionUnattributed + ", which marks a level with no value",
		}
	}
	return nil
}

// validateScopeSegment checks one level of a READ scope.
//
// IT ACCEPTS THE UNATTRIBUTED SENTINEL WHERE validateChainSegment REFUSES IT,
// and the asymmetry is exactly the point. On the WRITE side the sentinel is
// refused as a caller-supplied value so that "no task" and "a task literally
// named -" can never produce the same key. That guarantee is what makes the
// sentinel unambiguous on the READ side: because no entity can bear that id,
// a scope naming it can only mean one thing — the subtree of entries that
// have no value at this level.
//
// Without this, the subtree is REPORTABLE BUT NOT ADDRESSABLE: a roll-up
// shows an unattributed row and nothing can open it, and a job filed under no
// task has spend that no scope can name. A dashboard that can see a number
// and cannot drill into it is the shape of report this package exists to
// avoid.
func validateScopeSegment(field, value string) error {
	if value == consumptionUnattributed {
		return nil
	}
	return validateChainSegment(field, value)
}

// validateConsumptionScope checks that the scope names a contiguous path from
// the tenant down, and returns how many levels deep it goes.
//
// Contiguity is the whole rule. Each level is a key segment, so a scope that
// skips one is not a prefix of anything: answering it would mean scanning the
// tenant's entire history and filtering, on the path a dashboard calls.
func validateConsumptionScope(scope ConsumptionScope) (int, error) {
	if err := requireSegment("tenant_id", scope.TenantID); err != nil {
		return 0, err
	}

	levels := []struct {
		field string
		value string
		set   bool
	}{
		{"project_id", scope.ProjectID, scope.ProjectID != ""},
		{"task_id", scope.TaskID, scope.TaskID != ""},
		{"job_id", scope.JobID, scope.JobID != ""},
		{"session_id", scope.SessionID, scope.SessionID != ""},
		{"round", "", scope.RoundSet},
	}

	depth := 1
	for _, level := range levels {
		if !level.set {
			break
		}
		if level.value != "" {
			if err := validateScopeSegment(level.field, level.value); err != nil {
				return 0, err
			}
		}
		depth++
	}

	// Everything below the first unset level must also be unset.
	for i := depth - 1; i < len(levels); i++ {
		if levels[i].set {
			return 0, &ConsumptionInvalidError{
				Field:   levels[i].field,
				Message: "cannot be scoped while a level above it is not; the chain is a path, not a filter",
			}
		}
	}
	if scope.RoundSet && scope.Round < 0 {
		return 0, &ConsumptionInvalidError{Field: "round", Message: "must not be negative"}
	}
	if scope.Host != "" && strings.TrimSpace(scope.Host) == "" {
		return 0, &ConsumptionInvalidError{Field: "host", Message: "must name a host, or be empty for every host"}
	}
	if scope.Agent != "" && scope.Agent != consumptionUnattributed && !validCatalogID(scope.Agent) {
		return 0, &ConsumptionInvalidError{Field: "agent", Message: "must name an Agent, \"-\" for the turns of no Agent, or be empty for every Agent"}
	}
	if scope.Ranged() {
		if scope.From.IsZero() || scope.To.IsZero() {
			return 0, &ConsumptionInvalidError{
				Field:   "range",
				Message: "set both from and to; an open start is the whole history and an open end a clock the ledger does not own",
			}
		}
		if !scope.To.After(scope.From) {
			return 0, &ConsumptionInvalidError{Field: "range", Message: "to must be after from"}
		}
	}
	return depth, nil
}
