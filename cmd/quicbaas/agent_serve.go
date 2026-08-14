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

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	oauthpkg "golang.org/x/oauth2"

	"github.com/jeremyhahn/go-quicbaas/pkg/agent"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/ollama"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Agent Serve Command Flags
// =============================================================================
//
// Flag semantics mirror pkg/agent.Config so the CLI and config-file paths
// stay in lockstep. A config-file loader can be wired up later without
// changing this flag surface.

var (
	serveServerAddr     string
	serveTenantID       string
	serveBindAddr       string
	serveBackendsCSV    string
	serveSandboxPolicy  string
	serveSandboxNetwork string
	serveAllowedMounts  []string
	serveContainerRT    string
	serveImageMapCSV    string
	serveImageRegistry  string
	serveAutoPull       bool
	serveWorkDir        string
	serveAgentCertFile  string
	serveAgentKeyFile   string
	serveServerCAFile   string
	serveHeartbeatEvery time.Duration
	serveShutdownGrace  time.Duration
	serveMCPEndpoint    string
)

// newAgentServeCommand builds the `quicbaas agent serve` subcommand.
//
// This command is the long-lived daemon entrypoint on remote agent hosts.
// It registers with the QuicBaaS server over mTLS, exposes an AgentRuntime
// gRPC service, and heartbeats into the fleet registry.
func newAgentServeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the QuicBaaS agent daemon",
		Long: `Run the QuicBaaS agent runtime daemon on this host.

The daemon authenticates to the QuicBaaS server over mTLS using a client
certificate issued by the server's CA, exposes a gRPC AgentRuntime
service on the configured bind address, and heartbeats into the fleet
registry.

Examples:
  quicbaas agent serve \
    --server https://198.51.100.51:8443 \
    --tenant acme \
    --bind 127.0.0.1:9091 \
    --backends acp,ollama \
    --sandbox-policy required \
    --agent-cert /var/lib/quicbaas-agent/agent.crt \
    --agent-key /var/lib/quicbaas-agent/agent.key \
    --server-ca /var/lib/quicbaas-agent/server-ca.crt`,
		RunE: runAgentServe,
	}

	cmd.Flags().StringVar(&serveServerAddr, "server", "", "HTTPS endpoint of the QuicBaaS server (required)")
	cmd.Flags().StringVar(&serveTenantID, "tenant", "", "Tenant this agent serves (required)")
	cmd.Flags().StringVar(&serveBindAddr, "bind", "127.0.0.1:9091", "Local bind address for the AgentRuntime gRPC service")
	cmd.Flags().StringVar(&serveBackendsCSV, "backends", "acp,ollama", "Comma-separated backends to enable")
	cmd.Flags().StringVar(&serveSandboxPolicy, "sandbox-policy", "required", "Sandbox policy: required | preferred | opt-in")
	cmd.Flags().StringVar(&serveSandboxNetwork, "sandbox-network", "", "Container network for sandboxed sessions: bridge (default) | none | host | <network name>. \"host\" removes network isolation and exposes this host's loopback services to the agent.")
	cmd.Flags().StringVar(&serveContainerRT, "container-runtime", "auto", "OCI runtime: docker | podman | auto | none")
	cmd.Flags().StringVar(&serveImageMapCSV, "image-map", "", "Comma-separated backend=image mappings (e.g., claude-code=quicbaas/claude-code:v1)")
	cmd.Flags().StringVar(&serveImageRegistry, "image-registry", "", "Registry prefix applied to unqualified image references")
	cmd.Flags().BoolVar(&serveAutoPull, "auto-pull", false, "Pull missing container images at startup")
	cmd.Flags().StringVar(&serveWorkDir, "work-dir", "/var/lib/quicbaas-agent", "Working directory for state, scratch, and session workspaces")
	cmd.Flags().StringVar(&serveAgentCertFile, "agent-cert", "", "Path to the daemon's PEM-encoded mTLS client certificate (required)")
	cmd.Flags().StringVar(&serveAgentKeyFile, "agent-key", "", "Path to the daemon's PEM-encoded mTLS private key (required)")
	cmd.Flags().StringVar(&serveServerCAFile, "server-ca", "", "Path to the PEM-encoded CA bundle the daemon trusts when dialing --server (required)")
	cmd.Flags().DurationVar(&serveHeartbeatEvery, "heartbeat-interval", 30*time.Second, "How often to heartbeat into the fleet registry")
	cmd.Flags().DurationVar(&serveShutdownGrace, "shutdown-grace", 30*time.Second, "How long to wait for in-flight sessions to drain on shutdown")
	cmd.Flags().StringSliceVar(&serveAllowedMounts, "allowed-mount-root", nil, "Absolute host directory a sandboxed spawn may bind-mount from; repeatable. Every requested work_dir and extra mount must resolve under one of these. Omitting the flag denies all host mounts.")
	cmd.Flags().StringVar(&serveMCPEndpoint, "mcp-endpoint", "", "Fully-qualified https://host:port/mcp address spawned agents dial for the \"quicbaas\" MCP preset. Empty leaves the preset unresolved.")

	for _, flag := range []string{"server", "tenant", "agent-cert", "agent-key", "server-ca"} {
		if err := cmd.MarkFlagRequired(flag); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to mark --%s required: %v\n", flag, err)
		}
	}

	return cmd
}

