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

package sdlc

import (
	"errors"
	"strconv"
	"strings"
)

// =============================================================================
// Sentinel errors
// =============================================================================

var (
	// ErrNotFound indicates a record does not exist.
	ErrNotFound = errors.New("sdlc: record not found")

	// ErrAlreadyExists indicates a record already exists.
	ErrAlreadyExists = errors.New("sdlc: record already exists")

	// ErrVersionConflict indicates a guarded write lost to a concurrent one.
	ErrVersionConflict = errors.New("sdlc: version conflict")

	// ErrInvalidInput indicates a caller-supplied value was refused.
	ErrInvalidInput = errors.New("sdlc: invalid input")

	// ErrStrategyNotRegistered indicates a branching or merge strategy name
	// nothing registered.
	ErrStrategyNotRegistered = errors.New("sdlc: strategy not registered")

	// ErrBackend indicates a storage or forge backend failed.
	ErrBackend = errors.New("sdlc: backend failure")

	// ErrForgeNotRegistered indicates no adapter is registered for a forge type.
	ErrForgeNotRegistered = errors.New("sdlc: forge backend not registered")

	// ErrForgeUnsupported indicates the forge does not support an operation.
	ErrForgeUnsupported = errors.New("sdlc: forge does not support this operation")

	// ErrForgeWriteNotApplied indicates the forge accepted a write that its
	// read-back does not show.
	ErrForgeWriteNotApplied = errors.New("sdlc: forge accepted a write that did not take effect")

	// ErrCIHostDefect indicates a red pipeline that no change to the code
	// could turn green: the machine is broken, and an operator has to look.
	ErrCIHostDefect = errors.New("sdlc: continuous integration failed on a host defect")

	// ErrWebhookUnverified indicates a webhook signature did not verify.
	ErrWebhookUnverified = errors.New("sdlc: webhook signature did not verify")

	// ErrForgeNotReady indicates the forge has not finished computing the
	// state an operation needs (a pull request's mergeability, typically).
	ErrForgeNotReady = errors.New("sdlc: forge has not finished computing this state")

	// ErrCustodyCredentialAbsent indicates no custody credential is configured
	// for token minting.
	ErrCustodyCredentialAbsent = errors.New("sdlc: no custody credential configured for token minting")

	// ErrWebhookSecretMissing indicates a binding names no webhook secret, or
	// the named secret resolved empty.
	ErrWebhookSecretMissing = errors.New("sdlc: no webhook verification secret is configured")

	// ErrWebhookRepoMismatch indicates a delivery describes a repository other
	// than the one the binding names.
	ErrWebhookRepoMismatch = errors.New("sdlc: delivery names a different repository than the binding")

	// ErrMergeNotReadyExhausted indicates the forge stayed not-ready past the
	// merge retry bound.
	ErrMergeNotReadyExhausted = errors.New("sdlc: the forge did not become ready within the retry bound")

	// ErrMergeConflict indicates the forge settled on the pull request not
	// being mergeable into its base: the change conflicts with it, and only a
	// rebase onto the base can make it mergeable again.
	ErrMergeConflict = errors.New("sdlc: pull request conflicts with its base branch")

	// ErrHeadMoved indicates the pull request's head is no longer the head
	// the review judged.
	ErrHeadMoved = errors.New("sdlc: pull request head moved since it was reviewed")

	// ErrNoChanges indicates an attempt produced nothing to publish.
	ErrNoChanges = errors.New("sdlc: the attempt produced no changes to publish")

	// ErrDeliveryAbsent indicates a lifecycle moment ran for a task whose
	// attempt has no Delivery record, so Prepare never completed.
	ErrDeliveryAbsent = errors.New("sdlc: no delivery recorded for this attempt")

	// ErrTaskNotForgeIssue indicates a task's provenance is not a forge
	// issue, so the module cannot act on it.
	ErrTaskNotForgeIssue = errors.New("sdlc: task does not originate from a forge issue")

	// ErrUnavailable indicates the module is not running on this node.
	ErrUnavailable = errors.New("sdlc: the module is not running on this node")
)

// =============================================================================
// Record errors
// =============================================================================

// NotFoundError reports a record that does not exist.
type NotFoundError struct {
	Entity   string
	TenantID string
	ID       string
}

// Error implements the error interface.
func (e *NotFoundError) Error() string {
	return "sdlc: " + e.Entity + " " + e.ID + " not found in tenant " + e.TenantID
}

// Unwrap returns the sentinel for errors.Is support.
func (e *NotFoundError) Unwrap() error { return ErrNotFound }

// AlreadyExistsError reports a record that already exists.
type AlreadyExistsError struct {
	Entity   string
	TenantID string
	ID       string
}

// Error implements the error interface.
func (e *AlreadyExistsError) Error() string {
	return "sdlc: " + e.Entity + " " + e.ID + " already exists in tenant " + e.TenantID
}

// Unwrap returns the sentinel for errors.Is support.
func (e *AlreadyExistsError) Unwrap() error { return ErrAlreadyExists }

// VersionConflictError reports a guarded write that found the record changed
// underneath it. The caller re-reads and decides again; it never retries the
// same write blind.
type VersionConflictError struct {
	Entity   string
	TenantID string
	ID       string
	Expected int64
}

