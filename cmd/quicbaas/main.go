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

// Package main is the entry point for the QuicBaaS CLI client.
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/version"
)

var (
	// Global flags.
	cfgFile        string
	logLevel       string
	logFile        string
	fipsMode       bool
	dataDir        string
	address        string
	transport      string
	globalTimeout  time.Duration
	globalTenantID string

	// Global configuration.
	cfg *config.Config
	log *logger.Logger

	// ErrNoCommand is returned when no subcommand is provided.
	ErrNoCommand = errors.New("no command specified")

	// Root command.
	rootCmd = &cobra.Command{
		Use:          "quicbaas",
		Short:        "QuicBaaS CLI - multi-model database, AI platform, and BaaS client",
		SilenceUsage: true,
		Long: `QuicBaaS CLI is the command-line client for the QuicBaaS multi-model distributed
database and Backend-as-a-Service platform.

It connects via gRPC, REST, QUIC, MCP, or Unix socket to manage:

Multi-Model Data Operations:
  - Key-value: get, put, delete, list, transactions
  - Vector: similarity search, index management, embeddings
  - Time-series: write, query, aggregate, downsample
  - Objects: put, get, list, delete with bucket policies

AI and Embedding Operations:
  - RAG pipelines and document ingestion
  - Multi-provider embedding generation (OpenAI, Ollama, etc.)
  - AI agent orchestration and prompt routing

Security and Identity:
  - IAM: users, roles, groups, policies, tenants, federation
  - KMS: key creation, rotation, encryption, decryption
  - Secrets: store, fetch, rotate, delete
  - Barrier: storage seal/unseal lifecycle and certificate management
  - Cryptographic operations (sign, verify, encrypt, decrypt)

Platform Services:
  - Messaging: channels, pub/sub, subscriptions
  - Scheduler: recurring and one-shot job management
  - Cluster: status, node management, rebalancing
  - Backup and restore with compression and encryption
  - Cache management and warming

Default connection: /tmp/quicbaas.sock (gRPC over Unix socket)

Examples:
  # Data operations
  quicbaas data put mykey myvalue
  quicbaas data get mykey

  # Connect to remote server
  quicbaas --address localhost:9090 data get mykey

  # Vector search
  quicbaas vector search --collection docs --query "hello world" --top-k 10

  # AI embeddings
  quicbaas embedding generate "Hello, world!"
  quicbaas rag query "How do I configure logging?" --namespace docs

  # Security operations
  quicbaas iam user list
  quicbaas kms key generate mykey --algorithm AES-256-GCM
  quicbaas secrets put db/password --data password=secret

  # Messaging
  quicbaas msg create-channel orders --buffer-size 512
  quicbaas msg publish orders --payload '{"order_id": "ord_001"}'

  # Scheduler
  quicbaas scheduler job create backup --schedule "0 0 * * *"

For more information, visit: https://github.com/jeremyhahn/go-quicbaas`,
		Version: version.Short(),
		// RunE returns an error when no subcommand is provided
		RunE: func(cmd *cobra.Command, args []string) error {
			return &dberrors.WrappedMessageError{
				Message: "please specify a command (e.g., 'quicbaas data put key value')",
				Cause:   ErrNoCommand,
			}
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Skip init for version command
			if cmd.Name() == "version" {
				return nil
			}

			// Initialize configuration
			if err := initConfig(); err != nil {
				return err
			}

			// Initialize logger
			if err := initLogger(); err != nil {
				return err
			}

			// Initialize tracing (optional)
			shutdown, err := initTracing()
			if err != nil {
				return api.NewOperationFailedError("init tracing", err)
			}
			// On process exit, shutdown tracer provider
			cmd.Context().Done()
			_ = shutdown

			return nil
		},
	}
)

