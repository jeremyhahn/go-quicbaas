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

package quic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestRequest_ListTasksBySourceOverHTTP3 proves the task listing's source
// predicate (go-quicbaas#730) reaches the server over QUIC, escaped: a source
// ref is an issue reference such as owner/repo#12, whose '#' would otherwise
// end the query and drop the predicate.
func TestRequest_ListTasksBySourceOverHTTP3(t *testing.T) {
	gotQuery := make(chan url.Values, 1)

	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery <- r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"tasks": []interface{}{}, "total": 0})
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(addr))
	require.NoError(t, err)
	defer tr.Close()

	resp, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "ListTasks",
		&orchestratorclient.ListTasksRequest{
			TenantID: "t1", ProjectID: "proj-1", SourceKind: "operator", SourceRef: "owner/repo#12",
		})
	require.NoError(t, err)

	out, ok := resp.(*orchestratorclient.ListTasksResponse)
	require.True(t, ok, "transport returned %T", resp)
	assert.Empty(t, out.Tasks)

	query := <-gotQuery
	assert.Equal(t, "operator", query.Get("source_kind"))
	assert.Equal(t, "owner/repo#12", query.Get("source_ref"))
	assert.Equal(t, "proj-1", query.Get("project_id"))
}

// TestRequest_ListTasksWithoutSourceOverHTTP3 proves an unset predicate is
// not sent at all, so a plain listing is not narrowed to an empty source.
func TestRequest_ListTasksWithoutSourceOverHTTP3(t *testing.T) {
	gotQuery := make(chan url.Values, 1)

	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery <- r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"tasks": []interface{}{}})
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(addr))
	require.NoError(t, err)
	defer tr.Close()

	_, err = tr.Request(context.Background(), "quicbaas.Orchestrator", "ListTasks",
		&orchestratorclient.ListTasksRequest{TenantID: "t1"})
	require.NoError(t, err)

	query := <-gotQuery
	assert.False(t, query.Has("source_kind"), "source_kind sent for a plain listing")
	assert.False(t, query.Has("source_ref"), "source_ref sent for a plain listing")
}
