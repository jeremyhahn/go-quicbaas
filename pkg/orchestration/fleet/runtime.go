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

package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// Runtime is the daemon-side agent runtime. It wraps pkg/orchestration
// backends and enforces policy + sandbox resolution before each spawn.
// It never re-implements spawn paths — every Spawn call is delegated to
// orchestration.ACPBackend or orchestration.OllamaBackend.
type Runtime struct {
	mu               sync.RWMutex
	backends         map[string]orchestration.AgentBackend
	resolver         *SandboxResolver
	imageMap         map[string]string
	imageRegistry    string
	autoPull         bool
	containerRuntime orchestration.ContainerRuntime
	logger           interfaces.Logger

	// mountPolicy authorizes every host path a spawn would bind-mount into a
	// sandbox container on THIS host. Nil denies every mount, which is what an
	// unconfigured daemon must do — see Runtime.authorizeMounts.
	mountPolicy *orchestration.MountPolicy

	// workspaces provisions the named workspaces spawns ask for, under this
	// daemon's own projects root. Nil refuses every spawn that names one.
	workspaces orchestration.WorkspaceProvisioner

	// sessionWorkspaces tracks {sessionID -> *ProvisionedWorkspace} so Close
	// releases the spec directory — the forge token — a session was spawned
	// with. The workspace itself stays for the job's next session.
	sessionWorkspaces sync.Map

	// sessionContainers tracks {sessionID -> sessionContainer}: the container
	// each session this daemon spawned runs in, and the tenant it belongs to.
	// It is what ExecSession resolves a session to — the control plane names
	// a session, never a container.
	sessionContainers sync.Map

	// workDir is the parent for <workDir>/sessions. Session records are
	// persisted as plaintext JSON; at-rest confidentiality is provided by
	// qrdb's StorageBarrier. Tenants needing cryptographic isolation may
	// configure a dedicated per-tenant barrier.
	workDir string

	// sessionTenants tracks {sessionID -> tenantID} so Close can decrement the
	// ActiveSessions gauge against the same label series Spawn incremented.
	sessionTenantsMu sync.Mutex
	sessionTenants   map[string]string

	// eventForwarder ships structured session events to the control plane's
	// session event log over gRPC PushEvents. May be nil when the daemon
	// runs in embedded-only mode without a control plane to forward to.
	eventForwarder *EventForwarder

	// sessionBackends tracks {sessionID -> backend name} so Prompt can
	// tag outbound events with the backend that produced them without
	// requiring the caller to re-thread the backend name through every
	// PromptRequest.
	sessionBackendsMu sync.Mutex
	sessionBackends   map[string]string

	// socketsRoot is where relayed signing sockets are bound
	// (go-quicbaas#1013). Empty refuses every spawn that asks for a relay.
	socketsRoot string

	// signingRelays tracks {sessionID -> *signingRelay}: the relayed signing
	// agent each session was spawned with, released on Close.
	signingRelays sync.Map
}

// RuntimeConfig wires external dependencies into the runtime. Backends may
// be nil if a particular kind is disabled. ContainerRuntime is optional
// and only required for sandboxed sessions.
type RuntimeConfig struct {
	Backends         map[string]orchestration.AgentBackend
	ContainerRuntime orchestration.ContainerRuntime
	SandboxPolicy    orchestration.SandboxPolicy
	ImageMap         map[string]string
	ImageRegistry    string
	Logger           interfaces.Logger

	// WorkDir is the daemon's working directory; session records are stored
	// under <WorkDir>/sessions/. When empty, PersistSession and
	// LoadPersistedSessions are no-ops.
	WorkDir string

	// EventForwarder is the optional component that ships session events to
	// the control plane. Runtime.Prompt installs a SessionEventCallback
	// pointing at this forwarder before delegating to the backend so every
	// emitted event reaches attachers on the control plane side.
	EventForwarder *EventForwarder

	// MountPolicy authorizes the host bind mounts a spawn request asks for.
	// It is the SAME type the control plane enforces
	// (orchestration.MountPolicy), so both enforcement points resolve
	// symlinks, refuse a nonexistent source, and compare roots on segment
	// boundaries identically.
	//
	// A standalone daemon has no managed projects root of its own, so its
	// policy is the operator's allowlist (Config.AllowedMountRoots). Leaving
	// this nil is deny-by-default, not permissive: a sandboxed spawn naming
	// any work directory is refused.
	MountPolicy *orchestration.MountPolicy

	// Workspaces provisions the named workspaces the control plane's spawns
	// ask for (go-quicbaas#981): the spawn names tenant, project and key, and
	// this daemon makes the directory under its own projects root and reports
	// it back. The mount policy must admit that root — Config.MountPolicy
	// grants it tenant-scoped. Nil refuses every spawn that names a workspace
	// with SessionWorkspaceUnsupportedError.
	Workspaces orchestration.WorkspaceProvisioner

	// AccountResolver turns a spawn's AccountID / AccountAuto request into
	// concrete credential material for a pooled provider account. It is the
	// SAME orchestration.AccountResolver the control plane composes, over
	// API-backed stores (see NewAccountPool), so a remote spawn resolves an
	// account by the same rules a local one does.
	//
	// Leaving it nil is not a degraded mode: an ACP backend with no resolver
	// fails a pooled spawn closed with AccountResolverNotConfiguredError, which
	// is the defect go-quicbaas#734 recorded. It is nil only on a daemon with
	// no control plane to draw a pool from.
	AccountResolver orchestration.AccountResolver

	// SocketsRoot is where the runtime binds the relayed signing sockets a
	// spawn asks for (go-quicbaas#1013) — Config.EffectiveSocketsRoot on a
	// fleet daemon. The mount policy must admit it (MountPolicy.SocketsRoot).
	// Empty refuses every relay; the embedded agent leaves it so, because it
	// mounts the daemon's own socket directly.
	SocketsRoot string

	// GoBuildCache is the shared per-tenant Go cache every sandbox container
	// mounts (orchestration.GoBuildCache). The zero value leaves each
	// container its own caches. NewRuntime refuses one the mount policy's
	// grants could reach.
	GoBuildCache orchestration.GoBuildCache

	// AutoPull pulls a sandbox image a spawn NAMES (rather than takes from
	// ImageMap) when this host does not hold it, before the container starts.
	// It is the same --auto-pull the operator already sets for the image map,
	// extended to the images the control plane binds projects to: when the
	// worker image refresh promotes a registry reference, every node that
	// runs the project's next job fetches it once, by digest, instead of an
	// operator copying the image to each node by hand.
	AutoPull bool
}

