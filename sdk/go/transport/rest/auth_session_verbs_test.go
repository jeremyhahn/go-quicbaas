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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// The session verbs (logout, refresh, validate) live under quicbaas's own
// /api/v1/iam/ namespace. /api/v1/auth/ is the composed xkms node's, so a
// client that calls it there redeems against the wrong token store
// (go-quicbaas#259).

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized","message":"invalid bearer"}`))
}

func TestAuthRESTHandler_Logout_UnauthorizedIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeUnauthorized(w)
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), authServicePrefix, "Logout",
		map[string]interface{}{"access_token": "revoked"})
	if err == nil {
		t.Fatal("expected an error for a 401 logout, got nil")
	}
}

func TestAuthRESTHandler_ValidateToken_InvalidBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"valid":false}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), authServicePrefix, "ValidateToken",
		map[string]interface{}{"access_token": "dead"})
	if err != nil {
		t.Fatalf("expected nil error for 200 {\"valid\":false}, got %v", err)
	}
	claims, ok := result.(*service.TokenClaims)
	if !ok {
		t.Fatalf("expected *service.TokenClaims, got %T", result)
	}
	if claims.Valid {
		t.Error("a 200 {\"valid\":false} must report Valid == false")
	}
}

func TestAuthRESTHandler_ValidateToken_UnauthorizedIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeUnauthorized(w)
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), authServicePrefix, "ValidateToken",
		map[string]interface{}{"access_token": "forged"})
	if err == nil {
		t.Fatal("expected an error for a 401 validate, got nil")
	}
}

// TestAuthRESTHandler_ValidateToken_KeepsCallerAuthorization guards against
// ValidateToken rewriting the transport's shared headers: the validated token
// goes on that request only, and the caller's own bearer survives it.
func TestAuthRESTHandler_ValidateToken_KeepsCallerAuthorization(t *testing.T) {
	seen := make(map[string]string)
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.URL.Path] = r.Header.Get("Authorization")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"valid":true}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()
	tr.SetHeader("Authorization", "Bearer caller")

	ctx := context.Background()
	if _, err := tr.Request(ctx, authServicePrefix, "ValidateToken",
		map[string]interface{}{"access_token": "other"}); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	var next map[string]interface{}
	if err := tr.doJSONRequest(ctx, http.MethodGet, "/next", nil, &next); err != nil {
		t.Fatalf("follow-up request failed: %v", err)
	}

	if got := seen[authValidatePath]; got != "Bearer other" {
		t.Errorf("validate request: expected 'Bearer other', got %q", got)
	}
	if got := seen["/next"]; got != "Bearer caller" {
		t.Errorf("next request: expected caller's 'Bearer caller', got %q", got)
	}
}

func TestAuthRESTHandler_ValidateToken_ConcurrentTokensIsolated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"valid":true,"user_id":%q}`, token)
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	const workers = 32
	var wg sync.WaitGroup
	failures := make(chan string, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			result, err := tr.Request(context.Background(), authServicePrefix, "ValidateToken",
				map[string]interface{}{"access_token": token})
			if err != nil {
				failures <- fmt.Sprintf("token %q: %v", token, err)
				return
			}
			claims, ok := result.(*service.TokenClaims)
			if !ok {
				failures <- fmt.Sprintf("token %q: expected *service.TokenClaims, got %T", token, result)
				return
			}
			if claims.UserID != token {
				failures <- fmt.Sprintf("token %q: server saw bearer %q", token, claims.UserID)
			}
		}(fmt.Sprintf("token-%d", i))
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
}

func TestMapAuthMethod_SessionVerbsNotUnderXKMSAuth(t *testing.T) {
	tr := &Transport{}
	for _, method := range []string{"Logout", "RefreshToken", "ValidateToken"} {
		path, httpMethod := tr.mapAuthMethod(method)
		if !strings.HasPrefix(path, "/api/v1/iam/") {
			t.Errorf("%s: expected an /api/v1/iam/ route, got %s", method, path)
		}
		if strings.HasPrefix(path, "/api/v1/auth/") {
			t.Errorf("%s: must not map under xkms's /api/v1/auth/, got %s", method, path)
		}
		if httpMethod != http.MethodPost {
			t.Errorf("%s: expected POST, got %s", method, httpMethod)
		}
	}
}
