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
	"strings"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
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
		Description: "Spawn a persistent, multi-turn ACP agent session as a tenant Agent (agent_id), from a pool " +
			"(pool_id, failing over along its Agents), or on a raw backend, with project context. " +
			"Returns the session_id used by quicbaas_orchestrator_prompt / _status / _close.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant the session belongs to. Required.",
				},
				"agent_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant Agent the session runs as; it supplies backend, model and image. One of agent_id, pool_id or backend.",
				},
				"pool_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant pool the session runs from, failing over along its Agents. One of agent_id, pool_id or backend.",
				},
				"role": map[string]interface{}{
					"type":        "string",
					"description": "The kind of work the session does (general, coding, review, triage, lead, project_manager) for a pooled Agent's placement policy.",
				},
				"backend": map[string]interface{}{
					"type":        "string",
					"description": "Backend name to spawn the session on (e.g., 'claude', 'ollama'). One of agent_id, pool_id or backend.",
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
				"prompt_append": map[string]interface{}{
					"type":        "string",
					"description": "Prompt material appended after the session's resolved prompt (system_prompt, else the Agent's, else its role's built-in charter). At most 64 KiB. Optional.",
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
					"description": "Pin the session to a specific fleet node daemon by Name or Host. Optional.",
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
				"keep": map[string]interface{}{
					"type":        "boolean",
					"description": "Keep a session bound to no job past the idle bound, which otherwise concludes it. A job-bound session still ends with its job. Optional.",
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
			"required": []string{"tenant_id", "project_id"},
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
		Name: "quicbaas_orchestrator_session_conversation",
		Description: "Read a page of a session's CONVERSATION: who said what, which tools ran, and what each answer cost. " +
			"Coarser than quicbaas_orchestrator_session_history, which pages the raw event log — a streaming answer is many events " +
			"and exactly one turn here, and a page boundary always falls between turns. Use this to read a session; use the event " +
			"transcript to follow one asynchronous turn to its terminal event.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant the session belongs to. Required.",
				},
				"session_id": map[string]interface{}{
					"type":        "string",
					"description": "Session whose conversation to read",
				},
				"cursor": map[string]interface{}{
					"type":        "integer",
					"description": "next_cursor from a previous page (optional, default: start of the retained conversation)",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum turns to return (optional, default: 100, capped at 500)",
				},
			},
			"required": []string{"tenant_id", "session_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name: "quicbaas_orchestrator_spend_policy_get",
		Description: "Read a tenant's own spend limits — sliding windows in tokens and/or list-price dollars over every " +
			"turn charged to the tenant — and its live usage against each window. A tenant with no limits is unmetered.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{"type": "string", "description": "Tenant whose limits to read. Required."},
			},
			"required": []string{"tenant_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name: "quicbaas_orchestrator_spend_policy_set",
		Description: "Replace a tenant's own spend limits. Limits with no windows make the tenant unmetered. " +
			"The limits are stored and reported against the tenant's usage; admission does not enforce them yet.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{"type": "string", "description": "Tenant whose limits to set. Required."},
				"limits":    spendLimitsSchema("The tenant's limits, replacing the stored ones"),
			},
			"required": []string{"tenant_id", "limits"},
		},
	})

	r.addTool(ToolDefinition{
		Name: "quicbaas_orchestrator_cost",
		Description: "Read one scope of the CONSUMPTION LEDGER: the tokens spent under it, their list-price equivalent and " +
			"billed cost, the drill-down to the level below, and the spend policy of every pooled account that paid. The " +
			"attribution chain is a PATH — tenant/project/task/job/session/round — so each level may be supplied only if " +
			"every level above it is. from/to narrow it to a time range. Money is in micro-dollars: consult priced_entries " +
			"and billed_entries before reporting a figure — none priced is UNMEASURED, not free. A tenant-level report " +
			"also carries each subscription's monthly fee pro-rated over the range (subscription_fee_micros).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant whose spend to read. Required.",
				},
				"project_id": map[string]interface{}{
					"type":        "string",
					"description": "Narrow to one project (optional)",
				},
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "Narrow to one task (optional; requires project_id)",
				},
				"job_id": map[string]interface{}{
					"type":        "string",
					"description": "Narrow to one job (optional; requires task_id)",
				},
				"session_id": map[string]interface{}{
					"type":        "string",
					"description": "Narrow to one session (optional; requires job_id)",
				},
				"round": map[string]interface{}{
					"type":        "integer",
					"description": "Narrow to one round (optional; requires session_id)",
				},
				"from": map[string]interface{}{
					"type":        "string",
					"description": "Only turns at or after this RFC 3339 instant (optional)",
				},
				"to": map[string]interface{}{
					"type":        "string",
					"description": "Only turns before this RFC 3339 instant (optional; requires from, which alone ranges up to now)",
				},
				"host": map[string]interface{}{
					"type":        "string",
					"description": "Only turns that ran on this host, as the report's hosts breakdown names it (optional; \"-\" for turns that name none)",
				},
				"agent": map[string]interface{}{
					"type":        "string",
					"description": "Only turns of sessions that ran as this tenant Agent (optional; \"-\" for raw-backend sessions). The report then carries the Agent's own spend limits and usage against each window",
				},
			},
			"required": []string{"tenant_id"},
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
		Name: "quicbaas_orchestrator_list_sessions",
		Description: "List a tenant's agent sessions, optionally filtered by backend and " +
			"lifecycle state. The listing includes durable records, so an unfiltered " +
			"call returns closed sessions too.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": withPageProperties(map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant whose sessions are listed. Required.",
				},
				"backend": map[string]interface{}{
					"type":        "string",
					"description": "Filter sessions by backend name (optional)",
				},
				"status": map[string]interface{}{
					"type":        "string",
					"enum":        orchestration.SessionStates(),
					"description": "Filter sessions by lifecycle state (optional). Empty means every state.",
				},
				"project_id": filterSchemaProperty("Narrow the listing to one project's sessions."),
				"job_id":     filterSchemaProperty("Narrow the listing to the sessions one job spawned."),
				"runner":     filterSchemaProperty("Narrow the listing to the sessions whose job one runner drove — sdlc, review, triage, workflow — which is the ROLE the session played."),
				"agent_id":   filterSchemaProperty("Narrow the listing to the sessions running on one fleet node."),
			}, strings.Join(orchestration.SessionPageSpec().SortFields(), ", ")),
			"required": []string{"tenant_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_list_backends",
		Description: "List all registered agent backends with their capabilities",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": withPageProperties(map[string]interface{}{}, strings.Join(orchestration.BackendPageSpec().SortFields(), ", ")),
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_list_backend_models",
		Description: "List the models one agent backend offers, as seen from a fleet node",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"backend_name": map[string]interface{}{
					"type":        "string",
					"description": "Backend name (e.g. ollama, claude)",
				},
				"node_id": map[string]interface{}{
					"type":        "string",
					"description": "Fleet node to ask; empty or \"any\" lets the resolver choose",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID (defaults to the caller's tenant)",
				},
			},
			"required": []string{"backend_name"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_list_runners",
		Description: "List the registered job runners and the ordered states each drives a job through",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": withPageProperties(map[string]interface{}{}, strings.Join(orchestration.RunnerPageSpec().SortFields(), ", ")),
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
		Name:        "quicbaas_fleet_register",
		Description: "Register a fleet node for tracking and management",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Fleet node name",
				},
				"host": map[string]interface{}{
					"type":        "string",
					"description": "Host address",
				},
				"node_type": map[string]interface{}{
					"type":        "string",
					"description": "Fleet node type (e.g. agent-runtime, embedded)",
				},
				"backend": map[string]interface{}{
					"type":        "string",
					"description": "Backend type (acp, ollama)",
				},
				"model": map[string]interface{}{
					"type":        "string",
					"description": "Default model the node runs. Optional.",
				},
				"work_dir": map[string]interface{}{
					"type":        "string",
					"description": "Working directory",
				},
				"metadata": map[string]interface{}{
					"type":                 "object",
					"description":          "String metadata carried on the node. Optional.",
					"additionalProperties": map[string]interface{}{"type": "string"},
				},
			},
			"required": []string{"tenant_id", "name", "host", "node_type"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_fleet_list",
		Description: "List every fleet node registered for a tenant, with optional filters",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": withPageProperties(map[string]interface{}{
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
					"description": "Filter by fleet node status (optional)",
				},
				"node_type": map[string]interface{}{
					"type":        "string",
					"description": "Filter by fleet node type (optional)",
				},
				"backend": filterSchemaProperty("Narrow the listing to the fleet nodes that SERVE this backend, as their primary or as one of their capabilities."),
			}, strings.Join(orchestration.FleetNodePageSpec().SortFields(), ", ")),
			"required": []string{"tenant_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_fleet_status",
		Description: "Get the status of one fleet node",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"node_id": map[string]interface{}{
					"type":        "string",
					"description": "Fleet node ID",
				},
			},
			"required": []string{"tenant_id", "node_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_fleet_remove",
		Description: "Remove a fleet node from the fleet registry",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"node_id": map[string]interface{}{
					"type":        "string",
					"description": "Fleet node ID",
				},
			},
			"required": []string{"tenant_id", "node_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_fleet_heartbeat",
		Description: "Send a heartbeat for a fleet node to update its last-seen timestamp",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"node_id": map[string]interface{}{
					"type":        "string",
					"description": "Fleet node ID",
				},
				"observation": map[string]interface{}{
					"type": "object",
					"description": "What the node reports about itself: backends (required for the observation to count), " +
						"max_concurrent_jobs, signing_agents, signing_relay, images, devices and host_metrics. " +
						"Each reading is {value, provenance}; a reading without a measured or derived provenance is unread, never zero. " +
						"Omit for a bare heartbeat that reconciles nothing.",
				},
			},
			"required": []string{"tenant_id", "node_id"},
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
				"lead": agentRefSchema("The Agent or pool the lead runs as (optional; unset takes the project's default lead)."),
				"project_id": map[string]interface{}{
					"type":        "string",
					"description": "Project ID to scope the orchestration job (optional)",
				},
				"max_iterations": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of orchestration iterations (optional, default: 10)",
				},
				"runner": map[string]interface{}{
					"type":        "string",
					"description": "Job runner that drives the job's states (optional, default: workflow)",
				},
				"prompt_append": map[string]interface{}{
					"type":        "string",
					"description": "Prompt material appended to every session the job spawns, the lead's included. At most 64 KiB. Optional.",
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
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Optional; defaults to the caller's tenant.",
				},
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
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Optional; defaults to the caller's tenant.",
				},
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
		Description: "List one page of the current tenant's orchestration jobs, newest first, optionally narrowed by task, runner, project or state",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": withPageProperties(map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Optional; defaults to the caller's tenant.",
				},
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "Narrow the listing to the attempts at one task. Optional; omit to list every job.",
				},
				"runner": map[string]interface{}{
					"type":        "string",
					"description": "Narrow the listing to the jobs one runner drove (sdlc, triage, review). Optional.",
				},
				"project_id": map[string]interface{}{
					"type":        "string",
					"description": "Narrow the listing to one project's jobs. Optional.",
				},
				"statuses": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Narrow the listing to jobs in any of these states. Optional.",
				},
				"job_ids": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Narrow the listing to the named jobs — how a caller holding a page of sessions or tasks resolves the jobs they name in one call. Optional.",
				},
				"agent_id": filterSchemaProperty("Narrow the listing to the jobs dispatched to one fleet node."),
			}, strings.Join(orchestration.JobPageSpec().SortFields(), ", ")),
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_list_tasks",
		Description: "List one page of the current tenant's orchestration tasks, optionally narrowed by status, team, assignee, project, agent, job or source",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": withPageProperties(map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Optional; defaults to the caller's tenant.",
				},
				"status":      filterSchemaProperty("Narrow the listing to tasks in one status."),
				"team_name":   filterSchemaProperty("Narrow the listing to one team's tasks."),
				"assigned_to": filterSchemaProperty("Narrow the listing to the tasks assigned to one agent."),
				"created_by":  filterSchemaProperty("Narrow the listing to the tasks one principal created."),
				"project_id":  filterSchemaProperty("Narrow the listing to one project's tasks."),
				"statuses": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Narrow the listing to tasks in any of these statuses. Optional.",
				},
				"auto_spawn": map[string]interface{}{
					"type":        "boolean",
					"description": "Narrow the listing to the tasks that asked for immediate dispatch (true) or to the ones queued for a claim (false). Optional.",
				},
				"agent_id":    filterSchemaProperty("Narrow the listing to the tasks whose latest attempt ran on one fleet node."),
				"job_id":      filterSchemaProperty("Narrow the listing to the task whose latest attempt ran as one job."),
				"source_kind": filterSchemaProperty("With source_ref, narrow the listing to the task holding one source reservation in project_id's scope."),
				"source_ref":  filterSchemaProperty("With source_kind, the source reference the task holds."),
			}, strings.Join(orchestration.TaskPageSpec().SortFields(), ", ")),
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_task_get",
		Description: "Get one orchestration task by ID",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Optional; defaults to the caller's tenant.",
				},
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "ID of the task. Required.",
				},
			},
			"required": []string{"task_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_task_claim",
		Description: "Claim a pending orchestration task for a fleet node to execute",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Optional; defaults to the caller's tenant.",
				},
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "ID of the task. Required.",
				},
				"agent_id": map[string]interface{}{
					"type":        "string",
					"description": "Fleet node claiming the task. Required.",
				},
			},
			"required": []string{"task_id", "agent_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_task_complete",
		Description: "Mark an orchestration task completed, optionally recording its result",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Optional; defaults to the caller's tenant.",
				},
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "ID of the task. Required.",
				},
				"result": map[string]interface{}{
					"type":        "object",
					"description": "The task's result: {output, files, metadata}. Optional.",
				},
			},
			"required": []string{"task_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_task_submit_artifacts",
		Description: "Store typed output artifacts on an orchestration task, as POST /api/v1/orchestrator/tasks/{taskID}/artifacts does",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Optional; defaults to the caller's tenant.",
				},
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "ID of the task. Required.",
				},
				"artifacts": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "object"},
					"description": "Artifacts {kind, name, mime_type, size, content, ref, metadata}; inline content is base64, as on REST. Required.",
				},
			},
			"required": []string{"task_id", "artifacts"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_spawn_child_job",
		Description: "Spawn a child job under a parent job, from a workflow or inline task specs; the caller must lead the parent",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Optional; defaults to the caller's tenant.",
				},
				"parent_job_id": map[string]interface{}{
					"type":        "string",
					"description": "The job whose lead agent is spawning the child. Required.",
				},
				"caller_agent_id": filterSchemaProperty("Fleet node ID of the spawning lead."),
				"workflow_id":     filterSchemaProperty("An existing workflow definition for the child's task plan; exclusive with tasks."),
				"project_id":      filterSchemaProperty("Project the child job is scoped to."),
				"tasks": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "object"},
					"description": "Inline task specs when no workflow is named. Optional.",
				},
				"host_affinity": filterSchemaProperty("Pin the child to one fleet node daemon (name or host)."),
				"gpu_required": map[string]interface{}{
					"type":        "boolean",
					"description": "Dispatch only to GPU-capable daemons. Optional.",
				},
				"host_affinity_strict": map[string]interface{}{
					"type":        "boolean",
					"description": "Treat host_affinity as a hard constraint. Optional.",
				},
			},
			"required": []string{"parent_job_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_list_workflows",
		Description: "List one page of the current tenant's workflow definitions, optionally narrowed by project or mode",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": withPageProperties(map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Optional; defaults to the caller's tenant.",
				},
				"project_id": filterSchemaProperty("Narrow the listing to one project's workflows."),
				"mode":       filterSchemaProperty("Narrow the listing to one workflow mode."),
			}, strings.Join(orchestration.WorkflowPageSpec().SortFields(), ", ")),
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_get_workflow",
		Description: "Get one of the current tenant's workflow definitions by name",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Optional; defaults to the caller's tenant.",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Workflow name. Required.",
				},
			},
			"required": []string{"name"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_create_workflow",
		Description: "Validate and register a new workflow definition for the current tenant",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Optional; defaults to the caller's tenant.",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Workflow name. Optional; overrides the definition's name when set.",
				},
				"definition": map[string]interface{}{
					"type":        "object",
					"description": "The workflow definition: name, description, mode, agents, settings. Required.",
				},
			},
			"required": []string{"definition"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_update_workflow",
		Description: "Replace an existing workflow definition of the current tenant by name",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Optional; defaults to the caller's tenant.",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Name of the workflow to replace; overrides the definition's name. Required.",
				},
				"definition": map[string]interface{}{
					"type":        "object",
					"description": "The replacement workflow definition. Required.",
				},
			},
			"required": []string{"name", "definition"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_delete_workflow",
		Description: "Remove one of the current tenant's workflow definitions by name",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Optional; defaults to the caller's tenant.",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Workflow name. Required.",
				},
			},
			"required": []string{"name"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_task_create",
		Description: "Create an orchestration task, optionally dispatching it immediately as a one-shot job on the best-matching agent",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Optional; defaults to the caller's tenant.",
				},
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
				"host_affinity": map[string]interface{}{
					"type":        "string",
					"description": "Pin auto-spawn to a specific fleet node daemon by Name or Host. Ignored when auto_spawn is false. Optional.",
				},
				"runner": map[string]interface{}{
					"type":        "string",
					"description": "JobRunner that drives an attempt at the task. Empty means the engine's lead-driven 'workflow' runner; an unregistered name is refused. Optional.",
				},
				"prompt_append": map[string]interface{}{
					"type":        "string",
					"description": "Prompt material every attempt at the task carries to the sessions it spawns (the worker's, for a module runner). At most 64 KiB. Optional.",
				},
				"project_id": map[string]interface{}{
					"type":        "string",
					"description": "The project the task belongs to, by ID (never its name). Optional.",
				},
				"source": map[string]interface{}{
					"type":        "object",
					"description": "Where the task came from, e.g. {kind: forge_issue, ref: owner/repo#N, url}. A source is held by one live task; a finished holder is superseded. Optional.",
					"properties": map[string]interface{}{
						"kind": map[string]interface{}{"type": "string"},
						"ref":  map[string]interface{}{"type": "string"},
						"url":  map[string]interface{}{"type": "string"},
					},
				},
				"metadata": map[string]interface{}{
					"type":                 "object",
					"description":          "String metadata carried on the task. Optional.",
					"additionalProperties": map[string]interface{}{"type": "string"},
				},
				"dependencies": map[string]interface{}{
					"type":        "array",
					"description": "Task IDs this task waits on. Optional.",
					"items":       map[string]interface{}{"type": "string"},
				},
				"priority": map[string]interface{}{
					"type":        "integer",
					"description": "Dispatch priority; higher runs first. Optional.",
				},
				"attempt_budget": map[string]interface{}{
					"type":        "integer",
					"description": "How many attempts the task may spend; zero takes the default. Optional.",
				},
				"park": map[string]interface{}{
					"type":        "object",
					"description": "Create the task already parked for a person: {kind: needs_info|blocked|escalated, message}. Optional.",
					"properties": map[string]interface{}{
						"kind":    map[string]interface{}{"type": "string"},
						"message": map[string]interface{}{"type": "string"},
					},
				},
			},
			"required": []string{"subject"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_task_block",
		Description: "Park an orchestration task until a person readmits it. The kind says why (needs_info, blocked, escalated) and the reason is surfaced to whoever it waits for.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Optional; defaults to the caller's tenant.",
				},
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "ID of the task. Required.",
				},
				"kind": map[string]interface{}{
					"type":        "string",
					"description": "Why the task is parked: needs_info, blocked or escalated. Required.",
					"enum":        []string{"needs_info", "blocked", "escalated"},
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Human-readable reason. Required.",
				},
			},
			"required": []string{"task_id", "kind", "reason"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_task_readmit",
		Description: "Return a blocked orchestration task to the pending queue, clearing its block, assignment and retry hold.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Optional; defaults to the caller's tenant.",
				},
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "ID of the task. Required.",
				},
			},
			"required": []string{"task_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_attention_list",
		Description: "List the attention inbox: every blocked task plus environmental alerts (agent offline, pooled credential erroring, barrier sealed, project idle) — everything only an operator can clear. Paged and faceted by project and kind.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Required.",
				},
				"project_id": map[string]interface{}{
					"type":        "string",
					"description": "Restrict the listing to one project. Optional.",
				},
				"kind": map[string]interface{}{
					"type":        "string",
					"description": "Restrict the listing to one attention kind. Optional.",
					"enum":        []string{"parked_task", "agent_offline", "account_error", "barrier_sealed", "project_idle"},
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Page size. Optional; server default 25, max 100.",
				},
				"offset": map[string]interface{}{
					"type":        "integer",
					"description": "How many records to skip. Optional.",
				},
				"sort": map[string]interface{}{
					"type":        "string",
					"description": "\"<field>\" or \"<field>:desc\". Optional.",
				},
				"q": map[string]interface{}{
					"type":        "string",
					"description": "Case-insensitive substring search. Optional.",
				},
			},
			"required": []string{"tenant_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_attention_resolve",
		Description: "Apply an operator's decision to one blocked task from the attention inbox: the ruling is posted to the task's forge issue, then the task is readmitted to the queue with its issue handed back to the factory (park labels off, trigger label on), closed and completed without being readmitted, park labels off (close_as_done), or left where it is (note). A note alone is also accepted on a live task that is not parked (pending, claimed, in progress): it posts the ruling and changes no state.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Required.",
				},
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "ID of the blocked task. Required.",
				},
				"action": map[string]interface{}{
					"type":        "string",
					"description": "The resolution: approve_recommendation, answer, allow_another_round, close_as_done, readmit, note. Required.",
					"enum":        []string{"approve_recommendation", "answer", "allow_another_round", "close_as_done", "readmit", "note"},
				},
				"answer": map[string]interface{}{
					"type":        "string",
					"description": "Free text ruling. Required for \"answer\" and \"note\"; optional context for \"close_as_done\" and optional round scope for \"allow_another_round\"; ignored otherwise.",
				},
			},
			"required": []string{"tenant_id", "task_id", "action"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_orchestrator_task_delete",
		Description: "Delete an orchestration task, its artifacts, its claim mark and its source reservation, so the issue or ticket it was adopted from can be adopted again. Refused while a job holds a live lease on the task.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Optional; defaults to the caller's tenant.",
				},
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "ID of the task. Required.",
				},
			},
			"required": []string{"task_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_task_get_artifact",
		Description: "Retrieve a named output artifact from a task. For kv_key artifacts the content is resolved from the KV store.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Optional; defaults to the caller's tenant.",
				},
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
			"properties": withPageProperties(map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID. Optional; defaults to the caller's tenant.",
				},
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "ID of the task. Required.",
				},
			}, strings.Join(orchestration.ArtifactPageSpec().SortFields(), ", ")),
			"required": []string{"task_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name: "quicbaas_orchestrator_inference",
		Description: "Run a stateless inference call against a fleet-resident fleet node daemon. " +
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
					"description": "Pin the call to a specific fleet node daemon by Name or Host. Optional.",
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
	agentID, poolID := getString(args, "agent_id"), getString(args, "pool_id")
	if backend == "" && agentID == "" && poolID == "" {
		return nil, api.NewRequiredFieldError("agent_id, pool_id or backend")
	}

	projectID := getString(args, "project_id")
	if projectID == "" {
		return nil, api.NewRequiredFieldError("project_id")
	}

	hostAffinity := getString(args, "host_affinity")
	gpuRequired := getBool(args, "gpu_required")

	req := &orchestration.SpawnSessionRequest{
		AgentID:            agentID,
		PoolID:             poolID,
		Role:               orchestration.AgentRole(getString(args, "role")),
		Backend:            backend,
		ProjectID:          projectID,
		Model:              getString(args, "model"),
		SystemPrompt:       getString(args, "system_prompt"),
		PromptAppend:       getString(args, "prompt_append"),
		WorkDir:            getString(args, "work_dir"),
		HostAffinity:       hostAffinity,
		HostAffinityStrict: getBool(args, "host_affinity_strict"),
		GpuRequired:        gpuRequired,
		// MCP spawn creates conversational, multi-turn sessions — the same
		// contract POST /api/v1/orchestrator/sessions applies. A one-shot spawn
		// registers no durable session, so a follow-up prompt would fail with
		// "session not found". Callers opt out explicitly with persistent:false.
		Persistent: optionalBool(args, "persistent", true),
		Keep:       getBool(args, "keep"),

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
	return parseOptionalInstant(args, "available_at")
}

// parseOptionalInstant reads an optional RFC 3339 argument: absent or empty is
// the zero time, and a malformed value is refused rather than ignored.
func parseOptionalInstant(args map[string]interface{}, name string) (time.Time, error) {
	raw := getString(args, name)
	if raw == "" {
		return time.Time{}, nil
	}
	parsed, parseErr := time.Parse(time.RFC3339Nano, raw)
	if parseErr != nil {
		return time.Time{}, api.NewInvalidFormatError(name, parseErr)
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
		Backend:   getString(args, "backend"),
		Status:    orchestration.SessionState(getString(args, "status")),
		ProjectID: getString(args, "project_id"),
		JobID:     getString(args, "job_id"),
		Runner:    getString(args, "runner"),
		AgentID:   getString(args, "agent_id"),
		Page:      pageFromArgs(args),
	}

	resp, err := agentSvc.ListSessions(ctx, req)
	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{
		"sessions":  resp.Sessions,
		"timestamp": time.Unix(0, resp.Timestamp).Format(time.RFC3339Nano),
		"total":     resp.Total,
		"limit":     resp.Limit,
		"offset":    resp.Offset,
	}
	if resp.NextOffset > 0 {
		result["next_offset"] = resp.NextOffset
	}
	if resp.Facets != nil {
		result["facets"] = resp.Facets
	}
	return marshalResult(result)
}

func (r *ToolRegistry) executeAgentListBackends(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	result, err := orchestration.ListBackendsPage(ctx, agentSvc, pageFromArgs(args))
	if err != nil {
		return nil, err
	}

	return marshalResult(pageResultFields(result, map[string]interface{}{
		"backends":  result.Items,
		"timestamp": time.Now().Format(time.RFC3339Nano),
	}))
}

// executeOrchestratorListBackendModels handles
// quicbaas_orchestrator_list_backend_models from the same service call as
// GET /api/v1/fleet/{nodeID}/backends/{backendName}/models. A node_id other
// than "any" is threaded as host affinity, so the remote backend resolver asks
// that node. The tenant is the caller's own unless tenant_id names one it may
// target.
func (r *ToolRegistry) executeOrchestratorListBackendModels(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	backendName := getString(args, "backend_name")
	if backendName == "" {
		return nil, api.NewRequiredFieldError("backend_name")
	}

	ctx, _, err = orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}
	if nodeID := getString(args, "node_id"); nodeID != "" && nodeID != "any" {
		ctx = orchestration.WithHostAffinity(ctx, nodeID)
	}

	models, err := agentSvc.ListBackendModels(ctx, backendName)
	if err != nil {
		return nil, err
	}
	if models == nil {
		models = []string{}
	}

	return marshalResult(map[string]interface{}{
		"backend": backendName,
		"models":  models,
		"count":   len(models),
	})
}

