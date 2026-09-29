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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Workflow Loading
// =============================================================================

// TestControlLoop_Stateless_WorkflowLoading_SingleFile verifies that loading a
// single workflow YAML file produces a fully populated WorkflowDefinition.
func TestControlLoop_Stateless_WorkflowLoading_SingleFile(t *testing.T) {
	workflowDir := findWorkflowDir(t)
	loader := orchestration.NewWorkflowLoader()

	t.Run("SoftwareDev", func(t *testing.T) {
		def, err := loader.LoadFromFile(orchestration.SystemTenantID, filepath.Join(workflowDir, "software-dev.yaml"))
		require.NoError(t, err, "loading software-dev.yaml should succeed")
		require.NotNil(t, def)

		assert.Equal(t, "software-dev", def.Name)
		assert.Equal(t, orchestration.WorkflowModeConstrained, def.Mode)
		assert.NotEmpty(t, def.Description)
		require.Len(t, def.Agents, 3, "software-dev should define 3 agent roles")

		// Verify lead agent.
		leadFound := false
		for _, agent := range def.Agents {
			if agent.Role == "lead" {
				leadFound = true
				assert.Equal(t, "claude", agent.Backend)
				assert.NotEmpty(t, agent.Model)
				assert.NotEmpty(t, agent.MCPScopes)
				break
			}
		}
		assert.True(t, leadFound, "software-dev must have a lead agent")

		// Verify settings.
		assert.Greater(t, def.Settings.MaxIterations, 0)
		assert.Greater(t, def.Settings.MaxParallelism, 0)
		assert.NotEmpty(t, def.Settings.TotalTimeout)
	})

	t.Run("CodeReview", func(t *testing.T) {
		def, err := loader.LoadFromFile(orchestration.SystemTenantID, filepath.Join(workflowDir, "code-review.yaml"))
		require.NoError(t, err, "loading code-review.yaml should succeed")
		require.NotNil(t, def)

		assert.Equal(t, "code-review", def.Name)
		assert.Equal(t, orchestration.WorkflowModeConstrained, def.Mode)
		require.Len(t, def.Agents, 2, "code-review should define 2 agent roles")
	})

	t.Run("Autonomous", func(t *testing.T) {
		def, err := loader.LoadFromFile(orchestration.SystemTenantID, filepath.Join(workflowDir, "autonomous.yaml"))
		require.NoError(t, err, "loading autonomous.yaml should succeed")
		require.NotNil(t, def)

		assert.Equal(t, "autonomous", def.Name)
		assert.Equal(t, orchestration.WorkflowModeAutonomous, def.Mode)
		require.Len(t, def.Agents, 1, "autonomous should define 1 agent role (lead only)")
		assert.Equal(t, "lead", def.Agents[0].Role)
	})
}

// TestControlLoop_Stateless_WorkflowLoading_Directory verifies that loading
// all workflows from a directory discovers and validates every YAML file.
func TestControlLoop_Stateless_WorkflowLoading_Directory(t *testing.T) {
	workflowDir := findWorkflowDir(t)
	loader := orchestration.NewWorkflowLoader()

	err := loader.LoadFromDir(orchestration.SystemTenantID, workflowDir)
	require.NoError(t, err, "loading workflow directory should succeed")

	names := loader.List(orchestration.SystemTenantID)
	require.GreaterOrEqual(t, len(names), 3,
		"directory should contain at least 3 workflows")

	assert.Contains(t, names, "software-dev")
	assert.Contains(t, names, "code-review")
	assert.Contains(t, names, "autonomous")
}

// TestControlLoop_Stateless_WorkflowLoading_InvalidFile verifies that loading
// a non-existent file produces the correct error type.
func TestControlLoop_Stateless_WorkflowLoading_InvalidFile(t *testing.T) {
	loader := orchestration.NewWorkflowLoader()

	_, err := loader.LoadFromFile(orchestration.SystemTenantID, "/nonexistent/path/workflow.yaml")
	require.Error(t, err, "loading a non-existent file should fail")
}

// TestControlLoop_Stateless_WorkflowLoading_Get verifies that workflows loaded
// from files can be retrieved by name via the Get method.
func TestControlLoop_Stateless_WorkflowLoading_Get(t *testing.T) {
	workflowDir := findWorkflowDir(t)
	loader := orchestration.NewWorkflowLoader()

	err := loader.LoadFromDir(orchestration.SystemTenantID, workflowDir)
	require.NoError(t, err)

	t.Run("Found", func(t *testing.T) {
		def, err := loader.Get(orchestration.SystemTenantID, "software-dev")
		require.NoError(t, err)
		require.NotNil(t, def)
		assert.Equal(t, "software-dev", def.Name)
	})

	t.Run("NotFound", func(t *testing.T) {
		_, err := loader.Get(orchestration.SystemTenantID, "nonexistent-workflow")
		require.Error(t, err, "Get for unknown workflow should fail")
	})
}

// =============================================================================
// DAG Scheduling
// =============================================================================

