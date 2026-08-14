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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	webauthnhttp "github.com/jeremyhahn/go-xkms/pkg/webauthn/http"
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

func TestSecurity_FIDO2_BeginRegistration_HitsIAMMount(t *testing.T) {
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
		w.Header().Set(webauthnhttp.HeaderSessionID, sessionID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(optionsJSON))
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, tenant)
	defer tr.Close()

	result, err := tr.Request(context.Background(), securityServiceFIDO2, "BeginRegistration",
		&xkmstransport.BeginRegistrationRequest{Username: "user@example.com", DisplayName: "User"})
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

func TestSecurity_FIDO2_BeginRegistration_InvalidRequestType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server must not be called for an invalid request type")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, "acme-tenant")
	defer tr.Close()

	_, err := tr.Request(context.Background(), securityServiceFIDO2, "BeginRegistration",
		map[string]interface{}{"username": "user@example.com"})
	if err == nil {
		t.Fatal("expected error for invalid request type")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid-input error, got %v", err)
	}
}

func TestSecurity_FIDO2_FinishRegistration_SendsSessionHeader(t *testing.T) {
	const (
		tenant          = "acme-tenant"
		sessionID       = "sess-xyz-789"
		attestationJSON = `{"id":"cred","response":{}}`
	)
	var gotPath, gotSession string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotSession = r.Header.Get(webauthnhttp.HeaderSessionID)
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"token":"jwt-token","user_id":"dXNlcg"}`))
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, tenant)
	defer tr.Close()

	result, err := tr.Request(context.Background(), securityServiceFIDO2, "FinishRegistration",
		&xkmstransport.FinishRegistrationRequest{
			SessionID:       sessionID,
			AttestationJSON: attestationJSON,
		})
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

func TestSecurity_FIDO2_FinishRegistration_MissingFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server must not be called when required fields are missing")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, "acme-tenant")
	defer tr.Close()

	_, err := tr.Request(context.Background(), securityServiceFIDO2, "FinishRegistration",
		&xkmstransport.FinishRegistrationRequest{SessionID: "s"})
	if err == nil {
		t.Fatal("expected error for missing attestation_json")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid-input error, got %v", err)
	}
}

func TestSecurity_FIDO2_BeginAuthentication_HitsIAMMount(t *testing.T) {
	const tenant = "acme-tenant"
	var gotPath string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set(webauthnhttp.HeaderSessionID, "login-sess")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"publicKey":{}}`))
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, tenant)
	defer tr.Close()

	result, err := tr.Request(context.Background(), securityServiceFIDO2, "BeginAuthentication",
		&xkmstransport.BeginAuthenticationRequest{Username: "user@example.com"})
	if err != nil {
		t.Fatalf("BeginAuthentication failed: %v", err)
	}
	if gotPath != "/api/v1/iam/"+tenant+"/webauthn/login/begin" {
		t.Errorf("path = %q", gotPath)
	}
	if !contains(string(gotBody), `"email":"user@example.com"`) {
		t.Errorf("body = %q, expected email field", gotBody)
	}
	resp, ok := result.(*xkmstransport.BeginAuthenticationResponse)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if resp.SessionID != "login-sess" {
		t.Errorf("SessionID = %q", resp.SessionID)
	}
}

