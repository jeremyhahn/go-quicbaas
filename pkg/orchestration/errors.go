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
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

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

	// ErrOneShotNoOutput indicates a one-shot CLI exited successfully having
	// written nothing at all.
	//
	// ITS OWN SENTINEL, because "produced nothing" is a different diagnosis
	// from "produced something the SDLC module could not use", and before #241 the
	// first was reported as a completed turn holding the empty string —
	// indistinguishable from a CLI that reported {"result":""}. A caller that
	// needs to tell a silent exit from an empty answer matches on this.
	ErrOneShotNoOutput = errors.New("acp: one-shot command produced no output")

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

// ProtocolError indicates an ACP protocol violation THIS PACKAGE diagnosed.
//
// Message is authored here — "failed to parse session result" and its
// siblings — so it is rendered. Peer-supplied text does NOT belong on this
// type: see PeerRPCError, which exists so that the two cannot share a field.
type ProtocolError struct {
	Message string
	Cause   error
}

// PeerRPCError is a JSON-RPC error object the ACP peer returned.
//
// SEPARATE FROM ProtocolError SO THE SAFETY PROPERTY IS STRUCTURAL. Both are
// protocol failures, but one carries sentences this package wrote and the
// other carries text the agent subprocess wrote, and a single type holding
// both would need a branch in Error() to decide which is safe to render — a
// branch a future caller gets wrong by setting the wrong field. Two types
// cannot be confused that way (review 1330; the same choice CommandError made
// when its two authored sites moved off it).
type PeerRPCError struct {
	// Code is the JSON-RPC code, and it IS rendered. It is the structural
	// signal an operator actually needs — -32601 method not found, -32602
	// invalid params — and an integer cannot carry a credential.
	Code int

	// Message is the peer's own error text.
	//
	// IT IS NEVER RENDERED BY Error(). This is the same agent CLI whose stderr
	// #187 protected, reporting through a different channel; if we accepted
	// the diagnostic loss for its stderr we cannot decline it for its JSON-RPC
	// errors on the grounds that this channel looks tidier. It runs with
	// credential material in its environment — pooled provider accounts, MCP
	// client certificates, forge tokens — and since go-quicbaas#198 one of the
	// places an error string reaches is a COMMENT ON A FORGE ISSUE.
	//
	// The text stays on the field for callers with somewhere safe to put it.
	Message string
}

// Error implements the error interface.
//
// The code and the withheld size, never the peer's text — see the fields.
func (e *PeerRPCError) Error() string {
	msg := ErrProtocolViolation.Error() + ": peer returned JSON-RPC error " + strconv.Itoa(e.Code)
	if n := len(e.Message); n > 0 {
		msg += " (" + strconv.Itoa(n) + " bytes of peer message withheld)"
	}
	return msg
}

// Unwrap returns the sentinel, so callers matching ErrProtocolViolation keep
// matching whichever side of the split produced the failure.
func (e *PeerRPCError) Unwrap() error { return ErrProtocolViolation }

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

	// Stderr is the agent's captured diagnostic output.
	//
	// IT IS NEVER RENDERED BY Error(). See StreamJSONProcessExitedError.Stderr
	// for the whole argument; the short version is that an agent runs with
	// credential material in its environment, an error string reaches every
	// log through %v, and a field that is only unused today is a field
	// somebody logs tomorrow. Read it deliberately, by name.
	Stderr string

	// TurnFailureEvidence is the CLI's own structured account of the failure,
	// when it produced a parseable result object. Its reason class IS
	// rendered; see the type.
	TurnFailureEvidence

	Cause error
}

