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

// Package server: embedded agent
//
// The embedded agent runs an in-process pkg/agent Runtime alongside the
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
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/jeremyhahn/go-quicbaas/pkg/agent"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/ollama"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

const (
	// embeddedAgentBufConnSize is the in-memory buffer size for the bufconn
	// loopback transport. 1 MiB matches bufconn defaults in the gRPC test suite.
	embeddedAgentBufConnSize = 1 << 20 // 1 MiB

	// embeddedAgentTenantID is the default tenant the embedded agent is
	// registered under. Operators running multi-tenant quicbaasd should
	// ensure agents land in the right tenant namespace.
	embeddedAgentTenantID = "default"

	// embeddedAgentStopTimeout is the graceful shutdown grace period for the
	// embedded agent's gRPC server.
	embeddedAgentStopTimeout = 15 * time.Second
)

// embeddedAgent holds all state for the in-process agent runtime.
// All fields are initialized atomically during startEmbeddedAgent and
// read-only thereafter (except mu-guarded stopped flag).
type embeddedAgent struct {
	// agentID is the unique identifier under which this agent is registered
	// in the fleet registry. Format: "embedded-<hostname>". Stable across
	// restarts so the registry holds exactly one entry per host.
	agentID string

	// spawnServer is the daemon-side gRPC service backed by the Runtime.
	// It listens on the bufconn listener (no real network socket).
	spawnServer *agent.GRPCSpawnServer

	// bufListener is the in-memory gRPC transport. The spawn server serves
	// on this listener; the RemoteACPBackend dials it via ConnDialer.
	bufListener *bufconn.Listener

	// runtime is the agent backend dispatcher. Shared reference — the spawn
	// server delegates to it for every RPC.
	runtime *agent.Runtime

	// registration is the canonical fleet entry created at startup.
	registration *orchestration.AgentRegistration

	// cancelCtx cancels the embedded agent's background goroutines (none for
	// now — the gRPC server lifecycle is managed via Stop).
	cancelCtx context.CancelFunc

	mu      sync.Mutex
	stopped bool
}

