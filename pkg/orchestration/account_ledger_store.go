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
	consumptionPlaneWindow = "quota"
	consumptionPlaneChain  = "attribution"
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
	if err := l.put(ctx, consumptionPlaneWindow, l.windowKey(normalized), data, normalized.EntryID); err != nil {
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

// WindowUsage implements ConsumptionLedger.
func (l *kvConsumptionLedger) WindowUsage(ctx context.Context, tenantID, accountID string, from, to time.Time) (*ConsumptionRollup, error) {
	if l.kv == nil {
		return nil, &ConsumptionInvalidError{Field: "store", Message: "kv store must not be nil"}
	}
	if err := requireSegment("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := requireSegment("account_id", accountID); err != nil {
		return nil, err
	}

	// A window that cannot contain anything is refused rather than answered
	// with zero. The caller of a limit check reads a zero as headroom, so an
	// inverted or empty interval — which is a bug in whoever computed it —
	// would present itself as an account with nothing spent.
	if !to.After(from) {
		return nil, &ConsumptionInvalidError{
			Field:   "window",
			Message: "end must be after start; an empty window has no usable answer",
		}
	}

	from = from.UTC()
	to = to.UTC()
	rollup := newConsumptionRollup(ConsumptionScope{TenantID: tenantID})

	// Scan only the buckets the window overlaps. The last bucket is the one
	// holding to-1ns: using to itself would scan a bucket the half-open
	// interval cannot reach whenever to lands exactly on a boundary.
	base := consumptionWindowKeyspace + tenantID + keySeparator + accountID + keySeparator
	last := to.Add(-time.Nanosecond).Truncate(consumptionBucket)
	for bucket := from.Truncate(consumptionBucket); !bucket.After(last); bucket = bucket.Add(consumptionBucket) {
		prefix := base + nanosSegment(bucket) + keySeparator
		if err := l.scanEntries(ctx, prefix, func(_ string, entry ConsumptionEntry) error {
			at := entry.OccurredAt.UTC()
			if at.Before(from) || !at.Before(to) {
				return nil
			}
			// No child breakdown: the quota plane is keyed by account and
			// time, so the segment below the scan prefix is a timestamp. The
			// dimensions worth breaking a window down by — model, outcome —
			// are already fields on the roll-up.
			rollup.add(entry, "")
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return rollup, nil
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
func (l *kvConsumptionLedger) windowKey(entry ConsumptionEntry) string {
	bucket := entry.OccurredAt.UTC().Truncate(consumptionBucket)
	return consumptionWindowKeyspace + entry.TenantID + keySeparator +
		entry.AccountID + keySeparator +
		nanosSegment(bucket) + keySeparator +
		nanosSegment(entry.OccurredAt) + keySeparator +
		entry.EntryID
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
		{"account_id", entry.AccountID},
		{"engine", entry.Engine},
		{"model", entry.Model},
	}
	for _, r := range required {
		if err := requireSegment(r.field, r.value); err != nil {
			return err
		}
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
	if entry.InputTokens < 0 {
		return &ConsumptionInvalidError{Field: "input_tokens", Message: "must not be negative"}
	}
	if entry.OutputTokens < 0 {
		return &ConsumptionInvalidError{Field: "output_tokens", Message: "must not be negative"}
	}
	if entry.CostMicros < 0 {
		return &ConsumptionInvalidError{Field: "cost_micros", Message: "must not be negative"}
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
	return depth, nil
}