func init() {
	// Global persistent flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.quicbaas.yaml)")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "log level (trace|debug|info|warn|error|fatal|panic)")
	rootCmd.PersistentFlags().StringVar(&logFile, "log-file", "", "log file path (default: stderr; appends to the file when set)")
	rootCmd.PersistentFlags().BoolVar(&fipsMode, "fips", false, "enable FIPS 140-3 mode")
	rootCmd.PersistentFlags().StringVar(&dataDir, "data-dir", "./data", "data directory path")
	rootCmd.PersistentFlags().StringVar(&address, "address", config.DefaultUnixSocket, "QuicBaaS server address (Unix socket path or host:port)")
	rootCmd.PersistentFlags().StringVar(&transport, "transport", "unix", "Transport protocol (grpc, rest, quic, unix, mcp, embedded)")
	rootCmd.PersistentFlags().DurationVar(&globalTimeout, "timeout", 30*time.Second, "Command timeout duration (e.g., 10s, 1m)")
	rootCmd.PersistentFlags().BoolVar(&tlsEnabled, "tls", false, "Enable TLS")
	rootCmd.PersistentFlags().StringVar(&tlsCA, "tls-ca", "", "TLS CA certificate file (auto-detected from data-dir if not set)")
	rootCmd.PersistentFlags().StringVar(&tlsCert, "tls-cert", "", "TLS client certificate file (for mTLS)")
	rootCmd.PersistentFlags().StringVar(&tlsKey, "tls-key", "", "TLS client key file (for mTLS)")
	rootCmd.PersistentFlags().StringVar(&globalTenantID, "tenant", qrdbsdk.SystemTenantID, "Tenant ID for multi-tenant operations")

	// Bind flags to viper
	viper.BindPFlag("logging.level", rootCmd.PersistentFlags().Lookup("log-level"))
	viper.BindPFlag("logging.file", rootCmd.PersistentFlags().Lookup("log-file"))
	viper.BindPFlag("security.fips_mode", rootCmd.PersistentFlags().Lookup("fips"))
	viper.BindPFlag("storage.data_dir", rootCmd.PersistentFlags().Lookup("data-dir"))

	// Add subcommands
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(newDataCommands())
	rootCmd.AddCommand(newClusterCommands())
	rootCmd.AddCommand(newTransactionCommand())
	rootCmd.AddCommand(newBackupCommands())
	rootCmd.AddCommand(newMigrateCommands())
	rootCmd.AddCommand(newRebalanceCommands())
	rootCmd.AddCommand(newNodeCommands())
	rootCmd.AddCommand(newUpgradeCommands())
	rootCmd.AddCommand(newAuthCommand())
	rootCmd.AddCommand(newCacheCommands())

	// Add new KMS, Secrets, and Crypto commands
	rootCmd.AddCommand(newKMSCommand())
	rootCmd.AddCommand(newSecretsCommand())
	// Canonical surface: the StorageBarrier lifecycle is exposed via the
	// "barrier" command group per docs/08-bootstrap-auth-architecture.md §6.
	configureCanonicalSurface()
	rootCmd.AddCommand(newCryptoCommand())

	// Security composition surface: Data Encryption Keys, dynamic secrets,
	// trust artifacts, and ACME. Each tree delegates to the matching
	// Security() sub-service over the active SDK transport.
	rootCmd.AddCommand(newDEKCommand())
	rootCmd.AddCommand(newDynamicSecretsCommand())
	rootCmd.AddCommand(newTrustCommand())
	rootCmd.AddCommand(newACMECommand())

	rootCmd.AddCommand(newSSHCommand())
	rootCmd.AddCommand(newTimeSeriesCommands())
	rootCmd.AddCommand(newVectorCommands())
	rootCmd.AddCommand(newEmbeddingCommands())
	rootCmd.AddCommand(newRAGCommands())
	rootCmd.AddCommand(newOllamaCommands())
	rootCmd.AddCommand(newObjectStorageCommands())

	// Add IAM command (requires -tags=iam to enable full functionality)
	rootCmd.AddCommand(newIAMCommand())

	// Add OAuth2 client management commands (requires -tags=iam)
	rootCmd.AddCommand(newOAuth2Command())

	// Add Scheduler command for job scheduling
	rootCmd.AddCommand(newSchedulerCommand())

	// Add Messaging command for notifications and pub/sub
	rootCmd.AddCommand(newMsgCommands())

	// Add Channel command for MCP channel server
	rootCmd.AddCommand(newChannelCommand())

	// Add Orchestrator command for AI agent orchestration
	rootCmd.AddCommand(newOrchestratorCommand())

	// Add Fleet command for fleet node management
	rootCmd.AddCommand(newFleetCommand())

	// Add Account command for the provider subscription accounts agents run on
	rootCmd.AddCommand(newAccountCommand())
	rootCmd.AddCommand(newRegistryCommand())
	rootCmd.AddCommand(newAttestationCommand())

	// Add Project command for agentic project management
	rootCmd.AddCommand(newProjectCommand())

	// Add the SDLC module command group (forge bindings)
	rootCmd.AddCommand(newSDLCCommand())
	rootCmd.AddCommand(newAgentsCommand())

	// Add MCP command group (stdio bridge, tool discovery, tool invocation)
	rootCmd.AddCommand(newMCPCommand())

	// Signal bridge is gated on the quicbaas_signal build tag; the
	// registerSignalBridgeCommand stub is a no-op when the tag is off.
	registerSignalBridgeCommand(rootCmd)
}

