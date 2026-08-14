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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// sessionSSEHeartbeatInterval is how often the live tail emits an SSE comment
// frame while no session events are flowing.
//
// The heartbeat exists so a silent stream — an agent thinking for minutes with
// nothing to say — is distinguishable from a dead one by everything between
// the handler and the client. The interval is chosen against the smallest idle
// timeout in the common path: nginx's proxy_read_timeout, HAProxy's timeout
// tunnel, and AWS ALB's idle timeout all default to 60 seconds. At 15 seconds
// three consecutive heartbeats can be lost before any of them would reap the
// connection, and the cost is a dozen bytes per attached client per 15s.
const sessionSSEHeartbeatInterval = 15 * time.Second

// sessionSSERetryAdvice is the reconnection delay advertised to EventSource
// clients via the SSE `retry:` field. Browsers otherwise pick their own
// (typically 3s); stating it explicitly keeps browser and SDK reconnect
// windows inside orchestration.DefaultPresenceLinger, which is what makes a
// reconnect invisible in the transcript.
const sessionSSERetryAdvice = 2 * time.Second

// sessionLogAccessor is the optional interface the agent service may satisfy
// so REST handlers can reach the underlying SessionEventLog. Declared
// locally so this file has no dependency on the concrete implementation.
type sessionLogAccessor interface {
	SessionEventLog() orchestration.SessionEventLog
}

// HandleOrchestratorSessionEvents serves
// GET /api/v1/orchestrator/sessions/{sessionID}/events — the unified REST
// attach endpoint. Behavior is selected by query params:
//
//	?from=0       — replay from the beginning of the session
//	?from=N       — replay starting at sequence N
//	&follow=1     — after replay, stay connected and tail live events
//	&follow=0     — (default) close after replay is drained
//
// Response format: Server-Sent Events (text/event-stream) with one `event:`
// frame per SessionEvent. Each event carries `id: <sequence>` so browser
// EventSource clients can resume after a reconnect by setting
// `Last-Event-ID` to the previously-seen sequence. `data:` payload is the
// same SessionEvent JSON shape stored in the log.
//
// Clients that prefer a one-shot JSON page (for scrollback pagination)
// pass Accept: application/json; we return a JSON array instead of SSE.
// Browsers with EventSource pass the SSE Accept header by default.
func (s *Server) HandleOrchestratorSessionEvents(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		RespondBadRequest(w, r, "session ID is required")
		return
	}
	// Resolve tenant the SAME way every other orchestrator REST handler does
	// (query override -> authenticated principal's tenant -> system default).
	// This handler previously derived tenant from the query parameter ONLY,
	// silently discarding the authenticated caller's tenant binding — a
	// tenant-isolation gap now that the session log's KV store is
	// tenant-routed (see pkg/server durableKVStore): the context handed to
	// the log MUST carry the resolved tenant, or a barrier-isolated tenant's
	// transcript reads would resolve against the wrong (or no) tenant store.
	ctx, tenantID := resolveOrchestratorTenant(r)
	fromSeq := uint64(0)
	if raw := r.URL.Query().Get("from"); raw != "" {
		n, parseErr := strconv.ParseUint(raw, 10, 64)
		if parseErr != nil {
			RespondBadRequest(w, r, "invalid from parameter: "+parseErr.Error())
			return
		}
		fromSeq = n
	}
	// EventSource reconnect: honor Last-Event-ID over the `from` param so
	// browsers that drop and reconnect never miss or double-deliver events.
	if lastID := r.Header.Get("Last-Event-ID"); lastID != "" {
		if n, parseErr := strconv.ParseUint(lastID, 10, 64); parseErr == nil && n > 0 {
			fromSeq = n + 1
		}
	}
	follow := r.URL.Query().Get("follow") == "1" || r.URL.Query().Get("follow") == "true"

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}
	accessor, ok := agentSvc.(sessionLogAccessor)
	if !ok {
		RespondJSON(w, r, http.StatusServiceUnavailable, map[string]string{
			"error": "session event log not available",
		})
		return
	}
	log := accessor.SessionEventLog()
	if log == nil {
		RespondJSON(w, r, http.StatusServiceUnavailable, map[string]string{
			"error": "session event log not configured",
		})
		return
	}

	// Content negotiation: JSON mode for paginated history, SSE for live tail.
	wantJSON := r.Header.Get("Accept") == "application/json"
	if wantJSON && !follow {
		respondSessionHistoryJSON(ctx, w, r, log, tenantID, sessionID, fromSeq)
		return
	}
	serveSessionEventsSSE(ctx, w, r, sessionEventStreamOptions{
		Log:        log,
		Presence:   s.sessionPresence,
		TenantID:   tenantID,
		SessionID:  sessionID,
		FromSeq:    fromSeq,
		Follow:     follow,
		OperatorID: sessionOperatorID(ctx, tenantID),
		Interface:  "web",
		OnWarn:     s.warnSessionStream,
	})
}

