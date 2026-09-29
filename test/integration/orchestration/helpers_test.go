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
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/ollama"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// requireQuicBaaS fails the test if the QuicBaaS service is not available.
func requireQuicBaaS(t *testing.T) {
	t.Helper()
	require.True(t, env.quicbaasAvailable,
		"QuicBaaS service must be available at %s", env.quicbaasURL)
}

// requireOllama fails the test if Ollama is not available.
func requireOllama(t *testing.T) {
	t.Helper()
	require.True(t, env.ollamaAvailable,
		"Ollama service must be available at %s", env.ollamaURL)
}

// postJSON sends an HTTP POST request with a JSON body and returns the response.
func postJSON(ctx context.Context, url string, body interface{}) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, &testHTTPError{URL: url, Method: "POST", Cause: err}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, &testHTTPError{URL: url, Method: "POST", Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")

	client := helpers.NewHTTPClient(30 * time.Second)
	return client.Do(req)
}

// getJSON sends an HTTP GET request and decodes the response body into result.
func getJSON(ctx context.Context, url string, result interface{}) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &testHTTPError{URL: url, Method: "GET", Cause: err}
	}
	req.Header.Set("Accept", "application/json")

	client := helpers.NewHTTPClient(30 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, &testHTTPError{URL: url, Method: "GET", Cause: err}
	}

	if result != nil {
		defer resp.Body.Close()
		bodyBytes, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return resp, &testHTTPError{URL: url, Method: "GET", Cause: readErr}
		}
		if decodeErr := json.Unmarshal(bodyBytes, result); decodeErr != nil {
			return resp, &testHTTPError{URL: url, Method: "GET", Cause: decodeErr}
		}
	}

	return resp, nil
}

// agentServiceWirer provides access to wiring methods on the concrete
// AgentService implementation. These methods are not part of the public
// AgentService interface but are needed to configure task stores, tenant
// guards, and agent registries during integration test setup.
type agentServiceWirer interface {
	SetTaskStore(store orchestration.TaskStore)
	SetTenantGuard(guard *orchestration.TenantGuard)
	SetFleetRegistry(registry orchestration.FleetRegistry)
	SetAgentDirectory(directory *orchestration.AgentDirectory)
}

// anyModel is a backend model catalog with no curated list, so the tenant
// agent catalog accepts whatever model a test's Agent names.
type anyModel struct{}

func (anyModel) ListBackendModels(context.Context, string) ([]string, error) { return nil, nil }

// ollamaLead defines, in tenantID's agent catalog, the lead Agent a job
// leads with on the test Ollama model, and returns the reference a
// StartJobRequest names it by.
func ollamaLead(t *testing.T, svc orchestration.AgentService, tenantID string) orchestration.AgentRef {
	t.Helper()
	accessor, ok := svc.(orchestration.AgentDirectoryAccessor)
	require.True(t, ok, "AgentService must expose its agent catalog")
	directory := accessor.AgentDirectory()
	require.NotNil(t, directory, "the agent catalog must be wired")
	agent := &orchestration.Agent{
		ID: "ollama-lead", Name: "Ollama lead", Roles: []orchestration.AgentRole{orchestration.AgentRoleLead},
		Backend: "ollama", Model: env.ollamaModel,
	}
	_, err := directory.CreateAgent(context.Background(), tenantID, agent)
	require.NoError(t, err, "define the ollama lead agent")
	return orchestration.AgentRefTo(agent.ID)
}

// newAgentService creates a fully wired AgentService with an in-memory store,
// an in-memory task store, a tenant guard, an agent registry, and registered backends.
func newAgentService(t *testing.T) orchestration.AgentService {
	t.Helper()

	store := orchestration.NewInMemoryAgentStore()
	backends := make(map[string]orchestration.AgentBackend)

	if env.ollamaAvailable {
		backends["ollama"] = newOllamaBackend(t)
	}

	// Register ACP backends for persistent session tests.
	backends["opencode"] = orchestration.NewACPBackend(orchestration.ACPBackendConfig{
		Command:      "opencode",
		Args:         []string{},
		DefaultModel: "anthropic:claude-sonnet-4-5-20250514",
	})

	logger := &testLogger{t: t}

	svc, err := orchestration.NewAgentService(store, backends, logger)
	require.NoError(t, err, "failed to create agent service")

	// Wire up task store, tenant guard, and agent registry via type assertion
	// to the concrete implementation's wiring methods.
	wirer, ok := svc.(agentServiceWirer)
	require.True(t, ok, "AgentService must implement agentServiceWirer for test setup")

	taskStore := orchestration.NewInMemoryTaskStore()
	wirer.SetTaskStore(taskStore)

	guard := orchestration.NewTenantGuard(logger)
	wirer.SetTenantGuard(guard)

	registry := orchestration.NewInMemoryFleetRegistry()
	wirer.SetFleetRegistry(registry)

	directory, err := orchestration.NewAgentDirectory(orchestration.AgentDirectoryConfig{
		Store:  orchestration.NewInMemoryAgentCatalogStore(),
		Models: anyModel{},
	})
	require.NoError(t, err, "failed to create the agent catalog")
	wirer.SetAgentDirectory(directory)

	return svc
}

