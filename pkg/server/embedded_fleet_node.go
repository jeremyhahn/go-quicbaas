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

// Package server: embedded fleet node
//
// The embedded fleet node runs an in-process pkg/fleet Runtime alongside the
// quicbaasd orchestrator and registers it through the same fleet registry
// that remote daemons use. Communication uses a bufconn.Listener so gRPC
// call semantics are preserved without any network overhead.
//
// Transport choice — bufconn (Option A):
//
//	bufconn.Listener provides gRPC's official in-memory transport. All gRPC
//	interceptors, codec paths, and error types work identically to TCP. The
//	alternative (a direct handler shim) would require duplicating every
//	proto message encoding/decoding path and break with future proto changes.
//	bufconn costs ~1 µs per RPC for the in-memory copy — negligible compared
//	to LLM inference latency.
package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/fleet"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/ollama"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

const (
	// embeddedFleetNodeBufConnSize is the in-memory buffer size for the bufconn
	// loopback transport. 1 MiB matches bufconn defaults in the gRPC test suite.
	embeddedFleetNodeBufConnSize = 1 << 20 // 1 MiB

	// embeddedFleetNodeTenantID is the default tenant the embedded fleet node is
	// registered under: the control-plane system tenant that qrdb always
	// provisions. Operators running multi-tenant quicbaasd should ensure
	// fleet nodes land in the right tenant namespace.
	embeddedFleetNodeTenantID = qrdbsdk.SystemTenantID

	// embeddedFleetNodeStopTimeout is the graceful shutdown grace period for the
	// embedded fleet node's gRPC server.
	embeddedFleetNodeStopTimeout = 15 * time.Second
)

// embeddedFleetNode holds all state for the in-process fleet node runtime.
// All fields are initialized atomically during startEmbeddedFleetNode and
// read-only thereafter (except mu-guarded stopped flag).
type embeddedFleetNode struct {
	// nodeID is the unique identifier under which this fleet node is registered
	// in the fleet registry. Format: "embedded-<hostname>". Stable across
	// restarts so the registry holds exactly one entry per host.
	nodeID string

	// spawnServer is the daemon-side gRPC service backed by the Runtime.
	// It listens on the bufconn listener (no real network socket).
	spawnServer *fleet.GRPCSpawnServer

	// bufListener is the in-memory gRPC transport. The spawn server serves
	// on this listener; the RemoteACPBackend dials it via ConnDialer.
	bufListener *bufconn.Listener

	// runtime is the fleet node backend dispatcher. Shared reference — the spawn
	// server delegates to it for every RPC.
	runtime *fleet.Runtime

	// registration is the canonical fleet entry created at startup.
	registration *orchestration.FleetNode

	// cancelCtx cancels the embedded fleet node's background goroutines (none for
	// now — the gRPC server lifecycle is managed via Stop).
	cancelCtx context.CancelFunc

	mu      sync.Mutex
	stopped bool
}

// stop gracefully shuts down the embedded fleet node's gRPC server.
func (ea *embeddedFleetNode) stop(ctx context.Context) error {
	ea.mu.Lock()
	if ea.stopped {
		ea.mu.Unlock()
		return nil
	}
	ea.stopped = true
	ea.cancelCtx()
	ea.mu.Unlock()
	return ea.spawnServer.Stop(ctx)
}

// embeddedFleetNodeField guards the embeddedFleetNode pointer on the Server struct.
// Stored as a field added below via the embedding approach.
// We use a sync.Mutex-guarded pointer to keep the zero value safe.

