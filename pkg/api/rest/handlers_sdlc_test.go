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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/giteafake"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

type sdlcMemoryResolver struct {
	mu     sync.Mutex
	stores map[string]qrdbsdk.KVStore
}

func (r *sdlcMemoryResolver) Resolve(_ context.Context, tenantID string) (qrdbsdk.KVStore, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stores == nil {
		r.stores = map[string]qrdbsdk.KVStore{}
	}
	store, ok := r.stores[tenantID]
	if !ok {
		store = qrdbsdk.NewMemoryKVStore()
		r.stores[tenantID] = store
	}
	return store, nil
}

// sdlcRESTProjects answers GetProject from a map so Bind's project lookup
// runs; every other verb is unreachable here.
type sdlcRESTProjects struct {
	service.ProjectService
	projects map[string]*orchestration.Project
}

func (p *sdlcRESTProjects) GetProject(_ context.Context, req *service.GetProjectRequest) (*service.GetProjectResponse, error) {
	project, ok := p.projects[req.ProjectID]
	if !ok {
		return nil, &service.ProjectNotFoundError{TenantID: req.TenantID, ProjectID: req.ProjectID}
	}
	return &service.GetProjectResponse{Project: project}, nil
}

// Update implements service.ProjectWriter: binding a project turns it
// autonomous, and that has to be persisted somewhere.
func (p *sdlcRESTProjects) Update(_ context.Context, project *orchestration.Project) error {
	p.projects[project.ID] = project
	return nil
}

type sdlcRESTSecrets struct{ service.SecretsService }

func (sdlcRESTSecrets) GetSecret(context.Context, *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	return &service.GetSecretResponse{Data: []byte("x")}, nil
}

// sdlcRESTPublicURL is the node origin this rig's onboarder provisions
// webhooks to point at.
const sdlcRESTPublicURL = "https://factory.test"

// sdlcRESTForge is the stateful in-memory Gitea every rig binding points at.
// Binding a project now provisions that repository, so the rig needs a forge
// that actually answers — and one that REMEMBERS, or the idempotency the
// handlers rely on could not be observed here at all.
var sdlcRESTForge *giteafake.Server

// sdlcRESTProjectRecords is the rig's project store, so a test that exercises
// a verb which resolves the project (conformance does, to tell "no such
// project" from "no binding") can seed one.
var sdlcRESTProjectRecords map[string]*orchestration.Project

func setupSDLCServer(t *testing.T) (*Server, *service.SDLCService) {
	t.Helper()
	sdlcRESTForge = giteafake.New(t, "acme", "p1")
	server, mock, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	store, err := sdlc.NewStore(&sdlcMemoryResolver{})
	require.NoError(t, err)
	n := qrdbnode.Shared(t)
	routes, err := sdlc.NewWebhookRouteIndex(n.Transaction, n.KV)
	require.NoError(t, err)
	guard, err := sdlc.NewRepoHostGuard([]string{"forge.test"}, false)
	require.NoError(t, err)
	sdlcRESTProjectRecords = map[string]*orchestration.Project{}
	projects := &sdlcRESTProjects{projects: sdlcRESTProjectRecords}
	svc, err := service.NewSDLCService(service.SDLCServiceConfig{
		Store: store, Routes: routes, Guard: guard,
		Projects: projects, ProjectWriter: projects,
		Secrets: sdlcRESTSecrets{}, WebhookPublicURL: sdlcRESTPublicURL,
	})
	require.NoError(t, err)
	mock.SetSDLCService(svc)
	return server, svc
}

