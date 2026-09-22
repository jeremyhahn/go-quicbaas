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

/**
 * Minimal hash-based SPA router.
 *
 * Parses window.location.hash and exposes a reactive route object. No
 * external dependency — keeps the bundle lean.
 *
 * Supported routes:
 *   #/               → system dashboard (OVERVIEW)
 *   #/login          → login view
 *   #/dashboard      → system dashboard (OVERVIEW)
 *   #/orchestration  → orchestration dashboard (ORCHESTRATION)
 *   #/jobs           → jobs list
 *   #/jobs/:id       → job detail
 *   #/fleet          → agent fleet
 *   #/agents         → bookmark-friendly alias for /fleet
 *   #/agents/:id     → agent detail
 *   #/sessions       → session list
 *   #/sessions/:id   → session detail
 *   #/tasks          → tasks view
 *   #/tasks?tasks_project=:id → tasks view narrowed to one project
 *   #/tasks/:id      → task detail
 *   #/events         → live event stream
 *   #/live-events    → bookmark-friendly alias for /events
 *   #/cost           → cost tracking
 *   #/cost-tracking  → bookmark-friendly alias for /cost
 *   #/kv             → KV Store
 *   #/vectors        → Vector collections
 *   #/timeseries     → Time-Series
 *   #/objects        → Object storage
 *   #/iam            → IAM
 *   #/kms            → KMS
 *   #/secrets        → Secrets
 *   #/certificates   → Certificates / Certificate Authority
 *   #/trust          → Trust composition (cross-sign / AIA / DANE)
 *   #/crypto         → Crypto console (encrypt/decrypt/sign/verify/hash/random/datakey)
 *   #/dek            → Data Encryption Keys (create/rotate/reencrypt)
 *   #/dynamic-secrets → Dynamic Secrets (roles, credential leases)
 *   #/barrier        → Barrier (encryption barrier; replaces #/seal)
 *   #/ceremony       → Multi-officer key ceremony
 *   #/bootstrap      → First-contact trust bootstrap wizard
 *   #/ssh-keys       → SSH Keys
 *   #/sshkeys        → bookmark-friendly alias for /ssh-keys
 *   #/accounts       → Pooled provider accounts
 *   #/invite/:token → Invite activation (passwordless)
 */

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type RouteName =
  | 'login'
  | 'register'
  | 'dashboard'
  | 'orchestration'
  | 'jobs'
  | 'job-detail'
  | 'fleet'
  | 'agent-detail'
  | 'sessions'
  | 'session-detail'
  | 'tasks'
  | 'task-detail'
  | 'workflows'
  | 'events'
  | 'cost'
  | 'kv'
  | 'vectors'
  | 'timeseries'
  | 'objects'
  // AI/ML
  | 'rag'
  | 'embeddings'
  // ASYNC
  | 'messaging'
  | 'scheduler'
  // SECURITY
  | 'iam'
  | 'kms'
  | 'secrets'
  | 'certificates'
  | 'trust'
  | 'crypto'
  | 'dek'
  | 'dynamic-secrets'
  | 'barrier'
  | 'tenant-barrier'
  | 'ceremony'
  | 'bootstrap'
  | 'ssh-keys'
  // ORCHESTRATION — inference
  | 'inference'
  // ORCHESTRATION — projects
  | 'projects'
  // ORCHESTRATION — pooled provider accounts
  | 'accounts'
  // CLUSTER
  | 'cluster'
  | 'invite-activate'
  | 'not-found';

export interface Route {
  name: RouteName;
  params: Record<string, string>;
  hash: string;
}

// ---------------------------------------------------------------------------
// Reactive state
// ---------------------------------------------------------------------------

/**
 * parseQuery turns the part of a hash after `?` into a flat string map.
 * Repeated keys keep the last value; a key with no `=` maps to ''.
 */
function parseQuery(query: string): Record<string, string> {
  const params: Record<string, string> = {};
  for (const [key, value] of new URLSearchParams(query)) {
    params[key] = value;
  }
  return params;
}

/**
 * parseHash resolves a hash to a route. Query parameters (`#/tasks?project=p1`)
 * are merged into `params` for every route; a path parameter of the same name
 * wins, so `/tasks/:id?id=other` still names the task in the path.
 */