// runAgentServe is the `quicbaas agent serve` entrypoint.
//
// Wiring path:
//  1. Parse flags → build and validate agent.Config.
//  2. Build the backend set the daemon will expose (ACP + Ollama per flags).
//  3. Construct the Runtime over those backends.
//  4. Construct the ProductionEnroller (HTTP → server/oauth2).
//  5. Construct the GRPCSpawnServer, wiring the Runtime in.
//  6. Construct the Daemon with all of the above.
//  7. Start; post-enroll install identity on the spawn server.
//  8. Block on SIGINT/SIGTERM; graceful Stop with configured grace.
func runAgentServe(cmd *cobra.Command, args []string) error {
	cfg, err := buildServeConfig()
	if err != nil {
		return formatError(err)
	}
	if err := cfg.Validate(); err != nil {
		return formatError(err)
	}

	logger := cliLoggerForAgentServe()

	backends, buildErr := buildAgentBackends(cfg, logger)
	if buildErr != nil {
		return formatError(buildErr)
	}

	runtime, rtErr := agent.NewRuntime(agent.RuntimeConfig{
		Backends:      backends,
		SandboxPolicy: cfg.SandboxPolicy,
		ImageMap:      cfg.ImageMap,
		ImageRegistry: cfg.ImageRegistry,
		Logger:        logger,
		WorkDir:       cfg.WorkDir,
		// Bind-mount allowlist. Empty denies every host mount a sandboxed
		// spawn asks for, so a daemon started without --allowed-mount-root
		// cannot be talked into mounting an operator's filesystem.
		MountPolicy: cfg.MountPolicy(),
	})
	if rtErr != nil {
		return formatError(rtErr)
	}

	httpClient, hcErr := buildAgentHTTPClient(cfg)
	if hcErr != nil {
		return formatError(hcErr)
	}
	enroller := agent.NewProductionEnroller(agent.ProductionEnrollerDeps{
		Logger:     logger,
		HTTPClient: httpClient,
	})
	// Bind the validated config so the heartbeat sender can call
	// Reregister() without re-threading cfg through every call site.
	enroller.SetConfig(cfg)

	// Shared token source for all outbound authenticated paths (heartbeat,
	// event forwarding). Caches the JWT from /oauth2/token and refreshes
	// 5s before expiry. The enroller is wired in as the Reregisterer hook
	// so that when the server rejects /oauth2/token with invalid_client
	// (typically because the server's IAM store was wiped and the cached
	// client_secret is dead), the token source mints fresh credentials
	// and retries the exchange — symmetrical to the 404-on-heartbeat
	// re-register path further below.
	tokens := &agent.CachingTokenSource{
		Refresh:      enroller.RefreshTokenFn(cfg),
		Reregisterer: enroller,
		Logger:       logger,
	}

	// Heartbeat sender: keeps the server fleet registry's last-seen
	// timestamp fresh so the orchestrator dispatcher keeps routing work to
	// this daemon. Shares the token source with the event forwarder so
	// one /oauth2/token exchange serves both outbound paths. The
	// enroller doubles as the Reregisterer hook: when the server
	// returns 404 (server-side auto-recovery failed), the sender
	// invokes enroller.Reregister to bootstrap fresh OAuth2 credentials
	// before retrying the heartbeat.
	heartbeat, hbErr := agent.NewHTTPHeartbeatSender(agent.HTTPHeartbeatConfig{
		ServerAddr:   cfg.ServerAddr,
		TenantID:     cfg.TenantID,
		HTTPClient:   httpClient,
		Tokens:       tokens,
		Logger:       logger,
		Reregisterer: enroller,
	})
	if hbErr != nil {
		return formatError(hbErr)
	}

	spawnServer, ssErr := agent.NewGRPCSpawnServer(agent.GRPCSpawnServerDeps{
		Runtime:  runtime,
		BindAddr: cfg.BindAddr,
		Logger:   logger,
	})
	if ssErr != nil {
		return formatError(ssErr)
	}

	daemon, dErr := agent.NewDaemon(cfg, agent.DaemonDeps{
		Runtime:     runtime,
		Enroller:    enroller,
		SpawnServer: spawnServer,
		Heartbeat:   heartbeat,
		Logger:      logger,
	})
	if dErr != nil {
		return formatError(dErr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := daemon.Start(ctx); err != nil {
		return formatError(err)
	}
	if id := daemon.Identity(); id != nil {
		spawnServer.SetIdentity(id)
	}

	// Event forwarder ships session events from the daemon's ACP backends
	// to the server's session event log over gRPC PushEvents. The dial
	// target is whatever the server advertised at registration time
	// (persisted in credentials.json under event_ingest_addr); when the
	// server did not advertise one, event forwarding is disabled and a
	// clear warning is logged so operators can tell forwarding-off from
	// forwarding-failed.
	if err := startEventForwarder(ctx, enroller, cfg, runtime, tokens, logger); err != nil {
		return formatError(err)
	}

	logger.Infof("agent daemon ready; tenant=%s server=%s bind=%s backends=%s",
		cfg.TenantID, cfg.ServerAddr, spawnServer.Addr(),
		strings.Join(runtime.Backends(), ","))

	return waitForSignalAndStop(daemon)
}

// buildAgentBackends constructs the set of backends the daemon will expose,
// honoring cfg.EffectiveBackends(). ACP backends default to the claude / codex
// / opencode CLIs on PATH — operators override via --image-map when running
// sandboxed. Ollama points at the local ollama API on 127.0.0.1:11434.
func buildAgentBackends(cfg *agent.Config, logger interfaces.Logger) (map[string]orchestration.AgentBackend, error) {
	// Resolve the QuicBaaS MCP preset for spawned ACP agents. The resolver
	// points them at the long-lived on-disk mTLS identity the deploy flow
	// uploaded (no per-spawn minting). It is wired only when the control plane
	// supplied an --mcp-endpoint; otherwise it stays nil and the preset is
	// forwarded unmodified (current behavior).
	mcpResolver := buildAgentServeMCPResolver(cfg, logger)

	backends := map[string]orchestration.AgentBackend{}
	for _, kind := range cfg.EffectiveBackends() {
		switch kind {
		case agent.BackendACP:
			sandboxNetwork := cfg.EffectiveSandboxNetwork()
			backends["claude"] = orchestration.NewACPBackend(orchestration.ACPBackendConfig{
				Command:           "claude",
				Args:              []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--disable-slash-commands"},
				DefaultModel:      "",
				CLIMode:           true,
				SandboxNetwork:    sandboxNetwork,
				MCPPresetResolver: mcpResolver,
			})
			backends["codex"] = orchestration.NewACPBackend(orchestration.ACPBackendConfig{
				Command:           "codex",
				Args:              []string{},
				DefaultModel:      "o4-mini",
				SandboxNetwork:    sandboxNetwork,
				MCPPresetResolver: mcpResolver,
			})
			backends["opencode"] = orchestration.NewACPBackend(orchestration.ACPBackendConfig{
				Command:           "opencode",
				Args:              []string{"acp"},
				DefaultModel:      "",
				SandboxNetwork:    sandboxNetwork,
				MCPPresetResolver: mcpResolver,
			})
		case agent.BackendOllama:
			client := ollama.NewClient(ollama.ClientConfig{
				BaseURL:    "http://127.0.0.1:11434",
				MaxRetries: 2,
				Timeout:    10 * time.Minute,
			})
			backends["ollama"] = orchestration.NewOllamaBackend(orchestration.OllamaBackendConfig{
				ChatService:  ollama.NewChatService(client, ""),
				ModelService: ollama.NewModelService(client),
				DefaultModel: "",
			})
		}
	}
	if len(backends) == 0 {
		return nil, &MessageError{Message: fmt.Sprintf("no backends constructed from %v", cfg.EffectiveBackends())}
	}
	logger.Debugf("agent daemon backends constructed: %d", len(backends))
	return backends, nil
}

// buildAgentServeMCPResolver constructs the QuicBaaS MCP preset resolver for a
// remotely deployed agent daemon, or returns nil when no MCP endpoint was
// configured (cfg.MCPEndpoint empty) so the preset stays unresolved and current
// behavior is preserved. The resolver reuses the long-lived agent.crt/agent.key/
// server-ca.crt the deploy flow already uploaded — it mints nothing. A
// construction failure is logged and degrades to nil rather than aborting the
// daemon, matching the embedded agent's graceful behavior.
func buildAgentServeMCPResolver(cfg *agent.Config, logger interfaces.Logger) orchestration.MCPPresetResolver {
	if strings.TrimSpace(cfg.MCPEndpoint) == "" {
		return nil
	}
	resolver, err := agent.NewFileMCPPresetResolver(cfg.MCPEndpoint, cfg.AgentCertFile, cfg.AgentKeyFile, cfg.ServerCAFile)
	if err != nil {
		logger.Warnf("agent daemon: quicbaas MCP preset resolver disabled: %v", err)
		return nil
	}
	logger.Infof("agent daemon: quicbaas MCP preset resolver wired; endpoint=%s", cfg.MCPEndpoint)
	return resolver
}

// cliLoggerForAgentServe returns a minimal interfaces.Logger writing to the
// process's stderr. It mirrors the daemon-scale logger used by the server so
// operators see consistent formatting in journald / container logs.
func cliLoggerForAgentServe() interfaces.Logger {
	return newAgentServeLogger()
}

// startEventForwarder constructs the EventForwarder and installs it on
// the runtime once enrollment has completed. The dial target is read
// from credentials.json (set by the server in the register response
// under event_ingest_addr). When the server did not advertise an
// ingest address — older daemon, REST-only deployment — the function
// logs a clear warning and returns nil so the daemon proceeds without
// event forwarding rather than dialing the wrong port and emitting
// an endless stream of ALPN handshake errors.
func startEventForwarder(
	ctx context.Context,
	enroller *agent.ProductionEnroller,
	cfg *agent.Config,
	runtime *agent.Runtime,
	tokens agent.TokenSource,
	logger interfaces.Logger,
) error {
	ingestURL, err := enroller.EventIngestAddrFromDisk(cfg)
	if err != nil {
		return err
	}
	addr := stripURLScheme(ingestURL)
	if addr == "" {
		logger.Warnf("daemon did not advertise event ingest address; event forwarding disabled")
		return nil
	}
	forwarder, fwErr := agent.NewEventForwarder(agent.EventForwarderConfig{
		ServerAddr:  addr,
		TokenSource: &oauth2TokenAdapter{src: tokens},
		TLSConfig:   buildAgentTLSConfig(cfg),
		Logger:      logger,
	})
	if fwErr != nil {
		return fwErr
	}
	forwarder.Start(ctx)
	runtime.SetEventForwarder(forwarder)
	logger.Infof("event forwarder started; ingest=%s", addr)
	return nil
}

// stripURLScheme converts a "https://host:port" URL into the
// "host:port" form gRPC's NewClient consumes. Returns the input
// unchanged when no scheme is present so callers can pass either
// form. Trailing slashes are trimmed.
func stripURLScheme(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	return strings.TrimSuffix(u, "/")
}

// buildAgentTLSConfig returns the TLS config the forwarder uses when
// dialing the server. It loads the mTLS client identity so outbound
// gRPC streams authenticate the same way as the HTTP enrollment POST.
func buildAgentTLSConfig(cfg *agent.Config) *tls.Config {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.ServerCAFile != "" {
		pemBytes, err := os.ReadFile(cfg.ServerCAFile)
		if err == nil {
			pool, perr := x509.SystemCertPool()
			if perr != nil || pool == nil {
				pool = x509.NewCertPool()
			}
			pool.AppendCertsFromPEM(pemBytes)
			tlsCfg.RootCAs = pool
		}
	}
	if cfg.AgentCertFile != "" && cfg.AgentKeyFile != "" {
		if cert, err := tls.LoadX509KeyPair(cfg.AgentCertFile, cfg.AgentKeyFile); err == nil {
			tlsCfg.Certificates = []tls.Certificate{cert}
		}
	}
	return tlsCfg
}

// oauth2TokenAdapter bridges pkg/agent.TokenSource to
// golang.org/x/oauth2.TokenSource. Keeps the event forwarder decoupled
// from the HTTP heartbeat sender's token interface.
type oauth2TokenAdapter struct {
	src agent.TokenSource
}

// Token implements oauth2.TokenSource.
func (a *oauth2TokenAdapter) Token() (*oauth2Token, error) {
	tok, err := a.src.Token(context.Background())
	if err != nil {
		return nil, err
	}
	return &oauth2Token{AccessToken: tok, TokenType: "Bearer"}, nil
}

// oauth2Token is the minimal shape the forwarder reads. We avoid importing
// golang.org/x/oauth2 into this CLI file to keep the dependency graph
// tight; the forwarder uses the same type via its own import.
type oauth2Token = oauthpkg.Token

// buildAgentHTTPClient constructs the HTTP client the ProductionEnroller uses
// to reach the server. The client is configured for mTLS: it presents the
// daemon's client certificate on the TLS handshake and trusts the server
// CA bundle from ServerCAFile.
func buildAgentHTTPClient(cfg *agent.Config) (*http.Client, error) {
	cert, err := tls.LoadX509KeyPair(cfg.AgentCertFile, cfg.AgentKeyFile)
	if err != nil {
		return nil, &OpError{Op: "agent serve: load agent key pair", Cause: err}
	}

	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if cfg.ServerCAFile != "" {
		pemBytes, err := os.ReadFile(cfg.ServerCAFile)
		if err != nil {
			return nil, &OpError{Op: "agent serve: read server CA", Cause: err}
		}
		if !pool.AppendCertsFromPEM(pemBytes) {
			return nil, &MessageError{Message: fmt.Sprintf("agent serve: no PEM certificates parsed from %s", cfg.ServerCAFile)}
		}
	}

	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, &MessageError{Message: "agent serve: http.DefaultTransport is not an *http.Transport"}
	}
	transport := base.Clone()
	transport.TLSClientConfig = &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		MinVersion:   tls.VersionTLS12,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}, nil
}

