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
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bytes"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// TestRootCommand tests the root command functionality.
func TestRootCommand(t *testing.T) {
	t.Run("RootCommandExists", func(t *testing.T) {
		assert.NotNil(t, rootCmd)
		assert.Equal(t, "quicbaasd", rootCmd.Use)
		assert.NotEmpty(t, rootCmd.Short)
		assert.NotEmpty(t, rootCmd.Long)

		// Check persistent flags exist on rootCmd.
		assert.NotNil(t, rootCmd.PersistentFlags().Lookup("config"))
		assert.NotNil(t, rootCmd.PersistentFlags().Lookup("log-level"))
		assert.NotNil(t, rootCmd.PersistentFlags().Lookup("log-file"))
		assert.NotNil(t, rootCmd.PersistentFlags().Lookup("fips"))
		assert.NotNil(t, rootCmd.PersistentFlags().Lookup("data-dir"))

		// Server flags are on subcommands (startCmd, nodeCmd, clusterCmd), not rootCmd.
		// Check they exist on startCmd as representative.
		assert.NotNil(t, startCmd.Flags().Lookup("unix-socket"))
		assert.NotNil(t, startCmd.Flags().Lookup("http-port"))
		assert.NotNil(t, startCmd.Flags().Lookup("grpc-port"))

		// Raft flags are on nodeCmd and clusterCmd only.
		assert.NotNil(t, nodeCmd.Flags().Lookup("raft-port"))
		assert.NotNil(t, nodeCmd.Flags().Lookup("raft-address"))
		assert.NotNil(t, nodeCmd.Flags().Lookup("node-id"))
	})

	t.Run("FlagDefaults", func(t *testing.T) {
		// Server flags are on subcommands, check startCmd.
		unixSocketFlag := startCmd.Flags().Lookup("unix-socket")
		require.NotNil(t, unixSocketFlag, "unix-socket flag should exist on startCmd")
		assert.Equal(t, config.DefaultUnixSocket, unixSocketFlag.DefValue)

		httpPortFlag := startCmd.Flags().Lookup("http-port")
		require.NotNil(t, httpPortFlag, "http-port flag should exist on startCmd")
		assert.Equal(t, "0", httpPortFlag.DefValue)

		grpcPortFlag := startCmd.Flags().Lookup("grpc-port")
		require.NotNil(t, grpcPortFlag, "grpc-port flag should exist on startCmd")
		assert.Equal(t, "0", grpcPortFlag.DefValue)

		// Raft flags are on nodeCmd and clusterCmd.
		raftPortFlag := nodeCmd.Flags().Lookup("raft-port")
		require.NotNil(t, raftPortFlag, "raft-port flag should exist on nodeCmd")
		assert.Equal(t, "63000", raftPortFlag.DefValue)

		nodeIDFlag := nodeCmd.Flags().Lookup("node-id")
		require.NotNil(t, nodeIDFlag, "node-id flag should exist on nodeCmd")
		assert.Equal(t, "1", nodeIDFlag.DefValue)
	})
}

// TestGetSocketDir tests the getSocketDir function.
func TestGetSocketDir(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"absolute path", "/var/run/quicbaas/quicbaas.sock", "/var/run/quicbaas"},
		{"deep path", "/a/b/c/d/e.sock", "/a/b/c/d"},
		{"trailing slash", "/var/run/", "/var/run"},
		{"root level", "/db.sock", ""},
		{"no directory", "quicbaas.sock", ""},
		{"empty path", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, getSocketDir(tt.input))
		})
	}
}

// createTestLogger creates a logger for tests.
func createTestLogger(t *testing.T) *logger.Logger {
	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	if err != nil {
		t.Fatalf("Failed to create test logger: %v", err)
	}
	return testLog
}

// =============================================================================
// Consolidated from coverage_exit_test.go
// =============================================================================

// TestRootCmd_ExecuteWithVersion tests executing root command with version.
func TestRootCmd_Execute(t *testing.T) {
	// Each run fails with an unknown command or flag error if the subcommand
	// or flag is not registered on rootCmd.
	for _, args := range [][]string{
		{"--help"},
		{"--version"},
		{"start", "--help"},
		{"node", "--help"},
		{"cluster", "--help"},
		{"version"},
		{"version", "--short"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			rootCmd.SetArgs(args)
			assert.NoError(t, rootCmd.Execute())
		})
	}
}

