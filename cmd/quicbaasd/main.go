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

// Package main is the entry point for the QuicBaaS daemon.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	"github.com/jeremyhahn/go-quicbaas/pkg/version"
)

var (
	// Global flags.
	cfgFile  string
	logLevel string
	logFile  string
	fipsMode bool
	dataDir  string

	// Server flags.
	httpPort    int
	httpAddress string
	grpcPort    int
	raftPort    int
	raftAddress string
	nodeID      uint64
	unixSocket  string

	// Embedded agent flags.
	embeddedAgentEnabled       bool
	embeddedAgentBackends      []string
	embeddedAgentOllamaURL     string
	embeddedAgentHostAffinity  string
	embeddedAgentGPU           bool
	embeddedAgentDataDir       string
	embeddedAgentSandboxImage  string
	embeddedAgentSandboxPolicy string
	embeddedAgentSandboxNet    string
	embeddedAgentAutoPull      bool

	// Discovery flags for node command.
	discoveryType string
	joinAddresses string

	// Global configuration.
	cfg *config.Config
	log *logger.Logger

	// Root command.
	rootCmd = &cobra.Command{
		Use:          "quicbaasd",
		Short:        "QuicBaaS daemon - multi-model distributed database and Backend-as-a-Service",
		SilenceUsage: true,
		Long: `QuicBaaS daemon is a modern multi-model distributed database and Backend-as-a-Service
built for the agentic cloud era.

Multi-Model Database:
  - Key-value, vector, time-series, and object storage engines
  - Embedded PebbleDB with multi-Raft consensus via QuicRaft
  - Multiple Raft groups for horizontal scaling and multi-DC failover

AI-Native Platform:
  - RAG pipelines and multi-provider embeddings
  - AI agent discovery, spawning, prompt routing, and tool execution
  - 297+ MCP tools for AI agent integration

Backend-as-a-Service:
  - IAM with ABAC/PEP, OAuth2/OIDC, WebAuthn, and MFA
  - KMS, certificate management, and secrets management
  - Job scheduler, service discovery, and pub/sub messaging

Security and Compliance:
  - FIPS 140-3 compliance mode with per-tenant encryption
  - Field-level encryption with AES-128/256-GCM
  - TLS 1.3 with mutual authentication

Multi-Protocol Access:
  - gRPC, REST, QUIC, MCP, Unix socket, and WebSocket

Commands:
  start    Start with local PebbleDB (no Raft, fastest single-node)
  node     Start as multi-node Raft cluster member (production)
  cluster  Start single-node Raft cluster (development/testing)

For more information, visit: https://github.com/jeremyhahn/go-quicbaas`,
		Version: version.Full(),
	}

	// start command - local PebbleDB mode (no Raft).
	startCmd = &cobra.Command{
		Use:   "start",
		Short: "Start with local PebbleDB backend (no Raft consensus)",
		Long: `Start QuicBaaS with direct PebbleDB backend, bypassing Raft consensus.

This is the fastest mode for single-node deployments where distributed
consensus is not needed. Data is stored locally without replication.

Examples:
  # Start with defaults
  quicbaasd start

  # Start with custom data directory
  quicbaasd start --data-dir /var/lib/quicbaas

  # Start with TCP and REST enabled
  quicbaasd start --grpc-port 9090 --http-port 8080`,
		RunE: runStart,
	}

	// node command - multi-node Raft cluster (production).
	nodeCmd = &cobra.Command{
		Use:   "node",
		Short: "Start as multi-node Raft cluster member",
		Long: `Start QuicBaaS as a member of a multi-node Raft cluster.

This is the production mode with full distributed consensus. Nodes discover
each other using the configured discovery method (multicast by default).

Discovery methods:
  - multicast  Zero-config LAN discovery via UDP multicast (default)
  - static     Fixed peer list via --join-addresses
  - dns        DNS SRV record lookup
  - kubernetes Kubernetes pod discovery

Examples:
  # Start with multicast discovery (default)
  quicbaasd node --node-id 1

  # Start with static peer list
  quicbaasd node --node-id 1 --discovery static \
    --join-addresses "192.168.1.2:63001,192.168.1.3:63002"

  # Start with DNS discovery
  quicbaasd node --node-id 1 --discovery dns

  # Start with Kubernetes discovery
  quicbaasd node --node-id 1 --discovery kubernetes`,
		RunE: runNode,
	}

	// cluster command - single-node Raft cluster (development).
	clusterCmd = &cobra.Command{
		Use:   "cluster",
		Short: "Start single-node Raft cluster for development",
		Long: `Start QuicBaaS as a single-node Raft cluster for development and testing.

This mode provides the full Raft consensus stack but runs as a single node.
Useful for testing Raft behavior without multi-node complexity.

Examples:
  # Start single-node Raft cluster
  quicbaasd cluster

  # Start with custom node ID
  quicbaasd cluster --node-id 1

  # Start with TCP enabled
  quicbaasd cluster --grpc-port 9090`,
		RunE: runCluster,
	}
)

