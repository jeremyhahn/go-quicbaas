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
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

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
	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		tenantID = qrdbsdk.SystemTenantID
	}
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
		respondSessionHistoryJSON(w, r, log, tenantID, sessionID, fromSeq)
		return
	}
	serveSessionEventsSSE(w, r, log, tenantID, sessionID, fromSeq, follow)
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
func respondSessionHistoryJSON(w http.ResponseWriter, r *http.Request, log orchestration.SessionEventLog, tenantID, sessionID string, fromSeq uint64) {
	limit := 256
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	events, next, err := log.Range(r.Context(), tenantID, sessionID, fromSeq, limit)
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

// serveSessionEventsSSE streams events as Server-Sent Events. The stream
// begins with a replay from fromSeq, then (when follow=true) transitions
// to a live tail. Both phases write one `event:` frame per SessionEvent
// with the sequence as the SSE `id:` so browser clients can resume.
//
// Flushing goes through http.NewResponseController so the call reaches the real
// connection even when the logging middleware has wrapped the ResponseWriter
// (the wrapper exposes Unwrap; see middleware_flush.go). Flushing is treated as
// best-effort: a writer that cannot flush still streams (net/http auto-flushes
// on Write), so we never refuse the connection with a 500 the way the old
// `w.(http.Flusher)` hard assertion did.
func serveSessionEventsSSE(w http.ResponseWriter, r *http.Request, log orchestration.SessionEventLog, tenantID, sessionID string, fromSeq uint64, follow bool) {
	rc := http.NewResponseController(w)
	flush := func() {
		// ErrNotSupported is the only documented error and is non-fatal here:
		// the events still reach the client on the next Write/close.
		_ = rc.Flush()
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering
	w.WriteHeader(http.StatusOK)
	flush()

	// Register live subscription BEFORE draining history so events that
	// arrive during the history read get buffered for post-drain delivery.
	var liveCh <-chan *orchestration.SessionEvent
	var unsubscribe func()
	if follow {
		ch, unsub, err := log.Subscribe(r.Context(), tenantID, sessionID)
		if err != nil {
			writeSSEError(w, flush, "subscribe failed: "+err.Error())
			return
		}
		liveCh = ch
		unsubscribe = unsub
		defer unsubscribe()
	}

	// History replay. Paginate in 256-event chunks so an enormous session
	// doesn't pin memory or hold the write loop hostage.
	cursor := fromSeq
	if cursor == 0 {
		cursor = 1
	}
	historyEnd := uint64(0)
	for {
		page, next, err := log.Range(r.Context(), tenantID, sessionID, cursor, 256)
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

	if !follow {
		return
	}

	// Live tail. Drop events whose sequence falls within the history we
	// just delivered — covers the brief window where a live event arrived
	// while we were mid-page.
	pingTicker := time.NewTicker(15 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-r.Context().Done():
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
		case <-pingTicker.C:
			// SSE comment line keeps proxies and browsers from timing out
			// during quiet sessions. Comments are ignored by EventSource.
			if _, err := fmt.Fprintf(w, ": ping %d\n\n", time.Now().Unix()); err != nil {
				return
			}
			flush()
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
func writeSSEEvent(w http.ResponseWriter, flush func(), ev *orchestration.SessionEvent, fromReplay bool) error {
	payload, err := json.Marshal(newSessionEventEnvelope(ev, fromReplay))
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Sequence, ev.Kind, payload); err != nil {
		return err
	}
	flush()
	return nil
}

// writeSSEError emits a terminal "error" SSE frame before closing the
// connection. Clients can distinguish this from normal event frames via
// the `event: error` header.
func writeSSEError(w http.ResponseWriter, flush func(), msg string) {
	_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", msg)
	flush()
}
