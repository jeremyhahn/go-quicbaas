package orchestration

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
)

// errMetaWriteDown is the injected failure of the metadata roll-forward.
var errMetaWriteDown = errors.New("meta write unavailable")

// metaWriteFailingKV fails every Put to one session's metadata key while
// failMeta is set, and passes everything else through: the seam #200 names,
// where Append's second write (the roll-forward) fails after the event landed.
type metaWriteFailingKV struct {
	*memKVStore
	metaKey  string
	failMeta atomic.Bool
}

func (k *metaWriteFailingKV) Put(ctx context.Context, key string, value []byte) error {
	if key == k.metaKey && k.failMeta.Load() {
		return errMetaWriteDown
	}
	return k.memKVStore.Put(ctx, key, value)
}

// guardedMemKVStore is a memKVStore that can create-if-absent, so the log's
// event write takes the guarded path.
type guardedMemKVStore struct{ *memKVStore }

func (k *guardedMemKVStore) PutIfAbsent(_ context.Context, key string, value []byte) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.m[key]; ok {
		return false, nil
	}
	c := make([]byte, len(value))
	copy(c, value)
	k.m[key] = c
	return true, nil
}

// newStaleMetaLog builds a KV log over a store whose metadata write can be
// failed on demand, with the session's metadata registered.
func newStaleMetaLog(t *testing.T, tenant, sessionID string) (*KVSessionEventLog, *metaWriteFailingKV) {
	t.Helper()
	kv := &metaWriteFailingKV{memKVStore: newMemKVStore()}
	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: kv})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}
	kv.metaKey = log.metaKey(tenant, sessionID)
	if err := log.PutMeta(context.Background(), &SessionMeta{SessionID: sessionID, TenantID: tenant, Status: SessionStateActive}); err != nil {
		t.Fatalf("PutMeta: %v", err)
	}
	return log, kv
}

func appendPrompt(t *testing.T, ctx context.Context, log SessionEventLog, tenant, sessionID, text string) {
	t.Helper()
	if err := AppendNextSessionEvent(ctx, log, &SessionEvent{
		SessionID: sessionID, TenantID: tenant, Kind: KindPromptSubmitted,
		Prompt: &PromptSubmittedPayload{Prompt: text},
	}); err != nil {
		t.Fatalf("append %q: %v", text, err)
	}
}

// TestKVSessionEventLog_FailedMetaRollForwardKeepsTerminalEvent is #200's
// damage case: the roll-forward after the session's ending fails, so the
// metadata still names the event before it. A writer that takes its sequence
// from LatestSequence — the documented source — must be steered past the
// ending, not onto it, and the ending must still be stored and last-written.
func TestKVSessionEventLog_FailedMetaRollForwardKeepsTerminalEvent(t *testing.T) {
	t.Parallel()
	const tenant, sessionID = "t-200", "s-200"
	ctx := context.Background()
	log, kv := newStaleMetaLog(t, tenant, sessionID)

	appendPrompt(t, ctx, log, tenant, sessionID, "q1")
	appendPrompt(t, ctx, log, tenant, sessionID, "q2")

	kv.failMeta.Store(true)
	if err := AppendNextSessionEvent(ctx, log, &SessionEvent{
		SessionID: sessionID, TenantID: tenant, Kind: KindSessionClosed,
	}); err != nil {
		t.Fatalf("append ending: %v", err)
	}
	meta, err := log.GetMeta(ctx, tenant, sessionID)
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if meta.LatestSequence != 2 {
		t.Fatalf("precondition: meta.LatestSequence = %d, want the stale 2", meta.LatestSequence)
	}

	latest, err := log.LatestSequence(ctx, tenant, sessionID)
	if err != nil {
		t.Fatalf("LatestSequence: %v", err)
	}
	if latest != 3 {
		t.Fatalf("LatestSequence = %d with stale metadata, want the stored tail 3", latest)
	}
	// A writer appending at LatestSequence()+1 lands after the ending.
	late := newPromptEvent(tenant, sessionID, latest+1, "late")
	if err := log.Append(ctx, late); err != nil {
		t.Fatalf("append at the derived tail: %v", err)
	}
	// And one that still believes the stale value is refused, not admitted.
	if err := log.Append(ctx, newPromptEvent(tenant, sessionID, meta.LatestSequence+1, "stale")); !errors.Is(err, ErrSessionLogGap) {
		t.Fatalf("append at the stale sequence = %v, want ErrSessionLogGap", err)
	}

	events, _, err := log.Range(ctx, tenant, sessionID, 1, 0)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("got %d stored events, want 4", len(events))
	}
	if events[2].Sequence != 3 || events[2].Kind != KindSessionClosed {
		t.Fatalf("event 3 = seq %d kind %q, want the terminal %q — it was overwritten",
			events[2].Sequence, events[2].Kind, KindSessionClosed)
	}
}

