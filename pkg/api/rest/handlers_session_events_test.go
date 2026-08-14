//go:build rest

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

package rest

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// Integration-style tests for the REST session-events surface. They exercise
// the real SSE streaming loop, content negotiation, Last-Event-ID resume,
// multi-subscriber fan-out, and tenant isolation — end-to-end over HTTP.
//
// A minimal httptest.Server wraps the production handler helpers
// (respondSessionHistoryJSON, serveSessionEventsSSE) with a chi-free router,
// backed by a real MemorySessionEventLog. The SessionEventLog implementation
// is shared with production — what exercises here is the same publish / fanout
// / range path a running quicbaas server would.

// sseTestServer wires the real handler helpers against a MemorySessionEventLog
// so we can exercise the full SSE path over a real HTTP connection.
type sseTestServer struct {
	log      orchestration.SessionEventLog
	presence *orchestration.SessionPresence
	srv      *httptest.Server
}

// newSSETestServer constructs a test HTTP server that routes
// GET /sessions/{sessionID}/events to the production helpers.
func newSSETestServer(t *testing.T) *sseTestServer {
	t.Helper()
	log := orchestration.NewMemorySessionEventLog()
	// A short linger keeps the reconnect-suppression window observable in a
	// test without ever waiting on wall-clock time in the common path.
	presence := orchestration.NewSessionPresence(50*time.Millisecond, nil)
	t.Cleanup(presence.Close)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract sessionID from path: /sessions/{id}/events.
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 3 || parts[0] != "sessions" || parts[2] != "events" {
			http.NotFound(w, r)
			return
		}
		sessionID := parts[1]
		if sessionID == "" {
			http.Error(w, "session ID required", http.StatusBadRequest)
			return
		}
		tenantID := r.URL.Query().Get("tenant_id")
		if tenantID == "" {
			tenantID = qrdbsdk.SystemTenantID
		}
		fromSeq := uint64(0)
		if raw := r.URL.Query().Get("from"); raw != "" {
			n, err := strconv.ParseUint(raw, 10, 64)
			if err != nil {
				http.Error(w, "invalid from", http.StatusBadRequest)
				return
			}
			fromSeq = n
		}
		if lastID := r.Header.Get("Last-Event-ID"); lastID != "" {
			if n, err := strconv.ParseUint(lastID, 10, 64); err == nil && n > 0 {
				fromSeq = n + 1
			}
		}
		follow := r.URL.Query().Get("follow") == "1" || r.URL.Query().Get("follow") == "true"

		ctx := r.Context()
		if r.Header.Get("Accept") == "application/json" && !follow {
			respondSessionHistoryJSON(ctx, w, r, log, tenantID, sessionID, fromSeq)
			return
		}
		serveSessionEventsSSE(ctx, w, r, sessionEventStreamOptions{
			Log:        log,
			Presence:   presence,
			TenantID:   tenantID,
			SessionID:  sessionID,
			FromSeq:    fromSeq,
			Follow:     follow,
			OperatorID: sessionOperatorID(ctx, tenantID),
			Interface:  "web",
		})
	})

	return &sseTestServer{
		log:      log,
		presence: presence,
		srv:      httptest.NewServer(handler),
	}
}

func (s *sseTestServer) URL() string { return s.srv.URL }

func (s *sseTestServer) Close() { s.srv.Close() }

// appendPrompt is a convenience that records a prompt-kind event with the
// next sequence number for the given session.
func (s *sseTestServer) appendPrompt(t *testing.T, tenantID, sessionID, text string) *orchestration.SessionEvent {
	t.Helper()
	latest, err := s.log.LatestSequence(context.Background(), tenantID, sessionID)
	require.NoError(t, err)
	ev := &orchestration.SessionEvent{
		Sequence:  latest + 1,
		TenantID:  tenantID,
		SessionID: sessionID,
		Timestamp: time.Now().UTC(),
		Kind:      orchestration.KindPromptSubmitted,
		Source:    "operator:test",
		Prompt:    &orchestration.PromptSubmittedPayload{Prompt: text},
	}
	require.NoError(t, s.log.Append(context.Background(), ev))
	return ev
}

