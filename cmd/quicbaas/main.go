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
	fipsMode       bool
	dataDir        string
	address        string
	transport      string
	globalTimeout  time.Duration
	tlsSkipVerify  bool
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
  - Seal/unseal and certificate management
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
  quicbaas rag ingest --collection docs --file paper.pdf

  # Security operations
  quicbaas iam user list
  quicbaas kms create-key --algorithm aes-256-gcm
  quicbaas secrets store db/password --data password=secret

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
	rootCmd.PersistentFlags().BoolVar(&fipsMode, "fips", false, "enable FIPS 140-3 mode")
	rootCmd.PersistentFlags().StringVar(&dataDir, "data-dir", "./data", "data directory path")
	rootCmd.PersistentFlags().StringVar(&address, "address", config.DefaultUnixSocket, "QuicBaaS server address (Unix socket path or host:port)")
	rootCmd.PersistentFlags().StringVar(&transport, "transport", "grpc", "Transport protocol (grpc, rest, quic, unix, mcp, embedded)")
	rootCmd.PersistentFlags().DurationVar(&globalTimeout, "timeout", 30*time.Second, "Command timeout duration (e.g., 10s, 1m)")
	rootCmd.PersistentFlags().BoolVar(&tlsEnabled, "tls", false, "Enable TLS")
	rootCmd.PersistentFlags().BoolVar(&tlsSkipVerify, "tls-skip-verify", false, "Skip TLS certificate verification (insecure)")
	rootCmd.PersistentFlags().StringVar(&tlsCA, "tls-ca", "", "TLS CA certificate file (auto-detected from data-dir if not set)")
	rootCmd.PersistentFlags().StringVar(&tlsCert, "tls-cert", "", "TLS client certificate file (for mTLS)")
	rootCmd.PersistentFlags().StringVar(&tlsKey, "tls-key", "", "TLS client key file (for mTLS)")
	rootCmd.PersistentFlags().StringVar(&globalTenantID, "tenant", "default", "Tenant ID for multi-tenant operations")

	// Bind flags to viper
	viper.BindPFlag("logging.level", rootCmd.PersistentFlags().Lookup("log-level"))
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
	rootCmd.AddCommand(newUserCommand())
	rootCmd.AddCommand(newRoleCommand())
	rootCmd.AddCommand(newCacheCommands())

	// Add new KMS, Secrets, Seal, and Crypto commands
	rootCmd.AddCommand(newKMSCommand())
	rootCmd.AddCommand(newSecretsCommand())
	rootCmd.AddCommand(newSealCommand())
	rootCmd.AddCommand(newCryptoCommand())
	rootCmd.AddCommand(newTimeSeriesCommands())
	rootCmd.AddCommand(newVectorCommands())
	rootCmd.AddCommand(newEmbeddingCommands())
	rootCmd.AddCommand(newRAGCommands())
	rootCmd.AddCommand(newOllamaCommands())
	rootCmd.AddCommand(newObjectStorageCommands())

	// Add IAM command (requires -tags=iam to enable full functionality)
	rootCmd.AddCommand(newIAMCommand())

	// Add Scheduler command for job scheduling
	rootCmd.AddCommand(newSchedulerCommand())

	// Add Messaging command for notifications and pub/sub
	rootCmd.AddCommand(newMsgCommands())

	// Add Channel command for MCP channel server
	rootCmd.AddCommand(newChannelCommand())

	// Add Orchestrator command for AI agent orchestration
	rootCmd.AddCommand(newOrchestratorCommand())

	// Add Agent command for fleet management
	rootCmd.AddCommand(newAgentCommand())
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

// initLogger initializes the logger.
// For CLI applications, logs should go to stderr to avoid polluting stdout
// which is used for command output (especially JSON).
func initLogger() error {
	logConfig := logger.LogConfig{
		Level:  cfg.Logging.Level,
		Format: cfg.Logging.Format,
		// CLI applications MUST use stderr to avoid polluting stdout
		// which is used for command output (JSON, table, etc.)
		// This overrides any config file setting.
		Output: "stderr",
	}

	if logConfig.Level == "" {
		logConfig.Level = "info"
	}
	if logConfig.Format == "" {
		logConfig.Format = "json"
	}

	var err error
	log, err = logger.New(logConfig)
	if err != nil {
		return api.NewOperationFailedError("initialize logger", err)
	}

	return nil
}

// isGUIOnly returns true when no subcommand was provided.
// It checks os.Args to determine if only global flags (or nothing) were passed.
func isGUIOnly() bool {
	if len(os.Args) <= 1 {
		return true
	}
	// Check if all args are flags (start with -)
	for _, arg := range os.Args[1:] {
		if !strings.HasPrefix(arg, "-") {
			return false
		}
	}
	return true
}

func main() {
	// If no subcommand provided and display available, try GUI
	if isGUIOnly() && hasDisplay() {
		// Initialize minimal logging for GUI mode
		guiLog, logErr := logger.New(logger.LogConfig{
			Level:  "info",
			Format: "json",
			Output: "stderr",
		})
		if logErr != nil {
			fmt.Fprintf(os.Stderr, "failed to initialize GUI logger: %v\n", logErr)
			os.Exit(1)
		}

		if err := launchGUI(guiLog, ""); err != nil {
			// If GUI unavailable (no desktop build tag), fall through to CLI
			var guiErr *GUIUnavailableError
			if errors.As(err, &guiErr) {
				// Fall through to CLI
			} else {
				fmt.Fprintf(os.Stderr, "GUI error: %v\n", err)
				os.Exit(1)
			}
		} else {
			return // GUI ran and exited normally
		}
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
