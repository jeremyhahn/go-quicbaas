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
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// Regression tests for the session-events SSE stream dropping mid-turn.
//
// Root cause: http.Server.WriteTimeout arms an absolute write deadline on the
// connection when the request is read and never extends it, so a streaming
// response was severed exactly WriteTimeout after it started (30s with the
// shipped defaults) no matter how much data was still flowing. These tests run
// the production handler behind a real http.Server with a deliberately tiny
// WriteTimeout, which reproduces the field failure in milliseconds instead of
// half a minute.

// streamTestServer runs serveSessionEventsSSE behind a real http.Server whose
// WriteTimeout and heartbeat interval the test controls.
type streamTestServer struct {
	log      orchestration.SessionEventLog
	presence *orchestration.SessionPresence
	srv      *httptest.Server
}

// newStreamTestServer starts a server that serves GET /events for one session.
// writeTimeout is applied to the underlying http.Server exactly as
// pkg/server/lifecycle_rest.go applies the configured Server.WriteTimeout.
func newStreamTestServer(t *testing.T, writeTimeout, heartbeat, linger time.Duration) *streamTestServer {
	t.Helper()

	log := orchestration.NewMemorySessionEventLog()
	presence := orchestration.NewSessionPresence(linger, nil)
	t.Cleanup(presence.Close)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fromSeq := uint64(0)
		if raw := r.URL.Query().Get("from"); raw != "" {
			n, err := strconv.ParseUint(raw, 10, 64)
			require.NoError(t, err)
			fromSeq = n
		}
		if lastID := r.Header.Get("Last-Event-ID"); lastID != "" {
			if n, err := strconv.ParseUint(lastID, 10, 64); err == nil && n > 0 {
				fromSeq = n + 1
			}
		}
		serveSessionEventsSSE(r.Context(), w, r, sessionEventStreamOptions{
			Log:               log,
			Presence:          presence,
			TenantID:          "system",
			SessionID:         "s1",
			FromSeq:           fromSeq,
			Follow:            r.URL.Query().Get("follow") == "1",
			OperatorID:        r.URL.Query().Get("operator"),
			Interface:         "cli",
			HeartbeatInterval: heartbeat,
		})
	})

	srv := httptest.NewUnstartedServer(handler)
	srv.Config.WriteTimeout = writeTimeout
	srv.Config.ReadTimeout = 30 * time.Second
	srv.Start()
	t.Cleanup(srv.Close)

	return &streamTestServer{log: log, presence: presence, srv: srv}
}

// append records one prompt event at the next sequence.
func (s *streamTestServer) append(t *testing.T, text string) {
	t.Helper()
	require.NoError(t, orchestration.AppendNextSessionEvent(context.Background(), s.log, &orchestration.SessionEvent{
		TenantID:  "system",
		SessionID: "s1",
		Timestamp: time.Now().UTC(),
		Kind:      orchestration.KindPromptSubmitted,
		Source:    "operator:test",
		Prompt:    &orchestration.PromptSubmittedPayload{Prompt: text},
	}))
}

// presenceKindCount counts presence events of one kind in the transcript.
func (s *streamTestServer) presenceKindCount(t *testing.T, kind orchestration.SessionEventKind) int {
	t.Helper()
	events, _, err := s.log.Range(context.Background(), "system", "s1", 1, 0)
	require.NoError(t, err)
	n := 0
	for _, ev := range events {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}

// sseClient reads raw SSE lines so tests can observe comment/keepalive frames
// as well as events — an SSE decoder would hide exactly the frames under test.
type sseClient struct {
	reader *bufio.Reader
	cancel context.CancelFunc
	lines  chan string
	errs   chan error
}

// openSSE connects to the stream and starts pumping lines into a channel so
// tests select on arrivals instead of blocking reads.
func openSSE(t *testing.T, srv *streamTestServer, query string) *sseClient {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.srv.URL+"/events?"+query, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := srv.srv.Client().Do(req)
	if err != nil {
		cancel()
		t.Fatalf("open SSE stream: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("close SSE stream: %v", closeErr)
		}
	})
	require.Equal(t, http.StatusOK, resp.StatusCode)

	client := &sseClient{
		reader: bufio.NewReader(resp.Body),
		cancel: cancel,
		lines:  make(chan string, 256),
		errs:   make(chan error, 1),
	}
	go func() {
		defer close(client.lines)
		for {
			line, readErr := client.reader.ReadString('\n')
			if line != "" {
				client.lines <- strings.TrimRight(line, "\r\n")
			}
			if readErr != nil {
				client.errs <- readErr
				return
			}
		}
	}()
	return client
}

// close tears the connection down by cancelling the request; the cleanup
// registered by openSSE closes the body.
func (c *sseClient) close() {
	c.cancel()
}