// warnSessionStream reports a non-fatal session-stream condition (a presence
// append that failed, a connection whose write deadline could not be cleared)
// through the server's logger. Passed to the stream loop as a closure so the
// streaming helpers stay free of the Server type and remain directly testable.
func (s *Server) warnSessionStream(err error) {
	if err == nil || s.logger == nil {
		return
	}
	s.logger.WithField("component", "session-events").Warn(err.Error())
}

// sessionOperatorID derives the display identity used to attribute presence
// (operator_joined/operator_left) events for a live attach. It reads the same
// verified principal resolveOrchestratorTenant consulted for the tenant,
// never a client-supplied value. Falling back to the tenant ID mirrors
// orchestration.sessionCreator's fallback for SessionMeta.CreatedBy so an
// unauthenticated dev boot (no DPoP-aware validator configured) still
// produces a stable, non-empty identity instead of silently dropping
// presence tracking.
func sessionOperatorID(ctx context.Context, tenantID string) string {
	if ac := AuthorizedContextFromRequest(ctx); ac != nil && ac.Subject != "" {
		return ac.Subject
	}
	return tenantID
}

// sessionEventEnvelope is the transport-neutral wire shape every session-events
// reader (REST JSON history, SSE tail, and downstream SDK clients) consumes.
// It matches the SDK `SessionEventEnvelope` contract: the full SessionEvent is
// carried verbatim under payload_json so clients have a single parse path
// regardless of event kind, while sequence/timestamp/session/tenant are lifted
// to top-level fields for cheap addressing without unmarshalling the payload.
type sessionEventEnvelope struct {
	SessionID   string                      `json:"session_id"`
	TenantID    string                      `json:"tenant_id,omitempty"`
	Sequence    uint64                      `json:"sequence"`
	Timestamp   time.Time                   `json:"timestamp"`
	PayloadJSON *orchestration.SessionEvent `json:"payload_json"`
	FromReplay  bool                        `json:"from_replay"`
}

// newSessionEventEnvelope wraps a stored SessionEvent in the canonical wire
// envelope. fromReplay marks events delivered from history (vs the live tail)
// so UIs can suppress per-event animations during initial backfill.
func newSessionEventEnvelope(ev *orchestration.SessionEvent, fromReplay bool) sessionEventEnvelope {
	return sessionEventEnvelope{
		SessionID:   ev.SessionID,
		TenantID:    ev.TenantID,
		Sequence:    ev.Sequence,
		Timestamp:   ev.Timestamp,
		PayloadJSON: ev,
		FromReplay:  fromReplay,
	}
}

