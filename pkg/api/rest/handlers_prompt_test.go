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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	pt "github.com/jeremyhahn/go-quicbaas/pkg/testutil/prompttest"
)

// errPromptUnclassified is an error the prompt service does not classify.
var errPromptUnclassified = errors.New("unclassified")

// promptREST is a REST server with the prompt routes over a prompt fixture.
type promptREST struct {
	fixture *pt.Fixture
	router  chi.Router
}

// newPromptREST mounts the eight prompt routes, over a fresh fixture when
// mounted is true and with the prompt service unmounted otherwise.
func newPromptREST(t *testing.T, mounted bool) *promptREST {
	t.Helper()
	server, mockService, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)

	p := &promptREST{router: chi.NewRouter()}
	if mounted {
		p.fixture = pt.New(t)
		mockService.SetPromptService(p.fixture.Service)
	}
	p.router.Get("/api/v1/prompts/{set}", server.HandleListPrompts)
	p.router.Get("/api/v1/prompts/{set}/{name}", server.HandleGetPrompt)
	p.router.Get("/api/v1/prompts/{set}/{name}/effective", server.HandleGetEffectivePrompt)
	p.router.Get("/api/v1/prompts/{set}/{name}/diff", server.HandleDiffPrompt)
	p.router.Get("/api/v1/prompts/{set}/{name}/history", server.HandleGetPromptHistory)
	p.router.Put("/api/v1/prompts/{set}/{name}", server.HandlePutPrompt)
	p.router.Post("/api/v1/prompts/{set}/{name}/rollback", server.HandleRollbackPrompt)
	p.router.Delete("/api/v1/prompts/{set}/{name}", server.HandleResetPrompt)
	return p
}

// call sends a request as a caller of tenant; an empty tenant sends none.
func (p *promptREST) call(t *testing.T, tenant, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	ctx := pt.Context()
	if tenant != "" {
		ctx = tenantctx.WithTenant(ctx, tenant)
	}
	rec := httptest.NewRecorder()
	p.router.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

// decode reads a successful response into out.
func decodePromptResponse(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), out))
}

const (
	promptPath       = "/api/v1/prompts/" + pt.Set + "/" + pt.Charter
	promptTenantArgs = "?scope=tenant"
	promptEdited     = "Work the issue carefully.\nEnd with STOP: when done.\n"
)

func TestPromptREST_SuccessPaths(t *testing.T) {
	p := newPromptREST(t, true)

	// Put: the first edit at the tenant scope.
	rec := p.call(t, pt.Tenant, http.MethodPut, promptPath+promptTenantArgs,
		`{"content":"`+strings.ReplaceAll(promptEdited, "\n", `\n`)+`","note":"tighten","expected_version":0}`)
	var put service.PromptView
	decodePromptResponse(t, rec, &put)
	require.Equal(t, int64(1), put.Version)
	require.Equal(t, promptEdited, put.Content)

	// List: the edit shows as overridden.
	var list service.ListPromptsResponse
	decodePromptResponse(t, p.call(t, pt.Tenant, http.MethodGet, "/api/v1/prompts/"+pt.Set+promptTenantArgs, ""), &list)
	require.Len(t, list.Prompts, len(pt.Spec().Prompts))
	overridden := map[string]bool{}
	for _, entry := range list.Prompts {
		overridden[entry.Name] = entry.Overridden
	}
	require.True(t, overridden[pt.Charter])
	require.False(t, overridden[pt.Note])

	// Get: the record, and one of its revisions by version.
	var got service.PromptView
	decodePromptResponse(t, p.call(t, pt.Tenant, http.MethodGet, promptPath+promptTenantArgs, ""), &got)
	require.Equal(t, promptEdited, got.Content)
	decodePromptResponse(t, p.call(t, pt.Tenant, http.MethodGet, promptPath+promptTenantArgs+"&version=1", ""), &got)
	require.Equal(t, int64(1), got.Version)

	// Effective at the project scope inherits the tenant edit.
	var effective service.EffectivePrompt
	decodePromptResponse(t, p.call(t, pt.Tenant, http.MethodGet,
		promptPath+"/effective?scope=project&project_id="+pt.Project, ""), &effective)
	require.Equal(t, promptEdited, effective.Content)
	require.NotEmpty(t, effective.Chain)

	// Diff: the tenant edit against the system default.
	var diff service.PromptDiff
	decodePromptResponse(t, p.call(t, pt.Tenant, http.MethodGet, promptPath+"/diff"+promptTenantArgs, ""), &diff)
	require.False(t, diff.Same)

	// Rollback to the first revision makes a second.
	var rolled service.PromptView
	decodePromptResponse(t, p.call(t, pt.Tenant, http.MethodPost, promptPath+"/rollback"+promptTenantArgs,
		`{"to_version":1,"note":"back","expected_version":1}`), &rolled)
	require.Equal(t, int64(2), rolled.Version)

	// Reset follows the parent again.
	var reset service.PromptView
	decodePromptResponse(t, p.call(t, pt.Tenant, http.MethodDelete,
		promptPath+promptTenantArgs+"&expected_version=2&note=reset", ""), &reset)
	require.True(t, reset.FollowParent)

	// History: the edit, the rollback and the reset.
	var history service.PromptHistory
	decodePromptResponse(t, p.call(t, pt.Tenant, http.MethodGet, promptPath+"/history"+promptTenantArgs, ""), &history)
	require.Len(t, history.Revisions, 3)
}

