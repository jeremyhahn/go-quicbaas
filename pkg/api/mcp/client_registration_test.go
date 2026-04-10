//go:build mcp

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

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateRedirectURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
		errCode string
	}{
		// Valid HTTPS URIs
		{
			name:    "valid https uri",
			uri:     "https://example.com/callback",
			wantErr: false,
		},
		{
			name:    "valid https uri with port",
			uri:     "https://example.com:8443/callback",
			wantErr: false,
		},
		{
			name:    "valid https uri with path and query",
			uri:     "https://example.com/oauth/callback?state=123",
			wantErr: false,
		},

		// Valid HTTP localhost URIs
		{
			name:    "valid http localhost",
			uri:     "http://localhost/callback",
			wantErr: false,
		},
		{
			name:    "valid http localhost with port",
			uri:     "http://localhost:8080/callback",
			wantErr: false,
		},
		{
			name:    "valid http 127.0.0.1",
			uri:     "http://127.0.0.1/callback",
			wantErr: false,
		},
		{
			name:    "valid http 127.0.0.1 with port",
			uri:     "http://127.0.0.1:3000/callback",
			wantErr: false,
		},
		{
			name:    "valid http ipv6 localhost",
			uri:     "http://[::1]/callback",
			wantErr: false,
		},
		{
			name:    "valid http ipv6 localhost with port",
			uri:     "http://[::1]:8080/callback",
			wantErr: false,
		},

		// Valid custom schemes for native apps
		{
			name:    "valid custom scheme",
			uri:     "myapp://callback",
			wantErr: false,
		},
		{
			name:    "valid custom scheme with dots",
			uri:     "com.example.myapp://callback",
			wantErr: false,
		},

		// Invalid URIs
		{
			name:    "empty uri",
			uri:     "",
			wantErr: true,
			errCode: ErrorCodeInvalidRedirectURI,
		},
		{
			name:    "no scheme",
			uri:     "example.com/callback",
			wantErr: true,
			errCode: ErrorCodeInvalidRedirectURI,
		},
		{
			name:    "http non-localhost",
			uri:     "http://example.com/callback",
			wantErr: true,
			errCode: ErrorCodeInvalidRedirectURI,
		},
		{
			name:    "wildcard in uri",
			uri:     "https://example.com/*",
			wantErr: true,
			errCode: ErrorCodeInvalidRedirectURI,
		},
		{
			name:    "wildcard subdomain",
			uri:     "https://*.example.com/callback",
			wantErr: true,
			errCode: ErrorCodeInvalidRedirectURI,
		},
		{
			name:    "invalid custom scheme starting with number",
			uri:     "123app://callback",
			wantErr: true,
			errCode: ErrorCodeInvalidRedirectURI,
		},
		{
			name:    "invalid custom scheme starting with hyphen",
			uri:     "-app://callback",
			wantErr: true,
			errCode: ErrorCodeInvalidRedirectURI,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRedirectURI(tt.uri)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRedirectURI() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil {
				regErr, ok := err.(*ClientRegistrationError)
				if !ok {
					t.Errorf("expected ClientRegistrationError, got %T", err)
					return
				}
				if regErr.Code != tt.errCode {
					t.Errorf("expected error code %s, got %s", tt.errCode, regErr.Code)
				}
			}
		})
	}
}

func TestClientRegistrationError(t *testing.T) {
	t.Run("error with description", func(t *testing.T) {
		err := NewInvalidRedirectURIError("test description")
		if err.Code != ErrorCodeInvalidRedirectURI {
			t.Errorf("expected code %s, got %s", ErrorCodeInvalidRedirectURI, err.Code)
		}
		if err.Description != "test description" {
			t.Errorf("expected description 'test description', got '%s'", err.Description)
		}
		expected := "invalid_redirect_uri: test description"
		if err.Error() != expected {
			t.Errorf("expected error string '%s', got '%s'", expected, err.Error())
		}
	})

	t.Run("error without description", func(t *testing.T) {
		err := &ClientRegistrationError{Code: ErrorCodeInvalidClientMetadata}
		if err.Error() != ErrorCodeInvalidClientMetadata {
			t.Errorf("expected error string '%s', got '%s'", ErrorCodeInvalidClientMetadata, err.Error())
		}
	})

	t.Run("NewInvalidClientMetadataError", func(t *testing.T) {
		err := NewInvalidClientMetadataError("bad metadata")
		if err.Code != ErrorCodeInvalidClientMetadata {
			t.Errorf("expected code %s, got %s", ErrorCodeInvalidClientMetadata, err.Code)
		}
		if err.Description != "bad metadata" {
			t.Errorf("expected description 'bad metadata', got '%s'", err.Description)
		}
	})
}

