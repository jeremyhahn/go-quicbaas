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

	"github.com/jeremyhahn/go-quicbaas/pkg/fleet"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Fleet Serve Command Flags
// =============================================================================
//
// Flag semantics mirror pkg/fleet.Config so the CLI and config-file paths
// stay in lockstep: --config reads a fleet.Config YAML file, and every flag
// set explicitly on the command line overrides the matching key in it (see
// resolveServeConfig).

var (
	serveServerAddr       string
	serveTenantID         string
	serveBindAddr         string
	serveAdvertiseAddr    string
	serveBackendsCSV      string
	serveSandboxPolicy    string
	serveSandboxNetwork   string
	serveSandboxCapAdd    []string
	serveSandboxPidsLimit int64
	serveSandboxUlimits   []string
	serveOllamaBaseURL    string
	serveOpenVINOURL      string
	serveOpenVINOModel    string
	serveOpenVINOCtx      int
	serveOllamaAgentModel string
	serveOllamaAgentCtx   int
	serveVLLMBaseURL      string
	serveVLLMModel        string
	serveVLLMCtx          int
	serveCodexEffort      string
	serveAllowedMounts    []string
	serveContainerRT      string
	serveImageMapCSV      string
	serveImageRegistry    string
	serveAutoPull         bool
	serveWorkDir          string
	serveProjectsRoot     string
	serveSocketsRoot      string
	serveGoCacheDir       string
	serveNodeCertFile     string
	serveNodeKeyFile      string
	serveServerCAFile     string
	serveHeartbeatEvery   time.Duration
	serveShutdownGrace    time.Duration
	serveMaxJobs          int
	serveMCPEndpoint      string
	serveEnrollRetry      time.Duration
	serveEnrollRetryMax   time.Duration
	serveConfigFile       string
)

