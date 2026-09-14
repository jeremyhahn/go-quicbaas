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
	"errors"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Sentinel Errors
// =============================================================================

var (
	// ErrHookBudgetCheck indicates a budget check hook failure.
	ErrHookBudgetCheck = errors.New("hook: budget check failed")

	// ErrHookRecoveryBootstrap indicates a recovery bootstrap hook failure.
	ErrHookRecoveryBootstrap = errors.New("hook: recovery bootstrap failed")

	// ErrHookPeerDelivery indicates a peer delivery hook failure.
	ErrHookPeerDelivery = errors.New("hook: peer delivery failed")

	// ErrHookReindex indicates a reindex hook failure.
	ErrHookReindex = errors.New("hook: reindex failed")
)

// =============================================================================
// Typed Errors
// =============================================================================

// HookBudgetCheckError indicates a budget check hook failure with details.
type HookBudgetCheckError struct {
	TenantID string
	JobID    string
	Cause    error
}

// Error implements the error interface.
func (e *HookBudgetCheckError) Error() string {
	msg := ErrHookBudgetCheck.Error() + ": tenant=" + e.TenantID + " job=" + e.JobID
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *HookBudgetCheckError) Unwrap() error {
	return ErrHookBudgetCheck
}

// HookRecoveryBootstrapError indicates a recovery bootstrap hook failure with details.
type HookRecoveryBootstrapError struct {
	Step  string
	Cause error
}

// Error implements the error interface.
func (e *HookRecoveryBootstrapError) Error() string {
	msg := ErrHookRecoveryBootstrap.Error() + ": step=" + e.Step
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *HookRecoveryBootstrapError) Unwrap() error {
	return ErrHookRecoveryBootstrap
}

// HookPeerDeliveryError indicates a peer delivery hook failure with details.
type HookPeerDeliveryError struct {
	AgentID string
	Cause   error
}

// Error implements the error interface.
func (e *HookPeerDeliveryError) Error() string {
	msg := ErrHookPeerDelivery.Error() + ": agent=" + e.AgentID
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *HookPeerDeliveryError) Unwrap() error {
	return ErrHookPeerDelivery
}

// HookReindexError indicates a reindex hook failure with details.
type HookReindexError struct {
	Cause error
}

// Error implements the error interface.
func (e *HookReindexError) Error() string {
	if e.Cause != nil {
		return ErrHookReindex.Error() + ": " + e.Cause.Error()
	}
	return ErrHookReindex.Error()
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *HookReindexError) Unwrap() error {
	return ErrHookReindex
}

// =============================================================================
// Budget Action Constants
// =============================================================================

const (
	// BudgetActionShutdown triggers graceful shutdown of all agents.
	BudgetActionShutdown = "shutdown"

	// BudgetActionPause pauses execution and notifies the user.
	BudgetActionPause = "pause"

	// BudgetActionWarn logs a warning but allows execution to continue.
	BudgetActionWarn = "warn"
)

// =============================================================================
// Hook Result Types
// =============================================================================

// BudgetCheckResult describes the action the control loop should take after
// a budget check.
type BudgetCheckResult struct {
	// Status is the current budget status (ok, warning, exceeded).
	Status BudgetStatus

	// Action is the configured budget action to take when exceeded
	// ("shutdown", "pause", or "warn"). Empty when budget is ok/warning.
	Action string
}

// RecoveryStep describes a single step in the recovery bootstrap sequence.
// Steps are executed in the order they appear in the slice.
type RecoveryStep struct {
	// Name identifies this step for logging and error reporting.
	Name string

	// Fn executes the recovery step. It receives the context and returns
	// an error if the step fails.
	Fn func(ctx context.Context) error
}

// =============================================================================
// Hook Functions
// =============================================================================