// Error implements the error interface.
func (e *VersionConflictError) Error() string {
	return "sdlc: " + e.Entity + " " + e.ID + " changed underneath this write (expected version " +
		strconv.FormatInt(e.Expected, 10) + ")"
}

// Unwrap returns the sentinel for errors.Is support.
func (e *VersionConflictError) Unwrap() error { return ErrVersionConflict }

// InvalidInputError reports a caller-supplied value that was refused.
type InvalidInputError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *InvalidInputError) Error() string {
	return "sdlc: invalid " + e.Field + ": " + e.Message
}

// Unwrap returns the sentinel for errors.Is support.
func (e *InvalidInputError) Unwrap() error { return ErrInvalidInput }

// RepoURLRefusedError reports a repository URL outside the operator's
// allowlist of forge hosts.
type RepoURLRefusedError struct {
	RepoURL string
	Reason  string
}

// Error implements the error interface.
func (e *RepoURLRefusedError) Error() string {
	return "sdlc: repository url " + strconv.Quote(e.RepoURL) + " refused: " + e.Reason +
		"; allowed are ssh:// and https:// against hosts in sdlc.repo_hosts"
}

// Unwrap returns the sentinel for errors.Is support.
func (e *RepoURLRefusedError) Unwrap() error { return ErrInvalidInput }

// TriggerLabelCollisionError reports a trigger label that is also one of the
// binding's display labels. The trigger is how a person hands an issue over;
// a display label the module writes must never be the same string, or the
// module would hand issues to itself.
type TriggerLabelCollisionError struct {
	Key   string
	Label string
}

// Error implements the error interface.
func (e *TriggerLabelCollisionError) Error() string {
	return "sdlc: trigger label " + strconv.Quote(e.Label) + " is also the display label for " + e.Key +
		"; the module would re-trigger every issue it labels"
}

// Unwrap returns the sentinel for errors.Is support.
func (e *TriggerLabelCollisionError) Unwrap() error { return ErrInvalidInput }

// StrategyNotRegisteredError reports a strategy name nothing registered.
//
// It carries what IS registered so the refusal tells an operator which name
// to use instead of only which one failed.
type StrategyNotRegisteredError struct {
	Name      string
	Available []string
}

// Error implements the error interface.
func (e *StrategyNotRegisteredError) Error() string {
	msg := "sdlc: no strategy registered under " + quoteName(e.Name)
	if len(e.Available) > 0 {
		msg += "; available: " + strings.Join(e.Available, ", ")
	}
	return msg
}

// Unwrap returns the sentinel for errors.Is support.
func (e *StrategyNotRegisteredError) Unwrap() error { return ErrStrategyNotRegistered }

// BackendError reports a storage or forge failure beneath an operation.
type BackendError struct {
	Op    string
	Cause error
}

// Error implements the error interface.
func (e *BackendError) Error() string {
	return "sdlc: " + e.Op + ": " + e.Cause.Error()
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *BackendError) Unwrap() []error { return []error{ErrBackend, e.Cause} }

func backendError(op string, cause error) error {
	return &BackendError{Op: op, Cause: cause}
}

// =============================================================================
// Forge errors
// =============================================================================

// ForgeNotRegisteredError reports a forge type with no adapter.
//
// It carries the forge types that DO have one: gitea, github and gitlab are
// all selectable on a binding, and only the ones with a registered adapter
// can deliver, so the refusal has to say which those are.
type ForgeNotRegisteredError struct {
	Type      ForgeType
	Available []ForgeType
}

// Error implements the error interface.
func (e *ForgeNotRegisteredError) Error() string {
	msg := "sdlc: no forge backend registered for " + quoteName(string(e.Type))
	if len(e.Available) > 0 {
		names := make([]string, 0, len(e.Available))
		for _, forge := range e.Available {
			names = append(names, string(forge))
		}
		msg += "; available: " + strings.Join(names, ", ")
	}
	return msg
}

// Unwrap returns the sentinel for errors.Is support.
func (e *ForgeNotRegisteredError) Unwrap() error { return ErrForgeNotRegistered }

// ForgeUnsupportedError reports a capability the forge lacks.
type ForgeUnsupportedError struct {
	Type       ForgeType
	Capability string
}

// Error implements the error interface.
func (e *ForgeUnsupportedError) Error() string {
	return "sdlc: " + string(e.Type) + " does not support " + e.Capability
}

// Unwrap returns the sentinel for errors.Is support.
func (e *ForgeUnsupportedError) Unwrap() error { return ErrForgeUnsupported }

// CIHostDefectError reports a red pipeline that belongs to an operator rather
// than to a coding agent.
//
// IT TRAVELS AS AN ERROR SO THE RUN PARKS RATHER THAN RE-DELEGATES. A
// validation that merely FAILS sends the work back to the worker for another
// attempt, which is exactly the response that cost 1.5M tokens on 2026-09-12:
// no change an agent could author fixes a registry the host cannot reach or a
// module-resolution environment that is not there. The SDLC runner recognises
// this type and parks the task with TaskBlockKindEscalated, which posts Reason
// to the forge issue a person is watching.
//
// It is also what a spent re-run budget becomes. A void red is re-run
// automatically, but a host that keeps answering void is a host, and the
// escalation says how many times it was asked.
type CIHostDefectError struct {
	// HeadSHA is the head whose pipeline is red.
	HeadSHA string

	// Reason is the whole account an operator reads: which checks, which
	// signature, and the evidence each rests on.
	Reason string
}

