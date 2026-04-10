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

package orchestration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// defaultPollInterval is the default interval for polling task completion.
const defaultPollInterval = 5 * time.Second

// stateHandler is the function signature for state machine handlers.
// Each handler returns the next state to transition to.
type stateHandler func(ctx context.Context) (JobState, error)

// workerResult carries the outcome of a concurrent worker execution.
type workerResult struct {
	index  int
	output string
	err    error
}

// indexedWorkItem pairs a wave-relative index with its work item for
// session-grouped execution. Items targeting the same session are processed
// sequentially within a single goroutine.
type indexedWorkItem struct {
	index int
	item  *WorkItem
}

// ControlLoop orchestrates autonomous task execution via a lead agent.
// It uses a state machine with map-based dispatch for O(1) routing.
//
// Extended state flow:
//
//	initializing -> decomposing -> scheduling -> assigning -> executing ->
//	validating -> reviewing -> completing -> cleanup -> completed
//	                  ^                          |
//	                  +--- re-delegating <-------+
//	                           |
//	                      escalating -> failed
type ControlLoop struct {
	agentService        AgentService
	logger              interfaces.Logger
	status              atomic.Value // *JobStatus
	events              chan JobEvent
	handlers            map[JobState]stateHandler
	workItems           []*WorkItem
	taskIDs             []string
	leadSessionID       string
	config              *JobConfig
	jobID               string
	iteration           int
	workflow            *WorkflowDefinition
	workerSessions      map[string]string         // role -> sessionID
	agentIdentities     map[string]*AgentIdentity // role -> identity
	executionPlan       *ExecutionPlan
	currentWave         int
	jobMemory           *JobMemory
	validatorRegistry   *ValidatorRegistry
	validationResults   []ValidationResult
	reviewResponse      *ReviewResponse
	correlationID       string
	persistFn           func(status *JobStatus)
	auditFn             func(eventType, details string)
	peerMessaging       PeerMessagingService // nil if topology is hub_and_spoke
	fileLocking         FileLockService      // nil if not configured
	costTracker         CostTracker          // nil if no budget set
	auditMu             sync.Mutex
	auditEvents         []JobAuditEvent
	reindexFn           func(context.Context) error                 // nil if indexing not configured
	peerDeliverFn       func(context.Context, string, *PeerMessage) // nil if no delivery configured
	peerDeliveryCancels []func()                                    // cancel functions for peer delivery goroutines
}

// NewControlLoop creates a new ControlLoop with the given agent service and logger.
func NewControlLoop(svc AgentService, logger interfaces.Logger) *ControlLoop {
	cl := &ControlLoop{
		agentService:    svc,
		logger:          logger,
		events:          make(chan JobEvent, defaultEventBufferSize),
		workerSessions:  make(map[string]string),
		agentIdentities: make(map[string]*AgentIdentity),
	}
	cl.handlers = map[JobState]stateHandler{
		JobStateInitializing: cl.handleInitialize,
		JobStateDecomposing:  cl.handleDecompose,
		JobStateScheduling:   cl.handleSchedule,
		JobStateAssigning:    cl.handleAssign,
		JobStateExecuting:    cl.handleExecute,
		JobStateValidating:   cl.handleValidate,
		JobStateReviewing:    cl.handleReview,
		JobStateRedelegating: cl.handleRedelegate,
		JobStateCompleting:   cl.handleComplete,
		JobStateCleanup:      cl.handleCleanup,
		JobStateEscalating:   cl.handleEscalate,
	}
	return cl
}

// SetWorkflow sets the workflow definition for this control loop.
// Must be called before Run if a workflow is desired.
func (cl *ControlLoop) SetWorkflow(wf *WorkflowDefinition) {
	cl.workflow = wf
}

// Run executes the control loop state machine for the given job configuration.
// It blocks until the job completes, fails, or the context is cancelled.
// This always starts from the initializing state. For resumption from a
// specific state, use RunFromState.
func (cl *ControlLoop) Run(ctx context.Context, config *JobConfig) error {
	jobID, err := generateJobID()
	if err != nil {
		return &ControlLoopError{
			Sentinel: ErrControlLoopFailed,
			Message:  "failed to generate job ID: " + err.Error(),
		}
	}
	return cl.RunFromState(ctx, config, jobID, JobStateInitializing)
}

// RunFromState executes the control loop state machine starting from the
// specified state. This is used for job resumption after server restart,
// where the job ID is already known and the start state is determined by
// resolveResumeState based on the interrupted state.
func (cl *ControlLoop) RunFromState(ctx context.Context, config *JobConfig, jobID string, startState JobState) error {
	// Ensure tenant ID is in the context for downstream service calls.
	if config.TenantID != "" && TenantFromContext(ctx) == "" {
		ctx = ContextWithTenant(ctx, config.TenantID)
	}
	cl.config = config
	cl.jobID = jobID
	if cl.iteration == 0 {
		cl.iteration = 0
	}

	if config.PollInterval == 0 {
		cl.config.PollInterval = defaultPollInterval
	}

	cl.updateStatus(startState)
	cl.emitEvent(startState, "job started: "+config.Description)

	// State machine dispatch loop.
	state := startState
	for state != JobStateCompleted && state != JobStateFailed {
		handler, ok := cl.handlers[state]
		if !ok {
			cl.updateStatus(JobStateFailed)
			return &ControlLoopError{
				Sentinel: ErrControlLoopFailed,
				Message:  "unknown state: " + string(state),
			}
		}

		var err error
		var nextState JobState
		nextState, err = handler(ctx)
		if err != nil {
			cl.updateStatus(JobStateFailed)
			cl.emitEvent(JobStateFailed, "error in state "+string(state)+": "+err.Error())
			cl.cleanupAllSessions(ctx)
			return err
		}

		state = nextState
		cl.updateStatus(state)
	}

	if state == JobStateFailed {
		return &ControlLoopError{
			Sentinel: ErrControlLoopFailed,
			Message:  "job failed",
		}
	}

	return nil
}

// Status returns the current job status.
func (cl *ControlLoop) Status() *JobStatus {
	val := cl.status.Load()
	if val == nil {
		return nil
	}
	status, ok := val.(*JobStatus)
	if !ok {
		return nil
	}
	return status
}

// Events returns a read-only channel of job events.
func (cl *ControlLoop) Events() <-chan JobEvent {
	return cl.events
}

// =============================================================================
// State Handlers
// =============================================================================

// handleInitialize bootstraps the job: generates correlation ID, initializes
// job memory, sets up the validator registry, and loads the workflow if configured.
func (cl *ControlLoop) handleInitialize(ctx context.Context) (JobState, error) {
	if err := ctx.Err(); err != nil {
		return JobStateFailed, err
	}

	// Generate correlation ID for audit trail linking.
	corrID, err := generateCorrelationID()
	if err != nil {
		return JobStateFailed, &ControlLoopError{
			Sentinel: ErrControlLoopFailed,
			Message:  "failed to generate correlation ID: " + err.Error(),
		}
	}
	cl.correlationID = corrID

	// Initialize job memory for accumulated context across iterations.
	cl.jobMemory = NewJobMemory()

	// Initialize validator registry with built-in validators.
	cl.validatorRegistry = NewValidatorRegistry()
	cl.validatorRegistry.Register("command", NewCommandValidator())
	cl.validatorRegistry.Register("policy", NewPolicyValidator())
	cl.validatorRegistry.Register("agent", NewAgentValidator(
		cl.agentService,
		cl.config.LeadBackend,
		cl.config.LeadModel,
	))

	// Spawn a lead session if the backend supports persistent sessions.
	leadSessionErr := cl.trySpawnLeadSession(ctx)
	if leadSessionErr != nil {
		cl.logger.Warnf("control loop: failed to spawn lead session, using stateless mode: %s", leadSessionErr.Error())
	}

	cl.emitEvent(JobStateDecomposing, "initialization complete, correlation_id="+cl.correlationID)
	cl.audit("orchestration:job_started", "job_id="+cl.jobID+" correlation_id="+cl.correlationID)

	return JobStateDecomposing, nil
}

