//go:build raft

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

package ha

import (
	"context"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// ----------------------------------------------------------------------------
// Test helpers
// ----------------------------------------------------------------------------

// newHealthServer starts an httptest TLS server that serves GET /health with
// the given status code. t.Cleanup registers Close so the caller need not.
// Use caPoolFromServer to obtain the CA pool and pass it as PartitionConfig.ClusterCA
// so pingPeer can verify the server's self-signed certificate.
func newHealthServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// caPoolFromServer extracts an *x509.CertPool that trusts the self-signed
// certificate served by an httptest.NewTLSServer. Pass the returned pool as
// PartitionConfig.ClusterCA so peer health probes verify the cert properly.
func caPoolFromServer(srv *httptest.Server) *x509.CertPool {
	pool := x509.NewCertPool()
	if srv.TLS != nil {
		for _, cert := range srv.TLS.Certificates {
			for _, der := range cert.Certificate {
				c, err := x509.ParseCertificate(der)
				if err == nil {
					pool.AddCert(c)
				}
			}
		}
	}
	return pool
}

// caPoolFromServers merges the CA pools of multiple httptest TLS servers into
// a single *x509.CertPool. Use this when a PartitionConfig references more
// than one test server so all peer certificates are trusted.
func caPoolFromServers(srvs ...*httptest.Server) *x509.CertPool {
	pool := x509.NewCertPool()
	for _, srv := range srvs {
		if srv == nil || srv.TLS == nil {
			continue
		}
		for _, cert := range srv.TLS.Certificates {
			for _, der := range cert.Certificate {
				c, err := x509.ParseCertificate(der)
				if err == nil {
					pool.AddCert(c)
				}
			}
		}
	}
	return pool
}

// serverHostPort strips the "https://" scheme from the TLS test-server URL
// and returns the bare host:port to pass as a Peers address.
func serverHostPort(srv *httptest.Server) string {
	// srv.URL = "https://127.0.0.1:PORT"
	if len(srv.URL) > len("https://") {
		return srv.URL[len("https://"):]
	}
	return srv.URL[len("http://"):]
}

// unreachableAddr is a syntactically valid host:port that will always fail to
// connect in unit tests. Port 1 is reserved; there is no listener there.
const unreachableAddr = "127.0.0.1:1"

// TestNewPartitionDetector tests the constructor with various configurations.
func TestNewPartitionDetector(t *testing.T) {
	tests := []struct {
		name        string
		raftNode    interfaces.RaftNode
		config      *PartitionConfig
		logger      interfaces.Logger
		expectError bool
		errorCode   errors.ErrorCode
	}{
		{
			name:     "valid configuration",
			raftNode: newMockRaftNode(),
			config: &PartitionConfig{
				DetectionInterval:  10 * time.Second,
				PeerTimeout:        30 * time.Second,
				HealthCheckTimeout: 5 * time.Second,
				AutoRecovery:       true,
				ReadOnlyOnMinority: true,
				Peers: map[uint64]string{
					2: "node2:8001",
					3: "node3:8001",
				},
			},
			logger:      newMockLogger(),
			expectError: false,
		},
		{
			name:        "config with defaults",
			raftNode:    newMockRaftNode(),
			config:      &PartitionConfig{},
			logger:      newMockLogger(),
			expectError: false,
		},
		{
			name:        "nil raft node",
			raftNode:    nil,
			config:      &PartitionConfig{},
			logger:      newMockLogger(),
			expectError: true,
			errorCode:   errors.ErrInvalidInput,
		},
		{
			name:        "nil config",
			raftNode:    newMockRaftNode(),
			config:      nil,
			logger:      newMockLogger(),
			expectError: true,
			errorCode:   errors.ErrInvalidInput,
		},
		{
			name:        "nil logger",
			raftNode:    newMockRaftNode(),
			config:      &PartitionConfig{},
			logger:      nil,
			expectError: true,
			errorCode:   errors.ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pd, err := NewPartitionDetector(tt.raftNode, tt.config, tt.logger)

			if tt.expectError {
				require.Error(t, err)
				qbErr, ok := err.(*errors.QBError)
				require.True(t, ok, "expected QBError")
				assert.Equal(t, tt.errorCode, qbErr.Code)
				assert.Nil(t, pd)
			} else {
				require.NoError(t, err)
				require.NotNil(t, pd)

				// Verify defaults are applied
				assert.Equal(t, 10*time.Second, pd.config.DetectionInterval)
				assert.Equal(t, 30*time.Second, pd.config.PeerTimeout)
				assert.Equal(t, 5*time.Second, pd.config.HealthCheckTimeout)

				// Verify initialization
				assert.NotNil(t, pd.peers)
				assert.NotNil(t, pd.metrics)
			}
		})
	}
}

