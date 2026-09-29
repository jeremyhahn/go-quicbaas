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

package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Ollama Backend Prompt Tests
// =============================================================================

// TestIntegration_OllamaBackendPrompt verifies that a prompt sent through the
// agent service to the Ollama backend returns a non-empty text response. This
// exercises the full path: AgentService -> OllamaBackend -> Ollama API.
func TestIntegration_OllamaBackendPrompt(t *testing.T) {
	requireOllama(t)

	svc := newAgentService(t)

	ctx, cancel := context.WithTimeout(context.Background(), promptTimeout)
	defer cancel()

	resp, err := svc.PromptSession(ctx, &orchestration.PromptSessionRequest{
		Backend: "ollama",
		Prompt:  "Respond with exactly one word: hello",
		Model:   env.ollamaModel,
	})
	require.NoError(t, err, "prompt to Ollama backend should succeed")
	require.NotNil(t, resp, "response must not be nil")

	assert.NotEmpty(t, resp.Content, "response content must not be empty")
	assert.NotEmpty(t, resp.Content[0].Text, "first content block text must not be empty")
	assert.NotZero(t, resp.Timestamp, "response timestamp must be set")
}

// TestIntegration_OllamaBackendPrompt_EmptyPrompt verifies that prompting
// with an empty string returns a typed error.
func TestIntegration_OllamaBackendPrompt_EmptyPrompt(t *testing.T) {
	requireOllama(t)

	svc := newAgentService(t)

	ctx, cancel := context.WithTimeout(context.Background(), promptTimeout)
	defer cancel()

	_, err := svc.PromptSession(ctx, &orchestration.PromptSessionRequest{
		Backend: "ollama",
		Prompt:  "",
	})
	require.Error(t, err, "empty prompt must return an error")
	assert.ErrorIs(t, err, orchestration.ErrPromptRequired)
}

// TestIntegration_OllamaBackendPrompt_WithSystemPrompt verifies that a
// system prompt is forwarded to Ollama and influences the response.
func TestIntegration_OllamaBackendPrompt_WithSystemPrompt(t *testing.T) {
	requireOllama(t)

	svc := newAgentService(t)

	ctx, cancel := context.WithTimeout(context.Background(), promptTimeout)
	defer cancel()

	resp, err := svc.PromptSession(ctx, &orchestration.PromptSessionRequest{
		Backend:      "ollama",
		Prompt:       "What are you?",
		Model:        env.ollamaModel,
		SystemPrompt: "You are a helpful Go programming assistant. Always mention Go in your responses.",
	})
	require.NoError(t, err, "prompt with system prompt should succeed")
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.Content, "response content must not be empty")
}

// =============================================================================
// Project Discovery Tests
// =============================================================================

// TestIntegration_ProjectDiscovery verifies that DiscoverProjects finds
// directories containing CLAUDE.md and/or .claude/agents/ files.
func TestIntegration_ProjectDiscovery(t *testing.T) {
	svc := newAgentService(t)

	// Create a root directory with two test projects inside.
	root := t.TempDir()

	projA := filepath.Join(root, "project-alpha")
	require.NoError(t, os.MkdirAll(projA, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projA, "CLAUDE.md"), []byte("# Alpha\n"), 0o644))

	projB := filepath.Join(root, "project-beta")
	require.NoError(t, os.MkdirAll(projB, 0o755))
	agentsDir := filepath.Join(projB, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "backend.md"), []byte("# Backend\n"), 0o644))

	// An empty directory that should NOT be discovered.
	emptyDir := filepath.Join(root, "empty-project")
	require.NoError(t, os.MkdirAll(emptyDir, 0o755))

	ctx := context.Background()
	resp, err := svc.DiscoverProjects(ctx, &orchestration.DiscoverProjectsRequest{
		RootPath: root,
	})
	require.NoError(t, err, "discovery should succeed")
	require.NotNil(t, resp)

	// Expect exactly 2 discovered projects (alpha + beta), not the empty one.
	assert.GreaterOrEqual(t, len(resp.Projects), 2, "should discover at least 2 projects")

	projectNames := make(map[string]bool)
	for _, p := range resp.Projects {
		projectNames[p.Name] = true
	}
	assert.True(t, projectNames["project-alpha"], "should discover project-alpha")
	assert.True(t, projectNames["project-beta"], "should discover project-beta")
	assert.False(t, projectNames["empty-project"], "should not discover empty-project")
}