// executeOrchestratorListRunners answers quicbaas_orchestrator_list_runners.
//
// The runner list is REGISTRY data — the same for every caller, so no tenant is
// derived — but it is still gated on the agent service being present, matching
// the REST and embedded handlers: a node that runs no orchestrator drives no
// jobs, and answering "these are the runners" there would describe a capability
// the node does not have.
func (r *ToolRegistry) executeOrchestratorListRunners(_ context.Context, args map[string]interface{}) (interface{}, error) {
	if _, err := r.getAgentService(); err != nil {
		return nil, err
	}
	result, err := orchestration.ListJobRunnersPage(pageFromArgs(args))
	if err != nil {
		return nil, err
	}
	runners := make([]map[string]interface{}, 0, len(result.Items))
	for _, runner := range result.Items {
		states := make([]map[string]interface{}, 0, len(runner.States))
		for _, spec := range runner.States {
			states = append(states, map[string]interface{}{
				"state":          string(spec.State),
				"label":          spec.Label,
				"session_backed": spec.SessionBacked,
			})
		}
		runners = append(runners, map[string]interface{}{
			"name":   runner.Name,
			"states": states,
		})
	}
	return marshalResult(pageResultFields(result, map[string]interface{}{
		"runners": runners,
		"count":   len(runners),
	}))
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

// executeFleetRegister registers a node in the fleet registry.
func (r *ToolRegistry) executeFleetRegister(ctx context.Context, args map[string]interface{}) (interface{}, error) {
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

	nodeType := getString(args, "node_type")
	if nodeType == "" {
		return nil, api.NewRequiredFieldError("node_type")
	}

	req := &orchestration.RegisterFleetNodeRequest{
		TenantID: tenantID,
		Name:     name,
		Host:     host,
		NodeType: nodeType,
		Backend:  getString(args, "backend"),
		Model:    getString(args, "model"),
		WorkDir:  getString(args, "work_dir"),
		Metadata: getStringMap(args, "metadata"),
	}

	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	resp, err := agentSvc.RegisterFleetNode(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"node":      resp.Node,
		"timestamp": time.Now().Format(time.RFC3339Nano),
	})
}

