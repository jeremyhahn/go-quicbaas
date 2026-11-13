// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// KVSessionEventLog is a SessionEventLog backed by a KVStore — PebbleDB in
// standalone mode, Raft-replicated in cluster mode, transparently to
// callers. Event payloads are stored as plaintext JSON; confidentiality at
// rest is provided by qrdb's StorageBarrier, which encrypts all data written
// to the underlying store. Tenants requiring cryptographic isolation may
// configure a dedicated per-tenant barrier sealed by any xkms hardware or
// cloud strategy.
//
// Key layout (lexically sortable = chronological):
//
//	sessions/{tenant}/{session}/meta            → JSON-encoded SessionMeta
//	sessions/{tenant}/{session}/events/{seq:020d} → JSON-encoded SessionEvent
//
// The 20-digit zero-padded sequence suffix guarantees lexical order matches
// numeric order up to 10^20 events per session — far beyond any plausible
// single-session volume.
//
// Concurrency: appends are serialized per-session via sync.Mutex. Reads are
// lock-free: the KV store handles concurrent access itself. The fan-out hub
// is shared with the in-memory log so subscribe semantics are identical.
type KVSessionEventLog struct {
	kv     interfaces.KVStore
	fanout *sessionLogFanout

	// creator is kv's create-if-absent capability, when it has one: the
	// event write uses it so an append can never replace a stored event.
	creator   guardedCreator
	keyPrefix string // usually "sessions"

	// Per-session append serialization so Sequence validation stays atomic
	// against racing writers. Keyed by tenant\x00session.
	appendMu  sync.Mutex
	appendMus map[string]*sync.Mutex

	// retention bounds each session's transcript; see SessionRetention.
	retention SessionRetention

	// onRetentionError reports a prune that could not complete.
	onRetentionError func(error)
}

// Compile-time interface check.
var _ SessionEventLog = (*KVSessionEventLog)(nil)

// KVSessionEventLogConfig bundles dependencies for NewKVSessionEventLog.
type KVSessionEventLogConfig struct {
	// KVStore is required. Reads and writes are routed through it.
	KVStore interfaces.KVStore

	// KeyPrefix overrides the default "sessions" namespace. Rarely needed;
	// useful for tests that want isolated key spaces on a shared store.
	KeyPrefix string

	// Retention bounds each session's transcript. The zero value keeps
	// DefaultSessionRetentionTurns turns per session.
	Retention SessionRetention

	// OnRetentionError receives a prune failure. Nil discards it: the append
	// that triggered the prune already succeeded, so a transcript one turn
	// over its bound is worth reporting and not worth failing a turn for.
	OnRetentionError func(error)
}

// NewKVSessionEventLog constructs a KV-backed event log.
func NewKVSessionEventLog(cfg KVSessionEventLogConfig) (*KVSessionEventLog, error) {
	if cfg.KVStore == nil {
		return nil, errors.New("orchestration session log: kv store required")
	}
	prefix := cfg.KeyPrefix
	if prefix == "" {
		prefix = "sessions"
	}
	l := &KVSessionEventLog{
		kv:               cfg.KVStore,
		fanout:           newSessionLogFanout(),
		keyPrefix:        prefix,
		appendMus:        map[string]*sync.Mutex{},
		retention:        cfg.Retention,
		onRetentionError: cfg.OnRetentionError,
	}
	// Asserted once, as NewAccountStore does: the capability is a static
	// property of the injected store.
	if creator, ok := cfg.KVStore.(guardedCreator); ok {
		l.creator = creator
	}
	return l, nil
}

