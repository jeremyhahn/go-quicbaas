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

/**
 * Every RefProvider this build ships, registered once at import time.
 *
 * `RefPicker` and `RefChip` import this module for its side effect —
 * `registerCoreRefProviders()` runs the first time either component is
 * mounted — so no separate app-bootstrap wiring is needed and a kind's whole
 * definition (icon, list, search, status, create, detail route) lives in one
 * function here instead of scattered across the components that use it.
 *
 * TWO KINDS ARE DELIBERATELY ABSENT: 'agent' (the tenant-scoped Agent
 * definition) and 'pool' (the ordered Agent failover list). Both are owned
 * by the Agents/Pools lane landing in parallel; their SDK surface does not
 * exist yet. Faking either with the fleet's FleetNode would teach a
 * form to reference the wrong object. Register them here, following this
 * same pattern, once `client.agents` (or equivalent) ships.
 */

import {
  Building2,
  CreditCard,
  FolderKanban,
  KeyRound,
  KeySquare,
  Server,
  Shield,
  ShieldCheck,
  Timer,
  Terminal,
  User,
  Workflow,
} from 'lucide-svelte';

import { getSDKClient } from '../../stores/sdkClient.ts';
import { registerRefProvider } from './registry.ts';
import type { RefOption, RefProvider, RefProviderContext, RefStatus } from './types.ts';

/** PICKER_LIMIT bounds every browse call — operator scale, with search narrowing the rest. */
const PICKER_LIMIT = 100;

/** matchesQuery is the client-side filter every provider without server-side search falls back to. */
function matchesQuery(option: RefOption, query: string): boolean {
  const q = query.trim().toLowerCase();
  if (q === '') return true;
  return option.label.toLowerCase().includes(q) || (option.detail ?? '').toLowerCase().includes(q);
}

/** filterOptions is matchesQuery applied over a whole list — the shared fallback search body. */
function filterOptions(options: RefOption[], query: string): RefOption[] {
  return options.filter((option) => matchesQuery(option, query));
}

// ---------------------------------------------------------------------------
// secret — the one kind with inline create, per the owner's ruling
// ---------------------------------------------------------------------------

async function listSecrets(): Promise<RefOption[]> {
  const client = await getSDKClient();
  const resp = await client.secrets.listSecrets(PICKER_LIMIT);
  const seen = new Set<string>();
  const options: RefOption[] = [];
  for (const entry of resp.secrets) {
    if (seen.has(entry.path)) continue;
    seen.add(entry.path);
    options.push({ value: entry.path, label: entry.path, detail: `v${entry.version}`, status: 'exists' });
  }
  return options.sort((a, b) => a.label.localeCompare(b.label));
}

const secretProvider: RefProvider = {
  kind: 'secret',
  label: 'Secret',
  glyph: KeySquare,
  list: () => listSecrets(),
  async search(query) {
    return filterOptions(await listSecrets(), query);
  },
  async status(value) {
    try {
      const client = await getSDKClient();
      await client.secrets.getSecret(value);
      return 'exists';
    } catch {
      return 'missing';
    }
  },
  createLabel: 'Create secret',
  createFields: [
    { key: 'value', label: 'Value', type: 'password', placeholder: 'secret value', required: true },
  ],
  async create(path, extra) {
    const client = await getSDKClient();
    const trimmed = path.trim();
    await client.secrets.storeSecret({ path: trimmed, data: { value: extra.value ?? '' } });
    return { value: trimmed, label: trimmed, status: 'exists' };
  },
  detailRoute: () => '/secrets',
};

// ---------------------------------------------------------------------------
// kmsKey
// ---------------------------------------------------------------------------

const KMS_KEY_STATUS: Readonly<Record<string, RefStatus>> = {
  active: 'exists',
  inactive: 'disabled',
  pending_deletion: 'expired',
  destroyed: 'missing',
};

async function listKmsKeys(): Promise<RefOption[]> {
  const client = await getSDKClient();
  const resp = await client.kms.listKeys(PICKER_LIMIT);
  return (resp.keys ?? [])
    .map((key) => ({
      value: key.name,
      label: key.name,
      detail: key.algorithm,
      status: KMS_KEY_STATUS[key.status] ?? 'unknown',
    }))
    .sort((a, b) => a.label.localeCompare(b.label));
}

