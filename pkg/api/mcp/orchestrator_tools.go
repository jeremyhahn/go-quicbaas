//go:build mcp

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

package mcp

import (
	"context"
	"encoding/json"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// registerAgentTools registers all agent orchestration tools with the registry.
func (r *ToolRegistry) registerAgentTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_discover",
		Description: "Discover agent-enabled projects by scanning a directory tree for CLAUDE.md and .claude/agents/ files",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"root_path": map[string]interface{}{
					"type":        "string",
					"description": "Root directory path to scan for projects",
				},
			},
			"required": []string{"root_path"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_register",
		Description: "Register a project for agent orchestration with optional backend and model defaults",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Human-readable project name",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute filesystem path to the project",
				},
				"dependencies": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "List of project IDs this project depends on (optional)",
				},
				"default_backend": map[string]interface{}{
					"type":        "string",
					"description": "Default backend name for this project (optional)",
				},
				"default_model": map[string]interface{}{
					"type":        "string",
					"description": "Default model for this project (optional)",
				},
				"labels": map[string]interface{}{
					"type":        "object",
					"description": "Key-value labels for project categorization (optional)",
				},
			},
			"required": []string{"name", "path"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_list",
		Description: "List all registered agent projects with optional pagination",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"page_size": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of projects to return per page (optional)",
				},
				"page_token": map[string]interface{}{
					"type":        "string",
					"description": "Pagination token from previous response (optional)",
				},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name: "quicbaas_orchestrator_spawn",
		Description: "Spawn a persistent, multi-turn ACP agent session on a backend with project context. " +
			"Returns the session_id used by quicbaas_orchestrator_prompt / _status / _close.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant the session belongs to. Required.",
				},
				"backend": map[string]interface{}{
					"type":        "string",
					"description": "Backend name to spawn the session on (e.g., 'claude', 'ollama'). Required.",
				},
				"project_id": map[string]interface{}{
					"type":        "string",
					"description": "Registered project ID to associate with the session. Required.",
				},
				"model": map[string]interface{}{
					"type":        "string",
					"description": "Model to use for the session (optional, uses project/backend default)",
				},
				"system_prompt": map[string]interface{}{
					"type":        "string",
					"description": "System prompt to configure the agent (optional)",
				},
				"work_dir": map[string]interface{}{
					"type":        "string",
					"description": "Working directory for the agent process (optional, defaults to the project path)",
				},
				"sandbox_mode": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"default", "required", "none"},
					"description": "Sandbox policy selector. Empty defers to the server-side SandboxPolicy. Optional.",
				},
				"host_affinity": map[string]interface{}{
					"type":        "string",
					"description": "Pin the session to a specific agent daemon by Name or Host. Optional.",
				},
				"host_affinity_strict": map[string]interface{}{
					"type":        "boolean",
					"description": "When true an offline host_affinity fails instead of falling back to any available daemon. Optional.",
				},
				"gpu_required": map[string]interface{}{
					"type":        "boolean",
					"description": "Restrict dispatch to daemons advertising GPU capability. Optional.",
				},
				"persistent": map[string]interface{}{
					"type":        "boolean",
					"description": "Multi-turn session that keeps context across prompts. Defaults to true; set false for one-shot.",
				},
				"account_id": map[string]interface{}{
					"type":        "string",
					"description": "Pin the session to a specific pooled provider account. Optional.",
				},
				"account_auto": map[string]interface{}{
					"type":        "boolean",
					"description": "Select a pooled provider account with the named strategy when account_id is empty. Optional.",
				},
				"account_strategy": map[string]interface{}{
					"type":        "string",
					"description": "Account selection strategy: static or least_usage. Empty selects the registry default. Ignored unless account_auto is true.",
				},
				"account_allow": map[string]interface{}{
					"type":  "array",
					"items": map[string]interface{}{"type": "string"},
					"description": "Restrict the session to these pooled account IDs and authorize it to rotate between them. " +
						"Additive filter, never a source: an ID the tenant does not own is simply not a candidate. Optional.",
				},
				"mcp_servers": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"name":    map[string]interface{}{"type": "string"},
							"command": map[string]interface{}{"type": "string"},
							"args": map[string]interface{}{
								"type":  "array",
								"items": map[string]interface{}{"type": "string"},
							},
							"url": map[string]interface{}{"type": "string"},
							"env": map[string]interface{}{"type": "object"},
						},
					},
					"description": "MCP servers to attach to the session (optional)",
				},
			},
			"required": []string{"tenant_id", "backend", "project_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_prompt",
		Description: "Send a prompt to an agent session or backend. Provide session_id for existing sessions, or backend for stateless prompts",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant the session belongs to. Required.",
				},
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "The prompt text to send to the agent",
				},
				"backend": map[string]interface{}{
					"type":        "string",
					"description": "Backend name (optional if session_id provided)",
				},
				"session_id": map[string]interface{}{
					"type":        "string",
					"description": "Existing session ID to route the prompt to (optional)",
				},
				"project_id": map[string]interface{}{
					"type":        "string",
					"description": "Project ID for context resolution (optional)",
				},
				"model": map[string]interface{}{
					"type":        "string",
					"description": "Model to use (optional, uses project/backend default)",
				},
				"system_prompt": map[string]interface{}{
					"type":        "string",
					"description": "System prompt to configure the agent (optional)",
				},
				"use_rag": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to use RAG for context enrichment (optional, default: false)",
				},
			},
			"required": []string{"tenant_id", "prompt"},
		},
	})

	r.addTool(ToolDefinition{
		Name: "quicbaas_orchestrator_session_history",
		Description: "Read a page of a session's durable event transcript: prompts, assistant output, tool calls, and turn lifecycle events. " +
			"MCP has no streaming channel, so this is how an MCP caller observes an asynchronous turn: page from the sequence returned by " +
			"quicbaas_orchestrator_prompt until a turn_completed or turn_failed event carrying the same turn_id appears.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant the session belongs to. Required.",
				},
				"session_id": map[string]interface{}{
					"type":        "string",
					"description": "Session whose transcript to read",
				},
				"from_sequence": map[string]interface{}{
					"type":        "integer",
					"description": "Inclusive lower bound on the event sequence (optional, default: start of session)",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum events to return (optional, default: 256)",
				},
			},
			"required": []string{"tenant_id", "session_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name: "quicbaas_orchestrator_status",
		Description: "Get the status of an active agent session including state, last activity, and uptime. " +
			"MCP has no streaming channel, so poll this tool to observe session progress (see quicbaas_orchestrator_prompt for the turn result).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant the session belongs to. Required.",
				},
				"session_id": map[string]interface{}{
					"type":        "string",
					"description": "Session ID to query",
				},
			},
			"required": []string{"tenant_id", "session_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_close",
		Description: "Close and terminate an active agent session",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant the session belongs to. Required.",
				},
				"session_id": map[string]interface{}{
					"type":        "string",
					"description": "Session ID to close",
				},
			},
			"required": []string{"tenant_id", "session_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name: "quicbaas_orchestrator_rotate_account",
		Description: "Move an agent session onto a different pooled provider account. " +
			"Use when an account is bad or exhausted and the session should keep running on another seat. " +
			"Only a session spawned with account_auto or account_allow may rotate; a bare account_id pin is honored.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant the session belongs to. Required.",
				},
				"session_id": map[string]interface{}{
					"type":        "string",
					"description": "Session to move. Required.",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Recorded on the closed account segment and in the session transcript. Optional.",
				},
				"hold_outgoing": map[string]interface{}{
					"type":        "boolean",
					"description": "Hold the outgoing account out of the pool until available_at so the next selection does not walk back into it. Optional.",
				},
				"available_at": map[string]interface{}{
					"type":        "string",
					"description": "RFC 3339 time the held outgoing account rejoins the pool. Ignored unless hold_outgoing is true; empty applies the server default backoff.",
				},
			},
			"required": []string{"tenant_id", "session_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_list_sessions",
		Description: "List all active agent sessions for a tenant, optionally filtered by backend",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant whose sessions are listed. Required.",
				},
				"backend": map[string]interface{}{
					"type":        "string",
					"description": "Filter sessions by backend name (optional)",
				},
			},
			"required": []string{"tenant_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_list_backends",
		Description: "List all registered agent backends with their capabilities",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_decompose",
		Description: "Use an AI agent to decompose a complex task into discrete work items with dependencies and priorities",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"backend": map[string]interface{}{
					"type":        "string",
					"description": "Backend to use for task decomposition",
				},
				"task": map[string]interface{}{
					"type":        "string",
					"description": "Task description to decompose",
				},
				"model": map[string]interface{}{
					"type":        "string",
					"description": "Model to use (optional)",
				},
				"project_ids": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Target project IDs for context (optional)",
				},
				"use_rag": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to use RAG for context enrichment (optional, default: false)",
				},
			},
			"required": []string{"backend", "task"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_health",
		Description: "Check health of all active agent sessions, returning per-session status and summary counts",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_broadcast",
		Description: "Broadcast a message to a messaging channel for agent coordination",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"channel": map[string]interface{}{
					"type":        "string",
					"description": "Messaging channel to broadcast to",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Message content to broadcast",
				},
				"metadata": map[string]interface{}{
					"type":        "object",
					"description": "Additional metadata to include with the message (optional)",
				},
			},
			"required": []string{"channel", "message"},
		},
	})

	// =========================================================================
	// Fleet Management Tools
	// =========================================================================

	r.addTool(ToolDefinition{
		Name:        "quicbaas_agent_fleet_register",
		Description: "Register an agent with the fleet for tracking and management",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Agent name",
				},
				"host": map[string]interface{}{
					"type":        "string",
					"description": "Host address",
				},
				"agent_type": map[string]interface{}{
					"type":        "string",
					"description": "Agent type (claude-code, codex, opencode)",
				},
				"backend": map[string]interface{}{
					"type":        "string",
					"description": "Backend type (acp, ollama)",
				},
				"work_dir": map[string]interface{}{
					"type":        "string",
					"description": "Working directory",
				},
			},
			"required": []string{"tenant_id", "name", "host", "agent_type"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_agent_fleet_list",
		Description: "List all agents registered in the fleet for a tenant with optional filters",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"host": map[string]interface{}{
					"type":        "string",
					"description": "Filter by host address (optional)",
				},
				"status": map[string]interface{}{
					"type":        "string",
					"description": "Filter by agent status (optional)",
				},
				"agent_type": map[string]interface{}{
					"type":        "string",
					"description": "Filter by agent type (optional)",
				},
			},
			"required": []string{"tenant_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_agent_fleet_status",
		Description: "Get the status of a specific agent in the fleet",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"agent_id": map[string]interface{}{
					"type":        "string",
					"description": "Agent ID",
				},
			},
			"required": []string{"tenant_id", "agent_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_agent_fleet_remove",
		Description: "Remove an agent from the fleet registry",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"agent_id": map[string]interface{}{
					"type":        "string",
					"description": "Agent ID",
				},
			},
			"required": []string{"tenant_id", "agent_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_agent_fleet_heartbeat",
		Description: "Send a heartbeat for an agent to update its last-seen timestamp",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"agent_id": map[string]interface{}{
					"type":        "string",
					"description": "Agent ID",
				},
			},
			"required": []string{"tenant_id", "agent_id"},
		},
	})

	// =========================================================================
	// Orchestrator Job Tools
	// =========================================================================

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_start_job",
		Description: "Start an autonomous orchestration job that decomposes work, delegates to agents, and iterates until completion",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"description": map[string]interface{}{
					"type":        "string",
					"description": "High-level description of the work to be orchestrated",
				},
				"lead_backend": map[string]interface{}{
					"type":        "string",
					"description": "Backend for the lead orchestrator agent (optional)",
				},
				"lead_model": map[string]interface{}{
					"type":        "string",
					"description": "Model for the lead orchestrator agent (optional)",
				},
				"project_id": map[string]interface{}{
					"type":        "string",
					"description": "Project ID to scope the orchestration job (optional)",
				},
				"max_iterations": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of orchestration iterations (optional, default: 10)",
				},
			},
			"required": []string{"description"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_get_job_status",
		Description: "Get the current status of a running orchestration job including iteration count, task progress, and errors",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "string",
					"description": "Job ID to query",
				},
			},
			"required": []string{"job_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_cancel_job",
		Description: "Cancel a running orchestration job",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "string",
					"description": "Job ID to cancel",
				},
			},
			"required": []string{"job_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_list_jobs",
		Description: "List all orchestration jobs for the current tenant, sorted by start time descending",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_task_create",
		Description: "Create an orchestration task, optionally dispatching it immediately as a one-shot job on the best-matching agent",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"subject": map[string]interface{}{
					"type":        "string",
					"description": "Short description of the work to be done. Required.",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Detailed task description. Optional.",
				},
				"team_name": map[string]interface{}{
					"type":        "string",
					"description": "Team to assign the task to. Optional.",
				},
				"assigned_to": map[string]interface{}{
					"type":        "string",
					"description": "Agent ID to assign the task to. Optional.",
				},
				"auto_spawn": map[string]interface{}{
					"type":        "boolean",
					"description": "When true, immediately dispatch a one-shot job on the best-matching agent. Optional.",
				},
				"backend": map[string]interface{}{
					"type":        "string",
					"description": "Restrict auto-spawn agent selection to this backend (e.g. ollama, acp). Ignored when auto_spawn is false. Optional.",
				},
				"gpu_required": map[string]interface{}{
					"type":        "boolean",
					"description": "Restrict auto-spawn to agents advertising GPU capability. Ignored when auto_spawn is false. Optional.",
				},
			},
			"required": []string{"subject"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_task_get_artifact",
		Description: "Retrieve a named output artifact from a task. For kv_key artifacts the content is resolved from the KV store.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "ID of the task. Required.",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Artifact name. Required.",
				},
			},
			"required": []string{"task_id", "name"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_task_list_artifacts",
		Description: "List output artifact metadata for a task. Content is omitted for kv_key artifacts; use quicbaas_task_get_artifact to fetch content.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "ID of the task. Required.",
				},
			},
			"required": []string{"task_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name: "quicbaas_orchestrator_inference",
		Description: "Run a stateless inference call against a fleet-resident agent daemon. " +
			"Routes the prompt to the named backend without requiring a prior session. " +
			"Returns the aggregated text response.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"backend": map[string]interface{}{
					"type":        "string",
					"description": "Named backend to route to (e.g., 'ollama', 'claude'). Required.",
				},
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "User prompt text. Required.",
				},
				"model": map[string]interface{}{
					"type":        "string",
					"description": "Model to use. When empty the backend's default is used. Optional.",
				},
				"system_prompt": map[string]interface{}{
					"type":        "string",
					"description": "System message prepended to the conversation context. Optional.",
				},
				"host_affinity": map[string]interface{}{
					"type":        "string",
					"description": "Pin the call to a specific agent daemon by Name or Host. Optional.",
				},
				"gpu_required": map[string]interface{}{
					"type":        "boolean",
					"description": "Restrict dispatch to daemons that advertise GPU capability. Optional.",
				},
				"metadata": map[string]interface{}{
					"type":        "object",
					"description": "Opaque string-to-string map forwarded to the backend unchanged. Optional.",
				},
			},
			"required": []string{"backend", "prompt"},
		},
	})
}

