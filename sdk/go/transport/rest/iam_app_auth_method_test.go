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
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// authMethodServer is one app's token_endpoint_auth_method behind the REST
// app routes: register and update apply the key when the body carries it,
// get returns it. bodies records whether each write body carried the key.
type authMethodServer struct {
	mu     sync.Mutex
	method string
	bodies []bool
}

func (s *authMethodServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		method, ok := body["token_endpoint_auth_method"].(string)
		s.bodies = append(s.bodies, ok)
		if ok {
			s.method = method
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"client_id":                  "c1",
		"token_endpoint_auth_method": s.method,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// TestAppClient_UpdateToClientSecretBasic proves an update from
// client_secret_post to client_secret_basic reaches the server and reads back
// as client_secret_basic, and an update that omits the method leaves it alone
// (#1473).
func TestAppClient_UpdateToClientSecretBasic(t *testing.T) {
	fake := &authMethodServer{method: "client_secret_post"}
	server := httptest.NewServer(fake)
	defer server.Close()
	apps := iam.NewAppClient(newTestTransport(t, server))
	ctx := context.Background()

	basic := iam.TokenEndpointAuthClientSecretBasic
	if _, err := apps.Update(ctx, "t1", "c1", &iam.UpdateAppRequest{TokenEndpointAuthMethod: &basic}); err != nil {
		t.Fatalf("update to basic: %v", err)
	}
	if _, err := apps.Update(ctx, "t1", "c1", &iam.UpdateAppRequest{Name: "renamed"}); err != nil {
		t.Fatalf("update without method: %v", err)
	}

	app, err := apps.Get(ctx, "t1", "c1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if app.TokenEndpointAuthMethod != iam.TokenEndpointAuthClientSecretBasic {
		t.Fatalf("method = %v, want client_secret_basic", app.TokenEndpointAuthMethod)
	}
	if len(fake.bodies) != 2 || !fake.bodies[0] || fake.bodies[1] {
		t.Fatalf("method key present per update = %v, want [true false]", fake.bodies)
	}
}

// TestAppClient_RegisterSendsClientSecretBasic proves an explicit
// client_secret_basic on register is sent rather than dropped as the zero
// value.
func TestAppClient_RegisterSendsClientSecretBasic(t *testing.T) {
	fake := &authMethodServer{}
	server := httptest.NewServer(fake)
	defer server.Close()

	app, err := iam.NewAppClient(newTestTransport(t, server)).Register(context.Background(), "t1",
		&iam.RegisterAppRequest{
			Name:                    "app",
			RedirectURIs:            []string{"https://app.example/cb"},
			GrantTypes:              []iam.GrantType{iam.GrantTypeAuthorizationCode},
			TokenEndpointAuthMethod: iam.TokenEndpointAuthClientSecretBasic,
		})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if len(fake.bodies) != 1 || !fake.bodies[0] || fake.method != "client_secret_basic" {
		t.Fatalf("register body carried method %v as %q, want client_secret_basic", fake.bodies, fake.method)
	}
	if app.TokenEndpointAuthMethod != iam.TokenEndpointAuthClientSecretBasic {
		t.Fatalf("method = %v, want client_secret_basic", app.TokenEndpointAuthMethod)
	}
}

// TestAppClient_RegisterClientSecretPostReadsBack proves Register sends
// client_secret_post over REST and Get reads it back (#1473).
func TestAppClient_RegisterClientSecretPostReadsBack(t *testing.T) {
	fake := &authMethodServer{}
	server := httptest.NewServer(fake)
	defer server.Close()
	apps := iam.NewAppClient(newTestTransport(t, server))
	ctx := context.Background()

	_, err := apps.Register(ctx, "t1", &iam.RegisterAppRequest{
		Name:                    "app",
		RedirectURIs:            []string{"https://app.example/cb"},
		GrantTypes:              []iam.GrantType{iam.GrantTypeAuthorizationCode},
		TokenEndpointAuthMethod: iam.TokenEndpointAuthClientSecretPost,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if fake.method != "client_secret_post" {
		t.Fatalf("register body carried %q, want client_secret_post", fake.method)
	}
	app, err := apps.Get(ctx, "t1", "c1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if app.TokenEndpointAuthMethod != iam.TokenEndpointAuthClientSecretPost {
		t.Fatalf("method = %v, want client_secret_post", app.TokenEndpointAuthMethod)
	}
}

// TestAppClient_UpdateRefusalIsReturned proves a server refusal of the method
// reaches the caller as an error rather than a silent success.
func TestAppClient_UpdateRefusalIsReturned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"invalid token endpoint auth method"}`, http.StatusBadRequest)
	}))
	defer server.Close()

	unknown := iam.TokenEndpointAuthMethod(99)
	_, err := iam.NewAppClient(newTestTransport(t, server)).Update(context.Background(), "t1", "c1",
		&iam.UpdateAppRequest{TokenEndpointAuthMethod: &unknown})
	if err == nil {
		t.Fatal("expected the server's refusal to surface as an error")
	}
}
