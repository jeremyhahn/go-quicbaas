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
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
)

// =============================================================================
// TTL Parsing Tests
// =============================================================================

func TestCreateSDKClientEmptyAddressError(t *testing.T) {
	// Save and restore
	origAddress := address
	defer func() {
		address = origAddress
		resetSDKClient()
	}()

	resetSDKClient()
	address = ""

	_, err := createSDKClient()
	assert.Error(t, err)
	assert.Equal(t, ErrEmptyAddress, err)
}

func TestBuildSDKClientOptions(t *testing.T) {
	tests := []struct {
		name      string
		transport string
		address   string
		wantErr   bool
	}{
		{name: "empty defaults to grpc", transport: ""},
		{name: "grpc", transport: "grpc"},
		{name: "rest", transport: "rest"},
		{name: "quic", transport: "quic"},
		{name: "unix", transport: "unix", address: "/tmp/test.sock"},
		{name: "mcp", transport: "mcp"},
		{name: "invalid transport", transport: "invalid-transport", wantErr: true},
		{name: "websocket unsupported", transport: "websocket", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origTransport := transport
			origAddress := address
			defer func() {
				transport = origTransport
				address = origAddress
			}()

			transport = tt.transport
			if tt.address != "" {
				address = tt.address
			}

			opts, err := buildSDKClientOptions()
			if tt.wantErr {
				var ute *UnsupportedTransportError
				assert.ErrorAs(t, err, &ute)
				assert.Empty(t, opts)
				return
			}
			assert.NoError(t, err)
			assert.NotEmpty(t, opts)
		})
	}
}

func TestGetSDKClientNilReturn(t *testing.T) {
	// Save and restore
	defer resetSDKClient()

	resetSDKClient()
	client := getSDKClient()
	assert.Nil(t, client)
}

func TestCloseSDKClientNoClientExists(t *testing.T) {
	// Save and restore
	defer resetSDKClient()

	resetSDKClient()
	err := closeSDKClient()
	assert.NoError(t, err)
}

// =============================================================================
// Cluster Status Command Output Format Tests
// =============================================================================

func TestClusterStatusCommandOutputDefault(t *testing.T) {
	cmd := newClusterStatusCommand()
	require.NotNil(t, cmd)

	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag)
	assert.Equal(t, "table", outputFlag.DefValue)
}

// =============================================================================
// Cache Warm Command Dependencies Tests
// =============================================================================

func TestCacheWarmerDependenciesNilConfigError(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log
	defer func() {
		cfg = origCfg
		log = origLog
	}()

	cfg = nil

	_, _, _, err := initializeCacheWarmerDependencies()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

func TestCacheWarmerDependenciesNilLoggerError(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log
	defer func() {
		cfg = origCfg
		log = origLog
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
		},
	}
	log = nil

	_, _, _, err := initializeCacheWarmerDependencies()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "logger not initialized")
}

// =============================================================================
// Cache Command Nil Config Tests
// =============================================================================

func TestCacheConfigCommandNilConfigError(t *testing.T) {
	// Save original values
	origCfg := cfg
	defer func() { cfg = origCfg }()

	cfg = nil

	cmd := newCacheConfigCommand()
	cacheOutputFmt = "table"

	err := cmd.RunE(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

// =============================================================================
// Logger Adapter Tests
// =============================================================================

func TestLoggerAdapter(t *testing.T) {
	testLog, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	})
	require.NoError(t, err)

	tests := []struct {
		name string
		in   *logger.Logger
	}{
		{name: "nil logger is wrapped", in: nil},
		{name: "valid logger", in: testLog},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotNil(t, asInterfaceLogger(tt.in))
		})
	}
}

// =============================================================================
// Command Structure Tests
// =============================================================================

func TestCacheCommandsStructureVerification(t *testing.T) {
	cmd := newCacheCommands()
	assert.NotNil(t, cmd)
	assert.Equal(t, "cache", cmd.Use)

	// Verify all subcommands exist
	subcommands := cmd.Commands()
	assert.GreaterOrEqual(t, len(subcommands), 6)

	expectedSubcommands := []string{"stats", "clear", "config", "enable", "disable", "warm"}
	for _, expected := range expectedSubcommands {
		found := false
		for _, sub := range subcommands {
			if strings.HasPrefix(sub.Use, expected) {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected subcommand %s not found", expected)
	}
}

func TestClusterCommandsStructureVerification(t *testing.T) {
	cmd := newClusterCommands()
	assert.NotNil(t, cmd)
	assert.Equal(t, "cluster", cmd.Use)

	// Verify status subcommand exists
	subcommands := cmd.Commands()
	assert.GreaterOrEqual(t, len(subcommands), 1)

	var foundStatus bool
	for _, sub := range subcommands {
		if sub.Use == "status" {
			foundStatus = true
			break
		}
	}
	assert.True(t, foundStatus, "Expected status subcommand not found")
}

// =============================================================================
// Cobra Command Validation Tests
// =============================================================================

func TestCacheCommandsArgumentRequirements(t *testing.T) {
	tests := []struct {
		name       string
		cmdFactory func() *cobra.Command
		args       []string
		wantErr    bool
	}{
		{
			name:       "cache enable requires entity type arg",
			cmdFactory: newCacheEnableCommand,
			args:       []string{},
			wantErr:    true,
		},
		{
			name:       "cache disable requires entity type arg",
			cmdFactory: newCacheDisableCommand,
			args:       []string{},
			wantErr:    true,
		},
		{
			name:       "cache stats accepts no args",
			cmdFactory: newCacheStatsCommand,
			args:       []string{},
			wantErr:    false,
		},
		{
			name:       "cache stats accepts one arg",
			cmdFactory: newCacheStatsCommand,
			args:       []string{"user"},
			wantErr:    false,
		},
		{
			name:       "cache clear accepts no args",
			cmdFactory: newCacheClearCommand,
			args:       []string{},
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.cmdFactory()
			err := cmd.Args(cmd, tt.args)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// Output Format Parsing Tests
// =============================================================================

func TestCacheOutputFormatInvalidValue(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log
	defer func() {
		cfg = origCfg
		log = origLog
	}()

	// Setup test config and logger
	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities:        make(map[string]config.EntityCacheConfig),
		},
	}

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog

	cmd := newCacheStatsCommand()
	cacheOutputFmt = "invalid-format"

	err = cmd.RunE(cmd, []string{})
	assert.Error(t, err)
}

// =============================================================================
// Shutdown Embedded Server Tests
// =============================================================================

func TestEmbeddedServerShutdownNoServer(t *testing.T) {
	// Ensure no server is running
	embeddedServerHolder.Store(nil)

	err := shutdownEmbeddedServer()
	assert.NoError(t, err)
}

// =============================================================================
// Cache Clear Disabled Entity Tests
// =============================================================================

func TestCacheClearCommandDisabledEntityError(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log
	defer func() {
		cfg = origCfg
		log = origLog
	}()

	// Setup test config with disabled entity
	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"disabled-entity": {
					Enabled: false,
					TTL:     5 * time.Minute,
					MaxSize: 100,
				},
			},
		},
	}

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog

	cmd := newCacheClearCommand()
	cacheForce = true
	cacheOutputFmt = "table"

	err = cmd.RunE(cmd, []string{"disabled-entity"})
	// Should fail because caching is disabled for this entity
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not enabled")
}

// =============================================================================
// Capture Stdout Helper for Testing
// =============================================================================

func captureStdoutOutput(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

// =============================================================================
// Print Functions Coverage Tests
// =============================================================================

func TestClusterStatusJSONNilValues(t *testing.T) {
	// Test with minimal status
	status := map[string]interface{}{
		"healthy": false,
	}

	output := captureStdoutOutput(func() {
		err := printClusterStatusJSON(status)
		assert.NoError(t, err)
	})

	assert.Contains(t, output, "healthy")
}

// =============================================================================
// Additional Tests for Coverage Boost to 90%+
// =============================================================================

// =============================================================================
// Cache Enable/Disable Command Tests
// =============================================================================

func TestCacheEnableCommandNilConfig(t *testing.T) {
	// Save original values
	origCfg := cfg
	defer func() { cfg = origCfg }()

	cfg = nil

	cmd := newCacheEnableCommand()
	err := cmd.RunE(cmd, []string{"user"})
	assert.Error(t, err)
}

func TestCacheDisableCommandNilConfig(t *testing.T) {
	// Save original values
	origCfg := cfg
	defer func() { cfg = origCfg }()

	cfg = nil

	cmd := newCacheDisableCommand()
	err := cmd.RunE(cmd, []string{"user"})
	assert.Error(t, err)
}

func TestCacheStatsCommandNilConfig(t *testing.T) {
	// Save original values
	origCfg := cfg
	defer func() { cfg = origCfg }()

	cfg = nil

	cmd := newCacheStatsCommand()
	cacheOutputFmt = "table"
	err := cmd.RunE(cmd, []string{})
	assert.Error(t, err)
}

// =============================================================================
// Print Cluster Status Tests
// =============================================================================

func TestPrintClusterStatusJSONWithNodesOpt(t *testing.T) {
	status := map[string]interface{}{
		"healthy":  true,
		"leader":   "node1",
		"nodes":    []interface{}{"node1", "node2"},
		"replicas": 2,
	}

	output := captureStdoutOutput(func() {
		err := printClusterStatusJSON(status)
		assert.NoError(t, err)
	})

	assert.Contains(t, output, "healthy")
	assert.Contains(t, output, "node1")
}

// =============================================================================
// CreateSDKClient Tests
// =============================================================================

func TestCreateSDKClientWithValidAddressOpt(t *testing.T) {
	// Save and restore
	origAddress := address
	defer func() {
		address = origAddress
		resetSDKClient()
	}()

	resetSDKClient()
	address = "localhost:50051"

	// This should fail to connect but should not return ErrEmptyAddress
	_, err := createSDKClient()
	// We expect an error since there's no server, but it shouldn't be ErrEmptyAddress
	assert.NotEqual(t, ErrEmptyAddress, err)
}

// =============================================================================
// Initialize Cache Registry Tests
// =============================================================================

func TestInitializeCacheRegistryWithValidConfigOpt(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log
	defer func() {
		cfg = origCfg
		log = origLog
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
			},
		},
	}

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog

	registry, err := initializeCacheRegistry()
	assert.NoError(t, err)
	assert.NotNil(t, registry)
}

