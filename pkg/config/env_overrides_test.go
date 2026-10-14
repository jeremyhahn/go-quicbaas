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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parsedEnvOverride is one environment override whose value is parsed into a
// non-string field.
type parsedEnvOverride struct {
	name  string
	valid string
	bad   string
	want  any
	got   func(*Config) any
}

var parsedEnvOverrides = []parsedEnvOverride{
	{"QUICBAAS_SERVER_GRPC_ENABLED", "off", "maybe", false, func(c *Config) any { return c.Server.GRPC.Enabled }},
	{"QUICBAAS_SERVER_GRPC_PORT", "19443", "grpc", 19443, func(c *Config) any { return c.Server.GRPC.Port }},
	{"QUICBAAS_SERVER_REST_ENABLED", "no", "nope", false, func(c *Config) any { return c.Server.REST.Enabled }},
	{"QUICBAAS_SERVER_REST_PORT", "18443", "80a", 18443, func(c *Config) any { return c.Server.REST.Port }},
	{"QUICBAAS_SERVER_MCP_ENABLED", "yes", "y", true, func(c *Config) any { return c.Server.MCP.Enabled }},
	{"QUICBAAS_SERVER_MCP_PORT", "18444", "1.5", 18444, func(c *Config) any { return c.Server.MCP.Port }},
	{"QUICBAAS_SERVER_UNIX_ENABLED", "ON", "enable", true, func(c *Config) any { return c.Server.Unix.Enabled }},
	{"QUICBAAS_SECURITY_TLS_CLIENT_AUTH", "true", "ture", true, func(c *Config) any { return c.Security.TLSClientAuth }},
	{"QUICBAAS_SECURITY_REQUIRE_AUTHZ", "1", "2", true, func(c *Config) any { return c.Security.RequireAuthz }},
	{"QUICBAAS_SECURITY_INSECURE_DEV_MODE", "TRUE", "dev", true, func(c *Config) any { return c.Security.InsecureDevMode }},
	{"QUICBAAS_SECURITY_LIVENESS_CACHE_TTL", "45s", "45", 45 * time.Second, func(c *Config) any { return c.Security.LivenessCacheTTL }},
	{"QUICBAAS_RAFT_MUTUAL_TLS", "true", "mtls", true, func(c *Config) any { return c.Raft.MutualTLS }},
	{"QUICBAAS_ENABLE_SCHEDULER", "true", "sometimes", true, func(c *Config) any { return c.Scheduler.Enabled }},
	{"QUICBAAS_SCHEDULER_TICK_INTERVAL", "3s", "3 seconds", 3 * time.Second, func(c *Config) any { return c.Scheduler.TickInterval }},
	{"QUICBAAS_SCHEDULER_MAX_CONCURRENT_JOBS", "7", "seven", 7, func(c *Config) any { return c.Scheduler.MaxConcurrentJobs }},
	{"QUICBAAS_MESSAGING_ENABLED", "true", "t", true, func(c *Config) any { return c.Messaging.Enabled }},
	{"QUICBAAS_MESSAGING_BUFFER_SIZE", "4096", "4k", 4096, func(c *Config) any { return c.Messaging.BufferSize }},
	{"QUICBAAS_MESSAGING_DEDUP_ENABLED", "true", "dedup", true, func(c *Config) any { return c.Messaging.DedupEnabled }},
	{"QUICBAAS_MESSAGING_DEDUP_WINDOW", "90s", "1d", 90 * time.Second, func(c *Config) any { return c.Messaging.DedupWindow }},
	{"QUICBAAS_MESSAGING_WATCH_BRIDGE_ENABLED", "true", "bridge", true, func(c *Config) any { return c.Messaging.WatchBridge.Enabled }},
	{"QUICBAAS_ORCHESTRATION_ENABLED", "true", "orchestrate", true, func(c *Config) any { return c.Orchestration.Enabled }},
	{"QUICBAAS_ORCHESTRATION_HEARTBEAT_TIMEOUT", "11s", "eleven", 11 * time.Second, func(c *Config) any { return c.Orchestration.HeartbeatTimeout }},
	{"QUICBAAS_OBJECTSTORAGE_ENABLED", "true", "s3", true, func(c *Config) any { return c.ObjectStorage.Enabled }},
	{"QUICBAAS_BACKUP_ENABLED", "true", "nightly", true, func(c *Config) any { return c.Backup.Enabled }},
	{"QUICBAAS_BACKUP_TIMEOUT", "2h", "2 hours", 2 * time.Hour, func(c *Config) any { return c.Backup.Timeout }},
	{"QUICBAAS_OLLAMA_ENABLED", "true", "llm", true, func(c *Config) any { return c.Ollama.Enabled }},
	{"QUICBAAS_OLLAMA_TIMEOUT", "40s", "forty", 40 * time.Second, func(c *Config) any { return c.Ollama.Timeout }},
	{"QUICBAAS_OLLAMA_MAX_RETRIES", "5", "five", 5, func(c *Config) any { return c.Ollama.MaxRetries }},
	{"QUICBAAS_OLLAMA_RETRY_BACKOFF", "250ms", "250", 250 * time.Millisecond, func(c *Config) any { return c.Ollama.RetryBackoff }},
	{"QUICBAAS_OLLAMA_HEALTH_CHECK_INTERVAL", "15s", "often", 15 * time.Second, func(c *Config) any { return c.Ollama.HealthCheckInterval }},
}

