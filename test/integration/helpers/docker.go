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
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// DockerManager provides Docker container management for integration tests.
// This helper requires Docker CLI access (docker and docker-compose commands) and is
// intended to be used from a host environment or a container with Docker socket access.
//
// When running inside the containerized integration test environment (where
// QUICBAAS_TEST_MODE=integration), these methods will not work as Docker CLI
// is not available. Tests using DockerManager should skip when Docker is not available.
type DockerManager struct {
	projectName string
	composeFile string
	available   bool // whether Docker daemon is accessible
}

// errDockerNotAvailable is returned when Docker daemon is not accessible
var errDockerNotAvailable = fmt.Errorf("docker daemon is not available (running inside container without Docker socket access)")

// NewDockerManager creates a new DockerManager with default settings.
// It checks if Docker is available and sets the available flag accordingly.
func NewDockerManager() *DockerManager {
	dm := &DockerManager{
		projectName: "integration", // Docker Compose project name
		composeFile: "docker-compose.integration.yml",
		available:   IsDockerComposeAvailable(),
	}
	return dm
}

// IsAvailable returns true if the Docker daemon is accessible.
func (dm *DockerManager) IsAvailable() bool {
	return dm.available
}

// composeDir returns the directory containing the compose file.
func (dm *DockerManager) composeDir() string {
	return "/app/test/integration"
}

// WithProjectName sets a custom Docker Compose project name.
func (dm *DockerManager) WithProjectName(name string) *DockerManager {
	dm.projectName = name
	return dm
}

// WithComposeFile sets a custom Docker Compose file path.
func (dm *DockerManager) WithComposeFile(path string) *DockerManager {
	dm.composeFile = path
	return dm
}

// StartNode starts a stopped Docker container node.
// nodeName should be the service name (e.g., "test-node4")
func (dm *DockerManager) StartNode(ctx context.Context, t *testing.T, nodeName string) error {
	t.Helper()
	if !dm.available {
		return errDockerNotAvailable
	}
	t.Logf("Starting node: %s", nodeName)

	// Use docker-compose to start the service
	cmd := exec.CommandContext(ctx,
		"docker-compose",
		"-f", dm.composeFile,
		"-p", dm.projectName,
		"start",
		nodeName,
	)
	cmd.Dir = dm.composeDir()

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start node %s: %w (stderr: %s)", nodeName, err, stderr.String())
	}

	// Wait for container to be healthy
	if err := dm.waitForContainerHealthy(ctx, t, nodeName, 60*time.Second); err != nil {
		return fmt.Errorf("node %s did not become healthy: %w", nodeName, err)
	}

	t.Logf("Node %s started successfully", nodeName)
	return nil
}

// StopNode gracefully stops a running Docker container node.
func (dm *DockerManager) StopNode(ctx context.Context, t *testing.T, nodeName string) error {
	if !dm.available {
		return errDockerNotAvailable
	}
	t.Helper()
	t.Logf("Stopping node: %s", nodeName)

	cmd := exec.CommandContext(ctx,
		"docker-compose",
		"-f", dm.composeFile,
		"-p", dm.projectName,
		"stop",
		nodeName,
	)
	cmd.Dir = dm.composeDir()

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop node %s: %w (stderr: %s)", nodeName, err, stderr.String())
	}

	// Wait for container to stop
	if err := dm.waitForContainerStopped(ctx, t, nodeName, 30*time.Second); err != nil {
		t.Logf("Warning: node %s may not have stopped cleanly: %v", nodeName, err)
	}

	t.Logf("Node %s stopped successfully", nodeName)
	return nil
}

// KillNode forcefully kills a Docker container node (simulates crash).
func (dm *DockerManager) KillNode(ctx context.Context, t *testing.T, nodeName string) error {
	if !dm.available {
		return errDockerNotAvailable
	}
	t.Helper()
	t.Logf("Killing node: %s (simulating crash)", nodeName)

	cmd := exec.CommandContext(ctx,
		"docker-compose",
		"-f", dm.composeFile,
		"-p", dm.projectName,
		"kill",
		nodeName,
	)
	cmd.Dir = dm.composeDir()

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to kill node %s: %w (stderr: %s)", nodeName, err, stderr.String())
	}

	t.Logf("Node %s killed successfully", nodeName)
	return nil
}