// =============================================================================
// Agent Tool Execution Handlers
// =============================================================================

func (r *ToolRegistry) executeAgentDiscover(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	rootPath := getString(args, "root_path")
	if rootPath == "" {
		return nil, api.NewRequiredFieldError("root_path")
	}

	req := &orchestration.DiscoverProjectsRequest{
		RootPath: rootPath,
	}

	resp, err := agentSvc.DiscoverProjects(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"projects":  resp.Projects,
		"timestamp": time.Unix(0, resp.Timestamp).Format(time.RFC3339Nano),
	})
}

func (r *ToolRegistry) executeAgentRegister(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	name := getString(args, "name")
	if name == "" {
		return nil, api.NewRequiredFieldError("name")
	}

	path := getString(args, "path")
	if path == "" {
		return nil, api.NewRequiredFieldError("path")
	}

	req := &orchestration.RegisterProjectRequest{
		Name:           name,
		Path:           path,
		Dependencies:   getStringSlice(args, "dependencies"),
		DefaultBackend: getString(args, "default_backend"),
		DefaultModel:   getString(args, "default_model"),
	}

	// Parse labels
	if labelsRaw, ok := args["labels"].(map[string]interface{}); ok {
		labels := make(map[string]string, len(labelsRaw))
		for k, v := range labelsRaw {
			if str, isStr := v.(string); isStr {
				labels[k] = str
			}
		}
		req.Labels = labels
	}

	resp, err := agentSvc.RegisterProject(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"project_id": resp.ProjectID,
		"timestamp":  time.Unix(0, resp.Timestamp).Format(time.RFC3339Nano),
	})
}