// TestIntegration_ProjectDiscovery_InvalidPath verifies that discovery with
// a nonexistent path returns a typed error.
func TestIntegration_ProjectDiscovery_InvalidPath(t *testing.T) {
	svc := newAgentService(t)

	ctx := context.Background()
	_, err := svc.DiscoverProjects(ctx, &orchestration.DiscoverProjectsRequest{
		RootPath: "/nonexistent/path/that/should/not/exist",
	})
	require.Error(t, err, "discovery with invalid path must return an error")
	assert.ErrorIs(t, err, orchestration.ErrRootPathRequired)
}

// TestIntegration_ProjectDiscovery_EmptyPath verifies that discovery with
// an empty root path returns a typed error.
func TestIntegration_ProjectDiscovery_EmptyPath(t *testing.T) {
	svc := newAgentService(t)

	ctx := context.Background()
	_, err := svc.DiscoverProjects(ctx, &orchestration.DiscoverProjectsRequest{
		RootPath: "",
	})
	require.Error(t, err, "discovery with empty path must return an error")
	assert.ErrorIs(t, err, orchestration.ErrRootPathRequired)
}

// TestIntegration_ProjectDiscovery_WithAgentFiles verifies that projects
// with .claude/agents/ files report agent metadata correctly.
func TestIntegration_ProjectDiscovery_WithAgentFiles(t *testing.T) {
	svc := newAgentService(t)

	root := t.TempDir()
	agentNames := []string{"golang-pro", "qa-engineer", "backend-architect"}
	createTestProjectWithFleetNodes(t, filepath.Join(root, "full-project"), agentNames)

	ctx := context.Background()
	resp, err := svc.DiscoverProjects(ctx, &orchestration.DiscoverProjectsRequest{
		RootPath: root,
	})
	require.NoError(t, err)

	var found *orchestration.DiscoveredProject
	for _, p := range resp.Projects {
		if p.Name == "full-project" {
			found = p
			break
		}
	}
	require.NotNil(t, found, "should discover full-project")
	assert.True(t, found.HasClaudeMD, "should detect CLAUDE.md")
	assert.True(t, found.HasAgents, "should detect agents directory")
	assert.Len(t, found.AgentFiles, len(agentNames), "should find all agent files")
}

// =============================================================================
// Project Registration Tests
// =============================================================================

