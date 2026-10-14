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

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// Loader handles configuration loading from multiple sources.
type Loader struct {
	v *viper.Viper
}

// NewLoader creates a new configuration loader.
func NewLoader() *Loader {
	return &Loader{
		v: viper.New(),
	}
}

// Load loads configuration from files, environment variables, and CLI flags
// Priority: CLI flags > Environment variables > Config file > Defaults.
// If the config file doesn't exist, falls back to defaults instead of returning an error.
func (l *Loader) Load(configFile string) (*Config, error) {
	// Start with defaults
	cfg := Default()

	// Configure viper
	l.v.SetConfigType("yaml")
	l.v.SetEnvPrefix("QUICBAAS")
	l.v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	l.v.AutomaticEnv()
	BindSDLCEnv(l.v)
	BindSDLCSurfacingEnv(l.v)
	BindSDLCLabelReconcileEnv(l.v)
	BindSDLCWorkerImageEnv(l.v)
	BindIAMSelfServiceRegistrationEnv(l.v)
	BindStorageDirEnv(l.v)
	BindDevClusterHost(l.v)
	BindAddressEnv(l.v)

	// Handle empty path - return defaults
	if configFile == "" {
		// Try default config file locations
		l.v.SetConfigName("quicbaas")
		l.v.AddConfigPath(".")
		l.v.AddConfigPath("/etc/quicbaas")
		l.v.AddConfigPath("$HOME/.quicbaas")

		// Ignore error if no config file found in default locations
		_ = l.v.ReadInConfig()
	} else {
		// Check if file exists before trying to load
		if _, err := os.Stat(configFile); os.IsNotExist(err) {
			// FILE DOESN'T EXIST, SO THE FILE LAYER IS EMPTY — the ENVIRONMENT
			// layer is not, and this path used to skip it.
			//
			// Every key that reaches the config through a viper binding
			// (BindSDLCSurfacingEnv) is decoded by Unmarshal, which the early
			// return below it never reached, while applyEnvironmentOverrides
			// only covers the hand-maintained list of keys it names. So
			// `Load("/missing")` silently returned defaults for every bound
			// duration and the documented precedence — environment over
			// defaults — was false on this one path.
			//
			// Measured: with QUICBAAS_SDLC_SURFACING_RETRY_INTERVAL=7m,
			// Load("/missing") returned 2m while Load("") returned 7m.
			if err := RefuseRemovedKeys(l.v); err != nil {
				return nil, err
			}
			if err := l.v.Unmarshal(cfg); err != nil {
				return nil, api.NewConfigUnmarshalError("config.Loader.Load", err)
			}
			// Same defence in depth as the loaded path: no decoding route may
			// turn TLS off.
			cfg.Security.TLSEnabled = true
			if err := l.applyEnvironmentOverrides(cfg); err != nil {
				return nil, err
			}
			DeriveWALDir(l.v, cfg)
			return cfg, nil
		}

		l.v.SetConfigFile(configFile)
		if err := l.v.ReadInConfig(); err != nil {
			return nil, api.NewConfigFileReadError("config.Loader.Load", err)
		}
	}

	// An sdlc: key the module no longer reads is refused by name before the
	// decode, so a setting that silently stopped applying cannot pass.
	if err := RefuseRemovedKeys(l.v); err != nil {
		return nil, err
	}

	// Unmarshal into config struct
	if err := l.v.Unmarshal(cfg); err != nil {
		return nil, api.NewConfigUnmarshalError("config.Loader.Load", err)
	}

	// TLS is required for EVERYTHING: no config file or environment value
	// may disable it. The mapstructure binding for security.tls_enabled was
	// removed; this line is defence in depth for any future decoding path.
	cfg.Security.TLSEnabled = true

	// Apply environment variable overrides for fields that may not be in the config file
	// but have environment variables set. Viper's AutomaticEnv only works during Get()
	// calls, not during Unmarshal for keys not in the config file.
	if err := l.applyEnvironmentOverrides(cfg); err != nil {
		return nil, err
	}
	DeriveWALDir(l.v, cfg)

	return cfg, nil
}

// walSubdir is the directory under storage.data_dir that holds the WAL when
// storage.wal_dir is not set explicitly.
const walSubdir = "wal"