// handleDecompose calls the agent service to decompose the task into work items.
// When job memory has accumulated context from prior iterations, it is injected
// into the decomposition prompt so agents avoid repeating past mistakes.
func (cl *ControlLoop) handleDecompose(ctx context.Context) (JobState, error) {
	if err := ctx.Err(); err != nil {
		return JobStateFailed, err
	}

	var projectIDs []string
	if cl.config.ProjectID != "" {
		projectIDs = []string{cl.config.ProjectID}
	}

	// Build enhanced task description with accumulated context.
	taskDescription := cl.config.Description
	if cl.jobMemory != nil && !cl.jobMemory.IsEmpty() {
		taskDescription = taskDescription + "\n\n" + cl.jobMemory.Serialize()
	}

	// Append backend capabilities context from workflow if available.
	if cl.workflow != nil {
		taskDescription = taskDescription + "\n\n" + buildCapabilitiesContext(cl.workflow)
	}

	cl.audit("lead:decompose_start", "backend="+cl.config.LeadBackend+" model="+cl.config.LeadModel)

	resp, err := cl.agentService.DecomposeTask(ctx, &DecomposeTaskRequest{
		Backend:    cl.config.LeadBackend,
		Model:      cl.config.LeadModel,
		Task:       taskDescription,
		ProjectIDs: projectIDs,
	})
	if err != nil {
		cl.audit("lead:decompose_failed", "error="+err.Error())
		return JobStateFailed, err
	}

	// Log decomposed work items.
	for _, item := range resp.WorkItems {
		cl.audit("lead:work_item", "title="+item.Title)
	}

	if len(resp.WorkItems) == 0 {
		return JobStateFailed, &EmptyDecompositionError{
			Description: cl.config.Description,
		}
	}

	cl.workItems = resp.WorkItems

	// Extract constraints from the decomposition response and record them
	// in job memory so subsequent iterations are aware of discovered limits.
	cl.extractConstraints(resp.WorkItems)

	cl.emitEvent(JobStateScheduling, "decomposed into "+itoa(len(resp.WorkItems))+" work items")
	cl.audit("orchestration:decomposed", "work_items="+itoa(len(resp.WorkItems))+" iteration="+itoa(cl.iteration))

	return JobStateScheduling, nil
}

// handleSchedule builds a DAG execution plan from work items and computes
// topological waves for parallel execution.
func (cl *ControlLoop) handleSchedule(ctx context.Context) (JobState, error) {
	if err := ctx.Err(); err != nil {
		return JobStateFailed, err
	}

	maxParallelism := 0
	if cl.workflow != nil && cl.workflow.Settings.MaxParallelism > 0 {
		maxParallelism = cl.workflow.Settings.MaxParallelism
	}

	plan, err := ScheduleDAG(cl.workItems, maxParallelism)
	if err != nil {
		return JobStateFailed, &SchedulingError{
			Message: "failed to build execution plan",
			Cause:   err,
		}
	}

	cl.executionPlan = plan
	cl.currentWave = 0

	cl.emitEvent(JobStateAssigning, "scheduled "+itoa(plan.TotalWaves())+" execution waves")
	cl.audit("orchestration:scheduled", "waves="+itoa(plan.TotalWaves())+" items="+itoa(len(cl.workItems)))

	return JobStateAssigning, nil
}

// handleAssign auto-spawns worker sessions and creates tasks for each work item
// in the current execution wave. It branches on WorkflowMode to control which
// agents may be spawned and populates agent identities for security tracking.
func (cl *ControlLoop) handleAssign(ctx context.Context) (JobState, error) {
	if err := ctx.Err(); err != nil {
		return JobStateFailed, err
	}

	// Get backends for capability matching.
	backends, err := cl.agentService.ListBackends(ctx)
	if err != nil {
		return JobStateFailed, err
	}

	agents, err := cl.agentService.ListAgents(ctx, cl.config.TenantID, nil)
	if err != nil {
		return JobStateFailed, err
	}

	// In constrained/canned workflow mode, the fleet registry may be empty
	// because agents are defined as workflow role specs, not fleet registrations.
	// Synthesize virtual agent registrations from the workflow so that
	// matchAgentForTask can resolve them.
	if len(agents) == 0 && cl.workflow != nil && cl.isConstrainedWorkflowMode() {
		agents = cl.synthesizeWorkflowAgents()
	}

	if len(agents) == 0 {
		return JobStateFailed, &NoAgentsAvailableError{
			TaskTitle: "all work items",
		}
	}

	// Get the current wave of work items.
	var waveItems []*WorkItem
	if cl.executionPlan != nil && cl.currentWave < cl.executionPlan.TotalWaves() {
		waveItems = cl.executionPlan.Waves[cl.currentWave]
	} else {
		// Fallback: no DAG plan, use all work items as a single wave.
		waveItems = cl.workItems
	}

	cl.taskIDs = make([]string, 0, len(waveItems))

	for _, item := range waveItems {
		// Resolve backend based on workflow mode.
		backendName := cl.resolveBackendForMode(item, backends)

		// In constrained/canned mode, the workflow definition is authoritative.
		// Override LLM-suggested backend/role since the LLM may hallucinate
		// values that don't match actual workflow agent definitions.
		if cl.workflow != nil && cl.isConstrainedWorkflowMode() {
			item.Backend = backendName
			item.RequiredRole = cl.defaultWorkerRole()
			item.RequiredCapabilities = nil // Capabilities come from the workflow, not LLM
		}

		// Spawn a worker session for persistent backends if not already spawned.
		role := item.RequiredRole
		if role == "" {
			role = "worker"
		}

		sessionKey := role + "/" + backendName
		if _, exists := cl.workerSessions[sessionKey]; !exists {
			if cl.backendSupportsSessions(backendName, backends) {
				mcpServers := cl.buildMCPConfig(cl.resolveScopes(role))
				agentSpec := cl.resolveAgentSpec(role)
				spawnReq := &SpawnSessionRequest{
					Backend:    backendName,
					Model:      cl.resolveModelForRole(role),
					ProjectID:  cl.config.ProjectID,
					MCPServers: mcpServers,
					Persistent: cl.roleHasCapability(role, "persistent_sessions"),
				}

				// Apply sandbox configuration from workflow agent spec.
				if agentSpec != nil {
					if agentSpec.WorkspacePath != "" {
						spawnReq.WorkDir = agentSpec.WorkspacePath
					}
					if agentSpec.SkipPermissions {
						spawnReq.ExtraArgs = append(spawnReq.ExtraArgs, "--dangerously-skip-permissions")
					}
					if agentSpec.PermissionMode != "" {
						spawnReq.ExtraArgs = append(spawnReq.ExtraArgs, "--permission-mode", agentSpec.PermissionMode)
					}
					for k, v := range agentSpec.Env {
						if spawnReq.Env == nil {
							spawnReq.Env = make(map[string]string)
						}
						spawnReq.Env[k] = v
					}
					// Container sandbox: run the agent in an isolated container.
					if agentSpec.Sandboxed {
						spawnReq.Sandboxed = true
						spawnReq.ContainerImage = agentSpec.ContainerImage
					}
				}

				persistStr := "false"
				if spawnReq.Persistent {
					persistStr = "true"
				}
				cl.audit("worker:session_spawn", "role="+role+" backend="+backendName+" persistent="+persistStr)
				spawnResp, spawnErr := cl.agentService.SpawnSession(ctx, spawnReq)
				if spawnErr != nil {
					cl.audit("worker:session_spawn_failed", "role="+role+" backend="+backendName+" error="+spawnErr.Error())
					cl.logger.Warnf("control loop: failed to spawn worker session for role=%s backend=%s: %s",
						role, backendName, spawnErr.Error())
				} else {
					cl.workerSessions[sessionKey] = spawnResp.SessionID
					cl.audit("worker:session_spawned", "role="+role+" session="+spawnResp.SessionID)
				}
			}
		}

		// Populate agent identity for security tracking.
		if _, exists := cl.agentIdentities[role]; !exists {
			scopes := cl.resolveScopes(role)
			identity := &AgentIdentity{
				AgentID:  "agent-" + role + "-" + cl.jobID,
				TenantID: cl.config.TenantID,
				Role:     role,
				Scopes:   scopes,
			}
			cl.agentIdentities[role] = identity
		}

		// Match an agent for task assignment.
		agentID, _ := cl.matchAgentForTask(item, agents, backends)
		if agentID == "" {
			return JobStateFailed, &NoAgentsAvailableError{
				TaskTitle:            item.Title,
				RequiredCapabilities: item.RequiredCapabilities,
			}
		}

		task, createErr := cl.agentService.CreateTask(ctx, &CreateTaskRequest{
			TenantID:    cl.config.TenantID,
			Subject:     item.Title,
			Description: item.Description,
			AssignedTo:  agentID,
		})
		if createErr != nil {
			return JobStateFailed, createErr
		}

		// Claim the task so the state machine allows completion (pending -> claimed).
		if claimErr := cl.agentService.ClaimTask(ctx, cl.config.TenantID, task.ID, agentID); claimErr != nil {
			cl.logger.Warnf("control loop: failed to claim task %s for agent %s: %s",
				task.ID, agentID, claimErr.Error())
		}

		cl.taskIDs = append(cl.taskIDs, task.ID)
	}

	// Start peer message delivery for agents in this wave if peer messaging is configured.
	cl.startPeerDelivery(ctx, waveItems)
	cl.emitEvent(JobStateExecuting, "assigned "+itoa(len(cl.taskIDs))+" tasks for wave "+itoa(cl.currentWave))
	cl.audit("orchestration:task_assigned", "wave="+itoa(cl.currentWave)+" tasks="+itoa(len(cl.taskIDs)))

	return JobStateExecuting, nil
}