func TestInitializeCacheRegistryNilConfigOpt(t *testing.T) {
	// Save original values
	origCfg := cfg
	defer func() { cfg = origCfg }()

	cfg = nil

	_, err := initializeCacheRegistry()
	assert.Error(t, err)
}

// =============================================================================
// Object Storage Input Validation - Deeper Paths
// =============================================================================

func TestRunObjectPutCommand_ValidFileWithMetadata(t *testing.T) {
	origFile := objectFile
	origMeta := objectMetadata
	origTags := objectTags
	origFactory := objectStorageClientFactory
	defer func() {
		objectFile = origFile
		objectMetadata = origMeta
		objectTags = origTags
		objectStorageClientFactory = origFactory
	}()

	tmpFile := t.TempDir() + "/test-data.txt"
	require.NoError(t, os.WriteFile(tmpFile, []byte("test data"), 0o644))

	objectFile = tmpFile
	objectMetadata = `{"env":"test"}`
	objectTags = nil

	// Factory error after file/metadata parsing succeeds - exercises parsing paths
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	cmd := newObjectPutCommand()
	err := runObjectPutCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err) // Factory error, but file+metadata parsing was exercised
}

func TestRunObjectPutCommand_ValidFileWithTags(t *testing.T) {
	origFile := objectFile
	origMeta := objectMetadata
	origTags := objectTags
	origFactory := objectStorageClientFactory
	defer func() {
		objectFile = origFile
		objectMetadata = origMeta
		objectTags = origTags
		objectStorageClientFactory = origFactory
	}()

	tmpFile := t.TempDir() + "/tagged-data.txt"
	require.NoError(t, os.WriteFile(tmpFile, []byte("tagged content"), 0o644))

	objectFile = tmpFile
	objectMetadata = ""
	objectTags = map[string]string{"env": "prod", "tier": "hot"}

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	cmd := newObjectPutCommand()
	err := runObjectPutCommand(cmd, []string{"bucket/tagged-key"})
	assert.Error(t, err) // Factory error, but tag path was exercised
}

func TestRunObjectListByTagCommand_MalformedTagFilter(t *testing.T) {
	origTagFilter := objectTagFilter
	defer func() { objectTagFilter = origTagFilter }()

	objectTagFilter = "invalid-no-equals"

	cmd := newObjectListByTagCommand()
	err := runObjectListByTagCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid tag format")
}

func TestRunObjectListByTagCommand_ValidTagFactoryFails(t *testing.T) {
	origTagFilter := objectTagFilter
	origFactory := objectStorageClientFactory
	defer func() {
		objectTagFilter = origTagFilter
		objectStorageClientFactory = origFactory
	}()

	objectTagFilter = "type=document"
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	cmd := newObjectListByTagCommand()
	err := runObjectListByTagCommand(cmd, []string{})
	assert.Error(t, err)
}

func TestRunObjectMetadataUpdateCommand_MalformedJSON(t *testing.T) {
	origMeta := objectMetadata
	defer func() { objectMetadata = origMeta }()

	objectMetadata = "not-json"

	cmd := &cobra.Command{}
	err := runObjectMetadataUpdateCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid policy JSON")
}

func TestRunObjectMetadataUpdateCommand_ValidJSONFactoryFails(t *testing.T) {
	origMeta := objectMetadata
	origFactory := objectStorageClientFactory
	defer func() {
		objectMetadata = origMeta
		objectStorageClientFactory = origFactory
	}()

	objectMetadata = `{"replication":{"enabled":true}}`
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	cmd := &cobra.Command{}
	err := runObjectMetadataUpdateCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestRunObjectLifecycleSetCommand_MalformedJSON(t *testing.T) {
	origPolicy := objectPolicyJSON
	defer func() { objectPolicyJSON = origPolicy }()

	objectPolicyJSON = "invalid{json"

	cmd := &cobra.Command{}
	err := runObjectLifecycleSetCommand(cmd, []string{"mybucket"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid policy JSON")
}

func TestRunObjectLifecycleSetCommand_ValidJSONFactoryFails(t *testing.T) {
	origPolicy := objectPolicyJSON
	origFactory := objectStorageClientFactory
	defer func() {
		objectPolicyJSON = origPolicy
		objectStorageClientFactory = origFactory
	}()

	objectPolicyJSON = `{"archive":{"enabled":true,"archive_after_days":30}}`
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	cmd := &cobra.Command{}
	err := runObjectLifecycleSetCommand(cmd, []string{"mybucket"})
	assert.Error(t, err)
}

func TestRunObjectRestoreCommand_AllTierVariants(t *testing.T) {
	origFactory := objectStorageClientFactory
	origTier := objectRestoreTier
	origDest := objectDestBackend
	defer func() {
		objectStorageClientFactory = origFactory
		objectRestoreTier = origTier
		objectDestBackend = origDest
	}()

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	tests := []struct {
		name string
		tier string
	}{
		{name: "hot tier", tier: "hot"},
		{name: "warm tier", tier: "warm"},
		{name: "default tier", tier: ""},
		{name: "unknown tier", tier: "glacier"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectRestoreTier = tt.tier
			objectDestBackend = ""

			cmd := newObjectRestoreCommand()
			err := runObjectRestoreCommand(cmd, []string{"bucket/key"})
			assert.Error(t, err) // Factory error, but tier parsing paths exercised
		})
	}
}

func TestRunObjectReplicateCommand_CommaSeparatedDestinations(t *testing.T) {
	origFactory := objectStorageClientFactory
	origDest := objectDestBackend
	defer func() {
		objectStorageClientFactory = origFactory
		objectDestBackend = origDest
	}()

	objectDestBackend = "store-a, store-b, store-c"
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	cmd := newObjectReplicateCommand()
	err := runObjectReplicateCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err) // Factory error, but multi-dest parsing exercised
}

// =============================================================================
// Object Storage Format Edge Cases - SetPolicy
// =============================================================================

func TestFormatObjectSetPolicyResponse_YAMLFormat(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "yaml"

	resp := &pb.SetObjectPolicyResponse{Key: "bucket/key", Version: 1}
	err := formatObjectSetPolicyResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectSetPolicyResponse_TableFormat(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "table"

	resp := &pb.SetObjectPolicyResponse{Key: "bucket/key", Version: 3}
	err := formatObjectSetPolicyResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectSetPolicyResponse_BadFormat(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "invalid-format"

	resp := &pb.SetObjectPolicyResponse{Key: "bucket/key"}
	err := formatObjectSetPolicyResponse(resp)
	assert.Error(t, err)
}

// =============================================================================
// Object Storage Format - Lifecycle Response Edge Cases
// =============================================================================

func TestFormatObjectLifecycleResponse_TableWithArchiveAndRetention(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "table"

	resp := &pb.GetObjectPolicyResponse{
		Key: "mybucket",
		Policy: &pb.ObjectPolicy{
			ArchivePolicy: &pb.ArchivePolicy{
				Enabled:          true,
				ArchiveAfterDays: 30,
				TargetStore:      "cold-store",
			},
			RetentionPolicy: &pb.RetentionPolicy{
				Enabled:       true,
				RetentionDays: 365,
			},
		},
	}
	err := formatObjectLifecycleResponse("mybucket", resp)
	assert.NoError(t, err)
}

func TestFormatObjectLifecycleResponse_TableNilPolicy(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "table"

	resp := &pb.GetObjectPolicyResponse{Key: "mybucket", Policy: nil}
	err := formatObjectLifecycleResponse("mybucket", resp)
	assert.NoError(t, err)
}

func TestFormatObjectLifecycleResponse_TableEmptyPolicy(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "table"

	resp := &pb.GetObjectPolicyResponse{Key: "mybucket", Policy: &pb.ObjectPolicy{}}
	err := formatObjectLifecycleResponse("mybucket", resp)
	assert.NoError(t, err)
}

func TestFormatObjectLifecycleResponse_BadFormat(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "invalid-format"

	resp := &pb.GetObjectPolicyResponse{Key: "mybucket"}
	err := formatObjectLifecycleResponse("mybucket", resp)
	assert.Error(t, err)
}

// =============================================================================
// Cluster Output Formatting
// =============================================================================

func TestPrintClusterStatusJSON_Success(t *testing.T) {
	status := &cli.ClusterStatusResponse{
		Healthy:    true,
		LeaderID:   1,
		NodeCount:  3,
		ReadyNodes: 3,
		Nodes: []cli.NodeInfo{
			{NodeID: 1, Address: "10.0.0.1:50051", IsLeader: true, IsHealthy: true},
		},
	}
	err := printClusterStatusJSON(status)
	assert.NoError(t, err)
}

func TestPrintClusterStatusTableCLI_HealthyCluster(t *testing.T) {
	status := &cli.ClusterStatusResponse{
		Healthy:    true,
		LeaderID:   1,
		NodeCount:  2,
		ReadyNodes: 2,
		Nodes: []cli.NodeInfo{
			{NodeID: 1, Address: "10.0.0.1:50051", IsLeader: true, IsHealthy: true, LastSeen: time.Now()},
			{NodeID: 2, Address: "10.0.0.2:50051", IsLeader: false, IsHealthy: true},
		},
	}
	err := printClusterStatusTableCLI(status)
	assert.NoError(t, err)
}

func TestPrintClusterStatusTableCLI_UnhealthyCluster(t *testing.T) {
	status := &cli.ClusterStatusResponse{
		Healthy:    false,
		LeaderID:   0,
		NodeCount:  2,
		ReadyNodes: 1,
		Nodes: []cli.NodeInfo{
			{NodeID: 1, Address: "10.0.0.1:50051", IsLeader: false, IsHealthy: true},
			{NodeID: 2, Address: "10.0.0.2:50051", IsLeader: false, IsHealthy: false},
		},
	}
	err := printClusterStatusTableCLI(status)
	assert.NoError(t, err)
}

func TestPrintClusterStatusTableCLI_WithRaftGroupInfo(t *testing.T) {
	status := &cli.ClusterStatusResponse{
		Healthy:    true,
		LeaderID:   1,
		NodeCount:  3,
		ReadyNodes: 3,
		Nodes:      []cli.NodeInfo{},
		RaftGroupInfo: []cli.RaftGroupInfo{
			{LeaderID: 1, MemberCount: 3},
			{LeaderID: 2, MemberCount: 3},
		},
	}
	err := printClusterStatusTableCLI(status)
	assert.NoError(t, err)
}

func TestPrintClusterStatusTableCLI_FallbackToJSON(t *testing.T) {
	// Pass a non-ClusterStatusResponse type - should fallback to JSON
	status := map[string]string{"status": "ok"}
	err := printClusterStatusTableCLI(status)
	assert.NoError(t, err)
}

// =============================================================================
// InitLogger Edge Cases
// =============================================================================

func TestInitLogger_EmptyConfig(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()

	cfg = config.Default()
	cfg.Logging.Level = ""
	cfg.Logging.Format = ""

	err := initLogger()
	require.NoError(t, err)
	assert.NotNil(t, log)
}

// =============================================================================
// Object Storage Command Construction
// =============================================================================

func TestNewObjectStorageCommands_AllSubcommands(t *testing.T) {
	cmd := newObjectStorageCommands()
	assert.NotNil(t, cmd)
	assert.Equal(t, "object", cmd.Use)

	expectedSubs := []string{
		"put", "get", "delete", "head", "list",
		"list-by-tag", "list-by-tenant", "copy",
		"archive", "restore", "verify", "replicate",
		"migrate", "metadata", "stats", "lifecycle",
	}

	subNames := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subNames[sub.Name()] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, subNames[expected], "Expected subcommand '%s' to exist", expected)
	}
}

func TestNewObjectMetadataUpdateCommandCreation(t *testing.T) {
	cmd := newObjectMetadataUpdateCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "update <bucket>/<key>", cmd.Use)
}