// TestPartitionDetector_MonitorLoopTicker tests the automatic monitoring via ticker.
// It verifies that the monitor loop fires without panicking and that Stop()
// cleanly terminates the goroutine. The test deliberately uses a short ticker
// with a healthy peer so that the first completed checks register the peer as
// reachable before Stop is called.
func TestPartitionDetector_MonitorLoopTicker(t *testing.T) {
	srv := newHealthServer(t, http.StatusOK)

	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		DetectionInterval:  50 * time.Millisecond,
		HealthCheckTimeout: 500 * time.Millisecond,
		Peers: map[uint64]string{
			2: serverHostPort(srv),
		},
		ClusterCA: caPoolFromServer(srv),
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	err = pd.Start()
	require.NoError(t, err)

	// Wait for at least 3 ticks to complete.
	time.Sleep(200 * time.Millisecond)

	err = pd.Stop()
	require.NoError(t, err)

	// The monitor loop and all probe goroutines must have exited by now.
	// We cannot assert pd.currentPartition == nil because the inner probe
	// goroutines run concurrently; we assert only that Stop did not block
	// indefinitely and that the detector is accessible without data races.
	status := pd.GetStatus()
	assert.Equal(t, 1, status.TotalPeers, "peer count must match config")
}

// TestPartitionDetector_MonitorLoopStopChan tests stopping via stopChan.
func TestPartitionDetector_MonitorLoopStopChan(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		DetectionInterval: 10 * time.Second, // Long interval — no tick fires before Stop.
		Peers: map[uint64]string{
			2: unreachableAddr,
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	err = pd.Start()
	require.NoError(t, err)

	// Stop immediately via stopChan
	err = pd.Stop()
	require.NoError(t, err)

	// A second Stop must be idempotent
	err = pd.Stop()
	require.NoError(t, err)
}

// TestPartitionDetector_MonitorLoopContextCancel tests stopping via context cancel.
func TestPartitionDetector_MonitorLoopContextCancel(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		DetectionInterval: 10 * time.Second, // Long interval — no tick fires.
		Peers: map[uint64]string{
			2: unreachableAddr,
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	err = pd.Start()
	require.NoError(t, err)

	// Cancel context directly
	pd.cancel()

	// Wait for goroutine to exit
	pd.wg.Wait()

	// Verify it stopped cleanly
	assert.NotNil(t, pd)
}

// TestPartitionDetector_DetectPartition tests partition detection using real
// HTTP health servers. Reachable peers are backed by an httptest.Server
// returning 200; unreachable peers use an address that always refuses
// connections (127.0.0.1:1).
func TestPartitionDetector_DetectPartition(t *testing.T) {
	reachableSrv2 := newHealthServer(t, http.StatusOK)
	reachableSrv3 := newHealthServer(t, http.StatusOK)

	tests := []struct {
		name              string
		setupPeers        map[uint64]string
		expectPartitioned bool
	}{
		{
			name: "all peers reachable",
			setupPeers: map[uint64]string{
				2: serverHostPort(reachableSrv2),
				3: serverHostPort(reachableSrv3),
			},
			expectPartitioned: false,
		},
		{
			name: "one peer unreachable",
			setupPeers: map[uint64]string{
				2: unreachableAddr,
				3: serverHostPort(reachableSrv3),
			},
			expectPartitioned: true,
		},
		{
			name: "all peers unreachable",
			setupPeers: map[uint64]string{
				2: unreachableAddr,
				3: unreachableAddr,
			},
			expectPartitioned: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			mockRaft.setLeader(1, 1)

			config := &PartitionConfig{
				Peers:              tt.setupPeers,
				HealthCheckTimeout: 500 * time.Millisecond,
				ClusterCA:          caPoolFromServers(reachableSrv2, reachableSrv3),
			}

			pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			ctx := context.Background()
			partition, err := pd.DetectPartition(ctx)

			require.NoError(t, err)
			if tt.expectPartitioned {
				assert.NotNil(t, partition)
				assert.False(t, partition.Healed)
				assert.NotEmpty(t, partition.Minority)
			} else {
				assert.Nil(t, partition)
			}
		})
	}
}

// TestPartitionDetector_DetectPartitionHealing tests partition heal detection.
// It uses a controllable health server whose response code is changed between
// the first and second DetectPartition calls to simulate partition and healing.
func TestPartitionDetector_DetectPartitionHealing(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	// Use an atomic to switch the health response code between calls.
	var healthStatus atomic.Int32
	healthStatus.Store(int32(http.StatusInternalServerError)) // initially unhealthy

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(int(healthStatus.Load()))
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	config := &PartitionConfig{
		Peers: map[uint64]string{
			2: serverHostPort(srv),
		},
		HealthCheckTimeout: 500 * time.Millisecond,
		ClusterCA:          caPoolFromServer(srv),
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	ctx := context.Background()

	// First detection: peer returns 500 → unreachable → partition detected.
	partition, err := pd.DetectPartition(ctx)
	require.NoError(t, err)
	require.NotNil(t, partition)
	assert.False(t, partition.Healed)

	// Simulate healing: peer now returns 200.
	healthStatus.Store(int32(http.StatusOK))

	// Second detection: peer is reachable → partition healed.
	partition2, err := pd.DetectPartition(ctx)
	require.NoError(t, err)
	assert.Nil(t, partition2)

	// The previous partition must be marked as healed.
	pd.partitionMu.RLock()
	assert.True(t, pd.currentPartition.Healed)
	assert.False(t, pd.currentPartition.HealedAt.IsZero())
	pd.partitionMu.RUnlock()
}

// TestPartitionDetector_FindIsolatedNodes tests isolated node identification.
func TestPartitionDetector_FindIsolatedNodes(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		Peers: map[uint64]string{
			2: "node2:8001",
			3: "node3:8001",
			4: "node4:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	reachability := map[uint64]bool{
		2: true,
		3: false,
		4: false,
	}

	isolated := pd.findIsolatedNodes(reachability)
	assert.Len(t, isolated, 2)
	assert.Contains(t, isolated, uint64(3))
	assert.Contains(t, isolated, uint64(4))
}

// TestPartitionDetector_HandlePartition tests that handlePartition enters
// read-only mode only when ReadOnlyOnMinority is set and this node is in the
// minority partition.
func TestPartitionDetector_HandlePartition(t *testing.T) {
	tests := []struct {
		name               string
		readOnlyOnMinority bool
		partition          *Partition
		wantReadOnly       bool
	}{
		{
			name:               "minority enters read-only",
			readOnlyOnMinority: true,
			partition:          &Partition{Majority: []uint64{}, Minority: []uint64{2, 3}},
			wantReadOnly:       true,
		},
		{
			name:               "minority with read-only disabled",
			readOnlyOnMinority: false,
			partition:          &Partition{Majority: []uint64{}, Minority: []uint64{2, 3}},
			wantReadOnly:       false,
		},
		{
			name:               "majority stays read-write",
			readOnlyOnMinority: true,
			partition:          &Partition{Majority: []uint64{2, 3}, Minority: []uint64{}},
			wantReadOnly:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			mockRaft.setLeader(1, 1)

			config := &PartitionConfig{
				ReadOnlyOnMinority: tt.readOnlyOnMinority,
				Peers: map[uint64]string{
					2: "node2:8001",
					3: "node3:8001",
				},
			}

			pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			err = pd.handlePartition(context.Background(), tt.partition)
			require.NoError(t, err)
			assert.Equal(t, tt.wantReadOnly, pd.IsReadOnlyMode())
		})
	}
}

// TestPartitionDetector_ReadOnlyMode tests read-only mode handling.
func TestPartitionDetector_ReadOnlyMode(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		ReadOnlyOnMinority: true,
		Peers: map[uint64]string{
			2: "node2:8001",
			3: "node3:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	t.Run("initial state - read-write mode", func(t *testing.T) {
		assert.False(t, pd.IsReadOnlyMode())
	})

	t.Run("enter read-only mode", func(t *testing.T) {
		err := pd.enterReadOnlyMode()
		require.NoError(t, err)
		assert.True(t, pd.IsReadOnlyMode())

		// Entering again should be idempotent
		err = pd.enterReadOnlyMode()
		require.NoError(t, err)
		assert.True(t, pd.IsReadOnlyMode())
	})

	t.Run("exit read-only mode", func(t *testing.T) {
		err := pd.exitReadOnlyMode()
		require.NoError(t, err)
		assert.False(t, pd.IsReadOnlyMode())

		// Exiting again should be idempotent
		err = pd.exitReadOnlyMode()
		require.NoError(t, err)
		assert.False(t, pd.IsReadOnlyMode())
	})
}