func (r *ToolRegistry) executeAgentList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	req := &orchestration.ListProjectsRequest{
		PageSize:  getInt(args, "page_size"),
		PageToken: getString(args, "page_token"),
	}

	resp, err := agentSvc.ListProjects(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"projects":        resp.Projects,
		"next_page_token": resp.NextPageToken,
		"timestamp":       time.Unix(0, resp.Timestamp).Format(time.RFC3339Nano),
	})
}

func (r *ToolRegistry) executeAgentSpawn(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	ctx, tenantID, err := sessionTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	backend := getString(args, "backend")
	if backend == "" {
		return nil, api.NewRequiredFieldError("backend")
	}

	projectID := getString(args, "project_id")
	if projectID == "" {
		return nil, api.NewRequiredFieldError("project_id")
	}

	hostAffinity := getString(args, "host_affinity")
	gpuRequired := getBool(args, "gpu_required")

	req := &orchestration.SpawnSessionRequest{
		Backend:            backend,
		ProjectID:          projectID,
		Model:              getString(args, "model"),
		SystemPrompt:       getString(args, "system_prompt"),
		WorkDir:            getString(args, "work_dir"),
		HostAffinity:       hostAffinity,
		HostAffinityStrict: getBool(args, "host_affinity_strict"),
		GpuRequired:        gpuRequired,
		// MCP spawn creates conversational, multi-turn sessions — the same
		// contract POST /api/v1/orchestrator/sessions applies. A one-shot spawn
		// registers no durable session, so a follow-up prompt would fail with
		// "session not found". Callers opt out explicitly with persistent:false.
		Persistent: optionalBool(args, "persistent", true),

		// Pooled provider account selection. account_auto is what makes the
		// dynamic (least-usage) strategy requestable at all.
		AccountID:       getString(args, "account_id"),
		AccountAuto:     getBool(args, "account_auto"),
		AccountStrategy: getString(args, "account_strategy"),
		// account_allow is the second thing that authorizes rotation (the
		// other being account_auto), so a spawn that omits it here would leave
		// an explicitly enumerated pool unreachable from MCP.
		AccountAllow: getStringSlice(args, "account_allow"),
	}

	// sandbox_mode: "default" | "required" | "none". Empty/missing defers
	// to the server-side SandboxPolicy.
	if raw := getString(args, "sandbox_mode"); raw != "" {
		mode, parseErr := orchestration.ParseSandboxMode(raw)
		if parseErr != nil {
			return nil, parseErr
		}
		req.SandboxMode = mode
	}

	// Dispatch hints travel on the context for the remote backend resolver,
	// mirroring the REST spawn handler and the inference tool.
	if hostAffinity != "" {
		ctx = orchestration.WithHostAffinity(ctx, hostAffinity)
	}
	if gpuRequired {
		ctx = orchestration.WithGpuRequired(ctx)
	}
	if req.HostAffinityStrict {
		ctx = orchestration.WithHostAffinityStrict(ctx)
	}

	// Parse MCP servers
	if serversRaw, ok := args["mcp_servers"].([]interface{}); ok {
		servers := make([]orchestration.MCPServerConfig, 0, len(serversRaw))
		for _, raw := range serversRaw {
			if serverMap, isMap := raw.(map[string]interface{}); isMap {
				server := orchestration.MCPServerConfig{
					Name:    getString(serverMap, "name"),
					Command: getString(serverMap, "command"),
					Args:    getStringSlice(serverMap, "args"),
					URL:     getString(serverMap, "url"),
					Env:     getStringMap(serverMap, "env"),
				}
				servers = append(servers, server)
			}
		}
		req.MCPServers = servers
	}

	resp, err := agentSvc.SpawnSession(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"session_id": resp.SessionID,
		"backend":    resp.Backend,
		"model":      resp.Model,
		"tenant_id":  tenantID,
		"persistent": req.Persistent,
		"timestamp":  time.Unix(0, resp.Timestamp).Format(time.RFC3339Nano),
	})
}

