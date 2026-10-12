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

package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/grpc"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// installRemoteBackendResolver wires the BackendDispatcher's optional
// resolver that routes daemon-kind registrations to a RemoteACPBackend. When
// the resolver returns (backend, true), the orchestrator dispatches through
// it; otherwise the dispatcher falls back to local backends registered at
// NewAgentService. The resolver is the runtime glue that turns the registry
// + daemon-side gRPC server into a working remote execution path.
//
// Prerequisites:
//   - s.fleetRegistry is non-nil (set during IAM init; see iam_init.go)
//   - s.orchestratorDaemonClient is non-nil (bootstrapped during IAM init)
//
// When either is missing we log and skip resolver installation — the central
// continues to run with only local backends.
func (s *Server) installRemoteBackendResolver(agentSvc orchestration.AgentService) {
	if s.fleetRegistry == nil {
		s.logger.Warn("Remote backend resolver not installed: agent registry unavailable")
		return
	}
	if s.orchestratorDaemonClient == nil {
		s.logger.Warn("Remote backend resolver not installed: orchestrator daemon client not bootstrapped")
		return
	}
	wirer, ok := agentSvc.(interface {
		SetRemoteBackendResolver(orchestration.RemoteBackendResolver)
	})
	if !ok {
		s.logger.Warn("Remote backend resolver not installed: agent service lacks SetRemoteBackendResolver")
		return
	}

	adapter := logger.NewAdapter(s.logger)
	caCertPath := filepath.Join(s.config.Storage.DataDir, "certs", "ca-chain.crt")
	pool := newRemoteBackendPool(s.fleetRegistry, s.orchestratorDaemonClient, s.centralTokenEndpoint(), caCertPath, s.config.Orchestration.HostAffinity, adapter)
	// A daemon silent past the heartbeat bound is not dispatched to, and a
	// spawn that mounts this daemon's signing socket is placed only on an agent
	// that proved it can reach this daemon's sockets root (go-quicbaas#981).
	pool.staleAfter = s.config.Orchestration.HeartbeatTimeout
	pool.auditor = newOrchestrationStoreAuditor(s.auditLoggerForStores)
	if socketsRoot, rootErr := s.socketsRoot(); rootErr == nil {
		pool.signingRoot = socketsRoot
	}
	// Placement ranks eligible agents by the jobs live on each, so work
	// spreads across the fleet rather than onto the first agent listed.
	if reporter, ok := agentSvc.(orchestration.FleetLoadReporter); ok {
		pool.liveLoad = reporter.LiveJobsByHost
	}
	// Every signature relayed to a fleet node lands on the platform audit
	// trail, beside the orchestration stores' own records.
	pool.signingAudit = newSigningRelayAuditor(s.auditLoggerForStores)

	// Wire the embedded dialer registry so in-process agents bypass TCP.
	// Initialized here so the pool is ready before embedded agent startup;
	// the registry itself is populated later by installEmbeddedBackendDialer.
	if s.embeddedDialerRegistry == nil {
		s.embeddedDialerRegistry = NewEmbeddedDialerRegistry()
	}
	pool.embeddedDialers = s.embeddedDialerRegistry

	wirer.SetRemoteBackendResolver(pool.resolve)
	s.logger.Info("Remote backend resolver installed: daemon-kind registrations will be dispatched via RemoteACPBackend")
}

// centralTokenEndpoint returns the fully-qualified /oauth2/token URL for this
// server. The resolver uses it to mint JWTs for outbound daemon calls using
// the orchestrator's client_credentials app. Falls back to 127.0.0.1 when
// Server.Host is 0.0.0.0 since the central dials itself.
func (s *Server) centralTokenEndpoint() string {
	host := s.config.Server.Host
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	port := s.config.Server.REST.Port
	if port == 0 {
		// Fall back to the grpc port if REST is disabled — we only need to
		// reach the OIDC provider which is registered on both transports.
		port = s.config.Server.GRPC.Port
	}
	return fmt.Sprintf("https://%s:%d/oauth2/token", host, port)
}

// =============================================================================
// remoteBackendPool
// =============================================================================

