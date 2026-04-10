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

//go:build integration && rag

package orchestration

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestE2E_CrossBackend_OllamaSession verifies that the AgentService can
// prompt the Ollama backend and receive a non-empty response, exercising
// the full path: AgentService -> OllamaBackend -> Ollama API.
func TestE2E_CrossBackend_OllamaSession(t *testing.T) {
	requireOllama(t)

	svc := newAgentService(t)

	ctx, cancel := context.WithTimeout(context.Background(), promptTimeout)
	ctx = orchestration.ContextWithTenant(ctx, "test-tenant-cross-backend")
	defer cancel()

	resp, err := svc.PromptSession(ctx, &orchestration.PromptSessionRequest{
		Backend: "ollama",
		Prompt:  "Respond with exactly one word: hello",
		Model:   env.ollamaModel,
	})
	require.NoError(t, err, "Ollama prompt should succeed")
	require.NotNil(t, resp)

	assert.NotEmpty(t, resp.Content, "response content must not be empty")
	assert.NotEmpty(t, resp.Content[0].Text, "first content block text must not be empty")
	assert.NotZero(t, resp.Timestamp, "response timestamp must be set")
}

// TestE2E_CrossBackend_OllamaSession_EmptyPrompt verifies that prompting
// the Ollama backend with an empty string returns a typed error.
func TestE2E_CrossBackend_OllamaSession_EmptyPrompt(t *testing.T) {
	requireOllama(t)

	svc := newAgentService(t)

	ctx, cancel := context.WithTimeout(context.Background(), promptTimeout)
	ctx = orchestration.ContextWithTenant(ctx, "test-tenant-cross-backend")
	defer cancel()

	_, err := svc.PromptSession(ctx, &orchestration.PromptSessionRequest{
		Backend: "ollama",
		Prompt:  "",
	})
	require.Error(t, err, "empty prompt must return an error")
	assert.ErrorIs(t, err, orchestration.ErrPromptRequired)
}

// TestE2E_CrossBackend_ListBackends verifies that the Ollama backend is
// listed with correct type and capabilities when available.
func TestE2E_CrossBackend_ListBackends(t *testing.T) {
	svc := newAgentService(t)

	ctx := orchestration.ContextWithTenant(context.Background(), "test-tenant-cross-backend")
	backends, err := svc.ListBackends(ctx)
	require.NoError(t, err)

	if env.ollamaAvailable {
		require.NotEmpty(t, backends, "should have at least one backend when Ollama is available")

		found := false
		for _, b := range backends {
			if b.Name == "ollama" {
				found = true
				assert.Equal(t, "ollama", b.Type)
				assert.False(t, b.Capabilities.PersistentSessions,
					"Ollama does not support persistent sessions")
				assert.False(t, b.Capabilities.ACP,
					"Ollama is not an ACP backend")
				break
			}
		}
		assert.True(t, found, "ollama backend should be listed")
	} else {
		// No backends available - verify empty list is valid.
		assert.NotNil(t, backends, "backends list should not be nil")
	}
}

// TestE2E_CrossBackend_ListBackends_RegisterDynamic verifies that a new
// backend can be dynamically registered and subsequently listed.
func TestE2E_CrossBackend_ListBackends_RegisterDynamic(t *testing.T) {
	requireOllama(t)

	svc := newAgentService(t)
	ctx := orchestration.ContextWithTenant(context.Background(), "test-tenant-cross-backend")

	// Register a secondary Ollama backend.
	backend := newOllamaBackend(t)
	err := svc.RegisterBackend(ctx, &orchestration.RegisterBackendRequest{
		Name:    "ollama-secondary",
		Backend: backend,
	})
	require.NoError(t, err)

	backends, err := svc.ListBackends(ctx)
	require.NoError(t, err)
	assert.Len(t, backends, 3, "should now have three backends (ollama, opencode, ollama-secondary)")

	names := make(map[string]bool)
	for _, b := range backends {
		names[b.Name] = true
	}
	assert.True(t, names["ollama"])
	assert.True(t, names["opencode"])
	assert.True(t, names["ollama-secondary"])
}