func init() {
	cobra.OnInitialize(initConfig)

	// Global persistent flags.
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (optional, defaults to built-in defaults)")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "log level (trace|debug|info|warn|error|fatal|panic)")
	rootCmd.PersistentFlags().StringVar(&logFile, "log-file", "", "log file path (default: stdout; appends to the file when set so you can tail -f it)")
	rootCmd.PersistentFlags().BoolVar(&fipsMode, "fips", false, "enable FIPS 140-3 mode")
	rootCmd.PersistentFlags().StringVar(&dataDir, "data-dir", "./data", "data directory path")

	// Common server flags for all commands.
	addServerFlags(startCmd)
	addServerFlags(nodeCmd)
	addServerFlags(clusterCmd)

	// Raft-specific flags for node and cluster commands.
	addRaftFlags(nodeCmd)
	addRaftFlags(clusterCmd)

	// Discovery flags for node command only.
	nodeCmd.Flags().StringVar(&discoveryType, "discovery", "multicast", "discovery method (multicast|static|dns|kubernetes)")
	nodeCmd.Flags().StringVar(&joinAddresses, "join-addresses", "", "comma-separated peer addresses for static discovery")

	// Bind flags to viper.
	_ = viper.BindPFlag("logging.level", rootCmd.PersistentFlags().Lookup("log-level"))
	_ = viper.BindPFlag("logging.file", rootCmd.PersistentFlags().Lookup("log-file"))
	_ = viper.BindPFlag("security.fips_mode", rootCmd.PersistentFlags().Lookup("fips"))
	_ = viper.BindPFlag("storage.data_dir", rootCmd.PersistentFlags().Lookup("data-dir"))

	// Add subcommands.
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(nodeCmd)
	rootCmd.AddCommand(clusterCmd)

	// Wire the locked canonical CLI surface per
	// docs/08-bootstrap-auth-architecture.md §6.
	configureCanonicalSurface()
}

// addServerFlags adds common server flags to a command.
func addServerFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&unixSocket, "unix-socket", config.DefaultUnixSocket, "Unix socket path for local gRPC communication")
	cmd.Flags().IntVar(&httpPort, "http-port", 0, "HTTP REST API port (0 = disabled)")
	cmd.Flags().StringVar(&httpAddress, "http-address", "", "Bind address for the HTTP REST API (default: config or 127.0.0.1). Use 0.0.0.0 to listen on all interfaces.")
	cmd.Flags().IntVar(&grpcPort, "grpc-port", 0, "gRPC TCP port (0 = Unix socket only)")

	// Embedded agent flags.
	cmd.Flags().BoolVar(&embeddedAgentEnabled, "embedded-agent", false, "Start an in-process agent runtime alongside the orchestrator (default: false)")
	cmd.Flags().StringArrayVar(&embeddedAgentBackends, "embedded-agent-backend", []string{"ollama"}, "Backend(s) the embedded agent exposes (repeatable)")
	cmd.Flags().StringVar(&embeddedAgentOllamaURL, "embedded-agent-ollama-url", "http://127.0.0.1:11434", "Ollama API endpoint for the embedded agent")
	cmd.Flags().StringVar(&embeddedAgentHostAffinity, "embedded-agent-host-affinity", "", "Host-affinity tag for the embedded agent (default: hostname)")
	cmd.Flags().BoolVar(&embeddedAgentGPU, "embedded-agent-gpu", false, "Advertise GPU capability for the embedded agent")
	cmd.Flags().StringVar(&embeddedAgentDataDir, "embedded-agent-data-dir", "", "Working directory for embedded agent session state (default: <data-dir>/embedded-agent)")
	cmd.Flags().StringVar(&embeddedAgentSandboxImage, "embedded-agent-sandbox-image", "", "OCI image the embedded agent spawns for sandboxed sessions (must have the backend agent on PATH)")
	cmd.Flags().StringVar(&embeddedAgentSandboxPolicy, "embedded-agent-sandbox-policy", "", "Embedded agent sandbox policy: opt-in (default), preferred, or required")
	cmd.Flags().StringVar(&embeddedAgentSandboxNet, "embedded-agent-sandbox-network", "", "Container network for embedded-agent sandboxed sessions: bridge (default), none, host, or a network name. \"host\" removes network isolation from the agent container.")
	cmd.Flags().BoolVar(&embeddedAgentAutoPull, "embedded-agent-auto-pull", false, "Pre-pull the embedded agent sandbox image(s) at startup (leave off for locally-built images)")
}