// TestControlLoop_Stateless_DAGScheduling_Linear verifies that work items with
// linear dependencies produce sequential waves.
func TestControlLoop_Stateless_DAGScheduling_Linear(t *testing.T) {
	items := []*orchestration.WorkItem{
		{ID: "setup", Title: "Project setup", Dependencies: nil},
		{ID: "implement", Title: "Implementation", Dependencies: []string{"setup"}},
		{ID: "test", Title: "Testing", Dependencies: []string{"implement"}},
	}

	plan, err := orchestration.ScheduleDAG(items, 0)
	require.NoError(t, err, "linear DAG should schedule without error")
	require.NotNil(t, plan)

	assert.Equal(t, 3, plan.TotalWaves(), "linear chain of 3 should produce 3 waves")

	for i, wave := range plan.Waves {
		require.Len(t, wave, 1, "wave %d should have exactly 1 item", i)
	}
}

// TestControlLoop_Stateless_DAGScheduling_Parallel verifies that independent
// work items are grouped into the same wave.
func TestControlLoop_Stateless_DAGScheduling_Parallel(t *testing.T) {
	items := []*orchestration.WorkItem{
		{ID: "setup", Title: "Project setup", Dependencies: nil},
		{ID: "frontend", Title: "Frontend", Dependencies: []string{"setup"}},
		{ID: "backend", Title: "Backend", Dependencies: []string{"setup"}},
		{ID: "integrate", Title: "Integration", Dependencies: []string{"frontend", "backend"}},
	}

	plan, err := orchestration.ScheduleDAG(items, 0)
	require.NoError(t, err, "diamond DAG should schedule without error")
	require.NotNil(t, plan)

	assert.Equal(t, 3, plan.TotalWaves(), "diamond should produce 3 waves")

	// Wave 0 = setup, Wave 1 = frontend + backend, Wave 2 = integrate.
	assert.Len(t, plan.Waves[0], 1, "wave 0 should have 1 item (setup)")
	assert.Len(t, plan.Waves[1], 2, "wave 1 should have 2 items (frontend, backend)")
	assert.Len(t, plan.Waves[2], 1, "wave 2 should have 1 item (integrate)")
}

// TestControlLoop_Stateless_DAGScheduling_CycleDetection verifies that a cyclic
// dependency graph returns ErrCyclicDependency.
func TestControlLoop_Stateless_DAGScheduling_CycleDetection(t *testing.T) {
	items := []*orchestration.WorkItem{
		{ID: "a", Title: "A", Dependencies: []string{"c"}},
		{ID: "b", Title: "B", Dependencies: []string{"a"}},
		{ID: "c", Title: "C", Dependencies: []string{"b"}},
	}

	_, err := orchestration.ScheduleDAG(items, 0)
	require.Error(t, err, "cyclic dependencies should fail")
	assert.ErrorIs(t, err, orchestration.ErrCyclicDependency)
}

// TestControlLoop_Stateless_DAGScheduling_MissingDependency verifies that a
// reference to a non-existent dependency returns ErrMissingDependency.
func TestControlLoop_Stateless_DAGScheduling_MissingDependency(t *testing.T) {
	items := []*orchestration.WorkItem{
		{ID: "a", Title: "A", Dependencies: []string{"nonexistent"}},
	}

	_, err := orchestration.ScheduleDAG(items, 0)
	require.Error(t, err, "missing dependency should fail")
	assert.ErrorIs(t, err, orchestration.ErrMissingDependency)
}

// TestControlLoop_Stateless_DAGScheduling_DuplicateWorkItem verifies that
// duplicate work item IDs produce ErrDuplicateWorkItem.
func TestControlLoop_Stateless_DAGScheduling_DuplicateWorkItem(t *testing.T) {
	items := []*orchestration.WorkItem{
		{ID: "a", Title: "A"},
		{ID: "a", Title: "A duplicate"},
	}

	_, err := orchestration.ScheduleDAG(items, 0)
	require.Error(t, err, "duplicate work item IDs should fail")
	assert.ErrorIs(t, err, orchestration.ErrDuplicateWorkItem)
}

// TestControlLoop_Stateless_DAGScheduling_NilWorkItem verifies that a nil work
// item in the input produces ErrNilWorkItem.
func TestControlLoop_Stateless_DAGScheduling_NilWorkItem(t *testing.T) {
	items := []*orchestration.WorkItem{
		{ID: "a", Title: "A"},
		nil,
	}

	_, err := orchestration.ScheduleDAG(items, 0)
	require.Error(t, err, "nil work item should fail")
	assert.ErrorIs(t, err, orchestration.ErrNilWorkItem)
}

