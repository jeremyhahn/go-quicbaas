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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// credJSON marshals a qrdb credential wire struct into a request body.
//
// The credential wire types carry their values as []byte so binary material
// round-trips byte-for-byte; over JSON that renders as base64 (RFC 4648), not
// as a plain string. Marshalling the real wire struct keeps these tests
// honest about the encoding instead of hand-writing a literal that the
// decoder would reject.
func credJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return string(b)
}

// fakeCredentialService is a hand-written test double for the qrdb
// CredentialServicer interface. Each method delegates to an optional
// function field so individual tests can shape happy-path and error
// behavior without a heavyweight mocking framework.
type fakeCredentialService struct {
	submitFn          func(ctx context.Context, name string, value []byte) error
	strategyFn        func() string
	listFn            func(ctx context.Context) ([]string, error)
	deleteFn          func(ctx context.Context, name string) error
	rotateFn          func(ctx context.Context, name string, opts qrdbsdk.RotateOptions) (*qrdbsdk.RotateResult, error)
	listVersionsFn    func(ctx context.Context, name string) ([]qrdbsdk.CredentialVersion, error)
	retrieveVersionFn func(ctx context.Context, name string, version uint64) ([]byte, error)
	gcFn              func(ctx context.Context, name string) (int, error)
}

func (f *fakeCredentialService) CredentialStore(ctx context.Context, name string, value []byte) error {
	return nil
}

func (f *fakeCredentialService) CredentialRetrieve(ctx context.Context, name string) ([]byte, error) {
	return nil, nil
}

func (f *fakeCredentialService) CredentialSubmit(ctx context.Context, name string, value []byte) error {
	if f.submitFn != nil {
		return f.submitFn(ctx, name, value)
	}
	return nil
}

func (f *fakeCredentialService) CredentialDelete(ctx context.Context, name string) error {
	if f.deleteFn != nil {
		return f.deleteFn(ctx, name)
	}
	return nil
}

func (f *fakeCredentialService) CredentialStrategy() string {
	if f.strategyFn != nil {
		return f.strategyFn()
	}
	return "barrier"
}

func (f *fakeCredentialService) CredentialList(ctx context.Context) ([]string, error) {
	if f.listFn != nil {
		return f.listFn(ctx)
	}
	return nil, nil
}

func (f *fakeCredentialService) CredentialRotate(ctx context.Context, name string, opts qrdbsdk.RotateOptions) (*qrdbsdk.RotateResult, error) {
	if f.rotateFn != nil {
		return f.rotateFn(ctx, name, opts)
	}
	return &qrdbsdk.RotateResult{Name: name, NewVersion: 1}, nil
}

func (f *fakeCredentialService) CredentialRetrieveVersion(ctx context.Context, name string, version uint64) ([]byte, error) {
	if f.retrieveVersionFn != nil {
		return f.retrieveVersionFn(ctx, name, version)
	}
	return nil, nil
}

func (f *fakeCredentialService) CredentialListVersions(ctx context.Context, name string) ([]qrdbsdk.CredentialVersion, error) {
	if f.listVersionsFn != nil {
		return f.listVersionsFn(ctx, name)
	}
	return nil, nil
}

func (f *fakeCredentialService) CredentialGCExpired(ctx context.Context, name string) (int, error) {
	if f.gcFn != nil {
		return f.gcFn(ctx, name)
	}
	return 0, nil
}

// credServer returns a router-backed server with the given credential
// service installed. Pass nil to exercise the "service not configured"
// 501 path.
func credServer(t *testing.T, svc *fakeCredentialService) *Server {
	t.Helper()
	s, _ := newTestServer(t)
	if svc != nil {
		s.SetCredentialService(svc)
	}
	return s
}

