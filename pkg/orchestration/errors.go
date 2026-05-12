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

import "errors"

// Sentinel errors for the acp package.
var (
	// ErrSessionNotFound indicates a session was not found.
	ErrSessionNotFound = errors.New("acp: session not found")

	// ErrTransportFailed indicates a transport-level failure.
	ErrTransportFailed = errors.New("acp: transport failed")

	// ErrProtocolViolation indicates an ACP protocol violation.
	ErrProtocolViolation = errors.New("acp: protocol violation")

	// ErrBackendNotFound indicates the requested backend does not exist.
	ErrBackendNotFound = errors.New("acp: backend not found")

	// ErrUnsupportedOperation indicates the backend does not support the operation.
	ErrUnsupportedOperation = errors.New("acp: unsupported operation")

	// ErrSessionClosed indicates the session has been closed.
	ErrSessionClosed = errors.New("acp: session closed")

	// ErrSpawnFailed indicates a session spawn failure.
	ErrSpawnFailed = errors.New("acp: spawn failed")

	// ErrPromptFailed indicates a prompt failure.
	ErrPromptFailed = errors.New("acp: prompt failed")

	// ErrInvalidInput indicates invalid input was provided.
	ErrInvalidInput = errors.New("acp: invalid input")

	// ErrOneShotExecution indicates a one-shot CLI execution failure.
	ErrOneShotExecution = errors.New("acp: one-shot execution failed")

	// ErrStreamJSON indicates a stream-json protocol error from Claude Code.
	ErrStreamJSON = errors.New("acp: stream-json error")

	// ErrStreamJSONProcessExited indicates the Claude Code stream-json
	// subprocess exited unexpectedly.
	ErrStreamJSONProcessExited = errors.New("acp: stream-json process exited")

	// ErrChannelServer indicates a general channel server error.
	ErrChannelServer = errors.New("channel: server error")

	// ErrChannelEvent indicates a failed event push.
	ErrChannelEvent = errors.New("channel: event error")

	// ErrChannelConnectionClosed indicates the stdio connection was closed.
	ErrChannelConnectionClosed = errors.New("channel: connection closed")
)

// SessionNotFoundError indicates a session was not found.
type SessionNotFoundError struct {
	SessionID string
}

// Error implements the error interface.
func (e *SessionNotFoundError) Error() string {
	return ErrSessionNotFound.Error() + ": " + e.SessionID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *SessionNotFoundError) Unwrap() error {
	return ErrSessionNotFound
}

// SessionClosedError indicates an operation was attempted on a closed session.
type SessionClosedError struct {
	SessionID string
}

// Error implements the error interface.
func (e *SessionClosedError) Error() string {
	return ErrSessionClosed.Error() + ": " + e.SessionID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *SessionClosedError) Unwrap() error {
	return ErrSessionClosed
}

// TransportError indicates a transport-level failure.
type TransportError struct {
	Operation string
	Cause     error
}

// Error implements the error interface.
func (e *TransportError) Error() string {
	return ErrTransportFailed.Error() + ": " + e.Operation + ": " + e.Cause.Error()
}

// Unwrap returns both the sentinel and cause for errors.Is/As support.
func (e *TransportError) Unwrap() []error {
	return []error{ErrTransportFailed, e.Cause}
}

// ProtocolError indicates an ACP protocol violation.
type ProtocolError struct {
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *ProtocolError) Error() string {
	if e.Cause != nil {
		return ErrProtocolViolation.Error() + ": " + e.Message + ": " + e.Cause.Error()
	}
	return ErrProtocolViolation.Error() + ": " + e.Message
}

// Unwrap returns the sentinel and optional cause for errors.Is/As support.
func (e *ProtocolError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrProtocolViolation, e.Cause}
	}
	return []error{ErrProtocolViolation}
}

// BackendNotFoundError indicates the requested backend does not exist.
type BackendNotFoundError struct {
	Backend string
}

// Error implements the error interface.
func (e *BackendNotFoundError) Error() string {
	return ErrBackendNotFound.Error() + ": " + e.Backend
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *BackendNotFoundError) Unwrap() error {
	return ErrBackendNotFound
}

// UnsupportedOperationError indicates the backend does not support the operation.
type UnsupportedOperationError struct {
	Backend   string
	Operation string
}

// Error implements the error interface.
func (e *UnsupportedOperationError) Error() string {
	return ErrUnsupportedOperation.Error() + ": " + e.Backend + " does not support " + e.Operation
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UnsupportedOperationError) Unwrap() error {
	return ErrUnsupportedOperation
}

// SpawnError indicates a session spawn failure.
type SpawnError struct {
	Command string
	Cause   error
}

// Error implements the error interface.
func (e *SpawnError) Error() string {
	return ErrSpawnFailed.Error() + ": " + e.Command + ": " + e.Cause.Error()
}