// remoteBackendPool caches RemoteACPBackend instances so we don't re-dial per
// spawn request. The cache is two levels — daemon host, then the backend name
// the caller asked for — because both axes vary independently: one daemon
// serves several backends, and each backend name needs its own instance to
// stamp on the wire (go-quicbaas#937). Cache entries are evicted when the
// backing registration goes offline (via heartbeat expiry detected on next
// resolve call) or is replaced by a new registration on the same host.
type remoteBackendPool struct {
	registry    orchestration.FleetRegistry
	centralCred *OrchestratorDaemonClient
	tokenURL    string
	caCertPath  string
	logger      interfaces.Logger

	// hostAffinityCfg carries the operator-level enforcement default for
	// host_affinity hints. Per-request HostAffinityStrict overrides via
	// context still take precedence; this is the fall-through default.
	hostAffinityCfg config.SchedulerHostAffinityConfig

	// embeddedDialers maps FleetNode.Host to a bufconn dialer for
	// embedded agents. When non-nil and a dialer is found for a host, the
	// pool bypasses the TCP/TLS dial path entirely.
	embeddedDialers *EmbeddedDialerRegistry

	// staleAfter is the heartbeat bound past which a registration is not
	// dispatched to, whatever its status says. Non-positive selects
	// orchestration.DefaultFleetHeartbeatTimeout.
	staleAfter time.Duration

	// now is the clock liveness is judged against; nil is time.Now.
	now func() time.Time

	// auditor records remote spawns a daemon refused for an exhausted
	// account pool; nil records nothing.
	auditor orchestration.StoreAuditor

	// signingRoot is this daemon's signing sockets root. Its marker names the
	// identifier a signing spawn requires of an agent; empty (or a root with
	// no marker yet) means no agent can be proved to reach it.
	signingRoot string

	// liveLoad reports the jobs live on each agent host, which placement ranks
	// eligible agents by. Nil counts every agent as idle.
	liveLoad func() map[string]int

	// signingAudit records every signature relayed to a fleet node.
	signingAudit orchestration.SigningRelayAuditor

	// placeMu guards placements.
	placeMu sync.Mutex
	// placements maps agent host -> when spawns were placed there, pruned to
	// recentPlacementWindow on read.
	placements map[string][]time.Time

	mu sync.Mutex
	// entries maps daemon host -> the backends constructed for it.
	entries map[string]*pooledDaemon
}

// pooledDaemon holds every backend instance constructed for one daemon host.
//
// THE HOST IS THE INVALIDATION UNIT, THE BACKEND NAME IS THE CACHE AXIS. A
// daemon replaced at the same address (new registration id) invalidates all of
// its backends at once, which a flat map keyed by host+backend could only do by
// scanning; a lookup for a second backend on a live daemon adds an instance
// beside the first rather than replacing it.
type pooledDaemon struct {
	registrationID string
	// backends maps the REQUESTED backend name -> the instance that serves it.
	backends map[string]*orchestration.RemoteACPBackend
}

func newRemoteBackendPool(
	registry orchestration.FleetRegistry,
	cred *OrchestratorDaemonClient,
	tokenURL string,
	caCertPath string,
	hostAffinityCfg config.SchedulerHostAffinityConfig,
	logger interfaces.Logger,
) *remoteBackendPool {
	return &remoteBackendPool{
		registry:        registry,
		centralCred:     cred,
		tokenURL:        tokenURL,
		caCertPath:      caCertPath,
		hostAffinityCfg: hostAffinityCfg,
		logger:          logger,
		entries:         map[string]*pooledDaemon{},
	}
}

