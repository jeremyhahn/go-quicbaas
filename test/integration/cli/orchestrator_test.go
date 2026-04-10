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

// Package cli provides E2E integration tests for QuicBaaS CLI orchestrator commands.
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

// orchestratorUnsupportedTransports lists transports that do not support orchestrator commands.
// The orchestrator service currently only has REST API handlers. Proto definitions for gRPC
// are not yet implemented, so gRPC, QUIC, Unix, and MCP (which wrap gRPC) are excluded.
// TODO: Add proto definitions for orchestrator service to enable all transports.
var orchestratorUnsupportedTransports = []Transport{TransportGRPC, TransportQUIC, TransportUnix, TransportMCP}

// =============================================================================
// Orchestrator Command Definitions
// =============================================================================

// OrchestratorHealthCommand returns a command to check orchestrator health.
func OrchestratorHealthCommand() CommandDef {
	return CommandDef{
		Name:           "orchestrator health",
		Args:           []string{"orchestrator", "health"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"Healthy"},
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorHealthJSONCommand returns a command to check orchestrator health in JSON format.
func OrchestratorHealthJSONCommand() CommandDef {
	return CommandDef{
		Name:           "orchestrator health json",
		Args:           []string{"orchestrator", "health", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorHealthYAMLCommand returns a command to check orchestrator health in YAML format.
func OrchestratorHealthYAMLCommand() CommandDef {
	return CommandDef{
		Name:           "orchestrator health yaml",
		Args:           []string{"orchestrator", "health", "--output", "yaml"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorBackendsCommand returns a command to list available agent backends.
func OrchestratorBackendsCommand() CommandDef {
	return CommandDef{
		Name:           "orchestrator backends",
		Args:           []string{"orchestrator", "backends"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorBackendsJSONCommand returns a command to list backends in JSON format.
func OrchestratorBackendsJSONCommand() CommandDef {
	return CommandDef{
		Name:           "orchestrator backends json",
		Args:           []string{"orchestrator", "backends", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorBackendsYAMLCommand returns a command to list backends in YAML format.
func OrchestratorBackendsYAMLCommand() CommandDef {
	return CommandDef{
		Name:           "orchestrator backends yaml",
		Args:           []string{"orchestrator", "backends", "--output", "yaml"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorSessionsCommand returns a command to list active agent sessions.
func OrchestratorSessionsCommand() CommandDef {
	return CommandDef{
		Name:           "orchestrator sessions",
		Args:           []string{"orchestrator", "sessions"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorSessionsJSONCommand returns a command to list sessions in JSON format.
func OrchestratorSessionsJSONCommand() CommandDef {
	return CommandDef{
		Name:           "orchestrator sessions json",
		Args:           []string{"orchestrator", "sessions", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorSessionsYAMLCommand returns a command to list sessions in YAML format.
func OrchestratorSessionsYAMLCommand() CommandDef {
	return CommandDef{
		Name:           "orchestrator sessions yaml",
		Args:           []string{"orchestrator", "sessions", "--output", "yaml"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorSpawnCommand returns a command to spawn a new agent session.
func OrchestratorSpawnCommand(backend string, opts ...string) CommandDef {
	args := []string{"orchestrator", "spawn", "--backend", backend}
	args = append(args, opts...)
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator spawn %s", backend),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorSpawnWithModelCommand returns a command to spawn a session with a specific model.
func OrchestratorSpawnWithModelCommand(backend, model string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator spawn %s model=%s", backend, model),
		Args:           []string{"orchestrator", "spawn", "--backend", backend, "--model", model},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorSpawnWithProjectCommand returns a command to spawn a session with a project ID.
func OrchestratorSpawnWithProjectCommand(backend, projectID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator spawn %s project=%s", backend, projectID),
		Args:           []string{"orchestrator", "spawn", "--backend", backend, "--project", projectID},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorSpawnWithSystemPromptCommand returns a command to spawn a session with a system prompt.
func OrchestratorSpawnWithSystemPromptCommand(backend, systemPrompt string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator spawn %s with system prompt", backend),
		Args:           []string{"orchestrator", "spawn", "--backend", backend, "--system-prompt", systemPrompt},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorSpawnMissingBackendCommand returns a command that should fail due to missing backend.
// Note: This tests CLI validation which happens before transport.
func OrchestratorSpawnMissingBackendCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator spawn missing backend",
		Args:          []string{"orchestrator", "spawn"},
		ExpectSuccess: false,
		ExpectError:   []string{"backend"},
		Timeout:       10 * time.Second,
	}
}

// OrchestratorStatusCommand returns a command to get agent session status.
func OrchestratorStatusCommand(sessionID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator status %s", sessionID),
		Args:           []string{"orchestrator", "status", sessionID},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorStatusJSONCommand returns a command to get session status in JSON format.
func OrchestratorStatusJSONCommand(sessionID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator status json %s", sessionID),
		Args:           []string{"orchestrator", "status", sessionID, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorCloseCommand returns a command to close an agent session.
func OrchestratorCloseCommand(sessionID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator close %s", sessionID),
		Args:           []string{"orchestrator", "close", sessionID},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorPromptCommand returns a command to send a prompt to an agent session.
func OrchestratorPromptCommand(sessionID, prompt string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator prompt %s", sessionID),
		Args:           []string{"orchestrator", "prompt", sessionID, prompt},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// =============================================================================
// Orchestrator Task Command Definitions
// =============================================================================

// OrchestratorTaskCreateCommand returns a command to create an orchestration task.
func OrchestratorTaskCreateCommand(subject string, opts ...string) CommandDef {
	args := []string{"orchestrator", "task", "create", "--subject", subject}
	args = append(args, opts...)
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator task create %s", subject),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorTaskCreateWithTeamCommand returns a command to create a task with a team.
func OrchestratorTaskCreateWithTeamCommand(subject, team string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("orchestrator task create %s team=%s", subject, team),
		Args: []string{
			"orchestrator", "task", "create",
			"--subject", subject,
			"--team", team,
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorTaskCreateWithDescriptionCommand returns a command to create a task with a description.
func OrchestratorTaskCreateWithDescriptionCommand(subject, description string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("orchestrator task create %s with description", subject),
		Args: []string{
			"orchestrator", "task", "create",
			"--subject", subject,
			"--description", description,
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorTaskCreateWithTenantCommand returns a command to create a task with a tenant.
func OrchestratorTaskCreateWithTenantCommand(subject, tenantID string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("orchestrator task create %s tenant=%s", subject, tenantID),
		Args: []string{
			"orchestrator", "task", "create",
			"--subject", subject,
			"--tenant", tenantID,
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorTaskCreateMissingSubjectCommand returns a command that should fail due to missing subject.
// Note: This tests CLI validation which happens before transport.
func OrchestratorTaskCreateMissingSubjectCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator task create missing subject",
		Args:          []string{"orchestrator", "task", "create"},
		ExpectSuccess: false,
		ExpectError:   []string{"subject"},
		Timeout:       10 * time.Second,
	}
}

// OrchestratorTaskGetCommand returns a command to get task details.
func OrchestratorTaskGetCommand(taskID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator task get %s", taskID),
		Args:           []string{"orchestrator", "task", "get", taskID},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorTaskGetJSONCommand returns a command to get task details in JSON format.
func OrchestratorTaskGetJSONCommand(taskID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator task get json %s", taskID),
		Args:           []string{"orchestrator", "task", "get", taskID, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorTaskGetYAMLCommand returns a command to get task details in YAML format.
func OrchestratorTaskGetYAMLCommand(taskID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator task get yaml %s", taskID),
		Args:           []string{"orchestrator", "task", "get", taskID, "--output", "yaml"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorTaskGetNonexistentCommand returns a command that should fail for a nonexistent task.
func OrchestratorTaskGetNonexistentCommand() CommandDef {
	return CommandDef{
		Name:           "orchestrator task get nonexistent",
		Args:           []string{"orchestrator", "task", "get", "nonexistent-task-id-999"},
		ExpectSuccess:  false,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorTaskListCommand returns a command to list orchestration tasks.
func OrchestratorTaskListCommand() CommandDef {
	return CommandDef{
		Name:           "orchestrator task list",
		Args:           []string{"orchestrator", "task", "list"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorTaskListJSONCommand returns a command to list tasks in JSON format.
func OrchestratorTaskListJSONCommand() CommandDef {
	return CommandDef{
		Name:           "orchestrator task list json",
		Args:           []string{"orchestrator", "task", "list", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorTaskListYAMLCommand returns a command to list tasks in YAML format.
func OrchestratorTaskListYAMLCommand() CommandDef {
	return CommandDef{
		Name:           "orchestrator task list yaml",
		Args:           []string{"orchestrator", "task", "list", "--output", "yaml"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorTaskListWithStatusCommand returns a command to list tasks filtered by status.
func OrchestratorTaskListWithStatusCommand(status string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator task list status=%s", status),
		Args:           []string{"orchestrator", "task", "list", "--status", status},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorTaskListWithTeamCommand returns a command to list tasks filtered by team.
func OrchestratorTaskListWithTeamCommand(team string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator task list team=%s", team),
		Args:           []string{"orchestrator", "task", "list", "--team", team},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorTaskListWithAssignedToCommand returns a command to list tasks filtered by assigned agent.
func OrchestratorTaskListWithAssignedToCommand(agentID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator task list assigned-to=%s", agentID),
		Args:           []string{"orchestrator", "task", "list", "--assigned-to", agentID},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorTaskListWithTenantCommand returns a command to list tasks for a specific tenant.
func OrchestratorTaskListWithTenantCommand(tenantID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator task list tenant=%s", tenantID),
		Args:           []string{"orchestrator", "task", "list", "--tenant", tenantID},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorTaskClaimCommand returns a command to claim a task for an agent.
func OrchestratorTaskClaimCommand(taskID, agentID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator task claim %s agent=%s", taskID, agentID),
		Args:           []string{"orchestrator", "task", "claim", taskID, "--agent", agentID},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorTaskClaimMissingAgentCommand returns a command that should fail due to missing agent.
// Note: This tests CLI validation which happens before transport.
func OrchestratorTaskClaimMissingAgentCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator task claim missing agent",
		Args:          []string{"orchestrator", "task", "claim", "some-task-id"},
		ExpectSuccess: false,
		ExpectError:   []string{"agent"},
		Timeout:       10 * time.Second,
	}
}

// OrchestratorTaskCompleteCommand returns a command to complete a task.
func OrchestratorTaskCompleteCommand(taskID string, opts ...string) CommandDef {
	args := []string{"orchestrator", "task", "complete", taskID}
	args = append(args, opts...)
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator task complete %s", taskID),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorTaskCompleteWithResultCommand returns a command to complete a task with a result.
func OrchestratorTaskCompleteWithResultCommand(taskID, result string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("orchestrator task complete %s with result", taskID),
		Args: []string{
			"orchestrator", "task", "complete", taskID,
			"--result", result,
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// =============================================================================
// Orchestrator Help Command Definitions
// =============================================================================

// OrchestratorHelpCommand returns a command to show orchestrator help.
// Note: Help commands are handled locally by the CLI and do not require transport.
func OrchestratorHelpCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator help",
		Args:          []string{"orchestrator", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"spawn", "prompt", "status", "close", "sessions", "backends", "health", "task"},
		Timeout:       10 * time.Second,
	}
}

// OrchestratorTaskHelpCommand returns a command to show orchestrator task help.
// Note: Help commands are handled locally by the CLI and do not require transport.
func OrchestratorTaskHelpCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator task help",
		Args:          []string{"orchestrator", "task", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"create", "get", "list", "claim", "complete"},
		Timeout:       10 * time.Second,
	}
}

// OrchestratorTaskCreateHelpCommand returns a command to show task create help.
// Note: Help commands are handled locally by the CLI and do not require transport.
func OrchestratorTaskCreateHelpCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator task create help",
		Args:          []string{"orchestrator", "task", "create", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"subject", "description", "team", "assigned-to"},
		Timeout:       10 * time.Second,
	}
}

// OrchestratorSpawnHelpCommand returns a command to show spawn help.
// Note: Help commands are handled locally by the CLI and do not require transport.
func OrchestratorSpawnHelpCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator spawn help",
		Args:          []string{"orchestrator", "spawn", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"backend", "model", "system-prompt", "project"},
		Timeout:       10 * time.Second,
	}
}

// OrchestratorDecomposeHelpCommand returns a command to show decompose help.
// Note: Help commands are handled locally by the CLI and do not require transport.
func OrchestratorDecomposeHelpCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator decompose help",
		Args:          []string{"orchestrator", "decompose", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"decompose", "backend", "model", "project"},
		Timeout:       10 * time.Second,
	}
}

// OrchestratorTaskCreateWithAllOptionsCommand returns a command to create a task with all options.
func OrchestratorTaskCreateWithAllOptionsCommand(subject, description, team, assignedTo string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("orchestrator task create all options %s", subject),
		Args: []string{
			"orchestrator", "task", "create",
			"--subject", subject,
			"--description", description,
			"--team", team,
			"--assigned-to", assignedTo,
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// =============================================================================
// Orchestrator Run Command Definitions
// =============================================================================

// OrchestratorRunHelpCommand returns a command to show orchestrator run help.
// Note: Help commands are handled locally by the CLI and do not require transport.
func OrchestratorRunHelpCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator run help",
		Args:          []string{"orchestrator", "run", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"workflow", "backend", "max-iterations"},
		Timeout:       10 * time.Second,
	}
}

// OrchestratorRunMissingDescriptionCommand returns a command that should fail due to missing description.
// Note: This tests CLI validation which happens before transport.
func OrchestratorRunMissingDescriptionCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator run missing description",
		Args:          []string{"orchestrator", "run"},
		ExpectSuccess: false,
		ExpectError:   []string{"accepts 1 arg"},
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// Orchestrator Job Command Definitions
// =============================================================================

// OrchestratorJobStatusHelpCommand returns a command to show job status help.
// Note: Help commands are handled locally by the CLI and do not require transport.
func OrchestratorJobStatusHelpCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator job status help",
		Args:          []string{"orchestrator", "job", "status", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"status"},
		Timeout:       10 * time.Second,
	}
}

// OrchestratorJobCancelHelpCommand returns a command to show job cancel help.
// Note: Help commands are handled locally by the CLI and do not require transport.
func OrchestratorJobCancelHelpCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator job cancel help",
		Args:          []string{"orchestrator", "job", "cancel", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"cancel"},
		Timeout:       10 * time.Second,
	}
}

// OrchestratorJobStatusMissingIDCommand returns a command that should fail due to missing job ID.
// Note: This tests CLI validation which happens before transport.
func OrchestratorJobStatusMissingIDCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator job status missing id",
		Args:          []string{"orchestrator", "job", "status"},
		ExpectSuccess: false,
		ExpectError:   []string{"accepts 1 arg"},
		Timeout:       10 * time.Second,
	}
}

// OrchestratorJobCancelMissingIDCommand returns a command that should fail due to missing job ID.
// Note: This tests CLI validation which happens before transport.
func OrchestratorJobCancelMissingIDCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator job cancel missing id",
		Args:          []string{"orchestrator", "job", "cancel"},
		ExpectSuccess: false,
		ExpectError:   []string{"accepts 1 arg"},
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// Orchestrator Workflow Command Definitions
// =============================================================================

// OrchestratorWorkflowListHelpCommand returns a command to show workflow list help.
// Note: Help commands are handled locally by the CLI and do not require transport.
func OrchestratorWorkflowListHelpCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator workflow list help",
		Args:          []string{"orchestrator", "workflow", "list", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"list", "workflow"},
		Timeout:       10 * time.Second,
	}
}

// OrchestratorWorkflowShowHelpCommand returns a command to show workflow show help.
// Note: Help commands are handled locally by the CLI and do not require transport.
func OrchestratorWorkflowShowHelpCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator workflow show help",
		Args:          []string{"orchestrator", "workflow", "show", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"show", "workflow"},
		Timeout:       10 * time.Second,
	}
}

// OrchestratorWorkflowShowMissingNameCommand returns a command that should fail due to missing workflow name.
// Note: This tests CLI validation which happens before transport.
func OrchestratorWorkflowShowMissingNameCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator workflow show missing name",
		Args:          []string{"orchestrator", "workflow", "show"},
		ExpectSuccess: false,
		ExpectError:   []string{"accepts 1 arg"},
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// Orchestrator Workflow Command Definitions (Operational)
// =============================================================================

// OrchestratorWorkflowListCommand returns a command to list available workflows.
// Workflow listing is a client-side operation that reads the local workflows/ directory.
func OrchestratorWorkflowListCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator workflow list",
		Args:          []string{"orchestrator", "workflow", "list"},
		ExpectSuccess: true,
		Timeout:       10 * time.Second,
	}
}

// OrchestratorWorkflowListJSONCommand returns a command to list workflows in JSON format.
func OrchestratorWorkflowListJSONCommand() CommandDef {
	return CommandDef{
		Name:          "orchestrator workflow list json",
		Args:          []string{"orchestrator", "workflow", "list", "--output", "json"},
		ExpectSuccess: true,
		Timeout:       10 * time.Second,
	}
}

// OrchestratorWorkflowShowCommand returns a command to show a specific workflow definition.
func OrchestratorWorkflowShowCommand(name string) CommandDef {
	return CommandDef{
		Name:          fmt.Sprintf("orchestrator workflow show %s", name),
		Args:          []string{"orchestrator", "workflow", "show", name},
		ExpectSuccess: true,
		ExpectOutput:  []string{name},
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// Orchestrator Job Command Definitions (Operational)
// =============================================================================

// OrchestratorJobListCommand returns a command to list orchestration jobs.
func OrchestratorJobListCommand() CommandDef {
	return CommandDef{
		Name:           "orchestrator job list",
		Args:           []string{"orchestrator", "job", "list"},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorJobListJSONCommand returns a command to list jobs in JSON format.
func OrchestratorJobListJSONCommand() CommandDef {
	return CommandDef{
		Name:           "orchestrator job list json",
		Args:           []string{"orchestrator", "job", "list", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorJobStatusCommand returns a command to get a job's status.
func OrchestratorJobStatusCommand(jobID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator job status %s", jobID),
		Args:           []string{"orchestrator", "job", "status", jobID},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorJobStatusJSONCommand returns a command to get a job's status in JSON format.
func OrchestratorJobStatusJSONCommand(jobID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator job status %s json", jobID),
		Args:           []string{"orchestrator", "job", "status", jobID, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorJobEventsCommand returns a command to view job events.
func OrchestratorJobEventsCommand(jobID, verbosity string) CommandDef {
	args := []string{"orchestrator", "job", "events", jobID}
	if verbosity != "" {
		args = append(args, "--verbosity", verbosity)
	}
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator job events %s", jobID),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// OrchestratorJobCancelCommand returns a command to cancel a running job.
func OrchestratorJobCancelCommand(jobID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("orchestrator job cancel %s", jobID),
		Args:           []string{"orchestrator", "job", "cancel", jobID},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// =============================================================================
// Orchestrator Run Command Definition
// =============================================================================

// OrchestratorRunCommand returns a command to run an autonomous orchestration job.
func OrchestratorRunCommand(description, workflow, workflowFile, backend, model, project string) CommandDef {
	args := []string{"orchestrator", "run", description}
	if workflow != "" {
		args = append(args, "--workflow", workflow)
	}
	if workflowFile != "" {
		args = append(args, "--workflow-file", workflowFile)
	}
	if backend != "" {
		args = append(args, "--backend", backend)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if project != "" {
		args = append(args, "--project", project)
	}
	return CommandDef{
		Name:           "orchestrator run",
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        5 * time.Minute,
		SkipTransports: orchestratorUnsupportedTransports,
	}
}

// =============================================================================
// Tests
// =============================================================================

// TestOrchestratorHelp tests orchestrator help commands across all transports.
func TestOrchestratorHelp(t *testing.T) {
	RunForAllTransports(t, "OrchestratorHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorHelpCommand())
	})

	RunForAllTransports(t, "OrchestratorTaskHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorTaskHelpCommand())
	})

	RunForAllTransports(t, "OrchestratorTaskCreateHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorTaskCreateHelpCommand())
	})

	RunForAllTransports(t, "OrchestratorSpawnHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorSpawnHelpCommand())
	})

	RunForAllTransports(t, "OrchestratorDecomposeHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorDecomposeHelpCommand())
	})
}

// TestOrchestratorHealth tests orchestrator health check with all output formats.
func TestOrchestratorHealth(t *testing.T) {
	RunForAllTransports(t, "OrchestratorHealth", func(t *testing.T, runner *TestRunner) {
		cmd := OrchestratorHealthCommand()
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})

	RunForAllTransports(t, "OrchestratorHealthJSON", func(t *testing.T, runner *TestRunner) {
		cmd := OrchestratorHealthJSONCommand()
		if runner.IsSkipped(cmd) {
			return
		}
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err)

		// Verify JSON output is valid and contains expected fields
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "Output should be valid JSON")
		assert.Contains(t, result, "healthy", "JSON output should contain healthy field")
		assert.Contains(t, result, "status", "JSON output should contain status field")
	})

	RunForAllTransports(t, "OrchestratorHealthYAML", func(t *testing.T, runner *TestRunner) {
		cmd := OrchestratorHealthYAMLCommand()
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})
}

// TestOrchestratorBackends tests listing available agent backends across all transports.
func TestOrchestratorBackends(t *testing.T) {
	RunForAllTransports(t, "OrchestratorBackends", func(t *testing.T, runner *TestRunner) {
		cmd := OrchestratorBackendsCommand()
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})

	RunForAllTransports(t, "OrchestratorBackendsJSON", func(t *testing.T, runner *TestRunner) {
		cmd := OrchestratorBackendsJSONCommand()
		if runner.IsSkipped(cmd) {
			return
		}
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err)

		// Verify JSON output is valid and contains expected fields
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "Output should be valid JSON")
		assert.Contains(t, result, "backends", "JSON output should contain backends field")
		assert.Contains(t, result, "count", "JSON output should contain count field")
	})

	RunForAllTransports(t, "OrchestratorBackendsYAML", func(t *testing.T, runner *TestRunner) {
		cmd := OrchestratorBackendsYAMLCommand()
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})
}

// TestOrchestratorSessions tests listing active agent sessions across all transports.
func TestOrchestratorSessions(t *testing.T) {
	RunForAllTransports(t, "OrchestratorSessions", func(t *testing.T, runner *TestRunner) {
		cmd := OrchestratorSessionsCommand()
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})

	RunForAllTransports(t, "OrchestratorSessionsJSON", func(t *testing.T, runner *TestRunner) {
		cmd := OrchestratorSessionsJSONCommand()
		if runner.IsSkipped(cmd) {
			return
		}
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err)

		// Verify JSON output is valid and contains expected fields
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "Output should be valid JSON")
		assert.Contains(t, result, "sessions", "JSON output should contain sessions field")
		assert.Contains(t, result, "count", "JSON output should contain count field")
	})

	RunForAllTransports(t, "OrchestratorSessionsYAML", func(t *testing.T, runner *TestRunner) {
		cmd := OrchestratorSessionsYAMLCommand()
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})
}

// TestOrchestratorTaskLifecycle tests the complete task lifecycle via CLI:
// create task -> get task -> list tasks -> claim task -> complete task.
func TestOrchestratorTaskLifecycle(t *testing.T) {
	RunForAllTransports(t, "TaskLifecycle", func(t *testing.T, runner *TestRunner) {
		subject := fmt.Sprintf("lifecycle-test-%d", time.Now().UnixNano())
		agentID := fmt.Sprintf("agent-%d", time.Now().UnixNano())

		// 1. Create task
		createCmd := OrchestratorTaskCreateCommand(subject)
		if runner.IsSkipped(createCmd) {
			return
		}
		stdout, _, err := runner.Run(createCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, subject)

		// 2. List tasks in JSON to find the created task ID
		listCmd := OrchestratorTaskListJSONCommand()
		stdout, _, err = runner.Run(listCmd)
		require.NoError(t, err)

		taskID := findOrchestratorTaskID(t, stdout, subject)
		require.NotEmpty(t, taskID, "Created task must be findable by subject")

		// 3. Get task details
		getCmd := OrchestratorTaskGetCommand(taskID)
		stdout, _, err = runner.Run(getCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, subject)

		// 4. Get task in JSON format
		getJSONCmd := OrchestratorTaskGetJSONCommand(taskID)
		stdout, _, err = runner.Run(getJSONCmd)
		require.NoError(t, err)
		var taskResp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &taskResp)
		require.NoError(t, err, "Task get output should be valid JSON")

		// 5. Claim task
		claimCmd := OrchestratorTaskClaimCommand(taskID, agentID)
		stdout, _, err = runner.Run(claimCmd)
		require.NoError(t, err)
		t.Logf("Claim result: %s", stdout)

		// 6. Complete task with result
		completeCmd := OrchestratorTaskCompleteWithResultCommand(taskID, "Lifecycle test completed successfully")
		stdout, _, err = runner.Run(completeCmd)
		require.NoError(t, err)
		t.Logf("Complete result: %s", stdout)
	})
}

// TestOrchestratorTaskCreate tests task creation with various options across all transports.
func TestOrchestratorTaskCreate(t *testing.T) {
	RunForAllTransports(t, "CreateBasicTask", func(t *testing.T, runner *TestRunner) {
		subject := fmt.Sprintf("basic-task-%d", time.Now().UnixNano())
		cmd := OrchestratorTaskCreateCommand(subject)
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})

	RunForAllTransports(t, "CreateTaskWithTeam", func(t *testing.T, runner *TestRunner) {
		subject := fmt.Sprintf("team-task-%d", time.Now().UnixNano())
		cmd := OrchestratorTaskCreateWithTeamCommand(subject, "backend")
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})

	RunForAllTransports(t, "CreateTaskWithTenant", func(t *testing.T, runner *TestRunner) {
		subject := fmt.Sprintf("tenant-task-%d", time.Now().UnixNano())
		tenantID := UniqueKey("tenant")
		cmd := OrchestratorTaskCreateWithTenantCommand(subject, tenantID)
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})
}

// TestOrchestratorTaskList tests task listing with various filters across all transports.
func TestOrchestratorTaskList(t *testing.T) {
	RunForAllTransports(t, "ListTasks", func(t *testing.T, runner *TestRunner) {
		cmd := OrchestratorTaskListCommand()
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})

	RunForAllTransports(t, "ListTasksJSON", func(t *testing.T, runner *TestRunner) {
		cmd := OrchestratorTaskListJSONCommand()
		if runner.IsSkipped(cmd) {
			return
		}
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err)

		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "Output should be valid JSON")
	})

	RunForAllTransports(t, "ListTasksByStatus", func(t *testing.T, runner *TestRunner) {
		statuses := []string{"pending", "claimed", "in_progress", "completed", "failed", "cancelled"}
		for _, status := range statuses {
			t.Run(status, func(t *testing.T) {
				cmd := OrchestratorTaskListWithStatusCommand(status)
				if runner.IsSkipped(cmd) {
					return
				}
				runner.RunAndAssert(cmd)
			})
		}
	})

	RunForAllTransports(t, "ListTasksByTeam", func(t *testing.T, runner *TestRunner) {
		cmd := OrchestratorTaskListWithTeamCommand("backend")
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})

	RunForAllTransports(t, "ListTasksByAssignedTo", func(t *testing.T, runner *TestRunner) {
		cmd := OrchestratorTaskListWithAssignedToCommand("test-agent")
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})

	RunForAllTransports(t, "ListTasksWithTenant", func(t *testing.T, runner *TestRunner) {
		cmd := OrchestratorTaskListWithTenantCommand("test-tenant")
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})
}

// TestOrchestratorTaskErrors tests error handling for task commands.
func TestOrchestratorTaskErrors(t *testing.T) {
	RunForAllTransports(t, "CreateMissingSubject", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorTaskCreateMissingSubjectCommand())
	})

	RunForAllTransports(t, "GetNonexistentTask", func(t *testing.T, runner *TestRunner) {
		cmd := OrchestratorTaskGetNonexistentCommand()
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})

	RunForAllTransports(t, "ClaimMissingAgent", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorTaskClaimMissingAgentCommand())
	})

	RunForAllTransports(t, "SpawnMissingBackend", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorSpawnMissingBackendCommand())
	})
}

// TestOrchestratorOutputFormats tests all output formats for orchestrator commands.
func TestOrchestratorOutputFormats(t *testing.T) {
	formats := []string{"table", "json", "yaml"}

	for _, format := range formats {
		t.Run(fmt.Sprintf("Health_%s", format), func(t *testing.T) {
			RunForAllTransports(t, fmt.Sprintf("Health_%s", format), func(t *testing.T, runner *TestRunner) {
				cmd := CommandDef{
					Name:           fmt.Sprintf("orchestrator health %s", format),
					Args:           []string{"orchestrator", "health", "--output", format},
					ExpectSuccess:  true,
					Timeout:        10 * time.Second,
					SkipTransports: orchestratorUnsupportedTransports,
				}
				if runner.IsSkipped(cmd) {
					return
				}
				runner.RunAndAssert(cmd)
			})
		})

		t.Run(fmt.Sprintf("Backends_%s", format), func(t *testing.T) {
			RunForAllTransports(t, fmt.Sprintf("Backends_%s", format), func(t *testing.T, runner *TestRunner) {
				cmd := CommandDef{
					Name:           fmt.Sprintf("orchestrator backends %s", format),
					Args:           []string{"orchestrator", "backends", "--output", format},
					ExpectSuccess:  true,
					Timeout:        10 * time.Second,
					SkipTransports: orchestratorUnsupportedTransports,
				}
				if runner.IsSkipped(cmd) {
					return
				}
				runner.RunAndAssert(cmd)
			})
		})

		t.Run(fmt.Sprintf("Sessions_%s", format), func(t *testing.T) {
			RunForAllTransports(t, fmt.Sprintf("Sessions_%s", format), func(t *testing.T, runner *TestRunner) {
				cmd := CommandDef{
					Name:           fmt.Sprintf("orchestrator sessions %s", format),
					Args:           []string{"orchestrator", "sessions", "--output", format},
					ExpectSuccess:  true,
					Timeout:        10 * time.Second,
					SkipTransports: orchestratorUnsupportedTransports,
				}
				if runner.IsSkipped(cmd) {
					return
				}
				runner.RunAndAssert(cmd)
			})
		})

		t.Run(fmt.Sprintf("TaskList_%s", format), func(t *testing.T) {
			RunForAllTransports(t, fmt.Sprintf("TaskList_%s", format), func(t *testing.T, runner *TestRunner) {
				cmd := CommandDef{
					Name:           fmt.Sprintf("orchestrator task list %s", format),
					Args:           []string{"orchestrator", "task", "list", "--output", format},
					ExpectSuccess:  true,
					Timeout:        10 * time.Second,
					SkipTransports: orchestratorUnsupportedTransports,
				}
				if runner.IsSkipped(cmd) {
					return
				}
				runner.RunAndAssert(cmd)
			})
		})
	}
}

// TestOrchestratorTaskClaimComplete tests the claim and complete workflow.
func TestOrchestratorTaskClaimComplete(t *testing.T) {
	RunForAllTransports(t, "ClaimAndComplete", func(t *testing.T, runner *TestRunner) {
		subject := fmt.Sprintf("claim-complete-%d", time.Now().UnixNano())
		agentID := fmt.Sprintf("agent-%d", time.Now().UnixNano())

		// Create task
		createCmd := OrchestratorTaskCreateCommand(subject)
		if runner.IsSkipped(createCmd) {
			return
		}
		_, _, err := runner.Run(createCmd)
		require.NoError(t, err)

		// Find task ID
		listStdout, _, err := runner.Run(OrchestratorTaskListJSONCommand())
		require.NoError(t, err)

		taskID := findOrchestratorTaskID(t, listStdout, subject)
		require.NotEmpty(t, taskID, "Created task must be findable by subject")

		// Claim task
		claimCmd := OrchestratorTaskClaimCommand(taskID, agentID)
		_, _, err = runner.Run(claimCmd)
		require.NoError(t, err)

		// Verify task is claimed by getting it in JSON
		getStdout, _, err := runner.Run(OrchestratorTaskGetJSONCommand(taskID))
		require.NoError(t, err)
		var taskResp map[string]interface{}
		err = json.Unmarshal([]byte(getStdout), &taskResp)
		require.NoError(t, err)
		t.Logf("Task after claim: %v", taskResp)

		// Complete task without result
		completeCmd := OrchestratorTaskCompleteCommand(taskID)
		_, _, err = runner.Run(completeCmd)
		require.NoError(t, err)
	})

	RunForAllTransports(t, "CompleteWithResult", func(t *testing.T, runner *TestRunner) {
		subject := fmt.Sprintf("complete-result-%d", time.Now().UnixNano())
		agentID := fmt.Sprintf("agent-%d", time.Now().UnixNano())

		// Create task
		createCmd := OrchestratorTaskCreateCommand(subject)
		if runner.IsSkipped(createCmd) {
			return
		}
		_, _, err := runner.Run(createCmd)
		require.NoError(t, err)

		// Find task ID
		listStdout, _, err := runner.Run(OrchestratorTaskListJSONCommand())
		require.NoError(t, err)

		taskID := findOrchestratorTaskID(t, listStdout, subject)
		require.NotEmpty(t, taskID, "Created task must be findable by subject")

		// Claim task
		_, _, err = runner.Run(OrchestratorTaskClaimCommand(taskID, agentID))
		require.NoError(t, err)

		// Complete with result
		completeCmd := OrchestratorTaskCompleteWithResultCommand(taskID, "Deployment to staging succeeded")
		_, _, err = runner.Run(completeCmd)
		require.NoError(t, err)

		// Verify completed task
		getStdout, _, err := runner.Run(OrchestratorTaskGetJSONCommand(taskID))
		require.NoError(t, err)
		var taskResp map[string]interface{}
		err = json.Unmarshal([]byte(getStdout), &taskResp)
		require.NoError(t, err)
		t.Logf("Task after completion: %v", taskResp)
	})
}

// TestOrchestratorTaskGet tests getting a specific task by ID across all transports.
func TestOrchestratorTaskGet(t *testing.T) {
	RunForAllTransports(t, "GetTask", func(t *testing.T, runner *TestRunner) {
		subject := fmt.Sprintf("get-task-%d", time.Now().UnixNano())

		// Create a task first
		createCmd := OrchestratorTaskCreateCommand(subject)
		if runner.IsSkipped(createCmd) {
			return
		}
		_, _, err := runner.Run(createCmd)
		require.NoError(t, err)

		// Find task ID via list
		listStdout, _, err := runner.Run(OrchestratorTaskListJSONCommand())
		require.NoError(t, err)
		taskID := findOrchestratorTaskID(t, listStdout, subject)
		require.NotEmpty(t, taskID, "Created task must be findable by subject")

		// Get task in table format
		getCmd := OrchestratorTaskGetCommand(taskID)
		stdout, _, err := runner.Run(getCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, subject)

		// Get task in JSON format and verify structure
		getJSONCmd := OrchestratorTaskGetJSONCommand(taskID)
		stdout, _, err = runner.Run(getJSONCmd)
		require.NoError(t, err)
		var taskResp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &taskResp)
		require.NoError(t, err, "Task get JSON output should be valid")
		assert.Equal(t, subject, taskResp["subject"], "Task subject should match")

		// Get task in YAML format
		getYAMLCmd := OrchestratorTaskGetYAMLCommand(taskID)
		stdout, _, err = runner.Run(getYAMLCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, subject)
	})
}

// TestOrchestratorTaskClaim tests claiming a task for an agent across all transports.
func TestOrchestratorTaskClaim(t *testing.T) {
	RunForAllTransports(t, "ClaimTask", func(t *testing.T, runner *TestRunner) {
		subject := fmt.Sprintf("claim-task-%d", time.Now().UnixNano())
		agentID := fmt.Sprintf("claim-agent-%d", time.Now().UnixNano())

		// Create a task
		createCmd := OrchestratorTaskCreateCommand(subject)
		if runner.IsSkipped(createCmd) {
			return
		}
		_, _, err := runner.Run(createCmd)
		require.NoError(t, err)

		// Find task ID
		listStdout, _, err := runner.Run(OrchestratorTaskListJSONCommand())
		require.NoError(t, err)
		taskID := findOrchestratorTaskID(t, listStdout, subject)
		require.NotEmpty(t, taskID, "Created task must be findable by subject")

		// Claim the task
		claimCmd := OrchestratorTaskClaimCommand(taskID, agentID)
		stdout, _, err := runner.Run(claimCmd)
		require.NoError(t, err)
		t.Logf("Claim output: %s", stdout)

		// Verify the task is now claimed via JSON get
		getStdout, _, err := runner.Run(OrchestratorTaskGetJSONCommand(taskID))
		require.NoError(t, err)
		var taskResp map[string]interface{}
		err = json.Unmarshal([]byte(getStdout), &taskResp)
		require.NoError(t, err)

		// The task should reflect claimed state
		t.Logf("Task after claim: %v", taskResp)
	})
}

// TestOrchestratorTaskComplete tests completing a task across all transports.
func TestOrchestratorTaskComplete(t *testing.T) {
	RunForAllTransports(t, "CompleteTask", func(t *testing.T, runner *TestRunner) {
		subject := fmt.Sprintf("complete-task-%d", time.Now().UnixNano())
		agentID := fmt.Sprintf("complete-agent-%d", time.Now().UnixNano())

		// Create and claim a task
		createCmd := OrchestratorTaskCreateCommand(subject)
		if runner.IsSkipped(createCmd) {
			return
		}
		_, _, err := runner.Run(createCmd)
		require.NoError(t, err)

		listStdout, _, err := runner.Run(OrchestratorTaskListJSONCommand())
		require.NoError(t, err)
		taskID := findOrchestratorTaskID(t, listStdout, subject)
		require.NotEmpty(t, taskID, "Created task must be findable by subject")

		_, _, err = runner.Run(OrchestratorTaskClaimCommand(taskID, agentID))
		require.NoError(t, err)

		// Complete the task with a result
		completeCmd := OrchestratorTaskCompleteWithResultCommand(taskID, "Task completed successfully")
		stdout, _, err := runner.Run(completeCmd)
		require.NoError(t, err)
		t.Logf("Complete output: %s", stdout)

		// Verify the task shows completed via JSON get
		getStdout, _, err := runner.Run(OrchestratorTaskGetJSONCommand(taskID))
		require.NoError(t, err)
		var taskResp map[string]interface{}
		err = json.Unmarshal([]byte(getStdout), &taskResp)
		require.NoError(t, err)
		t.Logf("Task after completion: %v", taskResp)
	})

	RunForAllTransports(t, "CompleteTaskWithoutResult", func(t *testing.T, runner *TestRunner) {
		subject := fmt.Sprintf("complete-noresult-%d", time.Now().UnixNano())
		agentID := fmt.Sprintf("complete-noresult-agent-%d", time.Now().UnixNano())

		// Create and claim a task
		createCmd := OrchestratorTaskCreateCommand(subject)
		if runner.IsSkipped(createCmd) {
			return
		}
		_, _, err := runner.Run(createCmd)
		require.NoError(t, err)

		listStdout, _, err := runner.Run(OrchestratorTaskListJSONCommand())
		require.NoError(t, err)
		taskID := findOrchestratorTaskID(t, listStdout, subject)
		require.NotEmpty(t, taskID)

		_, _, err = runner.Run(OrchestratorTaskClaimCommand(taskID, agentID))
		require.NoError(t, err)

		// Complete without result
		completeCmd := OrchestratorTaskCompleteCommand(taskID)
		_, _, err = runner.Run(completeCmd)
		require.NoError(t, err)
	})
}

// TestOrchestratorDecomposeHelp tests the decompose help command across all transports.
func TestOrchestratorDecomposeHelp(t *testing.T) {
	RunForAllTransports(t, "DecomposeHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorDecomposeHelpCommand())
	})
}

// TestOrchestratorDecomposeMissingTask tests decompose without a task argument returns an error.
func TestOrchestratorDecomposeMissingTask(t *testing.T) {
	RunForAllTransports(t, "DecomposeMissingTask", func(t *testing.T, runner *TestRunner) {
		cmd := CommandDef{
			Name:          "DecomposeMissingTask",
			Args:          []string{"orchestrator", "decompose"},
			ExpectSuccess: false,
			ExpectError:   []string{"accepts 1 arg"},
		}
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})
}

// TestOrchestratorDecomposeInvalidBackend tests decompose with a non-existent backend returns an error.
func TestOrchestratorDecomposeInvalidBackend(t *testing.T) {
	RunForAllTransports(t, "DecomposeInvalidBackend", func(t *testing.T, runner *TestRunner) {
		cmd := CommandDef{
			Name:          "DecomposeInvalidBackend",
			Args:          []string{"orchestrator", "decompose", "some task", "--backend", "nonexistent-backend-xyz"},
			ExpectSuccess: false,
		}
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})
}

// TestOrchestratorTaskCreateWithAllOptions tests creating a task with all available flags.
func TestOrchestratorTaskCreateWithAllOptions(t *testing.T) {
	RunForAllTransports(t, "CreateTaskWithDescription", func(t *testing.T, runner *TestRunner) {
		subject := fmt.Sprintf("desc-task-%d", time.Now().UnixNano())
		cmd := OrchestratorTaskCreateWithDescriptionCommand(subject, "This is a detailed description of the task")
		if runner.IsSkipped(cmd) {
			return
		}
		runner.RunAndAssert(cmd)
	})

	RunForAllTransports(t, "CreateTaskWithAllOptions", func(t *testing.T, runner *TestRunner) {
		subject := fmt.Sprintf("allopts-task-%d", time.Now().UnixNano())
		agentID := fmt.Sprintf("allopts-agent-%d", time.Now().UnixNano())
		cmd := OrchestratorTaskCreateWithAllOptionsCommand(
			subject,
			"Full description for the task",
			"backend",
			agentID,
		)
		if runner.IsSkipped(cmd) {
			return
		}
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, subject)
	})
}

// TestOrchestratorRunHelp tests orchestrator run help command across all transports.
func TestOrchestratorRunHelp(t *testing.T) {
	RunForAllTransports(t, "RunHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorRunHelpCommand())
	})
}

// TestOrchestratorRunMissingDescription tests that run command requires a description argument.
func TestOrchestratorRunMissingDescription(t *testing.T) {
	RunForAllTransports(t, "RunMissingDesc", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorRunMissingDescriptionCommand())
	})
}

// TestOrchestratorJobStatusHelp tests orchestrator job status help command across all transports.
func TestOrchestratorJobStatusHelp(t *testing.T) {
	RunForAllTransports(t, "JobStatusHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorJobStatusHelpCommand())
	})
}

// TestOrchestratorJobCancelHelp tests orchestrator job cancel help command across all transports.
func TestOrchestratorJobCancelHelp(t *testing.T) {
	RunForAllTransports(t, "JobCancelHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorJobCancelHelpCommand())
	})
}

// TestOrchestratorJobErrors tests error handling for job commands.
func TestOrchestratorJobErrors(t *testing.T) {
	RunForAllTransports(t, "JobStatusMissingID", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorJobStatusMissingIDCommand())
	})

	RunForAllTransports(t, "JobCancelMissingID", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorJobCancelMissingIDCommand())
	})
}

// TestOrchestratorWorkflowListHelp tests workflow list help command across all transports.
func TestOrchestratorWorkflowListHelp(t *testing.T) {
	RunForAllTransports(t, "WorkflowListHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorWorkflowListHelpCommand())
	})
}

// TestOrchestratorWorkflowShowHelp tests workflow show help command across all transports.
func TestOrchestratorWorkflowShowHelp(t *testing.T) {
	RunForAllTransports(t, "WorkflowShowHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorWorkflowShowHelpCommand())
	})
}

// TestOrchestratorWorkflowErrors tests error handling for workflow commands.
func TestOrchestratorWorkflowErrors(t *testing.T) {
	RunForAllTransports(t, "WorkflowShowMissingName", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OrchestratorWorkflowShowMissingNameCommand())
	})
}

// =============================================================================
// Operational Tests (require server)
// =============================================================================

// TestOrchestratorWorkflowList tests listing available workflows across all transports.
// This is a client-side operation reading from the local workflows/ directory.
func TestOrchestratorWorkflowList(t *testing.T) {
	RunForAllTransports(t, "WorkflowList", func(t *testing.T, runner *TestRunner) {
		cmd := OrchestratorWorkflowListCommand()
		// Workflow list reads local dir -- may return empty if no workflows in CWD.
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err, "workflow list should not return an error")
		_ = stdout
	})
}

// TestOrchestratorWorkflowListJSON tests listing workflows in JSON output format.
func TestOrchestratorWorkflowListJSON(t *testing.T) {
	RunForAllTransports(t, "WorkflowListJSON", func(t *testing.T, runner *TestRunner) {
		cmd := OrchestratorWorkflowListJSONCommand()
		stdout, _, err := runner.Run(cmd)
		require.NoError(t, err, "workflow list JSON should not return an error")
		_ = stdout
	})
}

// TestOrchestratorJobList tests listing orchestration jobs across transports.
func TestOrchestratorJobList(t *testing.T) {
	RunForTransports(t, "JobList", orchestratorUnsupportedTransports, func(t *testing.T, runner *TestRunner) {
		cmd := OrchestratorJobListCommand()
		runner.RunAndAssert(cmd)
	})
}

// TestOrchestratorJobListJSON tests listing jobs in JSON format.
func TestOrchestratorJobListJSON(t *testing.T) {
	RunForTransports(t, "JobListJSON", orchestratorUnsupportedTransports, func(t *testing.T, runner *TestRunner) {
		cmd := OrchestratorJobListJSONCommand()
		runner.RunAndAssert(cmd)
	})
}

// TestOrchestratorJobStatusNonexistent tests that querying a non-existent job
// returns an appropriate error instead of crashing.
func TestOrchestratorJobStatusNonexistent(t *testing.T) {
	RunForTransports(t, "JobStatusNonexistent", orchestratorUnsupportedTransports, func(t *testing.T, runner *TestRunner) {
		cmd := OrchestratorJobStatusCommand("nonexistent-job-id")
		cmd.ExpectSuccess = false
		stdout, stderr, err := runner.Run(cmd)
		assert.Error(t, err, "querying non-existent job should fail")
		_ = stdout
		_ = stderr
	})
}

// TestOrchestratorJobCancelNonexistent tests that cancelling a non-existent job
// returns an appropriate error.
func TestOrchestratorJobCancelNonexistent(t *testing.T) {
	RunForTransports(t, "JobCancelNonexistent", orchestratorUnsupportedTransports, func(t *testing.T, runner *TestRunner) {
		cmd := OrchestratorJobCancelCommand("nonexistent-job-id")
		cmd.ExpectSuccess = false
		stdout, stderr, err := runner.Run(cmd)
		assert.Error(t, err, "cancelling non-existent job should fail")
		_ = stdout
		_ = stderr
	})
}

// =============================================================================
// Orchestrator Suites
// =============================================================================

// OrchestratorHelpSuite returns help commands for orchestrator.
func OrchestratorHelpSuite() []CommandDef {
	return []CommandDef{
		OrchestratorHelpCommand(),
		OrchestratorTaskHelpCommand(),
		OrchestratorTaskCreateHelpCommand(),
		OrchestratorSpawnHelpCommand(),
		OrchestratorDecomposeHelpCommand(),
		OrchestratorRunHelpCommand(),
		OrchestratorJobStatusHelpCommand(),
		OrchestratorJobCancelHelpCommand(),
		OrchestratorWorkflowListHelpCommand(),
		OrchestratorWorkflowShowHelpCommand(),
	}
}

// OrchestratorErrorSuite returns error case commands for orchestrator.
func OrchestratorErrorSuite() []CommandDef {
	return []CommandDef{
		OrchestratorSpawnMissingBackendCommand(),
		OrchestratorTaskCreateMissingSubjectCommand(),
		OrchestratorTaskClaimMissingAgentCommand(),
		OrchestratorRunMissingDescriptionCommand(),
		OrchestratorJobStatusMissingIDCommand(),
		OrchestratorJobCancelMissingIDCommand(),
		OrchestratorWorkflowShowMissingNameCommand(),
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

// findOrchestratorTaskID searches JSON task list output for a task matching the given subject
// and returns its ID. Returns empty string if not found.
func findOrchestratorTaskID(t *testing.T, jsonOutput, subject string) string {
	t.Helper()

	var listResp struct {
		Tasks []struct {
			ID      string `json:"id"`
			Subject string `json:"subject"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &listResp); err != nil {
		t.Logf("Failed to parse task list JSON: %v", err)
		return ""
	}

	for _, task := range listResp.Tasks {
		if task.Subject == subject {
			return task.ID
		}
	}

	// Fallback: search for subject substring in task fields
	for _, task := range listResp.Tasks {
		if strings.Contains(task.Subject, subject) {
			return task.ID
		}
	}

	return ""
}

// extractOrchestratorSessionID attempts to extract a session ID from spawn command output.
// The output format varies by output mode; this function searches for common patterns.
func extractOrchestratorSessionID(output string) string {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Table format: "Session ID:  sess_xxx"
		if strings.HasPrefix(line, "Session ID:") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				return parts[2]
			}
		}
		// Default format: "Session spawned: sess_xxx (backend: ...)"
		if strings.HasPrefix(line, "Session spawned:") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				return parts[2]
			}
		}
	}
	return ""
}

// extractOrchestratorSessionIDFromJSON extracts a session ID from JSON spawn output.
func extractOrchestratorSessionIDFromJSON(jsonOutput string) string {
	var resp struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &resp); err != nil {
		return ""
	}
	return resp.SessionID
}
