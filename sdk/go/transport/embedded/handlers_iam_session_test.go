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

package embedded

import (
	"context"
	"testing"
	"time"

	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// mockQuicBaaSWithSessions exposes a real DefaultSessionManager through the
// structural seam the embedded dispatch uses.
type mockQuicBaaSWithSessions struct {
	service.QuicBaaS
	sessions service.SessionManager
}

func (m *mockQuicBaaSWithSessions) GetSessionManager(_ context.Context) (service.SessionManager, error) {
	return m.sessions, nil
}

// The SDK sub-client wraps its payloads in unexported structs; these mirrors
// carry the same field names so the reflection-based extraction is exercised
// exactly as it is in production.
type embeddedSessionIDEnvelope struct {
	SessionID string
}

type embeddedListSessionsEnvelope struct {
	Request *iamsdk.ListSessionsRequest
}

type embeddedRevokeAllEnvelope struct {
	TenantID string
	UserID   string
}

type embeddedRevokeOthersEnvelope struct {
	KeepSessionID string
}

func newSessionTestService(t *testing.T) (*mockQuicBaaSWithSessions, service.SessionManager) {
	t.Helper()
	mgr, err := iam.NewSessionManager()
	if err != nil {
		t.Fatalf("NewSessionManager failed: %v", err)
	}
	return &mockQuicBaaSWithSessions{sessions: mgr}, mgr
}

func seedSession(t *testing.T, mgr service.SessionManager, id, userID, tenantID string) *iam.SessionExt {
	t.Helper()
	s := &iam.SessionExt{
		ID:         id,
		UserID:     userID,
		TenantID:   tenantID,
		TokenHash:  "hash-" + id,
		IPAddress:  "10.0.0.1",
		UserAgent:  "test-agent",
		DeviceInfo: &iam.DeviceInfo{Name: "workstation"},
		Metadata:   map[string]string{"auth_method": "mtls"},
		ExpiresAt:  time.Now().Add(time.Hour),
	}
	if err := mgr.Create(context.Background(), s); err != nil {
		t.Fatalf("seeding session %s failed: %v", id, err)
	}
	return s
}

// TestIAMSessionDispatchRegistered proves the gap in the issue is closed: the
// embedded table now answers every Session method the gRPC and REST handlers
// answer, which is what an in-process host dispatches through.
func TestIAMSessionDispatchRegistered(t *testing.T) {
	for _, method := range []string{
		"quicbaas.IAM.Session/Get",
		"quicbaas.IAM.Session/List",
		"quicbaas.IAM.Session/Revoke",
		"quicbaas.IAM.Session/RevokeAll",
		"quicbaas.IAM.Session/RevokeOthers",
		"quicbaas.IAM.Session/RevokeOther",
	} {
		if _, ok := unaryHandlers[method]; !ok {
			t.Errorf("%s is not registered in the embedded dispatch table", method)
		}
	}
}

func TestHandleIAMSessionGet(t *testing.T) {
	svc, mgr := newSessionTestService(t)
	ctx := context.Background()
	seedSession(t, mgr, "sess-1", "user-1", "t-1")

	out, err := handleIAMSessionGet(ctx, svc, &embeddedSessionIDEnvelope{SessionID: "sess-1"})
	if err != nil {
		t.Fatalf("handleIAMSessionGet failed: %v", err)
	}
	got, ok := out.(*iamsdk.Session)
	if !ok {
		t.Fatalf("expected *iam.Session, got %T", out)
	}
	if got.ID != "sess-1" || got.UserID != "user-1" || got.TenantID != "t-1" {
		t.Errorf("unexpected session identity: %+v", got)
	}
	// The domain models device info as a struct and auth method as metadata;
	// both must survive the collapse into the SDK's flat shape.
	if got.DeviceInfo != "workstation" {
		t.Errorf("DeviceInfo = %q, want \"workstation\"", got.DeviceInfo)
	}
	if got.AuthMethod != "mtls" {
		t.Errorf("AuthMethod = %q, want \"mtls\"", got.AuthMethod)
	}
}

func TestHandleIAMSessionGet_Errors(t *testing.T) {
	svc, mgr := newSessionTestService(t)
	ctx := context.Background()
	seedSession(t, mgr, "sess-1", "user-1", "t-1")

	tests := []struct {
		name string
		req  interface{}
	}{
		{name: "request without a SessionID field", req: struct{ Other string }{Other: "x"}},
		{name: "unknown session", req: &embeddedSessionIDEnvelope{SessionID: "missing"}},
		{name: "empty session id", req: &embeddedSessionIDEnvelope{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := handleIAMSessionGet(ctx, svc, tt.req); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestHandleIAMSessionList_Paginates(t *testing.T) {
	svc, mgr := newSessionTestService(t)
	ctx := context.Background()
	for _, id := range []string{"s1", "s2", "s3"} {
		seedSession(t, mgr, id, "user-1", "t-1")
	}

	out, err := handleIAMSessionList(ctx, svc, &embeddedListSessionsEnvelope{
		Request: &iamsdk.ListSessionsRequest{TenantID: "t-1", UserID: "user-1", Limit: 2},
	})
	if err != nil {
		t.Fatalf("handleIAMSessionList failed: %v", err)
	}
	got, ok := out.(*iamsdk.ListSessionsResponse)
	if !ok {
		t.Fatalf("expected *iam.ListSessionsResponse, got %T", out)
	}
	if len(got.Sessions) != 2 {
		t.Errorf("returned %d sessions, want the requested page of 2", len(got.Sessions))
	}
	if got.TotalCount != 3 {
		t.Errorf("TotalCount = %d, want 3 (the full match count, not the page)", got.TotalCount)
	}
	if !got.HasMore {
		t.Error("HasMore = false, want true with a page smaller than the total")
	}

	// An offset past the end is a normal outcome of a shrinking list.
	out, err = handleIAMSessionList(ctx, svc, &embeddedListSessionsEnvelope{
		Request: &iamsdk.ListSessionsRequest{TenantID: "t-1", UserID: "user-1", Offset: 99},
	})
	if err != nil {
		t.Fatalf("handleIAMSessionList with a large offset failed: %v", err)
	}
	if page := out.(*iamsdk.ListSessionsResponse); len(page.Sessions) != 0 {
		t.Errorf("expected an empty page past the end, got %d sessions", len(page.Sessions))
	}
}

func TestHandleIAMSessionList_Errors(t *testing.T) {
	svc, _ := newSessionTestService(t)
	ctx := context.Background()

	tests := []struct {
		name string
		req  interface{}
	}{
		{name: "request without the payload field", req: struct{ Other string }{}},
		{name: "nil payload", req: &embeddedListSessionsEnvelope{}},
		{name: "payload missing the user", req: &embeddedListSessionsEnvelope{Request: &iamsdk.ListSessionsRequest{TenantID: "t-1"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := handleIAMSessionList(ctx, svc, tt.req); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestHandleIAMSessionRevoke(t *testing.T) {
	svc, mgr := newSessionTestService(t)
	ctx := context.Background()
	seedSession(t, mgr, "sess-1", "user-1", "t-1")

	if _, err := handleIAMSessionRevoke(ctx, svc, &embeddedSessionIDEnvelope{SessionID: "sess-1"}); err != nil {
		t.Fatalf("handleIAMSessionRevoke failed: %v", err)
	}
	sessions, err := mgr.List(ctx, "user-1", "t-1")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected the session to be revoked, %d remain", len(sessions))
	}
}

func TestHandleIAMSessionRevoke_BadRequest(t *testing.T) {
	svc, _ := newSessionTestService(t)
	if _, err := handleIAMSessionRevoke(context.Background(), svc, struct{ Other string }{}); err == nil {
		t.Error("expected an error for a request with no SessionID field")
	}
}

func TestHandleIAMSessionRevokeAll(t *testing.T) {
	svc, mgr := newSessionTestService(t)
	ctx := context.Background()
	seedSession(t, mgr, "s1", "user-1", "t-1")
	seedSession(t, mgr, "s2", "user-1", "t-1")
	seedSession(t, mgr, "other", "user-2", "t-1")

	if _, err := handleIAMSessionRevokeAll(ctx, svc, &embeddedRevokeAllEnvelope{TenantID: "t-1", UserID: "user-1"}); err != nil {
		t.Fatalf("handleIAMSessionRevokeAll failed: %v", err)
	}

	revoked, err := mgr.List(ctx, "user-1", "t-1")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(revoked) != 0 {
		t.Errorf("expected every session for user-1 to be revoked, %d remain", len(revoked))
	}

	// Another user's sessions are untouched — the blast radius is the named
	// user, not the tenant.
	survivors, err := mgr.List(ctx, "user-2", "t-1")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(survivors) != 1 {
		t.Errorf("expected user-2's session to survive, got %d", len(survivors))
	}
}

func TestHandleIAMSessionRevokeAll_BadRequest(t *testing.T) {
	svc, _ := newSessionTestService(t)
	if _, err := handleIAMSessionRevokeAll(context.Background(), svc, &embeddedSessionIDEnvelope{SessionID: "s1"}); err == nil {
		t.Error("expected an error for a request missing TenantID/UserID")
	}
}

// RevokeOthers names only the session to keep; the handler must derive the
// owning user and tenant from that session rather than from caller input.
func TestHandleIAMSessionRevokeOthers(t *testing.T) {
	svc, mgr := newSessionTestService(t)
	ctx := context.Background()
	seedSession(t, mgr, "keep", "user-1", "t-1")
	seedSession(t, mgr, "drop-1", "user-1", "t-1")
	seedSession(t, mgr, "drop-2", "user-1", "t-1")

	if _, err := handleIAMSessionRevokeOthers(ctx, svc, &embeddedRevokeOthersEnvelope{KeepSessionID: "keep"}); err != nil {
		t.Fatalf("handleIAMSessionRevokeOthers failed: %v", err)
	}

	remaining, err := mgr.List(ctx, "user-1", "t-1")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != "keep" {
		t.Errorf("expected only the kept session to remain, got %+v", remaining)
	}
}

func TestHandleIAMSessionRevokeOthers_Errors(t *testing.T) {
	svc, _ := newSessionTestService(t)
	ctx := context.Background()

	tests := []struct {
		name string
		req  interface{}
	}{
		{name: "request without a KeepSessionID field", req: struct{ Other string }{}},
		{name: "unknown session to keep", req: &embeddedRevokeOthersEnvelope{KeepSessionID: "missing"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := handleIAMSessionRevokeOthers(ctx, svc, tt.req); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

// A host that wires no session manager must get a typed
// ServiceNotConfiguredError rather than a nil-pointer panic.
func TestSessionHandlers_ServiceNotConfigured(t *testing.T) {
	var svc service.QuicBaaS
	ctx := context.Background()

	if _, err := handleIAMSessionGet(ctx, svc, &embeddedSessionIDEnvelope{SessionID: "s1"}); err == nil {
		t.Fatal("expected an error when no session manager is configured")
	} else if _, ok := err.(*ServiceNotConfiguredError); !ok {
		t.Errorf("expected *ServiceNotConfiguredError, got %T: %v", err, err)
	}

	nilMgr := &mockQuicBaaSWithSessions{}
	if _, err := handleIAMSessionGet(ctx, nilMgr, &embeddedSessionIDEnvelope{SessionID: "s1"}); err == nil {
		t.Fatal("expected an error when the session manager is nil")
	} else if _, ok := err.(*ServiceNotConfiguredError); !ok {
		t.Errorf("expected *ServiceNotConfiguredError, got %T: %v", err, err)
	}
}

// The session surface is self-scoped over REST: naming another user's session
// answers 403. These cases prove the embedded transport is not a way around
// that gate when the SDK sits behind an authenticated surface.

func subjectCtx(userID, tenantID string) context.Context {
	return auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID:       userID,
		TenantID: tenantID,
	})
}

func TestHandleIAMSessionGet_DeniesAnotherSubjectsSession(t *testing.T) {
	svc, mgr := newSessionTestService(t)
	seedSession(t, mgr, "victim-session", "user-2", "t-1")

	ctx := subjectCtx("user-1", "t-1")
	_, err := handleIAMSessionGet(ctx, svc, &embeddedSessionIDEnvelope{SessionID: "victim-session"})
	if err == nil {
		t.Fatal("expected reading another subject's session to be denied")
	}
	if _, ok := err.(*PermissionDeniedError); !ok {
		t.Errorf("expected *PermissionDeniedError, got %T: %v", err, err)
	}

	// The subject's own session is still readable.
	seedSession(t, mgr, "own-session", "user-1", "t-1")
	if _, err := handleIAMSessionGet(ctx, svc, &embeddedSessionIDEnvelope{SessionID: "own-session"}); err != nil {
		t.Fatalf("reading the subject's own session failed: %v", err)
	}
}

// A denied caller must not be able to destroy a session it was never allowed
// to see, so ownership is checked before the revoke.
func TestHandleIAMSessionRevoke_DeniesAnotherSubjectsSession(t *testing.T) {
	svc, mgr := newSessionTestService(t)
	seedSession(t, mgr, "victim-session", "user-2", "t-1")

	ctx := subjectCtx("user-1", "t-1")
	_, err := handleIAMSessionRevoke(ctx, svc, &embeddedSessionIDEnvelope{SessionID: "victim-session"})
	if err == nil {
		t.Fatal("expected revoking another subject's session to be denied")
	}
	if _, ok := err.(*PermissionDeniedError); !ok {
		t.Errorf("expected *PermissionDeniedError, got %T: %v", err, err)
	}

	survivors, listErr := mgr.List(context.Background(), "user-2", "t-1")
	if listErr != nil {
		t.Fatalf("List failed: %v", listErr)
	}
	if len(survivors) != 1 {
		t.Errorf("the victim's session was revoked despite the denial: %d remain", len(survivors))
	}
}

// Substituting the subject's own identity for a mismatched request would
// answer a different question than the one asked, so the mismatch is refused.
func TestHandleIAMSessionList_DeniesMismatchedIdentity(t *testing.T) {
	svc, mgr := newSessionTestService(t)
	seedSession(t, mgr, "own", "user-1", "t-1")
	ctx := subjectCtx("user-1", "t-1")

	tests := []struct {
		name    string
		req     *iamsdk.ListSessionsRequest
		wantErr bool
	}{
		{name: "another user", req: &iamsdk.ListSessionsRequest{UserID: "user-2", TenantID: "t-1"}, wantErr: true},
		{name: "another tenant", req: &iamsdk.ListSessionsRequest{UserID: "user-1", TenantID: "t-2"}, wantErr: true},
		{name: "own identity", req: &iamsdk.ListSessionsRequest{UserID: "user-1", TenantID: "t-1"}},
		{name: "identity omitted, filled from the subject", req: &iamsdk.ListSessionsRequest{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := handleIAMSessionList(ctx, svc, &embeddedListSessionsEnvelope{Request: tt.req})
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected a mismatched identity to be denied")
				}
				if _, ok := err.(*PermissionDeniedError); !ok {
					t.Errorf("expected *PermissionDeniedError, got %T: %v", err, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("handleIAMSessionList failed: %v", err)
			}
			if page := out.(*iamsdk.ListSessionsResponse); len(page.Sessions) != 1 {
				t.Errorf("returned %d sessions, want the subject's own one", len(page.Sessions))
			}
		})
	}
}

func TestHandleIAMSessionRevokeAll_DeniesMismatchedIdentity(t *testing.T) {
	svc, mgr := newSessionTestService(t)
	seedSession(t, mgr, "victim", "user-2", "t-1")
	ctx := subjectCtx("user-1", "t-1")

	_, err := handleIAMSessionRevokeAll(ctx, svc, &embeddedRevokeAllEnvelope{TenantID: "t-1", UserID: "user-2"})
	if err == nil {
		t.Fatal("expected revoking another user's sessions to be denied")
	}
	if _, ok := err.(*PermissionDeniedError); !ok {
		t.Errorf("expected *PermissionDeniedError, got %T: %v", err, err)
	}

	survivors, listErr := mgr.List(context.Background(), "user-2", "t-1")
	if listErr != nil {
		t.Fatalf("List failed: %v", listErr)
	}
	if len(survivors) != 1 {
		t.Errorf("the victim's sessions were revoked despite the denial: %d remain", len(survivors))
	}
}

func TestHandleIAMSessionRevokeOthers_DeniesAnotherSubjectsSession(t *testing.T) {
	svc, mgr := newSessionTestService(t)
	seedSession(t, mgr, "victim-keep", "user-2", "t-1")
	seedSession(t, mgr, "victim-drop", "user-2", "t-1")
	ctx := subjectCtx("user-1", "t-1")

	_, err := handleIAMSessionRevokeOthers(ctx, svc, &embeddedRevokeOthersEnvelope{KeepSessionID: "victim-keep"})
	if err == nil {
		t.Fatal("expected revoking another subject's other sessions to be denied")
	}
	if _, ok := err.(*PermissionDeniedError); !ok {
		t.Errorf("expected *PermissionDeniedError, got %T: %v", err, err)
	}

	survivors, listErr := mgr.List(context.Background(), "user-2", "t-1")
	if listErr != nil {
		t.Fatalf("List failed: %v", listErr)
	}
	if len(survivors) != 2 {
		t.Errorf("the victim's sessions were revoked despite the denial: %d remain", len(survivors))
	}
}

// A context with no Subject is the host process calling its own service before
// any transport has run — the pre-existing contract for every embedded IAM
// handler — so it stays unrestricted.
func TestSessionHandlers_UnboundContextIsUnrestricted(t *testing.T) {
	svc, mgr := newSessionTestService(t)
	seedSession(t, mgr, "any-session", "user-2", "t-1")

	if _, err := handleIAMSessionGet(context.Background(), svc, &embeddedSessionIDEnvelope{SessionID: "any-session"}); err != nil {
		t.Fatalf("an unbound host call must not be restricted: %v", err)
	}
}