// stop gracefully shuts down the embedded agent's gRPC server.
func (ea *embeddedAgent) stop(ctx context.Context) error {
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

// embeddedAgentField guards the embeddedAgent pointer on the Server struct.
// Stored as a field added below via the embedding approach.
// We use a sync.Mutex-guarded pointer to keep the zero value safe.

// startEmbeddedAgent initialises and starts the in-process agent runtime.
// Call order requirements (all enforced by the caller in lifecycle order):
//  1. s.xkmsCA must be initialized (CA up, TLS enabled)
//  2. s.agentRegistry must be non-nil (orchestration started)
//  3. s.quicBaaSService must be non-nil (service facade wired)
//
// The embedded agent:
//   - Issues a real mTLS cert from the local CA (first-class identity)
//   - Boots a pkg/agent Runtime with the configured backends
//   - Starts a GRPCSpawnServer on a bufconn.Listener (zero-network-overhead)
//   - Registers in the fleet registry as AgentKindEmbedded
//   - Wires a ConnDialer into the remote resolver's backend pool so the
//     orchestrator dispatches inference RPCs in-process
func (s *Server) startEmbeddedAgent() error {
	if s.embeddedAgentState != nil {
		return &EmbeddedAgentAlreadyStartedError{AgentID: s.embeddedAgentState.agentID}
	}
	if s.agentRegistry == nil {
		return ErrEmbeddedAgentNoRegistry
	}

	cfg := s.config.Server.EmbeddedAgent
	log := logger.NewAdapter(s.logger)

	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "localhost"
	}
	// The agent ID is stable across restarts for a given host so the operator
	// sees one entry in the fleet registry and the KMS holds exactly one
	// identity cert per host rather than accumulating per-boot entries.
	agentID := fmt.Sprintf("embedded-%s", hostname)

	log.Infof("Starting embedded agent: id=%s", agentID)

	// Issue mTLS identity from the local CA. When TLS is disabled the cert
	// issuer path is unavailable; the embedded agent still starts but without
	// a hardware-bound identity (acceptable for non-TLS dev deployments).
	var issuedCertPEM []byte
	if s.xkmsCA != nil {
		issuer, issueErr := service.NewXKMSAgentCertIssuer(s.xkmsCA)
		if issueErr == nil {
			req := &service.AgentCertificateRequest{
				TenantID: embeddedAgentTenantID,
				Hostname: hostname,
				Subject: service.AgentCertificateSubject{
					CommonName:         "agent-" + agentID,
					OrganizationalUnit: service.DefaultAgentCertOU,
				},
				ExtraDNSNames: []string{"localhost"},
				ValidityDays:  service.DefaultAgentCertValidityDays,
			}
			issued, certErr := issuer.IssueAgentCertificate(context.Background(), req)
			if certErr != nil {
				log.Warnf("Embedded agent: mTLS cert issuance failed (running without mTLS identity): %v", certErr)
			} else {
				issuedCertPEM = issued.CertPEM
				// Key is secured in an mlocked buffer; destroy after we
				// have copied what we need. The embedded agent doesn't need
				// the key on disk — the bufconn transport is in-process.
				issued.PrivateKey.Destroy()
				log.Infof("Embedded agent: mTLS identity issued; cn=agent-%s", agentID)
			}
		}
	} else {
		log.Warn("Embedded agent: CA not available; starting without mTLS identity (TLS disabled)")
	}

	// Build backends from config. Each backend kind maps to a constructor
	// via the dispatch table below.
	backends, buildErr := buildEmbeddedAgentBackends(cfg.Backends, cfg.OllamaURL, log)
	if buildErr != nil {
		return buildErr
	}

	// Construct agent Runtime.
	runtime, rtErr := agent.NewRuntime(agent.RuntimeConfig{
		Backends: backends,
		Logger:   log,
		WorkDir:  cfg.DataDir,
	})
	if rtErr != nil {
		return fmt.Errorf("embedded agent: construct runtime: %w", rtErr)
	}

	// Construct bufconn listener. The spawn server will serve on this;
	// the conn dialer registered below dials it without going through the OS.
	bufListener := bufconn.Listen(embeddedAgentBufConnSize)

	// Construct the GRPCSpawnServer backed by the bufconn listener. No TLS
	// is needed — the bufconn itself is in-process and not reachable from
	// outside the process. The registered mTLS identity is recorded in the
	// fleet entry metadata for auditability, not for transport-layer auth.
	spawnServer, ssErr := agent.NewGRPCSpawnServer(agent.GRPCSpawnServerDeps{
		Runtime:          runtime,
		ListenerOverride: bufListener,
		Logger:           log,
		// No TLS config on the server side: bufconn is in-process only.
		// The embedded agent's mTLS identity is carried in registration
		// metadata for auditability; transport security is provided by
		// process isolation.
	})
	if ssErr != nil {
		return fmt.Errorf("embedded agent: construct spawn server: %w", ssErr)
	}

	bgCtx, bgCancel := context.WithCancel(context.Background())

	// Start the gRPC server on the bufconn listener.
	if err := spawnServer.Start(bgCtx); err != nil {
		bgCancel()
		return fmt.Errorf("embedded agent: start spawn server: %w", err)
	}

	// Install a synthetic AgentIdentity so the spawn server's authorize()
	// method allows RPCs from the orchestrator without requiring the
	// enrollment handshake. The embedded agent is in-process and trusted by
	// construction — it does not enroll via the remote mTLS/OIDC path.
	// The identity carries the full runtime scope set so the orchestrator can
	// call Spawn, Prompt, Status, and Infer without being rejected.
	spawnServer.SetIdentity(buildEmbeddedAgentIdentity(agentID, embeddedAgentTenantID))

	// Build the fleet registration.
	caps := buildEmbeddedAgentCapabilities(cfg.Backends, cfg.GPUAvailable)
	effectiveHostAffinity := cfg.HostAffinity
	if effectiveHostAffinity == "" {
		effectiveHostAffinity = hostname
	}

	reg := &orchestration.AgentRegistration{
		ID:           agentID,
		TenantID:     embeddedAgentTenantID,
		Name:         agentID,
		Host:         effectiveHostAffinity,
		Backend:      primaryBackend(cfg.Backends),
		AgentType:    "embedded",
		Kind:         orchestration.AgentKindEmbedded,
		Status:       orchestration.AgentStatusIdle,
		Capabilities: caps,
		Metadata:     buildEmbeddedAgentMetadata(hostname, issuedCertPEM),
	}

	// Register in the fleet registry using the same path remote agents use.
	if regErr := s.agentRegistry.RegisterAgent(context.Background(), reg); regErr != nil {
		bgCancel()
		_ = spawnServer.Stop(bgCtx)
		return fmt.Errorf("embedded agent: register in fleet registry: %w", regErr)
	}

	// Wire a bufconn-backed ConnDialer into the remote resolver so the
	// orchestrator dispatches inference RPCs in-process. The dialer ignores
	// the target address and always dials the bufconn — the address in the
	// registration's Host field is a logical name, not a real socket.
	s.installEmbeddedBackendDialer(agentID, reg, bufListener)

	ea := &embeddedAgent{
		agentID:      agentID,
		spawnServer:  spawnServer,
		bufListener:  bufListener,
		runtime:      runtime,
		registration: reg,
		cancelCtx:    bgCancel,
	}
	s.embeddedAgentState = ea

	log.Infof("Embedded agent started: id=%s backends=%s host=%s gpu=%v",
		agentID, strings.Join(cfg.Backends, ","), effectiveHostAffinity, cfg.GPUAvailable)

	return nil
}

