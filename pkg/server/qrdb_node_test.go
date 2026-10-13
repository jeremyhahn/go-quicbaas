//go:build !integration

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// ---------------------------------------------------------------------------
// buildQRDBConfig
// ---------------------------------------------------------------------------

func TestBuildQRDBConfig_MemoryBackend(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory

	qcfg := mustBuildQRDBConfig(t, cfg, false)

	require.NotNil(t, qcfg)
	assert.Equal(t, "", qcfg.Raft.DataDir, "no DataDir when storage is memory")
	require.Len(t, qcfg.Databases, 1)
	assert.Equal(t, "memory", qcfg.Databases[0].Backend)
	assert.Equal(t, "passphrase", qcfg.Seal.Strategy, "default strategy is passphrase")
	assert.False(t, qcfg.Seal.Enabled, "seal disabled when quicbaas seal is disabled")
}

func TestBuildQRDBConfig_PebbleBackend(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendPebble
	cfg.Storage.DataDir = "/tmp/qrdb-test-data"

	qcfg := mustBuildQRDBConfig(t, cfg, false)

	require.NotNil(t, qcfg)
	assert.Equal(t, "/tmp/qrdb-test-data", qcfg.Raft.DataDir)
	require.Len(t, qcfg.Databases, 1)
	assert.Equal(t, "pebble", qcfg.Databases[0].Backend)
}

func TestBuildQRDBConfig_FileBackend(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendFile
	cfg.Storage.DataDir = "/tmp/qrdb-file-data"

	qcfg := mustBuildQRDBConfig(t, cfg, false)

	require.NotNil(t, qcfg)
	require.Len(t, qcfg.Databases, 1)
	assert.Equal(t, "file", qcfg.Databases[0].Backend)
}

func TestBuildQRDBConfig_UnknownBackendDefaultsToMemory(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Backend = "nonexistent"

	qcfg := mustBuildQRDBConfig(t, cfg, false)

	require.NotNil(t, qcfg)
	require.Len(t, qcfg.Databases, 1)
	assert.Equal(t, "memory", qcfg.Databases[0].Backend, "unknown backend falls back to memory")
}

func TestBuildQRDBConfig_SealEnabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "shamir"
	cfg.Seal.Threshold = 2
	cfg.Seal.TotalShares = 3

	qcfg := mustBuildQRDBConfig(t, cfg, false)

	assert.True(t, qcfg.Seal.Enabled)
	assert.Equal(t, "shamir", qcfg.Seal.Strategy)
	assert.Equal(t, 2, qcfg.Seal.ShamirThreshold)
	assert.Equal(t, 3, qcfg.Seal.ShamirTotal)
}

func TestBuildQRDBConfig_SealStrategyDefault(t *testing.T) {
	// When Strategy is empty, it defaults to "passphrase".
	cfg := &config.Config{}
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = ""

	qcfg := mustBuildQRDBConfig(t, cfg, false)

	assert.Equal(t, "passphrase", qcfg.Seal.Strategy)
}

func TestBuildQRDBConfig_DataDirFallbackToBarrierDir(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.DataDir = ""
	cfg.Seal.BarrierDir = "/tmp/barrier/barrier.db"

	qcfg := mustBuildQRDBConfig(t, cfg, false)

	// DataDir should fall back to BarrierDir when Storage.DataDir is empty.
	assert.Equal(t, "/tmp/barrier/barrier.db", qcfg.Raft.DataDir)
}

// ---------------------------------------------------------------------------
// buildXKMSCryptoOptions
// ---------------------------------------------------------------------------

func TestBuildXKMSCryptoOptions_SoftwareStrategy(t *testing.T) {
	cfg := &config.Config{}
	cfg.Seal.Strategy = "passphrase"

	opts, _, err := buildXKMSCryptoOptions(cfg, nil, nil, nil)

	require.NoError(t, err)
	// Must return at least 3 options: StrategyRegistry, PINBackend, FactorRegistry.
	assert.GreaterOrEqual(t, len(opts), 3, "XKMSCryptoOptions must return >=3 options")
}

func TestBuildXKMSCryptoOptions_NoneStrategy(t *testing.T) {
	cfg := &config.Config{}
	cfg.Seal.Strategy = "none"

	opts, _, err := buildXKMSCryptoOptions(cfg, nil, nil, nil)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(opts), 3)
}