// executeAgentPrompt backs the quicbaas_orchestrator_prompt tool.
//
// MCP is strictly request/response — it has no server-push channel — so a
// prompt naming a session returns only the acceptance acknowledgement
// {session_id, turn_id, status, sequence}. The agent's output is NOT returned
// here. An MCP caller observes the turn by polling
// quicbaas_orchestrator_status for the session's state, or by reading the
// transcript with quicbaas_orchestrator_session_history from the returned
// sequence; the turn is finished when a turn_completed or turn_failed event
// carrying the returned turn_id appears in that transcript.
//
// A prompt with no session is a stateless one-shot: it has no event log to
// publish to, so it stays synchronous and the completed turn is returned
// inline under "content".
func (r *ToolRegistry) executeAgentPrompt(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	ctx, _, err = sessionTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	prompt := getString(args, "prompt")
	if prompt == "" {
		return nil, api.NewRequiredFieldError("prompt")
	}

	req := &orchestration.PromptSessionRequest{
		Backend:      getString(args, "backend"),
		SessionID:    getString(args, "session_id"),
		ProjectID:    getString(args, "project_id"),
		Prompt:       prompt,
		Model:        getString(args, "model"),
		SystemPrompt: getString(args, "system_prompt"),
		UseRAG:       getBool(args, "use_rag"),
	}

	if req.SessionID != "" {
		ack, ackErr := agentSvc.PromptSessionAsync(ctx, req)
		if ackErr != nil {
			return nil, ackErr
		}
		return marshalResult(map[string]interface{}{
			"session_id": ack.SessionID,
			"turn_id":    ack.TurnID,
			"status":     ack.Status,
			"sequence":   ack.Sequence,
			"backend":    ack.Backend,
			"model":      ack.Model,
			"timestamp":  time.Unix(0, ack.Timestamp).Format(time.RFC3339Nano),
		})
	}

	resp, err := agentSvc.PromptSession(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"session_id":  resp.SessionID,
		"content":     resp.Content,
		"model":       resp.Model,
		"stop_reason": resp.StopReason,
		"timestamp":   time.Unix(0, resp.Timestamp).Format(time.RFC3339Nano),
	})
}

func (r *ToolRegistry) executeAgentStatus(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	ctx, _, err = sessionTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	sessionID := getString(args, "session_id")
	if sessionID == "" {
		return nil, api.NewRequiredFieldError("session_id")
	}

	req := &orchestration.SessionStatusRequest{
		SessionID: sessionID,
	}

	resp, err := agentSvc.SessionStatus(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"session_id":    resp.SessionID,
		"state":         string(resp.State),
		"last_activity": resp.LastActivity,
		"uptime":        resp.Uptime,
		"timestamp":     time.Unix(0, resp.Timestamp).Format(time.RFC3339Nano),
	})
}

func (r *ToolRegistry) executeAgentClose(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	ctx, _, err = sessionTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	sessionID := getString(args, "session_id")
	if sessionID == "" {
		return nil, api.NewRequiredFieldError("session_id")
	}

	req := &orchestration.CloseSessionRequest{
		SessionID: sessionID,
	}

	resp, err := agentSvc.CloseSession(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"session_id": resp.SessionID,
		"timestamp":  time.Unix(0, resp.Timestamp).Format(time.RFC3339Nano),
	})
}

