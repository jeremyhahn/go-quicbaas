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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// fakeQRDBUserService is a hand-written test double for the qrdb
// UserServicer interface. Only GetUserByUsername is shaped per test — the
// remaining methods are unused by the mounted wire routes.
type fakeQRDBUserService struct {
	qrdbsdk.UserServicer

	getByUsernameFn func(ctx context.Context, tenantID, username string) (*qrdbsdk.User, error)
}

func (f *fakeQRDBUserService) GetUserByUsername(ctx context.Context, tenantID, username string) (*qrdbsdk.User, error) {
	if f.getByUsernameFn != nil {
		return f.getByUsernameFn(ctx, tenantID, username)
	}
	return nil, &qrdbsdk.UserNotFoundError{}
}

// fakeUserAuthFactorService is a hand-written test double for the qrdb
// UserAuthFactorServicer interface.
type fakeUserAuthFactorService struct {
	addFn     func(ctx context.Context, userID uint64, factor *qrdbsdk.AuthFactor) error
	listFn    func(ctx context.Context, userID uint64) ([]*qrdbsdk.AuthFactor, error)
	getFn     func(ctx context.Context, factorID string) (*qrdbsdk.AuthFactor, error)
	lookupFn  func(ctx context.Context, t qrdbsdk.AuthFactorType, key []byte) (*qrdbsdk.AuthFactor, error)
	removeFn  func(ctx context.Context, factorID string, force bool) error
	rotateFn  func(ctx context.Context, oldID string, newFactor *qrdbsdk.AuthFactor) error
	relabelFn func(ctx context.Context, factorID, label string) error
}

func (f *fakeUserAuthFactorService) Add(ctx context.Context, userID uint64, factor *qrdbsdk.AuthFactor) error {
	if f.addFn != nil {
		return f.addFn(ctx, userID, factor)
	}
	return nil
}

func (f *fakeUserAuthFactorService) List(ctx context.Context, userID uint64) ([]*qrdbsdk.AuthFactor, error) {
	if f.listFn != nil {
		return f.listFn(ctx, userID)
	}
	return nil, nil
}

func (f *fakeUserAuthFactorService) Get(ctx context.Context, factorID string) (*qrdbsdk.AuthFactor, error) {
	if f.getFn != nil {
		return f.getFn(ctx, factorID)
	}
	return &qrdbsdk.AuthFactor{ID: factorID}, nil
}

func (f *fakeUserAuthFactorService) LookupByCredentialKey(ctx context.Context, t qrdbsdk.AuthFactorType, key []byte) (*qrdbsdk.AuthFactor, error) {
	if f.lookupFn != nil {
		return f.lookupFn(ctx, t, key)
	}
	return nil, qrdbsdk.ErrFactorNotFound
}

func (f *fakeUserAuthFactorService) Remove(ctx context.Context, factorID string, force bool) error {
	if f.removeFn != nil {
		return f.removeFn(ctx, factorID, force)
	}
	return nil
}

func (f *fakeUserAuthFactorService) Rotate(ctx context.Context, oldID string, newFactor *qrdbsdk.AuthFactor) error {
	if f.rotateFn != nil {
		return f.rotateFn(ctx, oldID, newFactor)
	}
	return nil
}

func (f *fakeUserAuthFactorService) Relabel(ctx context.Context, factorID, label string) error {
	if f.relabelFn != nil {
		return f.relabelFn(ctx, factorID, label)
	}
	return nil
}

func (f *fakeUserAuthFactorService) MarkUsed(ctx context.Context, factorID string, ts time.Time) error {
	return nil
}

// userFactorServer returns a router-backed server with the given user and
// auth-factor services installed. Pass nils to exercise the
// "service not configured" 501 paths.
func userFactorServer(t *testing.T, userSvc qrdbsdk.UserServicer, uafSvc qrdbsdk.UserAuthFactorServicer) *Server {
	t.Helper()
	s, _ := newTestServer(t)
	if userSvc != nil {
		s.SetUserService(userSvc)
	}
	if uafSvc != nil {
		s.SetUserAuthFactorService(uafSvc)
	}
	return s
}

func doUserFactorReq(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
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
// Service-not-configured (501) across every route
// =============================================================================

func TestQRDBUserFactorRoutes_ServiceNotConfigured(t *testing.T) {
	s := userFactorServer(t, nil, nil)

	cases := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/users/by-username/admin", ""},
		{http.MethodPost, "/v1/users/factors", `{"user_id":1,"type":"mtls"}`},
		{http.MethodGet, "/v1/users/factors/list?user_id=1", ""},
		{http.MethodGet, "/v1/users/factors/f1", ""},
		{http.MethodDelete, "/v1/users/factors/f1", ""},
		{http.MethodPut, "/v1/users/factors/f1/label", `{"label":"x"}`},
		{http.MethodPost, "/v1/users/factors/rotate", `{"old_factor_id":"f1"}`},
		{http.MethodPost, "/v1/users/factors/login", `{"type":"mtls"}`},
	}
	for _, c := range cases {
		w := doUserFactorReq(t, s, c.method, c.path, c.body)
		if w.Code != http.StatusNotImplemented {
			t.Fatalf("%s %s: expected 501, got %d", c.method, c.path, w.Code)
		}
	}
}