// buildServeConfig converts CLI flag values into a validated Config.
func buildServeConfig() (*agent.Config, error) {
	policy, err := orchestration.ParseSandboxPolicy(serveSandboxPolicy)
	if err != nil {
		return nil, err
	}

	backends := make([]agent.BackendKind, 0, 2)
	for _, raw := range splitAndTrim(serveBackendsCSV) {
		backends = append(backends, agent.BackendKind(raw))
	}

	imageMap := map[string]string{}
	for _, pair := range splitAndTrim(serveImageMapCSV) {
		eq := strings.IndexByte(pair, '=')
		if eq <= 0 || eq == len(pair)-1 {
			return nil, &MessageError{Message: fmt.Sprintf("--image-map entry %q must be backend=image", pair)}
		}
		imageMap[pair[:eq]] = pair[eq+1:]
	}

	cfg := &agent.Config{
		ServerAddr:        serveServerAddr,
		TenantID:          serveTenantID,
		BindAddr:          serveBindAddr,
		Backends:          backends,
		SandboxPolicy:     policy,
		SandboxNetwork:    serveSandboxNetwork,
		AllowedMountRoots: serveAllowedMounts,
		ContainerRuntime:  agent.ContainerRuntimeSelector(serveContainerRT),
		ImageMap:          imageMap,
		ImageRegistry:     serveImageRegistry,
		AutoPull:          serveAutoPull,
		WorkDir:           serveWorkDir,
		AgentCertFile:     serveAgentCertFile,
		AgentKeyFile:      serveAgentKeyFile,
		ServerCAFile:      serveServerCAFile,
		HeartbeatInterval: serveHeartbeatEvery,
		ShutdownGrace:     serveShutdownGrace,
		MCPEndpoint:       serveMCPEndpoint,
	}
	return cfg, nil
}