const kmsKeyProvider: RefProvider = {
  kind: 'kmsKey',
  label: 'KMS key',
  glyph: KeyRound,
  list: () => listKmsKeys(),
  async search(query) {
    return filterOptions(await listKmsKeys(), query);
  },
  async status(value) {
    const found = (await listKmsKeys()).find((o) => o.value === value);
    return found?.status ?? 'missing';
  },
  detailRoute: () => '/kms',
};

// ---------------------------------------------------------------------------
// sshKey — scope/owner narrow the browse set; ctx.scope / ctx.owner carry them
// ---------------------------------------------------------------------------

async function listSshKeys(ctx: RefProviderContext): Promise<RefOption[]> {
  const client = await getSDKClient();
  const scope = typeof ctx.scope === 'string' && ctx.scope !== '' ? (ctx.scope as 'tenant' | 'user' | 'service') : undefined;
  const owner = typeof ctx.owner === 'string' && ctx.owner !== '' ? ctx.owner : undefined;
  const resp = await client.ssh.keys.listScoped(scope !== undefined || owner !== undefined ? { scope, owner } : undefined);
  return (resp.keys ?? [])
    .map((key) => ({
      value: key.name,
      label: key.name,
      detail: key.fingerprint,
      status: 'exists' as RefStatus,
    }))
    .sort((a, b) => a.label.localeCompare(b.label));
}

const sshKeyProvider: RefProvider = {
  kind: 'sshKey',
  label: 'SSH key',
  glyph: Terminal,
  list: (ctx) => listSshKeys(ctx),
  async search(query, ctx) {
    return filterOptions(await listSshKeys(ctx), query);
  },
  async status(value, ctx) {
    const found = (await listSshKeys(ctx)).find((o) => o.value === value);
    return found?.status ?? 'missing';
  },
  detailRoute: () => '/ssh-keys',
};

// ---------------------------------------------------------------------------
// certificate — identified by serial number; status blends expiry with a
// live revocation check, since ListCertificates does not carry "revoked".
// ---------------------------------------------------------------------------

function certificateStatusFromSummary(notAfter: string): RefStatus {
  const expires = Date.parse(notAfter);
  if (!Number.isNaN(expires) && expires < Date.now()) return 'expired';
  return 'exists';
}

async function listCertificates(query?: string): Promise<RefOption[]> {
  const client = await getSDKClient();
  const resp = await client.ca.listCertificates({
    limit: PICKER_LIMIT,
    include_expired: true,
    include_revoked: true,
    common_name: query !== undefined && query.trim() !== '' ? query.trim() : undefined,
  });
  return resp.certificates
    .map((cert) => ({
      value: cert.serial_number,
      label: cert.common_name || cert.subject || cert.serial_number,
      detail: cert.serial_number,
      status: certificateStatusFromSummary(cert.not_after),
    }))
    .sort((a, b) => a.label.localeCompare(b.label));
}

const certificateProvider: RefProvider = {
  kind: 'certificate',
  label: 'Certificate',
  glyph: ShieldCheck,
  list: () => listCertificates(),
  search: (query) => listCertificates(query),
  async status(value) {
    const client = await getSDKClient();
    try {
      const revoked = await client.ca.isRevoked(value);
      if (revoked.revoked) return 'expired';
    } catch {
      return 'missing';
    }
    const match = (await listCertificates()).find((o) => o.value === value);
    return match?.status ?? 'missing';
  },
  detailRoute: () => '/certificates',
};

// ---------------------------------------------------------------------------
// account
// ---------------------------------------------------------------------------

const ACCOUNT_STATUS: Readonly<Record<string, RefStatus>> = {
  active: 'exists',
  revoked: 'disabled',
  error: 'unknown',
};

async function listAccounts(ctx: RefProviderContext, query?: string): Promise<RefOption[]> {
  const client = await getSDKClient();
  const page = await client.accounts.listAccountsPage({
    tenant_id: ctx.tenantID,
    limit: PICKER_LIMIT,
    q: query,
  });
  return page.items
    .map((account) => ({
      value: account.id,
      label: account.display_name || `${account.provider} account`,
      detail: account.default ? `${account.provider} · default` : account.provider,
      status: ACCOUNT_STATUS[account.status] ?? 'unknown',
    }))
    .sort((a, b) => a.label.localeCompare(b.label));
}

const accountProvider: RefProvider = {
  kind: 'account',
  label: 'Account',
  glyph: CreditCard,
  list: (ctx) => listAccounts(ctx),
  search: (query, ctx) => listAccounts(ctx, query),
  async status(value, ctx) {
    const found = (await listAccounts(ctx)).find((o) => o.value === value);
    return found?.status ?? 'missing';
  },
  detailRoute: () => '/accounts',
};

