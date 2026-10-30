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
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// ctxWithTestSubject returns a context carrying an authenticated Subject
// exactly as the production authz middleware injects it (injectPrincipal ->
// auth.SubjectIntoContext). All REST handler tests must use this instead of
// ad-hoc context keys. userID is both the login name and the verified
// account, as for a bearer token whose sub and uid agree.
func ctxWithTestSubject(parent context.Context, userID, tenantID string) context.Context {
	subject := interfaces.Subject{
		ID:            userID,
		TenantID:      tenantID,
		Authenticated: true,
	}
	// A numeric login name is also the verified user id, as a token minted
	// for that account would carry it in its uid claim.
	if uid, err := strconv.ParseUint(userID, 10, 64); err == nil {
		subject.UserID = uid
	}
	return auth.SubjectIntoContext(parent, subject)
}

// ctxWithTestUser is ctxWithTestSubject for handlers that act on the
// caller's verified qrdb user id (Subject.UserID) rather than the login name.
func ctxWithTestUser(parent context.Context, userID uint64, tenantID string) context.Context {
	return auth.SubjectIntoContext(parent, interfaces.Subject{
		ID:            strconv.FormatUint(userID, 10),
		UserID:        userID,
		TenantID:      tenantID,
		Authenticated: true,
	})
}

func TestRequireSubject_Authenticated(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(ctxWithTestSubject(req.Context(), "30001", "t1"))
	w := httptest.NewRecorder()

	subject, ok := requireSubject(w, req)
	require.True(t, ok)
	assert.Equal(t, "30001", subject.ID)
	assert.Equal(t, "t1", subject.TenantID)
}

func TestRequireSubject_MissingPrincipal(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()

	_, ok := requireSubject(w, req)
	assert.False(t, ok)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "user not authenticated")
}

func TestRequireSubject_UnauthenticatedSubject(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(auth.SubjectIntoContext(req.Context(),
		interfaces.Subject{ID: "u1", Authenticated: false}))
	w := httptest.NewRecorder()

	_, ok := requireSubject(w, req)
	assert.False(t, ok)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequireSubject_EmptySubjectID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(ctxWithTestSubject(req.Context(), "", "t1"))
	w := httptest.NewRecorder()

	_, ok := requireSubject(w, req)
	assert.False(t, ok)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequireTenantSubject_WithTenant(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(ctxWithTestSubject(req.Context(), "30001", "t1"))
	w := httptest.NewRecorder()

	subject, ok := requireTenantSubject(w, req)
	require.True(t, ok)
	assert.Equal(t, "t1", subject.TenantID)
}

func TestRequireTenantSubject_MissingTenant(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(ctxWithTestSubject(req.Context(), "30001", ""))
	w := httptest.NewRecorder()

	_, ok := requireTenantSubject(w, req)
	assert.False(t, ok)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "tenant ID is required")
}

func TestRequireTenantSubject_MissingPrincipal(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()

	_, ok := requireTenantSubject(w, req)
	assert.False(t, ok)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestRequireTenantSubject_SystemTenantRejected proves a principal carrying
// the literal system tenant is rejected exactly like an empty TenantID: the
// system tenant is the structurally privileged platform scope, not an
// isolated tenant namespace, so tenant-scoped handlers (e.g. sessions) must
// never treat it as "has a tenant".
func TestRequireTenantSubject_SystemTenantRejected(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(ctxWithTestSubject(req.Context(), "u1", qrdbsdk.SystemTenantID))
	w := httptest.NewRecorder()

	_, ok := requireTenantSubject(w, req)
	assert.False(t, ok)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "tenant ID is required")
}

// TestRequireTenantSubject_OrdinaryTenantStillIsolated proves the system-tenant
// carve-out does not widen ordinary tenants' reach: a lookalike ID that merely
// shares the "system" prefix is still an ordinary, isolated tenant and passes
// the gate normally.
func TestRequireTenantSubject_OrdinaryTenantStillIsolated(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(ctxWithTestSubject(req.Context(), "30001", "system-2"))
	w := httptest.NewRecorder()

	subject, ok := requireTenantSubject(w, req)
	require.True(t, ok)
	assert.Equal(t, "system-2", subject.TenantID)
}

func TestQueryIdentityMatches_NoParams(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()

	assert.True(t, queryIdentityMatches(w, req,
		interfaces.Subject{ID: "u1", TenantID: "t1", Authenticated: true}, unusedCallerAccount(t)))
}

func TestQueryIdentityMatches_MatchingParams(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x?user_id=u1&tenant_id=t1", nil)
	w := httptest.NewRecorder()

	assert.True(t, queryIdentityMatches(w, req,
		interfaces.Subject{ID: "u1", TenantID: "t1", Authenticated: true}, unusedCallerAccount(t)))
}

func TestQueryIdentityMatches_UserMismatch(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x?user_id=victim", nil)
	w := httptest.NewRecorder()

	assert.False(t, queryIdentityMatches(w, req,
		interfaces.Subject{ID: "u1", TenantID: "t1", Authenticated: true}, unusedCallerAccount(t)))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "user_id parameter does not match")
}

func TestQueryIdentityMatches_TenantMismatch(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x?tenant_id=other", nil)
	w := httptest.NewRecorder()

	assert.False(t, queryIdentityMatches(w, req,
		interfaces.Subject{ID: "u1", TenantID: "t1", Authenticated: true}, unusedCallerAccount(t)))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "tenant_id parameter does not match")
}