// addRaftFlags adds Raft-specific flags to a command.
func addRaftFlags(cmd *cobra.Command) {
	cmd.Flags().IntVar(&raftPort, "raft-port", 63000, "Raft communication port")
	cmd.Flags().StringVar(&raftAddress, "raft-address", "", "Raft node address (default: <hostname>:<raft-port>)")
	cmd.Flags().Uint64Var(&nodeID, "node-id", 1, "Raft node ID")
}

func initConfig() {
	// Start with default configuration - config file is optional.
	cfg = config.Default()

	// Set viper defaults for vector config (important for proper unmarshal behavior).
	viper.SetDefault("vector.enabled", false)
	viper.SetDefault("vector.shard_id", 200)
	viper.SetDefault("vector.storage_path", "./data/vector")

	// Set viper defaults for timeseries config.
	viper.SetDefault("timeseries.enabled", false)
	viper.SetDefault("timeseries.shard_id", config.DefaultTimeSeriesConfig().ShardID)
	viper.SetDefault("timeseries.storage_path", "")

	// Set viper defaults for backup config (important for proper unmarshal behavior).
	viper.SetDefault("backup.enabled", false)
	viper.SetDefault("backup.storage_backend", "local")
	viper.SetDefault("backup.default_compression", "zstd")
	viper.SetDefault("backup.default_encryption", "aes-256-gcm")

	if cfgFile != "" {
		// If config file is explicitly specified, use it.
		viper.SetConfigFile(cfgFile)
		if err := viper.ReadInConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "Error reading config file: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	} else {
		// Search for config in standard locations (optional).
		// Try both "quicbaas" and "config" as config file names.
		configNames := []string{"quicbaas", "config"}
		configPaths := []string{"/etc/quicbaas", "$HOME/.quicbaas", "."}
		viper.SetConfigType("yaml")
		for _, p := range configPaths {
			viper.AddConfigPath(p)
		}

		found := false
		for _, name := range configNames {
			viper.SetConfigName(name)
			if err := viper.ReadInConfig(); err == nil {
				fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
				found = true
				break
			}
		}
		_ = found // Config file is optional; defaults used if not found.
	}

	// Environment variables.
	viper.SetEnvPrefix("QUICBAAS")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// Bind environment variables to config keys.
	bindEnvVars()

	// Unmarshal config into struct.
	if err := viper.Unmarshal(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to unmarshal configuration: %v\n", err)
		os.Exit(1)
	}

	// Parse special environment variables that viper doesn't handle automatically.
	parseSpecialEnvVars()

	// Initialize logger.
	logConfig := logger.LogConfig{
		Level:  cfg.Logging.Level,
		Format: cfg.Logging.Format,
		Output: cfg.Logging.Output,
	}
	if logConfig.Level == "" {
		logConfig.Level = "info"
	}
	if logConfig.Format == "" {
		logConfig.Format = "json"
	}
	if logConfig.Output == "" {
		logConfig.Output = "stdout"
	}
	// --log-file flag overrides Output — redirect to file so the user can
	// tail -f it in realtime. Matches go-qrdb / go-xkms convention.
	if logFile != "" {
		logConfig.Output = "file"
		logConfig.File = logFile
	}

	var err error
	log, err = logger.New(logConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
}

// bindEnvVars binds environment variables to config keys.
func bindEnvVars() {
	// Server configuration - nested protocol configs.
	_ = viper.BindEnv("server.host")
	_ = viper.BindEnv("server.mode")
	_ = viper.BindEnv("server.grpc.enabled")
	_ = viper.BindEnv("server.grpc.port")
	_ = viper.BindEnv("server.rest.enabled")
	_ = viper.BindEnv("server.rest.port")
	_ = viper.BindEnv("server.quic.enabled")
	_ = viper.BindEnv("server.quic.port")
	_ = viper.BindEnv("server.mcp.enabled")
	_ = viper.BindEnv("server.mcp.port")
	_ = viper.BindEnv("server.unix.enabled")
	_ = viper.BindEnv("server.unix.socket")
	// NOTE: server.shards and server.entity_shard_pins are NOT bound to viper
	// because viper cannot parse comma-separated strings to []uint64 or key:value
	// format to map[string]uint64. These are handled by parseSpecialEnvVars().

	// Server HTTP timeout configuration.
	_ = viper.BindEnv("server.read_timeout")
	_ = viper.BindEnv("server.write_timeout")
	_ = viper.BindEnv("server.idle_timeout")

	// Raft configuration.
	_ = viper.BindEnv("raft.node_id")
	_ = viper.BindEnv("raft.deployment_id")
	_ = viper.BindEnv("raft.raft_address")
	_ = viper.BindEnv("raft.listen_address")
	_ = viper.BindEnv("raft.join_addresses")

	// Storage configuration.
	_ = viper.BindEnv("storage.backend")
	_ = viper.BindEnv("storage.data_dir")
	_ = viper.BindEnv("storage.wal_dir")

	// Logging configuration.
	_ = viper.BindEnv("logging.level")
	_ = viper.BindEnv("logging.format")
	_ = viper.BindEnv("logging.output")

	// Security configuration.
	_ = viper.BindEnv("security.fips_mode")
	_ = viper.BindEnv("security.tls_enabled")
	_ = viper.BindEnv("security.tls_cert_file")
	_ = viper.BindEnv("security.tls_key_file")
	_ = viper.BindEnv("security.tls_ca_file")
	_ = viper.BindEnv("security.tls_client_auth")

	// Discovery configuration.
	_ = viper.BindEnv("discovery.type")
	_ = viper.BindEnv("discovery.enabled")
	_ = viper.BindEnv("discovery.static_peers")

	// KMS configuration.
	_ = viper.BindEnv("kms.enabled")
	_ = viper.BindEnv("kms.backend")
	_ = viper.BindEnv("kms.enabled_backends")

	// Seal/barrier configuration. Binding seal.strategy lets an operator
	// override the barrier strategy persisted by `quicbaasd init` via the
	// QUICBAAS_SEAL_STRATEGY env var (or a seal.strategy config-file key). When
	// set, runStart marks cfg.Seal.StrategyExplicit so the override wins over
	// the data dir's init marker. See pkg/server/init_marker.go.
	// QUICBAAS_SEAL_PASSPHRASE is handled separately in runDaemon because viper
	// does not zero secrets — it is read once via os.Getenv and passed directly
	// to server.WithSealPassphrase so the slice can be zeroed immediately.
	_ = viper.BindEnv("seal.enabled")
	_ = viper.BindEnv("seal.strategy")
	_ = viper.BindEnv("seal.threshold")
	_ = viper.BindEnv("seal.total_shares")

	// Raft join mode: QUICBAAS_RAFT_JOIN=true signals that this node is
	// joining an existing cluster rather than bootstrapping a new one.
	// Used for dynamic membership (node4+ joining a running 3-node cluster).
	_ = viper.BindEnv("raft.join")

	// Secrets configuration.
	_ = viper.BindEnv("secrets.enabled")

	// Backup configuration.
	_ = viper.BindEnv("backup.enabled")
	_ = viper.BindEnv("backup.storage_backend")
	_ = viper.BindEnv("backup.default_compression")
	_ = viper.BindEnv("backup.default_encryption")
	_ = viper.BindEnv("backup.encryption_key")
	_ = viper.BindEnv("backup.work_dir")

	// Vector configuration.
	_ = viper.BindEnv("vector.enabled")
	_ = viper.BindEnv("vector.shard_id")

	// TimeSeries configuration.
	_ = viper.BindEnv("timeseries.enabled")
	_ = viper.BindEnv("timeseries.shard_id")
	_ = viper.BindEnv("timeseries.storage_path")

	// Messaging configuration.
	_ = viper.BindEnv("messaging.enabled")
	_ = viper.BindEnv("messaging.buffer_size")
	_ = viper.BindEnv("messaging.dedup_enabled")
	_ = viper.BindEnv("messaging.dedup_window")
	_ = viper.BindEnv("messaging.watch_bridge.enabled")

	// Orchestration configuration.
	_ = viper.BindEnv("orchestration.enabled")

	// Ollama service configuration.
	_ = viper.BindEnv("ollama.enabled")
	_ = viper.BindEnv("ollama.base_url")
	_ = viper.BindEnv("ollama.default_chat_model")
	_ = viper.BindEnv("ollama.default_embedding_model")

	// Embedded agent configuration.
	_ = viper.BindEnv("server.embedded_agent.enabled")
	_ = viper.BindEnv("server.embedded_agent.backends")
	_ = viper.BindEnv("server.embedded_agent.ollama_url")
	_ = viper.BindEnv("server.embedded_agent.host_affinity")
	_ = viper.BindEnv("server.embedded_agent.gpu_available")
	_ = viper.BindEnv("server.embedded_agent.data_dir")
	_ = viper.BindEnv("server.embedded_agent.sandbox_image")
	_ = viper.BindEnv("server.embedded_agent.sandbox_policy")
	_ = viper.BindEnv("server.embedded_agent.image_registry")
	_ = viper.BindEnv("server.embedded_agent.auto_pull")
}

// parseSpecialEnvVars parses environment variables that viper doesn't handle automatically.
// Viper doesn't convert comma-separated strings to []uint64 or key:value format to maps.
func parseSpecialEnvVars() {
	// Parse QUICBAAS_SERVER_WRITE_TIMEOUT (duration string, e.g. "45s").
	// Viper does not automatically parse duration strings from env vars
	// without an explicit decode hook, so we do it here.
	if val := os.Getenv("QUICBAAS_SERVER_WRITE_TIMEOUT"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			cfg.Server.WriteTimeout = d
		}
	}
	if val := os.Getenv("QUICBAAS_SERVER_READ_TIMEOUT"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			cfg.Server.ReadTimeout = d
		}
	}
	if val := os.Getenv("QUICBAAS_SERVER_IDLE_TIMEOUT"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			cfg.Server.IdleTimeout = d
		}
	}

	// Parse QUICBAAS_RAFT_JOIN_ADDRESSES (comma-separated list of peer addresses).
	// Viper does not automatically split a comma-separated string into a []string
	// slice during Unmarshal, so we do it here. Entries may be plain "host:port"
	// or "id=host:port" format; buildClusterMembers filters for the "id=" prefix.
	if joinEnv := os.Getenv("QUICBAAS_RAFT_JOIN_ADDRESSES"); joinEnv != "" {
		var addrs []string
		for _, a := range strings.Split(joinEnv, ",") {
			a = strings.TrimSpace(a)
			if a != "" {
				addrs = append(addrs, a)
			}
		}
		if len(addrs) > 0 {
			cfg.Raft.JoinAddresses = addrs
		}
	}

	// Parse QUICBAAS_SERVER_SHARDS (comma-separated list of shard IDs).
	if shardsEnv := os.Getenv("QUICBAAS_SERVER_SHARDS"); shardsEnv != "" {
		parts := strings.Split(shardsEnv, ",")
		shards := make([]uint64, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			id, err := strconv.ParseUint(p, 10, 64)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Invalid shard ID %q in QUICBAAS_SERVER_SHARDS: %v\n", p, err)
				os.Exit(1)
			}
			shards = append(shards, id)
		}
		cfg.Server.Shards = shards
	}

	// Parse QUICBAAS_SERVER_ENTITY_SHARD_PINS (comma-separated key:value pairs).
	if pinsEnv := os.Getenv("QUICBAAS_SERVER_ENTITY_SHARD_PINS"); pinsEnv != "" {
		pins := make(map[string]uint64)
		pairs := strings.Split(pinsEnv, ",")
		for _, pair := range pairs {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			parts := strings.SplitN(pair, ":", 2)
			if len(parts) != 2 {
				fmt.Fprintf(os.Stderr, "Invalid entity shard pin %q (expected format: entityType:shardID)\n", pair)
				os.Exit(1)
			}
			entityType := strings.TrimSpace(parts[0])
			shardIDStr := strings.TrimSpace(parts[1])
			shardID, err := strconv.ParseUint(shardIDStr, 10, 64)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Invalid shard ID %q in entity shard pin %q: %v\n", shardIDStr, pair, err)
				os.Exit(1)
			}
			pins[entityType] = shardID
		}
		cfg.Server.EntityShardPins = pins
	}

	// Parse QUICBAAS_ENABLE_SCHEDULER (boolean for scheduler service).
	if val := os.Getenv("QUICBAAS_ENABLE_SCHEDULER"); val != "" {
		lower := strings.ToLower(val)
		cfg.Scheduler.Enabled = lower == "true" || lower == "1" || lower == "yes" || lower == "on"
	}

	// Parse QUICBAAS_TIMESERIES_ENABLED (boolean for timeseries service).
	if val := os.Getenv("QUICBAAS_TIMESERIES_ENABLED"); val != "" {
		lower := strings.ToLower(val)
		cfg.TimeSeries.Enabled = lower == "true" || lower == "1" || lower == "yes" || lower == "on"
	}

	// Parse QUICBAAS_VECTOR_ENABLED (boolean for vector service).
	if val := os.Getenv("QUICBAAS_VECTOR_ENABLED"); val != "" {
		lower := strings.ToLower(val)
		cfg.Vector.Enabled = lower == "true" || lower == "1" || lower == "yes" || lower == "on"
	}

	// Parse QUICBAAS_BACKUP_ENABLED (boolean for backup service).
	if val := os.Getenv("QUICBAAS_BACKUP_ENABLED"); val != "" {
		lower := strings.ToLower(val)
		cfg.Backup.Enabled = lower == "true" || lower == "1" || lower == "yes" || lower == "on"
	}
}