// executeFleetList lists every fleet node for a tenant.
func (r *ToolRegistry) executeFleetList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, api.NewRequiredFieldError("tenant_id")
	}

	filter := &orchestration.FleetNodeFilter{
		Host:     getString(args, "host"),
		NodeType: getString(args, "node_type"),
		Backend:  getString(args, "backend"),
	}

	if status := getString(args, "status"); status != "" {
		filter.Status = orchestration.FleetNodeStatus(status)
	}

	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	nodes, err := agentSvc.ListFleetNodesPage(ctx, tenantID, filter, pageFromArgs(args))
	if err != nil {
		return nil, err
	}

	return marshalResult(pageResultFields(nodes, map[string]interface{}{
		"nodes":     orchestration.FleetNodeViews(nodes.Items),
		"count":     len(nodes.Items),
		"timestamp": time.Now().Format(time.RFC3339Nano),
	}))
}

// executeFleetStatus retrieves the status of one fleet node.
func (r *ToolRegistry) executeFleetStatus(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, api.NewRequiredFieldError("tenant_id")
	}

	nodeID := getString(args, "node_id")
	if nodeID == "" {
		return nil, api.NewRequiredFieldError("node_id")
	}

	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	node, err := agentSvc.GetFleetNode(ctx, tenantID, nodeID)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"node":      orchestration.NewFleetNodeView(node),
		"timestamp": time.Now().Format(time.RFC3339Nano),
	})
}

