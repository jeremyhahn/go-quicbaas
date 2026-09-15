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
	"errors"
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
	serveOllamaBaseURL  string
	serveOpenVINOURL    string
	serveOpenVINOModel  string
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
	serveEnrollRetry    time.Duration
	serveEnrollRetryMax time.Duration
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
    --server https://quicbaas.example.com:8443 \
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
	cmd.Flags().StringVar(&serveOllamaBaseURL, "ollama-url", "", "Base URL of the ollama server the \"ollama\" backend uses, without /v1 (default http://127.0.0.1:11434).")
	cmd.Flags().StringVar(&serveOpenVINOURL, "openvino-url", "", "OpenVINO Model Server base URL, without /v3 (e.g. http://gpu-host:9000). Registers the \"openvino\" backend; empty leaves it unregistered.")
	cmd.Flags().StringVar(&serveOpenVINOModel, "openvino-model", "", "Model the OpenVINO server serves, e.g. qwen3-8b.")
	cmd.Flags().StringVar(&serveImageMapCSV, "image-map", "", "Comma-separated backend=image mappings (e.g., claude-code=quicbaas/claude-code:v1)")
	cmd.Flags().StringVar(&serveImageRegistry, "image-registry", "", "Registry prefix applied to unqualified image references")
	cmd.Flags().BoolVar(&serveAutoPull, "auto-pull", false, "Pull missing container images at startup")
	cmd.Flags().StringVar(&serveWorkDir, "work-dir", "/var/lib/quicbaas-agent", "Working directory for state, scratch, and session workspaces")
	cmd.Flags().StringVar(&serveAgentCertFile, "agent-cert", "", "Path to the daemon's PEM-encoded mTLS client certificate (required)")
	cmd.Flags().StringVar(&serveAgentKeyFile, "agent-key", "", "Path to the daemon's PEM-encoded mTLS private key (required)")
	cmd.Flags().StringVar(&serveServerCAFile, "server-ca", "", "Path to the PEM-encoded CA bundle the daemon trusts when dialing --server (required)")
	cmd.Flags().DurationVar(&serveHeartbeatEvery, "heartbeat-interval", 30*time.Second, "How often to heartbeat into the fleet registry")
	cmd.Flags().DurationVar(&serveShutdownGrace, "shutdown-grace", 30*time.Second, "How long to wait for in-flight sessions to drain on shutdown")
	cmd.Flags().DurationVar(&serveEnrollRetry, "enroll-retry-base", time.Second, "First backoff wait before retrying an enrollment that failed because the server was unreachable. Doubles per attempt up to --enroll-retry-max. Rejections (bad credentials, untrusted certificate) are never retried.")
	cmd.Flags().DurationVar(&serveEnrollRetryMax, "enroll-retry-max", time.Minute, "Upper bound on a single enrollment backoff wait")
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

	runtime, rtErr := buildAgentRuntime(cfg, backends, logger)
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
		// Register what this daemon SERVES, not the transport kinds --backends
		// named: one "acp" kind constructs claude, codex and opencode, and
		// those are the names a task routes on. Same call the ready log and
		// the heartbeat status use, so the fleet record, the log and the
		// heartbeat never disagree (go-quicbaas#681).
		ServedBackends: runtime.Backends,
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

	// Pooled provider accounts. The pool itself lives on the control plane;
	// this composes the SAME orchestration.AccountResolver the control plane
	// composes, over API-backed stores, so a dispatched spawn naming
	// account_id / account_auto resolves here instead of being refused
	// "account: resolver not configured" (go-quicbaas#734). It shares the
	// daemon's mTLS client and token source, so one enrollment serves the
	// heartbeat, the event forwarder and the credential release alike — which
	// is also why it is wired after the runtime rather than into it: those two
	// dependencies are built alongside the enroller, exactly as the event
	// forwarder's are.
	accountPool, apErr := agent.NewAccountPool(agent.AccountPoolConfig{
		ServerAddr: cfg.ServerAddr,
		TenantID:   cfg.TenantID,
		HTTPClient: httpClient,
		Tokens:     tokens,
		Logger:     logger,
	})
	if apErr != nil {
		return formatError(apErr)
	}
	runtime.SetAccountResolver(accountPool.Resolver)

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

	// Enrollment can now block: an unreachable server is waited out with
	// backoff rather than treated as fatal. Wire the shutdown signals into
	// the startup context for that window so `systemctl stop` ends the wait
	// instead of relying on SIGTERM's default disposition killing the
	// process mid-backoff. The handler is released as soon as startup
	// finishes; waitForSignalAndStop owns signals from then on.
	enrollCtx, releaseEnrollSignals := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	startErr := daemon.Start(enrollCtx)
	releaseEnrollSignals()
	if startErr != nil {
		// A shutdown requested while the daemon was still waiting for the
		// server is an ordinary stop, not a failure: reporting it as one
		// would make the supervisor restart a unit the operator just
		// asked to stop.
		if errors.Is(startErr, context.Canceled) {
			logger.Infof("agent daemon shutdown requested during enrollment; exiting")
			return nil
		}
		return formatError(startErr)
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

// buildAgentRuntime constructs the daemon's Runtime over the supplied
// backends, resolving --container-runtime into the engine that will host
// sandboxed sessions. Without that resolution the runtime holds no engine and
// every sandboxed spawn is refused for want of one, no matter what the
// operator selected (go-quicbaas#732).
//
// The daemon's own rules — chiefly that sandbox policy "required" needs a
// usable engine — live in agent.NewRuntime, so this path and the embedded
// agent enforce one rule rather than two.
func buildAgentRuntime(cfg *agent.Config, backends map[string]orchestration.AgentBackend, logger interfaces.Logger) (*agent.Runtime, error) {
	containerRuntime, rtErr := agent.ResolveContainerRuntime(cfg.EffectiveContainerRuntime(), logger)
	if rtErr != nil {
		return nil, rtErr
	}
	return agent.NewRuntime(agent.RuntimeConfig{
		Backends:         backends,
		ContainerRuntime: containerRuntime,
		SandboxPolicy:    cfg.SandboxPolicy,
		ImageMap:         cfg.ImageMap,
		ImageRegistry:    cfg.ImageRegistry,
		Logger:           logger,
		WorkDir:          cfg.WorkDir,
		// Bind-mount allowlist. Empty denies every host mount a sandboxed
		// spawn asks for, so a daemon started without --allowed-mount-root
		// cannot be talked into mounting an operator's filesystem.
		MountPolicy: cfg.MountPolicy(),
	})
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
			// CODEX SPEAKS NO ACP. `codex --help` lists exec, review, login,
			// mcp, app-server — nothing that speaks agent-client-protocol.
			// This was constructed with no subcommand and no transport, so the
			// daemon launched the INTERACTIVE CLI and sent it JSON-RPC; codex
			// read nothing it understood on a piped stdin and exited, which
			// arrived as `acp: transport failed: initialize` and read as a
			// broken protocol. Every independent review failed this way until
			// 2026-09-13.
			//
			// `codex exec --json` is the non-interactive mode: one turn per
			// invocation, prompt on stdin, JSONL events on stdout.
			// --sandbox danger-full-access because the CONTAINER is the
			// sandbox and codex's own would otherwise block the commands a
			// review runs. --skip-git-repo-check because a workspace root is
			// not always a repository root.
			backends["codex"] = orchestration.NewACPBackend(orchestration.ACPBackendConfig{
				Command:   "codex",
				Transport: orchestration.ACPTransportExec,
				// SerenaCodexConfigArgs registers serena, the semantic
				// code-retrieval server the reviewer charter tells this agent to
				// search with. codex has no --mcp-config flag — only
				// `-c <dotted.key>=<TOML>` — so its registration is stated on
				// argv. It is built in pkg/orchestration beside Claude Code's so
				// both CLIs get the same server argv and the same checkout root.
				ExecArgs: append([]string{
					"exec", "--json",
					"--sandbox", "danger-full-access",
					"--skip-git-repo-check",
				}, orchestration.SerenaCodexConfigArgs()...),
				ExecModelFlag: "--model",
				ExecStdinArg:  "-",
				// codex finds its credential through CODEX_HOME, not $HOME.
				CredentialDir: orchestration.CredentialDirSpec{
					Rel:     ".codex",
					Target:  "/home/agent/.codex",
					HomeEnv: "CODEX_HOME",
					// codex treats CODEX_HOME as its whole state directory and
					// refuses to start without write access to it:
					//   failed to initialize in-process app-server client:
					//   Read-only file system (os error 30)
					// The directory is a fresh per-spawn materialization of one
					// account's files, so the write cannot reach another
					// session's credential or the operator's own.
					Writable: true,
				},
				DefaultModel:      "",
				SandboxNetwork:    sandboxNetwork,
				MCPPresetResolver: mcpResolver,
			})
			backends["opencode"] = orchestration.NewACPBackend(orchestration.ACPBackendConfig{
				Command:           "opencode",
				Args:              []string{"acp"},
				DefaultModel:      "",
				SandboxNetwork:    sandboxNetwork,
				MCPPresetResolver: mcpResolver,
				// Was hardcoded in the backend; stated here now so a second
				// file-credential CLI needs no edit to the backend. Same paths,
				// same read-only mount, same behaviour as before.
				CredentialDir: orchestration.CredentialDirSpec{
					Rel:    ".config/opencode",
					Target: "/home/agent/.config/opencode",
				},
			})
		case agent.BackendOllama:
			client := ollama.NewClient(ollama.ClientConfig{
				BaseURL:    cfg.EffectiveOllamaBaseURL(),
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
	// EVERY ACP BACKEND GETS THE DAEMON'S LOGGER. ACPBackend.SetLogger is how
	// the backend reports what a CLI did when a turn fails — most importantly
	// the evidence withheld from the error string, which is otherwise a byte
	// count an operator cannot act on. Nothing called it here, so those
	// diagnostics were dead on every agent daemon: a reviewer whose CLI
	// produced no turn logged "(9882 bytes of stderr withheld)" and nothing
	// else, on every attempt, for as long as the fault lasted.
	for _, backend := range backends {
		if acp, ok := backend.(*orchestration.ACPBackend); ok {
			acp.SetLogger(logger)
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
		OllamaBaseURL:     serveOllamaBaseURL,
		OpenVINOBaseURL:   serveOpenVINOURL,
		OpenVINOModel:     serveOpenVINOModel,
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
		EnrollRetryBase:   serveEnrollRetry,
		EnrollRetryMax:    serveEnrollRetryMax,
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