// RestartNode restarts a Docker container node.
func (dm *DockerManager) RestartNode(ctx context.Context, t *testing.T, nodeName string) error {
	if !dm.available {
		return errDockerNotAvailable
	}
	t.Helper()
	t.Logf("Restarting node: %s", nodeName)

	cmd := exec.CommandContext(ctx,
		"docker-compose",
		"-f", dm.composeFile,
		"-p", dm.projectName,
		"restart",
		nodeName,
	)
	cmd.Dir = dm.composeDir()

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to restart node %s: %w (stderr: %s)", nodeName, err, stderr.String())
	}

	// Wait for container to be healthy
	if err := dm.waitForContainerHealthy(ctx, t, nodeName, 60*time.Second); err != nil {
		return fmt.Errorf("node %s did not become healthy after restart: %w", nodeName, err)
	}

	t.Logf("Node %s restarted successfully", nodeName)
	return nil
}

// GetNodeStatus returns the current status of a Docker container node.
func (dm *DockerManager) GetNodeStatus(ctx context.Context, t *testing.T, nodeName string) (string, error) {
	if !dm.available {
		return "", errDockerNotAvailable
	}
	t.Helper()

	// Get container name from service name
	containerName := dm.getContainerName(nodeName)

	cmd := exec.CommandContext(ctx,
		"docker",
		"inspect",
		"-f", "{{.State.Status}}",
		containerName,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to get status for node %s: %w (stderr: %s)", nodeName, err, stderr.String())
	}

	status := strings.TrimSpace(stdout.String())
	t.Logf("Node %s status: %s", nodeName, status)
	return status, nil
}

// UpNode starts a node using docker-compose up (creates and starts).
// Use this for nodes that don't exist yet or are in profiles.
func (dm *DockerManager) UpNode(ctx context.Context, t *testing.T, nodeName string) error {
	if !dm.available {
		return errDockerNotAvailable
	}
	t.Helper()
	t.Logf("Starting node with docker-compose up: %s", nodeName)

	cmd := exec.CommandContext(ctx,
		"docker-compose",
		"-f", dm.composeFile,
		"-p", dm.projectName,
		"up",
		"-d",
		nodeName,
	)
	cmd.Dir = dm.composeDir()

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to up node %s: %w (stderr: %s)", nodeName, err, stderr.String())
	}

	// Wait for container to be healthy
	if err := dm.waitForContainerHealthy(ctx, t, nodeName, 60*time.Second); err != nil {
		return fmt.Errorf("node %s did not become healthy: %w", nodeName, err)
	}

	t.Logf("Node %s is up and healthy", nodeName)
	return nil
}

// DownNode removes a node container.
func (dm *DockerManager) DownNode(ctx context.Context, t *testing.T, nodeName string) error {
	if !dm.available {
		return errDockerNotAvailable
	}
	t.Helper()
	t.Logf("Removing node: %s", nodeName)

	cmd := exec.CommandContext(ctx,
		"docker-compose",
		"-f", dm.composeFile,
		"-p", dm.projectName,
		"rm",
		"-f",
		"-s",
		"-v",
		nodeName,
	)
	cmd.Dir = dm.composeDir()

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to remove node %s: %w (stderr: %s)", nodeName, err, stderr.String())
	}

	t.Logf("Node %s removed successfully", nodeName)
	return nil
}