func TestPromptREST_StatusMapping(t *testing.T) {
	missingMarker := `{"content":"no marker here","note":"n","expected_version":0}`
	cases := []struct {
		name, method, target, body string
		status                     int
		bodyHas                    string
	}{
		{"get not overridden", http.MethodGet, promptPath + promptTenantArgs, "", http.StatusNotFound, ""},
		{"get unknown project", http.MethodGet, promptPath + "?scope=project&project_id=ghost", "", http.StatusNotFound, ""},
		{"stale expected_version", http.MethodPut, promptPath + promptTenantArgs,
			`{"content":"End with STOP: now.","note":"n","expected_version":7}`, http.StatusConflict, ""},
		{"missing marker", http.MethodPut, promptPath + promptTenantArgs, missingMarker, http.StatusBadRequest, pt.Marker},
		{"unknown set", http.MethodGet, "/api/v1/prompts/ghost" + promptTenantArgs, "", http.StatusBadRequest, ""},
		{"unknown name", http.MethodGet, "/api/v1/prompts/" + pt.Set + "/ghost/effective" + promptTenantArgs, "", http.StatusBadRequest, ""},
		{"system write", http.MethodPut, promptPath + "?scope=system",
			`{"content":"End with STOP: now.","note":"n","expected_version":0}`, http.StatusBadRequest, ""},
		{"non-integer version", http.MethodGet, promptPath + promptTenantArgs + "&version=x", "", http.StatusBadRequest, "version"},
		{"non-integer reset version", http.MethodDelete, promptPath + promptTenantArgs + "&expected_version=x", "", http.StatusBadRequest, "expected_version"},
		{"bad put body", http.MethodPut, promptPath + promptTenantArgs, "{", http.StatusBadRequest, "invalid JSON"},
		{"bad rollback body", http.MethodPost, promptPath + "/rollback" + promptTenantArgs, "{", http.StatusBadRequest, "invalid JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newPromptREST(t, true)
			rec := p.call(t, pt.Tenant, tc.method, tc.target, tc.body)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), tc.bodyHas)
		})
	}
}

func TestPromptREST_UnreachableBackendIsUnavailable(t *testing.T) {
	p := newPromptREST(t, true)
	p.fixture.Resolver.Fail()
	rec := p.call(t, pt.Tenant, http.MethodGet, promptPath+"/effective"+promptTenantArgs, "")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
}