func doCredReq(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
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
// Error-type behavior
// =============================================================================

func TestCredentialErrorTypes(t *testing.T) {
	if (&CredentialNotImplementedError{}).Error() != "credential service not configured" {
		t.Fatal("unexpected not-implemented message")
	}
	if (&CredentialMissingNameError{}).Error() != "credential name is required" {
		t.Fatal("unexpected missing-name message")
	}
	if got := (&CredentialInvalidVersionError{Value: "x"}).Error(); !strings.Contains(got, "x") {
		t.Fatalf("invalid-version message should contain value, got %q", got)
	}
	// nil-wrapped body error uses the fallback message.
	if got := (&CredentialReadBodyError{}).Error(); got != "invalid credential request body" {
		t.Fatalf("unexpected nil-wrapped body message: %q", got)
	}
	wrapped := &CredentialReadBodyError{Err: context.Canceled}
	if !strings.Contains(wrapped.Error(), context.Canceled.Error()) {
		t.Fatal("wrapped body error should include underlying message")
	}
	if wrapped.Unwrap() != context.Canceled {
		t.Fatal("Unwrap should return underlying error")
	}
}

// =============================================================================
// Service-not-configured (501) across every route
// =============================================================================

func TestCredentialRoutes_ServiceNotConfigured(t *testing.T) {
	s := credServer(t, nil)

	cases := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/v1/credentials/", ""},
		{http.MethodGet, "/v1/credentials/strategy", ""},
		{http.MethodPost, "/v1/credentials/submit", `{"name":"a","value":"b"}`},
		{http.MethodDelete, "/v1/credentials/a", ""},
		{http.MethodPost, "/v1/credentials/a/rotate", `{"new_value":"x"}`},
		{http.MethodPost, "/v1/credentials/a/gc", ""},
		{http.MethodGet, "/v1/credentials/a/versions", ""},
		{http.MethodGet, "/v1/credentials/a/versions/1", ""},
	}
	for _, c := range cases {
		w := doCredReq(t, s, c.method, c.path, c.body)
		if w.Code != http.StatusNotImplemented {
			t.Fatalf("%s %s: expected 501, got %d", c.method, c.path, w.Code)
		}
	}
}

// =============================================================================
// Submit
// =============================================================================

func TestHandleCredentialSubmit(t *testing.T) {
	var gotName string
	var gotValue []byte
	svc := &fakeCredentialService{
		submitFn: func(ctx context.Context, name string, value []byte) error {
			gotName, gotValue = name, value
			return nil
		},
	}
	s := credServer(t, svc)

	w := doCredReq(t, s, http.MethodPost, "/v1/credentials/submit",
		credJSON(t, qrdbtransport.CredentialSubmitRequest{Name: "db", Value: []byte("secret")}))
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if gotName != "db" || string(gotValue) != "secret" {
		t.Fatalf("submit got name=%q value=%q", gotName, string(gotValue))
	}
}

