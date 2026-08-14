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
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// A follow stream that drops mid-turn used to end the operator's `prompt
// --follow` with "event stream ended while turn was still running" even though
// the turn kept going server-side. These tests pin the resume contract: the
// reader re-opens from the last delivered sequence, delivering no duplicate and
// leaving no gap, and only reports an error when resumption genuinely fails.

// resumeTestServer serves the SSE endpoint and severs the connection after a
// configurable number of frames, so a drop mid-stream is reproducible.
type resumeTestServer struct {
	mu sync.Mutex
	// framesBeforeDrop is how many events each connection delivers before
	// hanging up. Zero means "never drop".
	framesBeforeDrop int
	// connections counts accepted stream requests.
	connections int
	// fromParams records the `from` query value of each connection.
	fromParams []string
	// lastEventIDs records the Last-Event-ID header of each connection.
	lastEventIDs []string
	// total is the highest sequence the session contains.
	total uint64
}

// ServeHTTP replays sequences [from, total] as SSE, dropping the connection
// after framesBeforeDrop frames when configured.
func (s *resumeTestServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.connections++
	connection := s.connections
	s.fromParams = append(s.fromParams, r.URL.Query().Get("from"))
	s.lastEventIDs = append(s.lastEventIDs, r.Header.Get("Last-Event-ID"))
	dropAfter := s.framesBeforeDrop
	total := s.total
	s.mu.Unlock()

	from, _ := strconv.ParseUint(r.URL.Query().Get("from"), 10, 64)
	if from == 0 {
		from = 1
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	written := 0
	for seq := from; seq <= total; seq++ {
		writeTestSSEEvent(w, seq, "assistant_chunk", map[string]interface{}{
			"kind":            "assistant_chunk",
			"assistant_chunk": map[string]interface{}{"text": fmt.Sprintf("chunk-%d", seq)},
		})
		written++
		if dropAfter > 0 && written >= dropAfter && seq < total {
			// Returning from the handler closes the response body, which the
			// client observes as an unexpected end of stream.
			return
		}
	}
	if connection > 0 && flusher != nil {
		flusher.Flush()
	}
	// Hold the connection open like a real follow tail so the client does not
	// treat the drained replay as a drop.
	<-r.Context().Done()
}

// newResumeStream builds a stream against srv with a compressed retry schedule
// so the reconnect path runs in milliseconds.
func newResumeStream(t *testing.T, ctx context.Context, tr *Transport, req *orchestrator.SubscribeSessionEventsRequest, maxReconnects int) *sseSessionEventStream {
	t.Helper()
	raw, err := tr.RequestStream(ctx, "quicbaas.Orchestrator", "SubscribeSessionEvents", req)
	if err != nil {
		t.Fatalf("RequestStream returned error: %v", err)
	}
	stream, ok := raw.(*sseSessionEventStream)
	if !ok {
		t.Fatalf("expected *sseSessionEventStream, got %T", raw)
	}
	stream.maxReconnects = maxReconnects
	stream.reconnectBase = time.Millisecond
	return stream
}

// TestSubscribeSessionEvents_ResumesAfterMidStreamDrop is the regression test
// for the reported failure: the connection dies mid-turn and the reader must
// carry on without the caller ever seeing it.
func TestSubscribeSessionEvents_ResumesAfterMidStreamDrop(t *testing.T) {
	backend := &resumeTestServer{framesBeforeDrop: 2, total: 6}
	server := httptest.NewServer(backend)
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	stream := newResumeStream(t, ctx, tr, &orchestrator.SubscribeSessionEventsRequest{
		SessionID: "s1", FromSequence: 1, Follow: true,
	}, 5)
	defer stream.Close()

	var sequences []uint64
	for len(sequences) < 6 {
		msg, err := stream.Recv()
		if err != nil {
			t.Fatalf("Recv failed after %d events: %v", len(sequences), err)
		}
		env, ok := msg.(*orchestrator.SessionEventEnvelope)
		if !ok {
			t.Fatalf("unexpected message type %T", msg)
		}
		sequences = append(sequences, env.Sequence)
	}

	want := []uint64{1, 2, 3, 4, 5, 6}
	for i, seq := range want {
		if sequences[i] != seq {
			t.Fatalf("sequence gap or duplicate: got %v, want %v", sequences, want)
		}
	}

	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.connections < 3 {
		t.Fatalf("expected the stream to be re-opened after each drop, got %d connections", backend.connections)
	}
	// Every resume must ask for exactly one past the last delivered sequence,
	// on both the query parameter and the RFC header.
	if backend.fromParams[1] != "3" || backend.lastEventIDs[1] != "2" {
		t.Errorf("first resume asked for from=%q Last-Event-ID=%q, want from=3 Last-Event-ID=2",
			backend.fromParams[1], backend.lastEventIDs[1])
	}
	if backend.fromParams[2] != "5" || backend.lastEventIDs[2] != "4" {
		t.Errorf("second resume asked for from=%q Last-Event-ID=%q, want from=5 Last-Event-ID=4",
			backend.fromParams[2], backend.lastEventIDs[2])
	}
}

// TestSubscribeSessionEvents_ResumeExhaustionReportsTypedError asserts a server
// that is genuinely gone produces a typed, actionable error rather than an
// infinite retry loop or a silent truncation.
func TestSubscribeSessionEvents_ResumeExhaustionReportsTypedError(t *testing.T) {
	backend := &resumeTestServer{framesBeforeDrop: 1, total: 9}
	server := httptest.NewServer(backend)

	tr := newTestTransport(t, server)
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	stream := newResumeStream(t, ctx, tr, &orchestrator.SubscribeSessionEventsRequest{
		SessionID: "s1", FromSequence: 1, Follow: true,
	}, 2)
	defer stream.Close()

	// First frame arrives, then the connection drops and the server goes away.
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("first Recv failed: %v", err)
	}
	server.Close()

	var recvErr error
	for i := 0; i < 10 && recvErr == nil; i++ {
		_, recvErr = stream.Recv()
	}
	if recvErr == nil {
		t.Fatal("expected an error once the server was gone")
	}

	var reconnectErr *SessionStreamReconnectError
	if !errors.As(recvErr, &reconnectErr) {
		t.Fatalf("expected *SessionStreamReconnectError, got %T: %v", recvErr, recvErr)
	}
	if reconnectErr.SessionID != "s1" {
		t.Errorf("expected session s1, got %q", reconnectErr.SessionID)
	}
	if reconnectErr.LastSequence != 1 {
		t.Errorf("expected LastSequence 1, got %d", reconnectErr.LastSequence)
	}
	if reconnectErr.Attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", reconnectErr.Attempts)
	}
	if reconnectErr.Cause == nil {
		t.Error("expected the final attempt's failure to be preserved")
	}
	if sdkerrors.Code(recvErr) != sdkerrors.CodeConnection {
		t.Errorf("expected CodeConnection, got %v", sdkerrors.Code(recvErr))
	}
}