// BindStorageDirEnv binds QUICBAAS_STORAGE_DATA_DIR and
// QUICBAAS_STORAGE_WAL_DIR, so an environment-only value is decoded by
// Unmarshal and observed by viper.IsSet in DeriveWALDir.
func BindStorageDirEnv(v *viper.Viper) {
	_ = v.BindEnv("storage.data_dir")
	_ = v.BindEnv("storage.wal_dir")
}

// DeriveWALDir places the WAL at <storage.data_dir>/wal when storage.data_dir
// was set by any source (flag bound to the key, environment or config file)
// and storage.wal_dir was not. An explicit storage.wal_dir always wins, and
// when neither key is set the defaults-layer pair is kept. Call it once, after
// every source has been decoded into cfg.
func DeriveWALDir(v *viper.Viper, cfg *Config) {
	if !v.IsSet("storage.data_dir") || v.IsSet("storage.wal_dir") {
		return
	}
	if cfg.Storage.DataDir == "" {
		return
	}
	cfg.Storage.WALDir = filepath.Join(cfg.Storage.DataDir, walSubdir)
}

// applyEnvironmentOverrides applies environment variable overrides for config fields
// that may not be present in the config file. Viper's AutomaticEnv() only works
// when calling Get() directly, not during Unmarshal for missing keys. A value
// that does not parse as its field's type is an *InvalidEnvValueError naming
// the variable; every such value is reported, joined.
func (l *Loader) applyEnvironmentOverrides(cfg *Config) error {
	// String values cannot be malformed.
	envString("QUICBAAS_STORAGE_BACKEND", &cfg.Storage.Backend)
	envString("QUICBAAS_SERVER_MODE", &cfg.Server.Mode)
	// Sealed-window bind host (pre-unseal ceremony/unseal listeners)
	envString("QUICBAAS_SERVER_SEALED_BIND_HOST", &cfg.Server.SealedBindHost)
	envString("QUICBAAS_SERVER_UNIX_SOCKET", &cfg.Server.Unix.Socket)
	envString("QUICBAAS_SECURITY_AUTHZ_MODE", &cfg.Security.AuthzMode)
	envString("QUICBAAS_RAFT_TLS_CERT_FILE", &cfg.Raft.TLSCertFile)
	envString("QUICBAAS_RAFT_TLS_KEY_FILE", &cfg.Raft.TLSKeyFile)
	envString("QUICBAAS_RAFT_TLS_CA_FILE", &cfg.Raft.TLSCAFile)
	envString("QUICBAAS_ORCHESTRATION_LEAD_AGENT_AGENT_ID", &cfg.Orchestration.LeadAgent.AgentID)
	envString("QUICBAAS_ORCHESTRATION_LEAD_AGENT_POOL_ID", &cfg.Orchestration.LeadAgent.PoolID)
	envString("QUICBAAS_BACKUP_STORAGE_BACKEND", &cfg.Backup.StorageBackend)
	envString("QUICBAAS_BACKUP_WORK_DIR", &cfg.Backup.WorkDir)
	envString("QUICBAAS_BACKUP_DEFAULT_COMPRESSION", &cfg.Backup.DefaultCompression)
	envString("QUICBAAS_BACKUP_DEFAULT_ENCRYPTION", &cfg.Backup.DefaultEncryption)
	envString("QUICBAAS_OLLAMA_BASE_URL", &cfg.Ollama.BaseURL)
	envString("QUICBAAS_OLLAMA_DEFAULT_EMBEDDING_MODEL", &cfg.Ollama.DefaultEmbeddingModel)

	// Every malformed parsed value is collected, so the operator sees all of
	// them in one failed start instead of fixing one per restart.
	return errors.Join(
		// Server listeners
		EnvBool("QUICBAAS_SERVER_GRPC_ENABLED", &cfg.Server.GRPC.Enabled),
		envInt("QUICBAAS_SERVER_GRPC_PORT", &cfg.Server.GRPC.Port),
		EnvBool("QUICBAAS_SERVER_REST_ENABLED", &cfg.Server.REST.Enabled),
		envInt("QUICBAAS_SERVER_REST_PORT", &cfg.Server.REST.Port),
		EnvBool("QUICBAAS_SERVER_MCP_ENABLED", &cfg.Server.MCP.Enabled),
		envInt("QUICBAAS_SERVER_MCP_PORT", &cfg.Server.MCP.Port),
		EnvBool("QUICBAAS_SERVER_UNIX_ENABLED", &cfg.Server.Unix.Enabled),

		// Security. There is deliberately NO override for
		// security.tls_enabled: TLS is required for everything and cannot be
		// disabled through configuration.
		EnvBool("QUICBAAS_SECURITY_TLS_CLIENT_AUTH", &cfg.Security.TLSClientAuth),
		EnvBool("QUICBAAS_SECURITY_REQUIRE_AUTHZ", &cfg.Security.RequireAuthz),
		EnvBool("QUICBAAS_SECURITY_INSECURE_DEV_MODE", &cfg.Security.InsecureDevMode),
		EnvDuration("QUICBAAS_SECURITY_LIVENESS_CACHE_TTL", &cfg.Security.LivenessCacheTTL),

		// Raft
		EnvBool("QUICBAAS_RAFT_MUTUAL_TLS", &cfg.Raft.MutualTLS),

		// Scheduler
		EnvBool("QUICBAAS_ENABLE_SCHEDULER", &cfg.Scheduler.Enabled),
		EnvDuration("QUICBAAS_SCHEDULER_TICK_INTERVAL", &cfg.Scheduler.TickInterval),
		envInt("QUICBAAS_SCHEDULER_MAX_CONCURRENT_JOBS", &cfg.Scheduler.MaxConcurrentJobs),

		// Messaging
		EnvBool("QUICBAAS_MESSAGING_ENABLED", &cfg.Messaging.Enabled),
		envInt("QUICBAAS_MESSAGING_BUFFER_SIZE", &cfg.Messaging.BufferSize),
		EnvBool("QUICBAAS_MESSAGING_DEDUP_ENABLED", &cfg.Messaging.DedupEnabled),
		EnvDuration("QUICBAAS_MESSAGING_DEDUP_WINDOW", &cfg.Messaging.DedupWindow),
		EnvBool("QUICBAAS_MESSAGING_WATCH_BRIDGE_ENABLED", &cfg.Messaging.WatchBridge.Enabled),

		// Orchestration
		EnvBool("QUICBAAS_ORCHESTRATION_ENABLED", &cfg.Orchestration.Enabled),
		EnvDuration("QUICBAAS_ORCHESTRATION_HEARTBEAT_TIMEOUT", &cfg.Orchestration.HeartbeatTimeout),

		// Object storage and backup
		EnvBool("QUICBAAS_OBJECTSTORAGE_ENABLED", &cfg.ObjectStorage.Enabled),
		EnvBool("QUICBAAS_BACKUP_ENABLED", &cfg.Backup.Enabled),
		EnvDuration("QUICBAAS_BACKUP_TIMEOUT", &cfg.Backup.Timeout),

		// Ollama
		EnvBool("QUICBAAS_OLLAMA_ENABLED", &cfg.Ollama.Enabled),
		EnvDuration("QUICBAAS_OLLAMA_TIMEOUT", &cfg.Ollama.Timeout),
		envInt("QUICBAAS_OLLAMA_MAX_RETRIES", &cfg.Ollama.MaxRetries),
		EnvDuration("QUICBAAS_OLLAMA_RETRY_BACKOFF", &cfg.Ollama.RetryBackoff),
		EnvDuration("QUICBAAS_OLLAMA_HEALTH_CHECK_INTERVAL", &cfg.Ollama.HealthCheckInterval),
	)
}