// ---------------------------------------------------------------------------
// fleetNode — the registered fleet node daemons, detailed at #/fleet/{id}.
// ---------------------------------------------------------------------------

const FLEET_STATUS: Readonly<Record<string, RefStatus>> = {
  registered: 'exists',
  running: 'exists',
  idle: 'exists',
  offline: 'unknown',
  error: 'unknown',
  disabled: 'disabled',
};

async function listFleetNodes(ctx: RefProviderContext, query?: string): Promise<RefOption[]> {
  const client = await getSDKClient();
  const page = await client.orchestrator.listFleetNodesPage(ctx.tenantID, { limit: PICKER_LIMIT, q: query });
  return page.items
    .map((node) => ({
      value: node.id,
      label: node.name || node.id,
      detail: node.host,
      status: FLEET_STATUS[node.status] ?? 'unknown',
    }))
    .sort((a, b) => a.label.localeCompare(b.label));
}

const fleetNodeProvider: RefProvider = {
  kind: 'fleetNode',
  label: 'Fleet node',
  glyph: Server,
  list: (ctx) => listFleetNodes(ctx),
  search: (query, ctx) => listFleetNodes(ctx, query),
  async status(value, ctx) {
    const found = (await listFleetNodes(ctx)).find((o) => o.value === value);
    return found?.status ?? 'missing';
  },
  detailRoute: (value) => `/fleet/${value}`,
};

// ---------------------------------------------------------------------------
// project
// ---------------------------------------------------------------------------

async function listProjects(ctx: RefProviderContext, query?: string): Promise<RefOption[]> {
  const client = await getSDKClient();
  const page = await client.projects.listProjectsPage(ctx.tenantID, { limit: PICKER_LIMIT, q: query });
  return page.items
    .map((project) => ({
      value: project.id,
      label: project.name || project.id,
      detail: project.description,
      status: (project.parked ? 'disabled' : 'exists') as RefStatus,
    }))
    .sort((a, b) => a.label.localeCompare(b.label));
}

const projectProvider: RefProvider = {
  kind: 'project',
  label: 'Project',
  glyph: FolderKanban,
  list: (ctx) => listProjects(ctx),
  search: (query, ctx) => listProjects(ctx, query),
  async status(value, ctx) {
    const found = (await listProjects(ctx)).find((o) => o.value === value);
    return found?.status ?? 'missing';
  },
  detailRoute: () => '/projects',
};

// ---------------------------------------------------------------------------
// tenant
// ---------------------------------------------------------------------------

async function listTenants(): Promise<RefOption[]> {
  const client = await getSDKClient();
  const resp = await client.iam.listTenants();
  return resp.tenants
    .map((tenant) => ({ value: tenant.id, label: tenant.name || tenant.id, detail: tenant.description, status: 'exists' as RefStatus }))
    .sort((a, b) => a.label.localeCompare(b.label));
}

const tenantProvider: RefProvider = {
  kind: 'tenant',
  label: 'Tenant',
  glyph: Building2,
  list: () => listTenants(),
  async search(query) {
    return filterOptions(await listTenants(), query);
  },
  async status(value) {
    const found = (await listTenants()).find((o) => o.value === value);
    return found?.status ?? 'missing';
  },
  detailRoute: () => '/iam/tenants',
};

// ---------------------------------------------------------------------------
// user
// ---------------------------------------------------------------------------

const USER_LIFECYCLE_STATUS: Readonly<Record<string, RefStatus>> = {
  active: 'exists',
  pending: 'unknown',
  suspended: 'disabled',
  locked: 'disabled',
  disabled: 'disabled',
  deleting: 'expired',
};

async function listUsers(ctx: RefProviderContext): Promise<RefOption[]> {
  const client = await getSDKClient();
  const resp = await client.iam.listUsers(ctx.tenantID);
  return resp.users
    .map((user) => ({
      value: user.id,
      label: user.username || user.email || user.id,
      detail: user.email,
      status: USER_LIFECYCLE_STATUS[user.lifecycle ?? 'active'] ?? 'unknown',
    }))
    .sort((a, b) => a.label.localeCompare(b.label));
}

const userProvider: RefProvider = {
  kind: 'user',
  label: 'User',
  glyph: User,
  list: (ctx) => listUsers(ctx),
  async search(query, ctx) {
    return filterOptions(await listUsers(ctx), query);
  },
  async status(value, ctx) {
    const found = (await listUsers(ctx)).find((o) => o.value === value);
    return found?.status ?? 'missing';
  },
  detailRoute: () => '/iam/users',
};