func TestBuildXKMSCryptoOptions_EmptyStrategy(t *testing.T) {
	// An empty strategy should behave like passphrase (software).
	cfg := &config.Config{}
	cfg.Seal.Strategy = ""

	opts, _, err := buildXKMSCryptoOptions(cfg, nil, nil, nil)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(opts), 3)
}

// ---------------------------------------------------------------------------
// newNodeBackedShardedClient
// ---------------------------------------------------------------------------

func TestNewNodeBackedShardedClient_IsEmbedded(t *testing.T) {
	// Build a real memory-backed qrdb standalone node so we have a valid
	// *qrdbnode.Node to pass to newNodeBackedShardedClient.
	node := buildMemoryStandaloneNode(t)
	defer node.Close()

	sc := newNodeBackedShardedClient(node)
	require.NotNil(t, sc)
	assert.True(t, sc.IsEmbedded(), "node-backed client must report embedded transport")
}

func TestNewNodeBackedShardedClient_ForShardReturnsSameClient(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	defer node.Close()

	sc := newNodeBackedShardedClient(node)

	c1, err := sc.ForShard(1)
	require.NoError(t, err)
	require.NotNil(t, c1)

	c2, err := sc.ForShard(99)
	require.NoError(t, err)
	require.NotNil(t, c2)

	assert.Equal(t, c1, c2, "node-backed client returns the same client for every shard ID")
}

func TestNewNodeBackedShardedClient_CloseIsNoop(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	defer node.Close()

	sc := newNodeBackedShardedClient(node)

	// Close the ShardedClient — must not close the underlying node client.
	require.NoError(t, sc.Close())

	// The node client is still usable after the ShardedClient is "closed".
	// Node Close() is the lifecycle owner.
	assert.NotNil(t, node.Client(), "node client must still be live after ShardedClient.Close()")
}

// ---------------------------------------------------------------------------
// QRDBNodeError
// ---------------------------------------------------------------------------

func TestQRDBNodeError_ErrorMessage(t *testing.T) {
	e := &QRDBNodeError{Stage: "new standalone node", Cause: nil}
	assert.Equal(t, "qrdb node: new standalone node failed", e.Error())
}

func TestQRDBNodeError_ErrorMessageWithCause(t *testing.T) {
	cause := errors.New("test operation unavailable in standalone mode")
	e := &QRDBNodeError{Stage: "close", Cause: cause}
	assert.Contains(t, e.Error(), "close")
	assert.Contains(t, e.Error(), "test operation")
	assert.Equal(t, cause, e.Unwrap())
}

// ---------------------------------------------------------------------------
// standaloneQRDBDataDir
// ---------------------------------------------------------------------------

func TestStandaloneQRDBDataDir_UsesStorageDataDir(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.DataDir = "/data/storage"
	cfg.Seal.BarrierDir = "/data/barrier"

	assert.Equal(t, "/data/storage", standaloneQRDBDataDir(cfg))
}

func TestStandaloneQRDBDataDir_FallsBackToBarrierDir(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.DataDir = ""
	cfg.Seal.BarrierDir = "/data/barrier/barrier.db"

	dir := standaloneQRDBDataDir(cfg)
	assert.Equal(t, "/data/barrier", dir, "falls back to parent of BarrierDir")
}

func TestStandaloneQRDBDataDir_EmptyWhenBothEmpty(t *testing.T) {
	cfg := &config.Config{}
	assert.Equal(t, "", standaloneQRDBDataDir(cfg))
}

// ---------------------------------------------------------------------------
// initStandaloneQRDBNode / stopQRDBNode via Server
// ---------------------------------------------------------------------------

func TestInitStandaloneQRDBNode_SetsQRDBNode(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	s := &Server{
		config: &config.Config{},
		logger: log,
	}
	s.config.Storage.Backend = config.StorageBackendMemory
	// Vector storage is PebbleDB-backed and requires a data dir even when
	// the KV data plane runs in memory.
	s.config.Storage.DataDir = t.TempDir()

	require.NoError(t, s.initStandaloneQRDBNode())
	require.NotNil(t, s.qrdbNode, "qrdbNode must be set after successful init")

	// Clean up.
	require.NoError(t, s.stopQRDBNode())
	assert.Nil(t, s.qrdbNode, "qrdbNode must be nil after stopQRDBNode")
}

func TestStopQRDBNode_NilNodeIsNoop(t *testing.T) {
	s := &Server{}
	require.NoError(t, s.stopQRDBNode(), "stopQRDBNode on nil node must return nil")
}

// ---------------------------------------------------------------------------
// Helper: build a memory-backed standalone node for testing
// ---------------------------------------------------------------------------

