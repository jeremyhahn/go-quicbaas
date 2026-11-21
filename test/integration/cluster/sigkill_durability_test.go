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

//go:build integration

package cluster

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

const (
	// clusterContainersEnv names the voting node containers, space-separated.
	// The Makefile's integration-test-cluster recipe exports it.
	clusterContainersEnv = "QUICBAAS_CLUSTER_CONTAINERS"

	sigkillClusterEntityType = "sigkill_cluster_durability"
	sigkillClusterWrites     = 200
	sigkillClusterVoters     = 3
	sigkillDockerTimeout     = 2 * time.Minute
	sigkillRecoveryTimeout   = 3 * time.Minute
)

// dialClusterNode opens a lazy gRPC connection to endpoint, so it can be
// created while the node is still down.
func dialClusterNode(t *testing.T, endpoint string) pb.QuicBaaSClient {
	t.Helper()
	conn, err := grpc.NewClient(endpoint,
		grpc.WithTransportCredentials(credentials.NewTLS(requireTLSConfig(t))),
		withTenantOperator(t),
	)
	require.NoError(t, err, "dial %s", endpoint)
	t.Cleanup(func() { _ = conn.Close() })
	return pb.NewQuicBaaSClient(conn)
}

// runDocker runs one docker CLI command, fails the test with its output when
// it does not succeed and returns its output otherwise.
func runDocker(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), sigkillDockerTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	require.NoError(t, err, "docker %s: %s", strings.Join(args, " "), out)
	return string(out)
}

// containerGRPCClients dials the gRPC port of each container. The compose
// file publishes container port 50051 on an ephemeral host port, which a
// docker start may change, so the port is read from docker each time.
func containerGRPCClients(t *testing.T, containers []string) []pb.QuicBaaSClient {
	t.Helper()
	clients := make([]pb.QuicBaaSClient, len(containers))
	for i, name := range containers {
		lines := strings.Fields(runDocker(t, "port", name, "50051/tcp"))
		require.NotEmpty(t, lines, "container %s publishes no gRPC port", name)
		// "0.0.0.0:32768" or "[::]:32768": the host port follows the last colon.
		port := lines[0][strings.LastIndex(lines[0], ":")+1:]
		require.NotEmpty(t, port, "container %s gRPC port mapping %q", name, lines[0])
		clients[i] = dialClusterNode(t, "localhost:"+port)
	}
	return clients
}

// TestSIGKILLAllVotersAcknowledgedWritesSurvive writes through the cluster,
// SIGKILLs all three voting node containers at once, starts them again and
// checks that every write the cluster acknowledged is readable on each node.
func TestSIGKILLAllVotersAcknowledgedWritesSurvive(t *testing.T) {
	containers := strings.Fields(os.Getenv(clusterContainersEnv))
	if len(containers) == 0 {
		t.Skip(clusterContainersEnv + " not set: the voting node containers to SIGKILL are unknown")
	}
	require.Len(t, containers, sigkillClusterVoters, "%s must name the %d voting node containers", clusterContainersEnv, sigkillClusterVoters)

	clients := containerGRPCClients(t, containers)

	// Writes go to the first container, which serves them through the Raft leader; a Put
	// that returns nil is a write the cluster acknowledged.
	pollUntil(t, clusterSettleTimeout, "cluster has a leader before the writes",
		func(ctx context.Context) (bool, error) { return grpcClusterHasLeader(clients[0])(ctx) == nil, nil })

	base := uint64(time.Now().UnixNano())
	acked := make([]*pb.PutRequest, 0, sigkillClusterWrites)
	for i := 0; i < sigkillClusterWrites; i++ {
		put := &pb.PutRequest{
			EntityType: sigkillClusterEntityType,
			Id:         base + uint64(i),
			Data:       []byte(fmt.Sprintf("sigkill-cluster-%03d", i)),
		}
		ctx, cancel := context.WithTimeout(t.Context(), pollRequestTimeout)
		_, err := clients[0].Put(ctx, put)
		cancel()
		require.NoError(t, err, "put %d", i)
		acked = append(acked, put)
	}
	t.Logf("cluster acknowledged %d writes; SIGKILL %v", len(acked), containers)

	runDocker(t, append([]string{"kill", "-s", "KILL"}, containers...)...)
	runDocker(t, append([]string{"start"}, containers...)...)
	clients = containerGRPCClients(t, containers)

	for n, client := range clients {
		pollUntil(t, sigkillRecoveryTimeout, fmt.Sprintf("node %d sees a leader after SIGKILL", n+1),
			func(ctx context.Context) (bool, error) { return grpcClusterHasLeader(client)(ctx) == nil, nil })
	}

	for n, client := range clients {
		for _, put := range acked {
			pollUntil(t, clusterSettleTimeout, fmt.Sprintf("node %d reads acknowledged entity %d", n+1, put.Id),
				func(ctx context.Context) (bool, error) {
					err := entityReadable(client, put)(ctx)
					return err == nil, err
				})
		}
	}
}
