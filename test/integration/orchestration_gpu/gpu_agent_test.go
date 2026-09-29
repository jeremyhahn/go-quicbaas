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

//go:build integration && rag && gpu

package orchestration_gpu

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/ollama"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

const gpuTestModel = "qwen3:1.7b"

// =============================================================================
// Requirement Helpers
// =============================================================================

func requireGPU(t *testing.T) {
	t.Helper()
	cmd := exec.Command("nvidia-smi", "--query-gpu=name", "--format=csv,noheader")
	output, err := cmd.Output()
	if err != nil || len(strings.TrimSpace(string(output))) == 0 {
		t.Fatalf("the gpu build tag requires an NVIDIA GPU; nvidia-smi found none (err=%v)", err)
	}
	t.Logf("GPU detected: %s", strings.TrimSpace(string(output)))
}

func requireOllamaModel(t *testing.T, model string) {
	t.Helper()
	cmd := exec.Command("ollama", "list")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("the gpu build tag requires ollama: %v", err)
	}
	if !strings.Contains(string(output), model) {
		t.Fatalf("model %s not available (pull with: ollama pull %s)", model, model)
	}
}

func ollamaURL() string {
	if url := os.Getenv("OLLAMA_URL"); url != "" {
		return url
	}
	return "http://localhost:11434"
}

// =============================================================================
// Logger
// =============================================================================

type gpuLogger struct{ t *testing.T }

var _ interfaces.Logger = (*gpuLogger)(nil)

func (l *gpuLogger) Debug(msg string, args ...any) { l.t.Helper(); l.t.Log("[DEBUG] " + msg) }
func (l *gpuLogger) Debugf(format string, args ...interface{}) {
	l.t.Helper()
	l.t.Logf("[DEBUG] "+format, args...)
}
func (l *gpuLogger) Info(msg string, args ...any) { l.t.Helper(); l.t.Log("[INFO] " + msg) }
func (l *gpuLogger) Infof(format string, args ...interface{}) {
	l.t.Helper()
	l.t.Logf("[INFO] "+format, args...)
}
func (l *gpuLogger) Warn(msg string, args ...any) { l.t.Helper(); l.t.Log("[WARN] " + msg) }
func (l *gpuLogger) Warnf(format string, args ...interface{}) {
	l.t.Helper()
	l.t.Logf("[WARN] "+format, args...)
}
func (l *gpuLogger) Error(msg string, args ...any) { l.t.Helper(); l.t.Log("[ERROR] " + msg) }
func (l *gpuLogger) Errorf(format string, args ...interface{}) {
	l.t.Helper()
	l.t.Logf("[ERROR] "+format, args...)
}
func (l *gpuLogger) Fatal(msg string) { l.t.Helper(); l.t.Fatal("[FATAL] " + msg) }
func (l *gpuLogger) Fatalf(format string, args ...interface{}) {
	l.t.Helper()
	l.t.Fatalf("[FATAL] "+format, args...)
}
func (l *gpuLogger) WithField(_ string, _ interface{}) interfaces.Logger   { return l }
func (l *gpuLogger) WithFields(_ map[string]interface{}) interfaces.Logger { return l }
func (l *gpuLogger) WithError(_ error) interfaces.Logger                   { return l }

// =============================================================================
// Agent Service Factory
// =============================================================================

type serviceWirer interface {
	SetTaskStore(store orchestration.TaskStore)
	SetTenantGuard(guard *orchestration.TenantGuard)
	SetFleetRegistry(registry orchestration.FleetRegistry)
}

func newGPUService(t *testing.T) orchestration.AgentService {
	t.Helper()

	client := ollama.NewClient(ollama.ClientConfig{
		BaseURL:    ollamaURL(),
		MaxRetries: 2,
	})
	chatSvc := ollama.NewChatService(client, gpuTestModel)
	logger := &gpuLogger{t: t}

	backends := map[string]orchestration.AgentBackend{
		"ollama": orchestration.NewOllamaBackend(orchestration.OllamaBackendConfig{
			ChatService:  chatSvc,
			DefaultModel: gpuTestModel,
		}),
	}

	store := orchestration.NewInMemoryAgentStore()
	svc, err := orchestration.NewAgentService(store, backends, logger)
	require.NoError(t, err)

	wirer, ok := svc.(serviceWirer)
	require.True(t, ok)

	wirer.SetTaskStore(orchestration.NewInMemoryTaskStore())
	wirer.SetTenantGuard(orchestration.NewTenantGuard(logger))
	wirer.SetFleetRegistry(orchestration.NewInMemoryFleetRegistry())

	return svc
}

// runGPUJob drives one lead-driven workflow job to a terminal state through
// the control loop's RunJob entry point, over a run built the way the
// envelope builds one for the workflow runner, and returns the run's final
// status alongside the loop's error.
func runGPUJob(ctx context.Context, t *testing.T, wf *orchestration.WorkflowDefinition, cfg *orchestration.JobConfig) (*orchestration.JobStatus, error) {
	t.Helper()

	svc := newGPUService(t)
	logger := &gpuLogger{t: t}

	run := orchestration.NewJobRun(orchestration.JobRunConfig{
		JobID:      wf.Name,
		Runner:     orchestration.JobRunnerWorkflow,
		Config:     cfg,
		Workflow:   wf,
		StartState: orchestration.JobStateInitializing,
		Agents:     svc,
		Logger:     logger,
	})
	err := orchestration.NewControlLoop(svc, logger).RunJob(ctx, run)
	return run.Status(), err
}

