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

//go:build integration

package helpers

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/dataplane"
)

// roleTestTenant is the tenant every ensureDataPlaneRole test asks for.
const roleTestTenant = "itest-dp-42"

// roleTestBearer is the bearer every ensureDataPlaneRole test sends.
const roleTestBearer = "role-test-bearer"

// fakeRoleServer answers the two role routes ensureDataPlaneRole calls: the
// create POST with createStatus, and the lookup GET with lookupStatus and
// lookupBody. It records every request so a test can assert what was asked.
type fakeRoleServer struct {
	createStatus int
	lookupStatus int
	lookupBody   string

	mu       sync.Mutex
	creates  []map[string]any
	lookups  []string
	bearers  []string
	stranger []string
}

func (f *fakeRoleServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tenants/{tenant}/roles", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.creates = append(f.creates, body)
		f.bearers = append(f.bearers, r.Header.Get("Authorization"))
		f.mu.Unlock()
		w.WriteHeader(f.createStatus)
		_, _ = w.Write([]byte(`{"error":"conflict"}`))
	})
	mux.HandleFunc("GET /api/v1/tenants/{tenant}/roles/{role}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.lookups = append(f.lookups, r.PathValue("tenant")+"/"+r.PathValue("role"))
		f.bearers = append(f.bearers, r.Header.Get("Authorization"))
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.lookupStatus)
		_, _ = w.Write([]byte(f.lookupBody))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.stranger = append(f.stranger, r.Method+" "+r.URL.Path)
		f.mu.Unlock()
		http.NotFound(w, r)
	})
	return mux
}

// startFakeRoleServer serves f over TLS and makes NewHTTPClient trust the
// server's certificate for the test, restoring the integration CA after.
func startFakeRoleServer(t *testing.T, f *fakeRoleServer) string {
	t.Helper()
	srv := httptest.NewTLSServer(f.handler())
	t.Cleanup(srv.Close)
	trustOnlyForTest(t, srv.Certificate())
	return srv.URL
}

// trustOnlyForTest points the cached integration CA pool at cert. Loading the
// pool first completes its sync.Once, so the swap is never overwritten; the
// cleanup restores whatever the real load produced (the pool or its error).
func trustOnlyForTest(t *testing.T, cert *x509.Certificate) {
	t.Helper()
	_, _ = IntegrationCertPool()
	savedPool, savedErr := integrationCAPool, integrationCAErr
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	integrationCAPool, integrationCAErr = pool, nil
	t.Cleanup(func() { integrationCAPool, integrationCAErr = savedPool, savedErr })
}

// roleJSON renders a role lookup body owned by tenantID with permissions.
func roleJSON(t *testing.T, tenantID string, permissions []string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"id":          dataplane.TenantRoleID(roleTestTenant),
		"name":        dataplane.RoleID,
		"tenant_id":   tenantID,
		"permissions": permissions,
	})
	require.NoError(t, err)
	return string(body)
}

// TestEnsureDataPlaneRoleCreates covers the plain path: the create POST
// succeeds, no lookup is made, and the tenant's data-plane role id returns.
func TestEnsureDataPlaneRoleCreates(t *testing.T) {
	f := &fakeRoleServer{createStatus: http.StatusCreated}
	baseURL := startFakeRoleServer(t, f)

	roleID, err := ensureDataPlaneRole(context.Background(), baseURL, roleTestTenant, roleTestBearer)

	require.NoError(t, err)
	require.Equal(t, dataplane.TenantRoleID(roleTestTenant), roleID)
	require.Len(t, f.creates, 1)
	require.Equal(t, dataplane.TenantRoleID(roleTestTenant), f.creates[0]["id"])
	require.Equal(t, dataplane.RoleID, f.creates[0]["name"])
	require.ElementsMatch(t, dataplane.Permissions, f.creates[0]["permissions"])
	require.Empty(t, f.lookups)
	require.Empty(t, f.stranger)
	require.Equal(t, []string{"Bearer " + roleTestBearer}, f.bearers)
}

// TestEnsureDataPlaneRoleConflictReusesMatchingRole covers the 409 a retried
// enrollment meets: the existing role is this tenant's and holds exactly
// dataplane.Permissions (in any order), so its id is reused.
func TestEnsureDataPlaneRoleConflictReusesMatchingRole(t *testing.T) {
	reversed := slices.Clone(dataplane.Permissions)
	slices.Reverse(reversed)
	tests := []struct {
		name        string
		permissions []string
	}{
		{name: "same order", permissions: dataplane.Permissions},
		{name: "other order", permissions: reversed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeRoleServer{
				createStatus: http.StatusConflict,
				lookupStatus: http.StatusOK,
				lookupBody:   roleJSON(t, roleTestTenant, tt.permissions),
			}
			baseURL := startFakeRoleServer(t, f)

			roleID, err := ensureDataPlaneRole(context.Background(), baseURL, roleTestTenant, roleTestBearer)

			require.NoError(t, err)
			require.Equal(t, dataplane.TenantRoleID(roleTestTenant), roleID)
			require.Len(t, f.creates, 1)
			require.Equal(t, []string{roleTestTenant + "/" + dataplane.TenantRoleID(roleTestTenant)}, f.lookups)
			require.Empty(t, f.stranger)
			require.Equal(t, []string{"Bearer " + roleTestBearer, "Bearer " + roleTestBearer}, f.bearers)
		})
	}
}

