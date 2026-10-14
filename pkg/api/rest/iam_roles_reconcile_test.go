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
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// reconcileTenant is the tenant these tests reconcile. It is deliberately not
// "system": reconciliation is a data-plane verb every tenant needs, and a test
// that only ever exercises the control-plane tenant would not notice a handler
// that hardcoded it.
const reconcileTenant = "tenant-reconcile"

// reconcilingRoleStorage is an iam.MemoryStorage that additionally speaks
// iam.BuiltInRoleReconciler — the storage-side seam IdentityStore probes for.
// Production satisfies it with iam.QRDBRoleStorage, which needs a live qrdb
// node and therefore cannot run in a unit test; reconciliation itself is
// qrdb's and is covered there. What these tests pin is the REST transport
// around it, so this records the call it receives and returns the report the
// transport has to render.
type reconcilingRoleStorage struct {
	*iam.MemoryStorage

	report *iam.RoleReconcileReport
	err    error

	// tenants records every tenant reconciliation was asked for, in order, so
	// a test can prove the handler forwarded the tenant from the URL and did
	// not reconcile some other one.
	tenants []string
}

func (s *reconcilingRoleStorage) ReconcileBuiltInRoles(_ context.Context, tenantID string) (*iam.RoleReconcileReport, error) {
	s.tenants = append(s.tenants, tenantID)
	if s.err != nil {
		return nil, s.err
	}
	return s.report, nil
}

// setupReconcileTest builds an IAM handler whose identity store is backed by a
// role storage that can reconcile, and seeds reconcileTenant. The returned
// storage is the knob each test turns to choose the reconciliation outcome.
func setupReconcileTest(t *testing.T) (*IAMHandler, *reconcilingRoleStorage, func()) {
	t.Helper()

	_, mockService, memStorage, handler, cleanup := setupIAMTestServer(t)

	roleStorage := &reconcilingRoleStorage{MemoryStorage: memStorage}
	mockService.identityStore = iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: memStorage,
		RoleStorage: roleStorage,
		Tenants:     mockService.tenantManager,
	})

	require.NoError(t, memStorage.CreateTenant(context.Background(), &iam.Tenant{
		ID:   reconcileTenant,
		Name: "Reconcile Tenant",
	}))

	return handler, roleStorage, cleanup
}

// reconcileRequest issues POST .../roles/reconcile for tenantID against the
// handler method directly, with the chi param the router would have set.
func reconcileRequest(handler *IAMHandler, tenantID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/"+tenantID+"/roles/reconcile", nil)
	req = withChiParams(req, map[string]string{"tenant_id": tenantID})
	w := httptest.NewRecorder()
	handler.ReconcileRoles(w, req)
	return w
}

// TestReconcileRoles_ReportsWhatChanged is the success path: the handler
// forwards the tenant from the URL, and renders the store's report as
// tenant_id/roles/changed with the per-role added and removed permission
// lists intact — those lists are the whole point of the verb, so a transport
// that dropped them would report a repair it did not describe.
func TestReconcileRoles_ReportsWhatChanged(t *testing.T) {
	handler, roleStorage, cleanup := setupReconcileTest(t)
	defer cleanup()

	roleStorage.report = &iam.RoleReconcileReport{
		TenantID: reconcileTenant,
		Roles: []iam.RoleReconcileResult{
			{
				RoleID:  "role-admin",
				Name:    "admin",
				Added:   []string{"roles:reconcile", "tenants:read"},
				Removed: []string{"tenants:legacy_read"},
			},
			{
				RoleID:  "role-viewer",
				Name:    "viewer",
				Created: true,
				Added:   []string{"tenants:read"},
			},
		},
	}

	w := reconcileRequest(handler, reconcileTenant)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Equal(t, []string{reconcileTenant}, roleStorage.tenants)

	var resp struct {
		TenantID string                    `json:"tenant_id"`
		Roles    []iam.RoleReconcileResult `json:"roles"`
		Changed  bool                      `json:"changed"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, reconcileTenant, resp.TenantID)
	assert.True(t, resp.Changed)
	require.Len(t, resp.Roles, 2)

	assert.Equal(t, "role-admin", resp.Roles[0].RoleID)
	assert.Equal(t, "admin", resp.Roles[0].Name)
	assert.False(t, resp.Roles[0].Created)
	assert.Equal(t, []string{"roles:reconcile", "tenants:read"}, resp.Roles[0].Added)
	assert.Equal(t, []string{"tenants:legacy_read"}, resp.Roles[0].Removed)

	assert.Equal(t, "role-viewer", resp.Roles[1].RoleID)
	assert.True(t, resp.Roles[1].Created)
	assert.Equal(t, []string{"tenants:read"}, resp.Roles[1].Added)
}

// TestReconcileRoles_AlreadyConvergedIsSuccessNotError covers the idempotent
// case: a tenant that already agrees with the build answers 200 with
// changed=false. It must not be an error status — the verb is meant to run on
// every daemon start, and the common outcome cannot look like a failure.
func TestReconcileRoles_AlreadyConvergedIsSuccessNotError(t *testing.T) {
	handler, roleStorage, cleanup := setupReconcileTest(t)
	defer cleanup()

	roleStorage.report = &iam.RoleReconcileReport{TenantID: reconcileTenant}

	w := reconcileRequest(handler, reconcileTenant)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var resp struct {
		TenantID string                    `json:"tenant_id"`
		Roles    []iam.RoleReconcileResult `json:"roles"`
		Changed  bool                      `json:"changed"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, reconcileTenant, resp.TenantID)
	assert.False(t, resp.Changed)
	assert.Empty(t, resp.Roles)
}

