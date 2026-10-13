//go:build rest

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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// These tests cover go-quicbaas#335: a field the identity store refuses is the
// caller's mistake and answers 400, naming the field, not 500. The QUIC
// transport is this REST dialect over HTTP/3, so it inherits the same answer.

func TestREST_CreateUserRefusesAMalformedFieldWith400(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		field string
	}{
		{"malformed email", `{"email":"not-an-email","username":"bob"}`, "email"},
		{"missing username", `{"email":"bob@example.com"}`, "username"},
		{"email-shaped display name", `{"email":"bob@example.com","username":"bob","name":"bob@example.com"}`, "name"},
		{"unsupported auth method", `{"email":"bob@example.com","username":"bob","auth_methods":["password"]}`, "auth_methods"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, _, cleanup := setupUserReadHandler(t)
			defer cleanup()

			w := postUser(t, handler, "acme", tt.body)
			assert.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
			assert.Contains(t, w.Body.String(), tt.field, "the error does not name the field")
		})
	}
}

func TestREST_UpdateUserRefusesAnEmailShapedNameWith400(t *testing.T) {
	handler, _, cleanup := setupUserReadHandler(t)
	defer cleanup()

	w := postUser(t, handler, "acme", `{"email":"dana@acme.com","username":"dana","name":"Dana"}`)
	require.Equal(t, http.StatusCreated, w.Code, "CreateUser failed: %s", w.Body.String())
	var created map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	userID, ok := created["id"].(string)
	require.True(t, ok)
	require.NotEmpty(t, userID)

	w = putUser(t, handler, "acme", userID, `{"name":"dana@acme.com"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "name")
}

// TestREST_CreateUserStoreFailureStaysServerError proves the fix did not widen
// 400 to cover everything: a store failure that is not a validation error is
// still the server's fault.
func TestREST_CreateUserStoreFailureStaysServerError(t *testing.T) {
	h, cl := setupDeepHandler(t, &mockSvcIdentityStore{QuicBaaS: testutil.NewMockQuicBaaS(), storeErr: errDeepFail})
	defer cl()
	r := httptest.NewRequest(http.MethodPost, "/u", strings.NewReader(`{"email":"a@b.com","username":"user1","name":"User"}`))
	r = withChiParams(r, map[string]string{"tenant_id": "t1"})
	w := httptest.NewRecorder()
	h.CreateUser(w, r)
	assert.GreaterOrEqual(t, w.Code, http.StatusInternalServerError, "body: %s", w.Body.String())
}
