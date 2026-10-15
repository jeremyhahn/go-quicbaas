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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestRequest_DeleteTaskOverHTTP3 drives the task delete verb over a real
// HTTP/3 server. QUIC is the REST surface on HTTP/3, so this proves the SDK
// method reaches the same DELETE route the REST transport builds, and that the
// response decodes into the SDK type rather than a bare map.
func TestRequest_DeleteTaskOverHTTP3(t *testing.T) {
	gotMethod := make(chan string, 1)
	gotPath := make(chan string, 1)
	gotTenant := make(chan string, 1)

	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod <- r.Method
		gotPath <- r.URL.Path
		gotTenant <- r.URL.Query().Get("tenant_id")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"task_id": "task-1", "deleted": true})
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()

	resp, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "DeleteTask",
		&orchestratorclient.DeleteTaskRequest{TenantID: "automatethethings", TaskID: "task-1"})
	require.NoError(t, err)

	out, ok := resp.(*orchestratorclient.DeleteTaskResponse)
	require.True(t, ok, "transport returned %T", resp)
	assert.True(t, out.Deleted)
	assert.Equal(t, "task-1", out.TaskID)

	assert.Equal(t, http.MethodDelete, <-gotMethod)
	assert.Equal(t, "/api/v1/orchestrator/tasks/task-1", <-gotPath)
	assert.Equal(t, "automatethethings", <-gotTenant)
}

// TestRequest_DeleteTaskLeasedOverHTTP3 proves the refusal survives the QUIC
// hop: a 409 from the server is an error to the caller, never a decoded
// response that reads as a successful delete.
func TestRequest_DeleteTaskLeasedOverHTTP3(t *testing.T) {
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "task is leased by a running job"})
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()

	_, err = tr.Request(context.Background(), "quicbaas.Orchestrator", "DeleteTask",
		&orchestratorclient.DeleteTaskRequest{TenantID: "t1", TaskID: "task-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "leased")
}
