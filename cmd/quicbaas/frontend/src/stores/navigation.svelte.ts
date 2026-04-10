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
 * Tracks the active view and sidebar collapse state.
 */

export type ViewID =
  | 'dashboard'
  | 'jobs'
  | 'agents'
  | 'sessions'
  | 'tasks'
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
  | 'seal'
  | 'cluster'
  | 'nodes'
  | 'backup'
  | 'connection'
  | 'theme';

export interface NavState {
  activeView: ViewID;
  sidebarCollapsed: boolean;
}

let activeView: ViewID = $state('dashboard');
let sidebarCollapsed = $state(false);

export const navStore = {
  get activeView() { return activeView; },
  get sidebarCollapsed() { return sidebarCollapsed; },
};

export function navigate(view: ViewID): void {
  activeView = view;
}

export function toggleSidebar(): void {
  sidebarCollapsed = !sidebarCollapsed;
}