// Unwrap returns both the sentinel and cause for errors.Is/As support.
func (e *SpawnError) Unwrap() []error {
	return []error{ErrSpawnFailed, e.Cause}
}

// PromptError indicates a prompt failure.
type PromptError struct {
	SessionID string
	Cause     error
}

// Error implements the error interface.
func (e *PromptError) Error() string {
	return ErrPromptFailed.Error() + ": session " + e.SessionID + ": " + e.Cause.Error()
}

// Unwrap returns both the sentinel and cause for errors.Is/As support.
func (e *PromptError) Unwrap() []error {
	return []error{ErrPromptFailed, e.Cause}
}

// InputValidationError indicates invalid input was provided.
type InputValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *InputValidationError) Error() string {
	return ErrInvalidInput.Error() + ": " + e.Field + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *InputValidationError) Unwrap() error {
	return ErrInvalidInput
}

// OneShotExecutionError indicates a failure when executing a CLI agent in
// one-shot mode (e.g., claude --print).
type OneShotExecutionError struct {
	Command string
	Stderr  string
	Cause   error
}

// Error implements the error interface.
func (e *OneShotExecutionError) Error() string {
	msg := ErrOneShotExecution.Error() + ": " + e.Command
	if e.Stderr != "" {
		msg += ": " + e.Stderr
	}
	return msg
}

// Unwrap returns the sentinel and optional cause for errors.Is/As support.
func (e *OneShotExecutionError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrOneShotExecution, e.Cause}
	}
	return []error{ErrOneShotExecution}
}

// StreamJSONError indicates the Claude Code stream-json protocol returned
// an error result.
type StreamJSONError struct {
	Command string
	Result  string
}

// Error implements the error interface.
func (e *StreamJSONError) Error() string {
	msg := ErrStreamJSON.Error() + ": " + e.Command
	if e.Result != "" {
		msg += ": " + e.Result
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *StreamJSONError) Unwrap() error {
	return ErrStreamJSON
}

// StreamJSONProcessExitedError indicates the Claude Code stream-json subprocess
// exited unexpectedly before producing the expected output.
type StreamJSONProcessExitedError struct {
	Command string
}

// Error implements the error interface.
func (e *StreamJSONProcessExitedError) Error() string {
	return ErrStreamJSONProcessExited.Error() + ": " + e.Command
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *StreamJSONProcessExitedError) Unwrap() error {
	return ErrStreamJSONProcessExited
}

// ChannelServerError indicates a general channel server error.
type ChannelServerError struct {
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *ChannelServerError) Error() string {
	if e.Cause != nil {
		return ErrChannelServer.Error() + ": " + e.Message + ": " + e.Cause.Error()
	}
	return ErrChannelServer.Error() + ": " + e.Message
}

// Unwrap returns the sentinel and optional cause for errors.Is/As support.
func (e *ChannelServerError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrChannelServer, e.Cause}
	}
	return []error{ErrChannelServer}
}

// ChannelEventError indicates a failed event push.
type ChannelEventError struct {
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *ChannelEventError) Error() string {
	if e.Cause != nil {
		return ErrChannelEvent.Error() + ": " + e.Message + ": " + e.Cause.Error()
	}
	return ErrChannelEvent.Error() + ": " + e.Message
}

// Unwrap returns the sentinel and optional cause for errors.Is/As support.
func (e *ChannelEventError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrChannelEvent, e.Cause}
	}
	return []error{ErrChannelEvent}
}

// ChannelConnectionClosedError indicates the stdio connection was closed.
type ChannelConnectionClosedError struct {
	Reason string
}

// Error implements the error interface.
func (e *ChannelConnectionClosedError) Error() string {
	return ErrChannelConnectionClosed.Error() + ": " + e.Reason
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ChannelConnectionClosedError) Unwrap() error {
	return ErrChannelConnectionClosed
}

// =============================================================================
// Control Loop Sentinel Errors
// =============================================================================

