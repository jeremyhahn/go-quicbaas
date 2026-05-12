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
 *   #/barrier        → Barrier (encryption barrier; replaces #/seal)
 *   #/ceremony       → Multi-officer key ceremony
 *   #/bootstrap      → First-contact trust bootstrap wizard
 *   #/ssh-keys       → SSH Keys
 *   #/sshkeys        → bookmark-friendly alias for /ssh-keys
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
  | 'barrier'
  | 'ceremony'
  | 'bootstrap'
  | 'ssh-keys'
  // ORCHESTRATION — inference
  | 'inference'
  // ORCHESTRATION — projects
  | 'projects'
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

function parseHash(hash: string): Route {
  const path = hash.replace(/^#/, '') || '/';

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
  if (path === '/tasks') {
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
  if (path === '/barrier' || path === '/seal') {
    // /seal is a legacy alias kept so existing bookmarks resolve to the
    // renamed Barrier surface without a hard 404.
    return { name: 'barrier', params: {}, hash };
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

  // Parameterised: /invite/:token
  const inviteMatch = /^\/invite\/([^/]+)/.exec(path);
  if (inviteMatch !== null) {
    return {
      name: 'invite-activate',
      params: { token: decodeURIComponent(inviteMatch[1] ?? '') },
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
	window.addEventListener('hashchange', () => {
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

export function routeDashboard(): void { navigateTo('/dashboard'); }
export function routeOrchestration(): void { navigateTo('/orchestration'); }
export function routeJobs(): void { navigateTo('/jobs'); }
export function routeSessions(): void { navigateTo('/sessions'); }
export function routeFleet(): void { navigateTo('/fleet'); }
export function routeTasks(): void { navigateTo('/tasks'); }
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
export function routeBarrier(): void { navigateTo('/barrier'); }
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
export function routeSessionDetail(id: string): void {
  navigateTo(`/sessions/${encodeURIComponent(id)}`);
}
export function routeJobDetail(id: string): void {
  navigateTo(`/jobs/${encodeURIComponent(id)}`);
}
export function routeAgentDetail(id: string): void {
  navigateTo(`/agents/${encodeURIComponent(id)}`);
}
export function routeInviteActivate(token: string): void {
  navigateTo(`/invite/${encodeURIComponent(token)}`);
}