// TestIntegration_ProjectRegistration verifies the full register-then-list
// lifecycle for projects.
func TestIntegration_ProjectRegistration(t *testing.T) {
	svc := newAgentService(t)
	ctx := context.Background()

	// Register two projects.
	regA, err := svc.RegisterProject(ctx, &orchestration.RegisterProjectRequest{
		Name:           "service-api",
		Path:           "/opt/projects/service-api",
		DefaultBackend: "ollama",
		DefaultModel:   env.ollamaModel,
		Labels:         map[string]string{"team": "backend", "env": "test"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, regA.ProjectID)

	regB, err := svc.RegisterProject(ctx, &orchestration.RegisterProjectRequest{
		Name:         "web-frontend",
		Path:         "/opt/projects/web-frontend",
		Dependencies: []string{regA.ProjectID},
	})
	require.NoError(t, err)
	require.NotEmpty(t, regB.ProjectID)

	// List and verify both projects exist.
	listResp, err := svc.ListProjects(ctx, &orchestration.ListProjectsRequest{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listResp.Projects), 2, "should list at least 2 registered projects")

	projectIDs := make(map[string]string) // name -> id
	for _, p := range listResp.Projects {
		projectIDs[p.Name] = p.ProjectID
	}
	assert.Equal(t, regA.ProjectID, projectIDs["service-api"])
	assert.Equal(t, regB.ProjectID, projectIDs["web-frontend"])
}

// TestIntegration_ProjectRegistration_MissingName verifies that registering
// a project without a name returns a typed error.
func TestIntegration_ProjectRegistration_MissingName(t *testing.T) {
	svc := newAgentService(t)

	_, err := svc.RegisterProject(context.Background(), &orchestration.RegisterProjectRequest{
		Name: "",
		Path: "/tmp/test",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, orchestration.ErrProjectNameRequired)
}

// TestIntegration_ProjectRegistration_MissingPath verifies that registering
// a project without a path returns a typed error.
func TestIntegration_ProjectRegistration_MissingPath(t *testing.T) {
	svc := newAgentService(t)

	_, err := svc.RegisterProject(context.Background(), &orchestration.RegisterProjectRequest{
		Name: "test-project",
		Path: "",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, orchestration.ErrProjectPathRequired)
}

// =============================================================================
// Task Decomposition Tests
// =============================================================================

// TestIntegration_TaskDecomposition verifies that DecomposeTask sends a task
// description to Ollama and returns at least one work item.
func TestIntegration_TaskDecomposition(t *testing.T) {
	requireOllama(t)

	svc := newAgentService(t)

	ctx, cancel := context.WithTimeout(context.Background(), promptTimeout)
	defer cancel()

	resp, err := svc.DecomposeTask(ctx, &orchestration.DecomposeTaskRequest{
		Backend: "ollama",
		Model:   env.ollamaModel,
		Task:    "Build a REST API with user authentication and a PostgreSQL database",
	})
	require.NoError(t, err, "task decomposition should succeed")
	require.NotNil(t, resp)

	// The response should contain at least one work item (either parsed JSON
	// or the fallback raw-text item).
	assert.NotEmpty(t, resp.WorkItems, "should return at least one work item")
	assert.NotZero(t, resp.Timestamp, "response timestamp must be set")

	for _, item := range resp.WorkItems {
		assert.NotEmpty(t, item.ID, "work item ID must not be empty")
		assert.NotEmpty(t, item.Title, "work item title must not be empty")
	}
}

// TestIntegration_TaskDecomposition_EmptyTask verifies that decomposing
// an empty task returns a typed error.
func TestIntegration_TaskDecomposition_EmptyTask(t *testing.T) {
	requireOllama(t)

	svc := newAgentService(t)

	ctx, cancel := context.WithTimeout(context.Background(), promptTimeout)
	defer cancel()

	_, err := svc.DecomposeTask(ctx, &orchestration.DecomposeTaskRequest{
		Backend: "ollama",
		Task:    "",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, orchestration.ErrTaskRequired)
}

// TestIntegration_TaskDecomposition_DefaultBackend verifies that decomposing
// without specifying a backend uses the default available backend.
func TestIntegration_TaskDecomposition_DefaultBackend(t *testing.T) {
	svc := newAgentService(t)

	resp, err := svc.DecomposeTask(context.Background(), &orchestration.DecomposeTaskRequest{
		Backend: "",
		Task:    "build a hello world app",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// TestIntegration_TaskDecomposition_NoBackendsAvailable verifies that decomposing
// with no backends registered returns a typed error.
func TestIntegration_TaskDecomposition_NoBackendsAvailable(t *testing.T) {
	store := orchestration.NewInMemoryAgentStore()
	svc, err := orchestration.NewAgentService(store, map[string]orchestration.AgentBackend{}, &testLogger{t: t})
	require.NoError(t, err)

	_, err = svc.DecomposeTask(context.Background(), &orchestration.DecomposeTaskRequest{
		Backend: "",
		Task:    "some task",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, orchestration.ErrBackendRequired)
}

// =============================================================================
// Backend Management Tests
// =============================================================================

// TestIntegration_ListBackends verifies that the Ollama backend is listed
// with the correct type and capabilities.
func TestIntegration_ListBackends(t *testing.T) {
	svc := newAgentService(t)

	backends, err := svc.ListBackends(context.Background())
	require.NoError(t, err)
	require.Len(t, backends, 1, "should have exactly one backend")

	b := backends[0]
	assert.Equal(t, "ollama", b.Name)
	assert.Equal(t, "ollama", b.Type)
	assert.False(t, b.Capabilities.PersistentSessions, "Ollama does not support persistent sessions")
	assert.False(t, b.Capabilities.ACP, "Ollama is not an ACP backend")
}

// TestIntegration_RegisterBackend verifies that a new backend can be
// dynamically registered and subsequently listed.
func TestIntegration_RegisterBackend(t *testing.T) {
	svc := newAgentService(t)
	ctx := context.Background()

	// Register a second Ollama backend with a different name.
	backend := newOllamaBackend(t)
	err := svc.RegisterBackend(ctx, &orchestration.RegisterBackendRequest{
		Name:    "ollama-secondary",
		Backend: backend,
	})
	require.NoError(t, err)

	backends, err := svc.ListBackends(ctx)
	require.NoError(t, err)
	assert.Len(t, backends, 2, "should now have two backends")

	names := make(map[string]bool)
	for _, b := range backends {
		names[b.Name] = true
	}
	assert.True(t, names["ollama"])
	assert.True(t, names["ollama-secondary"])
}

// TestIntegration_RegisterBackend_NilBackend verifies that registering a nil
// backend returns a typed error.
func TestIntegration_RegisterBackend_NilBackend(t *testing.T) {
	svc := newAgentService(t)

	err := svc.RegisterBackend(context.Background(), &orchestration.RegisterBackendRequest{
		Name:    "should-fail",
		Backend: nil,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, orchestration.ErrBackendNilRequired)
}

// =============================================================================
// Session Lifecycle Tests
// =============================================================================

// TestIntegration_SessionLifecycle_OllamaStateless verifies that Ollama
// correctly reports it does not support session operations. Spawn, Status,
// and Close should all return UnsupportedOperationError.
func TestIntegration_SessionLifecycle_OllamaStateless(t *testing.T) {
	svc := newAgentService(t)
	ctx := context.Background()

	// Spawn should fail because Ollama is stateless.
	_, err := svc.SpawnSession(ctx, &orchestration.SpawnSessionRequest{
		Backend: "ollama",
	})
	require.Error(t, err, "spawn on stateless Ollama backend must fail")
	assert.ErrorIs(t, err, orchestration.ErrUnsupportedOperation)
}

// TestIntegration_SessionLifecycle_SpawnMissingBackend verifies that
// spawning a session on a nonexistent backend returns a typed error.
func TestIntegration_SessionLifecycle_SpawnMissingBackend(t *testing.T) {
	svc := newAgentService(t)

	_, err := svc.SpawnSession(context.Background(), &orchestration.SpawnSessionRequest{
		Backend: "nonexistent-backend",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, orchestration.ErrBackendNotFound)
}

// TestIntegration_ListSessions_Empty verifies that listing sessions on a
// fresh service returns an empty list.
func TestIntegration_ListSessions_Empty(t *testing.T) {
	svc := newAgentService(t)

	resp, err := svc.ListSessions(context.Background(), &orchestration.ListSessionsRequest{})
	require.NoError(t, err)
	assert.Empty(t, resp.Sessions, "fresh service should have no sessions")
	assert.NotZero(t, resp.Timestamp)
}

// =============================================================================
// End-to-End Workflow Test
// =============================================================================

// TestIntegration_FullWorkflow exercises the complete agent orchestration
// pipeline: discover projects, register them, list backends, send a prompt,
// and decompose a task.
func TestIntegration_FullWorkflow(t *testing.T) {
	requireOllama(t)

	svc := newAgentService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*promptTimeout)
	defer cancel()

	// Step 1: Discover projects from a temp directory.
	root := t.TempDir()
	createTestProject(t, filepath.Join(root, "my-service"))
	createTestProjectWithFleetNodes(t, filepath.Join(root, "my-cli"), []string{"golang-pro"})

	discoverResp, err := svc.DiscoverProjects(ctx, &orchestration.DiscoverProjectsRequest{
		RootPath: root,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(discoverResp.Projects), 2)

	// Step 2: Register discovered projects.
	var projectIDs []string
	for _, discovered := range discoverResp.Projects {
		regResp, err := svc.RegisterProject(ctx, &orchestration.RegisterProjectRequest{
			Name:           discovered.Name,
			Path:           discovered.Path,
			DefaultBackend: "ollama",
			DefaultModel:   env.ollamaModel,
		})
		require.NoError(t, err)
		projectIDs = append(projectIDs, regResp.ProjectID)
	}

	// Step 3: List backends.
	backends, err := svc.ListBackends(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, backends)

	// Step 4: Send a prompt through the Ollama backend.
	promptResp, err := svc.PromptSession(ctx, &orchestration.PromptSessionRequest{
		Backend: "ollama",
		Prompt:  "Say 'integration test passed' in exactly those words.",
		Model:   env.ollamaModel,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, promptResp.Content)

	// Step 5: Decompose a task referencing the registered projects.
	decompResp, err := svc.DecomposeTask(ctx, &orchestration.DecomposeTaskRequest{
		Backend:    "ollama",
		Model:      env.ollamaModel,
		Task:       "Add a health check endpoint to the service and a CLI command to invoke it",
		ProjectIDs: projectIDs,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, decompResp.WorkItems, "decomposition should produce work items")

	// Step 6: Verify projects are listed.
	listResp, err := svc.ListProjects(ctx, &orchestration.ListProjectsRequest{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listResp.Projects), 2)
}