// handleExecute groups work items by their resolved session ID so that items
// targeting different sessions run in parallel (separate goroutines) while items
// targeting the same session run sequentially (queued within one goroutine).
// Stateless items (no persistent session) fall back to polling.
func (cl *ControlLoop) handleExecute(ctx context.Context) (JobState, error) {
	if err := ctx.Err(); err != nil {
		return JobStateFailed, err
	}

	// Resolve items for the current wave.
	var waveItems []*WorkItem
	if cl.executionPlan != nil && cl.currentWave < cl.executionPlan.TotalWaves() {
		waveItems = cl.executionPlan.Waves[cl.currentWave]
	} else {
		waveItems = cl.workItems
	}

	// Track which items were handled by direct prompting so we know which
	// ones to fall back to polling for.
	directHandled := make(map[int]struct{}, len(waveItems))

	// Group persistent-session items by their resolved session ID so that
	// items targeting the same session are serialized, while items targeting
	// different sessions execute concurrently.
	sessionItems := make(map[string][]*indexedWorkItem) // sessionID -> ordered items
	var totalPersistent int

	for i, item := range waveItems {
		if i >= len(cl.taskIDs) {
			break
		}
		sessionID := cl.resolveWorkerSession(item)
		if sessionID != "" {
			sessionItems[sessionID] = append(sessionItems[sessionID],
				&indexedWorkItem{index: i, item: item})
			totalPersistent++
		}
	}

	// Launch one goroutine per session. Within each goroutine, items are
	// processed sequentially to avoid concurrent prompts on the same session.
	if totalPersistent > 0 {
		results := make(chan workerResult, totalPersistent)

		for sid, items := range sessionItems {
			go func(sessionID string, work []*indexedWorkItem) {
				for _, iw := range work {
					// Validate communication policy before prompting worker.
					topology := TopologyHubAndSpoke
					if cl.workflow != nil && cl.workflow.Settings.Topology != "" {
						topology = cl.workflow.Settings.Topology
					}
					if commErr := ValidateCommunication(topology, "lead", "worker", "assign"); commErr != nil {
						results <- workerResult{index: iw.index, err: commErr}
						continue
					}

					promptRole := iw.item.RequiredRole
					if promptRole == "" {
						promptRole = "worker"
					}
					cl.audit("worker:prompt_start", "role="+promptRole+" task="+iw.item.Title+" backend="+iw.item.Backend+" session="+sessionID)

					agentSpec := cl.resolveAgentSpec(iw.item.RequiredRole)
					workspacePath := ""
					if agentSpec != nil && agentSpec.WorkspacePath != "" {
						// For sandboxed agents, use /workspace (container mount point)
						// instead of the host path.
						if agentSpec.Sandboxed {
							workspacePath = "/workspace"
						} else {
							workspacePath = agentSpec.WorkspacePath
						}
					}
					toolGuidance := cl.agentService.BackendToolGuidance(iw.item.Backend)
					workerPrompt := buildWorkerPrompt(iw.item, workspacePath, toolGuidance)
					resp, promptErr := cl.agentService.PromptSession(ctx, &PromptSessionRequest{
						SessionID: sessionID,
						Prompt:    workerPrompt,
						EventCallback: func(sid, evtType, details string) {
							cl.audit(evtType, "session="+sid+" "+details)
						},
					})
					if promptErr != nil {
						cl.audit("worker:prompt_failed", "role="+promptRole+" task="+iw.item.Title+" error="+promptErr.Error())
						results <- workerResult{index: iw.index, err: promptErr}
						continue
					}

					responseText := extractPromptSessionText(resp)
					cl.audit("worker:prompt_complete", "role="+promptRole+" task="+iw.item.Title+" response="+responseText)
					results <- workerResult{index: iw.index, output: responseText}
				}
			}(sid, items)
		}

		// Collect all results from session goroutines.
		collected := 0
		for collected < totalPersistent {
			select {
			case <-ctx.Done():
				return JobStateFailed, ctx.Err()
			case r := <-results:
				if r.err != nil {
					// Record failure in job memory.
					cl.jobMemory.AddFailure(
						waveItems[r.index].Title,
						waveItems[r.index].Description,
						r.err.Error(),
						cl.iteration,
					)
					cl.logger.Warnf("control loop: worker prompt failed for task %s: %s",
						cl.taskIDs[r.index], r.err.Error())
				} else {
					// Record success in job memory.
					cl.jobMemory.AddSuccess(waveItems[r.index].Title + ": " + waveItems[r.index].Description)

					// Complete the task with the response.
					completeErr := cl.agentService.CompleteTask(ctx, cl.config.TenantID, cl.taskIDs[r.index], &TaskResult{
						Output: r.output,
					})
					if completeErr != nil {
						cl.logger.Warnf("control loop: failed to complete task %s: %s",
							cl.taskIDs[r.index], completeErr.Error())
					}
				}
				directHandled[r.index] = struct{}{}
				collected++
			}
		}
	}

	// Check budget after wave execution completes.
	if budgetState, budgetErr := cl.checkBudget(ctx); budgetErr == nil && budgetState != nil {
		if budgetState.Status == BudgetExceeded && budgetState.Action == BudgetActionShutdown {
			cl.emitEvent(JobStateFailed, "budget exceeded, shutting down")
			cl.cleanupAllSessions(ctx)
			return JobStateFailed, nil
		}
		if budgetState.Status == BudgetExceeded && budgetState.Action == BudgetActionPause {
			cl.emitEvent(JobStateEscalating, "budget exceeded, pausing execution")
			return JobStateEscalating, nil
		}
	}

	// Check if all tasks were handled directly (no need to poll).
	if len(directHandled) == len(cl.taskIDs) {
		return cl.advanceOrValidate()
	}

	// Poll for remaining task completion (stateless backends).
	ticker := time.NewTicker(cl.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return JobStateFailed, ctx.Err()
		case <-ticker.C:
			allDone, pollErr := cl.pollTaskCompletion(ctx)
			if pollErr != nil {
				return JobStateFailed, pollErr
			}
			if allDone {
				return cl.advanceOrValidate()
			}
		}
	}
}