// buildMemoryStandaloneNode creates a real *qrdbnode.Node backed by an
// in-memory storage engine. The KV data plane stays in memory; a temp
// DataDir is still required because qrdb's vector storage subsystem is
// PebbleDB-backed and BuildServices rejects an empty data directory.
// The caller must call node.Close(); the temp dir is cleaned up by t.
func buildMemoryStandaloneNode(t *testing.T, nodeOpts ...qrdbnode.NodeOption) *qrdbnode.Node {
	t.Helper()

	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Storage.DataDir = t.TempDir()

	qcfg := mustBuildQRDBConfig(t, cfg, false)
	// Disable all protocol listeners: we only need the embedded transport.
	qcfg.Protocols.REST = false
	qcfg.Protocols.GRPC = false
	qcfg.Protocols.QUIC = false
	qcfg.Protocols.Unix = false
	qcfg.Protocols.MCP = false

	serverOpts, _, err := buildXKMSCryptoOptions(cfg, nil, nil, nil)
	require.NoError(t, err)

	node, err := qrdbnode.NewStandaloneNode(
		context.Background(),
		qcfg,
		serverOpts,
		nodeOpts...,
	)
	require.NoError(t, err)
	require.NotNil(t, node)
	return node
}

// ---------------------------------------------------------------------------
// buildQRDBConfig — cluster mode
// ---------------------------------------------------------------------------

func TestBuildQRDBConfig_ClusterMode_SetsRaftAddress(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Raft.RaftAddress = "10.0.0.1:63101"
	cfg.Raft.NodeID = 1

	qcfg := mustBuildQRDBConfig(t, cfg, true)

	assert.Equal(t, "10.0.0.1:63101", qcfg.Raft.RaftAddress)
	assert.Equal(t, uint64(1), qcfg.Raft.ReplicaID)
	assert.Equal(t, uint64(1), qcfg.Raft.ShardID)
}

func TestBuildQRDBConfig_ClusterMode_DefaultShardID(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Raft.NodeID = 2

	qcfg := mustBuildQRDBConfig(t, cfg, true)

	assert.Equal(t, uint64(1), qcfg.Raft.ShardID, "ShardID defaults to 1 for the KV shard")
	assert.Equal(t, uint64(2), qcfg.Raft.DeploymentID)
}

func TestBuildQRDBConfig_ClusterMode_PeersWithIDPrefix(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Raft.NodeID = 1
	cfg.Raft.JoinAddresses = []string{"1=10.0.0.1:63101", "2=10.0.0.2:63101", "3=10.0.0.3:63101"}

	qcfg := mustBuildQRDBConfig(t, cfg, true)

	assert.Contains(t, qcfg.Raft.Peers, "1=10.0.0.1:63101")
	assert.Contains(t, qcfg.Raft.Peers, "2=10.0.0.2:63101")
}

func TestBuildQRDBConfig_ClusterMode_MemberIdentities(t *testing.T) {
	id2 := "spki:abababababababababababababababababababababababababababababababab"
	id3 := "spki:cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"
	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Raft.NodeID = 1
	cfg.Raft.JoinAddresses = []string{"1=10.0.0.1:63101", "2=10.0.0.2:63101", "3=10.0.0.3:63101"}
	cfg.Raft.MemberIdentities = []string{"2=" + id2, "3=" + id3}

	qcfg := mustBuildQRDBConfig(t, cfg, true)

	assert.Equal(t, "2="+id2+",3="+id3, qcfg.Raft.MemberIdentities)
}

func TestBuildQRDBConfig_ClusterMode_NoMemberIdentities(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Raft.NodeID = 1

	qcfg := mustBuildQRDBConfig(t, cfg, true)

	assert.Empty(t, qcfg.Raft.MemberIdentities)
}

func TestBuildQRDBConfig_ClusterMode_NoPeersWithoutIDPrefix(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Raft.NodeID = 1
	// JoinAddresses without "id=" prefix: no Peers set
	cfg.Raft.JoinAddresses = []string{"10.0.0.1:63101", "10.0.0.2:63101"}

	qcfg := mustBuildQRDBConfig(t, cfg, true)

	assert.Empty(t, qcfg.Raft.Peers, "no Peers string when join addresses lack id= prefix")
}

