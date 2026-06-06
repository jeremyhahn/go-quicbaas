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
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// errBarrierBoom is a generic (non-QBError) failure used to drive the
// 500-class error branch of every barrier handler.
var errBarrierBoom = stderrors.New("barrier boom")

// fakeBarrierService is a hand-written test double for service.BarrierServicer.
// Methods delegate to optional function fields so a test can shape each path.
type fakeBarrierService struct {
	statusFn           func(ctx context.Context) (*service.BarrierStatusInfo, error)
	initializeFn       func(ctx context.Context, secret string) error
	unsealFn           func(ctx context.Context, secret string) error
	sealFn             func(ctx context.Context) error
	initShamirFn       func(ctx context.Context, secret string, threshold, total int) (*service.ShamirInitInfo, error)
	unsealWithShareFn  func(ctx context.Context, share string) (*service.UnsealShareProgress, error)
	unsealWithSharesFn func(ctx context.Context, shares []string) error
	rekeyFn            func(ctx context.Context, threshold, total int) (*service.ShamirInitInfo, error)
	genRecoveryFn      func(ctx context.Context, threshold, total int) (*service.RecoveryKeysInfo, error)
	recoverWithKeysFn  func(ctx context.Context, keys []string) error
	deleteRecoveryFn   func(ctx context.Context) error
	hasRecoveryFn      func(ctx context.Context) (bool, error)
	genRootTokenFn     func(ctx context.Context, shares []string) (*service.RootTokenInfo, error)
}

func (f *fakeBarrierService) BarrierStatus(ctx context.Context) (*service.BarrierStatusInfo, error) {
	if f.statusFn != nil {
		return f.statusFn(ctx)
	}
	return &service.BarrierStatusInfo{}, nil
}

func (f *fakeBarrierService) BarrierInitialize(ctx context.Context, secret string) error {
	if f.initializeFn != nil {
		return f.initializeFn(ctx, secret)
	}
	return nil
}

func (f *fakeBarrierService) BarrierUnseal(ctx context.Context, secret string) error {
	if f.unsealFn != nil {
		return f.unsealFn(ctx, secret)
	}
	return nil
}

func (f *fakeBarrierService) BarrierSeal(ctx context.Context) error {
	if f.sealFn != nil {
		return f.sealFn(ctx)
	}
	return nil
}

func (f *fakeBarrierService) BarrierInitializeShamir(ctx context.Context, secret string, threshold, total int) (*service.ShamirInitInfo, error) {
	if f.initShamirFn != nil {
		return f.initShamirFn(ctx, secret, threshold, total)
	}
	return &service.ShamirInitInfo{}, nil
}

func (f *fakeBarrierService) BarrierUnsealWithShare(ctx context.Context, share string) (*service.UnsealShareProgress, error) {
	if f.unsealWithShareFn != nil {
		return f.unsealWithShareFn(ctx, share)
	}
	return &service.UnsealShareProgress{}, nil
}

func (f *fakeBarrierService) BarrierUnsealWithShares(ctx context.Context, shares []string) error {
	if f.unsealWithSharesFn != nil {
		return f.unsealWithSharesFn(ctx, shares)
	}
	return nil
}

func (f *fakeBarrierService) BarrierRekey(ctx context.Context, threshold, total int) (*service.ShamirInitInfo, error) {
	if f.rekeyFn != nil {
		return f.rekeyFn(ctx, threshold, total)
	}
	return &service.ShamirInitInfo{}, nil
}

func (f *fakeBarrierService) BarrierGenerateRecoveryKeys(ctx context.Context, threshold, total int) (*service.RecoveryKeysInfo, error) {
	if f.genRecoveryFn != nil {
		return f.genRecoveryFn(ctx, threshold, total)
	}
	return &service.RecoveryKeysInfo{}, nil
}

func (f *fakeBarrierService) BarrierRecoverWithKeys(ctx context.Context, keys []string) error {
	if f.recoverWithKeysFn != nil {
		return f.recoverWithKeysFn(ctx, keys)
	}
	return nil
}

func (f *fakeBarrierService) BarrierDeleteRecoveryKeys(ctx context.Context) error {
	if f.deleteRecoveryFn != nil {
		return f.deleteRecoveryFn(ctx)
	}
	return nil
}

