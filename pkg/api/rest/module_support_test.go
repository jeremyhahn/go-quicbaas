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
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

func newModuleSupportServer(t *testing.T) *Server {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.MaxBodySize = 16
	cfg.MaxUploadBodySize = 32
	srv, err := NewServer(testutil.NewMockQuicBaaS(), cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// newModuleSupportServerWithFileLogger builds a server exactly like
// newModuleSupportServer, but at "warn" level with its log routed to a file
// this test can read back — the only way to assert a log LINE was written
// (rather than merely that a typed error came back), which is the entire
// point of the AuthenticateHTTP silent-branch defect these tests guard
// against: a caller cannot tell "refused, and it says why in the log" from
// "refused, silently" by inspecting the returned error alone.
func newModuleSupportServerWithFileLogger(t *testing.T) (*Server, string) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "auth.log")
	log, err := logger.New(logger.LogConfig{Level: "warn", Format: "json", Output: "file", File: logPath})
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	srv, err := NewServer(testutil.NewMockQuicBaaS(), cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	return srv, logPath
}

// readLog returns the log file's contents, or "" if it has not been written
// yet (a logger that buffers may not have flushed before the caller reads).
func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read log file: %v", err)
	}
	return string(data)
}

// fakeModuleTokenAuthorizer is a ModuleTokenAuthorizer test double. Validate
// authenticates wantToken (mirroring platformtoken.Validator's real
// signature/iss/aud/exp/DPoP/liveness pipeline collapsed to a single
// equality check) and projects the configured Subject/TenantID/Scopes,
// exactly as AuthenticateHTTP consumes it.
type fakeModuleTokenAuthorizer struct {
	wantToken string
	granted   []string
	subject   string
	userID    uint64
	tenant    string
}

func (f *fakeModuleTokenAuthorizer) authenticate(token string) (*platformtoken.AuthorizedContext, error) {
	if token != f.wantToken {
		return nil, &platformtoken.InvalidTokenError{Detail: "signature verification failed"}
	}
	return &platformtoken.AuthorizedContext{
		Subject:  f.subject,
		Username: f.subject,
		UserID:   f.userID,
		TenantID: f.tenant,
		Scopes:   f.granted,
	}, nil
}

func (f *fakeModuleTokenAuthorizer) Authorize(_ context.Context, token, requiredPerm, _, _, _ string) (*platformtoken.AuthorizedContext, error) {
	ac, err := f.authenticate(token)
	if err != nil {
		return nil, err
	}
	if err := ac.RequireScope(requiredPerm); err != nil {
		return nil, err
	}
	return ac, nil
}

func (f *fakeModuleTokenAuthorizer) Validate(_ context.Context, token, _, _, _ string) (*platformtoken.AuthorizedContext, error) {
	return f.authenticate(token)
}

// bodyEchoSpec builds a RouteSpec whose handler reads the request body and
// answers 413 on a MaxBytesReader error, mirroring the production handlers'
// oversized-body contract.
func bodyEchoSpec(group RouteGroup) RouteSpec {
	return RouteSpec{
		Method:  "POST",
		Pattern: "/api/v1/test",
		Group:   group,
		Handler: func(s *Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				if _, err := io.ReadAll(r.Body); err != nil {
					if IsMaxBytesError(err) {
						w.WriteHeader(http.StatusRequestEntityTooLarge)
						return
					}
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusOK)
			}
		},
	}
}

// TestBindRoute_BodyLimit proves BindRoute applies the group's request body
// limit: within-limit bodies pass, oversized bodies fail, and the upload
// group carries the larger limit.
func TestBindRoute_BodyLimit(t *testing.T) {
	srv := newModuleSupportServer(t)

	cases := []struct {
		name  string
		group RouteGroup
		body  string
		want  int
	}{
		{"api within limit", GroupAPI, strings.Repeat("a", 8), http.StatusOK},
		{"api over limit", GroupAPI, strings.Repeat("a", 64), http.StatusRequestEntityTooLarge},
		{"upload within larger limit", GroupAPIUpload, strings.Repeat("a", 24), http.StatusOK},
		{"upload over limit", GroupAPIUpload, strings.Repeat("a", 64), http.StatusRequestEntityTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := srv.BindRoute(bodyEchoSpec(tc.group))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/test", strings.NewReader(tc.body)))
			if w.Code != tc.want {
				t.Errorf("status = %d, want %d", w.Code, tc.want)
			}
		})
	}
}