// runStart handles the 'start' command - local PebbleDB mode.
func runStart(cmd *cobra.Command, args []string) error {
	// Use standalone mode with PebbleDB backend (no Raft consensus).
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendPebble
	applyServerFlags(cmd)
	applySealOverride()
	return runDaemon("local")
}

// applySealOverride records whether the operator explicitly supplied a seal
// strategy via config file or environment. When viper reports seal.strategy as
// set, cfg.Seal.StrategyExplicit is flipped on so the configured strategy wins
// over any barrier strategy persisted by `quicbaasd init` in the data dir
// (resolveBarrierStrategy precedence). When unset, the persisted init strategy
// remains authoritative for an already-initialised data dir.
func applySealOverride() {
	if viper.IsSet("seal.strategy") {
		cfg.Seal.StrategyExplicit = true
	}
}

// runNode handles the 'node' command - multi-node Raft cluster.
func runNode(cmd *cobra.Command, args []string) error {
	// Use cluster mode for distributed Raft consensus with PebbleDB storage.
	// Mode determines Raft usage, backend determines storage engine.
	cfg.Server.Mode = config.ServerModeCluster
	cfg.Storage.Backend = config.StorageBackendPebble
	applyServerFlags(cmd)
	applyRaftFlags(cmd)

	// Configure discovery.
	cfg.Discovery.Enabled = true
	if cmd.Flags().Changed("discovery") {
		cfg.Discovery.Type = discoveryType
	} else {
		cfg.Discovery.Type = "multicast" // Default to multicast.
	}

	// Handle static discovery join addresses.
	if cfg.Discovery.Type == "static" && cmd.Flags().Changed("join-addresses") {
		cfg.Discovery.StaticPeers = strings.Split(joinAddresses, ",")
	}

	applySealOverride()
	return runDaemon("node")
}

