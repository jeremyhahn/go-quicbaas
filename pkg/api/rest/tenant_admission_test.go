//go:build rest

package rest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestTenantAdmission_HeldTenantIsRefused503 pins #2212: while the server's
// unseal admission holds a tenant, a request bound to it and a request naming
// it in its URL are both answered 503, and once the hold is released (or no
// admission is configured) they pass through.
func TestTenantAdmission_HeldTenantIsRefused503(t *testing.T) {
	var held atomic.Bool
	held.Store(true)
	admit := func(tenantID string) error {
		if tenantID == "acme" && held.Load() {
			return errors.New("tenant \"acme\" is still running its unseal checks")
		}
		return nil
	}

	bound := func(s *Server) (int, bool) {
		var called bool
		h := s.projectQRDBIdentity(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}), true)
		req := asTenantCaller(httptest.NewRequest(http.MethodGet, "/api/v1/things", nil), "acme")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code, called
	}
	routed := func(s *Server) (int, bool) {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant", "acme")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/acme/things", nil)
		req = asTenantCaller(req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx)), qrdbsdk.SystemTenantID)
		req = systemAdminLiftRequest(t, req, "tenants", "read")
		rec := httptest.NewRecorder()
		_, tenantID, ok := s.urlTenant(rec, req, "list users", "tenant")
		if ok {
			assert.Equal(t, "acme", tenantID)
		}
		return rec.Code, ok
	}

	s := &Server{config: &Config{TenantAdmission: admit}}
	s.SetBarrierScopeAuthorizer(&identityRecordingAuthorizer{})

	code, called := bound(s)
	assert.Equal(t, http.StatusServiceUnavailable, code, "a request bound to a held tenant is refused")
	assert.False(t, called, "a held tenant's request never reaches the handler")

	code, ok := routed(s)
	assert.Equal(t, http.StatusServiceUnavailable, code, "a URL naming a held tenant is refused")
	assert.False(t, ok)

	held.Store(false)
	_, called = bound(s)
	assert.True(t, called, "a released tenant's request reaches the handler")
	_, ok = routed(s)
	assert.True(t, ok, "a URL naming a released tenant resolves")

	unrestricted := &Server{config: &Config{}}
	held.Store(true)
	_, called = bound(unrestricted)
	require.True(t, called, "a nil TenantAdmission admits every tenant")
}

// TestTenantAdmission_SystemAdminRepairsAHeldTenantsPolicies: only the system
// admin's policy route resolves a held tenant, marked for repair of that
// tenant alone; the tenant's own caller and every other route stay refused.
func TestTenantAdmission_SystemAdminRepairsAHeldTenantsPolicies(t *testing.T) {
	admit := func(tenantID string) error {
		if tenantID == "acme" {
			return errors.New("tenant \"acme\" is still running its unseal checks")
		}
		return nil
	}
	request := func(tenant, caller string) *http.Request {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant", tenant)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/"+tenant+"/policies/fix", nil)
		req = asTenantCaller(req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx)), caller)
		if caller == qrdbsdk.SystemTenantID {
			req = systemAdminLiftRequest(t, req, "tenants", "create")
		}
		return req
	}

	s := &Server{config: &Config{TenantAdmission: admit}}
	s.SetBarrierScopeAuthorizer(&identityRecordingAuthorizer{})

	rec := httptest.NewRecorder()
	ctx, tenantID, repair, ok := s.policyRepairTenant(rec, request("acme", qrdbsdk.SystemTenantID), "update policy", "tenant")
	require.True(t, ok, "the system admin's policy route resolves a held tenant")
	assert.True(t, repair)
	assert.Equal(t, "acme", tenantID)
	require.NotNil(t, ctx)
	assert.True(t, tenantctx.InPolicyRepair(ctx, "acme"), "the ctx is marked for acme's repair")
	assert.Equal(t, http.StatusOK, rec.Code, "the repair path writes no response")
	assert.Empty(t, rec.Body.String())

	rec = httptest.NewRecorder()
	_, _, repair, ok = s.policyRepairTenant(rec, request("acme", "acme"), "update policy", "tenant")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, "the held tenant's own caller is refused")
	assert.False(t, ok)
	assert.False(t, repair)

	rec = httptest.NewRecorder()
	ctx, tenantID, repair, ok = s.policyRepairTenant(rec, request("beta", qrdbsdk.SystemTenantID), "update policy", "tenant")
	require.True(t, ok, "a released tenant resolves")
	assert.False(t, repair, "a released tenant is not a repair")
	assert.Equal(t, "beta", tenantID)
	assert.False(t, tenantctx.InPolicyRepair(ctx, "beta"))

	rec = httptest.NewRecorder()
	_, _, ok = s.urlTenant(rec, request("acme", qrdbsdk.SystemTenantID), "list users", "tenant")
	assert.False(t, ok)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, "every other route still refuses the held tenant")
}

func TestTenantAdmission_RecoveryRoutesPassAHeldTenant(t *testing.T) {
	admit := func(tenantID string) error {
		if tenantID == "acme" {
			return errors.New("tenant \"acme\" is still running its unseal checks")
		}
		return nil
	}
	s := &Server{config: &Config{TenantAdmission: admit}}

	serve := func(recovery bool) (int, bool) {
		var called bool
		spec := RouteSpec{
			Method:         http.MethodPost,
			Pattern:        "/api/v1/tenants/{tenant_id}/barrier/unseal",
			Group:          GroupIAM,
			TenantRecovery: recovery,
			Handler: func(*Server) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					called = true
					w.WriteHeader(http.StatusOK)
				}
			},
		}
		req := asTenantCaller(httptest.NewRequest(http.MethodPost, "/api/v1/tenants/acme/barrier/unseal", nil), "acme")
		rec := httptest.NewRecorder()
		s.BindRoute(spec).ServeHTTP(rec, req)
		return rec.Code, called
	}

	code, called := serve(true)
	assert.Equal(t, http.StatusOK, code)
	assert.True(t, called, "a held tenant can retry a barrier recovery route")

	code, called = serve(false)
	assert.Equal(t, http.StatusServiceUnavailable, code, "a held tenant's other routes stay refused")
	assert.False(t, called)

	recovery := map[string]bool{}
	for _, spec := range iamManagedRouteSpecs() {
		if spec.TenantRecovery {
			recovery[spec.Method+" "+spec.Pattern] = true
		}
	}
	assert.Equal(t, map[string]bool{
		"POST /api/v1/tenants/{tenant_id}/barrier/unseal":       true,
		"POST /api/v1/tenants/{tenant_id}/barrier/unseal-share": true,
		"POST /api/v1/tenants/{tenant_id}/barrier/seal":         true,
	}, recovery, "only unseal, unseal share and seal skip the unseal admission")
}