func TestSecurity_FIDO2_FinishAuthentication_SendsSessionHeader(t *testing.T) {
	const tenant = "acme-tenant"
	var gotPath, gotSession string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotSession = r.Header.Get(webauthnhttp.HeaderSessionID)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"token":"sess-token","user_id":"dXNlcg"}`))
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, tenant)
	defer tr.Close()

	result, err := tr.Request(context.Background(), securityServiceFIDO2, "FinishAuthentication",
		&xkmstransport.FinishAuthenticationRequest{
			SessionID:     "login-sess",
			Username:      "user@example.com",
			AssertionJSON: `{"id":"cred"}`,
		})
	if err != nil {
		t.Fatalf("FinishAuthentication failed: %v", err)
	}
	if gotPath != "/api/v1/iam/"+tenant+"/webauthn/login/finish" {
		t.Errorf("path = %q", gotPath)
	}
	if gotSession != "login-sess" {
		t.Errorf("X-Session-Id = %q", gotSession)
	}
	resp, ok := result.(*xkmstransport.FinishAuthenticationResponse)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if resp.Token != "sess-token" || resp.Username != "user@example.com" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestSecurity_FIDO2_TenantFromContextOverridesDefault(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set(webauthnhttp.HeaderSessionID, "s")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, "default-tenant")
	defer tr.Close()

	ctx := transport.ContextWithTenant(context.Background(), "explicit-tenant")
	if _, err := tr.Request(ctx, securityServiceFIDO2, "BeginRegistration",
		&xkmstransport.BeginRegistrationRequest{Username: "user@example.com"}); err != nil {
		t.Fatalf("BeginRegistration failed: %v", err)
	}
	if gotPath != "/api/v1/iam/explicit-tenant/webauthn/registration/begin" {
		t.Errorf("path = %q, want explicit-tenant", gotPath)
	}
}

func TestSecurity_FIDO2_MissingTenant_TypedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server must not be called when tenant is unresolved")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, "") // no default tenant
	defer tr.Close()

	_, err := tr.Request(context.Background(), securityServiceFIDO2, "BeginRegistration",
		&xkmstransport.BeginRegistrationRequest{Username: "user@example.com"})
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
		{securityServicePIV, "ListReaders"},
		{securityServiceFIDO2, "BogusCeremonyVerb"},
		{securityServiceACME, "BogusUnknownVerb"},
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

// =============================================================================
// PIV bridging (/api/v1/xkms/piv/*)
// =============================================================================

func TestSecurity_PIV_ListPIVSlots_HitsXKMSRoute(t *testing.T) {
	var gotPath, gotQuery, gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(&xkmstransport.ListPIVSlotsResponse{
			Slots: []xkmstransport.PIVSlotStatus{{Slot: "9a", HasCert: true}},
		})
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, "")
	defer tr.Close()

	result, err := tr.Request(context.Background(), securityServicePIV, "ListPIVSlots",
		&xkmstransport.ListPIVSlotsRequest{Backend: "file"})
	if err != nil {
		t.Fatalf("ListPIVSlots failed: %v", err)
	}
	if gotPath != "/api/v1/xkms/piv/slots" {
		t.Errorf("path = %q", gotPath)
	}
	if gotQuery != "backend=file" {
		t.Errorf("query = %q, want backend=file", gotQuery)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	resp, ok := result.(*xkmstransport.ListPIVSlotsResponse)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if len(resp.Slots) != 1 || resp.Slots[0].Slot != "9a" {
		t.Errorf("unexpected slots: %+v", resp.Slots)
	}
}

func TestSecurity_PIV_ListPIVSlots_InvalidRequestType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server must not be called for an invalid request type")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, "")
	defer tr.Close()

	_, err := tr.Request(context.Background(), securityServicePIV, "ListPIVSlots", struct{}{})
	if err == nil {
		t.Fatal("expected error for invalid request type")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid-input error, got %v", err)
	}
}

func TestSecurity_PIV_GetAndExportCertificate_HitCertRoute(t *testing.T) {
	for _, method := range []string{"GetPIVCertificate", "ExportPIVCertificate"} {
		t.Run(method, func(t *testing.T) {
			var gotPath, gotQuery, gotMethod string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotQuery = r.URL.RawQuery
				gotMethod = r.Method
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(&xkmstransport.GetPIVCertificateResponse{
					Slot: "9a", Format: "pem", Certificate: []byte("PEM"),
				})
			}))
			defer server.Close()

			tr := newSecurityTestTransport(t, server, "")
			defer tr.Close()

			result, err := tr.Request(context.Background(), securityServicePIV, method,
				&xkmstransport.GetPIVCertificateRequest{Backend: "tpm2", Slot: "9a", Format: "pem"})
			if err != nil {
				t.Fatalf("%s failed: %v", method, err)
			}
			if gotPath != "/api/v1/xkms/piv/slots/9a/cert" {
				t.Errorf("path = %q", gotPath)
			}
			if !contains(gotQuery, "backend=tpm2") || !contains(gotQuery, "format=pem") {
				t.Errorf("query = %q", gotQuery)
			}
			if gotMethod != http.MethodGet {
				t.Errorf("method = %q, want GET", gotMethod)
			}
			resp, ok := result.(*xkmstransport.GetPIVCertificateResponse)
			if !ok {
				t.Fatalf("result type = %T", result)
			}
			if string(resp.Certificate) != "PEM" {
				t.Errorf("certificate = %q", resp.Certificate)
			}
		})
	}
}

func TestSecurity_PIV_GetCertificate_MissingSlot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server must not be called when slot is missing")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, "")
	defer tr.Close()

	_, err := tr.Request(context.Background(), securityServicePIV, "GetPIVCertificate",
		&xkmstransport.GetPIVCertificateRequest{Backend: "file"})
	if err == nil {
		t.Fatal("expected error for missing slot")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid-input error, got %v", err)
	}
}

func TestSecurity_PIV_StoreAndImportCertificate_HitPutCertRoute(t *testing.T) {
	for _, method := range []string{"StorePIVCertificate", "ImportPIVCertificate"} {
		t.Run(method, func(t *testing.T) {
			var gotPath, gotMethod string
			var gotBody []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotMethod = r.Method
				gotBody, _ = io.ReadAll(r.Body)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()

			tr := newSecurityTestTransport(t, server, "")
			defer tr.Close()

			result, err := tr.Request(context.Background(), securityServicePIV, method,
				&xkmstransport.StorePIVCertificateRequest{
					Backend: "file", Slot: "9c", Certificate: []byte("PEM"), Format: "pem",
				})
			if err != nil {
				t.Fatalf("%s failed: %v", method, err)
			}
			if result != nil {
				t.Errorf("result = %v, want nil", result)
			}
			if gotPath != "/api/v1/xkms/piv/slots/9c/cert" {
				t.Errorf("path = %q", gotPath)
			}
			if gotMethod != http.MethodPut {
				t.Errorf("method = %q, want PUT", gotMethod)
			}
			if !contains(string(gotBody), `"slot":"9c"`) {
				t.Errorf("body = %q, expected slot field", gotBody)
			}
		})
	}
}

func TestSecurity_PIV_DeleteCertificate_HitsDeleteRoute(t *testing.T) {
	var gotPath, gotQuery, gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, "")
	defer tr.Close()

	if _, err := tr.Request(context.Background(), securityServicePIV, "DeletePIVCertificate",
		&xkmstransport.DeletePIVCertificateRequest{Backend: "pkcs11", Slot: "9d"}); err != nil {
		t.Fatalf("DeletePIVCertificate failed: %v", err)
	}
	if gotPath != "/api/v1/xkms/piv/slots/9d/cert" {
		t.Errorf("path = %q", gotPath)
	}
	if gotQuery != "backend=pkcs11" {
		t.Errorf("query = %q", gotQuery)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
}

func TestSecurity_PIV_DeleteCertificate_MissingSlot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server must not be called when slot is missing")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, "")
	defer tr.Close()

	_, err := tr.Request(context.Background(), securityServicePIV, "DeletePIVCertificate",
		&xkmstransport.DeletePIVCertificateRequest{Backend: "pkcs11"})
	if err == nil {
		t.Fatal("expected error for missing slot")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid-input error, got %v", err)
	}
}

func TestSecurity_PIV_GenerateKey_HitsKeyRoute(t *testing.T) {
	var gotPath, gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(&xkmstransport.GeneratePIVKeyResponse{
			Slot: "9a", Certificate: []byte("CERT"),
		})
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, "")
	defer tr.Close()

	result, err := tr.Request(context.Background(), securityServicePIV, "GeneratePIVKey",
		&xkmstransport.GeneratePIVKeyRequest{Backend: "file", Slot: "9a", Algorithm: "ecdsap256"})
	if err != nil {
		t.Fatalf("GeneratePIVKey failed: %v", err)
	}
	if gotPath != "/api/v1/xkms/piv/slots/9a/key" {
		t.Errorf("path = %q", gotPath)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	resp, ok := result.(*xkmstransport.GeneratePIVKeyResponse)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if string(resp.Certificate) != "CERT" {
		t.Errorf("certificate = %q", resp.Certificate)
	}
}

func TestSecurity_PIV_GenerateKey_MissingSlot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server must not be called when slot is missing")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, "")
	defer tr.Close()

	_, err := tr.Request(context.Background(), securityServicePIV, "GeneratePIVKey",
		&xkmstransport.GeneratePIVKeyRequest{Backend: "file"})
	if err == nil {
		t.Fatal("expected error for missing slot")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid-input error, got %v", err)
	}
}

func TestSecurity_PIV_GenerateCSR_HitsCSRRoute(t *testing.T) {
	var gotPath, gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(&xkmstransport.GeneratePIVCSRResponse{
			Slot: "9c", CSR: []byte("CSR-PEM"),
		})
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, "")
	defer tr.Close()

	result, err := tr.Request(context.Background(), securityServicePIV, "GeneratePIVCSR",
		&xkmstransport.GeneratePIVCSRRequest{Backend: "file", Slot: "9c", Subject: "CN=test"})
	if err != nil {
		t.Fatalf("GeneratePIVCSR failed: %v", err)
	}
	if gotPath != "/api/v1/xkms/piv/slots/9c/csr" {
		t.Errorf("path = %q", gotPath)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	resp, ok := result.(*xkmstransport.GeneratePIVCSRResponse)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if string(resp.CSR) != "CSR-PEM" {
		t.Errorf("csr = %q", resp.CSR)
	}
}

func TestSecurity_PIV_GenerateCSR_MissingSlot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server must not be called when slot is missing")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tr := newSecurityTestTransport(t, server, "")
	defer tr.Close()

	_, err := tr.Request(context.Background(), securityServicePIV, "GeneratePIVCSR",
		&xkmstransport.GeneratePIVCSRRequest{Backend: "file"})
	if err == nil {
		t.Fatal("expected error for missing slot")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid-input error, got %v", err)
	}
}