// TestSubscribeSessionEvents_NonFollowEndsWithoutReconnect asserts a one-shot
// replay is still allowed to end: only a follow stream is resumed.
func TestSubscribeSessionEvents_NonFollowEndsWithoutReconnect(t *testing.T) {
	var connections int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections++
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		writeTestSSEEvent(w, 1, "assistant_chunk", map[string]interface{}{"kind": "assistant_chunk"})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	stream := newResumeStream(t, context.Background(), tr,
		&orchestrator.SubscribeSessionEventsRequest{SessionID: "s1", Follow: false}, 5)
	defer stream.Close()

	if _, err := stream.Recv(); err != nil {
		t.Fatalf("first Recv failed: %v", err)
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("expected io.EOF at the end of a non-follow replay, got %v", err)
	}
	if connections != 1 {
		t.Errorf("a non-follow stream must not reconnect, got %d connections", connections)
	}
}

// TestSubscribeSessionEvents_ServerErrorFrameIsNotRetried asserts an explicit
// server-side failure ends the stream immediately: the server has already said
// the subscription cannot continue, so retrying would only hammer it.
func TestSubscribeSessionEvents_ServerErrorFrameIsNotRetried(t *testing.T) {
	var connections int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections++
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "event: error\ndata: range failed: store offline\n\n")
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	stream := newResumeStream(t, context.Background(), tr,
		&orchestrator.SubscribeSessionEventsRequest{SessionID: "s1", Follow: true}, 5)
	defer stream.Close()

	_, err := stream.Recv()
	if err == nil {
		t.Fatal("expected the server's error frame to surface")
	}
	var reconnectErr *SessionStreamReconnectError
	if errors.As(err, &reconnectErr) {
		t.Fatalf("an explicit server error must not be treated as a dropped stream: %v", err)
	}
	if connections != 1 {
		t.Errorf("expected no reconnect after an error frame, got %d connections", connections)
	}
}

