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
	initFn        func(ctx context.Context, tenantID string, passphrase []byte, threshold, shares int, strategy string) (*qrdbtransport.TenantBarrierInitResponse, error)
	unsealFn      func(ctx context.Context, tenantID string, key []byte) error
	unsealShareFn func(ctx context.Context, tenantID, share string) (*qrdbtransport.TenantBarrierUnsealProgress, error)
	sealFn        func(ctx context.Context, tenantID string) error
	statusFn      func(ctx context.Context, tenantID string) (*qrdbtransport.TenantBarrierStatusResponse, error)
}

func (f *fakeTenantBarrierService) TenantBarrierInit(ctx context.Context, tenantID string, passphrase []byte, threshold, shares int, strategy string) (*qrdbtransport.TenantBarrierInitResponse, error) {
	if f.initFn != nil {
		return f.initFn(ctx, tenantID, passphrase, threshold, shares, strategy)
	}
	return &qrdbtransport.TenantBarrierInitResponse{TenantID: tenantID}, nil
}

func (f *fakeTenantBarrierService) TenantBarrierUnseal(ctx context.Context, tenantID string, key []byte) error {
	if f.unsealFn != nil {
		return f.unsealFn(ctx, tenantID, key)
	}
	return nil
}

func (f *fakeTenantBarrierService) TenantBarrierUnsealShare(ctx context.Context, tenantID, share string) (*qrdbtransport.TenantBarrierUnsealProgress, error) {
	if f.unsealShareFn != nil {
		return f.unsealShareFn(ctx, tenantID, share)
	}
	return &qrdbtransport.TenantBarrierUnsealProgress{TenantID: tenantID}, nil
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
		{"unseal-share", http.MethodPost, `{"share":"x"}`, s.HandleTenantBarrierUnsealShare},
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
		{"unseal-share", http.MethodPost, `{"share":"x"}`, s.HandleTenantBarrierUnsealShare},
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

func TestTenantBarrierUnsealShare_OK(t *testing.T) {
	svc := &fakeTenantBarrierService{
		unsealShareFn: func(_ context.Context, tenantID, _ string) (*qrdbtransport.TenantBarrierUnsealProgress, error) {
			return &qrdbtransport.TenantBarrierUnsealProgress{TenantID: tenantID, Required: 3, Submitted: 1}, nil
		},
	}
	w := httptest.NewRecorder()
	tenantBarrierServer(t, svc).HandleTenantBarrierUnsealShare(w,
		tenantBarrierReq(http.MethodPost, "tenant-a", `{"share":"abc"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestTenantBarrierUnsealShare_MissingShare(t *testing.T) {
	w := httptest.NewRecorder()
	tenantBarrierServer(t, &fakeTenantBarrierService{}).HandleTenantBarrierUnsealShare(w,
		tenantBarrierReq(http.MethodPost, "tenant-a", `{"share":""}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestTenantBarrierUnsealShare_ServiceError(t *testing.T) {
	svc := &fakeTenantBarrierService{
		unsealShareFn: func(_ context.Context, _, _ string) (*qrdbtransport.TenantBarrierUnsealProgress, error) {
			return nil, errTenantBarrierBoom
		},
	}
	w := httptest.NewRecorder()
	tenantBarrierServer(t, svc).HandleTenantBarrierUnsealShare(w,
		tenantBarrierReq(http.MethodPost, "tenant-a", `{"share":"abc"}`))
	if w.Code < 500 {
		t.Fatalf("expected 5xx, got %d", w.Code)
	}
}

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
		{"unseal-share", http.MethodPost, `{"share":"x"}`, s.HandleTenantBarrierUnsealShare,
			&fakeTenantBarrierService{unsealShareFn: func(context.Context, string, string) (*qrdbtransport.TenantBarrierUnsealProgress, error) {
				return nil, failErr
			}}},
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