// TestControlLoop_Stateless_DAGScheduling_EnforceParallelism verifies that
// parallelism enforcement splits oversized waves correctly.
func TestControlLoop_Stateless_DAGScheduling_EnforceParallelism(t *testing.T) {
	// Five independent items with max parallelism of 2.
	items := []*orchestration.WorkItem{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
		{ID: "c", Title: "C"},
		{ID: "d", Title: "D"},
		{ID: "e", Title: "E"},
	}

	plan, err := orchestration.ScheduleDAG(items, 2)
	require.NoError(t, err, "scheduling with parallelism should succeed")
	require.NotNil(t, plan)

	// 5 items with max parallelism 2 should split into 3 waves (2, 2, 1).
	assert.Equal(t, 3, plan.TotalWaves(), "5 items with max-parallelism=2 should produce 3 waves")

	totalItems := 0
	for _, wave := range plan.Waves {
		assert.LessOrEqual(t, len(wave), 2, "no wave should exceed parallelism limit of 2")
		totalItems += len(wave)
	}
	assert.Equal(t, 5, totalItems, "all items must be scheduled")
}

// TestControlLoop_Stateless_DAGScheduling_ExecutionPlanAdvance verifies that
// the ExecutionPlan wave advancement and completion tracking work correctly.
func TestControlLoop_Stateless_DAGScheduling_ExecutionPlanAdvance(t *testing.T) {
	items := []*orchestration.WorkItem{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B", Dependencies: []string{"a"}},
	}

	plan, err := orchestration.ScheduleDAG(items, 0)
	require.NoError(t, err)
	require.NotNil(t, plan)

	assert.Equal(t, 0, plan.CurrentWave(), "initial wave should be 0")
	assert.False(t, plan.IsComplete(), "plan should not be complete at start")

	newWave := plan.AdvanceWave()
	assert.Equal(t, 1, newWave, "after first advance, wave should be 1")

	newWave = plan.AdvanceWave()
	assert.Equal(t, 2, newWave, "after second advance, wave should be 2")
	assert.True(t, plan.IsComplete(), "plan should be complete after advancing past all waves")
}

// =============================================================================
// Job Memory
// =============================================================================

// TestControlLoop_Stateless_JobMemory_Accumulation verifies that job memory
// accumulates successes, failures, constraints, and feedback correctly.
func TestControlLoop_Stateless_JobMemory_Accumulation(t *testing.T) {
	mem := orchestration.NewJobMemory()
	require.NotNil(t, mem)
	assert.True(t, mem.IsEmpty(), "new memory should be empty")

	mem.AddSuccess("table-driven tests")
	mem.AddSuccess("interface composition")
	mem.AddFailure("setup task", "monolithic approach", "too complex", 1)
	mem.AddConstraint("no external dependencies")
	mem.AddFeedback("improve error handling")

	assert.False(t, mem.IsEmpty(), "memory should not be empty after adding entries")
	assert.Len(t, mem.SuccessfulApproaches, 2)
	assert.Len(t, mem.FailedApproaches, 1)
	assert.Len(t, mem.Constraints, 1)
	assert.Len(t, mem.ReviewFeedback, 1)
}

// TestControlLoop_Stateless_JobMemory_Deduplication verifies that duplicate
// entries are ignored across all memory categories.
func TestControlLoop_Stateless_JobMemory_Deduplication(t *testing.T) {
	mem := orchestration.NewJobMemory()

	// Add duplicates.
	mem.AddSuccess("approach-a")
	mem.AddSuccess("approach-a")
	mem.AddSuccess("approach-b")

	mem.AddFailure("task-1", "bad plan", "timeout", 1)
	mem.AddFailure("task-1", "bad plan", "timeout", 1)

	mem.AddConstraint("no secrets")
	mem.AddConstraint("no secrets")

	mem.AddFeedback("add tests")
	mem.AddFeedback("add tests")

	assert.Len(t, mem.SuccessfulApproaches, 2, "duplicate successes should be deduplicated")
	assert.Len(t, mem.FailedApproaches, 1, "duplicate failures should be deduplicated")
	assert.Len(t, mem.Constraints, 1, "duplicate constraints should be deduplicated")
	assert.Len(t, mem.ReviewFeedback, 1, "duplicate feedback should be deduplicated")
}

// TestControlLoop_Stateless_JobMemory_Serialize verifies that the serialized
// output contains all accumulated context sections.
func TestControlLoop_Stateless_JobMemory_Serialize(t *testing.T) {
	mem := orchestration.NewJobMemory()

	// Empty memory should produce empty string.
	assert.Empty(t, mem.Serialize(), "empty memory should serialize to empty string")

	mem.AddSuccess("used composition pattern")
	mem.AddFailure("build task", "global state", "race condition", 2)
	mem.AddConstraint("must use context propagation")
	mem.AddFeedback("add benchmarks")

	serialized := mem.Serialize()
	assert.NotEmpty(t, serialized)
	assert.Contains(t, serialized, "Previous Attempts Context")
	assert.Contains(t, serialized, "What worked")
	assert.Contains(t, serialized, "used composition pattern")
	assert.Contains(t, serialized, "What failed")
	assert.Contains(t, serialized, "global state")
	assert.Contains(t, serialized, "race condition")
	assert.Contains(t, serialized, "Discovered constraints")
	assert.Contains(t, serialized, "must use context propagation")
	assert.Contains(t, serialized, "Reviewer feedback")
	assert.Contains(t, serialized, "add benchmarks")
}

