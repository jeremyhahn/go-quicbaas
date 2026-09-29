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

// ---------------------------------------------------------------------------
// The tenant AI agent catalog.
//
// Mirrors sdk/go/client/agents/client.go and the REST surface at
// pkg/api/rest/handlers_agents.go (/api/v1/agents, /api/v1/agent-pools).
//
// An Agent is a named runtime (backend), model, provider endpoint and image a
// role slot references. A pool is an ordered list of Agents a slot fails over
// along. These are NOT the fleet's agent daemons (see /api/v1/fleet).
// ---------------------------------------------------------------------------

/** AgentRole is a role an Agent declares it can fill. */
import type { SpendLimits } from './spend.js';

export type AgentRole = 'coding' | 'review' | 'triage' | 'lead' | 'project_manager' | 'general';

/**
 * AgentRef is a role slot's reference: exactly one Agent (agent_id) or one
 * pool (pool_id). An absent or empty reference names nothing.
 */
export interface AgentRef {
  agent_id?: string;
  pool_id?: string;
}

/** ProviderEndpoint locates where an Agent's model is served; empty is the runtime's default. */
export interface ProviderEndpoint {
  name?: string;
  base_url?: string;
  /** The API dialect the endpoint speaks, e.g. "responses" or "chat". */
  wire_api?: string;
  requires_auth?: boolean;
}

/** AgentAccountPolicy is how an Agent's sessions pick a pooled provider account. */
export interface AgentAccountPolicy {
  /** "static" or "least_usage"; empty is the registry default. */
  strategy?: string;
  /** The pooled accounts the Agent may use and rotate through. */
  accounts?: string[];
}

/** AgentPrompt is the charter an Agent's sessions start with. */
export interface AgentPrompt {
  charter_set?: string;
  charter?: string;
  system?: string;
}

/** AgentPlacement reserves fleet capacity for a role. */
export interface AgentPlacement {
  prefer?: AgentRole;
  reserve?: number;
  exclusive?: boolean;
}

/** Agent is one entry in the tenant's catalog. */
export interface Agent {
  id: string;
  name: string;
  description?: string;
  roles: AgentRole[];
  backend: string;
  /** Required: a resolved Agent always has a model. */
  model: string;
  provider?: ProviderEndpoint;
  image?: string;
  /** minimal | low | medium | high | xhigh; empty takes the backend default. */
  reasoning_effort?: string;
  /**
   * The accounts this Agent's runs draw on. It governs the Agent's spawns
   * unless a spawn pins account_id, and it only ever narrows the project's
   * pool. Each account must be of the provider the backend draws on.
   */
  account?: AgentAccountPolicy;
  /**
   * Optional. With none, a spawn for a role that has a built-in charter
   * (coding, review, triage) starts with that charter, and any other starts
   * with no system prompt.
   */
  prompt?: AgentPrompt;
  mcp_scopes?: string[];
  capabilities?: string[];
  context_tokens?: number;
  max_concurrent?: number;
  placement?: AgentPlacement;
  /**
   * The Agent's own spend limits, checked when a session of it is spawned:
   * an Agent over a window passes a pool's work to its next member. The
   * project's and tenant's limits apply on top.
   */
  spend?: SpendLimits;
  created_at?: string;
  updated_at?: string;
}

/** AgentPool is an ordered failover list of Agents. */
export interface AgentPool {
  id: string;
  name: string;
  description?: string;
  /** Agent IDs in failover order. */
  agents: string[];
  created_at?: string;
  updated_at?: string;
}

/** AgentUsage is one holder of a reference into the catalog. */
export interface AgentUsage {
  /** pool | binding | project | config */
  kind: string;
  id: string;
  /** Which slot of the holder: member, worker, triage, reviewer:<seat>, default_lead, lead. */
  slot: string;
}

/** AgentAttempt is one Agent a pool spawn passed over, and why. */
export interface AgentAttempt {
  agent_id: string;
  /** account_exhausted | account_expired | rate_limited | backend_unavailable | at_capacity | held_for_role | over_budget */
  reason: string;
  detail?: string;
}

/** AgentsClient manages the tenant's Agents and pools. */
export interface AgentsClient {
  /** GET /api/v1/agents?tenant_id= */
  listAgents(tenantId?: string): Promise<Agent[]>;
  /** GET /api/v1/agents/:agentId?tenant_id= */
  getAgent(agentId: string, tenantId?: string): Promise<Agent>;
  /** POST /api/v1/agents?tenant_id= — checked against the live backend and model catalog. */
  createAgent(agent: Agent, tenantId?: string): Promise<Agent>;
  /** PUT /api/v1/agents/:agentId?tenant_id= — a role a slot still needs cannot be dropped. */
  updateAgent(agentId: string, agent: Agent, tenantId?: string): Promise<Agent>;
  /** DELETE /api/v1/agents/:agentId?tenant_id= — refused while anything references it. */
  deleteAgent(agentId: string, tenantId?: string): Promise<void>;
  /** GET /api/v1/agents/:agentId/used-by?tenant_id= */
  agentUsedBy(agentId: string, tenantId?: string): Promise<AgentUsage[]>;

  /** GET /api/v1/agent-pools?tenant_id= */
  listPools(tenantId?: string): Promise<AgentPool[]>;
  /** GET /api/v1/agent-pools/:poolId?tenant_id= */
  getPool(poolId: string, tenantId?: string): Promise<AgentPool>;
  /** POST /api/v1/agent-pools?tenant_id= — every member must exist. */
  createPool(pool: AgentPool, tenantId?: string): Promise<AgentPool>;
  /** PUT /api/v1/agent-pools/:poolId?tenant_id= — order is the failover chain. */
  updatePool(poolId: string, pool: AgentPool, tenantId?: string): Promise<AgentPool>;
  /** DELETE /api/v1/agent-pools/:poolId?tenant_id= — refused while anything references it. */
  deletePool(poolId: string, tenantId?: string): Promise<void>;
  /** GET /api/v1/agent-pools/:poolId/used-by?tenant_id= */
  poolUsedBy(poolId: string, tenantId?: string): Promise<AgentUsage[]>;
}

/** formatAgentRef renders a reference for a person: "agent:<id>", "pool:<id>" or "". */
export function formatAgentRef(ref: AgentRef | undefined): string {
  if (ref?.pool_id) return `pool:${ref.pool_id}`;
  if (ref?.agent_id) return `agent:${ref.agent_id}`;
  return '';
}

/**
 * parseAgentRef reads "agent:<id>" or "pool:<id>"; a bare ID is an Agent and
 * empty is no reference. It throws on any other kind.
 */
export function parseAgentRef(text: string): AgentRef {
  const trimmed = text.trim();
  if (trimmed === '') return {};
  const colon = trimmed.indexOf(':');
  if (colon < 0) return { agent_id: trimmed };
  const kind = trimmed.slice(0, colon).trim();
  const id = trimmed.slice(colon + 1).trim();
  if (kind === 'agent') return { agent_id: id };
  if (kind === 'pool') return { pool_id: id };
  throw new Error(`a reference is agent:<id> or pool:<id>, got ${trimmed}`);
}