// LoadWithDefaults loads configuration with default values only.
// This provides a complete, valid configuration for zero-config startup.
func (l *Loader) LoadWithDefaults() *Config {
	return Default()
}

// BindFlags binds cobra command flags to viper configuration.
func (l *Loader) BindFlags(cmd *cobra.Command) error {
	// Server flags
	if err := l.bindServerFlags(cmd); err != nil {
		return err
	}

	// Storage flags
	if err := l.bindStorageFlags(cmd); err != nil {
		return err
	}

	// Security flags
	if err := l.bindSecurityFlags(cmd); err != nil {
		return err
	}

	// Raft flags
	if err := l.bindRaftFlags(cmd); err != nil {
		return err
	}

	// Logging flags
	if err := l.bindLoggingFlags(cmd); err != nil {
		return err
	}

	return nil
}

// bindServerFlags binds server-related flags.
func (l *Loader) bindServerFlags(cmd *cobra.Command) error {
	flags := []string{
		"server.host",
		"server.grpc-port",
		"server.http-port",
	}
	for _, flag := range flags {
		if f := cmd.Flags().Lookup(flag); f != nil {
			if err := l.v.BindPFlag(flag, f); err != nil {
				return api.NewFlagBindError("config.Loader.bindServerFlags", flag, err)
			}
		}
	}
	return nil
}