// TestPrepareDaemon_DataDirCreationFail tests prepareDaemon when data directory
// creation fails due to permissions.
// The path below lives under /proc, where mkdir is refused for every uid --
// procfs simply does not support it. No root guard is needed or wanted
// (testing-guidelines.md, Fault Injection).
// TestPrepareDaemon covers each branch of prepareDaemon: the mode and
// listener logging, routing validation, and data and socket directory
// creation, including the failure of each.
func TestPrepareDaemon(t *testing.T) {
	log = createTestLogger(t)

	tests := []struct {
		name string
		// setup adjusts a default standalone config whose data dir is dir
		// and returns the directory prepareDaemon must leave in place.
		setup   func(dir string) string
		fips    bool
		wantErr string
	}{
		{
			name: "standalone with no listeners",
			setup: func(dir string) string {
				cfg.Server.Unix.Enabled = false
				cfg.Server.GRPC.Enabled = false
				cfg.Server.REST.Enabled = false
				cfg.Discovery.Enabled = false
				return dir
			},
		},
		{
			name: "standalone with every listener, discovery and FIPS",
			setup: func(dir string) string {
				cfg.Server.Unix.Socket = filepath.Join(dir, "test.sock")
				cfg.Server.GRPC.Enabled = true
				cfg.Server.GRPC.Port = 19090
				cfg.Server.REST.Enabled = true
				cfg.Server.REST.Port = 18080
				cfg.Discovery.Enabled = true
				cfg.Discovery.Type = "multicast"
				return dir
			},
			fips: true,
		},
		{
			name: "cluster mode logs raft config",
			setup: func(dir string) string {
				cfg.Server.Mode = config.ServerModeCluster
				cfg.Server.Unix.Socket = filepath.Join(dir, "cluster.sock")
				cfg.Raft.NodeID = 42
				cfg.Raft.RaftAddress = "192.168.1.100:63000"
				return dir
			},
		},
		{
			name: "devcluster mode logs raft config",
			setup: func(dir string) string {
				cfg.Server.Mode = config.ServerModeDevCluster
				cfg.Server.Unix.Socket = filepath.Join(dir, "devcluster.sock")
				cfg.Raft.NodeID = 7
				cfg.Raft.RaftAddress = "10.1.1.1:63000"
				return dir
			},
		},
		{
			name: "existing data directory",
			setup: func(dir string) string {
				dataDir := filepath.Join(dir, "existing-data")
				require.NoError(t, os.MkdirAll(dataDir, 0o755))
				cfg.Storage.DataDir = dataDir
				cfg.Server.Unix.Enabled = false
				return dataDir
			},
		},
		{
			name: "nested socket directory is created",
			setup: func(dir string) string {
				cfg.Server.Unix.Enabled = true
				cfg.Server.Unix.Socket = filepath.Join(dir, "a", "b", "c", "deep.sock")
				return filepath.Join(dir, "a", "b", "c")
			},
		},
		{
			name: "socket without directory component",
			setup: func(dir string) string {
				cfg.Server.Unix.Enabled = true
				cfg.Server.Unix.Socket = "bare-socket.sock"
				return dir
			},
		},
		{
			name: "valid routing config",
			setup: func(dir string) string {
				cfg.Server.Shards = []uint64{100, 200}
				cfg.Server.EntityShardPins = map[string]uint64{"user": 100}
				return dir
			},
		},
		{
			name: "invalid routing config",
			setup: func(dir string) string {
				cfg.Server.Shards = []uint64{100, 200}
				cfg.Server.EntityShardPins = map[string]uint64{"user": 999}
				return dir
			},
			wantErr: "invalid routing config",
		},
		{
			name: "data directory cannot be created",
			setup: func(string) string {
				cfg.Storage.DataDir = "/proc/nonexistent/data"
				cfg.Server.Unix.Enabled = false
				return ""
			},
			wantErr: "failed to create data directory",
		},
		{
			name: "socket directory cannot be created",
			setup: func(dir string) string {
				cfg.Server.Unix.Enabled = true
				cfg.Server.Unix.Socket = "/proc/nonexistent/socket.sock"
				return dir
			},
			wantErr: "failed to create socket directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg = config.Default()
			cfg.Server.Mode = config.ServerModeStandalone
			cfg.Storage.DataDir = dir
			wantDir := tt.setup(dir)
			fipsMode = tt.fips
			defer func() { fipsMode = false }()

			err := prepareDaemon(tt.name)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			info, statErr := os.Stat(wantDir)
			require.NoError(t, statErr)
			assert.True(t, info.IsDir())
		})
	}
}

// =============================================================================
// Consolidated from coverage_92_test.go
// =============================================================================