// splitAndTrim splits a comma-separated value and trims whitespace. Empty
// elements are dropped.
func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// waitForSignal blocks until SIGINT or SIGTERM is received, at which point
// it calls stopFn with a context bounded by the configured shutdown grace.
// This is used by the serve loop once Phases 3/4 wire the enroller and
// spawn server.
func waitForSignalAndStop(d *agent.Daemon) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	<-sigCh
	ctx, cancel := context.WithTimeout(context.Background(), d.Config().EffectiveShutdownGrace())
	defer cancel()
	return d.Stop(ctx)
}

// agentServeLogger is a minimal interfaces.Logger implementation that writes
// to stderr. Used only by `agent serve` so we don't pull in the full logger
// factory for a single process.
type agentServeLogger struct{}

func newAgentServeLogger() *agentServeLogger { return &agentServeLogger{} }

func (l *agentServeLogger) Debug(msg string, args ...any) {
	fmt.Fprintln(os.Stderr, append([]any{"DEBUG:", msg}, args...)...)
}
func (l *agentServeLogger) Debugf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "DEBUG: "+format+"\n", args...)
}
func (l *agentServeLogger) Info(msg string, args ...any) {
	fmt.Fprintln(os.Stderr, append([]any{"INFO:", msg}, args...)...)
}
func (l *agentServeLogger) Infof(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "INFO: "+format+"\n", args...)
}
func (l *agentServeLogger) Warn(msg string, args ...any) {
	fmt.Fprintln(os.Stderr, append([]any{"WARN:", msg}, args...)...)
}
func (l *agentServeLogger) Warnf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "WARN: "+format+"\n", args...)
}
func (l *agentServeLogger) Error(msg string, args ...any) {
	fmt.Fprintln(os.Stderr, append([]any{"ERROR:", msg}, args...)...)
}
func (l *agentServeLogger) Errorf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
}
func (l *agentServeLogger) Fatal(msg string) {
	fmt.Fprintln(os.Stderr, "FATAL:", msg)
	os.Exit(1)
}
func (l *agentServeLogger) Fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "FATAL: "+format+"\n", args...)
	os.Exit(1)
}
func (l *agentServeLogger) WithField(_ string, _ interface{}) interfaces.Logger { return l }
func (l *agentServeLogger) WithFields(_ map[string]interface{}) interfaces.Logger {
	return l
}
func (l *agentServeLogger) WithError(_ error) interfaces.Logger { return l }