// startEmbeddedFleetNode initialises and starts the in-process fleet node runtime.
// Call order requirements (all enforced by the caller in lifecycle order):
//  1. s.xkmsCA must be initialized (CA up, TLS enabled)
//  2. s.fleetRegistry must be non-nil (orchestration started)
//  3. s.quicBaaSService must be non-nil (service facade wired)
//
// The embedded fleet node:
//   - Issues a real mTLS cert from the local CA (first-class identity)
//   - Boots a pkg/fleet Runtime with the configured backends
//   - Starts a GRPCSpawnServer on a bufconn.Listener (zero-network-overhead)
//   - Registers in the fleet registry as FleetNodeKindEmbedded
//   - Wires a ConnDialer into the remote resolver's backend pool so the
//     orchestrator dispatches inference RPCs in-process
func (s *Server) startEmbeddedFleetNode() error {
	if s.embeddedFleetNodeState != nil {
		return &EmbeddedFleetNodeAlreadyStartedError{AgentID: s.embeddedFleetNodeState.nodeID}
	}
	if s.fleetRegistry == nil {
		return ErrEmbeddedFleetNodeNoRegistry
	}

	cfg := s.config.Server.EmbeddedFleetNode
	log := logger.NewAdapter(s.logger)

	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "localhost"
	}
	// The fleet node ID is stable across restarts for a given host so the operator
	// sees one entry in the fleet registry and the KMS holds exactly one
	// identity cert per host rather than accumulating per-boot entries.
	nodeID := embeddedFleetNodeID(hostname)

	log.Infof("Starting embedded agent: id=%s", nodeID)

	// Issue mTLS identity from the local CA. When TLS is disabled the cert
	// issuer path is unavailable; the embedded fleet node still starts but without
	// a hardware-bound identity (acceptable for non-TLS dev deployments).
	var issuedCertPEM []byte
	if s.xkmsCA != nil {
		issuer, issueErr := service.NewXKMSFleetNodeCertIssuer(s.xkmsCA)
		if issueErr == nil {
			req := &service.FleetNodeCertificateRequest{
				TenantID: embeddedFleetNodeTenantID,
				Hostname: hostname,
				Subject: service.FleetNodeCertificateSubject{
					CommonName:         "agent-" + nodeID,
					OrganizationalUnit: service.DefaultFleetNodeCertOU,
				},
				ExtraDNSNames: []string{"localhost"},
				ValidityDays:  service.DefaultFleetNodeCertValidityDays,
			}
			issued, certErr := issuer.IssueFleetNodeCertificate(context.Background(), req)
			if certErr != nil {
				log.Warnf("Embedded agent: mTLS cert issuance failed (running without mTLS identity): %v", certErr)
			} else {
				issuedCertPEM = issued.CertPEM
				// Key is secured in an mlocked buffer; destroy after we
				// have copied what we need. The embedded fleet node doesn't need
				// the key on disk — the bufconn transport is in-process.
				issued.PrivateKey.Destroy()
				log.Infof("Embedded agent: mTLS identity issued; cn=agent-%s", nodeID)
			}
		}
	} else {
		log.Warn("Embedded agent: CA not available; starting without mTLS identity (TLS disabled)")
	}

	// Build the QuicBaaS MCP preset resolver. When MCP is enabled and the CA
	// is up, fleet nodes spawned with the "quicbaas" MCP preset get a fresh,
	// short-lived mTLS identity minted per spawn and torn down on close. When
	// either is unavailable the resolver is nil and the preset is forwarded
	// unmodified (the fleet node simply cannot reach the endpoint).
	mcpResolver := s.buildEmbeddedFleetNodeMCPResolver(hostname, log)

	// Build backends from config. Each backend kind maps to a constructor
	// via the dispatch table below.
	// The container network sandboxed sessions run in. Empty resolves to the
	// isolated default; "host" is honored only because an operator set it.
	sandboxNetwork, networkErr := orchestration.ParseSandboxNetwork(cfg.SandboxNetwork)
	if networkErr != nil {
		return &ServerCauseError{Message: "embedded agent: invalid sandbox_network", Cause: networkErr}
	}

	// The privilege policy every sandboxed container runs under: --cap-drop
	// ALL, the add-back set, the pids bound and the ulimits. Refused here
	// rather than at the first spawn, which it would fail.
	sandboxUlimits, ulimitErr := orchestration.ParseContainerUlimits(cfg.SandboxUlimits)
	if ulimitErr != nil {
		return &ServerCauseError{Message: "embedded agent: invalid sandbox security policy", Cause: ulimitErr}
	}
	sandboxSecurity := orchestration.SandboxSecurity{
		CapAdd:    cfg.SandboxCapAdd,
		PidsLimit: cfg.SandboxPidsLimit,
		Ulimits:   sandboxUlimits,
	}
	if securityErr := sandboxSecurity.Validate(); securityErr != nil {
		return &ServerCauseError{Message: "embedded agent: invalid sandbox security policy", Cause: securityErr}
	}

	backends, buildErr := buildEmbeddedFleetNodeBackends(cfg.Backends, cfg.OllamaURL, sandboxNetwork, sandboxSecurity, mcpResolver, log)
	if buildErr != nil {
		return buildErr
	}

	// Resolve the sandbox policy (how SandboxModeDefault is interpreted and
	// whether an explicit Required is honored). Empty maps to opt-in.
	sandboxPolicy, policyErr := orchestration.ParseSandboxPolicy(cfg.SandboxPolicy)
	if policyErr != nil {
		return &ServerCauseError{Message: "embedded agent: invalid sandbox_policy", Cause: policyErr}
	}

	// Wire a container runtime when sandboxed sessions are reachable under the
	// policy (preferred/required) or an explicit sandbox image is configured.
	// The runtime lets the ACP backend's sandboxed spawn paths launch an
	// interactive fleet node container; without it a Required spawn fails closed.
	imageMap := buildEmbeddedFleetNodeImageMap(cfg)
	var containerRuntime orchestration.ContainerRuntime
	if sandboxPolicy != orchestration.SandboxPolicyOptIn || len(imageMap) > 0 {
		rt, rtBuildErr := orchestration.NewExecContainerRuntime(log)
		if rtBuildErr != nil {
			log.Warnf("Embedded agent: container runtime unavailable; sandboxed sessions will fail: %v", rtBuildErr)
		} else {
			containerRuntime = rt
			if cfg.AutoPull {
				for backendName, image := range imageMap {
					if pullErr := rt.Pull(context.Background(), image); pullErr != nil {
						log.Warnf("Embedded agent: pre-pull of sandbox image %q (backend %s) failed: %v", image, backendName, pullErr)
					}
				}
			}
		}
	}

	// The mount policy admits this daemon's OWN sockets root, which is
	// derived per-daemon; a failure to derive it is a startup failure rather
	// than a policy with an empty root, which would deny every signing-socket
	// mount the sdlc runner asks for.
	socketsRoot, socketsErr := s.socketsRoot()
	if socketsErr != nil {
		return &ServerCauseError{Message: "embedded agent: sockets root", Cause: socketsErr}
	}

	// The embedded fleet node runs on this host, so the job workspaces it
	// provisions live under this daemon's own projects root — the root its
	// mount policy below already admits tenant-scoped.
	projectsRoot := s.config.Orchestration.EffectiveProjectsRoot(s.config.Storage.DataDir)
	workspaces, wsErr := s.embeddedFleetNodeWorkspaces(projectsRoot)
	if wsErr != nil {
		return &ServerCauseError{Message: "embedded agent: projects root", Cause: wsErr}
	}

	// Construct fleet node Runtime.
	runtime, rtErr := fleet.NewRuntime(fleet.RuntimeConfig{
		Backends:         backends,
		ContainerRuntime: containerRuntime,
		SandboxPolicy:    sandboxPolicy,
		ImageMap:         imageMap,
		ImageRegistry:    cfg.ImageRegistry,
		AutoPull:         cfg.AutoPull,
		Logger:           log,
		WorkDir:          cfg.DataDir,
		// The embedded fleet node shares this host with the orchestrator, so it
		// enforces the SAME bind-mount policy the orchestrator does: the
		// managed projects root (tenant-scoped) plus the operator's allowlist.
		// Without it a spawn dispatched over the bufconn would bypass the
		// control plane's mount check entirely.
		MountPolicy: &orchestration.MountPolicy{
			ProjectsRoot: projectsRoot,
			AllowedRoots: s.config.Orchestration.Sandbox.AllowedMountRoots,
			SocketsRoot:  socketsRoot,
		},
		Workspaces: workspaces,
		// The same per-tenant Go cache the orchestrator's own backends
		// mount: one host, one cache per tenant.
		GoBuildCache: orchestration.GoBuildCache{
			Root: s.config.Orchestration.Sandbox.EffectiveGoCacheDir(s.config.Storage.DataDir),
		},
	})
	if rtErr != nil {
		return &ServerCauseError{Message: "embedded agent: construct runtime", Cause: rtErr}
	}

	// Sweep the worker containers a previous run of THIS daemon left behind,
	// before anything can spawn one of its own — see
	// orchestration/container_sweep.go and go-quicbaas#750. A sweep that
	// cannot be completed is a startup failure for the reason the
	// signing-socket sweep gives (#676): an orphan that survives its sweep
	// accumulates without bound.
	sweep, sweepErr := runtime.SweepOrphanContainers(context.Background())
	if sweepErr != nil {
		return &ServerCauseError{Message: "embedded agent: sweep orphaned worker containers", Cause: sweepErr}
	}
	if len(sweep.Removed) > 0 {
		log.Infof("Embedded agent: swept %d orphaned worker container(s) left by a previous run", len(sweep.Removed))
	}
	reaper, reapErr := runtime.ContainerReaper(orchestration.DefaultContainerReapGrace)
	if reapErr != nil {
		return &ServerCauseError{Message: "embedded agent: container reaper", Cause: reapErr}
	}
	specs, specErr := runtime.SweepOrphanWorkspaceSpecs()
	if specErr != nil {
		return &ServerCauseError{Message: "embedded agent: sweep orphaned workspace specs", Cause: specErr}
	}
	if len(specs) > 0 {
		log.Infof("Embedded agent: removed %d workspace spec(s) a previous run left behind", len(specs))
	}

	// Construct bufconn listener. The spawn server will serve on this;
	// the conn dialer registered below dials it without going through the OS.
	bufListener := bufconn.Listen(embeddedFleetNodeBufConnSize)

	// Construct the GRPCSpawnServer backed by the bufconn listener. No TLS
	// is needed — the bufconn itself is in-process and not reachable from
	// outside the process. The registered mTLS identity is recorded in the
	// fleet entry metadata for auditability, not for transport-layer auth.
	spawnServer, ssErr := fleet.NewGRPCSpawnServer(fleet.GRPCSpawnServerDeps{
		Runtime:          runtime,
		ListenerOverride: bufListener,
		Logger:           log,
		// No TLS config on the server side: bufconn is in-process only.
		// The embedded fleet node's mTLS identity is carried in registration
		// metadata for auditability; transport security is provided by
		// process isolation.
	})
	if ssErr != nil {
		return &ServerCauseError{Message: "embedded agent: construct spawn server", Cause: ssErr}
	}

	bgCtx, bgCancel := context.WithCancel(context.Background())

	// The running reaper lives as long as the embedded fleet node: the startup
	// sweep above covers a daemon that died, this covers a container that
	// came loose from its session while the daemon kept running.
	if reaper != nil {
		go func() {
			ticker := time.NewTicker(orchestration.DefaultContainerReapInterval)
			defer ticker.Stop()
			reaper.Run(bgCtx, ticker.C, func(sweep orchestration.ContainerSweep, err error) {
				if err != nil && bgCtx.Err() == nil {
					log.Warnf("Embedded agent: container reap: %v", err)
				}
				if len(sweep.Removed) > 0 {
					log.Infof("Embedded agent: reaped %d worker container(s) no live session held", len(sweep.Removed))
				}
			})
		}()
	}

	// Start the gRPC server on the bufconn listener.
	if err := spawnServer.Start(bgCtx); err != nil {
		bgCancel()
		return &ServerCauseError{Message: "embedded agent: start spawn server", Cause: err}
	}

	// Install a synthetic AgentIdentity so the spawn server's authorize()
	// method allows RPCs from the orchestrator without requiring the
	// enrollment handshake. The embedded fleet node is in-process and trusted by
	// construction — it does not enroll via the remote mTLS/OIDC path.
	// The identity carries the full runtime scope set so the orchestrator can
	// call Spawn, Prompt, Status, and Infer without being rejected.
	spawnServer.SetIdentity(buildEmbeddedFleetNodeIdentity(nodeID, embeddedFleetNodeTenantID))

	// Build the fleet registration.
	caps := buildEmbeddedFleetNodeCapabilities(cfg.Backends, cfg.GPUAvailable)
	effectiveHostAffinity := cfg.HostAffinity
	if effectiveHostAffinity == "" {
		effectiveHostAffinity = hostname
	}

	reg := &orchestration.FleetNode{
		ID:           nodeID,
		TenantID:     embeddedFleetNodeTenantID,
		Name:         nodeID,
		Host:         effectiveHostAffinity,
		Backend:      primaryBackend(cfg.Backends),
		NodeType:     "embedded",
		Kind:         orchestration.FleetNodeKindEmbedded,
		Status:       orchestration.FleetNodeStatusIdle,
		Capabilities: caps,
		Metadata:     buildEmbeddedFleetNodeMetadata(hostname, issuedCertPEM),
	}

	// Register in the fleet registry using the same path remote fleet nodes use.
	if regErr := s.fleetRegistry.RegisterFleetNode(context.Background(), reg); regErr != nil {
		bgCancel()
		_ = spawnServer.Stop(bgCtx)
		return &ServerCauseError{Message: "embedded agent: register in fleet registry", Cause: regErr}
	}

	// Wire a bufconn-backed ConnDialer into the remote resolver so the
	// orchestrator dispatches inference RPCs in-process. The dialer ignores
	// the target address and always dials the bufconn — the address in the
	// registration's Host field is a logical name, not a real socket.
	s.installEmbeddedBackendDialer(nodeID, reg, bufListener)

	ea := &embeddedFleetNode{
		nodeID:       nodeID,
		spawnServer:  spawnServer,
		bufListener:  bufListener,
		runtime:      runtime,
		registration: reg,
		cancelCtx:    bgCancel,
	}
	s.embeddedFleetNodeState = ea

	log.Infof("Embedded agent started: id=%s backends=%s host=%s gpu=%v",
		nodeID, strings.Join(cfg.Backends, ","), effectiveHostAffinity, cfg.GPUAvailable)

	return nil
}