// =============================================================================
// Consolidated from coverage_boost_test.go
// =============================================================================

// =============================================================================
// Consolidated from coverage_91_test.go
// =============================================================================

// =============================================================================
// Consolidated from coverage_additional_test.go
// =============================================================================

// =============================================================================
// Consolidated from main_coverage_test.go
// =============================================================================

// TestValidateRoutingConfig tests the validateRoutingConfig function.
func TestValidateRoutingConfig(t *testing.T) {
	tests := []struct {
		name   string
		shards []uint64
		pins   map[string]uint64
		// wantErr lists the substrings the error must carry; nil means success.
		wantErr []string
	}{
		{name: "nil pins", shards: []uint64{1, 2, 3}},
		{name: "empty pins", shards: []uint64{1, 2, 3}, pins: map[string]uint64{}},
		{
			name:   "every pin found in shards",
			shards: []uint64{100, 200, 300},
			pins:   map[string]uint64{"user": 100, "session": 200, "role": 300},
		},
		{
			name:    "pin references non-existent shard",
			shards:  []uint64{100, 200},
			pins:    map[string]uint64{"user": 999},
			wantErr: []string{"user", "999"},
		},
		{
			name:    "one invalid pin among valid ones",
			shards:  []uint64{100, 200},
			pins:    map[string]uint64{"user": 100, "session": 999},
			wantErr: []string{"session", "999"},
		},
		{
			name:    "nil shards with pins",
			pins:    map[string]uint64{"user": 100},
			wantErr: []string{"user"},
		},
		{
			name:    "empty shards with pins",
			shards:  []uint64{},
			pins:    map[string]uint64{"user": 100},
			wantErr: []string{"user"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRoutingConfig(tt.shards, tt.pins)
			if tt.wantErr == nil {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			for _, want := range tt.wantErr {
				assert.Contains(t, err.Error(), want)
			}
		})
	}
}

// newTestCmd creates a cobra command with the server flags bound to global variables
// (matching the real command's flag binding pattern).
func newTestCmdWithServerFlags() *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().StringVar(&unixSocket, "unix-socket", config.DefaultUnixSocket, "")
	cmd.Flags().IntVar(&httpPort, "http-port", 0, "")
	cmd.Flags().IntVar(&grpcPort, "grpc-port", 0, "")
	cmd.Flags().StringVar(&dataDir, "data-dir", "./data", "")
	return cmd
}

// newTestCmdWithRaftFlags creates a cobra command with raft flags bound to global variables.
func newTestCmdWithRaftFlags() *cobra.Command {
	cmd := newTestCmdWithServerFlags()
	addRaftFlags(cmd)
	return cmd
}

// stubHostname returns a hostname resolver that always reports name, so a
// test's expected advertised address does not depend on the host it runs on.
func stubHostname(name string) func() (string, error) {
	return func() (string, error) { return name, nil }
}