// HookCheckBudget checks the cost budget and returns the result describing
// what action the control loop should take. Returns a nil result with no
// error when the tracker is nil (no budget configured).
func HookCheckBudget(
	ctx context.Context,
	tracker CostTracker,
	settings CostControlSettings,
	tenantID, jobID string,
	logger interfaces.Logger,
) (*BudgetCheckResult, error) {
	// No tracker means no budget enforcement.
	if tracker == nil {
		return nil, nil
	}

	status, err := tracker.CheckBudget(ctx, tenantID, jobID)
	if err != nil {
		// ErrBudgetNotSet is non-fatal; treat as ok.
		if errors.Is(err, ErrBudgetNotSet) {
			return &BudgetCheckResult{Status: BudgetOK}, nil
		}
		return nil, &HookBudgetCheckError{
			TenantID: tenantID,
			JobID:    jobID,
			Cause:    err,
		}
	}

	result := &BudgetCheckResult{Status: status}

	switch status {
	case BudgetExceeded:
		result.Action = settings.BudgetAction
		if result.Action == "" {
			result.Action = BudgetActionWarn
		}
		logger.Warnf("hook: budget exceeded for tenant=%s job=%s, action=%s",
			tenantID, jobID, result.Action)

	case BudgetWarning:
		logger.Warnf("hook: budget >80%% consumed for tenant=%s job=%s",
			tenantID, jobID)
	}

	return result, nil
}

// HookRecoveryBootstrap executes the recovery bootstrap sequence in the
// correct order. Steps are executed sequentially; if any step fails, the
// remaining steps are skipped and the error is returned.
//
// The canonical ordering for recovery is:
//  1. Restore permissions on lead agent (MUST be first)
//  2. Scan fleet, force-transition busy agents to ready
//  3. Notify lead about interrupted agents
//  4. Subscribe to cleanup events (MUST be last before restart)
//  5. If auto_restart enabled AND budget not exceeded: restart agents
func HookRecoveryBootstrap(ctx context.Context, steps []RecoveryStep) error {
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return &HookRecoveryBootstrapError{
				Step:  step.Name,
				Cause: err,
			}
		}

		if err := step.Fn(ctx); err != nil {
			return &HookRecoveryBootstrapError{
				Step:  step.Name,
				Cause: err,
			}
		}
	}
	return nil
}

// HookStartPeerDelivery starts peer message delivery subscriptions for
// agents in a wave. Returns a slice of cancel functions that stop the
// delivery goroutines when called. Returns nil with no error when the
// peer messaging service is nil.
//
// Each subscription spawns a goroutine that reads from the subscription
// channel and calls the deliverFn callback to forward the message.
// The goroutine exits when the subscription channel is closed or the
// context is cancelled.
func HookStartPeerDelivery(
	ctx context.Context,
	peerSvc PeerMessagingService,
	tenantID string,
	agentIDs []string,
	deliverFn func(ctx context.Context, agentID string, msg *PeerMessage),
) ([]func(), error) {
	if peerSvc == nil {
		return nil, nil
	}

	if deliverFn == nil {
		return nil, nil
	}

	cancels := make([]func(), 0, len(agentIDs))

	for _, id := range agentIDs {
		ch, err := peerSvc.Subscribe(ctx, tenantID, id)
		if err != nil {
			// Clean up already started subscriptions on error.
			for _, cancel := range cancels {
				cancel()
			}
			return nil, &HookPeerDeliveryError{
				AgentID: id,
				Cause:   err,
			}
		}

		deliveryCtx, deliveryCancel := context.WithCancel(ctx)
		agentID := id // capture loop variable

		go func() {
			defer deliveryCancel()
			for {
				select {
				case <-deliveryCtx.Done():
					return
				case msg, ok := <-ch:
					if !ok {
						return
					}
					deliverFn(deliveryCtx, agentID, msg)
				}
			}
		}()

		cancels = append(cancels, deliveryCancel)
	}

	return cancels, nil
}

// HookTriggerReindex triggers re-indexing if the "iteration_complete" trigger
// is configured in the triggers slice. The reindexFn callback performs the
// actual re-indexing work. Returns nil if reindexFn is nil, triggers is empty,
// or "iteration_complete" is not in the triggers list.
func HookTriggerReindex(
	ctx context.Context,
	reindexFn func(context.Context) error,
	triggers []string,
) error {
	if reindexFn == nil {
		return nil
	}

	if len(triggers) == 0 {
		return nil
	}

	for _, trigger := range triggers {
		if trigger == "iteration_complete" {
			if err := reindexFn(ctx); err != nil {
				return &HookReindexError{Cause: err}
			}
			return nil
		}
	}

	return nil
}

