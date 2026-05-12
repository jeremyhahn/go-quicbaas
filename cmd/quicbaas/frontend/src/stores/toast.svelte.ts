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
 * Toast notification state store.
 *
 * Queue-based toast system with auto-dismiss and a max-visible cap.
 */

export type ToastType = 'success' | 'error' | 'info' | 'warning';

export interface Toast {
  readonly id: number;
  readonly type: ToastType;
  readonly message: string;
  readonly duration: number;
}

export interface ToastState {
  toasts: Toast[];
}

const MAX_VISIBLE = 3;
const DEFAULT_DURATION_MS = 5000;

let nextId = 0;
const timers = new Map<number, ReturnType<typeof setTimeout>>();

export const toastStore: ToastState = $state({ toasts: [] });

// showToast accepts either positional args (type, message, duration) or a
// single options object { type, message, duration }. Both forms are supported
// because view components evolved with different calling conventions.
export function showToast(
  typeOrOpts: ToastType | { type: ToastType; message: string; duration?: number },
  message?: string,
  duration?: number,
): void {
  let resolvedType: ToastType;
  let resolvedMessage: string;
  let resolvedDuration: number | undefined;
  if (typeof typeOrOpts === 'object') {
    resolvedType = typeOrOpts.type;
    resolvedMessage = typeOrOpts.message;
    resolvedDuration = typeOrOpts.duration;
  } else {
    resolvedType = typeOrOpts;
    resolvedMessage = message ?? '';
    resolvedDuration = duration;
  }
  return _showToast(resolvedType, resolvedMessage, resolvedDuration);
}

function _showToast(type: ToastType, message: string, duration?: number): void {
  const id = nextId++;
  const resolvedDuration = (duration ?? DEFAULT_DURATION_MS);

  const toast: Toast = { id, type, message, duration: resolvedDuration };

  // Enforce max visible cap by dropping oldest if at limit.
  const current = toastStore.toasts;
  const trimmed = current.length >= MAX_VISIBLE ? current.slice(-(MAX_VISIBLE - 1)) : current;
  toastStore.toasts = [...trimmed, toast];

  const timer = setTimeout(() => dismissToast(id), resolvedDuration);
  timers.set(id, timer);
}

export function dismissToast(id: number): void {
  const timer = timers.get(id);
  if (timer !== undefined) {
    clearTimeout(timer);
    timers.delete(id);
  }
  toastStore.toasts = toastStore.toasts.filter((t) => t.id !== id);
}