// assertDerivedListenAddr asserts the listen address was left to derivation
// and derives to want.
func assertDerivedListenAddr(t *testing.T, want string) {
	t.Helper()
	assert.Empty(t, cfg.Raft.ListenAddress, "listen address must stay derived")
	got, err := cfg.Raft.ListenAddr()
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// TestApplyServerFlags tests the applyServerFlags function.
func TestApplyServerFlags(t *testing.T) {
	log = createTestLogger(t)

	tests := []struct {
		name  string
		flags map[string]string
		check func(t *testing.T)
	}{
		{
			name:  "applies unix socket when changed",
			flags: map[string]string{"unix-socket": "/custom/path.sock"},
			check: func(t *testing.T) {
				assert.Equal(t, "/custom/path.sock", cfg.Server.Unix.Socket)
				assert.True(t, cfg.Server.Unix.Enabled)
			},
		},
		{
			name:  "disables unix socket when empty",
			flags: map[string]string{"unix-socket": ""},
			check: func(t *testing.T) {
				assert.Equal(t, "", cfg.Server.Unix.Socket)
				assert.False(t, cfg.Server.Unix.Enabled)
			},
		},
		{
			name:  "applies http port when changed",
			flags: map[string]string{"http-port": "8080"},
			check: func(t *testing.T) {
				assert.Equal(t, 8080, cfg.Server.REST.Port)
				assert.True(t, cfg.Server.REST.Enabled)
			},
		},
		{
			name:  "disables http when port is 0",
			flags: map[string]string{"http-port": "0"},
			check: func(t *testing.T) {
				assert.Equal(t, 0, cfg.Server.REST.Port)
				assert.False(t, cfg.Server.REST.Enabled)
			},
		},
		{
			name:  "applies grpc port when changed",
			flags: map[string]string{"grpc-port": "9090"},
			check: func(t *testing.T) {
				assert.Equal(t, 9090, cfg.Server.GRPC.Port)
				assert.True(t, cfg.Server.GRPC.Enabled)
			},
		},
		{
			name:  "disables grpc when port is 0",
			flags: map[string]string{"grpc-port": "0"},
			check: func(t *testing.T) {
				assert.False(t, cfg.Server.GRPC.Enabled)
			},
		},
		{
			name:  "applies data-dir when changed",
			flags: map[string]string{"data-dir": "/custom/data"},
			check: func(t *testing.T) {
				assert.Equal(t, "/custom/data", cfg.Storage.DataDir)
			},
		},
		{
			name: "does not apply unchanged flags",
			check: func(t *testing.T) {
				assert.Equal(t, config.Default().Server.Unix.Socket, cfg.Server.Unix.Socket)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg = config.Default()
			cmd := newTestCmdWithServerFlags()
			for name, value := range tt.flags {
				require.NoError(t, cmd.Flags().Set(name, value))
			}

			applyServerFlags(cmd)
			tt.check(t)
		})
	}
}

// TestApplyRaftFlags tests the applyRaftFlags function.
func TestApplyRaftFlags(t *testing.T) {
	testLog := createTestLogger(t)
	log = testLog

	t.Run("applies node-id when changed", func(t *testing.T) {
		cfg = config.Default()
		cmd := newTestCmdWithRaftFlags()

		err := cmd.Flags().Set("node-id", "42")
		require.NoError(t, err)

		require.NoError(t, applyRaftFlags(cmd, os.Hostname))
		assert.Equal(t, uint64(42), cfg.Raft.NodeID)
	})

	t.Run("applies raft-address directly when set", func(t *testing.T) {
		cfg = config.Default()
		cmd := newTestCmdWithRaftFlags()

		err := cmd.Flags().Set("raft-address", "192.168.1.100:63001")
		require.NoError(t, err)

		require.NoError(t, applyRaftFlags(cmd, os.Hostname))
		assert.Equal(t, "192.168.1.100:63001", cfg.Raft.RaftAddress)
		assertDerivedListenAddr(t, "192.168.1.100:63001")

		// Reset global for subsequent tests
		raftAddress = ""
	})

	t.Run("uses hostname with raft-port when raft-address is empty", func(t *testing.T) {
		cfg = config.Default()
		cmd := newTestCmdWithRaftFlags()

		// Ensure raft-address global is empty
		viper.Reset()
		raftAddress = ""
		err := cmd.Flags().Set("raft-port", "64000")
		require.NoError(t, err)

		require.NoError(t, applyRaftFlags(cmd, stubHostname("node-b")))
		assert.Equal(t, "node-b:64000", cfg.Raft.RaftAddress)
		assertDerivedListenAddr(t, "node-b:64000")
	})

	t.Run("does not modify config when nothing changed", func(t *testing.T) {
		cfg = config.Default()
		originalNodeID := cfg.Raft.NodeID
		cmd := newTestCmdWithRaftFlags()

		raftAddress = ""
		require.NoError(t, applyRaftFlags(cmd, os.Hostname))
		assert.Equal(t, originalNodeID, cfg.Raft.NodeID)
	})
}

// TestParseSpecialEnvVars tests the parseSpecialEnvVars function.
func TestParseSpecialEnvVars(t *testing.T) {
	// Value parsing (bool spellings, durations) belongs to the config
	// package; these cases cover the wiring of each variable to its field
	// and the shard and pin list parsers that live in this package.
	tests := []struct {
		name  string
		env   map[string]string
		check func(t *testing.T)
		errIs error
	}{
		{
			name: "shards",
			env:  map[string]string{"QUICBAAS_SERVER_SHARDS": "100,200,300"},
			check: func(t *testing.T) {
				assert.Equal(t, []uint64{100, 200, 300}, cfg.Server.Shards)
			},
		},
		{
			name: "shards skip empty and padded parts",
			env:  map[string]string{"QUICBAAS_SERVER_SHARDS": " 100 ,, 200, ,300, "},
			check: func(t *testing.T) {
				assert.Equal(t, []uint64{100, 200, 300}, cfg.Server.Shards)
			},
		},
		{
			name: "shards all empty after trim",
			env:  map[string]string{"QUICBAAS_SERVER_SHARDS": "  ,  ,  "},
			check: func(t *testing.T) {
				assert.Empty(t, cfg.Server.Shards)
			},
		},
		{
			name:  "shards invalid id",
			env:   map[string]string{"QUICBAAS_SERVER_SHARDS": "100,invalid,200"},
			errIs: config.ErrInvalidEnvValue,
		},
		{
			name: "entity shard pins",
			env:  map[string]string{"QUICBAAS_SERVER_ENTITY_SHARD_PINS": "user:100,session:200"},
			check: func(t *testing.T) {
				assert.Equal(t, map[string]uint64{"user": 100, "session": 200}, cfg.Server.EntityShardPins)
			},
		},
		{
			name: "entity shard pins skip empty and padded pairs",
			env:  map[string]string{"QUICBAAS_SERVER_ENTITY_SHARD_PINS": " user : 100 ,, session:200, "},
			check: func(t *testing.T) {
				assert.Equal(t, map[string]uint64{"user": 100, "session": 200}, cfg.Server.EntityShardPins)
			},
		},
		{
			name:  "entity shard pin without separator",
			env:   map[string]string{"QUICBAAS_SERVER_ENTITY_SHARD_PINS": "user100"},
			errIs: config.ErrInvalidEnvValue,
		},
		{
			name:  "entity shard pin with invalid shard id",
			env:   map[string]string{"QUICBAAS_SERVER_ENTITY_SHARD_PINS": "user:notanumber"},
			errIs: config.ErrInvalidEnvValue,
		},
		{
			name: "empty variables are no-ops",
			env: map[string]string{
				"QUICBAAS_SERVER_SHARDS":            "",
				"QUICBAAS_SERVER_ENTITY_SHARD_PINS": "",
				"QUICBAAS_ENABLE_SCHEDULER":         "",
				"QUICBAAS_VECTOR_ENABLED":           "",
			},
			check: func(t *testing.T) {
				def := config.Default()
				assert.Equal(t, def.Server.Shards, cfg.Server.Shards)
				assert.Equal(t, def.Server.EntityShardPins, cfg.Server.EntityShardPins)
				assert.Equal(t, def.Scheduler.Enabled, cfg.Scheduler.Enabled)
				assert.Equal(t, def.Vector.Enabled, cfg.Vector.Enabled)
			},
		},
		{
			name: "feature flags enabled",
			env: map[string]string{
				"QUICBAAS_ENABLE_SCHEDULER":   "true",
				"QUICBAAS_TIMESERIES_ENABLED": "1",
				"QUICBAAS_VECTOR_ENABLED":     "yes",
				"QUICBAAS_BACKUP_ENABLED":     "on",
			},
			check: func(t *testing.T) {
				assert.True(t, cfg.Scheduler.Enabled)
				assert.True(t, cfg.TimeSeries.Enabled)
				assert.True(t, cfg.Vector.Enabled)
				assert.True(t, cfg.Backup.Enabled)
			},
		},
		{
			name: "feature flags disabled",
			env: map[string]string{
				"QUICBAAS_ENABLE_SCHEDULER":   "false",
				"QUICBAAS_TIMESERIES_ENABLED": "0",
				"QUICBAAS_VECTOR_ENABLED":     "no",
				"QUICBAAS_BACKUP_ENABLED":     "off",
			},
			check: func(t *testing.T) {
				assert.False(t, cfg.Scheduler.Enabled)
				assert.False(t, cfg.TimeSeries.Enabled)
				assert.False(t, cfg.Vector.Enabled)
				assert.False(t, cfg.Backup.Enabled)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg = config.Default()
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			err := parseSpecialEnvVars()
			if tt.errIs != nil {
				require.ErrorIs(t, err, tt.errIs)
				return
			}
			require.NoError(t, err)
			tt.check(t)
		})
	}
}

// TestRunVersion tests the version command output.
func TestRunVersion(t *testing.T) {
	t.Run("full version output", func(t *testing.T) {
		buf := &bytes.Buffer{}
		cmd := &cobra.Command{Use: "test"}
		cmd.SetOut(buf)

		versionShort = false
		runVersion(cmd, nil)
		output := buf.String()
		assert.NotEmpty(t, output)
	})

	t.Run("short version output", func(t *testing.T) {
		buf := &bytes.Buffer{}
		cmd := &cobra.Command{Use: "test"}
		cmd.SetOut(buf)

		versionShort = true
		runVersion(cmd, nil)
		output := buf.String()
		assert.NotEmpty(t, output)
	})
}

// TestInitConfig tests the initConfig function.
func TestInitConfig(t *testing.T) {
	tests := []struct {
		name string
		// explicit is the content of a file passed as --config.
		explicit string
		// search is the content of a quicbaas.yaml in the working directory.
		search string
		env    map[string]string
		check  func(t *testing.T)
	}{
		{
			name: "no config file uses defaults",
			env:  map[string]string{"QUICBAAS_LOGGING_LEVEL": "error"},
			check: func(t *testing.T) {
				assert.Equal(t, "error", cfg.Logging.Level)
			},
		},
		{
			name: "environment overrides defaults",
			env:  map[string]string{"QUICBAAS_STORAGE_DATA_DIR": "/tmp/env-data"},
			check: func(t *testing.T) {
				assert.Equal(t, "/tmp/env-data", cfg.Storage.DataDir)
			},
		},
		{
			name:     "explicit config file",
			explicit: "logging:\n  level: warn\n  format: json\n  output: stdout\nstorage:\n  data_dir: /tmp/test-data\n",
			check: func(t *testing.T) {
				assert.Equal(t, "warn", cfg.Logging.Level)
				assert.Equal(t, "/tmp/test-data", cfg.Storage.DataDir)
			},
		},
		{
			name:     "empty logging fields fall back to logger defaults",
			explicit: "logging:\n  level: \"\"\n  format: \"\"\n  output: \"\"\n",
			check: func(t *testing.T) {
				assert.Empty(t, cfg.Logging.Level)
			},
		},
		{
			name:   "config file found in search path",
			search: "logging:\n  level: error\n  format: json\n  output: stdout\n",
			check: func(t *testing.T) {
				assert.Equal(t, "error", cfg.Logging.Level)
			},
		},
		{
			name:   "malformed config file in search path is ignored",
			search: "logging:\n  level: debug\n\tbad_indent: error\n",
			check: func(t *testing.T) {
				assert.Equal(t, config.Default().Logging.Level, cfg.Logging.Level)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origCfg, origLog, origCfgFile := cfg, log, cfgFile
			t.Cleanup(func() {
				cfg, log, cfgFile = origCfg, origLog, origCfgFile
				viper.Reset()
			})
			viper.Reset()
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			dir := t.TempDir()
			t.Chdir(dir)
			cfgFile = ""
			if tt.explicit != "" {
				cfgFile = filepath.Join(dir, "explicit.yaml")
				require.NoError(t, os.WriteFile(cfgFile, []byte(tt.explicit), 0o600))
			}
			if tt.search != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "quicbaas.yaml"), []byte(tt.search), 0o600))
			}

			initConfig()

			require.NotNil(t, cfg)
			require.NotNil(t, log)
			tt.check(t)
		})
	}
}