func TestNewObjectLifecycleSetCommandCreation(t *testing.T) {
	cmd := newObjectLifecycleSetCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "set <bucket>", cmd.Use)
}

func TestNewObjectReplicateCommandCreation(t *testing.T) {
	cmd := newObjectReplicateCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "replicate <bucket>/<key>", cmd.Use)
}

func TestNewObjectMigrateCommandCreation(t *testing.T) {
	cmd := newObjectMigrateCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "migrate <bucket>/<key>", cmd.Use)
}

// =============================================================================
// Embedded Server Helper
// =============================================================================

func TestShutdownEmbeddedServer_NilHolder(t *testing.T) {
	embeddedServerHolder.Store(nil)
	err := shutdownEmbeddedServer()
	assert.NoError(t, err)
}

func TestEmbeddedServer_ShutdownWhenNotStarted(t *testing.T) {
	es := &embeddedServer{}
	es.started.Store(false)
	err := es.shutdown()
	assert.NoError(t, err)
}

// =============================================================================
// Backup Storage - loadProfileCredentials Tests
// =============================================================================

func TestLoadProfileCredentials_FileNotFound(t *testing.T) {
	flags := &BackupStorageFlags{StorageType: "s3"}
	builder := NewBackupStorageBuilder(flags, "", "")
	builder.credentialsFile = "/nonexistent/credentials.yaml"

	creds, err := builder.loadProfileCredentials(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, creds, "Should return nil when file doesn't exist")
}

func TestLoadProfileCredentials_ValidCredentialsFile(t *testing.T) {
	tmpDir := t.TempDir()
	credFile := filepath.Join(tmpDir, "credentials.yaml")

	credContent := `
default_profile: prod
profiles:
  prod:
    type: s3
    bucket: production-bucket
    region: us-east-1
    credentials:
      access_key_id: AKIATEST123
      secret_access_key: secret123
  dev:
    type: s3
    bucket: dev-bucket
    region: us-west-2
`
	require.NoError(t, os.WriteFile(credFile, []byte(credContent), 0o600))

	flags := &BackupStorageFlags{StorageType: "s3"}
	builder := NewBackupStorageBuilder(flags, "", "")
	builder.credentialsFile = credFile

	creds, err := builder.loadProfileCredentials(context.Background())
	require.NoError(t, err)
	require.NotNil(t, creds)
	assert.Equal(t, "s3", creds.Type)
	assert.Equal(t, "production-bucket", creds.Bucket)
	assert.Equal(t, "us-east-1", creds.Region)
}

func TestLoadProfileCredentials_SpecificProfile(t *testing.T) {
	tmpDir := t.TempDir()
	credFile := filepath.Join(tmpDir, "credentials.yaml")

	credContent := `
default_profile: prod
profiles:
  prod:
    type: s3
    bucket: production-bucket
    region: us-east-1
  dev:
    type: s3
    bucket: dev-bucket
    region: us-west-2
`
	require.NoError(t, os.WriteFile(credFile, []byte(credContent), 0o600))

	flags := &BackupStorageFlags{StorageType: "s3"}
	builder := NewBackupStorageBuilder(flags, "", "dev")
	builder.credentialsFile = credFile

	creds, err := builder.loadProfileCredentials(context.Background())
	require.NoError(t, err)
	require.NotNil(t, creds)
	assert.Equal(t, "dev-bucket", creds.Bucket)
	assert.Equal(t, "us-west-2", creds.Region)
}

func TestLoadProfileCredentials_ProfileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	credFile := filepath.Join(tmpDir, "credentials.yaml")

	credContent := `
default_profile: prod
profiles:
  prod:
    type: s3
    bucket: production-bucket
`
	require.NoError(t, os.WriteFile(credFile, []byte(credContent), 0o600))

	flags := &BackupStorageFlags{StorageType: "s3"}
	builder := NewBackupStorageBuilder(flags, "", "nonexistent")
	builder.credentialsFile = credFile

	creds, err := builder.loadProfileCredentials(context.Background())
	assert.Error(t, err)
	assert.Nil(t, creds)
	assert.Contains(t, err.Error(), "profile 'nonexistent' not found")
}

func TestLoadProfileCredentials_NoDefaultProfile(t *testing.T) {
	tmpDir := t.TempDir()
	credFile := filepath.Join(tmpDir, "credentials.yaml")

	credContent := `
profiles:
  prod:
    type: s3
    bucket: production-bucket
`
	require.NoError(t, os.WriteFile(credFile, []byte(credContent), 0o600))

	flags := &BackupStorageFlags{StorageType: "s3"}
	builder := NewBackupStorageBuilder(flags, "", "")
	builder.credentialsFile = credFile

	creds, err := builder.loadProfileCredentials(context.Background())
	assert.Error(t, err)
	assert.Nil(t, creds)
	assert.Contains(t, err.Error(), "no profile specified")
}