func TestBuildQRDBConfig_ClusterMode_DiscoveryMethodMap(t *testing.T) {
	tests := []struct {
		name           string
		quicbaasMethod string
		wantQRDB       string
	}{
		{"static", "static", "static"},
		{"multicast", "multicast", "multicast"},
		{"dns", "dns", "dns"},
		{"token", "token", "token"},
		{"kubernetes_fallback", "kubernetes", "static"},
		{"consul_fallback", "consul", "static"},
		{"unknown_fallback", "unknown", "static"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Storage.Backend = config.StorageBackendMemory
			cfg.Raft.NodeID = 1
			cfg.Discovery.Type = tt.quicbaasMethod

			qcfg := mustBuildQRDBConfig(t, cfg, true)
			assert.Equal(t, tt.wantQRDB, qcfg.Discovery.Method)
		})
	}
}

func TestBuildQRDBConfig_ClusterMode_DeploymentIDFallback(t *testing.T) {
	// NodeID=0 → DeploymentID defaults to 1 to avoid zero deployment ID.
	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Raft.NodeID = 0

	qcfg := mustBuildQRDBConfig(t, cfg, true)

	assert.Equal(t, uint64(1), qcfg.Raft.DeploymentID, "DeploymentID must not be zero")
}

// TestBuildQRDBConfig_ClusterMode_ExplicitDeploymentIDWins verifies that an
// explicitly configured DeploymentID takes precedence over NodeID and the
// last-resort fallback. This is the production multi-node path — all nodes
// in a cluster set the same deployment_id in their config.
func TestBuildQRDBConfig_ClusterMode_ExplicitDeploymentIDWins(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Raft.NodeID = 7        // would otherwise be used as DeploymentID
	cfg.Raft.DeploymentID = 42 // explicit operator value — must win

	qcfg := mustBuildQRDBConfig(t, cfg, true)

	assert.Equal(t, uint64(42), qcfg.Raft.DeploymentID,
		"explicit cfg.Raft.DeploymentID must take precedence over NodeID")
}

// TestBuildQRDBConfig_ClusterMode_DeploymentIDFallsBackToNodeID verifies that
// when DeploymentID is unset (0) but NodeID is set, DeploymentID is derived
// from NodeID for backward-compatible single-node dev/test mode.
func TestBuildQRDBConfig_ClusterMode_DeploymentIDFallsBackToNodeID(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Raft.NodeID = 5
	cfg.Raft.DeploymentID = 0 // unset

	qcfg := mustBuildQRDBConfig(t, cfg, true)

	assert.Equal(t, uint64(5), qcfg.Raft.DeploymentID,
		"DeploymentID should fall back to NodeID when not explicitly set")
}

// ---------------------------------------------------------------------------
// buildClusterMembers
// ---------------------------------------------------------------------------

func TestBuildClusterMembers_IDPrefixedPeers(t *testing.T) {
	cfg := &config.Config{}
	cfg.Raft.NodeID = 1
	cfg.Raft.JoinAddresses = []string{"1=10.0.0.1:63101", "2=10.0.0.2:63101", "3=10.0.0.3:63101"}

	members, err := buildClusterMembers(cfg)

	require.NoError(t, err)
	require.Len(t, members, 3)
	assert.Equal(t, "10.0.0.1:63101", members[1])
	assert.Equal(t, "10.0.0.2:63101", members[2])
	assert.Equal(t, "10.0.0.3:63101", members[3])
}

func TestBuildClusterMembers_NoPeers_SingleNodeFallback(t *testing.T) {
	cfg := &config.Config{}
	cfg.Raft.NodeID = 1
	cfg.Raft.RaftAddress = "127.0.0.1:63101"
	cfg.Raft.JoinAddresses = nil

	members, err := buildClusterMembers(cfg)

	require.NoError(t, err)
	require.Len(t, members, 1, "should fall back to single-node map")
	assert.Equal(t, "127.0.0.1:63101", members[1])
}

func TestBuildClusterMembers_NoPrefixPeers_SingleNodeFallback(t *testing.T) {
	cfg := &config.Config{}
	cfg.Raft.NodeID = 1
	cfg.Raft.RaftAddress = "127.0.0.1:63101"
	cfg.Raft.JoinAddresses = []string{"10.0.0.1:63101", "10.0.0.2:63101"}

	members, err := buildClusterMembers(cfg)

	require.NoError(t, err)
	require.Len(t, members, 1, "no id-prefixed entries → single-node fallback")
}

func TestBuildClusterMembers_ZeroNodeID_FallsBackToReplicaID1(t *testing.T) {
	cfg := &config.Config{}
	cfg.Raft.NodeID = 0
	cfg.Raft.RaftAddress = "127.0.0.1:63101"

	members, err := buildClusterMembers(cfg)

	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, "127.0.0.1:63101", members[1], "replicaID=1 when NodeID=0")
}