// sseFrame represents one parsed Server-Sent Events frame.
type sseFrame struct {
	ID    string
	Event string
	Data  string
}

// readSSE parses SSE frames from the reader and pushes them onto out until
// the reader errors or ctx is cancelled. Terminates when the connection
// closes or the caller cancels.
func readSSE(ctx context.Context, r io.Reader) <-chan sseFrame {
	out := make(chan sseFrame, 64)
	go func() {
		defer close(out)
		reader := bufio.NewReader(r)
		var frame sseFrame
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				// Blank line terminates the frame. Flush if non-empty.
				if frame.ID != "" || frame.Data != "" {
					select {
					case out <- frame:
					case <-ctx.Done():
						return
					}
					frame = sseFrame{}
				}
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue // comment/ping
			}
			switch {
			case strings.HasPrefix(line, "id: "):
				frame.ID = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				frame.Event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				frame.Data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	return out
}

// collectFrames reads up to n frames from ch or returns early on ctx timeout.
func collectFrames(ctx context.Context, ch <-chan sseFrame, n int) []sseFrame {
	var out []sseFrame
	for len(out) < n {
		select {
		case f, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, f)
		case <-ctx.Done():
			return out
		}
	}
	return out
}

// collectFramesFiltered reads frames from ch until n frames satisfying keep
// have been collected (or ctx is done), returning both the matched frames and
// every other frame seen along the way. Presence (operator_joined /
// operator_left) now shares the same per-session sequence space as
// transcript content, so a live (follow=1) attach interleaves exactly one
// operator_joined frame with the content the attacher actually asked to
// observe. Tests that care about content ordering/count filter it out with
// this helper rather than asserting brittle absolute frame positions.
func collectFramesFiltered(ctx context.Context, ch <-chan sseFrame, keep func(sseFrame) bool, n int) (matched, other []sseFrame) {
	for len(matched) < n {
		select {
		case f, ok := <-ch:
			if !ok {
				return matched, other
			}
			if keep(f) {
				matched = append(matched, f)
			} else {
				other = append(other, f)
			}
		case <-ctx.Done():
			return matched, other
		}
	}
	return matched, other
}

// isPromptFrame reports whether f carries a prompt_submitted event — used to
// separate transcript content from interleaved presence frames in tests.
func isPromptFrame(f sseFrame) bool {
	return f.Event == string(orchestration.KindPromptSubmitted)
}

