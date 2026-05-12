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
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newPromptEvent is a focused helper for building prompt_submitted events in
// tests without repeating the same 7-line struct literal.
func newPromptEvent(tenantID, sessionID string, seq uint64, text string) *SessionEvent {
	return &SessionEvent{
		Sequence:  seq,
		TenantID:  tenantID,
		SessionID: sessionID,
		Timestamp: time.Now().UTC(),
		Source:    "operator:test",
		Kind:      KindPromptSubmitted,
		Prompt:    &PromptSubmittedPayload{Prompt: text},
	}
}

func TestMemorySessionEventLog_AppendMonotonic(t *testing.T) {
	t.Parallel()
	log := NewMemorySessionEventLog()
	ctx := context.Background()

	// Append in order: seq 1, 2, 3 all succeed.
	for i := 1; i <= 3; i++ {
		if err := log.Append(ctx, newPromptEvent("t", "s", uint64(i), fmt.Sprintf("p%d", i))); err != nil {
			t.Fatalf("append seq %d: %v", i, err)
		}
	}

	// Gap attempt (seq 5 instead of 4) rejected.
	if err := log.Append(ctx, newPromptEvent("t", "s", 5, "gap")); !errors.Is(err, ErrSessionLogGap) {
		t.Fatalf("expected ErrSessionLogGap, got %v", err)
	}

	// Duplicate seq (re-attempt 3) rejected.
	if err := log.Append(ctx, newPromptEvent("t", "s", 3, "dup")); !errors.Is(err, ErrSessionLogGap) {
		t.Fatalf("expected gap on duplicate, got %v", err)
	}

	// Sequence 4 continues cleanly.
	if err := log.Append(ctx, newPromptEvent("t", "s", 4, "p4")); err != nil {
		t.Fatalf("append seq 4: %v", err)
	}

	// LatestSequence reflects the appends.
	latest, _ := log.LatestSequence(ctx, "t", "s")
	if latest != 4 {
		t.Errorf("latest: got %d, want 4", latest)
	}
}

func TestMemorySessionEventLog_AppendRequiresIDs(t *testing.T) {
	t.Parallel()
	log := NewMemorySessionEventLog()
	ctx := context.Background()
	if err := log.Append(ctx, &SessionEvent{Sequence: 1, TenantID: "t"}); err == nil {
		t.Error("missing session_id accepted")
	}
	if err := log.Append(ctx, &SessionEvent{Sequence: 1, SessionID: "s"}); err == nil {
		t.Error("missing tenant_id accepted")
	}
	if err := log.Append(ctx, nil); err == nil {
		t.Error("nil event accepted")
	}
}

func TestMemorySessionEventLog_RangePagination(t *testing.T) {
	t.Parallel()
	log := NewMemorySessionEventLog()
	ctx := context.Background()
	for i := 1; i <= 10; i++ {
		_ = log.Append(ctx, newPromptEvent("t", "s", uint64(i), "p"+strconv.Itoa(i)))
	}

	// Page 1: from seq 1, limit 3 → events 1..3, nextSeq=4
	page, next, err := log.Range(ctx, "t", "s", 1, 3)
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	if len(page) != 3 || page[0].Sequence != 1 || page[2].Sequence != 3 {
		t.Errorf("page 1 wrong: %d events seqs=%v", len(page), seqsOf(page))
	}
	if next != 4 {
		t.Errorf("nextSeq: got %d, want 4", next)
	}

	// Page 2 via the cursor.
	page, next, _ = log.Range(ctx, "t", "s", next, 3)
	if len(page) != 3 || page[0].Sequence != 4 || page[2].Sequence != 6 {
		t.Errorf("page 2 wrong: seqs=%v", seqsOf(page))
	}
	if next != 7 {
		t.Errorf("nextSeq: got %d, want 7", next)
	}

	// Beyond-end request returns empty.
	page, next, _ = log.Range(ctx, "t", "s", 100, 10)
	if len(page) != 0 || next != 0 {
		t.Errorf("beyond-end: got %d events, next=%d", len(page), next)
	}

	// Unlimited read returns everything.
	page, next, _ = log.Range(ctx, "t", "s", 1, 0)
	if len(page) != 10 || next != 0 {
		t.Errorf("unlimited: got %d events, next=%d", len(page), next)
	}
}