// ---------------------------------------------------------------------------
// role
// ---------------------------------------------------------------------------

async function listRoles(ctx: RefProviderContext): Promise<RefOption[]> {
  const client = await getSDKClient();
  const resp = await client.iam.listRoles(ctx.tenantID);
  return resp.roles
    .map((role) => ({
      value: role.id,
      label: role.name || role.id,
      detail: role.description,
      // A role carrying retired_permissions has drifted from the build's
      // live registry — 'expired' surfaces that a Reconcile is due, rather
      // than showing a role that quietly grants less than it once did.
      status: ((role.retired_permissions?.length ?? 0) > 0 ? 'expired' : 'exists') as RefStatus,
    }))
    .sort((a, b) => a.label.localeCompare(b.label));
}

const roleProvider: RefProvider = {
  kind: 'role',
  label: 'Role',
  glyph: Shield,
  list: (ctx) => listRoles(ctx),
  async search(query, ctx) {
    return filterOptions(await listRoles(ctx), query);
  },
  async status(value, ctx) {
    const found = (await listRoles(ctx)).find((o) => o.value === value);
    return found?.status ?? 'missing';
  },
  detailRoute: () => '/iam/roles',
};

// ---------------------------------------------------------------------------
// schedulerJob
// ---------------------------------------------------------------------------

const SCHEDULER_STATUS: Readonly<Record<string, RefStatus>> = {
  paused: 'disabled',
  error: 'unknown',
  failed: 'unknown',
};

async function listSchedulerJobs(): Promise<RefOption[]> {
  const client = await getSDKClient();
  const resp = await client.scheduler.listJobs({ limit: PICKER_LIMIT });
  return resp.jobs
    .map((job) => ({
      value: String(job.id),
      label: job.name,
      detail: job.handler,
      status: SCHEDULER_STATUS[job.status] ?? 'exists',
    }))
    .sort((a, b) => a.label.localeCompare(b.label));
}

const schedulerJobProvider: RefProvider = {
  kind: 'schedulerJob',
  label: 'Scheduler job',
  glyph: Timer,
  list: () => listSchedulerJobs(),
  async search(query) {
    return filterOptions(await listSchedulerJobs(), query);
  },
  async status(value) {
    const found = (await listSchedulerJobs()).find((o) => o.value === value);
    return found?.status ?? 'missing';
  },
  detailRoute: () => '/scheduler',
};

// ---------------------------------------------------------------------------
// workflow — identified by name; a workflow carries no ID or status of its own.
// ---------------------------------------------------------------------------

async function listWorkflows(ctx: RefProviderContext, query?: string): Promise<RefOption[]> {
  const client = await getSDKClient();
  const page = await client.orchestrator.listWorkflowsPage(ctx.tenantID, { limit: PICKER_LIMIT, q: query });
  return page.items
    .map((workflow) => ({
      value: workflow.name,
      label: workflow.name,
      detail: `${workflow.mode} · ${workflow.agents.length} role${workflow.agents.length === 1 ? '' : 's'}`,
      status: 'exists' as RefStatus,
    }))
    .sort((a, b) => a.label.localeCompare(b.label));
}

const workflowProvider: RefProvider = {
  kind: 'workflow',
  label: 'Workflow',
  glyph: Workflow,
  list: (ctx) => listWorkflows(ctx),
  search: (query, ctx) => listWorkflows(ctx, query),
  async status(value, ctx) {
    const found = (await listWorkflows(ctx)).find((o) => o.value === value);
    return found?.status ?? 'missing';
  },
  detailRoute: () => '/workflows',
};

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

let registered = false;

/** registerCoreRefProviders installs every kind this build ships. Idempotent. */
export function registerCoreRefProviders(): void {
  if (registered) return;
  registered = true;
  registerRefProvider(secretProvider);
  registerRefProvider(kmsKeyProvider);
  registerRefProvider(sshKeyProvider);
  registerRefProvider(certificateProvider);
  registerRefProvider(accountProvider);
  registerRefProvider(fleetNodeProvider);
  registerRefProvider(projectProvider);
  registerRefProvider(tenantProvider);
  registerRefProvider(userProvider);
  registerRefProvider(roleProvider);
  registerRefProvider(schedulerJobProvider);
  registerRefProvider(workflowProvider);
}

registerCoreRefProviders();