// NewRuntime constructs a Runtime. Returns ErrRuntimeNoBackends if the
// backends map is empty.
func NewRuntime(cfg RuntimeConfig) (*Runtime, error) {
	if len(cfg.Backends) == 0 {
		return nil, ErrRuntimeNoBackends
	}
	if cfg.Logger == nil {
		return nil, ErrRuntimeNoLogger
	}
	hasRT := cfg.ContainerRuntime != nil && cfg.ContainerRuntime.IsAvailable()

	// A daemon under SandboxPolicyRequired that holds no usable container
	// runtime can satisfy no spawn at all: every request resolves to
	// SandboxModeRequired and ResolveSandboxMode refuses it for want of a
	// runtime. That is a misconfiguration of the host, and it belongs in the
	// boot log where the operator is watching, not in the first dispatched
	// task's error (go-quicbaas#732). Both the `fleet serve` daemon and the
	// embedded agent construct their runtime here, so the rule is enforced
	// once for both.
	if cfg.SandboxPolicy == orchestration.SandboxPolicyRequired && !hasRT {
		return nil, ErrRuntimeSandboxRequiredNoRuntime
	}

	backends := cloneBackends(cfg.Backends)

	// Give every ACP backend access to the container runtime so its
	// SandboxModeRequired paths — spawnContainerCLI for a CLI-mode backend,
	// spawnContainerJSONRPC for an ACP one — can launch an interactive agent
	// container. Without this the backend's containerRuntime stays nil and a
	// Required spawn fails with SandboxModeViolationError. This mirrors the
	// orchestrator-side wiring in pkg/server/lifecycle_orchestration.go.
	if hasRT {
		for _, b := range backends {
			if acp, ok := b.(*orchestration.ACPBackend); ok {
				acp.SetContainerRuntime(cfg.ContainerRuntime)
			}
		}
	}

	// The shared Go cache is the daemon's own mount, so no grant of the
	// request mount policy may reach it.
	if err := cfg.GoBuildCache.Validate(cfg.MountPolicy); err != nil {
		return nil, err
	}
	if hasRT && cfg.GoBuildCache.Enabled() {
		for _, b := range backends {
			if acp, ok := b.(*orchestration.ACPBackend); ok {
				acp.SetGoBuildCache(cfg.GoBuildCache)
			}
		}
	}

	// Stamp every sandbox container with the daemon that owns it, so a daemon
	// that restarts can recognise the containers its predecessor left running
	// and remove those and only those (SweepOrphanContainers, #750). The work
	// directory is the name: it is this daemon's own state, it survives a
	// restart, and two daemons never share one.
	owner := strings.TrimSpace(cfg.WorkDir)
	if hasRT && owner != "" {
		for _, b := range backends {
			if acp, ok := b.(*orchestration.ACPBackend); ok {
				acp.SetSandboxOwner(owner)
			}
		}
	}

	// Give every ACP backend the pooled-account resolver so a spawn naming
	// AccountID / AccountAuto resolves against the tenant's central pool
	// instead of failing with AccountResolverNotConfiguredError. This mirrors
	// the orchestrator-side wiring in pkg/server/lifecycle_orchestration.go —
	// which attaches by backend name — but attaches to EVERY ACP backend the
	// daemon serves: the daemon's own backend set is already the operator's
	// choice, and a name list here would silently exclude a backend the
	// control plane pools (the "claude-code" alias problem that wiring
	// documents).
	if cfg.AccountResolver != nil {
		for _, b := range backends {
			if acp, ok := b.(*orchestration.ACPBackend); ok {
				acp.SetAccountResolver(cfg.AccountResolver)
			}
		}
	}

	return &Runtime{
		backends:         backends,
		resolver:         NewSandboxResolver(cfg.SandboxPolicy, hasRT),
		imageMap:         cloneStringMap(cfg.ImageMap),
		imageRegistry:    cfg.ImageRegistry,
		autoPull:         cfg.AutoPull,
		containerRuntime: cfg.ContainerRuntime,
		logger:           cfg.Logger,
		mountPolicy:      cfg.MountPolicy,
		workspaces:       cfg.Workspaces,
		workDir:          cfg.WorkDir,
		sessionTenants:   map[string]string{},
		eventForwarder:   cfg.EventForwarder,
		sessionBackends:  map[string]string{},
		socketsRoot:      strings.TrimSpace(cfg.SocketsRoot),
	}, nil
}

// SetEventForwarder installs (or replaces) the event forwarder. Used by
// the daemon entrypoint to wire the forwarder after the runtime is built,
// since the forwarder's dependencies (token source, TLS config) are easier
// to construct alongside the enroller than inside NewRuntime.
func (r *Runtime) SetEventForwarder(f *EventForwarder) {
	r.mu.Lock()
	r.eventForwarder = f
	r.mu.Unlock()
}

// SetAccountResolver installs (or replaces) the pooled provider account
// resolver on every ACP backend this runtime serves.
//
// It exists for the same reason SetEventForwarder does: the resolver's
// dependencies are the daemon's control-plane HTTP client and token source,
// which are built alongside the enroller — after the runtime, because the
// enroller needs the runtime's served-backend list. Wiring it here rather than
// reordering that bring-up keeps one construction order for both late-bound
// dependencies.
//
// A nil resolver is ignored rather than clearing the backends' existing one:
// "no resolver configured" is a fail-closed state for every pooled spawn, and
// reaching it by accident is not a thing a setter should make easy.
func (r *Runtime) SetAccountResolver(resolver orchestration.AccountResolver) {
	if resolver == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range r.backends {
		if acp, ok := b.(*orchestration.ACPBackend); ok {
			acp.SetAccountResolver(resolver)
		}
	}
}

// rememberBackendForSession records which backend owns a session so that
// Prompt calls routed without an explicit backend (e.g. lookup by
// session_id) can still tag outbound events with the correct source.
func (r *Runtime) rememberBackendForSession(sessionID, backendName string) {
	r.sessionBackendsMu.Lock()
	r.sessionBackends[sessionID] = backendName
	r.sessionBackendsMu.Unlock()
}

