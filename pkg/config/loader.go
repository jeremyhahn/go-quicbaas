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
	"os"
	"strconv"
	"strings"
	"time"

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
			// File doesn't exist, return defaults
			l.applyEnvironmentOverrides(cfg)
			return cfg, nil
		}

		l.v.SetConfigFile(configFile)
		if err := l.v.ReadInConfig(); err != nil {
			return nil, api.NewConfigFileReadError("config.Loader.Load", err)
		}
	}

	// Unmarshal into config struct
	if err := l.v.Unmarshal(cfg); err != nil {
		return nil, api.NewConfigUnmarshalError("config.Loader.Load", err)
	}

	// Apply environment variable overrides for fields that may not be in the config file
	// but have environment variables set. Viper's AutomaticEnv only works during Get()
	// calls, not during Unmarshal for keys not in the config file.
	l.applyEnvironmentOverrides(cfg)

	return cfg, nil
}

// applyEnvironmentOverrides applies environment variable overrides for config fields
// that may not be present in the config file. Viper's AutomaticEnv() only works
// when calling Get() directly, not during Unmarshal for missing keys.
func (l *Loader) applyEnvironmentOverrides(cfg *Config) {
	// Storage backend override
	if val := os.Getenv("QUICBAAS_STORAGE_BACKEND"); val != "" {
		cfg.Storage.Backend = val
	}

	// Server mode override
	if val := os.Getenv("QUICBAAS_SERVER_MODE"); val != "" {
		cfg.Server.Mode = val
	}

	// Server GRPC configuration
	if val := os.Getenv("QUICBAAS_SERVER_GRPC_ENABLED"); val != "" {
		cfg.Server.GRPC.Enabled = parseBool(val)
	}
	if val := os.Getenv("QUICBAAS_SERVER_GRPC_PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			cfg.Server.GRPC.Port = port
		}
	}

	// Server REST configuration
	if val := os.Getenv("QUICBAAS_SERVER_REST_ENABLED"); val != "" {
		cfg.Server.REST.Enabled = parseBool(val)
	}
	if val := os.Getenv("QUICBAAS_SERVER_REST_PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			cfg.Server.REST.Port = port
		}
	}

	// Server QUIC configuration
	if val := os.Getenv("QUICBAAS_SERVER_QUIC_ENABLED"); val != "" {
		cfg.Server.QUIC.Enabled = parseBool(val)
	}
	if val := os.Getenv("QUICBAAS_SERVER_QUIC_PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			cfg.Server.QUIC.Port = port
		}
	}

	// Server MCP configuration
	if val := os.Getenv("QUICBAAS_SERVER_MCP_ENABLED"); val != "" {
		cfg.Server.MCP.Enabled = parseBool(val)
	}
	if val := os.Getenv("QUICBAAS_SERVER_MCP_PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			cfg.Server.MCP.Port = port
		}
	}

	// Server Unix socket configuration
	if val := os.Getenv("QUICBAAS_SERVER_UNIX_ENABLED"); val != "" {
		cfg.Server.Unix.Enabled = parseBool(val)
	}
	if val := os.Getenv("QUICBAAS_SERVER_UNIX_SOCKET"); val != "" {
		cfg.Server.Unix.Socket = val
	}

	// Security configuration
	if val := os.Getenv("QUICBAAS_SECURITY_TLS_ENABLED"); val != "" {
		cfg.Security.TLSEnabled = parseBool(val)
	}
	if val := os.Getenv("QUICBAAS_SECURITY_TLS_CLIENT_AUTH"); val != "" {
		cfg.Security.TLSClientAuth = parseBool(val)
	}
	if val := os.Getenv("QUICBAAS_SECURITY_REQUIRE_AUTHZ"); val != "" {
		cfg.Security.RequireAuthz = parseBool(val)
	}

	// Raft configuration
	if val := os.Getenv("QUICBAAS_RAFT_MUTUAL_TLS"); val != "" {
		cfg.Raft.MutualTLS = parseBool(val)
	}
	if val := os.Getenv("QUICBAAS_RAFT_TLS_CERT_FILE"); val != "" {
		cfg.Raft.TLSCertFile = val
	}
	if val := os.Getenv("QUICBAAS_RAFT_TLS_KEY_FILE"); val != "" {
		cfg.Raft.TLSKeyFile = val
	}
	if val := os.Getenv("QUICBAAS_RAFT_TLS_CA_FILE"); val != "" {
		cfg.Raft.TLSCAFile = val
	}

	// Scheduler configuration
	if val := os.Getenv("QUICBAAS_ENABLE_SCHEDULER"); val != "" {
		cfg.Scheduler.Enabled = parseBool(val)
	}
	if val := os.Getenv("QUICBAAS_SCHEDULER_TICK_INTERVAL"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			cfg.Scheduler.TickInterval = duration
		}
	}
	if val := os.Getenv("QUICBAAS_SCHEDULER_MAX_CONCURRENT_JOBS"); val != "" {
		if maxJobs, err := strconv.Atoi(val); err == nil {
			cfg.Scheduler.MaxConcurrentJobs = maxJobs
		}
	}

	// Messaging configuration
	if val := os.Getenv("QUICBAAS_MESSAGING_ENABLED"); val != "" {
		cfg.Messaging.Enabled = parseBool(val)
	}
	if val := os.Getenv("QUICBAAS_MESSAGING_BUFFER_SIZE"); val != "" {
		if size, err := strconv.Atoi(val); err == nil {
			cfg.Messaging.BufferSize = size
		}
	}
	if val := os.Getenv("QUICBAAS_MESSAGING_DEDUP_ENABLED"); val != "" {
		cfg.Messaging.DedupEnabled = parseBool(val)
	}
	if val := os.Getenv("QUICBAAS_MESSAGING_DEDUP_WINDOW"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			cfg.Messaging.DedupWindow = duration
		}
	}
	if val := os.Getenv("QUICBAAS_MESSAGING_WATCH_BRIDGE_ENABLED"); val != "" {
		cfg.Messaging.WatchBridge.Enabled = parseBool(val)
	}

	// Orchestration configuration
	if val := os.Getenv("QUICBAAS_ORCHESTRATION_ENABLED"); val != "" {
		cfg.Orchestration.Enabled = parseBool(val)
	}
	if val := os.Getenv("QUICBAAS_ORCHESTRATION_LEAD_AGENT_BACKEND"); val != "" {
		cfg.Orchestration.LeadAgent.Backend = val
	}
	if val := os.Getenv("QUICBAAS_ORCHESTRATION_LEAD_AGENT_MODEL"); val != "" {
		cfg.Orchestration.LeadAgent.Model = val
	}
	if val := os.Getenv("QUICBAAS_ORCHESTRATION_HEARTBEAT_TIMEOUT"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			cfg.Orchestration.HeartbeatTimeout = duration
		}
	}
	// Object Storage configuration
	if val := os.Getenv("QUICBAAS_OBJECTSTORAGE_ENABLED"); val != "" {
		cfg.ObjectStorage.Enabled = parseBool(val)
	}

	// Backup configuration
	if val := os.Getenv("QUICBAAS_BACKUP_ENABLED"); val != "" {
		cfg.Backup.Enabled = parseBool(val)
	}
	if val := os.Getenv("QUICBAAS_BACKUP_STORAGE_BACKEND"); val != "" {
		cfg.Backup.StorageBackend = val
	}
	if val := os.Getenv("QUICBAAS_BACKUP_WORK_DIR"); val != "" {
		cfg.Backup.WorkDir = val
	}
	if val := os.Getenv("QUICBAAS_BACKUP_DEFAULT_COMPRESSION"); val != "" {
		cfg.Backup.DefaultCompression = val
	}
	if val := os.Getenv("QUICBAAS_BACKUP_DEFAULT_ENCRYPTION"); val != "" {
		cfg.Backup.DefaultEncryption = val
	}
	if val := os.Getenv("QUICBAAS_BACKUP_TIMEOUT"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			cfg.Backup.Timeout = duration
		}
	}

	// Ollama configuration
	if val := os.Getenv("QUICBAAS_OLLAMA_ENABLED"); val != "" {
		cfg.Ollama.Enabled = parseBool(val)
	}
	if val := os.Getenv("QUICBAAS_OLLAMA_BASE_URL"); val != "" {
		cfg.Ollama.BaseURL = val
	}
	if val := os.Getenv("QUICBAAS_OLLAMA_DEFAULT_EMBEDDING_MODEL"); val != "" {
		cfg.Ollama.DefaultEmbeddingModel = val
	}
	if val := os.Getenv("QUICBAAS_OLLAMA_TIMEOUT"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			cfg.Ollama.Timeout = duration
		}
	}
	if val := os.Getenv("QUICBAAS_OLLAMA_MAX_RETRIES"); val != "" {
		if retries, err := strconv.Atoi(val); err == nil {
			cfg.Ollama.MaxRetries = retries
		}
	}
	if val := os.Getenv("QUICBAAS_OLLAMA_RETRY_BACKOFF"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			cfg.Ollama.RetryBackoff = duration
		}
	}
	if val := os.Getenv("QUICBAAS_OLLAMA_HEALTH_CHECK_INTERVAL"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			cfg.Ollama.HealthCheckInterval = duration
		}
	}
}

// parseBool parses a string to boolean, supporting common boolean representations.
func parseBool(val string) bool {
	lower := strings.ToLower(val)
	return lower == "true" || lower == "1" || lower == "yes" || lower == "on"
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