// advanceOrValidate checks if more execution waves remain. If so, it advances
// to the next wave via assigning; otherwise it transitions to validation.
func (cl *ControlLoop) advanceOrValidate() (JobState, error) {
	if cl.executionPlan != nil && cl.currentWave+1 < cl.executionPlan.TotalWaves() {
		cl.currentWave++
		cl.taskIDs = nil
		cl.emitEvent(JobStateAssigning, "advancing to wave "+itoa(cl.currentWave))
		return JobStateAssigning, nil
	}

	cl.emitEvent(JobStateValidating, "all tasks completed, starting validation")
	cl.audit("orchestration:task_completed", "wave="+itoa(cl.currentWave)+" tasks="+itoa(len(cl.taskIDs)))
	return JobStateValidating, nil
}

// handleValidate runs the pluggable validator pipeline against completed task
// outputs if review criteria are configured in the workflow settings.
func (cl *ControlLoop) handleValidate(ctx context.Context) (JobState, error) {
	if err := ctx.Err(); err != nil {
		return JobStateFailed, err
	}

	// Collect task results for validation.
	artifacts := cl.collectTaskResults(ctx)

	// Run validator pipeline if review criteria exist.
	var criteria []ReviewCriterion
	if cl.workflow != nil {
		criteria = cl.workflow.Settings.ReviewCriteria
	}

	if len(criteria) > 0 && cl.validatorRegistry != nil {
		results, allPassed, pipelineErr := cl.validatorRegistry.RunPipeline(ctx, criteria, artifacts)
		if pipelineErr != nil {
			cl.logger.Warnf("control loop: validator pipeline error: %s", pipelineErr.Error())
		}
		cl.validationResults = results

		if !allPassed {
			cl.emitEvent(JobStateReviewing, "validation: some required criteria failed")
		} else {
			cl.emitEvent(JobStateReviewing, "validation: all criteria passed")
		}
	} else {
		cl.validationResults = nil
		cl.emitEvent(JobStateReviewing, "no validation criteria configured, proceeding to review")
	}

	cl.audit("orchestration:validation_result", "criteria="+itoa(len(cl.validationResults)))

	// Trigger re-indexing after validation if configured.
	if cl.workflow != nil && cl.reindexFn != nil {
		if reindexErr := HookTriggerReindex(ctx, cl.reindexFn, cl.workflow.Settings.VectorDB.Triggers); reindexErr != nil {
			cl.logger.Warnf("control loop: reindex trigger failed: %s", reindexErr.Error())
		}
	}

	return JobStateReviewing, nil
}

// handleReview builds a review prompt from task results and validation outcomes,
// sends it to the lead agent, and parses the structured response. The communication
// policy is consulted to determine blocking behavior for completion signals.
func (cl *ControlLoop) handleReview(ctx context.Context) (JobState, error) {
	if err := ctx.Err(); err != nil {
		return JobStateFailed, err
	}

	// Consult communication policy to determine blocking behavior for review.
	// Worker completion signals use blocking policy so the lead waits for results.
	policy, _ := GetPolicy("worker", "complete")
	cl.logger.Debugf("control loop: review phase using %s policy for worker completion", string(policy))

	// Fetch all tasks for building the review prompt.
	tasks, err := cl.agentService.ListTasks(ctx, cl.config.TenantID, nil)
	if err != nil {
		return JobStateFailed, err
	}

	// Filter to only tasks belonging to this job.
	jobTasks := cl.filterJobTasks(tasks)

	reviewPrompt := buildReviewPrompt(cl.config.Description, jobTasks)

	// Append validation results to the review prompt if available.
	if len(cl.validationResults) > 0 {
		reviewPrompt = reviewPrompt + "\n\n" + buildValidationSummary(cl.validationResults)
	}

	// Send review to the lead agent.
	var responseText string
	if cl.leadSessionID != "" {
		resp, promptErr := cl.agentService.PromptSession(ctx, &PromptSessionRequest{
			SessionID:    cl.leadSessionID,
			Prompt:       reviewPrompt,
			SystemPrompt: "You are a quality reviewer. Respond ONLY with valid JSON.",
		})
		if promptErr != nil {
			return JobStateFailed, promptErr
		}
		responseText = extractPromptSessionText(resp)
	} else {
		// Stateless path: use DecomposeTask as a general prompt mechanism.
		resp, promptErr := cl.agentService.DecomposeTask(ctx, &DecomposeTaskRequest{
			Backend: cl.config.LeadBackend,
			Model:   cl.config.LeadModel,
			Task:    reviewPrompt,
		})
		if promptErr != nil {
			return JobStateFailed, promptErr
		}
		responseText = extractDecomposeText(resp)
	}

	review, parseErr := parseReviewResponse(responseText)
	if parseErr != nil {
		return JobStateFailed, parseErr
	}

	cl.reviewResponse = review

	if review.Approved {
		cl.emitEvent(JobStateCompleting, "review approved: "+review.Feedback)
		cl.audit("orchestration:review_decision", "approved=true feedback="+review.Feedback)
		return JobStateCompleting, nil
	}

	// Not approved: store new work items for re-delegation.
	if len(review.NewWorkItems) > 0 {
		cl.workItems = review.NewWorkItems
	}
	cl.emitEvent(JobStateRedelegating, "review rejected: "+review.Feedback)
	cl.audit("orchestration:review_decision", "approved=false feedback="+review.Feedback)

	return JobStateRedelegating, nil
}

// handleComplete finalizes the job results and emits a completion event.
func (cl *ControlLoop) handleComplete(ctx context.Context) (JobState, error) {
	if err := ctx.Err(); err != nil {
		return JobStateFailed, err
	}

	cl.emitEvent(JobStateCleanup, "job completing, starting cleanup")
	cl.audit("orchestration:job_completing", "job_id="+cl.jobID+" iteration="+itoa(cl.iteration))

	return JobStateCleanup, nil
}

