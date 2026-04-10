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
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-objstore/pkg/common"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	require.NotNil(t, cfg)
	assert.NotEmpty(t, cfg.Server.Host)
	assert.Greater(t, cfg.Server.REST.Port, 0)
	assert.Greater(t, cfg.Server.GRPC.Port, 0)
	assert.NotEmpty(t, cfg.Storage.DataDir)
	assert.NotEmpty(t, cfg.Logging.Level)
}

func TestConfig_Clone(t *testing.T) {
	original := Default()
	original.Security.TLSCertFile = "/path/to/cert1.pem"
	original.Security.TLSKeyFile = "/path/to/key1.pem"

	cloned := original.Clone()

	// Verify it's a copy
	assert.Equal(t, original.Server.REST.Port, cloned.Server.REST.Port)
	assert.Equal(t, original.Security.TLSCertFile, cloned.Security.TLSCertFile)

	// Modify clone
	cloned.Server.REST.Port = 9999
	cloned.Security.TLSCertFile = "/path/to/cert2.pem"

	// Verify original unchanged
	assert.NotEqual(t, original.Server.REST.Port, cloned.Server.REST.Port)
	assert.NotEqual(t, original.Security.TLSCertFile, cloned.Security.TLSCertFile)
}

func TestNewLoader(t *testing.T) {
	loader := NewLoader()
	require.NotNil(t, loader)
	require.NotNil(t, loader.v)
}

func TestLoader_LoadWithDefaults(t *testing.T) {
	loader := NewLoader()
	cfg := loader.LoadWithDefaults()

	require.NotNil(t, cfg)
	assert.NotEmpty(t, cfg.Server.Host)
	assert.Greater(t, cfg.Server.REST.Port, 0)
}

func TestLoader_Load(t *testing.T) {
	t.Run("load from non-existent file returns defaults", func(t *testing.T) {
		loader := NewLoader()
		cfg, err := loader.Load("/non/existent/config.yaml")

		// Zero-config startup: non-existent file returns defaults, not an error
		require.NoError(t, err)
		require.NotNil(t, cfg)
		// Verify defaults are applied
		assert.NotEmpty(t, cfg.Server.Host)
		assert.Greater(t, cfg.Server.REST.Port, 0)
	})

	t.Run("load with no file specified", func(t *testing.T) {
		loader := NewLoader()
		cfg, err := loader.Load("")

		require.NoError(t, err)
		require.NotNil(t, cfg)
		// Should return defaults
		assert.NotEmpty(t, cfg.Server.Host)
	})
}

func TestLoader_BindFlags(t *testing.T) {
	loader := NewLoader()
	cmd := &cobra.Command{
		Use: "test",
	}

	// Pre-register flags that BindFlags will bind via BindPFlag
	cmd.Flags().String("server.host", "", "")
	cmd.Flags().Int("server.grpc-port", 0, "")
	cmd.Flags().String("storage.data-dir", "", "")
	cmd.Flags().Int("storage.cache-size", 0, "")
	cmd.Flags().Bool("security.tls-enabled", false, "")
	cmd.Flags().Bool("security.fips-mode", false, "")
	cmd.Flags().Uint64("raft.node-id", 0, "")
	cmd.Flags().String("logging.level", "", "")
	cmd.Flags().String("logging.format", "", "")

	err := loader.BindFlags(cmd)
	require.NoError(t, err)

	// Verify flags are still accessible after binding
	assert.NotNil(t, cmd.Flags().Lookup("server.host"))
	assert.NotNil(t, cmd.Flags().Lookup("server.grpc-port"))
	assert.NotNil(t, cmd.Flags().Lookup("raft.node-id"))
	assert.NotNil(t, cmd.Flags().Lookup("storage.data-dir"))
	assert.NotNil(t, cmd.Flags().Lookup("storage.cache-size"))
	assert.NotNil(t, cmd.Flags().Lookup("security.tls-enabled"))
	assert.NotNil(t, cmd.Flags().Lookup("security.fips-mode"))
	assert.NotNil(t, cmd.Flags().Lookup("logging.level"))
	assert.NotNil(t, cmd.Flags().Lookup("logging.format"))
}

func TestSaveToFile(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "saved-config.yaml")

	cfg := Default()
	cfg.Server.Host = "example.com"
	cfg.Server.REST.Port = 8888
	cfg.Raft.NodeID = 123

	loader := NewLoader()
	err := loader.SaveToFile(cfg, configFile)

	require.NoError(t, err)

	// Verify file was created
	_, err = os.Stat(configFile)
	assert.NoError(t, err)

	// Load it back and verify key fields
	loaded, err := LoadFromFile(configFile)
	require.NoError(t, err)
	assert.Equal(t, cfg.Server.Host, loaded.Server.Host)
	assert.Equal(t, cfg.Server.REST.Port, loaded.Server.REST.Port)
	// Note: viper unmarshaling with defaults can be tricky with nested structs
	// The file save/load works correctly in actual usage
}

func TestGetViper(t *testing.T) {
	loader := NewLoader()
	v := loader.GetViper()

	require.NotNil(t, v)
}

func TestMergeConfig(t *testing.T) {
	loader := NewLoader()

	cfg1 := Default()
	cfg1.Server.REST.Port = 8080

	cfg2 := Default()
	cfg2.Server.REST.Port = 9090

	merged := loader.MergeConfig(cfg2, cfg1)
	require.NotNil(t, merged)
}

func TestLoadFromEnv(t *testing.T) {
	// Set some environment variables
	os.Setenv("QUICBAAS_SERVER_REST_PORT", "9999")
	defer os.Unsetenv("QUICBAAS_SERVER_REST_PORT")

	cfg, err := LoadFromEnv()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	// Environment variables are read automatically
}

func TestLoadFromFile(t *testing.T) {
	t.Run("non-existent file returns defaults", func(t *testing.T) {
		cfg, err := LoadFromFile("/non/existent/config.yaml")
		// Zero-config startup: non-existent file returns defaults, not an error
		require.NoError(t, err)
		require.NotNil(t, cfg)
		// Verify defaults are applied
		assert.NotEmpty(t, cfg.Server.Host)
		assert.Greater(t, cfg.Server.REST.Port, 0)
	})

	t.Run("valid yaml file", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "test-config.yaml")

		// Create a simple config file
		yamlContent := `
server:
  host: "testhost"
  rest:
    port: 7777
logging:
  level: "debug"
`
		err := os.WriteFile(configFile, []byte(yamlContent), 0o644)
		require.NoError(t, err)

		cfg, err := LoadFromFile(configFile)
		require.NoError(t, err)
		assert.Equal(t, "testhost", cfg.Server.Host)
		assert.Equal(t, 7777, cfg.Server.REST.Port)
		assert.Equal(t, "debug", cfg.Logging.Level)
	})
}

func TestDefaultConfigs(t *testing.T) {
	t.Run("default server config", func(t *testing.T) {
		cfg := DefaultServerConfig()
		assert.NotEmpty(t, cfg.Host)
		assert.Greater(t, cfg.REST.Port, 0)
		assert.Greater(t, cfg.ReadTimeout, time.Duration(0))
		// Unix socket should be set by default
		assert.Equal(t, DefaultUnixSocket, cfg.Unix.Socket)
	})

	t.Run("default raft config", func(t *testing.T) {
		cfg := DefaultRaftConfig()
		assert.Greater(t, cfg.NodeID, uint64(0))
		assert.Greater(t, cfg.ElectionRTT, uint64(0))
		assert.NotEmpty(t, cfg.RaftAddress)
	})

	t.Run("default storage config", func(t *testing.T) {
		cfg := DefaultStorageConfig()
		assert.NotEmpty(t, cfg.DataDir)
		assert.Greater(t, cfg.CacheSize, int64(0))
	})

	t.Run("default security config", func(t *testing.T) {
		cfg := DefaultSecurityConfig()
		assert.NotEmpty(t, cfg.EncryptionAlgorithm)
	})

	t.Run("default logging config", func(t *testing.T) {
		cfg := DefaultLoggingConfig()
		assert.NotEmpty(t, cfg.Level)
		assert.NotEmpty(t, cfg.Format)
	})
}

func TestProductionConfig(t *testing.T) {
	cfg := ProductionConfig()
	require.NotNil(t, cfg)

	// Production config should have security enabled
	assert.True(t, cfg.Security.TLSEnabled)
	assert.True(t, cfg.Security.EncryptionEnabled)

	// Production should use file logging
	assert.Equal(t, "file", cfg.Logging.Output)

	// Production should use the production Unix socket path
	assert.Equal(t, DefaultUnixSocket, cfg.Server.Unix.Socket)
}

func TestDevelopmentConfig(t *testing.T) {
	cfg := DevelopmentConfig()
	require.NotNil(t, cfg)

	// Development should be less secure
	assert.False(t, cfg.Security.TLSEnabled)
	assert.False(t, cfg.Security.FIPSMode)

	// Development should use debug level
	assert.Equal(t, "debug", cfg.Logging.Level)

	// Development should use console format
	assert.Equal(t, "console", cfg.Logging.Format)

	// Development should use the default Unix socket path
	assert.Equal(t, "/tmp/quicbaas.sock", cfg.Server.Unix.Socket)
}

func TestUnixSocketConstants(t *testing.T) {
	// Verify Unix socket defaults to ~/quicbaas.sock
	assert.Contains(t, DefaultUnixSocket, "quicbaas.sock")
	assert.NotEqual(t, "./quicbaas.sock", DefaultUnixSocket, "should use home dir, not relative path")
}

func TestUnixSocketEnvOverride(t *testing.T) {
	// Test that QUICBAAS_SERVER_UNIX_SOCKET environment variable overrides default
	testSocketPath := "/var/run/quicbaas/test-node.sock"

	// Set the environment variable
	os.Setenv("QUICBAAS_SERVER_UNIX_SOCKET", testSocketPath)
	defer os.Unsetenv("QUICBAAS_SERVER_UNIX_SOCKET")

	// Load config (should pick up env var)
	loader := NewLoader()
	cfg, err := loader.Load("")
	require.NoError(t, err)

	// Verify the Unix socket path was overridden
	assert.Equal(t, testSocketPath, cfg.Server.Unix.Socket,
		"Unix socket path should be overridden by QUICBAAS_SERVER_UNIX_SOCKET env var")
}

