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
 * Navigation state store.
 *
 * Sidebar collapse state lives here. The active view is derived from the
 * hash router (router.svelte.ts) so that the URL is always the source of
 * truth and deep-links work correctly.
 */

import { navigateTo, routerStore, type RouteName } from './router.svelte.ts';

export type ViewID =
  | 'dashboard'
  | 'attention'
  | 'orchestration'
  | 'jobs'
  | 'fleet'
  | 'sessions'
  | 'tasks'
  | 'workflows'
  | 'events'
  | 'costs'
  | 'kv'
  | 'vectors'
  | 'timeseries'
  | 'objects'
  | 'rag'
  | 'embeddings'
  | 'messaging'
  | 'scheduler'
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
  | 'my-sessions'
  | 'attestation'
  | 'oauth-apps'
  | 'identity-providers'
  | 'cluster'
  | 'nodes'
  | 'backup'
  | 'connection'
  | 'theme'
  | 'inference'
  | 'projects'
  | 'accounts';

/** Map each sidebar ViewID to its canonical hash path. */
const viewHashPaths: Partial<Record<ViewID, string>> = {
  dashboard:     '/dashboard',
  attention:     '/attention',
  orchestration: '/orchestration',
  jobs:          '/jobs',
  fleet:         '/fleet',
  sessions:      '/sessions',
  tasks:         '/tasks',
  workflows:     '/workflows',
  events:        '/events',
  costs:         '/cost',
  kv:            '/kv',
  vectors:       '/vectors',
  timeseries:    '/timeseries',
  objects:       '/objects',
  // AI/ML
  rag:           '/rag',
  embeddings:    '/embeddings',
  // ASYNC
  messaging:     '/messaging',
  scheduler:     '/scheduler',
  // SECURITY
  iam:           '/iam',
  kms:           '/kms',
  secrets:       '/secrets',
  certificates:  '/certificates',
  trust:         '/trust',
  crypto:        '/crypto',
  dek:           '/dek',
  'dynamic-secrets': '/dynamic-secrets',
  barrier:       '/barrier',
  'tenant-barrier': '/tenant-barrier',
  ceremony:      '/ceremony',
  bootstrap:     '/bootstrap',
  'ssh-keys':    '/ssh-keys',
  'my-sessions': '/my-sessions',
  attestation:   '/attestation',
  'oauth-apps':  '/oauth-apps',
  'identity-providers': '/identity-providers',
  // ORCHESTRATION — inference
  inference:     '/inference',
  // ORCHESTRATION — projects
  projects:      '/projects',
  // ORCHESTRATION — pooled provider accounts
  accounts:      '/accounts',
  // CLUSTER
  cluster:       '/cluster',
};

/**
 * Map each RouteName back to the ViewID it represents in the sidebar so the
 * active highlight stays in sync when the hash changes externally.
 */
const routeToViewID: Partial<Record<RouteName, ViewID>> = {
  dashboard:        'dashboard',
  attention:        'attention',
  orchestration:    'orchestration',
  jobs:             'jobs',
  fleet:            'fleet',
  'fleet-node':     'fleet',
  sessions:         'sessions',
  'session-detail': 'sessions',
  tasks:            'tasks',
  'task-detail':    'tasks',
  workflows:        'workflows',
  events:           'events',
  cost:             'costs',
  kv:               'kv',
  vectors:          'vectors',
  timeseries:       'timeseries',
  objects:          'objects',
  // AI/ML
  rag:              'rag',
  embeddings:       'embeddings',
  // ASYNC
  messaging:        'messaging',
  scheduler:        'scheduler',
  // SECURITY
  iam:              'iam',
  kms:              'kms',
  secrets:          'secrets',
  certificates:     'certificates',
  trust:            'trust',
  crypto:           'crypto',
  dek:              'dek',
  'dynamic-secrets': 'dynamic-secrets',
  barrier:          'barrier',
  'tenant-barrier': 'tenant-barrier',
  ceremony:         'ceremony',
  bootstrap:        'bootstrap',
  'ssh-keys':       'ssh-keys',
  'my-sessions':    'my-sessions',
  attestation:      'attestation',
  'oauth-apps':     'oauth-apps',
  'identity-providers': 'identity-providers',
  // ORCHESTRATION — inference
  inference:        'inference',
  // ORCHESTRATION — projects
  projects:         'projects',
  // ORCHESTRATION — pooled provider accounts
  accounts:         'accounts',
  // CLUSTER
  cluster:          'cluster',
};

export interface NavState {
  readonly activeView: ViewID;
  readonly sidebarCollapsed: boolean;
}

let sidebarCollapsed = $state(false);

export const navStore: NavState = {
  /** Active view derived from the router — URL is always the source of truth. */
  get activeView(): ViewID {
    return routeToViewID[routerStore.name] ?? 'dashboard';
  },
  get sidebarCollapsed() { return sidebarCollapsed; },
};

/**
 * Navigate to a view by setting window.location.hash. Views that don't have
 * a hash route yet fall through gracefully.
 */
export function navigate(view: ViewID): void {
  const path = viewHashPaths[view];
  if (path !== undefined) {
    navigateTo(path);
  }
}

export function toggleSidebar(): void {
  sidebarCollapsed = !sidebarCollapsed;
}