// handleCleanup closes all worker sessions and the lead session, releasing resources.
func (cl *ControlLoop) handleCleanup(ctx context.Context) (JobState, error) {
	// Log session count before cleanup.
	sessionCount := len(cl.workerSessions)
	if cl.leadSessionID != "" {
		sessionCount++
	}
	cl.audit("orchestration:cleanup_start", "sessions="+itoa(sessionCount))

	// Close all worker and lead sessions.
	cl.cleanupAllSessions(ctx)

	// Job completion summary.
	summary := "job_id=" + cl.jobID +
		" iterations=" + itoa(cl.iteration) +
		" work_items=" + itoa(len(cl.workItems))
	if status := cl.Status(); status != nil {
		summary += " tasks=" + itoa(status.TaskCount) +
			" completed=" + itoa(status.Completed) +
			" failed=" + itoa(status.Failed)
	}
	cl.audit("orchestration:job_summary", summary)

	cl.emitEvent(JobStateCompleted, "job completed successfully")
	cl.audit("orchestration:job_completed", "job_id="+cl.jobID)

	return JobStateCompleted, nil
}

// handleRedelegate adds review feedback to job memory, records failed items,
// checks the iteration limit, and transitions back to scheduling.
func (cl *ControlLoop) handleRedelegate(ctx context.Context) (JobState, error) {
	if err := ctx.Err(); err != nil {
		return JobStateFailed, err
	}

	cl.iteration++

	// Add review feedback to job memory.
	if cl.reviewResponse != nil && cl.reviewResponse.Feedback != "" {
		cl.jobMemory.AddFeedback(cl.reviewResponse.Feedback)
	}

	// Record failed items in job memory.
	if cl.reviewResponse != nil && len(cl.reviewResponse.NewWorkItems) > 0 {
		for _, item := range cl.reviewResponse.NewWorkItems {
			cl.jobMemory.AddFailure(item.Title, item.Description, "rejected in review", cl.iteration)
		}
	}

	// Check iteration limit.
	maxIterations := cl.config.MaxIterations
	if cl.workflow != nil && cl.workflow.Settings.MaxIterations > 0 {
		maxIterations = cl.workflow.Settings.MaxIterations
	}
	if maxIterations > 0 && cl.iteration >= maxIterations {
		cl.emitEvent(JobStateEscalating, "max iterations exceeded: "+itoa(cl.iteration))
		return JobStateEscalating, nil
	}

	// Clear old task IDs and work items for the new iteration.
	// Re-decomposition will generate fresh work items with new IDs.
	cl.taskIDs = nil
	cl.workItems = nil
	cl.executionPlan = nil
	cl.currentWave = 0

	cl.emitEvent(JobStateDecomposing, "re-delegating iteration "+itoa(cl.iteration))
	cl.audit("orchestration:redelegate", "iteration="+itoa(cl.iteration))

	return JobStateDecomposing, nil
}

// handleEscalate emits an escalation event with full context and transitions to failed.
func (cl *ControlLoop) handleEscalate(ctx context.Context) (JobState, error) {
	cl.emitEvent(JobStateFailed, "job escalated: max iterations exceeded after "+itoa(cl.iteration)+" iterations")
	cl.audit("orchestration:job_escalated", "iterations="+itoa(cl.iteration)+" reason=max_iterations_exceeded")

	// Clean up before failing.
	cl.cleanupAllSessions(ctx)

	return JobStateFailed, &EscalationError{
		JobID:      cl.jobID,
		Iterations: cl.iteration,
		Reason:     "max iterations exceeded",
	}
}

// =============================================================================
// Helper Methods
// =============================================================================

// cleanupAllSessions closes all worker sessions and the lead session.
func (cl *ControlLoop) cleanupAllSessions(ctx context.Context) {
	// Cancel all peer delivery goroutines.
	for _, cancel := range cl.peerDeliveryCancels {
		cancel()
	}
	cl.peerDeliveryCancels = nil

	for key, sessionID := range cl.workerSessions {
		if sessionID == "" {
			continue
		}
		_, err := cl.agentService.CloseSession(ctx, &CloseSessionRequest{
			SessionID: sessionID,
		})
		if err != nil {
			cl.logger.Warnf("control loop: failed to close worker session %s: %s", key, err.Error())
		}
	}
	cl.workerSessions = make(map[string]string)

	cl.closeLeadSession(ctx)
}

// resolveBackendForMode resolves the backend for a work item based on the workflow mode.
// In canned/constrained mode, only agents defined in the workflow are allowed.
// In autonomous mode, the work item's backend field is honored for dynamic spawning.
func (cl *ControlLoop) resolveBackendForMode(item *WorkItem, backends []*AgentBackendInfo) string {
	if cl.workflow == nil {
		return cl.resolveBackendForItem(item)
	}

	switch cl.workflow.Mode {
	case WorkflowModeCanned, WorkflowModeConstrained:
		// Only spawn agents defined in the workflow.
		if item.RequiredRole != "" {
			for i := range cl.workflow.Agents {
				if cl.workflow.Agents[i].Role == item.RequiredRole {
					return cl.workflow.Agents[i].Backend
				}
			}
		}
		// Fall back to the default worker role's backend. The lead role is a
		// model server (e.g., ollama) that cannot execute work items.
		defaultRole := cl.defaultWorkerRole()
		for i := range cl.workflow.Agents {
			if cl.workflow.Agents[i].Role == defaultRole {
				return cl.workflow.Agents[i].Backend
			}
		}
		return cl.config.LeadBackend

	case WorkflowModeAutonomous:
		// Autonomous mode: allow dynamic backend from work item specification.
		if item.Backend != "" {
			// Verify the backend exists before using it.
			for _, b := range backends {
				if b.Name == item.Backend {
					return item.Backend
				}
			}
			cl.logger.Warnf("control loop: autonomous mode requested backend %q not found, falling back", item.Backend)
		}
		return cl.resolveBackendForItem(item)

	default:
		return cl.resolveBackendForItem(item)
	}
}

// isConstrainedWorkflowMode returns true if the workflow is in canned or constrained mode.
func (cl *ControlLoop) isConstrainedWorkflowMode() bool {
	if cl.workflow == nil {
		return false
	}
	return cl.workflow.Mode == WorkflowModeCanned || cl.workflow.Mode == WorkflowModeConstrained
}

// synthesizeWorkflowAgents creates virtual AgentRegistration entries from the
// workflow's AgentRoleSpec entries. This allows matchAgentForTask to resolve
// agents when the fleet registry is empty but the workflow defines agent roles.
// The lead role is excluded because it handles decomposition, not work execution.
func (cl *ControlLoop) synthesizeWorkflowAgents() []*AgentRegistration {
	agents := make([]*AgentRegistration, 0, len(cl.workflow.Agents))
	for i := range cl.workflow.Agents {
		spec := &cl.workflow.Agents[i]
		if spec.Role == "lead" {
			continue
		}

		count := spec.Count
		if count < 1 {
			count = 1
		}

		for j := 0; j < count; j++ {
			idBuf := make([]byte, 8)
			_, _ = rand.Read(idBuf)
			agentID := "wf-" + spec.Role + "-" + hex.EncodeToString(idBuf)

			agents = append(agents, &AgentRegistration{
				ID:           agentID,
				TenantID:     cl.config.TenantID,
				Name:         spec.Role,
				Backend:      spec.Backend,
				Model:        spec.Model,
				AgentType:    "workflow",
				Status:       AgentStatusIdle,
				Capabilities: spec.Capabilities,
			})
		}
	}
	return agents
}