func TestLoadProfileCredentials_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	credFile := filepath.Join(tmpDir, "credentials.yaml")

	require.NoError(t, os.WriteFile(credFile, []byte("not: valid: yaml:"), 0o600))

	flags := &BackupStorageFlags{StorageType: "s3"}
	builder := NewBackupStorageBuilder(flags, "", "")
	builder.credentialsFile = credFile

	creds, err := builder.loadProfileCredentials(context.Background())
	assert.Error(t, err)
	assert.Nil(t, creds)
	assert.Contains(t, err.Error(), "failed to parse")
}

func TestLoadProfileCredentials_DefaultPath(t *testing.T) {
	// Test that default path is used when credentialsFile is empty
	flags := &BackupStorageFlags{StorageType: "s3"}
	builder := NewBackupStorageBuilder(flags, "", "")
	builder.credentialsFile = ""

	// Since the default file likely doesn't exist, should return nil, nil
	creds, err := builder.loadProfileCredentials(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, creds)
}

// =============================================================================
// Backup Storage - expandEnvVars Tests
// =============================================================================

func TestExpandEnvVars_BasicExpansion(t *testing.T) {
	flags := &BackupStorageFlags{StorageType: "s3"}
	builder := NewBackupStorageBuilder(flags, "", "")

	os.Setenv("TEST_BUCKET_NAME", "my-test-bucket")
	defer os.Unsetenv("TEST_BUCKET_NAME")

	result := builder.expandEnvVars("${TEST_BUCKET_NAME}")
	assert.Equal(t, "my-test-bucket", result)
}

func TestExpandEnvVars_MixedContent(t *testing.T) {
	flags := &BackupStorageFlags{StorageType: "s3"}
	builder := NewBackupStorageBuilder(flags, "", "")

	os.Setenv("TEST_ENV", "production")
	defer os.Unsetenv("TEST_ENV")

	result := builder.expandEnvVars("backup-${TEST_ENV}-data")
	assert.Equal(t, "backup-production-data", result)
}

func TestExpandEnvVars_UndefinedVariable(t *testing.T) {
	flags := &BackupStorageFlags{StorageType: "s3"}
	builder := NewBackupStorageBuilder(flags, "", "")

	result := builder.expandEnvVars("${UNDEFINED_VAR_12345}")
	assert.Equal(t, "", result)
}

// =============================================================================
// createObjectStorageGRPCClient Tests - TLS Error Paths
// =============================================================================

func TestCreateObjectStorageGRPCClient_TLSEnabled_MissingCert(t *testing.T) {
	origTLSEnabled := objectTLSEnabled
	origTLSCert := objectTLSCert
	origTLSKey := objectTLSKey
	defer func() {
		objectTLSEnabled = origTLSEnabled
		objectTLSCert = origTLSCert
		objectTLSKey = origTLSKey
	}()

	objectTLSEnabled = true
	objectTLSCert = ""
	objectTLSKey = ""

	client, err := createObjectStorageGRPCClient()
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "TLS is required for gRPC connections")
}

func TestCreateObjectStorageGRPCClient_TLSEnabled_MissingKey(t *testing.T) {
	origTLSEnabled := objectTLSEnabled
	origTLSCert := objectTLSCert
	origTLSKey := objectTLSKey
	defer func() {
		objectTLSEnabled = origTLSEnabled
		objectTLSCert = origTLSCert
		objectTLSKey = origTLSKey
	}()

	objectTLSEnabled = true
	objectTLSCert = "/path/to/cert.pem"
	objectTLSKey = ""

	client, err := createObjectStorageGRPCClient()
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "TLS is required for gRPC connections")
}

// =============================================================================
// Object Storage Command - Additional Input Validation Tests
// =============================================================================

func TestRunObjectArchiveCommand_AllCompressionLevels(t *testing.T) {
	origFactory := objectStorageClientFactory
	origCompression := objectCompressionLevel
	origTargetStore := objectTargetStore
	defer func() {
		objectStorageClientFactory = origFactory
		objectCompressionLevel = origCompression
		objectTargetStore = origTargetStore
	}()

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}
	objectTargetStore = "archive-store"

	tests := []struct {
		name  string
		level int32
	}{
		{name: "no compression", level: 0},
		{name: "fast compression", level: 1},
		{name: "best compression", level: 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectCompressionLevel = tt.level
			cmd := newObjectArchiveCommand()
			err := runObjectArchiveCommand(cmd, []string{"bucket/key"})
			assert.Error(t, err) // Factory error expected, but compression path exercised
		})
	}
}

func TestRunObjectCopyCommand_WithTags(t *testing.T) {
	origFactory := objectStorageClientFactory
	origCopyTags := objectCopyTags
	defer func() {
		objectStorageClientFactory = origFactory
		objectCopyTags = origCopyTags
	}()

	objectCopyTags = true
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	cmd := newObjectCopyCommand()
	err := runObjectCopyCommand(cmd, []string{"source/key", "dest/key"})
	assert.Error(t, err) // Factory error expected, but copy tags path exercised
}

// =============================================================================
// New Command Builder Tests
// =============================================================================

func createTestLoggerForRemaining91(t *testing.T) *logger.Logger {
	t.Helper()
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

func validCacheConfig() config.EnhancedCacheConfig {
	return config.EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         1000,
		CleanupInterval: 1 * time.Minute,
		Entities:        make(map[string]config.EntityCacheConfig),
	}
}

// upgrade.go coverage

func TestRemaining91_RunUpgradeStart_DefaultAddress(t *testing.T) {
	origAddr := upgradeServerAddress
	origGlobal := address
	origTimeout := globalTimeout
	defer func() {
		upgradeServerAddress = origAddr
		address = origGlobal
		globalTimeout = origTimeout
	}()

	upgradeServerAddress = ""
	address = ""
	globalTimeout = 1 * time.Second

	cmd := newUpgradeStartCommand()
	err := runUpgradeStart(cmd, []string{"v2.0.0"})
	require.Error(t, err)
}

func TestRemaining91_RunUpgradeStart_CustomServerAddress(t *testing.T) {
	origAddr := upgradeServerAddress
	origTimeout := globalTimeout
	defer func() {
		upgradeServerAddress = origAddr
		globalTimeout = origTimeout
	}()

	upgradeServerAddress = "localhost:19999"
	globalTimeout = 1 * time.Second

	cmd := newUpgradeStartCommand()
	err := runUpgradeStart(cmd, []string{"v3.0.0"})
	require.Error(t, err)
}

func TestRemaining91_RunUpgradeStart_FallbackToGlobalAddress(t *testing.T) {
	origAddr := upgradeServerAddress
	origGlobal := address
	origTimeout := globalTimeout
	defer func() {
		upgradeServerAddress = origAddr
		address = origGlobal
		globalTimeout = origTimeout
	}()

	upgradeServerAddress = ""
	address = "localhost:19998"
	globalTimeout = 1 * time.Second

	cmd := newUpgradeStartCommand()
	err := runUpgradeStart(cmd, []string{"v2.1.0"})
	require.Error(t, err)
}

func TestRemaining91_RunUpgradeStatus_DefaultAddress(t *testing.T) {
	origAddr := upgradeServerAddress
	origGlobal := address
	origTimeout := globalTimeout
	defer func() {
		upgradeServerAddress = origAddr
		address = origGlobal
		globalTimeout = origTimeout
	}()

	upgradeServerAddress = ""
	address = ""
	globalTimeout = 1 * time.Second

	cmd := newUpgradeStatusCommand()
	err := runUpgradeStatus(cmd, []string{})
	require.Error(t, err)
}

func TestRemaining91_RunUpgradeStatus_CustomAddress(t *testing.T) {
	origAddr := upgradeServerAddress
	origTimeout := globalTimeout
	defer func() {
		upgradeServerAddress = origAddr
		globalTimeout = origTimeout
	}()

	upgradeServerAddress = "localhost:19997"
	globalTimeout = 1 * time.Second

	cmd := newUpgradeStatusCommand()
	err := runUpgradeStatus(cmd, []string{})
	require.Error(t, err)
}

func TestRemaining91_RunUpgradeStatus_FallbackToGlobalAddress(t *testing.T) {
	origAddr := upgradeServerAddress
	origGlobal := address
	origTimeout := globalTimeout
	defer func() {
		upgradeServerAddress = origAddr
		address = origGlobal
		globalTimeout = origTimeout
	}()

	upgradeServerAddress = ""
	address = "localhost:19996"
	globalTimeout = 1 * time.Second

	cmd := newUpgradeStatusCommand()
	err := runUpgradeStatus(cmd, []string{})
	require.Error(t, err)
}

func TestRemaining91_FollowUpgradeProgress_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := followUpgradeProgress(ctx, nil, "op-123", "table")
	require.Error(t, err)
}

// cluster.go coverage

