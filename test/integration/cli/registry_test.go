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

// Package cli provides E2E integration tests for the `quicbaas registry`
// command tree — the service registry — on every transport against a live
// server.
package cli

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registryLease is the JSON a register or renew command prints.
type registryLease struct {
	LeaseID    int64 `json:"lease_id,string"`
	TTLSeconds int64 `json:"ttl_seconds"`
}

// registryResolution is the JSON a resolve command prints.
type registryResolution struct {
	Instances []struct {
		ID       string            `json:"id"`
		Address  string            `json:"address"`
		Metadata map[string]string `json:"metadata"`
	} `json:"instances"`
	Revision int64 `json:"revision"`
}

// registryWatchEvent is one JSON line a watch command prints.
type registryWatchEvent struct {
	Type     string `json:"type"`
	Revision int64  `json:"revision"`
	Instance struct {
		ID      string `json:"id"`
		Address string `json:"address"`
	} `json:"instance"`
}

// registryServiceName is unique per transport and run, so transports and
// reruns never observe one another's registrations.
func registryServiceName(transport Transport) string {
	return "itest-" + strings.ToLower(string(transport)) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func registryResolve(t *testing.T, runner *TestRunner, service string) registryResolution {
	t.Helper()
	stdout, stderr, err := runner.Run(CommandDef{Name: "registry resolve", Args: []string{"registry", "resolve", service, "--output", "json"}})
	require.NoError(t, err, "[%s] resolve: stdout=%s stderr=%s", runner.transport, stdout, stderr)
	var res registryResolution
	require.NoError(t, json.Unmarshal([]byte(stdout), &res), "[%s] resolve output: %s", runner.transport, stdout)
	return res
}

func registryWatchEvents(t *testing.T, runner *TestRunner, service string, from int64, count int, timeout time.Duration) []registryWatchEvent {
	t.Helper()
	stdout, stderr, err := runner.Run(CommandDef{
		Name: "registry watch",
		Args: []string{"registry", "watch", service, "--from-revision", strconv.FormatInt(from, 10),
			"--max-events", strconv.Itoa(count), "--output", "json"},
		Timeout: timeout,
	})
	require.NoError(t, err, "[%s] watch: stdout=%s stderr=%s", runner.transport, stdout, stderr)
	var events []registryWatchEvent
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		var ev registryWatchEvent
		require.NoError(t, json.Unmarshal([]byte(line), &ev), "[%s] watch line: %s", runner.transport, line)
		events = append(events, ev)
	}
	require.Len(t, events, count, "[%s] watch output: %s", runner.transport, stdout)
	return events
}

// TestServiceRegistryLifecycle drives register, resolve, watch (replayed from
// a revision), renew and deregister through the CLI on every transport.
func TestServiceRegistryLifecycle(t *testing.T) {
	RunForAllTransports(t, "ServiceRegistryLifecycle", func(t *testing.T, runner *TestRunner) {
		service := registryServiceName(runner.transport)

		before := registryResolve(t, runner, service)
		assert.Empty(t, before.Instances, "[%s] a fresh service has no instances", runner.transport)

		stdout, stderr, err := runner.Run(CommandDef{Name: "registry register", Args: []string{
			"registry", "register", "--service", service, "--id", "node-a", "--address", "10.9.0.1:443",
			"--ttl", "30", "--meta", "zone=z1", "--output", "json"}})
		require.NoError(t, err, "[%s] register: stdout=%s stderr=%s", runner.transport, stdout, stderr)
		var lease registryLease
		require.NoError(t, json.Unmarshal([]byte(stdout), &lease), "[%s] register output: %s", runner.transport, stdout)
		require.Positive(t, lease.LeaseID, "[%s] register returned no lease", runner.transport)

		after := registryResolve(t, runner, service)
		require.Len(t, after.Instances, 1, "[%s] resolve after register", runner.transport)
		assert.Equal(t, "10.9.0.1:443", after.Instances[0].Address)
		assert.Equal(t, "z1", after.Instances[0].Metadata["zone"])
		assert.Greater(t, after.Revision, before.Revision, "[%s] the registration advanced the revision", runner.transport)

		// Watching from just after the empty snapshot replays the registration.
		replayed := registryWatchEvents(t, runner, service, before.Revision+1, 1, 20*time.Second)
		assert.Equal(t, "PUT", replayed[0].Type, "[%s]", runner.transport)
		assert.Equal(t, "node-a", replayed[0].Instance.ID, "[%s]", runner.transport)

		stdout, stderr, err = runner.Run(CommandDef{Name: "registry renew", Args: []string{
			"registry", "renew", strconv.FormatInt(lease.LeaseID, 10), "--output", "json"}})
		require.NoError(t, err, "[%s] renew: stdout=%s stderr=%s", runner.transport, stdout, stderr)
		var renewed registryLease
		require.NoError(t, json.Unmarshal([]byte(stdout), &renewed))
		assert.Equal(t, lease.LeaseID, renewed.LeaseID, "[%s]", runner.transport)

		stdout, stderr, err = runner.Run(CommandDef{Name: "registry deregister", Args: []string{
			"registry", "deregister", strconv.FormatInt(lease.LeaseID, 10)}})
		require.NoError(t, err, "[%s] deregister: stdout=%s stderr=%s", runner.transport, stdout, stderr)

		assert.Empty(t, registryResolve(t, runner, service).Instances, "[%s] deregistered instance still listed", runner.transport)
		_, _, err = runner.Run(CommandDef{Name: "registry renew (gone)", Args: []string{
			"registry", "renew", strconv.FormatInt(lease.LeaseID, 10)}})
		assert.Error(t, err, "[%s] a revoked lease must not renew", runner.transport)
	})
}

// TestServiceRegistryLeaseExpiry proves the property the registry exists for:
// an instance whose owner stops renewing disappears on its own, and a watcher
// is told.
func TestServiceRegistryLeaseExpiry(t *testing.T) {
	RunForAllTransports(t, "ServiceRegistryLeaseExpiry", func(t *testing.T, runner *TestRunner) {
		service := registryServiceName(runner.transport)
		before := registryResolve(t, runner, service)

		stdout, stderr, err := runner.Run(CommandDef{Name: "registry register (ttl 1)", Args: []string{
			"registry", "register", "--service", service, "--id", "short-lived", "--address", "10.9.0.2:443",
			"--ttl", "1", "--output", "json"}})
		require.NoError(t, err, "[%s] register: stdout=%s stderr=%s", runner.transport, stdout, stderr)

		events := registryWatchEvents(t, runner, service, before.Revision+1, 2, 45*time.Second)
		assert.Equal(t, "PUT", events[0].Type, "[%s]", runner.transport)
		assert.Equal(t, "DELETE", events[1].Type, "[%s] the lapsed lease removed the instance", runner.transport)
		assert.Equal(t, "short-lived", events[1].Instance.ID, "[%s]", runner.transport)
		assert.Greater(t, events[1].Revision, events[0].Revision, "[%s]", runner.transport)

		assert.Empty(t, registryResolve(t, runner, service).Instances, "[%s] the expired instance is still listed", runner.transport)
	})
}