// respondSessionHistoryJSON returns one page of events as a JSON array of
// SessionEventEnvelope. Pagination cursor is carried both in the response body
// (next_seq) and the `X-Next-Sequence` response header.
func respondSessionHistoryJSON(ctx context.Context, w http.ResponseWriter, r *http.Request, log orchestration.SessionEventLog, tenantID, sessionID string, fromSeq uint64) {
	limit := 256
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	events, next, err := log.Range(ctx, tenantID, sessionID, fromSeq, limit)
	if err != nil {
		RespondJSON(w, r, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	envelopes := make([]sessionEventEnvelope, 0, len(events))
	for _, ev := range events {
		envelopes = append(envelopes, newSessionEventEnvelope(ev, true))
	}
	w.Header().Set("Content-Type", "application/json")
	if next > 0 {
		w.Header().Set("X-Next-Sequence", strconv.FormatUint(next, 10))
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"events":     envelopes,
		"next_seq":   next,
		"session_id": sessionID,
	})
}

// sessionEventStreamOptions carries everything serveSessionEventsSSE needs.
// Grouping them keeps the streaming loop free of the Server type — the tests
// drive it directly — while leaving room for the presence tracker and the
// warning sink without an unreadable positional parameter list.
type sessionEventStreamOptions struct {
	// Log is the transcript the stream replays and tails.
	Log orchestration.SessionEventLog
	// Presence records operator_joined/operator_left with reconnect
	// suppression. Nil disables presence tracking entirely.
	Presence *orchestration.SessionPresence
	// TenantID and SessionID address the transcript.
	TenantID  string
	SessionID string
	// FromSeq is the first sequence to replay.
	FromSeq uint64
	// Follow selects the live tail after replay drains.
	Follow bool
	// OperatorID attributes presence events; empty disables them.
	OperatorID string
	// Interface names the client surface in presence payloads.
	Interface string
	// HeartbeatInterval overrides sessionSSEHeartbeatInterval. Zero or less
	// selects the default; tests compress it to observe keepalive frames
	// without waiting on the production interval.
	HeartbeatInterval time.Duration
	// OnWarn receives non-fatal stream conditions. Nil discards them.
	OnWarn func(error)
}

// warn reports a non-fatal condition when a sink is configured.
func (o sessionEventStreamOptions) warn(err error) {
	if err != nil && o.OnWarn != nil {
		o.OnWarn(err)
	}
}

// SessionStreamDeadlineError reports that the connection's write deadline
// could not be cleared for a streaming response. The stream still runs, but it
// remains subject to http.Server.WriteTimeout and will be cut when that
// elapses, so the condition is worth surfacing to an operator.
type SessionStreamDeadlineError struct {
	// SessionID is the session whose stream is affected.
	SessionID string
	// Cause is the ResponseController failure.
	Cause error
}

// Error implements the error interface.
func (e *SessionStreamDeadlineError) Error() string {
	return fmt.Sprintf("session %s: cannot clear write deadline for stream, it will be cut at the server WriteTimeout: %v",
		e.SessionID, e.Cause)
}

// Unwrap exposes the ResponseController failure to errors.Is/As.
func (e *SessionStreamDeadlineError) Unwrap() error { return e.Cause }

// serveSessionEventsSSE streams events as Server-Sent Events. The stream
// begins with a replay from opts.FromSeq, then (when opts.Follow) transitions
// to a live tail. Both phases write one `event:` frame per SessionEvent with
// the sequence as the SSE `id:` so clients can resume from Last-Event-ID.
//
// Flushing and deadline control go through http.NewResponseController so the
// calls reach the real connection even when the logging middleware has wrapped
// the ResponseWriter (the wrapper exposes Unwrap; see middleware_flush.go).
//
// The write deadline is cleared first. http.Server.WriteTimeout arms an
// absolute write deadline on the connection when the request is read and never
// extends it, so without this a streaming response is severed exactly
// WriteTimeout after it began — 30s with the shipped defaults — regardless of
// how much data is still flowing. Clearing it per-connection is the supported
// way to exempt a streaming handler (raising WriteTimeout globally would
// weaken every unary route instead).
func serveSessionEventsSSE(ctx context.Context, w http.ResponseWriter, r *http.Request, opts sessionEventStreamOptions) {
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		opts.warn(&SessionStreamDeadlineError{SessionID: opts.SessionID, Cause: err})
	}
	// ErrNotSupported means the writer cannot flush (an in-memory recorder in
	// a test, say). That is not fatal: net/http auto-flushes on Write, so the
	// stream still reaches a real client. Any other flush error is the
	// connection failing and ends the stream.
	flush := func() error {
		if err := rc.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
		return nil
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintf(w, "retry: %d\n\n", sessionSSERetryAdvice.Milliseconds()); err != nil {
		return
	}
	if err := flush(); err != nil {
		return
	}

	// Register live subscription BEFORE draining history so events that
	// arrive during the history read get buffered for post-drain delivery.
	var liveCh <-chan *orchestration.SessionEvent
	if opts.Follow {
		ch, unsubscribe, err := opts.Log.Subscribe(ctx, opts.TenantID, opts.SessionID)
		if err != nil {
			writeSSEError(w, flush, "subscribe failed: "+err.Error())
			return
		}
		liveCh = ch
		defer unsubscribe()

		// Multi-participant presence: record this attach so every other live
		// attacher (human or agent) sees who is watching. The tracker decides
		// whether the join and the matching departure are worth writing to the
		// durable transcript — a stream that drops and resumes within the
		// linger window produces no events at all.
		defer opts.Presence.Join(ctx, opts.Log, opts.TenantID, opts.SessionID, opts.OperatorID, opts.Interface)()
	}

	// History replay. Paginate in 256-event chunks so an enormous session
	// doesn't pin memory or hold the write loop hostage.
	cursor := opts.FromSeq
	if cursor == 0 {
		cursor = 1
	}
	historyEnd := uint64(0)
	for {
		page, next, err := opts.Log.Range(ctx, opts.TenantID, opts.SessionID, cursor, 256)
		if err != nil {
			writeSSEError(w, flush, "range failed: "+err.Error())
			return
		}
		for _, ev := range page {
			if err := writeSSEEvent(w, flush, ev, true); err != nil {
				return
			}
			if ev.Sequence > historyEnd {
				historyEnd = ev.Sequence
			}
		}
		if next == 0 {
			break
		}
		cursor = next
	}

	if !opts.Follow {
		return
	}

	// Live tail. Drop events whose sequence falls within the history we
	// just delivered — covers the brief window where a live event arrived
	// while we were mid-page.
	interval := opts.HeartbeatInterval
	if interval <= 0 {
		interval = sessionSSEHeartbeatInterval
	}
	heartbeat := time.NewTicker(interval)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-liveCh:
			if !ok {
				return
			}
			if ev.Sequence <= historyEnd {
				continue
			}
			if err := writeSSEEvent(w, flush, ev, false); err != nil {
				return
			}
			historyEnd = ev.Sequence
		case <-heartbeat.C:
			// SSE comment line: ignored by every conforming reader, but it
			// puts bytes on the wire so an idle session cannot look dead to a
			// proxy — and so a client that has gone away is detected here
			// rather than at the next real event.
			if _, err := fmt.Fprintf(w, ": ping %d\n\n", time.Now().Unix()); err != nil {
				return
			}
			if err := flush(); err != nil {
				return
			}
		}
	}
}

// writeSSEEvent encodes one SessionEvent as an SSE frame. Format:
//
//	id: <sequence>
//	event: <kind>
//	data: <json>
//	<blank line>
//
// The data payload is a SessionEventEnvelope — the same shape returned by the
// JSON history path — so SSE and history readers share one parse path.
func writeSSEEvent(w http.ResponseWriter, flush func() error, ev *orchestration.SessionEvent, fromReplay bool) error {
	payload, err := json.Marshal(newSessionEventEnvelope(ev, fromReplay))
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Sequence, ev.Kind, payload); err != nil {
		return err
	}
	return flush()
}

// writeSSEError emits a terminal "error" SSE frame before closing the
// connection. Clients can distinguish this from normal event frames via
// the `event: error` header, and must not treat it as a dropped connection
// to be retried — the server has decided the stream cannot continue.
func writeSSEError(w http.ResponseWriter, flush func() error, msg string) {
	_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", msg)
	_ = flush()
}
