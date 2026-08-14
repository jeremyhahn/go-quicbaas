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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// REST has no bidirectional streaming, but SubscribeSessionEvents is a
// server-to-client stream: the client sends one GET and only ever reads
// afterward. That shape maps directly onto the orchestrator's SSE endpoint
// (GET /api/v1/orchestrator/sessions/{id}/events?follow=&from=), so this
// file lets SubscribeSessionEvents ride that endpoint through the same
// transport.Stream contract the gRPC and QUIC transports satisfy natively,
// instead of the flat "REST does not support streaming" rejection every
// other RequestStream call still returns.

const (
	// sessionStreamMaxReconnects bounds transparent resumption of a follow
	// stream. A follow stream that ends while the caller still wants it is
	// almost always transient (a proxy recycling a connection, a daemon
	// rolling over), so the reader resumes rather than surfacing an error the
	// operator can do nothing with. The bound exists so a server that is
	// genuinely gone produces an error instead of an infinite retry loop.
	sessionStreamMaxReconnects = 5

	// sessionStreamReconnectBase is the first reconnect delay; each further
	// attempt doubles it up to sessionStreamReconnectMax. The five attempts
	// therefore span roughly 8 seconds, which is deliberately inside the
	// server's presence linger window so a resumed stream leaves no
	// operator_left / operator_joined churn in the session transcript.
	sessionStreamReconnectBase = 250 * time.Millisecond

	// sessionStreamReconnectMax caps the exponential backoff.
	sessionStreamReconnectMax = 4 * time.Second
)

// SessionStreamReconnectError reports that a session-event follow stream ended
// unexpectedly and could not be re-established within the retry budget. The
// turn being observed is unaffected — it continues on the server — so callers
// should treat this as "observation lost", not "work failed".
type SessionStreamReconnectError struct {
	// SessionID is the session whose stream could not be resumed.
	SessionID string
	// LastSequence is the highest sequence successfully delivered before the
	// stream dropped. Resuming from LastSequence+1 loses nothing.
	LastSequence uint64
	// Attempts is the number of reconnect attempts made.
	Attempts int
	// Cause is the failure from the final attempt.
	Cause error
}

// Error implements the error interface.
func (e *SessionStreamReconnectError) Error() string {
	return fmt.Sprintf("session %s: event stream dropped after sequence %d and could not be resumed in %d attempts: %v",
		e.SessionID, e.LastSequence, e.Attempts, e.Cause)
}

// Unwrap exposes the final attempt's failure to errors.Is/As.
func (e *SessionStreamReconnectError) Unwrap() error { return e.Cause }

// subscribeSessionEvents opens the REST SSE session-events stream for
// SubscribeSessionEvents. It is the sole method transport.RequestStream
// delegates to for the Orchestrator service; every other streaming RPC
// keeps returning ErrStreamNotSupported.
func (h *orchestratorHandler) subscribeSessionEvents(ctx context.Context, req interface{}) (transport.Stream, error) {
	sdkReq, ok := req.(*orchestrator.SubscribeSessionEventsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("rest.orchestrator.SubscribeSessionEvents", "invalid request type")
	}
	if sdkReq.SessionID == "" {
		return nil, errors.NewInvalidInput("rest.orchestrator.SubscribeSessionEvents", "session_id is required")
	}
	if h.transport.closed.Load() {
		return nil, errors.NewConnection("rest.orchestrator.SubscribeSessionEvents", "transport is closed")
	}

	resp, err := h.transport.openSessionEventStream(ctx, sdkReq, sdkReq.FromSequence, 0)
	if err != nil {
		return nil, err
	}

	return &sseSessionEventStream{
		transport:     h.transport,
		ctx:           ctx,
		req:           sdkReq,
		resp:          resp,
		reader:        bufio.NewReaderSize(resp.Body, 64*1024),
		maxReconnects: sessionStreamMaxReconnects,
		reconnectBase: sessionStreamReconnectBase,
	}, nil
}