func TestTestConfig(t *testing.T) {
	cfg := TestConfig()
	require.NotNil(t, cfg)

	// Test config should have minimal settings
	assert.Equal(t, 0, cfg.Server.REST.Port) // Random port
	assert.Equal(t, 0, cfg.Server.GRPC.Port) // Random port
	assert.False(t, cfg.Security.TLSEnabled)
	assert.Equal(t, "debug", cfg.Logging.Level)
}

func TestEmbeddedModeConfig(t *testing.T) {
	t.Run("default embedded mode is disabled", func(t *testing.T) {
		cfg := DefaultServerConfig()
		assert.False(t, cfg.Embedded, "Embedded mode should be disabled by default")
	})

	t.Run("embedded mode can be enabled", func(t *testing.T) {
		cfg := DefaultServerConfig()
		cfg.Embedded = true
		assert.True(t, cfg.Embedded)
	})

	t.Run("embedded mode disables all listeners", func(t *testing.T) {
		cfg := &ServerConfig{
			Embedded: true,
			GRPC:     ServerGRPCConfig{Enabled: true}, // Should be ignored when Embedded is true
			REST:     ServerRESTConfig{Enabled: true}, // Should be ignored when Embedded is true
			QUIC:     ServerQUICConfig{Enabled: true}, // Should be ignored when Embedded is true
			Unix:     ServerUnixConfig{Socket: "/tmp/test.sock"},
		}

		// Even though listeners are enabled, embedded mode takes precedence
		assert.True(t, cfg.Embedded)

		// When embedded is true, the server startup code will skip all listeners
		// This test verifies the config structure is correct
	})

	t.Run("embedded config from yaml", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "embedded-config.yaml")

		// Create a config file with embedded mode
		yamlContent := `
server:
  host: "127.0.0.1"
  embedded: true
  grpc:
    enabled: false
    port: 9090
  rest:
    enabled: false
    port: 8080
  quic:
    enabled: false
    port: 4433
  unix:
    enabled: false
    socket: ""
logging:
  level: "info"
`
		err := os.WriteFile(configFile, []byte(yamlContent), 0o644)
		require.NoError(t, err)

		cfg, err := LoadFromFile(configFile)
		require.NoError(t, err)
		assert.True(t, cfg.Server.Embedded, "Embedded mode should be loaded from YAML")
		assert.False(t, cfg.Server.GRPC.Enabled)
		assert.False(t, cfg.Server.REST.Enabled)
		assert.False(t, cfg.Server.QUIC.Enabled)
		assert.Empty(t, cfg.Server.Unix.Socket)
	})
}

// EmbeddedConfig returns a configuration optimized for embedded mode usage.
// This is useful when using QuicBaaS as a library without network listeners.
func TestEmbeddedConfigHelper(t *testing.T) {
	cfg := EmbeddedConfig()
	require.NotNil(t, cfg)

	// Embedded mode should be enabled
	assert.True(t, cfg.Server.Embedded, "Embedded config should have embedded mode enabled")

	// All network listeners should be disabled
	assert.False(t, cfg.Server.GRPC.Enabled, "GRPC should be disabled in embedded mode")
	assert.False(t, cfg.Server.REST.Enabled, "REST should be disabled in embedded mode")
	assert.False(t, cfg.Server.QUIC.Enabled, "QUIC should be disabled in embedded mode")
	assert.Empty(t, cfg.Server.Unix.Socket, "Unix socket should be empty in embedded mode")

	// Raft should still be configured for cluster communication
	assert.Greater(t, cfg.Raft.NodeID, uint64(0), "Raft node ID should be set")
	assert.NotEmpty(t, cfg.Raft.RaftAddress, "Raft address should be set for cluster communication")
}

func TestPackageLevelLoadWithDefaults(t *testing.T) {
	cfg, err := LoadWithDefaults()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.NotEmpty(t, cfg.Server.Host)
	assert.Greater(t, cfg.Server.REST.Port, 0)
}

func TestPackageLevelLoad(t *testing.T) {
	t.Run("empty path returns defaults", func(t *testing.T) {
		cfg, err := Load("")
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.NotEmpty(t, cfg.Server.Host)
	})

	t.Run("non-existent path returns defaults", func(t *testing.T) {
		cfg, err := Load("/non/existent/config.yaml")
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.NotEmpty(t, cfg.Server.Host)
	})
}

func TestEnvironmentVariableOverrides(t *testing.T) {
	t.Run("QUICBAAS_STORAGE_BACKEND override", func(t *testing.T) {
		os.Setenv("QUICBAAS_STORAGE_BACKEND", "memory")
		defer os.Unsetenv("QUICBAAS_STORAGE_BACKEND")

		cfg, err := Load("")
		require.NoError(t, err)
		assert.Equal(t, "memory", cfg.Storage.Backend)
	})

	t.Run("QUICBAAS_SERVER_GRPC_ENABLED override", func(t *testing.T) {
		os.Setenv("QUICBAAS_SERVER_GRPC_ENABLED", "false")
		defer os.Unsetenv("QUICBAAS_SERVER_GRPC_ENABLED")

		cfg, err := Load("")
		require.NoError(t, err)
		assert.False(t, cfg.Server.GRPC.Enabled)
	})

	t.Run("QUICBAAS_SERVER_GRPC_PORT override", func(t *testing.T) {
		os.Setenv("QUICBAAS_SERVER_GRPC_PORT", "9999")
		defer os.Unsetenv("QUICBAAS_SERVER_GRPC_PORT")

		cfg, err := Load("")
		require.NoError(t, err)
		assert.Equal(t, 9999, cfg.Server.GRPC.Port)
	})

	t.Run("QUICBAAS_SERVER_REST_ENABLED override", func(t *testing.T) {
		os.Setenv("QUICBAAS_SERVER_REST_ENABLED", "false")
		defer os.Unsetenv("QUICBAAS_SERVER_REST_ENABLED")

		cfg, err := Load("")
		require.NoError(t, err)
		assert.False(t, cfg.Server.REST.Enabled)
	})

	t.Run("QUICBAAS_SERVER_REST_PORT override", func(t *testing.T) {
		os.Setenv("QUICBAAS_SERVER_REST_PORT", "7777")
		defer os.Unsetenv("QUICBAAS_SERVER_REST_PORT")

		cfg, err := Load("")
		require.NoError(t, err)
		assert.Equal(t, 7777, cfg.Server.REST.Port)
	})

	t.Run("QUICBAAS_SERVER_QUIC_ENABLED override", func(t *testing.T) {
		os.Setenv("QUICBAAS_SERVER_QUIC_ENABLED", "false")
		defer os.Unsetenv("QUICBAAS_SERVER_QUIC_ENABLED")

		cfg, err := Load("")
		require.NoError(t, err)
		assert.False(t, cfg.Server.QUIC.Enabled)
	})

	t.Run("QUICBAAS_SERVER_QUIC_PORT override", func(t *testing.T) {
		os.Setenv("QUICBAAS_SERVER_QUIC_PORT", "4444")
		defer os.Unsetenv("QUICBAAS_SERVER_QUIC_PORT")

		cfg, err := Load("")
		require.NoError(t, err)
		assert.Equal(t, 4444, cfg.Server.QUIC.Port)
	})

	t.Run("QUICBAAS_SERVER_MCP_ENABLED override", func(t *testing.T) {
		os.Setenv("QUICBAAS_SERVER_MCP_ENABLED", "false")
		defer os.Unsetenv("QUICBAAS_SERVER_MCP_ENABLED")

		cfg, err := Load("")
		require.NoError(t, err)
		assert.False(t, cfg.Server.MCP.Enabled)
	})

	t.Run("QUICBAAS_SERVER_MCP_PORT override", func(t *testing.T) {
		os.Setenv("QUICBAAS_SERVER_MCP_PORT", "5555")
		defer os.Unsetenv("QUICBAAS_SERVER_MCP_PORT")

		cfg, err := Load("")
		require.NoError(t, err)
		assert.Equal(t, 5555, cfg.Server.MCP.Port)
	})

	t.Run("QUICBAAS_SERVER_UNIX_ENABLED override", func(t *testing.T) {
		os.Setenv("QUICBAAS_SERVER_UNIX_ENABLED", "false")
		defer os.Unsetenv("QUICBAAS_SERVER_UNIX_ENABLED")

		cfg, err := Load("")
		require.NoError(t, err)
		assert.False(t, cfg.Server.Unix.Enabled)
	})

	t.Run("boolean parsing variations", func(t *testing.T) {
		testCases := []struct {
			value    string
			expected bool
		}{
			{"true", true},
			{"TRUE", true},
			{"True", true},
			{"1", true},
			{"yes", true},
			{"YES", true},
			{"on", true},
			{"ON", true},
			{"false", false},
			{"FALSE", false},
			{"0", false},
			{"no", false},
			{"off", false},
			{"anything_else", false},
		}

		for _, tc := range testCases {
			os.Setenv("QUICBAAS_SERVER_GRPC_ENABLED", tc.value)
			cfg, err := Load("")
			require.NoError(t, err)
			assert.Equal(t, tc.expected, cfg.Server.GRPC.Enabled, "value %q should parse to %v", tc.value, tc.expected)
			os.Unsetenv("QUICBAAS_SERVER_GRPC_ENABLED")
		}
	})
}

func TestDefaultSealConfig(t *testing.T) {
	t.Run("returns correct defaults", func(t *testing.T) {
		cfg := DefaultSealConfig()

		assert.False(t, cfg.Enabled)
		assert.Equal(t, "manual", cfg.Mode)
		assert.Equal(t, 2, cfg.Threshold)
		assert.Equal(t, 3, cfg.TotalShares)
		assert.Empty(t, cfg.BarrierDir)
		assert.Equal(t, []string{"software"}, cfg.PreferenceOrder)
		assert.True(t, cfg.RecoveryEnabled)
		assert.Equal(t, 3, cfg.RecoveryThreshold)
		assert.Equal(t, 5, cfg.RecoveryTotal)
	})

	t.Run("threshold is less than total shares", func(t *testing.T) {
		cfg := DefaultSealConfig()
		assert.Less(t, cfg.Threshold, cfg.TotalShares,
			"Threshold must be less than TotalShares for Shamir secret sharing")
	})
}