// =============================================================================
// ControlLoop Service Fields and Setters
// =============================================================================

// SetPeerMessaging sets the peer messaging service for inter-agent communication.
// When non-nil and the topology is not hub_and_spoke, peer message delivery is
// started for each wave of agents.
func (cl *ControlLoop) SetPeerMessaging(svc PeerMessagingService) {
	cl.peerMessaging = svc
}

// SetFileLocking sets the file locking service for distributed file coordination.
func (cl *ControlLoop) SetFileLocking(svc FileLockService) {
	cl.fileLocking = svc
}

// SetCostTracker sets the cost tracker for token budget enforcement.
func (cl *ControlLoop) SetCostTracker(tracker CostTracker) {
	cl.job.CostTracker = tracker
}

// SetReindexFn sets a callback function that triggers re-indexing of the
// vector database. This uses a callback pattern to avoid coupling the
// control loop to the rag build tag.
func (cl *ControlLoop) SetReindexFn(fn func(context.Context) error) {
	cl.reindexFn = fn
}

// SetPeerDeliverFn sets the callback invoked to deliver a peer message to an
// agent session. Callers should provide a function that pushes the message
// into the agent's ChannelServer event stream.
func (cl *ControlLoop) SetPeerDeliverFn(fn func(context.Context, string, *PeerMessage)) {
	cl.peerDeliverFn = fn
}

// =============================================================================
// ControlLoop Integration Helpers
// =============================================================================

// checkBudget delegates to HookCheckBudget using the control loop's configured
// cost tracker and workflow settings. Returns nil, nil when no cost tracker
// or workflow is configured.
func (cl *ControlLoop) checkBudget(ctx context.Context) (*BudgetCheckResult, error) {
	if cl.job.CostTracker == nil {
		return nil, nil
	}

	var settings CostControlSettings
	if cl.workflow != nil {
		settings = cl.workflow.Settings.CostControls
	}

	return HookCheckBudget(ctx, cl.job.CostTracker, settings, cl.job.Config.TenantID, cl.job.JobID, cl.job.Logger)
}

// startPeerDelivery starts peer message delivery for agents in the given wave
// items when peer messaging is configured and the topology supports it.
// Delivery cancel functions are accumulated in cl.peerDeliveryCancels for
// cleanup during session teardown.
func (cl *ControlLoop) startPeerDelivery(ctx context.Context, waveItems []*WorkItem) {
	if cl.peerMessaging == nil {
		return
	}

	// Only start peer delivery for hybrid and peer-to-peer topologies.
	if cl.workflow == nil || cl.workflow.Settings.Topology == TopologyHubAndSpoke || cl.workflow.Settings.Topology == "" {
		return
	}

	// Collect unique agent IDs from wave items.
	seen := make(map[string]struct{}, len(waveItems))
	agentIDs := make([]string, 0, len(waveItems))
	for _, item := range waveItems {
		role := item.RequiredRole
		if role == "" {
			role = "worker"
		}
		agentID := "agent-" + role + "-" + cl.job.JobID
		if _, exists := seen[agentID]; !exists {
			seen[agentID] = struct{}{}
			agentIDs = append(agentIDs, agentID)
		}
	}

	if len(agentIDs) == 0 {
		return
	}

	if cl.peerDeliverFn == nil {
		cl.job.Logger.Warnf("control loop: peer delivery function not configured, skipping delivery")
		return
	}

	cancels, err := HookStartPeerDelivery(ctx, cl.peerMessaging, cl.job.Config.TenantID, agentIDs, cl.peerDeliverFn)
	if err != nil {
		cl.job.Logger.Warnf("control loop: failed to start peer delivery: %s", err.Error())
		return
	}

	cl.peerDeliveryCancels = append(cl.peerDeliveryCancels, cancels...)
}