// Error implements the error interface.
func (e *CIHostDefectError) Error() string {
	return "sdlc: continuous integration on " + shortSHA(e.HeadSHA) + " failed on a host defect: " + e.Reason
}

// Unwrap returns the sentinel for errors.Is support.
func (e *CIHostDefectError) Unwrap() error { return ErrCIHostDefect }

// ParkReason is the text the run parks the task with.
func (e *CIHostDefectError) ParkReason() string { return e.Reason }

// ForgeWriteNotAppliedError reports a write the forge accepted but whose
// read-back does not show.
type ForgeWriteNotAppliedError struct {
	Type      ForgeType
	Operation string
	Subject   string
	Want      string
	Got       string
}

// Error implements the error interface.
func (e *ForgeWriteNotAppliedError) Error() string {
	msg := "sdlc: " + string(e.Type) + " accepted " + e.Operation + " on " + e.Subject +
		" but the change did not take effect"
	if e.Want != "" || e.Got != "" {
		msg += " (want " + e.Want + ", got " + e.Got + ")"
	}
	return msg
}

// Unwrap returns the sentinel for errors.Is support.
func (e *ForgeWriteNotAppliedError) Unwrap() error { return ErrForgeWriteNotApplied }

// ForgeNotReadyError reports a state the forge has not finished computing.
type ForgeNotReadyError struct {
	Type      ForgeType
	Operation string
	Subject   string
	Detail    string
}

// Error implements the error interface.
func (e *ForgeNotReadyError) Error() string {
	msg := "sdlc: " + string(e.Type) + " is not ready to " + e.Operation + " " + e.Subject
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	return msg
}

// Unwrap returns the sentinel for errors.Is support.
func (e *ForgeNotReadyError) Unwrap() error { return ErrForgeNotReady }

// WebhookUnverifiedError reports a delivery whose signature did not verify.
type WebhookUnverifiedError struct {
	Type   ForgeType
	Reason string
}

// Error implements the error interface.
func (e *WebhookUnverifiedError) Error() string {
	return "sdlc: " + string(e.Type) + " webhook signature did not verify: " + e.Reason
}

// Unwrap returns the sentinel for errors.Is support.
func (e *WebhookUnverifiedError) Unwrap() error { return ErrWebhookUnverified }

// WebhookSecretMissingError reports a binding that cannot verify deliveries.
type WebhookSecretMissingError struct {
	ProjectID string
	Reason    string
}

// Error implements the error interface.
func (e *WebhookSecretMissingError) Error() string {
	return "sdlc: project " + e.ProjectID + " cannot verify webhook deliveries: " + e.Reason
}

// Unwrap returns the sentinel for errors.Is support.
func (e *WebhookSecretMissingError) Unwrap() error { return ErrWebhookSecretMissing }

// WebhookRepoMismatchError reports a delivery about a repository the binding
// does not name.
type WebhookRepoMismatchError struct {
	ProjectID  string
	Configured RepoRef
	Delivered  RepoRef
}

// Error implements the error interface.
func (e *WebhookRepoMismatchError) Error() string {
	return "sdlc: project " + e.ProjectID + " is bound to " + e.Configured.String() +
		" but the delivery describes " + e.Delivered.String()
}

// Unwrap returns the sentinel for errors.Is support.
func (e *WebhookRepoMismatchError) Unwrap() error { return ErrWebhookRepoMismatch }

// MergeNotReadyExhaustedError reports a merge that stayed not-ready past the
// retry bound.
type MergeNotReadyExhaustedError struct {
	Subject  string
	Attempts int
	Last     error
}

// Error implements the error interface.
func (e *MergeNotReadyExhaustedError) Error() string {
	msg := "sdlc: " + e.Subject + " was still not ready to merge after " +
		strconv.Itoa(e.Attempts) + " attempt(s)"
	if e.Last != nil {
		msg += ": " + e.Last.Error()
	}
	return msg
}

// Unwrap returns the sentinel for errors.Is support.
func (e *MergeNotReadyExhaustedError) Unwrap() error { return ErrMergeNotReadyExhausted }

// MergeConflictError reports a pull request the forge still held not mergeable
// once the not-ready retry bound was spent, on the head the review approved.
//
// It is a verdict about the WORK, not about the forge: the base moved under
// the change and the two no longer merge. Nothing retrying the merge can fix
// it, and nothing about it is the environment's fault. The runner answers it
// with a rebase round on the same branch and pull request.
type MergeConflictError struct {
	Subject  string
	Base     string
	Head     string
	Attempts int
}

// Error implements the error interface.
func (e *MergeConflictError) Error() string {
	return "sdlc: " + e.Subject + " at " + e.Head + " conflicts with its base " + e.Base +
		": the forge still reported it not mergeable after " + strconv.Itoa(e.Attempts) + " attempt(s)"
}