// executeFleetRemove removes a node from the fleet registry.
func (r *ToolRegistry) executeFleetRemove(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, api.NewRequiredFieldError("tenant_id")
	}

	nodeID := getString(args, "node_id")
	if nodeID == "" {
		return nil, api.NewRequiredFieldError("node_id")
	}

	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	err = agentSvc.RemoveFleetNode(ctx, tenantID, nodeID)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"removed":   true,
		"node_id":   nodeID,
		"timestamp": time.Now().Format(time.RFC3339Nano),
	})
}

// executeFleetHeartbeat sends a heartbeat for a fleet node.
func (r *ToolRegistry) executeFleetHeartbeat(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, api.NewRequiredFieldError("tenant_id")
	}

	nodeID := getString(args, "node_id")
	if nodeID == "" {
		return nil, api.NewRequiredFieldError("node_id")
	}

	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	// The observation decodes through the same report a REST body does, so
	// an absent observation reports nothing and reconciles nothing.
	observed, err := orchestration.DecodeFleetHeartbeatReport(args["observation"])
	if err != nil {
		return nil, err
	}
	err = agentSvc.FleetNodeHeartbeat(ctx, tenantID, nodeID, observed)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"acknowledged": true,
		"node_id":      nodeID,
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
		Lead:          getAgentRef(args, "lead"),
		ProjectID:     getString(args, "project_id"),
		MaxIterations: getInt(args, "max_iterations"),
		Runner:        getString(args, "runner"),
		PromptAppend:  getString(args, "prompt_append"),
	}

	resp, err := agentSvc.StartJob(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"job_id":    resp.JobID,
		"timestamp": time.Unix(0, resp.Timestamp).UTC().Format(time.RFC3339Nano),
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

	ctx, tenantID, err := orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	jobStatus, err := agentSvc.GetJobStatus(ctx, tenantID, jobID)
	if err != nil {
		return nil, err
	}

	status := map[string]interface{}{
		"job_id":          jobStatus.JobID,
		"state":           string(jobStatus.State),
		"iteration":       jobStatus.Iteration,
		"task_count":      jobStatus.TaskCount,
		"completed":       jobStatus.Completed,
		"failed":          jobStatus.Failed,
		"description":     jobStatus.Description,
		"started_at":      jobStatus.StartedAt,
		"updated_at":      jobStatus.UpdatedAt,
		"error":           jobStatus.Error,
		"task_id":         jobStatus.TaskID,
		"attempt":         jobStatus.Attempt,
		"node":            jobStatus.Node,
		"heartbeat_at":    jobStatus.HeartbeatAt,
		"failure_class":   jobStatus.FailureClass,
		"lead_session_id": jobStatus.LeadSessionID,
		"session_ids":     jobStatus.SessionIDs,
		"stage":           jobStatus.Stage,
		"runner":          jobStatus.Runner,

		// Provenance: the same facts the REST and gRPC job shapes carry.
		// The domain types serialize themselves — a hand-rolled map per
		// nested shape is a second wire format waiting to drift.
		"agent_id":        jobStatus.AgentID,
		"stages":          jobStatus.Stages,
		"attempts":        jobStatus.Attempts,
		"attempt_budget":  jobStatus.AttemptBudget,
		"terminal_reason": string(jobStatus.TerminalReason),
		"readmit_allowed": jobStatus.ReadmitAllowed,
		"delivery":        jobStatus.Delivery,
		"verification":    jobStatus.Verification,

		"timestamp": time.Now().Format(time.RFC3339Nano),
	}

	// The forge issue behind the job's task, on the detail read exactly as on
	// quicbaas_orchestrator_list_jobs: the two must agree about the same job,
	// or a model that reads one and then the other sees the issue disappear.
	// It is OMITTED rather than emitted empty, which is the listing's rule and
	// the rest of this field's contract — absent means "no issue recorded",
	// never "the issue is blank".
	if jobStatus.Source != nil {
		status["source"] = jobStatus.Source
	}

	return marshalResult(status)
}