// TestEnsureDataPlaneRoleConflictRefusesMismatchedRole covers a 409 whose
// existing role is not this tenant's data-plane role: ensureDataPlaneRole
// returns DataPlaneRoleMismatchError naming what it found and no role id.
func TestEnsureDataPlaneRoleConflictRefusesMismatchedRole(t *testing.T) {
	extra := append(slices.Clone(dataplane.Permissions), "iam:admin")
	missing := slices.Clone(dataplane.Permissions[:len(dataplane.Permissions)-1])
	duplicated := append(slices.Clone(dataplane.Permissions[:len(dataplane.Permissions)-1]), dataplane.Permissions[0])
	tests := []struct {
		name        string
		owner       string
		permissions []string
	}{
		{name: "wrong tenant", owner: "itest-dp-other", permissions: dataplane.Permissions},
		{name: "no tenant", owner: "", permissions: dataplane.Permissions},
		{name: "extra permission", owner: roleTestTenant, permissions: extra},
		{name: "missing permission", owner: roleTestTenant, permissions: missing},
		{name: "duplicate in place of missing", owner: roleTestTenant, permissions: duplicated},
		{name: "no permissions", owner: roleTestTenant, permissions: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeRoleServer{
				createStatus: http.StatusConflict,
				lookupStatus: http.StatusOK,
				lookupBody:   roleJSON(t, tt.owner, tt.permissions),
			}
			baseURL := startFakeRoleServer(t, f)

			roleID, err := ensureDataPlaneRole(context.Background(), baseURL, roleTestTenant, roleTestBearer)

			require.Empty(t, roleID, "a refused role must not return a usable role id")
			var mismatch *DataPlaneRoleMismatchError
			require.ErrorAs(t, err, &mismatch)
			require.Equal(t, dataplane.TenantRoleID(roleTestTenant), mismatch.RoleID)
			require.Equal(t, roleTestTenant, mismatch.TenantID)
			require.Equal(t, tt.owner, mismatch.OwnerTenantID)
			require.Equal(t, tt.permissions, mismatch.Permissions)
			require.Len(t, f.lookups, 1)
			require.Empty(t, f.stranger)
		})
	}
}

// TestEnsureDataPlaneRoleConflictLookupFails covers a 409 whose role lookup
// answers non-2xx: the error carries both the 409 and the lookup status, and
// no role id returns.
func TestEnsureDataPlaneRoleConflictLookupFails(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{name: "not found", status: http.StatusNotFound},
		{name: "forbidden", status: http.StatusForbidden},
		{name: "server error", status: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeRoleServer{
				createStatus: http.StatusConflict,
				lookupStatus: tt.status,
				lookupBody:   roleJSON(t, roleTestTenant, dataplane.Permissions),
			}
			baseURL := startFakeRoleServer(t, f)

			roleID, err := ensureDataPlaneRole(context.Background(), baseURL, roleTestTenant, roleTestBearer)

			require.Empty(t, roleID, "a failed lookup must not return a usable role id")
			require.Error(t, err)
			var mismatch *DataPlaneRoleMismatchError
			require.False(t, errors.As(err, &mismatch), "a failed lookup is not a mismatch: %v", err)
			statuses := badStatuses(err)
			require.Equal(t, []int{http.StatusConflict, tt.status}, statuses)
			require.Len(t, f.lookups, 1)
			require.Empty(t, f.stranger)
		})
	}
}

// TestEnsureDataPlaneRoleConflictMalformedLookup covers a 409 whose role
// lookup answers 2xx with a body that is not a role: ensureDataPlaneRole
// returns the decode AuthError and no role id.
func TestEnsureDataPlaneRoleConflictMalformedLookup(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "not json", body: "<html>conflict</html>"},
		{name: "truncated", body: `{"tenant_id":"` + roleTestTenant + `","permissions":[`},
		{name: "permissions not a list", body: `{"tenant_id":"` + roleTestTenant + `","permissions":"kv:get"}`},
		{name: "empty", body: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeRoleServer{
				createStatus: http.StatusConflict,
				lookupStatus: http.StatusOK,
				lookupBody:   tt.body,
			}
			baseURL := startFakeRoleServer(t, f)

			roleID, err := ensureDataPlaneRole(context.Background(), baseURL, roleTestTenant, roleTestBearer)

			require.Empty(t, roleID, "a malformed lookup must not return a usable role id")
			var authErr *AuthError
			require.ErrorAs(t, err, &authErr)
			require.Equal(t, "decode role", authErr.Op)
			var syntaxErr *json.SyntaxError
			var typeErr *json.UnmarshalTypeError
			require.True(t, errors.As(err, &syntaxErr) || errors.As(err, &typeErr),
				"decode error should wrap the json failure: %v", err)
			var mismatch *DataPlaneRoleMismatchError
			require.False(t, errors.As(err, &mismatch))
			require.Len(t, f.lookups, 1)
			require.Empty(t, f.stranger)
		})
	}
}

// badStatuses returns, in order, the status of every AuthBadStatusError
// joined into err.
func badStatuses(err error) []int {
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		var status *AuthBadStatusError
		if errors.As(err, &status) {
			return []int{status.Status}
		}
		return nil
	}
	var out []int
	for _, e := range joined.Unwrap() {
		out = append(out, badStatuses(e)...)
	}
	return out
}