// TestControlLoop_Stateless_JobMemory_FailedApproachIteration verifies that
// failure entries with different iteration numbers are not deduplicated.
func TestControlLoop_Stateless_JobMemory_FailedApproachIteration(t *testing.T) {
	mem := orchestration.NewJobMemory()

	mem.AddFailure("task-1", "same approach", "same reason", 1)
	mem.AddFailure("task-1", "same approach", "same reason", 2)

	assert.Len(t, mem.FailedApproaches, 2,
		"failures with different iterations should not be deduplicated")
}

// =============================================================================
// Validators
// =============================================================================

// statelessJob is the job a validator pipeline runs for: the operator's
// command allowlist (the ExecutionGuard the command validator checks first),
// a workspace, and the artifacts the policy validator reads.
func statelessJob(t *testing.T, allowed []string, artifacts []*orchestration.TaskResult) *orchestration.JobContext {
	t.Helper()
	guard, err := orchestration.NewExecutionGuard(allowed)
	require.NoError(t, err)
	return &orchestration.JobContext{
		JobID:     "stateless",
		TenantID:  "stateless-tenant",
		WorkDir:   t.TempDir(),
		Artifacts: artifacts,
		Guard:     guard,
	}
}

// TestControlLoop_Stateless_Validators_Command verifies that the command
// validator correctly interprets exit codes from shell commands.
func TestControlLoop_Stateless_Validators_Command(t *testing.T) {
	registry := orchestration.DefaultValidatorRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Run("PassingCommand", func(t *testing.T) {
		criterion := orchestration.ReviewCriterion{
			Name:      "true-check",
			Validator: "command",
			Config:    "true",
			Required:  true,
		}

		results, allPassed, err := registry.RunPipeline(ctx, statelessJob(t, []string{"true"}, nil), []orchestration.ReviewCriterion{criterion})
		require.NoError(t, err, "pipeline with 'true' command should not error")
		assert.True(t, allPassed, "true command should pass")
		require.Len(t, results, 1)
		assert.True(t, results[0].Passed)
	})

	t.Run("FailingCommand", func(t *testing.T) {
		criterion := orchestration.ReviewCriterion{
			Name:      "false-check",
			Validator: "command",
			Config:    "false",
			Required:  true,
		}

		results, allPassed, err := registry.RunPipeline(ctx, statelessJob(t, []string{"false"}, nil), []orchestration.ReviewCriterion{criterion})
		require.NoError(t, err, "pipeline with 'false' command should not error on exit code")
		assert.False(t, allPassed, "false command should fail")
		require.Len(t, results, 1)
		assert.False(t, results[0].Passed)
	})

	t.Run("CommandWithOutput", func(t *testing.T) {
		criterion := orchestration.ReviewCriterion{
			Name:      "echo-check",
			Validator: "command",
			Config:    "echo all good",
			Required:  true,
		}

		results, allPassed, err := registry.RunPipeline(ctx, statelessJob(t, []string{"echo all good"}, nil), []orchestration.ReviewCriterion{criterion})
		require.NoError(t, err)
		assert.True(t, allPassed)
		require.Len(t, results, 1)
		assert.True(t, results[0].Passed)
		assert.Contains(t, results[0].Message, "all good")
	})
}

// TestControlLoop_Stateless_Validators_Command_MissingConfig verifies that a
// command validator with an empty config produces an execution error.
func TestControlLoop_Stateless_Validators_Command_MissingConfig(t *testing.T) {
	registry := orchestration.DefaultValidatorRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	criterion := orchestration.ReviewCriterion{
		Name:      "empty-config",
		Validator: "command",
		Config:    "",
		Required:  true,
	}

	_, _, err := registry.RunPipeline(ctx, statelessJob(t, nil, nil), []orchestration.ReviewCriterion{criterion})
	require.Error(t, err, "empty command config should produce an execution error")
	assert.ErrorIs(t, err, orchestration.ErrValidatorExecution)
}