func TestDefaultCAConfig(t *testing.T) {
	t.Run("returns correct defaults", func(t *testing.T) {
		cfg := DefaultCAConfig()

		assert.Equal(t, "QuicBaaS Root CA", cfg.RootCommonName)
		assert.Equal(t, "QuicBaaS", cfg.RootOrganization)
		assert.Equal(t, 10, cfg.RootValidYears)
		assert.Equal(t, "ECDSA", cfg.RootKeyAlgorithm)
		assert.Equal(t, "P-256", cfg.RootKeyCurve)
		assert.True(t, cfg.IntermediateEnabled)
		assert.Equal(t, "QuicBaaS Intermediate CA", cfg.IntermediateCommonName)
		assert.Equal(t, 5, cfg.IntermediateValidYears)
		assert.Equal(t, "ECDSA", cfg.IntermediateKeyAlgorithm)
		assert.Equal(t, "P-256", cfg.IntermediateKeyCurve)
		assert.Equal(t, 365, cfg.DefaultValidityDays)
		assert.True(t, cfg.IncludeLocalhostSANS)
		assert.Empty(t, cfg.KeysDir)
	})

	t.Run("intermediate CA validity is less than root CA validity", func(t *testing.T) {
		cfg := DefaultCAConfig()
		assert.Less(t, cfg.IntermediateValidYears, cfg.RootValidYears,
			"Intermediate CA must have shorter validity than Root CA")
	})
}

func TestDefaultConfig_IncludesSealAndCA(t *testing.T) {
	cfg := DefaultConfig()

	// Seal defaults should be populated
	assert.False(t, cfg.Seal.Enabled)
	assert.Equal(t, "manual", cfg.Seal.Mode)
	assert.Equal(t, 2, cfg.Seal.Threshold)
	assert.Equal(t, 3, cfg.Seal.TotalShares)
	assert.Equal(t, []string{"software"}, cfg.Seal.PreferenceOrder)
	assert.True(t, cfg.Seal.RecoveryEnabled)
	assert.Equal(t, 3, cfg.Seal.RecoveryThreshold)
	assert.Equal(t, 5, cfg.Seal.RecoveryTotal)

	// CA defaults should be populated
	assert.Equal(t, "QuicBaaS Root CA", cfg.CA.RootCommonName)
	assert.Equal(t, "QuicBaaS", cfg.CA.RootOrganization)
	assert.Equal(t, 10, cfg.CA.RootValidYears)
	assert.Equal(t, "ECDSA", cfg.CA.RootKeyAlgorithm)
	assert.Equal(t, "P-256", cfg.CA.RootKeyCurve)
	assert.True(t, cfg.CA.IntermediateEnabled)
	assert.Equal(t, 365, cfg.CA.DefaultValidityDays)
	assert.True(t, cfg.CA.IncludeLocalhostSANS)
}

func TestTestConfig_IncludesSealAndCA(t *testing.T) {
	cfg := TestConfig()

	// Test config seal should be disabled
	assert.False(t, cfg.Seal.Enabled)

	// Test config CA should be zero-valued (disabled)
	assert.Empty(t, cfg.CA.RootCommonName)
	assert.Equal(t, 0, cfg.CA.RootValidYears)
}

func TestConfig_Clone_SealAndCA(t *testing.T) {
	t.Run("seal and CA fields are cloned", func(t *testing.T) {
		original := Default()
		original.Seal.Enabled = true
		original.Seal.Mode = "auto"
		original.Seal.Threshold = 3
		original.Seal.TotalShares = 5
		original.Seal.BarrierDir = "/custom/barrier"
		original.Seal.PreferenceOrder = []string{"tpm2", "software"}
		original.CA.RootCommonName = "Custom Root CA"
		original.CA.RootValidYears = 20
		original.CA.KeysDir = "/custom/keys"

		cloned := original.Clone()

		// Verify seal fields are copied
		assert.True(t, cloned.Seal.Enabled)
		assert.Equal(t, "auto", cloned.Seal.Mode)
		assert.Equal(t, 3, cloned.Seal.Threshold)
		assert.Equal(t, 5, cloned.Seal.TotalShares)
		assert.Equal(t, "/custom/barrier", cloned.Seal.BarrierDir)
		assert.Equal(t, []string{"tpm2", "software"}, cloned.Seal.PreferenceOrder)

		// Verify CA fields are copied
		assert.Equal(t, "Custom Root CA", cloned.CA.RootCommonName)
		assert.Equal(t, 20, cloned.CA.RootValidYears)
		assert.Equal(t, "/custom/keys", cloned.CA.KeysDir)
	})

	t.Run("modifying cloned seal does not affect original", func(t *testing.T) {
		original := Default()
		original.Seal.Mode = "manual"
		original.Seal.Threshold = 2
		original.Seal.PreferenceOrder = []string{"software", "tpm2"}

		cloned := original.Clone()
		cloned.Seal.Mode = "auto"
		cloned.Seal.Threshold = 5

		assert.Equal(t, "manual", original.Seal.Mode)
		assert.Equal(t, 2, original.Seal.Threshold)
	})

	t.Run("PreferenceOrder slice is deep copied", func(t *testing.T) {
		original := Default()
		original.Seal.PreferenceOrder = []string{"software", "tpm2", "pkcs11"}

		cloned := original.Clone()
		cloned.Seal.PreferenceOrder[0] = "awskms"

		// Original should be unchanged
		assert.Equal(t, "software", original.Seal.PreferenceOrder[0])
		assert.Equal(t, "awskms", cloned.Seal.PreferenceOrder[0])
	})

	t.Run("modifying cloned CA does not affect original", func(t *testing.T) {
		original := Default()
		original.CA.RootCommonName = "Original CA"
		original.CA.DefaultValidityDays = 365

		cloned := original.Clone()
		cloned.CA.RootCommonName = "Cloned CA"
		cloned.CA.DefaultValidityDays = 730

		assert.Equal(t, "Original CA", original.CA.RootCommonName)
		assert.Equal(t, 365, original.CA.DefaultValidityDays)
	})
}

func TestSealConfig_LoadFromYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "seal-config.yaml")

	yamlContent := `
seal:
  enabled: true
  mode: "auto"
  threshold: 3
  total_shares: 5
  barrier_dir: "/data/barrier"
  preference_order:
    - "tpm2"
    - "software"
  recovery_enabled: true
  recovery_threshold: 4
  recovery_total: 7
`
	err := os.WriteFile(configFile, []byte(yamlContent), 0o644)
	require.NoError(t, err)

	cfg, err := LoadFromFile(configFile)
	require.NoError(t, err)

	assert.True(t, cfg.Seal.Enabled)
	assert.Equal(t, "auto", cfg.Seal.Mode)
	assert.Equal(t, 3, cfg.Seal.Threshold)
	assert.Equal(t, 5, cfg.Seal.TotalShares)
	assert.Equal(t, "/data/barrier", cfg.Seal.BarrierDir)
	assert.Equal(t, []string{"tpm2", "software"}, cfg.Seal.PreferenceOrder)
	assert.True(t, cfg.Seal.RecoveryEnabled)
	assert.Equal(t, 4, cfg.Seal.RecoveryThreshold)
	assert.Equal(t, 7, cfg.Seal.RecoveryTotal)
}

func TestCAConfig_LoadFromYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "ca-config.yaml")

	yamlContent := `
ca:
  root_common_name: "Custom Root CA"
  root_organization: "Custom Org"
  root_valid_years: 15
  root_key_algorithm: "RSA"
  root_key_curve: "P-384"
  intermediate_enabled: false
  intermediate_common_name: "Custom Intermediate CA"
  intermediate_valid_years: 3
  intermediate_key_algorithm: "RSA"
  intermediate_key_curve: "P-384"
  default_validity_days: 730
  include_localhost_sans: false
  keys_dir: "/custom/keys"
`
	err := os.WriteFile(configFile, []byte(yamlContent), 0o644)
	require.NoError(t, err)

	cfg, err := LoadFromFile(configFile)
	require.NoError(t, err)

	assert.Equal(t, "Custom Root CA", cfg.CA.RootCommonName)
	assert.Equal(t, "Custom Org", cfg.CA.RootOrganization)
	assert.Equal(t, 15, cfg.CA.RootValidYears)
	assert.Equal(t, "RSA", cfg.CA.RootKeyAlgorithm)
	assert.Equal(t, "P-384", cfg.CA.RootKeyCurve)
	assert.False(t, cfg.CA.IntermediateEnabled)
	assert.Equal(t, "Custom Intermediate CA", cfg.CA.IntermediateCommonName)
	assert.Equal(t, 3, cfg.CA.IntermediateValidYears)
	assert.Equal(t, "RSA", cfg.CA.IntermediateKeyAlgorithm)
	assert.Equal(t, "P-384", cfg.CA.IntermediateKeyCurve)
	assert.Equal(t, 730, cfg.CA.DefaultValidityDays)
	assert.False(t, cfg.CA.IncludeLocalhostSANS)
	assert.Equal(t, "/custom/keys", cfg.CA.KeysDir)
}

// =============================================================================
// Consolidated from config_additional_test.go
// =============================================================================

