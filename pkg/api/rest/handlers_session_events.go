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
		tenantID = "default"
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

// respondSessionHistoryJSON returns one page of events as a JSON array.
// Pagination cursor is carried in the `X-Next-Sequence` response header.
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
	w.Header().Set("Content-Type", "application/json")
	if next > 0 {
		w.Header().Set("X-Next-Sequence", strconv.FormatUint(next, 10))
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"events":     events,
		"next_seq":   next,
		"session_id": sessionID,
	})
}

// serveSessionEventsSSE streams events as Server-Sent Events. The stream
// begins with a replay from fromSeq, then (when follow=true) transitions
// to a live tail. Both phases write one `event:` frame per SessionEvent
// with the sequence as the SSE `id:` so browser clients can resume.
func serveSessionEventsSSE(w http.ResponseWriter, r *http.Request, log orchestration.SessionEventLog, tenantID, sessionID string, fromSeq uint64, follow bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		RespondJSON(w, r, http.StatusInternalServerError, map[string]string{
			"error": "streaming unsupported on this connection",
		})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Register live subscription BEFORE draining history so events that
	// arrive during the history read get buffered for post-drain delivery.
	var liveCh <-chan *orchestration.SessionEvent
	var unsubscribe func()
	if follow {
		ch, unsub, err := log.Subscribe(r.Context(), tenantID, sessionID)
		if err != nil {
			writeSSEError(w, flusher, "subscribe failed: "+err.Error())
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
			writeSSEError(w, flusher, "range failed: "+err.Error())
			return
		}
		for _, ev := range page {
			if err := writeSSEEvent(w, flusher, ev, true); err != nil {
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
			if err := writeSSEEvent(w, flusher, ev, false); err != nil {
				return
			}
			historyEnd = ev.Sequence
		case <-pingTicker.C:
			// SSE comment line keeps proxies and browsers from timing out
			// during quiet sessions. Comments are ignored by EventSource.
			if _, err := fmt.Fprintf(w, ": ping %d\n\n", time.Now().Unix()); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeSSEEvent encodes one SessionEvent as an SSE frame. Format:
//
//	id: <sequence>
//	event: <kind>
//	data: <json>
//	<blank line>
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, ev *orchestration.SessionEvent, fromReplay bool) error {
	payload, err := json.Marshal(map[string]interface{}{
		"event":       ev,
		"from_replay": fromReplay,
	})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Sequence, ev.Kind, payload); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// writeSSEError emits a terminal "error" SSE frame before closing the
// connection. Clients can distinguish this from normal event frames via
// the `event: error` header.
func writeSSEError(w http.ResponseWriter, flusher http.Flusher, msg string) {
	_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", msg)
	flusher.Flush()
}