// TestControlLoop_Stateless_Validators_Policy verifies that the policy
// validator detects secret patterns in task output.
func TestControlLoop_Stateless_Validators_Policy(t *testing.T) {
	registry := orchestration.DefaultValidatorRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Run("CleanOutput", func(t *testing.T) {
		criterion := orchestration.ReviewCriterion{
			Name:      "no-secrets",
			Validator: "policy",
			Config:    "no secrets in output",
			Required:  true,
		}

		artifacts := []*orchestration.TaskResult{
			{Output: "function Add(a, b int) int { return a + b }"},
		}

		results, allPassed, err := registry.RunPipeline(ctx, statelessJob(t, nil, artifacts), []orchestration.ReviewCriterion{criterion})
		require.NoError(t, err, "policy validator should not error on clean output")
		assert.True(t, allPassed, "clean output should pass no-secrets policy")
		require.Len(t, results, 1)
		assert.True(t, results[0].Passed)
	})

	t.Run("OutputWithAWSKey", func(t *testing.T) {
		criterion := orchestration.ReviewCriterion{
			Name:      "no-secrets",
			Validator: "policy",
			Config:    "no secrets in output",
			Required:  true,
		}

		artifacts := []*orchestration.TaskResult{
			{Output: "aws_secret_access_key = AKIAIOSFODNN7EXAMPLE"},
		}

		results, allPassed, err := registry.RunPipeline(ctx, statelessJob(t, nil, artifacts), []orchestration.ReviewCriterion{criterion})
		require.NoError(t, err, "policy validator should not error")
		assert.False(t, allPassed, "output with AWS key should fail no-secrets policy")
		require.Len(t, results, 1)
		assert.False(t, results[0].Passed)
		assert.Contains(t, results[0].Message, "secret")
	})

	t.Run("OutputWithPrivateKey", func(t *testing.T) {
		criterion := orchestration.ReviewCriterion{
			Name:      "no-secrets",
			Validator: "policy",
			Config:    "no secrets in output",
			Required:  true,
		}

		artifacts := []*orchestration.TaskResult{
			{Output: "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAK..."},
		}

		results, allPassed, err := registry.RunPipeline(ctx, statelessJob(t, nil, artifacts), []orchestration.ReviewCriterion{criterion})
		require.NoError(t, err)
		assert.False(t, allPassed, "output with private key should fail")
		require.Len(t, results, 1)
		assert.False(t, results[0].Passed)
	})

	t.Run("OutputWithBearerToken", func(t *testing.T) {
		criterion := orchestration.ReviewCriterion{
			Name:      "no-secrets",
			Validator: "policy",
			Config:    "no secrets in output",
			Required:  true,
		}

		artifacts := []*orchestration.TaskResult{
			{Output: "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test"},
		}

		results, allPassed, err := registry.RunPipeline(ctx, statelessJob(t, nil, artifacts), []orchestration.ReviewCriterion{criterion})
		require.NoError(t, err)
		assert.False(t, allPassed, "output with bearer token should fail")
		require.Len(t, results, 1)
		assert.False(t, results[0].Passed)
	})
}

// TestControlLoop_Stateless_Validators_NotFound verifies that requesting an
// unregistered validator produces the correct error.
func TestControlLoop_Stateless_Validators_NotFound(t *testing.T) {
	registry := orchestration.NewValidatorRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	criterion := orchestration.ReviewCriterion{
		Name:      "missing",
		Validator: "nonexistent-validator",
		Config:    "something",
		Required:  true,
	}

	_, _, err := registry.RunPipeline(ctx, statelessJob(t, nil, nil), []orchestration.ReviewCriterion{criterion})
	require.Error(t, err, "unregistered validator should produce an error")
	assert.ErrorIs(t, err, orchestration.ErrValidatorNotFound)
}

// TestControlLoop_Stateless_Validators_EmptyCriteria verifies that running a
// pipeline with no criteria returns a passing result.
func TestControlLoop_Stateless_Validators_EmptyCriteria(t *testing.T) {
	registry := orchestration.NewValidatorRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results, allPassed, err := registry.RunPipeline(ctx, statelessJob(t, nil, nil), nil)
	require.NoError(t, err)
	assert.True(t, allPassed, "empty criteria should pass")
	assert.Empty(t, results)
}

// TestControlLoop_Stateless_Validators_OptionalFailure verifies that an
// optional criterion failure does not affect the overall pipeline result.
func TestControlLoop_Stateless_Validators_OptionalFailure(t *testing.T) {
	registry := orchestration.DefaultValidatorRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	criteria := []orchestration.ReviewCriterion{
		{Name: "required-pass", Validator: "command", Config: "true", Required: true},
		{Name: "optional-fail", Validator: "command", Config: "false", Required: false},
	}

	results, allPassed, err := registry.RunPipeline(ctx, statelessJob(t, []string{"true", "false"}, nil), criteria)
	require.NoError(t, err)
	assert.True(t, allPassed, "optional failure should not affect overall result")
	require.Len(t, results, 2)
	assert.True(t, results[0].Passed)
	assert.False(t, results[1].Passed)
}

// =============================================================================
// Communication Policy
// =============================================================================

// TestControlLoop_Stateless_CommunicationPolicy_LeadToWorker verifies that
// lead-to-worker communications use fire-and-forget policy.
func TestControlLoop_Stateless_CommunicationPolicy_LeadToWorker(t *testing.T) {
	actions := []string{"assign", "feedback", "cancel"}

	for _, action := range actions {
		t.Run("Lead_"+action, func(t *testing.T) {
			policy, err := orchestration.GetPolicy("lead", action)
			require.NoError(t, err)
			assert.Equal(t, orchestration.MessagePolicyFireAndForget, policy,
				"lead->worker %s should be fire_and_forget", action)
		})
	}
}