// TestPartitionDetector_GetStatus tests status reporting.
func TestPartitionDetector_GetStatus(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		Peers: map[uint64]string{
			2: "node2:8001",
			3: "node3:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	status := pd.GetStatus()
	assert.NotNil(t, status)
	assert.False(t, status.ReadOnlyMode)
	assert.Equal(t, 2, status.TotalPeers)
}

// TestPartitionDetector_GetPeerStates tests peer state retrieval.
func TestPartitionDetector_GetPeerStates(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		Peers: map[uint64]string{
			2: "node2:8001",
			3: "node3:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	states := pd.GetPeerStates()
	assert.Len(t, states, 2)

	for nodeID, state := range states {
		assert.Equal(t, nodeID, state.NodeID)
		assert.NotEmpty(t, state.Address)
		assert.True(t, state.Reachable)
	}
}

// TestPartitionDetector_CheckPeerReachability checks a mixed peer set: one
// backed by a live TLS health server, one at an address nothing is listening on.
//
// HealthCheckTimeout is a DEADLOCK DETECTOR here, not a budget, and the two
// assertions below are load-sensitive in opposite directions:
//
//   - node 3 (unreachable) cannot fail from slowness. A slow machine only makes
//     it more unreachable, and it does not wait for this deadline at all:
//     127.0.0.1:1 is connection-refused, measured at 43-648us with a 30s dial
//     timeout available. So a larger value costs this test nothing.
//   - node 2 (healthy) is bounded by it. A real TLS handshake plus request has
//     to finish inside the deadline, so a scheduling delay past it reports a
//     healthy peer as unreachable -- the code is right and the test is wrong.
//
// At 500ms that was the whole flake (go-quicbaas#425): a value chosen to keep
// the unreachable case fast, imposed on the reachable case that actually needs
// time. The unreachable case never needed it, so the deadline is now set where
// only a genuine hang can reach it.
func TestPartitionDetector_CheckPeerReachability(t *testing.T) {
	srv := newHealthServer(t, http.StatusOK)

	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		Peers: map[uint64]string{
			2: serverHostPort(srv),
			3: unreachableAddr,
		},
		HealthCheckTimeout: 30 * time.Second,
		ClusterCA:          caPoolFromServer(srv),
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	ctx := context.Background()
	reachability := pd.checkPeerReachability(ctx)

	assert.NotNil(t, reachability)
	assert.Len(t, reachability, 2)
	assert.True(t, reachability[2], "node 2 backed by a healthy server must be reachable")
	assert.False(t, reachability[3], "node 3 at unreachable address must not be reachable")
}

// TestPartitionDetector_PingPeer tests that pingPeer treats a 2xx response
// and an HTTP 503 (sealed/not-ready qrdb node, process alive) as reachable,
// and a refused connection or any other status as unreachable. The RTT is
// positive even for failed probes.
func TestPartitionDetector_PingPeer(t *testing.T) {
	tests := []struct {
		name   string
		status int // 0 means no server: the address refuses connections
		want   bool
	}{
		{"reachable", http.StatusOK, true},
		{"sealed", http.StatusServiceUnavailable, true},
		{"unreachable", 0, false},
		{"unhealthy status", http.StatusInternalServerError, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			mockRaft.setLeader(1, 1)

			config := &PartitionConfig{
				Peers:              map[uint64]string{2: unreachableAddr},
				HealthCheckTimeout: 500 * time.Millisecond,
			}
			if tt.status != 0 {
				srv := newHealthServer(t, tt.status)
				config.Peers[2] = serverHostPort(srv)
				config.HealthCheckTimeout = 2 * time.Second
				config.ClusterCA = caPoolFromServer(srv)
			}

			pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			reachable, rtt := pd.pingPeer(context.Background(), pd.peers[2])

			assert.Equal(t, tt.want, reachable)
			assert.Greater(t, rtt, time.Duration(0), "RTT must be positive")
		})
	}
}

// TestPartitionDetector_IsPartitioned tests partition detection logic.
func TestPartitionDetector_IsPartitioned(t *testing.T) {
	tests := []struct {
		name              string
		reachability      map[uint64]bool
		expectPartitioned bool
	}{
		{
			name: "all reachable",
			reachability: map[uint64]bool{
				2: true,
				3: true,
			},
			expectPartitioned: false,
		},
		{
			name: "one unreachable",
			reachability: map[uint64]bool{
				2: true,
				3: false,
			},
			expectPartitioned: true,
		},
		{
			name: "all unreachable",
			reachability: map[uint64]bool{
				2: false,
				3: false,
			},
			expectPartitioned: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			mockRaft.setLeader(1, 1)

			config := &PartitionConfig{
				Peers: map[uint64]string{
					2: "node2:8001",
					3: "node3:8001",
				},
			}

			pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			partitioned := pd.isPartitioned(tt.reachability)
			assert.Equal(t, tt.expectPartitioned, partitioned)
		})
	}
}

