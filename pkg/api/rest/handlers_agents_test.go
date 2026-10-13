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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// catalogAgentService is an agent service holding a real agent directory.
type catalogAgentService struct {
	*mockAgentService
	directory *orchestration.AgentDirectory
}

func (a *catalogAgentService) AgentDirectory() *orchestration.AgentDirectory { return a.directory }

// servedModels serves claude with a curated catalog and nothing else.
type servedModels struct{}

func (servedModels) ListBackendModels(_ context.Context, backend string) ([]string, error) {
	if backend != "claude" {
		return nil, &orchestration.BackendNotFoundError{Backend: backend}
	}
	return []string{"claude-opus-5-5"}, nil
}

// agentsServer is a REST server whose agent service holds a catalog when
// wired, and none otherwise.
func agentsServer(t *testing.T, wired bool) *Server {
	t.Helper()
	server, _, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	var agent orchestration.AgentService = &mockAgentService{}
	if wired {
		dir, err := orchestration.NewAgentDirectory(orchestration.AgentDirectoryConfig{
			Store: orchestration.NewInMemoryAgentCatalogStore(), Models: servedModels{},
		})
		require.NoError(t, err)
		agent = &catalogAgentService{mockAgentService: &mockAgentService{}, directory: dir}
	}
	server.service = &MockQuicBaaSWithAgent{MockQuicBaaS: testutil.NewMockQuicBaaS(), agentService: agent}
	return server
}

// agentsCall routes one request through the Agents routes.
func agentsCall(t *testing.T, server *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Get("/api/v1/agents", server.HandleListAgents)
	r.Post("/api/v1/agents", server.HandleCreateAgent)
	r.Get("/api/v1/agents/{agentID}", server.HandleGetAgent)
	r.Put("/api/v1/agents/{agentID}", server.HandleUpdateAgent)
	r.Delete("/api/v1/agents/{agentID}", server.HandleDeleteAgent)
	r.Get("/api/v1/agents/{agentID}/used-by", server.HandleAgentUsedBy)
	r.Get("/api/v1/agent-pools", server.HandleListAgentPools)
	r.Post("/api/v1/agent-pools", server.HandleCreateAgentPool)
	r.Get("/api/v1/agent-pools/{poolID}", server.HandleGetAgentPool)
	r.Put("/api/v1/agent-pools/{poolID}", server.HandleUpdateAgentPool)
	r.Delete("/api/v1/agent-pools/{poolID}", server.HandleDeleteAgentPool)
	r.Get("/api/v1/agent-pools/{poolID}/used-by", server.HandleAgentPoolUsedBy)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, systemRequest(method, path, bytes.NewBufferString(body)))
	return w
}

const restAgentBody = `{"id":"claude-coder","name":"Claude coder","roles":["coding"],"backend":"claude","model":"claude-opus-5-5"}`

func TestAgentsOverREST_AgentLifecycle(t *testing.T) {
	server := agentsServer(t, true)

	w := agentsCall(t, server, http.MethodPost, "/api/v1/agents?tenant_id=acme", restAgentBody)
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	w = agentsCall(t, server, http.MethodGet, "/api/v1/agents?tenant_id=acme", "")
	require.Equal(t, http.StatusOK, w.Code)
	var list agentListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list.Agents, 1)
	assert.Equal(t, "claude-opus-5-5", list.Agents[0].Model)

	w = agentsCall(t, server, http.MethodPut, "/api/v1/agents/claude-coder?tenant_id=acme",
		`{"name":"Coder","roles":["coding","lead"],"backend":"claude","model":"claude-opus-5-5"}`)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	w = agentsCall(t, server, http.MethodGet, "/api/v1/agents/claude-coder?tenant_id=acme", "")
	require.Equal(t, http.StatusOK, w.Code)
	var agent orchestration.Agent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &agent))
	assert.Equal(t, "Coder", agent.Name)

	w = agentsCall(t, server, http.MethodPost, "/api/v1/agent-pools?tenant_id=acme",
		`{"id":"coders","name":"Coders","agents":["claude-coder"]}`)
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	w = agentsCall(t, server, http.MethodGet, "/api/v1/agents/claude-coder/used-by?tenant_id=acme", "")
	require.Equal(t, http.StatusOK, w.Code)
	var used agentUsedByResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &used))
	require.Len(t, used.UsedBy, 1)
	assert.Equal(t, "coders", used.UsedBy[0].ID)

	// An Agent a pool lists is refused, naming the pool.
	w = agentsCall(t, server, http.MethodDelete, "/api/v1/agents/claude-coder?tenant_id=acme", "")
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "coders")

	for _, step := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/agent-pools?tenant_id=acme", ""},
		{http.MethodGet, "/api/v1/agent-pools/coders?tenant_id=acme", ""},
		{http.MethodPut, "/api/v1/agent-pools/coders?tenant_id=acme", `{"name":"Coders","agents":["claude-coder"]}`},
		{http.MethodGet, "/api/v1/agent-pools/coders/used-by?tenant_id=acme", ""},
		{http.MethodDelete, "/api/v1/agent-pools/coders?tenant_id=acme", ""},
		{http.MethodDelete, "/api/v1/agents/claude-coder?tenant_id=acme", ""},
	} {
		w := agentsCall(t, server, step.method, step.path, step.body)
		assert.Equal(t, http.StatusOK, w.Code, "%s %s: %s", step.method, step.path, w.Body.String())
	}
}