// resolve implements orchestration.RemoteBackendResolver. Returns (backend,
// true) when a registered daemon satisfies all of: serving the requested
// backend, host-affinity, and GPU constraints; returns (nil, false) to let
// the dispatcher fall through to local backends.
//
// Filtering rules applied in order:
//  1. Backend filter: agent must serve the named backend, primary or capability
//     (orchestration.FleetNodeServesBackend — the one predicate every backend
//     filter in the stack shares).
//  2. Host-affinity filter (when non-empty): Name, Host exact match, or Host prefix match.
//  3. GPU filter (when GpuRequired): agent Capabilities must contain "gpu".
//
// When all online agents exist but none survives filtering, a
// NoMatchingFleetNodeError is logged; (nil, false) is still returned so the
// dispatcher can surface BackendNotFoundError to the caller.
func (p *remoteBackendPool) resolve(ctx context.Context, backend, projectID string) (orchestration.AgentBackend, bool) {
	tenantID := orchestration.TenantFromContext(ctx)
	if tenantID == "" {
		return nil, false
	}
	hostAffinity := orchestration.HostAffinityFromContext(ctx)
	gpuRequired := orchestration.GpuRequiredFromContext(ctx)
	image := orchestration.ContainerImageFromContext(ctx)

	// Pull all tenant+backend matches across multiple status values; a
	// just-enrolled daemon is in FleetNodeStatusRegistered until its first
	// heartbeat, which would otherwise hide it from dispatch.
	var allRegs []*orchestration.FleetNode
	for _, status := range []orchestration.FleetNodeStatus{
		orchestration.FleetNodeStatusRunning,
		orchestration.FleetNodeStatusIdle,
		orchestration.FleetNodeStatusRegistered,
	} {
		regs, err := p.registry.ListFleetNodes(ctx, tenantID, &orchestration.FleetNodeFilter{
			Backend: backend,
			Status:  status,
		})
		if err != nil {
			p.logger.Warnf("remote resolver: list agents failed: tenant=%s backend=%s status=%s err=%v",
				tenantID, backend, status, err)
			continue
		}
		allRegs = append(allRegs, regs...)
	}
	if len(allRegs) == 0 {
		return nil, false
	}

	// FRESHNESS DECIDES PLACEMENT ONLY. A new session is not sent to an agent
	// that has gone quiet; an existing session's operation still goes to the
	// agent that holds it, whose own answer is the only one that counts — a
	// missed heartbeat is not evidence the session is gone.
	if orchestration.SpawnPlacementFromContext(ctx) {
		allRegs = p.liveRegistrations(allRegs)
	}
	signingTrait, signingErr := p.requiredSigningTrait(ctx)
	if signingErr != nil {
		p.logger.Warnf("remote resolver: signing placement: %v", signingErr)
	}
	// SELECTION AND ITS RECORD ARE ONE DECISION for a new session: made
	// separately, a burst of concurrent spawns all ranked the fleet before any
	// of them was recorded, and all chose the same agent.
	placing := orchestration.SpawnPlacementFromContext(ctx)
	reg, constraint := p.chooseDaemon(allRegs, backend, hostAffinity, gpuRequired, signingTrait, image, placing)
	if reg == nil {
		noMatchErr := buildNoMatchError(backend, hostAffinity, gpuRequired, constraint)
		// In hard mode (operator default OR per-request override) and when
		// the failure was specifically the host-affinity filter, stash the
		// typed error on the context so the public-API handler can surface
		// 400 no_matching_agent rather than the soft 404 fall-through.
		strict := orchestration.HostAffinityStrictFromContext(ctx) || p.hostAffinityCfg.IsHard()
		if strict && constraint == "host_affinity" {
			noMatchErr.Detail = "host " + hostAffinity + " is offline"
			orchestration.StashResolveError(ctx, noMatchErr)
		}
		p.logger.Warnf("remote resolver: %s tenant=%s strict=%v", noMatchErr.Error(), tenantID, strict)
		return nil, false
	}
	built, ok := p.backendFor(ctx, reg, backend)
	if placing && !ok {
		// Nothing was placed after all; the reservation would only skew the
		// next ranking.
		p.forgetPlacement(reg.Host)
	}
	return built, ok
}

// chooseDaemon is pickDaemon made atomic with its record: under the placement
// lock it ranks the fleet and, when place is set, records the choice before
// any concurrent placement can rank.
func (p *remoteBackendPool) chooseDaemon(
	regs []*orchestration.FleetNode,
	backend, hostAffinity string,
	gpuRequired bool,
	signingTrait string,
	image string,
	place bool,
) (*orchestration.FleetNode, string) {
	p.placeMu.Lock()
	defer p.placeMu.Unlock()
	reg, constraint := p.pickDaemonLocked(regs, backend, hostAffinity, gpuRequired, signingTrait, image)
	if reg != nil && place {
		p.notePlacementLocked(reg.Host)
	}
	return reg, constraint
}

// pickDaemon chooses, among every daemon-kind or embedded agent that
// satisfies the spawn's constraints, the least-loaded one. It returns the
// chosen registration and an empty constraint name on success. On failure it
// returns (nil, <constraint>) where constraint names the first filter that
// eliminated all candidates, helping callers build actionable error messages.
//
// Constraint evaluation order mirrors the priority operators expect:
//  1. backend — the agent must serve the named backend
//  2. host_affinity — Name, Host exact match, or Host prefix match
//  3. gpu — Capabilities must contain "gpu"
//  4. signing_agent — for a spawn that mounts this daemon's signing socket,
//     the agent is embedded, advertises this daemon's signing-agent trait, or
//     advertises the signing relay (go-quicbaas#1013)
//  5. image — a spawn that names a container image goes only to an agent
//     that has reported having it (or has never reported its images)
//  6. capacity — an unpinned spawn skips an agent already at its advertised
//     MaxConcurrentJobs
//
// THE FIRST ELIGIBLE AGENT IS NOT THE ANSWER. It was, and every spawn landed on
// whichever agent the registry listed first while the rest of the fleet sat
// registered, online and idle. See leastLoaded for the ranking.
func (p *remoteBackendPool) pickDaemon(
	regs []*orchestration.FleetNode,
	backend, hostAffinity string,
	gpuRequired bool,
	signingTrait string,
) (reg *orchestration.FleetNode, failedConstraint string) {
	return p.chooseDaemon(regs, backend, hostAffinity, gpuRequired, signingTrait, "", false)
}

