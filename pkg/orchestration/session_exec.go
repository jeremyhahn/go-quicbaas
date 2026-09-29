// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
)

// =============================================================================
// Running one command in the container a session's agent runs in
// =============================================================================
//
// A sandboxed session IS a container, and the engine sometimes has to do
// something in it that is not the agent's turn to decide: a delivery's commit
// and push happen once, with the identity the project configured, whatever
// the worker said that turn. ExecSession is that door and only that door.
//
// IT IS DELIBERATELY OFF THE AgentService INTERFACE, and off every transport.
// A remote caller able to name a session and a command would have arbitrary
// code execution inside another tenant's sandbox, and every AgentService verb
// is transport-reachable by construction. So this follows the pattern
// AttestingContainerRuntime already established in this package: an optional
// capability a caller type-asserts for, implemented by the one service that
// owns a container runtime, and absent by default.

// SessionExecutor runs a command inside the container backing a session.
//
// agentServiceImpl implements it. A caller holding an AgentService asserts for
// it rather than requiring every implementation — the remote fleet node client, the
// SDK's orchestrator client, a test's fake — to carry a container runtime it
// does not have.
type SessionExecutor interface {
	// ExecSession runs one command inside the container the named session's
	// agent runs in. A session with no container is refused, never run
	// somewhere else.
	ExecSession(ctx context.Context, req *ExecSessionRequest) (*ExecResult, error)
}

// ExecSessionRequest names the session and the command.
type ExecSessionRequest struct {
	// SessionID is the session whose container the command runs in.
	SessionID string `json:"session_id"`

	// Spec is the command, its environment, its working directory inside the
	// container, and its stdin.
	Spec *ExecSpec `json:"spec"`
}

// ErrSessionNotContainerized indicates a session that runs as a host process,
// where a container command has nowhere to run.
var ErrSessionNotContainerized = errors.New("session: not containerized")

// SessionNotContainerizedError reports a session whose agent is a host
// process rather than a container.
//
// It is a REFUSAL, not a condition to work around. Running the command on the
// daemon instead would put a tenant's git — its author, its signature, its
// credentials — on the host the daemon runs on, which is the entire thing the
// container exists to prevent.
type SessionNotContainerizedError struct {
	SessionID string
}

// Error implements the error interface.
func (e *SessionNotContainerizedError) Error() string {
	return ErrSessionNotContainerized.Error() + ": " + e.SessionID +
		": the command runs in the session's container or not at all"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *SessionNotContainerizedError) Unwrap() error { return ErrSessionNotContainerized }

// ErrSessionExecUnsupported indicates an agent service that cannot run a
// command in a session's container.
var ErrSessionExecUnsupported = errors.New("session: exec unsupported")

// SessionExecUnsupportedError reports an agent service with no SessionExecutor
// behind it — a remote or embedded client, or a service with no container
// runtime wired.
type SessionExecUnsupportedError struct {
	SessionID string
}

// Error implements the error interface.
func (e *SessionExecUnsupportedError) Error() string {
	return ErrSessionExecUnsupported.Error() + ": " + e.SessionID +
		": this agent service owns no container runtime"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *SessionExecUnsupportedError) Unwrap() error { return ErrSessionExecUnsupported }

// Compile-time check: the engine's own service is the SessionExecutor.
var _ SessionExecutor = (*agentServiceImpl)(nil)

// ExecSession implements SessionExecutor.
//
// The container is resolved from the session's own record — the live one
// first, the durable one after a resume rebuilt neither — so the caller names
// a SESSION and never a container id it could have guessed.
func (s *agentServiceImpl) ExecSession(ctx context.Context, req *ExecSessionRequest) (*ExecResult, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.SessionID == "" {
		return nil, &AgentServiceError{Sentinel: ErrSessionIDRequired}
	}
	if err := req.Spec.Validate(); err != nil {
		return nil, err
	}
	// A SESSION ON A FLEET AGENT IS EXECUTED ON THAT AGENT. Its container is
	// on the agent's host, known to the agent's container runtime and to no
	// other; asking this daemon's runtime for it answers "no such container"
	// at best (go-quicbaas#981).
	if remote, host, err := s.remoteSessionHost(ctx, req.SessionID); err != nil {
		return nil, err
	} else if remote {
		return s.execOnAgent(ctx, req, host)
	}
	if s.containerRuntime == nil {
		return nil, &SessionExecUnsupportedError{SessionID: req.SessionID}
	}
	containerID, err := s.sessionContainerID(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}
	return s.containerRuntime.Exec(ctx, containerID, req.Spec)
}

