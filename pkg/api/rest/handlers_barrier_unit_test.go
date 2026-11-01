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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// errBarrierBoom is a generic (non-QBError) failure used to drive the
// 500-class error branch of every barrier handler.
var errBarrierBoom = stderrors.New("barrier boom")

// fakeBarrierService is a hand-written test double for service.BarrierServicer.
// Methods delegate to optional function fields so a test can shape each path.
type fakeBarrierService struct {
	statusFn           func(ctx context.Context) (*service.BarrierStatusInfo, error)
	initializeFn       func(ctx context.Context, secret []byte) error
	unsealFn           func(ctx context.Context, secret []byte) error
	sealFn             func(ctx context.Context, req *service.BarrierSealRequest) error
	initShamirFn       func(ctx context.Context, secret []byte, threshold, total int) (*service.ShamirInitInfo, error)
	unsealWithShareFn  func(ctx context.Context, share string) (*service.UnsealShareProgress, error)
	unsealWithSharesFn func(ctx context.Context, shares []string) error
	rekeyFn            func(ctx context.Context, threshold, total int) (*service.ShamirInitInfo, error)
	genRecoveryFn      func(ctx context.Context, threshold, total int) (*service.RecoveryKeysInfo, error)
	recoverWithKeysFn  func(ctx context.Context, keys []string) error
	deleteRecoveryFn   func(ctx context.Context) error
	hasRecoveryFn      func(ctx context.Context) (bool, error)
	genRootTokenFn     func(ctx context.Context, shares []string) (*service.RootTokenInfo, error)
	enableEncryptionFn func(ctx context.Context, tenantID string, opts qrdbsdk.MigrationOptions) error
}

func (f *fakeBarrierService) BarrierStatus(ctx context.Context) (*service.BarrierStatusInfo, error) {
	if f.statusFn != nil {
		return f.statusFn(ctx)
	}
	return &service.BarrierStatusInfo{}, nil
}

func (f *fakeBarrierService) BarrierInitialize(ctx context.Context, secret []byte) error {
	if f.initializeFn != nil {
		return f.initializeFn(ctx, secret)
	}
	return nil
}

func (f *fakeBarrierService) BarrierUnseal(ctx context.Context, secret []byte) error {
	if f.unsealFn != nil {
		return f.unsealFn(ctx, secret)
	}
	return nil
}

func (f *fakeBarrierService) BarrierSeal(ctx context.Context, req *service.BarrierSealRequest) error {
	if f.sealFn != nil {
		return f.sealFn(ctx, req)
	}
	return nil
}