// executeOrchestratorTaskBlock handles the quicbaas_orchestrator_task_block tool.
func (r *ToolRegistry) executeOrchestratorTaskBlock(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	taskID := getString(args, "task_id")
	if taskID == "" {
		return nil, api.NewRequiredFieldError("task_id")
	}
	kind := getString(args, "kind")
	if kind == "" {
		return nil, api.NewRequiredFieldError("kind")
	}
	reason := getString(args, "reason")
	if reason == "" {
		return nil, api.NewRequiredFieldError("reason")
	}

	ctx, tenantID, err := orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	if err := agentSvc.BlockTask(ctx, tenantID, taskID, orchestration.TaskBlockKind(kind), reason); err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"task_id":   taskID,
		"kind":      kind,
		"blocked":   true,
		"timestamp": time.Now().Format(time.RFC3339Nano),
	})
}

// executeOrchestratorTaskReadmit handles the quicbaas_orchestrator_task_readmit tool.
func (r *ToolRegistry) executeOrchestratorTaskReadmit(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	taskID := getString(args, "task_id")
	if taskID == "" {
		return nil, api.NewRequiredFieldError("task_id")
	}

	ctx, tenantID, err := orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	if err := agentSvc.ReadmitTask(ctx, tenantID, taskID); err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"task_id":    taskID,
		"readmitted": true,
		"timestamp":  time.Now().Format(time.RFC3339Nano),
	})
}

// executeOrchestratorAttentionList handles the
// quicbaas_orchestrator_attention_list tool.
func (r *ToolRegistry) executeOrchestratorAttentionList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getSDLCService()
	if err != nil {
		return nil, err
	}
	attention := svc.Attention()
	if attention == nil {
		return nil, api.NewServiceUnavailableError("sdlc")
	}

	ctx, tenantID, err := orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}
	filter := sdlc.AttentionFilter{
		ProjectID: getString(args, "project_id"),
		Kind:      sdlc.AttentionKind(getString(args, "kind")),
	}
	page := &orchestration.Page{
		Limit: getInt(args, "limit"), Offset: getInt(args, "offset"),
		Sort: getString(args, "sort"), Query: getString(args, "q"),
	}

	result, err := attention.List(ctx, tenantID, filter, page)
	if err != nil {
		return nil, err
	}
	envelope := result.Envelope("items")
	envelope["count"] = len(result.Items)
	return marshalResult(envelope)
}

// executeOrchestratorAttentionResolve handles the
// quicbaas_orchestrator_attention_resolve tool.
func (r *ToolRegistry) executeOrchestratorAttentionResolve(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getSDLCService()
	if err != nil {
		return nil, err
	}
	attention := svc.Attention()
	if attention == nil {
		return nil, api.NewServiceUnavailableError("sdlc")
	}

	taskID := getString(args, "task_id")
	if taskID == "" {
		return nil, api.NewRequiredFieldError("task_id")
	}
	action := getString(args, "action")
	if action == "" {
		return nil, api.NewRequiredFieldError("action")
	}

	ctx, tenantID, err := orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}
	item, err := attention.Resolve(ctx, tenantID, taskID, sdlc.AttentionResolution{
		Action: sdlc.AttentionAction(action), Answer: getString(args, "answer"),
	})
	if err != nil {
		return nil, err
	}
	return marshalResult(item)
}

// executeOrchestratorTaskDelete handles the quicbaas_orchestrator_task_delete tool.
func (r *ToolRegistry) executeOrchestratorTaskDelete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	taskID := getString(args, "task_id")
	if taskID == "" {
		return nil, api.NewRequiredFieldError("task_id")
	}

	ctx, tenantID, err := orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	if err := agentSvc.DeleteTask(ctx, tenantID, taskID); err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"task_id":   taskID,
		"deleted":   true,
		"timestamp": time.Now().Format(time.RFC3339Nano),
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

	ctx, tenantID, err := orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

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

// executeOrchestratorListJobs lists the tenant's orchestration jobs, narrowed
// to one task when the caller names one.
func (r *ToolRegistry) executeOrchestratorListJobs(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	ctx, tenantID, err := orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	// A STATE THE ENGINE DOES NOT HAVE IS REFUSED HERE TOO. The model sends
	// the same names REST does and must meet the same refusal, or a tool call
	// that misspells a state reads back as "no such jobs" and the model
	// reports a quiet factory.
	states, err := orchestration.JobStatesFrom("statuses", getStringSlice(args, "statuses"))
	if err != nil {
		return nil, err
	}

	jobs, err := agentSvc.ListJobsPage(ctx, tenantID, &orchestration.JobFilter{
		TaskID:    getString(args, "task_id"),
		Runner:    getString(args, "runner"),
		ProjectID: getString(args, "project_id"),
		AgentID:   getString(args, "agent_id"),
		Statuses:  states,
		JobIDs:    getStringSlice(args, "job_ids"),
	}, pageFromArgs(args))
	if err != nil {
		return nil, err
	}

	summaries := make([]map[string]interface{}, len(jobs.Items))
	for i, j := range jobs.Items {
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
			"task_id":     j.TaskID,
			"attempt":     j.Attempt,
		}
		// The forge issue behind the job, present only when the job is an
		// attempt at a task that records one. It is omitted rather than
		// emitted empty so a reader can tell "no issue" from "issue unknown".
		if j.Source != nil {
			summaries[i]["source"] = map[string]interface{}{
				"kind": string(j.Source.Kind),
				"ref":  j.Source.Ref,
				"url":  j.Source.URL,
			}
		}
	}

	return marshalResult(pageResultFields(jobs, map[string]interface{}{
		"jobs":      summaries,
		"count":     len(summaries),
		"timestamp": time.Now().Format(time.RFC3339Nano),
	}))
}