// Unwrap returns the sentinel for errors.Is support.
func (e *MergeConflictError) Unwrap() error { return ErrMergeConflict }

// HeadMovedError reports a pull request whose head is no longer the one the
// review judged, so the approval no longer covers what would merge.
type HeadMovedError struct {
	Subject  string
	Reviewed string
	Current  string
}

// Error implements the error interface.
func (e *HeadMovedError) Error() string {
	return "sdlc: " + e.Subject + " head moved from reviewed " + e.Reviewed + " to " + e.Current +
		"; the approval does not cover the current head"
}

// Unwrap returns the sentinel for errors.Is support.
func (e *HeadMovedError) Unwrap() error { return ErrHeadMoved }

// NoChangesError reports an attempt that left the worktree identical to its
// base, so there is nothing to commit, push or review.
//
// IT IS THE EXCEPTION NOW, NOT THE RULE. A turn that changed nothing and said
// why — a question, a rule it may not break, an environment that would not
// build — parks the task instead (turn.go). This error is what is left: a
// worker that reported work on a tree it did not touch, which is a failure of
// the work and is charged as one.
type NoChangesError struct {
	TaskID  string
	WorkDir string

	// Reply is what the worker said on the turn that changed nothing, so the
	// task result carries the account of the attempt rather than only the
	// fact that it produced no diff.
	Reply string
}

// Error implements the error interface.
func (e *NoChangesError) Error() string {
	msg := "sdlc: task " + e.TaskID + " produced no changes in " + e.WorkDir
	if reply := publicOutput(e.Reply); reply != "" {
		msg += "\n\nThe worker's last turn said:\n" + reply
	}
	return msg
}

// Unwrap returns the sentinel for errors.Is support.
func (e *NoChangesError) Unwrap() error { return ErrNoChanges }

// TaskNotForgeIssueError reports a task the module was asked to act on that
// did not originate from a forge issue.
type TaskNotForgeIssueError struct {
	TaskID string
	Kind   string
}

// Error implements the error interface.
func (e *TaskNotForgeIssueError) Error() string {
	return "sdlc: task " + e.TaskID + " originates from " + strconv.Quote(e.Kind) + ", not a forge issue"
}

// Unwrap returns the sentinel for errors.Is support.
func (e *TaskNotForgeIssueError) Unwrap() error { return ErrTaskNotForgeIssue }

// ErrIncompleteResponse indicates a backend answered without the part that
// mattered — misbehaving rather than unavailable.
var ErrIncompleteResponse = errors.New("sdlc: backend answered without the part that mattered")

// IncompleteResponseError reports a backend answer missing what the caller
// needed. It is both a backend failure and specifically an incomplete one.
type IncompleteResponseError struct {
	// Op is the call that answered.
	Op string

	// Missing names what the answer left out.
	Missing string
}

// Error implements the error interface.
func (e *IncompleteResponseError) Error() string {
	return ErrIncompleteResponse.Error() + ": " + e.Op + " returned no " + e.Missing
}

// Unwrap reports both sentinels for errors.Is support.
func (e *IncompleteResponseError) Unwrap() []error {
	return []error{ErrBackend, ErrIncompleteResponse}
}

// quoteName renders a name in a refusal so an empty one is visible rather
// than swallowed by the surrounding prose.
func quoteName(name string) string {
	if name == "" {
		return "\"\""
	}
	return name
}

// =============================================================================
// Runner errors
// =============================================================================

// ErrRunner indicates the sdlc runner refused or could not drive a job.
var ErrRunner = errors.New("sdlc runner: refused")

// UnknownRunnerStateError reports a job that reached a state this runner does
// not drive — a persisted state from another runner, or a resume point that
// does not belong to this sequence.
type UnknownRunnerStateError struct {
	// State is the state the job was in.
	State string
}

// Error implements the error interface.
func (e *UnknownRunnerStateError) Error() string {
	return ErrRunner.Error() + ": the sdlc runner drives no state " + strconv.Quote(e.State)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UnknownRunnerStateError) Unwrap() error { return ErrRunner }

// UnknownTurnActionError reports a classified worker turn whose action this
// runner does not implement.
//
// It cannot arise from a worker's reply — the classification table is closed
// and every outcome names one of three actions — so it is a refusal aimed at
// a future edit that adds an action and forgets the runner's half. Refusing
// is the fail-closed direction: the alternative default, "carry on", would
// send an unchanged tree to verification and review.
type UnknownTurnActionError struct {
	// Action is the action the classification named.
	Action string

	// Outcome is the turn outcome that named it.
	Outcome string
}

// Error implements the error interface.
func (e *UnknownTurnActionError) Error() string {
	return ErrRunner.Error() + ": the sdlc runner performs no turn action " + strconv.Quote(e.Action) +
		" (outcome " + strconv.Quote(e.Outcome) + ")"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UnknownTurnActionError) Unwrap() error { return ErrRunner }

// UnknownPostMergeStepError reports a strategy step no executor implements.
// It is a refusal rather than a skip: a back-merge silently not performed is
// a fix lost from the development line.
type UnknownPostMergeStepError struct {
	// Kind is the step kind the strategy declared.
	Kind string
}