// TestBindRoute_WireGroupHasNoBodyLimit proves the qrdb-wire groups bind
// without a body-limit wrapper, matching their historical root mounts.
func TestBindRoute_WireGroupHasNoBodyLimit(t *testing.T) {
	srv := newModuleSupportServer(t)
	h := srv.BindRoute(bodyEchoSpec(GroupBarrierWire))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/v1/barrier/unseal", strings.NewReader(strings.Repeat("a", 64))))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// TestBindRoute_ProjectsUpstreamQRDBIdentity proves BindRoute applies NO
// authentication or authorization of its own: go-qrdb's unified-server
// middleware owns that end-to-end (its own mTLS chain, or the module's
// AuthenticateHTTP fallback) and places the resulting identity in context
// BEFORE the handler ever runs; BindRoute only projects that identity into
// the quicbaas principal contract (user id, roles, Subject, tenant
// binding).
func TestBindRoute_ProjectsUpstreamQRDBIdentity(t *testing.T) {
	srv := newModuleSupportServer(t)

	var gotID string
	var gotRoles []string
	var gotSubjectOK bool
	var gotTenant string
	var gotAuthenticated bool
	var gotIDKind interfaces.SubjectIDKind
	spec := RouteSpec{Method: "GET", Pattern: "/api/v1/kms/keys", Auth: RouteAuthPermission, Resource: "keys", Action: "get",
		Handler: func(*Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				gotID = GetUserID(r.Context())
				gotRoles = GetUserRoles(r.Context())
				subject, ok := auth.SubjectFromContext(r.Context())
				gotSubjectOK = ok
				gotTenant = subject.TenantID
				gotAuthenticated = subject.Authenticated
				gotIDKind = subject.IDKind
				w.WriteHeader(http.StatusOK)
			}
		},
	}
	h := srv.BindRoute(spec)

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	identity := &qrdbsdk.Identity{
		Subject:  "operator@example",
		TenantID: "tenant-a",
		Roles:    []string{"admin"},
	}
	req = req.WithContext(qrdbsdk.WithIdentity(req.Context(), identity))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if gotID != "operator@example" {
		t.Errorf("user id = %q, want %q", gotID, "operator@example")
	}
	if len(gotRoles) != 1 || gotRoles[0] != "admin" {
		t.Errorf("roles = %v, want [admin]", gotRoles)
	}
	if !gotSubjectOK {
		t.Error("Subject not injected into context")
	}
	if gotTenant != "tenant-a" {
		t.Errorf("subject tenant = %q, want %q", gotTenant, "tenant-a")
	}
	// The projected subject must be marked Authenticated: a verified qrdb
	// identity is authenticated, and handlers that gate on the flag
	// (requireSubject, and impersonation actor resolution in
	// AdminManager.StartImpersonation — go-quicbaas#383) fail closed
	// without it.
	if !gotAuthenticated {
		t.Error("projected subject must be Authenticated for a verified qrdb identity")
	}
	// An unmarked identity is qrdb's own (mTLS): its subject is a username.
	if gotIDKind != interfaces.SubjectIDUsername {
		t.Errorf("subject IDKind = %v, want SubjectIDUsername", gotIDKind)
	}
}

// TestBindRoute_NoUpstreamIdentityPassesThrough proves a request reaching
// BindRoute without a qrdb identity in context (a RouteAuthPublic route:
// go-qrdb never attaches a module fallback to a HandlePublic route) serves
// unchanged, with no principal projected.
func TestBindRoute_NoUpstreamIdentityPassesThrough(t *testing.T) {
	srv := newModuleSupportServer(t)

	var gotID string
	spec := RouteSpec{Method: "GET", Pattern: "/api/v1/health", Auth: RouteAuthPublic,
		Handler: func(*Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				gotID = GetUserID(r.Context())
				w.WriteHeader(http.StatusOK)
			}
		},
	}
	h := srv.BindRoute(spec)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/health", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if gotID != "" {
		t.Errorf("user id = %q, want empty", gotID)
	}
}

