/**
 * fixtures/workflows_seed.ts
 *
 * REST helpers for seeding and tearing down test workflow definitions in
 * beforeAll/afterAll hooks.  Used by workflows_create.spec.ts and any future
 * spec that needs a live workflow provisioned out-of-band.
 *
 * Create:  POST /api/v1/orchestrator/workflows
 *            body: { tenant_id, name, definition }
 *            resp: { workflow: { name, mode, agents, settings, ... } }
 *            HTTP 201 on success
 * Delete:  DELETE /api/v1/orchestrator/workflows/{name}?tenant_id={tenantID}
 *
 * Verified REST paths against pkg/api/rest/handlers_orchestrator.go:
 *   POST   /api/v1/orchestrator/workflows       → HandleOrchestratorCreateWorkflow (lines 1615–1663)
 *   DELETE /api/v1/orchestrator/workflows/{name} → HandleOrchestratorDeleteWorkflow  (lines 1585–1613)
 *
 * Required body fields (handler lines 1641–1648):
 *   name        — "workflow name is required"
 *   definition  — "workflow definition is required"
 *
 * Uses the shared auth + fetch helpers from fixtures/_http.ts.
 */

import { serverBase, authHeader, apiFetch } from './_http.ts';
export { SYSTEM_TENANT_ID } from './_http.ts';

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

/**
 * SeededWorkflow holds the name returned by
 * POST /api/v1/orchestrator/workflows.
 *
 * name — used to locate the workflow row in the table and as the key for
 *         DELETE cleanup.
 */
export interface SeededWorkflow {
  name: string;
}

// ---------------------------------------------------------------------------
// Public helpers
// ---------------------------------------------------------------------------

/**
 * seedWorkflow provisions a minimal autonomous workflow via the admin REST API.
 *
 * The name is generated with a timestamp suffix so parallel runs and retries
 * never collide.  The definition uses the "autonomous" mode which has no
 * lead-agent requirement, making it the simplest valid definition.
 *
 * The minimal WorkflowDefinition shape is derived from:
 *   WorkflowWizard.svelte — assembled $derived value
 *   orchestration package — WorkflowDefinition / AgentRoleSpec / WorkflowSettings
 *
 * @param label    Short prefix used in the workflow name.
 * @param tenantID Tenant to create the workflow under (default: system).
 */
export async function seedWorkflow(
  label = 'e2e-wf',
  tenantID = 'system',
): Promise<SeededWorkflow> {
  const name = `${label}-${Date.now()}`;
  const url = `${serverBase()}/api/v1/orchestrator/workflows`;
  await apiFetch<unknown>(url, {
    method: 'POST',
    body: JSON.stringify({
      tenant_id: tenantID,
      name,
      definition: {
        name,
        description: 'E2E test workflow — safe to delete',
        mode: 'autonomous',
        agents: [
          {
            role: 'worker',
            backend: '',
            model: '',
            capabilities: [],
            mcp_scopes: [],
            count: 1,
            workspace_path: '',
            permission_mode: 'default',
            skip_permissions: false,
            env: {},
            sandboxed: false,
            container_image: '',
            host_affinity: '',
            num_ctx: 0,
          },
        ],
        settings: {
          max_iterations: 10,
          max_parallelism: 4,
          poll_interval: '30s',
          timeout_per_item: '5m',
          total_timeout: '1h',
          review_criteria: [],
          topology: 'hub_and_spoke',
          isolation: 'shared',
          verbosity: 'info',
          num_ctx: 0,
          peer_messaging: {
            max_message_size: 10240,
            max_inbox_size: 1000,
            overflow_action: 'reject',
            inbox_ttl: '1h',
          },
          file_locking: {
            lease_ttl: '5m',
            heartbeat_interval: '30s',
            max_renewals: 60,
          },
          vector_db: {
            enabled: false,
            index_type: 'auto',
            auto_index: false,
            triggers: [],
            gpu_accelerated: false,
          },
          cost_controls: {
            max_tokens_per_job: 0,
            max_tokens_per_agent: 0,
            budget_action: 'warn',
          },
          recovery: {
            auto_restart: false,
            max_restarts: 3,
            restart_backoff: '30s',
          },
        },
      },
    }),
  });
  return { name };
}

/**
 * deleteSeededWorkflow removes a workflow definition by name.
 *
 * Errors are swallowed (non-fatal) so afterAll cleanup never masks a real
 * test failure.  404 responses are silently ignored.
 *
 * Verified endpoint: DELETE /api/v1/orchestrator/workflows/{name}
 *   → HandleOrchestratorDeleteWorkflow (handlers_orchestrator.go lines 1585–1613)
 */
export async function deleteSeededWorkflow(name: string, tenantID = 'system'): Promise<void> {
  try {
    const url = `${serverBase()}/api/v1/orchestrator/workflows/${encodeURIComponent(name)}?tenant_id=${encodeURIComponent(tenantID)}`;
    const resp = await fetch(url, {
      method: 'DELETE',
      headers: { ...authHeader(), Accept: 'application/json' },
    });
    if (!resp.ok && resp.status !== 404) {
      console.warn(`deleteSeededWorkflow: DELETE ${url} returned ${resp.status}`);
    }
  } catch (e) {
    console.warn('deleteSeededWorkflow failed (non-fatal):', e);
  }
}
