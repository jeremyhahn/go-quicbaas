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
	"sort"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
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
	// job is the envelope this loop drives: the identity, the lease, the
	// status, the events, the audit trail, the hooks and the sessions all
	// live there (job_run.go). The loop owns only the lead-driven shape.
	job *JobRun

	handlers          map[JobState]stateHandler
	workItems         []*WorkItem
	taskIDs           []string
	leadSessionID     string
	reviewerSessionID string // optional persistent session for the dedicated reviewer role
	reviewerBackend   string // backend name for the reviewer session, captured from workflow
	reviewerModel     string // model name for the reviewer session
	workflow          *WorkflowDefinition
	workerSessions    map[string]string         // role -> sessionID
	agentIdentities   map[string]*AgentIdentity // role -> identity
	executionPlan     *ExecutionPlan
	currentWave       int
	validationResults []ValidationResult
	reviewResponse    *ReviewResponse
	correlationID     string

	peerMessaging       PeerMessagingService                        // nil if topology is hub_and_spoke
	fileLocking         FileLockService                             // nil if not configured
	reindexFn           func(context.Context) error                 // nil if indexing not configured
	peerDeliverFn       func(context.Context, string, *PeerMessage) // nil if no delivery configured
	peerDeliveryCancels []func()                                    // cancel functions for peer delivery goroutines

	// hostAffinityStrictDefault is the operator-level default applied to
	// host_affinity hints when the per-WorkItem HostAffinityStrict bit is
	// not set. When true, an offline host_affinity causes scheduling to
	// fail with NoMatchingFleetNodeError; when false the loop falls back to any
	// eligible agent. Wired via SetHostAffinityStrictDefault.
	hostAffinityStrictDefault bool

	reviewResults []ValidationResult
}

// SetHostAffinityStrictDefault sets the operator-level enforcement default
// for host_affinity. Per-WorkItem HostAffinityStrict still wins; this is the
// fall-through default. Typically wired from
// config.OrchestrationConfig.HostAffinity.IsHard().
func (cl *ControlLoop) SetHostAffinityStrictDefault(strict bool) {
	cl.hostAffinityStrictDefault = strict
}

// NewControlLoop creates a new ControlLoop with the given agent service and
// logger. The loop is built over a standalone JobRun: a loop the envelope
// launches is handed the real run in RunJob, and one built directly — by a
// test, or by a caller driving the state machine itself — drives this one.
func NewControlLoop(svc AgentService, logger interfaces.Logger) *ControlLoop {
	cl := &ControlLoop{
		workerSessions:  make(map[string]string),
		agentIdentities: make(map[string]*AgentIdentity),
	}
	cl.adopt(newJobRun(JobRunConfig{Agents: svc, Logger: logger}))
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

// adopt binds the loop to the run it drives and installs the loop's
// contribution to every status snapshot.
func (cl *ControlLoop) adopt(run *JobRun) {
	cl.job = run
	run.Decorate(cl.decorateStatus)
}

// decorateStatus adds the lead-driven job's recoverable state to every
// snapshot the envelope writes, so a persisted status carries enough context
// to resume the loop after a restart.
func (cl *ControlLoop) decorateStatus(status *JobStatus) {
	status.TaskCount = len(cl.taskIDs)
	status.WorkItems = cl.workItems
	status.LeadSessionID = cl.leadSessionID
	status.SessionIDs = cl.sessionIDs()
	if name := cl.workflowName(); name != "" {
		status.WorkflowName = name
	}
}

// SetWorkflow sets the workflow definition for this control loop.
// Must be called before RunJob if a workflow is desired.
func (cl *ControlLoop) SetWorkflow(wf *WorkflowDefinition) {
	cl.workflow = wf
	cl.job.Workflow = wf
}

// RunJob drives the lead-driven state machine over the run the envelope
// handed it. The envelope owns the lease, the heartbeat, the terminal status
// and the discard; this is the dispatch loop and only that.
func (cl *ControlLoop) RunJob(ctx context.Context, job *JobRun) error {
	cl.adopt(job)
	if job.Workflow != nil {
		cl.workflow = job.Workflow
	}
	cl.restore(job.Resumed)

	state := job.StartState
	for !IsTerminalJobState(state) {
		handler, ok := cl.handlers[state]
		if !ok {
			err := &ControlLoopError{
				Sentinel: ErrControlLoopFailed,
				Message:  "unknown state: " + string(state),
			}
			cl.job.Fail("", err)
			return err
		}

		nextState, err := handler(ctx)
		if err != nil {
			err = cl.job.terminalError(err)
			cl.job.Emit(JobStateFailed, "error in state "+string(state)+": "+err.Error())
			cl.job.Fail("", err)
			cl.cleanupAllSessions(ctx)
			return err
		}

		state = nextState
		// SetState is the choke point: it persists the snapshot, announces
		// the change and runs the lifecycle moment the state owns, so the
		// moment runs before the state does.
		if hookErr := cl.job.SetState(ctx, state); hookErr != nil {
			return cl.failFromHook(ctx, hookErr)
		}
	}

	if state == JobStateFailed {
		// The handler that returned failed has already written the status
		// and said why; the envelope must not overwrite either.
		return &ControlLoopError{
			Sentinel: ErrControlLoopFailed,
			Message:  "job failed",
		}
	}
	return nil
}

// restore reinstates the loop's own recoverable state from the status a
// resumed job continues. The envelope restored the memory and the iteration;
// the work items are the loop's.
func (cl *ControlLoop) restore(status *JobStatus) {
	if status == nil {
		return
	}
	if len(status.WorkItems) > 0 {
		cl.workItems = status.WorkItems
	}
}

// Status returns the current job status.
func (cl *ControlLoop) Status() *JobStatus {
	return cl.job.Status()
}

// Events returns a read-only channel of job events.
func (cl *ControlLoop) Events() <-chan JobEvent {
	return cl.job.Events()
}

// =============================================================================
// State Handlers
// =============================================================================

// handleInitialize bootstraps the job: generates correlation ID, initializes
// job memory, and spawns the lead and reviewer sessions if configured.
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
	// Review criteria are judged through the engine's validator registry
	// (validators.go), which a module extends by kind.
	cl.job.memory = NewJobMemory()

	// Spawn a lead session if the backend supports persistent sessions.
	leadSessionErr := cl.trySpawnLeadSession(ctx)
	if leadSessionErr != nil {
		cl.job.Logger.Warnf("control loop: failed to spawn lead session, using stateless mode: %s", leadSessionErr.Error())
	}

	// Spawn a dedicated reviewer session if the workflow defines a reviewer
	// role with persistent_sessions capability. handleReview will route the
	// approve/reject prompt to this session instead of the lead, so the
	// reviewer is the one judging requirements and emitting NewWorkItems.
	reviewerSessionErr := cl.trySpawnReviewerSession(ctx)
	if reviewerSessionErr != nil {
		cl.job.Logger.Warnf("control loop: failed to spawn reviewer session, falling back to lead-as-reviewer: %s", reviewerSessionErr.Error())
	}

	cl.emitEvent(JobStateDecomposing, "initialization complete, correlation_id="+cl.correlationID)
	cl.audit("orchestration:job_started", "job_id="+cl.job.JobID+" correlation_id="+cl.correlationID)

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
	if cl.job.Config.ProjectID != "" {
		projectIDs = []string{cl.job.Config.ProjectID}
	}

	// Build enhanced task description with accumulated context.
	taskDescription := cl.job.Config.Description
	if cl.job.memory != nil && !cl.job.memory.IsEmpty() {
		taskDescription = taskDescription + "\n\n" + cl.job.memory.SerializeFrom(cl.job.workflowRenderer())
	}

	// Append backend capabilities context from workflow if available.
	if cl.workflow != nil {
		taskDescription = taskDescription + "\n\n" + buildCapabilitiesContext(cl.job.workflowRenderer(), cl.workflow)
	}

	cl.audit("lead:decompose_start", "backend="+cl.job.Config.LeadBackend+" model="+cl.job.Config.LeadModel)

	resp, err := cl.job.Agents.DecomposeTask(ctx, &DecomposeTaskRequest{
		Backend:    cl.job.Config.LeadBackend,
		Model:      cl.job.Config.LeadModel,
		Task:       taskDescription,
		ProjectIDs: projectIDs,
		Prompts:    cl.job.Prompts,
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
			Description: cl.job.Config.Description,
		}
	}

	cl.workItems = cl.scopeWorkItems(resp.WorkItems)

	// Extract constraints from the decomposition response and record them
	// in job memory so subsequent iterations are aware of discovered limits.
	cl.extractConstraints(resp.WorkItems)

	cl.emitEvent(JobStateScheduling, "decomposed into "+itoa(len(resp.WorkItems))+" work items")
	cl.audit("orchestration:decomposed", "work_items="+itoa(len(resp.WorkItems))+" iteration="+itoa(cl.job.iteration))

	return JobStateScheduling, nil
}