// backendForSession returns the recorded backend for a session, or empty
// string when unknown. Used during Prompt to tag events.
func (r *Runtime) backendForSession(sessionID string) string {
	r.sessionBackendsMu.Lock()
	defer r.sessionBackendsMu.Unlock()
	return r.sessionBackends[sessionID]
}

// forgetSessionBackend removes the mapping when a session closes.
func (r *Runtime) forgetSessionBackend(sessionID string) {
	r.sessionBackendsMu.Lock()
	delete(r.sessionBackends, sessionID)
	r.sessionBackendsMu.Unlock()
}

// Spawn dispatches to the named backend after resolving the SandboxMode
// against the active policy and the tenant override.
func (r *Runtime) Spawn(ctx context.Context, tenantID string, tenantOverride *orchestration.SandboxPolicy, backendName string, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
	if req == nil {
		return nil, &orchestration.InputValidationError{Field: "request", Message: "cannot be nil"}
	}

	backend, ok := r.lookupBackend(backendName)
	if !ok {
		return nil, &SentinelDetailError{Sentinel: ErrRuntimeUnknownBackend, Detail: fmt.Sprintf("%q", backendName)}
	}

	resolver := r.resolver.WithTenantOverride(tenantOverride)
	effective, resolveErr := resolver.Resolve(req.EffectiveSandboxMode())
	if resolveErr != nil {
		return nil, resolveErr
	}

	req.SandboxMode = effective
	req.Sandboxed = effective == orchestration.SandboxModeRequired

	// A NAMED WORKSPACE IS MADE HERE, ON THIS HOST, before the mounts are
	// authorized — so the directory and spec this daemon wrote go through the
	// same check as every path the control plane named.
	workspace, wsErr := r.provisionWorkspace(tenantID, req)
	if wsErr != nil {
		return nil, wsErr
	}
	// THE RELAYED SIGNING SOCKET IS BOUND BEFORE THE MOUNTS ARE AUTHORIZED,
	// so the socket this host binds is checked by the same policy as every
	// other mount source (go-quicbaas#1013).
	relay, relayErr := r.startSigningRelay(tenantID, req)
	if relayErr != nil {
		workspace.Release()
		return nil, relayErr
	}
	spawned := false
	defer func() {
		if !spawned {
			workspace.Release()
			if relay != nil {
				if report := relay.release(); !report.Clean() {
					r.logger.Warnf("agent runtime: failed spawn's signing relay stopped uncleanly: abandoned=%d joined=%t",
						report.Abandoned, report.Joined)
				}
			}
		}
	}()

	// Authorize every host path this spawn would bind-mount, against the mode
	// THIS daemon resolved, before any side effect. See authorizeMounts.
	if mountErr := r.authorizeMounts(tenantID, req, effective); mountErr != nil {
		return nil, mountErr
	}

	// Resolve container image from the daemon's image map when the caller
	// didn't set one explicitly. This keeps the wire protocol simple:
	// control plane supplies the backend name; the daemon decides which image
	// to pull.
	if effective == orchestration.SandboxModeRequired && req.ContainerImage == "" {
		if img, ok := r.imageFor(backendName); ok {
			req.ContainerImage = img
		} else {
			return nil, &SentinelDetailError{Sentinel: ErrRuntimeNoImage, Detail: fmt.Sprintf("backend %q has no image mapping", backendName)}
		}
	} else if effective == orchestration.SandboxModeRequired && r.autoPull {
		if pullErr := r.ensureImage(ctx, req.ContainerImage); pullErr != nil {
			return nil, pullErr
		}
	}

	r.logger.Debugf("agent runtime spawn: tenant=%s backend=%s mode=%s image=%s",
		tenantID, backendName, effective.String(), req.ContainerImage)

	// Bind the tenant onto the context the backend spawns under. The pooled
	// account resolver reads tenancy from there and from nowhere else
	// (ACPBackend.resolveAccount -> TenantFromContext), so without this a
	// remote spawn would ask the tenant's pool for tenant "" and be refused —
	// the daemon knows the tenant, but only as an argument.
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	sess, spawnErr := backend.Spawn(ctx, req)
	if spawnErr != nil {
		return nil, spawnErr
	}
	spawned = true
	if sess.ContainerID != "" {
		r.sessionContainers.Store(sess.ID, sessionContainer{tenantID: tenantID, containerID: sess.ContainerID})
	}
	if relay != nil {
		relay.setSession(sess.ID)
		r.signingRelays.Store(sess.ID, relay)
	}
	if workspace != nil {
		// The directory this host provisioned is the session's work directory
		// whatever the backend echoed, because it is the answer the control
		// plane records and pins the job's later sessions to.
		sess.WorkDir = workspace.Dir
		r.sessionWorkspaces.Store(sess.ID, workspace)
	}

	// Remember the backend for this session so Prompt can tag outbound
	// events with the correct producer when callers don't re-supply it.
	r.rememberBackendForSession(sess.ID, backendName)

	// Persist session state + increment metrics on success. Persistence
	// errors are logged but not propagated: the backend has already claimed
	// the session and returning an error here would orphan it.
	if persistErr := r.persistSession(tenantID, backendName, sess); persistErr != nil {
		r.logger.Warnf("agent runtime: persist session %s: %v", sess.ID, persistErr)
	}
	SpawnsTotal.WithLabelValues(tenantID, backendName, sandboxLabel(req.Sandboxed)).Inc()
	ActiveSessions.WithLabelValues(tenantID, backendName).Inc()
	r.sessionTenantsMu.Lock()
	r.sessionTenants[sess.ID] = tenantID
	r.sessionTenantsMu.Unlock()

	return sess, nil
}

// sessionContainer is the container a spawned session runs in.
type sessionContainer struct {
	tenantID    string
	containerID string
}

// DefaultExecTimeout bounds a command run through ExecSession when the caller
// set no deadline, so a hung git cannot hold the RPC forever.
const DefaultExecTimeout = 30 * time.Minute

// MaxExecOutputBytes bounds each of a command's stdout and stderr as
// returned over the wire, below the gRPC message limit. A command that says
// more is truncated, and the result says so.
const MaxExecOutputBytes = 1 << 20

