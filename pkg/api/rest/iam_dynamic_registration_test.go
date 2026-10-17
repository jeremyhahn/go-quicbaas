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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// dcrAnonymousTenant is the non-system landing tenant used by the tests that
// exercise the anonymous-registration opt-in.
const dcrAnonymousTenant = "tenant-dcr-anon"

// setupDCRHandler builds an IAM handler whose service exposes NO AppManager
// (so every request that clears the authentication gate stops at the
// "not available" branch) with the supplied Dynamic Client Registration policy.
// It is the harness for the gate + request-validation assertions; the
// end-to-end lifecycle tests use setupAppHandler, which wires a real manager.
func setupDCRHandler(t *testing.T, cfg config.IAMDynamicClientRegistrationConfig) (*IAMHandler, func()) {
	t.Helper()
	handler, server, cleanup := setupIAMHandler(t)
	server.config.IAMDynamicClientRegistration = cfg
	return handler, cleanup
}

// setupOpenDCRHandler is setupDCRHandler with anonymous registration enabled
// into a non-system tenant — the configuration under which the RFC 7591
// request-validation rules are reachable without a token.
func setupOpenDCRHandler(t *testing.T) (*IAMHandler, func()) {
	t.Helper()
	return setupDCRHandler(t, config.IAMDynamicClientRegistrationConfig{
		Enabled:           true,
		AnonymousTenantID: dcrAnonymousTenant,
	})
}

// dcrRegisterRequest builds a POST /oauth2/register request with a JSON body.
func dcrRegisterRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// TestRegisterDynamicClient tests the RegisterDynamicClient handler.
func TestRegisterDynamicClient(t *testing.T) {
	t.Run("invalid JSON body", func(t *testing.T) {
		handler, cleanup := setupOpenDCRHandler(t)
		defer cleanup()

		w := httptest.NewRecorder()
		handler.RegisterDynamicClient(w, dcrRegisterRequest("invalid json"))

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON request body")
	})

	t.Run("missing redirect_uris for authorization_code grant", func(t *testing.T) {
		handler, cleanup := setupOpenDCRHandler(t)
		defer cleanup()

		w := httptest.NewRecorder()
		handler.RegisterDynamicClient(w, dcrRegisterRequest(`{"grant_types":["authorization_code"],"client_name":"Test Client"}`))

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "redirect_uris required for authorization_code")
	})

	t.Run("invalid redirect_uri - empty string in array", func(t *testing.T) {
		handler, cleanup := setupOpenDCRHandler(t)
		defer cleanup()

		w := httptest.NewRecorder()
		handler.RegisterDynamicClient(w, dcrRegisterRequest(`{"grant_types":["authorization_code"],"redirect_uris":[""]}`))

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid redirect_uri")
	})

	t.Run("unsupported grant_type", func(t *testing.T) {
		handler, cleanup := setupOpenDCRHandler(t)
		defer cleanup()

		w := httptest.NewRecorder()
		handler.RegisterDynamicClient(w, dcrRegisterRequest(`{"grant_types":["invalid_grant"],"redirect_uris":["https://example.com/callback"]}`))

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "unsupported grant_type")
	})

	t.Run("unsupported response_type", func(t *testing.T) {
		handler, cleanup := setupOpenDCRHandler(t)
		defer cleanup()

		w := httptest.NewRecorder()
		handler.RegisterDynamicClient(w, dcrRegisterRequest(`{"grant_types":["client_credentials"],"response_types":["invalid_response"]}`))

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "unsupported response_type")
	})

	t.Run("dynamic registration not available", func(t *testing.T) {
		handler, cleanup := setupOpenDCRHandler(t)
		defer cleanup()

		w := httptest.NewRecorder()
		handler.RegisterDynamicClient(w, dcrRegisterRequest(`{"grant_types":["client_credentials"],"client_name":"Test Client"}`))

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "dynamic registration not available")
	})

	// Each accepted redirect-URI form must clear validation and reach the
	// (unwired) app manager rather than being rejected as malformed.
	acceptedRedirects := map[string]string{
		"HTTPS":          "https://example.com/callback",
		"localhost HTTP": "http://localhost:8080/callback",
		"127.0.0.1 HTTP": "http://127.0.0.1:8080/callback",
		"IPv6 localhost": "http://[::1]:8080/callback",
		"custom scheme":  "com.example.app:/callback",
	}
	for name, uri := range acceptedRedirects {
		t.Run("valid redirect URI - "+name, func(t *testing.T) {
			handler, cleanup := setupOpenDCRHandler(t)
			defer cleanup()

			w := httptest.NewRecorder()
			handler.RegisterDynamicClient(w, dcrRegisterRequest(`{"grant_types":["authorization_code"],"redirect_uris":["`+uri+`"]}`))

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Contains(t, w.Body.String(), "dynamic registration not available")
		})
	}

	t.Run("default grant type requires redirect_uris", func(t *testing.T) {
		handler, cleanup := setupOpenDCRHandler(t)
		defer cleanup()

		// Empty grant_types defaults to authorization_code which requires redirect_uris
		w := httptest.NewRecorder()
		handler.RegisterDynamicClient(w, dcrRegisterRequest(`{"client_name":"Test Client"}`))

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "redirect_uris required for authorization_code")
	})

	t.Run("valid grant types - refresh_token", func(t *testing.T) {
		handler, cleanup := setupOpenDCRHandler(t)
		defer cleanup()

		w := httptest.NewRecorder()
		handler.RegisterDynamicClient(w, dcrRegisterRequest(`{"grant_types":["refresh_token"],"client_name":"Test Client"}`))

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "dynamic registration not available")
	})

	t.Run("device code grant type", func(t *testing.T) {
		handler, cleanup := setupOpenDCRHandler(t)
		defer cleanup()

		w := httptest.NewRecorder()
		handler.RegisterDynamicClient(w, dcrRegisterRequest(`{"grant_types":["urn:ietf:params:oauth:grant-type:device_code"],"client_name":"Test Client"}`))

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "dynamic registration not available")
	})

	t.Run("all valid response types", func(t *testing.T) {
		handler, cleanup := setupOpenDCRHandler(t)
		defer cleanup()

		w := httptest.NewRecorder()
		handler.RegisterDynamicClient(w, dcrRegisterRequest(`{"grant_types":["client_credentials"],"response_types":["code","token","id_token"]}`))

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "dynamic registration not available")
	})

	t.Run("all valid token auth methods", func(t *testing.T) {
		validMethods := []string{"client_secret_basic", "client_secret_post", "tls_client_auth"}
		handler, cleanup := setupOpenDCRHandler(t)
		defer cleanup()

		for _, method := range validMethods {
			w := httptest.NewRecorder()
			handler.RegisterDynamicClient(w, dcrRegisterRequest(`{"grant_types":["client_credentials"],"token_endpoint_auth_method":"`+method+`"}`))

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Contains(t, w.Body.String(), "dynamic registration not available")
		}
	})
}