// waitForContainerHealthy waits for a container to report healthy status.
func (dm *DockerManager) waitForContainerHealthy(ctx context.Context, t *testing.T, nodeName string, timeout time.Duration) error {
	if !dm.available {
		return errDockerNotAvailable
	}
	t.Helper()
	containerName := dm.getContainerName(nodeName)
	deadline := time.Now().Add(timeout)

	t.Logf("Waiting for container %s to become healthy (timeout: %v)...", containerName, timeout)

	for time.Now().Before(deadline) {
		// Check if container is running
		cmd := exec.CommandContext(ctx,
			"docker",
			"inspect",
			"-f", "{{.State.Status}}",
			containerName,
		)

		var stdout bytes.Buffer
		cmd.Stdout = &stdout

		if err := cmd.Run(); err != nil {
			// Container doesn't exist yet, wait
			time.Sleep(2 * time.Second)
			continue
		}

		status := strings.TrimSpace(stdout.String())
		if status != "running" {
			time.Sleep(2 * time.Second)
			continue
		}

		// Check health status
		cmd = exec.CommandContext(ctx,
			"docker",
			"inspect",
			"-f", "{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}",
			containerName,
		)

		stdout.Reset()
		cmd.Stdout = &stdout

		if err := cmd.Run(); err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		health := strings.TrimSpace(stdout.String())
		t.Logf("Container %s health: %s", containerName, health)

		if health == "healthy" {
			t.Logf("Container %s is healthy", containerName)
			return nil
		}

		// If container has no health check, just check if it's running
		if health == "none" && status == "running" {
			t.Logf("Container %s is running (no health check defined)", containerName)
			// Give it a few seconds to start up
			time.Sleep(5 * time.Second)
			return nil
		}

		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("container %s did not become healthy within %v", containerName, timeout)
}

// waitForContainerStopped waits for a container to stop.
func (dm *DockerManager) waitForContainerStopped(ctx context.Context, t *testing.T, nodeName string, timeout time.Duration) error {
	t.Helper()
	containerName := dm.getContainerName(nodeName)
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		cmd := exec.CommandContext(ctx,
			"docker",
			"inspect",
			"-f", "{{.State.Status}}",
			containerName,
		)

		var stdout bytes.Buffer
		cmd.Stdout = &stdout

		if err := cmd.Run(); err != nil {
			// Container doesn't exist, consider it stopped
			return nil
		}

		status := strings.TrimSpace(stdout.String())
		if status == "exited" || status == "stopped" {
			return nil
		}

		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("container %s did not stop within %v", containerName, timeout)
}

// getContainerName returns the Docker container name for a service.
// Docker Compose uses the format: <project>-<service>-<instance>
func (dm *DockerManager) getContainerName(serviceName string) string {
	// Docker Compose v2 format: <project>-<service>-<instance>
	return fmt.Sprintf("%s-%s-1", dm.projectName, serviceName)
}

// GetContainerLogs retrieves logs from a container.
func (dm *DockerManager) GetContainerLogs(ctx context.Context, t *testing.T, nodeName string, tail int) (string, error) {
	if !dm.available {
		return "", errDockerNotAvailable
	}
	t.Helper()
	containerName := dm.getContainerName(nodeName)

	cmd := exec.CommandContext(ctx,
		"docker",
		"logs",
		"--tail", fmt.Sprintf("%d", tail),
		containerName,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to get logs for node %s: %w", nodeName, err)
	}

	// Combine stdout and stderr
	logs := stdout.String() + stderr.String()
	return logs, nil
}

// IsNodeRunning checks if a node is currently running.
func (dm *DockerManager) IsNodeRunning(ctx context.Context, t *testing.T, nodeName string) (bool, error) {
	if !dm.available {
		return false, errDockerNotAvailable
	}
	t.Helper()
	status, err := dm.GetNodeStatus(ctx, t, nodeName)
	if err != nil {
		// If we can't get status, assume not running
		return false, nil
	}

	return status == "running", nil
}

// IsDockerComposeAvailable checks if docker-compose is available and functional.
// Returns true if docker-compose can be executed AND Docker daemon is accessible
// AND we're not running inside the integration test container.
// This prevents tests from hanging when trying to manage containers from within the test container.
func IsDockerComposeAvailable() bool {
	// If running inside integration test container, docker-compose operations
	// will cause conflicts as we're part of the same compose project.
	// The test container has QUICBAAS_TEST_MODE=integration set.
	if os.Getenv("QUICBAAS_TEST_MODE") == "integration" {
		return false
	}

	// Check if docker-compose binary exists
	_, err := exec.LookPath("docker-compose")
	if err != nil {
		return false
	}

	// Also verify Docker daemon is accessible by running a quick command with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "info")
	if err := cmd.Run(); err != nil {
		// Docker daemon is not accessible
		return false
	}

	return true
}

// RequireDockerCompose fails the test if docker-compose is not available.
// This is used for tests that require Docker container management.
func RequireDockerCompose(t *testing.T) {
	t.Helper()
	if !IsDockerComposeAvailable() {
		t.Fatal("docker-compose must be available in PATH for dynamic container management tests")
	}
}