func sdlcCall(t *testing.T, server *Server, tenant, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	if tenant != "" {
		req = req.WithContext(iam.ContextWithTenant(req.Context(), tenant))
	}
	r := chi.NewRouter()
	r.Get("/api/v1/sdlc/projects/{projectID}/binding", server.HandleGetSDLCBinding)
	r.Put("/api/v1/sdlc/projects/{projectID}/binding", server.HandleBindSDLCProject)
	r.Delete("/api/v1/sdlc/projects/{projectID}/binding", server.HandleUnbindSDLCProject)
	r.Get("/api/v1/sdlc/tasks/{taskID}/delivery", server.HandleGetSDLCDelivery)
	r.Get("/api/v1/sdlc/forges", server.HandleListSDLCForges)
	r.Post("/api/v1/sdlc/projects/{projectID}/onboarding", server.HandleOnboardSDLCProject)
	r.Get("/api/v1/sdlc/projects/{projectID}/conformance", server.HandleGetSDLCConformance)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func restBinding(id string) sdlc.ProjectBinding {
	return sdlc.ProjectBinding{
		ProjectID: id,
		Forge: sdlc.ForgeConfig{Type: sdlc.ForgeGitea, BaseURL: sdlcRESTForge.APIURL(), Owner: "acme", Repo: id,
			TriggerLabel: "task:ready", TokenSecretRef: "tok"},
		Strategy:        sdlc.GitHubFlowName,
		Worker:          sdlc.WorkerConfig{Backend: "claude", Model: "claude-opus-5"},
		Verification:    []string{"make test"},
		Review:          sdlc.ReviewPolicy{RequiredApprovals: 1},
		MaxReviewRounds: 2,
	}
}

func TestSDLCBinding_ReadUnbindAndDelivery(t *testing.T) {
	server, svc := setupSDLCServer(t)
	ctx := context.Background()
	require.NoError(t, svc.Store().PutBinding(ctx, "tenant-a", ptr(restBinding("p1"))))
	require.NoError(t, svc.Store().PutDelivery(ctx, "tenant-a", &sdlc.Delivery{TaskID: "task-1", ProjectID: "p1", Attempt: 1, Branch: "b",
		PullRequest: sdlc.DeliveryPullRequest{Number: 9, State: sdlc.PullRequestOpen}}))

	rec := sdlcCall(t, server, "tenant-a", http.MethodGet, "/api/v1/sdlc/projects/p1/binding", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got sdlc.ProjectBinding
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "task:ready", got.Forge.TriggerLabel)

	rec = sdlcCall(t, server, "tenant-a", http.MethodGet, "/api/v1/sdlc/tasks/task-1/delivery", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var delivery sdlc.Delivery
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &delivery))
	assert.Equal(t, 9, delivery.PullRequest.Number)

	rec = sdlcCall(t, server, "tenant-a", http.MethodGet, "/api/v1/sdlc/projects/absent/binding", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = sdlcCall(t, server, "tenant-a", http.MethodGet, "/api/v1/sdlc/tasks/absent/delivery", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = sdlcCall(t, server, "tenant-a", http.MethodDelete, "/api/v1/sdlc/projects/p1/binding", nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	rec = sdlcCall(t, server, "tenant-a", http.MethodGet, "/api/v1/sdlc/projects/p1/binding", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSDLCBinding_BindRefusalsAreTyped(t *testing.T) {
	server, _ := setupSDLCServer(t)
	// A project the tenant does not have: 404 from the project service.
	rec := sdlcCall(t, server, "tenant-a", http.MethodPut, "/api/v1/sdlc/projects/p1/binding", restBinding("p1"))
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	// An incomplete binding: 400.
	bad := restBinding("p1")
	bad.Forge.TriggerLabel = ""
	rec = sdlcCall(t, server, "tenant-a", http.MethodPut, "/api/v1/sdlc/projects/p1/binding", bad)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	// A body naming another project: 400.
	rec = sdlcCall(t, server, "tenant-a", http.MethodPut, "/api/v1/sdlc/projects/p1/binding", restBinding("other"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	// Malformed JSON: 400.
	req := httptest.NewRequest(http.MethodPut, "/api/v1/sdlc/projects/p1/binding", bytes.NewBufferString("{nope"))
	req = req.WithContext(iam.ContextWithTenant(req.Context(), "tenant-a"))
	r := chi.NewRouter()
	r.Put("/api/v1/sdlc/projects/{projectID}/binding", server.HandleBindSDLCProject)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	// An unknown forge type: 400.
	unknown := restBinding("p1")
	unknown.Forge.Type = "gitlab"
	rec = sdlcCall(t, server, "tenant-a", http.MethodPut, "/api/v1/sdlc/projects/p1/binding", unknown)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSDLCBinding_TenancyIsTheCallersOwn(t *testing.T) {
	server, svc := setupSDLCServer(t)
	require.NoError(t, svc.Store().PutBinding(context.Background(), "tenant-a", ptr(restBinding("p1"))))
	// Bound to tenant-a but asking for tenant-b: refused.
	rec := sdlcCall(t, server, "tenant-a", http.MethodGet, "/api/v1/sdlc/projects/p1/binding?tenant_id=tenant-b", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	// No tenant anywhere: refused.
	rec = sdlcCall(t, server, "", http.MethodGet, "/api/v1/sdlc/projects/p1/binding", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	// A query tenant with no bound tenant is honoured (an admin caller).
	rec = sdlcCall(t, server, "", http.MethodGet, "/api/v1/sdlc/projects/p1/binding?tenant_id=tenant-a", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	// Another tenant sees nothing.
	rec = sdlcCall(t, server, "tenant-b", http.MethodGet, "/api/v1/sdlc/projects/p1/binding", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSDLCBinding_UnavailableModuleAnswers503(t *testing.T) {
	server, mock, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	mock.SetSDLCService(nil)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/sdlc/projects/p1/binding"},
		{http.MethodPut, "/api/v1/sdlc/projects/p1/binding"},
		{http.MethodDelete, "/api/v1/sdlc/projects/p1/binding"},
		{http.MethodGet, "/api/v1/sdlc/tasks/t1/delivery"},
		{http.MethodGet, "/api/v1/sdlc/forges"},
	} {
		rec := sdlcCall(t, server, "tenant-a", c.method, c.path, nil)
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code, c.path)
	}
}

func TestSDLCBinding_PathParametersAreRequired(t *testing.T) {
	server, _ := setupSDLCServer(t)
	for _, h := range []http.HandlerFunc{server.HandleGetSDLCBinding, server.HandleBindSDLCProject, server.HandleUnbindSDLCProject, server.HandleGetSDLCDelivery} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(iam.ContextWithTenant(req.Context(), "tenant-a"))
		rec := httptest.NewRecorder()
		h(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	}
}

// TestSDLCForges_ListsRegisteredAdapters proves the endpoint reports what
// this build can actually talk to. The gitea adapter registers itself from
// pkg/sdlc's init, so a rest test binary that imports the module carries it;
// an adapter nobody wrote (github, gitlab) must be absent rather than listed
// and failing later at NewForgeBackend.
func TestSDLCForges_ListsRegisteredAdapters(t *testing.T) {
	server, _ := setupSDLCServer(t)

	rec := sdlcCall(t, server, "tenant-a", http.MethodGet, "/api/v1/sdlc/forges", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got SDLCForgesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Contains(t, got.Forges, string(sdlc.ForgeGitea))
	assert.Equal(t, len(got.Forges), got.Count)
	assert.True(t, sort.StringsAreSorted(got.Forges), "forges must be sorted: %v", got.Forges)
	for _, absent := range []sdlc.ForgeType{sdlc.ForgeGitHub, sdlc.ForgeGitLab} {
		if _, err := sdlc.NewForgeBackend(sdlc.ForgeConfig{Type: absent}, sdlc.ForgeCredentials{}, sdlc.ForgeTransport{}); err != nil {
			assert.NotContains(t, got.Forges, string(absent),
				"a type with no adapter must not be listed as available")
		}
	}
}

// TestSDLCForges_TenantIsNotRequired proves the endpoint answers build
// capability rather than tenant data: it is the same list for every caller,
// so a request carrying no bound tenant is answered, not refused.
func TestSDLCForges_TenantIsNotRequired(t *testing.T) {
	server, _ := setupSDLCServer(t)

	rec := sdlcCall(t, server, "", http.MethodGet, "/api/v1/sdlc/forges", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got SDLCForgesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.NotEmpty(t, got.Forges)
}

func ptr[T any](v T) *T { return &v }

// =============================================================================
// Onboarding and conformance
// =============================================================================

// TestSDLCOnboarding_ProvisionsAndReportsSeparately pins the split that is
// the whole point of the two routes: the GET reports and changes nothing, the
// POST repairs. A read that quietly rewrote a repository's configuration
// would make the conformance surface unusable as a fleet dashboard.
func TestSDLCOnboarding_ProvisionsAndReportsSeparately(t *testing.T) {
	server, svc := setupSDLCServer(t)
	ctx := context.Background()
	sdlcRESTProjectRecords["p1"] = &orchestration.Project{ID: "p1", TenantID: "tenant-a"}
	require.NoError(t, svc.Store().PutBinding(ctx, "tenant-a", ptr(restBinding("p1"))))

	// The read first: not conformant, and it wrote nothing.
	rec := sdlcCall(t, server, "tenant-a", http.MethodGet, "/api/v1/sdlc/projects/p1/conformance", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var before sdlc.ConformanceReport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &before))
	require.False(t, before.Conformant, "an unprovisioned repository reported conformant")
	require.NotEmpty(t, before.Findings)
	require.Empty(t, sdlcRESTForge.LabelNames(), "a read-only conformance route created labels")

	for _, finding := range before.Findings {
		require.NotEmpty(t, finding.Remedy, "finding %+v carries no remedy", finding)
		require.NotEmpty(t, finding.Class, "finding %+v carries no class", finding)
	}

	// Then the write.
	rec = sdlcCall(t, server, "tenant-a", http.MethodPost, "/api/v1/sdlc/projects/p1/onboarding", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var report sdlc.OnboardingReport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &report))
	require.Equal(t, []string{"task:ready"}, report.LabelsCreated)
	require.Equal(t, []string{"task:ready"}, sdlcRESTForge.LabelNames())
	require.NotNil(t, report.Conformance, "the onboarding report carries no conformance")
}

// TestSDLCOnboarding_RefusesAnUnknownProject: a project with no binding has
// nothing to provision from, and the route says so rather than answering 200
// with an empty report.
func TestSDLCOnboarding_RefusesAnUnknownProject(t *testing.T) {
	server, _ := setupSDLCServer(t)

	rec := sdlcCall(t, server, "tenant-a", http.MethodPost, "/api/v1/sdlc/projects/nope/onboarding", nil)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// TestSDLCOnboarding_TenancyIsTheCallersOwn: a tenant_id naming another
// tenant is refused on both routes, as it is on the binding routes.
func TestSDLCOnboarding_TenancyIsTheCallersOwn(t *testing.T) {
	server, _ := setupSDLCServer(t)

	for _, path := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/sdlc/projects/p1/onboarding?tenant_id=tenant-b"},
		{http.MethodGet, "/api/v1/sdlc/projects/p1/conformance?tenant_id=tenant-b"},
	} {
		rec := sdlcCall(t, server, "tenant-a", path.method, path.path, nil)
		require.Equal(t, http.StatusForbidden, rec.Code, "%s %s: %s", path.method, path.path, rec.Body.String())
	}
}

// TestSDLCOnboarding_AnsweredUnavailableWithoutTheModule keeps a node that
// does not run the module from answering 200 for a repository it could never
// have provisioned.
func TestSDLCOnboarding_AnsweredUnavailableWithoutTheModule(t *testing.T) {
	server, mock, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	mock.SetSDLCService(nil)

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/sdlc/projects/p1/onboarding"},
		{http.MethodGet, "/api/v1/sdlc/projects/p1/conformance"},
	} {
		rec := sdlcCall(t, server, "tenant-a", tc.method, tc.path, nil)
		require.Equal(t, http.StatusServiceUnavailable, rec.Code, "%s %s", tc.method, tc.path)
	}
}