var (
	// ErrMaxIterationsExceeded indicates the control loop exceeded its maximum iteration count.
	ErrMaxIterationsExceeded = errors.New("orchestration: max iterations exceeded")

	// ErrNoAgentsAvailable indicates no agents are available for a task assignment.
	ErrNoAgentsAvailable = errors.New("orchestration: no agents available for task")

	// ErrNoMatchingAgent indicates no online agent satisfies the combined
	// backend, GPU, and host-affinity constraints for the request.
	ErrNoMatchingAgent = errors.New("orchestration: no online agent matches constraints")

	// ErrReviewParseFailed indicates the lead agent's review response could not be parsed.
	ErrReviewParseFailed = errors.New("orchestration: failed to parse review response")

	// ErrJobNotFound indicates an orchestration job was not found.
	ErrJobNotFound = errors.New("orchestration: job not found")

	// ErrJobAlreadyRunning indicates an orchestration job is already running.
	ErrJobAlreadyRunning = errors.New("orchestration: job already running")

	// ErrEmptyDecomposition indicates the lead agent returned no work items.
	ErrEmptyDecomposition = errors.New("orchestration: empty decomposition result")

	// ErrControlLoopFailed indicates a general control loop failure.
	ErrControlLoopFailed = errors.New("orchestration: control loop failed")

	// ErrSchedulingFailed indicates the DAG scheduling phase failed.
	ErrSchedulingFailed = errors.New("orchestration: scheduling failed")

	// ErrValidationFailed indicates the validation pipeline failed.
	ErrValidationFailed = errors.New("orchestration: validation failed")

	// ErrEscalation indicates the job has been escalated due to exceeded limits.
	ErrEscalation = errors.New("orchestration: job escalated")

	// ErrWorkerSessionFailed indicates a worker session operation failed.
	ErrWorkerSessionFailed = errors.New("orchestration: worker session failed")
)

// =============================================================================
// Control Loop Typed Errors
// =============================================================================

// MaxIterationsError indicates the control loop exceeded its maximum iteration count.
type MaxIterationsError struct {
	JobID      string
	Iterations int
}

// Error implements the error interface.
func (e *MaxIterationsError) Error() string {
	return ErrMaxIterationsExceeded.Error() + ": job=" + e.JobID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *MaxIterationsError) Unwrap() error {
	return ErrMaxIterationsExceeded
}

// NoAgentsAvailableError indicates no agents are available for a task assignment.
type NoAgentsAvailableError struct {
	TaskTitle            string
	RequiredCapabilities []string
}

// Error implements the error interface.
func (e *NoAgentsAvailableError) Error() string {
	msg := ErrNoAgentsAvailable.Error() + ": task=" + e.TaskTitle
	if len(e.RequiredCapabilities) > 0 {
		msg += " capabilities=["
		for i, cap := range e.RequiredCapabilities {
			if i > 0 {
				msg += ","
			}
			msg += cap
		}
		msg += "]"
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *NoAgentsAvailableError) Unwrap() error {
	return ErrNoAgentsAvailable
}

// NoMatchingAgentError indicates no online agent satisfies the combined
// backend, GPU, and host-affinity constraints for the request.
type NoMatchingAgentError struct {
	// Constraint describes which constraint failed (e.g. "gpu", "host_affinity", "backend").
	Constraint string
	// Detail provides a human-readable summary of the unsatisfied constraint.
	Detail string
}

// Error implements the error interface.
func (e *NoMatchingAgentError) Error() string {
	return ErrNoMatchingAgent.Error() + ": " + e.Detail
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *NoMatchingAgentError) Unwrap() error {
	return ErrNoMatchingAgent
}

// ReviewParseError indicates the lead agent review response was not valid JSON.
type ReviewParseError struct {
	RawResponse string
	Cause       error
}

// Error implements the error interface.
func (e *ReviewParseError) Error() string {
	if e.Cause != nil {
		return ErrReviewParseFailed.Error() + ": " + e.Cause.Error()
	}
	return ErrReviewParseFailed.Error()
}

// Unwrap returns the sentinel and optional cause for errors.Is/As support.
func (e *ReviewParseError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrReviewParseFailed, e.Cause}
	}
	return []error{ErrReviewParseFailed}
}

// JobNotFoundError indicates an orchestration job was not found by ID.
type JobNotFoundError struct {
	JobID string
}

// Error implements the error interface.
func (e *JobNotFoundError) Error() string {
	return ErrJobNotFound.Error() + ": " + e.JobID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *JobNotFoundError) Unwrap() error {
	return ErrJobNotFound
}

// EmptyDecompositionError indicates the lead agent returned no work items.
type EmptyDecompositionError struct {
	Description string
}

// Error implements the error interface.
func (e *EmptyDecompositionError) Error() string {
	return ErrEmptyDecomposition.Error() + ": " + e.Description
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *EmptyDecompositionError) Unwrap() error {
	return ErrEmptyDecomposition
}

// ControlLoopError wraps a sentinel error with an additional message.
type ControlLoopError struct {
	Sentinel error
	Message  string
}

// Error implements the error interface.
func (e *ControlLoopError) Error() string {
	if e.Message != "" {
		return e.Sentinel.Error() + ": " + e.Message
	}
	return e.Sentinel.Error()
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ControlLoopError) Unwrap() error {
	return e.Sentinel
}

// SchedulingError indicates the DAG scheduling phase encountered an error.
type SchedulingError struct {
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *SchedulingError) Error() string {
	if e.Cause != nil {
		return ErrSchedulingFailed.Error() + ": " + e.Message + ": " + e.Cause.Error()
	}
	return ErrSchedulingFailed.Error() + ": " + e.Message
}