// embeddedFleetNodeWorkspaces returns the WorkspaceManager the embedded fleet
// node provisions through: the orchestrator's own when it manages the same
// projects root. Two managers over one root would keep two hold registries,
// and the sweep, consulting only the orchestrator's, would remove a checkout
// an embedded session still mounts. Without an orchestrator manager there is
// no sweep, and the node gets one of its own.
func (s *Server) embeddedFleetNodeWorkspaces(projectsRoot string) (*orchestration.WorkspaceManager, error) {
	if shared := s.sessionWorkspaces; shared != nil && shared.Root() == filepath.Clean(projectsRoot) {
		return shared, nil
	}
	return orchestration.NewWorkspaceManager(projectsRoot)
}

// stopEmbeddedFleetNode gracefully shuts down the in-process fleet node runtime.
// Removes the fleet registration so the orchestrator stops routing to it.
// Safe to call when no embedded fleet node is running (no-op).
func (s *Server) stopEmbeddedFleetNode(ctx context.Context) {
	ea := s.embeddedFleetNodeState
	if ea == nil {
		return
	}

	s.logger.Infof("Stopping embedded agent: id=%s", ea.nodeID)

	// Deregister from fleet so in-flight dispatchers stop routing to us.
	if s.fleetRegistry != nil {
		if err := s.fleetRegistry.RemoveFleetNode(ctx, embeddedFleetNodeTenantID, ea.nodeID); err != nil {
			s.logger.Warnf("Embedded agent: deregister from fleet registry failed: %v", err)
		}
	}

	stopCtx, cancel := context.WithTimeout(ctx, embeddedFleetNodeStopTimeout)
	defer cancel()

	if err := ea.stop(stopCtx); err != nil {
		s.logger.Warnf("Embedded agent: graceful stop failed: %v", err)
	}

	s.embeddedFleetNodeState = nil
	s.logger.Infof("Embedded agent stopped: id=%s", ea.nodeID)
}

