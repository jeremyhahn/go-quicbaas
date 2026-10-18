//go:build rest

package rest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertInvalidClientMetadata checks an RFC 7591 §3.2.2 refusal.
func assertInvalidClientMetadata(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, DCRErrorInvalidClientMetadata, resp["error"])
}

func TestRegisterDynamicClient_ResponseTypesRefused(t *testing.T) {
	tests := []struct {
		name          string
		grantTypes    []string
		responseTypes []string
	}{
		{"unsupported response type", []string{"authorization_code"}, []string{"token"}},
		{"code without authorization_code", []string{"client_credentials"}, []string{"code"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, cleanup := setupDCRLifecycleHandler(t)
			defer cleanup()

			body, err := json.Marshal(DynamicClientRequest{
				ClientName:    "DCR App",
				RedirectURIs:  []string{"https://app.example/cb"},
				GrantTypes:    tt.grantTypes,
				ResponseTypes: tt.responseTypes,
			})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/oauth2/register", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handler.RegisterDynamicClient(w, req)
			assertInvalidClientMetadata(t, w)
		})
	}
}

// TestValidateDCRRequest_ResponseTypesOutsideSupportedList proves the DCR
// validation itself refuses response types discovery does not advertise.
// The handler has no app manager: a request that got past validation would
// answer 500 "dynamic registration not available", so a 400 shows the
// manager was never reached.
func TestValidateDCRRequest_ResponseTypesOutsideSupportedList(t *testing.T) {
	for _, rt := range []string{"token", "id_token"} {
		t.Run(rt, func(t *testing.T) {
			handler, cleanup := setupOpenDCRHandler(t)
			defer cleanup()

			w := httptest.NewRecorder()
			handler.RegisterDynamicClient(w, dcrRegisterRequest(
				`{"grant_types":["authorization_code"],"redirect_uris":["https://app.example/cb"],"response_types":["`+rt+`"]}`))
			assertInvalidClientMetadata(t, w)
		})
	}
}

// TestRegisterDynamicClient_CodeWithAuthorizationCodeRegisters keeps the
// supported pairing registering after the DCR check moved to the shared list.
func TestRegisterDynamicClient_CodeWithAuthorizationCodeRegisters(t *testing.T) {
	handler, cleanup := setupDCRLifecycleHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.RegisterDynamicClient(w, dcrRegisterRequest(
		`{"client_name":"DCR App","grant_types":["authorization_code"],"redirect_uris":["https://app.example/cb"],"response_types":["code"]}`))
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
}

func TestUpdateDynamicClient_ResponseTypesRefused(t *testing.T) {
	handler, cleanup := setupDCRLifecycleHandler(t)
	defer cleanup()

	clientID, token := registerDCRClient(t, handler)

	body, err := json.Marshal(DynamicClientRequest{
		ClientName:    "DCR App",
		RedirectURIs:  []string{"https://app.example/cb"},
		GrantTypes:    []string{"authorization_code"},
		ResponseTypes: []string{"id_token"},
	})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	handler.UpdateDynamicClient(w, dcrManagementRequest(http.MethodPut, clientID, token, body))
	assertInvalidClientMetadata(t, w)
}