// TestE2E_CrossBackend_HealthCheck verifies the health check endpoint returns
// a valid report through the AgentService.
func TestE2E_CrossBackend_HealthCheck(t *testing.T) {
	svc := newAgentService(t)

	ctx := orchestration.ContextWithTenant(context.Background(), "test-tenant-cross-backend")
	report, err := svc.HealthCheck(ctx)
	require.NoError(t, err, "HealthCheck should succeed")
	require.NotNil(t, report, "report must not be nil")

	assert.NotZero(t, report.Timestamp, "timestamp must be set")
}

// TestE2E_CrossBackend_HealthCheck_REST verifies the health check endpoint
// via the REST API returns HTTP 200.
func TestE2E_CrossBackend_HealthCheck_REST(t *testing.T) {
	requireQuicBaaS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var healthResult map[string]interface{}
	resp, err := getJSON(ctx, env.quicbaasURL+"/api/v1/orchestrator/health", &healthResult)
	require.NoError(t, err, "health endpoint GET should succeed")
	if resp.Body != nil {
		defer resp.Body.Close()
	}

	assert.Equal(t, http.StatusOK, resp.StatusCode, "health endpoint should return 200")
	assert.NotNil(t, healthResult, "health response body should not be nil")
}

// TestE2E_CrossBackend_BackendNotFound verifies that prompting a nonexistent
// backend returns the appropriate typed error.
func TestE2E_CrossBackend_BackendNotFound(t *testing.T) {
	svc := newAgentService(t)

	ctx := orchestration.ContextWithTenant(context.Background(), "test-tenant-cross-backend")
	_, err := svc.PromptSession(ctx, &orchestration.PromptSessionRequest{
		Backend: "nonexistent-backend",
		Prompt:  "this should fail",
	})
	require.Error(t, err, "prompting nonexistent backend should fail")
	assert.ErrorIs(t, err, orchestration.ErrBackendNotFound)
}

// TestE2E_CrossBackend_SpawnUnsupported verifies that spawning a session on
// a stateless backend (Ollama) returns an unsupported operation error.
func TestE2E_CrossBackend_SpawnUnsupported(t *testing.T) {
	requireOllama(t)

	svc := newAgentService(t)

	ctx := orchestration.ContextWithTenant(context.Background(), "test-tenant-cross-backend")
	_, err := svc.SpawnSession(ctx, &orchestration.SpawnSessionRequest{
		Backend: "ollama",
	})
	require.Error(t, err, "spawn on stateless Ollama backend must fail")
	assert.ErrorIs(t, err, orchestration.ErrUnsupportedOperation)
}

// TestE2E_CrossBackend_ListBackends_Empty verifies that ListBackends returns
// an empty (non-nil) slice when no backends are registered.
func TestE2E_CrossBackend_ListBackends_Empty(t *testing.T) {
	store := orchestration.NewInMemoryAgentStore()
	backends := make(map[string]orchestration.AgentBackend)
	logger := &testLogger{t: t}

	svc, err := orchestration.NewAgentService(store, backends, logger)
	require.NoError(t, err, "failed to create agent service with no backends")

	wirer, ok := svc.(agentServiceWirer)
	require.True(t, ok, "AgentService must implement agentServiceWirer for test setup")

	taskStore := orchestration.NewInMemoryTaskStore()
	wirer.SetTaskStore(taskStore)

	guard := orchestration.NewTenantGuard(logger)
	wirer.SetTenantGuard(guard)

	ctx := orchestration.ContextWithTenant(context.Background(), "test-tenant-cross-backend")
	result, err := svc.ListBackends(ctx)
	require.NoError(t, err, "ListBackends should succeed with no backends")
	assert.NotNil(t, result, "backends list must not be nil")
	assert.Len(t, result, 0, "backends list must be empty")
}

// TestE2E_CrossBackend_RegisterBackend_NilBackend verifies that RegisterBackend
// with a nil backend returns ErrBackendNilRequired.
func TestE2E_CrossBackend_RegisterBackend_NilBackend(t *testing.T) {
	svc := newAgentService(t)

	ctx := orchestration.ContextWithTenant(context.Background(), "test-tenant-cross-backend")
	err := svc.RegisterBackend(ctx, &orchestration.RegisterBackendRequest{
		Name:    "nil-backend",
		Backend: nil,
	})
	require.Error(t, err, "registering nil backend must fail")
	assert.ErrorIs(t, err, orchestration.ErrBackendNilRequired)
}