// Error implements the error interface.
func (e *UnknownPostMergeStepError) Error() string {
	return ErrRunner.Error() + ": no executor for post-merge step " + strconv.Quote(e.Kind)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UnknownPostMergeStepError) Unwrap() error { return ErrRunner }

// WorkerSessionError reports an attempt with no usable worker session.
type WorkerSessionError struct {
	// TaskID is the task the attempt is for.
	TaskID string

	// Message says what was wrong with the session.
	Message string
}

// Error implements the error interface.
func (e *WorkerSessionError) Error() string {
	return ErrRunner.Error() + ": task " + e.TaskID + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *WorkerSessionError) Unwrap() error { return ErrRunner }

// =============================================================================
// A workspace nothing could work in
// =============================================================================
//
// ErrWorkspaceUnfit IS THE ONE TEST A RUNNER MAKES, and both errors below
// satisfy it, because the runner's answer to them is identical: park the task
// for an operator, attribute it to the factory, and DO NOT PROMPT ANYTHING.
// The two differ only in when the fault was provable — one on the daemon
// before a container existed, one inside the container before a token was
// spent — and a caller that had to enumerate them would acquire a third case
// to forget the next time a way to be unfit is found.
var ErrWorkspaceUnfit = errors.New("sdlc: the workspace is not fit for the work")

// WorkspaceCredentialError reports a checkout this spawn holds no credential
// for — an ssh remote with no agent socket, or an https one with no forge token
// — refused on the daemon before any container existed.
//
// IT IS ErrWorkspaceUnfit, which is the whole point of the type. The runner's
// answer is the one it gives every other way of being unfit: park for an
// operator, attribute it to the factory, prompt nothing. A missing credential is
// a CONFIGURATION — the binding's token secret, or the project's git.ssh_key —
// so failing the job instead would spend the task's whole attempt budget on the
// identical refusal and leave an operator reading a run of failures rather than
// one blocked task naming the fix. go-quicbaas#929 is what the unattributed
// version of that cost: one job every fifteen minutes, indefinitely.
type WorkspaceCredentialError struct {
	// ProjectID is the project whose checkout could not be authenticated.
	ProjectID string

	// Cause is the orchestration refusal, which names the repository, the
	// remote and the credential the transport requires.
	Cause error
}

// Error implements the error interface.
func (e *WorkspaceCredentialError) Error() string {
	return ErrRunner.Error() + ": project " + e.ProjectID + ": " + e.Cause.Error()
}

// Unwrap returns the sentinels and the cause, so a caller may test the one
// question a runner asks (is the workspace unfit) or read which repository and
// which credential were missing.
func (e *WorkspaceCredentialError) Unwrap() []error {
	return []error{ErrRunner, ErrWorkspaceUnfit, e.Cause}
}

// ProjectRecordMissingError reports a job whose project record the daemon does
// not hold. Everything an attempt needs to describe itself — the repository,
// the identity, the workspace root — hangs off it.
//
// IT IS ErrWorkspaceUnfit, and the family comment above predicted this exact
// addition: "a caller that had to enumerate them would acquire a third case to
// forget the next time a way to be unfit is found." This is that third way, and
// it belongs to the family on the family's own terms — provable on the daemon
// before a container existed, refused before a token is spent.
//
// IT USED TO BE AN InvalidInputError, which unwraps to ErrInvalidInput alone.
// prepareWorkspace parks only on ErrWorkspaceUnfit, so the refusal walked past
// the park, out of Run as a plain terminal error, through classifyJobFailure —
// which knows nothing about ErrInvalidInput — and was charged against the task's
// attempt budget as a failure of the WORK. Measured on the burn-in node: two
// jobs resumed after a daemon restart, both charged for a fault that was
// entirely the factory's, both reported as "charged as a failure of the work".
//
// A task whose project is genuinely gone is an operator's to fix, and parking it
// is what puts the fix in front of them instead of spending three attempts
// discovering the same refusal.
type ProjectRecordMissingError struct {
	// TaskID is the attempt that had nothing to describe itself from, empty for
	// a job bound to no task.
	TaskID string

	// ProjectID is the project the job named.
	ProjectID string
}

// Error implements the error interface.
func (e *ProjectRecordMissingError) Error() string {
	msg := ErrRunner.Error() + ": the job carries no project record; the worker's checkout is described from it"
	if e.ProjectID != "" {
		msg += " (project " + e.ProjectID + ")"
	}
	if e.TaskID != "" {
		msg += " (task " + e.TaskID + ")"
	}
	return msg
}

// Unwrap reports the sentinels, so a caller may test the one question a runner
// asks: is the workspace unfit.
func (e *ProjectRecordMissingError) Unwrap() []error {
	return []error{ErrRunner, ErrWorkspaceUnfit}
}

// WorkspaceUnavailableError reports a project whose worker has nowhere to
// work: no workspace path, or one the daemon could not make the container's
// mount point under.
type WorkspaceUnavailableError struct {
	// ProjectID is the project.
	ProjectID string

	// Cause is the filesystem failure, nil when the project simply names no
	// workspace.
	Cause error
}

