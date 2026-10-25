//go:build rest

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
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"fmt"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// errTenantBarrierBoom is a generic (non-QBError) failure used to drive the
// 5xx error branch of every per-tenant barrier handler.
var errTenantBarrierBoom = stderrors.New("tenant barrier boom")

// fakeTenantBarrierService is a hand-written test double for
// service.TenantBarrierServicer. Methods delegate to optional function fields so
// a test can shape each path.
type fakeTenantBarrierService struct {
	service.TenantBarrierServicer
	initFn func(ctx context.Context, tenantID string, passphrase []byte, threshold, shares int, strategy string) (*qrdbtransport.TenantBarrierInitResponse, error)
	// initReq records the request the last TenantBarrierInit call received.
	initReq  *qrdbtransport.TenantBarrierInitRequest
	unsealFn func(ctx context.Context, tenantID string, key []byte) error
	sealFn   func(ctx context.Context, tenantID string) error
	statusFn func(ctx context.Context, tenantID string) (*qrdbtransport.TenantBarrierStatusResponse, error)
}

func (f *fakeTenantBarrierService) TenantBarrierInit(ctx context.Context, req *qrdbtransport.TenantBarrierInitRequest) (*qrdbtransport.TenantBarrierInitResponse, error) {
	f.initReq = req
	if f.initFn != nil {
		return f.initFn(ctx, req.TenantID, req.Passphrase, req.Threshold, req.Shares, req.Strategy)
	}
	return &qrdbtransport.TenantBarrierInitResponse{TenantID: req.TenantID}, nil
}

func (f *fakeTenantBarrierService) TenantBarrierUnseal(ctx context.Context, tenantID string, key []byte) error {
	if f.unsealFn != nil {
		return f.unsealFn(ctx, tenantID, key)
	}
	return nil
}

func (f *fakeTenantBarrierService) TenantBarrierSeal(ctx context.Context, tenantID string) error {
	if f.sealFn != nil {
		return f.sealFn(ctx, tenantID)
	}
	return nil
}

func (f *fakeTenantBarrierService) TenantBarrierStatus(ctx context.Context, tenantID string) (*qrdbtransport.TenantBarrierStatusResponse, error) {
	if f.statusFn != nil {
		return f.statusFn(ctx, tenantID)
	}
	return &qrdbtransport.TenantBarrierStatusResponse{TenantID: tenantID}, nil
}

// tenantBarrierServer installs (or clears) the per-tenant barrier facade on a
// router-backed server.
func tenantBarrierServer(t *testing.T, svc service.TenantBarrierServicer) *Server {
	t.Helper()
	s, _, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	s.SetTenantBarrierService(svc)
	return s
}

// tenantBarrierReq builds a request carrying the chi {tenant_id} URL param so a
// handler can be invoked directly without mounting the IAM router (and its auth
// middleware). An empty tenantID exercises the missing-param branch.
func tenantBarrierReq(method, tenantID, body string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, "/", nil)
	} else {
		r = httptest.NewRequest(method, "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", tenantID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// =============================================================================
// requireTenantBarrierService: every handler fails closed with 503 when the
// facade is not yet wired. A nil facade must NEVER reach qrdb.
// =============================================================================

func TestTenantBarrier_ServiceUnavailable(t *testing.T) {
	s := tenantBarrierServer(t, nil) // facade not wired

	cases := []struct {
		name    string
		method  string
		body    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"init", http.MethodPost, `{}`, s.HandleTenantBarrierInit},
		{"unseal", http.MethodPost, `{}`, s.HandleTenantBarrierUnseal},
		{"seal", http.MethodPost, "", s.HandleTenantBarrierSeal},
		{"status", http.MethodGet, "", s.HandleTenantBarrierStatus},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c.handler(w, tenantBarrierReq(c.method, "tenant-a", c.body))
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected 503, got %d", w.Code)
			}
		})
	}
}

// =============================================================================
// Missing tenant_id: every handler rejects with 400 before touching the facade.
// =============================================================================