// TestInitConfig_ExitsOnInvalidConfig runs initConfig in a subprocess,
// because it exits the process when the configuration cannot be loaded.
func TestInitConfig_ExitsOnInvalidConfig(t *testing.T) {
	if os.Getenv("TEST_SUBPROCESS") == "1" {
		cfgFile = os.Getenv("TEST_SUBPROCESS_CONFIG")
		cfg = nil
		initConfig()
		return
	}

	invalidYAML := filepath.Join(t.TempDir(), "invalid_config.yaml")
	require.NoError(t, os.WriteFile(invalidYAML, []byte("{{{{invalid yaml syntax}}}}"), 0o600))

	tests := []struct {
		name       string
		configFile string
		env        string
		wantOutput string
	}{
		{
			name:       "explicit config file not found",
			configFile: "/nonexistent/path/config.yaml",
			wantOutput: "Error reading config file",
		},
		{
			name:       "explicit config file does not parse",
			configFile: invalidYAML,
			wantOutput: "Error reading config file",
		},
		{
			name:       "invalid shard id in environment",
			env:        "QUICBAAS_SERVER_SHARDS=100,invalid,200",
			wantOutput: config.ErrInvalidEnvValue.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestInitConfig_ExitsOnInvalidConfig$")
			cmd.Env = append(os.Environ(), "TEST_SUBPROCESS=1", "TEST_SUBPROCESS_CONFIG="+tt.configFile)
			if tt.env != "" {
				cmd.Env = append(cmd.Env, tt.env)
			}
			output, err := cmd.CombinedOutput()

			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr)
			assert.Contains(t, string(output), tt.wantOutput)
		})
	}
}

