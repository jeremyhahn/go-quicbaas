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
	"encoding/base64"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

var errCeremonyBoom = stderrors.New("ceremony boom")

// fakeCeremonyService is a hand-written test double for service.CeremonyServicer.
type fakeCeremonyService struct {
	initFn           func(ctx context.Context, req *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error)
	statusFn         func(ctx context.Context) (*service.CeremonyStatusInfo, error)
	beginClaimFn     func(ctx context.Context, username string) (*service.CeremonyChallenge, error)
	completeClaimFn  func(ctx context.Context, username, nonceHex, signatureBase64 string) (*service.CeremonyClaimCertResult, error)
	beginShareFn     func(ctx context.Context, username string) (*service.CeremonyChallenge, error)
	completeShareFn  func(ctx context.Context, username, nonceHex string, signature []byte) (*service.CeremonyClaimShareResult, error)
	signCSRFn        func(ctx context.Context, username, soPin, csrPEM, role string) (*service.CeremonySignCSRResult, error)
	enrollExternalFn func(ctx context.Context, req *service.CeremonyEnrollExternalOfficerRequest) (*service.CeremonyEnrollExternalOfficerResponse, error)
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

func (f *fakeCeremonyService) CeremonyBeginClaimCert(ctx context.Context, username string) (*service.CeremonyChallenge, error) {
	if f.beginClaimFn != nil {
		return f.beginClaimFn(ctx, username)
	}
	return &service.CeremonyChallenge{}, nil
}

func (f *fakeCeremonyService) CeremonyCompleteClaimCert(ctx context.Context, username, nonceHex, signatureBase64 string) (*service.CeremonyClaimCertResult, error) {
	if f.completeClaimFn != nil {
		return f.completeClaimFn(ctx, username, nonceHex, signatureBase64)
	}
	return &service.CeremonyClaimCertResult{}, nil
}

func (f *fakeCeremonyService) CeremonyBeginClaimShare(ctx context.Context, username string) (*service.CeremonyChallenge, error) {
	if f.beginShareFn != nil {
		return f.beginShareFn(ctx, username)
	}
	return &service.CeremonyChallenge{}, nil
}

func (f *fakeCeremonyService) CeremonyCompleteClaimShare(ctx context.Context, username, nonceHex string, signature []byte) (*service.CeremonyClaimShareResult, error) {
	if f.completeShareFn != nil {
		return f.completeShareFn(ctx, username, nonceHex, signature)
	}
	return &service.CeremonyClaimShareResult{}, nil
}

func (f *fakeCeremonyService) CeremonySignCSR(ctx context.Context, username, soPin, csrPEM, role string) (*service.CeremonySignCSRResult, error) {
	if f.signCSRFn != nil {
		return f.signCSRFn(ctx, username, soPin, csrPEM, role)
	}
	return &service.CeremonySignCSRResult{}, nil
}

func (f *fakeCeremonyService) CeremonyEnrollExternalOfficer(ctx context.Context, req *service.CeremonyEnrollExternalOfficerRequest) (*service.CeremonyEnrollExternalOfficerResponse, error) {
	if f.enrollExternalFn != nil {
		return f.enrollExternalFn(ctx, req)
	}
	return &service.CeremonyEnrollExternalOfficerResponse{}, nil
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

func TestHandleCeremonyInitialize_BadJSON(t *testing.T) {
	w := doCeremonyReq(t, ceremonyServer(t, &fakeCeremonyService{}), http.MethodPost, "/api/v1/init/initialize", `{bad`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
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

func TestHandleCeremonyClaimCertBegin_OK(t *testing.T) {
	exp := time.Now().Add(time.Minute)
	svc := &fakeCeremonyService{
		beginClaimFn: func(ctx context.Context, username string) (*service.CeremonyChallenge, error) {
			return &service.CeremonyChallenge{NonceHex: "abcd", ExpiresAt: exp}, nil
		},
	}
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/claim-cert/begin", `{"username":"alice"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["nonce_hex"] != "abcd" {
		t.Fatalf("unexpected challenge body: %v", resp)
	}
	if _, ok := resp["expires_at"]; !ok {
		t.Fatalf("expected expires_at: %v", resp)
	}
}

func TestHandleCeremonyClaimCertBegin_MissingUsername(t *testing.T) {
	w := doCeremonyReq(t, ceremonyServer(t, &fakeCeremonyService{}), http.MethodPost, "/api/v1/init/claim-cert/begin", `{"username":""}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleCeremonyClaimCertBegin_ServiceError(t *testing.T) {
	svc := &fakeCeremonyService{
		beginClaimFn: func(ctx context.Context, username string) (*service.CeremonyChallenge, error) {
			return nil, errCeremonyBoom
		},
	}
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/claim-cert/begin", `{"username":"alice"}`)
	if w.Code < 500 {
		t.Fatalf("expected 5xx, got %d", w.Code)
	}
}

// =============================================================================
// Claim-cert complete
// =============================================================================

func TestHandleCeremonyClaimCertComplete_OK(t *testing.T) {
	svc := &fakeCeremonyService{
		completeClaimFn: func(ctx context.Context, username, nonceHex, sig string) (*service.CeremonyClaimCertResult, error) {
			return &service.CeremonyClaimCertResult{CertPEM: []byte("cert"), CACertPEM: []byte("ca")}, nil
		},
	}
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/claim-cert/complete",
		`{"username":"alice","nonce_hex":"ab","signature":"sig"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["cert_pem"] != "cert" || resp["ca_cert_pem"] != "ca" {
		t.Fatalf("unexpected complete body: %v", resp)
	}
}

func TestHandleCeremonyClaimCertComplete_Validation(t *testing.T) {
	svc := &fakeCeremonyService{}
	for _, body := range []string{
		`{"username":"","nonce_hex":"ab","signature":"sig"}`,
		`{"username":"alice","nonce_hex":"","signature":"sig"}`,
		`{"username":"alice","nonce_hex":"ab","signature":""}`,
	} {
		w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/claim-cert/complete", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %s: expected 400, got %d", body, w.Code)
		}
	}
}

// =============================================================================
// Officer share claim handshake (begin / complete)
// =============================================================================

func TestHandleCeremonyBeginClaimShare_OK(t *testing.T) {
	expires := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	svc := &fakeCeremonyService{
		beginShareFn: func(ctx context.Context, username string) (*service.CeremonyChallenge, error) {
			if username != "alice" {
				t.Fatalf("expected alice, got %q", username)
			}
			return &service.CeremonyChallenge{NonceHex: "deadbeef", ExpiresAt: expires}, nil
		},
	}
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/officer-share/begin", `{"username":"alice"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["nonce_hex"] != "deadbeef" {
		t.Fatalf("unexpected begin body: %v", resp)
	}
	if resp["expires_at"] == "" {
		t.Fatalf("expected expires_at, got %v", resp)
	}
}

func TestHandleCeremonyBeginClaimShare_MissingUsername(t *testing.T) {
	w := doCeremonyReq(t, ceremonyServer(t, &fakeCeremonyService{}), http.MethodPost, "/api/v1/init/officer-share/begin", `{}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleCeremonyBeginClaimShare_ServiceError(t *testing.T) {
	svc := &fakeCeremonyService{
		beginShareFn: func(ctx context.Context, username string) (*service.CeremonyChallenge, error) {
			return nil, errCeremonyBoom
		},
	}
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/officer-share/begin", `{"username":"alice"}`)
	if w.Code < 500 {
		t.Fatalf("expected 5xx, got %d", w.Code)
	}
}

func TestHandleCeremonyCompleteClaimShare_OK(t *testing.T) {
	svc := &fakeCeremonyService{
		completeShareFn: func(ctx context.Context, username, nonceHex string, signature []byte) (*service.CeremonyClaimShareResult, error) {
			if username != "alice" || nonceHex != "deadbeef" || string(signature) != "sig" {
				t.Fatalf("unexpected params: %s %s %q", username, nonceHex, signature)
			}
			return &service.CeremonyClaimShareResult{ShareIndex: 2, ShareJSON: `{"share":"v"}`}, nil
		},
	}
	body := `{"username":"alice","nonce_hex":"deadbeef","signature":"` + base64.StdEncoding.EncodeToString([]byte("sig")) + `"}`
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/officer-share/complete", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["share_index"] != float64(2) || resp["share_json"] != `{"share":"v"}` {
		t.Fatalf("unexpected complete body: %v", resp)
	}
}

func TestHandleCeremonyCompleteClaimShare_Validation(t *testing.T) {
	svc := &fakeCeremonyService{}
	for _, body := range []string{
		`{"username":"","nonce_hex":"ab","signature":"c2ln"}`,
		`{"username":"alice","nonce_hex":"","signature":"c2ln"}`,
		`{"username":"alice","nonce_hex":"ab","signature":""}`,
		`{"username":"alice","nonce_hex":"ab","signature":"%%%not-base64%%%"}`,
	} {
		w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/officer-share/complete", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %s: expected 400, got %d", body, w.Code)
		}
	}
}

func TestHandleCeremonyCompleteClaimShare_ServiceError(t *testing.T) {
	svc := &fakeCeremonyService{
		completeShareFn: func(ctx context.Context, username, nonceHex string, signature []byte) (*service.CeremonyClaimShareResult, error) {
			return nil, errCeremonyBoom
		},
	}
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/officer-share/complete", `{"username":"alice","nonce_hex":"ab","signature":"c2ln"}`)
	if w.Code < 500 {
		t.Fatalf("expected 5xx, got %d", w.Code)
	}
}

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

func TestHandleCeremonyEnrollExternalOfficer_OK(t *testing.T) {
	var gotReq *service.CeremonyEnrollExternalOfficerRequest
	svc := &fakeCeremonyService{
		enrollExternalFn: func(ctx context.Context, req *service.CeremonyEnrollExternalOfficerRequest) (*service.CeremonyEnrollExternalOfficerResponse, error) {
			gotReq = req
			return &service.CeremonyEnrollExternalOfficerResponse{SealedShare: "sealed"}, nil
		},
	}
	body := `{"username":"alice","cert_pem":"cert","share_index":2,"auth_factor":"piv"}`
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/enroll-external-officer", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if gotReq.Username != "alice" || gotReq.ShareIndex != 2 || gotReq.AuthFactor != "piv" {
		t.Fatalf("unexpected forwarded request: %+v", gotReq)
	}
	var resp enrollExternalOfficerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SealedShare != "sealed" {
		t.Fatalf("unexpected enroll body: %+v", resp)
	}
}

func TestHandleCeremonyEnrollExternalOfficer_Validation(t *testing.T) {
	svc := &fakeCeremonyService{}
	cases := []struct {
		name string
		body string
	}{
		{"missing username", `{"username":"","cert_pem":"c","auth_factor":"piv"}`},
		{"neither cert nor pubkey", `{"username":"alice","auth_factor":"piv"}`},
		{"both cert and pubkey", `{"username":"alice","cert_pem":"c","pubkey_pem":"p","auth_factor":"piv"}`},
		{"missing auth_factor", `{"username":"alice","cert_pem":"c"}`},
	}
	for _, c := range cases {
		w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/enroll-external-officer", c.body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d", c.name, w.Code)
		}
	}
}

func TestHandleCeremonyEnrollExternalOfficer_ServiceError(t *testing.T) {
	svc := &fakeCeremonyService{
		enrollExternalFn: func(ctx context.Context, req *service.CeremonyEnrollExternalOfficerRequest) (*service.CeremonyEnrollExternalOfficerResponse, error) {
			return nil, errCeremonyBoom
		},
	}
	body := `{"username":"alice","pubkey_pem":"pk","auth_factor":"piv"}`
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/enroll-external-officer", body)
	if w.Code < 500 {
		t.Fatalf("expected 5xx, got %d", w.Code)
	}
}