// executeAgentRotateAccount handles quicbaas_orchestrator_rotate_account: move
// one session onto a different pooled provider account.
//
// It authorizes as orchestrator:spawn (see pkg/api/mcp/authz_qrdb.go) because
// the rotation closes the session's agent process and reconstitutes it against
// a newly selected pooled credential — a spawn's authority, not a close's.
func (r *ToolRegistry) executeAgentRotateAccount(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	rotator, err := orchestration.RotationServiceFrom(agentSvc, "mcp")
	if err != nil {
		return nil, err
	}

	ctx, _, err = sessionTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	sessionID := getString(args, "session_id")
	if sessionID == "" {
		return nil, api.NewRequiredFieldError("session_id")
	}

	availableAt, err := parseRotateAvailableAt(args)
	if err != nil {
		return nil, err
	}

	result, err := rotator.RotateSessionAccount(ctx, &orchestration.RotateSessionAccountRequest{
		SessionID:    sessionID,
		Reason:       getString(args, "reason"),
		HoldOutgoing: getBool(args, "hold_outgoing"),
		AvailableAt:  availableAt,
	})
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"session_id":          result.SessionID,
		"previous_account_id": result.PreviousAccountID,
		"account_id":          result.AccountID,
		"strategy":            result.Strategy,
		"mode":                string(result.Mode),
	})
}

// parseRotateAvailableAt reads the optional RFC 3339 available_at argument. An
// absent or empty value is the zero time, which lets the service apply its own
// default backoff; a malformed one is rejected rather than silently ignored,
// because silently dropping a hold window would leave the exhausted account
// back in the pool immediately.
func parseRotateAvailableAt(args map[string]interface{}) (time.Time, error) {
	raw := getString(args, "available_at")
	if raw == "" {
		return time.Time{}, nil
	}
	parsed, parseErr := time.Parse(time.RFC3339, raw)
	if parseErr != nil {
		return time.Time{}, api.NewInvalidFormatError("available_at", parseErr)
	}
	return parsed.UTC(), nil
}

func (r *ToolRegistry) executeAgentListSessions(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	ctx, _, err = sessionTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	req := &orchestration.ListSessionsRequest{
		Backend: getString(args, "backend"),
	}

	resp, err := agentSvc.ListSessions(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"sessions":  resp.Sessions,
		"timestamp": time.Unix(0, resp.Timestamp).Format(time.RFC3339Nano),
	})
}

func (r *ToolRegistry) executeAgentListBackends(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	backends, err := agentSvc.ListBackends(ctx)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"backends":  backends,
		"timestamp": time.Now().Format(time.RFC3339Nano),
	})
}

func (r *ToolRegistry) executeAgentDecompose(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	backend := getString(args, "backend")
	if backend == "" {
		return nil, api.NewRequiredFieldError("backend")
	}

	task := getString(args, "task")
	if task == "" {
		return nil, api.NewRequiredFieldError("task")
	}

	req := &orchestration.DecomposeTaskRequest{
		Backend:    backend,
		Model:      getString(args, "model"),
		Task:       task,
		ProjectIDs: getStringSlice(args, "project_ids"),
		UseRAG:     getBool(args, "use_rag"),
	}

	resp, err := agentSvc.DecomposeTask(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"work_items": resp.WorkItems,
		"timestamp":  time.Unix(0, resp.Timestamp).Format(time.RFC3339Nano),
	})
}

func (r *ToolRegistry) executeAgentHealth(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	report, err := agentSvc.HealthCheck(ctx)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"total_sessions": report.TotalSessions,
		"healthy":        report.Healthy,
		"unhealthy":      report.Unhealthy,
		"closed":         report.Closed,
		"results":        report.Results,
		"timestamp":      time.Unix(0, report.Timestamp).Format(time.RFC3339Nano),
	})
}

func (r *ToolRegistry) executeAgentBroadcast(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	channel := getString(args, "channel")
	if channel == "" {
		return nil, api.NewRequiredFieldError("channel")
	}

	message := getString(args, "message")
	if message == "" {
		return nil, api.NewRequiredFieldError("message")
	}

	msgService := r.service.Messaging()
	if msgService == nil {
		return nil, api.NewServiceNotConfiguredError("messaging")
	}

	// Build payload with message and optional metadata
	payload := map[string]interface{}{
		"message": message,
	}

	if metadata, ok := args["metadata"].(map[string]interface{}); ok {
		payload["metadata"] = metadata
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, api.NewOperationFailedError("broadcast marshal", err)
	}

	var payloadValue interface{}
	if jsonErr := json.Unmarshal(payloadBytes, &payloadValue); jsonErr != nil {
		payloadValue = string(payloadBytes)
	}

	result, err := msgService.Publish(ctx, channel, payloadValue)
	if err != nil {
		return nil, api.NewOperationFailedError("broadcast", err)
	}

	return map[string]interface{}{
		"published":  true,
		"channel":    channel,
		"message_id": result.MessageID,
		"timestamp":  time.Now().Format(time.RFC3339Nano),
	}, nil
}

// =============================================================================
// Fleet Management Execution Handlers
// =============================================================================

// executeAgentFleetRegister registers an agent with the fleet registry.
func (r *ToolRegistry) executeAgentFleetRegister(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, api.NewRequiredFieldError("tenant_id")
	}

	name := getString(args, "name")
	if name == "" {
		return nil, api.NewRequiredFieldError("name")
	}

	host := getString(args, "host")
	if host == "" {
		return nil, api.NewRequiredFieldError("host")
	}

	agentType := getString(args, "agent_type")
	if agentType == "" {
		return nil, api.NewRequiredFieldError("agent_type")
	}

	req := &orchestration.RegisterAgentRequest{
		TenantID:  tenantID,
		Name:      name,
		Host:      host,
		AgentType: agentType,
		Backend:   getString(args, "backend"),
		WorkDir:   getString(args, "work_dir"),
	}

	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	resp, err := agentSvc.RegisterAgent(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"agent":     resp.Agent,
		"timestamp": time.Now().Format(time.RFC3339Nano),
	})
}

// executeAgentFleetList lists all agents in the fleet for a tenant.
func (r *ToolRegistry) executeAgentFleetList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, api.NewRequiredFieldError("tenant_id")
	}

	filter := &orchestration.AgentFilter{
		Host:      getString(args, "host"),
		AgentType: getString(args, "agent_type"),
	}

	if status := getString(args, "status"); status != "" {
		filter.Status = orchestration.AgentStatus(status)
	}

	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	agents, err := agentSvc.ListAgents(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"agents":    agents,
		"count":     len(agents),
		"timestamp": time.Now().Format(time.RFC3339Nano),
	})
}