// runCluster handles the 'cluster' command - single-node Raft cluster.
func runCluster(cmd *cobra.Command, args []string) error {
	// Use cluster mode for single-node Raft cluster with PebbleDB storage.
	// Mode determines Raft usage, backend determines storage engine.
	cfg.Server.Mode = config.ServerModeCluster
	cfg.Storage.Backend = config.StorageBackendPebble
	applyServerFlags(cmd)
	applyRaftFlags(cmd)

	// Single-node Raft cluster doesn't need discovery.
	cfg.Discovery.Enabled = false

	applySealOverride()
	return runDaemon("cluster")
}

// applyServerFlags applies common server flags to config.
func applyServerFlags(cmd *cobra.Command) {
	if cmd.Flags().Changed("unix-socket") {
		cfg.Server.Unix.Socket = unixSocket
		cfg.Server.Unix.Enabled = unixSocket != ""
	}
	if cmd.Flags().Changed("http-port") {
		cfg.Server.REST.Port = httpPort
		cfg.Server.REST.Enabled = httpPort > 0
	}
	if cmd.Flags().Changed("http-address") {
		cfg.Server.Host = httpAddress
	}
	if cmd.Flags().Changed("grpc-port") {
		cfg.Server.GRPC.Port = grpcPort
		cfg.Server.GRPC.Enabled = grpcPort > 0
	}
	if cmd.Flags().Changed("data-dir") {
		cfg.Storage.DataDir = dataDir
		// Relocate the WAL directory under the new data dir unless the operator
		// explicitly pinned storage.wal_dir via env var or config file. viper.IsSet
		// is true when the QUICBAAS_STORAGE_WAL_DIR env var is bound (see BindEnv in
		// initConfig) or when the key is present in a loaded config file, so an
		// explicit wal_dir always wins; --data-dir alone derives it. Without this,
		// the absolute default (/var/lib/quicbaas/wal) survives a --data-dir
		// override and fails the boot-time directory-writable validation.
		if !viper.IsSet("storage.wal_dir") {
			cfg.Storage.WALDir = filepath.Join(cfg.Storage.DataDir, "wal")
		}
	}

	// Apply embedded agent flags.
	if cmd.Flags().Changed("embedded-agent") {
		cfg.Server.EmbeddedAgent.Enabled = embeddedAgentEnabled
	}
	if cmd.Flags().Changed("embedded-agent-backend") {
		cfg.Server.EmbeddedAgent.Backends = embeddedAgentBackends
	}
	if cmd.Flags().Changed("embedded-agent-ollama-url") {
		cfg.Server.EmbeddedAgent.OllamaURL = embeddedAgentOllamaURL
	}
	if cmd.Flags().Changed("embedded-agent-host-affinity") {
		cfg.Server.EmbeddedAgent.HostAffinity = embeddedAgentHostAffinity
	}
	if cmd.Flags().Changed("embedded-agent-gpu") {
		cfg.Server.EmbeddedAgent.GPUAvailable = embeddedAgentGPU
	}
	if cmd.Flags().Changed("embedded-agent-sandbox-image") {
		cfg.Server.EmbeddedAgent.SandboxImage = embeddedAgentSandboxImage
	}
	if cmd.Flags().Changed("embedded-agent-sandbox-policy") {
		cfg.Server.EmbeddedAgent.SandboxPolicy = embeddedAgentSandboxPolicy
	}
	if cmd.Flags().Changed("embedded-agent-sandbox-network") {
		cfg.Server.EmbeddedAgent.SandboxNetwork = embeddedAgentSandboxNet
	}
	if cmd.Flags().Changed("embedded-agent-auto-pull") {
		cfg.Server.EmbeddedAgent.AutoPull = embeddedAgentAutoPull
	}
	if cmd.Flags().Changed("embedded-agent-data-dir") {
		cfg.Server.EmbeddedAgent.DataDir = embeddedAgentDataDir
	} else if cfg.Server.EmbeddedAgent.DataDir == "" {
		// Default data dir for embedded agent: <storage.data_dir>/embedded-agent
		cfg.Server.EmbeddedAgent.DataDir = cfg.Storage.DataDir + "/embedded-agent"
	}
}