func TestBuildClusterMembers_InvalidPeerFormat(t *testing.T) {
	cfg := &config.Config{}
	cfg.Raft.NodeID = 1
	cfg.Raft.JoinAddresses = []string{"notvalid=noport"}

	// "notvalid" is not a valid uint64 → ParsePeers returns an error.
	_, err := buildClusterMembers(cfg)
	require.Error(t, err, "invalid id=addr peer format must error")
}

// ---------------------------------------------------------------------------
// buildRaftMTLSConfig
// ---------------------------------------------------------------------------

func TestBuildRaftMTLSConfig_NoPaths_ReturnsNil(t *testing.T) {
	cfg := &config.Config{}

	mtls, err := buildRaftMTLSConfig(cfg)

	require.NoError(t, err)
	assert.Nil(t, mtls, "no TLS paths → nil MTLSConfig (single-node dev mode)")
}

func TestBuildRaftMTLSConfig_MissingFile_ReturnsError(t *testing.T) {
	cfg := &config.Config{}
	cfg.Raft.TLSCAFile = "/nonexistent/ca.pem"
	cfg.Raft.TLSCertFile = "/nonexistent/cert.pem"
	cfg.Raft.TLSKeyFile = "/nonexistent/key.pem"

	_, err := buildRaftMTLSConfig(cfg)

	require.Error(t, err)
	var loadErr *RaftMTLSLoadError
	assert.ErrorAs(t, err, &loadErr, "error must be a RaftMTLSLoadError")
}

func TestBuildRaftMTLSConfig_LoadsFiles(t *testing.T) {
	dir := t.TempDir()

	caFile := filepath.Join(dir, "ca.pem")
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")

	require.NoError(t, os.WriteFile(caFile, []byte("ca-pem-bytes"), 0600))
	require.NoError(t, os.WriteFile(certFile, []byte("cert-pem-bytes"), 0600))
	require.NoError(t, os.WriteFile(keyFile, []byte("key-pem-bytes"), 0600))

	cfg := &config.Config{}
	cfg.Raft.TLSCAFile = caFile
	cfg.Raft.TLSCertFile = certFile
	cfg.Raft.TLSKeyFile = keyFile

	mtls, err := buildRaftMTLSConfig(cfg)

	require.NoError(t, err)
	require.NotNil(t, mtls)
	assert.Equal(t, []byte("ca-pem-bytes"), mtls.CACert)
	assert.Equal(t, []byte("cert-pem-bytes"), mtls.Cert)
	assert.Equal(t, []byte("key-pem-bytes"), mtls.Key)
}

// ---------------------------------------------------------------------------
// initClusteredQRDBNode + stopQRDBNode
// ---------------------------------------------------------------------------

func TestInitClusteredQRDBNode_SingleNode_BootsAndStops(t *testing.T) {
	// Single-node cluster (WithoutTransport) boots in-process without a
	// real network transport. This validates the full
	// buildQRDBConfig(clusterMode=true) → buildClusterMembers →
	// NewClusteredNode wiring.
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	s := &Server{
		config: &config.Config{},
		logger: log,
	}
	s.config.Storage.Backend = config.StorageBackendMemory
	// A temp DataDir keeps the clustered node from defaulting to ./data
	// in the package directory (and from flock-colliding with concurrent
	// runs); vector storage also requires a data dir.
	s.config.Storage.DataDir = t.TempDir()
	s.config.Raft.NodeID = 1
	// No JoinAddresses: buildClusterMembers returns a single-node map
	// with replicaID=1 → WithoutTransport is applied by NewClusteredNode.

	require.NoError(t, s.initClusteredQRDBNode())
	require.NotNil(t, s.qrdbNode, "qrdbNode must be set after successful clustered init")

	// Client and services must be live.
	assert.NotNil(t, s.qrdbNode.Client())
	assert.NotNil(t, s.qrdbNode.Services())

	require.NoError(t, s.stopQRDBNode())
	assert.Nil(t, s.qrdbNode, "qrdbNode must be nil after stopQRDBNode")
}