func TestInMemoryClientStore(t *testing.T) {
	store := NewInMemoryClientStore()

	t.Run("successful registration", func(t *testing.T) {
		req := &ClientRegistrationRequest{
			RedirectURIs: []string{"https://example.com/callback"},
			ClientName:   "Test App",
			ClientURI:    "https://example.com",
			Scope:        "openid profile",
		}

		resp, err := store.RegisterClient(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify response fields
		if resp.ClientID == "" {
			t.Error("client_id should not be empty")
		}
		if !strings.HasPrefix(resp.ClientID, "client_") {
			t.Errorf("client_id should start with 'client_', got '%s'", resp.ClientID)
		}
		if resp.ClientSecret == "" {
			t.Error("client_secret should not be empty")
		}
		if !strings.HasPrefix(resp.ClientSecret, "sk_") {
			t.Errorf("client_secret should start with 'sk_', got '%s'", resp.ClientSecret)
		}
		if resp.ClientIDIssuedAt == 0 {
			t.Error("client_id_issued_at should not be zero")
		}
		if resp.ClientSecretExpiresAt != 0 {
			t.Error("client_secret_expires_at should be 0 (no expiry)")
		}

		// Verify echoed fields
		if len(resp.RedirectURIs) != 1 || resp.RedirectURIs[0] != "https://example.com/callback" {
			t.Errorf("redirect_uris not echoed correctly")
		}
		if resp.ClientName != "Test App" {
			t.Errorf("client_name not echoed correctly")
		}
		if resp.ClientURI != "https://example.com" {
			t.Errorf("client_uri not echoed correctly")
		}
		if resp.Scope != "openid profile" {
			t.Errorf("scope not echoed correctly")
		}

		// Verify defaults applied
		if resp.TokenEndpointAuthMethod != "client_secret_basic" {
			t.Errorf("expected default token_endpoint_auth_method 'client_secret_basic', got '%s'", resp.TokenEndpointAuthMethod)
		}
		if len(resp.GrantTypes) != 1 || resp.GrantTypes[0] != "authorization_code" {
			t.Errorf("expected default grant_types ['authorization_code'], got %v", resp.GrantTypes)
		}
		if len(resp.ResponseTypes) != 1 || resp.ResponseTypes[0] != "code" {
			t.Errorf("expected default response_types ['code'], got %v", resp.ResponseTypes)
		}

		// Verify client is stored
		storedClient, ok := store.GetClient(resp.ClientID)
		if !ok {
			t.Error("client should be stored")
		}
		if storedClient.ClientID != resp.ClientID {
			t.Error("stored client ID mismatch")
		}
	})

	t.Run("registration with custom options", func(t *testing.T) {
		req := &ClientRegistrationRequest{
			RedirectURIs:            []string{"https://example.com/callback", "https://example.com/callback2"},
			TokenEndpointAuthMethod: "client_secret_post",
			GrantTypes:              []string{"authorization_code", "refresh_token"},
			ResponseTypes:           []string{"code"},
			ClientName:              "Custom App",
		}

		resp, err := store.RegisterClient(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp.TokenEndpointAuthMethod != "client_secret_post" {
			t.Errorf("expected token_endpoint_auth_method 'client_secret_post', got '%s'", resp.TokenEndpointAuthMethod)
		}
		if len(resp.GrantTypes) != 2 {
			t.Errorf("expected 2 grant types, got %d", len(resp.GrantTypes))
		}
		if len(resp.RedirectURIs) != 2 {
			t.Errorf("expected 2 redirect URIs, got %d", len(resp.RedirectURIs))
		}
	})

	t.Run("registration fails with empty redirect_uris", func(t *testing.T) {
		req := &ClientRegistrationRequest{
			RedirectURIs: []string{},
			ClientName:   "Test App",
		}

		_, err := store.RegisterClient(context.Background(), req)
		if err == nil {
			t.Error("expected error for empty redirect_uris")
		}
		regErr, ok := err.(*ClientRegistrationError)
		if !ok {
			t.Errorf("expected ClientRegistrationError, got %T", err)
			return
		}
		if regErr.Code != ErrorCodeInvalidClientMetadata {
			t.Errorf("expected error code %s, got %s", ErrorCodeInvalidClientMetadata, regErr.Code)
		}
	})

	t.Run("registration fails with invalid redirect_uri", func(t *testing.T) {
		req := &ClientRegistrationRequest{
			RedirectURIs: []string{"http://external.com/callback"},
			ClientName:   "Test App",
		}

		_, err := store.RegisterClient(context.Background(), req)
		if err == nil {
			t.Error("expected error for invalid redirect URI")
		}
		regErr, ok := err.(*ClientRegistrationError)
		if !ok {
			t.Errorf("expected ClientRegistrationError, got %T", err)
			return
		}
		if regErr.Code != ErrorCodeInvalidRedirectURI {
			t.Errorf("expected error code %s, got %s", ErrorCodeInvalidRedirectURI, regErr.Code)
		}
	})

	t.Run("GetClient returns false for non-existent client", func(t *testing.T) {
		_, ok := store.GetClient("non-existent-client")
		if ok {
			t.Error("expected GetClient to return false for non-existent client")
		}
	})
}

func TestHandleClientRegistration(t *testing.T) {
	// Create a server with client registration enabled
	config := DefaultConfig()
	config.EnableClientRegistration = true
	server := NewServer(nil, config, nil)

	t.Run("successful registration", func(t *testing.T) {
		req := ClientRegistrationRequest{
			RedirectURIs: []string{"https://example.com/callback"},
			ClientName:   "Test Client",
			ClientURI:    "https://example.com",
			Scope:        "openid profile email",
		}
		body, _ := json.Marshal(req)

		httpReq := httptest.NewRequest(http.MethodPost, "/oauth2/register", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		server.handleClientRegistration(recorder, httpReq)

		if recorder.Code != http.StatusCreated {
			t.Errorf("expected status %d, got %d", http.StatusCreated, recorder.Code)
			t.Logf("response body: %s", recorder.Body.String())
		}

		contentType := recorder.Header().Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("expected Content-Type 'application/json', got '%s'", contentType)
		}

		var resp ClientRegistrationResponse
		if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.ClientID == "" {
			t.Error("client_id should not be empty")
		}
		if resp.ClientSecret == "" {
			t.Error("client_secret should not be empty")
		}
		if resp.ClientName != "Test Client" {
			t.Errorf("expected client_name 'Test Client', got '%s'", resp.ClientName)
		}
		if resp.ClientURI != "https://example.com" {
			t.Errorf("expected client_uri 'https://example.com', got '%s'", resp.ClientURI)
		}
		if resp.Scope != "openid profile email" {
			t.Errorf("expected scope 'openid profile email', got '%s'", resp.Scope)
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		httpReq := httptest.NewRequest(http.MethodGet, "/oauth2/register", nil)
		recorder := httptest.NewRecorder()

		server.handleClientRegistration(recorder, httpReq)

		if recorder.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, recorder.Code)
		}

		allow := recorder.Header().Get("Allow")
		if allow != "POST" {
			t.Errorf("expected Allow header 'POST', got '%s'", allow)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		httpReq := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader("not json"))
		httpReq.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		server.handleClientRegistration(recorder, httpReq)

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
		}

		var resp ClientRegistrationErrorResponse
		if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}

		if resp.Error != ErrorCodeInvalidClientMetadata {
			t.Errorf("expected error code %s, got %s", ErrorCodeInvalidClientMetadata, resp.Error)
		}
	})

	t.Run("missing redirect_uris", func(t *testing.T) {
		req := ClientRegistrationRequest{
			ClientName: "Test Client",
		}
		body, _ := json.Marshal(req)

		httpReq := httptest.NewRequest(http.MethodPost, "/oauth2/register", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		server.handleClientRegistration(recorder, httpReq)

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
		}

		var resp ClientRegistrationErrorResponse
		if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}

		if resp.Error != ErrorCodeInvalidClientMetadata {
			t.Errorf("expected error code %s, got %s", ErrorCodeInvalidClientMetadata, resp.Error)
		}
	})

	t.Run("invalid redirect_uri rejected", func(t *testing.T) {
		req := ClientRegistrationRequest{
			RedirectURIs: []string{"http://external.com/callback"},
			ClientName:   "Test Client",
		}
		body, _ := json.Marshal(req)

		httpReq := httptest.NewRequest(http.MethodPost, "/oauth2/register", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		server.handleClientRegistration(recorder, httpReq)

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
		}

		var resp ClientRegistrationErrorResponse
		if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}

		if resp.Error != ErrorCodeInvalidRedirectURI {
			t.Errorf("expected error code %s, got %s", ErrorCodeInvalidRedirectURI, resp.Error)
		}
	})

	t.Run("http localhost allowed", func(t *testing.T) {
		req := ClientRegistrationRequest{
			RedirectURIs: []string{"http://localhost:8080/callback"},
			ClientName:   "Local Dev Client",
		}
		body, _ := json.Marshal(req)

		httpReq := httptest.NewRequest(http.MethodPost, "/oauth2/register", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		server.handleClientRegistration(recorder, httpReq)

		if recorder.Code != http.StatusCreated {
			t.Errorf("expected status %d, got %d", http.StatusCreated, recorder.Code)
			t.Logf("response body: %s", recorder.Body.String())
		}
	})

	t.Run("response contains all echoed fields", func(t *testing.T) {
		req := ClientRegistrationRequest{
			RedirectURIs:            []string{"https://example.com/callback", "https://example.com/callback2"},
			TokenEndpointAuthMethod: "client_secret_post",
			GrantTypes:              []string{"authorization_code", "refresh_token"},
			ResponseTypes:           []string{"code"},
			ClientName:              "Full Fields Client",
			ClientURI:               "https://example.com",
			Scope:                   "openid profile email",
		}
		body, _ := json.Marshal(req)

		httpReq := httptest.NewRequest(http.MethodPost, "/oauth2/register", bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		server.handleClientRegistration(recorder, httpReq)

		if recorder.Code != http.StatusCreated {
			t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, recorder.Code, recorder.Body.String())
		}

		var resp ClientRegistrationResponse
		if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		// Verify all echoed fields
		if len(resp.RedirectURIs) != 2 {
			t.Errorf("expected 2 redirect_uris, got %d", len(resp.RedirectURIs))
		}
		if resp.TokenEndpointAuthMethod != "client_secret_post" {
			t.Errorf("expected token_endpoint_auth_method 'client_secret_post', got '%s'", resp.TokenEndpointAuthMethod)
		}
		if len(resp.GrantTypes) != 2 {
			t.Errorf("expected 2 grant_types, got %d", len(resp.GrantTypes))
		}
		if len(resp.ResponseTypes) != 1 || resp.ResponseTypes[0] != "code" {
			t.Errorf("expected response_types ['code'], got %v", resp.ResponseTypes)
		}
		if resp.ClientName != "Full Fields Client" {
			t.Errorf("expected client_name 'Full Fields Client', got '%s'", resp.ClientName)
		}
		if resp.ClientURI != "https://example.com" {
			t.Errorf("expected client_uri 'https://example.com', got '%s'", resp.ClientURI)
		}
		if resp.Scope != "openid profile email" {
			t.Errorf("expected scope 'openid profile email', got '%s'", resp.Scope)
		}
	})

	t.Run("empty content type allowed", func(t *testing.T) {
		req := ClientRegistrationRequest{
			RedirectURIs: []string{"https://example.com/callback"},
			ClientName:   "Test Client",
		}
		body, _ := json.Marshal(req)

		httpReq := httptest.NewRequest(http.MethodPost, "/oauth2/register", bytes.NewReader(body))
		// Don't set Content-Type header (empty)
		recorder := httptest.NewRecorder()

		server.handleClientRegistration(recorder, httpReq)

		if recorder.Code != http.StatusCreated {
			t.Errorf("expected status %d, got %d", http.StatusCreated, recorder.Code)
		}
	})

	t.Run("wrong content type rejected", func(t *testing.T) {
		httpReq := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader("data"))
		httpReq.Header.Set("Content-Type", "text/plain")
		recorder := httptest.NewRecorder()

		server.handleClientRegistration(recorder, httpReq)

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
		}
	})

	t.Run("PUT method not allowed", func(t *testing.T) {
		httpReq := httptest.NewRequest(http.MethodPut, "/oauth2/register", nil)
		recorder := httptest.NewRecorder()

		server.handleClientRegistration(recorder, httpReq)

		if recorder.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, recorder.Code)
		}
	})

	t.Run("DELETE method not allowed", func(t *testing.T) {
		httpReq := httptest.NewRequest(http.MethodDelete, "/oauth2/register", nil)
		recorder := httptest.NewRecorder()

		server.handleClientRegistration(recorder, httpReq)

		if recorder.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, recorder.Code)
		}
	})
}