// bindStorageFlags binds storage-related flags.
func (l *Loader) bindStorageFlags(cmd *cobra.Command) error {
	flags := []string{
		"storage.data-dir",
		"storage.wal-dir",
		"storage.backend",
	}
	for _, flag := range flags {
		if f := cmd.Flags().Lookup(flag); f != nil {
			if err := l.v.BindPFlag(flag, f); err != nil {
				return api.NewFlagBindError("config.Loader.bindStorageFlags", flag, err)
			}
		}
	}
	return nil
}

// bindSecurityFlags binds security-related flags.
func (l *Loader) bindSecurityFlags(cmd *cobra.Command) error {
	flags := []string{
		"security.tls-enabled",
		"security.tls-cert-file",
		"security.tls-key-file",
	}
	for _, flag := range flags {
		if f := cmd.Flags().Lookup(flag); f != nil {
			if err := l.v.BindPFlag(flag, f); err != nil {
				return api.NewFlagBindError("config.Loader.bindSecurityFlags", flag, err)
			}
		}
	}
	return nil
}

// bindRaftFlags binds Raft-related flags.
func (l *Loader) bindRaftFlags(cmd *cobra.Command) error {
	flags := []string{
		"raft.node-id",
		"raft.data-dir",
		"raft.raft-address",
	}
	for _, flag := range flags {
		if f := cmd.Flags().Lookup(flag); f != nil {
			if err := l.v.BindPFlag(flag, f); err != nil {
				return api.NewFlagBindError("config.Loader.bindRaftFlags", flag, err)
			}
		}
	}
	return nil
}

// bindLoggingFlags binds logging-related flags.
func (l *Loader) bindLoggingFlags(cmd *cobra.Command) error {
	flags := []string{
		"logging.level",
		"logging.format",
		"logging.output",
		"logging.file",
	}
	for _, flag := range flags {
		if f := cmd.Flags().Lookup(flag); f != nil {
			if err := l.v.BindPFlag(flag, f); err != nil {
				return api.NewFlagBindError("config.Loader.bindLoggingFlags", flag, err)
			}
		}
	}
	return nil
}

// SaveToFile saves the configuration to a YAML file at the specified path.
func (l *Loader) SaveToFile(cfg *Config, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return api.NewConfigMarshalError("config.Loader.SaveToFile", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return api.NewConfigFileWriteError("config.Loader.SaveToFile", path, err)
	}
	return nil
}

// GetViper returns the underlying viper instance for advanced configuration access.
func (l *Loader) GetViper() *viper.Viper {
	return l.v
}

// MergeConfig merges two configurations. If current is non-nil, it returns a clone
// of current. If current is nil, it returns a clone of other. If both are nil,
// it returns the default configuration.
func (l *Loader) MergeConfig(other, current *Config) *Config {
	if current != nil {
		return current.Clone()
	}
	if other != nil {
		return other.Clone()
	}
	return Default()
}

// LoadFromFile is a convenience function to load configuration from a specific file.
func LoadFromFile(filename string) (*Config, error) {
	loader := NewLoader()
	return loader.Load(filename)
}

// LoadFromEnv loads configuration from environment variables only, using defaults
// as a base. Environment variables with the QUICBAAS_ prefix override defaults.
func LoadFromEnv() (*Config, error) {
	loader := NewLoader()
	return loader.Load("")
}

// LoadWithDefaults is a package-level convenience function that returns default
// configuration values.
func LoadWithDefaults() (*Config, error) {
	return Default(), nil
}

// Load is the package-level function for loading configuration.
// It tries to load from the given path, falling back to defaults if the file doesn't exist.
func Load(path string) (*Config, error) {
	loader := NewLoader()
	return loader.Load(path)
}