// The platform mints two first-class identifiers for a principal: the stable
// subject ID (token sub) and the username claim. A self-reference with either
// representation must pass.
func TestQueryIdentityMatches_UsernameRepresentation(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x?user_id=admin", nil)
	w := httptest.NewRecorder()

	assert.True(t, queryIdentityMatches(w, req,
		interfaces.Subject{ID: "9b2f2c1e-uuid", Username: "admin", TenantID: "t1", Authenticated: true}, unusedCallerAccount(t)))
}

// A foreign identity is still hard-403 even when the Subject carries a
// username: neither representation of the principal matches.
func TestQueryIdentityMatches_ForeignUserWithUsername(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x?user_id=victim", nil)
	w := httptest.NewRecorder()

	assert.False(t, queryIdentityMatches(w, req,
		interfaces.Subject{ID: "9b2f2c1e-uuid", Username: "admin", TenantID: "t1", Authenticated: true}, unusedCallerAccount(t)))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "user_id parameter does not match")
}

// boundBearerSubject projects alice's bearer identity the way the REST module
// sees it: the token marked it with her uid 42, then qrdb's BindUser bound the
// account and set Subject to "42".
func boundBearerSubject() interfaces.Subject {
	identity := auth.MarkTokenSubject(&qrdbsdk.Identity{Subject: "alice", TenantID: "t1"}, 42)
	identity.BindUser(42, "alice")
	return auth.SubjectFromIdentity(identity)
}

// A bound bearer caller asking for her own sessions by username is allowed.
func TestQueryIdentityMatches_BoundBearerOwnUsername(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x?user_id=alice&tenant_id=t1", nil)
	w := httptest.NewRecorder()

	assert.True(t, queryIdentityMatches(w, req, boundBearerSubject(), unusedCallerAccount(t)))
	assert.Equal(t, http.StatusOK, w.Code)
}

// A bound bearer caller asking for another user's sessions is refused.
func TestQueryIdentityMatches_BoundBearerOtherUsername(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x?user_id=bob", nil)
	w := httptest.NewRecorder()

	assert.False(t, queryIdentityMatches(w, req, boundBearerSubject(), unusedCallerAccount(t)))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// An empty Subject.Username never matches: only the subject ID counts.
func TestQueryIdentityMatches_EmptyUsernameNoMatch(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x?user_id=admin", nil)
	w := httptest.NewRecorder()

	assert.False(t, queryIdentityMatches(w, req,
		interfaces.Subject{ID: "9b2f2c1e-uuid", Username: "", TenantID: "t1", Authenticated: true}, unusedCallerAccount(t)))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestSubjectUserMatches(t *testing.T) {
	subject := interfaces.Subject{ID: "uuid-1", Username: "alice"}

	assert.True(t, subjectUserMatches(subject, "uuid-1"))
	assert.True(t, subjectUserMatches(subject, "alice"))
	assert.False(t, subjectUserMatches(subject, "bob"))
	assert.False(t, subjectUserMatches(subject, ""))
	assert.False(t, subjectUserMatches(interfaces.Subject{ID: "uuid-1"}, "alice"))
}

// unusedCallerAccount fails the test if the user_id check resolves the
// caller when the query alone settles the answer.
func unusedCallerAccount(t *testing.T) callerAccountFunc {
	t.Helper()
	return func() (uint64, bool) {
		t.Error("caller account resolved for a query that needs no resolution")
		return 0, false
	}
}

// A username login carries the name in the subject; the caller's own
// numeric account ID must still be accepted as their user_id.
func TestQueryIdentityMatches_OwnAccountIDUnderUsernameLogin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x?user_id=30001", nil)
	w := httptest.NewRecorder()

	assert.True(t, queryIdentityMatches(w, req,
		interfaces.Subject{ID: "alice", Username: "alice", TenantID: "t1", Authenticated: true},
		resolvedCallerAccount(30001)))
}

func TestQueryIdentityMatches_ForeignAccountIDUnderUsernameLogin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x?user_id=30002", nil)
	w := httptest.NewRecorder()

	assert.False(t, queryIdentityMatches(w, req,
		interfaces.Subject{ID: "alice", Username: "alice", TenantID: "t1", Authenticated: true},
		resolvedCallerAccount(30001)))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "user_id parameter does not match")
}

// A resolution failure is never a match: the resolver's own response stands.
func TestQueryIdentityMatches_ResolverFailureIsRefused(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x?user_id=30001", nil)
	w := httptest.NewRecorder()
	failing := func() (uint64, bool) {
		RespondIAMError(w, req, http.StatusForbidden, NewIAMAPIError(ErrForbidden, "caller is not a user"))
		return 0, false
	}

	assert.False(t, queryIdentityMatches(w, req,
		interfaces.Subject{ID: "alice", Username: "alice", TenantID: "t1", Authenticated: true},
		failing))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "caller is not a user")
}

// A tenant_id mismatch is still refused after the user_id matched by account.
func TestQueryIdentityMatches_OwnAccountIDWithForeignTenant(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x?user_id=30001&tenant_id=other", nil)
	w := httptest.NewRecorder()

	assert.False(t, queryIdentityMatches(w, req,
		interfaces.Subject{ID: "alice", Username: "alice", TenantID: "t1", Authenticated: true},
		resolvedCallerAccount(30001)))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "tenant_id parameter does not match")
}

func TestCallerAccountMatches(t *testing.T) {
	matched, ok := callerAccountMatches("30001", resolvedCallerAccount(30001))
	assert.True(t, matched)
	assert.True(t, ok)

	matched, ok = callerAccountMatches("0", unusedCallerAccount(t))
	assert.False(t, matched)
	assert.True(t, ok)

	matched, ok = callerAccountMatches("bob", unusedCallerAccount(t))
	assert.False(t, matched)
	assert.True(t, ok)
}
