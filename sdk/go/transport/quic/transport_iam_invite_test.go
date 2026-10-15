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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// inviteCall is one request the invite test server received.
type inviteCall struct {
	method string
	path   string
	body   map[string]interface{}
}

// inviteServer answers every request with reply and records what it got.
// The activation routes carry the tenant segment
// /api/v1/iam/{tenant_id}/invites/{token}; a client that drops it reaches no
// route on the real server, so the recorded path is what the tests pin.
func inviteServer(t *testing.T, calls chan<- inviteCall, reply map[string]interface{}) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := inviteCall{method: r.Method, path: r.URL.Path}
		if r.ContentLength > 0 {
			if err := json.NewDecoder(r.Body).Decode(&call.body); err != nil {
				t.Errorf("decode body: %v", err)
			}
		}
		calls <- call
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(reply); err != nil {
			t.Errorf("encode reply: %v", err)
		}
	})
}

// TestInviteOverHTTP3 verifies quicbaas.IAM.Invite reaches the per-tenant
// invite routes over QUIC, which carries the REST surface on HTTP/3.
func TestInviteOverHTTP3(t *testing.T) {
	expires := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	base := "/api/v1/iam/acme/invites/one-time-token"

	t.Run("GetInvite", func(t *testing.T) {
		calls := make(chan inviteCall, 1)
		addr, shutdown := startTestHTTP3Server(t, inviteServer(t, calls, map[string]interface{}{
			"user_id":      "7",
			"email":        "admin@acme.example",
			"auth_methods": []string{"fido2", "piv"},
			"expires_at":   expires.Format(time.RFC3339),
		}))
		defer shutdown()

		tr, err := New(testTransportConfig(t, addr))
		require.NoError(t, err)
		defer tr.Close()

		meta, err := iam.NewClient(tr).Invite().GetInvite(context.Background(), "acme", "one-time-token")
		require.NoError(t, err)
		assert.Equal(t, inviteCall{method: http.MethodGet, path: base}, <-calls)
		assert.Equal(t, &iam.InviteMetadata{
			UserID:      "7",
			Email:       "admin@acme.example",
			AuthMethods: []string{"fido2", "piv"},
			ExpiresAt:   expires,
		}, meta)
	})

	t.Run("ActivatePIV", func(t *testing.T) {
		calls := make(chan inviteCall, 1)
		addr, shutdown := startTestHTTP3Server(t, inviteServer(t, calls, map[string]interface{}{
			"user_id": "7",
			"message": "activated",
		}))
		defer shutdown()

		tr, err := New(testTransportConfig(t, addr))
		require.NoError(t, err)
		defer tr.Close()

		resp, err := iam.NewClient(tr).Invite().ActivatePIV(context.Background(), "acme", "one-time-token", "CERT-PEM")
		require.NoError(t, err)
		assert.Equal(t, inviteCall{
			method: http.MethodPost,
			path:   base + "/activate/piv",
			body:   map[string]interface{}{"cert_pem": "CERT-PEM"},
		}, <-calls)
		assert.Equal(t, &iam.ActivationResponse{UserID: "7", Message: "activated"}, resp)
	})
}

// TestInviteOverHTTP3_UnusableInvite verifies the server's refusal of a spent
// or unknown token comes back as a typed error, not a decoded empty result.
func TestInviteOverHTTP3_UnusableInvite(t *testing.T) {
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		if _, err := w.Write([]byte(`{"error": "invite not found"}`)); err != nil {
			t.Errorf("write reply: %v", err)
		}
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()

	_, err = iam.NewClient(tr).Invite().ActivatePIV(context.Background(), "acme", "spent-token", "CERT-PEM")
	assert.True(t, errors.IsNotFound(err), "expected a typed not-found error, got %v", err)
}

// TestInviteOverHTTP3_MissingTenant verifies the transport itself refuses a
// request without the tenant the route requires, before anything is sent: the
// tenant-less path it would otherwise build is served by no route.
func TestInviteOverHTTP3_MissingTenant(t *testing.T) {
	tr, err := New(testTransportConfig(t, "127.0.0.1:1"))
	require.NoError(t, err)
	defer tr.Close()

	_, err = tr.Request(context.Background(), "quicbaas.IAM.Invite", "GetInvite",
		map[string]interface{}{"token": "one-time-token"})
	assert.True(t, errors.IsInvalidInput(err), "expected a typed invalid-input error, got %v", err)
}
