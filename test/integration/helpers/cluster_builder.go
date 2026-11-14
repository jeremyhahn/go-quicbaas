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
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
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
	tc := &TestCluster{
		Endpoints:      cb.endpoints,
		Nodes:          make([]*TestNode, len(cb.endpoints)),
		Clients:        make([]quicbaas.Client, len(cb.endpoints)),
		Timeout:        cb.timeout,
		healthTimeout:  cb.healthTimeout,
		healthInterval: cb.healthInterval,
	}

	// The cluster calls (Status needs admin:status) and the KV calls of the
	// suites run as the tenant operator, whose Operator role holds both.
	bearer, err := tenantOperatorAuth.bearer(ctx, GetRESTEndpoints()[0])
	if err != nil {
		return nil, fmt.Errorf("failed to get the tenant operator bearer for the cluster clients: %w", err)
	}

	// Connect to all nodes
	for i, endpoint := range cb.endpoints {
		client, err := connectToNode(ctx, endpoint, cb.timeout, cb.retries, cb.retryBackoff, quicbaas.WithAuthToken(bearer))
		if err != nil {
			// Clean up any successful connections
			tc.Close()
			return nil, fmt.Errorf("failed to connect to node %d (%s): %w", i+1, endpoint, err)
		}

		tc.Clients[i] = client
		tc.Nodes[i] = &TestNode{
			ID:          uint64(i + 1),
			GRPCAddress: endpoint,
		}
	}

	return tc, nil
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
	var lastErr error
	for i, client := range tc.Clients {
		status, err := client.Cluster().Status(ctx)
		if err != nil {
			// A refused status says nothing about the leader: the caller's
			// credential is wrong, and asking again will not change it.
			if sdkerrors.IsUnauthorized(err) || sdkerrors.IsForbidden(err) {
				return nil, nil, &ClusterStatusRefusedError{NodeID: tc.Nodes[i].ID, Endpoint: tc.Nodes[i].GRPCAddress, Cause: err}
			}
			lastErr = err
			continue
		}

		if node, client, ok := tc.leaderOf(status); ok {
			return node, client, nil
		}
	}

	return nil, nil, &NoLeaderError{LastErr: lastErr}
}

// leaderOf matches the leader a status names to a node of the cluster: the
// member flagged as leader first, else the status' LeaderID.
func (tc *TestCluster) leaderOf(status *cluster.ClusterStatus) (*TestNode, quicbaas.Client, bool) {
	ids := make([]string, 0, len(status.Members)+1)
	for _, member := range status.Members {
		if member.IsLeader {
			ids = append(ids, member.ID)
		}
	}
	if status.LeaderID != "" {
		ids = append(ids, status.LeaderID)
	}

	for _, id := range ids {
		leaderID, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			continue
		}
		for j, n := range tc.Nodes {
			if n.ID == leaderID {
				tc.Nodes[j].IsLeader = true
				return tc.Nodes[j], tc.Clients[j], true
			}
		}
	}
	return nil, nil, false
}

// ClusterStatusRefusedError reports that a node refused the cluster status
// call as unauthenticated or forbidden.
type ClusterStatusRefusedError struct {
	NodeID   uint64
	Endpoint string
	Cause    error
}

func (e *ClusterStatusRefusedError) Error() string {
	return fmt.Sprintf("node %d (%s) refused the cluster status call: %v", e.NodeID, e.Endpoint, e.Cause)
}

func (e *ClusterStatusRefusedError) Unwrap() error { return e.Cause }

// NoLeaderError reports that no node's status named a leader of the cluster.
// LastErr is the last status error seen, nil if every node answered.
type NoLeaderError struct {
	LastErr error
}

func (e *NoLeaderError) Error() string {
	if e.LastErr != nil {
		return fmt.Sprintf("no leader found in cluster (last status error: %v)", e.LastErr)
	}
	return "no leader found in cluster"
}

func (e *NoLeaderError) Unwrap() error { return e.LastErr }

// connectToNode establishes a connection to a node using the SDK. The client
// carries no credential of its own: a caller whose calls are gated passes
// quicbaas.WithAuthToken with one of this package's principals in opts.
func connectToNode(ctx context.Context, endpoint string, timeout time.Duration, retries int, retryBackoff time.Duration, opts ...quicbaas.Option) (quicbaas.Client, error) {
	var client quicbaas.Client
	var err error

	tlsCfg, err := IntegrationTLSConfig()
	if err != nil {
		return nil, err
	}

	clientOpts := append([]quicbaas.Option{
		quicbaas.WithTimeout(timeout),
		quicbaas.WithTLSConfig(tlsCfg),
	}, opts...)

	// Retry connection with exponential backoff
	backoff := retryBackoff
	for attempt := 0; attempt <= retries; attempt++ {
		client, err = quicbaas.NewClient(endpoint, clientOpts...)
		if err == nil {
			return client, nil
		}

		if attempt < retries {
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			backoff *= 2
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
		}
	}

	return nil, fmt.Errorf("failed to connect after %d attempts: %w", retries+1, err)
}
