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
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	qrdbrest "github.com/jeremyhahn/go-qrdb/sdk/go/transport/rest"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// POST /v1/barrier/enable-encryption — the go-qrdb wire mount (go-quicbaas#990)
//
// This path is go-qrdb's, re-served here. Its client is go-qrdb's REST
// transport, which marshals transport.BarrierEnableEncryptionRequest
// (`strategy`, body `tenant_id` as the scope) and decodes a
// transport.BarrierEnableEncryptionResponse (`{status, scope}`) from a 200.
// The quicbaas-owned /api/v1 route keeps `target_strategy` and 204; the tests
// for that shape live in handlers_barrier_enable_encryption_test.go.
// =============================================================================

// enableEncryptionCapture records what the barrier service was asked to do.
// The credential is protected memory the handler destroys once the call
// returns, so it is opened and copied from inside the call.
type enableEncryptionCapture struct {
	called   bool
	tenantID string
	opts     qrdbsdk.MigrationOptions
	// passphraseIsWire reports whether the forwarded credential is
	// wirePassphrase byte for byte.
	passphraseIsWire bool
}

func (c *enableEncryptionCapture) service(t *testing.T) *fakeBarrierService {
	t.Helper()
	return &fakeBarrierService{
		enableEncryptionFn: func(_ context.Context, tenantID string, opts qrdbsdk.MigrationOptions) error {
			c.called = true
			c.tenantID = tenantID
			c.opts = opts
			if opts.Credentials.Secret != nil {
				plain, err := opts.Credentials.Secret.Open()
				if err != nil {
					t.Errorf("open the forwarded credential: %v", err)
					return nil
				}
				defer plain.Destroy()
				c.passphraseIsWire = plain.EqualTo(wirePassphrase)
			}
			return nil
		},
	}
}

// scopeDecision is one call the handler made to its scope authorizer.
type scopeDecision struct {
	// tenant is the target tenant bound to the context ("" = system).
	tenant   string
	resource string
	action   string
}

// scopeAuthorizerStub records every scope decision and answers with err.
// The decision itself is go-qrdb's RBAC authorizer's; the real one is
// exercised end to end against a running daemon in
// test/integration/tenantbarrier/enable_encryption_wire_test.go.
type scopeAuthorizerStub struct {
	mu        sync.Mutex
	err       error
	decisions []scopeDecision
}

func (a *scopeAuthorizerStub) Authorize(ctx context.Context, _ *qrdbsdk.Identity, resource, action string) error {
	tenant, _ := qrdbsdk.TenantFromContext(ctx)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.decisions = append(a.decisions, scopeDecision{tenant: tenant, resource: resource, action: action})
	return a.err
}

// made returns the decisions recorded so far.
func (a *scopeAuthorizerStub) made() []scopeDecision {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]scopeDecision(nil), a.decisions...)
}

// wireCaller is the authenticated identity go-qrdb's middleware places in
// the request context before the handler runs.
var wireCaller = &qrdbsdk.Identity{Subject: "operator", TenantID: "acme", Roles: []string{"admin"}}

// wireServer is barrierServer plus the scope authorizer the wire mount
// consults, admitting every decision unless the test sets its err.
func wireServer(t *testing.T, svc *fakeBarrierService) (*Server, *scopeAuthorizerStub) {
	t.Helper()
	s := barrierServer(t, svc)
	authz := &scopeAuthorizerStub{}
	s.SetBarrierScopeAuthorizer(authz)
	return s, authz
}

// asCaller serves s's router with identity authenticated, as go-qrdb's
// middleware does on the unified server.
func asCaller(s *Server, identity *qrdbsdk.Identity) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.router.ServeHTTP(w, r.WithContext(qrdbsdk.WithIdentity(r.Context(), identity)))
	})
}