// TestServer_AuthenticateHTTP_UserToken proves a platform token whose
// subject is (or may be) a qrdb user is authenticated with Subject/TenantID
// projected: qrdb's own finalizeIdentity resolves the real Roles from its
// user store afterward — AuthenticateHTTP's job is authentication only.
func TestServer_AuthenticateHTTP_UserToken(t *testing.T) {
	srv := newModuleSupportServer(t)
	srv.moduleAuthorizer = &fakeModuleTokenAuthorizer{
		wantToken: "user-token", subject: "alice", tenant: "tenant-a",
	}

	req := httptest.NewRequest("GET", "/api/v1/kms/keys", nil)
	req.Header.Set("Authorization", "Bearer user-token")

	identity, err := srv.AuthenticateHTTP(req)
	if err != nil {
		t.Fatalf("AuthenticateHTTP() error = %v", err)
	}
	if identity.Subject != "alice" {
		t.Errorf("Subject = %q, want %q", identity.Subject, "alice")
	}
	if identity.TenantID != "tenant-a" {
		t.Errorf("TenantID = %q, want %q", identity.TenantID, "tenant-a")
	}
	if len(identity.ScopePermissions) != 0 {
		t.Errorf("ScopePermissions = %v, want empty for a scopeless user token", identity.ScopePermissions)
	}
}

// A bearer's sub is a login name, never an account ID: a numeric sub projects
// as a username, and the account is the token's verified uid claim.
func TestServer_AuthenticateHTTP_AccountIsVerifiedUserID(t *testing.T) {
	srv := newModuleSupportServer(t)
	srv.moduleAuthorizer = &fakeModuleTokenAuthorizer{
		wantToken: "user-token", subject: "12345", userID: 67890, tenant: "tenant-a",
	}

	req := httptest.NewRequest("GET", "/api/v1/kms/keys", nil)
	req.Header.Set("Authorization", "Bearer user-token")

	identity, err := srv.AuthenticateHTTP(req)
	if err != nil {
		t.Fatalf("AuthenticateHTTP() error = %v", err)
	}
	subject := auth.SubjectFromIdentity(identity)
	if subject.ID != "12345" || subject.IDKind != interfaces.SubjectIDUsername || subject.UserID != 67890 {
		t.Fatalf("subject = %+v, want ID 12345 as a username with UserID 67890", subject)
	}
}

// TestServer_AuthenticateHTTP_AppToken_ScopePermissions proves an app
// (client-credential) token's scp-derived resource:action grants land on
// Identity.ScopePermissions verbatim — the additive grant set go-qrdb's RBAC
// authorizer unions in for a caller with no qrdb user record.
func TestServer_AuthenticateHTTP_AppToken_ScopePermissions(t *testing.T) {
	srv := newModuleSupportServer(t)
	srv.moduleAuthorizer = &fakeModuleTokenAuthorizer{
		wantToken: "app-token", subject: "svc-app", tenant: "tenant-a",
		granted: []string{"keys:get", "keys:list"},
	}

	req := httptest.NewRequest("GET", "/api/v1/kms/keys", nil)
	req.Header.Set("Authorization", "Bearer app-token")

	identity, err := srv.AuthenticateHTTP(req)
	if err != nil {
		t.Fatalf("AuthenticateHTTP() error = %v", err)
	}
	if identity.Subject != "svc-app" || identity.TenantID != "tenant-a" {
		t.Errorf("identity = %+v, want subject=svc-app tenant=tenant-a", identity)
	}
	want := []string{"keys:get", "keys:list"}
	if len(identity.ScopePermissions) != len(want) {
		t.Fatalf("ScopePermissions = %v, want %v", identity.ScopePermissions, want)
	}
	for i, p := range want {
		if identity.ScopePermissions[i] != p {
			t.Errorf("ScopePermissions[%d] = %q, want %q", i, identity.ScopePermissions[i], p)
		}
	}
}