func TestInitClusteredQRDBNode_MemoryKVRoundTrip(t *testing.T) {
	// Verify that a single-node clustered node can perform a KV round-trip.
	// Raft leader election completes asynchronously; we retry with
	// exponential back-off until the leader is elected or the test times out.
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	s := &Server{
		config: &config.Config{},
		logger: log,
	}
	s.config.Storage.Backend = config.StorageBackendMemory
	s.config.Storage.DataDir = t.TempDir() // avoid ./data default + flock collisions
	s.config.Raft.NodeID = 1

	require.NoError(t, s.initClusteredQRDBNode())
	t.Cleanup(func() { _ = s.stopQRDBNode() })

	require.NotNil(t, s.qrdbNode)
	client := s.qrdbNode.Client()
	require.NotNil(t, client)

	const key = "phase-c-cluster-key"
	value := []byte("phase-c-cluster-value")

	// Retry until the single-node Raft leader is elected and the Put
	// succeeds. Leader election in WithoutTransport mode is typically
	// sub-10ms but may take a few election RTT cycles under CPU contention.
	deadline := context.Background()
	ctx, cancel := context.WithTimeout(deadline, 30*time.Second)
	defer cancel()

	waitCh := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				waitCh <- ctx.Err()
				return
			case <-ticker.C:
				if putErr := client.KV().Put(ctx, key, value); putErr == nil {
					waitCh <- nil
					return
				}
			}
		}
	}()

	require.NoError(t, <-waitCh, "Put must succeed once leader is elected")

	got, err := client.KV().Get(ctx, key)
	require.NoError(t, err, "Get must succeed")
	assert.Equal(t, value, got, "Get must return the Put value")
}

// ---------------------------------------------------------------------------
// newNodeBackedShardedClient — clustered node path
// ---------------------------------------------------------------------------

func TestNewNodeBackedShardedClient_FromClusteredNode(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	s := &Server{
		config: &config.Config{},
		logger: log,
	}
	s.config.Storage.Backend = config.StorageBackendMemory
	s.config.Storage.DataDir = t.TempDir() // avoid ./data default + flock collisions
	s.config.Raft.NodeID = 1

	require.NoError(t, s.initClusteredQRDBNode())
	t.Cleanup(func() { _ = s.stopQRDBNode() })

	sc := newNodeBackedShardedClient(s.qrdbNode)
	require.NotNil(t, sc)
	assert.True(t, sc.IsEmbedded(), "clustered node-backed client must report embedded transport")

	c1, err := sc.ForShard(1)
	require.NoError(t, err)
	require.NotNil(t, c1)

	c2, err := sc.ForShard(99)
	require.NoError(t, err)
	assert.Equal(t, c1, c2, "all shard IDs resolve to the same node client")
}

// ---------------------------------------------------------------------------
// buildXKMSFactors
// ---------------------------------------------------------------------------

func TestBuildXKMSFactors_ProducesFIDO2PIVPkcs11AndMTLS(t *testing.T) {
	cfg := &config.Config{}
	cfg.IAM.WebAuthn.RPID = "example.com"

	factors := buildXKMSFactors(cfg)
	require.Len(t, factors, 4, "expected fido2 + piv + pkcs11 + mtls factors")

	names := map[string]bool{}
	for _, f := range factors {
		require.NotNil(t, f, "factor plugin must not be nil")
		names[f.Name()] = true
	}
	assert.True(t, names["fido2"], "fido2 factor must be registered for WebAuthn login")
	assert.True(t, names["piv"], "piv factor must be registered for PIV login")
	assert.True(t, names["pkcs11"], "pkcs11 factor must be registered for HSM login")
	assert.True(t, names["mtls"], "mtls factor must be registered for certificate-possession enrollment")

	// The produced factors must register cleanly into the composed qrdb
	// FactorRegistry (no nil, no duplicate-name collisions).
	opts, err := XKMSCryptoOptions(&XKMSCryptoConfig{Factors: factors})
	require.NoError(t, err)
	require.Len(t, opts, 3, "StrategyRegistry + PINBackend + FactorRegistry")
}

// TestBuildXKMSFactors_NilConfigStillProducesFactors proves the safety
// property: a node with NO PKCS#11 (or any HSM) configured still builds its
// full factor set without error, because the PKCS#11 factor — like FIDO2/PIV —
// is lazy and never touches an HSM at construction.
func TestBuildXKMSFactors_NilConfigStillProducesFactors(t *testing.T) {
	factors := buildXKMSFactors(nil)
	require.Len(t, factors, 4, "nil config must still yield the default factor set")

	names := map[string]bool{}
	for _, f := range factors {
		require.NotNil(t, f, "factor plugin must not be nil even with nil config")
		names[f.Name()] = true
	}
	assert.True(t, names["pkcs11"], "pkcs11 factor must register without an HSM configured")
}

