/**
 * fixtures/agents_seed.ts
 *
 * REST helpers for seeding and tearing down test agents in beforeAll/afterAll
 * hooks.  Used by agent_actions.spec.ts and any future spec that needs a live
 * fleet agent provisioned out-of-band.
 *
 * Create:  POST /api/v1/orchestrator/agents
 *            body: { name, host, agent_type, backend?, tenant_id? }
 *            resp: { agent: { id, name, host, backend, agent_type, status, ... } }
 * Remove:  DELETE /api/v1/orchestrator/agents/{id}?tenant_id={tenantID}
 *
 * Required body fields verified against pkg/api/rest/handlers_orchestrator.go
 * HandleOrchestratorRegisterAgent (lines 1160–1173):
 *   name        — "name is required"
 *   host        — "host is required"
 *   agent_type  — "agent_type is required"
 *
 * Response shape verified against pkg/orchestration/agent_registry_types.go:
 *   RegisterAgentResponse.Agent → AgentRegistration { id, name, host, backend,
 *                                                      agent_type, status, ... }
 *
 * Uses the shared auth + fetch helpers from fixtures/_http.ts.
 */

import { serverBase, authHeader, apiFetch } from './_http.ts';
export { SYSTEM_TENANT_ID } from './_http.ts';

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

/**
 * SeededAgent holds the id and name returned by
 * POST /api/v1/orchestrator/agents.
 *
 * id   — used for navigation (#/agents/{id}) and for the disable/remove APIs.
 * name — used to locate the fleet card (aria-label="View details for agent {name}").
 */
export interface SeededAgent {
  id: string;
  name: string;
  backend: string;
  host: string;
}

// Shape of the server's RegisterAgentResponse.
interface RegisterAgentResponseJSON {
  agent?: {
    id: string;
    name: string;
    host: string;
    backend: string;
    agent_type: string;
    status: string;
  };
}

// ---------------------------------------------------------------------------
// Public helpers
// ---------------------------------------------------------------------------

/**
 * seedAgent provisions a fresh agent via the orchestrator REST API.
 *
 * The name is generated with a timestamp suffix so parallel runs and
 * retries never collide.  The host is a synthetic non-routable address;
 * the daemon stores it in the registry but never dials it for these tests.
 *
 * The seeded agent_type is "agent-runtime" — the same type registered by
 * real agent daemons.  Using a recognized type ensures the agent appears in
 * the default fleet grid filter.
 *
 * @param label  Short prefix used in the agent name (e.g. "e2e-primary").
 * @param tenantID  Tenant to register the agent under (default: system).
 */
export async function seedAgent(
  label = 'e2e-agent',
  tenantID = 'system',
): Promise<SeededAgent> {
  const name = `${label}-${Date.now()}`;
  const url = `${serverBase()}/api/v1/orchestrator/agents`;
  const resp = await apiFetch<RegisterAgentResponseJSON>(url, {
    method: 'POST',
    body: JSON.stringify({
      tenant_id: tenantID,
      name,
      host: 'e2e.test.local:9999',
      agent_type: 'agent-runtime',
      backend: 'ollama',
    }),
  });
  const agent = resp.agent;
  if (!agent?.id) {
    throw new Error(`seedAgent: server returned unexpected body — ${JSON.stringify(resp)}`);
  }
  return { id: agent.id, name: agent.name, backend: agent.backend, host: agent.host };
}

/**
 * removeSeededAgent deregisters a test agent by ID.
 *
 * Errors are swallowed (non-fatal) so afterAll cleanup never masks a real
 * test failure.  404 responses are silently ignored — the agent may have
 * already been removed by a test step (e.g. the Remove round-trip test).
 *
 * Verified endpoint: DELETE /api/v1/orchestrator/agents/{id}
 *   → HandleOrchestratorRemoveAgent (handlers_orchestrator.go lines 1252–1279)
 */
export async function removeSeededAgent(agentID: string, tenantID = 'system'): Promise<void> {
  try {
    const url = `${serverBase()}/api/v1/orchestrator/agents/${encodeURIComponent(agentID)}?tenant_id=${encodeURIComponent(tenantID)}`;
    const resp = await fetch(url, {
      method: 'DELETE',
      headers: { ...authHeader(), Accept: 'application/json' },
    });
    if (!resp.ok && resp.status !== 404) {
      console.warn(`removeSeededAgent: DELETE ${url} returned ${resp.status}`);
    }
  } catch (e) {
    console.warn('removeSeededAgent failed (non-fatal):', e);
  }
}
