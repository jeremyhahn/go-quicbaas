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
	"strings"
)

// =============================================================================
// SpawnSession's half of a named workspace
// =============================================================================
//
// A spawn that names its workspace (SpawnSessionRequest.Workspace) is placed
// and provisioned in three steps, each of which refuses rather than degrades:
//
//  1. admitSessionWorkspace — the name is well formed, belongs to the tenant
//     the spawn runs under, and is not combined with a host path.
//  2. workspaceSpawnContext / requireWorkspacePlacement — a pinned workspace
//     resolves to the host that holds it, strictly, and the resolution is
//     checked after the fact. The dispatcher's soft fallback (a pinned host
//     that is offline falls through to the in-process backend) is exactly
//     the "fresh clone somewhere else" a pin exists to forbid.
//  3. provisionDaemonWorkspace — a spawn that resolved to this daemon's own
//     backend is provisioned here, under this daemon's projects root. A spawn
//     that resolved to a fleet node is provisioned THERE, and the agent
//     reports the directory back (requireAgentWorkspace).

// SetWorkspaceProvisioner configures the provisioner a spawn that names its
// workspace uses when it runs on this daemon's own in-process backend. Nil
// leaves such spawns refused with SessionWorkspaceUnsupportedError; a spawn
// dispatched to a fleet node is provisioned by that agent and never needs it.
func (s *agentServiceImpl) SetWorkspaceProvisioner(p WorkspaceProvisioner) {
	s.workspaces = p
}

// admitSessionWorkspace refuses a named workspace before anything is resolved
// or started.
func admitSessionWorkspace(ctx context.Context, req *SpawnSessionRequest) error {
	ws := req.Workspace
	if err := ws.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(req.WorkDir) != "" {
		return &SessionWorkspaceInvalidError{
			Field:  "work_dir",
			Reason: "a spawn names its workspace or gives a work directory, never both",
		}
	}
	// THE WORKSPACE IS THE SPAWNING TENANT'S OR IT IS NOTHING. Every host
	// authorizes mounts tenant-scoped under its projects root, so a spawn
	// naming another tenant's workspace would be refused at the mount check
	// anyway — refusing it here names the actual fault.
	if tenant := StorageTenant(ctx); ws.TenantID != tenant {
		return &SessionWorkspaceTenantError{SpawnTenant: tenant, WorkspaceTenant: ws.TenantID}
	}
	return nil
}

// workspaceSpawnContext threads a pinned workspace's placement into the
// resolver's context: strict affinity to the agent that holds it, or a pin to
// this daemon's in-process backend. An unpinned workspace leaves ctx alone.
func workspaceSpawnContext(ctx context.Context, ws *SessionWorkspace) context.Context {
	if !ws.Pinned() {
		return ctx
	}
	if ws.Pin.OnDaemon() {
		return withLocalBackendPin(ctx)
	}
	return WithHostAffinityStrict(WithHostAffinity(ctx, ws.Pin.AgentHost))
}

// requireWorkspacePlacement checks that a pinned spawn resolved to the host
// holding its workspace. resolveErr is the resolver's own failure, which for a
// pinned spawn is the pinned host being unavailable and is reported as such.
func requireWorkspacePlacement(req *SpawnSessionRequest, backend AgentBackend, fromRemote bool, resolveErr error) error {
	ws := req.Workspace
	if !ws.Pinned() {
		return resolveErr
	}
	refusal := &WorkspaceFleetNodeUnavailableError{
		AgentHost: ws.Pin.AgentHost,
		Key:       ws.Key,
		Backend:   req.Backend,
	}
	if resolveErr != nil {
		return refusal
	}
	if fromRemote {
		refusal.ResolvedHost = sessionHostAffinity(backend, "")
	}
	switch {
	case ws.Pin.OnDaemon() && !fromRemote:
		return nil
	case !ws.Pin.OnDaemon() && fromRemote && refusal.ResolvedHost == ws.Pin.AgentHost:
		return nil
	}
	return refusal
}

// pinnedFleetNodeUnreachable reports a pinned spawn whose agent resolved — its
// heartbeat still fresh — and then did not answer the spawn, as the workspace
// refusal it is.
//
// RESOLUTION IS NOT REACHABILITY. An agent that died inside its heartbeat
// window resolves like a live one, and the RPC then fails ErrFleetNodeUnreachable.
// Left as that, the job takes the ordinary environmental requeue — and the
// next job inherits no pin and may clone afresh elsewhere, exactly what the pin
// forbids. For a pinned spawn it is the pinned host being gone, so it is
// reported as WorkspaceFleetNodeUnavailableError and the job parks. Every other
// failure, and every unpinned spawn, is returned unchanged.
func pinnedFleetNodeUnreachable(req *SpawnSessionRequest, resolvedHost string, err error) error {
	if !req.Workspace.Pinned() || !errors.Is(err, ErrFleetNodeUnreachable) {
		return err
	}
	return &WorkspaceFleetNodeUnavailableError{
		AgentHost:    req.Workspace.Pin.AgentHost,
		Key:          req.Workspace.Key,
		Backend:      req.Backend,
		ResolvedHost: resolvedHost,
		Cause:        err,
	}
}

// provisionDaemonWorkspace provisions a named workspace for a spawn that runs
// on this daemon's own backend.
func (s *agentServiceImpl) provisionDaemonWorkspace(ws *SessionWorkspace) (*ProvisionedWorkspace, error) {
	if s.workspaces == nil {
		return nil, &SessionWorkspaceUnsupportedError{Host: "this daemon"}
	}
	return s.workspaces.ProvisionSession(ws)
}

// requireAgentWorkspace refuses a remote session whose agent did not provision
// the workspace the spawn named.
//
// AN AGENT THAT PREDATES NAMED WORKSPACES IGNORES THE FIELD. It spawns a
// container with no workspace mounted at all, and the checkout would then be
// made inside the container's own layer — gone with the container, and never
// the directory the job's later sessions are pinned to. An empty WorkDir on the
// session is that agent, and it is refused rather than used.
func requireAgentWorkspace(session *Session, agentHost string) error {
	if session == nil || strings.TrimSpace(session.WorkDir) == "" {
		host := agentHost
		if host == "" {
			host = "the agent"
		}
		return &SessionWorkspaceUnsupportedError{Host: host}
	}
	return nil
}

// workspacePlacement is the placement a spawn reports for the workspace it
// named, nil when it named none.
func workspacePlacement(ws *SessionWorkspace, backend, agentHost, dir string) *WorkspacePlacement {
	if ws == nil {
		return nil
	}
	return &WorkspacePlacement{AgentHost: agentHost, Backend: backend, Key: ws.Key, Dir: dir}
}