// defaultWorkerRole returns the first non-lead role defined in the workflow.
// Work items that do not specify a RequiredRole are assigned to this role so
// they can be matched to the correct backend during assignment.
func (cl *ControlLoop) defaultWorkerRole() string {
	if cl.workflow == nil {
		return ""
	}
	for i := range cl.workflow.Agents {
		if cl.workflow.Agents[i].Role != "lead" {
			return cl.workflow.Agents[i].Role
		}
	}
	return ""
}

// roleHasCapability checks whether a workflow agent role declares a specific capability.
func (cl *ControlLoop) roleHasCapability(role, capability string) bool {
	if cl.workflow == nil {
		return false
	}
	for i := range cl.workflow.Agents {
		if cl.workflow.Agents[i].Role == role {
			for _, cap := range cl.workflow.Agents[i].Capabilities {
				if cap == capability {
					return true
				}
			}
			return false
		}
	}
	return false
}

// resolveWorkerSession returns the persistent session ID for a work item,
// or an empty string if the item has no persistent session. The session key
// is constructed from the role and backend name.
func (cl *ControlLoop) resolveWorkerSession(item *WorkItem) string {
	role := item.RequiredRole
	if role == "" {
		role = "worker"
	}
	backendName := cl.resolveBackendForItem(item)
	sessionKey := role + "/" + backendName
	sessionID, ok := cl.workerSessions[sessionKey]
	if ok && sessionID != "" {
		return sessionID
	}
	return ""
}

// resolveBackendForItem determines the backend to use for a work item.
// It checks the work item's Backend field, then looks up the workflow's
// agent role spec, and finally falls back to the lead backend.
func (cl *ControlLoop) resolveBackendForItem(item *WorkItem) string {
	if item.Backend != "" {
		return item.Backend
	}

	if cl.workflow != nil && item.RequiredRole != "" {
		for i := range cl.workflow.Agents {
			if cl.workflow.Agents[i].Role == item.RequiredRole {
				return cl.workflow.Agents[i].Backend
			}
		}
	}

	return cl.config.LeadBackend
}

// resolveModelForRole determines the model to use for a given role.
// resolveAgentSpec returns the AgentRoleSpec for the given role from the
// workflow definition, or nil if no workflow is set or the role is not found.
func (cl *ControlLoop) resolveAgentSpec(role string) *AgentRoleSpec {
	if cl.workflow == nil {
		return nil
	}
	for i := range cl.workflow.Agents {
		if cl.workflow.Agents[i].Role == role {
			return &cl.workflow.Agents[i]
		}
	}
	return nil
}

func (cl *ControlLoop) resolveModelForRole(role string) string {
	if cl.workflow != nil {
		for i := range cl.workflow.Agents {
			if cl.workflow.Agents[i].Role == role {
				return cl.workflow.Agents[i].Model
			}
		}
	}
	return cl.config.LeadModel
}

// resolveScopes returns the MCP scopes for a given role from the workflow definition.
// Returns nil if no workflow is set or the role is not found.
func (cl *ControlLoop) resolveScopes(role string) []string {
	if cl.workflow == nil {
		return nil
	}
	for i := range cl.workflow.Agents {
		if cl.workflow.Agents[i].Role == role {
			return cl.workflow.Agents[i].MCPScopes
		}
	}
	return nil
}

// buildMCPConfig constructs MCP server configurations filtered by the given scopes.
// Each scope is mapped to an MCP server configuration with that scope as its name.
// Returns nil if no scopes are provided.
func (cl *ControlLoop) buildMCPConfig(scopes []string) []MCPServerConfig {
	if len(scopes) == 0 {
		return nil
	}
	configs := make([]MCPServerConfig, 0, len(scopes))
	for _, scope := range scopes {
		configs = append(configs, MCPServerConfig{
			Name: scope,
		})
	}
	return configs
}

// backendSupportsSessions returns true if the named backend supports persistent sessions.
func (cl *ControlLoop) backendSupportsSessions(backendName string, backends []*AgentBackendInfo) bool {
	for _, b := range backends {
		if b.Name == backendName && b.Capabilities.PersistentSessions {
			return true
		}
	}
	return false
}

// extractConstraints scans work items for constraint markers and records them
// in job memory. Items with description containing "constraint:" prefix lines
// have those constraints extracted.
func (cl *ControlLoop) extractConstraints(items []*WorkItem) {
	if cl.jobMemory == nil {
		return
	}
	for _, item := range items {
		lines := strings.Split(item.Description, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToLower(trimmed), "constraint:") {
				constraint := strings.TrimSpace(trimmed[len("constraint:"):])
				if constraint != "" {
					cl.jobMemory.AddConstraint(constraint)
				}
			}
		}
	}
}

// collectTaskResults fetches all tasks for this job and extracts their results.
func (cl *ControlLoop) collectTaskResults(ctx context.Context) []*TaskResult {
	tasks, err := cl.agentService.ListTasks(ctx, cl.config.TenantID, nil)
	if err != nil {
		cl.logger.Warnf("control loop: failed to list tasks for validation: %s", err.Error())
		return nil
	}

	jobTasks := cl.filterJobTasks(tasks)
	results := make([]*TaskResult, 0, len(jobTasks))
	for _, task := range jobTasks {
		if task.Result != nil {
			results = append(results, task.Result)
		}
	}
	return results
}

// pollTaskCompletion checks whether all tasks have reached a terminal state.
func (cl *ControlLoop) pollTaskCompletion(ctx context.Context) (bool, error) {
	tasks, err := cl.agentService.ListTasks(ctx, cl.config.TenantID, nil)
	if err != nil {
		return false, err
	}

	// Build a set of task IDs belonging to this job for fast lookup.
	jobTaskSet := make(map[string]struct{}, len(cl.taskIDs))
	for _, id := range cl.taskIDs {
		jobTaskSet[id] = struct{}{}
	}

	completed := 0
	failed := 0
	total := len(cl.taskIDs)

	for _, task := range tasks {
		if _, belongs := jobTaskSet[task.ID]; !belongs {
			continue
		}
		switch task.Status {
		case TaskStatusCompleted:
			completed++
		case TaskStatusFailed, TaskStatusCancelled:
			failed++
		}
	}

	done := (completed + failed) >= total
	if done {
		cl.logger.Debugf("control loop: tasks done: completed=%d failed=%d total=%d", completed, failed, total)
	}

	return done, nil
}

// matchAgentForTask finds the best agent for a work item based on capabilities.
func (cl *ControlLoop) matchAgentForTask(
	item *WorkItem,
	agents []*AgentRegistration,
	backends []*AgentBackendInfo,
) (agentID string, backendName string) {
	// Build a backend capabilities lookup map.
	backendCaps := make(map[string]BackendCapabilities, len(backends))
	for _, b := range backends {
		backendCaps[b.Name] = b.Capabilities
	}

	// If the work item specifies a backend, find an agent using that backend.
	if item.Backend != "" {
		return cl.findAgentByBackend(item.Backend, agents)
	}

	// If persistent sessions are required, filter to backends that support them.
	requiresPersistent := false
	for _, cap := range item.RequiredCapabilities {
		if cap == "persistent_sessions" {
			requiresPersistent = true
			break
		}
	}

	if requiresPersistent {
		return cl.findAgentWithPersistentSessions(agents, backendCaps)
	}

	// Default: prefer idle agents, then any available.
	return cl.findBestAvailableAgent(agents)
}