// Error implements the error interface.
func (e *WorkspaceUnavailableError) Error() string {
	msg := ErrRunner.Error() + ": project " + e.ProjectID + " has no workspace for its worker's checkout"
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap reports the sentinels and the cause for errors.Is/As support.
func (e *WorkspaceUnavailableError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrRunner, ErrWorkspaceUnfit}
	}
	return []error{ErrRunner, ErrWorkspaceUnfit, e.Cause}
}

// WorkspaceUnfitError reports a checkout the pre-flight refused, NAMING THE
// REPOSITORY an operator has to fix.
//
// THE REPOSITORY IS THE WHOLE POINT. The fault this type exists for is a
// context repository a project declared and nobody materialized, and a refusal
// that said only "the workspace is unfit" would send its reader looking at the
// project checkout — which is almost always fine. So the name, the directory
// as the CONTAINER sees it, and what was wrong with it all travel, and they
// reach the issue comment the operator reads.
type WorkspaceUnfitError struct {
	// TaskID is the attempt whose workspace was refused.
	TaskID string

	// Repo is the repository's directory name under the checkout root: the
	// project's own name, or a context repository's.
	Repo string

	// Dir is that directory as the container sees it.
	Dir string

	// Reason says what was wrong, in the words an operator acts on.
	Reason string
}

// Error implements the error interface.
func (e *WorkspaceUnfitError) Error() string {
	return ErrRunner.Error() + ": task " + e.TaskID + ": the workspace is not fit for the work: " +
		e.Repo + " (" + e.Dir + ") " + e.Reason
}

// Unwrap returns the sentinels for errors.Is support.
func (e *WorkspaceUnfitError) Unwrap() []error {
	return []error{ErrRunner, ErrWorkspaceUnfit}
}

// EscalatedError is the terminal error of a job the runner parked for a
// person. It fails the job, which is what an escalation is: the engine did
// not deliver the change and a human has been told why.
type EscalatedError struct {
	// TaskID is the parked task.
	TaskID string

	// Reason is what was posted to the issue.
	Reason string
}

// Error implements the error interface.
func (e *EscalatedError) Error() string {
	return ErrRunner.Error() + ": task " + e.TaskID + " escalated: " + e.Reason
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *EscalatedError) Unwrap() error { return ErrRunner }

// PublishCommandError reports a git command that RAN inside the worker's
// container and refused.
//
// THE OUTPUT IS SCRUBBED, NOT WITHHELD (#751). An error's string reaches every
// log, every wrapped chain, and — through this module's own surfacing path — a
// comment on a forge issue, and a push that prints a remote's rejection can
// print a token with it. That is why the output goes through the same
// redactSecrets every publishable reason goes through, bounded to a tail; it is
// not why an operator should have to exec into a container to learn that git
// could not find ssh. See diagnosticTail in public_reason.go.
type PublishCommandError struct {
	// TaskID is the attempt being published.
	TaskID string

	// Command is the git subcommand and its flags, which are configuration
	// and carry no credential value.
	Command string

	// ExitCode is what git exited with.
	ExitCode int

	// Output is the command's captured output, verbatim. Error() renders a
	// scrubbed tail of it; everything else that reads it must scrub it too.
	Output string
}