func TestTenantBarrier_MissingTenantID(t *testing.T) {
	s := tenantBarrierServer(t, &fakeTenantBarrierService{})

	cases := []struct {
		name    string
		method  string
		body    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"init", http.MethodPost, `{}`, s.HandleTenantBarrierInit},
		{"unseal", http.MethodPost, `{}`, s.HandleTenantBarrierUnseal},
		{"seal", http.MethodPost, "", s.HandleTenantBarrierSeal},
		{"status", http.MethodGet, "", s.HandleTenantBarrierStatus},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c.handler(w, tenantBarrierReq(c.method, "", c.body))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", w.Code)
			}
		})
	}
}

// =============================================================================
// Init: Shamir returns 200 with share-free metadata; non-Shamir returns 204;
// errors are 5xx.
// =============================================================================

func TestTenantBarrierInit_ShamirReturnsMetadata(t *testing.T) {
	svc := &fakeTenantBarrierService{
		initFn: func(_ context.Context, tenantID string, _ []byte, threshold, shares int, _ string) (*qrdbtransport.TenantBarrierInitResponse, error) {
			return &qrdbtransport.TenantBarrierInitResponse{
				TenantID:    tenantID,
				Strategy:    "shamir",
				Threshold:   threshold,
				TotalShares: shares,
				ShareCount:  shares,
			}, nil
		},
	}
	w := httptest.NewRecorder()
	tenantBarrierServer(t, svc).HandleTenantBarrierInit(w,
		tenantBarrierReq(http.MethodPost, "tenant-a", `{"strategy":"shamir","threshold":2,"shares":3}`))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["threshold"].(float64) != 2 || resp["total_shares"].(float64) != 3 || resp["share_count"].(float64) != 3 {
		t.Fatalf("unexpected init body: %v", resp)
	}
	// Share-free contract: raw shares must never appear in the response body.
	if _, leaked := resp["shares"]; leaked {
		t.Fatalf("shares leaked in init body: %v", resp)
	}
}

