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
 * Theme state store.
 *
 * Manages the active DaisyUI theme with localStorage persistence.
 * Maps between logical "dark"/"light" values and QuicBaaS theme names.
 */

export type Theme = 'dark' | 'light';

export interface ThemeState {
  theme: Theme;
}

const STORAGE_KEY = 'quicbaas-theme';
const DARK_THEME = 'quicbaas-dark';
const LIGHT_THEME = 'quicbaas-light';

export const themeStore: ThemeState = $state({ theme: 'dark' });

function isValidTheme(value: string): value is Theme {
  return value === 'dark' || value === 'light';
}

function applyTheme(theme: Theme): void {
  if (typeof document !== 'undefined') {
    document.documentElement.dataset['theme'] = theme === 'dark' ? DARK_THEME : LIGHT_THEME;
  }
}

function persistTheme(theme: Theme): void {
  if (typeof localStorage !== 'undefined') {
    localStorage.setItem(STORAGE_KEY, theme);
  }
}

export function toggleTheme(): void {
  const next: Theme = themeStore.theme === 'dark' ? 'light' : 'dark';
  themeStore.theme = next;
  persistTheme(next);
  applyTheme(next);
}

export function initTheme(): void {
  let theme: Theme = 'dark';

  if (typeof localStorage !== 'undefined') {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored !== null && isValidTheme(stored)) {
      theme = stored;
    } else if (typeof window !== 'undefined' && window.matchMedia('(prefers-color-scheme: light)').matches) {
      theme = 'light';
    }
  }

  themeStore.theme = theme;
  applyTheme(theme);
}