// scopeWorkItems binds every decomposed item to the project the JOB runs
// under.
//
// A work item is a lead or reviewer agent's free-form answer: the project
// reaches it only as decomposition context (DecomposeTaskRequest.ProjectIDs),
// so whatever comes back is unscoped, or scoped to whatever the model echoed.
// WorkItem.ProjectID existed and no production path ever set it (#613), which
// meant the task each item became was created unscoped — invisible to the
// project's listing and uncounted by its concurrency quota.
//
// THE JOB'S PROJECT OVERWRITES WHATEVER THE ITEM CARRIES, including a project
// the agent named itself: an agent must not be able to move work into another
// tenant's project by mentioning it. A job with no project leaves its items
// unscoped, because an unscoped job is a real state and inventing a project
// for it would key and lease work under one that does not exist.
func (cl *ControlLoop) scopeWorkItems(items []*WorkItem) []*WorkItem {
	for _, item := range items {
		if item == nil {
			continue
		}
		item.ProjectID = cl.job.Config.ProjectID
	}
	return items
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
//
// A job that is an attempt at a Task (JobConfig.TaskID) is no different
// here: it creates a Task per work item like any lead-driven job. The Task
// it is an attempt at is the accounting subject, not the unit of work.
func (cl *ControlLoop) handleAssign(ctx context.Context) (JobState, error) {
	if err := ctx.Err(); err != nil {
		return JobStateFailed, err
	}

	// Get backends for capability matching.
	backends, err := cl.job.Agents.ListBackends(ctx)
	if err != nil {
		return JobStateFailed, err
	}

	agents, err := cl.job.Agents.ListFleetNodes(ctx, cl.job.Config.TenantID, nil)
	if err != nil {
		return JobStateFailed, err
	}

	// In constrained/canned workflow mode, the fleet registry may be empty
	// because agents are defined as workflow role specs, not fleet registrations.
	// Synthesize virtual agent registrations from the workflow so that
	// matchFleetNodeForTask can resolve them.
	if len(agents) == 0 && cl.workflow != nil && cl.isConstrainedWorkflowMode() {
		agents = cl.synthesizeWorkflowAgents()
	}

	if len(agents) == 0 {
		return JobStateFailed, &NoFleetNodesAvailableError{
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
				agentSpec := cl.resolveNodeSpec(role)
				spawnReq := &SpawnSessionRequest{
					Backend:     backendName,
					Model:       cl.resolveModelForRole(role),
					ProjectID:   cl.job.Config.ProjectID,
					JobID:       cl.job.JobID,
					MCPServers:  mcpServers,
					Persistent:  cl.roleHasCapability(role, "persistent_sessions"),
					GpuRequired: workItemRequiresGPU(item),
				}

				// Apply sandbox configuration from workflow agent spec.
				if agentSpec != nil {
					spawnReq.WorkDir = cl.spawnWorkDir(agentSpec)
					spawnReq.SystemPrompt = agentSpec.SystemPrompt
					spawnReq.PromptAppend = agentSpec.PromptAppend
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
					// Host affinity: pin this session to a specific daemon.
					if agentSpec.HostAffinity != "" {
						spawnReq.HostAffinity = agentSpec.HostAffinity
						// Copy onto the work item so matchFleetNodeForTask can
						// filter fleet candidates by the same affinity.
						item.HostAffinity = agentSpec.HostAffinity
					}
				}
				if spawnReq.WorkDir == "" {
					spawnReq.WorkDir = cl.job.workDir
				}
				persistStr := "false"
				if spawnReq.Persistent {
					persistStr = "true"
				}
				cl.audit("worker:session_spawn", "role="+role+" backend="+backendName+" persistent="+persistStr)
				spawnResp, spawnErr := cl.job.Spawn(ctx, spawnReq)
				if spawnErr != nil {
					cl.audit("worker:session_spawn_failed", "role="+role+" backend="+backendName+" error="+spawnErr.Error())
					cl.job.Logger.Warnf("control loop: failed to spawn worker session for role=%s backend=%s: %s",
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
				AgentID:  "agent-" + role + "-" + cl.job.JobID,
				TenantID: cl.job.Config.TenantID,
				Role:     role,
				Scopes:   scopes,
			}
			cl.agentIdentities[role] = identity
		}

		// Match an agent for task assignment.
		agentID, _, matchErr := cl.matchFleetNodeForTask(item, agents, backends)
		if matchErr != nil {
			return JobStateFailed, matchErr
		}
		if agentID == "" {
			return JobStateFailed, &NoFleetNodesAvailableError{
				TaskTitle:            item.Title,
				RequiredCapabilities: item.RequiredCapabilities,
			}
		}

		task, createErr := cl.job.Agents.CreateTask(ctx, &CreateTaskRequest{
			TenantID:    cl.job.Config.TenantID,
			ProjectID:   item.ProjectID,
			Subject:     item.Title,
			Description: item.Description,
			AssignedTo:  agentID,
		})
		if createErr != nil {
			return JobStateFailed, createErr
		}

		// Claim the task so the state machine allows completion (pending -> claimed).
		if claimErr := cl.job.Agents.ClaimTask(ctx, cl.job.Config.TenantID, task.ID, agentID); claimErr != nil {
			cl.job.Logger.Warnf("control loop: failed to claim task %s for agent %s: %s",
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

					agentSpec := cl.resolveNodeSpec(iw.item.RequiredRole)
					workspacePath := cl.job.workDir
					if agentSpec != nil && agentSpec.WorkspacePath != "" {
						// For sandboxed agents, use /workspace (container mount point)
						// instead of the host path.
						if agentSpec.Sandboxed {
							workspacePath = "/workspace"
						} else {
							workspacePath = agentSpec.WorkspacePath
						}
					}
					toolGuidance := cl.job.Agents.BackendToolGuidance(iw.item.Backend)
					workerPrompt := buildWorkerPrompt(cl.job.workflowRenderer(), iw.item, workspacePath, toolGuidance)
					turnCtx := WithTurnAttribution(ctx, cl.turnAttribution(promptRole))
					resp, promptErr := cl.job.Agents.PromptSession(turnCtx, &PromptSessionRequest{
						SessionID: sessionID,
						Prompt:    workerPrompt,
						EventCallback: func(ev *SessionEvent) {
							if ev == nil {
								return
							}
							cl.audit(string(ev.Kind), "session="+ev.SessionID+" "+sessionEventSummary(ev))
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
					cl.job.memory.AddFailure(
						waveItems[r.index].Title,
						waveItems[r.index].Description,
						r.err.Error(),
						cl.job.iteration,
					)
					cl.job.Logger.Warnf("control loop: worker prompt failed for task %s: %s",
						cl.taskIDs[r.index], r.err.Error())
				} else {
					// Record success in job memory.
					cl.job.memory.AddSuccess(waveItems[r.index].Title + ": " + waveItems[r.index].Description)

					// Complete the task with the response.
					cl.recordItemResult(ctx, r.index, r.output)

					// A worker that answered is a worker that may have
					// changed the checkout, and on an ephemeral workspace
					// that work only exists until the container goes. The
					// push runs HERE, in the collector, rather than in the
					// per-session goroutines: publication is serialized per
					// job, exactly as a person pushing would be. A run with
					// no pusher installed — which is every lead-driven job
					// today — does nothing at all.
					if pushErr := cl.job.PushTurn(ctx); pushErr != nil {
						return JobStateFailed, pushErr
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
			cl.job.Escalate("budget exceeded")
			cl.emitEvent(JobStateEscalating, "budget exceeded, pausing execution")
			return JobStateEscalating, nil
		}
	}

	// Check if all tasks were handled directly (no need to poll).
	if len(directHandled) == len(cl.taskIDs) {
		return cl.advanceOrValidate()
	}

	// Poll for remaining task completion (stateless backends).
	ticker := time.NewTicker(cl.job.Config.PollInterval)
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

// handleValidate runs the validating-phase review criteria against the
// completed work, each through the validator registered for its kind.
func (cl *ControlLoop) handleValidate(ctx context.Context) (JobState, error) {
	if err := ctx.Err(); err != nil {
		return JobStateFailed, err
	}

	// Collect task results for validation.
	job := cl.jobContext()
	job.Artifacts = cl.collectTaskResults(ctx)

	// Run validator pipeline if review criteria exist.
	var criteria []ReviewCriterion
	if cl.workflow != nil {
		criteria = criteriaForPhase(cl.workflow.Settings.ReviewCriteria, CriterionPhaseValidating)
	}

	if len(criteria) > 0 {
		results, allPassed, pipelineErr := cl.validators().RunPipeline(ctx, job, criteria)
		if pipelineErr != nil {
			cl.job.Logger.Warnf("control loop: validator pipeline error: %s", pipelineErr.Error())
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
			cl.job.Logger.Warnf("control loop: reindex trigger failed: %s", reindexErr.Error())
		}
	}

	return JobStateReviewing, nil
}

// handleReview judges the work. When the workflow has reviewing-phase
// criteria they are the review: each is dispatched by kind through the
// validator registry and their verdict decides approval. Otherwise the
// engine's own agent review runs — a review prompt built from task results
// and validation outcomes, sent to the reviewer or lead session, its
// structured response parsed. The communication policy is consulted to
// determine blocking behavior for completion signals.
func (cl *ControlLoop) handleReview(ctx context.Context) (JobState, error) {
	if err := ctx.Err(); err != nil {
		return JobStateFailed, err
	}

	if cl.workflow != nil {
		if criteria := criteriaForPhase(cl.workflow.Settings.ReviewCriteria, CriterionPhaseReviewing); len(criteria) > 0 {
			review, reviewErr := cl.reviewByCriteria(ctx, criteria)
			if reviewErr != nil {
				return JobStateFailed, reviewErr
			}
			return cl.concludeReview(review), nil
		}
	}

	// Consult communication policy to determine blocking behavior for review.
	// Worker completion signals use blocking policy so the lead waits for results.
	policy, _ := GetPolicy("worker", "complete")
	cl.job.Logger.Debugf("control loop: review phase using %s policy for worker completion", string(policy))

	// Fetch the tasks the review prompt describes.
	jobTasks, err := cl.reviewTasks(ctx)
	if err != nil {
		return JobStateFailed, err
	}

	reviewPrompt := buildReviewPrompt(cl.job.workflowRenderer(), cl.job.Config.Description, jobTasks)

	// Append validation results to the review prompt if available.
	if len(cl.validationResults) > 0 {
		reviewPrompt = reviewPrompt + "\n\n" + buildValidationSummary(cl.job.workflowRenderer(), cl.validationResults)
	}

	// Route the review prompt to the dedicated reviewer session when one was
	// spawned for this job (workflow defines a "reviewer" role with the
	// persistent_sessions capability). Falls back to the lead session for
	// backward compatibility when no reviewer is configured, then to a
	// stateless DecomposeTask prompt as a last resort.
	reviewerSessionID := cl.reviewerSessionID
	reviewerBackend := cl.reviewerBackend
	reviewerModel := cl.reviewerModel
	systemPrompt := reviewerSystemPrompt(cl.job.workflowRenderer())

	// CLAUDE REVIEWS NOTHING. Owner ruling 2026-09-13: a review runs on codex
	// or on the local qwen model, and on nothing else.
	//
	// The check is here, on the RESOLVED backend, because that is the one place
	// all three routes below meet. Each of them could reach a Claude Code
	// session by a different road — a workflow that names claude for its
	// reviewer role, or either of the lead fallbacks, which take the lead's
	// backend when no reviewer is configured — and a rule enforced at the
	// configuration sites would be a rule with three holes in it.
	//
	// It refuses rather than substituting a different backend. Silently moving
	// a review to another model would produce a verdict the operator did not
	// ask for and cannot see; the job stops and says what to configure.
	if backend := cl.resolvedReviewBackend(); claudeCodeBackends[backend] {
		return JobStateFailed, &ReviewBackendRefusedError{
			JobID:   cl.job.JobID,
			Backend: backend,
		}
	}

	var responseText string
	switch {
	case reviewerSessionID != "":
		cl.audit("reviewer:review_prompt", "session="+reviewerSessionID)
		resp, promptErr := cl.job.Agents.PromptSession(WithTurnAttribution(ctx, cl.turnAttribution("reviewer")), &PromptSessionRequest{
			SessionID:    reviewerSessionID,
			Prompt:       reviewPrompt,
			SystemPrompt: systemPrompt,
		})
		if promptErr != nil {
			return JobStateFailed, promptErr
		}
		responseText = extractPromptSessionText(resp)
	case cl.leadSessionID != "":
		resp, promptErr := cl.job.Agents.PromptSession(WithTurnAttribution(ctx, cl.turnAttribution("lead")), &PromptSessionRequest{
			SessionID:    cl.leadSessionID,
			Prompt:       reviewPrompt,
			SystemPrompt: systemPrompt,
		})
		if promptErr != nil {
			return JobStateFailed, promptErr
		}
		responseText = extractPromptSessionText(resp)
	default:
		backend := reviewerBackend
		if backend == "" {
			backend = cl.job.Config.LeadBackend
		}
		model := reviewerModel
		if model == "" {
			model = cl.job.Config.LeadModel
		}
		resp, promptErr := cl.job.Agents.DecomposeTask(ctx, &DecomposeTaskRequest{
			Backend: backend,
			Model:   model,
			Task:    reviewPrompt,
			Prompts: cl.job.Prompts,
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

	return cl.concludeReview(review), nil
}

// concludeReview records the verdict and picks the next state: completing on
// approval, re-delegating otherwise. A lead-driven job takes the reviewer's
// new work items as the next decomposition's starting point.
func (cl *ControlLoop) concludeReview(review *ReviewResponse) JobState {
	cl.reviewResponse = review

	if review.Approved {
		cl.emitEvent(JobStateCompleting, "review approved: "+review.Feedback)
		cl.audit("orchestration:review_decision", "approved=true feedback="+review.Feedback)
		return JobStateCompleting
	}

	// Not approved: store new work items for re-delegation. They are the
	// reviewer's free-form answer and are scoped exactly as the lead's are.
	if len(review.NewWorkItems) > 0 {
		cl.workItems = cl.scopeWorkItems(review.NewWorkItems)
	}
	cl.emitEvent(JobStateRedelegating, "review rejected: "+review.Feedback)
	cl.audit("orchestration:review_decision", "approved=false feedback="+review.Feedback)

	return JobStateRedelegating
}

// handleComplete finalizes the job results and emits a completion event.
func (cl *ControlLoop) handleComplete(ctx context.Context) (JobState, error) {
	if err := ctx.Err(); err != nil {
		return JobStateFailed, err
	}

	cl.emitEvent(JobStateCleanup, "job completing, starting cleanup")
	cl.audit("orchestration:job_completing", "job_id="+cl.job.JobID+" iteration="+itoa(cl.job.iteration))

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
	summary := "job_id=" + cl.job.JobID +
		" iterations=" + itoa(cl.job.iteration) +
		" work_items=" + itoa(len(cl.workItems))
	if status := cl.Status(); status != nil {
		summary += " tasks=" + itoa(status.TaskCount) +
			" completed=" + itoa(status.Completed) +
			" failed=" + itoa(status.Failed)
	}
	cl.audit("orchestration:job_summary", summary)

	cl.emitEvent(JobStateCompleted, "job completed successfully")
	cl.audit("orchestration:job_completed", "job_id="+cl.job.JobID)

	return JobStateCompleted, nil
}

// handleRedelegate adds review feedback to job memory, records failed items,
// checks the iteration limit, and starts the next round: the job
// re-decomposes with the accumulated memory in the prompt.
func (cl *ControlLoop) handleRedelegate(ctx context.Context) (JobState, error) {
	if err := ctx.Err(); err != nil {
		return JobStateFailed, err
	}

	cl.job.iteration++

	// Add review feedback to job memory.
	if cl.reviewResponse != nil && cl.reviewResponse.Feedback != "" {
		cl.job.memory.AddFeedback(cl.reviewResponse.Feedback)
	}

	// Record failed items in job memory.
	if cl.reviewResponse != nil && len(cl.reviewResponse.NewWorkItems) > 0 {
		for _, item := range cl.reviewResponse.NewWorkItems {
			cl.job.memory.AddFailure(item.Title, item.Description, "rejected in review", cl.job.iteration)
		}
	}

	// Check iteration limit.
	maxIterations := cl.job.Config.MaxIterations
	if cl.workflow != nil && cl.workflow.Settings.MaxIterations > 0 {
		maxIterations = cl.workflow.Settings.MaxIterations
	}
	if maxIterations > 0 && cl.job.iteration >= maxIterations {
		cl.job.Escalate("max iterations exceeded: " + itoa(cl.job.iteration))
		cl.emitEvent(JobStateEscalating, cl.job.EscalationReason())
		return JobStateEscalating, nil
	}

	// Clear old task IDs and work items for the new iteration.
	// Re-decomposition will generate fresh work items with new IDs.
	cl.taskIDs = nil
	cl.workItems = nil
	cl.executionPlan = nil
	cl.currentWave = 0

	cl.emitEvent(JobStateDecomposing, "re-delegating iteration "+itoa(cl.job.iteration))
	cl.audit("orchestration:redelegate", "iteration="+itoa(cl.job.iteration))

	return JobStateDecomposing, nil
}

// handleEscalate emits an escalation event with full context and transitions to failed.
func (cl *ControlLoop) handleEscalate(ctx context.Context) (JobState, error) {
	reason := cl.job.EscalationReason()
	if reason == "" {
		reason = "max iterations exceeded"
	}
	cl.emitEvent(JobStateFailed, "job escalated: "+reason+" after "+itoa(cl.job.iteration)+" iterations")
	cl.audit("orchestration:job_escalated", "iterations="+itoa(cl.job.iteration)+" reason="+reason)

	// Clean up before failing.
	cl.cleanupAllSessions(ctx)

	return JobStateFailed, &EscalationError{
		JobID:      cl.job.JobID,
		Iterations: cl.job.iteration,
		Reason:     reason,
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

	for _, sessionID := range cl.workerSessions {
		cl.job.Close(ctx, sessionID)
	}
	cl.workerSessions = make(map[string]string)

	cl.closeLeadSession(ctx)
	cl.closeReviewerSession(ctx)
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
		return cl.job.Config.LeadBackend

	case WorkflowModeAutonomous:
		// Autonomous mode: allow dynamic backend from work item specification.
		if item.Backend != "" {
			// Verify the backend exists before using it.
			for _, b := range backends {
				if b.Name == item.Backend {
					return item.Backend
				}
			}
			cl.job.Logger.Warnf("control loop: autonomous mode requested backend %q not found, falling back", item.Backend)
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

// synthesizeWorkflowAgents creates virtual FleetNode entries from the
// workflow's AgentRoleSpec entries. This allows matchFleetNodeForTask to resolve
// agents when the fleet registry is empty but the workflow defines agent roles.
// The lead role is excluded because it handles decomposition, not work execution.
func (cl *ControlLoop) synthesizeWorkflowAgents() []*FleetNode {
	agents := make([]*FleetNode, 0, len(cl.workflow.Agents))
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

			agents = append(agents, &FleetNode{
				ID:           agentID,
				TenantID:     cl.job.Config.TenantID,
				Name:         spec.Role,
				Backend:      spec.Backend,
				Model:        spec.Model,
				NodeType:     "workflow",
				Status:       FleetNodeStatusIdle,
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

// turnAttribution names the job and agent a turn run for the given role is
// charged to, so the orchestrator can record the turn's token usage against
// this job's budget instead of leaving it unattributed. The identity is the
// one minted in handleAssign; a role that never got one still charges the job,
// under a role-derived agent ID, so a job total is never silently incomplete.
func (cl *ControlLoop) turnAttribution(role string) TurnAttribution {
	if role == "" {
		role = "worker"
	}
	taskID := ""
	if cl.job.Config != nil {
		taskID = cl.job.Config.TaskID
	}
	// The job's kind travels with the agent, so a control-loop turn is
	// broken down by runner like every other (go-quicbaas#1058).
	kind := cl.job.Runner
	if kind == "" {
		kind = JobRunnerWorkflow
	}
	if identity, ok := cl.agentIdentities[role]; ok && identity != nil && identity.AgentID != "" {
		return TurnAttribution{JobID: cl.job.JobID, TaskID: taskID, AgentID: identity.AgentID, Kind: kind}
	}
	return TurnAttribution{JobID: cl.job.JobID, TaskID: taskID, AgentID: "agent-" + role + "-" + cl.job.JobID, Kind: kind}
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

	return cl.job.Config.LeadBackend
}

// spawnWorkDir is the directory a session is spawned into: the one Prepare
// returned when the workflow has hooks, else the agent spec's workspace.
func (cl *ControlLoop) spawnWorkDir(spec *AgentRoleSpec) string {
	if cl.job.workDir != "" {
		return cl.job.workDir
	}
	if spec != nil {
		return spec.WorkspacePath
	}
	return ""
}

// resolveModelForRole determines the model to use for a given role.
// resolveNodeSpec returns the AgentRoleSpec for the given role from the
// workflow definition, or nil if no workflow is set or the role is not found.
func (cl *ControlLoop) resolveNodeSpec(role string) *AgentRoleSpec {
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
	return cl.job.Config.LeadModel
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
	if cl.job.memory == nil {
		return
	}
	for _, item := range items {
		lines := strings.Split(item.Description, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToLower(trimmed), "constraint:") {
				constraint := strings.TrimSpace(trimmed[len("constraint:"):])
				if constraint != "" {
					cl.job.memory.AddConstraint(constraint)
				}
			}
		}
	}
}

// collectTaskResults fetches all tasks for this job and extracts their results.
func (cl *ControlLoop) collectTaskResults(ctx context.Context) []*TaskResult {
	tasks, err := cl.job.Agents.ListTasks(ctx, cl.job.Config.TenantID, nil)
	if err != nil {
		cl.job.Logger.Warnf("control loop: failed to list tasks for validation: %s", err.Error())
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
	tasks, err := cl.job.Agents.ListTasks(ctx, cl.job.Config.TenantID, nil)
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
		cl.job.Logger.Debugf("control loop: tasks done: completed=%d failed=%d total=%d", completed, failed, total)
	}

	return done, nil
}

// matchFleetNodeForTask finds the best agent for a work item based on capabilities.
// Filtering is applied in priority order: backend → GPU requirement → host
// affinity → persistent sessions → best available.
//
// matchErr is non-nil only when host_affinity is strict (per-item override or
// operator-level default) AND no online agent matches the named host. In
// that case the caller must surface the typed error rather than substitute
// a generic NoFleetNodesAvailableError, so operators see "host X is offline"
// instead of "no agents".
func (cl *ControlLoop) matchFleetNodeForTask(
	item *WorkItem,
	agents []*FleetNode,
	backends []*AgentBackendInfo,
) (agentID string, backendName string, matchErr error) {
	// Build a backend capabilities lookup map.
	backendCaps := make(map[string]BackendCapabilities, len(backends))
	for _, b := range backends {
		backendCaps[b.Name] = b.Capabilities
	}

	// Apply GPU filter when the work item requires GPU acceleration.
	// This narrows the candidate set before any other filtering so that
	// subsequent selectors only consider GPU-capable agents.
	gpuRequired := workItemRequiresGPU(item)
	candidates := agents
	if gpuRequired {
		candidates = filterFleetNodesByCapability(agents, "gpu")
	}

	// Apply host-affinity filter: restrict candidates to agents whose Name
	// or Host field matches the requested affinity. When strict (per-item
	// override OR operator default), an empty result becomes a typed
	// NoMatchingFleetNodeError so the loop can fail loudly rather than silently
	// fall back to any eligible agent.
	if item.HostAffinity != "" {
		strict := item.HostAffinityStrict || cl.hostAffinityStrictDefault
		filtered, ok := filterFleetNodesByHostAffinity(candidates, item.HostAffinity, strict)
		if !ok {
			return "", "", &NoMatchingFleetNodeError{
				Constraint: "host_affinity",
				Detail:     "host " + item.HostAffinity + " is offline",
			}
		}
		candidates = filtered
	}

	// If the work item specifies a backend, find an agent using that backend.
	// When a backend is requested and no candidate agent uses it, return a
	// typed NoMatchingFleetNodeError so callers can surface a meaningful diagnostic
	// instead of silently dispatching to an incompatible agent.
	if item.Backend != "" {
		id, name := cl.findFleetNodeByBackend(item.Backend, candidates)
		if id == "" {
			return "", "", &NoMatchingFleetNodeError{
				Constraint: "backend",
				Detail:     "no online agent uses backend " + item.Backend,
			}
		}
		return id, name, nil
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
		id, name := cl.findAgentWithPersistentSessions(candidates, backendCaps)
		return id, name, nil
	}

	// Default: prefer idle agents, then any available.
	id, name := cl.findBestAvailableFleetNode(candidates)
	return id, name, nil
}

// workItemRequiresGPU reports whether the work item's RequiredCapabilities
// slice contains "gpu".
func workItemRequiresGPU(item *WorkItem) bool {
	if item == nil {
		return false
	}
	for _, cap := range item.RequiredCapabilities {
		if cap == "gpu" {
			return true
		}
	}
	return false
}

// filterFleetNodesByHostAffinity returns agents whose Name or Host field matches
// the requested affinity string. The comparison is case-sensitive and exact.
//
// The ok return distinguishes the two failure modes:
//   - strict=false, no match: returns (agents, true) — soft fallback to the
//     full candidate list, preserving existing behavior.
//   - strict=true, no match: returns (nil, false) — caller MUST surface a
//     typed NoMatchingFleetNodeError instead of dispatching to a random agent.
//
// When at least one agent matches, returns (filtered, true) regardless of
// strictness; strictness only changes the empty-set behavior.
func filterFleetNodesByHostAffinity(agents []*FleetNode, affinity string, strict bool) (out []*FleetNode, ok bool) {
	out = make([]*FleetNode, 0, len(agents))
	for _, a := range agents {
		if a == nil {
			continue
		}
		if a.Name == affinity || a.Host == affinity {
			out = append(out, a)
		}
	}
	if len(out) == 0 {
		if strict {
			return nil, false
		}
		return agents, true
	}
	return out, true
}

// filterFleetNodesByCapability returns the subset of agents whose Capabilities
// slice contains the named capability.
func filterFleetNodesByCapability(agents []*FleetNode, capability string) []*FleetNode {
	out := make([]*FleetNode, 0, len(agents))
	for _, a := range agents {
		if a == nil {
			continue
		}
		for _, c := range a.Capabilities {
			if c == capability {
				out = append(out, a)
				break
			}
		}
	}
	return out
}

// findFleetNodeByBackend finds the best agent that uses the specified backend.
// Matches against either agent.Backend (primary) or agent.Capabilities (all
// registered backend kinds), since the embedded agent and remote daemons can
// expose multiple backends (e.g. ollama + claude + codex).
func (cl *ControlLoop) findFleetNodeByBackend(
	backend string,
	agents []*FleetNode,
) (agentID string, backendName string) {
	var fallback *FleetNode
	for _, agent := range agents {
		if !FleetNodeServesBackend(agent, backend) {
			continue
		}
		if agent.Status == FleetNodeStatusIdle {
			return agent.ID, backend
		}
		if fallback == nil {
			fallback = agent
		}
	}
	if fallback != nil {
		return fallback.ID, backend
	}
	return "", ""
}

// findAgentWithPersistentSessions finds an agent whose backend supports persistent sessions.
func (cl *ControlLoop) findAgentWithPersistentSessions(
	agents []*FleetNode,
	backendCaps map[string]BackendCapabilities,
) (agentID string, backendName string) {
	var fallback *FleetNode
	for _, agent := range agents {
		caps, exists := backendCaps[agent.Backend]
		if !exists || !caps.PersistentSessions {
			continue
		}
		if agent.Status == FleetNodeStatusIdle {
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

// findBestAvailableFleetNode returns the best available agent, preferring idle status.
func (cl *ControlLoop) findBestAvailableFleetNode(
	agents []*FleetNode,
) (agentID string, backendName string) {
	var fallback *FleetNode
	for _, agent := range agents {
		if agent.Status == FleetNodeStatusIdle {
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

	agentSpec := cl.resolveNodeSpec("lead")

	req := &SpawnSessionRequest{
		JobID:      cl.job.JobID,
		Persistent: true,
		Role:       AgentRoleLead,
	}
	// A LEAD NAMED BY REFERENCE IS SPAWNED BY REFERENCE, so the session fails
	// over along the pool at the moment it starts rather than being fixed to
	// whatever the job resolved when it was queued.
	if lead := cl.job.Config.Lead; !lead.IsZero() {
		req.AgentID, req.PoolID = lead.AgentID, lead.PoolID
	} else {
		req.Backend, req.Model = cl.job.Config.LeadBackend, cl.job.Config.LeadModel
	}

	// Apply sandbox config from the workflow agent spec.
	req.WorkDir = cl.spawnWorkDir(agentSpec)
	if agentSpec != nil {
		req.PromptAppend = agentSpec.PromptAppend
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

	cl.audit("lead:session_spawn", "backend="+cl.job.Config.LeadBackend+" model="+cl.job.Config.LeadModel)

	resp, spawnErr := cl.job.Spawn(ctx, req)
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
	cl.job.Close(ctx, cl.leadSessionID)
	cl.leadSessionID = ""
}

// trySpawnReviewerSession attempts to spawn a persistent reviewer session if
// the workflow defines a "reviewer" role with the persistent_sessions
// capability. The reviewer session receives the approve/reject prompt in
// handleReview, so the reviewer (typically a codex agent) is the de facto
// judge of whether all job requirements have been met. Returns nil when the
// workflow does not configure a reviewer; the lead is then used as fallback
// reviewer for backward compatibility.
func (cl *ControlLoop) trySpawnReviewerSession(ctx context.Context) error {
	if !cl.roleHasCapability("reviewer", "persistent_sessions") {
		return nil
	}

	agentSpec := cl.resolveNodeSpec("reviewer")
	if agentSpec == nil {
		return nil
	}

	cl.reviewerBackend = agentSpec.Backend
	cl.reviewerModel = agentSpec.Model

	req := &SpawnSessionRequest{
		Backend:      agentSpec.Backend,
		Model:        agentSpec.Model,
		JobID:        cl.job.JobID,
		Persistent:   true,
		PromptAppend: agentSpec.PromptAppend,
	}

	req.WorkDir = cl.spawnWorkDir(agentSpec)
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

	cl.audit("reviewer:session_spawn", "backend="+agentSpec.Backend+" model="+agentSpec.Model)

	resp, spawnErr := cl.job.Spawn(ctx, req)
	if spawnErr != nil {
		cl.audit("reviewer:session_spawn_failed", "error="+spawnErr.Error())
		return spawnErr
	}

	cl.reviewerSessionID = resp.SessionID
	cl.audit("reviewer:session_spawned", "session="+resp.SessionID)
	return nil
}

// closeReviewerSession terminates the reviewer session if one was spawned.
func (cl *ControlLoop) closeReviewerSession(ctx context.Context) {
	if cl.reviewerSessionID == "" {
		return
	}
	cl.job.Close(ctx, cl.reviewerSessionID)
	cl.reviewerSessionID = ""
}

// emitEvent announces a transition of the loop's state on the run's event
// stream.
func (cl *ControlLoop) emitEvent(state JobState, message string) {
	cl.job.Emit(state, message)
}

// updateStatus writes the loop's status snapshot for state through the run,
// which decorates it with the loop's recoverable state and persists it.
// The state machine itself moves through JobRun.SetState; this is the write
// without the lifecycle moment, for the places that record a status the loop
// is not entering.
func (cl *ControlLoop) updateStatus(state JobState) {
	cl.job.snapshot(state)
}

// sessionIDs lists every session the loop currently holds — the lead, the
// reviewer and each worker role — in a stable order: lead first, reviewer
// second, then workers by role name. Roles are sorted so two status snapshots
// of the same sessions serialize identically.
func (cl *ControlLoop) sessionIDs() []string {
	ids := make([]string, 0, 2+len(cl.workerSessions))
	if cl.leadSessionID != "" {
		ids = append(ids, cl.leadSessionID)
	}
	if cl.reviewerSessionID != "" {
		ids = append(ids, cl.reviewerSessionID)
	}
	roles := make([]string, 0, len(cl.workerSessions))
	for role := range cl.workerSessions {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	for _, role := range roles {
		if id := cl.workerSessions[role]; id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

// audit records an event in the complete audit trail and calls the audit
// function if configured. Events are always captured regardless of whether
// an audit callback is set.
func (cl *ControlLoop) audit(eventType, details string) {
	cl.job.Audit(eventType, details)
}

// sessionEventSummaryMax caps the audit-trail rendering of a session event's
// payload so a long assistant answer or tool dump stays one scannable line.
const sessionEventSummaryMax = 200

// sessionEventSummary renders a one-line, truncated description of a session
// event's payload for the job audit trail — the prompt text, the chunk text,
// the tool name — never a raw struct or JSON dump.
func sessionEventSummary(ev *SessionEvent) string {
	var detail string
	switch ev.Kind {
	case KindPromptSubmitted:
		if ev.Prompt != nil {
			detail = ev.Prompt.Prompt
		}
	case KindAssistantChunk:
		if ev.AssistantChunk != nil {
			detail = ev.AssistantChunk.Text
		}
	case KindAgentThought:
		if ev.Thought != nil {
			detail = ev.Thought.Text
		}
	case KindToolInvocation:
		if ev.ToolInvocation != nil {
			detail = "tool=" + ev.ToolInvocation.Name
		}
	case KindToolResult:
		if ev.ToolResult != nil {
			detail = "invocation=" + ev.ToolResult.InvocationID
			if ev.ToolResult.IsError {
				detail += " error=true"
			}
		}
	case KindSystemMessage:
		if ev.System != nil {
			detail = ev.System.Code + " " + ev.System.Message
		}
	}
	if len(detail) > sessionEventSummaryMax {
		detail = detail[:sessionEventSummaryMax] + "..."
	}
	return detail
}

// workflowName returns the workflow name if a workflow is set, or the
// config's WorkflowName field as a fallback. Returns empty if neither is set.
func (cl *ControlLoop) workflowName() string {
	if cl.workflow != nil && cl.workflow.Name != "" {
		return cl.workflow.Name
	}
	if cl.job.Config != nil {
		return cl.job.Config.WorkflowName
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

// buildReviewPrompt assembles the review prompt from the workflow set: the
// job's goal, then one block per task.
func buildReviewPrompt(set prompts.Renderer, description string, tasks []*Task) string {
	var tasksText strings.Builder
	for i, task := range tasks {
		tasksText.WriteString(set.MustRender("reviewer_prompt_task", prompts.Vars{
			"index":   itoa(i + 1),
			"subject": task.Subject,
			"status":  string(task.Status),
		}))
		tasksText.WriteString("\n")
		if task.Result != nil && task.Result.Output != "" {
			tasksText.WriteString(set.MustRender("reviewer_prompt_result", prompts.Vars{
				"output": task.Result.Output,
			}))
			tasksText.WriteString("\n")
		}
		tasksText.WriteString("\n")
	}
	return set.MustRender("reviewer_prompt", prompts.Vars{
		"description": description,
		"tasks":       tasksText.String(),
	})
}

// reviewerSystemPrompt is the reviewer session's standing instructions.
func reviewerSystemPrompt(set prompts.Renderer) string {
	return set.MustRender("reviewer_system", nil)
}

// buildWorkerPrompt assembles the instruction one work item is handed to a
// worker session. The clauses that appear only sometimes — the workspace, the
// description, the project, the backend's tool guidance — are prompts of
// their own, spliced in where they belong and empty where they do not.
func buildWorkerPrompt(set prompts.Renderer, item *WorkItem, workspacePath, toolGuidance string) string {
	workspace := ""
	if workspacePath != "" {
		workspace = set.MustRender("worker_workspace", prompts.Vars{
			"workspace_path": workspacePath,
		}) + "\n\n"
	}
	description := ""
	if item.Description != "" {
		description = set.MustRender("worker_description", prompts.Vars{
			"description": item.Description,
		}) + "\n"
	}
	project := ""
	if item.ProjectID != "" {
		project = set.MustRender("worker_project", prompts.Vars{
			"project_id": item.ProjectID,
		}) + "\n"
	}
	guidance := ""
	if toolGuidance != "" {
		guidance = "\n\n" + toolGuidance
	}
	return set.MustRender("worker_prompt", prompts.Vars{
		"workspace":     workspace,
		"title":         item.Title,
		"description":   description,
		"project":       project,
		"tool_guidance": guidance,
	})
}

// buildCapabilitiesContext constructs a context string describing available
// backend capabilities from the workflow definition.
func buildCapabilitiesContext(set prompts.Renderer, wf *WorkflowDefinition) string {
	if wf == nil || len(wf.Agents) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(set.MustRender("capabilities_context", nil))
	sb.WriteString("\n")
	for _, agent := range wf.Agents {
		capabilities := ""
		if len(agent.Capabilities) > 0 {
			capabilities = ", Capabilities: " + strings.Join(agent.Capabilities, ", ")
		}
		sb.WriteString(set.MustRender("capabilities_agent", prompts.Vars{
			"role":         agent.Role,
			"backend":      agent.Backend,
			"capabilities": capabilities,
		}))
		sb.WriteString("\n")
	}
	return sb.String()
}

// buildValidationSummary constructs a summary of validation results for the review prompt.
func buildValidationSummary(set prompts.Renderer, results []ValidationResult) string {
	var sb strings.Builder
	sb.WriteString(set.MustRender("validation_summary", nil))
	sb.WriteString("\n")
	for _, r := range results {
		message := ""
		if r.Message != "" {
			message = " - " + r.Message
		}
		sb.WriteString(set.MustRender("validation_summary_row", prompts.Vars{
			"name":    r.Name,
			"verdict": validationVerdicts[r.Passed],
			"message": message,
		}))
		sb.WriteString("\n")
	}
	return sb.String()
}

// validationVerdicts is the word each verdict is written with. It is
// vocabulary the summary's reader matches on, not prose, so it stays here
// rather than becoming an editable prompt.
var validationVerdicts = map[bool]string{true: "PASSED", false: "FAILED"}

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

// extractJSONFromText finds the first JSON object in a text response. Braces
// inside string literals are content, not structure — see extractBalanced.
func extractJSONFromText(text string) string {
	return extractBalanced(text, '{', '}')
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