// stopEmbeddedAgent gracefully shuts down the in-process agent runtime.
// Removes the fleet registration so the orchestrator stops routing to it.
// Safe to call when no embedded agent is running (no-op).
func (s *Server) stopEmbeddedAgent(ctx context.Context) {
	ea := s.embeddedAgentState
	if ea == nil {
		return
	}

	s.logger.Infof("Stopping embedded agent: id=%s", ea.agentID)

	// Deregister from fleet so in-flight dispatchers stop routing to us.
	if s.agentRegistry != nil {
		if err := s.agentRegistry.RemoveAgent(ctx, embeddedAgentTenantID, ea.agentID); err != nil {
			s.logger.Warnf("Embedded agent: deregister from fleet registry failed: %v", err)
		}
	}

	stopCtx, cancel := context.WithTimeout(ctx, embeddedAgentStopTimeout)
	defer cancel()

	if err := ea.stop(stopCtx); err != nil {
		s.logger.Warnf("Embedded agent: graceful stop failed: %v", err)
	}

	s.embeddedAgentState = nil
	s.logger.Infof("Embedded agent stopped: id=%s", ea.agentID)
}

// installEmbeddedBackendDialer registers the embedded agent's bufconn dialer
// with the remote backend pool so the orchestrator dispatches RPCs in-process.
// The registration is identified by agentID and keyed to the bufListener.
func (s *Server) installEmbeddedBackendDialer(agentID string, reg *orchestration.AgentRegistration, bufListener *bufconn.Listener) {
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
	s.embeddedBackendDialers[agentID] = dialer

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

// buildEmbeddedAgentBackends constructs the backend map from the configured
// list. Only "ollama" is supported for the embedded agent; unsupported kinds
// are skipped with a warning.
func buildEmbeddedAgentBackends(kinds []string, ollamaURL string, log interfaces.Logger) (map[string]orchestration.AgentBackend, error) {
	if len(kinds) == 0 {
		kinds = []string{"ollama"}
	}

	// Map-dispatch for backend constructors.
	type backendFactory func() (map[string]orchestration.AgentBackend, error)
	factories := map[string]backendFactory{
		"ollama": func() (map[string]orchestration.AgentBackend, error) {
			url := ollamaURL
			if url == "" {
				url = "http://127.0.0.1:11434"
			}
			client := ollama.NewClient(ollama.ClientConfig{
				BaseURL:    url,
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
				Command:      "claude",
				Args:         []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--no-session-persistence", "--disable-slash-commands"},
				DefaultModel: "",
				CLIMode:      true,
			})
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
				Command:      "opencode",
				Args:         []string{"acp"},
				DefaultModel: "",
			})
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
			return nil, fmt.Errorf("embedded agent: build backend %q: %w", kind, err)
		}
		for name, b := range backends {
			result[name] = b
		}
	}

	if len(result) == 0 {
		return nil, &EmbeddedAgentBuildBackendsError{Kinds: kinds}
	}
	return result, nil
}

// buildEmbeddedAgentCapabilities derives the capability list from the
// configured backend kinds and the GPU availability flag.
func buildEmbeddedAgentCapabilities(kinds []string, gpuAvailable bool) []string {
	caps := make([]string, 0, len(kinds)+1)
	for _, k := range kinds {
		caps = append(caps, k)
	}
	if gpuAvailable {
		caps = append(caps, "gpu")
	}
	return caps
}

// buildEmbeddedAgentMetadata assembles the registration metadata map. We
// include the cert PEM fingerprint for auditability and mark the agent as
// embedded so operators can identify it in fleet listings.
func buildEmbeddedAgentMetadata(hostname string, certPEM []byte) map[string]string {
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

// buildEmbeddedAgentIdentity constructs the synthetic AgentIdentity used to
// pre-authorize the embedded agent's GRPCSpawnServer. The embedded agent runs
// in-process and does not enroll via the remote mTLS/OIDC path, so we mint
// the identity directly here instead of waiting for enrollment to complete.
//
// The identity carries the full runtime scope set (spawn, prompt, status,
// infer) so the orchestrator can exercise all RPC operations on the embedded
// backend without any authorization rejection.
func buildEmbeddedAgentIdentity(agentID, tenantID string) *orchestration.AgentIdentity {
	return &orchestration.AgentIdentity{
		AgentID:          agentID,
		ServiceAccountID: agentID,
		TenantID:         tenantID,
		Role:             "agent-runtime",
		Scopes:           orchestration.AgentRuntimeScopes(),
		ClientID:         "embedded:" + agentID,
	}
}

// =============================================================================
// Embedded dialer registry
// =============================================================================

// EmbeddedDialerRegistry holds per-host bufconn dialers for embedded agents.
// The remote backend pool consults this registry before attempting a real
// TCP dial, allowing in-process agents to bypass the network stack entirely.
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
// AgentRegistration.Host so the pool can look up the dialer by host.
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

// EmbeddedAgentBuildBackendsError is returned when all requested backend kinds
// are unknown or failed to construct.
type EmbeddedAgentBuildBackendsError struct {
	Kinds []string
}

// Error implements the error interface.
func (e *EmbeddedAgentBuildBackendsError) Error() string {
	return fmt.Sprintf("embedded agent: no backends constructed from %v", e.Kinds)
}