func TestHandleClientRegistrationNotConfigured(t *testing.T) {
	// Create a server without client registration enabled
	config := DefaultConfig()
	config.EnableClientRegistration = false
	server := NewServer(nil, config, nil)

	req := ClientRegistrationRequest{
		RedirectURIs: []string{"https://example.com/callback"},
		ClientName:   "Test Client",
	}
	body, _ := json.Marshal(req)

	httpReq := httptest.NewRequest(http.MethodPost, "/oauth2/register", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.handleClientRegistration(recorder, httpReq)

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, recorder.Code)
	}
}

func TestServerSetAppRegistrar(t *testing.T) {
	config := DefaultConfig()
	config.EnableClientRegistration = true
	server := NewServer(nil, config, nil)

	// Create mock registrar
	mockRegistrar := &mockAppRegistrar{
		response: &ClientRegistrationResponse{
			ClientID:                "mock_client_id",
			ClientSecret:            "mock_secret",
			ClientIDIssuedAt:        1234567890,
			ClientSecretExpiresAt:   0,
			RedirectURIs:            []string{"https://example.com/callback"},
			TokenEndpointAuthMethod: "client_secret_basic",
			GrantTypes:              []string{"authorization_code"},
			ResponseTypes:           []string{"code"},
		},
	}

	server.SetAppRegistrar(mockRegistrar)

	req := ClientRegistrationRequest{
		RedirectURIs: []string{"https://example.com/callback"},
		ClientName:   "Test Client",
	}
	body, _ := json.Marshal(req)

	httpReq := httptest.NewRequest(http.MethodPost, "/oauth2/register", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.handleClientRegistration(recorder, httpReq)

	if recorder.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, recorder.Code)
	}

	var resp ClientRegistrationResponse
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify mock registrar was used
	if resp.ClientID != "mock_client_id" {
		t.Errorf("expected client_id 'mock_client_id', got '%s'", resp.ClientID)
	}
}