// newFleetServeCommand builds the `quicbaas fleet serve` subcommand.
//
// This command is the long-lived daemon entrypoint on remote fleet node hosts.
// It registers with the QuicBaaS server over mTLS, exposes a FleetRuntime
// gRPC service, and heartbeats into the fleet registry.
func newFleetServeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the QuicBaaS fleet node daemon",
		Long: `Run the QuicBaaS agent runtime daemon on this host.

The daemon authenticates to the QuicBaaS server over mTLS using a client
certificate issued by the server's CA, exposes a gRPC FleetRuntime
service on the configured bind address, and heartbeats into the fleet
registry.

Examples:
  quicbaas fleet serve \
    --server https://quicbaas.example.com:8443 \
    --tenant acme \
    --bind 127.0.0.1:9091 \
    --backends acp,ollama \
    --sandbox-policy required \
    --node-cert /var/lib/quicbaas-agent/agent.crt \
    --node-key /var/lib/quicbaas-agent/agent.key \
    --server-ca /var/lib/quicbaas-agent/server-ca.crt \
    --max-concurrent-jobs 4

  # Every setting from a file (the fleet.Config schema, as YAML). A flag
  # set explicitly on the command line overrides the file's value; keys
  # the file omits take the flag defaults. This is how hosts installed by
  # "quicbaas fleet deploy" run.
  quicbaas fleet serve --config ~/.quicbaas/bin/agent.yaml`,
		PreRunE: relaxRequiredServeFlagsForConfigFile,
		RunE:    runFleetServe,
	}

	cmd.Flags().StringVar(&serveConfigFile, "config", "", "Path to an agent config file (YAML, the fleet.Config schema). Keys it omits take the flag defaults; a flag set explicitly on the command line overrides the file.")
	cmd.Flags().IntVar(&serveMaxJobs, "max-concurrent-jobs", 0, "How many orchestration jobs this host can run at once, reported on every heartbeat as its share of fleet capacity. Zero declares nothing, which leaves the fleet's capacity undeclared.")
	cmd.Flags().StringVar(&serveServerAddr, "server", "", "HTTPS endpoint of the QuicBaaS server (required)")
	cmd.Flags().StringVar(&serveTenantID, "tenant", "", "Tenant this agent serves (required)")
	cmd.Flags().StringVar(&serveBindAddr, "bind", "127.0.0.1:9091", "Local bind address for the FleetRuntime gRPC service")
	cmd.Flags().StringVar(&serveAdvertiseAddr, "advertise", "", "host:port the control plane dials to reach this agent, when it differs from --bind (a wildcard bind, NAT, a mapped port). Also "+serveAdvertiseEnv+". Enrollment is refused when the control plane cannot resolve and dial it. Empty derives it from --bind and the node certificate.")
	cmd.Flags().StringVar(&serveBackendsCSV, "backends", "acp,ollama", "Comma-separated backends this node serves, each by name: claude, codex, opencode, openvino, ollama-agent, vllm, ollama; or \"acp\", the group of every agent backend (openvino and vllm only when their URL is set). A node advertises exactly these, so placement never routes other work to it.")
	cmd.Flags().StringVar(&serveSandboxPolicy, "sandbox-policy", "required", "Sandbox policy: required | preferred | opt-in")
	cmd.Flags().StringVar(&serveSandboxNetwork, "sandbox-network", "", "Container network for sandboxed sessions: bridge (default) | none | host | <network name>. \"host\" removes network isolation and exposes this host's loopback services to the agent.")
	cmd.Flags().StringSliceVar(&serveSandboxCapAdd, "sandbox-cap-add", nil, "Capabilities given back to sandboxed containers after --cap-drop ALL; comma-separated or repeatable. Empty keeps the default CHOWN,DAC_OVERRIDE,FOWNER,SETGID,SETUID (what sudo and apk need); a value REPLACES that set. ALL is refused.")
	cmd.Flags().Int64Var(&serveSandboxPidsLimit, "sandbox-pids-limit", 0, fmt.Sprintf("Maximum processes plus threads in a sandboxed container (--pids-limit). Zero takes the default %d.", orchestration.DefaultSandboxPidsLimit))
	cmd.Flags().StringSliceVar(&serveSandboxUlimits, "sandbox-ulimit", nil, fmt.Sprintf("Resource limit for sandboxed containers as name=soft[:hard]; comma-separated or repeatable. The default is memlock=%d (4 GiB, what the CI runners give); an entry naming memlock replaces it.", orchestration.DefaultSandboxMemlockBytes))
	cmd.Flags().StringVar(&serveContainerRT, "container-runtime", "auto", "OCI runtime: docker | podman | auto | none")
	cmd.Flags().StringVar(&serveOllamaBaseURL, "ollama-url", "", "Base URL of the ollama server the \"ollama\" backend uses, without /v1 (default http://127.0.0.1:11434).")
	cmd.Flags().StringVar(&serveOpenVINOURL, "openvino-url", "", "OpenVINO Model Server base URL, without /v3 (e.g. http://gpu-host:9000). Registers the \"openvino\" backend; empty leaves it unregistered.")
	cmd.Flags().StringVar(&serveOpenVINOModel, "openvino-model", "", "Model the OpenVINO server serves, e.g. qwen3-8b.")
	cmd.Flags().StringVar(&serveOllamaAgentModel, "ollama-agent-model", "", "Model the \"ollama-agent\" backend uses: the container-and-tools agent loop against the ollama endpoint, for roles that must read a repository. Empty leaves the backend registered with no default model.")
	cmd.Flags().StringVar(&serveCodexEffort, "codex-reasoning-effort", "", "Reasoning effort the codex backend runs at: minimal, low, medium or high (default medium). Review is a gate, so a shallower pass approves what it should have caught rather than failing visibly.")
	cmd.Flags().StringVar(&serveVLLMBaseURL, "vllm-url", "", "Base URL of a vLLM server, without /v1 (e.g. http://gpu-host:11435). Registers the \"vllm\" backend alongside ollama so a project switches between them by naming a different backend; empty leaves it unregistered.")
	cmd.Flags().StringVar(&serveVLLMModel, "vllm-model", "", "The served-model name vLLM answers to (its --served-model-name), not a file path.")
	cmd.Flags().IntVar(&serveVLLMCtx, "vllm-context-tokens", 0, "How much prompt that vLLM model accepts. Zero learns the ceiling from the first refusal.")
	cmd.Flags().IntVar(&serveOllamaAgentCtx, "ollama-agent-context-tokens", 0, "How much prompt that ollama model accepts. Zero learns the ceiling from the first refusal.")
	cmd.Flags().IntVar(&serveOpenVINOCtx, "openvino-context-tokens", 0, "How much prompt that server accepts. Zero learns the ceiling from the first refusal. On an NPU the server reserves a static shape for this many tokens, so it is a memory decision too.")
	cmd.Flags().StringVar(&serveImageMapCSV, "image-map", "", "Comma-separated backend=image mappings (e.g., claude-code=quicbaas/claude-code:v1)")
	cmd.Flags().StringVar(&serveImageRegistry, "image-registry", "", "Registry prefix applied to unqualified image references")
	cmd.Flags().BoolVar(&serveAutoPull, "auto-pull", false, "Pull missing container images: the image map at startup, and an image a spawn names (a project binding's worker image) before it starts")
	cmd.Flags().StringVar(&serveWorkDir, "work-dir", "/var/lib/quicbaas-agent", "Working directory for state, scratch, and session workspaces")
	cmd.Flags().StringVar(&serveProjectsRoot, "projects-root", "", "Absolute directory this agent provisions job workspaces under (<root>/<tenant>/<project>/workspace/<key>); the checkouts live here and a job's later sessions are pinned back to them, so it must persist across restarts. Admitted for mounts tenant-scoped, like the server's own projects root. Empty takes <work-dir>/projects.")
	cmd.Flags().StringVar(&serveGoCacheDir, "go-cache-dir", "", "Absolute host directory holding the Go module and build caches every sandbox container shares, one pair per tenant (<dir>/<tenant>/{mod,build}). Empty takes <work-dir>/cache/go. Must lie outside the projects root and every --allowed-mount-root.")
	cmd.Flags().StringVar(&serveSocketsRoot, "sockets-root", "", "Absolute directory this agent binds relayed signing sockets under (go-quicbaas#1013): a session that signs commits or clones an ssh remote mounts one, and its signatures travel over this agent's link to the control plane's KMS. Must be short enough to bind a unix socket under and private to this user. Empty derives it as the server does: $XDG_RUNTIME_DIR/quicbaas/<instance>, or <work-dir>/s.")
	cmd.Flags().StringVar(&serveNodeCertFile, "node-cert", "", "Path to the daemon's PEM-encoded mTLS client certificate (required)")
	cmd.Flags().StringVar(&serveNodeKeyFile, "node-key", "", "Path to the daemon's PEM-encoded mTLS private key (required)")
	cmd.Flags().StringVar(&serveServerCAFile, "server-ca", "", "Path to the PEM-encoded CA bundle the daemon trusts when dialing --server (required)")
	cmd.Flags().DurationVar(&serveHeartbeatEvery, "heartbeat-interval", 30*time.Second, "How often to heartbeat into the fleet registry")
	cmd.Flags().DurationVar(&serveShutdownGrace, "shutdown-grace", 30*time.Second, "How long to wait for in-flight sessions to drain on shutdown")
	cmd.Flags().DurationVar(&serveEnrollRetry, "enroll-retry-base", time.Second, "First backoff wait before retrying an enrollment that failed because the server was unreachable. Doubles per attempt up to --enroll-retry-max. Rejections (bad credentials, untrusted certificate) are never retried.")
	cmd.Flags().DurationVar(&serveEnrollRetryMax, "enroll-retry-max", time.Minute, "Upper bound on a single enrollment backoff wait")
	cmd.Flags().StringSliceVar(&serveAllowedMounts, "allowed-mount-root", nil, "Absolute host directory a sandboxed spawn may bind-mount from; repeatable. Every requested work_dir and extra mount must resolve under one of these. Omitting the flag denies all host mounts.")
	cmd.Flags().StringVar(&serveMCPEndpoint, "mcp-endpoint", "", "Fully-qualified https://host:port/mcp address spawned agents dial for the \"quicbaas\" MCP preset. Empty leaves the preset unresolved.")

	for _, flag := range serveRequiredFlags {
		if err := cmd.MarkFlagRequired(flag); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to mark --%s required: %v\n", flag, err)
		}
	}

	return cmd
}