// TestGetDynamicClient tests the GetDynamicClient handler's gate.
func TestGetDynamicClient(t *testing.T) {
	t.Run("missing client_id", func(t *testing.T) {
		handler, cleanup := setupOpenDCRHandler(t)
		defer cleanup()

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("client_id", "")

		req := httptest.NewRequest(http.MethodGet, "/oauth2/register/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		handler.GetDynamicClient(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "client_id required")
	})

	// Every malformed-credential shape must produce the SAME opaque 401: the
	// endpoint must not describe which part of the credential was wrong.
	malformedAuth := map[string]string{
		"missing authorization header":      "",
		"invalid authorization header":      "InvalidFormat",
		"non-bearer authorization scheme":   "Basic YWJjOmRlZg==",
		"bearer with empty token":           "Bearer ",
		"bearer with whitespace-only token": "Bearer    ",
	}
	for name, header := range malformedAuth {
		t.Run(name, func(t *testing.T) {
			handler, cleanup := setupOpenDCRHandler(t)
			defer cleanup()

			req := httptest.NewRequest(http.MethodGet, "/oauth2/register/client-123", nil)
			if header != "" {
				req.Header.Set("Authorization", header)
			}
			req = withChiParams(req, map[string]string{"client_id": "client-123"})
			w := httptest.NewRecorder()

			handler.GetDynamicClient(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), DCRErrorInvalidToken)
			assert.Contains(t, w.Body.String(), dcrInvalidTokenDescription)
		})
	}

	t.Run("client management not available", func(t *testing.T) {
		handler, cleanup := setupOpenDCRHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/oauth2/register/client-123", nil)
		req.Header.Set("Authorization", "Bearer some-token")
		req = withChiParams(req, map[string]string{"client_id": "client-123"})
		w := httptest.NewRecorder()

		handler.GetDynamicClient(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "client management not available")
	})
}