func TestAgentsOverREST_RefusalsMapToStatuses(t *testing.T) {
	server := agentsServer(t, true)
	require.Equal(t, http.StatusCreated, agentsCall(t, server, http.MethodPost, "/api/v1/agents?tenant_id=acme", restAgentBody).Code)

	cases := map[string]struct {
		method, path, body string
		want               int
	}{
		"malformed body":     {http.MethodPost, "/api/v1/agents?tenant_id=acme", `{`, http.StatusBadRequest},
		"no model":           {http.MethodPost, "/api/v1/agents?tenant_id=acme", `{"id":"x","name":"X","roles":["coding"],"backend":"claude"}`, http.StatusBadRequest},
		"unserved backend":   {http.MethodPost, "/api/v1/agents?tenant_id=acme", `{"id":"x","name":"X","roles":["coding"],"backend":"vllm","model":"m"}`, http.StatusBadRequest},
		"duplicate":          {http.MethodPost, "/api/v1/agents?tenant_id=acme", restAgentBody, http.StatusConflict},
		"unknown agent":      {http.MethodGet, "/api/v1/agents/ghost?tenant_id=acme", "", http.StatusNotFound},
		"unknown pool":       {http.MethodGet, "/api/v1/agent-pools/ghost?tenant_id=acme", "", http.StatusNotFound},
		"pool of nothing":    {http.MethodPost, "/api/v1/agent-pools?tenant_id=acme", `{"id":"p","name":"P","agents":["ghost"]}`, http.StatusNotFound},
		"bad pool body":      {http.MethodPut, "/api/v1/agent-pools/p?tenant_id=acme", `{`, http.StatusBadRequest},
		"bad update body":    {http.MethodPut, "/api/v1/agents/claude-coder?tenant_id=acme", `{`, http.StatusBadRequest},
		"re-keying update":   {http.MethodPut, "/api/v1/agents/claude-coder?tenant_id=acme", `{"id":"other","name":"X","roles":["coding"],"backend":"claude","model":"claude-opus-5-5"}`, http.StatusBadRequest},
		"used-by of nothing": {http.MethodGet, "/api/v1/agent-pools/ghost/used-by?tenant_id=acme", "", http.StatusNotFound},
		"delete of nothing":  {http.MethodDelete, "/api/v1/agent-pools/ghost?tenant_id=acme", "", http.StatusNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := agentsCall(t, server, tc.method, tc.path, tc.body)
			assert.Equal(t, tc.want, w.Code, "body: %s", w.Body.String())
		})
	}
}

func TestAgentsOverREST_NoCatalogIsUnavailable(t *testing.T) {
	server := agentsServer(t, false)
	w := agentsCall(t, server, http.MethodGet, "/api/v1/agents?tenant_id=acme", "")
	assert.Equal(t, http.StatusServiceUnavailable, w.Code, "body: %s", w.Body.String())
}

func TestRespondAgentCatalogError_UnknownErrorsAreInternal(t *testing.T) {
	w := httptest.NewRecorder()
	respondAgentCatalogError(w, systemRequest(http.MethodGet, "/", nil), errors.New("boom"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleOrchestratorError_MapsCatalogRefusals(t *testing.T) {
	for err, want := range map[error]int{
		&orchestration.UnknownAgentError{ID: "a"}:                                              http.StatusNotFound,
		&orchestration.AgentPoolExhaustedError{Ref: orchestration.PoolRefTo("p")}:              http.StatusServiceUnavailable,
		&orchestration.AgentRoleMismatchError{AgentID: "a", Role: orchestration.AgentRoleLead}: http.StatusBadRequest,
	} {
		w := httptest.NewRecorder()
		handleOrchestratorError(w, systemRequest(http.MethodGet, "/", nil), err)
		assert.Equal(t, want, w.Code, "%T", err)
	}
}

// A fleet refusing a spawn on health or capacity answers as an exhausted
// Agent pool does: 503, not the 400 of a spawn no node could ever serve.
func TestHandleOrchestratorError_AFleetCapacityRefusalIsServiceUnavailable(t *testing.T) {
	refused := &orchestration.FleetCapacityRefusedError{
		Backend:  "claude",
		Refusals: []orchestration.FleetNodeRefusal{{NodeID: "n1", Host: "h1", Reason: orchestration.FleetAdmissionUnhealthy}},
	}
	w := httptest.NewRecorder()
	handleOrchestratorError(w, systemRequest(http.MethodPost, "/", nil), refused)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}