// writeEvent stores one event payload. Where the store can create-if-absent,
// an occupied key is refused with ErrSessionLogGap rather than overwritten:
// a sequence is written once, so finding it taken means the caller's view of
// the tail was stale and the stored event must survive. A store without the
// capability falls back to Put, guarded by LatestSequence's events-derived
// tail.
func (l *KVSessionEventLog) writeEvent(ctx context.Context, key string, payload []byte) error {
	if l.creator == nil {
		return l.kv.Put(ctx, key, payload)
	}
	stored, err := l.creator.PutIfAbsent(ctx, key, payload)
	if err != nil {
		return err
	}
	if !stored {
		return ErrSessionLogGap
	}
	return nil
}

// Append implements SessionEventLog.
func (l *KVSessionEventLog) Append(ctx context.Context, event *SessionEvent) error {
	if event == nil {
		return &SessionLogAppendError{Cause: errors.New("nil event")}
	}
	if event.SessionID == "" || event.TenantID == "" {
		return &SessionLogAppendError{Cause: errors.New("session_id and tenant_id required")}
	}
	ensureTimestamp(event)

	mu := l.lockFor(event.TenantID, event.SessionID)
	mu.Lock()
	defer mu.Unlock()

	// Validate monotonicity by reading the current latest sequence.
	latest, err := l.LatestSequence(ctx, event.TenantID, event.SessionID)
	if err != nil {
		return &SessionLogAppendError{SessionID: event.SessionID, Sequence: event.Sequence, Cause: err}
	}
	if event.Sequence != latest+1 {
		return ErrSessionLogGap
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return &SessionLogAppendError{SessionID: event.SessionID, Sequence: event.Sequence, Cause: err}
	}

	if err := l.writeEvent(ctx, l.eventKey(event.TenantID, event.SessionID, event.Sequence), payload); err != nil {
		if errors.Is(err, ErrSessionLogGap) {
			return err
		}
		return &SessionLogAppendError{SessionID: event.SessionID, Sequence: event.Sequence, Cause: err}
	}

	// Update meta's rolling fields if the session has meta registered, and
	// prune the transcript to the tenant's retention bound in the same pass:
	// the bookkeeping the prune needs (which sequence each retained turn began
	// at) lives on the metadata record this block is already rewriting, so
	// doing it anywhere else would mean a second read-modify-write of the same
	// key under the same lock.
	if meta, metaErr := l.GetMeta(ctx, event.TenantID, event.SessionID); metaErr == nil {
		meta.LatestSequence = event.Sequence
		meta.EventCount = event.Sequence // monotonic = dense, so count == latest
		meta.LastActivity = event.Timestamp
		l.applyRetention(ctx, meta, event)
		// Non-fatal on write failure: the event is durable, and LatestSequence
		// derives the tail from the stored events (the meta value is only a
		// starting hint), so a stale record cannot steer a later append onto
		// this event's sequence. The next successful append repairs it.
		_ = l.PutMeta(ctx, meta)
	}

	l.fanout.publish(event.TenantID, event.SessionID, event)
	return nil
}

// Range implements SessionEventLog.
func (l *KVSessionEventLog) Range(ctx context.Context, tenantID, sessionID string, fromSeq uint64, limit int) ([]*SessionEvent, uint64, error) {
	if fromSeq == 0 {
		fromSeq = 1
	}
	startKey := l.eventKey(tenantID, sessionID, fromSeq)
	prefix := l.eventsPrefix(tenantID, sessionID)

	// Collect all keys >= startKey under the events prefix. The KVStore
	// interface doesn't expose a bounded range scan — we use Scan with the
	// shared prefix then filter client-side. Acceptable because sessions
	// tend to have <10k events and the in-memory comparison is cheap.
	type kvEntry struct {
		key   string
		value []byte
	}
	var entries []kvEntry
	err := l.kv.Scan(ctx, prefix, func(key string, value []byte) error {
		if key < startKey {
			return nil
		}
		clone := make([]byte, len(value))
		copy(clone, value)
		entries = append(entries, kvEntry{key, clone})
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })

	out := make([]*SessionEvent, 0, len(entries))
	for i, e := range entries {
		if limit > 0 && i >= limit {
			// Caller requested a bounded page; the next sequence is one past
			// the last event in the page.
			last := out[len(out)-1]
			return out, last.Sequence + 1, nil
		}
		var ev SessionEvent
		if uErr := json.Unmarshal(e.value, &ev); uErr != nil {
			return nil, 0, &SessionLogError{Op: "unmarshal event", Cause: uErr}
		}
		out = append(out, &ev)
	}
	return out, 0, nil
}