// nextMatching returns the first line satisfying match, or fails after the
// deadline. Non-matching lines are consumed.
func (c *sseClient) nextMatching(t *testing.T, timeout time.Duration, match func(string) bool) string {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case line, ok := <-c.lines:
			if !ok {
				select {
				case err := <-c.errs:
					t.Fatalf("stream ended before the expected line arrived: %v", err)
				default:
					t.Fatal("stream ended before the expected line arrived")
				}
			}
			if match(line) {
				return line
			}
		case <-deadline.C:
			t.Fatalf("timed out waiting for a matching line after %s", timeout)
		}
	}
}

// hasPrefix builds a matcher for a line prefix.
func hasPrefix(prefix string) func(string) bool {
	return func(line string) bool { return strings.HasPrefix(line, prefix) }
}

// TestSessionEventsSSE_OutlivesServerWriteTimeout is the direct regression
// test: a follow stream must keep delivering long after http.Server's
// WriteTimeout would have expired. With the write deadline left armed the
// connection is torn down at writeTimeout and this test fails with a read
// error on the client side.
func TestSessionEventsSSE_OutlivesServerWriteTimeout(t *testing.T) {
	const writeTimeout = 150 * time.Millisecond
	srv := newStreamTestServer(t, writeTimeout, 20*time.Millisecond, time.Minute)

	client := openSSE(t, srv, "follow=1&operator=alice")

	// Drain heartbeats well past the write deadline. Ten 20ms heartbeats span
	// ~200ms, comfortably beyond the 150ms WriteTimeout that used to sever
	// the connection.
	for i := 0; i < 10; i++ {
		client.nextMatching(t, 2*time.Second, hasPrefix(": ping"))
	}

	// A real event appended after the old deadline must still be delivered.
	srv.append(t, "still here")
	line := client.nextMatching(t, 2*time.Second, hasPrefix("event: "))
	assert.Equal(t, "event: prompt_submitted", line,
		"the stream must survive past the server WriteTimeout")

	select {
	case err := <-client.errs:
		t.Fatalf("stream ended unexpectedly: %v", err)
	default:
	}
}

// TestSessionEventsSSE_HeartbeatEmittedWhileIdle asserts keepalive comment
// frames flow on a session with no activity at all, which is what keeps an
// intermediary from reaping a connection while an agent thinks.
func TestSessionEventsSSE_HeartbeatEmittedWhileIdle(t *testing.T) {
	srv := newStreamTestServer(t, 5*time.Second, 15*time.Millisecond, time.Minute)
	client := openSSE(t, srv, "follow=1&operator=alice")

	first := client.nextMatching(t, 2*time.Second, hasPrefix(": ping"))
	second := client.nextMatching(t, 2*time.Second, hasPrefix(": ping"))
	assert.True(t, strings.HasPrefix(first, ": ping"))
	assert.True(t, strings.HasPrefix(second, ": ping"))

	events, _, err := srv.log.Range(context.Background(), "system", "s1", 1, 0)
	require.NoError(t, err)
	for _, ev := range events {
		assert.NotEqual(t, orchestration.KindPromptSubmitted, ev.Kind,
			"heartbeats must not be persisted as transcript events")
	}
}

// TestSessionEventsSSE_AdvertisesRetryInterval asserts the stream tells
// EventSource clients how soon to come back, so a browser reconnect lands
// inside the presence linger window.
func TestSessionEventsSSE_AdvertisesRetryInterval(t *testing.T) {
	srv := newStreamTestServer(t, 5*time.Second, time.Second, time.Minute)
	client := openSSE(t, srv, "follow=1&operator=alice")

	line := client.nextMatching(t, 2*time.Second, hasPrefix("retry:"))
	assert.Equal(t, "retry: "+strconv.FormatInt(sessionSSERetryAdvice.Milliseconds(), 10), line)
}

// TestSessionEventsSSE_ReconnectDoesNotChurnPresence covers the operator_left
// / operator_joined churn from the bug report: a client that drops and comes
// straight back must leave no trace in the durable transcript.
func TestSessionEventsSSE_ReconnectDoesNotChurnPresence(t *testing.T) {
	srv := newStreamTestServer(t, 5*time.Second, 50*time.Millisecond, 3*time.Second)

	first := openSSE(t, srv, "follow=1&operator=alice")
	first.nextMatching(t, 2*time.Second, func(line string) bool {
		return line == "event: operator_joined"
	})
	require.Equal(t, 1, srv.presenceKindCount(t, orchestration.KindOperatorJoined))

	// Drop the connection and resume immediately, the way the SDK reader does.
	first.close()
	second := openSSE(t, srv, "follow=1&operator=alice&from=1")
	second.nextMatching(t, 2*time.Second, hasPrefix(": ping"))

	assert.Never(t, func() bool {
		return srv.presenceKindCount(t, orchestration.KindOperatorLeft) > 0 ||
			srv.presenceKindCount(t, orchestration.KindOperatorJoined) > 1
	}, time.Second, 25*time.Millisecond,
		"a resumed stream must not write operator_left or a second operator_joined")
}