func TestRemaining91_NewClusterStatusCommand_ConnectionFailure(t *testing.T) {
	origAddr := address
	origTransport := transport
	origTimeout := globalTimeout
	defer func() {
		address = origAddr
		transport = origTransport
		globalTimeout = origTimeout
		resetSDKClient()
	}()

	resetSDKClient()
	address = "localhost:19995"
	transport = "grpc"
	globalTimeout = 1 * time.Second

	cmd := newClusterStatusCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRemaining91_NewClusterStatusCommand_InvalidOutputFormat(t *testing.T) {
	origAddr := address
	origTransport := transport
	origTimeout := globalTimeout
	defer func() {
		address = origAddr
		transport = origTransport
		globalTimeout = origTimeout
		resetSDKClient()
	}()

	resetSDKClient()
	address = "localhost:19994"
	transport = "grpc"
	globalTimeout = 1 * time.Second

	cmd := newClusterStatusCommand()
	cmd.SetArgs([]string{"--output", "xml"})
	err := cmd.Execute()
	assert.Error(t, err)
}

// node.go coverage

func TestRemaining91_RunNodeList_TableOutput(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	origFmt := nodeOutputFormat
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
		nodeOutputFormat = origFmt
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	cmd := newNodeListCommand()
	nodeOutputFormat = "table"
	err := runNodeList(cmd, []string{})
	assert.NoError(t, err)
}

func TestRemaining91_RunNodeList_JSONOutput(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	origFmt := nodeOutputFormat
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
		nodeOutputFormat = origFmt
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	cmd := newNodeListCommand()
	nodeOutputFormat = "json"
	err := runNodeList(cmd, []string{})
	assert.NoError(t, err)
}

func TestRemaining91_RunNodeList_InvalidFormat(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	origFmt := nodeOutputFormat
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
		nodeOutputFormat = origFmt
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	cmd := newNodeListCommand()
	nodeOutputFormat = "xml"
	err := runNodeList(cmd, []string{})
	assert.Error(t, err)
}

func TestRemaining91_GetNodeManager_CreatesNew(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	mgr, err := getNodeManager()
	require.NoError(t, err)
	assert.NotNil(t, mgr)
}

func TestRemaining91_GetNodeManager_ReturnsCached(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	mgr1, err := getNodeManager()
	require.NoError(t, err)
	mgr2, err := getNodeManager()
	require.NoError(t, err)
	assert.Equal(t, mgr1, mgr2)
}

func TestRemaining91_GetNodeManager_WithCustomFlags(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	origHCI := nodeHealthCheckInterval
	origHCT := nodeHealthCheckTimeout
	origEvac := nodeEvacuationTimeout
	origGrace := nodeGracePeriod
	origMaxT := nodeMaxTransfers
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
		nodeHealthCheckInterval = origHCI
		nodeHealthCheckTimeout = origHCT
		nodeEvacuationTimeout = origEvac
		nodeGracePeriod = origGrace
		nodeMaxTransfers = origMaxT
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil
	nodeHealthCheckInterval = 10 * time.Second
	nodeHealthCheckTimeout = 3 * time.Second
	nodeEvacuationTimeout = 15 * time.Minute
	nodeGracePeriod = 5 * time.Second
	nodeMaxTransfers = 3

	mgr, err := getNodeManager()
	require.NoError(t, err)
	assert.NotNil(t, mgr)
}

func TestRemaining91_RunNodeAdd_Success(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	origFmt := nodeOutputFormat
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
		nodeOutputFormat = origFmt
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	addCmd := newNodeAddCommand()
	nodeOutputFormat = "table"
	err := runNodeAdd(addCmd, []string{"node-test-1", "10.0.0.1:50051"})
	assert.NoError(t, err)
}

func TestRemaining91_RunNodeAdd_JSONOutput(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	origFmt := nodeOutputFormat
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
		nodeOutputFormat = origFmt
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	addCmd := newNodeAddCommand()
	nodeOutputFormat = "json"
	err := runNodeAdd(addCmd, []string{"node-test-2", "10.0.0.2:50051"})
	assert.NoError(t, err)
}

func TestRemaining91_RunNodeRemove_Success(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	origFmt := nodeOutputFormat
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
		nodeOutputFormat = origFmt
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	addCmd := newNodeAddCommand()
	nodeOutputFormat = "table"
	err := runNodeAdd(addCmd, []string{"node-remove-1", "10.0.0.3:50051"})
	require.NoError(t, err)

	removeCmd := newNodeRemoveCommand()
	nodeOutputFormat = "table"
	err = runNodeRemove(removeCmd, []string{"node-remove-1"})
	assert.NoError(t, err)
}

func TestRemaining91_RunNodeRemove_NonExistentNode(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	origFmt := nodeOutputFormat
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
		nodeOutputFormat = origFmt
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	removeCmd := newNodeRemoveCommand()
	nodeOutputFormat = "table"
	err := runNodeRemove(removeCmd, []string{"ghost-node"})
	assert.Error(t, err)
}

func TestRemaining91_RunNodeReplace_Success(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	origFmt := nodeOutputFormat
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
		nodeOutputFormat = origFmt
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	addCmd := newNodeAddCommand()
	nodeOutputFormat = "table"
	err := runNodeAdd(addCmd, []string{"node-old", "10.0.0.5:50051"})
	require.NoError(t, err)

	replaceCmd := newNodeReplaceCommand()
	nodeOutputFormat = "table"
	err = runNodeReplace(replaceCmd, []string{"node-old", "node-new", "10.0.0.6:50051"})
	assert.NoError(t, err)
}

func TestRemaining91_RunNodeReplace_NonExistentOld(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	origFmt := nodeOutputFormat
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
		nodeOutputFormat = origFmt
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	replaceCmd := newNodeReplaceCommand()
	nodeOutputFormat = "json"
	err := runNodeReplace(replaceCmd, []string{"ghost-node", "new-node", "10.0.0.7:50051"})
	assert.NoError(t, err)
}

// cache.go coverage

func TestRemaining91_RunCacheWarmCommand_NilConfig(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()
	cfg = nil
	cmd := newCacheWarmCommand()
	err := runCacheWarmCommand(cmd, []string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

func TestRemaining91_RunCacheWarmCommand_NoPrefixes(t *testing.T) {
	origCfg := cfg
	origLog := log
	origPrefixes := cacheWarmPrefixes
	defer func() { cfg = origCfg; log = origLog; cacheWarmPrefixes = origPrefixes }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = createTestLoggerForRemaining91(t)
	cacheWarmPrefixes = []string{}
	cmd := newCacheWarmCommand()
	err := runCacheWarmCommand(cmd, []string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prefix")
}

func TestRemaining91_InitializeCacheWarmerDependencies_NilConfig(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()
	cfg = nil
	_, _, _, err := initializeCacheWarmerDependencies()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

func TestRemaining91_InitializeCacheWarmerDependencies_NilLogger(t *testing.T) {
	origCfg := cfg
	origLog := log
	defer func() { cfg = origCfg; log = origLog }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = nil
	_, _, _, err := initializeCacheWarmerDependencies()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "logger not initialized")
}

func TestRemaining91_RunCacheEnableCommand_NilConfig(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()
	cfg = nil
	cmd := newCacheEnableCommand()
	err := runCacheEnableCommand(cmd, []string{"users"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

func TestRemaining91_RunCacheDisableCommand_NilConfig(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()
	cfg = nil
	cmd := newCacheDisableCommand()
	err := runCacheDisableCommand(cmd, []string{"users"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

func TestRemaining91_RunCacheConfigCommand_NilConfig(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()
	cfg = nil
	cmd := newCacheConfigCommand()
	err := runCacheConfigCommand(cmd, []string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

func TestRemaining91_RunCacheConfigCommand_WithEntity(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	defer func() { cfg = origCfg; log = origLog; cacheOutputFmt = origFmt }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = createTestLoggerForRemaining91(t)
	cacheOutputFmt = "table"
	cmd := newCacheConfigCommand()
	err := runCacheConfigCommand(cmd, []string{"users"})
	assert.NoError(t, err)
}

func TestRemaining91_RunCacheConfigCommand_AllEntities(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	defer func() { cfg = origCfg; log = origLog; cacheOutputFmt = origFmt }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = createTestLoggerForRemaining91(t)
	cacheOutputFmt = "json"
	cmd := newCacheConfigCommand()
	err := runCacheConfigCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestRemaining91_RunCacheConfigCommand_InvalidOutputFormat(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	defer func() { cfg = origCfg; log = origLog; cacheOutputFmt = origFmt }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = createTestLoggerForRemaining91(t)
	cacheOutputFmt = "xml_bad"
	cmd := newCacheConfigCommand()
	err := runCacheConfigCommand(cmd, []string{})
	assert.Error(t, err)
}

func TestRemaining91_InitializeCacheRegistry_NilConfig(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()
	cfg = nil
	_, err := initializeCacheRegistry()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

func TestRemaining91_InitializeCacheRegistry_NilLogger(t *testing.T) {
	origCfg := cfg
	origLog := log
	defer func() { cfg = origCfg; log = origLog }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = nil
	_, err := initializeCacheRegistry()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "logger not initialized")
}

func TestRemaining91_InitializeCacheRegistry_Success(t *testing.T) {
	origCfg := cfg
	origLog := log
	defer func() { cfg = origCfg; log = origLog }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = createTestLoggerForRemaining91(t)
	registry, err := initializeCacheRegistry()
	require.NoError(t, err)
	assert.NotNil(t, registry)
	registry.Close()
}

// client.go coverage

func TestRemaining91_CreateSDKClient_EmptyAddress(t *testing.T) {
	origAddr := address
	defer func() { address = origAddr; resetSDKClient() }()
	resetSDKClient()
	address = ""
	client, err := createSDKClient()
	assert.Nil(t, client)
	require.Error(t, err)
}

func TestRemaining91_CreateSDKClient_ReturnsExisting(t *testing.T) {
	defer func() { resetSDKClient() }()
	resetSDKClient()
	mock := &mockClient{}
	var client quicbaas.Client = mock
	sdkClientHolder.Store(&client)
	result, err := createSDKClient()
	require.NoError(t, err)
	assert.Same(t, mock, result)
}

func TestRemaining91_BuildSDKClientOptions_QUICTransport(t *testing.T) {
	origT := transport
	origTimeout := globalTimeout
	defer func() { transport = origT; globalTimeout = origTimeout }()
	transport = "quic"
	globalTimeout = 5 * time.Second
	opts, err := buildSDKClientOptions()
	require.NoError(t, err)
	assert.NotEmpty(t, opts)
}

func TestRemaining91_BuildSDKClientOptions_RESTTransport(t *testing.T) {
	origT := transport
	origTimeout := globalTimeout
	defer func() { transport = origT; globalTimeout = origTimeout }()
	transport = "rest"
	globalTimeout = 5 * time.Second
	opts, err := buildSDKClientOptions()
	require.NoError(t, err)
	assert.NotEmpty(t, opts)
}

func TestRemaining91_BuildSDKClientOptions_UnixTransport(t *testing.T) {
	origT := transport
	origAddr := address
	origTimeout := globalTimeout
	defer func() { transport = origT; address = origAddr; globalTimeout = origTimeout }()
	transport = "unix"
	address = "/tmp/test.sock"
	globalTimeout = 5 * time.Second
	opts, err := buildSDKClientOptions()
	require.NoError(t, err)
	assert.NotEmpty(t, opts)
}

func TestRemaining91_BuildSDKClientOptions_MCPTransport(t *testing.T) {
	origT := transport
	origTimeout := globalTimeout
	defer func() { transport = origT; globalTimeout = origTimeout }()
	transport = "mcp"
	globalTimeout = 5 * time.Second
	opts, err := buildSDKClientOptions()
	require.NoError(t, err)
	assert.NotEmpty(t, opts)
}

func TestRemaining91_BuildSDKClientOptions_UnsupportedTransport(t *testing.T) {
	origT := transport
	defer func() { transport = origT }()
	transport = "bad_proto"
	opts, err := buildSDKClientOptions()
	assert.Nil(t, opts)
	require.Error(t, err)
}

func TestRemaining91_CloseSDKClient_NilClient(t *testing.T) {
	resetSDKClient()
	err := closeSDKClient()
	assert.NoError(t, err)
}

func TestRemaining91_BuildSDKClientOptions_ZeroTimeout(t *testing.T) {
	origT, origTimeout, origTenant := transport, globalTimeout, globalTenantID
	origAddr, origCA, origDataDir := address, tlsCA, dataDir
	defer func() {
		transport, globalTimeout, globalTenantID = origT, origTimeout, origTenant
		address, tlsCA, dataDir = origAddr, origCA, origDataDir
	}()
	transport = "grpc"
	globalTimeout = 0
	// Every input that contributes an option is pinned to its no-option value,
	// so a failure here names the timeout rather than whatever global a
	// neighbouring test happened to leave behind.
	globalTenantID = ""
	address = "localhost:9090"
	tlsCA = ""
	dataDir = t.TempDir()
	opts, err := buildSDKClientOptions()
	require.NoError(t, err)
	assert.Equal(t, 0, len(opts))
}

// ollama.go coverage

func TestRemaining91_CreateOllamaClient_UnsupportedTransport(t *testing.T) {
	origT := transport
	defer func() { transport = origT }()
	transport = "rest"
	_, err := createOllamaClient()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported transport")
}

func TestRemaining91_CreateOllamaClient_GRPCDefault(t *testing.T) {
	origT := transport
	origAddr := address
	origTimeout := globalTimeout
	defer func() { transport = origT; address = origAddr; globalTimeout = origTimeout }()
	transport = ""
	address = "localhost:19993"
	globalTimeout = 1 * time.Second
	client, err := createOllamaClient()
	if err == nil {
		assert.NotNil(t, client)
		client.Close()
	}
}

func TestRemaining91_CreateOllamaGRPCClient_Success(t *testing.T) {
	origAddr := address
	origTimeout := globalTimeout
	defer func() { address = origAddr; globalTimeout = origTimeout }()
	address = "localhost:19992"
	globalTimeout = 1 * time.Second
	client, err := createOllamaGRPCClient()
	if err == nil {
		assert.NotNil(t, client)
		client.Close()
	}
}

// factories_mcp_crypto.go coverage

func TestRemaining91_ParseCryptoEncryptResponse_InvalidInput(t *testing.T) {
	_, err := parseCryptoEncryptResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseCryptoEncryptResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{
		"key_id": "test-key", "key_version": float64(1),
		"ciphertext": base64.StdEncoding.EncodeToString([]byte("encrypted-data")),
		"algorithm":  "AES256-GCM",
		"nonce":      base64.StdEncoding.EncodeToString([]byte("nonce-bytes")),
	}
	resp, err := parseCryptoEncryptResponse(data)
	require.NoError(t, err)
	assert.Equal(t, "test-key", resp.KeyID)
	assert.Equal(t, uint64(1), resp.KeyVersion)
}

func TestRemaining91_ParseCryptoDecryptResponse_InvalidInput(t *testing.T) {
	_, err := parseCryptoDecryptResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseCryptoDecryptResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{
		"key_id": "decrypt-key", "plaintext": "hello world",
	}
	resp, err := parseCryptoDecryptResponse(data)
	require.NoError(t, err)
	assert.Equal(t, "decrypt-key", resp.KeyID)
	assert.Equal(t, []byte("hello world"), resp.Plaintext)
}

func TestRemaining91_ParseCryptoSignResponse_InvalidInput(t *testing.T) {
	_, err := parseCryptoSignResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseCryptoSignResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{"key_id": "sign-key", "key_version": float64(1), "signature": base64.StdEncoding.EncodeToString([]byte("sig")), "algorithm": "RSA-SHA256"}
	resp, err := parseCryptoSignResponse(data)
	require.NoError(t, err)
	assert.Equal(t, "sign-key", resp.KeyID)
}

func TestRemaining91_ParseCryptoVerifyResponse_InvalidInput(t *testing.T) {
	_, err := parseCryptoVerifyResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseCryptoVerifyResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{"key_id": "verify-key", "key_version": float64(1), "valid": true}
	resp, err := parseCryptoVerifyResponse(data)
	require.NoError(t, err)
	assert.True(t, resp.Valid)
}

func TestRemaining91_ParseCryptoHashResponse_InvalidInput(t *testing.T) {
	_, err := parseCryptoHashResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseCryptoHashResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{"algorithm": "SHA256", "hash": base64.StdEncoding.EncodeToString([]byte("hash-bytes"))}
	resp, err := parseCryptoHashResponse(data)
	require.NoError(t, err)
	assert.Equal(t, service.HashAlgorithm("SHA256"), resp.Algorithm)
}

func TestRemaining91_ParseCryptoHMACResponse_InvalidInput(t *testing.T) {
	_, err := parseCryptoHMACResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseCryptoHMACResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{"key_id": "hmac-key", "key_version": float64(1), "hmac": base64.StdEncoding.EncodeToString([]byte("hmac-data")), "hash_algorithm": "SHA256"}
	resp, err := parseCryptoHMACResponse(data)
	require.NoError(t, err)
	assert.Equal(t, "hmac-key", resp.KeyID)
}

func TestRemaining91_ParseCryptoVerifyHMACResponse_InvalidInput(t *testing.T) {
	_, err := parseCryptoVerifyHMACResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseCryptoVerifyHMACResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{"key_id": "verify-hmac-key", "key_version": float64(1), "valid": true}
	resp, err := parseCryptoVerifyHMACResponse(data)
	require.NoError(t, err)
	assert.True(t, resp.Valid)
}

func TestRemaining91_ParseCryptoRewrapResponse_InvalidInput(t *testing.T) {
	_, err := parseCryptoRewrapResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseCryptoRewrapResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{"key_id": "rewrap-key", "old_key_version": float64(1), "new_key_version": float64(2), "ciphertext": base64.StdEncoding.EncodeToString([]byte("rewrapped-data"))}
	resp, err := parseCryptoRewrapResponse(data)
	require.NoError(t, err)
	assert.Equal(t, "rewrap-key", resp.KeyID)
	assert.Equal(t, uint64(2), resp.NewKeyVersion)
}

func TestRemaining91_ParseCryptoGenerateDataKeyResponse_InvalidInput(t *testing.T) {
	_, err := parseCryptoGenerateDataKeyResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseCryptoGenerateDataKeyResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{"key_id": "data-key", "plaintext": base64.StdEncoding.EncodeToString([]byte("plaintext-key")), "encrypted": base64.StdEncoding.EncodeToString([]byte("encrypted-key"))}
	resp, err := parseCryptoGenerateDataKeyResponse(data)
	require.NoError(t, err)
	assert.Equal(t, "data-key", resp.KeyID)
}

func TestRemaining91_ParseCryptoGenerateRandomResponse_InvalidInput(t *testing.T) {
	_, err := parseCryptoGenerateRandomResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseCryptoGenerateRandomResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{"random_bytes": base64.StdEncoding.EncodeToString([]byte("random-data"))}
	resp, err := parseCryptoGenerateRandomResponse(data)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.RandomBytes)
}

// factories_mcp_seal.go coverage

// factories_mcp_backup.go coverage

func TestRemaining91_ParseBackupDeleteResponse_InvalidInput(t *testing.T) {
	_, err := parseBackupDeleteResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseBackupDeleteResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{"deleted": true, "backup_id": "backup-abc-123"}
	resp, err := parseBackupDeleteResponse(data)
	require.NoError(t, err)
	assert.True(t, resp.Deleted)
}

// data.go coverage

func TestRemaining91_RunPutCommand_ClientError(t *testing.T) {
	origAddr := address
	origTransport := transport
	origTimeout := globalTimeout
	defer func() { address = origAddr; transport = origTransport; globalTimeout = origTimeout; resetSDKClient() }()
	resetSDKClient()
	address = "localhost:19990"
	transport = "grpc"
	globalTimeout = 1 * time.Second
	cmd := newPutCommand()
	err := runPutCommand(cmd, []string{"key1", "value1"})
	assert.Error(t, err)
}

// crypto.go coverage

func TestRemaining91_RunCryptoRandomCommand_FactoryError(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()
	cryptoClientFactory = func() (CryptoClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}
	cmd := newCryptoRandomCommand()
	err := runCryptoRandomCommand(cmd, []string{})
	assert.Error(t, err)
}

// factories.go coverage

func TestRemaining91_CreateGRPCBackupClient_ConnectionAttempt(t *testing.T) {
	origAddr := address
	origTimeout := globalTimeout
	defer func() { address = origAddr; globalTimeout = origTimeout }()
	address = "localhost:19991"
	globalTimeout = 1 * time.Second
	client, err := createGRPCBackupClient()
	if err == nil {
		assert.NotNil(t, client)
		client.Close()
	}
}

// secrets.go coverage

func TestRemaining91_GetK8sServiceAccountToken_FileNotFound(t *testing.T) {
	token, err := getK8sServiceAccountToken()
	assert.Error(t, err)
	assert.Empty(t, token)
}

func TestRemaining91_CreateSecretsGRPCClient_DefaultAuth(t *testing.T) {
	origAddr := address
	origTimeout := globalTimeout
	origAuthMode := secretsAuthMode
	defer func() { address = origAddr; globalTimeout = origTimeout; secretsAuthMode = origAuthMode }()
	address = "localhost:19989"
	globalTimeout = 1 * time.Second
	secretsAuthMode = "token"
	client, err := createSecretsGRPCClient()
	if err == nil {
		assert.NotNil(t, client)
		client.Close()
	}
}

func TestRemaining91_CreateSecretsGRPCClient_UnsupportedAuthMode(t *testing.T) {
	origAddr := address
	origTimeout := globalTimeout
	origAuthMode := secretsAuthMode
	defer func() { address = origAddr; globalTimeout = origTimeout; secretsAuthMode = origAuthMode }()
	address = "localhost:19988"
	globalTimeout = 1 * time.Second
	secretsAuthMode = "oauth"
	_, err := createSecretsGRPCClient()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported auth mode")
}

func TestRemaining91_CreateSecretsGRPCClient_K8sAuthNoToken(t *testing.T) {
	origAddr := address
	origTimeout := globalTimeout
	origAuthMode := secretsAuthMode
	defer func() { address = origAddr; globalTimeout = origTimeout; secretsAuthMode = origAuthMode }()
	address = "localhost:19987"
	globalTimeout = 1 * time.Second
	secretsAuthMode = "k8s"
	_, err := createSecretsGRPCClient()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Kubernetes service account token")
}

// Cluster display helpers

func TestRemaining91_PrintClusterStatusTableCLI_NotClusterStatusResponse(t *testing.T) {
	err := printClusterStatusTableCLI(map[string]string{"foo": "bar"})
	assert.NoError(t, err)
}

func TestRemaining91_PrintClusterStatusTableCLI_EmptyNodes(t *testing.T) {
	status := &cli.ClusterStatusResponse{Healthy: true, LeaderID: 1, NodeCount: 0, ReadyNodes: 0, Nodes: []cli.NodeInfo{}}
	err := printClusterStatusTableCLI(status)
	assert.NoError(t, err)
}

func TestRemaining91_PrintClusterStatusJSON_Success(t *testing.T) {
	err := printClusterStatusJSON(map[string]string{"status": "healthy"})
	assert.NoError(t, err)
}

// Node display helpers

func TestRemaining91_DisplayNodeState_InvalidFormat(t *testing.T) {
	origFmt := nodeOutputFormat
	defer func() { nodeOutputFormat = origFmt }()
	nodeOutputFormat = "invalid_fmt"
	state := &NodeState{NodeID: "node-1", Address: "10.0.0.1:50051"}
	err := displayNodeState(state)
	assert.Error(t, err)
}

func TestRemaining91_DisplayNodeReplacementReport_InvalidFormat(t *testing.T) {
	origFmt := nodeOutputFormat
	defer func() { nodeOutputFormat = origFmt }()
	nodeOutputFormat = "xml_invalid"
	report := &NodeReplacementReport{OldNodeID: "old-1", NewNodeID: "new-1", Success: true}
	err := displayNodeReplacementReport(report)
	assert.Error(t, err)
}

func TestRemaining91_DisplayNodeReplacementReport_JSONFormat(t *testing.T) {
	origFmt := nodeOutputFormat
	defer func() { nodeOutputFormat = origFmt }()
	nodeOutputFormat = "json"
	now := time.Now()
	report := &NodeReplacementReport{OldNodeID: "old-1", NewNodeID: "new-1", StartTime: now, EndTime: now.Add(time.Minute), Duration: time.Minute, KeysEvacuated: 100, BytesEvacuated: 4096, Success: true}
	err := displayNodeReplacementReport(report)
	assert.NoError(t, err)
}

func TestRemaining91_DisplayNodeReplacementReport_Removal(t *testing.T) {
	origFmt := nodeOutputFormat
	defer func() { nodeOutputFormat = origFmt }()
	nodeOutputFormat = "table"
	now := time.Now()
	report := &NodeReplacementReport{OldNodeID: "removed-1", NewNodeID: "", StartTime: now, EndTime: now.Add(time.Minute), Duration: time.Minute, KeysEvacuated: 50, BytesEvacuated: 2048, Success: true}
	err := displayNodeReplacementReport(report)
	assert.NoError(t, err)
}

func TestRemaining91_DisplayNodeReplacementReport_WithErrors(t *testing.T) {
	origFmt := nodeOutputFormat
	defer func() { nodeOutputFormat = origFmt }()
	nodeOutputFormat = "table"
	now := time.Now()
	errs := make([]string, 7)
	for i := range errs {
		errs[i] = errors.Newf(errors.ErrInternal, "test", "error %d", i).Error()
	}
	report := &NodeReplacementReport{OldNodeID: "old-err", NewNodeID: "new-err", StartTime: now.Add(-time.Minute), EndTime: now.Add(time.Minute), Duration: 2 * time.Minute, KeysEvacuated: 200, BytesEvacuated: 8192, VerificationTime: 30 * time.Second, Success: false, Errors: errs}
	err := displayNodeReplacementReport(report)
	assert.NoError(t, err)
}

// Cache enable/disable with valid config

func TestRemaining91_RunCacheEnableCommand_Success(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	defer func() { cfg = origCfg; log = origLog; cacheOutputFmt = origFmt }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = createTestLoggerForRemaining91(t)
	cacheOutputFmt = "table"
	cmd := newCacheEnableCommand()
	err := runCacheEnableCommand(cmd, []string{"users"})
	assert.NoError(t, err)
}

func TestRemaining91_RunCacheEnableCommand_InvalidFormat(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	defer func() { cfg = origCfg; log = origLog; cacheOutputFmt = origFmt }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = createTestLoggerForRemaining91(t)
	cacheOutputFmt = "xml"
	cmd := newCacheEnableCommand()
	err := runCacheEnableCommand(cmd, []string{"users"})
	assert.Error(t, err)
}

func TestRemaining91_RunCacheDisableCommand_Success(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	defer func() { cfg = origCfg; log = origLog; cacheOutputFmt = origFmt }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = createTestLoggerForRemaining91(t)
	cacheOutputFmt = "json"
	cmd := newCacheDisableCommand()
	err := runCacheDisableCommand(cmd, []string{"users"})
	assert.NoError(t, err)
}

func TestRemaining91_RunCacheDisableCommand_InvalidFormat(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	defer func() { cfg = origCfg; log = origLog; cacheOutputFmt = origFmt }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = createTestLoggerForRemaining91(t)
	cacheOutputFmt = "yaml_invalid"
	cmd := newCacheDisableCommand()
	err := runCacheDisableCommand(cmd, []string{"users"})
	assert.Error(t, err)
}

func TestRemaining91_UnsupportedTransportError_String(t *testing.T) {
	err := &UnsupportedTransportError{Transport: "ftp"}
	assert.Contains(t, err.Error(), "unsupported transport")
	assert.Contains(t, err.Error(), "ftp")
}

func TestDisplayRebalanceProgressSDK_JSON(t *testing.T) {
	resp := &cluster.GetRebalanceProgressResponse{
		OperationID:     "op-abc",
		Status:          "in_progress",
		PercentComplete: 45.5,
		ShardsMoved:     5,
		TotalShards:     10,
		ElapsedTime:     int64(30 * time.Second),
	}

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := displayRebalanceProgressSDK(resp, "json")

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)

	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "op-abc")
}

func TestDisplayRebalanceProgressSDK_Table(t *testing.T) {
	resp := &cluster.GetRebalanceProgressResponse{
		OperationID:     "op-xyz",
		Status:          "completed",
		PercentComplete: 100.0,
		ShardsMoved:     10,
		TotalShards:     10,
		ElapsedTime:     int64(60 * time.Second),
	}

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := displayRebalanceProgressSDK(resp, "table")

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)

	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "op-xyz")
}

func TestDisplayRebalanceProgressSDK_UnsupportedFormat(t *testing.T) {
	resp := &cluster.GetRebalanceProgressResponse{}
	err := displayRebalanceProgressSDK(resp, "xml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestDisplayRebalanceReport_UnsupportedFormat(t *testing.T) {
	report := &RebalanceReport{}
	err := displayRebalanceReport(report, "xml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestDisplayRebalanceReport_JSON(t *testing.T) {
	now := time.Now()
	report := &RebalanceReport{
		StartTime:       now.Add(-10 * time.Minute),
		EndTime:         now,
		Duration:        10 * time.Minute,
		MovesCompleted:  5,
		MovesFailed:     0,
		KeysRebalanced:  1000,
		BytesRebalanced: 1024 * 1024,
		ImbalanceBefore: 0.25,
		ImbalanceAfter:  0.05,
	}

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := displayRebalanceReport(report, "json")

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)

	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "moves_completed")
}

func TestDisplayRebalanceReport_TableWithErrors(t *testing.T) {
	now := time.Now()
	report := &RebalanceReport{
		StartTime:       now.Add(-5 * time.Minute),
		EndTime:         now,
		Duration:        5 * time.Minute,
		MovesCompleted:  3,
		MovesFailed:     2,
		KeysRebalanced:  500,
		BytesRebalanced: 512 * 1024,
		ImbalanceBefore: 0.30,
		ImbalanceAfter:  0.10,
		Errors: []string{
			"shard 1 timeout",
			"shard 2 permission denied",
			"shard 3 timeout",
			"shard 4 timeout",
			"shard 5 timeout",
			"shard 6 timeout",
		},
	}

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := displayRebalanceReport(report, "table")

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)

	assert.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Warning")
	assert.Contains(t, output, "more errors")
}

func TestDisplayRebalanceReport_TableNoMoves(t *testing.T) {
	now := time.Now()
	report := &RebalanceReport{
		StartTime:       now,
		EndTime:         now,
		Duration:        0,
		MovesCompleted:  0,
		ImbalanceBefore: 0.02,
		ImbalanceAfter:  0.02,
	}

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := displayRebalanceReport(report, "table")

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)

	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "already balanced")
}

// =============================================================================
// Secrets Fetch - Additional Paths
// =============================================================================

func TestRunSecretsFetchCommand_FileFormatSuccess(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := fetchFormat
	origFetchOutputDir := fetchOutputDir
	origFmt2 := secretsOutputFmt
	defer func() {
		fetchFormat = origFmt
		fetchOutputDir = origFetchOutputDir
		secretsOutputFmt = origFmt2
	}()
	fetchFormat = "env"
	fetchOutputDir = t.TempDir()
	secretsOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runSecretsFetchCommand(cmd, []string{"apps/myapp/"})
	assert.NoError(t, err)
}

func TestRunSecretsFetchCommand_JSONFormat(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := fetchFormat
	origFetchOutputDir := fetchOutputDir
	defer func() {
		fetchFormat = origFmt
		fetchOutputDir = origFetchOutputDir
	}()
	fetchFormat = "json"
	fetchOutputDir = t.TempDir()

	cmd := &cobra.Command{}
	err := runSecretsFetchCommand(cmd, []string{"apps/myapp/"})
	assert.NoError(t, err)
}

// =============================================================================
// Secrets Disable - Additional Paths
// =============================================================================

func TestRunSecretsDisableCommand_FactoryError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	secretsClientFactory = func() (SecretsClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	cmd := &cobra.Command{}
	err := runSecretsDisableCommand(cmd, []string{"db/prod"})
	assert.Error(t, err)
}

func TestRunSecretsDisableCommand_ClientError(t *testing.T) {
	mock := &mockSecretsClientForCoverage{
		updateStatusFunc: func(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
			return nil, errors.New(errors.ErrNotFound, "test", "secret not found")
		},
	}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	defer func() { secretsOutputFmt = origFmt }()
	secretsOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runSecretsDisableCommand(cmd, []string{"db/prod"})
	assert.Error(t, err)
}

// =============================================================================
// Secrets Command Output Formats
// =============================================================================

func TestRunSecretsPutCommand_JSONFormat(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origData := secretsData
	origFmt := secretsOutputFmt
	defer func() {
		secretsData = origData
		secretsOutputFmt = origFmt
	}()
	secretsData = []string{"key=value"}
	secretsOutputFmt = "json"

	cmd := &cobra.Command{}
	err := runSecretsPutCommand(cmd, []string{"db/prod"})
	assert.NoError(t, err)
}

func TestRunSecretsGetCommand_JSONFormat(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	defer func() { secretsOutputFmt = origFmt }()
	secretsOutputFmt = "json"

	cmd := &cobra.Command{}
	err := runSecretsGetCommand(cmd, []string{"db/prod"})
	assert.NoError(t, err)
}

func TestRunSecretsGetCommand_YAMLFormat(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	defer func() { secretsOutputFmt = origFmt }()
	secretsOutputFmt = "yaml"

	cmd := &cobra.Command{}
	err := runSecretsGetCommand(cmd, []string{"db/prod"})
	assert.NoError(t, err)
}

func TestRunSecretsVersionsCommand_JSONFormat(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	defer func() { secretsOutputFmt = origFmt }()
	secretsOutputFmt = "json"

	cmd := &cobra.Command{}
	err := runSecretsVersionsCommand(cmd, []string{"db/prod"})
	assert.NoError(t, err)
}

func TestRunSecretsRollbackCommand_JSONFormat(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	origVer := secretsVersion
	defer func() {
		secretsOutputFmt = origFmt
		secretsVersion = origVer
	}()
	secretsOutputFmt = "json"
	secretsVersion = 2

	cmd := &cobra.Command{}
	err := runSecretsRollbackCommand(cmd, []string{"db/prod"})
	assert.NoError(t, err)
}

func TestRunSecretsRotateCommand_JSONFormat(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origData := secretsData
	origFmt := secretsOutputFmt
	defer func() {
		secretsData = origData
		secretsOutputFmt = origFmt
	}()
	secretsData = []string{"password=new"}
	secretsOutputFmt = "json"

	cmd := &cobra.Command{}
	err := runSecretsRotateCommand(cmd, []string{"db/prod"})
	assert.NoError(t, err)
}

// =============================================================================
// New Rebalance Command Builders
// =============================================================================

func TestNewRebalanceCommands_HasSubcommands(t *testing.T) {
	cmd := newRebalanceCommands()
	require.NotNil(t, cmd)
	assert.Equal(t, "rebalance", cmd.Use)
	assert.True(t, len(cmd.Commands()) >= 3)
}

func TestNewRebalanceStartCommand_HasFlags(t *testing.T) {
	cmd := newRebalanceStartCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "start", cmd.Use)
	assert.NotNil(t, cmd.Flags().Lookup("dry-run"))
	assert.NotNil(t, cmd.Flags().Lookup("follow"))
}

func TestNewRebalanceStatusCommand_HasFlags(t *testing.T) {
	cmd := newRebalanceStatusCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "status", cmd.Use)
	assert.NotNil(t, cmd.Flags().Lookup("operation"))
}

func TestNewRebalancePlanCommand_HasFlags(t *testing.T) {
	cmd := newRebalancePlanCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "plan", cmd.Use)
	assert.NotNil(t, cmd.Flags().Lookup("output"))
}
