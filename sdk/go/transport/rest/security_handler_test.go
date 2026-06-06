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
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// newSecurityTestTransport stands up a Transport pointed at the supplied
// httptest server with the given default tenant.
func newSecurityTestTransport(t *testing.T, server *httptest.Server, tenant string) *Transport {
	t.Helper()
	tr, err := New(&transport.Config{
		Address:    server.URL,
		MaxRetries: 0,
	}, WithDefaultTenant(tenant))
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	return tr
}

// webAuthn request wire shapes mirror the SDK wrapper structs declared in
// sdk/go/services_security.go. The handler reads them via JSON round-trip
// (toMap), so structurally-equivalent local types exercise the exact same
// path the SDK client uses.
type webAuthnUserWire struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
	UserID      string `json:"user_id,omitempty"`
}

type webAuthnBeginWire struct {
	User *webAuthnUserWire `json:"user"`
}

type webAuthnFinishRegWire struct {
	User     *webAuthnUserWire `json:"user"`
	Response struct {
		SessionID       string `json:"session_id"`
		AttestationJSON string `json:"attestation_json"`
	} `json:"response"`
}

type webAuthnFinishLoginWire struct {
	User     *webAuthnUserWire `json:"user"`
	Response struct {
		SessionID     string `json:"session_id"`
		Username      string `json:"username"`
		AssertionJSON string `json:"assertion_json"`
	} `json:"response"`
}

func TestSecurity_WebAuthn_BeginRegistration_HitsIAMMount(t *testing.T) {
	const (
		tenant      = "acme-tenant"
		sessionID   = "sess-abc-123"
		optionsJSON = `{"publicKey":{"challenge":"Zm9v"}}`
	)
	var gotPath, gotMethod string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set(headerSessionID, sessionID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(optionsJSON))
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, tenant)
	defer tr.Close()

	result, err := tr.Request(context.Background(), securityServiceWebAuthn, "BeginRegistration",
		&webAuthnBeginWire{User: &webAuthnUserWire{Username: "user@example.com", DisplayName: "User"}})
	if err != nil {
		t.Fatalf("BeginRegistration failed: %v", err)
	}

	wantPath := "/api/v1/iam/" + tenant + "/webauthn/registration/begin"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if !contains(string(gotBody), `"email":"user@example.com"`) {
		t.Errorf("body = %q, expected email field", gotBody)
	}

	resp, ok := result.(*xkmstransport.BeginRegistrationResponse)
	if !ok {
		t.Fatalf("result type = %T, want *xkmstransport.BeginRegistrationResponse", result)
	}
	if resp.SessionID != sessionID {
		t.Errorf("SessionID = %q, want %q", resp.SessionID, sessionID)
	}
	if resp.OptionsJSON != optionsJSON {
		t.Errorf("OptionsJSON = %q, want %q", resp.OptionsJSON, optionsJSON)
	}
}

func TestSecurity_WebAuthn_FinishRegistration_SendsSessionHeader(t *testing.T) {
	const (
		tenant          = "acme-tenant"
		sessionID       = "sess-xyz-789"
		attestationJSON = `{"id":"cred","response":{}}`
	)
	var gotPath, gotSession string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotSession = r.Header.Get(headerSessionID)
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"token":"jwt-token","user_id":"dXNlcg"}`))
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, tenant)
	defer tr.Close()

	req := &webAuthnFinishRegWire{User: &webAuthnUserWire{Username: "user@example.com"}}
	req.Response.SessionID = sessionID
	req.Response.AttestationJSON = attestationJSON

	result, err := tr.Request(context.Background(), securityServiceWebAuthn, "FinishRegistration", req)
	if err != nil {
		t.Fatalf("FinishRegistration failed: %v", err)
	}

	wantPath := "/api/v1/iam/" + tenant + "/webauthn/registration/finish"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if gotSession != sessionID {
		t.Errorf("X-Session-Id = %q, want %q", gotSession, sessionID)
	}
	if string(gotBody) != attestationJSON {
		t.Errorf("body = %q, want raw attestation %q", gotBody, attestationJSON)
	}

	resp, ok := result.(*xkmstransport.FinishRegistrationResponse)
	if !ok {
		t.Fatalf("result type = %T, want *xkmstransport.FinishRegistrationResponse", result)
	}
	if !resp.Success || resp.Token != "jwt-token" || resp.CredentialID != "dXNlcg" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestSecurity_WebAuthn_BeginLogin_HitsIAMMount(t *testing.T) {
	const tenant = "acme-tenant"
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set(headerSessionID, "login-sess")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"publicKey":{}}`))
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, tenant)
	defer tr.Close()

	result, err := tr.Request(context.Background(), securityServiceWebAuthn, "BeginLogin",
		&webAuthnBeginWire{User: &webAuthnUserWire{Username: "user@example.com"}})
	if err != nil {
		t.Fatalf("BeginLogin failed: %v", err)
	}
	if gotPath != "/api/v1/iam/"+tenant+"/webauthn/login/begin" {
		t.Errorf("path = %q", gotPath)
	}
	resp, ok := result.(*xkmstransport.BeginAuthenticationResponse)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if resp.SessionID != "login-sess" {
		t.Errorf("SessionID = %q", resp.SessionID)
	}
}

