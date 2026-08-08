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

import { mount } from 'svelte';
import App from './App.svelte';
import { showToast } from './stores/toast.svelte.ts';
import { installAuthFetch } from './stores/authFetch.ts';
import './app.css';

// ---------------------------------------------------------------------------
// Session recovery.
//
// Installed before the app mounts so the very first request already benefits:
// a 401 caused by a lapsed 15-minute access token renews the session and
// replays the request instead of leaving the operator on a dead page.
// ---------------------------------------------------------------------------

installAuthFetch();

// ---------------------------------------------------------------------------
// Global error boundary.
//
// Unhandled errors and promise rejections that escape component try-catch
// blocks are caught here. Surfacing them as toasts prevents the UI from
// silently entering a broken state and ensures the operator sees actionable
// error messages.
//
// Neither handler suppresses the error — both re-surface it so browser
// devtools still receive the full stack trace.
// ---------------------------------------------------------------------------

window.onerror = (
  message: string | Event,
  _source?: string,
  _lineno?: number,
  _colno?: number,
  error?: Error,
): boolean => {
  const msg =
    error instanceof Error
      ? error.message
      : typeof message === 'string'
        ? message
        : 'Unexpected error';
  // Do not suppress — return false lets the browser report the error normally.
  showToast({ type: 'error', message: msg });
  return false;
};

window.addEventListener('unhandledrejection', (ev: PromiseRejectionEvent) => {
  const reason = ev.reason;
  const msg =
    reason instanceof Error
      ? reason.message
      : typeof reason === 'string'
        ? reason
        : 'Unexpected error';
  showToast({ type: 'error', message: msg });
});

const app = mount(App, { target: document.getElementById('app')! });

export default app;