func TestTenantBarrierInit_NonShamirNoContent(t *testing.T) {
	svc := &fakeTenantBarrierService{
		initFn: func(_ context.Context, tenantID string, _ []byte, _, _ int, _ string) (*qrdbtransport.TenantBarrierInitResponse, error) {
			return &qrdbtransport.TenantBarrierInitResponse{TenantID: tenantID, Strategy: "none"}, nil
		},
	}
	w := httptest.NewRecorder()
	tenantBarrierServer(t, svc).HandleTenantBarrierInit(w,
		tenantBarrierReq(http.MethodPost, "tenant-a", `{"strategy":"none"}`))
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestTenantBarrierInit_BadJSON(t *testing.T) {
	w := httptest.NewRecorder()
	tenantBarrierServer(t, &fakeTenantBarrierService{}).HandleTenantBarrierInit(w,
		tenantBarrierReq(http.MethodPost, "tenant-a", `{bad`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestTenantBarrierInit_ServiceError(t *testing.T) {
	svc := &fakeTenantBarrierService{
		initFn: func(_ context.Context, _ string, _ []byte, _, _ int, _ string) (*qrdbtransport.TenantBarrierInitResponse, error) {
			return nil, errTenantBarrierBoom
		},
	}
	w := httptest.NewRecorder()
	tenantBarrierServer(t, svc).HandleTenantBarrierInit(w,
		tenantBarrierReq(http.MethodPost, "tenant-a", `{"strategy":"none"}`))
	if w.Code < 500 {
		t.Fatalf("expected 5xx, got %d", w.Code)
	}
}

// TestTenantBarrierInit_StatementUnsupportedIs501 pins Codex 6757 finding 1
// on REST: the JSON body cannot carry the go-qrdb Signer, so a raft-isolated
// tenant's init is refused 501 with the typed refusal's text.
func TestTenantBarrierInit_StatementUnsupportedIs501(t *testing.T) {
	refusal := &service.TenantBarrierInitStatementUnsupportedError{TenantID: "tenant-a"}
	svc := &fakeTenantBarrierService{
		initFn: func(_ context.Context, _ string, _ []byte, _, _ int, _ string) (*qrdbtransport.TenantBarrierInitResponse, error) {
			return nil, refusal
		},
	}
	w := httptest.NewRecorder()
	tenantBarrierServer(t, svc).HandleTenantBarrierInit(w,
		tenantBarrierReq(http.MethodPost, "tenant-a", `{"strategy":"none"}`))
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "raft-isolated") {
		t.Fatalf("expected the refusal text, got %s", w.Body.String())
	}
}

// The inline Shamir roster and its policy in the body reach the service in
// go-qrdb's request type, the URL names the tenant, and the holder
// certificates go-qrdb issues come back (Codex 6709 P1).
func TestTenantBarrierInit_ForwardsOfficersAndPolicy(t *testing.T) {
	svc := &fakeTenantBarrierService{
		initFn: func(_ context.Context, tenantID string, _ []byte, threshold, shares int, _ string) (*qrdbtransport.TenantBarrierInitResponse, error) {
			return &qrdbtransport.TenantBarrierInitResponse{
				TenantID:    tenantID,
				Strategy:    "shamir",
				Threshold:   threshold,
				TotalShares: shares,
				Holders:     []qrdbtransport.TenantBarrierHolderCert{{Username: "officer-1", Role: "unseal", CertPEM: "cert-1"}},
			}, nil
		},
	}
	body := `{"tenant_id":"tenant-b","strategy":"shamir","threshold":1,"shares":1,"policy":"any-enrolled",` +
		`"officers":[{"username":"officer-1","role":"unseal","csr_pem":"csr-1","factors":[{"name":"mtls"}],"designated":"mtls"}]}`
	w := httptest.NewRecorder()
	tenantBarrierServer(t, svc).HandleTenantBarrierInit(w, tenantBarrierReq(http.MethodPost, "tenant-a", body))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	got := svc.initReq
	if got == nil {
		t.Fatal("service received no init request")
	}
	if got.TenantID != "tenant-a" {
		t.Errorf("tenant = %q, want the URL's tenant-a", got.TenantID)
	}
	if got.Policy != "any-enrolled" {
		t.Errorf("policy = %q, want any-enrolled", got.Policy)
	}
	want := []qrdbtransport.OfficerRegisterRequest{{
		Username: "officer-1", Role: "unseal", CSRPEM: "csr-1",
		Factors:    []qrdbtransport.HolderFactorEnrolment{{Name: "mtls"}},
		Designated: "mtls",
	}}
	if !reflect.DeepEqual(got.Officers, want) {
		t.Errorf("officers = %+v, want %+v", got.Officers, want)
	}

	var resp qrdbtransport.TenantBarrierInitResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Holders) != 1 || resp.Holders[0].CertPEM != "cert-1" {
		t.Errorf("holder certificates = %+v, want officer-1's", resp.Holders)
	}
}

// =============================================================================
// Unseal
// =============================================================================

func TestTenantBarrierUnseal_OK(t *testing.T) {
	var gotKey []byte
	svc := &fakeTenantBarrierService{
		// Copy: the handler wipes the key once the call returns.
		unsealFn: func(_ context.Context, _ string, key []byte) error {
			gotKey = append([]byte(nil), key...)
			return nil
		},
	}
	w := httptest.NewRecorder()
	tenantBarrierServer(t, svc).HandleTenantBarrierUnseal(w,
		tenantBarrierReq(http.MethodPost, "tenant-a", `{"key":"YWJj"}`)) // base64("abc")
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if string(gotKey) != "abc" {
		t.Fatalf("expected key to be forwarded, got %q", string(gotKey))
	}
}

func TestTenantBarrierUnseal_ServiceError(t *testing.T) {
	svc := &fakeTenantBarrierService{
		unsealFn: func(_ context.Context, _ string, _ []byte) error { return errTenantBarrierBoom },
	}
	w := httptest.NewRecorder()
	tenantBarrierServer(t, svc).HandleTenantBarrierUnseal(w,
		tenantBarrierReq(http.MethodPost, "tenant-a", `{}`))
	if w.Code < 500 {
		t.Fatalf("expected 5xx, got %d", w.Code)
	}
}

// =============================================================================
// Unseal-share
// =============================================================================

// =============================================================================
// Seal
// =============================================================================

func TestTenantBarrierSeal_OK(t *testing.T) {
	svc := &fakeTenantBarrierService{sealFn: func(_ context.Context, _ string) error { return nil }}
	w := httptest.NewRecorder()
	tenantBarrierServer(t, svc).HandleTenantBarrierSeal(w, tenantBarrierReq(http.MethodPost, "tenant-a", ""))
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestTenantBarrierSeal_ServiceError(t *testing.T) {
	svc := &fakeTenantBarrierService{sealFn: func(_ context.Context, _ string) error { return errTenantBarrierBoom }}
	w := httptest.NewRecorder()
	tenantBarrierServer(t, svc).HandleTenantBarrierSeal(w, tenantBarrierReq(http.MethodPost, "tenant-a", ""))
	if w.Code < 500 {
		t.Fatalf("expected 5xx, got %d", w.Code)
	}
}

// =============================================================================
// Status
// =============================================================================

func TestTenantBarrierStatus_OK(t *testing.T) {
	svc := &fakeTenantBarrierService{
		statusFn: func(_ context.Context, tenantID string) (*qrdbtransport.TenantBarrierStatusResponse, error) {
			return &qrdbtransport.TenantBarrierStatusResponse{TenantID: tenantID, Sealed: true, Initialized: true}, nil
		},
	}
	w := httptest.NewRecorder()
	tenantBarrierServer(t, svc).HandleTenantBarrierStatus(w, tenantBarrierReq(http.MethodGet, "tenant-a", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "tenant-a") {
		t.Fatalf("expected tenant id in body: %s", w.Body.String())
	}
}

func TestTenantBarrierStatus_ServiceError(t *testing.T) {
	svc := &fakeTenantBarrierService{
		statusFn: func(_ context.Context, _ string) (*qrdbtransport.TenantBarrierStatusResponse, error) {
			return nil, errTenantBarrierBoom
		},
	}
	w := httptest.NewRecorder()
	tenantBarrierServer(t, svc).HandleTenantBarrierStatus(w, tenantBarrierReq(http.MethodGet, "tenant-a", ""))
	if w.Code < 500 {
		t.Fatalf("expected 5xx, got %d", w.Code)
	}
}

// =============================================================================
// qrdb error canon: tenant.ErrBarrierNotEnabled (per-tenant barrier verb on a
// node whose seal subsystem is disabled) maps to 422 Unprocessable Entity,
// non-retryable, with the canonical message preserved
// (handleTenantBarrierError). Both the bare sentinel and a %w-wrapped chain
// must classify identically.
// =============================================================================

// barrierNotEnabledCases enumerates every tenant-barrier verb that delegates
// errors through handleTenantBarrierError, paired with a fake that fails the
// verb with the supplied error.
func barrierNotEnabledCases(s *Server, failErr error) []struct {
	name    string
	method  string
	body    string
	handler func(http.ResponseWriter, *http.Request)
	svc     *fakeTenantBarrierService
} {
	return []struct {
		name    string
		method  string
		body    string
		handler func(http.ResponseWriter, *http.Request)
		svc     *fakeTenantBarrierService
	}{
		{"init", http.MethodPost, `{"strategy":"passphrase","passphrase":"cA=="}`, s.HandleTenantBarrierInit,
			&fakeTenantBarrierService{initFn: func(context.Context, string, []byte, int, int, string) (*qrdbtransport.TenantBarrierInitResponse, error) {
				return nil, failErr
			}}},
		{"unseal", http.MethodPost, `{"key":"YWJj"}`, s.HandleTenantBarrierUnseal,
			&fakeTenantBarrierService{unsealFn: func(context.Context, string, []byte) error { return failErr }}},
		{"seal", http.MethodPost, "", s.HandleTenantBarrierSeal,
			&fakeTenantBarrierService{sealFn: func(context.Context, string) error { return failErr }}},
		{"status", http.MethodGet, "", s.HandleTenantBarrierStatus,
			&fakeTenantBarrierService{statusFn: func(context.Context, string) (*qrdbtransport.TenantBarrierStatusResponse, error) {
				return nil, failErr
			}}},
	}
}

// requireBarrierNotEnabled422 asserts the canonical 422 response contract:
// non-retryable, message preserved.
func requireBarrierNotEnabled422(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Error     string `json:"error"`
		Retryable bool   `json:"retryable"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode 422 body: %v; raw=%s", err, w.Body.String())
	}
	if !strings.Contains(resp.Error, "barrier not enabled (seal subsystem disabled)") {
		t.Fatalf("422 body must preserve the canonical message; got %q", resp.Error)
	}
	if resp.Retryable {
		t.Fatalf("422 barrier-not-enabled response must be non-retryable; body=%s", w.Body.String())
	}
}

func TestTenantBarrier_BarrierNotEnabled_Maps422(t *testing.T) {
	s := tenantBarrierServer(t, nil)
	for _, c := range barrierNotEnabledCases(s, qrdbsdk.ErrTenantBarrierNotEnabled) {
		t.Run(c.name, func(t *testing.T) {
			s.SetTenantBarrierService(c.svc)
			w := httptest.NewRecorder()
			c.handler(w, tenantBarrierReq(c.method, "tenant-a", c.body))
			requireBarrierNotEnabled422(t, w)
		})
	}
}

func TestTenantBarrier_BarrierNotEnabledWrapped_Maps422(t *testing.T) {
	// A %w-wrapped chain (as produced by intermediate layers) must classify
	// identically to the bare sentinel via errors.Is traversal.
	wrapped := fmt.Errorf("tenant barrier init: %w", qrdbsdk.ErrTenantBarrierNotEnabled)
	s := tenantBarrierServer(t, nil)
	for _, c := range barrierNotEnabledCases(s, wrapped) {
		t.Run(c.name, func(t *testing.T) {
			s.SetTenantBarrierService(c.svc)
			w := httptest.NewRecorder()
			c.handler(w, tenantBarrierReq(c.method, "tenant-a", c.body))
			requireBarrierNotEnabled422(t, w)
		})
	}
}

// TestTenantBarrier_GenericErrorStill500 pins that the 422 classification is
// scoped to the barrier-not-enabled sentinel: an unrelated error keeps the
// generic handleServiceError classification (500 for a plain error).
func TestTenantBarrier_GenericErrorStill500(t *testing.T) {
	s := tenantBarrierServer(t, &fakeTenantBarrierService{
		sealFn: func(context.Context, string) error { return errTenantBarrierBoom },
	})
	w := httptest.NewRecorder()
	s.HandleTenantBarrierSeal(w, tenantBarrierReq(http.MethodPost, "tenant-a", ""))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for a generic error, got %d", w.Code)
	}
}

// TestTenantBarrierEnableEncryption_BarrierNotEnabled_Maps422 covers the
// enable-encryption verb, which delegates through the system BarrierServicer
// rather than the per-tenant facade but shares handleTenantBarrierError.
func TestTenantBarrierEnableEncryption_BarrierNotEnabled_Maps422(t *testing.T) {
	svc := &fakeBarrierService{
		enableEncryptionFn: func(context.Context, string, qrdbsdk.MigrationOptions) error {
			return qrdbsdk.ErrTenantBarrierNotEnabled
		},
	}
	s := barrierServer(t, svc)
	w := httptest.NewRecorder()
	s.HandleTenantBarrierEnableEncryption(w,
		tenantBarrierReq(http.MethodPost, "tenant-a", `{"target_strategy":"passphrase","passphrase":"cA=="}`))
	requireBarrierNotEnabled422(t, w)
}