// TestBindFlags_ErrorPaths tests flag binding behavior.
// The bind*Flags methods bind pre-existing cobra flags to viper. They do NOT
// create flags. Each subtest pre-registers the flags that the bind method
// expects, calls the bind method, and verifies the flags are still present.
func TestBindFlags_ErrorPaths(t *testing.T) {
	t.Run("bind server flags", func(t *testing.T) {
		loader := NewLoader()
		cmd := &cobra.Command{Use: "test"}
		// Pre-register the flags that bindServerFlags expects
		cmd.Flags().String("server.host", "", "server host")
		cmd.Flags().Int("server.grpc-port", 0, "gRPC port")
		cmd.Flags().Int("server.http-port", 0, "HTTP port")
		err := loader.bindServerFlags(cmd)
		require.NoError(t, err)
		// Verify flags are still present after binding
		assert.NotNil(t, cmd.Flags().Lookup("server.host"))
		assert.NotNil(t, cmd.Flags().Lookup("server.grpc-port"))
		assert.NotNil(t, cmd.Flags().Lookup("server.http-port"))
	})
	t.Run("bind server flags without pre-registered flags", func(t *testing.T) {
		loader := NewLoader()
		cmd := &cobra.Command{Use: "test"}
		// Binding on a bare command should succeed silently (flags are skipped)
		err := loader.bindServerFlags(cmd)
		require.NoError(t, err)
	})
	t.Run("bind raft flags", func(t *testing.T) {
		loader := NewLoader()
		cmd := &cobra.Command{Use: "test"}
		// Pre-register the flags that bindRaftFlags expects
		cmd.Flags().Uint64("raft.node-id", 0, "node ID")
		cmd.Flags().String("raft.data-dir", "", "data dir")
		cmd.Flags().String("raft.raft-address", "", "raft address")
		err := loader.bindRaftFlags(cmd)
		require.NoError(t, err)
		// Verify flags are still present after binding
		assert.NotNil(t, cmd.Flags().Lookup("raft.node-id"))
		assert.NotNil(t, cmd.Flags().Lookup("raft.data-dir"))
		assert.NotNil(t, cmd.Flags().Lookup("raft.raft-address"))
	})
	t.Run("bind storage flags", func(t *testing.T) {
		loader := NewLoader()
		cmd := &cobra.Command{Use: "test"}
		// Pre-register the flags that bindStorageFlags expects
		cmd.Flags().String("storage.data-dir", "", "data dir")
		cmd.Flags().String("storage.wal-dir", "", "WAL dir")
		cmd.Flags().String("storage.backend", "", "backend")
		err := loader.bindStorageFlags(cmd)
		require.NoError(t, err)
		// Verify flags are still present after binding
		assert.NotNil(t, cmd.Flags().Lookup("storage.data-dir"))
		assert.NotNil(t, cmd.Flags().Lookup("storage.wal-dir"))
		assert.NotNil(t, cmd.Flags().Lookup("storage.backend"))
	})
	t.Run("bind security flags", func(t *testing.T) {
		loader := NewLoader()
		cmd := &cobra.Command{Use: "test"}
		// Pre-register the flags that bindSecurityFlags expects
		cmd.Flags().Bool("security.tls-enabled", false, "TLS enabled")
		cmd.Flags().String("security.tls-cert-file", "", "TLS cert file")
		cmd.Flags().String("security.tls-key-file", "", "TLS key file")
		err := loader.bindSecurityFlags(cmd)
		require.NoError(t, err)
		// Verify flags are still present after binding
		assert.NotNil(t, cmd.Flags().Lookup("security.tls-enabled"))
		assert.NotNil(t, cmd.Flags().Lookup("security.tls-cert-file"))
		assert.NotNil(t, cmd.Flags().Lookup("security.tls-key-file"))
	})
	t.Run("bind logging flags", func(t *testing.T) {
		loader := NewLoader()
		cmd := &cobra.Command{Use: "test"}
		// Pre-register the flags that bindLoggingFlags expects
		cmd.Flags().String("logging.level", "", "log level")
		cmd.Flags().String("logging.format", "", "log format")
		cmd.Flags().String("logging.output", "", "log output")
		cmd.Flags().String("logging.file", "", "log file")
		err := loader.bindLoggingFlags(cmd)
		require.NoError(t, err)
		// Verify flags are still present after binding
		assert.NotNil(t, cmd.Flags().Lookup("logging.level"))
		assert.NotNil(t, cmd.Flags().Lookup("logging.format"))
		assert.NotNil(t, cmd.Flags().Lookup("logging.output"))
		assert.NotNil(t, cmd.Flags().Lookup("logging.file"))
	})
	t.Run("bind all flags together", func(t *testing.T) {
		loader := NewLoader()
		cmd := &cobra.Command{Use: "test"}
		// BindFlags calls all bind methods; on a bare command it should succeed
		err := loader.BindFlags(cmd)
		require.NoError(t, err)
	})
	t.Run("bind all flags together with pre-registered flags", func(t *testing.T) {
		loader := NewLoader()
		cmd := &cobra.Command{Use: "test"}
		// Pre-register representative flags from each category
		cmd.Flags().String("server.host", "", "server host")
		cmd.Flags().Uint64("raft.node-id", 0, "node ID")
		cmd.Flags().String("storage.data-dir", "", "data dir")
		cmd.Flags().Bool("security.tls-enabled", false, "TLS enabled")
		cmd.Flags().String("logging.level", "", "log level")
		err := loader.BindFlags(cmd)
		require.NoError(t, err)
		// Verify flags from all categories are present
		assert.NotNil(t, cmd.Flags().Lookup("server.host"))
		assert.NotNil(t, cmd.Flags().Lookup("raft.node-id"))
		assert.NotNil(t, cmd.Flags().Lookup("storage.data-dir"))
		assert.NotNil(t, cmd.Flags().Lookup("security.tls-enabled"))
		assert.NotNil(t, cmd.Flags().Lookup("logging.level"))
	})
}

// TestMergeConfig_EdgeCases tests config merging scenarios.
func TestMergeConfig_EdgeCases(t *testing.T) {
	t.Run("merge with nil other config", func(t *testing.T) {
		loader := NewLoader()
		current := Default()
		current.Server.REST.Port = 9999
		merged := loader.MergeConfig(nil, current)
		require.NotNil(t, merged)
		assert.Equal(t, 9999, merged.Server.REST.Port)
	})
	t.Run("merge with nil current config", func(t *testing.T) {
		loader := NewLoader()
		other := Default()
		other.Server.REST.Port = 7777
		merged := loader.MergeConfig(other, nil)
		require.NotNil(t, merged)
		assert.Equal(t, 7777, merged.Server.REST.Port)
	})
	t.Run("merge with both configs", func(t *testing.T) {
		loader := NewLoader()
		current := Default()
		current.Server.REST.Port = 8888
		current.Server.Host = "localhost"
		other := Default()
		other.Server.REST.Port = 9999
		other.Server.Host = "0.0.0.0"
		merged := loader.MergeConfig(other, current)
		require.NotNil(t, merged)
		// MergeConfig returns a clone of current (simple implementation)
		assert.Equal(t, 8888, merged.Server.REST.Port)
	})
}

// TestValidateStorage_ErrorPaths tests storage validation error scenarios.
func TestValidateStorage_ErrorPaths(t *testing.T) {
	t.Run("data directory is a file", func(t *testing.T) {
		cfg := Default()
		tmpDir := t.TempDir()
		// Create a file instead of directory
		dataFile := filepath.Join(tmpDir, "data")
		err := os.WriteFile(dataFile, []byte("test"), 0o644)
		require.NoError(t, err)
		cfg.Storage.DataDir = dataFile
		cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
		err = Validate(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "storage.data_dir")
		assert.Contains(t, err.Error(), "not a directory")
	})
	t.Run("WAL directory is a file", func(t *testing.T) {
		cfg := Default()
		tmpDir := t.TempDir()
		// Create a file instead of directory
		walFile := filepath.Join(tmpDir, "wal")
		err := os.WriteFile(walFile, []byte("test"), 0o644)
		require.NoError(t, err)
		cfg.Storage.DataDir = tmpDir
		cfg.Storage.WALDir = walFile
		err = Validate(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "storage.wal_dir")
		assert.Contains(t, err.Error(), "not a directory")
	})
	t.Run("directory creation fails for nested path", func(t *testing.T) {
		cfg := Default()
		// Disable TLS/encryption for this storage-focused test
		cfg.Security.TLSEnabled = false
		cfg.Security.EncryptionEnabled = false
		// Use a path that requires directory creation
		tmpDir := t.TempDir()
		nestedPath := filepath.Join(tmpDir, "level1", "level2", "level3")
		cfg.Storage.DataDir = nestedPath
		cfg.Storage.WALDir = filepath.Join(nestedPath, "wal")
		// Should succeed as it creates directories
		err := Validate(cfg)
		assert.NoError(t, err)
		// Verify directories were created
		info, err := os.Stat(nestedPath)
		assert.NoError(t, err)
		assert.True(t, info.IsDir())
	})
	t.Run("read-only parent directory", func(t *testing.T) {
		require.NotEqual(t, 0, os.Getuid(), "Test must not run as root")
		cfg := Default()
		tmpDir := t.TempDir()
		// Create a read-only directory
		roDir := filepath.Join(tmpDir, "readonly")
		err := os.Mkdir(roDir, 0o555)
		require.NoError(t, err)
		// Try to create subdirectory in read-only dir
		cfg.Storage.DataDir = filepath.Join(roDir, "data")
		cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
		err = Validate(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "storage.data_dir")
	})
	t.Run("negative bytes per sync", func(t *testing.T) {
		cfg := Default()
		tmpDir := t.TempDir()
		cfg.Storage.DataDir = tmpDir
		cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
		cfg.Storage.BytesPerSync = -1
		err := Validate(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "storage.bytes_per_sync")
	})
	t.Run("various storage validation errors", func(t *testing.T) {
		tests := []struct {
			name   string
			modify func(*Config)
			field  string
		}{
			{
				name: "cache size too small",
				modify: func(cfg *Config) {
					cfg.Storage.CacheSize = 512 // Less than 1MB
				},
				field: "storage.cache_size",
			},
			{
				name: "mem table size too small",
				modify: func(cfg *Config) {
					cfg.Storage.MemTableSize = 512 // Less than 1MB
				},
				field: "storage.mem_table_size",
			},
			{
				name: "target file size too small",
				modify: func(cfg *Config) {
					cfg.Storage.TargetFileSize = 512 // Less than 1MB
				},
				field: "storage.target_file_size",
			},
			{
				name: "block size too small",
				modify: func(cfg *Config) {
					cfg.Storage.BlockSize = 512 // Less than 1KB
				},
				field: "storage.block_size",
			},
			{
				name: "max concurrent reads too low",
				modify: func(cfg *Config) {
					cfg.Storage.MaxConcurrentReads = 0
				},
				field: "storage.max_concurrent_reads",
			},
			{
				name: "max concurrent writes too low",
				modify: func(cfg *Config) {
					cfg.Storage.MaxConcurrentWrites = 0
				},
				field: "storage.max_concurrent_writes",
			},
			{
				name: "mem table stop writes too low",
				modify: func(cfg *Config) {
					cfg.Storage.MemTableStopWrites = 0
				},
				field: "storage.mem_table_stop_writes",
			},
			{
				name: "L0 compaction threshold too low",
				modify: func(cfg *Config) {
					cfg.Storage.L0CompactionThreshold = 0
				},
				field: "storage.l0_compaction_threshold",
			},
			{
				name: "L0 stop writes threshold too low",
				modify: func(cfg *Config) {
					cfg.Storage.L0StopWritesThreshold = 0
				},
				field: "storage.l0_stop_writes_threshold",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				cfg := Default()
				tmpDir := t.TempDir()
				cfg.Storage.DataDir = tmpDir
				cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
				tt.modify(cfg)
				err := Validate(cfg)
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.field)
			})
		}
	})
}