// Unwrap returns the sentinel and optional cause for errors.Is/As support.
func (e *SchedulingError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrSchedulingFailed, e.Cause}
	}
	return []error{ErrSchedulingFailed}
}

// EscalationError indicates the job was escalated due to exceeded iteration limits.
type EscalationError struct {
	JobID      string
	Iterations int
	Reason     string
}

// Error implements the error interface.
func (e *EscalationError) Error() string {
	return ErrEscalation.Error() + ": job=" + e.JobID + " iterations=" + itoa(e.Iterations) + " reason=" + e.Reason
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *EscalationError) Unwrap() error {
	return ErrEscalation
}

// WorkerSessionError indicates a worker session operation failed.
type WorkerSessionError struct {
	Role    string
	Backend string
	Cause   error
}

// Error implements the error interface.
func (e *WorkerSessionError) Error() string {
	msg := ErrWorkerSessionFailed.Error() + ": role=" + e.Role + " backend=" + e.Backend
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and optional cause for errors.Is/As support.
func (e *WorkerSessionError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrWorkerSessionFailed, e.Cause}
	}
	return []error{ErrWorkerSessionFailed}
}

// itoa converts an int to its string representation without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// =============================================================================
// Job Persistence Errors
// =============================================================================

var (
	// ErrJobPersistenceFailed indicates a job status persistence operation failed.
	ErrJobPersistenceFailed = errors.New("orchestration: job persistence failed")

	// ErrJobRecoveryFailed indicates job recovery from persistent storage failed.
	ErrJobRecoveryFailed = errors.New("orchestration: job recovery failed")

	// ErrKVStoreRequired indicates a KV store is required but not configured.
	ErrKVStoreRequired = errors.New("orchestration: kv store is required")
)

// JobPersistenceError indicates a failure during job status persistence.
type JobPersistenceError struct {
	JobID   string
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *JobPersistenceError) Error() string {
	msg := ErrJobPersistenceFailed.Error()
	if e.JobID != "" {
		msg += ": job=" + e.JobID
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and optional cause for errors.Is/As support.
func (e *JobPersistenceError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrJobPersistenceFailed, e.Cause}
	}
	return []error{ErrJobPersistenceFailed}
}

// JobRecoveryError indicates a failure during job recovery from persistent storage.
type JobRecoveryError struct {
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *JobRecoveryError) Error() string {
	msg := ErrJobRecoveryFailed.Error()
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and optional cause for errors.Is/As support.
func (e *JobRecoveryError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrJobRecoveryFailed, e.Cause}
	}
	return []error{ErrJobRecoveryFailed}
}

// =============================================================================
// Job Resumption Errors
// =============================================================================

var (
	// ErrJobResumeFailed indicates job resumption from persisted state failed.
	ErrJobResumeFailed = errors.New("orchestration: job resume failed")
)

// JobResumeError indicates a failure during job resumption from persisted state.
type JobResumeError struct {
	JobID   string
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *JobResumeError) Error() string {
	msg := ErrJobResumeFailed.Error()
	if e.JobID != "" {
		msg += ": job=" + e.JobID
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and optional cause for errors.Is/As support.
func (e *JobResumeError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrJobResumeFailed, e.Cause}
	}
	return []error{ErrJobResumeFailed}
}

// =============================================================================
// Sub-job Spawning Errors
// =============================================================================

var (
	// ErrNotLeadOfParent indicates the calling agent is not the registered
	// lead of the parent job and therefore may not spawn child jobs.
	ErrNotLeadOfParent = errors.New("orchestration: not lead of parent job")

	// ErrParentJobNotFound indicates the parent job ID does not exist.
	ErrParentJobNotFound = errors.New("orchestration: parent job not found")
)

// NotLeadOfParentError is returned when an agent attempts to spawn a child
// job but is not the registered lead of the referenced parent job.
type NotLeadOfParentError struct {
	ParentJobID   string
	CallerAgentID string
	LeadAgentID   string
}

// Error implements the error interface.
func (e *NotLeadOfParentError) Error() string {
	return ErrNotLeadOfParent.Error() + ": parent=" + e.ParentJobID +
		" caller=" + e.CallerAgentID + " lead=" + e.LeadAgentID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *NotLeadOfParentError) Unwrap() error { return ErrNotLeadOfParent }

// ParentJobNotFoundError is returned when the referenced parent job does not exist.
type ParentJobNotFoundError struct {
	ParentJobID string
}

// Error implements the error interface.
func (e *ParentJobNotFoundError) Error() string {
	return ErrParentJobNotFound.Error() + ": " + e.ParentJobID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ParentJobNotFoundError) Unwrap() error { return ErrParentJobNotFound }