// Error implements the error interface.
//
// METADATA ONLY, PLUS THE REASON CLASS. The stderr tail is deliberately absent
// — see the field. The CLI's own terminal reason is not part of that payload:
// it is a closed-enum label, and withholding it is what left #235's operator
// reading "34 bytes withheld" for an authentication failure.
func (e *OneShotExecutionError) Error() string {
	msg := ErrOneShotExecution.Error() + ": " + e.Command
	if class := e.ReasonClass(); class != "" {
		msg += " terminal_reason=" + class
	}
	if diagnostic, ok := scrubEnvironmentDiagnostic(e.Stderr, e.TurnFailureEvidence); ok {
		msg += " diagnostic=" + diagnostic
	}
	if n := len(e.Stderr); n > 0 {
		msg += " (" + strconv.Itoa(n) + " bytes of stderr withheld)"
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
	// Command is the configured binary, which is operator-supplied and
	// rendered.
	Command string

	// Result is the AGENT'S OWN result text from the failing turn.
	//
	// IT IS NEVER RENDERED BY Error(), for the reason
	// StreamJSONProcessExitedError.Stderr is not — this is the same subprocess
	// and the same hazard, reached through the protocol rather than through
	// stderr. Found by the review-1330 audit: the stderr tail had been
	// protected at the type and this field, one struct away, had not, which is
	// precisely the "fixed one path, the next path still leaks" shape that
	// motivated putting the rule on the field in the first place.
	//
	// An agent's free-form result text on a FAILING turn is the most likely
	// place in this package for a credential to appear: it is whatever the
	// model wrote about what went wrong, and what went wrong is often a
	// command it ran with a token in it.
	//
	// The text stays on the field for callers with somewhere safe to put it.
	Result string

	// TurnFailureEvidence is the CLI's own structured account of the failing
	// turn — the terminal reason, the turn count, the token accounting. Its
	// reason class IS rendered; see the type.
	TurnFailureEvidence
}

// Error implements the error interface.
//
// METADATA ONLY, PLUS THE REASON CLASS. The agent's result text is deliberately
// absent — see the field.
//
// THE TERMINAL REASON IS NOT PART OF THAT PAYLOAD, and #235 is the measurement
// that says withholding it was a mistake rather than a precaution. The burn-in
// surfaced "acp: stream-json error: claude (34 bytes of result withheld)" onto
// a forge issue; the one fact a human needed — that the CLI had died on
// `api_error` before its first turn, i.e. that this was the SDLC module's
// credentials and not the work item — was in a closed-enum field beside the
// withheld text, and was thrown away with it. Redaction is for secrets. A
// reason code is not one, and it is shape-checked before it is rendered.
func (e *StreamJSONError) Error() string {
	msg := ErrStreamJSON.Error() + ": " + e.Command
	if class := e.ReasonClass(); class != "" {
		msg += " terminal_reason=" + class
	}
	if diagnostic, ok := scrubEnvironmentDiagnostic(e.Result, e.TurnFailureEvidence); ok {
		msg += " diagnostic=" + diagnostic
	}
	if n := len(e.Result); n > 0 {
		msg += " (" + strconv.Itoa(n) + " bytes of result withheld)"
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *StreamJSONError) Unwrap() error {
	return ErrStreamJSON
}

// StreamJSONProcessExitedError indicates the Claude Code stream-json subprocess
// exited unexpectedly before producing the expected output. ExitCode and Stderr
// are populated when the subprocess has been reaped and its captured stderr tail
// is available, turning an opaque "broken pipe" into an actionable diagnostic
// such as "acp: stream-json process exited: claude exited code 1: <stderr>".
type StreamJSONProcessExitedError struct {
	Command  string
	ExitCode int

	// Stderr is the bounded tail of the agent's own diagnostic output.
	//
	// IT IS NEVER RENDERED BY Error(), AND THAT IS THE POINT OF PUTTING THE
	// RULE HERE RATHER THAN AT A LOG SITE. An agent CLI runs with credential
	// material in its environment — pooled provider accounts, MCP client
	// certificates, forge tokens — and a diagnostic it prints while dying can
	// carry any of it. An error's string reaches every %v, %s, structured
	// logger field and wrapped-error chain in the process, so a redaction
	// applied at one call site is a redaction that the next call site does not
	// have. Measured: the supervisor's log line was fixed for this, and the
	// SDLC stage observer then rendered the same error type through a different
	// path (#187 review).
	//
	// The tail stays HERE, on the field, for callers that have somewhere safe
	// to put it and ask for it by name — accountLimitClassifier reads it to
	// classify a provider limit, and an operator surface behind authorization
	// can show it. Nothing gets it by accident.
	Stderr string
}

// Error implements the error interface.
//
// METADATA ONLY: the command, the exit code, and how much stderr was withheld.
// The byte count is deliberate — it tells an operator there IS evidence and
// roughly how much, without being the evidence.
func (e *StreamJSONProcessExitedError) Error() string {
	msg := ErrStreamJSONProcessExited.Error() + ": " + e.Command
	if e.ExitCode != 0 {
		msg += " exited code " + strconv.Itoa(e.ExitCode)
	}
	if n := len(e.Stderr); n > 0 {
		msg += " (" + strconv.Itoa(n) + " bytes of stderr withheld)"
	}
	return msg
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

	// ErrNoFleetNodesAvailable indicates no agents are available for a task assignment.
	ErrNoFleetNodesAvailable = errors.New("orchestration: no agents available for task")

	// ErrNoMatchingFleetNode indicates no online agent satisfies the combined
	// backend, GPU, and host-affinity constraints for the request.
	ErrNoMatchingFleetNode = errors.New("orchestration: no online agent matches constraints")

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

// NoFleetNodesAvailableError indicates no agents are available for a task assignment.
type NoFleetNodesAvailableError struct {
	TaskTitle            string
	RequiredCapabilities []string
}

// Error implements the error interface.
func (e *NoFleetNodesAvailableError) Error() string {
	msg := ErrNoFleetNodesAvailable.Error() + ": task=" + e.TaskTitle
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
func (e *NoFleetNodesAvailableError) Unwrap() error {
	return ErrNoFleetNodesAvailable
}

// WorkflowAgentIDError reports that no ID could be minted for a synthesized
// workflow agent, because the random source failed.
type WorkflowAgentIDError struct {
	Role  string
	Cause error
}

// Error implements the error interface.
func (e *WorkflowAgentIDError) Error() string {
	return "orchestration: could not mint an ID for workflow agent role " + e.Role + ": " + e.Cause.Error()
}

// Unwrap returns the random-source failure.
func (e *WorkflowAgentIDError) Unwrap() error {
	return e.Cause
}

// NoMatchingFleetNodeError indicates no online agent satisfies the combined
// backend, GPU, and host-affinity constraints for the request.
type NoMatchingFleetNodeError struct {
	// Constraint describes which constraint failed (e.g. "gpu", "host_affinity", "backend").
	Constraint string
	// Detail provides a human-readable summary of the unsatisfied constraint.
	Detail string
}

// Error implements the error interface.
func (e *NoMatchingFleetNodeError) Error() string {
	return ErrNoMatchingFleetNode.Error() + ": " + e.Detail
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *NoMatchingFleetNodeError) Unwrap() error {
	return ErrNoMatchingFleetNode
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

	// ErrJobStatusContended indicates a job status update lost its
	// compare-and-swap on every attempt the retry bound allows.
	ErrJobStatusContended = errors.New("orchestration: job status too contended to update")

	// ErrJobStoreUnguarded indicates a job status write was refused because
	// the job store cannot guard it: it lacks the guarded create or swap.
	ErrJobStoreUnguarded = errors.New("orchestration: job store cannot guard a job record write")

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

// JobStatusContendedError reports a job status update whose compare-and-swap
// lost to another writer on every attempt. The mutation was NOT applied.
type JobStatusContendedError struct {
	JobID    string
	Attempts int
}

// Error implements the error interface.
func (e *JobStatusContendedError) Error() string {
	return ErrJobStatusContended.Error() + ": job=" + e.JobID +
		": lost the swap on all " + strconv.Itoa(e.Attempts) + " attempts"
}

// Unwrap returns the contention sentinel and the persistence sentinel, so a
// caller matching either finds it.
func (e *JobStatusContendedError) Unwrap() []error {
	return []error{ErrJobStatusContended, ErrJobPersistenceFailed}
}

// JobStoreUnguardedError reports a job status write refused because the job
// store lacks a guarded verb the write needs: CompareAndSwap for every write,
// PutIfAbsent as well for a whole snapshot. Nothing was written.
type JobStoreUnguardedError struct {
	JobID string
}

// Error implements the error interface.
func (e *JobStoreUnguardedError) Error() string {
	return ErrJobStoreUnguarded.Error() + ": job=" + e.JobID
}

// Unwrap returns the unguarded-store sentinel and the persistence sentinel.
func (e *JobStoreUnguardedError) Unwrap() []error {
	return []error{ErrJobStoreUnguarded, ErrJobPersistenceFailed}
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

// =============================================================================
// Closed store
// =============================================================================

var (
	// ErrStoreClosed indicates an operation reached the store after the
	// daemon closed it.
	ErrStoreClosed = errors.New("orchestration: store closed")
)

// storeClosedMarkers are the closed-store conditions a job can meet on the
// way out while the node is shutting down.
//
// THEY ARE MATCHED BY TEXT ON PURPOSE. The engine under go-qrdb reports an
// already-closed store by PANICKING rather than by returning an error, and
// the value it panics with crosses the SDK boundary as an opaque one: there
// is no exported type or sentinel on this side of the boundary to match it
// against. The markers are the stable prefixes those values carry.
var storeClosedMarkers = []string{
	"pebble: closed",   // the storage engine under go-qrdb
	"backend: closed",  // go-qrdb's local backend
	"client is closed", // go-qrdb's SDK client
	"store is closed",
	"database is closed",
}

// StoreClosedError reports work that could not be recorded because the store
// had already been closed — the daemon is shutting down under a job that is
// still writing. It is not a failure of the job: the job's record is simply
// the one it had before the store went away, and a restart resumes it from
// there.
type StoreClosedError struct {
	// JobID is the job whose write met the closed store, empty when the
	// write did not belong to one.
	JobID string

	// Op names the operation that met it, for the log line.
	Op string

	// Value is how the store reported being closed.
	Value string
}

// Error implements the error interface.
func (e *StoreClosedError) Error() string {
	msg := ErrStoreClosed.Error() + ": op=" + e.Op
	if e.JobID != "" {
		msg += " job=" + e.JobID
	}
	if e.Value != "" {
		msg += ": " + e.Value
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *StoreClosedError) Unwrap() error { return ErrStoreClosed }

// IsStoreClosed reports whether v is the store refusing an operation because
// it has been closed. v is either an error or a value recovered from a panic,
// because the store reports the condition both ways.
func IsStoreClosed(v any) bool {
	switch value := v.(type) {
	case nil:
		return false
	case error:
		if errors.Is(value, ErrStoreClosed) {
			return true
		}
		return matchesStoreClosed(value.Error())
	case string:
		return matchesStoreClosed(value)
	default:
		return false
	}
}

// matchesStoreClosed reports whether text names a closed store.
func matchesStoreClosed(text string) bool {
	lowered := strings.ToLower(text)
	for _, marker := range storeClosedMarkers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

// guardStoreClosed runs fn and returns a StoreClosedError when fn met a
// closed store, nil when it did not.
//
// EVERY OTHER PANIC IS RE-RAISED. A panic that is not the store going away
// is a bug, and a guard that swallowed it would hide the bug and leave the
// daemon running on state nothing checked. Only the one condition the daemon
// creates for itself by closing storage under its own running work is
// converted into an error.
func guardStoreClosed(op, jobID string, fn func()) (closed *StoreClosedError) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		if !IsStoreClosed(recovered) {
			panic(recovered)
		}
		closed = &StoreClosedError{JobID: jobID, Op: op, Value: fmt.Sprint(recovered)}
	}()
	fn()
	return nil
}

// =============================================================================
// Shutdown drain
// =============================================================================

var (
	// ErrDrainTimeout indicates the shutdown drain gave up waiting for work
	// that was still running.
	ErrDrainTimeout = errors.New("orchestration: drain timed out")

	// ErrServiceShuttingDown indicates the service refused an operation
	// because it is draining for shutdown.
	ErrServiceShuttingDown = errors.New("orchestration: service is shutting down")
)

// DrainTimeoutError reports a shutdown drain that reached its bound with work
// still in flight. It is not a failure of that work: what it names keeps its
// non-terminal record and is resumed by RecoverJobs on the next start.
type DrainTimeoutError struct {
	// Phase is the drain phase that gave up, DrainPhaseJobs or
	// DrainPhaseTurns.
	Phase string

	// Waited is how long that phase waited before giving up.
	Waited time.Duration

	// Running names what was still in flight, keyed as tenant/job for a
	// job. Empty when the phase cannot enumerate its work.
	Running []string
}

// Error implements the error interface.
func (e *DrainTimeoutError) Error() string {
	msg := ErrDrainTimeout.Error() + ": phase=" + e.Phase + " waited=" + e.Waited.String()
	if len(e.Running) > 0 {
		msg += " running=" + strings.Join(e.Running, ",")
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *DrainTimeoutError) Unwrap() error { return ErrDrainTimeout }

// ServiceShuttingDownError is returned when an operation is refused because
// the service has begun draining for shutdown.
type ServiceShuttingDownError struct {
	// Operation names what was refused.
	Operation string
}

// Error implements the error interface.
func (e *ServiceShuttingDownError) Error() string {
	if e.Operation == "" {
		return ErrServiceShuttingDown.Error()
	}
	return ErrServiceShuttingDown.Error() + ": " + e.Operation
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ServiceShuttingDownError) Unwrap() error { return ErrServiceShuttingDown }

// CredentialDirMissingError reports a backend whose file-based credential
// directory was configured but did not exist to mount.
//
// IT IS AN ERROR RATHER THAN A DEGRADED SPAWN because the CLI is told where
// the credential went through its own environment variable, and a variable
// pointing at an unmounted path sends it looking in an empty container
// directory. The CLI then fails with something about its own configuration,
// far from the cause, and the spawn reports only the byte count of evidence it
// withheld. Naming the directory here costs the same attempt and says what to
// repair.
type CredentialDirMissingError struct {
	// Backend is the CLI the credential belongs to.
	Backend string

	// Source is the host path that was expected to hold it; empty when no
	// account home was resolved and the process has no home directory either.
	Source string

	// Target is where the mount would have landed in the container.
	Target string

	// Rel is the credential directory's path relative to the account home,
	// which is what a pooled account's stored files must be keyed under.
	Rel string
}

// Error implements the error interface.
func (e *CredentialDirMissingError) Error() string {
	source := e.Source
	if source == "" {
		source = "(no account home and no user home)"
	}
	return "orchestration: " + e.Backend + " credential directory not found: " +
		source + " does not exist, so " + e.Target + " would be empty; " +
		"a pooled account's files must be keyed under " + e.Rel + "/"
}