// doWireReq POSTs body to the wire mount through h.
func doWireReq(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/v1/barrier/enable-encryption", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// newQRDBRESTClientOver is newQRDBRESTClient over an arbitrary handler, so
// the go-qrdb REST transport reaches the router as an authenticated caller.
func newQRDBRESTClientOver(t *testing.T, h http.Handler) *qrdbrest.Transport {
	t.Helper()

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	tr, err := qrdbrest.New(qrdbtransport.WithAddress(srv.URL))
	if err != nil {
		t.Fatalf("qrdbrest.New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	if err := tr.Connect(context.Background()); err != nil && !strings.Contains(err.Error(), "health check failed") {
		t.Fatalf("connect: %v", err)
	}
	return tr
}

// TestBarrierWire_EnableEncryptionSystemScope drives the REAL go-qrdb REST
// transport — the client `quicbaas barrier enable-encryption --transport rest`
// uses — against the router. Before #990 this failed with
// `validation error: target_strategy is required`, and a successful migration
// still surfaced as a JSON decode error (EOF) because the handler answered 204.
func TestBarrierWire_EnableEncryptionSystemScope(t *testing.T) {
	var got enableEncryptionCapture
	s, authz := wireServer(t, got.service(t))
	client := newQRDBRESTClientOver(t, asCaller(s, wireCaller))

	resp, err := client.BarrierEnableEncryption(context.Background(), &qrdbtransport.BarrierEnableEncryptionRequest{
		Strategy:   "passphrase",
		Passphrase: append([]byte(nil), wirePassphrase...),
	})
	if err != nil {
		t.Fatalf("BarrierEnableEncryption over the qrdb REST transport: %v", err)
	}
	if !got.called {
		t.Fatal("the barrier service was never asked to migrate")
	}
	if got.tenantID != "" {
		t.Errorf("no tenant_id was sent, so the system barrier is the scope; service saw tenant %q", got.tenantID)
	}
	if got.opts.TargetStrategy != qrdbsdk.StrategyID("passphrase") {
		t.Errorf("strategy reached the service as %q, want %q", got.opts.TargetStrategy, "passphrase")
	}
	if !got.passphraseIsWire {
		t.Error("passphrase did not reach the service byte for byte")
	}
	if resp.Status != barrierMigrationStarted {
		t.Errorf("status = %q, want %q", resp.Status, barrierMigrationStarted)
	}
	if resp.Scope != barrierScopeSystem {
		t.Errorf("scope = %q, want %q", resp.Scope, barrierScopeSystem)
	}
	want := []scopeDecision{{tenant: "", resource: "seal", action: "enable_encryption"}}
	if !reflect.DeepEqual(authz.made(), want) {
		t.Errorf("scope decisions = %+v, want %+v", authz.made(), want)
	}
}

// TestBarrierWire_EnableEncryptionTenantScope asserts the body `tenant_id` is
// the scope selector on this mount, exactly as go-qrdb serves it. Before #990
// it was silently dropped and the SYSTEM barrier was migrated instead.
// It is authorized as tenant_barrier:enable_encryption against THAT tenant.
func TestBarrierWire_EnableEncryptionTenantScope(t *testing.T) {
	var got enableEncryptionCapture
	s, authz := wireServer(t, got.service(t))
	client := newQRDBRESTClientOver(t, asCaller(s, wireCaller))

	resp, err := client.BarrierEnableEncryption(context.Background(), &qrdbtransport.BarrierEnableEncryptionRequest{
		TenantID:        "acme",
		Strategy:        "shamir",
		Mechanism:       "inplace",
		Availability:    "quiesced",
		ShamirThreshold: 2,
		ShamirTotal:     3,
	})
	if err != nil {
		t.Fatalf("BarrierEnableEncryption over the qrdb REST transport: %v", err)
	}
	if got.tenantID != "acme" {
		t.Errorf("tenant scope reached the service as %q, want %q", got.tenantID, "acme")
	}
	if got.opts.TargetStrategy != qrdbsdk.StrategyID("shamir") {
		t.Errorf("strategy = %q, want shamir", got.opts.TargetStrategy)
	}
	if got.opts.Mechanism != qrdbsdk.MigrationMechanism("inplace") ||
		got.opts.Availability != qrdbsdk.MigrationAvailability("quiesced") {
		t.Errorf("mechanism/availability = %q/%q, want inplace/quiesced", got.opts.Mechanism, got.opts.Availability)
	}
	if got.opts.ShamirThreshold != 2 || got.opts.ShamirTotal != 3 {
		t.Errorf("quorum = %d-of-%d, want 2-of-3", got.opts.ShamirThreshold, got.opts.ShamirTotal)
	}
	if resp.Status != barrierMigrationStarted || resp.Scope != barrierScopeTenant {
		t.Errorf("response = %+v, want {%s %s}", resp, barrierMigrationStarted, barrierScopeTenant)
	}
	want := []scopeDecision{{tenant: "acme", resource: "tenant_barrier", action: "enable_encryption"}}
	if !reflect.DeepEqual(authz.made(), want) {
		t.Errorf("scope decisions = %+v, want %+v", authz.made(), want)
	}
}

// TestBarrierWire_EnableEncryptionDefaultsApplied pins the same defaults the
// /api/v1 route applies, so the two mounts migrate identically.
func TestBarrierWire_EnableEncryptionDefaultsApplied(t *testing.T) {
	var got enableEncryptionCapture
	s, _ := wireServer(t, got.service(t))
	w := doWireReq(t, asCaller(s, wireCaller), `{"strategy":"passphrase","passphrase":"eA=="}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	if got.opts.Mechanism != qrdbsdk.MechanismSwap {
		t.Errorf("mechanism default = %q, want %q", got.opts.Mechanism, qrdbsdk.MechanismSwap)
	}
	if got.opts.Availability != qrdbsdk.AvailabilityOnline {
		t.Errorf("availability default = %q, want %q", got.opts.Availability, qrdbsdk.AvailabilityOnline)
	}
	var body qrdbtransport.BarrierEnableEncryptionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v (body=%s)", err, w.Body.String())
	}
}

// TestBarrierWire_EnableEncryptionRequiresStrategy asserts the wire mount
// names ITS field in the 400, and never reaches the service.
func TestBarrierWire_EnableEncryptionRequiresStrategy(t *testing.T) {
	var got enableEncryptionCapture
	s, _ := wireServer(t, got.service(t))
	w := doWireReq(t, asCaller(s, wireCaller), `{"target_strategy":"passphrase"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "strategy is required") ||
		strings.Contains(w.Body.String(), "target_strategy") {
		t.Errorf("400 should name the wire field `strategy`, body=%s", w.Body.String())
	}
	if got.called {
		t.Error("the service was called for a request with no strategy")
	}
}

// TestBarrierWire_EnableEncryptionErrors pins the error mapping for both
// scopes: a forbidden migration is 403, an unwired facade is 503.
func TestBarrierWire_EnableEncryptionErrors(t *testing.T) {
	forbidden := &fakeBarrierService{
		enableEncryptionFn: func(context.Context, string, qrdbsdk.MigrationOptions) error {
			return &dberrors.QBError{Code: dberrors.ErrForbidden, Err: errBarrierBoom}
		},
	}
	for _, body := range []string{`{"strategy":"shamir"}`, `{"strategy":"shamir","tenant_id":"other"}`} {
		s, _ := wireServer(t, forbidden)
		w := doWireReq(t, asCaller(s, wireCaller), body)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: expected 403, got %d (body=%s)", body, w.Code, w.Body.String())
		}
	}

	s, _, cleanup := setupMockServer(t)
	defer cleanup()
	w := doWireReq(t, asCaller(s, wireCaller), `{"strategy":"shamir"}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("unwired facade: expected 503, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// TestBarrierWire_EnableEncryptionRequestIsQRDBAlias keeps the wire mount's
// request shape the go-qrdb type itself, so an upstream rename is a compile
// error here rather than a silent mis-decode (the #990 defect class).
func TestBarrierWire_EnableEncryptionRequestIsQRDBAlias(t *testing.T) {
	local := reflect.TypeOf(barrierWireEnableEncryptionRequest{})
	sdk := reflect.TypeOf(qrdbtransport.BarrierEnableEncryptionRequest{})
	if local != sdk {
		t.Fatalf("%s is a local copy of %s, not an alias of it", local, sdk)
	}
}

// TestBarrierWire_EnableEncryptionScopeRefusals pins every way the scope
// authorization refuses, and that none of them reaches the service.
func TestBarrierWire_EnableEncryptionScopeRefusals(t *testing.T) {
	cases := []struct {
		name     string
		authzErr error
		unwired  bool
		identity *qrdbsdk.Identity
		want     int
	}{
		{name: "denied scope is 403", authzErr: errors.New("insufficient permissions"), identity: wireCaller, want: http.StatusForbidden},
		{
			name:     "authorizer unable to decide is 503",
			authzErr: &BarrierScopeAuthorizerUnavailableError{Reason: "node composing"},
			identity: wireCaller, want: http.StatusServiceUnavailable,
		},
		{name: "unwired authorizer fails closed with 503", unwired: true, identity: wireCaller, want: http.StatusServiceUnavailable},
		{name: "no identity is 401", want: http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got enableEncryptionCapture
			s, authz := wireServer(t, got.service(t))
			authz.err = tc.authzErr
			if tc.unwired {
				s.SetBarrierScopeAuthorizer(nil)
			}
			var h http.Handler = s.router
			if tc.identity != nil {
				h = asCaller(s, tc.identity)
			}
			w := doWireReq(t, h, `{"strategy":"passphrase","passphrase":"eA==","tenant_id":"acme"}`)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tc.want, w.Body.String())
			}
			if got.called {
				t.Error("a refused request reached the barrier service")
			}
		})
	}
}

// TestBarrierWire_EnableEncryptionSystemScopeBindsSystemTenant asserts the
// system decision is made against the SYSTEM tenant even when the request
// context already carries the caller's tenant, so a tenant binding upstream
// can never turn the system check into a self-tenant one.
func TestBarrierWire_EnableEncryptionSystemScopeBindsSystemTenant(t *testing.T) {
	var got enableEncryptionCapture
	s, authz := wireServer(t, got.service(t))
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := qrdbsdk.ContextWithTenant(r.Context(), "acme")
		asCaller(s, wireCaller).ServeHTTP(w, r.WithContext(ctx))
	})
	w := doWireReq(t, h, `{"strategy":"passphrase","passphrase":"eA=="}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	want := []scopeDecision{{tenant: "", resource: "seal", action: "enable_encryption"}}
	if !reflect.DeepEqual(authz.made(), want) {
		t.Errorf("scope decisions = %+v, want %+v", authz.made(), want)
	}
}