// TestSubcommandRegistration tests that all subcommands are registered.
func TestSubcommandRegistration(t *testing.T) {
	expected := map[string]bool{
		"start":   false,
		"node":    false,
		"cluster": false,
		"version": false,
	}

	for _, cmd := range rootCmd.Commands() {
		if _, ok := expected[cmd.Use]; ok {
			expected[cmd.Use] = true
		}
	}

	for name, found := range expected {
		assert.True(t, found, "subcommand %q should be registered", name)
	}
}

// setupTestLogger creates a logger that writes to stderr for test usage.
// =============================================================================
// Consolidated from coverage_93_test.go
// =============================================================================

// =============================================================================
// Coverage improvement tests - exercises actual runStart/runNode/runCluster/runDaemon
// =============================================================================

// newTestCmdWithDiscoveryFlags creates a cobra command with server, raft, and
// discovery flags bound to global variables, matching the flags on nodeCmd.
func newTestCmdWithDiscoveryFlags() *cobra.Command {
	cmd := newTestCmdWithRaftFlags()
	cmd.Flags().StringVar(&discoveryType, "discovery", "multicast", "")
	cmd.Flags().StringVar(&joinAddresses, "join-addresses", "", "")
	return cmd
}

// TestRunStart_DirectCall calls runStart directly to cover its config-setting
// lines. Uses invalid routing config to make prepareDaemon fail before
// server.NewServer is called.
func TestRunStart_DirectCall(t *testing.T) {
	testLog := createTestLogger(t)
	log = testLog

	cfg = config.Default()
	cfg.Storage.DataDir = t.TempDir()
	// Invalid routing: pin to shard 99 which is not in the configured shards list.
	cfg.Server.Shards = []uint64{1, 2}
	cfg.Server.EntityShardPins = map[string]uint64{"user": 99}
	cfg.Server.Unix.Enabled = false

	cmd := newTestCmdWithServerFlags()

	err := runStart(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid routing config")

	// Verify config was set before the error occurred.
	assert.Equal(t, config.ServerModeStandalone, cfg.Server.Mode)
	assert.Equal(t, config.StorageBackendPebble, cfg.Storage.Backend)
}

// TestRunNode_DirectCall calls runNode directly and checks the cluster and
// discovery settings it applies before the invalid routing pin stops it.
func TestRunNode_DirectCall(t *testing.T) {
	log = createTestLogger(t)

	tests := []struct {
		name      string
		flags     map[string]string
		wantType  string
		wantPeers []string
	}{
		{
			name:     "default discovery",
			wantType: "multicast",
		},
		{
			name:     "explicit discovery",
			flags:    map[string]string{"discovery": "dns"},
			wantType: "dns",
		},
		{
			name: "static discovery with join addresses",
			flags: map[string]string{
				"discovery":      "static",
				"join-addresses": "10.0.0.1:63001,10.0.0.2:63002",
			},
			wantType:  "static",
			wantPeers: []string{"10.0.0.1:63001", "10.0.0.2:63002"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg = config.Default()
			cfg.Storage.DataDir = t.TempDir()
			// An invalid routing pin stops runDaemon in prepareDaemon, after
			// runNode has applied its settings and before a server starts.
			cfg.Server.Shards = []uint64{1, 2}
			cfg.Server.EntityShardPins = map[string]uint64{"user": 99}
			cfg.Server.Unix.Enabled = false
			raftAddress = ""

			cmd := newTestCmdWithDiscoveryFlags()
			for name, value := range tt.flags {
				require.NoError(t, cmd.Flags().Set(name, value))
			}

			err := runNode(cmd, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid routing config")

			assert.Equal(t, config.ServerModeCluster, cfg.Server.Mode)
			assert.Equal(t, config.StorageBackendPebble, cfg.Storage.Backend)
			assert.True(t, cfg.Discovery.Enabled)
			assert.Equal(t, tt.wantType, cfg.Discovery.Type)
			if tt.wantPeers != nil {
				assert.Equal(t, tt.wantPeers, cfg.Discovery.StaticPeers)
			}
		})
	}
}

// TestRunCluster_DirectCall calls runCluster directly to cover its
// config-setting lines including discovery disabled.
func TestRunCluster_DirectCall(t *testing.T) {
	testLog := createTestLogger(t)
	log = testLog

	cfg = config.Default()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Server.Shards = []uint64{1, 2}
	cfg.Server.EntityShardPins = map[string]uint64{"user": 99}
	cfg.Server.Unix.Enabled = false
	raftAddress = ""

	cmd := newTestCmdWithRaftFlags()

	err := runCluster(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid routing config")

	// Verify config was set before the error.
	assert.Equal(t, config.ServerModeCluster, cfg.Server.Mode)
	assert.Equal(t, config.StorageBackendPebble, cfg.Storage.Backend)
	assert.False(t, cfg.Discovery.Enabled)
}

// TestRunDaemon_ServerNewServerError tests the runDaemon path where
// prepareDaemon succeeds but server.NewServer returns an error.
// This is achieved by setting an invalid logging level that the server's
// internal logger validation rejects.
func TestRunDaemon_ServerNewServerError(t *testing.T) {
	testLog := createTestLogger(t)
	log = testLog

	cfg = config.Default()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Server.Unix.Enabled = false
	// No routing pins, so prepareDaemon succeeds.
	cfg.Server.Shards = nil
	cfg.Server.EntityShardPins = nil

	// Set invalid logging level so server.NewServer's internal logger.New fails.
	cfg.Logging.Level = "INVALID_LOG_LEVEL"

	err := runDaemon("test-newserver-fail")
	require.Error(t, err)
}

// TestRunDaemon_ServerStartError tests the runDaemon path where
// server.NewServer succeeds but srv.Start() returns an error. This covers
// the signal handling setup and error channel receive path in runDaemon.
// The server start fails because the data directory is made read-only after
// prepareDaemon creates it, causing TLS/storage initialization to fail.
func TestRunDaemon_ServerStartError(t *testing.T) {
	testLog := createTestLogger(t)
	log = testLog

	tempDir := t.TempDir()
	cfg = config.Default()
	cfg.Storage.DataDir = tempDir
	cfg.Server.Unix.Enabled = false
	cfg.Server.Shards = nil
	cfg.Server.EntityShardPins = nil
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendPebble
	// Valid logging so NewServer succeeds.
	cfg.Logging.Level = "error"
	cfg.Logging.Format = "json"
	cfg.Logging.Output = "stdout"

	// Start puts its TLS material in "<data dir>/certs" (pkg/server/lifecycle.go).
	// Occupying that name with a regular file makes the directory creation fail
	// with ENOTDIR, which root cannot walk through -- a read-only data directory
	// would only stop an unprivileged uid, and CI job containers run as root
	// (testing-guidelines.md, Fault Injection). prepareDaemon still succeeds
	// first, so the error genuinely comes from srv.Start().
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "certs"), []byte("blocking"), 0o644))

	err := runDaemon("test-start-fail")
	require.Error(t, err)
}