func TestMemorySessionEventLog_RangeEmptySession(t *testing.T) {
	t.Parallel()
	log := NewMemorySessionEventLog()
	events, next, err := log.Range(context.Background(), "t", "s", 0, 10)
	if err != nil {
		t.Fatalf("empty range: %v", err)
	}
	if len(events) != 0 || next != 0 {
		t.Errorf("empty: got %d events, next=%d", len(events), next)
	}
}

func TestMemorySessionEventLog_SubscribeDeliversLive(t *testing.T) {
	t.Parallel()
	log := NewMemorySessionEventLog()
	ctx := context.Background()

	ch, unsub, err := log.Subscribe(ctx, "t", "s")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer unsub()

	go func() {
		for i := 1; i <= 3; i++ {
			_ = log.Append(ctx, newPromptEvent("t", "s", uint64(i), fmt.Sprintf("p%d", i)))
		}
	}()

	var got []uint64
	for i := 0; i < 3; i++ {
		select {
		case ev := <-ch:
			got = append(got, ev.Sequence)
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for event %d", i+1)
		}
	}
	if len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Errorf("subscribe delivery: got %v", got)
	}
}

func TestMemorySessionEventLog_SubscribeUnsubscribeCloses(t *testing.T) {
	t.Parallel()
	log := NewMemorySessionEventLog()
	ch, unsub, _ := log.Subscribe(context.Background(), "t", "s")
	unsub()
	// Second unsubscribe must be no-op (idempotent).
	unsub()
	if _, open := <-ch; open {
		t.Error("channel should be closed after unsubscribe")
	}
}

func TestMemorySessionEventLog_MultiSubscriberIdenticalStream(t *testing.T) {
	t.Parallel()
	log := NewMemorySessionEventLog()
	ctx := context.Background()

	const subscribers = 3
	chs := make([]<-chan *SessionEvent, subscribers)
	unsubs := make([]func(), subscribers)
	for i := 0; i < subscribers; i++ {
		chs[i], unsubs[i], _ = log.Subscribe(ctx, "t", "s")
		defer unsubs[i]()
	}

	const events = 50
	go func() {
		for i := 1; i <= events; i++ {
			_ = log.Append(ctx, newPromptEvent("t", "s", uint64(i), fmt.Sprintf("p%d", i)))
		}
	}()

	var wg sync.WaitGroup
	results := make([][]uint64, subscribers)
	for i := 0; i < subscribers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < events; j++ {
				select {
				case ev := <-chs[idx]:
					results[idx] = append(results[idx], ev.Sequence)
				case <-time.After(2 * time.Second):
					return
				}
			}
		}(i)
	}
	wg.Wait()

	for i := 0; i < subscribers; i++ {
		if len(results[i]) != events {
			t.Errorf("subscriber %d: got %d events, want %d", i, len(results[i]), events)
			continue
		}
		for j := 0; j < events; j++ {
			if results[i][j] != uint64(j+1) {
				t.Errorf("subscriber %d: event %d got seq %d, want %d", i, j, results[i][j], j+1)
				break
			}
		}
	}
}

func TestMemorySessionEventLog_SlowSubscriberDoesNotBlock(t *testing.T) {
	t.Parallel()
	log := NewMemorySessionEventLog()
	ctx := context.Background()

	_, unsub, _ := log.Subscribe(ctx, "t", "s")
	defer unsub()

	// Flood the log with 2000 events; the unread channel (buffer=256) will
	// fill up after ~256 events but append must keep succeeding.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 1; i <= 2000; i++ {
			if err := log.Append(ctx, newPromptEvent("t", "s", uint64(i), "x")); err != nil {
				t.Errorf("append %d blocked on slow subscriber: %v", i, err)
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("slow subscriber blocked append path")
	}

	latest, _ := log.LatestSequence(ctx, "t", "s")
	if latest != 2000 {
		t.Errorf("latest: got %d, want 2000", latest)
	}
}