// TestSubscribeSessionEvents_CancelledContextEndsQuietly asserts an operator
// detaching (ctrl-c) ends the stream without an error and without reconnecting.
func TestSubscribeSessionEvents_CancelledContextEndsQuietly(t *testing.T) {
	backend := &resumeTestServer{framesBeforeDrop: 1, total: 5}
	server := httptest.NewServer(backend)
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	stream := newResumeStream(t, ctx, tr,
		&orchestrator.SubscribeSessionEventsRequest{SessionID: "s1", FromSequence: 1, Follow: true}, 5)
	defer stream.Close()

	if _, err := stream.Recv(); err != nil {
		t.Fatalf("first Recv failed: %v", err)
	}
	cancel()

	var err error
	for i := 0; i < 10 && err == nil; i++ {
		_, err = stream.Recv()
	}
	if err != io.EOF {
		t.Fatalf("expected io.EOF after cancellation, got %v", err)
	}
}

// TestSubscribeSessionEvents_CloseStopsReconnect asserts an explicit Close
// terminates the stream even while a resume is in flight, so a caller can
// always unblock a reader.
func TestSubscribeSessionEvents_CloseStopsReconnect(t *testing.T) {
	backend := &resumeTestServer{framesBeforeDrop: 1, total: 5}
	server := httptest.NewServer(backend)
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	stream := newResumeStream(t, context.Background(), tr,
		&orchestrator.SubscribeSessionEventsRequest{SessionID: "s1", FromSequence: 1, Follow: true}, 5)

	if _, err := stream.Recv(); err != nil {
		t.Fatalf("first Recv failed: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close must be idempotent, got: %v", err)
	}

	var err error
	for i := 0; i < 10 && err == nil; i++ {
		_, err = stream.Recv()
	}
	if err != io.EOF {
		t.Fatalf("expected io.EOF after Close, got %v", err)
	}
}

// TestSessionStreamBackoff asserts the retry schedule grows and is capped, so
// a persistent outage cannot turn into a hot loop.
func TestSessionStreamBackoff(t *testing.T) {
	stream := &sseSessionEventStream{reconnectBase: sessionStreamReconnectBase}
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 250 * time.Millisecond},
		{2, 500 * time.Millisecond},
		{3, time.Second},
		{4, 2 * time.Second},
		{5, sessionStreamReconnectMax},
		{6, sessionStreamReconnectMax},
		{99, sessionStreamReconnectMax},
	}
	for _, tc := range cases {
		if got := stream.backoff(tc.attempt); got != tc.want {
			t.Errorf("backoff(%d) = %s, want %s", tc.attempt, got, tc.want)
		}
	}
}

// TestSessionStreamReconnectError_Message asserts the typed error tells an
// operator what was lost and where to resume from.
func TestSessionStreamReconnectError_Message(t *testing.T) {
	cause := errors.New("connection refused")
	err := &SessionStreamReconnectError{SessionID: "s1", LastSequence: 42, Attempts: 5, Cause: cause}
	if !errors.Is(err, cause) {
		t.Error("expected the cause to remain reachable through Unwrap")
	}
	for _, want := range []string{"s1", "42", "5", "connection refused"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected error message to contain %q, got %q", want, err.Error())
		}
	}
}

// TestRequestStream_ClosedTransportRejectsSubscribe asserts a closed transport
// refuses to open a stream instead of dialing a torn-down connection pool.
func TestRequestStream_ClosedTransportRejectsSubscribe(t *testing.T) {
	tr, err := New(&transport.Config{Address: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	_, err = tr.RequestStream(context.Background(), "quicbaas.Orchestrator", "SubscribeSessionEvents",
		&orchestrator.SubscribeSessionEventsRequest{SessionID: "s1", Follow: true})
	if err == nil {
		t.Fatal("expected an error from a closed transport")
	}
	if sdkerrors.Code(err) != sdkerrors.CodeConnection {
		t.Errorf("expected CodeConnection, got %v", sdkerrors.Code(err))
	}
}