// ExecSession runs one command in the container of a session THIS daemon
// spawned, for tenantID (go-quicbaas#981). The control plane routes here
// every command a job runs in a session on this host — the checkout, the
// verification, the publish — because the container is known to this host's
// runtime and no other.
//
// It is scoped exactly as the control plane's local path is: the session
// must be one this daemon holds, of the caller's tenant (another tenant's is
// answered as not found, never as someone else's), running in a container;
// the command is validated the same way. Output is bounded and the command
// carries a deadline.
func (r *Runtime) ExecSession(ctx context.Context, tenantID, sessionID string, spec *orchestration.ExecSpec) (*orchestration.ExecResult, bool, error) {
	if err := spec.Validate(); err != nil {
		return nil, false, err
	}
	held, ok := r.sessionContainers.Load(sessionID)
	if !ok {
		return nil, false, &orchestration.SessionNotFoundError{SessionID: sessionID}
	}
	container, _ := held.(sessionContainer)
	if container.tenantID != tenantID {
		return nil, false, &orchestration.SessionNotFoundError{SessionID: sessionID}
	}
	if container.containerID == "" || r.containerRuntime == nil {
		return nil, false, &orchestration.SessionNotContainerizedError{SessionID: sessionID}
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultExecTimeout)
		defer cancel()
	}
	result, err := r.containerRuntime.Exec(ctx, container.containerID, spec)
	if err != nil {
		return nil, false, err
	}
	stdout, cutOut := boundOutput(result.Stdout)
	stderr, cutErr := boundOutput(result.Stderr)
	return &orchestration.ExecResult{ExitCode: result.ExitCode, Stdout: stdout, Stderr: stderr}, cutOut || cutErr, nil
}

// boundOutput truncates s to at most MaxExecOutputBytes, keeping the END —
// where a failing command says why — and reports whether it cut anything.
//
// THE CUT LANDS ON A RUNE BOUNDARY: when it would start inside a multibyte
// UTF-8 character, the character's remaining continuation bytes are dropped
// too, so valid output stays valid. Nothing else is rewritten — output that
// was never UTF-8 is carried byte for byte, exactly as the local exec path
// hands it back (orchestration.ExecResult holds the bytes the command wrote),
// and it travels as protobuf bytes, so no encoding is imposed on the wire.
func boundOutput(s string) (string, bool) {
	if len(s) <= MaxExecOutputBytes {
		return s, false
	}
	start := len(s) - MaxExecOutputBytes
	for i := 0; i < utf8.UTFMax-1 && start < len(s) && !utf8.RuneStart(s[start]); i++ {
		start++
	}
	return s[start:], true
}

// provisionWorkspace makes the workspace a spawn names, under this daemon's
// projects root, and puts it on the request. A spawn that names none is
// returned untouched with a nil workspace.
func (r *Runtime) provisionWorkspace(tenantID string, req *orchestration.SpawnRequest) (*orchestration.ProvisionedWorkspace, error) {
	ws := req.Workspace
	if ws == nil {
		return nil, nil
	}
	if strings.TrimSpace(req.WorkDir) != "" {
		return nil, &orchestration.SessionWorkspaceInvalidError{
			Field:  "work_dir",
			Reason: "a spawn names its workspace or gives a work directory, never both",
		}
	}
	// THE AUTHENTICATED TENANT DECIDES, not the request's own claim: a
	// principal enrolled for one tenant may not have this daemon provision —
	// or find a pinned checkout of — another tenant's workspace.
	if ws.TenantID != tenantID {
		return nil, &orchestration.SessionWorkspaceTenantError{SpawnTenant: tenantID, WorkspaceTenant: ws.TenantID}
	}
	if r.workspaces == nil {
		return nil, &orchestration.SessionWorkspaceUnsupportedError{Host: "agent " + r.workDir}
	}
	provisioned, err := r.workspaces.ProvisionSession(ws)
	if err != nil {
		return nil, err
	}
	provisioned.ApplyTo(req)
	return provisioned, nil
}

// authorizeMounts authorizes every host path this spawn would bind-mount into
// a sandbox container on this host, scoped to the requesting tenant. It runs
// after the daemon has resolved the effective sandbox mode and before any side
// effect, so a rejected spawn has started no container and called no backend.
//
// This is the last line, not a duplicate of the control plane's check. Two
// things make it necessary:
//
//   - The daemon's Spawn RPC takes work_dir straight off the wire. A principal
//     holding only AgentScopeRuntimeSpawn would otherwise get any host path
//     bind-mounted read-write into the container, which runs as the daemon's
//     own uid — work_dir "/" is host root.
//   - A remote spawn whose CONTROL-PLANE-resolved mode is not Required skips
//     the control plane's work_dir authorization entirely, yet this daemon's
//     own SandboxPolicy may still containerize it. Only a check against the
//     mode resolved HERE covers that case.
//
// The split between the two fields mirrors the control plane's
// authorizeSandboxMounts exactly: the work directory becomes a host bind mount
// only when the spawn is containerized, while extra mounts exist for no other
// purpose and are authorized on every path.
func (r *Runtime) authorizeMounts(
	tenantID string,
	req *orchestration.SpawnRequest,
	mode orchestration.SandboxMode,
) error {
	if len(req.ExtraMounts) > 0 {
		if err := r.mountPolicy.Authorize(
			tenantID, orchestration.MountFieldExtraMounts, req.ExtraMounts); err != nil {
			return err
		}
	}
	if mode != orchestration.SandboxModeRequired {
		return nil
	}
	if req.WorkDir != "" && req.WorkDir != orchestration.WorkDirCurrent {
		if err := r.mountPolicy.AuthorizeSource(
			tenantID, orchestration.MountFieldWorkDir, req.WorkDir); err != nil {
			return err
		}
	}
	return nil
}