// findAgentByBackend finds the best agent that uses the specified backend.
func (cl *ControlLoop) findAgentByBackend(
	backend string,
	agents []*AgentRegistration,
) (agentID string, backendName string) {
	var fallback *AgentRegistration
	for _, agent := range agents {
		if agent.Backend != backend {
			continue
		}
		if agent.Status == AgentStatusIdle {
			return agent.ID, agent.Backend
		}
		if fallback == nil {
			fallback = agent
		}
	}
	if fallback != nil {
		return fallback.ID, fallback.Backend
	}
	return "", ""
}

// findAgentWithPersistentSessions finds an agent whose backend supports persistent sessions.
func (cl *ControlLoop) findAgentWithPersistentSessions(
	agents []*AgentRegistration,
	backendCaps map[string]BackendCapabilities,
) (agentID string, backendName string) {
	var fallback *AgentRegistration
	for _, agent := range agents {
		caps, exists := backendCaps[agent.Backend]
		if !exists || !caps.PersistentSessions {
			continue
		}
		if agent.Status == AgentStatusIdle {
			return agent.ID, agent.Backend
		}
		if fallback == nil {
			fallback = agent
		}
	}
	if fallback != nil {
		return fallback.ID, fallback.Backend
	}
	return "", ""
}

// findBestAvailableAgent returns the best available agent, preferring idle status.
func (cl *ControlLoop) findBestAvailableAgent(
	agents []*AgentRegistration,
) (agentID string, backendName string) {
	var fallback *AgentRegistration
	for _, agent := range agents {
		if agent.Status == AgentStatusIdle {
			return agent.ID, agent.Backend
		}
		if fallback == nil {
			fallback = agent
		}
	}
	if fallback != nil {
		return fallback.ID, fallback.Backend
	}
	return "", ""
}

// filterJobTasks returns only tasks whose IDs belong to this job.
func (cl *ControlLoop) filterJobTasks(tasks []*Task) []*Task {
	jobTaskSet := make(map[string]struct{}, len(cl.taskIDs))
	for _, id := range cl.taskIDs {
		jobTaskSet[id] = struct{}{}
	}

	filtered := make([]*Task, 0, len(cl.taskIDs))
	for _, task := range tasks {
		if _, belongs := jobTaskSet[task.ID]; belongs {
			filtered = append(filtered, task)
		}
	}
	return filtered
}

// trySpawnLeadSession attempts to spawn a persistent lead session if the
// workflow defines the lead role with persistent_sessions capability.
func (cl *ControlLoop) trySpawnLeadSession(ctx context.Context) error {
	if !cl.roleHasCapability("lead", "persistent_sessions") {
		return nil
	}

	agentSpec := cl.resolveAgentSpec("lead")

	req := &SpawnSessionRequest{
		Backend:    cl.config.LeadBackend,
		Model:      cl.config.LeadModel,
		Persistent: true,
	}

	// Apply sandbox config from the workflow agent spec.
	if agentSpec != nil {
		if agentSpec.WorkspacePath != "" {
			req.WorkDir = agentSpec.WorkspacePath
		}
		if agentSpec.SkipPermissions {
			req.ExtraArgs = append(req.ExtraArgs, "--dangerously-skip-permissions")
		}
		if agentSpec.PermissionMode != "" {
			req.ExtraArgs = append(req.ExtraArgs, "--permission-mode", agentSpec.PermissionMode)
		}
		req.Env = agentSpec.Env
		if agentSpec.Sandboxed {
			req.Sandboxed = true
			req.ContainerImage = agentSpec.ContainerImage
		}
	}

	cl.audit("lead:session_spawn", "backend="+cl.config.LeadBackend+" model="+cl.config.LeadModel)

	resp, spawnErr := cl.agentService.SpawnSession(ctx, req)
	if spawnErr != nil {
		cl.audit("lead:session_spawn_failed", "error="+spawnErr.Error())
		return spawnErr
	}

	cl.leadSessionID = resp.SessionID
	cl.audit("lead:session_spawned", "session="+resp.SessionID)
	return nil
}

// closeLeadSession terminates the lead session if one was spawned.
func (cl *ControlLoop) closeLeadSession(ctx context.Context) {
	if cl.leadSessionID == "" {
		return
	}
	_, err := cl.agentService.CloseSession(ctx, &CloseSessionRequest{
		SessionID: cl.leadSessionID,
	})
	if err != nil {
		cl.logger.Warnf("control loop: failed to close lead session: %s", err.Error())
	}
	cl.leadSessionID = ""
}

// emitEvent sends a job event to the events channel.
// Events are dropped if the channel buffer is full.
func (cl *ControlLoop) emitEvent(state JobState, message string) {
	event := JobEvent{
		JobID:     cl.jobID,
		State:     state,
		Message:   message,
		Iteration: cl.iteration,
		Timestamp: time.Now().UnixNano(),
	}
	select {
	case cl.events <- event:
	default:
		cl.logger.Warnf("control loop: event channel full, dropping event: %s", message)
	}
}

// updateStatus atomically updates the current job status.
// Recoverable state (work items, config, memory, workflow name) is included
// so that persisted status contains enough context to resume after restart.
func (cl *ControlLoop) updateStatus(state JobState) {
	status := &JobStatus{
		JobID:        cl.jobID,
		State:        state,
		Iteration:    cl.iteration,
		TaskCount:    len(cl.taskIDs),
		Description:  cl.config.Description,
		StartedAt:    cl.statusStartedAt(),
		UpdatedAt:    time.Now().UnixNano(),
		Events:       cl.auditEventsCopy(),
		WorkItems:    cl.workItems,
		Config:       cl.config,
		Memory:       cl.jobMemory,
		WorkflowName: cl.workflowName(),
	}
	cl.status.Store(status)

	// Persist to durable storage if configured (best-effort).
	if cl.persistFn != nil {
		cl.persistFn(status)
	}
}

// statusStartedAt returns the started timestamp from the current status,
// or the current time if no status exists yet.
func (cl *ControlLoop) statusStartedAt() int64 {
	current := cl.Status()
	if current != nil {
		return current.StartedAt
	}
	return time.Now().UnixNano()
}

// audit records an event in the complete audit trail and calls the audit
// function if configured. Events are always captured regardless of whether
// an audit callback is set.
func (cl *ControlLoop) audit(eventType, details string) {
	cl.auditMu.Lock()
	cl.auditEvents = append(cl.auditEvents, JobAuditEvent{
		Timestamp: time.Now().UnixNano(),
		Type:      eventType,
		Details:   details,
	})
	cl.auditMu.Unlock()
	if cl.auditFn != nil {
		cl.auditFn(eventType, details)
	}
}

// auditEventsCopy returns a snapshot of the audit trail under the audit mutex.
// The returned slice is safe to read without holding the lock.
func (cl *ControlLoop) auditEventsCopy() []JobAuditEvent {
	cl.auditMu.Lock()
	cp := make([]JobAuditEvent, len(cl.auditEvents))
	copy(cp, cl.auditEvents)
	cl.auditMu.Unlock()
	return cp
}

// workflowName returns the workflow name if a workflow is set, or the
// config's WorkflowName field as a fallback. Returns empty if neither is set.
func (cl *ControlLoop) workflowName() string {
	if cl.workflow != nil && cl.workflow.Name != "" {
		return cl.workflow.Name
	}
	if cl.config != nil {
		return cl.config.WorkflowName
	}
	return ""
}