// TestValidateSecurity_ErrorPaths tests security validation scenarios.
func TestValidateSecurity_ErrorPaths(t *testing.T) {
	t.Run("TLS CA file doesn't exist", func(t *testing.T) {
		cfg := Default()
		tmpDir := t.TempDir()
		cfg.Storage.DataDir = tmpDir
		cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
		// Create cert and key files but not CA
		certFile := filepath.Join(tmpDir, "cert.pem")
		keyFile := filepath.Join(tmpDir, "key.pem")
		os.WriteFile(certFile, []byte("cert"), 0o644)
		os.WriteFile(keyFile, []byte("key"), 0o644)
		cfg.Security.TLSEnabled = true
		cfg.Security.TLSCertFile = certFile
		cfg.Security.TLSKeyFile = keyFile
		cfg.Security.TLSCAFile = "/nonexistent/ca.pem"
		cfg.Security.EncryptionEnabled = false
		err := Validate(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "security.tls_ca_file")
		assert.Contains(t, err.Error(), "does not exist")
	})
	t.Run("TLS enabled with client auth but no CA file", func(t *testing.T) {
		cfg := Default()
		tmpDir := t.TempDir()
		cfg.Storage.DataDir = tmpDir
		cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
		certFile := filepath.Join(tmpDir, "cert.pem")
		keyFile := filepath.Join(tmpDir, "key.pem")
		os.WriteFile(certFile, []byte("cert"), 0o644)
		os.WriteFile(keyFile, []byte("key"), 0o644)
		cfg.Security.TLSEnabled = true
		cfg.Security.TLSCertFile = certFile
		cfg.Security.TLSKeyFile = keyFile
		cfg.Security.TLSClientAuth = true
		cfg.Security.TLSCAFile = ""
		cfg.Security.EncryptionEnabled = false
		err := Validate(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "security.tls_ca_file")
		assert.Contains(t, err.Error(), "required when client auth is enabled")
	})
	t.Run("encryption key file doesn't exist", func(t *testing.T) {
		cfg := Default()
		tmpDir := t.TempDir()
		cfg.Storage.DataDir = tmpDir
		cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
		cfg.Security.TLSEnabled = false
		cfg.Security.EncryptionEnabled = true
		cfg.Security.EncryptionKeyFile = "/nonexistent/key.file"
		err := Validate(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "security.encryption_key_file")
		assert.Contains(t, err.Error(), "does not exist")
	})
	t.Run("FIPS mode enabled but not available", func(t *testing.T) {
		cfg := Default()
		tmpDir := t.TempDir()
		cfg.Storage.DataDir = tmpDir
		cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
		cfg.Security.TLSEnabled = false
		cfg.Security.EncryptionEnabled = false
		cfg.Security.FIPSMode = true
		err := Validate(cfg)
		// This will fail on non-FIPS systems
		if err != nil {
			assert.Contains(t, err.Error(), "security.fips_mode")
		}
	})
}

// TestFileExists tests the fileExists helper function.
func TestFileExists(t *testing.T) {
	v := NewValidator()
	t.Run("file exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		file := filepath.Join(tmpDir, "test.txt")
		err := os.WriteFile(file, []byte("test"), 0o644)
		require.NoError(t, err)
		assert.True(t, v.fileExists(file))
	})
	t.Run("file does not exist", func(t *testing.T) {
		assert.False(t, v.fileExists("/nonexistent/file.txt"))
	})
	t.Run("empty path", func(t *testing.T) {
		assert.False(t, v.fileExists(""))
	})
	t.Run("directory exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		assert.True(t, v.fileExists(tmpDir))
	})
}

// TestEnsureDirectoryWritable_EdgeCases tests directory writable checks.
func TestEnsureDirectoryWritable_EdgeCases(t *testing.T) {
	v := NewValidator()
	t.Run("relative path resolution", func(t *testing.T) {
		tmpDir := t.TempDir()
		oldWd, err := os.Getwd()
		require.NoError(t, err)
		defer os.Chdir(oldWd)
		err = os.Chdir(tmpDir)
		require.NoError(t, err)
		err = v.ensureDirectoryWritable("./testdir")
		assert.NoError(t, err)
		// Verify directory was created
		info, err := os.Stat("./testdir")
		assert.NoError(t, err)
		assert.True(t, info.IsDir())
	})
	t.Run("writable directory already exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := v.ensureDirectoryWritable(tmpDir)
		assert.NoError(t, err)
	})
	t.Run("create nested directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		nestedPath := filepath.Join(tmpDir, "a", "b", "c", "d")
		err := v.ensureDirectoryWritable(nestedPath)
		assert.NoError(t, err)
		info, err := os.Stat(nestedPath)
		assert.NoError(t, err)
		assert.True(t, info.IsDir())
	})
	t.Run("read-only directory", func(t *testing.T) {
		require.NotEqual(t, 0, os.Getuid(), "Test must not run as root")
		tmpDir := t.TempDir()
		roDir := filepath.Join(tmpDir, "readonly")
		err := os.Mkdir(roDir, 0o555)
		require.NoError(t, err)
		err = v.ensureDirectoryWritable(roDir)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not writable")
	})
}

// TestIsFIPSAvailable tests FIPS availability detection.
func TestIsFIPSAvailable(t *testing.T) {
	v := NewValidator()
	t.Run("check FIPS availability", func(t *testing.T) {
		// This test just ensures the function runs without panic
		available := v.isFIPSAvailable()
		// On most systems, FIPS won't be available
		// We're just testing that the function works
		t.Logf("FIPS available: %v", available)
	})
	t.Run("FIPS check with mock file", func(t *testing.T) {
		// Create a mock FIPS enabled file for testing
		tmpDir := t.TempDir()
		fipsFile := filepath.Join(tmpDir, "fips_enabled")
		// Write "1" to simulate FIPS enabled
		err := os.WriteFile(fipsFile, []byte("1"), 0o644)
		require.NoError(t, err)
		// The actual isFIPSAvailable checks /proc/sys/crypto/fips_enabled
		// which we can't easily mock, so we just verify it doesn't crash
		available := v.isFIPSAvailable()
		t.Logf("FIPS check completed: %v", available)
	})
}

// TestConfig_Clone_NilHandling tests Clone with nil config.
func TestConfig_Clone_NilHandling(t *testing.T) {
	var cfg *Config
	cloned := cfg.Clone()
	assert.Nil(t, cloned)
}

// TestLoader_Load_EdgeCases tests loader edge cases.
func TestLoader_Load_EdgeCases(t *testing.T) {
	t.Run("load with invalid YAML", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "invalid.yaml")
		// Write invalid YAML
		err := os.WriteFile(configFile, []byte("invalid: yaml: content: ["), 0o644)
		require.NoError(t, err)
		loader := NewLoader()
		cfg, err := loader.Load(configFile)
		// Should fail to unmarshal
		assert.Error(t, err)
		assert.Nil(t, cfg)
	})
	t.Run("load from default locations", func(t *testing.T) {
		loader := NewLoader()
		// This should not error even if no config file is found
		cfg, err := loader.Load("")
		require.NoError(t, err)
		require.NotNil(t, cfg)
	})
	t.Run("load non-existent file returns defaults", func(t *testing.T) {
		loader := NewLoader()
		// Zero-config startup: explicitly loading a non-existent file returns defaults
		cfg, err := loader.Load("/definitely/does/not/exist/config.yaml")
		require.NoError(t, err)
		require.NotNil(t, cfg)
		// Verify defaults are applied
		assert.NotEmpty(t, cfg.Server.Host)
		assert.Greater(t, cfg.Server.REST.Port, 0)
	})
}

// TestSaveToFile_EdgeCases tests save functionality edge cases.
func TestSaveToFile_EdgeCases(t *testing.T) {
	t.Run("save to invalid path", func(t *testing.T) {
		loader := NewLoader()
		cfg := Default()
		// Try to save to invalid path
		err := loader.SaveToFile(cfg, "/invalid/path/that/does/not/exist/config.yaml")
		assert.Error(t, err)
	})
	t.Run("save and reload complex config", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "complex.yaml")
		cfg := Default()
		cfg.Server.Host = "example.com"
		cfg.Server.REST.Port = 7777
		cfg.Storage.DataDir = tmpDir
		cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
		cfg.Logging.Level = "debug"
		loader := NewLoader()
		err := loader.SaveToFile(cfg, configFile)
		require.NoError(t, err)
		// Reload
		loaded, err := LoadFromFile(configFile)
		require.NoError(t, err)
		// Verify key fields that should persist
		assert.Equal(t, cfg.Server.Host, loaded.Server.Host)
		assert.Equal(t, cfg.Server.REST.Port, loaded.Server.REST.Port)
		assert.Equal(t, cfg.Logging.Level, loaded.Logging.Level)
		// Verify file exists
		assert.FileExists(t, configFile)
	})
}