// Prompt sends a prompt to the given backend. The session must already
// have been established by a prior Spawn (or by the backend accepting
// stateless prompts — Ollama).
//
// When an EventForwarder is configured, Prompt installs a
// SessionEventCallback that stamps each backend-emitted structured event with
// the session's tenant and source and enqueues it for forwarding to the
// control plane's session log. The callback wraps any caller-supplied
// EventCallback (rather than replacing it) so interactive SDK callers keep
// seeing events locally.
func (r *Runtime) Prompt(ctx context.Context, backendName string, req *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
	backend, ok := r.lookupBackend(backendName)
	if !ok {
		return nil, &SentinelDetailError{Sentinel: ErrRuntimeUnknownBackend, Detail: fmt.Sprintf("%q", backendName)}
	}

	// Record a PromptSubmitted event up front so attachers see the prompt
	// even when the backend call fails. Tenant is the session's recorded
	// tenant from Spawn; we fall back to "" when the session is unknown.
	if r.eventForwarder != nil && req != nil && req.Prompt != "" {
		r.sessionTenantsMu.Lock()
		tenantID := r.sessionTenants[req.SessionID]
		r.sessionTenantsMu.Unlock()
		r.eventForwarder.ObserveEvent(&orchestration.SessionEvent{
			SessionID: req.SessionID,
			TenantID:  tenantID,
			Source:    "operator:" + tenantID,
			Kind:      orchestration.KindPromptSubmitted,
			Prompt: &orchestration.PromptSubmittedPayload{
				Prompt:       req.Prompt,
				Model:        req.Model,
				SystemPrompt: req.SystemPrompt,
			},
		})
	}

	// Install a callback that forwards every backend-emitted event to the
	// control plane. Wrap any caller-supplied callback so both paths see events.
	if r.eventForwarder != nil {
		r.sessionTenantsMu.Lock()
		tenantID := r.sessionTenants[req.SessionID]
		r.sessionTenantsMu.Unlock()
		forwardCB := r.eventForwarder.CallbackFor(tenantID, backendName)
		existing := req.EventCallback
		req.EventCallback = func(ev *orchestration.SessionEvent) {
			forwardCB(ev)
			if existing != nil {
				existing(ev)
			}
		}
	}

	return backend.Prompt(ctx, req)
}

// Infer runs a stateless prompt against the named backend without creating a
// session. Mirrors Prompt but skips all session-lifecycle plumbing. The
// backend must accept sessionless calls (e.g., OllamaBackend). Session-based
// backends (ACP-style) are rejected with InferUnsupportedError so callers
// know to use the Spawn+Prompt path instead.
//
// When an EventForwarder is configured, Infer emits KindInferenceSubmitted
// before the backend call and KindInferenceCompleted (on the final chunk)
// into the event log, enabling operators to observe sessionless calls.
//
// The returned channel streams InferChunk values and is closed when the
// backend finishes. Callers that want an aggregated result should use
// orchestration.AccumulateChunks.
func (r *Runtime) Infer(ctx context.Context, backendName string, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
	backend, ok := r.lookupBackend(backendName)
	if !ok {
		return nil, &SentinelDetailError{Sentinel: ErrRuntimeUnknownBackend, Detail: fmt.Sprintf("%q", backendName)}
	}

	// Session-based backends require Spawn first; surface a typed error so
	// the gRPC handler can map it to codes.Unimplemented.
	if backend.Capabilities().PersistentSessions {
		return nil, &orchestration.InferUnsupportedError{Backend: backendName, Operation: "infer"}
	}

	// Synthesize a per-call session_id so the control plane PushEvents handler
	// (which validates session_id and tenant_id are non-empty) accepts the
	// pair of lifecycle events. Both Submitted and Completed share infID so
	// they group under one queryable key in the session event log.
	infID := "inference-" + uuid.NewString()

	// Empty TenantID would still be rejected by the control plane handler, so
	// suppress emission entirely and log at debug level instead of producing
	// WARN noise on every call. Production callers should always populate
	// TenantID; this branch defends against misconfigured operator paths.
	emitEvents := r.eventForwarder != nil && req != nil && req.Prompt != "" && req.TenantID != ""
	if r.eventForwarder != nil && req != nil && req.TenantID == "" {
		r.logger.Debugf("skipping inference event: tenant unknown (backend=%s)", backendName)
	}

	if emitEvents {
		r.eventForwarder.ObserveEvent(&orchestration.SessionEvent{
			SessionID: infID,
			TenantID:  req.TenantID,
			Source:    "operator:" + req.TenantID,
			Kind:      orchestration.KindInferenceSubmitted,
		})
	}

	// Delegate to the backend's Prompt method (Ollama is sessionless so the
	// empty SessionID is fine). Wrap the single response as a one-element
	// channel so the caller always sees a uniform streaming interface.
	promptReq := &orchestration.PromptRequest{
		Prompt:       req.Prompt,
		Model:        req.Model,
		SystemPrompt: req.SystemPrompt,
	}
	resp, err := backend.Prompt(ctx, promptReq)
	if err != nil {
		return nil, &orchestration.InferBackendError{Op: "Prompt", Err: err}
	}

	if emitEvents {
		r.eventForwarder.ObserveEvent(&orchestration.SessionEvent{
			SessionID: infID,
			TenantID:  req.TenantID,
			Source:    "agent:" + backendName,
			Kind:      orchestration.KindInferenceCompleted,
		})
	}

	r.logger.Debugf("agent runtime infer: tenant=%s backend=%s model=%s stop=%s",
		req.TenantID, backendName, resp.Model, resp.StopReason)

	ch := make(chan *orchestration.InferChunk, 1)
	ch <- &orchestration.InferChunk{
		Delta:      resp.Content,
		Model:      resp.Model,
		StopReason: resp.StopReason,
		Final:      true,
	}
	close(ch)
	return ch, nil
}

// Status returns session health for a persistent backend.
func (r *Runtime) Status(ctx context.Context, backendName, sessionID string) (*orchestration.SessionStatus, error) {
	backend, ok := r.lookupBackend(backendName)
	if !ok {
		return nil, &SentinelDetailError{Sentinel: ErrRuntimeUnknownBackend, Detail: fmt.Sprintf("%q", backendName)}
	}
	return backend.Status(ctx, sessionID)
}

