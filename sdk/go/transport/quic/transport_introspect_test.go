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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/auth"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// introspectServer answers POST /oauth2/introspect over HTTP/3 as the
// quicbaas server does: 400 invalid_request for a missing token,
// {"active":false} for "garbage" and an attested session otherwise.
func introspectServer(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/oauth2/introspect" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		var reply map[string]interface{}
		switch r.PostForm.Get("token") {
		case "":
			w.WriteHeader(http.StatusBadRequest)
			reply = map[string]interface{}{"error": "invalid_request"}
		case "garbage":
			reply = map[string]interface{}{"active": false}
		default:
			reply = map[string]interface{}{
				"active":       true,
				"sub":          "user-1",
				"exp":          3000,
				"attest_level": "hardware",
			}
		}
		if err := json.NewEncoder(w).Encode(reply); err != nil {
			t.Errorf("encode reply: %v", err)
		}
	})
}

func newIntrospectTransport(t *testing.T) *Transport {
	t.Helper()
	addr, shutdown := startTestHTTP3Server(t, introspectServer(t))
	t.Cleanup(shutdown)
	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	t.Cleanup(func() { tr.Close() })
	return tr
}

func TestIntrospectTokenOverHTTP3(t *testing.T) {
	tr := newIntrospectTransport(t)
	client := auth.NewClient(tr)

	got, err := client.IntrospectToken(context.Background(), "live")
	require.NoError(t, err)
	assert.Equal(t, &service.TokenIntrospection{
		Active:      true,
		Subject:     "user-1",
		ExpiresAt:   time.Unix(3000, 0).UTC(),
		AttestLevel: "hardware",
	}, got)

	got, err = client.IntrospectToken(context.Background(), "garbage")
	require.NoError(t, err)
	assert.Equal(t, &service.TokenIntrospection{Active: false}, got)
}

func TestIntrospectTokenOverHTTP3_MissingToken(t *testing.T) {
	tr := newIntrospectTransport(t)

	_, err := tr.Request(context.Background(), "quicbaas.Auth", "IntrospectToken",
		&service.IntrospectTokenRequest{})
	assert.True(t, errors.IsInvalidInput(err), "expected invalid input, got %v", err)
}