// promptRoutes is every prompt route, with a body that would otherwise pass.
var promptRoutes = []struct{ method, target, body string }{
	{http.MethodGet, "/api/v1/prompts/" + pt.Set, ""},
	{http.MethodGet, promptPath, ""},
	{http.MethodGet, promptPath + "/effective", ""},
	{http.MethodGet, promptPath + "/diff", ""},
	{http.MethodGet, promptPath + "/history", ""},
	{http.MethodPut, promptPath, `{"content":"End with STOP: now.","note":"n"}`},
	{http.MethodPost, promptPath + "/rollback", `{"to_version":1,"note":"n","expected_version":1}`},
	{http.MethodDelete, promptPath, ""},
}

func TestPromptREST_UnmountedIsUnavailable(t *testing.T) {
	p := newPromptREST(t, false)
	for _, route := range promptRoutes {
		rec := p.call(t, pt.Tenant, route.method, route.target+promptTenantArgs, route.body)
		require.Equal(t, http.StatusServiceUnavailable, rec.Code, "%s %s", route.method, route.target)
		require.Contains(t, rec.Body.String(), service.ErrPromptsUnavailable.Error())
	}
}

func TestPromptREST_TenantMismatchIsForbidden(t *testing.T) {
	p := newPromptREST(t, true)
	for _, route := range promptRoutes {
		rec := p.call(t, pt.Tenant, route.method, route.target+promptTenantArgs+"&tenant_id=tenant-b", route.body)
		require.Equal(t, http.StatusForbidden, rec.Code, "%s %s", route.method, route.target)

		rec = p.call(t, "", route.method, route.target+promptTenantArgs, route.body)
		require.Equal(t, http.StatusForbidden, rec.Code, "no tenant: %s %s", route.method, route.target)
	}
}

func TestPromptREST_OwnTenantIDIsAccepted(t *testing.T) {
	p := newPromptREST(t, true)
	rec := p.call(t, pt.Tenant, http.MethodGet, promptPath+"/effective"+promptTenantArgs+"&tenant_id="+pt.Tenant, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestRespondPromptError(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"classified", service.ErrPromptsUnavailable, http.StatusServiceUnavailable},
		{"unclassified", errPromptUnclassified, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			respondPromptError(rec, httptest.NewRequest(http.MethodGet, "/", nil), tc.err)
			require.Equal(t, tc.status, rec.Code)
		})
	}
}

// TestPromptRoutesResolveToRegisteredPermissions pins the permission each
// prompt route demands. RequireAuthz refuses a principal that lacks it, so a
// row missing or naming the wrong action is a principal let through.
func TestPromptRoutesResolveToRegisteredPermissions(t *testing.T) {
	cases := []struct{ method, path, action string }{
		{http.MethodGet, "/api/v1/prompts/s", "read"},
		{http.MethodGet, "/api/v1/prompts/s/n", "read"},
		{http.MethodGet, "/api/v1/prompts/s/n/effective", "read"},
		{http.MethodGet, "/api/v1/prompts/s/n/diff", "read"},
		{http.MethodGet, "/api/v1/prompts/s/n/history", "history"},
		{http.MethodPut, "/api/v1/prompts/s/n", "write"},
		{http.MethodPost, "/api/v1/prompts/s/n/rollback", "write"},
		{http.MethodDelete, "/api/v1/prompts/s/n", "write"},
	}
	for _, tc := range cases {
		resource, action, ok := routePermissionForPath(tc.path, tc.method)
		if !ok {
			t.Errorf("%s %s is unmapped (hard-403 under RequireAuthz)", tc.method, tc.path)
			continue
		}
		if resource != "prompts" || action != tc.action {
			t.Errorf("%s %s -> %s:%s, want prompts:%s", tc.method, tc.path, resource, action, tc.action)
		}
		auth, ok := routeAuthForPath(tc.path, tc.method)
		if !ok || auth == RouteAuthPublic {
			t.Errorf("%s %s must be permission-gated, got %v", tc.method, tc.path, auth)
		}
	}
}

func TestPromptRoutes_UnknownVerbIsUnmapped(t *testing.T) {
	if _, _, ok := routePermissionForPath("/api/v1/prompts/s/n/promote", http.MethodPost); ok {
		t.Fatal("an unrouted prompt verb must not resolve to a permission")
	}
}
