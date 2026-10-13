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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// introspectServer answers POST /oauth2/introspect the way the quicbaas
// server does: a form-encoded token, 400 invalid_request when it is missing,
// {"active":false} for "garbage" and an attested session otherwise.
func introspectServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != authIntrospectPath {
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
			reply = map[string]interface{}{"error": "invalid_request", "error_description": "token is required"}
		case "garbage":
			reply = map[string]interface{}{"active": false}
		default:
			reply = map[string]interface{}{
				"active":        true,
				"sub":           "user-1",
				"username":      "alice",
				"exp":           3000,
				"aud":           []string{"quicbaas"},
				"attest_level":  "hardware",
				"attest_aaguid": "cb69481e8ff7403993ec0a2729a154a8",
			}
		}
		if err := json.NewEncoder(w).Encode(reply); err != nil {
			t.Errorf("encode reply: %v", err)
		}
	}))
}

func introspectOverREST(t *testing.T, token string) (interface{}, error) {
	t.Helper()
	server := introspectServer(t)
	t.Cleanup(server.Close)
	tr := newTestTransport(t, server)
	t.Cleanup(func() { tr.Close() })
	return tr.Request(context.Background(), authServicePrefix, "IntrospectToken",
		&service.IntrospectTokenRequest{Token: token})
}

func TestAuthRESTHandler_IntrospectToken_Active(t *testing.T) {
	resp, err := introspectOverREST(t, "live")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := resp.(*service.TokenIntrospection)
	if !ok {
		t.Fatalf("expected *service.TokenIntrospection, got %T", resp)
	}
	if !got.Active || got.Subject != "user-1" || got.AttestLevel != "hardware" {
		t.Errorf("active introspection lost claims: %+v", got)
	}
	if got.AttestAAGUID != "cb69481e8ff7403993ec0a2729a154a8" {
		t.Errorf("expected AAGUID to round-trip, got %q", got.AttestAAGUID)
	}
	if !got.ExpiresAt.Equal(time.Unix(3000, 0)) || !got.IssuedAt.IsZero() {
		t.Errorf("expected exp 3000 and a zero iat, got %v and %v", got.ExpiresAt, got.IssuedAt)
	}
	if !reflect.DeepEqual(got.Audience, []string{"quicbaas"}) {
		t.Errorf("expected audience [quicbaas], got %v", got.Audience)
	}
}

func TestAuthRESTHandler_IntrospectToken_InactiveCarriesOnlyActive(t *testing.T) {
	resp, err := introspectOverREST(t, "garbage")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(resp, &service.TokenIntrospection{Active: false}) {
		t.Errorf("inactive introspection must carry only active=false, got %+v", resp)
	}
}

func TestAuthRESTHandler_IntrospectToken_MissingTokenIsInvalidInput(t *testing.T) {
	_, err := introspectOverREST(t, "")
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid-input error, got %v", err)
	}
}

func TestAuthRESTHandler_IntrospectToken_BadRequestType(t *testing.T) {
	h := newAuthRESTHandler(nil)
	_, err := h.handleRequest(context.Background(), authServicePrefix, "IntrospectToken", "wrong")
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid-input error for wrong request type, got %v", err)
	}
}

func TestAuthRESTHandler_IntrospectToken_UnauthenticatedCallerRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		if _, err := w.Write([]byte(`{"error":"invalid_client"}`)); err != nil {
			t.Errorf("write reply: %v", err)
		}
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	resp, err := tr.Request(context.Background(), authServicePrefix, "IntrospectToken",
		&service.IntrospectTokenRequest{Token: "live"})
	if !errors.IsUnauthorized(err) {
		t.Errorf("expected unauthorized error, got %v", err)
	}
	if resp != nil {
		t.Errorf("a refused introspection must not return a reply, got %+v", resp)
	}
}