// =============================================================================
// User lookup wire route
// =============================================================================

func TestHandleQRDBUserByUsername_Success(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	userSvc := &fakeQRDBUserService{
		getByUsernameFn: func(_ context.Context, tenantID, username string) (*qrdbsdk.User, error) {
			if tenantID != "system" {
				t.Fatalf("expected tenantID=system, got %q", tenantID)
			}
			if username != "admin" {
				t.Fatalf("expected username=admin, got %q", username)
			}
			return &qrdbsdk.User{
				ID:        42,
				Username:  "admin",
				TenantID:  "system",
				Enabled:   true,
				System:    true,
				CreatedAt: created,
				UpdatedAt: created,
			}, nil
		},
	}
	s := userFactorServer(t, userSvc, nil)

	w := doUserFactorReq(t, s, http.MethodGet, "/users/by-username/admin?tenantID=system", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// The response must decode into the qrdb SDK wire shape (transport.User)
	// exactly as the qrdb SDK REST transport does — that is the contract the
	// factor surface's resolveSubjectUserID depends on.
	var u qrdbtransport.User
	if err := json.Unmarshal(w.Body.Bytes(), &u); err != nil {
		t.Fatalf("decode wire user: %v", err)
	}
	if u.ID != 42 || u.Username != "admin" || u.TenantID != "system" {
		t.Fatalf("unexpected wire user: %+v", u)
	}
	if u.CreatedAt != created.Format(time.RFC3339) {
		t.Fatalf("unexpected created_at: %q", u.CreatedAt)
	}
	// role_ids must be [] (never null) to match the qrdb wire contract.
	if !strings.Contains(w.Body.String(), `"role_ids":[]`) {
		t.Fatalf("role_ids must serialize as empty array, got %s", w.Body.String())
	}
}

func TestHandleQRDBUserByUsername_NotFound(t *testing.T) {
	userSvc := &fakeQRDBUserService{
		getByUsernameFn: func(context.Context, string, string) (*qrdbsdk.User, error) {
			return nil, &qrdbsdk.UserNotFoundError{}
		},
	}
	s := userFactorServer(t, userSvc, nil)

	w := doUserFactorReq(t, s, http.MethodGet, "/users/by-username/ghost", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// =============================================================================
// Factor Add / List / Get / Remove / Relabel / Rotate / Login
// =============================================================================

func TestHandleUserAuthFactorAdd_Success(t *testing.T) {
	var gotUserID uint64
	uaf := &fakeUserAuthFactorService{
		addFn: func(_ context.Context, userID uint64, factor *qrdbsdk.AuthFactor) error {
			gotUserID = userID
			factor.ID = "factor-1"
			factor.RegisteredAt = time.Now().UTC()
			return nil
		},
	}
	s := userFactorServer(t, nil, uaf)

	body := `{"user_id":42,"type":"mtls","label":"admin cert","credential":"AQID"}`
	w := doUserFactorReq(t, s, http.MethodPost, "/v1/users/factors", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotUserID != 42 {
		t.Fatalf("expected Add called with user 42, got %d", gotUserID)
	}

	var resp qrdbtransport.AuthFactorAddResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode add response: %v", err)
	}
	if resp.Factor.ID != "factor-1" || resp.Factor.Type != "mtls" || resp.Factor.UserID != 42 {
		t.Fatalf("unexpected factor record: %+v", resp.Factor)
	}
}

func TestHandleUserAuthFactorAdd_Errors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"already enrolled", qrdbsdk.ErrFactorAlreadyExists, http.StatusConflict},
		{"invalid factor", qrdbsdk.ErrInvalidFactor, http.StatusBadRequest},
		{"type unsupported", qrdbsdk.ErrFactorTypeUnsupported, http.StatusBadRequest},
		{"unknown", context.DeadlineExceeded, http.StatusInternalServerError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			uaf := &fakeUserAuthFactorService{
				addFn: func(context.Context, uint64, *qrdbsdk.AuthFactor) error { return c.err },
			}
			s := userFactorServer(t, nil, uaf)
			w := doUserFactorReq(t, s, http.MethodPost, "/v1/users/factors", `{"user_id":1,"type":"mtls"}`)
			if w.Code != c.want {
				t.Fatalf("expected %d, got %d", c.want, w.Code)
			}
		})
	}
}