// TestServer_AuthenticateHTTP_DPoPAndMethodThreaded proves the DPoP proof
// header and the request method/URI are threaded into Validate so a
// cnf.jkt-bound token's sender-constraint check has what it needs.
func TestServer_AuthenticateHTTP_DPoPAndMethodThreaded(t *testing.T) {
	srv := newModuleSupportServer(t)
	capture := &capturingAuthorizer{
		fakeModuleTokenAuthorizer: fakeModuleTokenAuthorizer{
			wantToken: "dpop-token", subject: "alice", tenant: "tenant-a",
		},
	}
	srv.moduleAuthorizer = capture

	req := httptest.NewRequest("POST", "/api/v1/kms/keys", nil)
	req.Header.Set("Authorization", "Bearer dpop-token")
	req.Header.Set("DPoP", "proof-value")

	if _, err := srv.AuthenticateHTTP(req); err != nil {
		t.Fatalf("AuthenticateHTTP() error = %v", err)
	}
	if capture.gotDPoP != "proof-value" {
		t.Errorf("dpop proof = %q, want %q", capture.gotDPoP, "proof-value")
	}
	if capture.gotMethod != "POST" {
		t.Errorf("method = %q, want POST", capture.gotMethod)
	}
	if !strings.Contains(capture.gotHTU, "/api/v1/kms/keys") {
		t.Errorf("htu = %q, want it to contain the request path", capture.gotHTU)
	}
}

// capturingAuthorizer records the arguments AuthenticateHTTP passes to
// Validate, layered over fakeModuleTokenAuthorizer's authentication logic.
type capturingAuthorizer struct {
	fakeModuleTokenAuthorizer
	gotDPoP, gotMethod, gotHTU string
}

func (c *capturingAuthorizer) Validate(ctx context.Context, token, dpopProof, htm, htu string) (*platformtoken.AuthorizedContext, error) {
	c.gotDPoP = dpopProof
	c.gotMethod = htm
	c.gotHTU = htu
	return c.fakeModuleTokenAuthorizer.Validate(ctx, token, dpopProof, htm, htu)
}

// TestServer_AuthenticateHTTP_GarbageToken proves an unrecognized/garbage
// bearer token fails closed with the validator's error.
func TestServer_AuthenticateHTTP_GarbageToken(t *testing.T) {
	srv := newModuleSupportServer(t)
	srv.moduleAuthorizer = &fakeModuleTokenAuthorizer{wantToken: "good-token", subject: "alice", tenant: "tenant-a"}

	req := httptest.NewRequest("GET", "/api/v1/kms/keys", nil)
	req.Header.Set("Authorization", "Bearer not-the-right-token")

	identity, err := srv.AuthenticateHTTP(req)
	if err == nil {
		t.Fatal("expected an error for a garbage token")
	}
	var invalid *platformtoken.InvalidTokenError
	if !errors.As(err, &invalid) {
		t.Errorf("error = %v, want *platformtoken.InvalidTokenError", err)
	}
	if identity != nil {
		t.Errorf("identity = %+v, want nil", identity)
	}
}

func TestServer_AuthenticateHTTP_EmptyTenantToken(t *testing.T) {
	srv := newModuleSupportServer(t)
	srv.moduleAuthorizer = &fakeModuleTokenAuthorizer{wantToken: "tenantless-token", subject: "alice"}

	req := httptest.NewRequest("GET", "/api/v1/kms/keys", nil)
	req.Header.Set("Authorization", "Bearer tenantless-token")

	identity, err := srv.AuthenticateHTTP(req)
	var tenantErr *EmptyTenantTokenError
	if !errors.As(err, &tenantErr) {
		t.Fatalf("expected EmptyTenantTokenError, got identity=%v err=%v", identity, err)
	}
	if identity != nil {
		t.Fatalf("expected no identity for tenantless token, got %+v", identity)
	}
}

// TestServer_AuthenticateHTTP_MissingBearer proves a request with no
// Authorization header fails closed with a typed error, never a nil
// identity with a nil error.
func TestServer_AuthenticateHTTP_MissingBearer(t *testing.T) {
	srv := newModuleSupportServer(t)
	srv.moduleAuthorizer = &fakeModuleTokenAuthorizer{wantToken: "good-token", subject: "alice", tenant: "tenant-a"}

	identity, err := srv.AuthenticateHTTP(httptest.NewRequest("GET", "/api/v1/kms/keys", nil))
	var missing *MissingAuthHeaderError
	if !errors.As(err, &missing) {
		t.Errorf("error = %v, want *MissingAuthHeaderError", err)
	}
	if identity != nil {
		t.Errorf("identity = %+v, want nil", identity)
	}
}