// ---------------------------------------------------------------------------
// buildPINBackend
// ---------------------------------------------------------------------------

func TestBuildPINBackend_PersistentSoftwareUnderDataDir(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.Storage.DataDir = dir

	be, err := buildPINBackend(cfg, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, be)
	// With no TPM provider wired, the selector resolves the software backend.
	assert.Equal(t, "software", string(be.Strategy()))

	// The persistent PIN directory must be created under the data dir so SO/
	// User PIN hashes survive a daemon restart (the bug this fixes).
	if _, statErr := os.Stat(filepath.Join(dir, "pin")); statErr != nil {
		t.Fatalf("expected persistent pin dir under data dir: %v", statErr)
	}
}

func TestBuildPINBackend_NoDataDirIsInMemorySoftware(t *testing.T) {
	be, err := buildPINBackend(&config.Config{}, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, be)
	assert.Equal(t, "software", string(be.Strategy()))
}

// initStandaloneQRDBNode with a passphrase seal config exercises the
// WithSealPassphrase injection branch and seal-enabled strategy resolution.
func TestInitStandaloneQRDBNode_SealPassphrase(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Storage.DataDir = t.TempDir()
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "passphrase"

	srv, err := NewServer(cfg, WithSealPassphrase([]byte("super-secret-passphrase")))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	if err := srv.initStandaloneQRDBNode(); err != nil {
		t.Fatalf("initStandaloneQRDBNode: %v", err)
	}
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	if srv.qrdbNode == nil {
		t.Fatal("qrdb node must be constructed")
	}
	// The passphrase slice must be zeroed after node construction.
	if srv.opts.sealPassphrase != nil {
		t.Fatal("seal passphrase must be cleared after node construction")
	}
}

// initClusteredQRDBNode with a passphrase seal config exercises the cluster
// seal-passphrase injection and seal-enabled strategy resolution branches.
func TestInitClusteredQRDBNode_SealPassphrase(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Storage.DataDir = t.TempDir()
	cfg.Server.Mode = config.ServerModeCluster
	cfg.Raft.NodeID = 1
	cfg.Raft.RaftAddress = "127.0.0.1:" + itoaPort(getFreePort(t))
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "passphrase"

	srv, err := NewServer(cfg, WithSealPassphrase([]byte("cluster-passphrase")))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	if err := srv.initClusteredQRDBNode(); err != nil {
		t.Fatalf("initClusteredQRDBNode: %v", err)
	}
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	if srv.qrdbNode == nil {
		t.Fatal("clustered qrdb node must be constructed")
	}
	if srv.opts.sealPassphrase != nil {
		t.Fatal("seal passphrase must be cleared after node construction")
	}
}

func itoaPort(p int) string {
	return strconv.Itoa(p)
}

// TestInitStandaloneQRDBNode_HardwareStrategyNotCompiled covers the
// hardware-seal-strategy error branch of initStandaloneQRDBNode: selecting a
// build-tagged hardware strategy (awskms) that is not compiled into the default
// build makes buildHardwareSealStrategies fail, and the node init must surface a
// typed QRDBNodeError wrapping the StrategyNotCompiledError.
func TestInitStandaloneQRDBNode_HardwareStrategyNotCompiled(t *testing.T) {
	// awskms is build-tagged and absent from the default test build, so its
	// factory must NOT be registered — this is the premise that makes the
	// not-compiled branch reachable. (Guarded as a require, never skipped.)
	if _, registered := hardwareStrategyFactories["awskms"]; registered {
		t.Fatal("awskms must not be compiled into the default build for this test")
	}

	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Storage.DataDir = t.TempDir()
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "awskms" // build-tagged, absent from the default build

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	err = srv.initStandaloneQRDBNode()
	if err == nil {
		t.Fatal("expected a QRDBNodeError for a not-compiled hardware strategy")
	}
	var nodeErr *QRDBNodeError
	if !errors.As(err, &nodeErr) {
		t.Fatalf("expected *QRDBNodeError, got %T: %v", err, err)
	}
	var notCompiled *StrategyNotCompiledError
	if !errors.As(err, &notCompiled) {
		t.Fatalf("QRDBNodeError must wrap a *StrategyNotCompiledError, got %v", err)
	}
}