func TestHandleUserAuthFactorAdd_BadBody(t *testing.T) {
	s := userFactorServer(t, nil, &fakeUserAuthFactorService{})
	w := doUserFactorReq(t, s, http.MethodPost, "/v1/users/factors", `not-json`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleUserAuthFactorList_Success(t *testing.T) {
	revoked := time.Now().UTC()
	uaf := &fakeUserAuthFactorService{
		listFn: func(_ context.Context, userID uint64) ([]*qrdbsdk.AuthFactor, error) {
			if userID != 42 {
				t.Fatalf("expected user 42, got %d", userID)
			}
			return []*qrdbsdk.AuthFactor{
				{ID: "f1", UserID: 42, Type: qrdbsdk.AuthFactorMTLS},
				{ID: "f2", UserID: 42, Type: qrdbsdk.AuthFactorPIV, RevokedAt: &revoked},
			}, nil
		},
	}
	s := userFactorServer(t, nil, uaf)

	w := doUserFactorReq(t, s, http.MethodGet, "/v1/users/factors/list?user_id=42", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp qrdbtransport.AuthFactorListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if resp.Total != 2 || len(resp.Factors) != 2 {
		t.Fatalf("expected 2 factors, got %+v", resp)
	}
	if resp.Factors[1].RevokedAt == nil {
		t.Fatal("revoked factor must carry revoked_at")
	}
}

func TestHandleUserAuthFactorList_InvalidUserID(t *testing.T) {
	s := userFactorServer(t, nil, &fakeUserAuthFactorService{})
	for _, q := range []string{"", "user_id=0", "user_id=abc"} {
		path := "/v1/users/factors/list"
		if q != "" {
			path += "?" + q
		}
		w := doUserFactorReq(t, s, http.MethodGet, path, "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%q: expected 400, got %d", q, w.Code)
		}
	}
}

func TestHandleUserAuthFactorGet_NotFound(t *testing.T) {
	uaf := &fakeUserAuthFactorService{
		getFn: func(context.Context, string) (*qrdbsdk.AuthFactor, error) {
			return nil, qrdbsdk.ErrFactorNotFound
		},
	}
	s := userFactorServer(t, nil, uaf)
	w := doUserFactorReq(t, s, http.MethodGet, "/v1/users/factors/ghost", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleUserAuthFactorRemove_Success(t *testing.T) {
	var gotForce bool
	var gotID string
	uaf := &fakeUserAuthFactorService{
		removeFn: func(_ context.Context, factorID string, force bool) error {
			gotID, gotForce = factorID, force
			return nil
		},
	}
	s := userFactorServer(t, nil, uaf)

	w := doUserFactorReq(t, s, http.MethodDelete, "/v1/users/factors/f1?force=true", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if gotID != "f1" || !gotForce {
		t.Fatalf("expected Remove(f1, force=true), got (%q, %v)", gotID, gotForce)
	}

	var resp qrdbtransport.AuthFactorRemoveResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode remove response: %v", err)
	}
	if resp.Status != "revoked" {
		t.Fatalf("expected revoked status, got %q", resp.Status)
	}
}

func TestHandleUserAuthFactorRemove_LastFactorConflict(t *testing.T) {
	uaf := &fakeUserAuthFactorService{
		removeFn: func(context.Context, string, bool) error { return qrdbsdk.ErrLastFactor },
	}
	s := userFactorServer(t, nil, uaf)
	w := doUserFactorReq(t, s, http.MethodDelete, "/v1/users/factors/f1", "")
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestHandleUserAuthFactorRelabel_Success(t *testing.T) {
	var relabeled string
	uaf := &fakeUserAuthFactorService{
		relabelFn: func(_ context.Context, factorID, label string) error {
			relabeled = factorID + ":" + label
			return nil
		},
		getFn: func(_ context.Context, factorID string) (*qrdbsdk.AuthFactor, error) {
			return &qrdbsdk.AuthFactor{ID: factorID, Label: "new label"}, nil
		},
	}
	s := userFactorServer(t, nil, uaf)

	w := doUserFactorReq(t, s, http.MethodPut, "/v1/users/factors/f1/label", `{"label":"new label"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if relabeled != "f1:new label" {
		t.Fatalf("expected Relabel(f1, new label), got %q", relabeled)
	}

	var resp qrdbtransport.AuthFactorRelabelResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode relabel response: %v", err)
	}
	if resp.Factor.Label != "new label" {
		t.Fatalf("expected updated label in response, got %+v", resp.Factor)
	}
}

func TestHandleUserAuthFactorRelabel_BadBody(t *testing.T) {
	s := userFactorServer(t, nil, &fakeUserAuthFactorService{})
	w := doUserFactorReq(t, s, http.MethodPut, "/v1/users/factors/f1/label", `not-json`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleUserAuthFactorRotate_Success(t *testing.T) {
	var gotOldID string
	uaf := &fakeUserAuthFactorService{
		rotateFn: func(_ context.Context, oldID string, newFactor *qrdbsdk.AuthFactor) error {
			gotOldID = oldID
			newFactor.ID = "factor-2"
			return nil
		},
	}
	s := userFactorServer(t, nil, uaf)

	body := `{"old_factor_id":"f1","new_factor":{"user_id":42,"type":"mtls"}}`
	w := doUserFactorReq(t, s, http.MethodPost, "/v1/users/factors/rotate", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotOldID != "f1" {
		t.Fatalf("expected rotate of f1, got %q", gotOldID)
	}

	var resp qrdbtransport.AuthFactorRotateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode rotate response: %v", err)
	}
	if resp.NewFactor.ID != "factor-2" {
		t.Fatalf("expected new factor id in response, got %+v", resp.NewFactor)
	}
}

func TestHandleUserAuthFactorRotate_RevokedGone(t *testing.T) {
	uaf := &fakeUserAuthFactorService{
		rotateFn: func(context.Context, string, *qrdbsdk.AuthFactor) error {
			return qrdbsdk.ErrFactorRevoked
		},
	}
	s := userFactorServer(t, nil, uaf)
	w := doUserFactorReq(t, s, http.MethodPost, "/v1/users/factors/rotate", `{"old_factor_id":"f1","new_factor":{"user_id":1,"type":"mtls"}}`)
	if w.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d", w.Code)
	}
}

func TestHandleUserAuthFactorLogin_Success(t *testing.T) {
	uaf := &fakeUserAuthFactorService{
		lookupFn: func(_ context.Context, ft qrdbsdk.AuthFactorType, key []byte) (*qrdbsdk.AuthFactor, error) {
			if ft != qrdbsdk.AuthFactorMTLS {
				t.Fatalf("expected mtls lookup, got %q", ft)
			}
			return &qrdbsdk.AuthFactor{ID: "f1", UserID: 42, Type: ft, Credential: key}, nil
		},
	}
	s := userFactorServer(t, nil, uaf)

	w := doUserFactorReq(t, s, http.MethodPost, "/v1/users/factors/login", `{"type":"mtls","lookup_key":"AQID"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp qrdbtransport.AuthFactorLoginResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if resp.UserID != 42 || !resp.Active || resp.Factor.ID != "f1" {
		t.Fatalf("unexpected login response: %+v", resp)
	}
}

func TestHandleUserAuthFactorLogin_UnknownKey(t *testing.T) {
	s := userFactorServer(t, nil, &fakeUserAuthFactorService{})
	w := doUserFactorReq(t, s, http.MethodPost, "/v1/users/factors/login", `{"type":"mtls","lookup_key":"AQID"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// =============================================================================
// Error-type behavior
// =============================================================================

func TestQRDBUserFactorErrorTypes(t *testing.T) {
	if (&QRDBUserServiceNotImplementedError{}).Error() != "qrdb user service not configured" {
		t.Fatal("unexpected user not-implemented message")
	}
	if (&UserAuthFactorNotImplementedError{}).Error() != "qrdb user auth-factor service not configured" {
		t.Fatal("unexpected factor not-implemented message")
	}
	if got := (&UserAuthFactorMissingParameterError{Param: "user_id"}).Error(); !strings.Contains(got, "user_id") {
		t.Fatalf("missing-parameter message should contain param, got %q", got)
	}
	if got := (&UserAuthFactorReadBodyError{}).Error(); got != "invalid user auth-factor request body" {
		t.Fatalf("unexpected nil-wrapped body message: %q", got)
	}
	wrapped := &UserAuthFactorReadBodyError{Err: context.Canceled}
	if !strings.Contains(wrapped.Error(), context.Canceled.Error()) {
		t.Fatal("wrapped body error should include underlying message")
	}
	if wrapped.Unwrap() != context.Canceled {
		t.Fatal("Unwrap should return underlying error")
	}
}

// =============================================================================
// Default-deny scope table coverage for the new mounts
// =============================================================================

func TestRequiredScopeForRequest_QRDBUserWireMounts(t *testing.T) {
	cases := []struct {
		path, method, want string
	}{
		{"/users/by-username/admin", http.MethodGet, "users:read"},
		{"/v1/users/factors", http.MethodPost, "user_authfactors:add"},
		{"/v1/users/factors/list", http.MethodGet, "user_authfactors:get"},
		{"/v1/users/factors/f1", http.MethodDelete, "user_authfactors:add"},
		{"/v1/users/factors/f1/label", http.MethodPut, "user_authfactors:add"},
	}
	for _, c := range cases {
		if got := requiredScopeForRequest(c.path, c.method); got != c.want {
			t.Fatalf("%s %s: expected scope %q, got %q", c.method, c.path, c.want, got)
		}
	}
}