// TestServer_AuthenticateHTTP_NoAuthorizerWired proves AuthenticateHTTP
// fails closed with a typed error when no ModuleTokenAuthorizer is
// installed (RequireAuthz off, or the OIDC plane unavailable) — never a
// silent open door for platform-token callers.
func TestServer_AuthenticateHTTP_NoAuthorizerWired(t *testing.T) {
	srv := newModuleSupportServer(t)

	req := httptest.NewRequest("GET", "/api/v1/kms/keys", nil)
	req.Header.Set("Authorization", "Bearer anything")

	identity, err := srv.AuthenticateHTTP(req)
	var required *ModuleAuthorizerRequiredError
	if !errors.As(err, &required) {
		t.Errorf("error = %v, want *ModuleAuthorizerRequiredError", err)
	}
	if identity != nil {
		t.Errorf("identity = %+v, want nil", identity)
	}
}

// TestServer_AuthenticateHTTP_NoAuthorizerWired_Logs proves the
// no-moduleAuthorizer rejection is logged exactly like a refused bearer.
// Before this, a deployment running with no fallback wired (RequireAuthz
// off, or the gate never finished construction) 401ed every platform-token
// caller — the entire UI, since a browser bearer never satisfies qrdb's own
// mTLS chain — and left quicbaasd.out silent for every single one of them,
// so "the WARN log never fires" could not distinguish this misconfiguration
// from any other rejection reason.
func TestServer_AuthenticateHTTP_NoAuthorizerWired_Logs(t *testing.T) {
	srv, logPath := newModuleSupportServerWithFileLogger(t)

	req := httptest.NewRequest("GET", "/api/v1/orchestrator/status", nil)
	req.Header.Set("Authorization", "Bearer anything")

	if _, err := srv.AuthenticateHTTP(req); err == nil {
		t.Fatal("expected an error with no authorizer wired")
	}

	got := readLog(t, logPath)
	if !strings.Contains(got, "rest: bearer token refused") {
		t.Errorf("log = %q, want it to contain %q", got, "rest: bearer token refused")
	}
	if !strings.Contains(got, "/api/v1/orchestrator/status") {
		t.Errorf("log = %q, want it to contain the request path", got)
	}
	if !strings.Contains(got, "module platform-token authorizer is not configured") {
		t.Errorf("log = %q, want it to name the ModuleAuthorizerRequiredError reason", got)
	}
}

// TestServer_AuthenticateHTTP_MissingBearer_Logs proves a missing/malformed
// Authorization header is logged exactly like a refused bearer, not only a
// validator failure — the same silence gap TestServer_AuthenticateHTTP_
// NoAuthorizerWired_Logs closes for the no-authorizer branch.
func TestServer_AuthenticateHTTP_MissingBearer_Logs(t *testing.T) {
	srv, logPath := newModuleSupportServerWithFileLogger(t)
	srv.moduleAuthorizer = &fakeModuleTokenAuthorizer{wantToken: "good-token", subject: "alice", tenant: "tenant-a"}

	req := httptest.NewRequest("GET", "/api/v1/orchestrator/status", nil)
	if _, err := srv.AuthenticateHTTP(req); err == nil {
		t.Fatal("expected an error for a missing Authorization header")
	}

	got := readLog(t, logPath)
	if !strings.Contains(got, "rest: bearer token refused") {
		t.Errorf("log = %q, want it to contain %q", got, "rest: bearer token refused")
	}
	if !strings.Contains(got, "authorization header is missing") {
		t.Errorf("log = %q, want it to name the MissingAuthHeaderError reason", got)
	}
}

// TestRouteEnabled covers the nil-guard and both guard outcomes.
func TestRouteEnabled(t *testing.T) {
	srv := newModuleSupportServer(t)
	if !srv.RouteEnabled(RouteSpec{}) {
		t.Error("nil Enabled guard must report enabled")
	}
	if !srv.RouteEnabled(RouteSpec{Enabled: func(*Server) bool { return true }}) {
		t.Error("true guard must report enabled")
	}
	if srv.RouteEnabled(RouteSpec{Enabled: func(*Server) bool { return false }}) {
		t.Error("false guard must report disabled")
	}
}