// Close terminates a session owned by the given backend.
func (r *Runtime) Close(ctx context.Context, backendName, sessionID string) error {
	backend, ok := r.lookupBackend(backendName)
	if !ok {
		return &SentinelDetailError{Sentinel: ErrRuntimeUnknownBackend, Detail: fmt.Sprintf("%q", backendName)}
	}
	// THE DAEMON STOPS TRACKING THE SESSION EITHER WAY. Both the in-memory
	// backend mapping and the durable record say "this daemon holds this
	// session", so a close that dropped one and kept the other would leave a
	// record naming a session nothing can answer for — which is exactly the
	// residue go-quicbaas#750 reports piling up under <workDir>/sessions.
	// Whether the backend's own teardown succeeded is still the caller's
	// answer, reported below.
	r.forgetSessionBackend(sessionID)
	r.removePersistedSession(sessionID)
	err := backend.Close(ctx, sessionID)
	// The spec directory goes whether or not the backend's teardown did: it
	// holds the forge token, and the session it was written for is one the
	// control plane has stopped addressing.
	r.sessionContainers.Delete(sessionID)
	// The relayed signing socket goes too: the session's signing window is
	// over whether or not the backend's teardown succeeded.
	r.releaseSigningRelay(sessionID)
	if held, ok := r.sessionWorkspaces.LoadAndDelete(sessionID); ok {
		if ws, isWS := held.(*orchestration.ProvisionedWorkspace); isWS {
			ws.Release()
		}
	}
	if err != nil {
		return err
	}

	// Use the recorded tenant so the gauge decrement hits the same label
	// series Spawn incremented. Missing entries are quietly ignored — an
	// operator restart may have lost the in-memory map.
	r.sessionTenantsMu.Lock()
	tenantID, ok := r.sessionTenants[sessionID]
	if ok {
		delete(r.sessionTenants, sessionID)
	}
	r.sessionTenantsMu.Unlock()
	if ok {
		ActiveSessions.WithLabelValues(tenantID, backendName).Dec()
	}
	return nil
}

// SandboxResolver returns the runtime's resolver for inspection. Daemons
// surface this via the status RPC.
// Images lists the container images on this host, or nil when the runtime
// cannot list them or the listing failed. Nil means "not reported", which the
// control plane reads as unknown rather than as a host with no images.
func (r *Runtime) Images(ctx context.Context) []string {
	lister, ok := r.containerRuntime.(orchestration.ImageLister)
	if !ok {
		return nil
	}
	images, err := lister.ListImages(ctx)
	if err != nil {
		if r.logger != nil {
			r.logger.Warnf("agent: listing container images for the heartbeat: %v", err)
		}
		return nil
	}
	return images
}

func (r *Runtime) SandboxResolver() *SandboxResolver {
	return r.resolver
}

// ImageFor returns the fully qualified image reference for a backend name.
// Applies the registry prefix when the map entry is unqualified.
func (r *Runtime) ImageFor(backendName string) (string, bool) {
	return r.imageFor(backendName)
}

// Backends returns the names of the backends this runtime actually serves,
// sorted.
//
// THIS IS THE DAEMON'S SERVED TRUTH, and it is deliberately not the same
// vocabulary as the --backends flag. The flag names TRANSPORT KINDS ("acp",
// "ollama"); a single "acp" kind constructs the claude, codex and opencode
// backends, and those three names are what a task routes on. Everything that
// tells the control plane what this daemon can run reads it here — the ready
// log, the heartbeat status, and the enrollment register payload — so the
// fleet registry records the names dispatch will ask for (go-quicbaas#681).
//
// Sorted because the underlying set is a map: a payload an operator diffs and
// a log line an operator greps must not reorder between restarts.
func (r *Runtime) Backends() []string {
	r.mu.RLock()
	names := make([]string, 0, len(r.backends))
	for k := range r.backends {
		names = append(names, k)
	}
	r.mu.RUnlock()
	sort.Strings(names)
	return names
}

// ListModels returns the model names available on the named backend. Backends
// that implement orchestration.ModelLister are queried directly; others return
// an empty slice. Returns ErrRuntimeUnknownBackend when the backend is unknown.
func (r *Runtime) ListModels(ctx context.Context, backendName string) ([]string, error) {
	r.mu.RLock()
	backend, ok := r.backends[backendName]
	r.mu.RUnlock()
	if !ok {
		return nil, &SentinelDetailError{Sentinel: ErrRuntimeUnknownBackend, Detail: fmt.Sprintf("%q", backendName)}
	}
	lister, ok := backend.(orchestration.ModelLister)
	if !ok {
		return []string{}, nil
	}
	return lister.ListModels(ctx)
}

// PrepareImages optionally pre-pulls every mapped container image so the
// first Spawn doesn't pay registry-fetch latency. When autoPull is false,
// this is a no-op.
//
// Individual Pull failures are logged and counted but do NOT cause
// PrepareImages to return an error: an air-gapped host may already have
// every image baked in, and the daemon should remain serviceable.
func (r *Runtime) PrepareImages(ctx context.Context, autoPull bool) error {
	if !autoPull {
		return nil
	}
	if r.containerRuntime == nil {
		r.logger.Infof("agent runtime: auto-pull requested but no container runtime is configured; skipping")
		return nil
	}
	r.mu.RLock()
	images := make([]string, 0, len(r.imageMap))
	for backendName := range r.imageMap {
		img, ok := r.imageMap[backendName]
		if !ok {
			continue
		}
		images = append(images, qualifyImage(img, r.imageRegistry))
	}
	r.mu.RUnlock()
	if len(images) == 0 {
		return nil
	}
	r.logger.Infof("agent runtime: pre-pulling %d container images", len(images))
	for _, img := range images {
		if err := r.containerRuntime.Pull(ctx, img); err != nil {
			r.logger.Warnf("agent runtime: image pull %q failed: %v", img, err)
			ImagePullsTotal.WithLabelValues("error").Inc()
			continue
		}
		r.logger.Infof("agent runtime: image pulled: %s", img)
		ImagePullsTotal.WithLabelValues("ok").Inc()
	}
	return nil
}

// imageInspector is the optional capability of a container runtime to say
// whether this host already holds an image; orchestration.ExecContainerRuntime
// has it.
type imageInspector interface {
	InspectImage(ctx context.Context, image string) (*orchestration.ImageInfo, error)
}