// TestPartitionDetector_FindPartitions tests that the majority partition holds
// the reachable peers and the minority partition the unreachable ones.
func TestPartitionDetector_FindPartitions(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		Peers: map[uint64]string{
			2: "node2:8001",
			3: "node3:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	reachability := map[uint64]bool{
		2: true,
		3: false,
	}

	assert.ElementsMatch(t, []uint64{2}, pd.findMajorityPartition(reachability))
	assert.ElementsMatch(t, []uint64{3}, pd.findMinorityPartition(reachability))
}

// TestPartitionDetector_IsInMinority tests minority detection.
func TestPartitionDetector_IsInMinority(t *testing.T) {
	tests := []struct {
		name           string
		majorityCount  int
		totalPeers     int
		expectMinority bool
	}{
		{
			name:           "in majority (2/3)",
			majorityCount:  2,
			totalPeers:     3,
			expectMinority: false,
		},
		{
			name:           "in minority (0/3)",
			majorityCount:  0,
			totalPeers:     3,
			expectMinority: true,
		},
		{
			name:           "exactly quorum (1/3)",
			majorityCount:  1,
			totalPeers:     3,
			expectMinority: false,
		},
		{
			name:           "5 node cluster - in majority (3/5)",
			majorityCount:  3,
			totalPeers:     5,
			expectMinority: false,
		},
		{
			name:           "5 node cluster - in minority (1/5)",
			majorityCount:  1,
			totalPeers:     5,
			expectMinority: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRaft := newMockRaftNode()
			mockRaft.setLeader(1, 1)

			// Build peers map
			peers := make(map[uint64]string)
			for i := 2; i <= tt.totalPeers; i++ {
				peers[uint64(i)] = fmt.Sprintf("node%d:8001", i)
			}

			config := &PartitionConfig{
				Peers: peers,
			}

			pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
			require.NoError(t, err)

			// Build majority list
			majority := make([]uint64, 0)
			for i := 2; i < 2+tt.majorityCount; i++ {
				majority = append(majority, uint64(i))
			}

			partition := &Partition{
				Majority: majority,
			}

			inMinority := pd.isInMinority(partition)
			assert.Equal(t, tt.expectMinority, inMinority)
		})
	}
}