// executeAgentFleetStatus retrieves the status of a specific fleet agent.
func (r *ToolRegistry) executeAgentFleetStatus(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, api.NewRequiredFieldError("tenant_id")
	}

	agentID := getString(args, "agent_id")
	if agentID == "" {
		return nil, api.NewRequiredFieldError("agent_id")
	}

	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	agent, err := agentSvc.GetAgent(ctx, tenantID, agentID)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"agent":     agent,
		"timestamp": time.Now().Format(time.RFC3339Nano),
	})
}

// executeAgentFleetRemove removes an agent from the fleet registry.
func (r *ToolRegistry) executeAgentFleetRemove(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, api.NewRequiredFieldError("tenant_id")
	}

	agentID := getString(args, "agent_id")
	if agentID == "" {
		return nil, api.NewRequiredFieldError("agent_id")
	}

	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	err = agentSvc.RemoveAgent(ctx, tenantID, agentID)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"removed":   true,
		"agent_id":  agentID,
		"timestamp": time.Now().Format(time.RFC3339Nano),
	})
}

// executeAgentFleetHeartbeat sends a heartbeat for a fleet agent.
func (r *ToolRegistry) executeAgentFleetHeartbeat(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, api.NewRequiredFieldError("tenant_id")
	}

	agentID := getString(args, "agent_id")
	if agentID == "" {
		return nil, api.NewRequiredFieldError("agent_id")
	}

	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	err = agentSvc.AgentHeartbeat(ctx, tenantID, agentID)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"acknowledged": true,
		"agent_id":     agentID,
		"timestamp":    time.Now().Format(time.RFC3339Nano),
	})
}

// =============================================================================
// Orchestrator Job Execution Handlers
// =============================================================================

// executeOrchestratorStartJob starts an autonomous orchestration job.
func (r *ToolRegistry) executeOrchestratorStartJob(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	description := getString(args, "description")
	if description == "" {
		return nil, api.NewRequiredFieldError("description")
	}

	req := &orchestration.StartJobRequest{
		Description:   description,
		LeadBackend:   getString(args, "lead_backend"),
		LeadModel:     getString(args, "lead_model"),
		ProjectID:     getString(args, "project_id"),
		MaxIterations: getInt(args, "max_iterations"),
	}

	resp, err := agentSvc.StartJob(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"job_id":    resp.JobID,
		"timestamp": time.Unix(resp.Timestamp, 0).Format(time.RFC3339Nano),
	})
}

// executeOrchestratorGetJobStatus returns the current status of an orchestration job.
func (r *ToolRegistry) executeOrchestratorGetJobStatus(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	jobID := getString(args, "job_id")
	if jobID == "" {
		return nil, api.NewRequiredFieldError("job_id")
	}

	tenantID := orchestration.TenantFromContext(ctx)

	jobStatus, err := agentSvc.GetJobStatus(ctx, tenantID, jobID)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"job_id":      jobStatus.JobID,
		"state":       string(jobStatus.State),
		"iteration":   jobStatus.Iteration,
		"task_count":  jobStatus.TaskCount,
		"completed":   jobStatus.Completed,
		"failed":      jobStatus.Failed,
		"description": jobStatus.Description,
		"started_at":  jobStatus.StartedAt,
		"updated_at":  jobStatus.UpdatedAt,
		"error":       jobStatus.Error,
		"timestamp":   time.Now().Format(time.RFC3339Nano),
	})
}

// executeOrchestratorCancelJob cancels a running orchestration job.
func (r *ToolRegistry) executeOrchestratorCancelJob(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	jobID := getString(args, "job_id")
	if jobID == "" {
		return nil, api.NewRequiredFieldError("job_id")
	}

	tenantID := orchestration.TenantFromContext(ctx)

	err = agentSvc.CancelJob(ctx, tenantID, jobID)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"cancelled": true,
		"job_id":    jobID,
		"timestamp": time.Now().Format(time.RFC3339Nano),
	})
}

// executeOrchestratorListJobs lists all orchestration jobs for a tenant.
func (r *ToolRegistry) executeOrchestratorListJobs(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	tenantID := orchestration.TenantFromContext(ctx)

	jobs, err := agentSvc.ListJobs(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	summaries := make([]map[string]interface{}, len(jobs))
	for i, j := range jobs {
		summaries[i] = map[string]interface{}{
			"job_id":      j.JobID,
			"state":       string(j.State),
			"iteration":   j.Iteration,
			"task_count":  j.TaskCount,
			"completed":   j.Completed,
			"failed":      j.Failed,
			"description": j.Description,
			"started_at":  j.StartedAt,
			"updated_at":  j.UpdatedAt,
			"error":       j.Error,
			"event_count": len(j.Events),
		}
	}

	return marshalResult(map[string]interface{}{
		"jobs":      summaries,
		"count":     len(jobs),
		"timestamp": time.Now().Format(time.RFC3339Nano),
	})
}

// executeOrchestratorTaskCreate creates an orchestration task, optionally
// dispatching it as a one-shot job via auto-spawn.
func (r *ToolRegistry) executeOrchestratorTaskCreate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	subject := getString(args, "subject")
	if subject == "" {
		return nil, api.NewRequiredFieldError("subject")
	}

	tenantID := orchestration.TenantFromContext(ctx)

	req := &orchestration.CreateTaskRequest{
		TenantID:    tenantID,
		Subject:     subject,
		Description: getString(args, "description"),
		TeamName:    getString(args, "team_name"),
		AssignedTo:  getString(args, "assigned_to"),
		AutoSpawn:   getBool(args, "auto_spawn"),
		Backend:     getString(args, "backend"),
		GpuRequired: getBool(args, "gpu_required"),
	}

	task, createErr := agentSvc.CreateTask(ctx, req)

	// Always return the task; createErr surfaces no-matching-agent.
	result := map[string]interface{}{
		"task_id":    "",
		"subject":    subject,
		"status":     "",
		"auto_spawn": req.AutoSpawn,
		"timestamp":  time.Now().Format(time.RFC3339Nano),
	}
	if task != nil {
		result["task_id"] = task.ID
		result["status"] = string(task.Status)
		result["spawned_job_id"] = task.SpawnedJobID
	}
	if createErr != nil {
		result["error"] = createErr.Error()
	}

	return marshalResult(result)
}