// SessionContainerExecutor is the capability of a backend whose sessions'
// containers live on another host: it runs a command in one of them there.
// RemoteACPBackend implements it over the agent's authenticated FleetRuntime
// link.
type SessionContainerExecutor interface {
	ExecInSession(ctx context.Context, sessionID string, spec *ExecSpec) (*ExecResult, error)
}

// remoteSessionHost reports whether a session lives on a fleet node, and
// which. The caller's tenant must own the session, as for the local path.
func (s *agentServiceImpl) remoteSessionHost(ctx context.Context, sessionID string) (bool, string, error) {
	if mem, ok := s.liveSessionRecord(sessionID); ok {
		if caller := TenantFromContext(ctx); caller != "" && caller != mem.TenantID {
			return false, "", &SessionNotFoundError{SessionID: sessionID}
		}
		return !mem.BackendLocal && mem.HostAffinity != "", mem.HostAffinity, nil
	}
	record, err := s.store.GetSession(ctx, StorageTenant(ctx), sessionID)
	if err != nil {
		return false, "", err
	}
	if record == nil {
		return false, "", &SessionNotFoundError{SessionID: sessionID}
	}
	return !record.BackendLocal && record.HostAffinity != "", record.HostAffinity, nil
}

// execOnAgent runs the command on the agent that owns the session's
// container, through the backend routed to that agent. It reaches that agent
// or fails; there is no other host the container could be on.
func (s *agentServiceImpl) execOnAgent(ctx context.Context, req *ExecSessionRequest, host string) (*ExecResult, error) {
	backendName, err := s.findSessionBackend(req.SessionID)
	if err != nil {
		record, storeErr := s.store.GetSession(ctx, StorageTenant(ctx), req.SessionID)
		if storeErr != nil || record == nil {
			return nil, err
		}
		backendName = record.Backend
	}
	routed := withRemoteSessionPin(WithHostAffinityStrict(WithHostAffinity(ctx, host)), req.SessionID, host)
	backend, err := s.lookupBackendCtx(routed, backendName, "")
	if err != nil {
		return nil, err
	}
	executor, ok := backend.(SessionContainerExecutor)
	if !ok {
		return nil, &SessionExecUnsupportedError{SessionID: req.SessionID}
	}
	return executor.ExecInSession(ctx, req.SessionID, req.Spec)
}

// sessionContainerID answers which container a session's agent runs in.
func (s *agentServiceImpl) sessionContainerID(ctx context.Context, sessionID string) (string, error) {
	if mem, ok := s.liveSessionRecord(sessionID); ok {
		// The live record is a process-wide map, not a tenant-scoped read.
		// A caller naming another tenant's session is told it does not
		// exist rather than that it belongs to someone else — the same
		// answer, and the same reasoning, as the resume path
		// (orchestrator_resume.go). Without this the store read below would
		// be scoped and this one would not, which is the whole isolation.
		if caller := TenantFromContext(ctx); caller != "" && caller != mem.TenantID {
			return "", &SessionNotFoundError{SessionID: sessionID}
		}
		if mem.ContainerID == "" {
			return "", &SessionNotContainerizedError{SessionID: sessionID}
		}
		return mem.ContainerID, nil
	}
	record, err := s.store.GetSession(ctx, StorageTenant(ctx), sessionID)
	if err != nil {
		return "", err
	}
	if record == nil {
		return "", &SessionNotFoundError{SessionID: sessionID}
	}
	if record.ContainerID == "" {
		return "", &SessionNotContainerizedError{SessionID: sessionID}
	}
	return record.ContainerID, nil
}
