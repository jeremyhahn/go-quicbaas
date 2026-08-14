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

//go:build docker_host

// These tests require Docker CLI access from the HOST machine.
// They should NOT run inside the Docker integration test container.
// Run with: go test -tags docker_host ./test/integration/helpers/...

package helpers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireDockerAvailable fails the test if Docker is not available.
// These tests require Docker CLI access (docker and docker-compose commands).
// When running with the docker_host build tag, Docker must be accessible.
func requireDockerAvailable(t *testing.T) {
	t.Helper()
	require.True(t, IsDockerComposeAvailable(),
		"docker-compose must be available when running with docker_host build tag")
}

// TestDockerManagerGetContainerName tests container name generation.
func TestDockerManagerGetContainerName(t *testing.T) {
	dm := NewDockerManager()

	tests := []struct {
		name        string
		serviceName string
		expected    string
	}{
		{
			name:        "node1",
			serviceName: "test-node1",
			expected:    "integration-test-node1-1",
		},
		{
			name:        "node4",
			serviceName: "test-node4",
			expected:    "integration-test-node4-1",
		},
		{
			name:        "floci",
			serviceName: "floci",
			expected:    "integration-floci-1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := dm.GetContainerName(tc.serviceName)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestDockerManagerStopAndStart tests stopping and starting a container.
func TestDockerManagerStopAndStart(t *testing.T) {
	requireDockerAvailable(t)

	dm := NewDockerManager()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Stop a test node
	err := dm.StopContainer(ctx, "test-node1")
	require.NoError(t, err, "should be able to stop test-node1")

	// Give it a moment
	time.Sleep(2 * time.Second)

	// Start it back
	err = dm.StartContainer(ctx, "test-node1")
	require.NoError(t, err, "should be able to start test-node1")

	// Wait for it to be healthy
	err = dm.WaitForHealthy(ctx, "test-node1", 60*time.Second)
	require.NoError(t, err, "test-node1 should become healthy after restart")
}

// TestDockerManagerIsContainerRunning tests container status check.
func TestDockerManagerIsContainerRunning(t *testing.T) {
	requireDockerAvailable(t)

	dm := NewDockerManager()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	running, err := dm.IsContainerRunning(ctx, "test-node1")
	require.NoError(t, err, "should be able to check container status")
	assert.True(t, running, "test-node1 should be running")
}

// TestDockerManagerGetContainerIP tests getting container IP.
func TestDockerManagerGetContainerIP(t *testing.T) {
	requireDockerAvailable(t)

	dm := NewDockerManager()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ip, err := dm.GetContainerIP(ctx, "test-node1")
	require.NoError(t, err, "should be able to get container IP")
	assert.NotEmpty(t, ip, "container IP should not be empty")
	t.Logf("test-node1 IP: %s", ip)
}