// resolveResumeState determines the safe restart state for a job that was
// interrupted at the given state. After a server restart, all agent sessions
// are dead, so any state that depends on live sessions must restart from an
// earlier checkpoint.
func resolveResumeState(interrupted JobState) JobState {
	switch interrupted {
	case JobStateInitializing, JobStateDecomposing:
		// Early stages: restart from the beginning.
		return JobStateInitializing

	case JobStateScheduling, JobStateAssigning, JobStateExecuting,
		JobStateValidating, JobStateReviewing:
		// Sessions are dead after restart; re-decompose and re-spawn.
		// Work items and memory are preserved from persisted state.
		return JobStateDecomposing

	case JobStateRedelegating:
		// Memory is preserved; re-decompose with accumulated context.
		return JobStateDecomposing

	case JobStateEscalating:
		// Was escalating due to limits; mark as failed on resume.
		return JobStateFailed

	case JobStateCompleting, JobStateCleanup:
		// Was almost done; just complete.
		return JobStateCompleting

	default:
		return JobStateInitializing
	}
}

// =============================================================================
// Prompt Builders
// =============================================================================

// buildReviewPrompt constructs a review prompt containing all task results.
func buildReviewPrompt(description string, tasks []*Task) string {
	var sb strings.Builder
	sb.WriteString("The following tasks were completed for: ")
	sb.WriteString(description)
	sb.WriteString("\n\n")

	for i, task := range tasks {
		sb.WriteString("Task ")
		sb.WriteString(itoa(i + 1))
		sb.WriteString(": ")
		sb.WriteString(task.Subject)
		sb.WriteString("\nStatus: ")
		sb.WriteString(string(task.Status))
		sb.WriteString("\n")
		if task.Result != nil && task.Result.Output != "" {
			sb.WriteString("Result: ")
			sb.WriteString(task.Result.Output)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Do the results meet the original requirements?\n")
	sb.WriteString("Respond with JSON: {\"approved\": true/false, \"feedback\": \"...\", \"new_work_items\": [...]}")

	return sb.String()
}

// buildWorkerPrompt constructs a prompt for a worker session from a work item.
// The prompt explicitly instructs the agent to use its tools (Write, Bash, Edit)
// to create actual files rather than just describing code in text responses.
// The workspacePath tells the agent where to write files (absolute path).
func buildWorkerPrompt(item *WorkItem, workspacePath, toolGuidance string) string {
	var sb strings.Builder
	sb.WriteString("Execute the following task by using your tools to create and modify files.\n\n")
	sb.WriteString("IMPORTANT: Do NOT just describe code in your response. Use the Write tool to create files, ")
	sb.WriteString("the Bash tool to run commands, and the Edit tool to modify existing files. ")
	sb.WriteString("Actually create the files on disk.\n\n")
	if workspacePath != "" {
		sb.WriteString("Workspace directory: ")
		sb.WriteString(workspacePath)
		sb.WriteString("\nAll files MUST be written using absolute paths under this directory.\n\n")
	}
	sb.WriteString("Title: ")
	sb.WriteString(item.Title)
	sb.WriteString("\n")
	if item.Description != "" {
		sb.WriteString("Description: ")
		sb.WriteString(item.Description)
		sb.WriteString("\n")
	}
	if item.ProjectID != "" {
		sb.WriteString("Project: ")
		sb.WriteString(item.ProjectID)
		sb.WriteString("\n")
	}
	sb.WriteString("\nCreate all necessary files using the Write tool. Run any setup commands using the Bash tool. ")
	sb.WriteString("Verify your work compiles/runs by using the Bash tool.")
	if toolGuidance != "" {
		sb.WriteString("\n\n")
		sb.WriteString(toolGuidance)
	}
	return sb.String()
}

// buildCapabilitiesContext constructs a context string describing available
// backend capabilities from the workflow definition.
func buildCapabilitiesContext(wf *WorkflowDefinition) string {
	if wf == nil || len(wf.Agents) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Available Agent Capabilities\n")
	for _, agent := range wf.Agents {
		sb.WriteString("- Role: ")
		sb.WriteString(agent.Role)
		sb.WriteString(", Backend: ")
		sb.WriteString(agent.Backend)
		if len(agent.Capabilities) > 0 {
			sb.WriteString(", Capabilities: ")
			sb.WriteString(strings.Join(agent.Capabilities, ", "))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// buildValidationSummary constructs a summary of validation results for the review prompt.
func buildValidationSummary(results []ValidationResult) string {
	var sb strings.Builder
	sb.WriteString("## Validation Results\n")
	for _, r := range results {
		sb.WriteString("- ")
		sb.WriteString(r.Name)
		sb.WriteString(": ")
		if r.Passed {
			sb.WriteString("PASSED")
		} else {
			sb.WriteString("FAILED")
		}
		if r.Message != "" {
			sb.WriteString(" - ")
			sb.WriteString(r.Message)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// =============================================================================
// Response Parsing Helpers
// =============================================================================

// parseReviewResponse parses a JSON review response from the lead agent.
// LLMs sometimes return malformed JSON (e.g., new_work_items as a string instead
// of an array). We try strict parsing first, then fall back to a tolerant parser
// that extracts just approved and feedback.
func parseReviewResponse(text string) (*ReviewResponse, error) {
	jsonStr := extractJSONFromText(text)
	if jsonStr == "" {
		return nil, &ReviewParseError{
			RawResponse: text,
			Cause:       nil,
		}
	}

	var review ReviewResponse
	if err := json.Unmarshal([]byte(jsonStr), &review); err != nil {
		// Strict parse failed — try tolerant parse with raw fields.
		var tolerant struct {
			Approved bool            `json:"approved"`
			Feedback string          `json:"feedback"`
			NewItems json.RawMessage `json:"new_work_items"`
		}
		if tolerantErr := json.Unmarshal([]byte(jsonStr), &tolerant); tolerantErr != nil {
			return nil, &ReviewParseError{
				RawResponse: text,
				Cause:       err,
			}
		}
		review.Approved = tolerant.Approved
		review.Feedback = tolerant.Feedback
		// Ignore new_work_items if it's not a valid array.
	}

	return &review, nil
}

// extractJSONFromText finds the first JSON object in a text response.
func extractJSONFromText(text string) string {
	start := strings.Index(text, "{")
	if start < 0 {
		return ""
	}

	depth := 0
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}

	return ""
}

// extractPromptSessionText extracts the text content from a PromptSessionResponse.
func extractPromptSessionText(resp *PromptSessionResponse) string {
	if resp == nil {
		return ""
	}
	var sb strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" && block.Text != "" {
			sb.WriteString(block.Text)
		}
	}
	return sb.String()
}

// extractDecomposeText extracts text from a DecomposeTaskResponse work items.
func extractDecomposeText(resp *DecomposeTaskResponse) string {
	if resp == nil || len(resp.WorkItems) == 0 {
		return ""
	}
	// The decompose response is being used as a general prompt mechanism here.
	// Return the description of the first work item which contains the raw response.
	return resp.WorkItems[0].Description
}

// =============================================================================
// ID Generation
// =============================================================================

// generateJobID produces a random hex-encoded job ID using crypto/rand.
func generateJobID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// generateCorrelationID produces a random hex-encoded correlation ID using crypto/rand.
func generateCorrelationID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "corr-" + hex.EncodeToString(b), nil
}