func (f *fakeBarrierService) BarrierInitializeShamir(ctx context.Context, secret []byte, threshold, total int) (*service.ShamirInitInfo, error) {
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

func (f *fakeBarrierService) EnableEncryption(ctx context.Context, tenantID string, opts qrdbsdk.MigrationOptions) error {
	if f.enableEncryptionFn != nil {
		return f.enableEncryptionFn(ctx, tenantID, opts)
	}
	return nil
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

// TestHandleBarrier_ServiceError checks that every barrier handler maps a
// service failure to a 5xx response.
func TestHandleBarrier_ServiceError(t *testing.T) {
	tests := []struct {
		name   string
		svc    *fakeBarrierService
		method string
		path   string
		body   string
	}{
		{
			name: "status",
			svc: &fakeBarrierService{statusFn: func(context.Context) (*service.BarrierStatusInfo, error) {
				return nil, errBarrierBoom
			}},
			method: http.MethodGet, path: "/api/v1/barrier/status",
		},
		{
			name:   "initialize",
			svc:    &fakeBarrierService{initializeFn: func(context.Context, []byte) error { return errBarrierBoom }},
			method: http.MethodPost, path: "/api/v1/barrier/initialize", body: `{"secret":"eA=="}`,
		},
		{
			name:   "unseal",
			svc:    &fakeBarrierService{unsealFn: func(context.Context, []byte) error { return errBarrierBoom }},
			method: http.MethodPost, path: "/api/v1/barrier/unseal", body: `{"secret":"eA=="}`,
		},
		{
			name:   "seal",
			svc:    &fakeBarrierService{sealFn: func(context.Context, *service.BarrierSealRequest) error { return errBarrierBoom }},
			method: http.MethodPost, path: "/api/v1/barrier/seal",
		},
		{
			name: "shamir initialize",
			svc: &fakeBarrierService{initShamirFn: func(context.Context, []byte, int, int) (*service.ShamirInitInfo, error) {
				return nil, errBarrierBoom
			}},
			method: http.MethodPost, path: "/api/v1/barrier/shamir/initialize",
			body: `{"secret":"eA==","threshold":2,"total_shares":3}`,
		},
		{
			name: "shamir unseal share",
			svc: &fakeBarrierService{unsealWithShareFn: func(context.Context, string) (*service.UnsealShareProgress, error) {
				return nil, errBarrierBoom
			}},
			method: http.MethodPost, path: "/api/v1/barrier/shamir/unseal-share", body: `{"share":"abc"}`,
		},
		{
			name:   "shamir unseal shares",
			svc:    &fakeBarrierService{unsealWithSharesFn: func(context.Context, []string) error { return errBarrierBoom }},
			method: http.MethodPost, path: "/api/v1/barrier/shamir/unseal-shares", body: `{"shares":["a","b"]}`,
		},
		{
			name: "rekey",
			svc: &fakeBarrierService{rekeyFn: func(context.Context, int, int) (*service.ShamirInitInfo, error) {
				return nil, errBarrierBoom
			}},
			method: http.MethodPost, path: "/api/v1/barrier/rekey", body: `{"threshold":2,"total":3}`,
		},
		{
			name: "recovery generate",
			svc: &fakeBarrierService{genRecoveryFn: func(context.Context, int, int) (*service.RecoveryKeysInfo, error) {
				return nil, errBarrierBoom
			}},
			method: http.MethodPost, path: "/api/v1/barrier/recovery/generate", body: `{"threshold":2,"total":3}`,
		},
		{
			name:   "recovery recover",
			svc:    &fakeBarrierService{recoverWithKeysFn: func(context.Context, []string) error { return errBarrierBoom }},
			method: http.MethodPost, path: "/api/v1/barrier/recovery/recover", body: `{"keys":["a","b"]}`,
		},
		{
			name:   "recovery delete",
			svc:    &fakeBarrierService{deleteRecoveryFn: func(context.Context) error { return errBarrierBoom }},
			method: http.MethodDelete, path: "/api/v1/barrier/recovery",
		},
		{
			name:   "recovery status",
			svc:    &fakeBarrierService{hasRecoveryFn: func(context.Context) (bool, error) { return false, errBarrierBoom }},
			method: http.MethodGet, path: "/api/v1/barrier/recovery/status",
		},
		{
			name: "root token",
			svc: &fakeBarrierService{genRootTokenFn: func(context.Context, []string) (*service.RootTokenInfo, error) {
				return nil, errBarrierBoom
			}},
			method: http.MethodPost, path: "/api/v1/barrier/root-token", body: `{"shares":["a"]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := doBarrierReq(t, barrierServer(t, tt.svc), tt.method, tt.path, tt.body)
			if w.Code < 500 {
				t.Fatalf("expected 5xx, got %d", w.Code)
			}
		})
	}
}

// =============================================================================
// Initialize / Unseal / Seal
// =============================================================================

func TestHandleBarrierInitialize(t *testing.T) {
	var gotSecret string
	svc := &fakeBarrierService{
		// Copy: the handler wipes this slice once the call returns.
		initializeFn: func(ctx context.Context, secret []byte) error { gotSecret = string(secret); return nil },
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/initialize", `{"secret":"czNjcjN0"}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if gotSecret != "s3cr3t" {
		t.Fatalf("expected secret to be forwarded, got %q", gotSecret)
	}
}

// TestHandleBarrier_BadInput checks that every barrier handler that takes a
// body rejects malformed JSON and out-of-range values with 400.
func TestHandleBarrier_BadInput(t *testing.T) {
	sealNotReached := &fakeBarrierService{sealFn: func(context.Context, *service.BarrierSealRequest) error {
		t.Error("a malformed body must not reach the service")
		return nil
	}}
	tests := []struct {
		name string
		svc  *fakeBarrierService
		path string
		body string
	}{
		{name: "initialize bad json", path: "/api/v1/barrier/initialize", body: `{bad`},
		{name: "seal malformed body", svc: sealNotReached, path: "/api/v1/barrier/seal", body: `{"so_pin":`},
		{name: "shamir initialize zero threshold", path: "/api/v1/barrier/shamir/initialize", body: `{"secret":"eA==","threshold":0,"total_shares":3}`},
		{name: "shamir initialize zero total", path: "/api/v1/barrier/shamir/initialize", body: `{"secret":"eA==","threshold":2,"total_shares":0}`},
		{name: "shamir initialize threshold over total", path: "/api/v1/barrier/shamir/initialize", body: `{"secret":"eA==","threshold":4,"total_shares":3}`},
		{name: "shamir unseal share empty", path: "/api/v1/barrier/shamir/unseal-share", body: `{"share":""}`},
		{name: "shamir unseal shares empty", path: "/api/v1/barrier/shamir/unseal-shares", body: `{"shares":[]}`},
		{name: "rekey zero threshold", path: "/api/v1/barrier/rekey", body: `{"threshold":0,"total":3}`},
		{name: "rekey zero total", path: "/api/v1/barrier/rekey", body: `{"threshold":2,"total":0}`},
		{name: "rekey threshold over total", path: "/api/v1/barrier/rekey", body: `{"threshold":4,"total":3}`},
		{name: "recovery generate zero threshold", path: "/api/v1/barrier/recovery/generate", body: `{"threshold":0,"total":3}`},
		{name: "recovery generate zero total", path: "/api/v1/barrier/recovery/generate", body: `{"threshold":2,"total":0}`},
		{name: "recovery generate threshold over total", path: "/api/v1/barrier/recovery/generate", body: `{"threshold":5,"total":3}`},
		{name: "recovery recover empty", path: "/api/v1/barrier/recovery/recover", body: `{"keys":[]}`},
		{name: "root token empty", path: "/api/v1/barrier/root-token", body: `{"shares":[]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := tt.svc
			if svc == nil {
				svc = &fakeBarrierService{}
			}
			w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, tt.path, tt.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", w.Code)
			}
		})
	}
}

func TestHandleBarrierUnseal(t *testing.T) {
	svc := &fakeBarrierService{unsealFn: func(ctx context.Context, secret []byte) error { return nil }}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/unseal", `{"secret":"eA=="}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestHandleBarrierSeal(t *testing.T) {
	svc := &fakeBarrierService{sealFn: func(_ context.Context, req *service.BarrierSealRequest) error {
		if len(req.SOPIN) != 0 {
			t.Errorf("an empty body must reach the service with no SO PIN, got %q", req.SOPIN)
		}
		return nil
	}}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/seal", "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

// soPINCheckingSeal stands in for a service whose PIN backend holds want: it
// refuses any other PIN with the service's typed forbidden error and keeps
// the PIN slice it received so the caller can check it was wiped.
func soPINCheckingSeal(want string, got *[]byte) func(context.Context, *service.BarrierSealRequest) error {
	return func(_ context.Context, req *service.BarrierSealRequest) error {
		*got = req.SOPIN
		if string(req.SOPIN) != want {
			return &dberrors.QBError{Code: dberrors.ErrForbidden, Op: "barrier.Seal", Err: qrdbsdk.ErrPINInvalid}
		}
		return nil
	}
}

func TestHandleBarrierSeal_SOPIN(t *testing.T) {
	const soPIN = "so-pin-1234"
	tests := []struct {
		name     string
		path     string
		body     string
		wantCode int
	}{
		{name: "right PIN seals", path: "/api/v1/barrier/seal", body: `{"so_pin":"` + soPIN + `"}`, wantCode: http.StatusNoContent},
		{name: "right PIN seals on the qrdb route", path: "/v1/barrier/seal", body: `{"so_pin":"` + soPIN + `"}`, wantCode: http.StatusNoContent},
		{name: "wrong PIN is refused", path: "/api/v1/barrier/seal", body: `{"so_pin":"wrong"}`, wantCode: http.StatusForbidden},
		{name: "wrong PIN is refused on the qrdb route", path: "/v1/barrier/seal", body: `{"so_pin":"wrong"}`, wantCode: http.StatusForbidden},
		{name: "missing PIN is refused", path: "/api/v1/barrier/seal", body: `{}`, wantCode: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []byte
			svc := &fakeBarrierService{sealFn: soPINCheckingSeal(soPIN, &got)}
			w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, tt.path, tt.body)
			if w.Code != tt.wantCode {
				t.Fatalf("expected %d, got %d: %s", tt.wantCode, w.Code, w.Body.String())
			}
			for _, b := range got {
				if b != 0 {
					t.Fatalf("handler must wipe the SO PIN after the call, got %q", got)
				}
			}
		})
	}
}

// =============================================================================
// Shamir initialize (validation + happy path)
// =============================================================================

func TestHandleBarrierShamirInitialize_OK(t *testing.T) {
	svc := &fakeBarrierService{
		initShamirFn: func(ctx context.Context, secret []byte, threshold, total int) (*service.ShamirInitInfo, error) {
			return &service.ShamirInitInfo{Threshold: threshold, TotalShares: total, ShareCount: total}, nil
		},
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/shamir/initialize",
		`{"secret":"eA==","threshold":2,"total_shares":3}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["threshold"].(float64) != 2 || resp["total_shares"].(float64) != 3 || resp["share_count"].(float64) != 3 {
		t.Fatalf("unexpected shamir init body: %v", resp)
	}
	// Share-free contract: raw shares must never appear in the response body.
	if _, leaked := resp["shares"]; leaked {
		t.Fatalf("shares leaked in shamir init body: %v", resp)
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

func TestHandleBarrierShamirUnsealShares_OK(t *testing.T) {
	svc := &fakeBarrierService{unsealWithSharesFn: func(ctx context.Context, shares []string) error { return nil }}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/shamir/unseal-shares", `{"shares":["a","b"]}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

// =============================================================================
// Rekey
// =============================================================================

func TestHandleBarrierRekey_OK(t *testing.T) {
	svc := &fakeBarrierService{
		rekeyFn: func(ctx context.Context, threshold, total int) (*service.ShamirInitInfo, error) {
			return &service.ShamirInitInfo{Threshold: threshold, TotalShares: total, ShareCount: total}, nil
		},
	}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/rekey", `{"threshold":2,"total":3}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// =============================================================================
// Recovery generate / recover / delete / status
// =============================================================================

func TestHandleBarrierRecoveryGenerate_OK(t *testing.T) {
	svc := &fakeBarrierService{
		genRecoveryFn: func(ctx context.Context, threshold, total int) (*service.RecoveryKeysInfo, error) {
			return &service.RecoveryKeysInfo{Threshold: threshold, Total: total, ShareCount: total}, nil
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
	if resp["threshold"].(float64) != 2 || resp["total"].(float64) != 3 || resp["share_count"].(float64) != 3 {
		t.Fatalf("unexpected recovery body: %v", resp)
	}
	// Share-free contract: raw recovery keys must never appear in the response body.
	if _, leaked := resp["keys"]; leaked {
		t.Fatalf("recovery keys leaked in body: %v", resp)
	}
}

func TestHandleBarrierRecoveryRecover_OK(t *testing.T) {
	svc := &fakeBarrierService{recoverWithKeysFn: func(ctx context.Context, keys []string) error { return nil }}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodPost, "/api/v1/barrier/recovery/recover", `{"keys":["a","b"]}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
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
	if resp["has_keys"] != true {
		t.Fatalf("unexpected recovery status body: %v", resp)
	}
	if _, ok := resp["has_recovery_keys"]; ok {
		t.Fatalf("has_recovery_keys is the MCP/gRPC field name, not REST's: %v", resp)
	}
}

// TestHandleBarrierRecoveryStatus_DecodesIntoQRDBTransportType pins the wire
// format to the client that actually reads it: go-qrdb's REST transport
// decodes GET /v1/barrier/recovery/status into
// transport.BarrierHasRecoveryKeysResponse, whose field is tagged "has_keys".
// A mismatched key decodes to false for a barrier that HAS recovery keys —
// a wrong answer on a disaster-recovery precondition, not an error
// (go-quicbaas#991).
func TestHandleBarrierRecoveryStatus_DecodesIntoQRDBTransportType(t *testing.T) {
	svc := &fakeBarrierService{hasRecoveryFn: func(ctx context.Context) (bool, error) { return true, nil }}
	w := doBarrierReq(t, barrierServer(t, svc), http.MethodGet, "/api/v1/barrier/recovery/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var decoded qrdbtransport.BarrierHasRecoveryKeysResponse
	if err := json.Unmarshal(w.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !decoded.HasKeys {
		t.Fatalf("qrdb REST transport decoded HasKeys=false from %s", w.Body.String())
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