func (f *fakeBarrierService) BarrierHasRecoveryKeys(ctx context.Context) (bool, error) {
	if f.hasRecoveryFn != nil {
		return f.hasRecoveryFn(ctx)
	}
	return false, nil
}

func (f *fakeBarrierService) BarrierGenerateRootToken(ctx context.Context, shares []string) (*service.RootTokenInfo, error) {
	if f.genRootTokenFn != nil {
		return f.genRootTokenFn(ctx, shares)
	}
	return &service.RootTokenInfo{}, nil
}

// barrierServer installs the given barrier service on a router-backed server.
func barrierServer(t *testing.T, svc service.BarrierServicer) *Server {
	t.Helper()
	s, _, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	s.SetBarrierService(svc)
	return s
}

func doBarrierReq(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
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
// Status
// =============================================================================

func TestHandleBarrierStatus_OK(t *testing.T) {
	now := time.Now()
	svc := &fakeBarrierService{
		statusFn: func(ctx context.Context) (*service.BarrierStatusInfo, error) {
			return &service.BarrierStatusInfo{
				Sealed:         true,
				Strategy:       "shamir",
				HardwareBacked: true,
				InitializedAt:  now,
			}, nil
		},
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodGet, "/api/v1/barrier/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["sealed"] != true || resp["strategy"] != "shamir" || resp["hardware_backed"] != true {
		t.Fatalf("unexpected status body: %v", resp)
	}
	if _, ok := resp["initialized_at"]; !ok {
		t.Fatalf("expected initialized_at in body: %v", resp)
	}
}

func TestHandleBarrierStatus_ZeroTimeOmitsInitializedAt(t *testing.T) {
	svc := &fakeBarrierService{
		statusFn: func(ctx context.Context) (*service.BarrierStatusInfo, error) {
			return &service.BarrierStatusInfo{Sealed: false, Strategy: "none"}, nil
		},
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodGet, "/api/v1/barrier/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "initialized_at") {
		t.Fatalf("zero InitializedAt should be omitted: %s", w.Body.String())
	}
}

func TestHandleBarrierStatus_ServiceError(t *testing.T) {
	svc := &fakeBarrierService{
		statusFn: func(ctx context.Context) (*service.BarrierStatusInfo, error) {
			return nil, errBarrierBoom
		},
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodGet, "/api/v1/barrier/status", "")
	if w.Code < 500 {
		t.Fatalf("expected 5xx error, got %d", w.Code)
	}
}

// =============================================================================
// Initialize / Unseal / Seal
// =============================================================================

func TestHandleBarrierInitialize(t *testing.T) {
	var gotSecret string
	svc := &fakeBarrierService{
		initializeFn: func(ctx context.Context, secret string) error { gotSecret = secret; return nil },
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/initialize", `{"secret":"s3cr3t"}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if gotSecret != "s3cr3t" {
		t.Fatalf("expected secret to be forwarded, got %q", gotSecret)
	}
}

func TestHandleBarrierInitialize_BadJSON(t *testing.T) {
	w := doBarrierReq(t, barrierServer(t, &fakeBarrierService{}), http.MethodPost, "/api/v1/barrier/initialize", `{bad`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleBarrierInitialize_ServiceError(t *testing.T) {
	svc := &fakeBarrierService{
		initializeFn: func(ctx context.Context, secret string) error { return errBarrierBoom },
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/initialize", `{"secret":"x"}`)
	if w.Code < 500 {
		t.Fatalf("expected 5xx, got %d", w.Code)
	}
}

func TestHandleBarrierUnseal(t *testing.T) {
	svc := &fakeBarrierService{unsealFn: func(ctx context.Context, secret string) error { return nil }}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/unseal", `{"secret":"x"}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestHandleBarrierUnseal_ServiceError(t *testing.T) {
	svc := &fakeBarrierService{unsealFn: func(ctx context.Context, secret string) error { return errBarrierBoom }}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/unseal", `{"secret":"x"}`)
	if w.Code < 500 {
		t.Fatalf("expected 5xx, got %d", w.Code)
	}
}

func TestHandleBarrierSeal(t *testing.T) {
	svc := &fakeBarrierService{sealFn: func(ctx context.Context) error { return nil }}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/seal", "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestHandleBarrierSeal_ServiceError(t *testing.T) {
	svc := &fakeBarrierService{sealFn: func(ctx context.Context) error { return errBarrierBoom }}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/seal", "")
	if w.Code < 500 {
		t.Fatalf("expected 5xx, got %d", w.Code)
	}
}

// =============================================================================
// Shamir initialize (validation + happy path)
// =============================================================================

func TestHandleBarrierShamirInitialize_OK(t *testing.T) {
	svc := &fakeBarrierService{
		initShamirFn: func(ctx context.Context, secret string, threshold, total int) (*service.ShamirInitInfo, error) {
			return &service.ShamirInitInfo{Shares: []string{"a", "b", "c"}, Threshold: threshold, TotalShares: total}, nil
		},
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/shamir/initialize",
		`{"secret":"x","threshold":2,"total_shares":3}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["threshold"].(float64) != 2 || resp["total_shares"].(float64) != 3 {
		t.Fatalf("unexpected shamir init body: %v", resp)
	}
}

func TestHandleBarrierShamirInitialize_Validation(t *testing.T) {
	svc := &fakeBarrierService{}
	cases := []string{
		`{"secret":"x","threshold":0,"total_shares":3}`,
		`{"secret":"x","threshold":2,"total_shares":0}`,
		`{"secret":"x","threshold":4,"total_shares":3}`,
	}
	for _, body := range cases {
		w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/shamir/initialize", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %s: expected 400, got %d", body, w.Code)
		}
	}
}

func TestHandleBarrierShamirInitialize_ServiceError(t *testing.T) {
	svc := &fakeBarrierService{
		initShamirFn: func(ctx context.Context, secret string, threshold, total int) (*service.ShamirInitInfo, error) {
			return nil, errBarrierBoom
		},
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/shamir/initialize",
		`{"secret":"x","threshold":2,"total_shares":3}`)
	if w.Code < 500 {
		t.Fatalf("expected 5xx, got %d", w.Code)
	}
}

// =============================================================================
// Shamir unseal-share / unseal-shares
// =============================================================================

func TestHandleBarrierShamirUnsealShare_OK(t *testing.T) {
	svc := &fakeBarrierService{
		unsealWithShareFn: func(ctx context.Context, share string) (*service.UnsealShareProgress, error) {
			return &service.UnsealShareProgress{Required: 3, Submitted: 1, Complete: false}, nil
		},
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/shamir/unseal-share", `{"share":"abc"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["required"].(float64) != 3 || resp["submitted"].(float64) != 1 || resp["complete"] != false {
		t.Fatalf("unexpected progress body: %v", resp)
	}
}

func TestHandleBarrierShamirUnsealShare_MissingShare(t *testing.T) {
	w := doBarrierReq(t, barrierServer(t, &fakeBarrierService{}), http.MethodPost, "/api/v1/barrier/shamir/unseal-share", `{"share":""}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleBarrierShamirUnsealShare_ServiceError(t *testing.T) {
	svc := &fakeBarrierService{
		unsealWithShareFn: func(ctx context.Context, share string) (*service.UnsealShareProgress, error) {
			return nil, errBarrierBoom
		},
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/shamir/unseal-share", `{"share":"abc"}`)
	if w.Code < 500 {
		t.Fatalf("expected 5xx, got %d", w.Code)
	}
}

func TestHandleBarrierShamirUnsealShares_OK(t *testing.T) {
	svc := &fakeBarrierService{unsealWithSharesFn: func(ctx context.Context, shares []string) error { return nil }}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/shamir/unseal-shares", `{"shares":["a","b"]}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestHandleBarrierShamirUnsealShares_Empty(t *testing.T) {
	w := doBarrierReq(t, barrierServer(t, &fakeBarrierService{}), http.MethodPost, "/api/v1/barrier/shamir/unseal-shares", `{"shares":[]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// =============================================================================
// Rekey
// =============================================================================

func TestHandleBarrierRekey_OK(t *testing.T) {
	svc := &fakeBarrierService{
		rekeyFn: func(ctx context.Context, threshold, total int) (*service.ShamirInitInfo, error) {
			return &service.ShamirInitInfo{Shares: []string{"x"}, Threshold: threshold, TotalShares: total}, nil
		},
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/rekey", `{"threshold":2,"total":3}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleBarrierRekey_Validation(t *testing.T) {
	svc := &fakeBarrierService{}
	for _, body := range []string{
		`{"threshold":0,"total":3}`,
		`{"threshold":2,"total":0}`,
		`{"threshold":4,"total":3}`,
	} {
		w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/rekey", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %s: expected 400, got %d", body, w.Code)
		}
	}
}

// =============================================================================
// Recovery generate / recover / delete / status
// =============================================================================

func TestHandleBarrierRecoveryGenerate_OK(t *testing.T) {
	svc := &fakeBarrierService{
		genRecoveryFn: func(ctx context.Context, threshold, total int) (*service.RecoveryKeysInfo, error) {
			return &service.RecoveryKeysInfo{Keys: []string{"k1", "k2"}, Threshold: threshold, Total: total}, nil
		},
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/recovery/generate", `{"threshold":2,"total":3}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if keys, ok := resp["keys"].([]interface{}); !ok || len(keys) != 2 {
		t.Fatalf("unexpected recovery keys body: %v", resp)
	}
}

func TestHandleBarrierRecoveryGenerate_Validation(t *testing.T) {
	svc := &fakeBarrierService{}
	for _, body := range []string{
		`{"threshold":0,"total":3}`,
		`{"threshold":2,"total":0}`,
		`{"threshold":5,"total":3}`,
	} {
		w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/recovery/generate", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %s: expected 400, got %d", body, w.Code)
		}
	}
}

func TestHandleBarrierRecoveryRecover_OK(t *testing.T) {
	svc := &fakeBarrierService{recoverWithKeysFn: func(ctx context.Context, keys []string) error { return nil }}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/recovery/recover", `{"keys":["a","b"]}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestHandleBarrierRecoveryRecover_Empty(t *testing.T) {
	w := doBarrierReq(t, barrierServer(t, &fakeBarrierService{}), http.MethodPost, "/api/v1/barrier/recovery/recover", `{"keys":[]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleBarrierRecoveryDelete_OK(t *testing.T) {
	svc := &fakeBarrierService{deleteRecoveryFn: func(ctx context.Context) error { return nil }}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodDelete, "/api/v1/barrier/recovery", "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestHandleBarrierRecoveryStatus_OK(t *testing.T) {
	svc := &fakeBarrierService{hasRecoveryFn: func(ctx context.Context) (bool, error) { return true, nil }}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodGet, "/api/v1/barrier/recovery/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["has_recovery_keys"] != true {
		t.Fatalf("unexpected recovery status body: %v", resp)
	}
}

func TestHandleBarrierRecoveryStatus_ServiceError(t *testing.T) {
	svc := &fakeBarrierService{hasRecoveryFn: func(ctx context.Context) (bool, error) { return false, errBarrierBoom }}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodGet, "/api/v1/barrier/recovery/status", "")
	if w.Code < 500 {
		t.Fatalf("expected 5xx, got %d", w.Code)
	}
}

// =============================================================================
// Root token
// =============================================================================

func TestHandleBarrierRootToken_OK(t *testing.T) {
	now := time.Now()
	svc := &fakeBarrierService{
		genRootTokenFn: func(ctx context.Context, shares []string) (*service.RootTokenInfo, error) {
			return &service.RootTokenInfo{Token: "deadbeef", CreatedAt: now}, nil
		},
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/root-token", `{"shares":["a","b"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["token"] != "deadbeef" {
		t.Fatalf("unexpected root token body: %v", resp)
	}
	if _, ok := resp["created_at"]; !ok {
		t.Fatalf("expected created_at in body: %v", resp)
	}
}

func TestHandleBarrierRootToken_Empty(t *testing.T) {
	w := doBarrierReq(t, barrierServer(t, &fakeBarrierService{}), http.MethodPost, "/api/v1/barrier/root-token", `{"shares":[]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleBarrierRootToken_ZeroCreatedAtOmitted(t *testing.T) {
	svc := &fakeBarrierService{
		genRootTokenFn: func(ctx context.Context, shares []string) (*service.RootTokenInfo, error) {
			return &service.RootTokenInfo{Token: "deadbeef"}, nil
		},
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/root-token", `{"shares":["a"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "created_at") {
		t.Fatalf("zero CreatedAt should be omitted: %s", w.Body.String())
	}
}