// TestE2E_CrossBackend_RegisterBackend_EmptyName verifies that RegisterBackend
// with an empty name returns ErrBackendNameRequired.
func TestE2E_CrossBackend_RegisterBackend_EmptyName(t *testing.T) {
	svc := newAgentService(t)

	ctx := orchestration.ContextWithTenant(context.Background(), "test-tenant-cross-backend")
	err := svc.RegisterBackend(ctx, &orchestration.RegisterBackendRequest{
		Name:    "",
		Backend: nil,
	})
	require.Error(t, err, "registering backend with empty name must fail")
	assert.ErrorIs(t, err, orchestration.ErrBackendNameRequired)
}

// TestE2E_CrossBackend_PromptSession_EmptyPrompt_Error verifies that PromptSession
// with an empty prompt returns ErrPromptRequired.
func TestE2E_CrossBackend_PromptSession_EmptyPrompt_Error(t *testing.T) {
	svc := newAgentService(t)

	ctx := orchestration.ContextWithTenant(context.Background(), "test-tenant-cross-backend")
	_, err := svc.PromptSession(ctx, &orchestration.PromptSessionRequest{
		Backend: "any-backend",
		Prompt:  "",
	})
	require.Error(t, err, "empty prompt must return an error")
	assert.ErrorIs(t, err, orchestration.ErrPromptRequired)
}

// TestE2E_CrossBackend_PromptSession_BackendNotFound_Error verifies that
// PromptSession with a non-existent backend returns ErrBackendNotFound.
func TestE2E_CrossBackend_PromptSession_BackendNotFound_Error(t *testing.T) {
	svc := newAgentService(t)

	ctx := orchestration.ContextWithTenant(context.Background(), "test-tenant-cross-backend")
	_, err := svc.PromptSession(ctx, &orchestration.PromptSessionRequest{
		Backend: "nonexistent-backend",
		Prompt:  "this should fail",
	})
	require.Error(t, err, "prompting nonexistent backend must fail")
	assert.ErrorIs(t, err, orchestration.ErrBackendNotFound)
}

// TestE2E_CrossBackend_SpawnSession_BackendNotFound verifies that SpawnSession
// with a non-existent backend returns ErrBackendNotFound.
func TestE2E_CrossBackend_SpawnSession_BackendNotFound(t *testing.T) {
	svc := newAgentService(t)

	ctx := orchestration.ContextWithTenant(context.Background(), "test-tenant-cross-backend")
	_, err := svc.SpawnSession(ctx, &orchestration.SpawnSessionRequest{
		Backend: "nonexistent-backend",
	})
	require.Error(t, err, "spawning session on nonexistent backend must fail")
	assert.ErrorIs(t, err, orchestration.ErrBackendNotFound)
}

// TestE2E_CrossBackend_SpawnSession_EmptyBackend verifies that SpawnSession
// with an empty backend name returns ErrBackendRequired.
func TestE2E_CrossBackend_SpawnSession_EmptyBackend(t *testing.T) {
	svc := newAgentService(t)

	ctx := orchestration.ContextWithTenant(context.Background(), "test-tenant-cross-backend")
	_, err := svc.SpawnSession(ctx, &orchestration.SpawnSessionRequest{
		Backend: "",
	})
	require.Error(t, err, "spawning session with empty backend must fail")
	assert.ErrorIs(t, err, orchestration.ErrBackendRequired)
}

// TestE2E_CrossBackend_SessionStatus_NotFound verifies that SessionStatus
// with a non-existent session ID returns ErrSessionNotFound.
func TestE2E_CrossBackend_SessionStatus_NotFound(t *testing.T) {
	svc := newAgentService(t)

	ctx := orchestration.ContextWithTenant(context.Background(), "test-tenant-cross-backend")
	_, err := svc.SessionStatus(ctx, &orchestration.SessionStatusRequest{
		SessionID: "nonexistent-session-id",
	})
	require.Error(t, err, "querying nonexistent session must fail")
	assert.ErrorIs(t, err, orchestration.ErrSessionNotFound)
}

