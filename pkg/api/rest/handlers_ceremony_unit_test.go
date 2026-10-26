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
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

var errCeremonyBoom = stderrors.New("ceremony boom")

// fakeCeremonyService is a hand-written test double for service.CeremonyServicer.
type fakeCeremonyService struct {
	initFn    func(ctx context.Context, req *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error)
	statusFn  func(ctx context.Context) (*service.CeremonyStatusInfo, error)
	signCSRFn func(ctx context.Context, username, soPin, csrPEM, role string) (*service.CeremonySignCSRResult, error)
}

func (f *fakeCeremonyService) CeremonyInitialize(ctx context.Context, req *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error) {
	if f.initFn != nil {
		return f.initFn(ctx, req)
	}
	return &service.CeremonyInitInfo{}, nil
}

func (f *fakeCeremonyService) CeremonyStatus(ctx context.Context) (*service.CeremonyStatusInfo, error) {
	if f.statusFn != nil {
		return f.statusFn(ctx)
	}
	return &service.CeremonyStatusInfo{}, nil
}

func (f *fakeCeremonyService) CeremonySignCSR(ctx context.Context, username, soPin, csrPEM, role string) (*service.CeremonySignCSRResult, error) {
	if f.signCSRFn != nil {
		return f.signCSRFn(ctx, username, soPin, csrPEM, role)
	}
	return &service.CeremonySignCSRResult{}, nil
}

func ceremonyServer(t *testing.T, svc service.CeremonyServicer) *Server {
	t.Helper()
	s, _, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	s.SetCeremonyService(svc)
	return s
}

func doCeremonyReq(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, r)
	return w
}

// =============================================================================
// Initialize
// =============================================================================

func TestHandleCeremonyInitialize_OK(t *testing.T) {
	var gotReq *service.CeremonyInitRequest
	svc := &fakeCeremonyService{
		initFn: func(ctx context.Context, req *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error) {
			gotReq = req
			return &service.CeremonyInitInfo{
				SPKIPin:    "pin",
				CACertPEM:  []byte("ca"),
				TLSCertPEM: []byte("tls"),
				State:      "initialized",
				ShareCount: 3,
			}, nil
		},
	}
	body := `{"so_pin":"1234","user_pin":"5678","threshold":2,"officers":[{"username":"a","csr_pem":"csr"}],"hostname":"h","sans":["x"]}`
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/initialize", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if gotReq.Threshold != 2 || len(gotReq.Officers) != 1 || gotReq.Officers[0].Username != "a" {
		t.Fatalf("unexpected forwarded request: %+v", gotReq)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["spki_pin"] != "pin" {
		t.Fatalf("unexpected init body: %v", resp)
	}
	// The SO identity is CSR-based; the server never returns SO certificate
	// or key material.
	if _, present := resp["so_cert_pem"]; present {
		t.Fatalf("SO certificate leaked into REST response: %v", resp)
	}
	if _, present := resp["so_key_pem"]; present {
		t.Fatalf("SO private key leaked into REST response: %v", resp)
	}
}

func TestHandleCeremonyInitialize_OmitsEmptySOFields(t *testing.T) {
	svc := &fakeCeremonyService{
		initFn: func(ctx context.Context, req *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error) {
			return &service.CeremonyInitInfo{SPKIPin: "pin", State: "initialized"}, nil
		},
	}
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/initialize", `{"so_pin":"x","user_pin":"y","threshold":1}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "so_cert_pem") || strings.Contains(w.Body.String(), "so_key_pem") {
		t.Fatalf("empty SO fields should be omitted: %s", w.Body.String())
	}
}