// Error implements the error interface. It names the command and the scrubbed
// tail of what the command said.
func (e *PublishCommandError) Error() string {
	msg := ErrRunner.Error() + ": task " + e.TaskID + ": git " + e.Command +
		" exited " + strconv.Itoa(e.ExitCode)
	if tail := diagnosticTail(e.Output); tail != "" {
		msg += ": " + tail
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *PublishCommandError) Unwrap() error { return ErrRunner }

// CheckoutCommandError reports one git command of the container's checkout
// that ran inside the worker's container and refused.
//
// Its output is SCRUBBED AND RENDERED, for the reason PublishCommandError
// states. A clone that fails against a remote can print whatever that remote
// chose to say, so what Error() renders is the redacted, bounded tail — and
// this is the error #751 was written against: `git clone … exited 128 (102
// bytes of output withheld)` withheld `sh: ssh: not found`, which was the
// entire diagnosis. The full output remains where it also belongs, in the
// job's audit trail as the checkout's evidence.
type CheckoutCommandError struct {
	// TaskID is the attempt whose checkout was being made.
	TaskID string

	// Command is the git subcommand and its flags, which are configuration
	// and carry no credential value.
	Command string

	// ExitCode is what git exited with.
	ExitCode int

	// Output is the command's captured output, verbatim. Error() renders a
	// scrubbed tail of it; everything else that reads it must scrub it too.
	Output string
}

// Error implements the error interface. It names the command and the scrubbed
// tail of what the command said.
func (e *CheckoutCommandError) Error() string {
	msg := ErrRunner.Error() + ": task " + e.TaskID + ": checkout: git " + e.Command +
		" exited " + strconv.Itoa(e.ExitCode)
	if tail := diagnosticTail(e.Output); tail != "" {
		msg += ": " + tail
	}
	return msg
}

// Unwrap returns the sentinels for errors.Is support.
//
// IT IS A WORKSPACE FAULT, and that is the whole of why the sentinel is here.
// Every git command this error can come from runs BEFORE the worker is
// prompted — the clone of the project, the clone of a context repository the
// project declared, the fetch, the branch — so nothing it reports is a
// judgement about the work. A clone of a declared context repository that the
// forge does not serve is the burn-in's own fault (go-quicbaas#908) and used to
// be charged against the task; it is an operator's to repair.
func (e *CheckoutCommandError) Unwrap() []error { return []error{ErrRunner, ErrWorkspaceUnfit} }

// CheckoutHeadMismatchError reports a checkout that was pinned to one commit
// and ended on another.
//
// IT IS THE FAIL-CLOSED HALF OF PINNING. A caller pins because it intends to
// state, afterwards, which tree was read — a review verdict, a commit status,
// a signature. If HEAD is not the pinned commit then every such statement
// would be about a tree nothing inspected, so the attempt stops here instead,
// with both commits named so a person can see which push moved underneath it.
type CheckoutHeadMismatchError struct {
	// TaskID is the attempt whose checkout was being made.
	TaskID string

	// Want is the commit the plan pinned the checkout to.
	Want string

	// Got is the commit HEAD actually resolved to. Empty means the checkout
	// answered with nothing at all, which is the same refusal.
	Got string
}

// Error implements the error interface.
func (e *CheckoutHeadMismatchError) Error() string {
	got := e.Got
	if got == "" {
		got = "nothing"
	}
	return ErrRunner.Error() + ": task " + e.TaskID + ": checkout: HEAD is " + got +
		", not the pinned " + e.Want + "; the branch moved and this tree is not the one to judge"
}

// Unwrap returns the sentinels for errors.Is support.
//
// A WORKSPACE FAULT for the same reason the command failure is: the checkout
// could not be put on the tree the caller meant to act on, and that happened
// before anything was prompted. The author force-pushing over a captured
// commit is not the reviewed change's fault, and charging it would spend an
// attempt on a race.
func (e *CheckoutHeadMismatchError) Unwrap() []error {
	return []error{ErrRunner, ErrWorkspaceUnfit}
}

// WorkerNotContainerizedError reports a worker session that is a host process
// rather than a container.
//
// It is a REFUSAL at spawn, not a condition discovered at publish time. The
// delivery's commit, its signature and its push all happen inside the
// worker's container; a session with no container has nowhere to do them, and
// doing them on the daemon instead is the exact thing the container exists to
// prevent.
type WorkerNotContainerizedError struct {
	// TaskID is the task the attempt is for.
	TaskID string

	// SessionID is the session that came back without a container.
	SessionID string
}

// Error implements the error interface.
func (e *WorkerNotContainerizedError) Error() string {
	return ErrRunner.Error() + ": task " + e.TaskID + ": worker session " + e.SessionID +
		" runs no container; the delivery is committed, signed and pushed inside the worker's container or not at all"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *WorkerNotContainerizedError) Unwrap() error { return ErrRunner }

// SigningSessionError reports an ssh agent that came back unusable: no
// session, no socket to mount, or — for a delivery scope — no commit signing
// key.
//
// It names the SCOPE because the two scopes fail for different reasons and are
// fixed in different places: a checkout scope with nothing to serve is a
// project with no access key (git.ssh_key, or git.signing.key standing in for
// it), and a delivery scope with no signing key is a project whose signing
// policy is on and whose key could not be resolved.
type SigningSessionError struct {
	// Scope is what the agent was asked to serve.
	Scope SigningScope

	// Message says what came back instead.
	Message string
}

// Error implements the error interface.
func (e *SigningSessionError) Error() string {
	return ErrRunner.Error() + ": the " + e.Scope.String() + " ssh agent is unusable: " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *SigningSessionError) Unwrap() error { return ErrRunner }

// SigningSocketPathError reports a signing-agent socket whose absolute path
// cannot be bound.
//
// A unix socket address is a fixed-size kernel field, so a workspace root
// nested deeply enough silently makes signing impossible. Saying so with the
// path and the limit is the difference between a configuration an operator
// can fix and a listen error with no subject.
type SigningSocketPathError struct {
	// Path is the socket the agent would have bound.
	Path string

	// Limit is the longest path this platform accepts.
	Limit int
}

// Error implements the error interface.
func (e *SigningSocketPathError) Error() string {
	return ErrRunner.Error() + ": the signing agent's socket path is " + strconv.Itoa(len(e.Path)) +
		" bytes and the limit is " + strconv.Itoa(e.Limit) +
		"; the project's workspace root is too deeply nested to hold one: " + e.Path
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *SigningSocketPathError) Unwrap() error { return ErrRunner }

// SigningSocketRootError reports a configured sockets root under which no
// session could ever bind a socket, because the socket's absolute path would
// exceed the kernel's limit.
//
// It is raised AT STARTUP rather than at the first delivery: the arithmetic
// is fixed — the session directory and the socket's name are constant widths
// — so the root alone decides it, and an operator can fix a refusal that
// names the root and the limit.
type SigningSocketRootError struct {
	// Root is the effective orchestration.sockets_root.
	Root string

	// Longest is the length of the longest socket path Root can yield.
	Longest int

	// Limit is the longest path this platform accepts.
	Limit int
}

// Error implements the error interface.
func (e *SigningSocketRootError) Error() string {
	return ErrRunner.Error() + ": the signing agent's sockets root " + e.Root +
		" yields socket paths of up to " + strconv.Itoa(e.Longest) +
		" bytes and the limit is " + strconv.Itoa(e.Limit) +
		"; set orchestration.sockets_root to a shorter absolute path"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *SigningSocketRootError) Unwrap() error { return ErrRunner }

// SigningSocketRootUnusableError reports a sockets root that exists but
// cannot hold an agent socket safely, or one the daemon could not create.
//
// A unix socket's own mode does not protect its pathname: the containing
// directory decides who may unlink and re-bind it, so the root every session
// directory is created in must be private to the daemon's own user.
type SigningSocketRootUnusableError struct {
	// Root is the effective orchestration.sockets_root.
	Root string

	// Reason states what is wrong with it.
	Reason string

	// Cause is the underlying filesystem error, when there was one.
	Cause error
}

// Error implements the error interface.
func (e *SigningSocketRootUnusableError) Error() string {
	msg := ErrRunner.Error() + ": the signing agent's sockets root " + e.Root + " " + e.Reason +
		"; an agent socket needs a directory private to the daemon's user"
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *SigningSocketRootUnusableError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrRunner}
	}
	return []error{ErrRunner, e.Cause}
}