// loadPaths returns both Load paths that apply the environment overrides: an
// existing (empty) config file and a missing one.
func loadPaths(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	existing := filepath.Join(dir, "quicbaas.yaml")
	require.NoError(t, os.WriteFile(existing, []byte("{}\n"), 0o600))
	return map[string]string{
		"existing file": existing,
		"missing file":  filepath.Join(dir, "missing.yaml"),
	}
}

func TestLoadAppliesValidParsedEnvOverride(t *testing.T) {
	for _, tc := range parsedEnvOverrides {
		for pathName, path := range loadPaths(t) {
			t.Run(tc.name+"/"+pathName, func(t *testing.T) {
				t.Setenv(tc.name, tc.valid)

				cfg, err := NewLoader().Load(path)

				require.NoError(t, err)
				assert.Equal(t, tc.want, tc.got(cfg))
			})
		}
	}
}

func TestLoadRejectsMalformedParsedEnvOverride(t *testing.T) {
	for _, tc := range parsedEnvOverrides {
		for pathName, path := range loadPaths(t) {
			t.Run(tc.name+"/"+pathName, func(t *testing.T) {
				t.Setenv(tc.name, tc.bad)

				cfg, err := NewLoader().Load(path)

				require.Error(t, err)
				assert.Nil(t, cfg)
				assert.ErrorIs(t, err, ErrInvalidEnvValue)
				var envErr *InvalidEnvValueError
				require.True(t, errors.As(err, &envErr), "want InvalidEnvValueError, got %T: %v", err, err)
				assert.Equal(t, tc.name, envErr.Name)
				assert.Equal(t, tc.bad, envErr.Value)
				assert.Contains(t, err.Error(), tc.name)
			})
		}
	}
}

func TestLoadReportsEveryMalformedEnvOverride(t *testing.T) {
	t.Setenv("QUICBAAS_SERVER_GRPC_PORT", "grpc")
	t.Setenv("QUICBAAS_OLLAMA_TIMEOUT", "forty")

	cfg, err := NewLoader().Load(filepath.Join(t.TempDir(), "missing.yaml"))

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "QUICBAAS_SERVER_GRPC_PORT")
	assert.Contains(t, err.Error(), "QUICBAAS_OLLAMA_TIMEOUT")
}

func TestInvalidEnvValueErrorMessage(t *testing.T) {
	err := &InvalidEnvValueError{Name: "QUICBAAS_SERVER_REST_PORT", Value: "80a", Kind: "integer"}

	assert.Equal(t,
		`config: invalid environment variable value: QUICBAAS_SERVER_REST_PORT="80a" is not a valid integer`,
		err.Error())
	assert.ErrorIs(t, err, ErrInvalidEnvValue)
}