// initConfig reads in config file and ENV variables.
func initConfig() error {
	// Set default configuration
	cfg = config.Default()

	if cfgFile != "" {
		// Use config file from the flag
		viper.SetConfigFile(cfgFile)
	} else {
		// Search for config in home directory and current directory
		home, err := os.UserHomeDir()
		if err != nil {
			return api.NewOperationFailedError("get user home directory", err)
		}

		viper.AddConfigPath(home)
		viper.AddConfigPath(".")
		viper.AddConfigPath("/etc/quicbaas/")
		viper.SetConfigType("yaml")
		viper.SetConfigName(".quicbaas")
	}

	// Environment variable configuration
	// QUICBAAS_SERVER_PORT=8080 maps to server.port
	viper.SetEnvPrefix("QUICBAAS")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// Bind environment variables to config keys
	// This is needed because viper's AutomaticEnv only works with explicitly bound keys

	// Server configuration
	viper.BindEnv("server.host")
	viper.BindEnv("server.expose_shard_header")
	// Protocol configs (nested structure)
	viper.BindEnv("server.grpc.enabled")
	viper.BindEnv("server.grpc.port")
	viper.BindEnv("server.rest.enabled")
	viper.BindEnv("server.rest.port")
	viper.BindEnv("server.quic.enabled")
	viper.BindEnv("server.quic.port")
	viper.BindEnv("server.mcp.enabled")
	viper.BindEnv("server.mcp.port")
	viper.BindEnv("server.unix.enabled")
	viper.BindEnv("server.unix.socket")
	// Note: server.shards and server.entity_shard_pins are complex types
	// that cannot be parsed from environment variables - use config file instead

	// Raft configuration
	viper.BindEnv("raft.node_id")
	viper.BindEnv("raft.raft_address")
	viper.BindEnv("raft.join_addresses")
	viper.BindEnv("raft.listen_address")
	viper.BindEnv("raft.election_rtt")
	viper.BindEnv("raft.heartbeat_rtt")

	// Storage configuration
	viper.BindEnv("storage.data_dir")
	viper.BindEnv("storage.wal_dir")

	// Logging configuration
	viper.BindEnv("logging.level")
	viper.BindEnv("logging.format")
	viper.BindEnv("logging.output")
	viper.BindEnv("logging.file")

	// Security configuration
	viper.BindEnv("security.fips_mode")
	viper.BindEnv("security.tls_enabled")

	// Metrics configuration
	viper.BindEnv("metrics.port")

	// Read config file
	if err := viper.ReadInConfig(); err != nil {
		// Config file not found is acceptable - we'll use defaults
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if !errors.As(err, &configFileNotFoundError) {
			return api.NewConfigurationError("cli", "read config file", err)
		}
	}

	// Unmarshal config into struct
	if err := viper.Unmarshal(cfg); err != nil {
		return api.NewConfigurationError("cli", "unmarshal config", err)
	}

	return nil
}

// buildLoggerConfig constructs a logger.LogConfig from the current global
// CLI state. When --log-file is set, the logger writes to that file
// (append + create); otherwise it writes to stderr so JSON command output
// on stdout stays clean. This helper is shared between initLogger (the CLI
// path) and launchGUI (the Wails path) so both honour --log-file.
func buildLoggerConfig(level, format, file string) logger.LogConfig {
	lc := logger.LogConfig{
		Level:  level,
		Format: format,
	}
	if lc.Level == "" {
		lc.Level = "info"
	}
	if lc.Format == "" {
		lc.Format = "json"
	}
	if file != "" {
		lc.Output = "file"
		lc.File = file
	} else {
		// CLI applications MUST use stderr to avoid polluting stdout
		// which is used for command output (JSON, table, etc.).
		lc.Output = "stderr"
	}
	return lc
}

// initLogger initializes the logger.
// For CLI applications, logs default to stderr to avoid polluting stdout
// which is used for command output (especially JSON). When --log-file is
// set the logger is redirected to that path instead.
func initLogger() error {
	logConfig := buildLoggerConfig(cfg.Logging.Level, cfg.Logging.Format, logFile)

	var err error
	log, err = logger.New(logConfig)
	if err != nil {
		return api.NewOperationFailedError("initialize logger", err)
	}

	return nil
}