// TestKVSessionEventLog_FailedMetaRollForwardLosesNoEvent drives the shared
// write path while every roll-forward fails: each append must take a fresh
// sequence, so every event survives.
func TestKVSessionEventLog_FailedMetaRollForwardLosesNoEvent(t *testing.T) {
	t.Parallel()
	const tenant, sessionID = "t-200b", "s-200b"
	ctx := context.Background()
	log, kv := newStaleMetaLog(t, tenant, sessionID)

	appendPrompt(t, ctx, log, tenant, sessionID, "p1")
	kv.failMeta.Store(true)
	for i := 2; i <= 5; i++ {
		appendPrompt(t, ctx, log, tenant, sessionID, fmt.Sprintf("p%d", i))
	}

	events, _, err := log.Range(ctx, tenant, sessionID, 1, 0)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("got %d stored events, want 5 — appends overwrote each other", len(events))
	}
	for i, ev := range events {
		want := fmt.Sprintf("p%d", i+1)
		if ev.Sequence != uint64(i+1) || ev.Prompt == nil || ev.Prompt.Prompt != want {
			t.Fatalf("event %d = seq %d %+v, want seq %d %q", i, ev.Sequence, ev.Prompt, i+1, want)
		}
	}

	// The next successful roll-forward repairs the metadata.
	kv.failMeta.Store(false)
	appendPrompt(t, ctx, log, tenant, sessionID, "p6")
	meta, err := log.GetMeta(ctx, tenant, sessionID)
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if meta.LatestSequence != 6 {
		t.Fatalf("meta.LatestSequence = %d after a successful append, want 6", meta.LatestSequence)
	}
}

// TestKVSessionEventLog_GuardedWriteRefusesOccupiedSequence proves the second
// guard on its own: with a create-if-absent store, an event write onto an
// occupied key is refused with ErrSessionLogGap and the stored event is kept.
func TestKVSessionEventLog_GuardedWriteRefusesOccupiedSequence(t *testing.T) {
	t.Parallel()
	const tenant, sessionID = "t-200c", "s-200c"
	ctx := context.Background()
	kv := &guardedMemKVStore{memKVStore: newMemKVStore()}
	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: kv})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}
	if log.creator == nil {
		t.Fatal("the log did not take the store's create-if-absent capability")
	}
	if err := log.Append(ctx, newPromptEvent(tenant, sessionID, 1, "kept")); err != nil {
		t.Fatalf("append: %v", err)
	}

	key := log.eventKey(tenant, sessionID, 1)
	if err := log.writeEvent(ctx, key, []byte(`{"overwrite":true}`)); !errors.Is(err, ErrSessionLogGap) {
		t.Fatalf("guarded write onto an occupied sequence = %v, want ErrSessionLogGap", err)
	}
	events, _, err := log.Range(ctx, tenant, sessionID, 1, 0)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if len(events) != 1 || events[0].Prompt == nil || events[0].Prompt.Prompt != "kept" {
		t.Fatalf("stored event changed: %+v", events)
	}
}

// TestKVSessionEventLog_RetentionUnderStaleMetaKeepsTail pins that retention
// never prunes an event above the tail the stored metadata record names. With
// MaxTurns=1 and metadata writes failing, pruning past the stale tail would
// delete event 2 while event 3 stands, and the tail scan would read the gap as
// the end of the log, handing the next append sequence 2 again.
func TestKVSessionEventLog_RetentionUnderStaleMetaKeepsTail(t *testing.T) {
	ctx := context.Background()
	const tenant, sessionID = "tenant-a", "sess-retention-stale"

	kv := &metaWriteFailingKV{memKVStore: newMemKVStore()}
	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{
		KVStore:   kv,
		Retention: SessionRetention{MaxTurns: 1},
	})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}
	kv.metaKey = log.metaKey(tenant, sessionID)
	if err := log.PutMeta(ctx, &SessionMeta{SessionID: sessionID, TenantID: tenant, Status: SessionStateActive}); err != nil {
		t.Fatalf("PutMeta: %v", err)
	}

	appendPrompt(t, ctx, log, tenant, sessionID, "p1")
	kv.failMeta.Store(true)
	appendPrompt(t, ctx, log, tenant, sessionID, "p2")
	appendPrompt(t, ctx, log, tenant, sessionID, "p3")

	latest, err := log.LatestSequence(ctx, tenant, sessionID)
	if err != nil {
		t.Fatalf("LatestSequence: %v", err)
	}
	if latest != 3 {
		t.Fatalf("LatestSequence = %d after p3 under failing metadata writes, want 3", latest)
	}

	appendPrompt(t, ctx, log, tenant, sessionID, "p4")
	events, _, err := log.Range(ctx, tenant, sessionID, 0, 0)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("Range returned no events")
	}
	prompts := map[uint64]string{}
	for i, ev := range events {
		if i > 0 && ev.Sequence <= events[i-1].Sequence {
			t.Fatalf("Range sequences not strictly increasing: %d after %d", ev.Sequence, events[i-1].Sequence)
		}
		if ev.Prompt != nil {
			prompts[ev.Sequence] = ev.Prompt.Prompt
		}
	}
	if last := events[len(events)-1].Sequence; last != 4 {
		t.Fatalf("last stored sequence = %d, want 4", last)
	}
	if prompts[3] != "p3" || prompts[4] != "p4" {
		t.Fatalf("prompts at 3,4 = %q,%q, want p3,p4", prompts[3], prompts[4])
	}

	kv.failMeta.Store(false)
	appendPrompt(t, ctx, log, tenant, sessionID, "p5")
	appendPrompt(t, ctx, log, tenant, sessionID, "p6")
	events, _, err = log.Range(ctx, tenant, sessionID, 0, 0)
	if err != nil {
		t.Fatalf("Range after recovery: %v", err)
	}
	if len(events) != 1 || events[0].Sequence != 6 || events[0].Prompt == nil || events[0].Prompt.Prompt != "p6" {
		got := make([]uint64, 0, len(events))
		for _, ev := range events {
			got = append(got, ev.Sequence)
		}
		t.Fatalf("after metadata writes recover, stored sequences = %v, want exactly [6] holding p6", got)
	}
}
