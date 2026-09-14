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

package quic

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestRequest_GetSessionConversationOverHTTP3 drives the session-conversation
// verb over a real HTTP/3 server. QUIC is the REST surface on HTTP/3, so this
// proves the SDK method reaches the same /history route the REST transport
// builds and decodes into the SDK type rather than a bare map.
func TestRequest_GetSessionConversationOverHTTP3(t *testing.T) {
	gotPath := make(chan string, 1)
	gotTenant := make(chan string, 1)

	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath <- r.URL.Path
		gotTenant <- r.URL.Query().Get("tenant_id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"session_id":"s1","tenant_id":"automatethethings","next_cursor":7,
			"turns":[{"sequence":1,"session_id":"s1","role":"assistant","text":"hello",
			          "usage":{"output_tokens":8}}]}`))
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(addr))
	require.NoError(t, err)
	defer tr.Close()

	resp, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "GetSessionConversation",
		&orchestratorclient.GetSessionConversationRequest{TenantID: "automatethethings", SessionID: "s1"})
	require.NoError(t, err)

	page, ok := resp.(*orchestratorclient.GetSessionConversationResponse)
	require.True(t, ok, "transport returned %T", resp)
	require.Len(t, page.Turns, 1)
	assert.Equal(t, orchestratorclient.SessionTurnRoleAssistant, page.Turns[0].Role)
	require.NotNil(t, page.Turns[0].Usage)
	assert.Equal(t, int64(8), page.Turns[0].Usage.OutputTokens)
	assert.Equal(t, uint64(7), page.NextCursor)

	assert.Equal(t, "/api/v1/orchestrator/sessions/s1/history", <-gotPath)
	assert.Equal(t, "automatethethings", <-gotTenant)
}

// TestRequest_GetSessionConversationRefusalOverHTTP3 proves a server refusal
// survives the QUIC hop as an error, never as a decoded page that reads as a
// session which said nothing.
func TestRequest_GetSessionConversationRefusalOverHTTP3(t *testing.T) {
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"permission denied"}`))
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(addr))
	require.NoError(t, err)
	defer tr.Close()

	_, err = tr.Request(context.Background(), "quicbaas.Orchestrator", "GetSessionConversation",
		&orchestratorclient.GetSessionConversationRequest{TenantID: "automatethethings", SessionID: "s1"})
	require.Error(t, err)
}