// persistentFlagsWithValues lists the long-form persistent flags that
// take a value in the next arg. Used by isGUIOnly to distinguish
// `--log-level debug` (two args, GUI launch) from
// `data put key value` (four args, CLI subcommand). Short forms (`-x`)
// aren't declared at the root, so we only match long-form.
var persistentFlagsWithValues = map[string]struct{}{
	"--config":    {},
	"--log-level": {},
	"--log-file":  {},
	"--data-dir":  {},
	"--address":   {},
	"--transport": {},
	"--timeout":   {},
	"--tls-ca":    {},
	"--tls-cert":  {},
	"--tls-key":   {},
	"--tenant":    {},
}

// isGUIOnly reports whether the invocation has no CLI subcommand and
// should be treated as a GUI launch when a display is present.
//
//	quicbaas                                          → true
//	quicbaas --log-level debug                        → true  (flag+value, no subcommand)
//	quicbaas --log-level=debug --log-file /tmp/x.log  → true  (flag+value x2)
//	quicbaas --fips                                   → true  (bool flag)
//	quicbaas --help / -h / help / --version / version → false (CLI intent)
//	quicbaas data put key value                       → false (subcommand)
//
// Matches the go-qrdb / go-xkms convention so all three projects behave
// consistently when the user runs the client binary with log flags.
func isGUIOnly() bool {
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-h", "--help", "help", "-v", "--version", "version":
			return false
		}
		// --flag=value form — always a single arg, skip.
		if strings.HasPrefix(arg, "--") && strings.Contains(arg, "=") {
			continue
		}
		// --flag <value> form — skip the value arg.
		if _, ok := persistentFlagsWithValues[arg]; ok {
			i++ // consume the value
			continue
		}
		// Any other long or short flag: bool flag, no value.
		if strings.HasPrefix(arg, "-") {
			continue
		}
		// Bare non-flag arg — it's a subcommand (or its positional arg).
		return false
	}
	return true
}

// extractLogFileFlag scans os.Args for --log-file / --log-file=VALUE and
// returns the path if present, or the empty string otherwise. The GUI
// launch path needs the flag value before cobra parses arguments (since
// cobra.Execute runs after the GUI check), so we do a minimal scan here.
// This mirrors the shape cobra would have produced after parse.
func extractLogFileFlag(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--log-file" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		const prefix = "--log-file="
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
	}
	return ""
}

func main() {
	if isGUIOnly() {
		// User intent: launch GUI (no subcommand, no CLI-intent flags).
		if !hasDisplay() {
			// Wanted GUI but no display is visible from this process.
			// Print an actionable hint instead of cobra's generic
			// "please specify a command" fallback.
			fmt.Fprintln(os.Stderr, "quicbaas: cannot launch desktop GUI — no graphical display detected.")
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "The GUI needs one of: $DISPLAY (X11) or $WAYLAND_DISPLAY (Wayland).")
			fmt.Fprintln(os.Stderr, "Fixes (pick what matches your environment):")
			fmt.Fprintln(os.Stderr, "  - Local desktop:  DISPLAY=:0 quicbaas")
			fmt.Fprintln(os.Stderr, "  - SSH session:    ssh -X user@host     (enable X11 forwarding)")
			fmt.Fprintln(os.Stderr, "  - Headless use:   run as CLI instead — e.g. `quicbaas --help`")
			fmt.Fprintln(os.Stderr, "  - Web UI:         start the daemon (`quicbaasd start`) and browse /ui/")
			os.Exit(2)
		}

		// Honour --log-file in GUI mode too so users can pipe Wails
		// logs to a file for debugging. cobra has not yet parsed the
		// flag at this point, so we scan os.Args directly.
		guiLogFile := extractLogFileFlag(os.Args[1:])

		// Initialize minimal logging for GUI mode
		guiLog, logErr := logger.New(buildLoggerConfig("info", "json", guiLogFile))
		if logErr != nil {
			fmt.Fprintf(os.Stderr, "failed to initialize GUI logger: %v\n", logErr)
			os.Exit(1)
		}

		if err := launchGUI(guiLog, guiLogFile); err != nil {
			// If GUI unavailable (no desktop build tag), fall through to CLI
			var guiErr *GUIUnavailableError
			if errors.As(err, &guiErr) {
				fmt.Fprintln(os.Stderr, "quicbaas: this build was compiled without the desktop GUI.")
				fmt.Fprintln(os.Stderr, "Rebuild with `make build` to get the GUI-capable binary, or run a CLI subcommand (see `quicbaas --help`).")
				os.Exit(2)
			}
			fmt.Fprintf(os.Stderr, "GUI error: %v\n", err)
			os.Exit(1)
		}
		return // GUI ran and exited normally
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