// TestE2E_CrossBackend_CloseSession_NotFound verifies that CloseSession
// with a non-existent session ID returns ErrSessionNotFound.
func TestE2E_CrossBackend_CloseSession_NotFound(t *testing.T) {
	svc := newAgentService(t)

	ctx := orchestration.ContextWithTenant(context.Background(), "test-tenant-cross-backend")
	_, err := svc.CloseSession(ctx, &orchestration.CloseSessionRequest{
		SessionID: "nonexistent-session-id",
	})
	require.Error(t, err, "closing nonexistent session must fail")
	assert.ErrorIs(t, err, orchestration.ErrSessionNotFound)
}

// TestE2E_CrossBackend_ListSessions_Empty verifies that ListSessions returns
// an empty list when no sessions exist.
func TestE2E_CrossBackend_ListSessions_Empty(t *testing.T) {
	svc := newAgentService(t)

	ctx := orchestration.ContextWithTenant(context.Background(), "test-tenant-cross-backend")
	resp, err := svc.ListSessions(ctx, &orchestration.ListSessionsRequest{})
	require.NoError(t, err, "ListSessions should succeed with no sessions")
	require.NotNil(t, resp, "response must not be nil")
	assert.NotNil(t, resp.Sessions, "sessions list must not be nil")
	assert.Len(t, resp.Sessions, 0, "sessions list must be empty")
}

// TestE2E_CrossBackend_RegisterProject verifies that RegisterProject with
// valid name and path succeeds, and the project appears in ListProjects.
func TestE2E_CrossBackend_RegisterProject(t *testing.T) {
	svc := newAgentService(t)

	ctx := orchestration.ContextWithTenant(context.Background(), "test-tenant-cross-backend")

	regResp, err := svc.RegisterProject(ctx, &orchestration.RegisterProjectRequest{
		Name: "test-project",
		Path: "/tmp/test-project",
	})
	require.NoError(t, err, "RegisterProject should succeed")
	require.NotNil(t, regResp, "response must not be nil")
	assert.NotEmpty(t, regResp.ProjectID, "project ID must be set")
	assert.NotZero(t, regResp.Timestamp, "timestamp must be set")

	listResp, err := svc.ListProjects(ctx, &orchestration.ListProjectsRequest{})
	require.NoError(t, err, "ListProjects should succeed")
	require.NotNil(t, listResp, "list response must not be nil")
	require.Len(t, listResp.Projects, 1, "should have exactly one project")
	assert.Equal(t, "test-project", listResp.Projects[0].Name)
	assert.Equal(t, "/tmp/test-project", listResp.Projects[0].Path)
}

// TestE2E_CrossBackend_RegisterProject_MissingName verifies that
// RegisterProject with an empty name returns ErrProjectNameRequired.
func TestE2E_CrossBackend_RegisterProject_MissingName(t *testing.T) {
	svc := newAgentService(t)

	ctx := orchestration.ContextWithTenant(context.Background(), "test-tenant-cross-backend")
	_, err := svc.RegisterProject(ctx, &orchestration.RegisterProjectRequest{
		Name: "",
		Path: "/tmp/test-project",
	})
	require.Error(t, err, "registering project with empty name must fail")
	assert.ErrorIs(t, err, orchestration.ErrProjectNameRequired)
}

// TestE2E_CrossBackend_RegisterProject_MissingPath verifies that
// RegisterProject with an empty path returns ErrProjectPathRequired.
func TestE2E_CrossBackend_RegisterProject_MissingPath(t *testing.T) {
	svc := newAgentService(t)

	ctx := orchestration.ContextWithTenant(context.Background(), "test-tenant-cross-backend")
	_, err := svc.RegisterProject(ctx, &orchestration.RegisterProjectRequest{
		Name: "test-project",
		Path: "",
	})
	require.Error(t, err, "registering project with empty path must fail")
	assert.ErrorIs(t, err, orchestration.ErrProjectPathRequired)
}

// TestE2E_CrossBackend_HealthCheck_InProcess verifies that HealthCheck returns
// a non-nil report with a Timestamp field set when called in-process.
func TestE2E_CrossBackend_HealthCheck_InProcess(t *testing.T) {
	svc := newAgentService(t)

	ctx := orchestration.ContextWithTenant(context.Background(), "test-tenant-cross-backend")
	report, err := svc.HealthCheck(ctx)
	require.NoError(t, err, "HealthCheck should succeed")
	require.NotNil(t, report, "report must not be nil")
	assert.NotZero(t, report.Timestamp, "timestamp must be set")
}