func TestMemorySessionEventLog_SubscribeMissesHistory(t *testing.T) {
	t.Parallel()
	// Subscribe delivers only live events — history must be fetched via
	// Range. This test documents that contract.
	log := NewMemorySessionEventLog()
	ctx := context.Background()
	_ = log.Append(ctx, newPromptEvent("t", "s", 1, "before"))

	ch, unsub, _ := log.Subscribe(ctx, "t", "s")
	defer unsub()

	_ = log.Append(ctx, newPromptEvent("t", "s", 2, "after"))

	select {
	case ev := <-ch:
		if ev.Sequence != 2 {
			t.Errorf("first live event: got seq %d, want 2", ev.Sequence)
		}
	case <-time.After(time.Second):
		t.Fatal("no live event delivered")
	}

	// No second event; subscriber didn't see seq 1.
	select {
	case ev := <-ch:
		t.Fatalf("unexpected additional event: seq=%d", ev.Sequence)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestMemorySessionEventLog_MetaLifecycle(t *testing.T) {
	t.Parallel()
	log := NewMemorySessionEventLog()
	ctx := context.Background()

	// Missing meta returns ErrSessionLogNotFound.
	if _, err := log.GetMeta(ctx, "t", "s"); !errors.Is(err, ErrSessionLogNotFound) {
		t.Fatalf("got %v, want ErrSessionLogNotFound", err)
	}

	created := time.Now().UTC()
	meta := &SessionMeta{
		SessionID: "s",
		TenantID:  "t",
		Backend:   "ollama",
		Model:     "qwen3:1.7b",
		Status:    SessionStateActive,
		CreatedAt: created,
		Labels:    map[string]string{"role": "worker"},
	}
	if err := log.PutMeta(ctx, meta); err != nil {
		t.Fatalf("put meta: %v", err)
	}
	got, err := log.GetMeta(ctx, "t", "s")
	if err != nil {
		t.Fatalf("get meta: %v", err)
	}
	if got.Backend != "ollama" || got.Labels["role"] != "worker" {
		t.Errorf("meta roundtrip wrong: %+v", got)
	}

	// Append advances LatestSequence + EventCount on the stored meta.
	_ = log.Append(ctx, newPromptEvent("t", "s", 1, "p"))
	got, _ = log.GetMeta(ctx, "t", "s")
	if got.LatestSequence != 1 || got.EventCount != 1 {
		t.Errorf("meta not updated on append: seq=%d count=%d", got.LatestSequence, got.EventCount)
	}
}

func TestMemorySessionEventLog_ListSessionsFilterAndSort(t *testing.T) {
	t.Parallel()
	log := NewMemorySessionEventLog()
	ctx := context.Background()
	t0 := time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)

	sessions := []SessionMeta{
		{SessionID: "s1", TenantID: "t", Backend: "ollama", Status: SessionStateActive, CreatedAt: t0},
		{SessionID: "s2", TenantID: "t", Backend: "claude", Status: SessionStateActive, CreatedAt: t0.Add(time.Hour)},
		{SessionID: "s3", TenantID: "t", Backend: "ollama", Status: SessionStateClosed, CreatedAt: t0.Add(2 * time.Hour)},
		{SessionID: "other", TenantID: "t2", Backend: "ollama", Status: SessionStateActive, CreatedAt: t0},
	}
	for i := range sessions {
		m := sessions[i]
		_ = log.PutMeta(ctx, &m)
	}

	// Default filter: all tenant t sessions, newest first.
	out, _ := log.ListSessions(ctx, "t", nil, 0)
	if len(out) != 3 || out[0].SessionID != "s3" || out[2].SessionID != "s1" {
		t.Errorf("default listing: %v", idsOf(out))
	}

	// Backend filter.
	out, _ = log.ListSessions(ctx, "t", &SessionFilter{Backend: "ollama"}, 0)
	if len(out) != 2 {
		t.Errorf("ollama-only: got %d", len(out))
	}

	// Status filter.
	out, _ = log.ListSessions(ctx, "t", &SessionFilter{Status: SessionStateClosed}, 0)
	if len(out) != 1 || out[0].SessionID != "s3" {
		t.Errorf("closed-only: got %v", idsOf(out))
	}

	// Limit.
	out, _ = log.ListSessions(ctx, "t", nil, 1)
	if len(out) != 1 || out[0].SessionID != "s3" {
		t.Errorf("limit 1: got %v", idsOf(out))
	}

	// Tenant isolation: t2 list doesn't include t's sessions.
	out, _ = log.ListSessions(ctx, "t2", nil, 0)
	if len(out) != 1 || out[0].SessionID != "other" {
		t.Errorf("tenant 2: got %v", idsOf(out))
	}
}

func TestSessionLogAppendError_Message(t *testing.T) {
	t.Parallel()
	inner := errors.New("disk full")
	e := &SessionLogAppendError{SessionID: "s", Sequence: 42, Cause: inner}
	msg := e.Error()
	if !contains(msg, "s") || !contains(msg, "42") || !contains(msg, "disk full") {
		t.Errorf("error message missing fields: %q", msg)
	}
	if !errors.Is(e, inner) {
		t.Error("errors.Is should unwrap to cause")
	}
}

func TestSessionLogFanout_DropsCountUnderLoad(t *testing.T) {
	t.Parallel()
	log := NewMemorySessionEventLog()
	ctx := context.Background()

	// Subscribe but never read from the channel; after flooding, verify
	// the subscriber's drop count is non-zero but channel stays open.
	ch, unsub, _ := log.Subscribe(ctx, "t", "s")
	defer unsub()

	for i := 1; i <= 1000; i++ {
		_ = log.Append(ctx, newPromptEvent("t", "s", uint64(i), "x"))
	}

	// Drain what's buffered so the channel drains; count deliveries.
	delivered := 0
	for {
		select {
		case <-ch:
			delivered++
		case <-time.After(50 * time.Millisecond):
			goto done
		}
	}
done:
	if delivered >= 1000 {
		t.Errorf("expected drops under 1000-event flood with buffer=256, got %d delivered", delivered)
	}
	if delivered == 0 {
		t.Errorf("expected at least some deliveries, got 0")
	}
}

// Concurrent-access smoke test. Verifies no data races under -race and that
// every append eventually makes it to LatestSequence.
func TestMemorySessionEventLog_ConcurrentAppend(t *testing.T) {
	t.Parallel()
	log := NewMemorySessionEventLog()
	ctx := context.Background()

	// Single writer goroutine (appends must be serialized by the caller —
	// that's the SessionEventLog contract) + concurrent readers.
	const total = 500
	var wrote atomic.Int64
	writer := make(chan struct{})
	go func() {
		defer close(writer)
		for i := 1; i <= total; i++ {
			if err := log.Append(ctx, newPromptEvent("t", "s", uint64(i), "x")); err != nil {
				t.Errorf("append %d: %v", i, err)
				return
			}
			wrote.Add(1)
		}
	}()

	// Concurrent readers pound Range / LatestSequence.
	var rdWg sync.WaitGroup
	for r := 0; r < 4; r++ {
		rdWg.Add(1)
		go func() {
			defer rdWg.Done()
			for wrote.Load() < total {
				_, _, _ = log.Range(ctx, "t", "s", 1, 50)
				_, _ = log.LatestSequence(ctx, "t", "s")
			}
		}()
	}
	<-writer
	rdWg.Wait()
	latest, _ := log.LatestSequence(ctx, "t", "s")
	if latest != total {
		t.Errorf("final latest: got %d, want %d", latest, total)
	}
}

// seqsOf is a trivial test helper that extracts the sequence numbers from a
// slice of events for tidier failure messages.
func seqsOf(events []*SessionEvent) []uint64 {
	out := make([]uint64, len(events))
	for i, e := range events {
		out[i] = e.Sequence
	}
	return out
}

// idsOf extracts SessionIDs from a slice of SessionMeta, for assertion messages.
func idsOf(metas []*SessionMeta) []string {
	out := make([]string, len(metas))
	for i, m := range metas {
		out[i] = m.SessionID
	}
	return out
}