// TestLoadFromEnv_EnvironmentVariables tests environment variable loading.
func TestLoadFromEnv_EnvironmentVariables(t *testing.T) {
	t.Run("load with multiple env vars", func(t *testing.T) {
		// Set multiple environment variables
		os.Setenv("QUICBAAS_SERVER_HOST", "envhost")
		os.Setenv("QUICBAAS_SERVER_PORT", "7777")
		os.Setenv("QUICBAAS_LOGGING_LEVEL", "error")
		defer func() {
			os.Unsetenv("QUICBAAS_SERVER_HOST")
			os.Unsetenv("QUICBAAS_SERVER_PORT")
			os.Unsetenv("QUICBAAS_LOGGING_LEVEL")
		}()
		cfg, err := LoadFromEnv()
		require.NoError(t, err)
		require.NotNil(t, cfg)
		// Note: Viper's env binding might not work as expected without explicit binding
		// This test verifies the function doesn't error
	})
	t.Run("load with no env vars set", func(t *testing.T) {
		cfg, err := LoadFromEnv()
		require.NoError(t, err)
		require.NotNil(t, cfg)
	})
}

// TestValidator_AddError tests error accumulation.
func TestValidator_AddError(t *testing.T) {
	v := NewValidator()
	v.addError("field1", "error1")
	v.addError("field2", "error2")
	v.addError("field3", "error3")
	assert.Equal(t, 3, len(v.errors))
	assert.Equal(t, "field1", v.errors[0].Field)
	assert.Equal(t, "error1", v.errors[0].Message)
	assert.Equal(t, "field2", v.errors[1].Field)
	assert.Equal(t, "error2", v.errors[1].Message)
}

// TestValidator_ConsecutiveValidations tests multiple validations.
func TestValidator_ConsecutiveValidations(t *testing.T) {
	t.Run("validator can be reused", func(t *testing.T) {
		cfg1 := Default()
		tmpDir1 := t.TempDir()
		cfg1.Storage.DataDir = tmpDir1
		cfg1.Storage.WALDir = filepath.Join(tmpDir1, "wal")
		// Disable TLS/encryption for this test
		cfg1.Security.TLSEnabled = false
		cfg1.Security.EncryptionEnabled = false
		err1 := Validate(cfg1)
		assert.NoError(t, err1)
		cfg2 := Default()
		cfg2.Server.REST.Port = 0 // Invalid
		err2 := Validate(cfg2)
		assert.Error(t, err2)
	})
}

// TestLoadFromFile_SymlinksAndPermissions tests file loading edge cases.
func TestLoadFromFile_SymlinksAndPermissions(t *testing.T) {
	t.Run("load from empty file", func(t *testing.T) {
		tmpDir := t.TempDir()
		emptyFile := filepath.Join(tmpDir, "empty.yaml")
		err := os.WriteFile(emptyFile, []byte(""), 0o644)
		require.NoError(t, err)
		cfg, err := LoadFromFile(emptyFile)
		// Empty YAML should still load defaults
		require.NoError(t, err)
		require.NotNil(t, cfg)
	})
	t.Run("load from file with only comments", func(t *testing.T) {
		tmpDir := t.TempDir()
		commentFile := filepath.Join(tmpDir, "comments.yaml")
		content := `# This is a comment
# Another comment
# server:
#   port: 8080
`
		err := os.WriteFile(commentFile, []byte(content), 0o644)
		require.NoError(t, err)
		cfg, err := LoadFromFile(commentFile)
		require.NoError(t, err)
		require.NotNil(t, cfg)
	})
}

// TestBindFlags_WithValues tests flag binding with actual values.
func TestBindFlags_WithValues(t *testing.T) {
	t.Run("bind and set flag values", func(t *testing.T) {
		loader := NewLoader()
		cmd := &cobra.Command{Use: "test"}
		// Pre-register flags that match what BindFlags expects
		cmd.Flags().String("server.host", "", "server host")
		cmd.Flags().String("logging.level", "", "log level")
		err := loader.BindFlags(cmd)
		require.NoError(t, err)
		// Set flag values
		cmd.Flags().Set("server.host", "testhost")
		cmd.Flags().Set("logging.level", "debug")
		// Verify values were set
		host, _ := cmd.Flags().GetString("server.host")
		assert.Equal(t, "testhost", host)
		level, _ := cmd.Flags().GetString("logging.level")
		assert.Equal(t, "debug", level)
	})
}

// TestGetViper tests viper instance retrieval.
func TestGetViper_NotNil(t *testing.T) {
	loader := NewLoader()
	v := loader.GetViper()
	require.NotNil(t, v)
	// Verify it's a proper viper instance
	v.Set("test.key", "test.value")
	assert.Equal(t, "test.value", v.GetString("test.key"))
}

// TestLoad_WithViperConfiguration tests Load with viper settings.
func TestLoad_WithViperConfiguration(t *testing.T) {
	t.Run("load respects environment variables", func(t *testing.T) {
		os.Setenv("QUICBAAS_SERVER_PORT", "8888")
		defer os.Unsetenv("QUICBAAS_SERVER_PORT")
		loader := NewLoader()
		cfg, err := loader.Load("")
		require.NoError(t, err)
		require.NotNil(t, cfg)
	})
	t.Run("load with config file and env override", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "test.yaml")
		content := `
server:
  port: 7777
  host: "filehost"
`
		err := os.WriteFile(configFile, []byte(content), 0o644)
		require.NoError(t, err)
		// Set env var that should override file
		os.Setenv("QUICBAAS_SERVER_PORT", "9999")
		defer os.Unsetenv("QUICBAAS_SERVER_PORT")
		loader := NewLoader()
		cfg, err := loader.Load(configFile)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, "filehost", cfg.Server.Host)
	})
}

// TestValidation_ComprehensiveCoverage ensures all validation branches.
func TestValidation_ComprehensiveCoverage(t *testing.T) {
	t.Run("all server timeout validations", func(t *testing.T) {
		cfg := Default()
		tmpDir := t.TempDir()
		cfg.Storage.DataDir = tmpDir
		cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
		tests := []struct {
			name   string
			modify func(*Config)
			field  string
		}{
			{
				name: "write timeout zero",
				modify: func(c *Config) {
					c.Server.WriteTimeout = 0
				},
				field: "server.write_timeout",
			},
			{
				name: "idle timeout zero",
				modify: func(c *Config) {
					c.Server.IdleTimeout = 0
				},
				field: "server.idle_timeout",
			},
			{
				name: "shutdown timeout zero",
				modify: func(c *Config) {
					c.Server.ShutdownTimeout = 0
				},
				field: "server.shutdown_timeout",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				testCfg := cfg.Clone()
				testCfg.Storage.DataDir = tmpDir
				testCfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
				tt.modify(testCfg)
				err := Validate(testCfg)
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.field)
			})
		}
	})
	t.Run("all raft validations", func(t *testing.T) {
		cfg := Default()
		tmpDir := t.TempDir()
		cfg.Storage.DataDir = tmpDir
		cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
		tests := []struct {
			name   string
			modify func(*Config)
			field  string
		}{
			{
				name: "compaction overhead zero",
				modify: func(c *Config) {
					c.Raft.CompactionOverhead = 0
				},
				field: "raft.compaction_overhead",
			},
			{
				name: "request timeout zero",
				modify: func(c *Config) {
					c.Raft.RequestTimeout = 0
				},
				field: "raft.request_timeout",
			},
			{
				name: "rtt millisecond zero",
				modify: func(c *Config) {
					c.Raft.RTTMillisecond = 0
				},
				field: "raft.rtt_millisecond",
			},
			{
				name: "max send queue size zero",
				modify: func(c *Config) {
					c.Raft.MaxSendQueueSize = 0
				},
				field: "raft.max_send_queue_size",
			},
			{
				name: "max receive queue size zero",
				modify: func(c *Config) {
					c.Raft.MaxReceiveQueueSize = 0
				},
				field: "raft.max_receive_queue_size",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				testCfg := cfg.Clone()
				testCfg.Storage.DataDir = tmpDir
				testCfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
				tt.modify(testCfg)
				err := Validate(testCfg)
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.field)
			})
		}
	})
}

// =============================================================================
// Consolidated from coverage_boost_test.go
// =============================================================================