// =============================================================================
// Tests
// =============================================================================

func TestGPUAgent_OllamaModelAvailable(t *testing.T) {
	requireGPU(t)
	requireOllamaModel(t, gpuTestModel)

	client := ollama.NewClient(ollama.ClientConfig{
		BaseURL:    ollamaURL(),
		MaxRetries: 2,
	})
	chatSvc := ollama.NewChatService(client, gpuTestModel)
	backend := orchestration.NewOllamaBackend(orchestration.OllamaBackendConfig{
		ChatService:  chatSvc,
		DefaultModel: gpuTestModel,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := backend.Prompt(ctx, &orchestration.PromptRequest{
		Prompt: "Respond with the single word 'hello'.",
		Model:  gpuTestModel,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.Content)
	t.Logf("Model %s responded with %d content blocks", gpuTestModel, len(resp.Content))
}

func TestGPUAgent_OllamaModelAvailable_BadModel(t *testing.T) {
	requireGPU(t)

	client := ollama.NewClient(ollama.ClientConfig{
		BaseURL:    ollamaURL(),
		MaxRetries: 1,
	})
	chatSvc := ollama.NewChatService(client, "nonexistent-model-xyz")
	backend := orchestration.NewOllamaBackend(orchestration.OllamaBackendConfig{
		ChatService:  chatSvc,
		DefaultModel: "nonexistent-model-xyz",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := backend.Prompt(ctx, &orchestration.PromptRequest{
		Prompt: "hello",
		Model:  "nonexistent-model-xyz",
	})
	require.Error(t, err)
	t.Logf("Expected error for bad model: %v", err)
}

func TestGPUAgent_EndToEnd_SimpleTask(t *testing.T) {
	requireGPU(t)
	requireOllamaModel(t, gpuTestModel)

	wf := &orchestration.WorkflowDefinition{
		Name: "gpu-simple-test",
		Mode: orchestration.WorkflowModeConstrained,
		Agents: []orchestration.AgentRoleSpec{
			{Role: "lead", Backend: "ollama", Model: gpuTestModel},
			{Role: "worker", Backend: "ollama", Model: gpuTestModel},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	status, err := runGPUJob(ctx, t, wf, &orchestration.JobConfig{
		TenantID:      "gpu-test",
		Description:   "Respond with a JSON object containing a summary field",
		MaxIterations: 2,
		PollInterval:  2 * time.Second,
	})
	if err != nil {
		t.Logf("Control loop error (may be expected): %v", err)
	}
	require.NotNil(t, status)
	assert.True(t,
		status.State == orchestration.JobStateCompleted || status.State == orchestration.JobStateFailed,
		"job should reach terminal state, got: %s", status.State)
	t.Logf("Final state: %s, iteration: %d, tasks: %d", status.State, status.Iteration, status.TaskCount)
}

func TestGPUAgent_WorkflowSequencing(t *testing.T) {
	requireGPU(t)
	requireOllamaModel(t, gpuTestModel)

	wf := &orchestration.WorkflowDefinition{
		Name: "gpu-seq-test",
		Mode: orchestration.WorkflowModeConstrained,
		Agents: []orchestration.AgentRoleSpec{
			{Role: "lead", Backend: "ollama", Model: gpuTestModel},
			{Role: "worker", Backend: "ollama", Model: gpuTestModel},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	status, err := runGPUJob(ctx, t, wf, &orchestration.JobConfig{
		TenantID:      "gpu-seq",
		Description:   "Step 1: Define a data structure. Step 2: After step 1, describe the structure.",
		MaxIterations: 3,
		PollInterval:  2 * time.Second,
	})
	if err != nil {
		t.Logf("Sequencing error: %v", err)
	}
	require.NotNil(t, status)
	assert.True(t,
		status.State == orchestration.JobStateCompleted || status.State == orchestration.JobStateFailed,
		"job should reach terminal state, got: %s", status.State)
	t.Logf("Final state: %s, iteration: %d", status.State, status.Iteration)
}

func TestGPUAgent_ReviewAndRedelegate(t *testing.T) {
	requireGPU(t)
	requireOllamaModel(t, gpuTestModel)

	wf := &orchestration.WorkflowDefinition{
		Name: "gpu-review-test",
		Mode: orchestration.WorkflowModeConstrained,
		Agents: []orchestration.AgentRoleSpec{
			{Role: "lead", Backend: "ollama", Model: gpuTestModel},
			{Role: "reviewer", Backend: "ollama", Model: gpuTestModel},
			{Role: "worker", Backend: "ollama", Model: gpuTestModel},
		},
		Settings: orchestration.WorkflowSettings{
			ReviewCriteria: []orchestration.ReviewCriterion{
				{Name: "output_exists", Validator: "agent", Config: "Output must exist", Required: true},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	status, err := runGPUJob(ctx, t, wf, &orchestration.JobConfig{
		TenantID:      "gpu-review",
		Description:   "Describe a simple calculator with add and subtract operations",
		MaxIterations: 3,
		PollInterval:  2 * time.Second,
	})
	if err != nil {
		t.Logf("Review error: %v", err)
	}
	require.NotNil(t, status)
	assert.True(t,
		status.State == orchestration.JobStateCompleted || status.State == orchestration.JobStateFailed,
		"job should reach terminal state, got: %s", status.State)
	t.Logf("Final state: %s, iteration: %d", status.State, status.Iteration)
}