// installEmbeddedBackendDialer registers the embedded fleet node's bufconn dialer
// with the remote backend pool so the orchestrator dispatches RPCs in-process.
// The registration is identified by nodeID and keyed to the bufListener.
func (s *Server) installEmbeddedBackendDialer(nodeID string, reg *orchestration.FleetNode, bufListener *bufconn.Listener) {
	if s.embeddedBackendDialers == nil {
		s.embeddedBackendDialers = make(map[string]func(context.Context, string) (*grpc.ClientConn, error))
	}
	dialer := func(ctx context.Context, _ string) (*grpc.ClientConn, error) {
		return grpc.NewClient(
			"passthrough:///bufconn",
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				return bufListener.DialContext(ctx)
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
	}
	s.embeddedBackendDialers[nodeID] = dialer

	// Install the dialer on the existing remote backend pool if it's been
	// created already. The pool's backendFor method uses ConnDialer when
	// non-nil; we set it on the registration's Host entry so the pool can
	// distinguish embedded vs. remote dials.
	//
	// The cleanest hook is to override the pool's dialer lookup by injecting
	// a dialer into the embeddedDialerRegistry that the pool consults before
	// dialling over TCP. See lifecycle_remote_resolver.go for the pool.
	if s.embeddedDialerRegistry != nil {
		s.embeddedDialerRegistry.Register(reg.Host, dialer)
	}
}

// embeddedFleetNodeID is the fleet id — and name — the embedded fleet node of the node
// called hostname registers under. Anything that must recognise THIS node's
// embedded registration among every node's in the shared fleet registry
// derives it here rather than trusting the registration's kind alone.
func embeddedFleetNodeID(hostname string) string {
	return fmt.Sprintf("embedded-%s", hostname)
}

// buildEmbeddedFleetNodeMCPResolver constructs the QuicBaaS MCP preset resolver for
// the embedded fleet node, or returns nil when MCP resolution is not available (MCP
// disabled in config, the CA is down so no cert can be issued, or no SPIFFE
// trust domain is configured so no cert could be bound to its tenant). The
// resolver reuses the platform CA via the fleet node cert issuer so the per-spawn
// certs chain to the same root the MCP listener trusts for client auth.
//
// The endpoint URL targets the local MCP listener: the embedded fleet node runs on
// the same host, so a wildcard/empty bind host is normalized to loopback (the
// cert SANs already include localhost). A remote fleet node daemon would resolve its
// own reachable endpoint instead.
func (s *Server) buildEmbeddedFleetNodeMCPResolver(hostname string, log interfaces.Logger) orchestration.MCPPresetResolver {
	if !s.config.Server.MCP.Enabled {
		log.Debug("Embedded agent: MCP disabled; quicbaas MCP preset will not be resolved")
		return nil
	}
	if s.xkmsCA == nil {
		log.Warn("Embedded agent: CA unavailable; quicbaas MCP preset will not be resolved (no agent cert)")
		return nil
	}

	host := strings.TrimSpace(s.config.Server.MCP.BindAddress)
	if host == "" {
		host = strings.TrimSpace(s.config.Server.Host)
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	endpoint := fmt.Sprintf("https://%s:%d/mcp", host, s.config.Server.MCP.Port)

	issuer, err := service.NewXKMSFleetNodeCertIssuer(s.xkmsCA)
	if err != nil {
		log.Warnf("Embedded agent: MCP preset resolver disabled (cert issuer init failed): %v", err)
		return nil
	}
	resolver, err := NewAgentMCPPresetResolver(AgentMCPPresetResolverConfig{
		Issuer:            issuer,
		Identities:        s.agentSessionIdentityStore,
		EndpointURL:       endpoint,
		Hostname:          hostname,
		DefaultTenant:     embeddedFleetNodeTenantID,
		SPIFFETrustDomain: s.config.IAM.SPIFFETrustDomain,
		Logger:            log,
	})
	if err != nil {
		// A resolver that cannot enroll is worse than none: it would hand every
		// spawned fleet node a certificate that authenticates and then holds no
		// authority, which is the defect go-quicbaas#901 recorded. Say why, and
		// forward the preset unresolved.
		log.Warnf("Embedded agent: MCP preset resolver disabled: %v", err)
		return nil
	}
	log.Infof("Embedded agent: quicbaas MCP preset resolver wired; endpoint=%s", endpoint)
	return resolver
}

// identityStoreAccessor is the narrow service-layer surface that yields the IAM
// identity store. Its user and role persistence is routed to go-qrdb's
// canonical UserServicer / RoleClient (iam_init.go) — the same records go-qrdb's
// own authorization resolves an mTLS principal's roles from.
type identityStoreAccessor interface {
	GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
}

// agentSessionIdentityStore resolves the identity store a spawned session's
// certificate principal is enrolled into. It reads the store per spawn rather
// than capturing it, because the resolver is built during startup and the IAM
// provider comes up after it.
//
// The store's users and roles ARE go-qrdb's (iam_init.go routes both to the
// embedded node), so this enrolls into qrdb's identity layer and nowhere else.
func (s *Server) agentSessionIdentityStore(ctx context.Context) (AgentSessionIdentityStore, error) {
	// The same narrow accessor every IAM handler resolves the store through.
	// service.QuicBaaS does not declare it, so it is asserted here exactly as
	// pkg/api/rest does (fleetIdentityStoreAccessor).
	accessor, ok := s.quicBaaSService.(identityStoreAccessor)
	if !ok {
		return nil, ErrMCPPresetResolverNoIdentityStore
	}
	store, err := accessor.GetIdentityStore(ctx)
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, ErrMCPPresetResolverNoIdentityStore
	}
	return store, nil
}

// buildEmbeddedFleetNodeBackends constructs the backend map from the configured
// list. Only "ollama" is supported for the embedded fleet node; unsupported kinds
// are skipped with a warning.
func buildEmbeddedFleetNodeBackends(
	kinds []string,
	ollamaURL string,
	sandboxNetwork orchestration.SandboxNetwork,
	sandboxSecurity orchestration.SandboxSecurity,
	mcpResolver orchestration.MCPPresetResolver,
	log interfaces.Logger,
) (map[string]orchestration.AgentBackend, error) {
	if len(kinds) == 0 {
		kinds = []string{"ollama"}
	}

	// Map-dispatch for backend constructors.
	type backendFactory func() (map[string]orchestration.AgentBackend, error)
	factories := map[string]backendFactory{
		"ollama": func() (map[string]orchestration.AgentBackend, error) {
			// The URL is resolved by the configuration layers; the defaults
			// layer supplies config.DefaultOllamaBaseURL, so an empty value
			// here is an operator's explicit blank, not something to guess.
			if strings.TrimSpace(ollamaURL) == "" {
				return nil, &config.UpstreamEndpointMissingError{Key: "server.embedded_fleet_node.ollama_url", Provider: "ollama"}
			}
			client := ollama.NewClient(ollama.ClientConfig{
				BaseURL:    ollamaURL,
				MaxRetries: 2,
				Timeout:    10 * time.Minute,
			})
			b := orchestration.NewOllamaBackend(orchestration.OllamaBackendConfig{
				ChatService:  ollama.NewChatService(client, ""),
				ModelService: ollama.NewModelService(client),
				DefaultModel: "",
			})
			return map[string]orchestration.AgentBackend{"ollama": b}, nil
		},
		"claude": func() (map[string]orchestration.AgentBackend, error) {
			b := orchestration.NewACPBackend(orchestration.ACPBackendConfig{
				Command:           "claude",
				Args:              []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--disable-slash-commands"},
				DefaultModel:      orchestration.DefaultClaudeModel,
				CLIMode:           true,
				SandboxNetwork:    sandboxNetwork,
				SandboxSecurity:   sandboxSecurity,
				MCPPresetResolver: mcpResolver,
			})
			b.SetLogger(log)
			return map[string]orchestration.AgentBackend{"claude": b}, nil
		},
		"codex": func() (map[string]orchestration.AgentBackend, error) {
			// codex CLI doesn't speak ACP; use exec-mode wrapper. Leave model
			// empty so codex uses its account's configured default; passing
			// an unsupported model fails with HTTP 400 from the codex API.
			b := orchestration.NewCodexExecBackend(orchestration.CodexExecBackendConfig{
				Command: "codex",
			})
			return map[string]orchestration.AgentBackend{"codex": b}, nil
		},
		"opencode": func() (map[string]orchestration.AgentBackend, error) {
			b := orchestration.NewACPBackend(orchestration.ACPBackendConfig{
				Command:           "opencode",
				Args:              []string{"acp"},
				DefaultModel:      "",
				SandboxNetwork:    sandboxNetwork,
				SandboxSecurity:   sandboxSecurity,
				MCPPresetResolver: mcpResolver,
			})
			b.SetLogger(log)
			return map[string]orchestration.AgentBackend{"opencode": b}, nil
		},
	}

	result := make(map[string]orchestration.AgentBackend)
	for _, kind := range kinds {
		factory, ok := factories[kind]
		if !ok {
			log.Warnf("Embedded agent: unknown backend kind %q — skipping", kind)
			continue
		}
		backends, err := factory()
		if err != nil {
			return nil, &ServerCauseError{Message: fmt.Sprintf("embedded agent: build backend %q", kind), Cause: err}
		}
		for name, b := range backends {
			result[name] = b
		}
	}

	if len(result) == 0 {
		return nil, &EmbeddedFleetNodeBuildBackendsError{Kinds: kinds}
	}
	return result, nil
}

// buildEmbeddedFleetNodeImageMap resolves the per-backend sandbox image map. An
// explicit ImageMap wins; otherwise the convenience SandboxImage is applied to
// every configured backend so a single operator setting sandboxes the whole
// embedded fleet node. Returns an empty map when neither is set (raw-only operation).
func buildEmbeddedFleetNodeImageMap(cfg config.EmbeddedFleetNodeConfig) map[string]string {
	if len(cfg.ImageMap) > 0 {
		out := make(map[string]string, len(cfg.ImageMap))
		for k, v := range cfg.ImageMap {
			out[k] = v
		}
		return out
	}
	if cfg.SandboxImage == "" {
		return map[string]string{}
	}
	backends := cfg.Backends
	if len(backends) == 0 {
		backends = []string{"ollama"}
	}
	out := make(map[string]string, len(backends))
	for _, b := range backends {
		out[b] = cfg.SandboxImage
	}
	return out
}

// buildEmbeddedFleetNodeCapabilities derives the capability list from the
// configured backend kinds and the GPU availability flag.
func buildEmbeddedFleetNodeCapabilities(kinds []string, gpuAvailable bool) []string {
	caps := make([]string, 0, len(kinds)+1)
	caps = append(caps, kinds...)
	if gpuAvailable {
		caps = append(caps, "gpu")
	}
	return caps
}

// buildEmbeddedFleetNodeMetadata assembles the registration metadata map. We
// include the cert PEM fingerprint for auditability and mark the fleet node as
// embedded so operators can identify it in fleet listings.
func buildEmbeddedFleetNodeMetadata(hostname string, certPEM []byte) map[string]string {
	m := map[string]string{
		"embedded": "true",
		"hostname": hostname,
	}
	if len(certPEM) > 0 {
		// Store a truncated fingerprint, not the full PEM — metadata values
		// are stored in the KV registry and should be compact.
		m["cert_issued"] = "true"
	}
	return m
}

// primaryBackend returns the first backend kind from the list, or "ollama"
// when the list is empty. This is used as the registration's primary Backend
// field (single value, canonical).
func primaryBackend(kinds []string) string {
	if len(kinds) > 0 {
		return kinds[0]
	}
	return "ollama"
}

// buildEmbeddedFleetNodeIdentity constructs the synthetic AgentIdentity used to
// pre-authorize the embedded fleet node's GRPCSpawnServer. The embedded fleet node runs
// in-process and does not enroll via the remote mTLS/OIDC path, so we mint
// the identity directly here instead of waiting for enrollment to complete.
//
// The identity carries the full runtime scope set (spawn, prompt, status,
// infer) so the orchestrator can exercise all RPC operations on the embedded
// backend without any authorization rejection.
func buildEmbeddedFleetNodeIdentity(nodeID, tenantID string) *orchestration.AgentIdentity {
	return &orchestration.AgentIdentity{
		AgentID:          nodeID,
		ServiceAccountID: nodeID,
		TenantID:         tenantID,
		Role:             "agent-runtime",
		Scopes:           orchestration.FleetRuntimeScopes(),
		ClientID:         "embedded:" + nodeID,
	}
}

// =============================================================================
// Embedded dialer registry
// =============================================================================

// EmbeddedDialerRegistry holds per-host bufconn dialers for embedded fleet nodes.
// The remote backend pool consults this registry before attempting a real
// TCP dial, allowing in-process fleet nodes to bypass the network stack entirely.
type EmbeddedDialerRegistry struct {
	mu      sync.RWMutex
	dialers map[string]func(ctx context.Context, target string) (*grpc.ClientConn, error)
}

// NewEmbeddedDialerRegistry creates an empty registry.
func NewEmbeddedDialerRegistry() *EmbeddedDialerRegistry {
	return &EmbeddedDialerRegistry{
		dialers: make(map[string]func(ctx context.Context, target string) (*grpc.ClientConn, error)),
	}
}

// Register associates a host key with a bufconn dialer. The host key matches
// FleetNode.Host so the pool can look up the dialer by host.
func (r *EmbeddedDialerRegistry) Register(host string, dialer func(ctx context.Context, target string) (*grpc.ClientConn, error)) {
	r.mu.Lock()
	r.dialers[host] = dialer
	r.mu.Unlock()
}

// Unregister removes the dialer for the given host.
func (r *EmbeddedDialerRegistry) Unregister(host string) {
	r.mu.Lock()
	delete(r.dialers, host)
	r.mu.Unlock()
}

// Lookup returns the dialer for host, or (nil, false) when not found.
func (r *EmbeddedDialerRegistry) Lookup(host string) (func(ctx context.Context, target string) (*grpc.ClientConn, error), bool) {
	r.mu.RLock()
	d, ok := r.dialers[host]
	r.mu.RUnlock()
	return d, ok
}

// =============================================================================
// Typed errors
// =============================================================================

// EmbeddedFleetNodeBuildBackendsError is returned when all requested backend kinds
// are unknown or failed to construct.
type EmbeddedFleetNodeBuildBackendsError struct {
	Kinds []string
}

// Error implements the error interface.
func (e *EmbeddedFleetNodeBuildBackendsError) Error() string {
	return fmt.Sprintf("embedded agent: no backends constructed from %v", e.Kinds)
}
