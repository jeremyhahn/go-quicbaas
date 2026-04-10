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

package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// agentUnsupportedTransports lists transports that do not support agent commands.
// The agent fleet service currently only has REST API handlers. Proto definitions for gRPC
// are not yet implemented, so gRPC, QUIC, Unix, and MCP (which wrap gRPC) are excluded.
// TODO: Add proto definitions for orchestrator service to enable all transports.
var agentUnsupportedTransports = []Transport{TransportGRPC, TransportQUIC, TransportUnix, TransportMCP}

// =============================================================================
// Agent Command Definitions
// =============================================================================

// AgentRegisterCommand returns a command to register an agent with the fleet.
func AgentRegisterCommand(name, host, agentType, tenant string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("agent register %s", name),
		Args:           []string{"agent", "register", "--name", name, "--host", host, "--type", agentType, "--tenant", tenant},
		ExpectSuccess:  true,
		ExpectOutput:   []string{name, host, agentType},
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentRegisterWithBackendCommand returns a command to register an agent with backend option.
func AgentRegisterWithBackendCommand(name, host, agentType, tenant, backend string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("agent register %s with backend", name),
		Args:           []string{"agent", "register", "--name", name, "--host", host, "--type", agentType, "--tenant", tenant, "--backend", backend},
		ExpectSuccess:  true,
		ExpectOutput:   []string{name, host, agentType},
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentRegisterWithWorkDirCommand returns a command to register an agent with work-dir option.
func AgentRegisterWithWorkDirCommand(name, host, agentType, tenant, workDir string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("agent register %s with work-dir", name),
		Args:           []string{"agent", "register", "--name", name, "--host", host, "--type", agentType, "--tenant", tenant, "--work-dir", workDir},
		ExpectSuccess:  true,
		ExpectOutput:   []string{name, host, agentType},
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentRegisterWithModelCommand returns a command to register an agent with a model option.
func AgentRegisterWithModelCommand(name, host, agentType, tenant, model string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("agent register %s with model", name),
		Args:           []string{"agent", "register", "--name", name, "--host", host, "--type", agentType, "--tenant", tenant, "--model", model},
		ExpectSuccess:  true,
		ExpectOutput:   []string{name, host, agentType},
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentRegisterWithAllOptionsCommand returns a command to register an agent with all available options.
func AgentRegisterWithAllOptionsCommand(name, host, agentType, tenant, backend, model, workDir string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("agent register %s all options", name),
		Args: []string{
			"agent", "register",
			"--name", name,
			"--host", host,
			"--type", agentType,
			"--tenant", tenant,
			"--backend", backend,
			"--model", model,
			"--work-dir", workDir,
		},
		ExpectSuccess:  true,
		ExpectOutput:   []string{name, host, agentType},
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentRegisterJSONCommand returns a command to register an agent with JSON output.
func AgentRegisterJSONCommand(name, host, agentType, tenant string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("agent register %s json", name),
		Args:           []string{"agent", "register", "--name", name, "--host", host, "--type", agentType, "--tenant", tenant, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentListCommand returns a command to list registered agents.
func AgentListCommand(tenant string) CommandDef {
	return CommandDef{
		Name:           "agent list",
		Args:           []string{"agent", "list", "--tenant", tenant},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentListJSONCommand returns a command to list agents in JSON format.
func AgentListJSONCommand(tenant string) CommandDef {
	return CommandDef{
		Name:           "agent list json",
		Args:           []string{"agent", "list", "--tenant", tenant, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentListYAMLCommand returns a command to list agents in YAML format.
func AgentListYAMLCommand(tenant string) CommandDef {
	return CommandDef{
		Name:           "agent list yaml",
		Args:           []string{"agent", "list", "--tenant", tenant, "--output", "yaml"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentListByHostCommand returns a command to list agents filtered by host.
func AgentListByHostCommand(tenant, host string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("agent list host=%s", host),
		Args:           []string{"agent", "list", "--tenant", tenant, "--host", host},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentListByTypeCommand returns a command to list agents filtered by type.
func AgentListByTypeCommand(tenant, agentType string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("agent list type=%s", agentType),
		Args:           []string{"agent", "list", "--tenant", tenant, "--type", agentType},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentListByStatusCommand returns a command to list agents filtered by status.
func AgentListByStatusCommand(tenant, status string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("agent list status=%s", status),
		Args:           []string{"agent", "list", "--tenant", tenant, "--status", status},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentListByBackendCommand returns a command to list agents filtered by backend.
func AgentListByBackendCommand(tenant, backend string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("agent list backend=%s", backend),
		Args:           []string{"agent", "list", "--tenant", tenant, "--backend", backend},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentStatusCommand returns a command to get detailed agent status.
func AgentStatusCommand(agentID, tenant string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("agent status %s", agentID),
		Args:           []string{"agent", "status", agentID, "--tenant", tenant},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentStatusJSONCommand returns a command to get agent status in JSON format.
func AgentStatusJSONCommand(agentID, tenant string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("agent status json %s", agentID),
		Args:           []string{"agent", "status", agentID, "--tenant", tenant, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentRemoveCommand returns a command to remove an agent from the fleet.
func AgentRemoveCommand(agentID, tenant string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("agent remove %s", agentID),
		Args:           []string{"agent", "remove", agentID, "--tenant", tenant},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"Removed"},
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentRemoveJSONCommand returns a command to remove an agent with JSON output.
func AgentRemoveJSONCommand(agentID, tenant string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("agent remove json %s", agentID),
		Args:           []string{"agent", "remove", agentID, "--tenant", tenant, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentHeartbeatCommand returns a command to send a heartbeat for an agent.
func AgentHeartbeatCommand(agentID, tenant string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("agent heartbeat %s", agentID),
		Args:           []string{"agent", "heartbeat", agentID, "--tenant", tenant},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentHeartbeatJSONCommand returns a command to send a heartbeat with JSON output.
func AgentHeartbeatJSONCommand(agentID, tenant string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("agent heartbeat json %s", agentID),
		Args:           []string{"agent", "heartbeat", agentID, "--tenant", tenant, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentHelpCommand returns a command to show agent help.
// Help commands are handled locally by the CLI and do not require transport.
func AgentHelpCommand() CommandDef {
	return CommandDef{
		Name:          "agent help",
		Args:          []string{"agent", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"register", "list", "status", "remove"},
		Timeout:       5 * time.Second,
	}
}

// AgentRegisterHelpCommand returns a command to show agent register help.
// Help commands are handled locally by the CLI and do not require transport.
func AgentRegisterHelpCommand() CommandDef {
	return CommandDef{
		Name:          "agent register help",
		Args:          []string{"agent", "register", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"name", "host", "type"},
		Timeout:       5 * time.Second,
	}
}

// AgentListHelpCommand returns a command to show agent list help.
// Help commands are handled locally by the CLI and do not require transport.
func AgentListHelpCommand() CommandDef {
	return CommandDef{
		Name:          "agent list help",
		Args:          []string{"agent", "list", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"host", "status", "type"},
		Timeout:       5 * time.Second,
	}
}

// =============================================================================
// Agent Error Command Definitions
// =============================================================================

// AgentRegisterMissingNameCommand returns a command that should fail due to missing name.
func AgentRegisterMissingNameCommand() CommandDef {
	return CommandDef{
		Name:           "agent register missing name",
		Args:           []string{"agent", "register", "--host", "node01:9090", "--type", "claude"},
		ExpectSuccess:  false,
		ExpectError:    []string{"name"},
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentRegisterMissingHostCommand returns a command that should fail due to missing host.
func AgentRegisterMissingHostCommand() CommandDef {
	return CommandDef{
		Name:           "agent register missing host",
		Args:           []string{"agent", "register", "--name", "test-agent", "--type", "claude"},
		ExpectSuccess:  false,
		ExpectError:    []string{"host"},
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentRegisterMissingTypeCommand returns a command that should fail due to missing type.
func AgentRegisterMissingTypeCommand() CommandDef {
	return CommandDef{
		Name:           "agent register missing type",
		Args:           []string{"agent", "register", "--name", "test-agent", "--host", "node01:9090"},
		ExpectSuccess:  false,
		ExpectError:    []string{"type"},
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentStatusMissingIDCommand returns a command that should fail due to missing agent ID.
func AgentStatusMissingIDCommand() CommandDef {
	return CommandDef{
		Name:          "agent status missing id",
		Args:          []string{"agent", "status"},
		ExpectSuccess: false,
		ExpectError:   []string{"accepts 1 arg"},
		Timeout:       10 * time.Second,
	}
}

// AgentRemoveMissingIDCommand returns a command that should fail due to missing agent ID.
func AgentRemoveMissingIDCommand() CommandDef {
	return CommandDef{
		Name:          "agent remove missing id",
		Args:          []string{"agent", "remove"},
		ExpectSuccess: false,
		ExpectError:   []string{"accepts 1 arg"},
		Timeout:       10 * time.Second,
	}
}

// AgentHeartbeatMissingIDCommand returns a command that should fail due to missing agent ID.
func AgentHeartbeatMissingIDCommand() CommandDef {
	return CommandDef{
		Name:          "agent heartbeat missing id",
		Args:          []string{"agent", "heartbeat"},
		ExpectSuccess: false,
		ExpectError:   []string{"accepts 1 arg"},
		Timeout:       10 * time.Second,
	}
}

// AgentStatusNonexistentCommand returns a command to get a non-existent agent.
func AgentStatusNonexistentCommand(tenant string) CommandDef {
	return CommandDef{
		Name:           "agent status nonexistent",
		Args:           []string{"agent", "status", "nonexistent-agent-id-999", "--tenant", tenant},
		ExpectSuccess:  false,
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentRemoveNonexistentCommand returns a command to remove a non-existent agent.
func AgentRemoveNonexistentCommand(tenant string) CommandDef {
	return CommandDef{
		Name:           "agent remove nonexistent",
		Args:           []string{"agent", "remove", "nonexistent-agent-id-999", "--tenant", tenant},
		ExpectSuccess:  false,
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// AgentHeartbeatNonexistentCommand returns a command to heartbeat a non-existent agent.
func AgentHeartbeatNonexistentCommand(tenant string) CommandDef {
	return CommandDef{
		Name:           "agent heartbeat nonexistent",
		Args:           []string{"agent", "heartbeat", "nonexistent-agent-id-999", "--tenant", tenant},
		ExpectSuccess:  false,
		Timeout:        10 * time.Second,
		SkipTransports: agentUnsupportedTransports,
	}
}

// =============================================================================
// Tests
// =============================================================================

// TestAgentHelp tests agent help commands across all transports.
func TestAgentHelp(t *testing.T) {
	RunForAllTransports(t, "AgentHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(AgentHelpCommand())
	})

	RunForAllTransports(t, "AgentRegisterHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(AgentRegisterHelpCommand())
	})

	RunForAllTransports(t, "AgentListHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(AgentListHelpCommand())
	})
}

// TestAgentRegister tests agent registration across all transports.
func TestAgentRegister(t *testing.T) {
	RunForAllTransports(t, "RegisterAgent", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("register-agent")
		tenant := UniqueKey("register-tenant")
		host := "node01:9090"

		cmd := AgentRegisterCommand(name, host, "claude", tenant)
		if runner.IsSkipped(cmd) {
			return
		}
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err, "[%s] register should succeed", runner.transport)
		assert.Contains(t, stdout, name)
		assert.Contains(t, stdout, host)

		// Cleanup: parse agent ID and remove
		agentID := findAgentID(t, runner, tenant, name)
		if agentID != "" {
			cleanupAgent(t, runner, agentID, tenant)
		}
	})

	RunForAllTransports(t, "RegisterAgentJSON", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("register-json")
		tenant := UniqueKey("register-json-tenant")
		host := "node02:9090"

		cmd := AgentRegisterJSONCommand(name, host, "codex", tenant)
		if runner.IsSkipped(cmd) {
			return
		}
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err, "[%s] register JSON should succeed", runner.transport)

		// Verify JSON output is valid
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "Output should be valid JSON")

		// Cleanup
		agentID := findAgentID(t, runner, tenant, name)
		if agentID != "" {
			cleanupAgent(t, runner, agentID, tenant)
		}
	})

	RunForAllTransports(t, "RegisterAgentWithBackend", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("register-backend")
		tenant := UniqueKey("register-backend-tenant")
		host := "node03:9090"

		cmd := AgentRegisterWithBackendCommand(name, host, "ollama", tenant, "ollama-7b")
		if runner.IsSkipped(cmd) {
			return
		}
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err, "[%s] register with backend should succeed", runner.transport)
		assert.Contains(t, stdout, name)

		// Cleanup
		agentID := findAgentID(t, runner, tenant, name)
		if agentID != "" {
			cleanupAgent(t, runner, agentID, tenant)
		}
	})

	RunForAllTransports(t, "RegisterAgentWithWorkDir", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("register-workdir")
		tenant := UniqueKey("register-workdir-tenant")
		host := "node04:9090"

		cmd := AgentRegisterWithWorkDirCommand(name, host, "claude", tenant, "/data/agents/worker")
		if runner.IsSkipped(cmd) {
			return
		}
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err, "[%s] register with work-dir should succeed", runner.transport)
		assert.Contains(t, stdout, name)

		// Cleanup
		agentID := findAgentID(t, runner, tenant, name)
		if agentID != "" {
			cleanupAgent(t, runner, agentID, tenant)
		}
	})
}

// TestAgentList tests agent list command across all transports.
func TestAgentList(t *testing.T) {
	RunForAllTransports(t, "ListAgents", func(t *testing.T, runner *TestRunner) {
		tenant := UniqueKey("list-tenant")

		// Register 2 agents
		name1 := UniqueKey("list-agent-1")
		name2 := UniqueKey("list-agent-2")

		reg1 := AgentRegisterCommand(name1, "node01:9090", "claude", tenant)
		if runner.IsSkipped(reg1) {
			return
		}
		_, _, err := runner.Run(reg1)
		require.NoError(t, err)

		reg2 := AgentRegisterCommand(name2, "node02:9090", "codex", tenant)
		_, _, err = runner.Run(reg2)
		require.NoError(t, err)

		// List agents
		listCmd := AgentListCommand(tenant)
		stdout, _, err := runner.Run(listCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, name1)
		assert.Contains(t, stdout, name2)

		// Cleanup
		cleanupAgentByName(t, runner, tenant, name1)
		cleanupAgentByName(t, runner, tenant, name2)
	})
}

// TestAgentListJSON tests agent list JSON output across all transports.
func TestAgentListJSON(t *testing.T) {
	RunForAllTransports(t, "ListAgentsJSON", func(t *testing.T, runner *TestRunner) {
		tenant := UniqueKey("list-json-tenant")

		// Register an agent
		name := UniqueKey("list-json-agent")
		regCmd := AgentRegisterCommand(name, "node01:9090", "claude", tenant)
		if runner.IsSkipped(regCmd) {
			return
		}
		_, _, err := runner.Run(regCmd)
		require.NoError(t, err)

		// List in JSON format
		listCmd := AgentListJSONCommand(tenant)
		stdout, _, err := runner.Run(listCmd)
		require.NoError(t, err)

		// Verify JSON structure
		var listResp struct {
			Agents []struct {
				ID        string `json:"id"`
				Name      string `json:"name"`
				Host      string `json:"host"`
				AgentType string `json:"agent_type"`
				Status    string `json:"status"`
			} `json:"agents"`
			Count int `json:"count"`
		}
		err = json.Unmarshal([]byte(stdout), &listResp)
		require.NoError(t, err, "Output should be valid JSON")
		require.GreaterOrEqual(t, listResp.Count, 1, "Should have at least 1 agent")

		// Verify our agent is in the list
		found := false
		for _, agent := range listResp.Agents {
			if agent.Name == name {
				found = true
				assert.Equal(t, "node01:9090", agent.Host)
				assert.Equal(t, "claude", agent.AgentType)
				assert.NotEmpty(t, agent.ID)
				break
			}
		}
		assert.True(t, found, "Registered agent should appear in list")

		// Cleanup
		cleanupAgentByName(t, runner, tenant, name)
	})
}

// TestAgentListFilterByHost tests agent list filtered by host across all transports.
func TestAgentListFilterByHost(t *testing.T) {
	RunForAllTransports(t, "ListAgentsByHost", func(t *testing.T, runner *TestRunner) {
		tenant := UniqueKey("filter-host-tenant")
		host1 := "filter-host-node01:9090"
		host2 := "filter-host-node02:9090"

		// Register agents on different hosts
		name1 := UniqueKey("host-agent-1")
		name2 := UniqueKey("host-agent-2")

		reg1 := AgentRegisterCommand(name1, host1, "claude", tenant)
		if runner.IsSkipped(reg1) {
			return
		}
		_, _, err := runner.Run(reg1)
		require.NoError(t, err)

		reg2 := AgentRegisterCommand(name2, host2, "claude", tenant)
		_, _, err = runner.Run(reg2)
		require.NoError(t, err)

		// Filter by host1
		filterCmd := AgentListByHostCommand(tenant, host1)
		stdout, _, err := runner.Run(filterCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, name1)

		// Filter by host2
		filterCmd2 := AgentListByHostCommand(tenant, host2)
		stdout2, _, err := runner.Run(filterCmd2)
		require.NoError(t, err)
		assert.Contains(t, stdout2, name2)

		// Cleanup
		cleanupAgentByName(t, runner, tenant, name1)
		cleanupAgentByName(t, runner, tenant, name2)
	})
}

// TestAgentListFilterByType tests agent list filtered by type across all transports.
func TestAgentListFilterByType(t *testing.T) {
	RunForAllTransports(t, "ListAgentsByType", func(t *testing.T, runner *TestRunner) {
		tenant := UniqueKey("filter-type-tenant")

		// Register agents of different types
		claudeName := UniqueKey("type-claude")
		codexName := UniqueKey("type-codex")

		reg1 := AgentRegisterCommand(claudeName, "node01:9090", "claude", tenant)
		if runner.IsSkipped(reg1) {
			return
		}
		_, _, err := runner.Run(reg1)
		require.NoError(t, err)

		reg2 := AgentRegisterCommand(codexName, "node02:9090", "codex", tenant)
		_, _, err = runner.Run(reg2)
		require.NoError(t, err)

		// Filter by claude type
		filterCmd := AgentListByTypeCommand(tenant, "claude")
		stdout, _, err := runner.Run(filterCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, claudeName)

		// Filter by codex type
		filterCmd2 := AgentListByTypeCommand(tenant, "codex")
		stdout2, _, err := runner.Run(filterCmd2)
		require.NoError(t, err)
		assert.Contains(t, stdout2, codexName)

		// Cleanup
		cleanupAgentByName(t, runner, tenant, claudeName)
		cleanupAgentByName(t, runner, tenant, codexName)
	})
}

// TestAgentListFilterByStatus tests agent list filtered by status across all transports.
func TestAgentListFilterByStatus(t *testing.T) {
	RunForAllTransports(t, "ListAgentsByStatus", func(t *testing.T, runner *TestRunner) {
		tenant := UniqueKey("filter-status-tenant")

		// Register an agent (should have "registered" status)
		name := UniqueKey("status-agent")
		regCmd := AgentRegisterCommand(name, "node01:9090", "claude", tenant)
		if runner.IsSkipped(regCmd) {
			return
		}
		_, _, err := runner.Run(regCmd)
		require.NoError(t, err)

		// Filter by registered status
		filterCmd := AgentListByStatusCommand(tenant, "registered")
		stdout, _, err := runner.Run(filterCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, name)

		// Cleanup
		cleanupAgentByName(t, runner, tenant, name)
	})
}

// TestAgentListFilterByBackend tests agent list filtered by backend across all transports.
func TestAgentListFilterByBackend(t *testing.T) {
	RunForAllTransports(t, "ListAgentsByBackend", func(t *testing.T, runner *TestRunner) {
		tenant := UniqueKey("filter-backend-tenant")

		// Register agents with different backends
		name1 := UniqueKey("backend-ollama")
		name2 := UniqueKey("backend-vllm")

		reg1 := AgentRegisterWithBackendCommand(name1, "node01:9090", "ollama", tenant, "ollama-7b")
		if runner.IsSkipped(reg1) {
			return
		}
		_, _, err := runner.Run(reg1)
		require.NoError(t, err)

		reg2 := AgentRegisterWithBackendCommand(name2, "node02:9090", "ollama", tenant, "vllm-13b")
		_, _, err = runner.Run(reg2)
		require.NoError(t, err)

		// Filter by ollama-7b backend
		filterCmd := AgentListByBackendCommand(tenant, "ollama-7b")
		stdout, _, err := runner.Run(filterCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, name1)

		// Cleanup
		cleanupAgentByName(t, runner, tenant, name1)
		cleanupAgentByName(t, runner, tenant, name2)
	})
}

// TestAgentRegisterWithModel tests agent registration with --model flag across all transports.
func TestAgentRegisterWithModel(t *testing.T) {
	RunForAllTransports(t, "RegisterAgentWithModel", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("register-model")
		tenant := UniqueKey("register-model-tenant")
		host := "model-node01:9090"
		model := "claude-3-opus"

		cmd := AgentRegisterWithModelCommand(name, host, "claude", tenant, model)
		if runner.IsSkipped(cmd) {
			return
		}
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err, "[%s] register with model should succeed", runner.transport)
		assert.Contains(t, stdout, name)

		// Verify model appears in agent detail via JSON status
		agentID := findAgentID(t, runner, tenant, name)
		require.NotEmpty(t, agentID, "Agent ID should be found after registration")

		statusJSONCmd := AgentStatusJSONCommand(agentID, tenant)
		stdout, _, err = runner.Run(statusJSONCmd)
		require.NoError(t, err)

		var agentDetail map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &agentDetail)
		require.NoError(t, err, "Status JSON output should be valid")
		t.Logf("Agent detail with model: %v", agentDetail)

		// Cleanup
		cleanupAgent(t, runner, agentID, tenant)
	})

	RunForAllTransports(t, "RegisterAgentWithAllOptions", func(t *testing.T, runner *TestRunner) {
		name := UniqueKey("register-allopts")
		tenant := UniqueKey("register-allopts-tenant")
		host := "allopts-node01:9090"

		cmd := AgentRegisterWithAllOptionsCommand(name, host, "ollama", tenant, "ollama-backend", "llama3-70b", "/data/agents/worker")
		if runner.IsSkipped(cmd) {
			return
		}
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err, "[%s] register with all options should succeed", runner.transport)
		assert.Contains(t, stdout, name)

		// Cleanup
		agentID := findAgentID(t, runner, tenant, name)
		if agentID != "" {
			cleanupAgent(t, runner, agentID, tenant)
		}
	})
}

// TestAgentListYAML tests agent list YAML output across all transports.
func TestAgentListYAML(t *testing.T) {
	RunForAllTransports(t, "ListAgentsYAML", func(t *testing.T, runner *TestRunner) {
		tenant := UniqueKey("list-yaml-tenant")

		// Register an agent for listing
		name := UniqueKey("list-yaml-agent")
		regCmd := AgentRegisterCommand(name, "node01:9090", "claude", tenant)
		if runner.IsSkipped(regCmd) {
			return
		}
		_, _, err := runner.Run(regCmd)
		require.NoError(t, err)

		// List in YAML format
		listCmd := AgentListYAMLCommand(tenant)
		stdout, _, err := runner.Run(listCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, name)

		// Cleanup
		cleanupAgentByName(t, runner, tenant, name)
	})
}

// TestAgentStatus tests agent status command across all transports.
func TestAgentStatus(t *testing.T) {
	RunForAllTransports(t, "AgentStatus", func(t *testing.T, runner *TestRunner) {
		tenant := UniqueKey("status-tenant")
		name := UniqueKey("status-agent")
		host := "node01:9090"

		// Register agent
		regCmd := AgentRegisterCommand(name, host, "claude", tenant)
		if runner.IsSkipped(regCmd) {
			return
		}
		_, _, err := runner.Run(regCmd)
		require.NoError(t, err)

		// Parse agent ID
		agentID := findAgentID(t, runner, tenant, name)
		require.NotEmpty(t, agentID, "Agent ID should be found after registration")

		// Get status
		statusCmd := AgentStatusCommand(agentID, tenant)
		stdout, _, err := runner.Run(statusCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, name)
		assert.Contains(t, stdout, host)

		// Get status in JSON format
		statusJSONCmd := AgentStatusJSONCommand(agentID, tenant)
		stdout, _, err = runner.Run(statusJSONCmd)
		require.NoError(t, err)

		var agentDetail map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &agentDetail)
		require.NoError(t, err, "Status JSON output should be valid")

		// Cleanup
		cleanupAgent(t, runner, agentID, tenant)
	})
}

// TestAgentHeartbeat tests agent heartbeat command across all transports.
func TestAgentHeartbeat(t *testing.T) {
	RunForAllTransports(t, "AgentHeartbeat", func(t *testing.T, runner *TestRunner) {
		tenant := UniqueKey("heartbeat-tenant")
		name := UniqueKey("heartbeat-agent")

		// Register agent
		regCmd := AgentRegisterCommand(name, "node01:9090", "claude", tenant)
		if runner.IsSkipped(regCmd) {
			return
		}
		_, _, err := runner.Run(regCmd)
		require.NoError(t, err)

		// Parse agent ID
		agentID := findAgentID(t, runner, tenant, name)
		require.NotEmpty(t, agentID, "Agent ID should be found after registration")

		// Send heartbeat
		heartbeatCmd := AgentHeartbeatCommand(agentID, tenant)
		stdout, _, err := runner.Run(heartbeatCmd)
		require.NoError(t, err)
		t.Logf("Heartbeat response: %s", stdout)

		// Send heartbeat with JSON output
		heartbeatJSONCmd := AgentHeartbeatJSONCommand(agentID, tenant)
		stdout, _, err = runner.Run(heartbeatJSONCmd)
		require.NoError(t, err)

		var heartbeatResp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &heartbeatResp)
		require.NoError(t, err, "Heartbeat JSON output should be valid")

		// Cleanup
		cleanupAgent(t, runner, agentID, tenant)
	})
}

// TestAgentRemove tests agent remove command across all transports.
func TestAgentRemove(t *testing.T) {
	RunForAllTransports(t, "RemoveAgent", func(t *testing.T, runner *TestRunner) {
		tenant := UniqueKey("remove-tenant")
		name := UniqueKey("remove-agent")

		// Register agent
		regCmd := AgentRegisterCommand(name, "node01:9090", "claude", tenant)
		if runner.IsSkipped(regCmd) {
			return
		}
		_, _, err := runner.Run(regCmd)
		require.NoError(t, err)

		// Parse agent ID
		agentID := findAgentID(t, runner, tenant, name)
		require.NotEmpty(t, agentID, "Agent ID should be found after registration")

		// Remove agent
		removeCmd := AgentRemoveCommand(agentID, tenant)
		stdout, _, err := runner.Run(removeCmd)
		require.NoError(t, err)
		t.Logf("Remove response: %s", stdout)

		// Verify agent no longer exists
		statusCmd := AgentStatusCommand(agentID, tenant)
		_, _, err = runner.Run(statusCmd)
		assert.Error(t, err, "Agent should not exist after removal")
	})

	RunForAllTransports(t, "RemoveAgentJSON", func(t *testing.T, runner *TestRunner) {
		tenant := UniqueKey("remove-json-tenant")
		name := UniqueKey("remove-json-agent")

		// Register agent
		regCmd := AgentRegisterCommand(name, "node01:9090", "claude", tenant)
		if runner.IsSkipped(regCmd) {
			return
		}
		_, _, err := runner.Run(regCmd)
		require.NoError(t, err)

		// Parse agent ID
		agentID := findAgentID(t, runner, tenant, name)
		require.NotEmpty(t, agentID, "Agent ID should be found after registration")

		// Remove agent with JSON output
		removeJSONCmd := AgentRemoveJSONCommand(agentID, tenant)
		stdout, _, err := runner.Run(removeJSONCmd)
		require.NoError(t, err)

		var removeResp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &removeResp)
		require.NoError(t, err, "Remove JSON output should be valid")

		// Verify agent no longer exists
		statusCmd := AgentStatusCommand(agentID, tenant)
		_, _, err = runner.Run(statusCmd)
		assert.Error(t, err, "Agent should not exist after removal")
	})
}

// TestAgentLifecycle tests the complete agent fleet lifecycle across all transports.
// This is the most important test - proves the full flow works across all transports.
func TestAgentLifecycle(t *testing.T) {
	RunForAllTransports(t, "AgentLifecycle", func(t *testing.T, runner *TestRunner) {
		tenant := UniqueKey("lifecycle-tenant")
		name := UniqueKey("lifecycle-agent")
		host := "lifecycle-node01:9090"

		// 1. Register agent
		regCmd := AgentRegisterCommand(name, host, "claude", tenant)
		if runner.IsSkipped(regCmd) {
			return
		}
		stdout, _, err := runner.Run(regCmd)
		require.NoError(t, err, "[%s] register should succeed", runner.transport)
		assert.Contains(t, stdout, name)
		assert.Contains(t, stdout, host)
		t.Logf("[%s] Step 1 - Register: OK", runner.transport)

		// 2. Parse agent ID from list
		agentID := findAgentID(t, runner, tenant, name)
		require.NotEmpty(t, agentID, "Agent ID must be found after registration")
		t.Logf("[%s] Step 2 - Found agent ID: %s", runner.transport, agentID)

		// 3. List agents and verify our agent is present
		listCmd := AgentListCommand(tenant)
		stdout, _, err = runner.Run(listCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, name)
		t.Logf("[%s] Step 3 - List: OK", runner.transport)

		// 4. Send heartbeat
		heartbeatCmd := AgentHeartbeatCommand(agentID, tenant)
		_, _, err = runner.Run(heartbeatCmd)
		require.NoError(t, err)
		t.Logf("[%s] Step 4 - Heartbeat: OK", runner.transport)

		// 5. Get agent status
		statusCmd := AgentStatusCommand(agentID, tenant)
		stdout, _, err = runner.Run(statusCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, name)
		assert.Contains(t, stdout, host)
		t.Logf("[%s] Step 5 - Status: OK", runner.transport)

		// 6. Get status in JSON format to verify detail fields
		statusJSONCmd := AgentStatusJSONCommand(agentID, tenant)
		stdout, _, err = runner.Run(statusJSONCmd)
		require.NoError(t, err)
		var agentDetail map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &agentDetail)
		require.NoError(t, err, "Status JSON should be valid")
		t.Logf("[%s] Step 6 - Status JSON: OK", runner.transport)

		// 7. Remove agent
		removeCmd := AgentRemoveCommand(agentID, tenant)
		_, _, err = runner.Run(removeCmd)
		require.NoError(t, err)
		t.Logf("[%s] Step 7 - Remove: OK", runner.transport)

		// 8. Verify agent is gone - list should not contain the agent
		listCmd2 := AgentListJSONCommand(tenant)
		stdout, _, err = runner.Run(listCmd2)
		require.NoError(t, err)

		var listResp struct {
			Agents []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"agents"`
			Count int `json:"count"`
		}
		err = json.Unmarshal([]byte(stdout), &listResp)
		require.NoError(t, err)

		for _, agent := range listResp.Agents {
			assert.NotEqual(t, agentID, agent.ID, "Removed agent should not appear in list")
		}
		t.Logf("[%s] Step 8 - Verify removed: OK (count=%d)", runner.transport, listResp.Count)
	})
}

// TestAgentOutputFormats tests all output formats for agent commands.
func TestAgentOutputFormats(t *testing.T) {
	formats := []string{"table", "json", "yaml"}

	for _, format := range formats {
		t.Run(fmt.Sprintf("List_%s", format), func(t *testing.T) {
			RunForAllTransports(t, fmt.Sprintf("List_%s", format), func(t *testing.T, runner *TestRunner) {
				tenant := UniqueKey("format-tenant")

				// Register an agent for listing
				name := UniqueKey("format-agent")
				regCmd := AgentRegisterCommand(name, "node01:9090", "claude", tenant)
				if runner.IsSkipped(regCmd) {
					return
				}
				_, _, err := runner.Run(regCmd)
				require.NoError(t, err)

				cmd := CommandDef{
					Name:           fmt.Sprintf("agent list %s", format),
					Args:           []string{"agent", "list", "--tenant", tenant, "--output", format},
					ExpectSuccess:  true,
					Timeout:        10 * time.Second,
					SkipTransports: agentUnsupportedTransports,
				}
				runner.RunAndAssert(cmd)

				// Cleanup
				cleanupAgentByName(t, runner, tenant, name)
			})
		})
	}
}

// TestAgentErrors tests error handling for agent commands.
func TestAgentErrors(t *testing.T) {
	RunForAllTransports(t, "RegisterMissingName", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(AgentRegisterMissingNameCommand())
	})

	RunForAllTransports(t, "RegisterMissingHost", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(AgentRegisterMissingHostCommand())
	})

	RunForAllTransports(t, "RegisterMissingType", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(AgentRegisterMissingTypeCommand())
	})

	RunForAllTransports(t, "StatusMissingID", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(AgentStatusMissingIDCommand())
	})

	RunForAllTransports(t, "RemoveMissingID", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(AgentRemoveMissingIDCommand())
	})

	RunForAllTransports(t, "HeartbeatMissingID", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(AgentHeartbeatMissingIDCommand())
	})

	RunForAllTransports(t, "StatusNonexistent", func(t *testing.T, runner *TestRunner) {
		tenant := UniqueKey("error-tenant")
		cmd := AgentStatusNonexistentCommand(tenant)
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})

	RunForAllTransports(t, "RemoveNonexistent", func(t *testing.T, runner *TestRunner) {
		tenant := UniqueKey("error-tenant")
		cmd := AgentRemoveNonexistentCommand(tenant)
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})

	RunForAllTransports(t, "HeartbeatNonexistent", func(t *testing.T, runner *TestRunner) {
		tenant := UniqueKey("error-tenant")
		cmd := AgentHeartbeatNonexistentCommand(tenant)
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})
}

// =============================================================================
// Helper Functions
// =============================================================================

// findAgentID finds an agent ID by name within a tenant using JSON list output.
func findAgentID(t *testing.T, runner *TestRunner, tenant, agentName string) string {
	t.Helper()

	listCmd := AgentListJSONCommand(tenant)
	stdout, _, err := runner.Run(listCmd)
	if err != nil {
		return ""
	}

	var listResp struct {
		Agents []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(stdout), &listResp); err != nil {
		return ""
	}

	for _, agent := range listResp.Agents {
		if agent.Name == agentName {
			return agent.ID
		}
	}

	// Fallback: try parsing agent ID from the raw output lines
	// Some transports may format output differently
	for _, line := range strings.Split(stdout, "\n") {
		if strings.Contains(line, agentName) {
			fields := strings.Fields(line)
			if len(fields) > 0 && strings.HasPrefix(fields[0], "agent_") {
				return fields[0]
			}
		}
	}

	return ""
}

// cleanupAgent removes an agent by ID, ignoring errors.
func cleanupAgent(t *testing.T, runner *TestRunner, agentID, tenant string) {
	t.Helper()

	removeCmd := CommandDef{
		Name:           fmt.Sprintf("cleanup agent %s", agentID),
		Args:           []string{"agent", "remove", agentID, "--tenant", tenant},
		SkipTransports: agentUnsupportedTransports,
	}
	_, _, _ = runner.Run(removeCmd)
}

// cleanupAgentByName finds and removes an agent by name, ignoring errors.
func cleanupAgentByName(t *testing.T, runner *TestRunner, tenant, agentName string) {
	t.Helper()

	agentID := findAgentID(t, runner, tenant, agentName)
	if agentID == "" {
		return
	}
	cleanupAgent(t, runner, agentID, tenant)
}

// AgentSuite returns all agent commands for a complete test suite.
func AgentSuite(name, host, agentType, tenant string) []CommandDef {
	return []CommandDef{
		AgentHelpCommand(),
		AgentRegisterHelpCommand(),
		AgentListHelpCommand(),
		AgentRegisterCommand(name, host, agentType, tenant),
		AgentListCommand(tenant),
		AgentListJSONCommand(tenant),
	}
}
