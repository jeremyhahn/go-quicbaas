package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/query"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestQueryPaging_RefusedBeforeAnyCall pins that the REST transport refuses
// a Query Execute that asks for paging: the REST server's query routes serve
// no page size or continuation token, so sending the request would silently
// drop Limit and Cursor and report HasMore false.
func TestQueryPaging_RefusedBeforeAnyCall(t *testing.T) {
	tests := []struct {
		name string
		req  *query.QueryRequest
	}{
		{
			name: "limit",
			req:  &query.QueryRequest{EntityType: "users", Index: "email", Operator: query.OpEqual, Value: "a@example.com", Limit: 10},
		},
		{
			name: "cursor",
			req:  &query.QueryRequest{EntityType: "users", Index: "email", Operator: query.OpEqual, Value: "a@example.com", Cursor: "page2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write([]byte(`{"results":[]}`))
				if err != nil {
					t.Errorf("write response: %v", err)
				}
			}))
			defer server.Close()
			tr := newTestTransport(t, server)
			defer tr.Close()

			resp, err := tr.Request(context.Background(), "quicbaas.Query", "Execute", tt.req)
			if err == nil {
				t.Fatalf("Request() = %+v, want an unimplemented error", resp)
			}
			if !errors.IsUnimplemented(err) {
				t.Errorf("Request() error = %v, want CodeUnimplemented", err)
			}
			if got := calls.Load(); got != 0 {
				t.Errorf("server calls = %d, want 0 (refused before any HTTP call)", got)
			}
		})
	}
}

// TestQueryPaging_UnpagedRequestIsSent pins that a Query Execute without
// Limit or Cursor is not refused and still reaches the server.
func TestQueryPaging_UnpagedRequestIsSent(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"results":[]}`))
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	req := &query.QueryRequest{EntityType: "users", Index: "email", Operator: query.OpEqual, Value: "a@example.com"}
	_, err := tr.Request(context.Background(), "quicbaas.Query", "Execute", req)
	if errors.IsUnimplemented(err) {
		t.Fatalf("Request() error = %v, want no paging refusal", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server calls = %d, want 1", got)
	}
}