// pickDaemonLocked is pickDaemon; the caller holds placeMu.
func (p *remoteBackendPool) pickDaemonLocked(
	regs []*orchestration.FleetNode,
	backend, hostAffinity string,
	gpuRequired bool,
	signingTrait string,
	image string,
) (reg *orchestration.FleetNode, failedConstraint string) {
	passedBackend := false
	passedAffinity := false
	passedGPU := false
	passedSigning := false
	eligible := make([]*orchestration.FleetNode, 0, len(regs))

	for _, r := range regs {
		if r == nil {
			continue
		}
		kind := r.EffectiveKind()
		if kind != orchestration.FleetNodeKindDaemon && kind != orchestration.FleetNodeKindEmbedded {
			continue
		}
		if !orchestration.FleetNodeServesBackend(r, backend) {
			continue
		}
		passedBackend = true

		if hostAffinity != "" && r.Name != hostAffinity && r.Host != hostAffinity && !strings.HasPrefix(r.Host, hostAffinity+":") {
			continue
		}
		passedAffinity = true

		if gpuRequired && !capabilityContains(r.Capabilities, "gpu") {
			continue
		}
		passedGPU = true

		if signingTrait != "" && !canServeSigningSocket(r, signingTrait) {
			continue
		}
		passedSigning = true

		// THE IMAGE IS ON THE HOST, OR THE SPAWN FAILS THERE. Worker images
		// are loaded onto hosts rather than pulled, so an agent without the
		// spawn's image fails it at image pull (measured 2026-09-23: every
		// review placed on a fleet node without the reviewer image failed).
		if !orchestration.FleetNodeHasImage(r, image) {
			continue
		}
		eligible = append(eligible, r)
	}

	if len(eligible) == 0 {
		// Identify the earliest constraint that blocked all candidates.
		switch {
		case !passedBackend:
			return nil, "backend"
		case hostAffinity != "" && !passedAffinity:
			return nil, "host_affinity"
		case gpuRequired && !passedGPU:
			return nil, "gpu"
		case signingTrait != "" && !passedSigning:
			return nil, "signing_agent"
		default:
			return nil, "image"
		}
	}
	chosen := p.leastLoaded(eligible, hostAffinity != "")
	if chosen == nil {
		return nil, "capacity"
	}
	return chosen, ""
}

// canServeSigningSocket reports whether a spawn that mounts this daemon's
// signing socket can run on r. An EMBEDDED agent runs inside this daemon, so
// the socket is local to it by construction (its mount policy admits the
// sockets root); a daemon-kind agent either proves it reaches the root — this
// daemon's signing-agent trait — or serves the socket from a relay over its
// FleetRuntime link.
func canServeSigningSocket(r *orchestration.FleetNode, signingTrait string) bool {
	return r.EffectiveKind() == orchestration.FleetNodeKindEmbedded ||
		capabilityContains(r.Capabilities, signingTrait) ||
		capabilityContains(r.Capabilities, orchestration.FleetNodeTraitSigningRelay)
}

// recentPlacementWindow is how long a placement counts as recent when two
// agents are otherwise equally loaded. A burst of spawns is placed before any
// of them has a live session record to count, and without this tie-break the
// whole burst would go to the one agent the name order favours.
const recentPlacementWindow = 10 * time.Minute

// leastLoaded returns the eligible agent with the lowest load relative to its
// capacity, or nil when every one is full.
//
// THE RANKING, in order:
//  1. live jobs on the agent (orchestration.FleetLoadReporter) over its
//     advertised MaxConcurrentJobs — compared as cross products, so no
//     division and no floating point; an agent reporting no capacity is
//     ranked as a capacity of one and is never skipped as full;
//  2. fewest placements within recentPlacementWindow;
//  3. name, then host, so the choice is deterministic.
//
// A PINNED SPAWN IS NEVER SKIPPED FOR CAPACITY. Host affinity names the host
// that holds the job's workspace, and the job's later sessions must reach it;
// the job already holds its slot there.
//
// The caller holds placeMu.
func (p *remoteBackendPool) leastLoaded(eligible []*orchestration.FleetNode, pinned bool) *orchestration.FleetNode {
	load := map[string]int{}
	if p.liveLoad != nil {
		load = p.liveLoad()
	}
	recent := p.recentPlacementsLocked()
	var best *orchestration.FleetNode
	for _, r := range eligible {
		if !pinned && r.MaxConcurrentJobs > 0 && load[r.Host] >= r.MaxConcurrentJobs {
			p.logger.Debugf("remote resolver: skipping agent %s (%s): at capacity %d/%d",
				r.ID, r.Host, load[r.Host], r.MaxConcurrentJobs)
			continue
		}
		if best == nil || lessLoaded(r, best, load, recent) {
			best = r
		}
	}
	return best
}