// SigningSocketSweepError reports a startup sweep of the signing agent's
// sockets root that could not be completed.
//
// The sweep DECLINES what it cannot positively identify as one of its own
// session directories, so this is never "something under the root looked
// odd": it is the root itself refusing to be enumerated, or a directory the
// sweep did identify as an orphan and could not probe or remove. Either
// leaves the root in a state the daemon cannot vouch for before it binds a
// socket under it, which is a startup failure for the same reason an
// unusable root is.
type SigningSocketSweepError struct {
	// Root is the effective orchestration.sockets_root.
	Root string

	// Entry is the session directory that failed, when one did. It is empty
	// when the root itself is the subject.
	Entry string

	// Reason states what could not be done.
	Reason string

	// Cause is the underlying error.
	Cause error
}

// Error implements the error interface.
func (e *SigningSocketSweepError) Error() string {
	subject := "the signing agent's sockets root " + e.Root
	if e.Entry != "" {
		subject = "the orphaned signing session " + e.Entry + " under " + e.Root
	}
	msg := ErrRunner.Error() + ": " + subject + " " + e.Reason +
		"; the daemon sweeps the sessions a killed process left before it binds a new one"
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *SigningSocketSweepError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrRunner}
	}
	return []error{ErrRunner, e.Cause}
}

// NoPublishedHeadError reports a delivery asked to become reviewable before
// anything was published for it.
//
// The commit and the push happen in the worker's container and record the
// head on the Delivery; a forge write that ran without one would open a pull
// request against a branch the remote has never seen.
type NoPublishedHeadError struct {
	// TaskID is the attempt.
	TaskID string
}

// Error implements the error interface.
func (e *NoPublishedHeadError) Error() string {
	return ErrRunner.Error() + ": task " + e.TaskID +
		": no head is recorded for this attempt; the container publishes before the forge is written"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *NoPublishedHeadError) Unwrap() error { return ErrRunner }

// VerificationNotContainerizedError reports a verification command asked to
// run for a job that spawned no containerized worker session.
//
// It is a REFUSAL, not a reason to run it somewhere else. A binding's
// verification commands are a tenant repository's build and tests; running
// them on the daemon would put arbitrary tenant code on the daemon host, and
// would judge the work against an environment the worker never saw. The
// spawn already refuses a worker with no container
// (WorkerNotContainerizedError); this is the same rule where the command
// would have run.
type VerificationNotContainerizedError struct {
	// TaskID is the attempt whose work was to be verified.
	TaskID string

	// Criterion is the verification command that was refused. The command
	// itself stays off the message: it is tenant data, and this error
	// reaches logs and forge comments.
	Criterion string
}

// Error implements the error interface.
func (e *VerificationNotContainerizedError) Error() string {
	return ErrRunner.Error() + ": task " + e.TaskID + ": criterion " + e.Criterion +
		" has no worker container to run in; verification runs where the work is or not at all"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *VerificationNotContainerizedError) Unwrap() error { return ErrRunner }

// ReviewArtifactError reports review rounds that could not be encoded for
// publication as a Task artifact.
//
// It FAILS THE HOOK rather than being logged past: the job page reads the
// review rounds only from the artifact, so a publication that dropped them
// would leave a merged run looking unreviewed.
type ReviewArtifactError struct {
	// TaskID is the attempt whose reviews were being published.
	TaskID string

	// Cause is what the encoder refused.
	Cause error
}

// Error implements the error interface.
func (e *ReviewArtifactError) Error() string {
	msg := ErrRunner.Error() + ": task " + e.TaskID + ": the review rounds could not be encoded for publication"
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *ReviewArtifactError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrRunner}
	}
	return []error{ErrRunner, e.Cause}
}