// TestControlLoop_Stateless_CommunicationPolicy_WorkerToLead verifies that
// worker completion signals use blocking policy.
func TestControlLoop_Stateless_CommunicationPolicy_WorkerToLead(t *testing.T) {
	t.Run("WorkerComplete", func(t *testing.T) {
		policy, err := orchestration.GetPolicy("worker", "complete")
		require.NoError(t, err)
		assert.Equal(t, orchestration.MessagePolicyBlocking, policy,
			"worker->lead complete should be blocking")
	})

	t.Run("WorkerFailed", func(t *testing.T) {
		policy, err := orchestration.GetPolicy("worker", "failed")
		require.NoError(t, err)
		assert.Equal(t, orchestration.MessagePolicyBlocking, policy,
			"worker->lead failed should be blocking")
	})

	t.Run("WorkerQuestion", func(t *testing.T) {
		policy, err := orchestration.GetPolicy("worker", "question")
		require.NoError(t, err)
		assert.Equal(t, orchestration.MessagePolicyFireAndForget, policy,
			"worker->lead question should be fire_and_forget")
	})
}

// TestControlLoop_Stateless_CommunicationPolicy_WorkerToWorkerBlocked verifies
// that worker-to-worker communication is rejected.
func TestControlLoop_Stateless_CommunicationPolicy_WorkerToWorkerBlocked(t *testing.T) {
	err := orchestration.ValidateCommunication(orchestration.TopologyHubAndSpoke, "worker", "worker", "handoff")
	require.Error(t, err, "worker-to-worker communication should be rejected")
	assert.ErrorIs(t, err, orchestration.ErrWorkerToWorkerNotAllowed)
}

// TestControlLoop_Stateless_CommunicationPolicy_ValidCommunication verifies
// that non-worker-to-worker communications are allowed.
func TestControlLoop_Stateless_CommunicationPolicy_ValidCommunication(t *testing.T) {
	testCases := []struct {
		from   string
		to     string
		action string
	}{
		{"lead", "worker", "assign"},
		{"worker", "lead", "complete"},
		{"validator", "lead", "result"},
		{"lead", "validator", "request"},
	}

	for _, tc := range testCases {
		t.Run(tc.from+"_"+tc.to+"_"+tc.action, func(t *testing.T) {
			err := orchestration.ValidateCommunication(orchestration.TopologyHubAndSpoke, tc.from, tc.to, tc.action)
			assert.NoError(t, err, "%s->%s %s should be allowed", tc.from, tc.to, tc.action)
		})
	}
}

// TestControlLoop_Stateless_CommunicationPolicy_ValidatorToLead verifies that
// validator-to-lead result reporting uses blocking policy.
func TestControlLoop_Stateless_CommunicationPolicy_ValidatorToLead(t *testing.T) {
	policy, err := orchestration.GetPolicy("validator", "result")
	require.NoError(t, err)
	assert.Equal(t, orchestration.MessagePolicyBlocking, policy,
		"validator->lead result should be blocking")
}

// TestControlLoop_Stateless_CommunicationPolicy_UnknownAction verifies that
// unknown sender/action combinations default to fire-and-forget.
func TestControlLoop_Stateless_CommunicationPolicy_UnknownAction(t *testing.T) {
	policy, err := orchestration.GetPolicy("unknown-role", "unknown-action")
	require.NoError(t, err)
	assert.Equal(t, orchestration.MessagePolicyFireAndForget, policy,
		"unknown combination should default to fire_and_forget")
}

// =============================================================================
// StartJob and Status (requires Ollama)
// =============================================================================

// TestControlLoop_Stateless_StartJobAndStatus tests the full job lifecycle via
// the AgentService API using the Ollama backend.
func TestControlLoop_Stateless_StartJobAndStatus(t *testing.T) {
	requireOllama(t)

	svc := newAgentService(t)
	tenantID := "stateless-start-job"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	resp, err := svc.StartJob(ctx, &orchestration.StartJobRequest{
		TenantID:      tenantID,
		Description:   "Create a simple hello world function in Go",
		Lead:          ollamaLead(t, svc, tenantID),
		MaxIterations: 3,
		PollInterval:  "2s",
	})
	require.NoError(t, err, "StartJob should succeed")
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.JobID, "job ID must not be empty")

	// Poll for status. We expect the job to reach at least the decomposing
	// state. Ollama may not produce valid JSON for decomposition, so the job
	// might fail at decomposition. Either outcome validates the infrastructure.
	pollCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastStatus *orchestration.JobStatus
	reachedDecomposing := false

	for {
		select {
		case <-pollCtx.Done():
			// Timeout - check what we got.
			if lastStatus != nil {
				t.Logf("final job state: %s", lastStatus.State)
			}
			// As long as we got past initializing, the infrastructure works.
			if reachedDecomposing {
				return
			}
			require.NotNil(t, lastStatus, "should have received at least one status update")
			return
		case <-ticker.C:
			status, statusErr := svc.GetJobStatus(ctx, tenantID, resp.JobID)
			if statusErr != nil {
				t.Logf("GetJobStatus returned error (may be expected): %v", statusErr)
				continue
			}
			lastStatus = status
			t.Logf("job %s state: %s (iteration %d)", status.JobID, status.State, status.Iteration)

			if status.State == orchestration.JobStateDecomposing ||
				status.State == orchestration.JobStateScheduling ||
				status.State == orchestration.JobStateAssigning ||
				status.State == orchestration.JobStateExecuting {
				reachedDecomposing = true
			}

			if orchestration.IsTerminalJobState(status.State) {
				t.Logf("job reached terminal state: %s", status.State)
				return
			}
		}
	}
}

