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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// HandleBarrierEnableEncryption — system-scope (POST /api/v1/barrier/enable-encryption)
// =============================================================================

// TestHandleBarrierEnableEncryption_ServiceUnavailable pins the 503 contract
// when the barrier facade is not wired.
func TestHandleBarrierEnableEncryption_ServiceUnavailable(t *testing.T) {
	s, _, cleanup := setupMockServer(t)
	defer cleanup()
	// barrierSvc is nil by default — don't call SetBarrierService.
	w := doBarrierReq(t, s, http.MethodPost, "/api/v1/barrier/enable-encryption",
		`{"target_strategy":"passphrase","passphrase":"eA=="}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// TestHandleBarrierEnableEncryption_MissingTargetStrategy asserts that an
// empty target_strategy yields a 400 before touching the service.
func TestHandleBarrierEnableEncryption_MissingTargetStrategy(t *testing.T) {
	w := doBarrierReq(t, barrierServer(t, &fakeBarrierService{}),
		http.MethodPost, "/api/v1/barrier/enable-encryption", `{"passphrase":"eA=="}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// TestHandleBarrierEnableEncryption_RejectsBodyTenantID pins item 3 of
// go-quicbaas#990 on the quicbaas-owned route: its scope is the path (this one
// is the system barrier; the tenant route carries the tenant in its path), so
// a body tenant_id must be refused rather than silently dropped — dropping it
// migrated the SYSTEM barrier for a caller who asked for a tenant's.
func TestHandleBarrierEnableEncryption_RejectsBodyTenantID(t *testing.T) {
	called := false
	svc := &fakeBarrierService{
		enableEncryptionFn: func(context.Context, string, qrdbsdk.MigrationOptions) error {
			called = true
			return nil
		},
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost,
		"/api/v1/barrier/enable-encryption",
		`{"target_strategy":"shamir","tenant_id":"acme","shamir_threshold":2,"shamir_total":3}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "tenant_id") {
		t.Errorf("the 400 should name tenant_id, body=%s", w.Body.String())
	}
	if called {
		t.Error("the system barrier was migrated for a request that named a tenant")
	}
}

// TestHandleBarrierEnableEncryption_BadJSON asserts that malformed JSON yields 400.
func TestHandleBarrierEnableEncryption_BadJSON(t *testing.T) {
	w := doBarrierReq(t, barrierServer(t, &fakeBarrierService{}),
		http.MethodPost, "/api/v1/barrier/enable-encryption", `{bad`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// TestHandleBarrierEnableEncryption_OK asserts the system-scope success path:
// the handler returns 204, passes an empty tenant scope to EnableEncryption,
// and forwards the parsed migration options.
func TestHandleBarrierEnableEncryption_OK(t *testing.T) {
	var gotTenantID string
	var gotOpts qrdbsdk.MigrationOptions
	var passphraseMatched bool
	svc := &fakeBarrierService{
		enableEncryptionFn: func(_ context.Context, tenantID string, opts qrdbsdk.MigrationOptions) error {
			gotTenantID = tenantID
			gotOpts = opts
			// The credential is protected memory the handler destroys once
			// this call returns, so it can only be compared from in here.
			if opts.Credentials.Secret != nil {
				plain, err := opts.Credentials.Secret.Open()
				if err != nil {
					t.Errorf("open the forwarded credential: %v", err)
					return nil
				}
				defer plain.Destroy()
				passphraseMatched = plain.EqualTo([]byte("s3cr3t"))
			}
			return nil
		},
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost,
		"/api/v1/barrier/enable-encryption",
		`{"target_strategy":"passphrase","passphrase":"czNjcjN0"}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (body=%s)", w.Code, w.Body.String())
	}
	// System barrier migration must carry an empty tenant scope.
	if gotTenantID != "" {
		t.Errorf("expected empty tenant ID for system scope, got %q", gotTenantID)
	}
	if gotOpts.TargetStrategy != qrdbsdk.StrategyID("passphrase") {
		t.Errorf("target_strategy: got %q, want %q", gotOpts.TargetStrategy, "passphrase")
	}
	if !passphraseMatched {
		t.Error("passphrase not forwarded to the migration credentials")
	}
}

// TestHandleBarrierEnableEncryption_DefaultsApplied asserts that when
// mechanism and availability are absent the handler supplies "swap" and
// "online" defaults before forwarding to the service.
func TestHandleBarrierEnableEncryption_DefaultsApplied(t *testing.T) {
	var gotOpts qrdbsdk.MigrationOptions
	svc := &fakeBarrierService{
		enableEncryptionFn: func(_ context.Context, _ string, opts qrdbsdk.MigrationOptions) error {
			gotOpts = opts
			return nil
		},
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost,
		"/api/v1/barrier/enable-encryption",
		`{"target_strategy":"passphrase","passphrase":"eA=="}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if gotOpts.Mechanism != qrdbsdk.MigrationMechanism("swap") {
		t.Errorf("mechanism default: got %q, want %q", gotOpts.Mechanism, "swap")
	}
	if gotOpts.Availability != qrdbsdk.MigrationAvailability("online") {
		t.Errorf("availability default: got %q, want %q", gotOpts.Availability, "online")
	}
}

// TestHandleBarrierEnableEncryption_ServiceError asserts a generic service error
// results in a 5xx response.
func TestHandleBarrierEnableEncryption_ServiceError(t *testing.T) {
	svc := &fakeBarrierService{
		enableEncryptionFn: func(_ context.Context, _ string, _ qrdbsdk.MigrationOptions) error {
			return errBarrierBoom
		},
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost,
		"/api/v1/barrier/enable-encryption",
		`{"target_strategy":"passphrase","passphrase":"eA=="}`)
	if w.Code < 500 {
		t.Fatalf("expected 5xx, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// TestHandleBarrierEnableEncryption_ForbiddenMapsTo403 asserts that a
// QBError with ErrForbidden code (e.g. from an EntitlementError via the
// IAM mapping layer) is translated to HTTP 403 by handleServiceError.
func TestHandleBarrierEnableEncryption_ForbiddenMapsTo403(t *testing.T) {
	svc := &fakeBarrierService{
		enableEncryptionFn: func(_ context.Context, _ string, _ qrdbsdk.MigrationOptions) error {
			return &dberrors.QBError{Code: dberrors.ErrForbidden, Err: errBarrierBoom}
		},
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost,
		"/api/v1/barrier/enable-encryption",
		`{"target_strategy":"passphrase","passphrase":"eA=="}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// =============================================================================
// HandleTenantBarrierEnableEncryption — tenant-scope
// (POST /api/v1/tenants/{tenant_id}/barrier/enable-encryption)
// =============================================================================

// TestHandleTenantBarrierEnableEncryption_ServiceUnavailable pins the 503
// contract when the barrier facade is not wired (nil barrierSvc).
func TestHandleTenantBarrierEnableEncryption_ServiceUnavailable(t *testing.T) {
	s, _, cleanup := setupMockServer(t)
	defer cleanup()
	// barrierSvc is nil by default — no SetBarrierService call.
	w := httptest.NewRecorder()
	s.HandleTenantBarrierEnableEncryption(w,
		tenantBarrierReq(http.MethodPost, "tenant-a",
			`{"target_strategy":"passphrase","passphrase":"eA=="}`))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// TestHandleTenantBarrierEnableEncryption_MissingTenantID asserts that an
// empty tenant_id URL param yields 400.
func TestHandleTenantBarrierEnableEncryption_MissingTenantID(t *testing.T) {
	w := httptest.NewRecorder()
	barrierServer(t, &fakeBarrierService{}).HandleTenantBarrierEnableEncryption(w,
		tenantBarrierReq(http.MethodPost, "",
			`{"target_strategy":"passphrase","passphrase":"eA=="}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// TestHandleTenantBarrierEnableEncryption_MissingTargetStrategy asserts that
// an absent target_strategy yields 400 before calling the service.
func TestHandleTenantBarrierEnableEncryption_MissingTargetStrategy(t *testing.T) {
	w := httptest.NewRecorder()
	barrierServer(t, &fakeBarrierService{}).HandleTenantBarrierEnableEncryption(w,
		tenantBarrierReq(http.MethodPost, "tenant-a", `{"passphrase":"eA=="}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// TestHandleTenantBarrierEnableEncryption_OK asserts the tenant-scope success
// path: returns 204 and forwards the tenant_id to EnableEncryption.
func TestHandleTenantBarrierEnableEncryption_OK(t *testing.T) {
	var gotTenantID string
	svc := &fakeBarrierService{
		enableEncryptionFn: func(_ context.Context, tenantID string, _ qrdbsdk.MigrationOptions) error {
			gotTenantID = tenantID
			return nil
		},
	}
	w := httptest.NewRecorder()
	barrierServer(t, svc).HandleTenantBarrierEnableEncryption(w,
		tenantBarrierReq(http.MethodPost, "tenant-xyz",
			`{"target_strategy":"passphrase","passphrase":"eA=="}`))
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (body=%s)", w.Code, w.Body.String())
	}
	if gotTenantID != "tenant-xyz" {
		t.Errorf("tenant_id forwarded: got %q, want %q", gotTenantID, "tenant-xyz")
	}
}

// TestHandleTenantBarrierEnableEncryption_ServiceError asserts a generic
// service error results in a 5xx response.
func TestHandleTenantBarrierEnableEncryption_ServiceError(t *testing.T) {
	svc := &fakeBarrierService{
		enableEncryptionFn: func(_ context.Context, _ string, _ qrdbsdk.MigrationOptions) error {
			return errBarrierBoom
		},
	}
	w := httptest.NewRecorder()
	barrierServer(t, svc).HandleTenantBarrierEnableEncryption(w,
		tenantBarrierReq(http.MethodPost, "tenant-a",
			`{"target_strategy":"passphrase","passphrase":"eA=="}`))
	if w.Code < 500 {
		t.Fatalf("expected 5xx, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// TestHandleTenantBarrierEnableEncryption_ForbiddenMapsTo403 asserts that a
// QBError with ErrForbidden code is translated to HTTP 403 by
// handleServiceError (exercises the end-to-end mapping path).
func TestHandleTenantBarrierEnableEncryption_ForbiddenMapsTo403(t *testing.T) {
	svc := &fakeBarrierService{
		enableEncryptionFn: func(_ context.Context, _ string, _ qrdbsdk.MigrationOptions) error {
			return &dberrors.QBError{Code: dberrors.ErrForbidden, Err: errBarrierBoom}
		},
	}
	w := httptest.NewRecorder()
	barrierServer(t, svc).HandleTenantBarrierEnableEncryption(w,
		tenantBarrierReq(http.MethodPost, "tenant-a",
			`{"target_strategy":"passphrase","passphrase":"eA=="}`))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (body=%s)", w.Code, w.Body.String())
	}
}