// openSessionEventStream issues the SSE GET. fromSeq is the first sequence the
// server should deliver; lastEventID, when non-zero, is additionally sent as
// the RFC-standard Last-Event-ID header, which the handler prefers over the
// query parameter. Both are set on a resume so the request is unambiguous to
// any intermediary that only understands one of them.
func (t *Transport) openSessionEventStream(ctx context.Context, req *orchestrator.SubscribeSessionEventsRequest, fromSeq, lastEventID uint64) (*http.Response, error) {
	const op = "rest.orchestrator.SubscribeSessionEvents"

	url := t.baseURL + t.basePath +
		"/api/v1/orchestrator/sessions/" + req.SessionID + "/events" +
		"?from=" + strconv.FormatUint(fromSeq, 10) +
		"&follow=" + strconv.FormatBool(req.Follow)
	if req.TenantID != "" {
		url += "&tenant_id=" + req.TenantID
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, op, "failed to create HTTP request")
	}
	httpReq.Header.Set("Accept", "text/event-stream")
	if lastEventID > 0 {
		httpReq.Header.Set("Last-Event-ID", strconv.FormatUint(lastEventID, 10))
	}

	t.mu.RLock()
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}
	t.mu.RUnlock()

	// The follow tail can run far longer than the transport's configured
	// request timeout (that timeout bounds a unary call, not a session's
	// lifetime), so the stream is issued on a client sharing the transport's
	// connection pool and TLS config but with no per-request deadline.
	// Cancellation is the caller's context alone — exactly like the gRPC and
	// QUIC stream transports.
	resp, err := t.streamHTTPClient().Do(httpReq)
	if err != nil {
		return nil, t.convertHTTPError(err, op)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, t.parseErrorResponse(resp.StatusCode, body)
	}
	return resp, nil
}

// sseSessionEventStream adapts an SSE HTTP response body to transport.Stream
// so SubscribeSessionEvents's caller (sdk/go/client/orchestrator.client) can
// consume it with the exact same receive loop it uses for gRPC and QUIC.
//
// A follow stream that ends without the server saying why is resumed
// transparently from the last delivered sequence, so a dropped connection is
// invisible to the caller. Only a stream that cannot be re-established, or one
// the server explicitly ended with an `event: error` frame, reaches the caller
// as an error.
type sseSessionEventStream struct {
	transport *Transport
	// ctx is the subscription's lifetime; cancelling it ends the stream and
	// suppresses reconnection.
	ctx context.Context
	// req is retained so a resume reproduces the original subscription
	// (session, tenant, follow mode).
	req *orchestrator.SubscribeSessionEventsRequest

	// mu guards resp, which Recv swaps on a resume while Close may be
	// closing it from another goroutine. reader is touched only by Recv,
	// which the transport.Stream contract restricts to one goroutine.
	mu     sync.Mutex
	resp   *http.Response
	reader *bufio.Reader

	// lastSeq is the highest sequence handed to the caller. A resume asks for
	// lastSeq+1, which is what makes reconnection gap-free and duplicate-free.
	lastSeq uint64
	// attempts counts consecutive failed resumes; any delivered frame resets
	// it so a long-lived stream is never starved by earlier, healed blips.
	attempts int
	// maxReconnects and reconnectBase bound resumption. They default to the
	// package constants at construction and are fields rather than constants
	// so tests can compress the retry schedule without a mutable global.
	maxReconnects int
	reconnectBase time.Duration

	closed atomic.Bool
}

// Send always fails: the SSE stream is server-to-client only.
// SubscribeSessionEvents never sends a follow-up message once the
// subscription is open, so this path is not exercised in practice.
func (s *sseSessionEventStream) Send(msg interface{}) error {
	return errors.NewUnimplemented("rest.orchestrator.SubscribeSessionEvents",
		"REST session-event stream does not accept client-sent messages")
}

// CloseSend is a no-op: there is no client-to-server half to close.
func (s *sseSessionEventStream) CloseSend() error {
	return nil
}

// Close releases the underlying HTTP response body, unblocking any read in
// progress on the connection. It also marks the stream closed so an in-flight
// Recv reports end-of-stream instead of trying to reconnect.
func (s *sseSessionEventStream) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	s.mu.Lock()
	body := s.resp.Body
	s.mu.Unlock()
	return body.Close()
}

// Recv decodes the next SSE frame into a SessionEventEnvelope. It skips
// comment/ping lines transparently, resumes the stream when the connection
// drops mid-follow, and reports the server's "error" event as a QBError so the
// caller's receive loop ends the same way a broken gRPC stream would.
func (s *sseSessionEventStream) Recv() (interface{}, error) {
	for {
		eventType, data, err := readSSEFrame(s.reader)
		if err != nil {
			if resumeErr := s.resume(err); resumeErr != nil {
				return nil, resumeErr
			}
			continue
		}
		if data == "" {
			// Ping/comment-only frame (no event, no data): keep waiting. It
			// still proves the connection is alive, so the reconnect budget
			// is restored.
			s.attempts = 0
			continue
		}
		if eventType == "error" {
			return nil, errors.NewInternal("rest.orchestrator.SubscribeSessionEvents", data)
		}

		var wire sseSessionEventEnvelope
		if err := json.Unmarshal([]byte(data), &wire); err != nil {
			return nil, errors.Wrap(err, errors.CodeInternal, "rest.orchestrator.SubscribeSessionEvents",
				"failed to decode event")
		}
		s.attempts = 0
		if wire.Sequence > s.lastSeq {
			s.lastSeq = wire.Sequence
		}
		return &orchestrator.SessionEventEnvelope{
			SessionID:   wire.SessionID,
			TenantID:    wire.TenantID,
			Sequence:    wire.Sequence,
			PayloadJSON: wire.PayloadJSON,
			Timestamp:   wire.Timestamp,
			FromReplay:  wire.FromReplay,
		}, nil
	}
}