// TestHandleCeremonyInitialize_DeliversIssuedCredentials pins Codex 6800 P1:
// the generated admin key, the admin certificate and the holder certificates
// reach the caller in go-qrdb's own response shape, the one the SDK decodes.
func TestHandleCeremonyInitialize_DeliversIssuedCredentials(t *testing.T) {
	holders := []transport.CeremonyHolderCert{{Username: "carol", Role: "custodian", CertPEM: "carol-cert"}}
	svc := &fakeCeremonyService{
		initFn: func(ctx context.Context, req *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error) {
			return &service.CeremonyInitInfo{
				State:                 "initialized",
				Holders:               holders,
				AdminCertPEM:          []byte("admin-cert"),
				AdminKeyPEM:           []byte("admin-key"),
				AdminCredentialSource: "generated",
			}, nil
		},
	}
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/initialize", `{"admin_generate":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp transport.CeremonyInitializeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(resp.AdminKeyPEM) != "admin-key" || resp.AdminCertPEM != "admin-cert" || resp.AdminCredentialSource != "generated" {
		t.Fatalf("admin credential not delivered: %+v", resp)
	}
	if len(resp.Holders) != 1 || resp.Holders[0] != holders[0] {
		t.Fatalf("holder certificates not delivered: %+v", resp.Holders)
	}
}

// TestHandleCeremonyInitialize_ForwardsEveryOfficerField pins that the REST
// payload reaches the service as go-qrdb's own officer request, policy
// included.
func TestHandleCeremonyInitialize_ForwardsEveryOfficerField(t *testing.T) {
	var gotReq *service.CeremonyInitRequest
	svc := &fakeCeremonyService{
		initFn: func(ctx context.Context, req *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error) {
			gotReq = req
			return &service.CeremonyInitInfo{}, nil
		},
	}
	body := `{"threshold":2,"policy":"any-enrolled","admin_generate":true,"officers":[{"username":"carol","role":"custodian","csr_pem":"csr","encryption_kem":"dhkem-x25519","encryption_key":"AQID","factors":[{"name":"totp","params":{"digits":6}}],"designated":"totp"}]}`
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/initialize", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	want := service.CeremonyOfficer{
		Username:      "carol",
		Role:          "custodian",
		CSRPEM:        "csr",
		EncryptionKEM: "dhkem-x25519",
		EncryptionKey: []byte{1, 2, 3},
		Factors:       []transport.HolderFactorEnrolment{{Name: "totp", Params: map[string]any{"digits": float64(6)}}},
		Designated:    "totp",
	}
	if gotReq.Policy != "any-enrolled" || len(gotReq.Officers) != 1 || !reflect.DeepEqual(gotReq.Officers[0], want) {
		t.Fatalf("officer request narrowed: policy=%q officers=%+v", gotReq.Policy, gotReq.Officers)
	}
}

// TestHandleCeremonyInitialize_RefusesUndecodableOfficer pins that an officer
// whose encryption key is not base64 is refused before the service is called.
func TestHandleCeremonyInitialize_RefusesUndecodableOfficer(t *testing.T) {
	called := false
	svc := &fakeCeremonyService{
		initFn: func(ctx context.Context, req *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error) {
			called = true
			return &service.CeremonyInitInfo{}, nil
		},
	}
	body := `{"admin_generate":true,"officers":[{"username":"carol","csr_pem":"csr","encryption_key":"not base64!"}]}`
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/initialize", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if called {
		t.Fatal("service called with an undecodable officer")
	}
}