// applyRaftFlags applies Raft-specific flags to config.
func applyRaftFlags(cmd *cobra.Command) {
	if cmd.Flags().Changed("node-id") {
		cfg.Raft.NodeID = nodeID
	}
	if raftAddress != "" {
		cfg.Raft.RaftAddress = raftAddress
		cfg.Raft.ListenAddress = raftAddress
	} else if cmd.Flags().Changed("raft-port") {
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "localhost"
		}
		cfg.Raft.RaftAddress = fmt.Sprintf("%s:%d", hostname, raftPort)
		cfg.Raft.ListenAddress = fmt.Sprintf("0.0.0.0:%d", raftPort)
	}
}

// prepareDaemon performs pre-server startup tasks: logging configuration details,
// validating routing config, and creating required directories. This is separated
// from runDaemon to allow unit testing of the startup logic without starting a
// real server that blocks indefinitely.
func prepareDaemon(modeName string) error {
	// Log startup information.
	log.Info("Starting QuicBaaS daemon")
	log.Infof("Version: %s", version.Full())
	log.Infof("Mode: %s", modeName)
	log.Infof("Server mode: %s", cfg.Server.Mode)
	log.Infof("Storage backend: %s", cfg.Storage.Backend)
	log.Infof("Data directory: %s", cfg.Storage.DataDir)

	if cfg.Server.Unix.Enabled {
		log.Infof("Unix socket: %s", cfg.Server.Unix.Socket)
	}

	// Log Raft info if in cluster mode
	if cfg.Server.Mode == config.ServerModeCluster || cfg.Server.Mode == config.ServerModeDevCluster {
		log.Infof("Node ID: %d", cfg.Raft.NodeID)
		log.Infof("Raft address: %s", cfg.Raft.RaftAddress)
	}

	if cfg.Server.GRPC.Enabled {
		log.Infof("gRPC TCP port: %d", cfg.Server.GRPC.Port)
	}
	if cfg.Server.REST.Enabled {
		log.Infof("HTTP REST port: %d", cfg.Server.REST.Port)
	}

	if cfg.Discovery.Enabled {
		log.Infof("Discovery: %s", cfg.Discovery.Type)
	}

	if fipsMode {
		log.Info("FIPS 140-3 mode: enabled")
	}

	// Validate routing configuration early - before creating server.
	// This ensures invalid entity shard pins are rejected at startup.
	if err := validateRoutingConfig(cfg.Server.Shards, cfg.Server.EntityShardPins); err != nil {
		return &dberrors.WrappedMessageError{Message: "invalid routing config", Cause: err}
	}

	// Create data directory if it doesn't exist.
	if err := os.MkdirAll(cfg.Storage.DataDir, 0o755); err != nil {
		return &dberrors.WrappedMessageError{Message: "failed to create data directory", Cause: err}
	}

	// Create Unix socket directory if needed.
	if cfg.Server.Unix.Enabled {
		socketDir := getSocketDir(cfg.Server.Unix.Socket)
		if socketDir != "" {
			if err := os.MkdirAll(socketDir, 0o755); err != nil {
				return &dberrors.WrappedMessageError{Message: "failed to create socket directory", Cause: err}
			}
		}
	}

	return nil
}

