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
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

func TestRootCommand(t *testing.T) {
	// Reset root command for testing
	rootCmd.SetArgs([]string{"--help"})

	// Execute command
	err := rootCmd.Execute()
	require.NoError(t, err)
}

func TestInitConfig(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func(t *testing.T) string
		expectError bool
	}{
		{
			name: "default config without file",
			setupFunc: func(t *testing.T) string {
				cfgFile = ""
				viper.Reset()
				return ""
			},
			expectError: false,
		},
		{
			name: "valid yaml config file",
			setupFunc: func(t *testing.T) string {
				tmpDir := t.TempDir()
				configPath := filepath.Join(tmpDir, ".quicbaas.yaml")

				configContent := `
server:
  rest:
    port: 9090
  grpc:
    port: 9091

logging:
  level: debug
  format: json

storage:
  data_dir: /tmp/test-data
`
				err := os.WriteFile(configPath, []byte(configContent), 0o644)
				require.NoError(t, err)

				cfgFile = configPath
				viper.Reset()
				return tmpDir
			},
			expectError: false,
		},
		{
			name: "invalid yaml config file",
			setupFunc: func(t *testing.T) string {
				tmpDir := t.TempDir()
				configPath := filepath.Join(tmpDir, ".quicbaas.yaml")

				// Invalid YAML content
				err := os.WriteFile(configPath, []byte("invalid: yaml: content:"), 0o644)
				require.NoError(t, err)

				cfgFile = configPath
				viper.Reset()
				return tmpDir
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			if tt.setupFunc != nil {
				tt.setupFunc(t)
			}

			// Initialize configuration
			err := initConfig()

			// Verify result
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, cfg)
			}

			// Cleanup
			viper.Reset()
		})
	}
}

func TestInitConfigPrecedence(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".quicbaas.yaml")

	// Create config file with specific values
	configContent := `
server:
  rest:
    port: 7070

logging:
  level: warn
`
	err := os.WriteFile(configPath, []byte(configContent), 0o644)
	require.NoError(t, err)

	// Reset viper
	viper.Reset()
	cfgFile = configPath

	// Set environment variable (should override config file)
	os.Setenv("QUICBAAS_SERVER_REST_PORT", "8888")
	defer os.Unsetenv("QUICBAAS_SERVER_REST_PORT")

	// Initialize config
	err = initConfig()
	require.NoError(t, err)

	// Environment variable should take precedence over config file
	assert.Equal(t, 8888, cfg.Server.REST.Port)

	// Config file value should be used for non-overridden values
	assert.Equal(t, "warn", cfg.Logging.Level)

	// Cleanup
	viper.Reset()
}

func TestInitConfigEnvironmentVariables(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		validate func(t *testing.T, cfg *config.Config)
	}{
		{
			name: "server port from env",
			envVars: map[string]string{
				"QUICBAAS_SERVER_REST_PORT": "9999",
			},
			validate: func(t *testing.T, cfg *config.Config) {
				assert.Equal(t, 9999, cfg.Server.REST.Port)
			},
		},
		{
			name: "log level from env",
			envVars: map[string]string{
				"QUICBAAS_LOGGING_LEVEL": "debug",
			},
			validate: func(t *testing.T, cfg *config.Config) {
				assert.Equal(t, "debug", cfg.Logging.Level)
			},
		},
		{
			name: "data dir from env",
			envVars: map[string]string{
				"QUICBAAS_STORAGE_DATA_DIR": "/custom/data",
			},
			validate: func(t *testing.T, cfg *config.Config) {
				assert.Equal(t, "/custom/data", cfg.Storage.DataDir)
			},
		},
		{
			name: "fips mode from env",
			envVars: map[string]string{
				"QUICBAAS_SECURITY_FIPS_MODE": "true",
			},
			validate: func(t *testing.T, cfg *config.Config) {
				assert.True(t, cfg.Security.FIPSMode)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset
			viper.Reset()
			cfgFile = ""

			// Set environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			// Initialize config
			err := initConfig()
			require.NoError(t, err)

			// Validate
			tt.validate(t, cfg)

			// Cleanup
			viper.Reset()
		})
	}
}

func TestInitLogger(t *testing.T) {
	// Setup default config
	cfg = config.Default()

	// Initialize logger
	err := initLogger()
	require.NoError(t, err)
	assert.NotNil(t, log)

	// Verify logger configuration
	assert.Equal(t, cfg.Logging.Level, log.GetLevel())
}