// =============================================================================
// Secure subsystem — graceful-shutdown invariant
//
// Regression: main() once called secure.CatchInterrupt(), which installed
// memguard's signal handler. That handler wiped mlocked memory and then hard
// os.Exit(1)ed from INSIDE the signal handler, so it always beat the daemon's
// own SIGTERM handler and Server.Shutdown never ran. The embedded qrdb node
// was therefore never closed, and every PebbleDB write still sitting in the
// memtable / unsynced WAL block was discarded — API-created IAM users vanished
// across a restart while the already-flushed first-boot admin survived.
//
// The secure subsystem now installs no signal handler at all: run() defers
// secure.WipeAll, which the daemon's own graceful shutdown reaches on every
// orderly exit. This test pins that the daemon still owns SIGTERM.
// =============================================================================

// TestSecureSubsystem_DoesNotInstallProcessKillingSignalHandler is the
// regression test for the data-loss bug: SIGINT/SIGTERM must reach the
// daemon's own handler. If a handler that exits the process is ever
// reinstated, this test kills the test binary instead of passing, which is
// exactly the signal a reviewer needs.
func TestSecureSubsystem_DoesNotInstallProcessKillingSignalHandler(t *testing.T) {
	sec, err := secure.NewSecret([]byte("held across a SIGTERM"))
	require.NoError(t, err)
	t.Cleanup(sec.Destroy)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM),
		"deliver SIGTERM to this process")

	select {
	case got := <-sigChan:
		assert.Equal(t, syscall.SIGTERM, got,
			"the daemon's own handler must receive SIGTERM so graceful shutdown can run")
	case <-time.After(5 * time.Second):
		t.Fatal("SIGTERM was never delivered to the process handler; " +
			"a signal handler is swallowing it and the graceful shutdown that " +
			"closes the embedded qrdb node will never run")
	}

	// Delivery alone is not enough: memguard's old handler also delivered the
	// signal to registered channels and THEN called os.Exit(1), so the daemon
	// could observe SIGTERM and still be killed mid-shutdown. Hold the
	// goroutine past that window; surviving it is the assertion.
	<-time.After(250 * time.Millisecond)
	opened, err := sec.Open()
	require.NoError(t, err, "the process must still be alive, with its secrets, after SIGTERM")
	opened.Destroy()
}