// newOllamaBackend creates an Ollama backend configured to use the test
// environment's Ollama instance.
func newOllamaBackend(t *testing.T) *orchestration.OllamaBackend {
	t.Helper()

	client := ollama.NewClient(ollama.ClientConfig{
		BaseURL:    env.ollamaURL,
		MaxRetries: 2,
	})
	chatSvc := ollama.NewChatService(client, env.ollamaModel)

	return orchestration.NewOllamaBackend(orchestration.OllamaBackendConfig{
		ChatService:  chatSvc,
		DefaultModel: env.ollamaModel,
	})
}

// createTestTask creates a task via the AgentService and returns it.
func createTestTask(t *testing.T, svc orchestration.AgentService, tenantID, subject, teamName string) *orchestration.Task {
	t.Helper()

	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)
	task, err := svc.CreateTask(ctx, &orchestration.CreateTaskRequest{
		TenantID: tenantID,
		Subject:  subject,
		TeamName: teamName,
	})
	require.NoError(t, err, "failed to create test task")
	require.NotNil(t, task)
	require.NotEmpty(t, task.ID)

	return task
}

// claimTestTask claims a task via the AgentService.
func claimTestTask(t *testing.T, svc orchestration.AgentService, tenantID, taskID, agentID string) {
	t.Helper()

	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)
	err := svc.ClaimTask(ctx, tenantID, taskID, agentID)
	require.NoError(t, err, "failed to claim test task")
}

// completeTestTask completes a task via the AgentService.
func completeTestTask(t *testing.T, svc orchestration.AgentService, tenantID, taskID string) {
	t.Helper()

	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)
	err := svc.CompleteTask(ctx, tenantID, taskID, &orchestration.TaskResult{
		Output: "completed by integration test",
	})
	require.NoError(t, err, "failed to complete test task")
}

// testHTTPError represents an HTTP operation failure in the test infrastructure.
type testHTTPError struct {
	URL    string
	Method string
	Cause  error
}

// Error implements the error interface.
func (e *testHTTPError) Error() string {
	return "test http " + e.Method + " " + e.URL + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *testHTTPError) Unwrap() error {
	return e.Cause
}

// testLogger implements interfaces.Logger and forwards output to testing.T.
type testLogger struct {
	t *testing.T
}

// Compile-time interface satisfaction check.
var _ interfaces.Logger = (*testLogger)(nil)

func (l *testLogger) Debug(msg string, args ...any) { l.t.Helper(); l.t.Log("[DEBUG] " + msg) }
func (l *testLogger) Debugf(format string, args ...interface{}) {
	l.t.Helper()
	l.t.Logf("[DEBUG] "+format, args...)
}
func (l *testLogger) Info(msg string, args ...any) { l.t.Helper(); l.t.Log("[INFO] " + msg) }
func (l *testLogger) Infof(format string, args ...interface{}) {
	l.t.Helper()
	l.t.Logf("[INFO] "+format, args...)
}
func (l *testLogger) Warn(msg string, args ...any) { l.t.Helper(); l.t.Log("[WARN] " + msg) }
func (l *testLogger) Warnf(format string, args ...interface{}) {
	l.t.Helper()
	l.t.Logf("[WARN] "+format, args...)
}
func (l *testLogger) Error(msg string, args ...any) { l.t.Helper(); l.t.Log("[ERROR] " + msg) }
func (l *testLogger) Errorf(format string, args ...interface{}) {
	l.t.Helper()
	l.t.Logf("[ERROR] "+format, args...)
}
func (l *testLogger) Fatal(msg string) { l.t.Helper(); l.t.Fatal("[FATAL] " + msg) }
func (l *testLogger) Fatalf(format string, args ...interface{}) {
	l.t.Helper()
	l.t.Fatalf("[FATAL] "+format, args...)
}

func (l *testLogger) WithField(key string, value interface{}) interfaces.Logger  { return l }
func (l *testLogger) WithFields(fields map[string]interface{}) interfaces.Logger { return l }
func (l *testLogger) WithError(err error) interfaces.Logger                      { return l }
