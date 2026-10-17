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
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// appAuthMethodServer holds one app's token_endpoint_auth_method behind the
// app routes: a write applies the key when the body carries it, every reply
// returns it. present records whether each write body carried the key.
func appAuthMethodServer(t *testing.T, method *string, present *[]bool) http.Handler {
	t.Helper()
	var mu sync.Mutex
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
			}
			got, ok := body["token_endpoint_auth_method"].(string)
			*present = append(*present, ok)
			if ok {
				*method = got
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"client_id":                  "c1",
			"token_endpoint_auth_method": *method,
		}); err != nil {
			t.Errorf("encode reply: %v", err)
		}
	})
}

// TestAppUpdateToClientSecretBasicOverHTTP3 proves an update from
// client_secret_post to client_secret_basic reaches the server over HTTP/3 and
// reads back as client_secret_basic, and an update that omits the method
// leaves it unchanged (#1473).
func TestAppUpdateToClientSecretBasicOverHTTP3(t *testing.T) {
	method := "client_secret_post"
	var present []bool
	addr, shutdown := startTestHTTP3Server(t, appAuthMethodServer(t, &method, &present))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()
	apps := iam.NewAppClient(tr)
	ctx := context.Background()

	basic := iam.TokenEndpointAuthClientSecretBasic
	_, err = apps.Update(ctx, "t1", "c1", &iam.UpdateAppRequest{TokenEndpointAuthMethod: &basic})
	require.NoError(t, err)
	_, err = apps.Update(ctx, "t1", "c1", &iam.UpdateAppRequest{Name: "renamed"})
	require.NoError(t, err)

	app, err := apps.Get(ctx, "t1", "c1")
	require.NoError(t, err)
	assert.Equal(t, iam.TokenEndpointAuthClientSecretBasic, app.TokenEndpointAuthMethod)
	assert.Equal(t, []bool{true, false}, present, "method key present per update")
}

// TestAppRegisterClientSecretPostOverHTTP3 proves Register sends
// client_secret_post over QUIC and Get reads it back (#1473).
func TestAppRegisterClientSecretPostOverHTTP3(t *testing.T) {
	var method string
	var present []bool
	addr, shutdown := startTestHTTP3Server(t, appAuthMethodServer(t, &method, &present))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()
	apps := iam.NewAppClient(tr)
	ctx := context.Background()

	_, err = apps.Register(ctx, "t1", &iam.RegisterAppRequest{
		Name:                    "app",
		RedirectURIs:            []string{"https://app.example/cb"},
		GrantTypes:              []iam.GrantType{iam.GrantTypeAuthorizationCode},
		TokenEndpointAuthMethod: iam.TokenEndpointAuthClientSecretPost,
	})
	require.NoError(t, err)
	assert.Equal(t, []bool{true}, present, "register body carries the method")
	assert.Equal(t, "client_secret_post", method)

	app, err := apps.Get(ctx, "t1", "c1")
	require.NoError(t, err)
	assert.Equal(t, iam.TokenEndpointAuthClientSecretPost, app.TokenEndpointAuthMethod)
}

// TestAppUpdateRefusalOverHTTP3 proves a server refusal of the method reaches
// the caller as an error.
func TestAppUpdateRefusalOverHTTP3(t *testing.T) {
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"invalid token endpoint auth method"}`, http.StatusBadRequest)
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()

	unknown := iam.TokenEndpointAuthMethod(99)
	_, err = iam.NewAppClient(tr).Update(context.Background(), "t1", "c1",
		&iam.UpdateAppRequest{TokenEndpointAuthMethod: &unknown})
	require.Error(t, err)
}