func TestHandleCredentialSubmit_BadBody(t *testing.T) {
	s := credServer(t, &fakeCredentialService{})
	w := doCredReq(t, s, http.MethodPost, "/v1/credentials/submit", `{not json`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleCredentialSubmit_ServiceError(t *testing.T) {
	svc := &fakeCredentialService{
		submitFn: func(ctx context.Context, name string, value []byte) error {
			return qrdbsdk.ErrEmptyCredentialValue
		},
	}
	w := doCredReq(t, credServer(t, svc), http.MethodPost, "/v1/credentials/submit", `{"name":"a","value":""}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty value, got %d", w.Code)
	}
}

// =============================================================================
// Strategy
// =============================================================================

func TestHandleCredentialStrategy(t *testing.T) {
	svc := &fakeCredentialService{strategyFn: func() string { return "manual" }}
	w := doCredReq(t, credServer(t, svc), http.MethodGet, "/v1/credentials/strategy", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp qrdbtransport.CredentialStrategyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Strategy != "manual" {
		t.Fatalf("expected strategy manual, got %q", resp.Strategy)
	}
}

// =============================================================================
// List
// =============================================================================

func TestHandleCredentialList(t *testing.T) {
	svc := &fakeCredentialService{
		listFn: func(ctx context.Context) ([]string, error) { return []string{"a", "b"}, nil },
	}
	w := doCredReq(t, credServer(t, svc), http.MethodGet, "/v1/credentials/", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp qrdbtransport.CredentialListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Names) != 2 {
		t.Fatalf("expected 2 names, got %v", resp.Names)
	}
}

func TestHandleCredentialList_NilNamesNormalizedToEmpty(t *testing.T) {
	svc := &fakeCredentialService{
		listFn: func(ctx context.Context) ([]string, error) { return nil, nil },
	}
	w := doCredReq(t, credServer(t, svc), http.MethodGet, "/v1/credentials/", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// nil slice must serialize as [] not null.
	if !strings.Contains(w.Body.String(), "[]") {
		t.Fatalf("expected empty array, got %s", w.Body.String())
	}
}

func TestHandleCredentialList_EnumerationNotSupported(t *testing.T) {
	svc := &fakeCredentialService{
		listFn: func(ctx context.Context) ([]string, error) {
			return nil, qrdbsdk.ErrEnumerationNotSupported
		},
	}
	w := doCredReq(t, credServer(t, svc), http.MethodGet, "/v1/credentials/", "")
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", w.Code)
	}
}

// =============================================================================
// Delete
// =============================================================================

func TestHandleCredentialDelete(t *testing.T) {
	svc := &fakeCredentialService{deleteFn: func(ctx context.Context, name string) error { return nil }}
	w := doCredReq(t, credServer(t, svc), http.MethodDelete, "/v1/credentials/mycred", "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestHandleCredentialDelete_NotFound(t *testing.T) {
	svc := &fakeCredentialService{
		deleteFn: func(ctx context.Context, name string) error { return qrdbsdk.ErrCredentialNotFound },
	}
	w := doCredReq(t, credServer(t, svc), http.MethodDelete, "/v1/credentials/missing", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// =============================================================================
// Rotate
// =============================================================================

func TestHandleCredentialRotate_WithGrace(t *testing.T) {
	grace := time.Now().Add(time.Hour)
	var gotOpts qrdbsdk.RotateOptions
	svc := &fakeCredentialService{
		rotateFn: func(ctx context.Context, name string, opts qrdbsdk.RotateOptions) (*qrdbsdk.RotateResult, error) {
			gotOpts = opts
			return &qrdbsdk.RotateResult{
				Name:            name,
				PreviousVersion: 1,
				NewVersion:      2,
				GraceUntil:      grace,
			}, nil
		},
	}
	w := doCredReq(t, credServer(t, svc), http.MethodPost, "/v1/credentials/db/rotate",
		credJSON(t, qrdbtransport.CredentialRotateRequest{
			Name:               "ignored",
			NewValue:           []byte("newpass"),
			GracePeriodSeconds: 30,
		}))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// Path name is authoritative over the body name.
	if string(gotOpts.NewValue) != "newpass" || gotOpts.GracePeriod != 30*time.Second {
		t.Fatalf("unexpected rotate opts: %+v", gotOpts)
	}
	var resp qrdbtransport.CredentialRotateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Name != "db" || resp.PreviousVersion != 1 || resp.NewVersion != 2 {
		t.Fatalf("unexpected rotate response: %+v", resp)
	}
	if resp.GraceUntilUnix != grace.Unix() {
		t.Fatalf("expected grace unix %d, got %d", grace.Unix(), resp.GraceUntilUnix)
	}
}

func TestHandleCredentialRotate_NoGraceOmitsField(t *testing.T) {
	svc := &fakeCredentialService{
		rotateFn: func(ctx context.Context, name string, opts qrdbsdk.RotateOptions) (*qrdbsdk.RotateResult, error) {
			return &qrdbsdk.RotateResult{Name: name, NewVersion: 1}, nil
		},
	}
	w := doCredReq(t, credServer(t, svc), http.MethodPost, "/v1/credentials/db/rotate",
		credJSON(t, qrdbtransport.CredentialRotateRequest{NewValue: []byte("x")}))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "grace_until_unix") {
		t.Fatalf("zero grace should omit grace_until_unix, got %s", w.Body.String())
	}
}

func TestHandleCredentialRotate_BadBody(t *testing.T) {
	w := doCredReq(t, credServer(t, &fakeCredentialService{}), http.MethodPost, "/v1/credentials/db/rotate", `{bad`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleCredentialRotate_InvalidGracePeriod(t *testing.T) {
	svc := &fakeCredentialService{
		rotateFn: func(ctx context.Context, name string, opts qrdbsdk.RotateOptions) (*qrdbsdk.RotateResult, error) {
			return nil, qrdbsdk.ErrInvalidGracePeriod
		},
	}
	w := doCredReq(t, credServer(t, svc), http.MethodPost, "/v1/credentials/db/rotate",
		credJSON(t, qrdbtransport.CredentialRotateRequest{NewValue: []byte("x")}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// =============================================================================
// List versions
// =============================================================================

func TestHandleCredentialListVersions(t *testing.T) {
	created := time.Now().Add(-time.Hour)
	grace := time.Now().Add(time.Hour)
	svc := &fakeCredentialService{
		listVersionsFn: func(ctx context.Context, name string) ([]qrdbsdk.CredentialVersion, error) {
			return []qrdbsdk.CredentialVersion{
				{Version: 2, IsCurrent: true, CreatedAt: created},
				{Version: 1, IsCurrent: false, CreatedAt: created, GraceUntil: grace},
			}, nil
		},
	}
	w := doCredReq(t, credServer(t, svc), http.MethodGet, "/v1/credentials/db/versions", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp qrdbtransport.CredentialListVersionsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Name != "db" || len(resp.Versions) != 2 {
		t.Fatalf("unexpected versions response: %+v", resp)
	}
	if resp.Versions[0].CreatedAtUnix != created.Unix() {
		t.Fatalf("current version should carry created-at unix")
	}
	if resp.Versions[1].GraceUntilUnix != grace.Unix() {
		t.Fatalf("grace version should carry grace-until unix")
	}
}

func TestHandleCredentialListVersions_NotFound(t *testing.T) {
	svc := &fakeCredentialService{
		listVersionsFn: func(ctx context.Context, name string) ([]qrdbsdk.CredentialVersion, error) {
			return nil, qrdbsdk.ErrCredentialNotFound
		},
	}
	w := doCredReq(t, credServer(t, svc), http.MethodGet, "/v1/credentials/x/versions", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// =============================================================================
// Retrieve version
// =============================================================================

func TestHandleCredentialRetrieveVersion(t *testing.T) {
	var gotVersion uint64
	svc := &fakeCredentialService{
		retrieveVersionFn: func(ctx context.Context, name string, version uint64) ([]byte, error) {
			gotVersion = version
			return []byte("the-value"), nil
		},
	}
	w := doCredReq(t, credServer(t, svc), http.MethodGet, "/v1/credentials/db/versions/3", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if gotVersion != 3 {
		t.Fatalf("expected version 3, got %d", gotVersion)
	}
	var resp qrdbtransport.CredentialRetrieveVersionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(resp.Value) != "the-value" {
		t.Fatalf("unexpected value %q", resp.Value)
	}
}

func TestHandleCredentialRetrieveVersion_InvalidVersion(t *testing.T) {
	w := doCredReq(t, credServer(t, &fakeCredentialService{}), http.MethodGet, "/v1/credentials/db/versions/abc", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-numeric version, got %d", w.Code)
	}
}

func TestHandleCredentialRetrieveVersion_Expired(t *testing.T) {
	svc := &fakeCredentialService{
		retrieveVersionFn: func(ctx context.Context, name string, version uint64) ([]byte, error) {
			return nil, qrdbsdk.ErrVersionExpired
		},
	}
	w := doCredReq(t, credServer(t, svc), http.MethodGet, "/v1/credentials/db/versions/1", "")
	if w.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d", w.Code)
	}
}

// =============================================================================
// GC expired
// =============================================================================

func TestHandleCredentialGCExpired(t *testing.T) {
	svc := &fakeCredentialService{
		gcFn: func(ctx context.Context, name string) (int, error) { return 4, nil },
	}
	w := doCredReq(t, credServer(t, svc), http.MethodPost, "/v1/credentials/db/gc", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp qrdbtransport.CredentialGCResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Reaped != 4 {
		t.Fatalf("expected 4 reaped, got %d", resp.Reaped)
	}
}

func TestHandleCredentialGCExpired_Error(t *testing.T) {
	svc := &fakeCredentialService{
		gcFn: func(ctx context.Context, name string) (int, error) {
			return 0, qrdbsdk.ErrCredentialNotFound
		},
	}
	w := doCredReq(t, credServer(t, svc), http.MethodPost, "/v1/credentials/db/gc", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// =============================================================================
// mapCredentialHTTPError unit coverage for the status-code matrix
// =============================================================================

func TestMapCredentialHTTPError(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{qrdbsdk.ErrCredentialNotFound, http.StatusNotFound},
		{qrdbsdk.ErrEnumerationNotSupported, http.StatusNotImplemented},
		{qrdbsdk.ErrVersionNotFound, http.StatusGone},
		{qrdbsdk.ErrVersionExpired, http.StatusGone},
		{qrdbsdk.ErrInvalidGracePeriod, http.StatusBadRequest},
		{qrdbsdk.ErrEmptyCredentialName, http.StatusBadRequest},
		{qrdbsdk.ErrEmptyCredentialValue, http.StatusBadRequest},
		{qrdbsdk.ErrInvalidStrategy, http.StatusInternalServerError},
		{context.Canceled, http.StatusInternalServerError},
	}
	for _, c := range cases {
		if got := mapCredentialHTTPError(c.err); got != c.want {
			t.Fatalf("mapCredentialHTTPError(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}