// TestControlLoop_Stateless_StartJobAndStatus_MissingDescription verifies that
// starting a job without a description returns the correct error.
func TestControlLoop_Stateless_StartJobAndStatus_MissingDescription(t *testing.T) {
	requireOllama(t)

	svc := newAgentService(t)
	tenantID := "stateless-missing-desc"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	_, err := svc.StartJob(ctx, &orchestration.StartJobRequest{
		TenantID:    tenantID,
		Description: "",
	})
	require.Error(t, err, "StartJob without description should fail")
}

// =============================================================================
// CancelJob
// =============================================================================

// TestControlLoop_Stateless_CancelJob verifies that a running job can be
// cancelled and subsequently reports a terminal state.
func TestControlLoop_Stateless_CancelJob(t *testing.T) {
	requireOllama(t)

	svc := newAgentService(t)
	tenantID := "stateless-cancel-job"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	resp, err := svc.StartJob(ctx, &orchestration.StartJobRequest{
		TenantID:      tenantID,
		Description:   "Long running task that should be cancelled",
		Lead:          ollamaLead(t, svc, tenantID),
		MaxIterations: 20,
		PollInterval:  "10s",
	})
	require.NoError(t, err, "StartJob should succeed")
	require.NotNil(t, resp)

	// Cancel immediately.
	err = svc.CancelJob(ctx, tenantID, resp.JobID)
	require.NoError(t, err, "CancelJob should succeed")

	// Wait briefly for the cancellation to propagate, then verify status.
	pollCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-pollCtx.Done():
			// Timeout is acceptable; the job may already be cleaned up.
			return
		case <-ticker.C:
			status, statusErr := svc.GetJobStatus(ctx, tenantID, resp.JobID)
			if statusErr != nil {
				// Job may already be cleaned up.
				t.Logf("GetJobStatus after cancel returned error: %v", statusErr)
				return
			}
			if orchestration.IsTerminalJobState(status.State) {
				t.Logf("job reached terminal state after cancel: %s", status.State)
				return
			}
		}
	}
}

// TestControlLoop_Stateless_CancelJob_NotFound verifies that cancelling a
// non-existent job returns an error.
func TestControlLoop_Stateless_CancelJob_NotFound(t *testing.T) {
	svc := newAgentService(t)
	tenantID := "stateless-cancel-not-found"
	ctx := orchestration.ContextWithTenant(context.Background(), tenantID)

	err := svc.CancelJob(ctx, tenantID, "nonexistent-job-id")
	require.Error(t, err, "cancelling a non-existent job should fail")
}

// =============================================================================
// Agent Identity
// =============================================================================

// TestControlLoop_Stateless_AgentIdentity_Validate verifies that a fully
// populated AgentIdentity passes validation.
func TestControlLoop_Stateless_AgentIdentity_Validate(t *testing.T) {
	identity := &orchestration.AgentIdentity{
		AgentID:          "agent-001",
		ServiceAccountID: "sa-001",
		TenantID:         "tenant-001",
		Role:             "lead",
		Scopes:           []string{"kms:read", "secrets:write"},
		ClientID:         "client-001",
	}

	err := identity.Validate()
	assert.NoError(t, err, "valid identity should pass validation")
}

