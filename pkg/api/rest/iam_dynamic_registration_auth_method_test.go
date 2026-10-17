//go:build rest

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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// dcrRegisterWithMethod registers a client_credentials client dynamically,
// asking for method ("" asks for none), and returns the recorded response.
func dcrRegisterWithMethod(t *testing.T, handler *IAMHandler, method string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"grant_types":["client_credentials"],"client_name":"DCR Method"`
	if method != "" {
		body += `,"token_endpoint_auth_method":"` + method + `"`
	}
	w := httptest.NewRecorder()
	handler.RegisterDynamicClient(w, dcrRegisterRequest(body+"}"))
	return w
}

// dcrStoredApps lists the applications stored in the DCR tenant.
func dcrStoredApps(t *testing.T, handler *IAMHandler) []*iam.Application {
	t.Helper()
	svc, ok := handler.server.service.(*appMockService)
	require.True(t, ok)
	apps, err := svc.manager.ListApps(context.Background(), appsTenant, iam.ListOptions{})
	require.NoError(t, err)
	return apps
}

// TestRegisterDynamicClient_StoresAuthMethod proves the RFC 7591 response
// reports the token_endpoint_auth_method that was stored: a later RFC 7592
// read returns the same method, and no method means client_secret_basic.
func TestRegisterDynamicClient_StoresAuthMethod(t *testing.T) {
	cases := map[string]string{
		"default":             "",
		"client_secret_basic": iam.AuthMethodClientSecretBasic,
		"client_secret_post":  iam.AuthMethodClientSecretPost,
		"tls_client_auth":     iam.AuthMethodTLSClientAuth,
	}
	for name, method := range cases {
		t.Run(name, func(t *testing.T) {
			handler, cleanup := setupDCRLifecycleHandler(t)
			defer cleanup()

			w := dcrRegisterWithMethod(t, handler, method)
			require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
			var registered DynamicClientResponse
			require.NoError(t, json.NewDecoder(w.Body).Decode(&registered))

			want := method
			if want == "" {
				want = iam.AuthMethodClientSecretBasic
			}
			assert.Equal(t, want, registered.TokenEndpointAuthMethod)

			w = httptest.NewRecorder()
			handler.GetDynamicClient(w, dcrManagementRequest(http.MethodGet,
				registered.ClientID, registered.RegistrationAccessToken, nil))
			require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
			var read DynamicClientResponse
			require.NoError(t, json.NewDecoder(w.Body).Decode(&read))
			assert.Equal(t, want, read.TokenEndpointAuthMethod)
		})
	}
}

// TestRegisterDynamicClient_RefusesUnsupportedAuthMethod proves a method the
// register validator refuses yields RFC 7591 invalid_client_metadata (400) and
// stores nothing. self_signed_tls_client_auth is refused because a
// registration carries no certificate thumbprint to pin.
func TestRegisterDynamicClient_RefusesUnsupportedAuthMethod(t *testing.T) {
	methods := []string{
		"invalid_method",
		"none",
		"private_key_jwt",
		iam.AuthMethodSelfSignedTLSClientAuth,
	}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			handler, cleanup := setupDCRLifecycleHandler(t)
			defer cleanup()

			w := dcrRegisterWithMethod(t, handler, method)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			var dcrErr DCRError
			require.NoError(t, json.NewDecoder(w.Body).Decode(&dcrErr))
			assert.Equal(t, DCRErrorInvalidClientMetadata, dcrErr.Error)
			assert.Empty(t, dcrStoredApps(t, handler))
		})
	}
}