func TestHandleCeremonyInitialize_BadJSON(t *testing.T) {
	w := doCeremonyReq(t, ceremonyServer(t, &fakeCeremonyService{}), http.MethodPost, "/api/v1/init/initialize", `{bad`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// TestHandleCeremonyInitialize_PartialDecodeWipesSecret pins Codex 7395 P2: a
// body whose secret decodes and whose threshold does not is refused, and the
// cleanup the handler registers before the decode zeroes the secret the
// partial decode produced.
func TestHandleCeremonyInitialize_PartialDecodeWipesSecret(t *testing.T) {
	const body = `{"secret":"aHVudGVyMg==","threshold":"two"}`
	called := false
	svc := &fakeCeremonyService{
		initFn: func(ctx context.Context, req *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error) {
			called = true
			return &service.CeremonyInitInfo{}, nil
		},
	}
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/initialize", body)
	if w.Code != http.StatusBadRequest || called {
		t.Fatalf("expected 400 without a service call, got %d (called=%v)", w.Code, called)
	}

	var req transport.CeremonyInitializeRequest
	cleanup := wipeCredential(&req.Secret)
	if err := json.Unmarshal([]byte(body), &req); err == nil {
		t.Fatal("precondition: this body must fail to decode")
	}
	if string(req.Secret) != "hunter2" {
		t.Fatalf("precondition: a partial decode must populate the secret, got %q", req.Secret)
	}
	cleanup()
	for i, b := range req.Secret {
		if b != 0 {
			t.Fatalf("secret byte %d survived the cleanup", i)
		}
	}
}

func TestHandleCeremonyInitialize_ServiceError(t *testing.T) {
	svc := &fakeCeremonyService{
		initFn: func(ctx context.Context, req *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error) {
			return nil, errCeremonyBoom
		},
	}
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/initialize", `{"so_pin":"x","user_pin":"y","threshold":1}`)
	if w.Code < 500 {
		t.Fatalf("expected 5xx, got %d", w.Code)
	}
}

// =============================================================================
// Status
// =============================================================================

func TestHandleCeremonyStatus_OK(t *testing.T) {
	svc := &fakeCeremonyService{
		statusFn: func(ctx context.Context) (*service.CeremonyStatusInfo, error) {
			return &service.CeremonyStatusInfo{State: "ready", SPKIPin: "pin"}, nil
		},
	}
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodGet, "/api/v1/init/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["state"] != "ready" || resp["spki_pin"] != "pin" {
		t.Fatalf("unexpected status body: %v", resp)
	}
}

func TestHandleCeremonyStatus_OmitsEmptyPin(t *testing.T) {
	svc := &fakeCeremonyService{
		statusFn: func(ctx context.Context) (*service.CeremonyStatusInfo, error) {
			return &service.CeremonyStatusInfo{State: "uninitialized"}, nil
		},
	}
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodGet, "/api/v1/init/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "spki_pin") {
		t.Fatalf("empty pin should be omitted: %s", w.Body.String())
	}
}

func TestHandleCeremonyStatus_ServiceError(t *testing.T) {
	svc := &fakeCeremonyService{
		statusFn: func(ctx context.Context) (*service.CeremonyStatusInfo, error) { return nil, errCeremonyBoom },
	}
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodGet, "/api/v1/init/status", "")
	if w.Code < 500 {
		t.Fatalf("expected 5xx, got %d", w.Code)
	}
}

// =============================================================================
// Claim-cert begin
// =============================================================================

// =============================================================================
// Claim-cert complete
// =============================================================================

// =============================================================================
// Officer share claim handshake (begin / complete)
// =============================================================================

// =============================================================================
// Sign-CSR
// =============================================================================

func TestHandleCeremonySignCSR_OK(t *testing.T) {
	svc := &fakeCeremonyService{
		signCSRFn: func(ctx context.Context, username, soPin, csrPEM, role string) (*service.CeremonySignCSRResult, error) {
			return &service.CeremonySignCSRResult{CertPEM: []byte("signed")}, nil
		},
	}
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/sign-csr",
		`{"username":"alice","so_pin":"1234","csr_pem":"csr","role":"officer"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["cert_pem"] != "signed" {
		t.Fatalf("unexpected sign body: %v", resp)
	}
}

func TestHandleCeremonySignCSR_Validation(t *testing.T) {
	svc := &fakeCeremonyService{}
	for _, body := range []string{
		`{"username":"","csr_pem":"csr"}`,
		`{"username":"alice","csr_pem":""}`,
	} {
		w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/sign-csr", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %s: expected 400, got %d", body, w.Code)
		}
	}
}

// =============================================================================
// List officers (always empty roster)
// =============================================================================

func TestHandleCeremonyListOfficers_EmptyRoster(t *testing.T) {
	w := doCeremonyReq(t, ceremonyServer(t, &fakeCeremonyService{}), http.MethodGet, "/api/v1/init/officers", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	officers, ok := resp["officers"].([]interface{})
	if !ok || len(officers) != 0 {
		t.Fatalf("expected empty officers array, got %v", resp)
	}
}

// =============================================================================
// Enroll external officer
// =============================================================================