// TestReconcileRoles_MissingTenantID rejects a request with no tenant in the
// path before it reaches the identity store.
func TestReconcileRoles_MissingTenantID(t *testing.T) {
	handler, roleStorage, cleanup := setupReconcileTest(t)
	defer cleanup()

	w := reconcileRequest(handler, "")

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "tenant ID is required")
	assert.Empty(t, roleStorage.tenants, "no tenant was named; nothing may be reconciled")
}

// TestReconcileRoles_UnknownTenant answers 404 for a tenant that does not
// exist, and — the part that matters — reconciles nothing. Without the
// existence check the request would reach the storage with an arbitrary
// caller-supplied tenant id.
func TestReconcileRoles_UnknownTenant(t *testing.T) {
	handler, roleStorage, cleanup := setupReconcileTest(t)
	defer cleanup()

	roleStorage.report = &iam.RoleReconcileReport{TenantID: "tenant-absent"}

	w := reconcileRequest(handler, "tenant-absent")

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "tenant not found")
	assert.Empty(t, roleStorage.tenants)
}

// TestReconcileRoles_IdentityStoreUnavailable covers the service that cannot
// produce an identity store at all: the handler says so rather than panicking
// on the type assertion.
func TestReconcileRoles_IdentityStoreUnavailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	w := reconcileRequest(handler, reconcileTenant)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "identity store not available")
}

// TestReconcileRoles_StorageRefusesReconciliation covers the storage that does
// not implement iam.BuiltInRoleReconciler: the built-in definitions belong to
// qrdb, so a storage that cannot reach them refuses rather than converging the
// tenant onto a set this package invented. The refusal is a server-side
// configuration fault, so it must not be reported to the caller as a bad
// request.
func TestReconcileRoles_StorageRefusesReconciliation(t *testing.T) {
	_, mockService, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	// MemoryStorage is a plain RoleStorage: no BuiltInRoleReconciler.
	mockService.identityStore = iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: memStorage,
		RoleStorage: memStorage,
		Tenants:     mockService.tenantManager,
	})
	require.NoError(t, memStorage.CreateTenant(context.Background(), &iam.Tenant{
		ID:   reconcileTenant,
		Name: "Reconcile Tenant",
	}))

	w := reconcileRequest(handler, reconcileTenant)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestReconcileRoles_StorageErrorIsSurfaced proves a failure inside
// reconciliation reaches the caller as a server error through the shared
// service-error mapping, instead of a 200 reporting a repair that never
// happened.
func TestReconcileRoles_StorageErrorIsSurfaced(t *testing.T) {
	handler, roleStorage, cleanup := setupReconcileTest(t)
	defer cleanup()

	roleStorage.err = iam.ErrRoleNotFound

	w := reconcileRequest(handler, reconcileTenant)

	assert.GreaterOrEqual(t, w.Code, http.StatusBadRequest)
	assert.NotEqual(t, http.StatusOK, w.Code)
	assert.Equal(t, []string{reconcileTenant}, roleStorage.tenants)
}

// TestReconcileRoles_RouteReachesHandler serves the request through the router
// the legacy listener builds, so the mount is proven rather than assumed:
// "reconcile" is a literal segment sharing a parent with /roles/{role_id}, and
// a param pattern that swallowed it would send the POST somewhere else.
func TestReconcileRoles_RouteReachesHandler(t *testing.T) {
	_, mockService, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	roleStorage := &reconcilingRoleStorage{
		MemoryStorage: memStorage,
		report:        &iam.RoleReconcileReport{TenantID: reconcileTenant},
	}
	mockService.identityStore = iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: memStorage,
		RoleStorage: roleStorage,
		Tenants:     mockService.tenantManager,
	})
	require.NoError(t, memStorage.CreateTenant(context.Background(), &iam.Tenant{
		ID:   reconcileTenant,
		Name: "Reconcile Tenant",
	}))

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/"+reconcileTenant+"/roles/reconcile", nil)
	req = req.WithContext(tenantctx.WithTenant(req.Context(), reconcileTenant))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Equal(t, []string{reconcileTenant}, roleStorage.tenants,
		"the router sent the request somewhere other than ReconcileRoles")
}

// TestReconcileRoles_RouteSpecUsesRoleCreateGrant pins the authorization
// decision, not just its presence in the frozen table. Reconciliation is
// gated on the role-CREATE family (tenants:create) on go-qrdb's guidance
// (go-quicbaas#573): a dedicated roles:reconcile verb would be missing from
// every role seeded before that verb existed, so no principal would hold the
// grant needed to repair precisely that absence. Re-permissioning this route
// to a verb of its own is the mistake this test exists to catch.
func TestReconcileRoles_RouteSpecUsesRoleCreateGrant(t *testing.T) {
	const pattern = "/api/v1/tenants/{tenant_id}/roles/reconcile"

	var found bool
	for _, spec := range Routes() {
		if spec.Method != http.MethodPost || spec.Pattern != pattern {
			continue
		}
		found = true
		assert.Equal(t, RouteAuthPermission, spec.Auth)
		assert.Equal(t, "tenants", spec.Resource)
		assert.Equal(t, "create", spec.Action)

		for _, sibling := range Routes() {
			if sibling.Method == http.MethodPost && sibling.Pattern == "/api/v1/tenants/{tenant_id}/roles/" {
				assert.Equal(t, sibling.Resource, spec.Resource,
					"reconcile must share the role-create grant")
				assert.Equal(t, sibling.Action, spec.Action,
					"reconcile must share the role-create grant")
			}
		}
	}
	require.True(t, found, "POST %s is absent from the route table", pattern)
}