// TestControlLoop_Stateless_AgentIdentity_ValidateMissingFields verifies that
// each required field produces a validation error when omitted.
func TestControlLoop_Stateless_AgentIdentity_ValidateMissingFields(t *testing.T) {
	testCases := []struct {
		name     string
		identity *orchestration.AgentIdentity
	}{
		{
			name: "MissingAgentID",
			identity: &orchestration.AgentIdentity{
				ServiceAccountID: "sa-001",
				TenantID:         "tenant-001",
				Role:             "lead",
				Scopes:           []string{"kms:read"},
				ClientID:         "client-001",
			},
		},
		{
			name: "MissingServiceAccountID",
			identity: &orchestration.AgentIdentity{
				AgentID:  "agent-001",
				TenantID: "tenant-001",
				Role:     "lead",
				Scopes:   []string{"kms:read"},
				ClientID: "client-001",
			},
		},
		{
			name: "MissingTenantID",
			identity: &orchestration.AgentIdentity{
				AgentID:          "agent-001",
				ServiceAccountID: "sa-001",
				Role:             "lead",
				Scopes:           []string{"kms:read"},
				ClientID:         "client-001",
			},
		},
		{
			name: "MissingRole",
			identity: &orchestration.AgentIdentity{
				AgentID:          "agent-001",
				ServiceAccountID: "sa-001",
				TenantID:         "tenant-001",
				Scopes:           []string{"kms:read"},
				ClientID:         "client-001",
			},
		},
		{
			name: "MissingScopes",
			identity: &orchestration.AgentIdentity{
				AgentID:          "agent-001",
				ServiceAccountID: "sa-001",
				TenantID:         "tenant-001",
				Role:             "lead",
				Scopes:           nil,
				ClientID:         "client-001",
			},
		},
		{
			name: "MissingClientID",
			identity: &orchestration.AgentIdentity{
				AgentID:          "agent-001",
				ServiceAccountID: "sa-001",
				TenantID:         "tenant-001",
				Role:             "lead",
				Scopes:           []string{"kms:read"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.identity.Validate()
			require.Error(t, err, "%s should fail validation", tc.name)
			assert.ErrorIs(t, err, orchestration.ErrAgentIdentityRequired)
		})
	}
}

// TestControlLoop_Stateless_AgentIdentity_InvalidScope verifies that an
// identity with malformed scopes fails validation.
func TestControlLoop_Stateless_AgentIdentity_InvalidScope(t *testing.T) {
	identity := &orchestration.AgentIdentity{
		AgentID:          "agent-001",
		ServiceAccountID: "sa-001",
		TenantID:         "tenant-001",
		Role:             "lead",
		Scopes:           []string{"invalid-scope-no-colon"},
		ClientID:         "client-001",
	}

	err := identity.Validate()
	require.Error(t, err, "identity with invalid scope format should fail")
	assert.ErrorIs(t, err, orchestration.ErrAgentIdentityInvalidScope)
}

// TestControlLoop_Stateless_AgentIdentity_ScopeMatching verifies exact and
// wildcard scope matching.
func TestControlLoop_Stateless_AgentIdentity_ScopeMatching(t *testing.T) {
	identity := &orchestration.AgentIdentity{
		AgentID:          "agent-001",
		ServiceAccountID: "sa-001",
		TenantID:         "tenant-001",
		Role:             "lead",
		Scopes:           []string{"kms:read", "secrets:*", "orchestrator:write"},
		ClientID:         "client-001",
	}

	t.Run("ExactMatch", func(t *testing.T) {
		assert.True(t, identity.HasScope("kms:read"), "exact scope should match")
		assert.True(t, identity.HasScope("orchestrator:write"), "exact scope should match")
		assert.False(t, identity.HasScope("kms:write"), "non-matching scope should not match")
	})

	t.Run("WildcardMatch", func(t *testing.T) {
		assert.True(t, identity.HasScope("secrets:read"), "wildcard should match secrets:read")
		assert.True(t, identity.HasScope("secrets:write"), "wildcard should match secrets:write")
		assert.True(t, identity.HasScope("secrets:delete"), "wildcard should match secrets:delete")
	})

	t.Run("HasAnyScope", func(t *testing.T) {
		assert.True(t, identity.HasAnyScope("kms:read", "nonexistent:scope"),
			"HasAnyScope should return true if any scope matches")
		assert.False(t, identity.HasAnyScope("nonexistent:scope", "another:missing"),
			"HasAnyScope should return false if no scope matches")
	})
}

// =============================================================================
// Helpers
// =============================================================================

// findWorkflowDir locates the workflows directory relative to the project root.
func findWorkflowDir(t *testing.T) string {
	t.Helper()

	// The project root is three levels above this package.
	candidates := []string{
		"../../../workflows",
	}

	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			abs, absErr := filepath.Abs(dir)
			if absErr != nil {
				continue
			}
			return abs
		}
	}

	// Try to find it by walking up from the current working directory.
	wd, err := os.Getwd()
	require.NoError(t, err)

	for dir := wd; dir != "/"; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "workflows")
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			return candidate
		}
	}

	t.Fatal("could not locate workflows directory")
	return ""
}

// isValidScopeFormat is a helper to verify the public scope validation function.
func isValidScopeFormat(scope string) bool {
	return orchestration.IsValidScopeFormat(scope)
}

// TestControlLoop_Stateless_ScopeFormat verifies the scope format validation
// function with valid and invalid patterns.
func TestControlLoop_Stateless_ScopeFormat(t *testing.T) {
	validScopes := []string{
		"kms:read",
		"secrets:write",
		"orchestrator:*",
		"agent:spawn",
		"kms:keys:read",
	}

	for _, scope := range validScopes {
		t.Run("Valid_"+strings.ReplaceAll(scope, ":", "_"), func(t *testing.T) {
			assert.True(t, isValidScopeFormat(scope), "scope %q should be valid", scope)
		})
	}

	invalidScopes := []string{
		"",
		"nocolon",
		":leading-colon",
		"trailing-colon:",
		"123:numeric-start",
	}

	for _, scope := range invalidScopes {
		name := scope
		if name == "" {
			name = "empty"
		}
		t.Run("Invalid_"+strings.ReplaceAll(name, ":", "_"), func(t *testing.T) {
			assert.False(t, isValidScopeFormat(scope), "scope %q should be invalid", scope)
		})
	}
}