func TestInitLoggerWithCustomConfig(t *testing.T) {
	tests := []struct {
		name      string
		logLevel  string
		logFormat string
	}{
		{
			name:      "debug level",
			logLevel:  "debug",
			logFormat: "json",
		},
		{
			name:      "error level",
			logLevel:  "error",
			logFormat: "json",
		},
		{
			name:      "console format",
			logLevel:  "info",
			logFormat: "console",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup config
			cfg = config.Default()
			cfg.Logging.Level = tt.logLevel
			cfg.Logging.Format = tt.logFormat

			// Initialize logger
			err := initLogger()
			require.NoError(t, err)
			assert.NotNil(t, log)

			// Verify logger level
			assert.Equal(t, tt.logLevel, log.GetLevel())
		})
	}
}

func TestGlobalFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		validate func(t *testing.T)
	}{
		{
			name: "config flag",
			args: []string{"--config", "/tmp/config.yaml"},
			validate: func(t *testing.T) {
				assert.Equal(t, "/tmp/config.yaml", cfgFile)
			},
		},
		{
			name: "log level flag",
			args: []string{"--log-level", "debug"},
			validate: func(t *testing.T) {
				assert.Equal(t, "debug", logLevel)
			},
		},
		{
			name: "fips flag",
			args: []string{"--fips"},
			validate: func(t *testing.T) {
				assert.True(t, fipsMode)
			},
		},
		{
			name: "data dir flag",
			args: []string{"--data-dir", "/custom/data"},
			validate: func(t *testing.T) {
				assert.Equal(t, "/custom/data", dataDir)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset flags
			cfgFile = ""
			logLevel = "info"
			fipsMode = false
			dataDir = "./data"

			// Parse flags
			rootCmd.SetArgs(tt.args)
			rootCmd.ParseFlags(tt.args)

			// Validate
			tt.validate(t)
		})
	}
}

func TestSubcommandsRegistered(t *testing.T) {
	// Verify subcommands are registered.
	// Note: The "start" server command has been moved to quicbaasd daemon.
	commands := rootCmd.Commands()

	commandNames := make([]string, len(commands))
	for i, cmd := range commands {
		commandNames[i] = cmd.Name()
	}

	// Core CLI commands that should be registered.
	// Note: "put" and "get" are subcommands of "data", not top-level commands.
	assert.Contains(t, commandNames, "version", "version command should be registered")
	assert.Contains(t, commandNames, "data", "data command should be registered")
	assert.Contains(t, commandNames, "cluster", "cluster command should be registered")
	assert.Contains(t, commandNames, "auth", "auth command should be registered")
	assert.Contains(t, commandNames, "vector", "vector command should be registered")
}

func TestConfigFileFormats(t *testing.T) {
	tests := []struct {
		name       string
		format     string
		content    string
		fileExt    string
		shouldWork bool
	}{
		{
			name:   "YAML format",
			format: "yaml",
			content: `
server:
  rest:
    port: 8080
`,
			fileExt:    ".yaml",
			shouldWork: true,
		},
		{
			name:   "YML format",
			format: "yml",
			content: `
server:
  rest:
    port: 8080
`,
			fileExt:    ".yml",
			shouldWork: true,
		},
		{
			name:   "JSON format",
			format: "json",
			content: `{
  "server": {
    "rest": {
      "port": 8080
    }
  }
}`,
			fileExt:    ".json",
			shouldWork: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, ".quicbaas"+tt.fileExt)

			err := os.WriteFile(configPath, []byte(tt.content), 0o644)
			require.NoError(t, err)

			viper.Reset()
			cfgFile = configPath

			err = initConfig()

			if tt.shouldWork {
				assert.NoError(t, err)
				assert.Equal(t, 8080, cfg.Server.REST.Port)
			} else {
				assert.Error(t, err)
			}

			viper.Reset()
		})
	}
}

// BenchmarkInitConfig benchmarks config initialization.
func BenchmarkInitConfig(b *testing.B) {
	cfgFile = ""

	for i := 0; i < b.N; i++ {
		viper.Reset()
		_ = initConfig()
	}
}

// BenchmarkInitLogger benchmarks logger initialization.
func BenchmarkInitLogger(b *testing.B) {
	cfg = config.Default()

	for i := 0; i < b.N; i++ {
		_ = initLogger()
	}
}
