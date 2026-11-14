package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// sessionHistoryServer serves GET /api/v1/orchestrator/sessions/{id}/events
// in the shape of the server's JSON history mode (respondSessionHistoryJSON
// over SessionEventLog.Range): sequences are dense from 1, from=0 or absent
// starts at the first event, and next_seq is 0 when no event follows the page.
// sessions maps a session ID to its event count; an unknown session has none.
// Each request's raw query is sent on queries.
func sessionHistoryServer(t *testing.T, sessions map[string]int, queries chan<- string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/orchestrator/sessions/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		queries <- r.URL.RawQuery
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q, want application/json (the server's JSON history mode)", r.Header.Get("Accept"))
		}
		sessionID := r.PathValue("id")
		from := 1
		if raw := r.URL.Query().Get("from"); raw != "" && raw != "0" {
			n, err := strconv.Atoi(raw)
			if err != nil {
				t.Errorf("from %q: %v", raw, err)
			}
			from = n
		}
		limit := 256
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil {
				t.Errorf("limit %q: %v", raw, err)
			}
			limit = n
		}
		total := sessions[sessionID]
		end := min(from+limit-1, total)
		events := make([]map[string]interface{}, 0, limit)
		for seq := from; seq <= end; seq++ {
			events = append(events, map[string]interface{}{
				"session_id":   sessionID,
				"sequence":     seq,
				"payload_json": map[string]interface{}{"kind": "text"},
				"from_replay":  true,
			})
		}
		next := 0
		if end < total {
			next = end + 1
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"events":     events,
			"next_seq":   next,
			"session_id": sessionID,
		}); err != nil {
			t.Errorf("encode page: %v", err)
		}
	})
	return httptest.NewServer(mux)
}

// getSessionHistory sends one GetSessionHistory through the REST transport.
func getSessionHistory(t *testing.T, tr *Transport, req *orchestrator.GetSessionHistoryRequest) (*orchestrator.GetSessionHistoryResponse, error) {
	t.Helper()
	return orchestrator.NewClient(tr).GetSessionHistory(context.Background(), req)
}

func sequences(events []*orchestrator.SessionEventEnvelope) []uint64 {
	out := make([]uint64, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.Sequence)
	}
	return out
}

func equalSequences(got []uint64, want ...uint64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestGetSessionHistory_FullPageWithMoreEventsReturnsNextSeq(t *testing.T) {
	queries := make(chan string, 1)
	server := sessionHistoryServer(t, map[string]int{"s1": 5}, queries)
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	got, err := getSessionHistory(t, tr, &orchestrator.GetSessionHistoryRequest{
		TenantID: "t1", SessionID: "s1", FromSequence: 2, Limit: 2,
	})
	if err != nil {
		t.Fatalf("GetSessionHistory: %v", err)
	}
	if seqs := sequences(got.Events); !equalSequences(seqs, 2, 3) {
		t.Errorf("sequences = %v, want [2 3]", seqs)
	}
	if got.NextSeq != 4 {
		t.Errorf("NextSeq = %d, want 4 (the event after the page)", got.NextSeq)
	}
	query := <-queries
	for _, want := range []string{"tenant_id=t1", "from=2", "limit=2", "follow=false"} {
		if !containsQuery(query, want) {
			t.Errorf("query %q dropped %q", query, want)
		}
	}
	if ev := got.Events[0]; ev.SessionID != "s1" || !ev.FromReplay || string(ev.PayloadJSON) != `{"kind":"text"}` {
		t.Errorf("event = %+v, want session s1 replayed with its payload", ev)
	}
}

func TestGetSessionHistory_ExactlyFullLastPageReturnsNextSeqZero(t *testing.T) {
	queries := make(chan string, 1)
	server := sessionHistoryServer(t, map[string]int{"s1": 4}, queries)
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	got, err := getSessionHistory(t, tr, &orchestrator.GetSessionHistoryRequest{
		SessionID: "s1", FromSequence: 3, Limit: 2,
	})
	if err != nil {
		t.Fatalf("GetSessionHistory: %v", err)
	}
	if seqs := sequences(got.Events); !equalSequences(seqs, 3, 4) {
		t.Errorf("sequences = %v, want [3 4]", seqs)
	}
	if got.NextSeq != 0 {
		t.Errorf("NextSeq = %d, want 0: no event follows the last page", got.NextSeq)
	}
}

func TestGetSessionHistory_FromSequenceZeroStartsAtTheFirstEvent(t *testing.T) {
	queries := make(chan string, 1)
	server := sessionHistoryServer(t, map[string]int{"s1": 3}, queries)
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	got, err := getSessionHistory(t, tr, &orchestrator.GetSessionHistoryRequest{SessionID: "s1"})
	if err != nil {
		t.Fatalf("GetSessionHistory: %v", err)
	}
	if seqs := sequences(got.Events); !equalSequences(seqs, 1, 2, 3) {
		t.Errorf("sequences = %v, want [1 2 3]", seqs)
	}
	if got.NextSeq != 0 {
		t.Errorf("NextSeq = %d, want 0", got.NextSeq)
	}
	if query := <-queries; containsQuery(query, "follow=true") || !containsQuery(query, "follow=false") {
		t.Errorf("query %q must ask for history without following", query)
	}
}

// An unknown session has no events. gRPC answers it with an empty replay that
// ends at EOF, so the page is empty, NextSeq is 0 and there is no error.
func TestGetSessionHistory_UnknownSessionIsAnEmptyPage(t *testing.T) {
	queries := make(chan string, 1)
	server := sessionHistoryServer(t, map[string]int{"s1": 3}, queries)
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	got, err := getSessionHistory(t, tr, &orchestrator.GetSessionHistoryRequest{SessionID: "missing", Limit: 2})
	if err != nil {
		t.Fatalf("GetSessionHistory: %v", err)
	}
	if len(got.Events) != 0 || got.NextSeq != 0 {
		t.Errorf("got %d events, NextSeq %d; want an empty page with NextSeq 0", len(got.Events), got.NextSeq)
	}
}

func TestGetSessionHistory_ServerErrorIsTyped(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   errors.ErrorCode
	}{
		{http.StatusInternalServerError, errors.CodeInternal},
		{http.StatusServiceUnavailable, errors.CodeUnavailable},
	} {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				if _, err := w.Write([]byte(`{"error":"session event log not configured"}`)); err != nil {
					t.Errorf("write: %v", err)
				}
			}))
			defer server.Close()
			tr := newTestTransport(t, server)
			defer tr.Close()

			_, err := getSessionHistory(t, tr, &orchestrator.GetSessionHistoryRequest{SessionID: "s1"})
			if got := errors.Code(err); got != tc.want {
				t.Errorf("error code = %v (%v), want %v", got, err, tc.want)
			}
		})
	}
}

func TestGetSessionHistoryHandler_RejectsBadInput(t *testing.T) {
	h := &orchestratorHandler{}
	ctx := context.Background()
	if _, err := h.handleGetSessionHistory(ctx, &orchestrator.ListSessionsRequest{}); !errors.IsInvalidInput(err) {
		t.Errorf("wrong request type: got %v, want invalid input", err)
	}
	if _, err := h.handleGetSessionHistory(ctx, &orchestrator.GetSessionHistoryRequest{}); !errors.IsInvalidInput(err) {
		t.Errorf("no session ID: got %v, want invalid input", err)
	}
}