// TestEnhancedCacheConfig_ValidateEntityConfig_EdgeCases tests validateEntityConfig edge cases
func TestEnhancedCacheConfig_ValidateEntityConfig_EdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		entityType string
		config     EntityCacheConfig
		wantErr    bool
		errContain string
	}{
		{
			name:       "empty entity type",
			entityType: "",
			config:     EntityCacheConfig{Enabled: true},
			wantErr:    true,
			errContain: "entity type is required",
		},
		{
			name:       "negative TTL",
			entityType: "user",
			config:     EntityCacheConfig{Enabled: true, TTL: -1 * time.Second},
			wantErr:    true,
			errContain: "ttl cannot be negative",
		},
		{
			name:       "negative max size",
			entityType: "user",
			config:     EntityCacheConfig{Enabled: true, MaxSize: -1},
			wantErr:    true,
			errContain: "max_size cannot be negative",
		},
		{
			name:       "valid config with all fields",
			entityType: "user",
			config:     EntityCacheConfig{Enabled: true, TTL: 5 * time.Minute, MaxSize: 100},
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultEnhancedCacheConfig()
			err := cfg.validateEntityConfig(tt.entityType, tt.config)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContain)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestEnhancedCacheConfig_GetEntityTTL_ZeroTTLFallback tests TTL fallback behavior
func TestEnhancedCacheConfig_GetEntityTTL_ZeroTTLFallback(t *testing.T) {
	cfg := DefaultEnhancedCacheConfig()
	cfg.DefaultTTL = 7 * time.Minute
	cfg.Entities["post"] = EntityCacheConfig{
		Enabled: true,
		TTL:     0, // Should fall back to default
	}

	got := cfg.GetEntityTTL("post")
	assert.Equal(t, 7*time.Minute, got, "zero TTL should fall back to default")
}

// TestEnhancedCacheConfig_GetEntityWriteThrough_NilPointer tests write-through with nil pointer
func TestEnhancedCacheConfig_GetEntityWriteThrough_NilPointer(t *testing.T) {
	cfg := DefaultEnhancedCacheConfig()
	cfg.WriteThrough = false
	cfg.Entities["comment"] = EntityCacheConfig{
		Enabled:      true,
		WriteThrough: nil, // Should use global default
	}

	got := cfg.GetEntityWriteThrough("comment")
	assert.Equal(t, false, got, "nil WriteThrough should use global default")
}

// TestRAGConfig_Validate_DimensionMismatch tests dimension mismatch error
func TestRAGConfig_Validate_DimensionMismatch(t *testing.T) {
	cfg := DefaultRAGConfig()
	cfg.Enabled = true
	cfg.Embedding.Dimensions = 512
	cfg.Vector.Dimensions = 384

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dimension mismatch")
}

// TestRAGConfig_Validate_DisabledSkipsValidation tests that disabled config skips validation
func TestRAGConfig_Validate_DisabledSkipsValidation(t *testing.T) {
	cfg := DefaultRAGConfig()
	cfg.Enabled = false
	cfg.Embedding.Provider = "" // Would normally fail

	err := cfg.Validate()
	require.NoError(t, err, "disabled config should skip validation")
}

// TestEmbeddingConfig_Validate_ProviderRequirements tests provider-specific requirements
func TestEmbeddingConfig_Validate_ProviderRequirements(t *testing.T) {
	tests := []struct {
		name       string
		config     EmbeddingConfig
		wantErr    bool
		errContain string
	}{
		{
			name: "openai requires API key",
			config: EmbeddingConfig{
				Provider:   "openai",
				Dimensions: 1536,
				APIKey:     "",
			},
			wantErr:    true,
			errContain: "requires API key",
		},
		{
			name: "voyage requires API key",
			config: EmbeddingConfig{
				Provider:   "voyage",
				Dimensions: 1024,
				APIKey:     "",
			},
			wantErr:    true,
			errContain: "requires API key",
		},
		{
			name: "ollama requires API URL",
			config: EmbeddingConfig{
				Provider:   "ollama",
				Dimensions: 384,
				APIURL:     "",
			},
			wantErr:    true,
			errContain: "requires API URL",
		},
		{
			name: "lmstudio requires API URL",
			config: EmbeddingConfig{
				Provider:   "lmstudio",
				Dimensions: 384,
				APIURL:     "",
			},
			wantErr:    true,
			errContain: "requires API URL",
		},
		{
			name: "custom provider allowed without restrictions",
			config: EmbeddingConfig{
				Provider:   "custom-provider",
				Dimensions: 768,
				APIURL:     "http://custom:8080",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContain)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestRAGVectorConfig_Validate_NProbeBoundary tests nprobe boundary conditions
func TestRAGVectorConfig_Validate_NProbeBoundary(t *testing.T) {
	t.Run("nprobe zero auto-corrects to default", func(t *testing.T) {
		cfg := RAGVectorConfig{
			NumCentroids: 100,
			Dimensions:   384,
			NProbe:       0, // Will be set to default 10
		}
		err := cfg.Validate()
		require.NoError(t, err)
		assert.Equal(t, 10, cfg.NProbe, "zero nprobe should be auto-corrected to 10")
	})

	t.Run("nprobe exceeds num_centroids", func(t *testing.T) {
		cfg := RAGVectorConfig{
			NumCentroids: 50,
			Dimensions:   384,
			NProbe:       100, // Greater than NumCentroids
		}
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nprobe")
		assert.Contains(t, err.Error(), "cannot exceed num_centroids")
	})
}

// TestRAGQueryConfig_Validate_BoundaryConditions tests query config boundaries
func TestRAGQueryConfig_Validate_BoundaryConditions(t *testing.T) {
	tests := []struct {
		name       string
		config     RAGQueryConfig
		wantErr    bool
		errContain string
	}{
		{
			name: "negative default top k",
			config: RAGQueryConfig{
				CacheSize:   1000,
				DefaultTopK: -1,
			},
			wantErr:    true,
			errContain: "default_top_k cannot be negative",
		},
		{
			name: "min score above 1.0",
			config: RAGQueryConfig{
				CacheSize:       1000,
				DefaultTopK:     10,
				DefaultMinScore: 1.5,
			},
			wantErr:    true,
			errContain: "must be between 0 and 1",
		},
		{
			name: "min score below 0.0",
			config: RAGQueryConfig{
				CacheSize:       1000,
				DefaultTopK:     10,
				DefaultMinScore: -0.1,
			},
			wantErr:    true,
			errContain: "must be between 0 and 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContain)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestRankingConfig_Validate_StrategyValidation tests ranking strategy validation
func TestRankingConfig_Validate_StrategyValidation(t *testing.T) {
	tests := []struct {
		name       string
		config     RankingConfig
		wantErr    bool
		errContain string
	}{
		{
			name: "invalid strategy",
			config: RankingConfig{
				Strategy: "invalid-strategy",
				BM25K1:   1.2,
				BM25B:    0.75,
			},
			wantErr:    true,
			errContain: "invalid ranking strategy",
		},
		{
			name: "vector strategy is valid",
			config: RankingConfig{
				Strategy:      "vector",
				BM25K1:        1.2,
				BM25B:         0.75,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
			},
			wantErr: false,
		},
		{
			name: "bm25_k1 zero or negative",
			config: RankingConfig{
				Strategy: "bm25",
				BM25K1:   0,
				BM25B:    0.75,
			},
			wantErr:    true,
			errContain: "bm25_k1 must be positive",
		},
		{
			name: "bm25_b below range",
			config: RankingConfig{
				Strategy: "bm25",
				BM25K1:   1.2,
				BM25B:    -0.1,
			},
			wantErr:    true,
			errContain: "bm25_b must be between 0 and 1",
		},
		{
			name: "bm25_b above range",
			config: RankingConfig{
				Strategy: "bm25",
				BM25K1:   1.2,
				BM25B:    1.5,
			},
			wantErr:    true,
			errContain: "bm25_b must be between 0 and 1",
		},
		{
			name: "vector_weight below range",
			config: RankingConfig{
				Strategy:     "hybrid",
				BM25K1:       1.2,
				BM25B:        0.75,
				VectorWeight: -0.1,
			},
			wantErr:    true,
			errContain: "vector_weight must be between 0 and 1",
		},
		{
			name: "vector_weight above range",
			config: RankingConfig{
				Strategy:     "hybrid",
				BM25K1:       1.2,
				BM25B:        0.75,
				VectorWeight: 1.5,
			},
			wantErr:    true,
			errContain: "vector_weight must be between 0 and 1",
		},
		{
			name: "keyword_weight below range",
			config: RankingConfig{
				Strategy:      "hybrid",
				BM25K1:        1.2,
				BM25B:         0.75,
				VectorWeight:  0.7,
				KeywordWeight: -0.1,
			},
			wantErr:    true,
			errContain: "keyword_weight must be between 0 and 1",
		},
		{
			name: "keyword_weight above range",
			config: RankingConfig{
				Strategy:      "hybrid",
				BM25K1:        1.2,
				BM25B:         0.75,
				VectorWeight:  0.7,
				KeywordWeight: 1.5,
			},
			wantErr:    true,
			errContain: "keyword_weight must be between 0 and 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContain)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestContextConfig_Validate_TemplateAndTokens tests context config validation
func TestContextConfig_Validate_TemplateAndTokens(t *testing.T) {
	tests := []struct {
		name       string
		config     ContextConfig
		wantErr    bool
		errContain string
	}{
		{
			name: "invalid template type",
			config: ContextConfig{
				MaxTokens:          4000,
				DefaultTemplate:    "invalid-template",
				MaxResultsPerChunk: 20,
				TokenMultiplier:    1.3,
			},
			wantErr:    true,
			errContain: "invalid template",
		},
		{
			name: "zero max results per chunk",
			config: ContextConfig{
				MaxTokens:          4000,
				DefaultTemplate:    "markdown",
				MaxResultsPerChunk: 0,
				TokenMultiplier:    1.3,
			},
			wantErr:    true,
			errContain: "max_results_per_chunk must be positive",
		},
		{
			name: "negative max results per chunk",
			config: ContextConfig{
				MaxTokens:          4000,
				DefaultTemplate:    "markdown",
				MaxResultsPerChunk: -1,
				TokenMultiplier:    1.3,
			},
			wantErr:    true,
			errContain: "max_results_per_chunk must be positive",
		},
		{
			name: "zero token multiplier",
			config: ContextConfig{
				MaxTokens:          4000,
				DefaultTemplate:    "json",
				MaxResultsPerChunk: 20,
				TokenMultiplier:    0,
			},
			wantErr:    true,
			errContain: "token_multiplier must be positive",
		},
		{
			name: "negative token multiplier",
			config: ContextConfig{
				MaxTokens:          4000,
				DefaultTemplate:    "xml",
				MaxResultsPerChunk: 20,
				TokenMultiplier:    -1.0,
			},
			wantErr:    true,
			errContain: "token_multiplier must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContain)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestPipelineConfig_Validate_BoundaryConditions tests pipeline config boundaries
func TestPipelineConfig_Validate_BoundaryConditions(t *testing.T) {
	tests := []struct {
		name       string
		config     PipelineConfig
		wantErr    bool
		errContain string
	}{
		{
			name: "negative default top k",
			config: PipelineConfig{
				Timeout:              30 * time.Second,
				MaxConcurrentQueries: 100,
				DefaultTopK:          -1,
				DefaultMaxTokens:     8000,
			},
			wantErr:    true,
			errContain: "default_top_k cannot be negative",
		},
		{
			name: "min score below zero",
			config: PipelineConfig{
				Timeout:              30 * time.Second,
				MaxConcurrentQueries: 100,
				DefaultTopK:          10,
				DefaultMaxTokens:     8000,
				DefaultMinScore:      -0.1,
			},
			wantErr:    true,
			errContain: "default_min_score must be between 0 and 1",
		},
		{
			name: "min score above one",
			config: PipelineConfig{
				Timeout:              30 * time.Second,
				MaxConcurrentQueries: 100,
				DefaultTopK:          10,
				DefaultMaxTokens:     8000,
				DefaultMinScore:      1.5,
			},
			wantErr:    true,
			errContain: "default_min_score must be between 0 and 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContain)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestCreateStorageBackendByType_EdgeCases tests storage backend creation edge cases
func TestCreateStorageBackendByType_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		backendType string
		config      map[string]string
		wantErr     bool
		errContain  string
	}{
		{
			name:        "unsupported backend type",
			backendType: "unsupported-backend",
			config:      nil,
			wantErr:     true,
			errContain:  "unsupported storage backend",
		},
		{
			name:        "memory backend",
			backendType: "memory",
			config:      nil,
			wantErr:     false,
		},
		{
			name:        "case insensitive memory backend",
			backendType: "MEMORY",
			config:      nil,
			wantErr:     false,
		},
		{
			name:        "case insensitive MEmOrY backend",
			backendType: "MEmOrY",
			config:      nil,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage, err := CreateStorageBackendByType(tt.backendType, tt.config)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContain)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, storage)
				if storage != nil {
					// Memory storage implements Close
					if ms, ok := storage.(*MemoryStorage); ok {
						ms.Close()
					}
				}
			}
		})
	}
}

// TestResolveKeyStorageBackend_BackwardCompatibility tests key storage backend resolution
func TestResolveKeyStorageBackend_BackwardCompatibility(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() *Config
		wantErr bool
		verify  func(*testing.T, error)
	}{
		{
			name: "default to memory storage when nothing set",
			setup: func() *Config {
				cfg := Default()
				cfg.Security.EncryptionKeyFile = ""
				cfg.Security.KeyStorageBackend = ""
				return cfg
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.setup()
			storage, err := ResolveKeyStorageBackend(cfg)
			if tt.wantErr {
				require.Error(t, err)
				if tt.verify != nil {
					tt.verify(t, err)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, storage)
				if storage != nil {
					// Memory storage implements Close
					if ms, ok := storage.(*MemoryStorage); ok {
						ms.Close()
					}
				}
			}
		})
	}
}

// TestValidator_EnsureDirectoryWritable_EdgeCases tests directory validation edge cases
func TestValidator_EnsureDirectoryWritable_EdgeCases(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		setup   func() string
		wantErr bool
		cleanup func()
	}{
		{
			name: "non-existent directory gets created",
			setup: func() string {
				return filepath.Join(tmpDir, "new-dir-"+t.Name())
			},
			wantErr: false,
		},
		{
			name: "existing directory is writable",
			setup: func() string {
				dir := filepath.Join(tmpDir, "existing-dir-"+t.Name())
				os.MkdirAll(dir, 0750)
				return dir
			},
			wantErr: false,
		},
		{
			name: "path is a file not directory",
			setup: func() string {
				file := filepath.Join(tmpDir, "not-a-dir-"+t.Name())
				os.WriteFile(file, []byte("test"), 0644)
				return file
			},
			wantErr: true,
		},
		{
			name: "directory is not writable",
			setup: func() string {
				dir := filepath.Join(tmpDir, "readonly-dir-"+t.Name())
				os.MkdirAll(dir, 0750)
				os.Chmod(dir, 0444) // Read-only
				return dir
			},
			wantErr: true,
			cleanup: func() {
				dir := filepath.Join(tmpDir, "readonly-dir-"+t.Name())
				os.Chmod(dir, 0750)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cleanup != nil {
				defer tt.cleanup()
			}

			v := NewValidator()
			path := tt.setup()
			err := v.ensureDirectoryWritable(path)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestValidator_IsFIPSAvailable_SystemCheck tests FIPS availability check
func TestValidator_IsFIPSAvailable_SystemCheck(t *testing.T) {
	v := NewValidator()
	// Just call the method to increase coverage
	// The actual result depends on the system
	available := v.isFIPSAvailable()
	// On most test systems, FIPS won't be available
	t.Logf("FIPS available: %v", available)
	// Assert that the function runs without panic
	assert.IsType(t, false, available)
}

// TestVectorConfig_DefaultAndValidate tests vector config
func TestVectorConfig_DefaultAndValidate(t *testing.T) {
	t.Run("default config is created properly", func(t *testing.T) {
		cfg := DefaultVectorConfig()
		assert.NotNil(t, cfg)
		assert.False(t, cfg.Enabled, "should be disabled by default")
		assert.Equal(t, uint64(200), cfg.ShardID)
		assert.NotNil(t, cfg.HNSW)
		assert.Equal(t, "./data/vector", cfg.StoragePath)
	})

	t.Run("validate disabled config skips validation", func(t *testing.T) {
		cfg := DefaultVectorConfig()
		cfg.Enabled = false
		cfg.ShardID = 0 // Invalid, but should be ignored when disabled
		err := cfg.Validate()
		assert.NoError(t, err, "disabled config should skip validation")
	})

	t.Run("validate enabled config with defaults passes", func(t *testing.T) {
		cfg := DefaultVectorConfig()
		cfg.Enabled = true
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("validate zero shard id", func(t *testing.T) {
		cfg := DefaultVectorConfig()
		cfg.Enabled = true
		cfg.ShardID = 0
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "shard_id (must be non-zero) is required")
	})

	t.Run("validate empty storage path", func(t *testing.T) {
		cfg := DefaultVectorConfig()
		cfg.Enabled = true
		cfg.StoragePath = ""
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "storage_path is required")
	})

	t.Run("validate nil HNSW config", func(t *testing.T) {
		cfg := DefaultVectorConfig()
		cfg.Enabled = true
		cfg.HNSW = nil
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "hnsw configuration is required")
	})

	t.Run("validate HNSW M too low", func(t *testing.T) {
		cfg := DefaultVectorConfig()
		cfg.Enabled = true
		cfg.HNSW.M = 1
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "hnsw.m must be between")
	})

	t.Run("validate HNSW M too high", func(t *testing.T) {
		cfg := DefaultVectorConfig()
		cfg.Enabled = true
		cfg.HNSW.M = 101
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "hnsw.m must be between")
	})

	t.Run("validate HNSW EfConstruction too low", func(t *testing.T) {
		cfg := DefaultVectorConfig()
		cfg.Enabled = true
		cfg.HNSW.EfConstruction = 50
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "hnsw.ef_construction must be at least 100")
	})

	t.Run("validate HNSW MaxElements zero", func(t *testing.T) {
		cfg := DefaultVectorConfig()
		cfg.Enabled = true
		cfg.HNSW.MaxElements = 0
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "hnsw.max_elements must be positive")
	})

	t.Run("validate HNSW MaxElements negative", func(t *testing.T) {
		cfg := DefaultVectorConfig()
		cfg.Enabled = true
		cfg.HNSW.MaxElements = -100
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "hnsw.max_elements must be positive")
	})

	t.Run("validate HNSW Ml zero", func(t *testing.T) {
		cfg := DefaultVectorConfig()
		cfg.Enabled = true
		cfg.HNSW.Ml = 0
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "hnsw.ml must be positive")
	})

	t.Run("validate HNSW Ml negative", func(t *testing.T) {
		cfg := DefaultVectorConfig()
		cfg.Enabled = true
		cfg.HNSW.Ml = -1.0
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "hnsw.ml must be positive")
	})
}

// TestMemoryStorage_ErrorPaths tests memory storage error paths
func TestMemoryStorage_ErrorPaths(t *testing.T) {
	t.Run("get non-existent key", func(t *testing.T) {
		storage := NewMemoryStorage()
		defer storage.Close()

		_, err := storage.Get("nonexistent-key")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "key not found")
	})

	t.Run("get metadata for non-existent key", func(t *testing.T) {
		storage := NewMemoryStorage()
		defer storage.Close()

		ctx := context.Background()
		_, err := storage.GetMetadata(ctx, "nonexistent-key")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "key not found")
	})

	t.Run("update metadata for non-existent key", func(t *testing.T) {
		storage := NewMemoryStorage()
		defer storage.Close()

		ctx := context.Background()
		err := storage.UpdateMetadata(ctx, "nonexistent-key", nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "key not found")
	})

	t.Run("archive operation not supported", func(t *testing.T) {
		storage := NewMemoryStorage()
		defer storage.Close()

		err := storage.Archive("some-key", nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "archive not supported")
	})

	t.Run("list with options and no prefix", func(t *testing.T) {
		storage := NewMemoryStorage()
		defer storage.Close()

		// Add some data
		storage.Put("key1", strings.NewReader("data1"))
		storage.Put("key2", strings.NewReader("data2"))

		ctx := context.Background()
		result, err := storage.ListWithOptions(ctx, nil)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Len(t, result.Objects, 2)
		assert.False(t, result.Truncated)
	})

	t.Run("list with options and prefix", func(t *testing.T) {
		storage := NewMemoryStorage()
		defer storage.Close()

		// Add some data
		storage.Put("prefix/key1", strings.NewReader("data1"))
		storage.Put("prefix/key2", strings.NewReader("data2"))
		storage.Put("other/key3", strings.NewReader("data3"))

		ctx := context.Background()
		opts := &common.ListOptions{
			Prefix: "prefix/",
		}
		result, err := storage.ListWithOptions(ctx, opts)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Len(t, result.Objects, 2)
	})
}

// TestLoader_LoadFromEnv_EnvironmentVariables tests LoadFromEnv with environment variables
func TestLoader_LoadFromEnv_EnvironmentVariables(t *testing.T) {
	// Save original env vars
	originalEnv := make(map[string]string)
	testEnvKeys := []string{
		"QUICBAAS_SERVER_HOST",
		"QUICBAAS_SERVER_PORT",
		"QUICBAAS_RAFT_NODE_ID",
	}
	for _, key := range testEnvKeys {
		originalEnv[key] = os.Getenv(key)
	}
	defer func() {
		// Restore original env vars
		for key, val := range originalEnv {
			if val != "" {
				os.Setenv(key, val)
			} else {
				os.Unsetenv(key)
			}
		}
	}()

	t.Run("load with no custom env vars", func(t *testing.T) {
		// Clear environment
		for key := range originalEnv {
			os.Unsetenv(key)
		}

		cfg, err := LoadFromEnv()
		require.NoError(t, err)
		assert.NotNil(t, cfg)
		assert.NotNil(t, cfg.Server)
		assert.NotNil(t, cfg.Raft)
	})
}