// executeOrchestratorListTasks lists the tenant's orchestration tasks with the
// filters, paging and facets GET /api/v1/orchestrator/tasks answers with.
func (r *ToolRegistry) executeOrchestratorListTasks(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	ctx, tenantID, err := orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	// A status the engine does not have is refused, as REST refuses it, so a
	// misspelt status does not read back as "no such tasks".
	statuses, err := orchestration.TaskStatusesFrom("statuses", getStringSlice(args, "statuses"))
	if err != nil {
		return nil, err
	}

	tasks, err := agentSvc.ListTasksPage(ctx, tenantID, &orchestration.TaskFilter{
		Status:     orchestration.TaskStatus(getString(args, "status")),
		TeamName:   getString(args, "team_name"),
		AssignedTo: getString(args, "assigned_to"),
		CreatedBy:  getString(args, "created_by"),
		ProjectID:  getString(args, "project_id"),
		Statuses:   statuses,
		AutoSpawn:  getOptionalBool(args, "auto_spawn"),
		AgentID:    getString(args, "agent_id"),
		JobID:      getString(args, "job_id"),
		SourceKind: orchestration.TaskSourceKind(getString(args, "source_kind")),
		SourceRef:  getString(args, "source_ref"),
	}, pageFromArgs(args))
	if err != nil {
		return nil, err
	}

	return marshalResult(pageResultFields(tasks, map[string]interface{}{
		"tasks": tasks.Items,
		"count": len(tasks.Items),
	}))
}

// executeOrchestratorTaskGet handles the quicbaas_orchestrator_task_get tool.
func (r *ToolRegistry) executeOrchestratorTaskGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	taskID := getString(args, "task_id")
	if taskID == "" {
		return nil, api.NewRequiredFieldError("task_id")
	}

	ctx, tenantID, err := orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	task, err := agentSvc.GetTask(ctx, tenantID, taskID)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"task":      task,
		"timestamp": time.Now().Format(time.RFC3339Nano),
	})
}

// executeOrchestratorTaskClaim handles the quicbaas_orchestrator_task_claim tool.
func (r *ToolRegistry) executeOrchestratorTaskClaim(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	taskID := getString(args, "task_id")
	if taskID == "" {
		return nil, api.NewRequiredFieldError("task_id")
	}
	agentID := getString(args, "agent_id")
	if agentID == "" {
		return nil, api.NewRequiredFieldError("agent_id")
	}

	ctx, tenantID, err := orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	if err := agentSvc.ClaimTask(ctx, tenantID, taskID, agentID); err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"task_id":   taskID,
		"agent_id":  agentID,
		"claimed":   true,
		"timestamp": time.Now().Format(time.RFC3339Nano),
	})
}

// executeOrchestratorTaskComplete handles the
// quicbaas_orchestrator_task_complete tool.
func (r *ToolRegistry) executeOrchestratorTaskComplete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	taskID := getString(args, "task_id")
	if taskID == "" {
		return nil, api.NewRequiredFieldError("task_id")
	}

	var body struct {
		Result *orchestration.TaskResult `json:"result"`
	}
	if err := parseJSONObject(args, &body); err != nil {
		return nil, api.NewValidationError("result", err.Error())
	}

	ctx, tenantID, err := orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	if err := agentSvc.CompleteTask(ctx, tenantID, taskID, body.Result); err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"task_id":   taskID,
		"completed": true,
		"timestamp": time.Now().Format(time.RFC3339Nano),
	})
}

// executeTaskSubmitArtifacts handles the quicbaas_task_submit_artifacts tool.
// The artifacts decode exactly as the REST body does, so inline content is
// base64, the JSON encoding of the bytes every transport carries.
func (r *ToolRegistry) executeTaskSubmitArtifacts(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	taskID := getString(args, "task_id")
	if taskID == "" {
		return nil, api.NewRequiredFieldError("task_id")
	}

	var body struct {
		Artifacts []orchestration.Artifact `json:"artifacts"`
	}
	if err := parseJSONObject(args, &body); err != nil {
		return nil, api.NewValidationError("artifacts", err.Error())
	}
	if body.Artifacts == nil {
		return nil, api.NewRequiredFieldError("artifacts")
	}

	ctx, tenantID, err := orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	if err := agentSvc.SubmitTaskArtifacts(ctx, tenantID, taskID, body.Artifacts); err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"task_id": taskID,
		"stored":  len(body.Artifacts),
	})
}

// executeOrchestratorSpawnChildJob handles the
// quicbaas_orchestrator_spawn_child_job tool. The arguments decode as the
// REST body does, caller_agent_id included.
func (r *ToolRegistry) executeOrchestratorSpawnChildJob(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	var req orchestration.SpawnChildJobRequest
	if err := parseJSONObject(args, &req); err != nil {
		return nil, api.NewValidationError("arguments", err.Error())
	}
	if req.ParentJobID == "" {
		return nil, api.NewRequiredFieldError("parent_job_id")
	}

	ctx, _, err = orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	resp, err := agentSvc.SpawnChildJob(ctx, &req)
	if err != nil {
		return nil, err
	}

	return marshalResult(resp)
}

func (r *ToolRegistry) executeOrchestratorListWorkflows(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	ctx, _, err = orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	workflows, err := agentSvc.ListWorkflowsPage(ctx, &orchestration.WorkflowFilter{
		ProjectID: getString(args, "project_id"),
		Mode:      orchestration.WorkflowMode(getString(args, "mode")),
	}, pageFromArgs(args))
	if err != nil {
		return nil, err
	}
	return marshalResult(pageResultFields(workflows, map[string]interface{}{
		"workflows": workflows.Items,
		"count":     len(workflows.Items),
	}))
}

// executeOrchestratorGetWorkflow handles the quicbaas_orchestrator_get_workflow tool.
func (r *ToolRegistry) executeOrchestratorGetWorkflow(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	name := getString(args, "name")
	if name == "" {
		return nil, api.NewRequiredFieldError("name")
	}

	ctx, _, err = orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	wf, err := agentSvc.GetWorkflow(ctx, name)
	if err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{"workflow": wf})
}

// executeOrchestratorCreateWorkflow handles the
// quicbaas_orchestrator_create_workflow tool. A non-empty name argument wins
// over the definition's own name, as it does on REST.
func (r *ToolRegistry) executeOrchestratorCreateWorkflow(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	def, err := workflowDefinitionArg(args)
	if err != nil {
		return nil, err
	}
	if name := getString(args, "name"); name != "" {
		def.Name = name
	}
	if def.Name == "" {
		return nil, api.NewRequiredFieldError("name")
	}

	ctx, _, err = orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	if err := agentSvc.RegisterWorkflow(ctx, def); err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{"workflow": def})
}

// executeOrchestratorUpdateWorkflow handles the
// quicbaas_orchestrator_update_workflow tool. The name argument names the
// workflow to replace and overrides the definition's own name.
func (r *ToolRegistry) executeOrchestratorUpdateWorkflow(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	name := getString(args, "name")
	if name == "" {
		return nil, api.NewRequiredFieldError("name")
	}
	def, err := workflowDefinitionArg(args)
	if err != nil {
		return nil, err
	}
	def.Name = name

	ctx, _, err = orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	if err := agentSvc.UpdateWorkflow(ctx, def); err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{"workflow": def})
}

// executeOrchestratorDeleteWorkflow handles the
// quicbaas_orchestrator_delete_workflow tool.
func (r *ToolRegistry) executeOrchestratorDeleteWorkflow(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	name := getString(args, "name")
	if name == "" {
		return nil, api.NewRequiredFieldError("name")
	}

	ctx, _, err = orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	if err := agentSvc.DeleteWorkflow(ctx, name); err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{
		"name":    name,
		"deleted": true,
	})
}