function parseHash(hash: string): Route {
  const full = hash.replace(/^#/, '') || '/';
  const queryAt = full.indexOf('?');
  const path = queryAt === -1 ? full : full.slice(0, queryAt);
  const query = queryAt === -1 ? {} : parseQuery(full.slice(queryAt + 1));
  const route = matchPath(path, hash);
  return { ...route, params: { ...query, ...route.params } };
}

function matchPath(path: string, hash: string): Route {

  if (path === '/' || path === '') {
    return { name: 'dashboard', params: {}, hash };
  }
  if (path === '/login') {
    return { name: 'login', params: {}, hash };
  }
  if (path === '/register') {
    return { name: 'register', params: {}, hash };
  }
  if (path === '/dashboard') {
    return { name: 'dashboard', params: {}, hash };
  }
  if (path === '/orchestration') {
    return { name: 'orchestration', params: {}, hash };
  }
  if (path === '/jobs') {
    return { name: 'jobs', params: {}, hash };
  }
  // /fleet is canonical; /agents is a bookmark-friendly alias that
  // resolves to the same Agent Fleet view.
  if (path === '/fleet' || path === '/agents') {
    return { name: 'fleet', params: {}, hash };
  }
  if (path === '/sessions') {
    return { name: 'sessions', params: {}, hash };
  }

  // Parameterised: /jobs/:id
  const jobDetailMatch = /^\/jobs\/([^/]+)/.exec(path);
  if (jobDetailMatch !== null) {
    return {
      name: 'job-detail',
      params: { id: decodeURIComponent(jobDetailMatch[1] ?? '') },
      hash,
    };
  }

  // Parameterised: /agents/:id
  const agentDetailMatch = /^\/agents\/([^/]+)/.exec(path);
  if (agentDetailMatch !== null) {
    return {
      name: 'agent-detail',
      params: { id: decodeURIComponent(agentDetailMatch[1] ?? '') },
      hash,
    };
  }
  // Parameterised: /tasks/:id. A bare trailing slash (`/tasks/`) names no
  // task and resolves to the list, not to a detail with an empty id.
  const taskDetailMatch = /^\/tasks\/([^/]+)/.exec(path);
  if (taskDetailMatch !== null) {
    return {
      name: 'task-detail',
      params: { id: decodeURIComponent(taskDetailMatch[1] ?? '') },
      hash,
    };
  }
  if (path === '/tasks' || path === '/tasks/') {
    return { name: 'tasks', params: {}, hash };
  }
  if (path === '/workflows') {
    return { name: 'workflows', params: {}, hash };
  }
  // /events is canonical; /live-events is a bookmark-friendly alias
  // that resolves to the same Live Events stream view.
  if (path === '/events' || path === '/live-events') {
    return { name: 'events', params: {}, hash };
  }
  // /cost is canonical; /cost-tracking is a bookmark-friendly alias that
  // matches the sidebar label so a direct URL also resolves to the view.
  if (path === '/cost' || path === '/cost-tracking') {
    return { name: 'cost', params: {}, hash };
  }
  if (path === '/kv') {
    return { name: 'kv', params: {}, hash };
  }
  if (path === '/vectors') {
    return { name: 'vectors', params: {}, hash };
  }
  if (path === '/timeseries') {
    return { name: 'timeseries', params: {}, hash };
  }
  if (path === '/objects') {
    return { name: 'objects', params: {}, hash };
  }
  // /iam/:tab deep-links directly into a named IAM tab.
  // Supported tab names: users, tenants, roles, policies.
  const iamTabMatch = /^\/iam\/(users|tenants|roles|policies)$/.exec(path);
  if (iamTabMatch !== null) {
    return {
      name: 'iam',
      params: { tab: iamTabMatch[1] ?? 'users' },
      hash,
    };
  }
  if (path === '/iam') {
    return { name: 'iam', params: {}, hash };
  }
  if (path === '/kms') {
    return { name: 'kms', params: {}, hash };
  }
  if (path === '/secrets') {
    return { name: 'secrets', params: {}, hash };
  }
  if (path === '/certificates') {
    return { name: 'certificates', params: {}, hash };
  }
  if (path === '/trust') {
    return { name: 'trust', params: {}, hash };
  }
  if (path === '/crypto') {
    return { name: 'crypto', params: {}, hash };
  }
  if (path === '/dek') {
    return { name: 'dek', params: {}, hash };
  }
  if (path === '/dynamic-secrets') {
    return { name: 'dynamic-secrets', params: {}, hash };
  }
  if (path === '/barrier' || path === '/seal') {
    // /seal is a legacy alias kept so existing bookmarks resolve to the
    // renamed Barrier surface without a hard 404.
    return { name: 'barrier', params: {}, hash };
  }
  if (path === '/tenant-barrier') {
    return { name: 'tenant-barrier', params: {}, hash };
  }
  if (path === '/ceremony') {
    return { name: 'ceremony', params: {}, hash };
  }
  if (path === '/bootstrap') {
    return { name: 'bootstrap', params: {}, hash };
  }
  // /ssh-keys is canonical; /sshkeys is a bookmark-friendly alias for
  // operators who type the path without a hyphen.
  if (path === '/ssh-keys' || path === '/sshkeys') {
    return { name: 'ssh-keys', params: {}, hash };
  }
  // AI/ML
  if (path === '/rag') {
    return { name: 'rag', params: {}, hash };
  }
  if (path === '/embeddings') {
    return { name: 'embeddings', params: {}, hash };
  }
  // ASYNC
  if (path === '/messaging') {
    return { name: 'messaging', params: {}, hash };
  }
  if (path === '/scheduler') {
    return { name: 'scheduler', params: {}, hash };
  }
  if (path === '/inference') {
    return { name: 'inference', params: {}, hash };
  }
  if (path === '/projects') {
    return { name: 'projects', params: {}, hash };
  }
  if (path === '/accounts') {
    return { name: 'accounts', params: {}, hash };
  }
  if (path === '/cluster') {
    return { name: 'cluster', params: {}, hash };
  }

  // Parameterised: /sessions/:id
  const sessionDetailMatch = /^\/sessions\/([^/]+)/.exec(path);
  if (sessionDetailMatch !== null) {
    return {
      name: 'session-detail',
      params: { id: decodeURIComponent(sessionDetailMatch[1] ?? '') },
      hash,
    };
  }

  // Parameterised: /invite/:tenant/:token
  //
  // THE TENANT IS PART OF THE LINK because the invite API is tenant-scoped by
  // design: a token issued for one tenant is absent from another's store, so
  // isolation is structural rather than a check. A browser opening an
  // activation link knows nothing but what the link carries, so the link has to
  // carry both. The tenant-less shape called routes that were never registered
  // and the flow had never worked from a browser.
  const inviteMatch = /^\/invite\/([^/]+)\/([^/]+)/.exec(path);
  if (inviteMatch !== null) {
    return {
      name: 'invite-activate',
      params: {
        tenant: decodeURIComponent(inviteMatch[1] ?? ''),
        token: decodeURIComponent(inviteMatch[2] ?? ''),
      },
      hash,
    };
  }

  return { name: 'not-found', params: {}, hash };
}

// Reactive routerStore using the module-scope `let x = $state(...)` + getter
// pattern. This is the only Svelte 5 cross-module reactivity shape that
// reliably propagates template updates in this Vite/Svelte 5 build (5.55.x):
// every other store in this app (sessionStore, connectionStore, toastStore)
// uses the same shape. Earlier attempts to export a $state proxy directly
// (`export const routerStore = $state({...})`) silently broke template
// reactivity — the proxy was mutated correctly but template consumers in
// other modules never re-rendered.
const initial = parseHash(typeof window !== 'undefined' ? window.location.hash : '#/dashboard');
let _name = $state<RouteName>(initial.name);
let _params = $state<Record<string, string>>(initial.params);
let _hash = $state<string>(initial.hash);

export const routerStore = {
	get name(): RouteName {
		return _name;
	},
	get params(): Record<string, string> {
		return _params;
	},
	get hash(): string {
		return _hash;
	},
};

if (typeof window !== 'undefined') {
	window.addEventListener('hashchange', (event: HashChangeEvent) => {
		// The hash being LEFT is the one a list's breadcrumb wants back, and
		// the event is the only place it still exists — see listReturns.
		const leaving = event.oldURL.indexOf('#');
		if (leaving !== -1) rememberList(event.oldURL.slice(leaving));
		const next = parseHash(window.location.hash);
		_name = next.name;
		_params = next.params;
		_hash = next.hash;
	});
}

// ---------------------------------------------------------------------------
// Navigation helpers
// ---------------------------------------------------------------------------

/** Navigate to a hash path, e.g. navigateTo('/sessions'). */
export function navigateTo(path: string): void {
  const hash = path.startsWith('#') ? path : `#${path}`;
  if (typeof window !== 'undefined') {
    window.location.hash = hash;
  }
}

// ---------------------------------------------------------------------------
// Going back to the list you came from
// ---------------------------------------------------------------------------

/**
 * The last hash seen for each list path — `/tasks` as
 * `#/tasks?tasks_status=blocked&tasks_page=3`, not as `#/tasks`.
 *
 * WHY THIS IS NOT `history.back()`. A detail page is reached by a link as
 * often as by a click on a row: from a job's task, from an agent's sessions,
 * from a pasted address. Back would then leave the app, or walk into whatever
 * the operator was doing before — while "Back to Tasks" has one meaning, and
 * it is the list they were narrowing.
 *
 * It is not read off the route either. A table records its page, sort, search
 * and filters with replaceState, precisely so paging is not navigation; by
 * the time a detail view asks, the route object is the DETAIL's. The old hash
 * is captured from the hashchange event, which still carries it.
 */
const listReturns = new Map<string, string>();

/** listPath is the route path a hash names, without its query. */
function listPath(hash: string): string {
  const bare = hash.replace(/^#/, '');
  const queryAt = bare.indexOf('?');
  return queryAt === -1 ? bare : bare.slice(0, queryAt);
}

/** rememberList records a hash as the return address for its own path. */
function rememberList(hash: string): void {
  const path = listPath(hash);
  if (path === '') return;
  listReturns.set(path, hash.startsWith('#') ? hash : `#${hash}`);
}

/**
 * listReturnHash is where a detail view's breadcrumb goes: the narrowed list
 * the operator left, or the bare path when they never saw it.
 */
export function listReturnHash(path: string): string {
  return listReturns.get(path) ?? `#${path}`;
}

/** forgetListReturns drops the remembered addresses — the hook a test uses. */
export function forgetListReturns(): void {
  listReturns.clear();
}

export function routeDashboard(): void { navigateTo('/dashboard'); }
export function routeOrchestration(): void { navigateTo('/orchestration'); }
export function routeJobs(): void { navigateTo('/jobs'); }
export function routeSessions(): void { navigateTo('/sessions'); }
export function routeFleet(): void { navigateTo('/fleet'); }
/**
 * routeTasks opens the task list, narrowed to one project when an id is given.
 *
 * THE PARAMETER IS THE TABLE'S OWN, not a second filter grammar beside it.
 * `tasks_project` is what the Tasks table reads its project facet from
 * (FilterBar, lib/filters.ts), so a link built here and a link built by
 * clicking the chip are the same link — and the × on the chip clears it.
 */
export function routeTasks(projectId?: string): void {
  if (projectId) {
    navigateTo(`/tasks?tasks_project=${encodeURIComponent(projectId)}`);
    return;
  }
  navigateTo('/tasks');
}
export function routeTaskDetail(id: string): void {
  navigateTo(`/tasks/${encodeURIComponent(id)}`);
}
export function routeWorkflows(): void { navigateTo('/workflows'); }
export function routeEvents(): void { navigateTo('/events'); }
export function routeCost(): void { navigateTo('/cost'); }
export function routeLogin(): void { navigateTo('/login'); }
export function routeRegister(): void { navigateTo('/register'); }
export function routeKV(): void { navigateTo('/kv'); }
export function routeVectors(): void { navigateTo('/vectors'); }
export function routeTimeSeries(): void { navigateTo('/timeseries'); }
export function routeObjects(): void { navigateTo('/objects'); }
export function routeIAM(): void { navigateTo('/iam'); }
export function routeIAMTab(tab: 'users' | 'tenants' | 'roles' | 'policies'): void {
  navigateTo(`/iam/${tab}`);
}
export function routeKMS(): void { navigateTo('/kms'); }
export function routeSecrets(): void { navigateTo('/secrets'); }
export function routeCertificates(): void { navigateTo('/certificates'); }
export function routeTrust(): void { navigateTo('/trust'); }
export function routeCrypto(): void { navigateTo('/crypto'); }
export function routeDEK(): void { navigateTo('/dek'); }
export function routeDynamicSecrets(): void { navigateTo('/dynamic-secrets'); }
export function routeBarrier(): void { navigateTo('/barrier'); }
export function routeTenantBarrier(): void { navigateTo('/tenant-barrier'); }
export function routeCeremony(): void { navigateTo('/ceremony'); }
export function routeBootstrap(): void { navigateTo('/bootstrap'); }
/** @deprecated Use routeBarrier — /seal is the legacy hash. */
export function routeSeal(): void { routeBarrier(); }
export function routeSSHKeys(): void { navigateTo('/ssh-keys'); }
// AI/ML
export function routeRAG(): void { navigateTo('/rag'); }
export function routeEmbeddings(): void { navigateTo('/embeddings'); }
// ASYNC
export function routeMessaging(): void { navigateTo('/messaging'); }
export function routeScheduler(): void { navigateTo('/scheduler'); }
export function routeInference(): void { navigateTo('/inference'); }
export function routeProjects(): void { navigateTo('/projects'); }
export function routeAccounts(): void { navigateTo('/accounts'); }
export function routeSessionDetail(id: string): void {
  navigateTo(`/sessions/${encodeURIComponent(id)}`);
}
export function routeJobDetail(id: string): void {
  navigateTo(`/jobs/${encodeURIComponent(id)}`);
}
export function routeAgentDetail(id: string): void {
  navigateTo(`/agents/${encodeURIComponent(id)}`);
}
// routeInviteActivate is DELETED, not repaired (found while fixing #532's
// gates). It built `/invite/{token}` — the tenant-less shape 1f95595c removed,
// because the invite API is tenant-scoped and a browser knows only what the
// link carries. The router has required `/invite/{tenant}/{token}` ever since,
// so the helper produced a link that resolves to not-found. It had no callers,
// and a broken affordance with no users is removed rather than left for the
// next person to reach for. Build the link with navigateTo and both segments.