// executeTaskGetArtifact handles the quicbaas_task_get_artifact MCP tool.
func (r *ToolRegistry) executeTaskGetArtifact(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	taskID := getString(args, "task_id")
	if taskID == "" {
		return nil, api.NewRequiredFieldError("task_id")
	}
	name := getString(args, "name")
	if name == "" {
		return nil, api.NewRequiredFieldError("name")
	}

	tenantID := orchestration.TenantFromContext(ctx)
	artifact, getErr := agentSvc.GetTaskArtifact(ctx, tenantID, taskID, name)
	if getErr != nil {
		return nil, getErr
	}
	if artifact == nil {
		return nil, api.NewNotFoundByNameError("mcp.executeTaskGetArtifact", "artifact", name)
	}

	return marshalResult(map[string]interface{}{
		"task_id":   taskID,
		"name":      artifact.Name,
		"kind":      string(artifact.Kind),
		"mime_type": artifact.MIMEType,
		"size":      artifact.Size,
		"content":   string(artifact.Content),
		"ref":       artifact.Ref,
		"metadata":  artifact.Metadata,
	})
}

// executeTaskListArtifacts handles the quicbaas_task_list_artifacts MCP tool.
func (r *ToolRegistry) executeTaskListArtifacts(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	taskID := getString(args, "task_id")
	if taskID == "" {
		return nil, api.NewRequiredFieldError("task_id")
	}

	tenantID := orchestration.TenantFromContext(ctx)
	artifacts, listErr := agentSvc.ListTaskArtifacts(ctx, tenantID, taskID)
	if listErr != nil {
		return nil, listErr
	}

	items := make([]map[string]interface{}, len(artifacts))
	for i, a := range artifacts {
		items[i] = map[string]interface{}{
			"name":      a.Name,
			"kind":      string(a.Kind),
			"mime_type": a.MIMEType,
			"size":      a.Size,
			"ref":       a.Ref,
			"metadata":  a.Metadata,
		}
	}

	return marshalResult(map[string]interface{}{
		"task_id":   taskID,
		"artifacts": items,
		"count":     len(items),
	})
}

// =============================================================================
// Helper Functions
// =============================================================================

// sessionTenantContext enforces the explicit-tenant contract shared by every
// agent-session tool and stamps the tenant onto the orchestration context.
//
// MCP authenticates with a bearer token (or an mTLS agent certificate), and the
// authorization gate injects the caller's tenant under the iam context key —
// which the orchestrator's tenant guard does not read. Every session tool
// therefore takes an explicit tenant_id argument, exactly as the fleet and
// project tools do, and re-stamps it with orchestration.ContextWithTenant so
// the guard (and the embedded-agent gRPC backend behind it) observes a
// non-empty tenant on every code path.
func sessionTenantContext(ctx context.Context, args map[string]interface{}) (context.Context, string, error) {
	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return ctx, "", api.NewRequiredFieldError("tenant_id")
	}
	return orchestration.ContextWithTenant(ctx, tenantID), tenantID, nil
}

// optionalBool returns the bool argument when the caller supplied the key and
// def when the key is absent. A plain bool decode cannot distinguish an absent
// field from an explicit false, which matters for defaults that are true.
func optionalBool(args map[string]interface{}, key string, def bool) bool {
	if _, present := args[key]; !present {
		return def
	}
	return getBool(args, key)
}

// sessionLogAccessor is the optional interface the agent service satisfies so
// MCP can read the durable session transcript. Declared locally so this file
// has no dependency on the concrete orchestrator implementation, mirroring the
// REST handler's accessor.
type sessionLogAccessor interface {
	SessionEventLog() orchestration.SessionEventLog
}

// defaultSessionHistoryLimit caps an unbounded transcript read. It matches the
// REST history pager so both surfaces page identically.
const defaultSessionHistoryLimit = 256

// executeAgentSessionHistory backs quicbaas_orchestrator_session_history.
//
// It is the MCP replacement for the streaming attach the other transports
// offer: MCP is request/response only, so an MCP caller observes an
// asynchronous turn by paging this transcript from the sequence the prompt
// acknowledgement returned until the turn's terminal event appears.
func (r *ToolRegistry) executeAgentSessionHistory(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	ctx, tenantID, err := sessionTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	sessionID := getString(args, "session_id")
	if sessionID == "" {
		return nil, api.NewRequiredFieldError("session_id")
	}

	accessor, ok := agentSvc.(sessionLogAccessor)
	if !ok {
		return nil, api.NewServiceUnavailableError("session event log")
	}
	log := accessor.SessionEventLog()
	if log == nil {
		return nil, api.NewServiceUnavailableError("session event log")
	}

	limit := getInt(args, "limit")
	if limit <= 0 {
		limit = defaultSessionHistoryLimit
	}

	events, nextSeq, err := log.Range(ctx, tenantID, sessionID, getUint64(args, "from_sequence"), limit)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"session_id": sessionID,
		"tenant_id":  tenantID,
		"events":     events,
		"next_seq":   nextSeq,
	})
}

// getAgentService retrieves the AgentService from the QuicBaaS service.
func (r *ToolRegistry) getAgentService() (orchestration.AgentService, error) {
	agentSvc := r.service.Agent()
	if agentSvc == nil {
		return nil, api.NewServiceUnavailableError("agent")
	}
	return agentSvc, nil
}