// LatestSequence implements SessionEventLog.
func (l *KVSessionEventLog) LatestSequence(ctx context.Context, tenantID, sessionID string) (uint64, error) {
	// The meta record's LatestSequence is a HINT, not the tail: Append rolls
	// it forward in a second write after the event lands, and that write can
	// fail. Trusting a stale hint would hand the next append a sequence that
	// already holds an event, and the append would overwrite it (#200). So
	// start at the hint and walk forward over occupied event keys; in the
	// healthy case that costs one Exists. With no meta at all, scan the
	// events prefix for the max key.
	if meta, err := l.GetMeta(ctx, tenantID, sessionID); err == nil {
		return l.tailFrom(ctx, tenantID, sessionID, meta.LatestSequence)
	} else if !errors.Is(err, ErrSessionLogNotFound) {
		return 0, err
	}

	prefix := l.eventsPrefix(tenantID, sessionID)
	var maxSeq uint64
	err := l.kv.Scan(ctx, prefix, func(key string, _ []byte) error {
		seq, ok := parseSequenceFromKey(key, prefix)
		if ok && seq > maxSeq {
			maxSeq = seq
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return maxSeq, nil
}

// tailFrom returns the highest sequence of the contiguous run of stored events
// that continues past hint: it probes hint+1, hint+2, … and stops at the first
// unoccupied key. Sequences are dense (Append refuses gaps), so the first free
// key after the hint is the true next slot.
func (l *KVSessionEventLog) tailFrom(ctx context.Context, tenantID, sessionID string, hint uint64) (uint64, error) {
	tail := hint
	for {
		occupied, err := l.kv.Exists(ctx, l.eventKey(tenantID, sessionID, tail+1))
		if err != nil {
			if qerrors.IsNotFound(err) {
				return tail, nil
			}
			return 0, err
		}
		if !occupied {
			return tail, nil
		}
		tail++
	}
}

// Subscribe implements SessionEventLog.
func (l *KVSessionEventLog) Subscribe(ctx context.Context, tenantID, sessionID string) (<-chan *SessionEvent, func(), error) {
	return l.fanout.subscribe(tenantID, sessionID)
}

// PutMeta implements SessionEventLog.
func (l *KVSessionEventLog) PutMeta(ctx context.Context, meta *SessionMeta) error {
	if meta == nil || meta.SessionID == "" || meta.TenantID == "" {
		return errors.New("orchestration session log: put meta: session_id and tenant_id required")
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return l.kv.Put(ctx, l.metaKey(meta.TenantID, meta.SessionID), raw)
}

// GetMeta implements SessionEventLog.
func (l *KVSessionEventLog) GetMeta(ctx context.Context, tenantID, sessionID string) (*SessionMeta, error) {
	raw, err := l.kv.Get(ctx, l.metaKey(tenantID, sessionID))
	if err != nil {
		// KV stores signal "missing key" as a typed not-found error rather than
		// (nil, nil). Translate to the orchestration sentinel so callers can
		// rely on errors.Is(err, ErrSessionLogNotFound) regardless of which
		// backing store is plumbed in.
		if qerrors.IsNotFound(err) {
			return nil, ErrSessionLogNotFound
		}
		return nil, err
	}
	if raw == nil {
		return nil, ErrSessionLogNotFound
	}
	var meta SessionMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, &SessionLogError{Op: fmt.Sprintf("unmarshal meta %s/%s", tenantID, sessionID), Cause: err}
	}
	return &meta, nil
}

// ListSessions implements SessionEventLog.
func (l *KVSessionEventLog) ListSessions(ctx context.Context, tenantID string, filter *SessionFilter, limit int) ([]*SessionMeta, error) {
	prefix := l.keyPrefix + "/" + logTenant(tenantID) + "/"
	var metas []*SessionMeta
	err := l.kv.Scan(ctx, prefix, func(key string, value []byte) error {
		// Only meta keys end in /meta; skip event keys.
		if !strings.HasSuffix(key, "/meta") {
			return nil
		}
		var meta SessionMeta
		if umErr := json.Unmarshal(value, &meta); umErr != nil {
			// Skip malformed rather than aborting the scan; the operator
			// can audit the KV store separately.
			return nil
		}
		if !matchesFilter(&meta, filter) {
			return nil
		}
		metas = append(metas, &meta)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].CreatedAt.After(metas[j].CreatedAt)
	})
	if limit > 0 && len(metas) > limit {
		metas = metas[:limit]
	}
	return metas, nil
}

// =============================================================================
// Helpers
// =============================================================================

// eventKey builds the KV key for a single event. The sequence is zero-padded
// to 20 digits so lexical order == numeric order.
func (l *KVSessionEventLog) eventKey(tenantID, sessionID string, seq uint64) string {
	return fmt.Sprintf("%s/%s/%s/events/%020d", l.keyPrefix, logTenant(tenantID), sessionID, seq)
}

// eventsPrefix is the common prefix for all events of one session.
func (l *KVSessionEventLog) eventsPrefix(tenantID, sessionID string) string {
	return fmt.Sprintf("%s/%s/%s/events/", l.keyPrefix, logTenant(tenantID), sessionID)
}

// metaKey is the KV key for the session's metadata record.
func (l *KVSessionEventLog) metaKey(tenantID, sessionID string) string {
	return fmt.Sprintf("%s/%s/%s/meta", l.keyPrefix, logTenant(tenantID), sessionID)
}

// lockFor returns a per-session mutex. Callers Lock before appending so that
// Sequence validation against LatestSequence() is atomic vs concurrent writers
// for the same session. Cross-session writes run in parallel.
func (l *KVSessionEventLog) lockFor(tenantID, sessionID string) *sync.Mutex {
	key := memKey(tenantID, sessionID)
	l.appendMu.Lock()
	defer l.appendMu.Unlock()
	mu, ok := l.appendMus[key]
	if !ok {
		mu = &sync.Mutex{}
		l.appendMus[key] = mu
	}
	return mu
}

// parseSequenceFromKey extracts the trailing 20-digit sequence from an
// events key. Returns 0/false if the suffix isn't a well-formed sequence.
func parseSequenceFromKey(key, prefix string) (uint64, bool) {
	if !strings.HasPrefix(key, prefix) {
		return 0, false
	}
	suffix := key[len(prefix):]
	if len(suffix) != 20 {
		return 0, false
	}
	var seq uint64
	for i := 0; i < len(suffix); i++ {
		c := suffix[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		seq = seq*10 + uint64(c-'0')
	}
	return seq, true
}

// encodeSeqBE is a small helper for implementations that want binary
// sequence numbers in keys; we use zero-padded decimal today for
// readability, but this is here as a future-compat escape hatch.
func encodeSeqBE(seq uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], seq)
	return b[:]
}

// sinceDuration returns how long ago t occurred, in a format suitable for
// diagnostic logs. Unused by the log itself but useful for callers that
// stamp LastActivity before calling PutMeta.
func sinceDuration(t time.Time) time.Duration {
	if t.IsZero() {
		return 0
	}
	return time.Since(t)
}

// applyRetention records the turn boundary this event may have opened and
// drops whole turns that have fallen outside the tenant's retention bound.
//
// IT PRUNES BY WHOLE TURNS, never by event count, because half a turn is worse
// than none of it: an operator scrolling to the top of a transcript that had
// been cut mid-turn would see an agent's answer with no question above it, and
// a tool result with no invocation. TurnStarts on the metadata record makes
// that exact — the first retained turn's opening sequence IS the cutoff, so the
// delete is bounded and needs no scan.
//
// The caller holds the per-session append lock and writes the metadata record
// afterwards; this mutates meta in place and never writes it itself.
func (l *KVSessionEventLog) applyRetention(ctx context.Context, meta *SessionMeta, event *SessionEvent) {
	if event.Kind != KindPromptSubmitted {
		return
	}
	meta.TurnStarts = append(meta.TurnStarts, event.Sequence)
	if meta.OldestSequence == 0 {
		meta.OldestSequence = meta.TurnStarts[0]
	}

	maxTurns := l.retention.TurnsFor(meta.TenantID)
	if maxTurns <= 0 || len(meta.TurnStarts) <= maxTurns {
		return
	}

	// Everything below the first turn we intend to keep goes. Deleting from
	// OldestSequence rather than from 1 keeps a second prune from re-walking
	// keys the first one already removed.
	drop := len(meta.TurnStarts) - maxTurns
	cutoff := meta.TurnStarts[drop]
	from := meta.OldestSequence
	if from == 0 {
		from = 1
	}
	for seq := from; seq < cutoff; seq++ {
		if err := l.kv.Delete(ctx, l.eventKey(meta.TenantID, meta.SessionID, seq)); err != nil {
			// A KEY THAT WOULD NOT DELETE STOPS THE PRUNE WHERE IT STANDS.
			// Continuing past it would advance OldestSequence over a key that
			// is still there, and no later prune would ever revisit it: the
			// transcript would keep a permanently orphaned prefix that no
			// reader expects and no pass reclaims. Stopping leaves the bound
			// exceeded, which the next append retries from the same place.
			l.reportRetention(&SessionRetentionError{
				TenantID:  meta.TenantID,
				SessionID: meta.SessionID,
				Sequence:  seq,
				Cause:     err,
			})
			meta.OldestSequence = seq
			return
		}
	}
	meta.OldestSequence = cutoff
	meta.TurnStarts = append(meta.TurnStarts[:0], meta.TurnStarts[drop:]...)
}

// reportRetention surfaces a prune failure when a sink is configured.
func (l *KVSessionEventLog) reportRetention(err error) {
	if err != nil && l.onRetentionError != nil {
		l.onRetentionError(err)
	}
}

// DeleteSession implements SessionEventLog.
func (l *KVSessionEventLog) DeleteSession(ctx context.Context, tenantID, sessionID string) error {
	if sessionID == "" {
		return &SessionLogError{Op: "delete session: session_id required"}
	}

	// Under the append lock so a turn landing concurrently cannot write an
	// event between the scan and the delete and leave it stranded under a
	// metadata record that is already gone.
	mu := l.lockFor(tenantID, sessionID)
	mu.Lock()
	defer mu.Unlock()

	var keys []string
	if err := l.kv.Scan(ctx, l.eventsPrefix(tenantID, sessionID), func(key string, _ []byte) error {
		keys = append(keys, key)
		return nil
	}); err != nil {
		return &SessionTranscriptDeleteError{
			TenantID: tenantID, SessionID: sessionID,
			Key: l.eventsPrefix(tenantID, sessionID), Cause: err,
		}
	}
	// The metadata record goes LAST. It is what LatestSequence derives a new
	// event's sequence from, so removing it first would let a concurrent
	// append restart at sequence 1 and land on top of events this call has not
	// deleted yet.
	keys = append(keys, l.metaKey(tenantID, sessionID))
	for _, key := range keys {
		if err := l.kv.Delete(ctx, key); err != nil && !qerrors.IsNotFound(err) {
			return &SessionTranscriptDeleteError{
				TenantID: tenantID, SessionID: sessionID, Key: key, Cause: err,
			}
		}
	}
	return nil
}