// runDaemon runs the daemon with the configured storage backend.
func runDaemon(modeName string) error {
	// Perform pre-server startup tasks.
	if err := prepareDaemon(modeName); err != nil {
		return err
	}

	// Refuse to start on an invalid configuration. This is the boot-time
	// compliance gate: a fail-open posture (require_authz=false with network
	// transports enabled and no insecure_dev_mode acknowledgment) or any other
	// invalid configuration must prevent the daemon from starting.
	if err := config.Validate(cfg); err != nil {
		log.WithError(err).Error("Invalid configuration")
		return err
	}

	// Collect optional server construction options.
	var srvOpts []server.ServerOption

	// When QUICBAAS_SEAL_PASSPHRASE is set and seal is enabled, inject the
	// passphrase so the node auto-initialises and unseals on startup. The
	// passphrase byte slice is zeroed after the server is fully constructed
	// (not before — the WithSealPassphrase option closure holds a reference to
	// the same underlying array, so it must remain valid until NewServer returns).
	// This is the correct injection point for containerised / CI deployments
	// where the passphrase is supplied via the environment rather than an
	// interactive `quicbaasd barrier unseal` call.
	sealPassphrase := os.Getenv("QUICBAAS_SEAL_PASSPHRASE")
	if sealPassphrase != "" && cfg.Seal.Enabled {
		srvOpts = append(srvOpts, server.WithSealPassphrase([]byte(sealPassphrase)))
	}

	// Create server instance. The server's node builder zeros the passphrase
	// slice after consuming it (inside initStandaloneQRDBNode/initClusteredQRDBNode).
	srv, err := server.NewServer(cfg, srvOpts...)
	if err != nil {
		log.WithError(err).Error("Failed to create server")
		return err
	}

	// Set up signal handling for graceful shutdown.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start server in background.
	errChan := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil {
			errChan <- err
		}
	}()

	// Wait for shutdown signal or error.
	select {
	case sig := <-sigChan:
		log.Infof("Received signal %v, initiating shutdown...", sig)
	case err := <-errChan:
		log.WithError(err).Error("Server error")
		return err
	}

	// Graceful shutdown.
	log.Info("Performing graceful shutdown...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.WithError(err).Error("Shutdown error")
		// Flush coverage even on a failed shutdown so the integration-coverage
		// pipeline still captures the daemon-boot profile (no-op in production).
		flushCoverage()
		return err
	}

	// Clean up Unix socket file.
	if cfg.Server.Unix.Enabled && cfg.Server.Unix.Socket != "" {
		_ = os.Remove(cfg.Server.Unix.Socket)
	}

	// Flush the Go coverage profile to GOCOVERDIR (no-op in production builds).
	// Done after Shutdown so the shutdown code paths are included in the profile.
	flushCoverage()

	log.Info("QuicBaaS daemon shutdown complete")
	return nil
}