// ensureImage pulls image when this host does not hold it. A runtime that
// cannot say whether it holds an image is asked to pull, which a registry
// answers for a present image without transferring its layers.
func (r *Runtime) ensureImage(ctx context.Context, image string) error {
	if r.containerRuntime == nil {
		return nil
	}
	if inspector, ok := r.containerRuntime.(imageInspector); ok {
		_, err := inspector.InspectImage(ctx, image)
		if err == nil {
			return nil
		}
		if !errors.Is(err, orchestration.ErrImageNotFound) {
			return err
		}
	}
	r.logger.Infof("agent runtime: pulling sandbox image %s before spawning", image)
	if err := r.containerRuntime.Pull(ctx, image); err != nil {
		ImagePullsTotal.WithLabelValues("error").Inc()
		return err
	}
	ImagePullsTotal.WithLabelValues("ok").Inc()
	return nil
}

// SweepOrphanContainers removes the sandbox containers a previous run of THIS
// daemon left behind, and reports what it removed.
//
// It belongs at startup and nowhere else: a running daemon's own containers
// carry the same owner label as its predecessor's, so the only moment the two
// sets cannot be confused is before this daemon has spawned anything. See
// orchestration/container_sweep.go for what makes the claim exact.
//
// A daemon with no container runtime, or one whose runtime cannot enumerate
// its own containers, has nothing to sweep and says so with an empty report
// rather than an error — it never created a container to leak.
func (r *Runtime) SweepOrphanContainers(ctx context.Context) (orchestration.ContainerSweep, error) {
	if r.containerRuntime == nil || r.workDir == "" {
		return orchestration.ContainerSweep{}, nil
	}
	sweeper, ok := r.containerRuntime.(orchestration.ContainerOwnerSweeper)
	if !ok {
		return orchestration.ContainerSweep{}, nil
	}
	return sweeper.SweepOwned(ctx, r.workDir)
}

// ContainerReaper returns the running reaper for this daemon's sandbox
// containers (orchestration.ContainerReaper): every container carrying this
// daemon's owner label that none of its backends holds a live session for,
// once past the grace period. It returns nil, nil for a daemon that launches
// no containers, or whose runtime cannot enumerate them.
func (r *Runtime) ContainerReaper(grace time.Duration) (*orchestration.ContainerReaper, error) {
	if r.containerRuntime == nil || r.workDir == "" {
		return nil, nil
	}
	lister, ok := r.containerRuntime.(orchestration.OwnedContainerLister)
	if !ok {
		return nil, nil
	}
	return orchestration.NewContainerReaper(orchestration.ContainerReaperConfig{
		Runtime: lister,
		Owner:   r.workDir,
		Live:    r.HoldsSession,
		Grace:   grace,
		Logger:  r.logger,
	})
}

// HoldsSession reports whether any backend this daemon serves holds a live
// session with this ID.
func (r *Runtime) HoldsSession(sessionID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, b := range r.backends {
		if holder, ok := b.(sessionHolder); ok && holder.HoldsSession(sessionID) {
			return true
		}
	}
	return false
}

// sessionHolder is a backend that can say whether it holds a live session.
type sessionHolder interface {
	HoldsSession(sessionID string) bool
}

// SweepOrphanWorkspaceSpecs removes every workspace spec directory — each one
// holding a forge token in plaintext — a previous run of this daemon left
// under its projects root, and reports what it removed. The checkouts stay.
//
// It belongs at startup with SweepOrphanContainers and for the same reason:
// no session survives a restart (that sweep removed its container), so every
// spec directory present before the spawn server accepts anything is an
// orphan, and one this daemon's Close will never be asked to release. A daemon
// that provisions no workspaces has nothing to sweep.
func (r *Runtime) SweepOrphanWorkspaceSpecs() ([]string, error) {
	if r.workspaces == nil {
		return nil, nil
	}
	return r.workspaces.SweepSessionSpecs()
}

// RemoveWorkspace removes a session workspace this daemon provisioned, once
// the control plane's task is done with it (go-quicbaas#777), and reports
// whether there was one to remove.
//
// THE AUTHENTICATED TENANT DECIDES, as it does for provisioning: a principal
// enrolled for one tenant may not have this daemon remove another tenant's
// checkout. The manager confines the removal to this daemon's projects root,
// checks it against the mount policy, never follows a link, and refuses a
// workspace a session on this daemon still holds.
func (r *Runtime) RemoveWorkspace(tenantID string, ws *orchestration.SessionWorkspace) (bool, error) {
	if ws == nil {
		return false, &orchestration.SessionWorkspaceInvalidError{Field: "workspace", Reason: "is required"}
	}
	if ws.TenantID != tenantID {
		return false, &orchestration.SessionWorkspaceTenantError{SpawnTenant: tenantID, WorkspaceTenant: ws.TenantID}
	}
	if r.workspaces == nil {
		return false, &orchestration.SessionWorkspaceUnsupportedError{Host: "agent " + r.workDir}
	}
	return r.workspaces.RemoveSessionWorkspace(ws.TenantID, ws.ProjectID, ws.Key)
}

// LoadPersistedSessions reads every session record under <workDir>/sessions
// and returns the decoded entries. Records that cannot be decoded are moved to
// "<name>.corrupt" and surfaced via PersistedSessionCorruptError so an
// operator can investigate; the scan continues with the remaining records.
func (r *Runtime) LoadPersistedSessions(ctx context.Context) ([]PersistedSession, error) {
	if r.workDir == "" {
		return nil, nil
	}
	dir := filepath.Join(r.workDir, "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, &PersistSessionIOError{Op: "read-dir", Path: dir, Err: err}
	}
	var out []PersistedSession
	var corruptCount int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		path := filepath.Join(dir, name)
		sess, loadErr := r.loadSessionFile(path)
		if loadErr != nil {
			corruptCount++
			r.logger.Warnf("agent runtime: load session %q: %v; moving to .corrupt", path, loadErr)
			if mvErr := os.Rename(path, path+".corrupt"); mvErr != nil {
				r.logger.Errorf("agent runtime: rename corrupt session %q: %v", path, mvErr)
			}
			continue
		}
		out = append(out, sess)
	}
	if corruptCount > 0 {
		return out, &PersistedSessionCorruptError{Count: corruptCount}
	}
	return out, nil
}

// PersistedSession is a minimal, on-disk-safe record of a live session.
// It stores only the fields needed to reattach to an existing backend
// process after daemon restart; runtime state lives elsewhere.
type PersistedSession struct {
	SessionID     string    `json:"session_id"`
	TenantID      string    `json:"tenant_id"`
	Backend       string    `json:"backend"`
	WorkspacePath string    `json:"workspace_path"`
	CreatedAt     time.Time `json:"created_at"`
}