// TestUpdateDynamicClient tests the UpdateDynamicClient handler's gate.
func TestUpdateDynamicClient(t *testing.T) {
	t.Run("missing client_id", func(t *testing.T) {
		handler, cleanup := setupOpenDCRHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPut, "/oauth2/register/", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req = withChiParams(req, map[string]string{"client_id": ""})
		w := httptest.NewRecorder()

		handler.UpdateDynamicClient(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "client_id required")
	})

	t.Run("missing authorization header", func(t *testing.T) {
		handler, cleanup := setupOpenDCRHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPut, "/oauth2/register/client-123", strings.NewReader(`{"grant_types":["client_credentials"]}`))
		req.Header.Set("Content-Type", "application/json")
		req = withChiParams(req, map[string]string{"client_id": "client-123"})
		w := httptest.NewRecorder()

		handler.UpdateDynamicClient(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), dcrInvalidTokenDescription)
	})

	// An unauthorized caller must be refused BEFORE the body is looked at, so a
	// malformed body from an anonymous caller is still a 401 (not a 400 that
	// would confirm the endpoint parsed anything for them).
	t.Run("invalid JSON body without credential is still 401", func(t *testing.T) {
		handler, cleanup := setupOpenDCRHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPut, "/oauth2/register/client-123", strings.NewReader("invalid json"))
		req.Header.Set("Content-Type", "application/json")
		req = withChiParams(req, map[string]string{"client_id": "client-123"})
		w := httptest.NewRecorder()

		handler.UpdateDynamicClient(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("client management not available", func(t *testing.T) {
		handler, cleanup := setupOpenDCRHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPut, "/oauth2/register/client-123", strings.NewReader(`{"grant_types":["client_credentials"]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer some-token")
		req = withChiParams(req, map[string]string{"client_id": "client-123"})
		w := httptest.NewRecorder()

		handler.UpdateDynamicClient(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "client management not available")
	})
}

// TestDeleteDynamicClient tests the DeleteDynamicClient handler's gate.
func TestDeleteDynamicClient(t *testing.T) {
	t.Run("missing client_id", func(t *testing.T) {
		handler, cleanup := setupOpenDCRHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodDelete, "/oauth2/register/", nil)
		req = withChiParams(req, map[string]string{"client_id": ""})
		w := httptest.NewRecorder()

		handler.DeleteDynamicClient(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "client_id required")
	})

	t.Run("missing authorization header", func(t *testing.T) {
		handler, cleanup := setupOpenDCRHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodDelete, "/oauth2/register/client-123", nil)
		req = withChiParams(req, map[string]string{"client_id": "client-123"})
		w := httptest.NewRecorder()

		handler.DeleteDynamicClient(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), dcrInvalidTokenDescription)
	})

	t.Run("client management not available", func(t *testing.T) {
		handler, cleanup := setupOpenDCRHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodDelete, "/oauth2/register/client-123", nil)
		req.Header.Set("Authorization", "Bearer some-token")
		req = withChiParams(req, map[string]string{"client_id": "client-123"})
		w := httptest.NewRecorder()

		handler.DeleteDynamicClient(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "client management not available")
	})
}

// TestIsValidRedirectURI tests the isValidRedirectURI helper function.
func TestIsValidRedirectURI(t *testing.T) {
	t.Run("empty URI", func(t *testing.T) {
		assert.False(t, isValidRedirectURI(""))
	})

	t.Run("HTTPS URI", func(t *testing.T) {
		assert.True(t, isValidRedirectURI("https://example.com/callback"))
	})

	t.Run("localhost HTTP", func(t *testing.T) {
		assert.True(t, isValidRedirectURI("http://localhost:8080/callback"))
	})

	t.Run("127.0.0.1 HTTP", func(t *testing.T) {
		assert.True(t, isValidRedirectURI("http://127.0.0.1:8080/callback"))
	})

	t.Run("IPv6 localhost", func(t *testing.T) {
		assert.True(t, isValidRedirectURI("http://[::1]:8080/callback"))
	})

	t.Run("custom scheme - native app", func(t *testing.T) {
		assert.True(t, isValidRedirectURI("com.example.app:/callback"))
	})

	t.Run("custom scheme - FTP is valid per RFC 8252", func(t *testing.T) {
		// The implementation allows any valid scheme (alphanumeric with dots/hyphens)
		// This matches RFC 8252 which allows custom URI schemes for native apps
		assert.True(t, isValidRedirectURI("ftp://example.com/callback"))
	})

	t.Run("HTTP non-localhost is valid as custom scheme", func(t *testing.T) {
		// http:// is a valid scheme per the custom scheme validation
		assert.True(t, isValidRedirectURI("http://example.com/callback"))
	})

	t.Run("invalid scheme with special chars", func(t *testing.T) {
		// @ breaks before reaching the colon
		assert.False(t, isValidRedirectURI("invalid@scheme:/callback"))
	})

	t.Run("colon at start is invalid", func(t *testing.T) {
		// Colon at position 0 means empty scheme name
		assert.False(t, isValidRedirectURI(":/callback"))
	})

	t.Run("scheme only without path", func(t *testing.T) {
		// "a:" has length 2, i (position of colon) = 1, len(uri) = 2
		// len(uri) > i+1 means 2 > 2 which is false
		assert.False(t, isValidRedirectURI("a:"))
	})

	t.Run("scheme with path", func(t *testing.T) {
		assert.True(t, isValidRedirectURI("myapp:/callback"))
	})
}

// TestValidateDCRRequest_RedirectURIFragment pins draft-ietf-oauth-v2-1
// §2.3: dynamic registration refuses a redirect URI with a fragment
// component, even an empty one, with invalid_redirect_uri.
func TestValidateDCRRequest_RedirectURIFragment(t *testing.T) {
	for _, uri := range []string{"https://a.example/cb#x", "https://a.example/cb#"} {
		t.Run(uri, func(t *testing.T) {
			assert.False(t, isValidRedirectURI(uri))
			err := validateDCRRequest(&DynamicClientRequest{RedirectURIs: []string{uri}})
			require.NotNil(t, err)
			assert.Equal(t, DCRErrorInvalidRedirectURI, err.code)
		})
	}

	t.Run("no fragment accepted", func(t *testing.T) {
		assert.True(t, isValidRedirectURI("https://a.example/cb"))
		assert.Nil(t, validateDCRRequest(&DynamicClientRequest{RedirectURIs: []string{"https://a.example/cb"}}))
	})
}

// TestIsValidDCRGrantType tests the isValidDCRGrantType helper function.
func TestIsValidDCRGrantType(t *testing.T) {
	t.Run("authorization_code", func(t *testing.T) {
		assert.True(t, isValidDCRGrantType("authorization_code"))
	})

	t.Run("client_credentials", func(t *testing.T) {
		assert.True(t, isValidDCRGrantType("client_credentials"))
	})

	t.Run("refresh_token", func(t *testing.T) {
		assert.True(t, isValidDCRGrantType("refresh_token"))
	})

	t.Run("device_code", func(t *testing.T) {
		assert.True(t, isValidDCRGrantType("urn:ietf:params:oauth:grant-type:device_code"))
	})

	t.Run("invalid grant type", func(t *testing.T) {
		assert.False(t, isValidDCRGrantType("invalid"))
	})

	t.Run("implicit grant not supported", func(t *testing.T) {
		assert.False(t, isValidDCRGrantType("implicit"))
	})

	t.Run("password grant not supported", func(t *testing.T) {
		assert.False(t, isValidDCRGrantType("password"))
	})
}

// TestIsValidDCRResponseType tests the isValidDCRResponseType helper function.
func TestIsValidDCRResponseType(t *testing.T) {
	t.Run("code", func(t *testing.T) {
		assert.True(t, isValidDCRResponseType("code"))
	})

	t.Run("token", func(t *testing.T) {
		assert.True(t, isValidDCRResponseType("token"))
	})

	t.Run("id_token", func(t *testing.T) {
		assert.True(t, isValidDCRResponseType("id_token"))
	})

	t.Run("invalid response type", func(t *testing.T) {
		assert.False(t, isValidDCRResponseType("invalid"))
	})

	t.Run("code token not supported as single value", func(t *testing.T) {
		assert.False(t, isValidDCRResponseType("code token"))
	})
}

// TestRespondDCRError tests the respondDCRError helper function.
func TestRespondDCRError(t *testing.T) {
	t.Run("basic error response", func(t *testing.T) {
		w := httptest.NewRecorder()
		respondDCRError(w, http.StatusBadRequest, "invalid_request", "test error")

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
		assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		assert.Equal(t, "no-cache", w.Header().Get("Pragma"))
		assert.Contains(t, w.Body.String(), "invalid_request")
		assert.Contains(t, w.Body.String(), "test error")
	})

	t.Run("internal server error", func(t *testing.T) {
		w := httptest.NewRecorder()
		respondDCRError(w, http.StatusInternalServerError, DCRErrorInvalidClientMetadata, "service unavailable")

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), DCRErrorInvalidClientMetadata)
	})

	t.Run("unauthorized error", func(t *testing.T) {
		w := httptest.NewRecorder()
		respondDCRError(w, http.StatusUnauthorized, DCRErrorInvalidToken, "token expired")

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "invalid_token")
	})
}

// TestDCRErrorConstants tests the DCR error constants.
func TestDCRErrorConstants(t *testing.T) {
	assert.Equal(t, "invalid_redirect_uri", DCRErrorInvalidRedirectURI)
	assert.Equal(t, "invalid_client_metadata", DCRErrorInvalidClientMetadata)
	assert.Equal(t, "invalid_software_statement", DCRErrorInvalidSoftwareStatement)
	assert.Equal(t, "unapproved_software_statement", DCRErrorUnapprovedSoftwareStatement)
	assert.Equal(t, "invalid_token", DCRErrorInvalidToken)
	assert.Equal(t, "access_denied", DCRErrorAccessDenied)
}