// workflowDefinitionArg decodes the definition argument of the workflow
// create and update tools.
func workflowDefinitionArg(args map[string]interface{}) (*orchestration.WorkflowDefinition, error) {
	var body struct {
		Definition *orchestration.WorkflowDefinition `json:"definition"`
	}
	if err := parseJSONObject(args, &body); err != nil {
		return nil, api.NewValidationError("definition", err.Error())
	}
	if body.Definition == nil {
		return nil, api.NewRequiredFieldError("definition")
	}
	return body.Definition, nil
}

// executeOrchestratorTaskCreate creates an orchestration task, optionally
// dispatching it as a one-shot job via auto-spawn.
// taskCreateObjects decodes task_create's object arguments — the source and
// the park — into req, the same shapes the REST body carries. A present
// argument that is not an object is refused rather than dropped.
func taskCreateObjects(args map[string]interface{}, req *orchestration.CreateTaskRequest) error {
	if raw, present := args["source"]; present && raw != nil {
		obj, ok := raw.(map[string]interface{})
		if !ok {
			return api.NewValidationError("source", "must be an object {kind, ref, url}")
		}
		var source orchestration.TaskSourceRef
		if err := parseJSONObject(obj, &source); err != nil {
			return api.NewValidationError("source", err.Error())
		}
		req.Source = &source
	}
	if raw, present := args["park"]; present && raw != nil {
		obj, ok := raw.(map[string]interface{})
		if !ok {
			return api.NewValidationError("park", "must be an object {kind, message}")
		}
		var park orchestration.BlockReason
		if err := parseJSONObject(obj, &park); err != nil {
			return api.NewValidationError("park", err.Error())
		}
		req.Park = &park
	}
	return nil
}

func (r *ToolRegistry) executeOrchestratorTaskCreate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}

	subject := getString(args, "subject")
	if subject == "" {
		return nil, api.NewRequiredFieldError("subject")
	}

	ctx, tenantID, err := orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}

	req := &orchestration.CreateTaskRequest{
		TenantID:     tenantID,
		Subject:      subject,
		Description:  getString(args, "description"),
		TeamName:     getString(args, "team_name"),
		AssignedTo:   getString(args, "assigned_to"),
		AutoSpawn:    getBool(args, "auto_spawn"),
		Backend:      getString(args, "backend"),
		GpuRequired:  getBool(args, "gpu_required"),
		HostAffinity: getString(args, "host_affinity"),
		Runner:       getString(args, "runner"),
		PromptAppend: getString(args, "prompt_append"),

		ProjectID:     getString(args, "project_id"),
		Metadata:      getStringMap(args, "metadata"),
		Dependencies:  getStringSlice(args, "dependencies"),
		Priority:      getInt(args, "priority"),
		AttemptBudget: getInt(args, "attempt_budget"),
	}
	if err := taskCreateObjects(args, req); err != nil {
		return nil, err
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
		result["runner"] = task.Runner
		result["dispatch_reason"] = task.DispatchReason
		// The full record, as REST and gRPC return it.
		result["task"] = task
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

	ctx, tenantID, err := orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}
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

	ctx, tenantID, err := orchestratorTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}
	result, listErr := orchestration.ListTaskArtifactsPage(ctx, agentSvc, tenantID, taskID, pageFromArgs(args))
	if listErr != nil {
		return nil, listErr
	}

	items := make([]map[string]interface{}, len(result.Items))
	for i, a := range result.Items {
		items[i] = map[string]interface{}{
			"name":      a.Name,
			"kind":      string(a.Kind),
			"mime_type": a.MIMEType,
			"size":      a.Size,
			"ref":       a.Ref,
			"metadata":  a.Metadata,
		}
	}

	return marshalResult(pageResultFields(result, map[string]interface{}{
		"task_id":   taskID,
		"artifacts": items,
		"count":     len(items),
	}))
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
// the guard (and the embedded-fleet-node gRPC backend behind it) observes a
// non-empty tenant on every code path.
//
// The argument is still required, but it is honoured only as
// orchestratorTenantContext allows: it may name only the caller's own tenant
// (tenantctx.Target), so a session tool cannot be pointed at another tenant.
func sessionTenantContext(ctx context.Context, args map[string]interface{}) (context.Context, string, error) {
	if getString(args, "tenant_id") == "" {
		return ctx, "", api.NewRequiredFieldError("tenant_id")
	}
	return orchestratorTenantContext(ctx, args)
}

// orchestratorTenantContext resolves the tenant of a task, job, artifact or
// attention tool the way REST's resolveOrchestratorTenant does: an explicit
// tenant_id argument names it, and without one the caller's own tenant
// binding does. The result is stamped with orchestration.ContextWithTenant,
// because the MCP gate injects the caller's tenant under the iam key only and
// the orchestrator's tenant guard reads neither the argument nor that key —
// reading the orchestration key alone left these tools answering "tenant ID
// required" for every caller, even one that passed tenant_id.
//
// The tenant_id argument is honoured only as tenantctx.Target allows: a
// caller may name only its own tenant, the system tenant included
// (go-quicbaas#1150). A call with no tenant bound is refused.
func orchestratorTenantContext(ctx context.Context, args map[string]interface{}) (context.Context, string, error) {
	tenantID, err := tenantctx.Target(ctx, "mcp.orchestrator", getString(args, "tenant_id"))
	if err != nil {
		return ctx, "", err
	}
	return orchestration.ContextWithTenant(tenantctx.WithTenant(ctx, tenantID), tenantID), tenantID, nil
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

// executeAgentSessionConversation backs
// quicbaas_orchestrator_session_conversation.
//
// It folds the same durable event log the transcript tool pages into the
// conversation an operator reads. One store, two projections: the raw events
// for a caller following a turn, the turns for a caller reading a session.
func (r *ToolRegistry) executeAgentSessionConversation(ctx context.Context, args map[string]interface{}) (interface{}, error) {
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

	page, err := orchestration.ReadSessionHistory(ctx, log, &orchestration.SessionHistoryRequest{
		TenantID:  tenantID,
		SessionID: sessionID,
		Cursor:    getUint64(args, "cursor"),
		Limit:     getInt(args, "limit"),
	})
	if err != nil {
		return nil, err
	}
	return marshalResult(page)
}

// spendLedgerAccessor is the optional interface the agent service satisfies so
// MCP can read the consumption ledger. Declared locally for the same reason
// sessionLogAccessor is: this file takes no dependency on the concrete
// orchestrator implementation.
type spendLedgerAccessor = orchestration.SpendLedgerAccessor

// executeAgentCost backs quicbaas_orchestrator_cost.
//
// It rolls up the same ledger every other transport reads, through the same
// projection, so an agent asking what a job cost and an operator reading the
// cost page get one answer rather than two.
func (r *ToolRegistry) executeAgentCost(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}
	ctx, tenantID, err := sessionTenantContext(ctx, args)
	if err != nil {
		return nil, err
	}
	accessor, ok := agentSvc.(spendLedgerAccessor)
	if !ok {
		return nil, api.NewServiceUnavailableError("consumption ledger")
	}

	req := &orchestration.SpendReportRequest{
		TenantID:  tenantID,
		ProjectID: getString(args, "project_id"),
		TaskID:    getString(args, "task_id"),
		JobID:     getString(args, "job_id"),
		SessionID: getString(args, "session_id"),
		Host:      getString(args, "host"),
		Agent:     getString(args, "agent"),
	}
	// Presence, not value: round 0 is a real level, and getInt alone cannot
	// tell it from "every round".
	if _, present := args["round"]; present {
		req.Round = getInt(args, "round")
		req.RoundSet = true
	}
	if req.From, err = parseOptionalInstant(args, "from"); err != nil {
		return nil, err
	}
	if req.To, err = parseOptionalInstant(args, "to"); err != nil {
		return nil, err
	}

	report, err := orchestration.ReadSpendReport(ctx, orchestration.SpendSourcesOf(accessor), req)
	if err != nil {
		return nil, err
	}
	return marshalResult(report)
}