// resume re-establishes a dropped follow stream, returning nil when the caller
// should keep reading and a terminal error otherwise.
//
// A non-follow stream ends for real when the server finishes the replay, so
// its end-of-body is reported verbatim. A cancelled context or an explicit
// Close is the caller's own doing and ends the stream quietly.
func (s *sseSessionEventStream) resume(cause error) error {
	if s.closed.Load() {
		return io.EOF
	}
	if err := s.ctx.Err(); err != nil {
		return io.EOF
	}
	if !s.req.Follow {
		if cause == io.EOF {
			return io.EOF
		}
		return s.transport.convertHTTPError(cause, "rest.orchestrator.SubscribeSessionEvents")
	}

	s.mu.Lock()
	previous := s.resp.Body
	s.mu.Unlock()
	_ = previous.Close()

	for s.attempts < s.maxReconnects {
		s.attempts++
		if err := s.wait(s.backoff(s.attempts)); err != nil {
			return io.EOF
		}
		if s.closed.Load() {
			return io.EOF
		}
		resp, err := s.transport.openSessionEventStream(s.ctx, s.req, s.lastSeq+1, s.lastSeq)
		if err != nil {
			cause = err
			continue
		}
		s.mu.Lock()
		s.resp = resp
		s.mu.Unlock()
		// Close may have raced the dial; the loser closes the connection it
		// just opened rather than leaking it.
		if s.closed.Load() {
			_ = resp.Body.Close()
			return io.EOF
		}
		s.reader = bufio.NewReaderSize(resp.Body, 64*1024)
		return nil
	}

	return errors.Wrap(&SessionStreamReconnectError{
		SessionID:    s.req.SessionID,
		LastSequence: s.lastSeq,
		Attempts:     s.attempts,
		Cause:        cause,
	}, errors.CodeConnection, "rest.orchestrator.SubscribeSessionEvents",
		"session event stream could not be resumed")
}

// wait sleeps for d unless the subscription context ends first, in which case
// it returns the context's error.
func (s *sseSessionEventStream) wait(d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case <-timer.C:
		return nil
	}
}

// backoff returns the delay before attempt n (1-based): exponential from
// reconnectBase, capped at sessionStreamReconnectMax.
func (s *sseSessionEventStream) backoff(attempt int) time.Duration {
	delay := s.reconnectBase << (attempt - 1)
	if delay > sessionStreamReconnectMax || delay <= 0 {
		return sessionStreamReconnectMax
	}
	return delay
}

// sseSessionEventEnvelope mirrors the wire shape the server's SSE handler
// writes for every `data:` frame (pkg/api/rest handlers_session_events.go's
// sessionEventEnvelope). PayloadJSON is captured as raw JSON rather than
// decoded: the SDK's SessionEventEnvelope carries it the same way, leaving
// event-kind dispatch to the caller.
type sseSessionEventEnvelope struct {
	SessionID   string          `json:"session_id"`
	TenantID    string          `json:"tenant_id,omitempty"`
	Sequence    uint64          `json:"sequence"`
	Timestamp   time.Time       `json:"timestamp"`
	PayloadJSON json.RawMessage `json:"payload_json"`
	FromReplay  bool            `json:"from_replay"`
}

// readSSEFrame reads one Server-Sent Events frame from r, returning its
// `event:` type (empty for the default message event) and the concatenation
// of its `data:` lines. `id:` lines and comment lines (leading ':') are
// consumed and discarded — the sequence they'd carry is already present in
// the JSON payload, and comments exist only as keep-alive pings. A frame
// with no data (a ping) is reported as ("", "", nil) so the caller can loop
// for the next one; end of stream surfaces as (_, _, io.EOF) or the
// underlying read error.
func readSSEFrame(r *bufio.Reader) (eventType, data string, err error) {
	var dataLines []string
	for {
		line, readErr := r.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, ":"):
			// Comment/ping line — ignored.
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}

		if readErr != nil {
			return "", "", readErr
		}
		if line == "" {
			// Blank line: end of one SSE frame. A ping (no event, no data)
			// resolves to ("", "") so the caller keeps reading.
			return eventType, strings.Join(dataLines, "\n"), nil
		}
	}
}