// validateRoutingConfig validates that all entity shard pins reference valid shards.
// This is called early in startup to fail fast on invalid configuration.
func validateRoutingConfig(shards []uint64, pins map[string]uint64) error {
	if len(pins) == 0 {
		return nil
	}
	shardSet := make(map[uint64]struct{}, len(shards))
	for _, s := range shards {
		shardSet[s] = struct{}{}
	}
	for entityType, shardID := range pins {
		if _, ok := shardSet[shardID]; !ok {
			return &dberrors.MessageError{
				Message: "entity " + entityType + " pinned to shard " + strconv.FormatUint(shardID, 10) + " which is not in configured shards",
			}
		}
	}
	return nil
}

// getSocketDir returns the directory portion of a socket path.
func getSocketDir(socketPath string) string {
	for i := len(socketPath) - 1; i >= 0; i-- {
		if socketPath[i] == '/' {
			return socketPath[:i]
		}
	}
	return ""
}

func main() {
	os.Exit(run())
}

// run executes the root command and returns the process exit code. main must
// not call os.Exit itself: os.Exit skips deferred calls, and the deferred
// secure.Purge below is what wipes mlocked memory on every orderly exit.
func run() int {
	// Arm the secure subsystem so the deferred Purge below wipes all mlocked
	// memory on every orderly exit path, WITHOUT installing memguard's
	// process signal handler. quicbaasd owns its own signal handling:
	// runDaemon registers SIGINT/SIGTERM and drives Server.Shutdown, which
	// closes the embedded qrdb node and therefore its PebbleDB.
	//
	// memguard's CatchSignal handler (installed by secure.CatchInterrupt)
	// wipes memory and then hard os.Exit(1)s from inside the signal handler.
	// That races ahead of — and always beats — the graceful-shutdown path, so
	// the embedded qrdb node was never closed and every PebbleDB write still
	// buffered in the memtable / unsynced WAL block was discarded on SIGTERM.
	// Concretely: IAM entities created through the API (users, roles, groups,
	// apps, invites) vanished across a daemon restart while the already-flushed
	// first-boot bootstrap admin survived. Mirrors the qrdbd pattern
	// (go-qrdb/cmd/qrdbd/main.go), and lets a graceful SIGTERM exit 0 as
	// systemd / docker / k8s expect.
	secure.EnablePurge()
	// In coverage builds, flush the Go coverage profile on SIGINT/SIGTERM.
	// No-op in production builds.
	setupCoverageSignalHook()
	defer func() {
		if err := secure.Purge(); err != nil {
			fmt.Fprintf(os.Stderr, "quicbaasd: secure.Purge: %v\n", err)
		}
	}()

	if err := rootCmd.Execute(); err != nil {
		return 1
	}
	return 0
}