// isAgentTool checks if a tool name is an agent tool.
func isAgentTool(name string) bool {
	switch name {
	case "quicbaas_orchestrator_discover",
		"quicbaas_orchestrator_register",
		"quicbaas_orchestrator_list",
		"quicbaas_orchestrator_spawn",
		"quicbaas_orchestrator_prompt",
		"quicbaas_orchestrator_status",
		"quicbaas_orchestrator_session_history",
		"quicbaas_orchestrator_close",
		"quicbaas_orchestrator_rotate_account",
		"quicbaas_orchestrator_list_sessions",
		"quicbaas_orchestrator_list_backends",
		"quicbaas_orchestrator_decompose",
		"quicbaas_orchestrator_health",
		"quicbaas_orchestrator_broadcast",
		"quicbaas_agent_fleet_register",
		"quicbaas_agent_fleet_list",
		"quicbaas_agent_fleet_status",
		"quicbaas_agent_fleet_remove",
		"quicbaas_agent_fleet_heartbeat",
		"quicbaas_orchestrator_start_job",
		"quicbaas_orchestrator_get_job_status",
		"quicbaas_orchestrator_cancel_job",
		"quicbaas_orchestrator_list_jobs",
		"quicbaas_orchestrator_task_create",
		"quicbaas_task_get_artifact",
		"quicbaas_task_list_artifacts",
		"quicbaas_orchestrator_inference":
		return true
	default:
		return false
	}
}

// executeAgentTool dispatches agent tool execution to the appropriate handler.
func (r *ToolRegistry) executeAgentTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	case "quicbaas_orchestrator_discover":
		return r.executeAgentDiscover(ctx, args)
	case "quicbaas_orchestrator_register":
		return r.executeAgentRegister(ctx, args)
	case "quicbaas_orchestrator_list":
		return r.executeAgentList(ctx, args)
	case "quicbaas_orchestrator_spawn":
		return r.executeAgentSpawn(ctx, args)
	case "quicbaas_orchestrator_prompt":
		return r.executeAgentPrompt(ctx, args)
	case "quicbaas_orchestrator_status":
		return r.executeAgentStatus(ctx, args)
	case "quicbaas_orchestrator_session_history":
		return r.executeAgentSessionHistory(ctx, args)
	case "quicbaas_orchestrator_close":
		return r.executeAgentClose(ctx, args)
	case "quicbaas_orchestrator_rotate_account":
		return r.executeAgentRotateAccount(ctx, args)
	case "quicbaas_orchestrator_list_sessions":
		return r.executeAgentListSessions(ctx, args)
	case "quicbaas_orchestrator_list_backends":
		return r.executeAgentListBackends(ctx, args)
	case "quicbaas_orchestrator_decompose":
		return r.executeAgentDecompose(ctx, args)
	case "quicbaas_orchestrator_health":
		return r.executeAgentHealth(ctx, args)
	case "quicbaas_orchestrator_broadcast":
		return r.executeAgentBroadcast(ctx, args)
	case "quicbaas_agent_fleet_register":
		return r.executeAgentFleetRegister(ctx, args)
	case "quicbaas_agent_fleet_list":
		return r.executeAgentFleetList(ctx, args)
	case "quicbaas_agent_fleet_status":
		return r.executeAgentFleetStatus(ctx, args)
	case "quicbaas_agent_fleet_remove":
		return r.executeAgentFleetRemove(ctx, args)
	case "quicbaas_agent_fleet_heartbeat":
		return r.executeAgentFleetHeartbeat(ctx, args)
	case "quicbaas_orchestrator_start_job":
		return r.executeOrchestratorStartJob(ctx, args)
	case "quicbaas_orchestrator_get_job_status":
		return r.executeOrchestratorGetJobStatus(ctx, args)
	case "quicbaas_orchestrator_cancel_job":
		return r.executeOrchestratorCancelJob(ctx, args)
	case "quicbaas_orchestrator_list_jobs":
		return r.executeOrchestratorListJobs(ctx, args)
	case "quicbaas_orchestrator_task_create":
		return r.executeOrchestratorTaskCreate(ctx, args)
	case "quicbaas_task_get_artifact":
		return r.executeTaskGetArtifact(ctx, args)
	case "quicbaas_task_list_artifacts":
		return r.executeTaskListArtifacts(ctx, args)
	case "quicbaas_orchestrator_inference":
		return r.executeOrchestratorInference(ctx, args)
	default:
		return nil, api.NewUnknownToolError(name)
	}
}

// executeOrchestratorInference handles the quicbaas_orchestrator_inference MCP
// tool. It calls agentSvc.Infer, accumulates all chunks, and returns the
// aggregated text content as a JSON result.
func (r *ToolRegistry) executeOrchestratorInference(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	backend := getString(args, "backend")
	if backend == "" {
		return nil, api.NewRequiredFieldError("backend")
	}

	prompt := getString(args, "prompt")
	if prompt == "" {
		return nil, api.NewRequiredFieldError("prompt")
	}

	var metadata map[string]string
	if raw, ok := args["metadata"].(map[string]interface{}); ok {
		metadata = make(map[string]string, len(raw))
		for k, v := range raw {
			if s, isStr := v.(string); isStr {
				metadata[k] = s
			}
		}
	}

	hostAffinity := getString(args, "host_affinity")
	if hostAffinity != "" {
		ctx = orchestration.WithHostAffinity(ctx, hostAffinity)
	}

	gpuRequired := getBool(args, "gpu_required")
	if gpuRequired {
		ctx = orchestration.WithGpuRequired(ctx)
	}

	// MCP tool invocations arrive with a bare context. Propagate the
	// caller's tenant so the orchestrator's RemoteBackendResolver can
	// scope its fleet lookup correctly.
	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		tenantID = qrdbsdk.SystemTenantID
	}
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	inferReq := &orchestration.InferRequest{
		Backend:      backend,
		Model:        getString(args, "model"),
		Prompt:       prompt,
		SystemPrompt: getString(args, "system_prompt"),
		HostAffinity: hostAffinity,
		GpuRequired:  gpuRequired,
		TenantID:     tenantID,
		Metadata:     metadata,
	}

	chunks, err := agentSvc.Infer(ctx, inferReq)
	if err != nil {
		return nil, err
	}

	resp, err := orchestration.AccumulateChunks(chunks)
	if err != nil {
		return nil, err
	}

	// Build a human-readable text summary alongside the structured content.
	var textContent string
	for _, block := range resp.Content {
		if block.Type == "text" {
			textContent += block.Text
		}
	}

	result := map[string]interface{}{
		"content":     resp.Content,
		"text":        textContent,
		"model":       resp.Model,
		"stop_reason": resp.StopReason,
		"timestamp":   time.Now().Format(time.RFC3339),
	}

	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return string(raw), nil
}
