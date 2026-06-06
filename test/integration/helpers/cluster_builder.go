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

//go:build integration

package helpers

import (
	"context"
	"fmt"
	"strconv"
	"time"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// ClusterBuilder provides a fluent API for building test cluster configurations.
type ClusterBuilder struct {
	endpoints      []string
	timeout        time.Duration
	retries        int
	retryBackoff   time.Duration
	healthTimeout  time.Duration
	healthInterval time.Duration
}

// NewClusterBuilder creates a new ClusterBuilder with default values.
func NewClusterBuilder() *ClusterBuilder {
	return &ClusterBuilder{
		endpoints:      GetClusterEndpoints(),
		timeout:        30 * time.Second,
		retries:        3,
		retryBackoff:   100 * time.Millisecond,
		healthTimeout:  180 * time.Second,
		healthInterval: 2 * time.Second,
	}
}

// WithEndpoints sets custom cluster endpoints.
func (cb *ClusterBuilder) WithEndpoints(endpoints []string) *ClusterBuilder {
	cb.endpoints = endpoints
	return cb
}

// WithTimeout sets the operation timeout.
func (cb *ClusterBuilder) WithTimeout(timeout time.Duration) *ClusterBuilder {
	cb.timeout = timeout
	return cb
}

// WithRetries sets the number of retry attempts.
func (cb *ClusterBuilder) WithRetries(retries int) *ClusterBuilder {
	cb.retries = retries
	return cb
}

// WithHealthCheck sets health check parameters.
func (cb *ClusterBuilder) WithHealthCheck(timeout, interval time.Duration) *ClusterBuilder {
	cb.healthTimeout = timeout
	cb.healthInterval = interval
	return cb
}

// Build creates a TestCluster with the configured settings.
func (cb *ClusterBuilder) Build(ctx context.Context) (*TestCluster, error) {
	cluster := &TestCluster{
		Endpoints:      cb.endpoints,
		Nodes:          make([]*TestNode, len(cb.endpoints)),
		Clients:        make([]quicbaas.Client, len(cb.endpoints)),
		Timeout:        cb.timeout,
		healthTimeout:  cb.healthTimeout,
		healthInterval: cb.healthInterval,
	}

	// Connect to all nodes
	for i, endpoint := range cb.endpoints {
		client, err := connectToNode(ctx, endpoint, cb.timeout, cb.retries, cb.retryBackoff)
		if err != nil {
			// Clean up any successful connections
			cluster.Close()
			return nil, fmt.Errorf("failed to connect to node %d (%s): %w", i+1, endpoint, err)
		}

		cluster.Clients[i] = client
		cluster.Nodes[i] = &TestNode{
			ID:          uint64(i + 1),
			GRPCAddress: endpoint,
		}
	}

	return cluster, nil
}

// TestCluster represents a cluster of QuicBaaS nodes for testing.
type TestCluster struct {
	Endpoints      []string
	Nodes          []*TestNode
	Clients        []quicbaas.Client
	Timeout        time.Duration
	healthTimeout  time.Duration
	healthInterval time.Duration
}

// TestNode represents a single node in a test cluster.
type TestNode struct {
	ID          uint64
	RaftAddress string
	GRPCAddress string
	HTTPAddress string
	IsLeader    bool
	IsHealthy   bool
	ShardLeader map[uint64]bool // ShardID -> is leader for this shard
}

// Close closes all connections in the cluster.
func (tc *TestCluster) Close() error {
	var firstErr error

	// Close all clients
	for i, client := range tc.Clients {
		if client != nil {
			if err := client.Close(); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("failed to close client %d: %w", i, err)
			}
		}
	}

	return firstErr
}

// FindLeader returns the leader node and client.
// It queries Members from cluster status to find which node is the leader,
// then returns that node and its client.
func (tc *TestCluster) FindLeader(ctx context.Context) (*TestNode, quicbaas.Client, error) {
	for i, client := range tc.Clients {
		status, err := client.Cluster().Status(ctx)
		if err != nil {
			continue
		}

		// Check Members for leader information
		for _, member := range status.Members {
			if member.IsLeader {
				// Parse member ID to uint64
				memberID, err := strconv.ParseUint(member.ID, 10, 64)
				if err != nil {
					continue
				}

				// Find the matching node and client
				for j, n := range tc.Nodes {
					if n.ID == memberID {
						tc.Nodes[j].IsLeader = true
						return tc.Nodes[j], tc.Clients[j], nil
					}
				}
			}
		}

		// Fallback: check if LeaderID is set directly on the response
		if status.LeaderID != "" {
			leaderID, err := strconv.ParseUint(status.LeaderID, 10, 64)
			if err == nil {
				for j, n := range tc.Nodes {
					if n.ID == leaderID {
						tc.Nodes[j].IsLeader = true
						return tc.Nodes[j], tc.Clients[j], nil
					}
				}
			}
		}

		// If we got a valid status but no leader info, continue to next node
		if len(status.Members) == 0 {
			_ = i // avoid unused variable
			continue
		}
	}

	return nil, nil, fmt.Errorf("no leader found in cluster")
}

// connectToNode establishes a connection to a node using the SDK.
func connectToNode(ctx context.Context, endpoint string, timeout time.Duration, retries int, retryBackoff time.Duration) (quicbaas.Client, error) {
	var client quicbaas.Client
	var err error

	tlsCfg, err := IntegrationTLSConfig()
	if err != nil {
		return nil, err
	}

	// Retry connection with exponential backoff
	backoff := retryBackoff
	for attempt := 0; attempt <= retries; attempt++ {
		client, err = quicbaas.NewClient(
			endpoint,
			quicbaas.WithTimeout(timeout),
			quicbaas.WithTLSConfig(tlsCfg),
		)
		if err == nil {
			return client, nil
		}

		if attempt < retries {
			time.Sleep(backoff)
			backoff *= 2
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
		}
	}

	return nil, fmt.Errorf("failed to connect after %d attempts: %w", retries+1, err)
}
