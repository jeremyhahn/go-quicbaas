//go:build rest

// Copyright (c) 2026 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package rest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane/serviceplanetest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func servicesRouter(t *testing.T, svc service.ServicesService) http.Handler {
	t.Helper()
	server, mockSvc, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	mockSvc.ServicesSvc = svc
	r := chi.NewRouter()
	for _, spec := range rawRouteSpecs() {
		if spec.Pattern == "/api/v1/services" || len(spec.Pattern) > len("/api/v1/services/") &&
			spec.Pattern[:len("/api/v1/services/")] == "/api/v1/services/" {
			r.MethodFunc(spec.Method, spec.Pattern, spec.Handler(server))
		}
	}
	return r
}

func serveServices(t *testing.T, h http.Handler, tenant, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		require.NoError(t, err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	if tenant != "" {
		req = req.WithContext(tenantctx.WithTenant(req.Context(), tenant))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func decodeServiceRecord(t *testing.T, w *httptest.ResponseRecorder) *service.ServiceRecord {
	t.Helper()
	var rec service.ServiceRecord
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rec), w.Body.String())
	return &rec
}

func TestHandleServices_TheSevenVerbsOverTheRouteTable(t *testing.T) {
	rt := serviceplanetest.NewRuntime(t)
	h := servicesRouter(t, service.NewServicesService(rt.Resolver(t)))
	deploy := &service.DeployServiceRequest{Manifest: rt.Sign(t, "acme", "web")}

	w := serveServices(t, h, "acme", http.MethodPost, "/api/v1/services", deploy)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	rec := decodeServiceRecord(t, w)
	assert.Equal(t, "running", rec.State)
	assert.Equal(t, "web", rec.Manifest.Name)
	assert.Equal(t, "identity:acme/web", rec.Grants["identity"])

	for _, step := range []struct{ verb, state string }{
		{"stop", "stopped"}, {"start", "running"}, {"restart", "running"},
	} {
		w = serveServices(t, h, "acme", http.MethodPost, "/api/v1/services/web/"+step.verb, nil)
		require.Equal(t, http.StatusOK, w.Code, step.verb+": "+w.Body.String())
		assert.Equal(t, step.state, decodeServiceRecord(t, w).State, step.verb)
	}

	w = serveServices(t, h, "acme", http.MethodPut, "/api/v1/services",
		&service.DeployServiceRequest{Manifest: rt.Sign(t, "acme", "web")})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.EqualValues(t, 2, decodeServiceRecord(t, w).Revision)

	w = serveServices(t, h, "acme", http.MethodGet, "/api/v1/services", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var list service.ListServicesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list.Services, 1)

	w = serveServices(t, h, "acme", http.MethodGet, "/api/v1/services/web", nil)
	require.Equal(t, http.StatusOK, w.Code)

	w = serveServices(t, h, "acme", http.MethodDelete, "/api/v1/services/web", nil)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	w = serveServices(t, h, "acme", http.MethodGet, "/api/v1/services/web", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleServices_ResumeAfterAFailedLaunch(t *testing.T) {
	rt := serviceplanetest.NewRuntime(t)
	h := servicesRouter(t, service.NewServicesService(rt.Resolver(t)))
	rt.Launcher.Fail(assert.AnError)
	w := serveServices(t, h, "acme", http.MethodPost, "/api/v1/services",
		&service.DeployServiceRequest{Manifest: rt.Sign(t, "acme", "web")})
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	rt.Launcher.Fail(nil)
	w = serveServices(t, h, "acme", http.MethodPost, "/api/v1/services/web/resume", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "running", decodeServiceRecord(t, w).State)
}

func TestHandleServices_Refusals(t *testing.T) {
	rt := serviceplanetest.NewRuntime(t)
	h := servicesRouter(t, service.NewServicesService(rt.Resolver(t)))
	deploy := &service.DeployServiceRequest{Manifest: rt.Sign(t, "acme", "web")}
	require.Equal(t, http.StatusCreated, serveServices(t, h, "acme", http.MethodPost, "/api/v1/services", deploy).Code)

	assert.Equal(t, http.StatusConflict, serveServices(t, h, "acme", http.MethodPost, "/api/v1/services", deploy).Code)
	assert.Equal(t, http.StatusPreconditionFailed,
		serveServices(t, h, "acme", http.MethodPost, "/api/v1/services/web/start", nil).Code)
	assert.Equal(t, http.StatusForbidden, serveServices(t, h, "acme", http.MethodPost, "/api/v1/services",
		&service.DeployServiceRequest{Manifest: rt.Sign(t, "other", "api")}).Code)
	assert.Equal(t, http.StatusNotFound, serveServices(t, h, "other", http.MethodGet, "/api/v1/services/web", nil).Code)
	assert.Equal(t, http.StatusBadRequest, serveServices(t, h, "acme", http.MethodPost, "/api/v1/services", map[string]any{}).Code)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/services", bytes.NewReader([]byte("{")))
	req = req.WithContext(tenantctx.WithTenant(req.Context(), "acme"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleServices_ServiceUnavailable(t *testing.T) {
	h := servicesRouter(t, nil)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/services"},
		{http.MethodPost, "/api/v1/services"},
		{http.MethodPut, "/api/v1/services"},
		{http.MethodGet, "/api/v1/services/web"},
		{http.MethodDelete, "/api/v1/services/web"},
		{http.MethodPost, "/api/v1/services/web/start"},
		{http.MethodPost, "/api/v1/services/web/stop"},
		{http.MethodPost, "/api/v1/services/web/resume"},
		{http.MethodPost, "/api/v1/services/web/restart"},
	} {
		assert.Equal(t, http.StatusServiceUnavailable, serveServices(t, h, "acme", c.method, c.path, map[string]any{}).Code, c.method+" "+c.path)
	}
}