// =============================================================================
// Lifecycle seams: the loop's view of the envelope
// =============================================================================
//
// The five lifecycle moments (job_hooks.go) live on the JobRun now: SetState
// fires the state-keyed moment and the envelope fires Discard. What is left
// here is the loop's view of them — the hook set the workflow selected, the
// operator's policy, and the JobContext refreshed with the loop's own
// recoverable state before a hook or a validator reads it.

// lifecyclePolicy returns the operator policy the run resolved.
func (cl *ControlLoop) lifecyclePolicy() *JobLifecyclePolicy {
	return cl.job.Policy()
}

// validators returns the registry criteria are judged through.
func (cl *ControlLoop) validators() *ValidatorRegistry {
	return cl.job.Validators()
}

// jobContext returns the job's context refreshed with the loop's own
// recoverable state — the workflow definition it is running — on top of the
// envelope's.
func (cl *ControlLoop) jobContext() *JobContext {
	cl.job.Workflow = cl.workflow
	return cl.job.Context()
}

// enterStage runs the lifecycle moment that belongs to the state the loop is
// about to run.
func (cl *ControlLoop) enterStage(ctx context.Context, state JobState) error {
	cl.jobContext()
	return cl.job.enterStage(ctx, state)
}

// discard runs Discard once the job is over.
func (cl *ControlLoop) discard(ctx context.Context, completed bool) error {
	cl.jobContext()
	return cl.job.discard(ctx, completed)
}

// failFromHook fails the job on a hook error: the failure class is set so
// the task's attempt accounting can tell a hook failure from a failure of
// the work, sessions are released, and the typed error is returned.
func (cl *ControlLoop) failFromHook(ctx context.Context, err error) error {
	cl.cleanupAllSessions(ctx)
	return err
}

// recordItemResult records one work item's output by completing the Task the
// job created for that item.
func (cl *ControlLoop) recordItemResult(ctx context.Context, index int, output string) {
	result := &TaskResult{Output: output}
	if err := cl.job.Agents.CompleteTask(ctx, cl.job.Config.TenantID, cl.taskIDs[index], result); err != nil {
		cl.job.Logger.Warnf("control loop: failed to complete task %s: %s", cl.taskIDs[index], err.Error())
	}
}

// reviewTasks returns the Tasks the review prompt describes: the ones this
// job created for its work items.
func (cl *ControlLoop) reviewTasks(ctx context.Context) ([]*Task, error) {
	tasks, err := cl.job.Agents.ListTasks(ctx, cl.job.Config.TenantID, nil)
	if err != nil {
		return nil, err
	}
	return cl.filterJobTasks(tasks), nil
}

// reviewByCriteria judges the published work through the reviewing-phase
// criteria and turns their verdict into the review response the rest of the
// loop already understands: approved when every required criterion passed,
// otherwise the failures as feedback.
func (cl *ControlLoop) reviewByCriteria(ctx context.Context, criteria []ReviewCriterion) (*ReviewResponse, error) {
	job := cl.jobContext()
	job.Artifacts = cl.collectTaskResults(ctx)
	results, allPassed, err := cl.validators().RunPipeline(ctx, job, criteria)
	if err != nil {
		return nil, err
	}
	cl.reviewResults = results
	cl.audit("orchestration:review_criteria", "criteria="+itoa(len(cl.reviewResults))+" passed="+boolStr(allPassed))
	if allPassed {
		return &ReviewResponse{Approved: true, Feedback: "all review criteria passed"}, nil
	}
	var feedback strings.Builder
	for _, r := range results {
		if r.Passed {
			continue
		}
		if feedback.Len() > 0 {
			feedback.WriteString("; ")
		}
		feedback.WriteString(r.Name)
		feedback.WriteString(": ")
		feedback.WriteString(r.Message)
		if r.Details != "" {
			feedback.WriteString(" (")
			feedback.WriteString(r.Details)
			feedback.WriteString(")")
		}
	}
	return &ReviewResponse{Approved: false, Feedback: feedback.String()}, nil
}