// TestInitClusteredQRDBNode_HardwareStrategyNotCompiled covers the
// hardware-seal-strategy error branch of initClusteredQRDBNode: as with the
// standalone path, a not-compiled hardware strategy must surface a typed
// QRDBNodeError before any raft host is constructed.
func TestInitClusteredQRDBNode_HardwareStrategyNotCompiled(t *testing.T) {
	if _, registered := hardwareStrategyFactories["awskms"]; registered {
		t.Fatal("awskms must not be compiled into the default build for this test")
	}

	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Storage.DataDir = t.TempDir()
	cfg.Server.Mode = config.ServerModeCluster
	cfg.Raft.NodeID = 1
	cfg.Raft.RaftAddress = "127.0.0.1:" + itoaPort(getFreePort(t))
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "awskms"

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	err = srv.initClusteredQRDBNode()
	if err == nil {
		t.Fatal("expected a QRDBNodeError for a not-compiled hardware strategy")
	}
	var notCompiled *StrategyNotCompiledError
	if !errors.As(err, &notCompiled) {
		t.Fatalf("clustered init must wrap a *StrategyNotCompiledError, got %v", err)
	}
}

// TestBuildRaftMTLSConfig_CertReadError isolates the cert-file read-error branch:
// a present CA file but a missing cert file makes the cert read fail after the
// CA read succeeds.
func TestBuildRaftMTLSConfig_CertReadError(t *testing.T) {
	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(caFile, []byte("ca"), 0o600))

	cfg := &config.Config{}
	cfg.Raft.TLSCAFile = caFile
	cfg.Raft.TLSCertFile = filepath.Join(dir, "missing-cert.pem")

	_, err := buildRaftMTLSConfig(cfg)
	require.Error(t, err)
	var loadErr *RaftMTLSLoadError
	require.ErrorAs(t, err, &loadErr)
	require.Equal(t, cfg.Raft.TLSCertFile, loadErr.File)
}

// TestBuildRaftMTLSConfig_KeyReadError isolates the key-file read-error branch:
// present CA + cert files but a missing key file makes the key read fail.
func TestBuildRaftMTLSConfig_KeyReadError(t *testing.T) {
	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	certFile := filepath.Join(dir, "cert.pem")
	require.NoError(t, os.WriteFile(caFile, []byte("ca"), 0o600))
	require.NoError(t, os.WriteFile(certFile, []byte("cert"), 0o600))

	cfg := &config.Config{}
	cfg.Raft.TLSCAFile = caFile
	cfg.Raft.TLSCertFile = certFile
	cfg.Raft.TLSKeyFile = filepath.Join(dir, "missing-key.pem")

	_, err := buildRaftMTLSConfig(cfg)
	require.Error(t, err)
	var loadErr *RaftMTLSLoadError
	require.ErrorAs(t, err, &loadErr)
	require.Equal(t, cfg.Raft.TLSKeyFile, loadErr.File)
}

// TestSoftwarePINProvider_Storage_EmptyDataDir covers the in-memory branch of
// softwarePINProvider.Storage: an empty data dir yields (nil, nil) so the PIN
// backend uses in-memory storage.
func TestSoftwarePINProvider_Storage_EmptyDataDir(t *testing.T) {
	p := softwarePINProvider{dataDir: ""}
	store, err := p.Storage()
	require.NoError(t, err)
	require.Nil(t, store, "an empty data dir must yield a nil (in-memory) PIN store")
}

// TestSoftwarePINProvider_Storage_MkdirError covers the MkdirAll error branch:
// when the data dir is a regular file, creating the pin subdirectory fails and
// Storage returns a typed PINBackendError.
func TestSoftwarePINProvider_Storage_MkdirError(t *testing.T) {
	base := t.TempDir()
	dataFile := filepath.Join(base, "datadir-as-file")
	require.NoError(t, os.WriteFile(dataFile, []byte("x"), 0o600))

	p := softwarePINProvider{dataDir: dataFile}
	store, err := p.Storage()
	require.Error(t, err, "MkdirAll under a regular file must fail")
	require.Nil(t, store)
	var pinErr *PINBackendError
	require.ErrorAs(t, err, &pinErr)
	require.Equal(t, "mkdir pin dir", pinErr.Stage)
}

// mustBuildQRDBConfig wraps buildQRDBConfig for tests that exercise the
// mapping logic itself and treat a resolution failure as fatal.
func mustBuildQRDBConfig(t *testing.T, cfg *config.Config, clusterMode bool) *qrdbsdk.DaemonConfig {
	t.Helper()
	qcfg, err := buildQRDBConfig(cfg, clusterMode)
	if err != nil {
		t.Fatalf("buildQRDBConfig: %v", err)
	}
	return qcfg
}
