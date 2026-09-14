// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// newListSessionsQueryCapture starts a server that records the query string of
// the single request the handler makes and answers with an empty session list.
// The query travels over a buffered channel so the handler goroutine and the
// test goroutine are synchronized by the channel.
func newListSessionsQueryCapture(t *testing.T) (*Transport, chan url.Values) {
	t.Helper()
	got := make(chan url.Values, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"sessions":[]}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return newTestTransport(t, server), got
}

// TestRESTListSessions_ForwardsStatusFilter pins the status filter into the
// REST query string. The QUIC transport embeds this transport, so this is
// also QUIC's coverage of the filter. The sibling gRPC transport silently
// dropped Status (go-quicbaas#792); this holds the REST path honest.
func TestRESTListSessions_ForwardsStatusFilter(t *testing.T) {
	tr, got := newListSessionsQueryCapture(t)

	if _, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "ListSessions",
		&orchestrator.ListSessionsRequest{Backend: "claude", Status: "active"}); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	query := <-got
	if query.Get("status") != "active" {
		t.Errorf("status query = %q, want %q", query.Get("status"), "active")
	}
	if query.Get("backend") != "claude" {
		t.Errorf("backend query = %q, want %q", query.Get("backend"), "claude")
	}
}

// TestRESTListSessions_EmptyStatusIsOmitted proves an unset filter sends no
// status parameter at all. The server reads a missing status as "every
// state"; sending an empty one must not be mistaken for a real filter value.
func TestRESTListSessions_EmptyStatusIsOmitted(t *testing.T) {
	tr, got := newListSessionsQueryCapture(t)

	if _, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "ListSessions",
		&orchestrator.ListSessionsRequest{}); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	query := <-got
	if _, present := query["status"]; present {
		t.Errorf("status query present for an unfiltered list: %v", query)
	}
	if _, present := query["backend"]; present {
		t.Errorf("backend query present for an unfiltered list: %v", query)
	}
}

// TestRESTListSessions_CarriesTheTenantFromContext is the REST counterpart of
// the gRPC transport putting TenantId on the proto.
//
// The server derives tenancy from ?tenant_id=, then the authenticated tenant,
// then the system tenant. Omitting the parameter therefore did not fail — it
// quietly listed a DIFFERENT tenant's sessions than the one the caller had
// scoped the context to, and answered the same request differently depending
// on which transport the caller happened to use.
func TestRESTListSessions_CarriesTheTenantFromContext(t *testing.T) {
	tr, got := newListSessionsQueryCapture(t)

	ctx := transport.ContextWithTenant(context.Background(), "tenant-a")
	if _, err := tr.Request(ctx, "quicbaas.Orchestrator", "ListSessions",
		&orchestrator.ListSessionsRequest{Status: "active"}); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	query := <-got
	if query.Get("tenant_id") != "tenant-a" {
		t.Errorf("tenant_id query = %q, want tenant-a; without it the server falls back to "+
			"the authenticated or system tenant and lists the wrong sessions",
			query.Get("tenant_id"))
	}
	if query.Get("status") != "active" {
		t.Errorf("status query = %q, want active", query.Get("status"))
	}
}

// TestRESTListSessions_EscapesFilterValues pins the encoding.
//
// The query used to be built by string concatenation, so a backend name
// carrying an & or an = truncated it — and the status filter sitting behind
// that backend went missing, silently widening the listing to every state.
func TestRESTListSessions_EscapesFilterValues(t *testing.T) {
	tr, got := newListSessionsQueryCapture(t)

	if _, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "ListSessions",
		&orchestrator.ListSessionsRequest{Backend: "claude&status=closed", Status: "active"}); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	query := <-got
	if query.Get("backend") != "claude&status=closed" {
		t.Errorf("backend query = %q, want the value verbatim", query.Get("backend"))
	}
	if query.Get("status") != "active" {
		t.Errorf("status query = %q, want active; an unescaped backend value swallowed the "+
			"filter behind it and widened the listing to every state", query.Get("status"))
	}
}