// TestSessionEventsSSE_GenuineDepartureIsRecorded is the counterpart: when the
// operator really goes away, the transcript still says so once the linger
// window closes. Suppressing churn must not suppress real departures.
func TestSessionEventsSSE_GenuineDepartureIsRecorded(t *testing.T) {
	srv := newStreamTestServer(t, 5*time.Second, 50*time.Millisecond, 50*time.Millisecond)

	client := openSSE(t, srv, "follow=1&operator=alice")
	client.nextMatching(t, 2*time.Second, func(line string) bool {
		return line == "event: operator_joined"
	})
	client.close()

	require.Eventually(t, func() bool {
		return srv.presenceKindCount(t, orchestration.KindOperatorLeft) == 1
	}, 3*time.Second, 25*time.Millisecond, "a real departure must reach the transcript")
	assert.Equal(t, 1, srv.presenceKindCount(t, orchestration.KindOperatorJoined))
}

// TestSessionEventsSSE_NoFollowStillEndsAfterReplay asserts the non-follow
// path is untouched by the streaming changes: it drains history and closes,
// writing no presence events at all.
func TestSessionEventsSSE_NoFollowStillEndsAfterReplay(t *testing.T) {
	srv := newStreamTestServer(t, 5*time.Second, 50*time.Millisecond, time.Minute)
	srv.append(t, "one")
	srv.append(t, "two")

	client := openSSE(t, srv, "follow=0&operator=alice")
	client.nextMatching(t, 2*time.Second, func(line string) bool { return line == "id: 2" })

	select {
	case err := <-client.errs:
		require.Error(t, err, "a non-follow stream ends after the replay drains")
	case <-time.After(2 * time.Second):
		t.Fatal("non-follow stream did not end after replay")
	}
	assert.Equal(t, 0, srv.presenceKindCount(t, orchestration.KindOperatorJoined),
		"a one-shot replay is not an attach and records no presence")
}

// TestServeSessionEventsSSE_RangeFailureEmitsErrorFrame asserts a store
// failure is reported to the client as a terminal SSE error frame rather than
// a silently truncated stream, so the reader does not retry forever.
func TestServeSessionEventsSSE_RangeFailureEmitsErrorFrame(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events?follow=0", nil)

	serveSessionEventsSSE(context.Background(), rec, req, sessionEventStreamOptions{
		Log:       &rangeFailureLog{SessionEventLog: orchestration.NewMemorySessionEventLog()},
		TenantID:  "system",
		SessionID: "s1",
	})

	assert.Contains(t, rec.Body.String(), "event: error")
	assert.Contains(t, rec.Body.String(), "range failed")
}

// TestServeSessionEventsSSE_SubscribeFailureEmitsErrorFrame asserts a fan-out
// registration failure ends the stream with an error frame before any presence
// event is written.
func TestServeSessionEventsSSE_SubscribeFailureEmitsErrorFrame(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events?follow=1", nil)
	log := &subscribeFailureLog{SessionEventLog: orchestration.NewMemorySessionEventLog()}
	presence := orchestration.NewSessionPresence(time.Minute, nil)
	t.Cleanup(presence.Close)

	serveSessionEventsSSE(context.Background(), rec, req, sessionEventStreamOptions{
		Log:        log,
		Presence:   presence,
		TenantID:   "system",
		SessionID:  "s1",
		Follow:     true,
		OperatorID: "alice",
	})

	assert.Contains(t, rec.Body.String(), "event: error")
	assert.Contains(t, rec.Body.String(), "subscribe failed")
	events, _, err := log.Range(context.Background(), "system", "s1", 1, 0)
	require.NoError(t, err)
	assert.Empty(t, events, "a failed subscribe must not announce an operator")
}

// rangeFailureLog fails every Range call.
type rangeFailureLog struct {
	orchestration.SessionEventLog
}

func (l *rangeFailureLog) Range(context.Context, string, string, uint64, int) ([]*orchestration.SessionEvent, uint64, error) {
	return nil, 0, assert.AnError
}

// subscribeFailureLog fails every Subscribe call.
type subscribeFailureLog struct {
	orchestration.SessionEventLog
}

func (l *subscribeFailureLog) Subscribe(context.Context, string, string) (<-chan *orchestration.SessionEvent, func(), error) {
	return nil, nil, assert.AnError
}