func TestSecurity_WebAuthn_FinishLogin_SendsHeaders(t *testing.T) {
	const tenant = "acme-tenant"
	var gotPath, gotSession, gotUser string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotSession = r.Header.Get(headerSessionID)
		gotUser = r.Header.Get(headerUserID)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"token":"sess-token","user_id":"dXNlcg"}`))
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, tenant)
	defer tr.Close()

	req := &webAuthnFinishLoginWire{User: &webAuthnUserWire{Username: "user@example.com", UserID: "dXNlcg"}}
	req.Response.SessionID = "login-sess"
	req.Response.AssertionJSON = `{"id":"cred"}`

	result, err := tr.Request(context.Background(), securityServiceWebAuthn, "FinishLogin", req)
	if err != nil {
		t.Fatalf("FinishLogin failed: %v", err)
	}
	if gotPath != "/api/v1/iam/"+tenant+"/webauthn/login/finish" {
		t.Errorf("path = %q", gotPath)
	}
	if gotSession != "login-sess" {
		t.Errorf("X-Session-Id = %q", gotSession)
	}
	if gotUser != "dXNlcg" {
		t.Errorf("X-User-Id = %q", gotUser)
	}
	resp, ok := result.(*xkmstransport.FinishAuthenticationResponse)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if resp.Token != "sess-token" || resp.Username != "user@example.com" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestSecurity_WebAuthn_TenantFromRequestOverridesDefault(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set(headerSessionID, "s")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, "default-tenant")
	defer tr.Close()

	// tenant_id carried alongside the request takes precedence over the
	// transport default, mirroring the IAM transport mechanism.
	reqMap := map[string]interface{}{
		"tenant_id": "explicit-tenant",
		"user":      map[string]interface{}{"username": "user@example.com"},
	}
	if _, err := tr.Request(context.Background(), securityServiceWebAuthn, "BeginRegistration", reqMap); err != nil {
		t.Fatalf("BeginRegistration failed: %v", err)
	}
	if gotPath != "/api/v1/iam/explicit-tenant/webauthn/registration/begin" {
		t.Errorf("path = %q, want explicit-tenant", gotPath)
	}
}

func TestSecurity_WebAuthn_MissingTenant_TypedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server must not be called when tenant is unresolved")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, "") // no default tenant
	defer tr.Close()

	_, err := tr.Request(context.Background(), securityServiceWebAuthn, "BeginRegistration",
		&webAuthnBeginWire{User: &webAuthnUserWire{Username: "user@example.com"}})
	if err == nil {
		t.Fatal("expected error when tenant is unresolved")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid-input error, got %v", err)
	}
}

func TestSecurity_UnsupportedVerbs_TypedUnimplemented(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server must not be called for unimplemented verbs")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, "acme-tenant")
	defer tr.Close()

	cases := []struct {
		service string
		method  string
	}{
		{securityServiceFIDO2, "ListDevices"},
		{securityServicePIV, "ListReaders"},
		{securityServiceACME, "NewOrder"},
		{securityServiceTrust, "PublishDANE"},
	}
	for _, tc := range cases {
		t.Run(tc.service+"."+tc.method, func(t *testing.T) {
			_, err := tr.Request(context.Background(), tc.service, tc.method, struct{}{})
			if err == nil {
				t.Fatalf("expected unimplemented error for %s.%s", tc.service, tc.method)
			}
			if !errors.IsUnimplemented(err) {
				t.Errorf("expected unimplemented error, got %v", err)
			}
		})
	}
}
