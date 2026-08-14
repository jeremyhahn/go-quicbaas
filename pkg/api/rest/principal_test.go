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
// ad-hoc context keys.
func ctxWithTestSubject(parent context.Context, userID, tenantID string) context.Context {
	return auth.SubjectIntoContext(parent, interfaces.Subject{
		ID:            userID,
		TenantID:      tenantID,
		Authenticated: true,
	})
}

func TestRequireSubject_Authenticated(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(ctxWithTestSubject(req.Context(), "u1", "t1"))
	w := httptest.NewRecorder()

	subject, ok := requireSubject(w, req)
	require.True(t, ok)
	assert.Equal(t, "u1", subject.ID)
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
	req = req.WithContext(ctxWithTestSubject(req.Context(), "u1", "t1"))
	w := httptest.NewRecorder()

	subject, ok := requireTenantSubject(w, req)
	require.True(t, ok)
	assert.Equal(t, "t1", subject.TenantID)
}

func TestRequireTenantSubject_MissingTenant(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(ctxWithTestSubject(req.Context(), "u1", ""))
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
	req = req.WithContext(ctxWithTestSubject(req.Context(), "u1", "system-2"))
	w := httptest.NewRecorder()

	subject, ok := requireTenantSubject(w, req)
	require.True(t, ok)
	assert.Equal(t, "system-2", subject.TenantID)
}

func TestQueryIdentityMatches_NoParams(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()

	assert.True(t, queryIdentityMatches(w, req,
		interfaces.Subject{ID: "u1", TenantID: "t1", Authenticated: true}))
}

func TestQueryIdentityMatches_MatchingParams(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x?user_id=u1&tenant_id=t1", nil)
	w := httptest.NewRecorder()

	assert.True(t, queryIdentityMatches(w, req,
		interfaces.Subject{ID: "u1", TenantID: "t1", Authenticated: true}))
}

func TestQueryIdentityMatches_UserMismatch(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x?user_id=victim", nil)
	w := httptest.NewRecorder()

	assert.False(t, queryIdentityMatches(w, req,
		interfaces.Subject{ID: "u1", TenantID: "t1", Authenticated: true}))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "user_id parameter does not match")
}

func TestQueryIdentityMatches_TenantMismatch(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x?tenant_id=other", nil)
	w := httptest.NewRecorder()

	assert.False(t, queryIdentityMatches(w, req,
		interfaces.Subject{ID: "u1", TenantID: "t1", Authenticated: true}))
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
		interfaces.Subject{ID: "9b2f2c1e-uuid", Username: "admin", TenantID: "t1", Authenticated: true}))
}

// A foreign identity is still hard-403 even when the Subject carries a
// username: neither representation of the principal matches.
func TestQueryIdentityMatches_ForeignUserWithUsername(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x?user_id=victim", nil)
	w := httptest.NewRecorder()

	assert.False(t, queryIdentityMatches(w, req,
		interfaces.Subject{ID: "9b2f2c1e-uuid", Username: "admin", TenantID: "t1", Authenticated: true}))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "user_id parameter does not match")
}

// An empty Subject.Username never matches: only the subject ID counts.
func TestQueryIdentityMatches_EmptyUsernameNoMatch(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x?user_id=admin", nil)
	w := httptest.NewRecorder()

	assert.False(t, queryIdentityMatches(w, req,
		interfaces.Subject{ID: "9b2f2c1e-uuid", Username: "", TenantID: "t1", Authenticated: true}))
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