func (r *Runtime) lookupBackend(name string) (orchestration.AgentBackend, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.backends[name]
	return b, ok
}

func (r *Runtime) imageFor(backendName string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	img, ok := r.imageMap[backendName]
	if !ok {
		return "", false
	}
	return qualifyImage(img, r.imageRegistry), true
}

// persistSession serializes a PersistedSession and writes it to disk under
// <workDir>/sessions/<session_id>.json. At-rest confidentiality is provided
// by qrdb's StorageBarrier; no application-level encryption is applied here.
func (r *Runtime) persistSession(tenantID, backendName string, sess *orchestration.Session) error {
	if r.workDir == "" || sess == nil || sess.ID == "" {
		return nil
	}
	record := PersistedSession{
		SessionID:     sess.ID,
		TenantID:      tenantID,
		Backend:       backendName,
		WorkspacePath: sess.WorkDir,
		CreatedAt:     time.Unix(0, sess.CreatedAt),
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return &PersistSessionEncodeError{SessionID: sess.ID, Err: err}
	}
	dir := filepath.Join(r.workDir, "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return &PersistSessionIOError{Op: "mkdir", Path: dir, Err: err}
	}
	path := filepath.Join(dir, sess.ID+".json")
	return writeAtomic(path, payload)
}

// removePersistedSession deletes the persisted record for a session.
// Missing files are ignored.
func (r *Runtime) removePersistedSession(sessionID string) {
	if r.workDir == "" || sessionID == "" {
		return
	}
	path := filepath.Join(r.workDir, "sessions", sessionID+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		r.logger.Warnf("agent runtime: remove persisted session %q: %v", path, err)
	}
}

// loadSessionFile decodes a plaintext JSON session record.
func (r *Runtime) loadSessionFile(path string) (PersistedSession, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return PersistedSession{}, &PersistSessionIOError{Op: "read", Path: path, Err: err}
	}
	if len(raw) == 0 {
		return PersistedSession{}, &PersistSessionDecodeError{Path: path, Err: errors.New("empty file")}
	}
	var rec PersistedSession
	if uErr := json.Unmarshal(raw, &rec); uErr != nil {
		return PersistedSession{}, &PersistSessionDecodeError{Path: path, Err: uErr}
	}
	return rec, nil
}

// writeAtomic writes data to path via a rename from a sibling temp file so
// readers never observe a half-written record.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return &PersistSessionIOError{Op: "temp-create", Path: path, Err: err}
	}
	tmpPath := tmp.Name()
	// Best-effort cleanup on any pre-rename error path.
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return &PersistSessionIOError{Op: "temp-write", Path: path, Err: err}
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return &PersistSessionIOError{Op: "chmod", Path: path, Err: err}
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return &PersistSessionIOError{Op: "temp-close", Path: path, Err: err}
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return &PersistSessionIOError{Op: "rename", Path: path, Err: err}
	}
	return nil
}

// qualifyImage prefixes an image reference with registry when the image
// is not already fully qualified. "ghcr.io/x/y:tag" stays untouched;
// "claude-code:v1" becomes "<registry>/claude-code:v1".
func qualifyImage(image, registry string) string {
	if registry == "" {
		return image
	}
	if strings.Contains(image, "/") {
		// Already has a slash — assume it's registry-qualified or
		// repo-qualified. Don't double-prefix.
		return image
	}
	return strings.TrimRight(registry, "/") + "/" + image
}

func cloneBackends(in map[string]orchestration.AgentBackend) map[string]orchestration.AgentBackend {
	out := make(map[string]orchestration.AgentBackend, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// Runtime error sentinels.
var (
	ErrRuntimeNoBackends     = errors.New("agent: runtime requires at least one backend")
	ErrRuntimeNoLogger       = errors.New("agent: runtime requires a logger")
	ErrRuntimeUnknownBackend = errors.New("agent: unknown backend")
	ErrRuntimeNoImage        = errors.New("agent: no container image mapped for backend")

	// ErrRuntimeSandboxRequiredNoRuntime reports a daemon configured to
	// require a sandbox on a host where no container runtime is usable.
	// Starting would produce a daemon that enrolls, heartbeats and refuses
	// every spawn it is dispatched, so it is refused at construction instead.
	ErrRuntimeSandboxRequiredNoRuntime = errors.New("agent: sandbox policy \"required\" but no container runtime is available on this host")
)

// PersistSessionIOError wraps a filesystem failure during persistence.
type PersistSessionIOError struct {
	Op   string
	Path string
	Err  error
}

// Error implements the error interface.
func (e *PersistSessionIOError) Error() string {
	return "agent: persist session " + e.Op + " " + e.Path + ": " + e.Err.Error()
}

// Unwrap supports errors.Is/As traversal.
func (e *PersistSessionIOError) Unwrap() error { return e.Err }

// PersistSessionEncodeError wraps a JSON marshal failure.
type PersistSessionEncodeError struct {
	SessionID string
	Err       error
}

// Error implements the error interface.
func (e *PersistSessionEncodeError) Error() string {
	return "agent: persist session encode " + e.SessionID + ": " + e.Err.Error()
}

// Unwrap supports errors.Is/As traversal.
func (e *PersistSessionEncodeError) Unwrap() error { return e.Err }

// PersistSessionDecodeError wraps a JSON unmarshal or plaintext-parse failure.
type PersistSessionDecodeError struct {
	Path string
	Err  error
}

// Error implements the error interface.
func (e *PersistSessionDecodeError) Error() string {
	return "agent: persist session decode " + e.Path + ": " + e.Err.Error()
}

// Unwrap supports errors.Is/As traversal.
func (e *PersistSessionDecodeError) Unwrap() error { return e.Err }

// PersistedSessionCorruptError signals that one or more records on disk
// could not be decoded and have been renamed to ".corrupt". Returned
// alongside the successfully-decoded records so startup can continue.
type PersistedSessionCorruptError struct {
	Count int
}

// Error implements the error interface.
func (e *PersistedSessionCorruptError) Error() string {
	return fmt.Sprintf("agent: %d persisted session(s) unreadable; moved to .corrupt", e.Count)
}