func TestServerSetAppRegistrarWithError(t *testing.T) {
	config := DefaultConfig()
	config.EnableClientRegistration = true
	server := NewServer(nil, config, nil)

	// Create mock registrar that returns an error
	mockRegistrar := &mockAppRegistrar{
		err: NewInvalidClientMetadataError("mock error"),
	}

	server.SetAppRegistrar(mockRegistrar)

	req := ClientRegistrationRequest{
		RedirectURIs: []string{"https://example.com/callback"},
		ClientName:   "Test Client",
	}
	body, _ := json.Marshal(req)

	httpReq := httptest.NewRequest(http.MethodPost, "/oauth2/register", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.handleClientRegistration(recorder, httpReq)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	var resp ClientRegistrationErrorResponse
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Error != ErrorCodeInvalidClientMetadata {
		t.Errorf("expected error code %s, got %s", ErrorCodeInvalidClientMetadata, resp.Error)
	}
}

func TestServerSetAppRegistrarWithGenericError(t *testing.T) {
	config := DefaultConfig()
	config.EnableClientRegistration = true
	server := NewServer(nil, config, nil)

	// Create mock registrar that returns a generic error (not ClientRegistrationError)
	mockRegistrar := &mockAppRegistrar{
		err: errors.New("generic error"),
	}

	server.SetAppRegistrar(mockRegistrar)

	req := ClientRegistrationRequest{
		RedirectURIs: []string{"https://example.com/callback"},
		ClientName:   "Test Client",
	}
	body, _ := json.Marshal(req)

	httpReq := httptest.NewRequest(http.MethodPost, "/oauth2/register", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.handleClientRegistration(recorder, httpReq)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	var resp ClientRegistrationErrorResponse
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Generic errors should be wrapped as invalid_client_metadata
	if resp.Error != ErrorCodeInvalidClientMetadata {
		t.Errorf("expected error code %s, got %s", ErrorCodeInvalidClientMetadata, resp.Error)
	}
}

func TestServerEnableClientRegistrationEndpoint(t *testing.T) {
	config := DefaultConfig()
	config.EnableClientRegistration = false
	server := NewServer(nil, config, nil)

	// Initially no in-memory store
	if server.inMemoryClientStore != nil {
		t.Error("expected no in-memory client store initially")
	}

	// Enable registration
	server.EnableClientRegistrationEndpoint(true)

	if !server.enableClientRegistration {
		t.Error("expected enableClientRegistration to be true")
	}
	if server.inMemoryClientStore == nil {
		t.Error("expected in-memory client store to be initialized")
	}

	// Disable registration
	server.EnableClientRegistrationEndpoint(false)
	if server.enableClientRegistration {
		t.Error("expected enableClientRegistration to be false")
	}
}

func TestIsValidCustomScheme(t *testing.T) {
	tests := []struct {
		scheme string
		valid  bool
	}{
		{"myapp", true},
		{"com.example.app", true},
		{"app+custom", true},
		{"app-mobile", true},
		{"MyApp", true},
		{"a", true},
		{"A1b2c3", true},
		{"", false},
		{"123app", false},
		{"-app", false},
		{".app", false},
		{"+app", false},
	}

	for _, tt := range tests {
		t.Run(tt.scheme, func(t *testing.T) {
			result := isValidCustomScheme(tt.scheme)
			if result != tt.valid {
				t.Errorf("isValidCustomScheme(%q) = %v, want %v", tt.scheme, result, tt.valid)
			}
		})
	}
}

func TestContainsWildcard(t *testing.T) {
	tests := []struct {
		uri      string
		contains bool
	}{
		{"https://example.com/callback", false},
		{"https://example.com/*", true},
		{"https://*.example.com/callback", true},
		{"https://example.com/path*", true},
		{"*", true},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			result := containsWildcard(tt.uri)
			if result != tt.contains {
				t.Errorf("containsWildcard(%q) = %v, want %v", tt.uri, result, tt.contains)
			}
		})
	}
}