func (r *ToolRegistry) tenantSpendSources(ctx context.Context, args map[string]interface{}) (context.Context, string, service.TenantSpendPolicySource, error) {
	agentSvc, err := r.getAgentService()
	if err != nil {
		return ctx, "", nil, err
	}
	ctx, tenantID, err := sessionTenantContext(ctx, args)
	if err != nil {
		return ctx, "", nil, err
	}
	accessor, ok := agentSvc.(service.TenantSpendPolicySource)
	if !ok {
		return ctx, "", nil, api.NewServiceUnavailableError("tenant spend policy")
	}
	return ctx, tenantID, accessor, nil
}

// executeTenantSpendPolicyGet handles quicbaas_orchestrator_spend_policy_get.
func (r *ToolRegistry) executeTenantSpendPolicyGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	ctx, tenantID, accessor, err := r.tenantSpendSources(ctx, args)
	if err != nil {
		return nil, err
	}
	report, err := service.GetTenantSpendPolicy(ctx, accessor, tenantID)
	if err != nil {
		return nil, err
	}
	return marshalResult(report)
}

// executeTenantSpendPolicySet handles quicbaas_orchestrator_spend_policy_set.
func (r *ToolRegistry) executeTenantSpendPolicySet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	ctx, tenantID, accessor, err := r.tenantSpendSources(ctx, args)
	if err != nil {
		return nil, err
	}
	limits, present, err := getSpendLimits(args, "limits")
	if err != nil {
		return nil, err
	}
	if !present {
		return nil, api.NewRequiredFieldError("limits")
	}
	report, err := service.SetTenantSpendPolicy(ctx, accessor, tenantID, limits)
	if err != nil {
		return nil, err
	}
	return marshalResult(report)
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
		"quicbaas_orchestrator_session_conversation",
		"quicbaas_orchestrator_cost",
		"quicbaas_orchestrator_spend_policy_get",
		"quicbaas_orchestrator_spend_policy_set",
		"quicbaas_orchestrator_close",
		"quicbaas_orchestrator_rotate_account",
		"quicbaas_orchestrator_list_sessions",
		"quicbaas_orchestrator_list_backends",
		"quicbaas_orchestrator_list_backend_models",
		"quicbaas_orchestrator_list_runners",
		"quicbaas_orchestrator_decompose",
		"quicbaas_orchestrator_health",
		"quicbaas_orchestrator_broadcast",
		"quicbaas_fleet_register",
		"quicbaas_fleet_list",
		"quicbaas_fleet_status",
		"quicbaas_fleet_remove",
		"quicbaas_fleet_heartbeat",
		"quicbaas_orchestrator_start_job",
		"quicbaas_orchestrator_get_job_status",
		"quicbaas_orchestrator_cancel_job",
		"quicbaas_orchestrator_list_jobs",
		"quicbaas_orchestrator_list_tasks",
		"quicbaas_orchestrator_task_get",
		"quicbaas_orchestrator_task_claim",
		"quicbaas_orchestrator_task_complete",
		"quicbaas_task_submit_artifacts",
		"quicbaas_orchestrator_spawn_child_job",
		"quicbaas_orchestrator_list_workflows",
		"quicbaas_orchestrator_get_workflow",
		"quicbaas_orchestrator_create_workflow",
		"quicbaas_orchestrator_update_workflow",
		"quicbaas_orchestrator_delete_workflow",
		"quicbaas_orchestrator_task_create",
		"quicbaas_orchestrator_task_block",
		"quicbaas_orchestrator_task_readmit",
		"quicbaas_orchestrator_task_delete",
		"quicbaas_orchestrator_attention_list",
		"quicbaas_orchestrator_attention_resolve",
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
	case "quicbaas_orchestrator_session_conversation":
		return r.executeAgentSessionConversation(ctx, args)
	case "quicbaas_orchestrator_cost":
		return r.executeAgentCost(ctx, args)
	case "quicbaas_orchestrator_spend_policy_get":
		return r.executeTenantSpendPolicyGet(ctx, args)
	case "quicbaas_orchestrator_spend_policy_set":
		return r.executeTenantSpendPolicySet(ctx, args)
	case "quicbaas_orchestrator_close":
		return r.executeAgentClose(ctx, args)
	case "quicbaas_orchestrator_rotate_account":
		return r.executeAgentRotateAccount(ctx, args)
	case "quicbaas_orchestrator_list_sessions":
		return r.executeAgentListSessions(ctx, args)
	case "quicbaas_orchestrator_list_backends":
		return r.executeAgentListBackends(ctx, args)
	case "quicbaas_orchestrator_list_backend_models":
		return r.executeOrchestratorListBackendModels(ctx, args)
	case "quicbaas_orchestrator_list_runners":
		return r.executeOrchestratorListRunners(ctx, args)
	case "quicbaas_orchestrator_decompose":
		return r.executeAgentDecompose(ctx, args)
	case "quicbaas_orchestrator_health":
		return r.executeAgentHealth(ctx, args)
	case "quicbaas_orchestrator_broadcast":
		return r.executeAgentBroadcast(ctx, args)
	case "quicbaas_fleet_register":
		return r.executeFleetRegister(ctx, args)
	case "quicbaas_fleet_list":
		return r.executeFleetList(ctx, args)
	case "quicbaas_fleet_status":
		return r.executeFleetStatus(ctx, args)
	case "quicbaas_fleet_remove":
		return r.executeFleetRemove(ctx, args)
	case "quicbaas_fleet_heartbeat":
		return r.executeFleetHeartbeat(ctx, args)
	case "quicbaas_orchestrator_start_job":
		return r.executeOrchestratorStartJob(ctx, args)
	case "quicbaas_orchestrator_get_job_status":
		return r.executeOrchestratorGetJobStatus(ctx, args)
	case "quicbaas_orchestrator_cancel_job":
		return r.executeOrchestratorCancelJob(ctx, args)
	case "quicbaas_orchestrator_list_jobs":
		return r.executeOrchestratorListJobs(ctx, args)
	case "quicbaas_orchestrator_list_tasks":
		return r.executeOrchestratorListTasks(ctx, args)
	case "quicbaas_orchestrator_task_get":
		return r.executeOrchestratorTaskGet(ctx, args)
	case "quicbaas_orchestrator_task_claim":
		return r.executeOrchestratorTaskClaim(ctx, args)
	case "quicbaas_orchestrator_task_complete":
		return r.executeOrchestratorTaskComplete(ctx, args)
	case "quicbaas_task_submit_artifacts":
		return r.executeTaskSubmitArtifacts(ctx, args)
	case "quicbaas_orchestrator_spawn_child_job":
		return r.executeOrchestratorSpawnChildJob(ctx, args)
	case "quicbaas_orchestrator_list_workflows":
		return r.executeOrchestratorListWorkflows(ctx, args)
	case "quicbaas_orchestrator_get_workflow":
		return r.executeOrchestratorGetWorkflow(ctx, args)
	case "quicbaas_orchestrator_create_workflow":
		return r.executeOrchestratorCreateWorkflow(ctx, args)
	case "quicbaas_orchestrator_update_workflow":
		return r.executeOrchestratorUpdateWorkflow(ctx, args)
	case "quicbaas_orchestrator_delete_workflow":
		return r.executeOrchestratorDeleteWorkflow(ctx, args)
	case "quicbaas_orchestrator_task_create":
		return r.executeOrchestratorTaskCreate(ctx, args)
	case "quicbaas_orchestrator_task_block":
		return r.executeOrchestratorTaskBlock(ctx, args)
	case "quicbaas_orchestrator_task_readmit":
		return r.executeOrchestratorTaskReadmit(ctx, args)
	case "quicbaas_orchestrator_task_delete":
		return r.executeOrchestratorTaskDelete(ctx, args)
	case "quicbaas_orchestrator_attention_list":
		return r.executeOrchestratorAttentionList(ctx, args)
	case "quicbaas_orchestrator_attention_resolve":
		return r.executeOrchestratorAttentionResolve(ctx, args)
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