// TestSessionEvents_SSE_ReplayPlusTail appends history, subscribes with
// follow=1, drains the replay, then appends live events and verifies they
// arrive in sequence order.
func TestSessionEvents_SSE_ReplayPlusTail(t *testing.T) {
	s := newSSETestServer(t)
	defer s.Close()

	const tenant = "tenant-a"
	const session = "session-replay-tail"

	// Replay history: 5 events.
	for i := 0; i < 5; i++ {
		s.appendPrompt(t, tenant, session, fmt.Sprintf("hist %d", i))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/sessions/%s/events?tenant_id=%s&from=0&follow=1", s.URL(), session, tenant)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	frames := readSSE(ctx, resp.Body)

	// Drain the 5 replayed prompt frames PLUS the one operator_joined
	// presence frame a follow=1 attach records right after Subscribe (before
	// the replay dump runs); both land in the SAME replay batch since the
	// join is durably appended before the history Range scan executes. Read
	// the deterministic total (6) and partition by kind rather than
	// asserting a stale frame count that presence would otherwise break.
	replayBatch := collectFrames(ctx, frames, 6)
	require.Len(t, replayBatch, 6, "expected 5 replayed prompt frames + 1 presence frame")

	var replayed, presence []sseFrame
	for _, f := range replayBatch {
		if isPromptFrame(f) {
			replayed = append(replayed, f)
		} else {
			presence = append(presence, f)
		}
	}
	require.Len(t, replayed, 5, "expected 5 replayed prompt frames")
	for i, f := range replayed {
		require.Equal(t, strconv.Itoa(i+1), f.ID, "frame %d id", i)
	}
	require.Len(t, presence, 1, "expected exactly one interleaved presence frame")
	require.Equal(t, string(orchestration.KindOperatorJoined), presence[0].Event)
	require.Equal(t, "6", presence[0].ID, "presence consumes the next sequence after history")

	// Append 3 more events after subscription is live. Sequences continue
	// from 7 since the join event above claimed sequence 6.
	for i := 0; i < 3; i++ {
		s.appendPrompt(t, tenant, session, fmt.Sprintf("live %d", i))
	}

	live, _ := collectFramesFiltered(ctx, frames, isPromptFrame, 3)
	require.Len(t, live, 3, "expected 3 live frames")
	for i, f := range live {
		require.Equal(t, strconv.Itoa(i+7), f.ID, "live frame %d id", i)
	}
}

// TestSessionEvents_SSE_LastEventIDResume verifies that a client sending
// Last-Event-ID: N receives only events with sequence > N, correctly
// ignoring events it already received before its disconnect.
func TestSessionEvents_SSE_LastEventIDResume(t *testing.T) {
	s := newSSETestServer(t)
	defer s.Close()

	const tenant = "tenant-resume"
	const session = "session-resume"

	for i := 0; i < 10; i++ {
		s.appendPrompt(t, tenant, session, fmt.Sprintf("msg %d", i))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/sessions/%s/events?tenant_id=%s&follow=0", s.URL(), session, tenant)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)
	req.Header.Set("Last-Event-ID", "7")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	frames := readSSE(ctx, resp.Body)
	got := collectFrames(ctx, frames, 3)
	require.Len(t, got, 3, "expected events 8..10")
	assert.Equal(t, "8", got[0].ID)
	assert.Equal(t, "9", got[1].ID)
	assert.Equal(t, "10", got[2].ID)
}

// TestSessionEvents_JSON_History verifies Accept: application/json returns
// a paginated JSON array instead of SSE when follow is not set.
func TestSessionEvents_JSON_History(t *testing.T) {
	s := newSSETestServer(t)
	defer s.Close()

	const tenant = "tenant-json"
	const session = "session-json"

	for i := 0; i < 4; i++ {
		s.appendPrompt(t, tenant, session, fmt.Sprintf("p %d", i))
	}

	url := fmt.Sprintf("%s/sessions/%s/events?tenant_id=%s", s.URL(), session, tenant)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var body struct {
		Events    []json.RawMessage `json:"events"`
		NextSeq   uint64            `json:"next_seq"`
		SessionID string            `json:"session_id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Len(t, body.Events, 4)
	assert.Equal(t, session, body.SessionID)
}

// TestSessionEvents_SSE_MultiSubscriber verifies fan-out: several concurrent
// subscribers each receive the full stream of history + live events in
// correct order, independently of the others.
func TestSessionEvents_SSE_MultiSubscriber(t *testing.T) {
	s := newSSETestServer(t)
	defer s.Close()

	const tenant = "tenant-multi"
	const session = "session-multi"
	const subscribers = 3
	const liveEvents = 5

	// Two events of history before subscription.
	s.appendPrompt(t, tenant, session, "hist 0")
	s.appendPrompt(t, tenant, session, "hist 1")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	type clientResult struct {
		frames []sseFrame
	}
	results := make([]clientResult, subscribers)
	var wg sync.WaitGroup
	ready := make(chan struct{}, subscribers)

	for i := 0; i < subscribers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			url := fmt.Sprintf("%s/sessions/%s/events?tenant_id=%s&from=0&follow=1", s.URL(), session, tenant)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			frames := readSSE(ctx, resp.Body)
			// Filter to prompt content: each concurrent subscriber's own
			// follow=1 attach also appends an operator_joined presence event
			// into the SAME per-session sequence space, and the three
			// subscribers race each other for those sequence numbers, so the
			// exact sequence a given subscriber's join lands on (and thus
			// where it interleaves with content) is not deterministic across
			// subscribers. Content ordering and count are still guaranteed.
			collected, _ := collectFramesFiltered(ctx, frames, isPromptFrame, 2)
			ready <- struct{}{}
			more, _ := collectFramesFiltered(ctx, frames, isPromptFrame, liveEvents)
			collected = append(collected, more...)
			results[idx] = clientResult{frames: collected}
		}(i)
	}

	// Wait for all subscribers to drain history before publishing live events.
	for i := 0; i < subscribers; i++ {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatalf("subscriber %d never drained history", i)
		}
	}

	for i := 0; i < liveEvents; i++ {
		s.appendPrompt(t, tenant, session, fmt.Sprintf("live %d", i))
	}

	wg.Wait()

	for i, r := range results {
		require.Lenf(t, r.frames, 2+liveEvents, "subscriber %d frame count", i)
		// Sequence numbers are strictly increasing per subscriber (fan-out
		// preserves order) even though the absolute values now vary between
		// subscribers because of the interleaved, per-subscriber presence
		// append race.
		prevSeq := 0
		for j, f := range r.frames {
			seq, err := strconv.Atoi(f.ID)
			require.NoErrorf(t, err, "subscriber %d frame %d id %q", i, j, f.ID)
			require.Greaterf(t, seq, prevSeq, "subscriber %d frame %d out of order", i, j)
			prevSeq = seq
		}
	}
}

// TestSessionEvents_SSE_CrossTenantIsolation verifies that a subscribe
// request carrying a different tenant_id never sees another tenant's
// events. The fanout key includes the tenant, so a mismatched tenant_id
// gets an empty stream even when the session_id collides.
func TestSessionEvents_SSE_CrossTenantIsolation(t *testing.T) {
	s := newSSETestServer(t)
	defer s.Close()

	const sharedSession = "shared-session-id"

	// Tenant A writes 3 events.
	for i := 0; i < 3; i++ {
		s.appendPrompt(t, "tenant-a", sharedSession, fmt.Sprintf("a%d", i))
	}

	// Tenant B subscribes to the *same* session ID — should see nothing.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/sessions/%s/events?tenant_id=%s&from=0", s.URL(), sharedSession, "tenant-b")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	frames := readSSE(ctx, resp.Body)
	got := collectFrames(ctx, frames, 3)
	assert.Empty(t, got, "tenant-b must not see tenant-a events")

	// And tenant A should still see its events — prove the log isn't empty.
	latest, err := s.log.LatestSequence(context.Background(), "tenant-a", sharedSession)
	require.NoError(t, err)
	assert.Equal(t, uint64(3), latest)
}

// TestSessionEvents_SSE_ReplayFromMidpoint verifies replay honors the `from`
// query parameter, skipping events below that sequence.
func TestSessionEvents_SSE_ReplayFromMidpoint(t *testing.T) {
	s := newSSETestServer(t)
	defer s.Close()

	const tenant = "tenant-mid"
	const session = "session-mid"

	for i := 0; i < 10; i++ {
		s.appendPrompt(t, tenant, session, fmt.Sprintf("msg %d", i))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/sessions/%s/events?tenant_id=%s&from=6&follow=0", s.URL(), session, tenant)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	frames := readSSE(ctx, resp.Body)
	got := collectFrames(ctx, frames, 5)
	require.Len(t, got, 5)
	for i, f := range got {
		require.Equal(t, strconv.Itoa(i+6), f.ID, "expected sequence %d, got %s", i+6, f.ID)
	}
}