func TestIsLocalhostAddress(t *testing.T) {
	tests := []struct {
		host      string
		localhost bool
	}{
		{"localhost", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"example.com", false},
		{"192.168.1.1", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			result := isLocalhostAddress(tt.host)
			if result != tt.localhost {
				t.Errorf("isLocalhostAddress(%q) = %v, want %v", tt.host, result, tt.localhost)
			}
		})
	}
}

// mockAppRegistrar is a mock implementation of AppRegistrar for testing
type mockAppRegistrar struct {
	response *ClientRegistrationResponse
	err      error
}

func (m *mockAppRegistrar) RegisterClient(ctx context.Context, req *ClientRegistrationRequest) (*ClientRegistrationResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func TestClientRegistrationEndpointDisabled(t *testing.T) {
	// Test that the endpoint is not registered when disabled
	config := &Config{
		ServerName:               "Test Server",
		ServerVersion:            "1.0.0",
		Transport:                "http",
		HTTPAddr:                 "127.0.0.1:0",
		EnableClientRegistration: false,
	}
	server := NewServer(nil, config, nil)

	// The handler should return error when endpoint is not configured
	req := ClientRegistrationRequest{
		RedirectURIs: []string{"https://example.com/callback"},
	}
	body, _ := json.Marshal(req)

	httpReq := httptest.NewRequest(http.MethodPost, "/oauth2/register", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.handleClientRegistration(recorder, httpReq)

	// Should fail because client registration is not configured
	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d when disabled, got %d", http.StatusInternalServerError, recorder.Code)
	}
}

func TestGenerateSecureID(t *testing.T) {
	id1, err := generateSecureID("test_")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(id1, "test_") {
		t.Errorf("expected prefix 'test_', got '%s'", id1)
	}

	id2, err := generateSecureID("test_")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// IDs should be unique
	if id1 == id2 {
		t.Error("expected unique IDs")
	}
}

func TestGenerateClientSecretWithHash(t *testing.T) {
	plaintext, hash, err := generateClientSecretWithHash()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(plaintext, "sk_") {
		t.Errorf("expected secret to start with 'sk_', got '%s'", plaintext)
	}

	if hash == "" {
		t.Error("hash should not be empty")
	}

	if plaintext == hash {
		t.Error("plaintext and hash should be different")
	}

	// Generate another and verify they're different
	plaintext2, _, err := generateClientSecretWithHash()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plaintext == plaintext2 {
		t.Error("expected unique secrets")
	}
}

func TestValidateRedirectURIInvalidSchemeNotCustom(t *testing.T) {
	// Test scheme that is not http, https, or a valid custom scheme
	// A scheme starting with a number is not valid
	err := ValidateRedirectURI("123scheme://callback")
	if err == nil {
		t.Error("expected error for invalid scheme starting with number")
	}
	regErr, ok := err.(*ClientRegistrationError)
	if !ok {
		t.Errorf("expected ClientRegistrationError, got %T", err)
		return
	}
	if regErr.Code != ErrorCodeInvalidRedirectURI {
		t.Errorf("expected error code %s, got %s", ErrorCodeInvalidRedirectURI, regErr.Code)
	}
}

func TestClientRegistrationRequestJSONSerialization(t *testing.T) {
	req := &ClientRegistrationRequest{
		RedirectURIs:            []string{"https://example.com/callback"},
		TokenEndpointAuthMethod: "client_secret_basic",
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		ClientName:              "Test App",
		ClientURI:               "https://example.com",
		Scope:                   "openid profile",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ClientRegistrationRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(decoded.RedirectURIs) != 1 || decoded.RedirectURIs[0] != "https://example.com/callback" {
		t.Error("redirect_uris not preserved")
	}
	if decoded.ClientName != "Test App" {
		t.Error("client_name not preserved")
	}
}

func TestClientRegistrationResponseJSONSerialization(t *testing.T) {
	resp := &ClientRegistrationResponse{
		ClientID:                "client_123",
		ClientSecret:            "sk_secret",
		ClientIDIssuedAt:        1234567890,
		ClientSecretExpiresAt:   0,
		RedirectURIs:            []string{"https://example.com/callback"},
		TokenEndpointAuthMethod: "client_secret_basic",
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		ClientName:              "Test App",
		ClientURI:               "https://example.com",
		Scope:                   "openid profile",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ClientRegistrationResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ClientID != "client_123" {
		t.Error("client_id not preserved")
	}
	if decoded.ClientSecret != "sk_secret" {
		t.Error("client_secret not preserved")
	}
}

func TestClientRegistrationErrorResponseJSONSerialization(t *testing.T) {
	resp := &ClientRegistrationErrorResponse{
		Error:            ErrorCodeInvalidRedirectURI,
		ErrorDescription: "test description",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ClientRegistrationErrorResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Error != ErrorCodeInvalidRedirectURI {
		t.Error("error not preserved")
	}
	if decoded.ErrorDescription != "test description" {
		t.Error("error_description not preserved")
	}
}