// runFleetServe is the `quicbaas fleet serve` entrypoint.
//
// Wiring path:
//  1. Parse flags → build and validate fleet.Config.
//  2. Build the backend set the daemon will expose (ACP + Ollama per flags).
//  3. Construct the Runtime over those backends.
//  4. Construct the ProductionEnroller (HTTP → server/oauth2).
//  5. Construct the GRPCSpawnServer, wiring the Runtime in.
//  6. Construct the Daemon with all of the above.
//  7. Start; post-enroll install identity on the spawn server.
//  8. Block on SIGINT/SIGTERM; graceful Stop with configured grace.
func runFleetServe(cmd *cobra.Command, args []string) error {
	cfg, err := resolveServeConfig(cmd.Flags())
	if err != nil {
		return formatError(err)
	}
	if err := cfg.Validate(); err != nil {
		return formatError(err)
	}

	logger := cliLoggerForFleetServe()

	backends, buildErr := buildFleetBackends(cfg, logger)
	if buildErr != nil {
		return formatError(buildErr)
	}

	runtime, rtErr := buildFleetRuntime(cfg, backends, logger)
	if rtErr != nil {
		return formatError(rtErr)
	}

	httpClient, hcErr := buildFleetHTTPClient(cfg)
	if hcErr != nil {
		return formatError(hcErr)
	}
	enroller := fleet.NewProductionEnroller(fleet.ProductionEnrollerDeps{
		Logger:     logger,
		HTTPClient: httpClient,
		// Register what this daemon SERVES, not the transport kinds --backends
		// named: one "acp" kind constructs claude, codex and opencode, and
		// those are the names a task routes on. Same call the ready log and
		// the heartbeat status use, so the fleet record, the log and the
		// heartbeat never disagree (go-quicbaas#681).
		ServedBackends: runtime.Backends,
		SigningRelay:   runtime.SigningRelayEnabled,
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
	tokens := &fleet.CachingTokenSource{
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
	accountPool, apErr := fleet.NewAccountPool(fleet.AccountPoolConfig{
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
	heartbeat, hbErr := fleet.NewHTTPHeartbeatSender(fleet.HTTPHeartbeatConfig{
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

	spawnServer, ssErr := fleet.NewGRPCSpawnServer(fleet.GRPCSpawnServerDeps{
		Runtime:  runtime,
		BindAddr: cfg.BindAddr,
		Logger:   logger,
	})
	if ssErr != nil {
		return formatError(ssErr)
	}

	// The host's accelerators, sampled on every heartbeat so the control
	// plane can cost this node's local turns from measured energy.
	devices, devErr := fleet.NewHostDeviceSampler(logger)
	if devErr != nil {
		return formatError(devErr)
	}

	// The host's CPU, load, memory and work-dir disk, sampled on every
	// heartbeat so the control plane can judge this node's health.
	hostMetrics, hmErr := fleet.NewHostMetricsSampler(logger, cfg.WorkDir)
	if hmErr != nil {
		return formatError(hmErr)
	}

	daemon, dErr := fleet.NewDaemon(cfg, fleet.DaemonDeps{
		Runtime:     runtime,
		Enroller:    enroller,
		SpawnServer: spawnServer,
		Heartbeat:   heartbeat,
		Logger:      logger,
		Devices:     devices,
		HostMetrics: hostMetrics,
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
			logger.Infof("fleet node daemon shutdown requested during enrollment; exiting")
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

	logger.Infof("fleet node daemon ready; tenant=%s server=%s bind=%s backends=%s",
		cfg.TenantID, cfg.ServerAddr, spawnServer.Addr(),
		strings.Join(runtime.Backends(), ","))

	return waitForSignalAndStop(daemon)
}

// buildFleetRuntime constructs the daemon's Runtime over the supplied
// backends, resolving --container-runtime into the engine that will host
// sandboxed sessions. Without that resolution the runtime holds no engine and
// every sandboxed spawn is refused for want of one, no matter what the
// operator selected (go-quicbaas#732).
//
// The daemon's own rules — chiefly that sandbox policy "required" needs a
// usable engine — live in fleet.NewRuntime, so this path and the embedded
// fleet node enforce one rule rather than two.
func buildFleetRuntime(cfg *fleet.Config, backends map[string]orchestration.AgentBackend, logger interfaces.Logger) (*fleet.Runtime, error) {
	containerRuntime, rtErr := fleet.ResolveContainerRuntime(cfg.EffectiveContainerRuntime(), logger)
	if rtErr != nil {
		return nil, rtErr
	}
	// A BACKEND THAT RUNS ITS OWN CONTAINERS NEEDS THE RUNTIME TOO, and this
	// is the only place it exists. The CLI backends are handed one by
	// fleet.Runtime when it spawns them; the openvino backend is not a CLI --
	// it drives a remote model over HTTP and reaches the repository through a
	// tool it execs INSIDE a container of its own -- so it holds the runtime
	// directly. Without it every sandboxed spawn is refused with "a sandboxed
	// openvino session needs a container runtime and none is installed", which
	// on a daemon running --sandbox-policy required is every spawn there is.
	// Measured on the burn-in node 2026-09-16: every triage job failed at
	// spawn the moment triage was pointed at the NPU.
	//
	// A LOOP RATHER THAN A CALL AT THE REGISTRATION, matching how SetLogger is
	// applied: the backends are constructed before the runtime is resolved, so
	// this is where the two meet.
	for _, backend := range backends {
		if ov, ok := backend.(*orchestration.OpenVINOBackend); ok {
			ov.SetContainerRuntime(containerRuntime)
		}
	}
	// The job workspaces this daemon provisions live under its own projects
	// root (go-quicbaas#981); the mount policy below admits that root
	// tenant-scoped, so nothing else has to be allowlisted for them.
	workspaces, wsErr := orchestration.NewWorkspaceManager(cfg.EffectiveProjectsRoot())
	if wsErr != nil {
		return nil, wsErr
	}
	// A workspace is removed only where the same policy admits it as a mount
	// source (go-quicbaas#777).
	mountPolicy := cfg.MountPolicy()
	workspaces.SetMountPolicy(mountPolicy)
	// Relayed signing sockets (go-quicbaas#1013) are bound under this root;
	// the mount policy below admits exactly those sockets under it.
	socketsRoot, socketsErr := cfg.EffectiveSocketsRoot()
	if socketsErr != nil {
		return nil, socketsErr
	}
	return fleet.NewRuntime(fleet.RuntimeConfig{
		Backends:         backends,
		ContainerRuntime: containerRuntime,
		SandboxPolicy:    cfg.SandboxPolicy,
		ImageMap:         cfg.ImageMap,
		ImageRegistry:    cfg.ImageRegistry,
		AutoPull:         cfg.AutoPull,
		Logger:           logger,
		WorkDir:          cfg.WorkDir,
		// Bind-mount policy: this daemon's projects root, tenant-scoped, plus
		// the --allowed-mount-root allowlist. Nothing outside those is ever
		// mounted, so a daemon cannot be talked into mounting an operator's
		// filesystem.
		MountPolicy:  mountPolicy,
		Workspaces:   workspaces,
		SocketsRoot:  socketsRoot,
		GoBuildCache: cfg.GoBuildCache(),
	})
}

// buildFleetServeMCPResolver constructs the QuicBaaS MCP preset resolver for a
// remotely deployed fleet node daemon, or returns nil when no MCP endpoint was
// configured (cfg.MCPEndpoint empty) so the preset stays unresolved and current
// behavior is preserved. The resolver reuses the long-lived fleet node.crt/agent.key/
// server-ca.crt the deploy flow already uploaded — it mints nothing. A
// construction failure is logged and degrades to nil rather than aborting the
// daemon, matching the embedded fleet node's graceful behavior.
func buildFleetServeMCPResolver(cfg *fleet.Config, logger interfaces.Logger) orchestration.MCPPresetResolver {
	if strings.TrimSpace(cfg.MCPEndpoint) == "" {
		return nil
	}
	resolver, err := fleet.NewFileMCPPresetResolver(cfg.MCPEndpoint, cfg.NodeCertFile, cfg.NodeKeyFile, cfg.ServerCAFile)
	if err != nil {
		logger.Warnf("fleet node daemon: quicbaas MCP preset resolver disabled: %v", err)
		return nil
	}
	logger.Infof("fleet node daemon: quicbaas MCP preset resolver wired; endpoint=%s", cfg.MCPEndpoint)
	return resolver
}

// cliLoggerForFleetServe returns a minimal interfaces.Logger writing to the
// process's stderr. It mirrors the daemon-scale logger used by the server so
// operators see consistent formatting in journald / container logs.
func cliLoggerForFleetServe() interfaces.Logger {
	return newFleetServeLogger()
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
	enroller *fleet.ProductionEnroller,
	cfg *fleet.Config,
	runtime *fleet.Runtime,
	tokens fleet.TokenSource,
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
	forwarder, fwErr := fleet.NewEventForwarder(fleet.EventForwarderConfig{
		ServerAddr:  addr,
		TokenSource: &oauth2TokenAdapter{src: tokens},
		TLSConfig:   buildFleetTLSConfig(cfg),
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

// buildFleetTLSConfig returns the TLS config the forwarder uses when
// dialing the server. It loads the mTLS client identity so outbound
// gRPC streams authenticate the same way as the HTTP enrollment POST.
func buildFleetTLSConfig(cfg *fleet.Config) *tls.Config {
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
	if cfg.NodeCertFile != "" && cfg.NodeKeyFile != "" {
		if cert, err := tls.LoadX509KeyPair(cfg.NodeCertFile, cfg.NodeKeyFile); err == nil {
			tlsCfg.Certificates = []tls.Certificate{cert}
		}
	}
	return tlsCfg
}

// oauth2TokenAdapter bridges pkg/fleet.TokenSource to
// golang.org/x/oauth2.TokenSource. Keeps the event forwarder decoupled
// from the HTTP heartbeat sender's token interface.
type oauth2TokenAdapter struct {
	src fleet.TokenSource
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

// buildFleetHTTPClient constructs the HTTP client the ProductionEnroller uses
// to reach the server. The client is configured for mTLS: it presents the
// daemon's client certificate on the TLS handshake and trusts the server
// CA bundle from ServerCAFile.
func buildFleetHTTPClient(cfg *fleet.Config) (*http.Client, error) {
	cert, err := tls.LoadX509KeyPair(cfg.NodeCertFile, cfg.NodeKeyFile)
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
func buildServeConfig() (*fleet.Config, error) {
	policy, err := orchestration.ParseSandboxPolicy(serveSandboxPolicy)
	if err != nil {
		return nil, err
	}

	backends := fleet.ParseBackendKinds(serveBackendsCSV)

	imageMap := map[string]string{}
	for _, pair := range splitAndTrim(serveImageMapCSV) {
		eq := strings.IndexByte(pair, '=')
		if eq <= 0 || eq == len(pair)-1 {
			return nil, &MessageError{Message: fmt.Sprintf("--image-map entry %q must be backend=image", pair)}
		}
		imageMap[pair[:eq]] = pair[eq+1:]
	}

	cfg := &fleet.Config{
		ServerAddr:               serveServerAddr,
		TenantID:                 serveTenantID,
		BindAddr:                 serveBindAddr,
		AdvertiseAddr:            serveAdvertiseAddr,
		Backends:                 backends,
		SandboxPolicy:            policy,
		SandboxNetwork:           serveSandboxNetwork,
		SandboxCapAdd:            serveSandboxCapAdd,
		SandboxPidsLimit:         serveSandboxPidsLimit,
		SandboxUlimits:           serveSandboxUlimits,
		OllamaBaseURL:            serveOllamaBaseURL,
		OpenVINOBaseURL:          serveOpenVINOURL,
		OpenVINOModel:            serveOpenVINOModel,
		OpenVINOContextTokens:    serveOpenVINOCtx,
		OllamaAgentModel:         serveOllamaAgentModel,
		OllamaAgentContextTokens: serveOllamaAgentCtx,
		VLLMBaseURL:              serveVLLMBaseURL,
		VLLMModel:                serveVLLMModel,
		VLLMContextTokens:        serveVLLMCtx,
		CodexReasoningEffort:     serveCodexEffort,
		AllowedMountRoots:        serveAllowedMounts,
		ContainerRuntime:         fleet.ContainerRuntimeSelector(serveContainerRT),
		ImageMap:                 imageMap,
		ImageRegistry:            serveImageRegistry,
		AutoPull:                 serveAutoPull,
		WorkDir:                  serveWorkDir,
		ProjectsRoot:             serveProjectsRoot,
		SocketsRoot:              serveSocketsRoot,
		GoCacheDir:               serveGoCacheDir,
		NodeCertFile:             serveNodeCertFile,
		NodeKeyFile:              serveNodeKeyFile,
		ServerCAFile:             serveServerCAFile,
		HeartbeatInterval:        serveHeartbeatEvery,
		ShutdownGrace:            serveShutdownGrace,
		MaxConcurrentJobs:        serveMaxJobs,
		EnrollRetryBase:          serveEnrollRetry,
		EnrollRetryMax:           serveEnrollRetryMax,
		MCPEndpoint:              serveMCPEndpoint,
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
func waitForSignalAndStop(d *fleet.Daemon) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	<-sigCh
	ctx, cancel := context.WithTimeout(context.Background(), d.Config().EffectiveShutdownGrace())
	defer cancel()
	return d.Stop(ctx)
}

// fleetServeLogger is a minimal interfaces.Logger implementation that writes
// to stderr. Used only by `fleet serve` so we don't pull in the full logger
// factory for a single process.
type fleetServeLogger struct{}

func newFleetServeLogger() *fleetServeLogger { return &fleetServeLogger{} }

func (l *fleetServeLogger) Debug(msg string, args ...any) {
	fmt.Fprintln(os.Stderr, append([]any{"DEBUG:", msg}, args...)...)
}
func (l *fleetServeLogger) Debugf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "DEBUG: "+format+"\n", args...)
}
func (l *fleetServeLogger) Info(msg string, args ...any) {
	fmt.Fprintln(os.Stderr, append([]any{"INFO:", msg}, args...)...)
}
func (l *fleetServeLogger) Infof(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "INFO: "+format+"\n", args...)
}
func (l *fleetServeLogger) Warn(msg string, args ...any) {
	fmt.Fprintln(os.Stderr, append([]any{"WARN:", msg}, args...)...)
}
func (l *fleetServeLogger) Warnf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "WARN: "+format+"\n", args...)
}
func (l *fleetServeLogger) Error(msg string, args ...any) {
	fmt.Fprintln(os.Stderr, append([]any{"ERROR:", msg}, args...)...)
}
func (l *fleetServeLogger) Errorf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
}
func (l *fleetServeLogger) Fatal(msg string) {
	fmt.Fprintln(os.Stderr, "FATAL:", msg)
	os.Exit(1)
}
func (l *fleetServeLogger) Fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "FATAL: "+format+"\n", args...)
	os.Exit(1)
}
func (l *fleetServeLogger) WithField(_ string, _ interface{}) interfaces.Logger { return l }
func (l *fleetServeLogger) WithFields(_ map[string]interface{}) interfaces.Logger {
	return l
}
func (l *fleetServeLogger) WithError(_ error) interfaces.Logger { return l }