// TestPartitionDetector_RecoverFromPartition tests partition recovery.
func TestPartitionDetector_RecoverFromPartition(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		AutoRecovery: true,
		Peers: map[uint64]string{
			2: "node2:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	// Enter read-only mode first
	err = pd.enterReadOnlyMode()
	require.NoError(t, err)
	assert.True(t, pd.IsReadOnlyMode())

	// Recover
	ctx := context.Background()
	err = pd.recoverFromPartition(ctx)
	require.NoError(t, err)
	assert.False(t, pd.IsReadOnlyMode())
}

// TestPartitionDetector_ConcurrentAccess tests thread safety.
func TestPartitionDetector_ConcurrentAccess(t *testing.T) {
	mockRaft := newMockRaftNode()
	mockRaft.setLeader(1, 1)

	config := &PartitionConfig{
		Peers: map[uint64]string{
			2: "node2:8001",
			3: "node3:8001",
		},
	}

	pd, err := NewPartitionDetector(mockRaft, config, newMockLogger())
	require.NoError(t, err)

	var wg sync.WaitGroup
	numGoroutines := 20

	ctx := context.Background()

	// Concurrent partition detection
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = pd.DetectPartition(ctx)
		}()
	}

	// Concurrent status checks
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pd.GetStatus()
		}()
	}

	// Concurrent read-only mode toggles
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pd.enterReadOnlyMode()
			_ = pd.exitReadOnlyMode()
		}()
	}

	wg.Wait()

	// No panic means thread safety is working
}

// ----------------------------------------------------------------------------
// healthURL unit tests
// ----------------------------------------------------------------------------

// TestHealthURL verifies the health probe URL built from a peer address: a
// bare host:port gets "https://", an explicit scheme is kept, and a trailing
// slash is trimmed before /health is appended.
func TestHealthURL(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{"bare host port", "192.0.2.10:8443", "https://192.0.2.10:8443/health"},
		{"already https", "https://node2.cluster.local:8443", "https://node2.cluster.local:8443/health"},
		{"already http", "http://127.0.0.1:8080", "http://127.0.0.1:8080/health"},
		{"trailing slash", "https://node3.cluster.local:8443/", "https://node3.cluster.local:8443/health"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, healthURL(tt.addr))
		})
	}
}
