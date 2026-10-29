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
	"crypto/tls"
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

	// restEndpoints holds each node's REST base URL, index-aligned with
	// endpoints; a login goes to the REST URL of the node being dialled.
	restEndpoints []string

	// tlsConfig supplies the TLS trust every client is dialled with.
	tlsConfig func() (*tls.Config, error)

	// login, when set, returns the bearer the node at a REST base URL
	// issues; every client then carries its own node's bearer. Nil leaves
	// clients unauthenticated.
	login nodeLogin
}

// nodeLogin logs in at the cluster node whose REST base URL is restURL and
// returns the bearer that node issued.
type nodeLogin func(ctx context.Context, restURL string) (string, error)

// ClusterRESTEndpointError reports that a node has no REST endpoint to log
// in at, so its client cannot be authenticated.
type ClusterRESTEndpointError struct {
	Node     int
	Endpoint string
}

func (e *ClusterRESTEndpointError) Error() string {
	return "cluster node " + strconv.Itoa(e.Node) + " (" + e.Endpoint + ") has no REST endpoint to log in at"
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
		restEndpoints:  GetRESTEndpoints(),
		tlsConfig:      IntegrationTLSConfig,
	}
}

// WithSystemAdmin makes every client log in as the system admin at its own
// node, which issues the bearer that node's authorization accepts. A login
// refused while the cluster is still electing is retried like a failed dial.
func (cb *ClusterBuilder) WithSystemAdmin() *ClusterBuilder {
	cb.login = pivLogin(pivCertPath(), pivKeyPath(), pivCAFile())
	return cb
}

// pivLogin returns a nodeLogin that logs in as the system admin with the PIV
// certificate and key at certPath and keyPath, trusting the CA at caPath.
func pivLogin(certPath, keyPath, caPath string) nodeLogin {
	return func(ctx context.Context, restURL string) (string, error) {
		bearer, _, err := authPIV(ctx, restURL, certPath, keyPath, caPath)
		if err != nil {
			return "", &PrincipalAuthError{Principal: systemAdminPrincipal, Step: "login", Cause: err}
		}
		return bearer, nil
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
	if cb.login != nil && len(cb.restEndpoints) < len(cb.endpoints) {
		node := len(cb.restEndpoints)
		return nil, &ClusterRESTEndpointError{Node: node + 1, Endpoint: cb.endpoints[node]}
	}

	for i, endpoint := range cb.endpoints {
		client, err := cb.buildNode(ctx, i)
		if err != nil {
			// Clean up any successful connections
			cluster.Close()
			return nil, err
		}

		cluster.Clients[i] = client
		cluster.Nodes[i] = &TestNode{
			ID:          uint64(i + 1),
			GRPCAddress: endpoint,
		}
	}

	return cluster, nil
}

// buildNode connects to the i-th configured endpoint, logging in at its REST
// URL when the builder logs in.
func (cb *ClusterBuilder) buildNode(ctx context.Context, i int) (quicbaas.Client, error) {
	endpoint := cb.endpoints[i]
	var restURL string
	if i < len(cb.restEndpoints) {
		restURL = cb.restEndpoints[i]
	} else if cb.login != nil {
		return nil, &ClusterRESTEndpointError{Node: i + 1, Endpoint: endpoint}
	}
	client, err := cb.connectToNode(ctx, endpoint, restURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to node %d (%s): %w", i+1, endpoint, err)
	}
	return client, nil
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

// connectToNode establishes a connection to a node using the SDK, retrying a
// failed dial or login with exponential backoff.
func (cb *ClusterBuilder) connectToNode(ctx context.Context, endpoint, restURL string) (quicbaas.Client, error) {
	var client quicbaas.Client
	var err error

	tlsCfg, err := cb.tlsConfig()
	if err != nil {
		return nil, err
	}

	// Retry connection with exponential backoff
	backoff := cb.retryBackoff
	for attempt := 0; attempt <= cb.retries; attempt++ {
		client, err = cb.dialNode(ctx, endpoint, restURL, tlsCfg)
		if err == nil {
			return client, nil
		}

		if attempt < cb.retries {
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

	return nil, fmt.Errorf("failed to connect after %d attempts: %w", cb.retries+1, err)
}

// dialNode creates one client for endpoint. When the builder logs in, it
// first logs in at the node's REST URL so the client carries the bearer that
// node issued.
func (cb *ClusterBuilder) dialNode(ctx context.Context, endpoint, restURL string, tlsCfg *tls.Config) (quicbaas.Client, error) {
	opts := []quicbaas.Option{
		quicbaas.WithTimeout(cb.timeout),
		quicbaas.WithTLSConfig(tlsCfg),
	}
	if cb.login != nil {
		bearer, err := cb.login(ctx, restURL)
		if err != nil {
			return nil, err
		}
		opts = append(opts, quicbaas.WithAuthToken(bearer))
	}
	return quicbaas.NewClient(endpoint, opts...)
}