// lessLoaded reports whether a ranks before b. See leastLoaded.
func lessLoaded(a, b *orchestration.FleetNode, load, recent map[string]int) bool {
	left := load[a.Host] * rankingCapacity(b)
	right := load[b.Host] * rankingCapacity(a)
	if left != right {
		return left < right
	}
	if recent[a.Host] != recent[b.Host] {
		return recent[a.Host] < recent[b.Host]
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.Host < b.Host
}

// rankingCapacity is the capacity an agent is ranked against: its advertised
// MaxConcurrentJobs, or one when it advertises none.
func rankingCapacity(r *orchestration.FleetNode) int {
	if r.MaxConcurrentJobs > 0 {
		return r.MaxConcurrentJobs
	}
	return 1
}

// notePlacementLocked records that a spawn was placed on host. The caller
// holds placeMu.
func (p *remoteBackendPool) notePlacementLocked(host string) {
	if p.placements == nil {
		p.placements = map[string][]time.Time{}
	}
	p.placements[host] = append(p.placements[host], p.clock())
}

// forgetPlacement drops the newest placement recorded on host: one reserved
// for a spawn that then went nowhere.
func (p *remoteBackendPool) forgetPlacement(host string) {
	p.placeMu.Lock()
	defer p.placeMu.Unlock()
	times := p.placements[host]
	switch len(times) {
	case 0:
	case 1:
		delete(p.placements, host)
	default:
		p.placements[host] = times[:len(times)-1]
	}
}

// recentPlacementsLocked counts, per host, the placements within
// recentPlacementWindow, dropping the older ones as it goes. The caller holds
// placeMu.
func (p *remoteBackendPool) recentPlacementsLocked() map[string]int {
	cutoff := p.clock().Add(-recentPlacementWindow)
	counts := make(map[string]int, len(p.placements))
	for host, times := range p.placements {
		kept := times[:0]
		for _, at := range times {
			if at.After(cutoff) {
				kept = append(kept, at)
			}
		}
		if len(kept) == 0 {
			delete(p.placements, host)
			continue
		}
		p.placements[host] = kept
		counts[host] = len(kept)
	}
	return counts
}

// clock is the pool's notion of now.
func (p *remoteBackendPool) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

// liveRegistrations drops every registration that is not a daemon that will
// take work — above all one whose last heartbeat is older than the bound.
//
// A CRASHED DAEMON CANNOT MARK ITSELF OFFLINE, so its record keeps saying
// "running" until something decides otherwise, and nothing else did: the
// dispatcher routed the remote fleet node's reviews to a daemon that had been dead for hours,
// and every one failed on a refused connection. The same predicate the fleet's
// capacity sum uses decides it here (orchestration.FleetNodeAlive).
func (p *remoteBackendPool) liveRegistrations(regs []*orchestration.FleetNode) []*orchestration.FleetNode {
	staleAfter := p.staleAfter
	if staleAfter <= 0 {
		staleAfter = orchestration.DefaultFleetHeartbeatTimeout
	}
	now := p.clock()
	live := make([]*orchestration.FleetNode, 0, len(regs))
	for _, r := range regs {
		// An embedded agent is this process: it is alive exactly when the
		// resolver asking is, and it heartbeats nothing to prove it.
		if r != nil && r.EffectiveKind() == orchestration.FleetNodeKindEmbedded {
			live = append(live, r)
			continue
		}
		if orchestration.FleetNodeAlive(r, now, staleAfter) {
			live = append(live, r)
			continue
		}
		if r != nil {
			p.logger.Debugf("remote resolver: skipping agent %s (%s): no heartbeat within %s",
				r.ID, r.Host, staleAfter)
		}
	}
	return live
}

// requiredSigningTrait is the trait a spawn resolved under ctx requires of an
// agent: this daemon's signing-agent trait when the spawn mounts the signing
// socket, "" when it does not. A signing spawn on a daemon whose sockets root
// carries no identifier yet can be placed on no agent, and gets a trait no
// agent holds rather than none at all.
func (p *remoteBackendPool) requiredSigningTrait(ctx context.Context) (string, error) {
	if !orchestration.SigningAgentRequiredFromContext(ctx) {
		return "", nil
	}
	if p.signingRoot == "" {
		return unreachableSigningTrait, nil
	}
	id, err := orchestration.ReadSigningRootID(p.signingRoot)
	if err != nil {
		return unreachableSigningTrait, err
	}
	return orchestration.SigningAgentTrait(id), nil
}

// localSigningTrait is this daemon's signing-agent trait, or "" when its
// sockets root carries no identifier: what a remote backend compares an
// agent's capabilities against to decide between a direct mount and a relay.
func (p *remoteBackendPool) localSigningTrait() string {
	if p.signingRoot == "" {
		return ""
	}
	id, err := orchestration.ReadSigningRootID(p.signingRoot)
	if err != nil {
		return ""
	}
	return orchestration.SigningAgentTrait(id)
}

// unreachableSigningTrait is a trait no agent can advertise — its identifier
// is not the shape DiscoverSigningRootIDs reports — so a signing spawn this
// daemon cannot identify is placed on no agent.
const unreachableSigningTrait = orchestration.FleetNodeTraitSigningAgentPrefix + "unidentified"

// buildNoMatchError constructs a NoMatchingFleetNodeError that names the
// unsatisfied constraint and all active filters for clear operator messaging.
func buildNoMatchError(backend, hostAffinity string, gpuRequired bool, constraint string) *orchestration.NoMatchingFleetNodeError {
	detail := "no online agents serve backend=" + backend
	if hostAffinity != "" {
		detail += " and host_affinity=" + hostAffinity
	}
	if gpuRequired {
		detail += " and have capability=gpu"
	}
	switch constraint {
	case "signing_agent":
		detail += " and can mount or relay this daemon's signing socket"
	case "image":
		detail += " and has the spawn's container image loaded"
	case "capacity":
		detail += " and have a free job slot"
	}
	return &orchestration.NoMatchingFleetNodeError{
		Constraint: constraint,
		Detail:     detail,
	}
}

// capabilityContains reports whether the capabilities slice advertises the
// named trait. The backend axis is answered by
// orchestration.FleetNodeServesBackend; this is the trait axis — "gpu" — which a
// registration carries beside its backends rather than as one of them.
func capabilityContains(caps []string, backend string) bool {
	for _, c := range caps {
		if c == backend {
			return true
		}
	}
	return false
}

// backendFor returns a cached *RemoteACPBackend for the (daemon, backend)
// pair, or constructs one on first use.
//
// BOTH HALVES OF THE KEY ARE LOAD-BEARING. The host is the dialable target;
// the backend is the name the instance stamps on every RPC. Keying on the host
// alone handed a request for "codex" the instance built for "claude", which
// then asked the daemon for claude — a working session running the wrong model
// family, with the session record still saying codex (go-quicbaas#937).
//
// A registration that does not serve the requested backend is refused rather
// than served by its primary: NewRemoteACPBackend returns a typed
// NoMatchingFleetNodeError, which is stashed on the context so the public API
// surfaces the refusal instead of silently falling through to a local backend.
func (p *remoteBackendPool) backendFor(
	ctx context.Context,
	reg *orchestration.FleetNode,
	backend string,
) (orchestration.AgentBackend, bool) {
	if cached, ok := p.cachedBackend(reg, backend); ok {
		return cached, true
	}

	// For embedded agents, use the bufconn dialer so RPCs stay in-process.
	// The dialer is keyed by Host, which is the logical agent name set at
	// registration time (e.g. the hostname or host-affinity tag) — the backend
	// axis of the cache key above does not reach this lookup, so an embedded
	// daemon serving several backends resolves the same dialer for each.
	var connDialer func(ctx context.Context, target string) (*grpc.ClientConn, error)
	isEmbedded := false
	if p.embeddedDialers != nil {
		if d, ok := p.embeddedDialers.Lookup(reg.Host); ok {
			connDialer = d
			isEmbedded = true
		}
	}

	// Embedded agents run in-process with a synthetic identity set on their
	// GRPCSpawnServer (see embedded_fleet_node.go buildEmbeddedFleetNodeIdentity).
	// Their bufconn gRPC server has no bearer-token interceptor, so the token
	// fetch is unnecessary and causes TLS handshake noise when the loopback
	// client tries to hit /oauth2/token. Skip the token source for embedded
	// agents; only remote daemon backends need OAuth2 bearer tokens.
	var tokenSource orchestration.RemoteTokenSource
	if !isEmbedded {
		tokenSource = newCentralOAuth2TokenSource(p.centralCred, p.tokenURL, p.caCertPath)
	}

	built, err := orchestration.NewRemoteACPBackend(orchestration.RemoteACPBackendDeps{
		Registration: reg,
		Backend:      backend,
		TokenSource:  tokenSource,
		ConnDialer:   connDialer,
		Logger:       p.logger,
		Auditor:      p.auditor,
		SigningTrait: p.localSigningTrait,
		SigningAudit: p.signingAudit,
	})
	if err != nil {
		p.logger.Warnf("remote resolver: build RemoteACPBackend failed: host=%s backend=%s err=%v",
			reg.Host, backend, err)
		// A refusal the caller can act on travels; a construction fault does
		// not become a silent fall-through to some other backend.
		var noMatch *orchestration.NoMatchingFleetNodeError
		if errors.As(err, &noMatch) {
			orchestration.StashResolveError(ctx, noMatch)
		}
		return nil, false
	}

	stored := p.storeBackend(reg, backend, built)
	p.logger.Infof("remote resolver: routed backend=%q tenant=%s → daemon=%s host=%s primary=%q",
		backend, reg.TenantID, reg.Name, reg.Host, reg.Backend)
	return stored, true
}

// cachedBackend returns the pooled instance for the (daemon, backend) pair.
// A registration id that no longer matches means the daemon at this host was
// replaced, so EVERY backend cached for the host is dropped — invalidation is
// per daemon even though lookup is per backend.
//
// The dropped instances are not closed here: a session spawned before the
// replacement still holds its instance and its conn, and closing the pool
// underneath it would fail a live turn. They are released when the holder
// calls CloseAll.
func (p *remoteBackendPool) cachedBackend(
	reg *orchestration.FleetNode,
	backend string,
) (*orchestration.RemoteACPBackend, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.entries[reg.Host]
	if !ok {
		return nil, false
	}
	if entry.registrationID != reg.ID {
		delete(p.entries, reg.Host)
		return nil, false
	}
	cached, ok := entry.backends[backend]
	return cached, ok
}

// storeBackend records a freshly constructed instance and returns the one the
// pool now holds. A concurrent caller that constructed the same pair first
// wins, so every caller for one (daemon, backend) shares a single conn pool.
func (p *remoteBackendPool) storeBackend(
	reg *orchestration.FleetNode,
	backend string,
	built *orchestration.RemoteACPBackend,
) *orchestration.RemoteACPBackend {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.entries[reg.Host]
	if !ok || entry.registrationID != reg.ID {
		entry = &pooledDaemon{
			registrationID: reg.ID,
			backends:       map[string]*orchestration.RemoteACPBackend{},
		}
		p.entries[reg.Host] = entry
	}
	if existing, ok := entry.backends[backend]; ok {
		return existing
	}
	entry.backends[backend] = built
	return built
}

// =============================================================================
// centralOAuth2TokenSource — orchestration.RemoteTokenSource implementation
// =============================================================================

// centralOAuth2TokenSource implements orchestration.RemoteTokenSource by calling the
// central's own /oauth2/token endpoint with client_credentials.
//
// Using the OIDC network round-trip (rather than minting the JWT in-process)
// keeps this code honest: the central authenticates to daemons exactly the
// way any third-party client would, which means daemons never need special
// trust of the central beyond the standard JWT validation.
type centralOAuth2TokenSource struct {
	client     *http.Client
	cred       *OrchestratorDaemonClient
	tokenURL   string
	loopback   bool
	caCertPath string

	// mu guards cached and flight and is never held across the network
	// call: a caller that finds a fetch in flight waits on it outside the
	// lock, where its own context can release it (go-quicbaas#193).
	mu     sync.Mutex
	cached *oauth2.Token
	flight *centralTokenFetch
}

// centralTokenFetch is one token exchange in flight, shared by every caller
// that needs a token while it runs. done closes once tok and err are set.
type centralTokenFetch struct {
	done chan struct{}
	tok  *oauth2.Token
	err  error
}

// centralTokenExpiryLeeway is how long before its expiry a cached token is
// already treated as expired, so a token is never sent with a moment to live.
const centralTokenExpiryLeeway = 5 * time.Second

// newCentralOAuth2TokenSource constructs the source. The http.Client is the
// default client — we rely on the central's cert being self-trusted via the
// system trust store. For loopback-local token endpoint calls (central
// talking to itself on 127.0.0.1) we build a dedicated http.Client that
// trusts the daemon's on-disk ca-chain.crt, since the cert was issued by
// the daemon's own CA and is not in the system trust store.
func newCentralOAuth2TokenSource(cred *OrchestratorDaemonClient, tokenURL, caCertPath string) *centralOAuth2TokenSource {
	return &centralOAuth2TokenSource{
		client:     &http.Client{Timeout: 15 * time.Second},
		cred:       cred,
		tokenURL:   tokenURL,
		loopback:   isLoopback(tokenURL),
		caCertPath: caCertPath,
	}
}

// Token returns the cached token while it is valid. Otherwise it waits, within
// ctx, for one token exchange shared by every caller that needs it.
func (t *centralOAuth2TokenSource) Token(ctx context.Context) (*oauth2.Token, error) {
	t.mu.Lock()
	if t.cached != nil && time.Until(t.cached.Expiry) > centralTokenExpiryLeeway {
		tok := t.cached
		t.mu.Unlock()
		return tok, nil
	}
	f := t.flight
	if f == nil {
		f = &centralTokenFetch{done: make(chan struct{})}
		t.flight = f
		// THE EXCHANGE IS NOT CANCELLED WITH THE CALLER THAT STARTED IT.
		// Others may be waiting on it; each leaves on its own context, and
		// the exchange itself is bounded by the client's timeout.
		go t.fetch(context.WithoutCancel(ctx), f)
	}
	t.mu.Unlock()

	select {
	case <-f.done:
		return f.tok, f.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// fetch runs one token exchange, caches a token it gets, and releases every
// caller waiting on f.
func (t *centralOAuth2TokenSource) fetch(ctx context.Context, f *centralTokenFetch) {
	tok, err := t.refresh(ctx)
	t.mu.Lock()
	if err == nil {
		t.cached = tok
	}
	t.flight = nil
	t.mu.Unlock()
	f.tok, f.err = tok, err
	close(f.done)
}

// refresh performs the client_credentials exchange. Uses HTTP Basic auth per
// RFC 6749 §2.3.1 since that is the form pkg/api/rest/iam_oidc.go's Token
// handler prefers.
func (t *centralOAuth2TokenSource) refresh(ctx context.Context) (*oauth2.Token, error) {
	body := url.Values{}
	body.Set("grant_type", "client_credentials")
	body.Set("scope", strings.Join(t.cred.Scopes, " "))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.tokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(t.cred.ClientID, t.cred.ClientSecret)

	client := t.client
	if t.loopback {
		lbClient, lbErr := loopbackTrustedClient(t.caCertPath)
		if lbErr != nil {
			return nil, &ServerCauseError{Message: "loopback token client", Cause: lbErr}
		}
		client = lbClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, &ServerMessageError{Message: fmt.Sprintf("central token exchange: status=%d", resp.StatusCode)}
	}

	// Minimal decode — we only need access_token + expires_in.
	var out struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
		Scope       string `json:"scope"`
	}
	if err := decodeJSON(resp.Body, &out); err != nil {
		return nil, err
	}
	if out.AccessToken == "" {
		return nil, &ServerMessageError{Message: "central token exchange: empty access_token"}
	}
	ttl := time.Duration(out.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &oauth2.Token{
		AccessToken: out.AccessToken,
		TokenType:   out.TokenType,
		Expiry:      time.Now().Add(ttl),
	}, nil
}

// decodeJSON wraps json.Decode with a 1 MiB body cap so a compromised or
// pathological token endpoint can't exhaust memory.
func decodeJSON(r io.Reader, v interface{}) error {
	return json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(v)
}

// isLoopback reports whether the URL host resolves to 127.0.0.1 / ::1. Used
// to decide whether to accept the self-issued cert on the loopback token
// endpoint. Non-loopback token URLs go through the normal system roots.
func isLoopback(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

// loopbackTrustedClient returns an http.Client that trusts the daemon's own
// CA chain from disk. Used when the central is talking to itself on 127.0.0.1
// (the cert is issued by the daemon's CA). QuicBaaS bans insecure TLS, so we
// load the on-disk ca-chain.crt explicitly rather than skip verification.
//
// The caCertPath is set on the remoteBackendPool at construction time from
// the server config's data-dir/certs/ca-chain.crt. Empty path returns an
// error — there is no fallback to insecure mode.
func loopbackTrustedClient(caCertPath string) (*http.Client, error) {
	if caCertPath == "" {
		return nil, &ServerMessageError{Message: "loopbackTrustedClient: caCertPath is required (QuicBaaS bans insecure TLS)"}
	}
	caPEM, err := os.ReadFile(caCertPath) //nolint:gosec // caCertPath comes from server config
	if err != nil {
		return nil, &ServerCauseError{Message: fmt.Sprintf("read ca-chain %s", caCertPath), Cause: err}
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, &ServerMessageError{Message: fmt.Sprintf("no PEM certs in ca-chain %s", caCertPath)}
	}
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				MinVersion: tls.VersionTLS13,
			},
		},
	}, nil
}
